package cli

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/doctor"
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

func TestRunInitNonInteractive(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"engines":{"node":"22"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := runInit(root, []string{"--non-interactive", "--ai", "none", "--port", "3000"}, &bytes.Buffer{}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Generated .devcontainer") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".devcontainer", "devcontainer.json")); err != nil {
		t.Fatalf("generated devcontainer.json: %v", err)
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
