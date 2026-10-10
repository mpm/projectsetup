package generate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/presets"
	"github.com/mpm/projectsetup/internal/validate"
)

// Record the actual Docker configuration before the checker removes its probe.
type runtimeIntegrationRunner struct {
	probeInspections [][]byte
	inspectionErrors []error
}

func (*runtimeIntegrationRunner) LookPath(name string) (string, error) { return exec.LookPath(name) }

func (r *runtimeIntegrationRunner) Run(name string, args ...string) ([]byte, error) {
	command := exec.Command(name, args...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return append(output, stderr.Bytes()...), err
	}
	if name == "docker" && args[0] == "create" && slices.Contains(args, "none") {
		inspection, err := exec.Command("docker", "inspect", strings.TrimSpace(string(output))).CombinedOutput()
		r.probeInspections = append(r.probeInspections, inspection)
		r.inspectionErrors = append(r.inspectionErrors, err)
	}
	return output, nil
}

// Use real locally installed Node and gh releases, not illustrative declaration
// versions or synthetic binaries. No AI mounts, lifecycle, or feature downloads.
func TestSharedImageRuntimeIntegration(t *testing.T) {
	if os.Getenv("PROJECTSETUP_RUNTIME_TESTS") != "1" {
		t.Skip("set PROJECTSETUP_RUNTIME_TESTS=1 and PROJECTSETUP_RUNTIME_BASE to a local Node/gh vscode image")
	}
	base := os.Getenv("PROJECTSETUP_RUNTIME_BASE")
	if base == "" {
		t.Fatal("PROJECTSETUP_RUNTIME_BASE is required")
	}
	run := func(name string, args ...string) []byte {
		t.Helper()
		output, err := exec.Command(name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, output)
		}
		return output
	}
	run("docker", "image", "inspect", base)
	name := fmt.Sprintf("ps-runtime-%d", time.Now().UnixNano())
	image := name + "-base:latest"
	command := exec.Command("docker", "build", "-t", image, "-")
	command.Stdin = strings.NewReader("FROM " + base + "\nENV HOME=/home/vscode\nLABEL devcontainer.metadata=\"[]\"\nHEALTHCHECK --interval=1s CMD touch /home/vscode/image-healthcheck-marker\nUSER vscode\n")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build toolchain-only base: %v\n%s", err, output)
	}
	t.Cleanup(func() {
		if output, err := exec.Command("docker", "image", "rm", image).CombinedOutput(); err != nil {
			t.Errorf("remove owned base: %v\n%s", err, output)
		}
	})
	// Discover paths and releases on this local artifact explicitly. This is
	// fixture construction only; the production checker never infers claims.
	output := run("docker", "run", "--rm", "--network", "none", "--user", "vscode", "--env", "PATH=/usr/local/share/nvm/current/bin:/usr/bin:/bin", "--entrypoint", "/bin/bash", image, "--noprofile", "--norc", "-c", "command -v node; node --version; command -v gh; gh --version")
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) < 4 {
		t.Fatalf("unexpected tool discovery: %s", output)
	}
	nodePath, nodeVersion, ghPath := lines[0], strings.TrimPrefix(lines[1], "v"), lines[2]
	ghFields := strings.Fields(lines[3])
	if len(ghFields) < 3 {
		t.Fatalf("unexpected gh version: %s", output)
	}
	for _, scenario := range []string{"valid", "wrong-release", "missing-executable", "shadowed-command", "unsupported-custom", "broken-core", "corrupt-ca", "unwritable-home"} {
		t.Run(scenario, func(t *testing.T) {
			version, executable := nodeVersion, nodePath
			if scenario == "wrong-release" {
				version = "0.0.0"
			}
			if scenario == "missing-executable" {
				executable = path.Join(path.Dir(nodePath), "missing-node")
			}
			raw := fmt.Sprintf(`schema = 2
kind = "preset"
name = "runtime-test"
version = "1.0.0"
description = "Local artifact verification test"
[image]
base = %q
[image.preinstalled]
core_packages = ["bash", "ca-certificates", "curl", "git", "gnupg", "sudo"]
[image.preinstalled.tools.node]
version = %q
executable = %q
path = [%q]
[image.preinstalled.tools.gh]
version = %q
executable = %q
path = [%q]
[setup]
script = "touch project-setup-marker"
`, image, version, executable, path.Dir(executable), ghFields[2], ghPath, path.Dir(ghPath))
			if scenario == "broken-core" {
				raw = strings.Replace(raw, "[image]\n", "[image]\nroot_run = ['rm /etc/ssl/certs/ca-certificates.crt']\n", 1)
			}
			if scenario == "corrupt-ca" {
				raw = strings.Replace(raw, "[image]\n", "[image]\nroot_run = ['printf invalid-certificate > /etc/ssl/certs/ca-certificates.crt']\n", 1)
			}
			if scenario == "shadowed-command" {
				raw = strings.Replace(raw, "[image]\n", "[image]\nroot_run = ['mkdir -p /home/vscode/.local/bin && cp /bin/true /home/vscode/.local/bin/node']\n", 1)
			}
			if scenario == "unwritable-home" {
				raw = strings.Replace(raw, "[image]\n", "[image]\nroot_run = ['chmod 0555 /home/vscode']\n", 1)
			}
			if scenario == "unsupported-custom" {
				raw += fmt.Sprintf("[image.preinstalled.tools.team-cli]\nversion = 'release_1'\nexecutable = %q\npath = [%q]\n", nodePath, path.Dir(nodePath))
			}
			definition, err := presets.Parse([]byte(raw), "runtime-test.toml")
			if err != nil {
				t.Fatal(err)
			}
			registry, err := presets.NewRegistry(definition)
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			cfg, err := config.Normalize(config.Input{Root: root, ProjectName: name + "-" + scenario, Preset: definition.Name, Registry: registry, AITools: []config.AITool{}})
			if err != nil {
				t.Fatal(err)
			}
			if err := Write(root, cfg, false); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if output, err := exec.Command("docker", "image", "rm", cfg.Container.ComposeProjectName+"-app").CombinedOutput(); err != nil {
					t.Errorf("remove owned build image: %v\n%s", err, output)
				}
			})
			manifestPath := filepath.Join(root, ".devcontainer", "projectsetup.json")
			snapshotPath := filepath.Join(root, ".devcontainer", "presets", "runtime-test.toml")
			manifestBefore, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			snapshotBefore, err := os.ReadFile(snapshotPath)
			if err != nil {
				t.Fatal(err)
			}
			runner := &runtimeIntegrationRunner{}
			diagnostics := validate.Check(root, validate.Options{Runtime: true, Runner: runner})
			want := map[string]string{"wrong-release": "differs from declared", "missing-executable": "executable", "shadowed-command": "PATH lookup", "unsupported-custom": "no supported version probe", "broken-core": "core prerequisites", "corrupt-ca": "core prerequisites", "unwritable-home": "core prerequisites"}[scenario]
			if scenario == "valid" && validate.ErrorCount(diagnostics) != 0 || scenario != "valid" && (validate.ErrorCount(diagnostics) == 0 || !strings.Contains(fmt.Sprint(diagnostics), want)) {
				t.Fatalf("runtime diagnostics: %v", diagnostics)
			}
			if _, err := os.Stat(filepath.Join(root, "project-setup-marker")); !os.IsNotExist(err) {
				t.Fatal("verification ran project lifecycle")
			}
			if len(runner.probeInspections) != 1 || runner.inspectionErrors[0] != nil {
				t.Fatalf("runtime probe inspection: %s %v", runner.probeInspections, runner.inspectionErrors)
			}
			var probes []struct {
				Config struct {
					User, WorkingDir string
					Healthcheck      struct{ Test []string }
				}
				HostConfig struct{ NetworkMode string }
				Mounts     []any
			}
			if err := json.Unmarshal(runner.probeInspections[0], &probes); err != nil || len(probes) != 1 {
				t.Fatalf("parse runtime probe: %v %s", err, runner.probeInspections[0])
			}
			probe := probes[0]
			if !slices.Equal(probe.Config.Healthcheck.Test, []string{"NONE"}) || probe.HostConfig.NetworkMode != "none" || len(probe.Mounts) != 0 || probe.Config.User != "vscode" || probe.Config.WorkingDir != "/home/vscode" {
				t.Fatalf("runtime probe is not isolated: %s", runner.probeInspections[0])
			}
			containers := run("docker", "ps", "-aq", "--filter", "ancestor="+cfg.Container.ComposeProjectName+"-app")
			if strings.TrimSpace(string(containers)) != "" {
				t.Fatalf("probe leaked: %s", containers)
			}
			manifest, err := os.ReadFile(manifestPath)
			if err != nil || !bytes.Equal(manifestBefore, manifest) {
				t.Fatal("verification changed manifest")
			}
			snapshot, err := os.ReadFile(snapshotPath)
			if err != nil || !bytes.Equal(snapshotBefore, snapshot) {
				t.Fatal("verification changed definition snapshot")
			}
		})
	}
}
