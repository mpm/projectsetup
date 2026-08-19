package doctor_test

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/doctor"
)

func TestCheckReportsHealthyEnvironment(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	writeManifest(t, root, `["opencode","claude"]`)
	for _, relative := range []string{".cache/opencode", ".claude", ".config/opencode", ".local/share/claude", ".local/share/opencode", ".opencode"} {
		if err := os.MkdirAll(filepath.Join(home, relative), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sshSocket := unixSocket(t, filepath.Join(t.TempDir(), "ssh.sock"))
	gpgSocket := unixSocket(t, filepath.Join(t.TempDir(), "gpg.sock"))

	environment := commandEnvironment(map[string]string{
		"docker":       "/tools/docker",
		"devcontainer": "/tools/devcontainer",
		"dworm":        "/tools/dworm",
		"gpgconf":      "/tools/gpgconf",
	}, func(name string, args ...string) ([]byte, error) {
		switch {
		case name == "/tools/docker" && len(args) == 1 && args[0] == "info":
			return []byte("daemon ok"), nil
		case name == "/tools/gpgconf":
			return []byte(gpgSocket + "\n"), nil
		default:
			return []byte(filepath.Base(name) + " v1.2.3\n"), nil
		}
	})
	environment.GOOS = "linux"
	environment.GOARCH = "amd64"
	environment.Getenv = func(name string) string {
		if name == "SSH_AUTH_SOCK" {
			return sshSocket
		}
		return ""
	}
	environment.UserHomeDir = func() (string, error) { return home, nil }

	diagnostics := doctor.Check(root, environment)
	if count := doctor.ErrorCount(diagnostics); count != 0 {
		t.Fatalf("ErrorCount() = %d; diagnostics = %#v", count, diagnostics)
	}
	assertDiagnostic(t, diagnostics, doctor.Info, "docker", "/tools/docker (docker v1.2.3)")
	assertDiagnostic(t, diagnostics, doctor.Info, "docker daemon", "available")
	assertDiagnostic(t, diagnostics, doctor.Info, "SSH_AUTH_SOCK", "is a socket")
	assertDiagnostic(t, diagnostics, doctor.Info, "GPG agent", "is a socket")
	assertDiagnostic(t, diagnostics, doctor.Info, "AI host directory", "exists and is writable")
}

func TestCheckAggregatesFailuresAndWarnings(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	writeManifest(t, root, `["opencode"]`)
	if err := os.MkdirAll(filepath.Join(home, ".config/opencode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".opencode"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	environment := commandEnvironment(map[string]string{"docker": "/tools/docker"}, func(name string, args ...string) ([]byte, error) {
		if len(args) == 1 && args[0] == "info" {
			return []byte("cannot connect"), errors.New("exit status 1")
		}
		return []byte("Docker version 1"), nil
	})
	environment.GOOS = "darwin"
	environment.GOARCH = "arm64"
	environment.Getenv = func(name string) string {
		if name == "SSH_AUTH_SOCK" {
			return filepath.Join(root, "missing.sock")
		}
		return ""
	}
	environment.UserHomeDir = func() (string, error) { return home, nil }
	environment.CheckWritable = func(path string) error {
		if strings.HasSuffix(path, ".config/opencode") {
			return errors.New("permission denied")
		}
		return nil
	}

	diagnostics := doctor.Check(root, environment)
	if doctor.ErrorCount(diagnostics) < 5 {
		t.Fatalf("expected aggregated errors, got %#v", diagnostics)
	}
	assertDiagnostic(t, diagnostics, doctor.Warning, "dworm compatibility", "darwin/arm64")
	assertDiagnostic(t, diagnostics, doctor.Error, "docker daemon", "cannot connect")
	assertDiagnostic(t, diagnostics, doctor.Error, "devcontainer", "not found")
	assertDiagnostic(t, diagnostics, doctor.Error, "dworm", "not found")
	assertDiagnostic(t, diagnostics, doctor.Warning, "GPG agent", "gpgconf not found")
	assertDiagnostic(t, diagnostics, doctor.Warning, "AI host directory", "is missing")
	assertDiagnostic(t, diagnostics, doctor.Error, "AI host directory", "is not writable")
	assertDiagnostic(t, diagnostics, doctor.Error, "AI host directory", "is not a directory")
}

func TestCheckWithoutManifestReportsSkippedAIDirectories(t *testing.T) {
	environment := commandEnvironment(map[string]string{}, func(string, ...string) ([]byte, error) {
		return nil, errors.New("unexpected command")
	})
	environment.Getenv = func(string) string { return "" }
	diagnostics := doctor.Check(t.TempDir(), environment)
	assertDiagnostic(t, diagnostics, doctor.Info, "AI host directories", "no projectsetup manifest")
}

func commandEnvironment(paths map[string]string, run func(string, ...string) ([]byte, error)) doctor.Environment {
	return doctor.Environment{
		LookPath: func(name string) (string, error) {
			if path := paths[name]; path != "" {
				return path, nil
			}
			return "", fmt.Errorf("%s not found", name)
		},
		Run: run,
	}
}

func writeManifest(t *testing.T, root, tools string) {
	t.Helper()
	devDir := filepath.Join(root, ".devcontainer")
	if err := os.Mkdir(devDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"schemaVersion":1,"projectName":"test","preset":"node","database":"none","aiTools":` + tools + `,"languageVersion":"22","ports":[],"systemPackages":[],"generatedBy":"projectsetup"}`
	if err := os.WriteFile(filepath.Join(devDir, "projectsetup.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func unixSocket(t *testing.T, path string) string {
	t.Helper()
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return path
}

func assertDiagnostic(t *testing.T, diagnostics []doctor.Diagnostic, severity doctor.Severity, subject, message string) {
	t.Helper()
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == severity && diagnostic.Subject == subject && strings.Contains(diagnostic.Message, message) {
			return
		}
	}
	t.Errorf("missing %s diagnostic for %s containing %q in %#v", severity, subject, message, diagnostics)
}
