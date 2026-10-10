package generate

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/presets"
	"github.com/mpm/projectsetup/internal/validate"
)

func fixedImageInput(t *testing.T, uid, gid int) config.Input {
	t.Helper()
	raw := fmt.Sprintf(`schema = 2
kind = "preset"
name = "fixed-image"
version = "1.0.0"
description = "Ownership-only image fixture"
[image]
base = "registry.example/team/toolchain:fixed"
[image.ownership]
mode = "fixed"
uid = %d
gid = %d
`, uid, gid)
	d, err := presets.Parse([]byte(raw), "fixed-image.toml")
	if err != nil {
		t.Fatal(err)
	}
	d.Source = "user"
	registry, err := presets.NewRegistry(d)
	if err != nil {
		t.Fatal(err)
	}
	return config.Input{ProjectName: "fixed-app", Preset: "fixed-image", Registry: registry, AITools: []config.AITool{}}
}

func TestFixedImageGenerationAndHostChecks(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getuid() == 0 || os.Getgid() == 0 {
		t.Skip("requires ordinary Linux host IDs")
	}
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	input := fixedImageInput(t, os.Getuid(), os.Getgid())
	input.Root = root
	cfg, err := config.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	files, err := render(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var dockerfile, document []byte
	for _, f := range files {
		if f.name == "Dockerfile" {
			dockerfile = f.data
		}
		if f.name == "devcontainer.json" {
			document = f.data
		}
	}
	if !bytes.Contains(document, []byte(`"updateRemoteUserUID": false`)) {
		t.Fatal("missing fixed policy")
	}
	if !bytes.Contains(dockerfile, []byte(fmt.Sprintf(`test "$(id -u vscode)" = "%d"`, os.Getuid()))) {
		t.Fatal("missing base assertion")
	}
	if bytes.Contains(dockerfile, []byte("chown -R")) {
		t.Fatal("recursive image ownership repair")
	}
	if diagnostics := validate.Check(root, validate.Options{CheckHostMounts: true}); validate.ErrorCount(diagnostics) != 0 {
		t.Fatalf("%v", diagnostics)
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
	again, err := render(restored)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(again) {
		t.Fatal("snapshot regeneration changed tree")
	}
	for i := range files {
		if !bytes.Equal(files[i].data, again[i].data) {
			t.Fatalf("regeneration changed %s", files[i].name)
		}
	}
	// Changing the source selection must fail before replacing existing output.
	bad := fixedImageInput(t, os.Getuid()+1, os.Getgid())
	bad.Root = root
	badCfg, err := config.Normalize(bad)
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(root, badCfg, true); err == nil || !strings.Contains(err.Error(), "Linux host IDs") {
		t.Fatalf("mismatch accepted: %v", err)
	}
	current, err := os.ReadFile(filepath.Join(root, directoryName, "devcontainer.json"))
	if err != nil || !bytes.Equal(current, document) {
		t.Fatal("failed validation changed existing output")
	}
}
