package cli

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunHelp(t *testing.T) {
	var stdout bytes.Buffer
	if err := Run([]string{"--help"}, &stdout, &bytes.Buffer{}); err != nil {
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
	if err := runInit(root, []string{"--non-interactive", "--ai", "none", "--port", "3000"}, &stdout, &bytes.Buffer{}); err != nil {
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
	err := runInit(root, []string{"--non-interactive"}, &bytes.Buffer{}, &bytes.Buffer{})
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
	err := runInit(root, []string{"--non-interactive"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "multiple package managers detected") {
		t.Fatalf("runInit() error = %v", err)
	}
}

func TestRunInitTreatsExplicitFlagsAsNonInteractive(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	err := runInit(root, []string{"--preset", "node", "--ai", "none"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("runInit() error = %v", err)
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
	err := Run([]string{"generate"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), `unknown command "generate"`) {
		t.Fatalf("Run() error = %v", err)
	}
}
