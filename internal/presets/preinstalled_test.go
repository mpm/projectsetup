package presets

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

var sharedPreset = strings.Replace(minimalPreset, "schema = 1", "schema = 2", 1)

const installedNode = `[image.preinstalled.tools.node]
version = "22.14.0"
executable = "/opt/node/bin/node"
path = ["/opt/node/bin", "/usr/bin"]
`

func TestPreinstalledRequiresFixedBase(t *testing.T) {
	for _, base := range []string{"example:${option:flavor}", "example:${project:name}", "example:$TAG", "example:`tag`"} {
		for _, contract := range []string{installedNode, "[image.preinstalled]\ncore_packages = ['git']\n", "[image.preinstalled]\n", ""} {
			t.Run(base+"/"+contract, func(t *testing.T) {
				definition := mustParse(t, sharedPreset+contract)
				definition.Image.Base = base
				definition.Options = map[string]Option{"flavor": {Default: "full", Choices: []string{"full"}}}
				_, err := NewRegistry(definition)
				claims := contract == installedNode || strings.Contains(contract, "core_packages")
				if !claims && err != nil {
					t.Fatal(err)
				}
				if claims && (err == nil || !strings.Contains(err.Error(), "image.base must be literal")) {
					t.Fatalf("registry = %v", err)
				}
			})
		}
	}
}

func TestPreinstalledParserAndEditorSchema(t *testing.T) {
	schema := loadSchema(t)
	tests := []struct{ name, source, want string }{
		{"legacy absent", minimalPreset, ""},
		{"new absent", sharedPreset, ""},
		{"empty", sharedPreset + "[image.preinstalled]\n", ""},
		{"empty members", sharedPreset + "[image.preinstalled]\ncore_packages = []\ntools = {}\n", ""},
		{"tools", sharedPreset + installedNode, ""},
		{"core only", sharedPreset + "[image.preinstalled]\ncore_packages = ['bash', 'ca-certificates', 'curl', 'git', 'gnupg', 'sudo']\n", ""},
		{"prerelease build", sharedPreset + strings.Replace(installedNode, "22.14.0", "22.14.0-rc.1+build.2", 1), ""},
		{"custom tool and shim", sharedPreset + "[image.preinstalled.tools.my-cli]\nversion = 'release_2026.10'\nexecutable = '/opt/shims/command'\npath = ['/opt/shims']\n", ""},
		{"legacy empty forbidden", minimalPreset + "[image.preinstalled]\n", "requires schema 2"},
		{"unsupported schema", strings.Replace(sharedPreset, "schema = 2", "schema = 3", 1), "schema is 3"},
		{"missing schema", strings.Replace(sharedPreset, "schema = 2", "", 1), "schema is 0"},
		{"addon empty forbidden", "schema = 2\nkind = 'addon'\nname = 'extra'\nversion = '1.0.0'\ndescription = 'extra'\n[image.preinstalled]\n", "only be set by a preset"},
		{"variant empty forbidden", sharedPreset + "[[variant]]\nwhen = { preset = ['base'] }\n[variant.image.preinstalled]\n", "cannot be set in a variant"},
		{"unknown contract field", sharedPreset + "[image.preinstalled]\nskip_install = true\n", "unknown fields"},
		{"unknown tool field", sharedPreset + installedNode + "env = {}\n", "unknown fields"},
		{"unknown core package", sharedPreset + "[image.preinstalled]\ncore_packages = ['make']\n", "unknown core package"},
		{"duplicate core package", sharedPreset + "[image.preinstalled]\ncore_packages = ['git', 'git']\n", "duplicate core package"},
		{"bad tool key", sharedPreset + strings.Replace(installedNode, "tools.node", "tools.Node", 1), "tool name must match"},
		{"AI ownership", sharedPreset + strings.Replace(installedNode, "tools.node", "tools.opencode", 1), "AI tools are managed"},
		{"missing version", sharedPreset + strings.Replace(installedNode, "version = \"22.14.0\"\n", "", 1), "literal concrete release"},
		{"missing executable", sharedPreset + strings.Replace(installedNode, "executable = \"/opt/node/bin/node\"\n", "", 1), "absolute Linux executable"},
		{"missing path", sharedPreset + strings.Replace(installedNode, "path = [\"/opt/node/bin\", \"/usr/bin\"]\n", "", 1), "at least one directory"},
		{"short runtime version", sharedPreset + strings.Replace(installedNode, "22.14.0", "22.14", 1), "MAJOR.MINOR.PATCH"},
		{"moving version", sharedPreset + strings.Replace(installedNode, "22.14.0", "latest", 1), "literal concrete release"},
		{"option expansion", sharedPreset + strings.Replace(installedNode, "22.14.0", "${option:version}", 1), "literal concrete release"},
		{"shell expansion", sharedPreset + strings.Replace(installedNode, "/opt/node/bin/node", "$HOME/node", 1), "absolute Linux executable"},
	}
	for _, name := range []string{"node", "ruby", "python", "go", "rust", "gh"} {
		tests = append(tests, struct{ name, source, want string }{name + " concrete version", sharedPreset + strings.Replace(installedNode, "tools.node", "tools."+name, 1), ""})
	}
	for _, value := range []string{"lts", "stable", "nightly", "LATEST"} {
		tests = append(tests, struct{ name, source, want string }{"custom moving " + value, sharedPreset + strings.Replace(strings.Replace(installedNode, "tools.node", "tools.custom", 1), "22.14.0", value, 1), "literal concrete release"})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.source), "claims.toml")
			var document map[string]any
			if decodeErr := toml.Unmarshal([]byte(tt.source), &document); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			problems := validateSchema(schema, schema, document, "claims")
			if tt.want == "" {
				if err != nil || len(problems) > 0 {
					t.Fatalf("parser = %v; schema = %v", err, problems)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), "claims.toml") {
					t.Fatalf("error = %v, want %q", err, tt.want)
				}
				if len(problems) == 0 {
					t.Fatal("editor schema accepted invalid declaration")
				}
			}
		})
	}
}

func TestPreinstalledPathValidation(t *testing.T) {
	for _, entry := range []string{"relative", "/opt//bin", "/opt/../bin", "/opt/bin/", "/opt/bin:bad", "/opt/a b", "/opt/${HOME}", "/opt/$(pwd)", "/opt/`pwd`", "/opt/~user", "/opt/\\bin", "/opt/\x01bin", "/opt/\u0085bin"} {
		t.Run(fmt.Sprintf("%q", entry), func(t *testing.T) {
			definition, err := Parse([]byte(sharedPreset+installedNode), "base.toml")
			if err != nil {
				t.Fatal(err)
			}
			tool := definition.Image.Preinstalled.Tools["node"]
			tool.Path = []string{entry, "/opt/node/bin"}
			definition.Image.Preinstalled.Tools["node"] = tool
			if _, err := NewRegistry(definition); err == nil || !strings.Contains(err.Error(), "tools.node.path") {
				t.Fatalf("error = %v", err)
			}
			tool.Executable = entry
			tool.Path = []string{"/opt/node/bin"}
			definition.Image.Preinstalled.Tools["node"] = tool
			if _, err := NewRegistry(definition); err == nil || !strings.Contains(err.Error(), "tools.node.executable") {
				t.Fatalf("error = %v", err)
			}
		})
	}
	source := strings.Replace(installedNode, `path = ["/opt/node/bin", "/usr/bin"]`, `path = ["/usr/bin"]`, 1)
	if _, err := Parse([]byte(sharedPreset+source), "base.toml"); err == nil || !strings.Contains(err.Error(), "parent directory") {
		t.Fatalf("error = %v", err)
	}
}

func TestPreinstalledResolveOrderingAndIsolation(t *testing.T) {
	preset := mustParse(t, sharedPreset+installedNode+`[image.preinstalled.tools.aaa]
version = "release-1"
executable = "/usr/bin/aaa"
path = ["/usr/bin", "/opt/common"]
[container]
path = ["/project/bin"]
`)
	addon := mustParse(t, "schema = 1\nkind = 'addon'\nname = 'extra'\nversion = '1.0.0'\ndescription = 'extra'\n[image]\napt = ['git']\n[container]\npath = ['/addon/bin']\n")
	registry, err := NewRegistry(preset, addon)
	if err != nil {
		t.Fatal(err)
	}
	selection := Selection{Preset: "base", Addons: []string{"extra"}}
	resolved, err := registry.Resolve(selection)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resolved.PreinstalledPath, []string{"/usr/bin", "/opt/common", "/opt/node/bin"}) || !reflect.DeepEqual(resolved.Path, []string{"/project/bin", "/addon/bin"}) || !reflect.DeepEqual(resolved.Apt, [][]string{{"git"}}) {
		t.Fatalf("resolved = %+v", resolved)
	}
	tool := resolved.Preinstalled.Tools["aaa"]
	tool.Path[0] = "/changed"
	resolved.Preinstalled.Tools["aaa"] = tool
	again, err := registry.Resolve(selection)
	if err != nil || again.Preinstalled.Tools["aaa"].Path[0] != "/usr/bin" {
		t.Fatalf("resolution mutated registry: %+v, %v", again, err)
	}
}

func TestPreinstalledInstallerConflicts(t *testing.T) {
	for _, pair := range []struct{ tool, repository string }{
		{"node", "ghcr.io/devcontainers/features/node"}, {"python", "ghcr.io/devcontainers/features/python"},
		{"go", "ghcr.io/devcontainers/features/go"}, {"rust", "ghcr.io/devcontainers/features/rust"},
		{"gh", "ghcr.io/devcontainers/features/github-cli"}, {"ruby", "ghcr.io/rails/devcontainer/features/ruby"},
	} {
		for _, suffix := range []string{":1", ":99", "@sha256:abc", ":1@sha256:abc", ""} {
			for _, location := range []string{"preset", "addon", "variant", "inactive variant"} {
				t.Run(pair.tool+suffix+"/"+location, func(t *testing.T) {
					claims := strings.Replace(installedNode, "tools.node", "tools."+pair.tool, 1)
					feature := fmt.Sprintf("[features.%q]\nversion = '22.14.0'\n", pair.repository+suffix)
					presetSource := sharedPreset + claims
					addonSource := "schema = 1\nkind = 'addon'\nname = 'extra'\nversion = '1.0.0'\ndescription = 'extra'\n"
					switch location {
					case "preset":
						presetSource += feature
					case "addon":
						addonSource += feature
					default:
						match := "base"
						if location == "inactive variant" {
							match = "other"
						}
						addonSource += "[[variant]]\nwhen = { preset = ['" + match + "'] }\n" + strings.Replace(feature, "[features.", "[variant.features.", 1)
					}
					registry, err := NewRegistry(mustParse(t, presetSource), mustParse(t, addonSource))
					if err != nil {
						t.Fatal(err)
					}
					_, err = registry.Resolve(Selection{Preset: "base", Addons: []string{"extra"}})
					if location == "inactive variant" {
						if err != nil {
							t.Fatal(err)
						}
						return
					}
					if err == nil || !strings.Contains(err.Error(), pair.tool) || !strings.Contains(err.Error(), pair.repository+suffix) || !strings.Contains(err.Error(), "definition") {
						t.Fatalf("error = %v", err)
					}
				})
			}
		}
	}
	// Similar and opaque feature IDs are not inferred to install a capability.
	for _, feature := range []string{"example/node:1", "ghcr.io/devcontainers/features/node-extra:1"} {
		registry, err := NewRegistry(mustParse(t, sharedPreset+installedNode+fmt.Sprintf("[features.%q]\n", feature)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := registry.Resolve(Selection{Preset: "base"}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPreinstalledAggregatesFailures(t *testing.T) {
	_, err := Parse([]byte(sharedPreset+"[image.preinstalled]\ncore_packages = ['unknown', 'git', 'git']\n[image.preinstalled.tools.node]\nversion = '22'\nexecutable = 'relative'\npath = []\n"), "broken.toml")
	for _, want := range []string{"broken.toml", "unknown core package", "duplicate core package", ".version", ".executable", ".path"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want %q", err, want)
		}
	}
}
