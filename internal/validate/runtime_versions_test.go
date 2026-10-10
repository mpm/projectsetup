package validate_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/generate"
	"github.com/mpm/projectsetup/internal/presets"
	"github.com/mpm/projectsetup/internal/validate"
)

func TestFixedImageRuntimeVersionFiles(t *testing.T) {
	tests := []struct {
		name, runtime, installed, file, value string
		wantError                             bool
	}{
		{"node exact", "node", "22.14.0", ".node-version", "v22.14.0\n", false},
		{"node major", "node", "22.14.0", ".nvmrc", "22\n", false},
		{"node minor", "node", "22.14.0", ".node-version", "22.14\n", false},
		{"node wrong patch", "node", "22.14.0", ".nvmrc", "22.14.1\n", true},
		{"node boundary", "node", "22.14.0", ".node-version", "2\n", true},
		{"node moving selector", "node", "22.14.0", ".nvmrc", "lts/*\n", true},
		{"node invalid numeric extension", "node", "22.14.0", ".node-version", "22.14.0.1\n", true},
		{"ruby prefix", "ruby", "3.3.7", ".ruby-version", "ruby-3.3.7 trailing\n", false},
		{"ruby mismatch", "ruby", "3.3.7", ".ruby-version", "3.2\n", true},
		{"python minor", "python", "3.14.0", ".python-version", "3.14\n", false},
		{"python mismatch", "python", "3.14.0", ".python-version", "3.13.2\n", true},
		{"prerelease exact", "node", "22.14.0-rc.1", ".node-version", "22.14.0-rc.1\n", false},
		{"prerelease differs", "node", "22.14.0-rc.1", ".node-version", "22.14.0\n", true},
		{"no files", "ruby", "3.3.7", "", "", false},
		{"unrelated runtime", "node", "22.14.0", ".ruby-version", "3.2\n", false},
		{"python requirement range", "python", "3.14.0", "pyproject.toml", "[project]\nrequires-python = '>=3.10,<4'\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			definition, err := presets.Parse([]byte(fmt.Sprintf(`schema = 2
kind = "preset"
name = "company-toolchain"
version = "1.0.0"
description = "Fixed image without options or detection rules"
[image]
base = "registry.example/toolchain:fixed"
[image.preinstalled.tools.%s]
version = "%s"
executable = "/opt/bin/runtime"
path = ["/opt/bin"]
`, tt.runtime, tt.installed)), "toolchain.toml")
			if err != nil {
				t.Fatal(err)
			}
			registry, err := presets.NewRegistry(definition)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Normalize(config.Input{Root: root, ProjectName: "example", Preset: definition.Name, Registry: registry, AITools: []config.AITool{}})
			if err != nil {
				t.Fatal(err)
			}
			if err := generate.Write(root, cfg, false); err != nil {
				t.Fatal(err)
			}
			// Add pins after generation to exercise check using only snapshots.
			if tt.file != "" {
				if err := os.WriteFile(filepath.Join(root, tt.file), []byte(tt.value), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			diagnostics := validate.Check(root, validate.Options{})
			if tt.wantError {
				assertDiagnostic(t, diagnostics, validate.Error, "image.preinstalled.tools."+tt.runtime+".version is fixed at")
				if len(diagnostics) != 1 || diagnostics[0].Path != tt.file {
					t.Fatalf("diagnostics = %#v", diagnostics)
				}
				// Staged generation must fail without replacing the old tree.
				manifestPath := filepath.Join(root, ".devcontainer", "projectsetup.json")
				before, err := os.ReadFile(manifestPath)
				if err != nil {
					t.Fatal(err)
				}
				replacement := cfg
				replacement.SystemPackages = []string{"jq"}
				err = generate.Write(root, replacement, true)
				if err == nil || !strings.Contains(err.Error(), "fixed at") {
					t.Fatalf("Write = %v", err)
				}
				after, err := os.ReadFile(manifestPath)
				if err != nil || string(before) != string(after) {
					t.Fatalf("failed generation changed manifest: %v", err)
				}
			} else if validate.ErrorCount(diagnostics) != 0 {
				t.Fatalf("diagnostics = %#v", diagnostics)
			}
		})
	}
}

func TestFixedImageChecksEveryNodeVersionFile(t *testing.T) {
	root := t.TempDir()
	definition, err := presets.Parse([]byte(`schema = 2
kind = 'preset'
name = 'shared'
version = '1.0.0'
description = 'Node'
[image]
base = 'example:fixed'
[image.preinstalled.tools.node]
version = '22.14.0'
executable = '/opt/bin/node'
path = ['/opt/bin']
`), "shared.toml")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := presets.NewRegistry(definition)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Normalize(config.Input{Root: root, ProjectName: "example", Preset: "shared", Registry: registry, AITools: []config.AITool{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := generate.Write(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	for file, value := range map[string]string{".node-version": "22", ".nvmrc": "20"} {
		if err := os.WriteFile(filepath.Join(root, file), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	diagnostics := validate.Check(root, validate.Options{})
	if len(diagnostics) != 1 || diagnostics[0].Path != ".nvmrc" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if err := os.WriteFile(filepath.Join(root, ".node-version"), []byte("21"), 0o644); err != nil {
		t.Fatal(err)
	}
	diagnostics = validate.Check(root, validate.Options{})
	if len(diagnostics) != 2 || diagnostics[0].Path != ".node-version" || diagnostics[1].Path != ".nvmrc" {
		t.Fatalf("independent failures = %#v", diagnostics)
	}
}

func TestFixedRuntimeDoesNotTreatPythonRequirementAsExactPin(t *testing.T) {
	root := t.TempDir()
	definition, _ := presets.Builtin().Lookup("python")
	// An ejected preset may retain old inference rules and a compatible
	// version constraint; neither turns requires-python into an exact pin.
	source := strings.Replace(string(definition.Raw), "schema = 1", "schema = 2", 1)
	source = strings.Replace(source, `name = "python"`, `name = "shared-python"`, 1)
	source = strings.Replace(source, "[features.\"ghcr.io/devcontainers/features/python:1\"]\nversion = \"${option:version}\"", `[image.preinstalled.tools.python]
version = "3.14.0"
executable = "/opt/bin/python"
path = ["/opt/bin"]`, 1)
	definition, err := presets.Parse([]byte(source), "shared-python.toml")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := presets.NewRegistry(definition)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte("[project]\nrequires-python = '>=3.10,<4'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Normalize(config.Input{Root: root, ProjectName: "example", Preset: definition.Name, Registry: registry, AITools: []config.AITool{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := generate.Write(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	if diagnostics := validate.Check(root, validate.Options{}); validate.ErrorCount(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if err := os.WriteFile(filepath.Join(root, ".python-version"), []byte("3.13"), 0o644); err != nil {
		t.Fatal(err)
	}
	assertDiagnostic(t, validate.Check(root, validate.Options{}), validate.Error, "image.preinstalled.tools.python.version is fixed at")
}

func TestCustomLegacyAndEmptyContractsRetainVersionOptionChecks(t *testing.T) {
	for _, contract := range []string{"", "[image.preinstalled]\n", "[image.preinstalled]\ncore_packages = ['git']\n"} {
		t.Run(contract, func(t *testing.T) {
			root := t.TempDir()
			definition, _ := presets.Builtin().Lookup("node")
			source := strings.Replace(string(definition.Raw), `name = "node"`, `name = "custom-node"`, 1)
			if contract != "" {
				source = strings.Replace(source, "schema = 1", "schema = 2", 1)
			}
			definition, err := presets.Parse([]byte(source+contract), "custom-node.toml")
			if err != nil {
				t.Fatal(err)
			}
			registry, err := presets.NewRegistry(definition)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Normalize(config.Input{Root: root, ProjectName: "example", Preset: definition.Name, Registry: registry, AITools: []config.AITool{}})
			if err != nil {
				t.Fatal(err)
			}
			if err := generate.Write(root, cfg, false); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".node-version"), []byte("20"), 0o644); err != nil {
				t.Fatal(err)
			}
			assertDiagnostic(t, validate.Check(root, validate.Options{}), validate.Error, `options.custom-node.version is "26" but the project specifies "20"`)
		})
	}
}
