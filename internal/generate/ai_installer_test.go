package generate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func runAIInstaller(t *testing.T, home, bin string, args ...string) (string, error) {
	t.Helper()
	script, err := filepath.Abs("templates/install-ai-tools.sh")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", append([]string{script}, args...)...)
	command.Env = append(os.Environ(), "HOME="+home, "CODEX_HOME="+filepath.Join(home, ".codex"),
		"PATH="+bin+":"+filepath.Join(home, ".local/bin")+":"+os.Getenv("PATH"))
	output, err := command.CombinedOutput()
	return string(output), err
}

func TestOpenCodeExplicitUpdate(t *testing.T) {
	for _, tt := range []struct {
		name, failure   string
		update, wantErr bool
	}{
		{"ordinary setup", "", false, false}, {"update", "", true, false},
		{"failed update", "upgrade", true, true}, {"broken updated binary", "version", true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			home, bin := t.TempDir(), t.TempDir()
			t.Setenv("TEST_FAILURE", tt.failure)
			writeTestFile(t, filepath.Join(bin, "curl"), "#!/bin/sh\nexit 99\n", 0755)
			writeTestFile(t, filepath.Join(home, ".opencode/bin/opencode"), `#!/bin/bash
if [[ "$1" == --version ]]; then
 if [[ -f "$HOME/updated" && "$TEST_FAILURE" == version ]]; then exit 1; fi
 echo opencode-test
elif [[ "$*" == "upgrade --method curl" ]]; then
 [[ "$TEST_FAILURE" != upgrade ]] || exit 2
 touch "$HOME/updated"
else
 exit 3
fi
`, 0755)
			args := []string{"opencode"}
			if tt.update {
				args = append([]string{"--update"}, args...)
			}
			output, err := runAIInstaller(t, home, bin, args...)
			if (err != nil) != tt.wantErr {
				t.Fatalf("%v\n%s", err, output)
			}
			_, statErr := os.Stat(filepath.Join(home, "updated"))
			wantUpdated := tt.update && tt.failure != "upgrade"
			if (statErr == nil) != wantUpdated {
				t.Fatalf("update marker: %v\n%s", statErr, output)
			}
		})
	}
}

const fakeCodexInstaller = `#!/bin/sh
set -eu
test "$CODEX_NON_INTERACTIVE" = 1
test "$CODEX_HOME" = "$HOME/.local/share/codex"
test "$CODEX_INSTALL_DIR" = "$CODEX_HOME/bin"
test "${TEST_FAILURE:-}" != install
release="$CODEX_HOME/packages/standalone/releases/${TEST_CODEX_VERSION:-1}-linux"
mkdir -p "$release/bin" "$CODEX_INSTALL_DIR"
printf '#!/bin/sh\necho codex-%s\n' "${TEST_CODEX_VERSION:-1}" > "$release/bin/codex"
chmod +x "$release/bin/codex"
ln -sfn "$release" "$CODEX_HOME/packages/standalone/current"
ln -sfn "$CODEX_HOME/packages/standalone/current/bin/codex" "$CODEX_INSTALL_DIR/codex"
`

func fakeCodexCurl(t *testing.T, bin string) {
	t.Helper()
	writeTestFile(t, filepath.Join(bin, "installer"), fakeCodexInstaller, 0644)
	writeTestFile(t, filepath.Join(bin, "curl"), `#!/bin/sh
set -eu
test "$*" != ""
test "${TEST_FAILURE:-}" != download
test "$2" = https://chatgpt.com/codex/install.sh
test "$3" = -o
cp "$(dirname "$0")/installer" "$4"
`, 0755)
}

func TestCodexSharedInstallationAndUpdate(t *testing.T) {
	home, bin := t.TempDir(), t.TempDir()
	fakeCodexCurl(t, bin)
	t.Setenv("TEST_FAILURE", "")
	t.Setenv("TEST_CODEX_VERSION", "1")
	writeTestFile(t, filepath.Join(home, ".codex/auth.json"), "test-credential-sentinel", 0600)
	writeTestFile(t, filepath.Join(home, ".codex/history.jsonl"), "test-history-sentinel", 0600)
	if output, err := runAIInstaller(t, home, bin, "codex"); err != nil {
		t.Fatalf("%v\n%s", err, output)
	}
	t.Setenv("TEST_FAILURE", "download")
	if output, err := runAIInstaller(t, home, bin, "codex"); err != nil {
		t.Fatalf("%v\n%s", err, output)
	}
	t.Setenv("TEST_FAILURE", "")
	t.Setenv("TEST_CODEX_VERSION", "2")
	if output, err := runAIInstaller(t, home, bin, "--update", "codex"); err != nil || !strings.Contains(output, "codex-2") {
		t.Fatalf("%v\n%s", err, output)
	}
	// Moving the entire home invalidates every absolute link to its former path.
	relocated := filepath.Join(t.TempDir(), "different-home")
	if err := os.Rename(home, relocated); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(filepath.Join(relocated, ".local/bin/codex"), "--version").CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "codex-2" {
		t.Fatalf("relocated command: %v\n%s", err, output)
	}
	for name, want := range map[string]string{"auth.json": "test-credential-sentinel", "history.jsonl": "test-history-sentinel"} {
		data, err := os.ReadFile(filepath.Join(relocated, ".codex", name))
		if err != nil || string(data) != want {
			t.Fatalf("state %s was changed: %v", name, err)
		}
	}
}

func TestCodexInstallationFailures(t *testing.T) {
	for _, failure := range []string{"download", "install", "incompatible", "broken-link"} {
		t.Run(failure, func(t *testing.T) {
			home, bin := t.TempDir(), t.TempDir()
			fakeCodexCurl(t, bin)
			t.Setenv("TEST_FAILURE", failure)
			target := filepath.Join(home, ".local/share/codex/bin/codex")
			if failure == "incompatible" {
				writeTestFile(t, target, "#!/bin/sh\nexit 126\n", 0755)
			}
			if failure == "broken-link" {
				if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("/missing-host-path/codex", target); err != nil {
					t.Fatal(err)
				}
			}
			output, err := runAIInstaller(t, home, bin, "--update", "codex")
			if err == nil {
				t.Fatalf("expected error: %s", output)
			}
			if (failure == "incompatible" || failure == "broken-link") && !strings.Contains(output, "host and container") {
				t.Fatalf("unhelpful error: %s", output)
			}
		})
	}
}

func TestAIInstallerRejectsArgumentsBeforeMutating(t *testing.T) {
	for _, args := range [][]string{{"--update"}, {"--unknown"}, {"opencode", "bogus"}, {"--update", "codex", "none"}} {
		home, bin := t.TempDir(), t.TempDir()
		output, err := runAIInstaller(t, home, bin, args...)
		if err == nil {
			t.Fatalf("%v accepted: %s", args, output)
		}
		entries, _ := os.ReadDir(home)
		if len(entries) != 0 {
			t.Fatalf("%v mutated home before validation", args)
		}
	}
}

func TestConcurrentCodexSetup(t *testing.T) {
	home, bin := t.TempDir(), t.TempDir()
	fakeCodexCurl(t, bin)
	t.Setenv("TEST_FAILURE", "")
	t.Setenv("TEST_CODEX_VERSION", "1")
	errors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			output, err := runAIInstaller(t, home, bin, "codex")
			if err != nil {
				err = fmt.Errorf("%w: %s", err, output)
			}
			errors <- err
		}()
	}
	for i := 0; i < 2; i++ {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
}

func TestClaudeExplicitUpdate(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			home, bin := t.TempDir(), t.TempDir()
			t.Setenv("TEST_FAILURE", "")
			if failed {
				t.Setenv("TEST_FAILURE", "update")
			}
			writeTestFile(t, filepath.Join(bin, "curl"), "#!/bin/sh\nexit 99\n", 0755)
			old := filepath.Join(home, ".local/share/claude/versions/1")
			writeTestFile(t, old, `#!/bin/bash
if [[ "$1" == --version ]]; then
 echo claude-1
elif [[ "$1" == update ]]; then
 [[ "$TEST_FAILURE" != update ]] || exit 1
 printf '#!/bin/sh\necho claude-2\n' > "$HOME/.local/share/claude/versions/2"
 chmod +x "$HOME/.local/share/claude/versions/2"
else
 exit 2
fi
`, 0755)
			if err := os.Chtimes(old, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour)); err != nil {
				t.Fatal(err)
			}
			output, err := runAIInstaller(t, home, bin, "--update", "claude")
			if (err != nil) != failed {
				t.Fatalf("%v\n%s", err, output)
			}
			if !failed && !strings.Contains(output, "claude-2") {
				t.Fatalf("updated binary was not relinked: %s", output)
			}
		})
	}
}

func TestOpenCodeInstallsMissingOrBrokenBinary(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			home, bin := t.TempDir(), t.TempDir()
			if existing {
				writeTestFile(t, filepath.Join(home, ".opencode/bin/opencode"), "#!/bin/sh\nexit 126\n", 0755)
			}
			writeTestFile(t, filepath.Join(bin, "curl"), `#!/bin/sh
cat <<'INSTALLER'
mkdir -p "$HOME/.opencode/bin"
printf '#!/bin/sh\necho opencode-repaired\n' > "$HOME/.opencode/bin/opencode"
chmod +x "$HOME/.opencode/bin/opencode"
INSTALLER
`, 0755)
			output, err := runAIInstaller(t, home, bin, "opencode")
			if err != nil || !strings.Contains(output, "opencode-repaired") {
				t.Fatalf("%v\n%s", err, output)
			}
		})
	}
}
