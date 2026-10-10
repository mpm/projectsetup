package validate

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/presets"
)

func TestHostIntegrationPlatformProblem(t *testing.T) {
	for _, tc := range []struct {
		os, host, context string
		bad               bool
	}{
		{"linux", "", "", false}, {"linux", "unix:///run/docker.sock", "default", false},
		{"darwin", "", "", true}, {"windows", "", "", true},
		{"linux", "ssh://server", "", true}, {"linux", "tcp://localhost:2375", "", true},
		{"linux", "unix:///run/docker.sock", "remote", true},
	} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			if got := hostIntegrationPlatformProblem(tc.os, tc.host, tc.context); (got != "") != tc.bad {
				t.Fatalf("problem = %q; want bad %v", got, tc.bad)
			}
		})
	}
}

func TestHostIntegrationDockerConfig(t *testing.T) {
	directory := t.TempDir()
	if err := hostIntegrationDockerConfig(directory); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		data string
		bad  bool
	}{{`{}`, false}, {`{"currentContext":"default"}`, false}, {`{"currentContext":"remote"}`, true}, {`invalid`, true}} {
		if err := os.WriteFile(filepath.Join(directory, "config.json"), []byte(tc.data), 0600); err != nil {
			t.Fatal(err)
		}
		if err := hostIntegrationDockerConfig(directory); (err != nil) != tc.bad {
			t.Fatalf("config %q: %v", tc.data, err)
		}
	}
}

func TestHostIntegrationSources(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only integration access")
	}
	t.Setenv("DOCKER_CONTEXT", "default")
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("PROJECTSETUP_MISSING_SOURCE", "")
	directory := t.TempDir()
	file := filepath.Join(directory, "file")
	if err := os.WriteFile(file, []byte("example"), 0600); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(directory, "socket")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	readOnly := true
	for _, tc := range []struct{ name, source, kind, want string }{
		{"directory", directory, "directory", ""}, {"file", file, "file", ""}, {"socket", socket, "socket", ""},
		{"missing variable", "${localEnv:PROJECTSETUP_MISSING_SOURCE}/file", "file", "PROJECTSETUP_MISSING_SOURCE"},
		{"missing source", filepath.Join(directory, "absent"), "socket", "must already exist"},
		{"file as directory", file, "directory", "must be a directory"}, {"directory as file", directory, "file", "must be a file"},
		{"file as socket", file, "socket", "must be a socket"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var messages []string
			validateHostIntegrations([]presets.BindMount{{Source: tc.source, Target: "/run/example", SourceKind: tc.kind, ReadOnly: &readOnly}}, nil, func(_ Severity, _ string, message string, args ...any) {
				messages = append(messages, fmt.Sprintf(message, args...))
			})
			got := strings.Join(messages, "\n")
			if tc.want == "" && got != "" || tc.want != "" && (!strings.Contains(got, tc.want) || !strings.Contains(got, "/run/example")) {
				t.Fatalf("diagnostics %q; want %q", got, tc.want)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(directory, "absent")); !os.IsNotExist(err) {
		t.Fatalf("missing source was created: %v", err)
	}
}

func TestProtectedHostSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ssh := filepath.Join(home, ".ssh")
	if err := os.Mkdir(ssh, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(ssh, "key")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(ssh, alias); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{home, ssh, file, alias, filepath.Join(alias, "key")} {
		if err := protectedHostSource(source); err == nil {
			t.Errorf("protected source %q accepted", source)
		}
	}
	if err := protectedHostSource(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	// A protected path itself can be a symlink to an innocuous-looking directory.
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(home, ".gitconfig")); err != nil {
		t.Fatal(err)
	}
	if err := protectedHostSource(target); err == nil {
		t.Fatal("resolved credential alias accepted")
	}
}
