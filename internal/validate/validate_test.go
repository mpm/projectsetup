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

func TestCheckAcceptsRubyConfiguration(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(root, ".ruby-version"), []byte("3.3.7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Normalize(config.Input{Root: root, ProjectName: "example", Preset: config.PresetRuby, LanguageVersion: "3.3.7", AITools: []config.AITool{}})
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

func TestCheckRejectsMismatchedRubyFeature(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	cfg, err := config.Normalize(config.Input{Root: root, ProjectName: "example", Preset: config.PresetRuby, AITools: []config.AITool{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := generate.Write(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".devcontainer", "devcontainer.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), `"version": "3.3"`, `"version": "3.2"`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	diagnostics := validate.Check(root, validate.Options{})
	assertDiagnostic(t, diagnostics, validate.Error, "Ruby feature version does not match")
}

func TestCheckRequiresSQLitePackages(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	cfg, err := config.Normalize(config.Input{Root: root, ProjectName: "example", Preset: config.PresetRails, Database: config.DatabaseSQLite, AITools: []config.AITool{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := generate.Write(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".devcontainer", "Dockerfile")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	modified := strings.Replace(string(data), "libsqlite3-dev sqlite3", "sqlite3", 1)
	if err := os.WriteFile(path, []byte(modified), 0o644); err != nil {
		t.Fatal(err)
	}
	diagnostics := validate.Check(root, validate.Options{})
	assertDiagnostic(t, diagnostics, validate.Error, `SQLite database requires apt package "libsqlite3-dev"`)
}

func TestCheckRejectsMismatchedRuntimeNames(t *testing.T) {
	tests := []struct {
		name        string
		database    config.Database
		file        string
		old         string
		replacement string
		want        string
	}{
		{name: "compose project", file: "compose.yaml", old: "name: example", replacement: "name: other", want: `missing expected Compose configuration "name: example"`},
		{name: "compose service", database: config.DatabasePostgres, file: "devcontainer.json", old: `"service": "app"`, replacement: `"service": "other"`, want: "must use compose.yaml service app"},
		{name: "unselected postgres", file: "compose.yaml", old: "services:\n", replacement: "services:\n  postgres:\n    image: postgres:17\n", want: "PostgreSQL service is configured but database is"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", t.TempDir())
			cfg, err := config.Normalize(config.Input{Root: root, ProjectName: "example", Preset: config.PresetNode, Database: tt.database, AITools: []config.AITool{}})
			if err != nil {
				t.Fatal(err)
			}
			if err := generate.Write(root, cfg, false); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, ".devcontainer", tt.file)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(strings.Replace(string(data), tt.old, tt.replacement, 1)), 0o644); err != nil {
				t.Fatal(err)
			}

			diagnostics := validate.Check(root, validate.Options{})
			assertDiagnostic(t, diagnostics, validate.Error, tt.want)
		})
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
	wantCompose := fmt.Sprintf("docker compose -f %s config", filepath.Join(root, ".devcontainer", "compose.yaml"))
	if !contains(runner.commands, want) {
		t.Fatalf("commands = %q, want %q", runner.commands, want)
	}
	if !contains(runner.commands, wantCompose) {
		t.Fatalf("commands = %q, want %q", runner.commands, wantCompose)
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
