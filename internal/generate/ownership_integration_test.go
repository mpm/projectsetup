package generate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/presets"
	"github.com/mpm/projectsetup/internal/validate"
)

// Opt-in, offline apart from the local Docker daemon. Derive an owned clean
// artifact, use only isolated fixture bind sources, never real credentials.
type ownershipIntegrationRunner struct {
	runtimeIntegrationRunner
	t *testing.T
}

func (r *ownershipIntegrationRunner) Run(name string, args ...string) ([]byte, error) {
	output, err := r.runtimeIntegrationRunner.Run(name, args...)
	if err != nil {
		r.t.Logf("%s %v failed: %s", name, args, output)
	}
	return output, err
}

func TestFixedOwnershipIntegration(t *testing.T) {
	if os.Getenv("PROJECTSETUP_OWNERSHIP_TESTS") != "1" {
		t.Skip("set PROJECTSETUP_OWNERSHIP_TESTS=1 and PROJECTSETUP_OWNERSHIP_BASE to a local compatible vscode/gh image")
	}
	base := os.Getenv("PROJECTSETUP_OWNERSHIP_BASE")
	if base == "" {
		t.Fatal("PROJECTSETUP_OWNERSHIP_BASE is required")
	}
	run := func(args ...string) string {
		t.Helper()
		output, err := exec.Command("docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %v: %v\n%s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	ids := strings.Fields(run("run", "--rm", "--network", "none", "--user", "vscode", "--entrypoint", "/bin/bash", base, "-c", "id -u; id -g; gh --version | head -1"))
	if len(ids) < 5 {
		t.Fatalf("unexpected base IDs/gh: %v", ids)
	}
	name := fmt.Sprintf("ps-ownership-%d", time.Now().UnixNano())
	image := name + "-base:latest"
	build := exec.Command("docker", "build", "-t", image, "-")
	build.Stdin = strings.NewReader("FROM " + base + "\nUSER root\nRUN rm -rf /home/vscode/.local /home/vscode/.config /home/vscode/.cache\nENV HOME=/home/vscode\nLABEL devcontainer.metadata=\"[]\"\nUSER vscode\n")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build isolated base: %v\n%s", err, output)
	}
	t.Cleanup(func() {
		output, err := exec.Command("docker", "image", "rm", image).CombinedOutput()
		if err != nil {
			t.Errorf("remove owned base: %v\n%s", err, output)
		}
	})
	raw := fmt.Sprintf(`schema = 2
kind = "preset"
name = "ownership-test"
version = "1.0.0"
description = "Local ownership verification"
[image]
base = %q
[image.ownership]
mode = "fixed"
uid = %s
gid = %s
[image.preinstalled]
core_packages = ["bash", "ca-certificates", "curl", "git", "gnupg", "sudo"]
[image.preinstalled.tools.gh]
version = %q
executable = "/usr/bin/gh"
path = ["/usr/bin"]
`, image, ids[0], ids[1], ids[4])
	d, err := presets.Parse([]byte(raw), "ownership-test.toml")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := presets.NewRegistry(d)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfg, err := config.Normalize(config.Input{Root: root, ProjectName: name, Preset: d.Name, Registry: registry, AITools: []config.AITool{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := exec.Command("docker", "image", "inspect", cfg.Container.ComposeProjectName+"-app").Run(); err != nil {
			return
		}
		output, err := exec.Command("docker", "image", "rm", cfg.Container.ComposeProjectName+"-app").CombinedOutput()
		if err != nil {
			t.Errorf("remove owned consumer: %v\n%s", err, output)
		}
	})
	if diagnostics := validate.Check(root, validate.Options{CheckHostMounts: true, Runtime: true, Runner: &ownershipIntegrationRunner{t: t}}); validate.ErrorCount(diagnostics) != 0 {
		t.Fatalf("fixed runtime checks: %v", diagnostics)
	}
	// The fixed contract rejects an artifact with different numeric IDs before
	// any consumer tool install. Keep the normal host contract unchanged and
	// exercise the generated build assertion directly with a mismatched fixture.
	files, err := render(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var mismatch string
	for _, f := range files {
		if f.name == "Dockerfile" {
			mismatch = strings.Replace(string(f.data), `test "$(id -u vscode)" = "`+ids[0]+`"`, `test "$(id -u vscode)" = "2147483647"`, 1)
		}
	}
	badBuild := exec.Command("docker", "build", "-")
	badBuild.Stdin = strings.NewReader(mismatch)
	failure, err := badBuild.CombinedOutput()
	if err == nil || !strings.Contains(string(failure), "base vscode IDs differ from image.ownership") {
		t.Fatalf("mismatched artifact accepted: %v\n%s", err, failure)
	}
	// First-run installer must repair root-owned container parents while leaving
	// a read-only nested host bind untouched; use fake installer, no downloads.
	bin, installation, state, nested := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	fakeCodexCurl(t, bin)
	writeTestFile(t, filepath.Join(nested, "sentinel"), "nested-host-state", 0400)
	script, err := filepath.Abs("templates/install-ai-tools.sh")
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"run", "--rm", "--network", "none", "--user", "vscode", "--env", "PATH=/fixtures:/usr/bin:/bin", "--env", "CODEX_HOME=/home/vscode/.codex", "--mount", "type=bind,source=" + script + ",target=/installer,readonly", "--mount", "type=bind,source=" + bin + ",target=/fixtures,readonly", "--mount", "type=bind,source=" + installation + ",target=/home/vscode/.local/share/codex", "--mount", "type=bind,source=" + state + ",target=/home/vscode/.codex", "--mount", "type=bind,source=" + nested + ",target=/home/vscode/.local/state/nested,readonly", "--entrypoint", "/bin/bash", image, "/installer", "codex"}
	output := run(args...)
	if !strings.Contains(output, "codex-1") {
		t.Fatalf("first-run output: %s", output)
	}
	if data, err := os.ReadFile(filepath.Join(nested, "sentinel")); err != nil || string(data) != "nested-host-state" {
		t.Fatal("nested host bind changed")
	}
	if info, err := os.Stat(filepath.Join(nested, "sentinel")); err != nil || info.Mode().Perm() != 0400 {
		t.Fatal("nested source permissions changed")
	}
	// Mount a read-only AI source: error must identify the host source and avoid
	// attempting to repair credentials/state or contacting an installer.
	blocked := []string{"run", "--rm", "--network", "none", "--user", "vscode", "--mount", "type=bind,source=" + script + ",target=/installer,readonly", "--mount", "type=bind,source=" + nested + ",target=/home/vscode/.opencode,readonly", "--entrypoint", "/bin/bash", image, "/installer", "opencode"}
	failure, err = exec.Command("docker", blocked...).CombinedOutput()
	if err == nil || !strings.Contains(string(failure), "host ~/.opencode") {
		t.Fatalf("unwritable mount: %v\n%s", err, failure)
	}
	t.Logf("verified fixed vscode %s:%s, merged UID policy, mount-free runtime, first-run nested read-only bind preservation, actionable unwritable AI mount", ids[0], ids[1])
}
