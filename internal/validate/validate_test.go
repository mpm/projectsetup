package validate_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/generate"
	"github.com/mpm/projectsetup/internal/validate"
)

type fakeRunner struct {
	commands []string
}

func (runner *fakeRunner) LookPath(name string) (string, error) { return "/bin/" + name, nil }
func (runner *fakeRunner) Run(name string, args ...string) ([]byte, error) {
	runner.commands = append(runner.commands, strings.Join(append([]string{name}, args...), " "))
	return nil, nil
}

func TestCheckAcceptsGeneratedConfiguration(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"engines":{"node":"24"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Normalize(config.Input{Root: root, ProjectName: "example", Preset: config.PresetNode, AITools: []config.AITool{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := generate.Write(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	diagnostics := validate.Check(root, validate.Options{CheckHostMounts: true})
	if count := validate.ErrorCount(diagnostics); count != 0 {
		t.Fatalf("Check() errors = %d, diagnostics = %#v", count, diagnostics)
	}
}

func TestCheckAcceptsRailsConfigurationWithoutProjectSignals(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	cfg, err := config.Normalize(config.Input{Root: root, ProjectName: "example", Preset: config.PresetRails, AITools: []config.AITool{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := generate.Write(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	diagnostics := validate.Check(root, validate.Options{})
	if len(diagnostics) != 0 {
		t.Fatalf("Check() diagnostics = %#v, want none", diagnostics)
	}
}

func TestCheckAggregatesIndependentFailures(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	packageJSON := filepath.Join(root, "package.json")
	if err := os.WriteFile(packageJSON, []byte(`{"engines":{"node":"22"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Normalize(config.Input{Root: root, ProjectName: "example", Preset: config.PresetNode, LanguageVersion: "22", Ports: []int{80}, AITools: []config.AITool{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := generate.Write(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packageJSON, []byte(`{"engines":{"node":"20"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	devDir := filepath.Join(root, ".devcontainer")
	if err := os.Chmod(filepath.Join(devDir, "scripts", "post-create.sh"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(devDir, "devcontainer.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.ReplaceAll(string(data), `"remoteUser": "vscode"`, `"remoteUser": "root"`))
	data = []byte(strings.ReplaceAll(string(data), `:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin`, `:${containerEnv:PATH}`))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	diagnostics := validate.Check(root, validate.Options{})
	if count := validate.ErrorCount(diagnostics); count < 3 {
		t.Fatalf("Check() errors = %d, want at least 3; diagnostics = %#v", count, diagnostics)
	}
	assertDiagnostic(t, diagnostics, validate.Error, "containerUser and remoteUser")
	assertDiagnostic(t, diagnostics, validate.Error, "not executable")
	assertDiagnostic(t, diagnostics, validate.Error, "disagrees with detected project version")
	assertDiagnostic(t, diagnostics, validate.Error, "containerEnv.PATH must include /usr/bin")
	assertDiagnostic(t, diagnostics, validate.Warning, "outside dworm's scanned range")
}

func TestCheckRejectsUnsupportedManifestBeforeCrossFileChecks(t *testing.T) {
	root := t.TempDir()
	devDir := filepath.Join(root, ".devcontainer")
	if err := os.Mkdir(devDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"schemaVersion":99,"projectName":"example","preset":"node","database":"none","aiTools":[],"packageManager":"npm","languageVersion":"22","ports":[],"systemPackages":[],"generatedBy":"someone"}`
	if err := os.WriteFile(filepath.Join(devDir, "projectsetup.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	diagnostics := validate.Check(root, validate.Options{})
	assertDiagnostic(t, diagnostics, validate.Error, "unsupported schemaVersion")
	assertDiagnostic(t, diagnostics, validate.Error, "generatedBy must be")
}

func TestCheckBuildRunsAfterStaticValidation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	cfg, err := config.Normalize(config.Input{Root: root, ProjectName: "example", Preset: config.PresetNode, AITools: []config.AITool{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := generate.Write(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	diagnostics := validate.Check(root, validate.Options{External: true, Build: true, Runner: runner})
	if count := validate.ErrorCount(diagnostics); count != 0 {
		t.Fatalf("Check() errors = %d, diagnostics = %#v", count, diagnostics)
	}
	want := fmt.Sprintf("devcontainer build --workspace-folder %s", root)
	if !contains(runner.commands, want) {
		t.Fatalf("commands = %q, want %q", runner.commands, want)
	}

	if err := os.Remove(filepath.Join(root, ".devcontainer", "Dockerfile")); err != nil {
		t.Fatal(err)
	}
	runner.commands = nil
	diagnostics = validate.Check(root, validate.Options{External: true, Build: true, Runner: runner})
	if validate.ErrorCount(diagnostics) == 0 {
		t.Fatalf("Check() diagnostics = %#v, want static error", diagnostics)
	}
	if contains(runner.commands, want) {
		t.Fatalf("build ran despite static errors: %q", runner.commands)
	}
}

func assertDiagnostic(t *testing.T, diagnostics []validate.Diagnostic, severity validate.Severity, message string) {
	t.Helper()
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == severity && strings.Contains(diagnostic.Message, message) {
			return
		}
	}
	t.Fatalf("diagnostics do not contain %s %q: %#v", severity, message, diagnostics)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
