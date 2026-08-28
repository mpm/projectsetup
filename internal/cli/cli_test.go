package cli

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/doctor"
	"github.com/mpm/projectsetup/internal/generate"
)

func TestRunHelp(t *testing.T) {
	var stdout bytes.Buffer
	if err := Run([]string{"--help"}, &bytes.Buffer{}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "projectsetup init") {
		t.Fatalf("help output does not describe init:\n%s", stdout.String())
	}
}

func TestRunVersion(t *testing.T) {
	var stdout bytes.Buffer
	if err := Run([]string{"--version"}, &bytes.Buffer{}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.HasPrefix(stdout.String(), "projectsetup ") || !strings.Contains(stdout.String(), "commit:") {
		t.Fatalf("version output = %q", stdout.String())
	}
}

func TestRunInitNonInteractive(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"engines":{"node":"22"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := runInit(root, []string{"--non-interactive", "--database", "sqlite", "--ai", "none", "--port", "3000"}, &bytes.Buffer{}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Generated .devcontainer") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".devcontainer", "devcontainer.json")); err != nil {
		t.Fatalf("generated devcontainer.json: %v", err)
	}
	dockerfile, err := os.ReadFile(filepath.Join(root, ".devcontainer", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(dockerfile), "libsqlite3-dev sqlite3") {
		t.Fatalf("generated Dockerfile lacks SQLite packages:\n%s", dockerfile)
	}
}

func TestRunInitRejectsAmbiguousDetection(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"package.json", "pyproject.toml"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	err := runInit(root, []string{"--non-interactive"}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "multiple project presets detected") {
		t.Fatalf("runInit() error = %v", err)
	}
}

func TestRunUpgradeConvertsLegacyGeneratedConfigurationToCompose(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	cfg, err := config.Normalize(config.Input{
		Root: root, ProjectName: "legacy", Preset: config.PresetNode,
		PackageManager: config.PackageManagerNPM, Ports: []int{3000}, AITools: []config.AITool{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := generate.Write(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	devDir := filepath.Join(root, ".devcontainer")
	if err := os.Remove(filepath.Join(devDir, "compose.yaml")); err != nil {
		t.Fatal(err)
	}
	legacy := `{"name":"legacy","build":{"dockerfile":"Dockerfile","context":".."},"workspaceFolder":"/workspaces/legacy","containerUser":"vscode","remoteUser":"vscode"}`
	if err := os.WriteFile(filepath.Join(devDir, "devcontainer.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := runUpgrade(root, nil, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("runUpgrade() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Upgraded .devcontainer") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	devcontainer, err := os.ReadFile(filepath.Join(devDir, "devcontainer.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(devcontainer), `"dockerComposeFile": "compose.yaml"`) || strings.Contains(string(devcontainer), `"build"`) {
		t.Fatalf("upgraded devcontainer.json does not use Compose:\n%s", devcontainer)
	}
	if _, err := os.Stat(filepath.Join(devDir, "compose.yaml")); err != nil {
		t.Fatalf("upgraded compose.yaml: %v", err)
	}
	manifest, err := os.ReadFile(filepath.Join(devDir, "projectsetup.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), `"ports": [`) || !strings.Contains(string(manifest), "3000") {
		t.Fatalf("upgrade did not preserve manifest options:\n%s", manifest)
	}
}

func TestRunUpgradeRejectsHandWrittenConfiguration(t *testing.T) {
	root := t.TempDir()
	devDir := filepath.Join(root, ".devcontainer")
	if err := os.Mkdir(devDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"schemaVersion":1,"generatedBy":"someone-else"}`
	if err := os.WriteFile(filepath.Join(devDir, "projectsetup.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runUpgrade(root, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "not recognized") {
		t.Fatalf("runUpgrade() error = %v, want ownership refusal", err)
	}
}

func TestRunUpgradeRejectsMalformedGeneratedManifest(t *testing.T) {
	root := t.TempDir()
	devDir := filepath.Join(root, ".devcontainer")
	if err := os.Mkdir(devDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"schemaVersion":1,"projectName":"legacy","preset":"node","database":"none","packageManager":"npm","languageVersion":"24","generatedBy":"projectsetup"}`
	if err := os.WriteFile(filepath.Join(devDir, "projectsetup.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runUpgrade(root, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "not normalized") {
		t.Fatalf("runUpgrade() error = %v, want malformed-manifest refusal", err)
	}
}

func TestRunInitRejectsAmbiguousManagers(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"package.json", "package-lock.json", "yarn.lock"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	err := runInit(root, []string{"--non-interactive"}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "multiple package managers detected") {
		t.Fatalf("runInit() error = %v", err)
	}
}

func TestRunInitInteractiveUsesDetectedDefaults(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"engines":{"node":"22"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package-lock.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err := runInit(root, nil, strings.NewReader("\n\ny\n"), &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("runInit() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Configuration:") || !strings.Contains(stdout.String(), "AI tools: opencode") {
		t.Fatalf("stdout does not contain normalized summary:\n%s", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".devcontainer", "projectsetup.json")); err != nil {
		t.Fatalf("generated projectsetup.json: %v", err)
	}
}

func TestRunInitInteractiveResolvesAmbiguity(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	for _, name := range []string{"package.json", "pyproject.toml", "package-lock.json", "yarn.lock"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	input := strings.NewReader("node\nyarn\nnone\nnone\n\ny\n")
	if err := runInit(root, nil, input, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}
}

func TestRunInitInteractiveCancellationWritesNothing(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"engines":{"node":"22"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package-lock.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := runInit(root, nil, strings.NewReader("\n\n\n"), &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".devcontainer")); !os.IsNotExist(err) {
		t.Fatalf(".devcontainer exists after cancellation: %v", err)
	}
	if !strings.Contains(stdout.String(), "Initialization cancelled") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestResolveAIToolsRejectsUnsupportedCombination(t *testing.T) {
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.String("ai", "", "")
	if err := flags.Parse([]string{"--ai", "claude"}); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveAITools("claude", flags); err == nil {
		t.Fatal("resolveAITools() accepted Claude without OpenCode")
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	err := Run([]string{"generate"}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), `unknown command "generate"`) {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunCheckReportsAggregatedFailure(t *testing.T) {
	root := t.TempDir()
	devDir := filepath.Join(root, ".devcontainer")
	if err := os.Mkdir(devDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(devDir, "projectsetup.json"), []byte(`{"schemaVersion":99}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	err := runCheck(root, nil, &bytes.Buffer{}, &stderr)
	if err == nil || !strings.Contains(err.Error(), "configuration check failed") {
		t.Fatalf("runCheck() error = %v", err)
	}
	if !strings.Contains(stderr.String(), "unsupported schemaVersion") || !strings.Contains(stderr.String(), "required generated file") {
		t.Fatalf("stderr does not contain aggregated diagnostics:\n%s", stderr.String())
	}
}

func TestRunDoctorRendersDistinctSeverities(t *testing.T) {
	root := t.TempDir()
	environment := doctor.Environment{
		GOOS:   "linux",
		GOARCH: "amd64",
		Getenv: func(string) string { return "" },
		LookPath: func(string) (string, error) {
			return "", os.ErrNotExist
		},
		ReadFile: func(string) ([]byte, error) { return nil, os.ErrNotExist },
	}
	var stdout, stderr bytes.Buffer
	err := runDoctor(root, nil, &stdout, &stderr, environment)
	if err == nil || !strings.Contains(err.Error(), "doctor found 3 error(s)") {
		t.Fatalf("runDoctor() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "INFO: host: GOOS=linux GOARCH=amd64") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "ERROR: docker: not found") || !strings.Contains(stderr.String(), "WARNING: SSH_AUTH_SOCK:") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
