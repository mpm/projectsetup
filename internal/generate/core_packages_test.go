package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/presets"
	"github.com/mpm/projectsetup/internal/validate"
)

const allCoreClaims = `core_packages = ["sudo", "git", "curl", "bash", "gnupg", "ca-certificates"]`

func TestCorePackageInstallation(t *testing.T) {
	for _, tt := range []struct {
		name, claims, apt, want string
		system                  []string
	}{
		{"absent", "", "", "bash ca-certificates curl git gnupg sudo", nil},
		{"empty", "core_packages = []", "", "bash ca-certificates curl git gnupg sudo", nil},
		{"partial", `core_packages = ["git", "bash", "curl"]`, "", "ca-certificates gnupg sudo", nil},
		{"all without packages", allCoreClaims, "", "", nil},
		{"all with preset overlap", allCoreClaims, `apt = ["bash", "make"]`, "bash make", nil},
		{"all with explicit overlap", allCoreClaims, "", "curl \\\n        jq", []string{"jq", "curl"}},
		{"partial with overlap", `core_packages = ["bash", "git"]`, `apt = ["git", "make"]`, "ca-certificates curl gnupg sudo \\\n        git make \\\n        bash", []string{"bash"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			raw := "schema = 2\nkind = 'preset'\nname = 'shared-base'\nversion = '1.0.0'\ndescription = 'core package fixture'\n[image]\nbase = 'registry.example/team/base:fixed'\n" + tt.apt + "\nroot_run = ['install -d /opt/project']\nuser_run = ['mkdir -p /home/vscode/project-cache']\n"
			if tt.claims != "" {
				raw += "[image.preinstalled]\n" + tt.claims + "\n"
			}
			definition, err := presets.Parse([]byte(raw), "shared-base.toml")
			if err != nil {
				t.Fatal(err)
			}
			registry, err := presets.NewRegistry(definition)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Normalize(config.Input{Root: root, Preset: definition.Name, Registry: registry, SystemPackages: tt.system})
			if err != nil {
				t.Fatal(err)
			}
			if err := Write(root, cfg, false); err != nil {
				t.Fatal(err)
			}
			if diagnostics := validate.Check(root, validate.Options{}); validate.ErrorCount(diagnostics) != 0 {
				t.Fatalf("Check: %#v", diagnostics)
			}
			path := filepath.Join(root, directoryName, "Dockerfile")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			_, after, installs := strings.Cut(text, "apt-get install -y --no-install-recommends \\\n        ")
			if installs != (tt.want != "") {
				t.Fatalf("unexpected apt installation:\n%s", text)
			}
			if installs {
				actual, _, _ := strings.Cut(after, " \\\n    && rm")
				if actual != tt.want {
					t.Fatalf("apt packages = %q, want %q", actual, tt.want)
				}
			} else if strings.Contains(text, "apt-get") || strings.Contains(text, "/var/lib/apt/lists") {
				t.Fatalf("empty installation still touches apt:\n%s", text)
			}
			ordered := []string{"USER root", "RUN install -d /opt/project", "RUN mkdir -p /home/vscode/.local/bin", "USER vscode", "RUN mkdir -p /home/vscode/project-cache"}
			rest := text
			for _, step := range ordered {
				var found bool
				_, rest, found = strings.Cut(rest, step)
				if !found {
					t.Fatalf("missing or reordered step %q:\n%s", step, text)
				}
			}
		})
	}
}

func TestCheckRejectsMissingUnclaimedAndProjectPackages(t *testing.T) {
	for _, tt := range []struct{ name, remove, want string }{
		{"unclaimed core", "ca-certificates", `core apt package "ca-certificates"`},
		{"declared core explicitly requested by preset", "git", `apt package "git" required by the selected definitions`},
		{"declared core explicitly requested by flag", "curl", `explicit system package "curl"`},
		{"addon package", "sqlite3", `apt package "sqlite3" required by the selected definitions`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			input := sharedImageInput(t, true)
			input.Root = root
			cfg, err := config.Normalize(input)
			if err != nil {
				t.Fatal(err)
			}
			if tt.name == "unclaimed core" {
				// Keep bash declared while requiring CA installation from the core.
				raw := strings.Replace(string(cfg.Definitions[0].Raw), `"bash", "ca-certificates", "curl"`, `"bash", "curl"`, 1)
				definition, err := presets.Parse([]byte(raw), "shared-node.toml")
				if err != nil {
					t.Fatal(err)
				}
				definition.Source = cfg.Definitions[0].Source
				input.Registry, err = presets.NewRegistry(definition, cfg.Definitions[1])
				if err != nil {
					t.Fatal(err)
				}
				cfg, err = config.Normalize(input)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := Write(root, cfg, false); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, directoryName, "Dockerfile")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(data), tt.remove, "")), 0o644); err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range validate.Check(root, validate.Options{}) {
				if diagnostic.Severity == validate.Error && strings.Contains(diagnostic.Message, tt.want) {
					return
				}
			}
			t.Fatalf("missing %q error", tt.want)
		})
	}
}

func TestCoreClaimsPreserveVariantAndAddonSteps(t *testing.T) {
	root := t.TempDir()
	input := sharedImageInput(t, true)
	input.Root = root
	preset, _ := input.Registry.Lookup(input.Preset)
	raw := string(preset.Raw) + `
[[variant]]
when = { addon = ["extra"] }
[variant.image]
apt = ["sudo"]
root_run = ["echo variant-root"]
user_run = ["echo variant-user"]
`
	preset, err := presets.Parse([]byte(raw), "shared-node.toml")
	if err != nil {
		t.Fatal(err)
	}
	addon, err := presets.Parse([]byte(`schema = 1
kind = "addon"
name = "extra"
version = "1.0.0"
description = "Explicit packages and steps"
[image]
apt = ["gnupg"]
root_run = ["echo addon-root"]
user_run = ["echo addon-user"]
`), "extra.toml")
	if err != nil {
		t.Fatal(err)
	}
	input.Registry, err = presets.NewRegistry(preset, addon)
	if err != nil {
		t.Fatal(err)
	}
	input.Addons = []string{"extra"}
	cfg, err := config.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, directoryName, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "git make \\\n        sudo \\\n        gnupg \\\n        curl \\\n        jq") {
		t.Fatalf("project apt contributions lost or reordered:\n%s", text)
	}
	for _, step := range []string{"USER root", "RUN install -d /opt/project", "RUN echo variant-root", "RUN echo addon-root", "USER vscode", "RUN mkdir -p /home/vscode/project-cache", "RUN echo variant-user", "RUN echo addon-user"} {
		var found bool
		_, text, found = strings.Cut(text, step)
		if !found {
			t.Fatalf("missing or reordered step %q:\n%s", step, data)
		}
	}
}
