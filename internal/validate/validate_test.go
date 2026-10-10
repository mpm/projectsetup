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
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"engines":{"node":"26"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Normalize(config.Input{Root: root, ProjectName: "example", Preset: "node", AITools: []config.AITool{}})
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
	cfg, err := config.Normalize(config.Input{Root: root, ProjectName: "example", Preset: "rails", AITools: []config.AITool{}})
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
	cfg, err := config.Normalize(config.Input{Root: root, ProjectName: "example", Preset: "ruby", Options: map[string]map[string]string{"ruby": {"version": "3.3.7"}}, AITools: []config.AITool{}})
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
	cfg, err := config.Normalize(config.Input{Root: root, ProjectName: "example", Preset: "ruby", AITools: []config.AITool{}})
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
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), `"version": "4.0"`, `"version": "3.2"`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	diagnostics := validate.Check(root, validate.Options{})
	assertDiagnostic(t, diagnostics, validate.Error, `feature "ghcr.io/rails/devcontainer/features/ruby:2" option version is "3.2"; expected "4.0"`)
}

func TestCheckRequiresSQLitePackages(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	cfg, err := config.Normalize(config.Input{Root: root, ProjectName: "example", Preset: "rails", Addons: []string{"sqlite"}, AITools: []config.AITool{}})
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
	assertDiagnostic(t, diagnostics, validate.Error, `apt package "libsqlite3-dev" required by the selected definitions is missing`)
}

func TestCheckRejectsMismatchedRuntimeNames(t *testing.T) {
	tests := []struct {
		name        string
		addons      []string
		file        string
		old         string
		replacement string
		want        string
	}{
		{name: "compose project", file: "compose.yaml", old: "name: example", replacement: "name: other", want: `Compose project name is "other"; expected "example"`},
		{name: "compose service", addons: []string{"postgres"}, file: "devcontainer.json", old: `"service": "app"`, replacement: `"service": "other"`, want: "must use compose.yaml service app"},
		{name: "unselected postgres", file: "compose.yaml", old: "services:\n", replacement: "services:\n  postgres:\n    image: postgres:17\n", want: `Compose service "postgres" is not provided by the selected definitions`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", t.TempDir())
			cfg, err := config.Normalize(config.Input{Root: root, ProjectName: "example", Preset: "node", Addons: tt.addons, AITools: []config.AITool{}})
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
	cfg, err := config.Normalize(config.Input{Root: root, ProjectName: "example", Preset: "node", Options: map[string]map[string]string{"node": {"version": "22"}}, Ports: []int{80}, AITools: []config.AITool{}})
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
	assertDiagnostic(t, diagnostics, validate.Error, `options.node.version is "22" but the project specifies "20"`)
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
	assertDiagnostic(t, diagnostics, validate.Error, "unsupported schemaVersion 99")
}

func TestCheckRejectsIncompleteOptions(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	cfg, err := config.Normalize(config.Input{Root: root, ProjectName: "example", Preset: "node", AITools: []config.AITool{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := generate.Write(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".devcontainer", "projectsetup.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), `"package_manager": "npm",`, "", 1)
	if edited == string(data) {
		t.Fatalf("manifest has no package_manager option:\n%s", data)
	}
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	assertDiagnostic(t, validate.Check(root, validate.Options{}), validate.Error, "manifest values are not normalized")
}

func TestCheckBuildRunsAfterStaticValidation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	cfg, err := config.Normalize(config.Input{Root: root, ProjectName: "example", Preset: "node", AITools: []config.AITool{}})
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

func TestCodexMountsAndEnvironment(t *testing.T) {
	for _, mutation := range []string{"none", "state-mount", "binary-mount", "environment", "host-missing", "relative-home"} {
		t.Run(mutation, func(t *testing.T) {
			root, home, state := t.TempDir(), t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", state)
			cfg, err := config.Normalize(config.Input{Root: root, Preset: "python", AITools: []config.AITool{config.AIToolCodex}})
			if err != nil {
				t.Fatal(err)
			}
			if err := generate.Write(root, cfg, false); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, ".devcontainer/devcontainer.json")
			data, _ := os.ReadFile(path)
			switch mutation {
			case "state-mount":
				composePath := filepath.Join(root, ".devcontainer/compose.yaml")
				compose, err := os.ReadFile(composePath)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(composePath, []byte(strings.ReplaceAll(string(compose), config.CodexStateSource, "/wrong")), 0644); err != nil {
					t.Fatal(err)
				}
			case "binary-mount":
				data = []byte(strings.ReplaceAll(string(data), "source=${localEnv:HOME}/.local/share/codex", "source=/wrong"))
			case "environment":
				data = []byte(strings.ReplaceAll(string(data), `"CODEX_HOME": "/home/vscode/.codex"`, `"CODEX_HOME": "/wrong"`))
			case "host-missing":
				t.Setenv("CODEX_HOME", filepath.Join(state, "missing"))
			case "relative-home":
				t.Setenv("CODEX_HOME", "relative")
			}
			if err := os.WriteFile(path, data, 0644); err != nil {
				t.Fatal(err)
			}
			diagnostics := validate.Check(root, validate.Options{CheckHostMounts: true})
			if (validate.ErrorCount(diagnostics) > 0) != (mutation != "none") {
				t.Fatalf("%#v", diagnostics)
			}
		})
	}
}

// legacyCompose is compose.yaml as v0.8.0 and earlier rendered it from a text
// template, with different quoting and key order than the current renderer.
const legacyCompose = `# Generated by projectsetup. Edit projectsetup.json and regenerate instead of editing this file.
name: example

services:
  app:
    build:
      context: ..
      dockerfile: .devcontainer/Dockerfile
    command: sleep infinity
    user: vscode
    volumes:
      - ..:/workspaces/example
      - type: bind
        source: "${CODEX_HOME:-${HOME}/.codex}"
        target: /home/vscode/.codex
    depends_on:
      postgres:
        condition: service_healthy

  postgres:
    image: postgres:18-trixie
    restart: unless-stopped
    environment:
      POSTGRES_USER: projectsetup
      POSTGRES_PASSWORD: projectsetup
      POSTGRES_DB: 'example'
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U projectsetup -d example"]
      interval: 5s
      timeout: 5s
      retries: 10
    volumes:
      - postgres-data:/var/lib/postgresql

volumes:
  postgres-data:
`

func TestCheckAcceptsComposeFromEarlierReleases(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", "")
	cfg, err := config.Normalize(config.Input{Root: root, ProjectName: "example", Preset: "node", Addons: []string{"postgres"}, AITools: []config.AITool{config.AIToolCodex}})
	if err != nil {
		t.Fatal(err)
	}
	if err := generate.Write(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".devcontainer", "compose.yaml")
	if err := os.WriteFile(path, []byte(legacyCompose), 0o644); err != nil {
		t.Fatal(err)
	}
	if diagnostics := validate.Check(root, validate.Options{}); validate.ErrorCount(diagnostics) > 0 {
		t.Fatalf("Check() = %#v, want no errors", diagnostics)
	}

	for _, tt := range []struct{ old, replacement, want string }{
		{"image: postgres:18-trixie", "image: postgres:17-bookworm", `service postgres image is "postgres:17-bookworm"; expected "postgres:18-trixie"`},
		{"POSTGRES_DB: 'example'", "POSTGRES_DB: other", `service postgres environment POSTGRES_DB is "other"; expected "example"`},
		{"- postgres-data:/var/lib/postgresql", "- postgres-data:/var/lib/postgresql/data", `service postgres must mount volume "postgres-data:/var/lib/postgresql"`},
		{"condition: service_healthy", "condition: service_started", "service app must depend on postgres with condition service_healthy"},
		{`-d example"]`, `-d other"]`, "service postgres healthcheck test is"},
		{"\n  postgres:\n", "\n  db:\n", `Compose service "postgres" required by the selected definitions is missing`},
		{"- ..:/workspaces/example", "- ..:/workspace", `service app must mount the project with volume "..:/workspaces/example"`},
		{"dockerfile: .devcontainer/Dockerfile", "dockerfile: Dockerfile", "service app must build context .. with dockerfile .devcontainer/Dockerfile"},
		{"volumes:\n  postgres-data:\n", "", `named volume "postgres-data" is not declared`},
		{"services:", "services: [", "parse YAML"},
	} {
		if err := os.WriteFile(path, []byte(strings.Replace(legacyCompose, tt.old, tt.replacement, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		assertDiagnostic(t, validate.Check(root, validate.Options{}), validate.Error, tt.want)
	}
}
