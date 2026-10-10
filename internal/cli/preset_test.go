package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/presets"
	"github.com/mpm/projectsetup/internal/validate"
)

// useConfigDir points the user definition directory at a fresh directory
// and returns its presets subdirectory.
func useConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(presets.ConfigDirEnv, dir)
	return filepath.Join(dir, "presets")
}

func runPresetCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := Run(append([]string{"preset"}, args...), &bytes.Buffer{}, &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

func editFile(t *testing.T, path, old, replacement string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s does not contain %q", path, old)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), old, replacement, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUserDefinitionLifecycle(t *testing.T) {
	userDir := useConfigDir(t)
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	writeNodeProject(t, root)

	stdout, _, err := runPresetCommand(t, "eject", "node", "--as", "mynode")
	if err != nil {
		t.Fatalf("eject: %v", err)
	}
	userFile := filepath.Join(userDir, "mynode.toml")
	if !strings.Contains(stdout, userFile) {
		t.Errorf("eject output = %q", stdout)
	}
	editFile(t, userFile, `description = "Node.js with npm, pnpm, or Yarn"`, `description = "My Node"`)

	stdout, _, err = runPresetCommand(t, "list")
	if err != nil || !strings.Contains(stdout, "mynode") || !strings.Contains(stdout, "user") || !strings.Contains(stdout, "My Node") {
		t.Fatalf("list = %q, %v", stdout, err)
	}
	stdout, _, err = runPresetCommand(t, "show", "mynode")
	if err != nil || !strings.HasPrefix(stdout, "schema = 1\n") || !strings.Contains(stdout, `name = "mynode"`) {
		t.Fatalf("show = %q, %v", stdout, err)
	}

	// The copied detection rules read engines.node from package.json.
	if err := runInit(root, []string{"--non-interactive", "--preset", "mynode", "--ai", "none", "--addon", "sqlite"}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("init: %v", err)
	}
	manifest := readManifest(t, root)
	if manifest.Preset.Name != "mynode" || manifest.Preset.Source != presets.SourceUser || manifest.Options["mynode"]["version"] != "22" {
		t.Fatalf("manifest = %+v", manifest)
	}
	devDir := filepath.Join(root, ".devcontainer")
	copied, err := os.ReadFile(filepath.Join(devDir, config.PresetsDir, "mynode.toml"))
	if err != nil || !strings.Contains(string(copied), "My Node") {
		t.Fatalf("project copy = %q, %v", copied, err)
	}
	if diagnostics := validate.Check(root, validate.Options{}); validate.ErrorCount(diagnostics) != 0 {
		t.Fatalf("check: %#v", diagnostics)
	}

	// check and upgrade use the project copy, not the registry.
	if err := os.Remove(userFile); err != nil {
		t.Fatal(err)
	}
	if diagnostics := validate.Check(root, validate.Options{}); validate.ErrorCount(diagnostics) != 0 {
		t.Fatalf("check without the user definition: %#v", diagnostics)
	}
	if err := runUpgrade(root, nil, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("upgrade without the user definition: %v", err)
	}
	if err := runUpgrade(root, []string{"--refresh-presets"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), `unsupported preset "mynode"`) {
		t.Fatalf("refresh without the user definition: %v", err)
	}

	// --refresh-presets picks up a changed user definition and keeps values.
	if err := os.WriteFile(userFile, copied, 0o644); err != nil {
		t.Fatal(err)
	}
	editFile(t, userFile, `version = "1.0.0"`, `version = "1.1.0"`)
	editFile(t, userFile, "[setup]", "[container.env]\nMY_FLAG = \"on\"\n\n[setup]")
	if err := runUpgrade(root, []string{"--refresh-presets"}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	manifest = readManifest(t, root)
	if manifest.Preset.Version != "1.1.0" || manifest.Options["mynode"]["version"] != "22" || len(manifest.Addons) != 1 {
		t.Fatalf("refreshed manifest = %+v", manifest)
	}
	devcontainer, err := os.ReadFile(filepath.Join(devDir, "devcontainer.json"))
	if err != nil || !strings.Contains(string(devcontainer), `"MY_FLAG": "on"`) {
		t.Fatalf("refreshed devcontainer.json = %s, %v", devcontainer, err)
	}

	// Edited project copies are reported, and upgrade refuses them.
	editFile(t, filepath.Join(devDir, config.PresetsDir, "mynode.toml"), `MY_FLAG = "on"`, `MY_FLAG = "off"`)
	diagnostics := validate.Check(root, validate.Options{})
	assertCheckMessage(t, diagnostics, "mynode.toml: sha256 is")
	if err := runUpgrade(root, nil, &bytes.Buffer{}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "do not edit project copies") {
		t.Fatalf("upgrade with edited copy: %v", err)
	}
}

func assertCheckMessage(t *testing.T, diagnostics []validate.Diagnostic, want string) {
	t.Helper()
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == validate.Error && strings.Contains(diagnostic.Message, want) {
			return
		}
	}
	t.Fatalf("diagnostics do not contain error %q: %#v", want, diagnostics)
}

func TestInitRejectsUserDefinitionNamedLikeBuiltin(t *testing.T) {
	userDir := useConfigDir(t)
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatal(err)
	}
	node, _ := presets.Builtin().Lookup("node")
	if err := os.WriteFile(filepath.Join(userDir, "node.toml"), node.Raw, 0o644); err != nil {
		t.Fatal(err)
	}
	err := runInit(t.TempDir(), []string{"--non-interactive", "--preset", "node"}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "is a built-in definition name") {
		t.Fatalf("runInit() error = %v", err)
	}
}

func TestPresetListJSON(t *testing.T) {
	useConfigDir(t)
	stdout, _, err := runPresetCommand(t, "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var infos []presets.Info
	if err := json.Unmarshal([]byte(stdout), &infos); err != nil {
		t.Fatalf("parse %q: %v", stdout, err)
	}
	if len(infos) != 6 || infos[0].Name != "node" || infos[4].Name != "postgres" || infos[4].Kind != presets.KindAddon {
		t.Fatalf("infos = %+v", infos)
	}
}

func TestPresetEjectRejectsInvalidRequests(t *testing.T) {
	userDir := useConfigDir(t)
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"eject", "node"}, "requires --as NEW"},
		{[]string{"eject", "--as", "x"}, "expects 1 argument(s), got 0"},
		{[]string{"eject", "go", "--as", "mygo"}, `unknown definition "go"`},
		{[]string{"eject", "node", "--as", "ruby"}, `definition "ruby" already exists`},
		{[]string{"eject", "node", "--as", "My-Node"}, `name "My-Node" must match`},
		{[]string{"show", "missing"}, `unknown definition "missing"`},
		{[]string{"frobnicate"}, `unknown preset subcommand "frobnicate"`},
	}
	for _, tt := range tests {
		_, _, err := runPresetCommand(t, tt.args...)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("preset %v error = %v, want containing %q", tt.args, err, tt.want)
		}
	}
	if entries, _ := os.ReadDir(userDir); len(entries) != 0 {
		t.Fatalf("rejected requests wrote %v", entries)
	}
	if _, _, err := runPresetCommand(t, "eject", "postgres", "--as", "pg"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runPresetCommand(t, "eject", "postgres", "--as", "pg"); err == nil || !strings.Contains(err.Error(), `definition "pg" already exists`) {
		t.Fatalf("second eject error = %v", err)
	}
}

func TestPresetValidate(t *testing.T) {
	useConfigDir(t)
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	header := "schema = 1\nkind = \"preset\"\nname = \"custom\"\nversion = \"1.0.0\"\ndescription = \"d\"\n[image]\nbase = \"debian\"\n"

	stdout, stderr, err := runPresetCommand(t, "validate", write("custom.toml", header))
	if err != nil || !strings.Contains(stdout, "valid preset custom 1.0.0") || stderr != "" {
		t.Fatalf("validate = %q, %q, %v", stdout, stderr, err)
	}
	_, stderr, err = runPresetCommand(t, "validate", write("draft.toml", header))
	if err != nil || !strings.Contains(stderr, "does not match the file name") {
		t.Fatalf("validate draft = %q, %v", stderr, err)
	}
	_, stderr, err = runPresetCommand(t, "validate", write("node.toml", strings.Replace(header, `"custom"`, `"node"`, 1)))
	if err != nil || !strings.Contains(stderr, "is a built-in definition name") {
		t.Fatalf("validate builtin name = %q, %v", stderr, err)
	}
	_, _, err = runPresetCommand(t, "validate", write("bad.toml", header+"colour = \"red\"\n"))
	if err == nil || !strings.Contains(err.Error(), "unknown fields") {
		t.Fatalf("validate unknown field error = %v", err)
	}
	_, _, err = runPresetCommand(t, "validate", write("sidecar.toml", header+"[services.cache]\nrestart = \"always\"\n"))
	if err == nil || !strings.Contains(err.Error(), `service "cache" has no image`) {
		t.Fatalf("validate unresolvable preset error = %v", err)
	}
}
