package generate

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/presets"
	"github.com/mpm/projectsetup/internal/validate"
	yaml "go.yaml.in/yaml/v3"
)

func waylandInput(t *testing.T) config.Input {
	t.Helper()
	raw := `schema = 2
kind = "addon"
name = "wayland"
version = "1.0.0"
description = "Explicit Linux Wayland socket"
[[container.mounts]]
source = "${localEnv:XDG_RUNTIME_DIR}/${localEnv:WAYLAND_DISPLAY}"
target = "/run/host-wayland/wayland-0"
source_kind = "socket"
read_only = true
[container.env]
WAYLAND_DISPLAY = "/run/host-wayland/wayland-0"
`
	d, err := presets.Parse([]byte(raw), "wayland.toml")
	if err != nil {
		t.Fatal(err)
	}
	d.Source = "user"
	node, _ := presets.Builtin().Lookup("node")
	registry, err := presets.NewRegistry(node, d)
	if err != nil {
		t.Fatal(err)
	}
	return config.Input{ProjectName: "wayland-app", Preset: "node", Addons: []string{"wayland"}, Registry: registry, AITools: []config.AITool{}}
}

func TestHostMountSnapshotRegeneration(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux integrations")
	}
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("DOCKER_CONFIG", t.TempDir())
	source := t.TempDir()
	listener, err := net.Listen("unix", filepath.Join(source, "wayland-0"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("XDG_RUNTIME_DIR", source)
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	root := t.TempDir()
	input := waylandInput(t)
	input.Root = root
	cfg, err := config.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile(filepath.Join(root, directoryName, "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(doc, []byte("source: ${XDG_RUNTIME_DIR:?")) || !bytes.Contains(doc, []byte("read_only: true")) {
		t.Fatalf("%s", doc)
	}
	snapshot, err := os.ReadFile(filepath.Join(root, directoryName, "presets", "wayland.toml"))
	if err != nil || !bytes.Contains(snapshot, []byte("${localEnv:XDG_RUNTIME_DIR}/${localEnv:WAYLAND_DISPLAY}")) {
		t.Fatalf("snapshot lost localEnv references: %v\n%s", err, snapshot)
	}
	if ds := validate.Check(root, validate.Options{CheckHostMounts: true}); validate.ErrorCount(ds) != 0 {
		t.Fatal(ds)
	}
	manifestFile, err := os.Open(filepath.Join(root, directoryName, "projectsetup.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := config.ReadManifest(manifestFile)
	manifestFile.Close()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := presets.LoadProject(filepath.Join(root, directoryName, "presets"), manifest.Refs())
	if err != nil {
		t.Fatal(err)
	}
	restored, err := config.Normalize(manifest.Input(root, registry))
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(root, restored, true); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(filepath.Join(root, directoryName, "compose.yaml"))
	if !bytes.Equal(doc, again) {
		t.Fatal("upgrade changed host references")
	}
	os.Unsetenv("WAYLAND_DISPLAY")
	if err := Write(root, restored, true); err == nil || !strings.Contains(err.Error(), "WAYLAND_DISPLAY") {
		t.Fatalf("missing host env accepted: %v", err)
	}
	still, _ := os.ReadFile(filepath.Join(root, directoryName, "compose.yaml"))
	if !bytes.Equal(doc, still) {
		t.Fatal("failed upgrade replaced files")
	}
}

const hostIntegrationsDefinition = `schema = 2
kind = "addon"
name = "integrations"
version = "1.0.0"
description = "Read-only and read-write host integrations"
[[container.mounts]]
source = "${localEnv:HOME}/.local/share/acp-tools"
target = "/opt/acp-tools"
source_kind = "directory"
read_only = true
[[container.mounts]]
source = "${localEnv:PROJECTSETUP_TEST_CONFIG}"
target = "/run/host-config/settings.json"
source_kind = "file"
read_only = false
[[container.mounts]]
source = "${localEnv:XDG_RUNTIME_DIR}/projectsetup-test.sock"
target = "/run/host-agent.sock"
source_kind = "socket"
read_only = true
`

// hostIntegrationEnv satisfies the required Compose variables of the
// wayland and host-integrations fixtures; sources need not exist for config.
var hostIntegrationEnv = []string{"XDG_RUNTIME_DIR=/run/user/1000", "WAYLAND_DISPLAY=wayland-0", "PROJECTSETUP_TEST_CONFIG=/tmp/projectsetup-test/settings.json"}

func hostIntegrationsInput(t *testing.T) config.Input {
	t.Helper()
	d, err := presets.Parse([]byte(hostIntegrationsDefinition), "integrations.toml")
	if err != nil {
		t.Fatal(err)
	}
	d.Source = "user"
	node, _ := presets.Builtin().Lookup("node")
	registry, err := presets.NewRegistry(node, d)
	if err != nil {
		t.Fatal(err)
	}
	return config.Input{ProjectName: "host-integrations", Preset: "node", Addons: []string{"integrations"}, Registry: registry, AITools: []config.AITool{}, Options: map[string]map[string]string{"node": {"package_manager": "npm"}}}
}

type composeBind struct {
	Type, Source, Target string
	ReadOnly             bool `json:"read_only"`
}

// The Dev Container CLI drops readonly from devcontainer.json mounts of
// Compose projects, so typed binds must reach Compose in long syntax.
func TestHostIntegrationsRenderAsComposeBinds(t *testing.T) {
	cfg, err := config.Normalize(hostIntegrationsInput(t))
	if err != nil {
		t.Fatal(err)
	}
	files, err := render(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]struct{ Volumes []any }
	}
	var devcontainer map[string]any
	for _, file := range files {
		switch file.name {
		case "compose.yaml":
			if err := yaml.Unmarshal(file.data, &compose); err != nil {
				t.Fatal(err)
			}
		case "devcontainer.json":
			if err := json.Unmarshal(file.data, &devcontainer); err != nil {
				t.Fatal(err)
			}
		}
	}
	if mounts := devcontainer["mounts"]; !reflect.DeepEqual(mounts, []any{config.WorkspaceHostMount}) {
		t.Fatalf("devcontainer.json mounts = %v; want only the core host-path workspace mount", mounts)
	}
	want := []any{
		"..:/workspaces/host-integrations",
		map[string]any{"type": "bind", "source": config.ComposeWorktreeRoot, "target": config.ComposeWorktreeRoot, "bind": map[string]any{"create_host_path": false}},
		map[string]any{"type": "bind", "source": "${HOME:?export it on the host for projectsetup host integration /opt/acp-tools}/.local/share/acp-tools", "target": "/opt/acp-tools", "read_only": true},
		map[string]any{"type": "bind", "source": "${XDG_RUNTIME_DIR:?export it on the host for projectsetup host integration /run/host-agent.sock}/projectsetup-test.sock", "target": "/run/host-agent.sock", "read_only": true},
		map[string]any{"type": "bind", "source": "${PROJECTSETUP_TEST_CONFIG:?export it on the host for projectsetup host integration /run/host-config/settings.json}", "target": "/run/host-config/settings.json"},
	}
	if got := compose.Services["app"].Volumes; !reflect.DeepEqual(got, want) {
		t.Fatalf("app volumes = %#v\nwant %#v", got, want)
	}
}

func TestHostIntegrationComposeResolution(t *testing.T) {
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Skip("docker is not installed")
	}
	if _, err := exec.Command(docker, "compose", "version").CombinedOutput(); err != nil {
		t.Skip("docker compose is not installed")
	}
	compose := filepath.Join("testdata", "golden", "host-integrations", "compose.yaml")
	command := exec.Command(docker, "compose", "-f", compose, "config", "--format", "json")
	command.Env = append(os.Environ(), "HOME=/home/host", "PROJECTSETUP_TEST_CONFIG=/srv/settings.json", "XDG_RUNTIME_DIR=/run/user/1000")
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Services map[string]struct{ Volumes []composeBind }
	}
	if err := json.Unmarshal(output, &document); err != nil {
		t.Fatal(err)
	}
	want := []composeBind{
		{"bind", "/home/host/.local/share/acp-tools", "/opt/acp-tools", true},
		{"bind", "/run/user/1000/projectsetup-test.sock", "/run/host-agent.sock", true},
		{"bind", "/srv/settings.json", "/run/host-config/settings.json", false},
	}
	if got := document.Services["app"].Volumes[2:]; !reflect.DeepEqual(got, want) {
		t.Fatalf("resolved binds = %#v\nwant %#v", got, want)
	}
	for _, empty := range []string{"PROJECTSETUP_TEST_CONFIG=", "XDG_RUNTIME_DIR="} {
		name := strings.TrimSuffix(empty, "=")
		t.Run(name, func(t *testing.T) {
			command := exec.Command(docker, "compose", "-f", compose, "config")
			command.Env = append(os.Environ(), "HOME=/home/host", "PROJECTSETUP_TEST_CONFIG=/srv/settings.json", "XDG_RUNTIME_DIR=/run/user/1000", empty)
			output, err := command.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "required variable "+name+" is missing a value: export it on the host for projectsetup host integration") {
				t.Fatalf("empty %s accepted or unclear: %v\n%s", name, err, output)
			}
		})
	}
}

// This opt-in test starts a real container through the Dev Container CLI
// and asserts the access mode Docker applied, not only the generated files.
// It uses temporary host sources and removes only its own Compose project.
func TestSmokeHostIntegrationAccessModes(t *testing.T) {
	if os.Getenv("PROJECTSETUP_SMOKE_TESTS") != "1" {
		t.Skip("set PROJECTSETUP_SMOKE_TESTS=1 to verify host integration access modes in a real container")
	}
	if runtime.GOOS != "linux" {
		t.Skip("Linux integrations")
	}
	devcontainer, err := exec.LookPath("devcontainer")
	if err != nil {
		t.Fatal(err)
	}
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Fatal(err)
	}
	root, home, runtimeDir := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.MkdirAll(config.WorktreeRootPath(home), 0o700); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(home, "settings.json")
	writeTestFile(t, filepath.Join(home, ".local/share/acp-tools/marker"), "tools\n", 0o644)
	writeTestFile(t, settings, "{}\n", 0o644)
	listener, err := net.Listen("unix", filepath.Join(runtimeDir, "projectsetup-test.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	env := append(os.Environ(), "HOME="+home, "PROJECTSETUP_TEST_CONFIG="+settings, "XDG_RUNTIME_DIR="+runtimeDir)
	if err := replaceTree(filepath.Join(root, directoryName), filepath.Join("testdata", "golden", "host-integrations")); err != nil {
		t.Fatal(err)
	}
	compose := filepath.Join(root, directoryName, "compose.yaml")
	t.Cleanup(func() {
		down := exec.Command(docker, "compose", "-f", compose, "down", "--volumes")
		down.Env = env
		if output, err := down.CombinedOutput(); err != nil {
			t.Errorf("remove host integration smoke project: %v\n%s", err, output)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	up := exec.CommandContext(ctx, devcontainer, "up", "--workspace-folder", root)
	up.Env = env
	var stderr bytes.Buffer
	up.Stderr = &stderr
	output, err := up.Output()
	if err != nil {
		log := stderr.String()
		if len(log) > 12000 {
			log = log[len(log)-12000:]
		}
		t.Fatalf("devcontainer up: %v\n%s\n%s", err, output, log)
	}
	var result struct {
		ContainerID string `json:"containerId"`
	}
	if err := json.Unmarshal(output, &result); err != nil || result.ContainerID == "" {
		t.Fatalf("parse container result: %v\n%s", err, output)
	}
	output, err = exec.CommandContext(ctx, docker, "inspect", "--format", "{{json .Mounts}}", result.ContainerID).Output()
	if err != nil {
		t.Fatal(err)
	}
	var mounts []struct {
		Destination string
		RW          bool
	}
	if err := json.Unmarshal(output, &mounts); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"/opt/acp-tools": false, "/run/host-agent.sock": false, "/run/host-config/settings.json": true}
	for _, mount := range mounts {
		if rw, ok := want[mount.Destination]; ok {
			if mount.RW != rw {
				t.Errorf("%s RW=%t; want %t", mount.Destination, mount.RW, rw)
			}
			delete(want, mount.Destination)
		}
	}
	if len(want) > 0 {
		t.Fatalf("missing mounts %v in %s", want, output)
	}
	probe := exec.CommandContext(ctx, docker, "exec", "--user", "vscode", result.ContainerID, "bash", "-c", `
set -euo pipefail
test "$(cat /opt/acp-tools/marker)" = tools
if touch /opt/acp-tools/new 2>/dev/null; then echo "read-only directory is writable"; exit 1; fi
printf '{"written":true}\n' > /run/host-config/settings.json
`)
	if output, err := probe.CombinedOutput(); err != nil {
		t.Fatalf("container access probe: %v\n%s", err, output)
	}
	if data, err := os.ReadFile(settings); err != nil || string(data) != "{\"written\":true}\n" {
		t.Fatalf("read-write file not shared: %q, %v", data, err)
	}
}
