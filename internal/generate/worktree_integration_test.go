package generate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mpm/projectsetup/internal/config"
)

// This opt-in test proves that a linked worktree can follow its absolute .git
// pointer because both the primary repository and global worktree root retain
// their host paths. It removes only its unique container/network and temp data.
func TestSmokeHostPathWorktree(t *testing.T) {
	if os.Getenv("PROJECTSETUP_WORKTREE_TESTS") != "1" {
		t.Skip("set PROJECTSETUP_WORKTREE_TESTS=1 to verify a real linked worktree in a container")
	}
	dworm, err := exec.LookPath("dworm")
	if err != nil {
		t.Fatal("dworm is not installed")
	}
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Fatal("docker is not installed")
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal("git is not installed")
	}

	root, home := t.TempDir(), t.TempDir()
	env := append(os.Environ(), "HOME="+home)
	run := func(name string, args ...string) []byte {
		t.Helper()
		command := exec.Command(name, args...)
		command.Env = env
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, output)
		}
		return output
	}
	run(git, "-C", root, "init", "--initial-branch=main")
	run(git, "-C", root, "config", "user.name", "projectsetup test")
	run(git, "-C", root, "config", "user.email", "projectsetup@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "primary.txt"), []byte("primary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(git, "-C", root, "add", "primary.txt")
	run(git, "-C", root, "commit", "-m", "initial")

	name := fmt.Sprintf("worktree-smoke-%d", time.Now().UnixNano())
	cfg, err := config.Normalize(config.Input{Root: root, ProjectName: name, Preset: "node", AITools: []config.AITool{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	worktreeRoot := config.WorktreeRootPath(home)
	worktree := filepath.Join(worktreeRoot, "project-digest", "stable-checkout")
	if err := os.MkdirAll(filepath.Dir(worktree), 0o700); err != nil {
		t.Fatal(err)
	}
	run(git, "-C", root, "worktree", "add", "-b", "worktree-test", worktree)
	t.Cleanup(func() {
		_ = os.Remove(filepath.Join(worktree, "container-marker"))
		command := exec.Command(git, "-C", root, "worktree", "remove", worktree)
		command.Env = env
		if output, err := command.CombinedOutput(); err != nil && !os.IsNotExist(err) {
			t.Errorf("remove test worktree: %v\n%s", err, output)
		}
	})

	t.Cleanup(func() {
		command := exec.Command(dworm, "remove", "-f")
		command.Dir = root
		command.Env = env
		if output, err := command.CombinedOutput(); err != nil {
			t.Errorf("remove worktree smoke container: %v\n%s", err, output)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	up := exec.CommandContext(ctx, dworm, "up", "-d")
	up.Dir = root
	up.Env = env
	output, err := up.CombinedOutput()
	if err != nil {
		t.Fatalf("dworm up -d: %v\n%s", err, output)
	}

	status := exec.CommandContext(ctx, dworm, "status", "--json")
	status.Dir = root
	status.Env = env
	output, err = status.Output()
	if err != nil {
		t.Fatalf("dworm status --json: %v\n%s", err, output)
	}
	var result struct {
		Container struct {
			ID string `json:"id"`
		} `json:"container"`
	}
	if err := json.Unmarshal(output, &result); err != nil || result.Container.ID == "" {
		t.Fatalf("parse dworm status output: %v\n%s", err, output)
	}

	probe := exec.CommandContext(ctx, dworm, "exec", "-w", worktree, "--", "bash", "-c", `
set -euo pipefail
	test "$PWD" = "$2"
	test "$(git -C "$1" rev-parse --show-toplevel)" = "$1"
	test "$(git rev-parse --show-toplevel)" = "$2"
	test "$(cat primary.txt)" = primary
	printf 'container\n' > container-marker
`, "worktree-probe", root, worktree)
	probe.Dir = root
	probe.Env = env
	if output, err := probe.CombinedOutput(); err != nil {
		t.Fatalf("dworm exec -w worktree probe: %v\n%s", err, output)
	}
	if marker, err := os.ReadFile(filepath.Join(worktree, "container-marker")); err != nil || string(marker) != "container\n" {
		t.Fatalf("host did not observe worktree write: %q, %v", marker, err)
	}

	inspect := exec.CommandContext(ctx, docker, "inspect", "--format", "{{json .Mounts}}", result.Container.ID)
	output, err = inspect.Output()
	if err != nil {
		t.Fatal(err)
	}
	var mounts []struct{ Source, Destination string }
	if err := json.Unmarshal(output, &mounts); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{root: root, worktreeRoot: worktreeRoot}
	for _, mount := range mounts {
		if source, ok := want[mount.Destination]; ok && source == mount.Source {
			delete(want, mount.Destination)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing identical host-path mounts %v in %s", want, output)
	}
}
