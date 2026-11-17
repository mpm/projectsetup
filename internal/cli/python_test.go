package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/generate"
	"github.com/mpm/projectsetup/internal/presets"
	"github.com/mpm/projectsetup/internal/validate"
)

func TestPythonInstallTools(t *testing.T) {
	for _, manager := range []string{"pip", "uv", "poetry"} {
		for _, value := range []string{"", "false", "true", "yes", "TRUE"} {
			t.Run(manager+"/"+value, func(t *testing.T) {
				useConfigDir(t)
				t.Setenv("HOME", t.TempDir())
				root := t.TempDir()
				args := []string{"--non-interactive", "--preset", "python", "--name", "filecatalog", "--set", "python.version=3.12", "--set", "python.package_manager=" + manager, "--addon", "sqlite", "--ai", "none"}
				if value != "" {
					args = append(args, "--set", "python.install_tools="+value)
				}
				err := runInit(root, args, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{})
				if value == "yes" || value == "TRUE" {
					if err == nil || !strings.Contains(err.Error(), "install_tools") {
						t.Fatalf("invalid choice: %v", err)
					}
					if _, err := os.Stat(filepath.Join(root, ".devcontainer")); !os.IsNotExist(err) {
						t.Fatalf("invalid choice wrote output: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				want := value
				if want == "" {
					want = "false"
				}
				before := readManifest(t, root)
				if before.Options["python"]["install_tools"] != want {
					t.Fatalf("options = %v", before.Options)
				}
				assertPythonTools(t, root, want == "true", true)
				// Explicit choices and defaults both survive later refreshes.
				if err := runUpgrade(root, []string{"--refresh-presets"}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
					t.Fatal(err)
				}
				if after := readManifest(t, root); !reflect.DeepEqual(before, after) {
					t.Fatalf("refresh changed selection: before %+v, after %+v", before, after)
				}
				assertPythonTools(t, root, want == "true", true)
			})
		}
	}
}

func TestPythonToolsChangeRequiresExplicitRefresh(t *testing.T) {
	useConfigDir(t)
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	raw, err := os.ReadFile(filepath.Join("testdata", "python-1.0.0.toml"))
	if err != nil {
		t.Fatal(err)
	}
	old, err := presets.Parse(raw, "python-1.0.0.toml")
	if err != nil {
		t.Fatal(err)
	}
	old.Source = presets.SourceBuiltin
	sqlite, _ := presets.Builtin().Lookup("sqlite")
	registry, err := presets.NewRegistry(old, sqlite)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Normalize(config.Input{
		Root: root, Registry: registry, ProjectName: "filecatalog", Preset: "python", Addons: []string{"sqlite"},
		Options: map[string]map[string]string{"python": {"version": "3.12", "package_manager": "uv"}},
		AITools: []config.AITool{}, Ports: []int{8000}, SystemPackages: []string{"make"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := generate.Write(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	before := readManifest(t, root)
	if before.Preset.SHA256 != "085617d28277f5725ce8c3f2b7955d6022ea75e07cbe966bf39cfc0927b435b1" {
		t.Fatalf("historical fixture hash changed: %s", before.Preset.SHA256)
	}
	assertPythonTools(t, root, true, false)
	if err := runUpgrade(root, nil, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if after := readManifest(t, root); !reflect.DeepEqual(before, after) {
		t.Fatalf("ordinary upgrade changed manifest: %+v", after)
	}
	snapshot := filepath.Join(root, ".devcontainer", "presets", "python.toml")
	if got, err := os.ReadFile(snapshot); err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("ordinary upgrade changed historical snapshot: %s, %v", got, err)
	}
	assertPythonTools(t, root, true, false)
	if err := runUpgrade(root, []string{"--refresh-presets"}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	current, _ := presets.Builtin().Lookup("python")
	want := before
	want.Preset = current.Ref()
	want.Options["python"]["install_tools"] = "false"
	if after := readManifest(t, root); !reflect.DeepEqual(want, after) {
		t.Fatalf("refresh did not preserve selection and fill new default: want %+v, got %+v", want, after)
	}
	if got, err := os.ReadFile(snapshot); err != nil || !bytes.Equal(got, current.Raw) {
		t.Fatalf("refresh did not adopt current snapshot: %s, %v", got, err)
	}
	assertPythonTools(t, root, false, true)
	// Refresh still validates the recorded hash before replacing definitions.
	if err := os.WriteFile(snapshot, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runUpgrade(root, []string{"--refresh-presets"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("tampered snapshot refresh: %v", err)
	}
}

func assertPythonTools(t *testing.T, root string, enabled, explicit bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".devcontainer", "devcontainer.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Features     map[string]map[string]any `json:"features"`
		ContainerEnv map[string]string         `json:"containerEnv"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	feature := document.Features["ghcr.io/devcontainers/features/python:1"]
	wantOptions := 1
	if explicit {
		wantOptions++
	}
	if feature["version"] != "3.12" || len(feature) != wantOptions {
		t.Fatalf("unexpected feature options: %v", feature)
	}
	value, present := feature["installTools"]
	if present != explicit || (explicit && value != enabled) {
		t.Fatalf("installTools = %#v (present %v), want boolean %v (present %v)", value, present, enabled, explicit)
	}
	path := strings.Split(document.ContainerEnv["PATH"], ":")
	if !slices.Contains(path, "/usr/local/python/current/bin") || slices.Contains(path, "/usr/local/py-utils/bin") != enabled {
		t.Fatalf("unexpected Python PATH: %v", path)
	}
	if diagnostics := validate.Check(root, validate.Options{}); validate.ErrorCount(diagnostics) != 0 {
		t.Fatalf("check: %#v", diagnostics)
	}
}
