package generate

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/presets"
	"github.com/mpm/projectsetup/internal/validate"
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
	doc, err := os.ReadFile(filepath.Join(root, directoryName, "devcontainer.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(doc, []byte("${localEnv:XDG_RUNTIME_DIR}/${localEnv:WAYLAND_DISPLAY}")) || !bytes.Contains(doc, []byte(`"WAYLAND_DISPLAY": "/run/host-wayland/wayland-0"`)) {
		t.Fatalf("%s", doc)
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
	again, _ := os.ReadFile(filepath.Join(root, directoryName, "devcontainer.json"))
	if !bytes.Equal(doc, again) {
		t.Fatal("upgrade changed host references")
	}
	os.Unsetenv("WAYLAND_DISPLAY")
	if err := Write(root, restored, true); err == nil || !strings.Contains(err.Error(), "WAYLAND_DISPLAY") {
		t.Fatalf("missing host env accepted: %v", err)
	}
	still, _ := os.ReadFile(filepath.Join(root, directoryName, "devcontainer.json"))
	if !bytes.Equal(doc, still) {
		t.Fatal("failed upgrade replaced files")
	}
}
