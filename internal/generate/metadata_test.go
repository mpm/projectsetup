package generate

import (
	"encoding/json"
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

// This focused build uses a local image and no feature downloads or apt work.
// It verifies metadata isolation, not the declared tools' actual versions.
func TestSharedImageMetadataIntegration(t *testing.T) {
	if os.Getenv("PROJECTSETUP_METADATA_TESTS") != "1" {
		t.Skip("set PROJECTSETUP_METADATA_TESTS=1 and PROJECTSETUP_METADATA_BASE to a local compatible vscode image")
	}
	base := os.Getenv("PROJECTSETUP_METADATA_BASE")
	if base == "" {
		t.Fatal("PROJECTSETUP_METADATA_BASE must name a local Debian/Ubuntu image with vscode, /bin/bash and core prerequisites")
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
	name := fmt.Sprintf("ps-metadata-%d", time.Now().UnixNano())
	image := name + "-base:latest"
	label, _ := json.Marshal([]map[string]any{
		{"id": "ghcr.io/devcontainers/features/node:metadata-only-invalid", "containerEnv": map[string]string{"PATH": "/inherited/bin", "BAKED_PROJECT": "wrong"}, "remoteEnv": map[string]string{"HOME": "/root"}},
		{"containerUser": "root", "remoteUser": "root", "updateRemoteUserUID": false, "postCreateCommand": "touch /inherited-project-setup", "postStartCommand": "npm start", "mounts": []string{"source=/inherited-project,target=/old-project,type=bind"}},
	})
	quoted, _ := json.Marshal(string(label))
	command := exec.Command("docker", "build", "-t", image, "-")
	command.Stdin = strings.NewReader("FROM " + base + "\nENV IMAGE_KEEP=available HOME=/home/vscode\nLABEL devcontainer.metadata=" + string(quoted) + "\n")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build metadata base: %v\n%s", err, output)
	}
	t.Cleanup(func() {
		if output, err := exec.Command("docker", "image", "rm", image).CombinedOutput(); err != nil {
			t.Errorf("remove test base: %v\n%s", err, output)
		}
	})
	raw := fmt.Sprintf(`schema = 2
kind = "preset"
name = "metadata-test"
version = "1.0.0"
description = "Metadata isolation test only"
[image]
base = %q
[image.preinstalled]
core_packages = ["bash", "ca-certificates", "curl", "git", "gnupg", "sudo"]
[image.preinstalled.tools.gh]
version = "2.0.0"
executable = "/usr/bin/gh"
path = ["/usr/bin"]
[container]
env = { TEAM = "project" }
[setup]
script = "touch project-setup-marker"
`, image)
	definition, err := presets.Parse([]byte(raw), "metadata-test.toml")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := presets.NewRegistry(definition)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfg, err := config.Normalize(config.Input{Root: root, ProjectName: name, Preset: definition.Name, Registry: registry, AITools: []config.AITool{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { exec.Command("docker", "image", "rm", cfg.Container.ComposeProjectName+"-app").Run() })
	diagnostics := validate.Check(root, validate.Options{External: true, Build: true})
	if validate.ErrorCount(diagnostics) == 0 || !strings.Contains(fmt.Sprint(diagnostics), "inherited project behavior") {
		t.Fatalf("unsafe metadata was not rejected: %#v", diagnostics)
	}
	// Rebuild the recipe's metadata, keeping baked feature IDs as descriptive
	// records. Their invalid installer tag must never cause a feature fetch.
	label, _ = json.Marshal([]map[string]any{{"id": "ghcr.io/devcontainers/features/node:metadata-only-invalid", "containerEnv": map[string]string{"PATH": "/inherited/bin"}, "remoteUser": "root"}})
	quoted, _ = json.Marshal(string(label))
	command = exec.Command("docker", "build", "-t", image, "-")
	command.Stdin = strings.NewReader("FROM " + base + "\nENV IMAGE_KEEP=available HOME=/home/vscode\nLABEL devcontainer.metadata=" + string(quoted) + "\n")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build toolchain metadata: %v\n%s", err, output)
	}
	diagnostics = validate.Check(root, validate.Options{External: true, Build: true})
	if validate.ErrorCount(diagnostics) != 0 {
		t.Fatalf("metadata build check: %#v", diagnostics)
	}
	if _, err := os.Stat(filepath.Join(root, "project-setup-marker")); !os.IsNotExist(err) {
		t.Fatal("metadata inspection executed project setup")
	}
	output := run("docker", "image", "inspect", cfg.Container.ComposeProjectName+"-app", "--format", "{{json .Config.Env}}")
	if !strings.Contains(string(output), "IMAGE_KEEP=available") {
		t.Fatal("metadata reset removed baked image ENV")
	}
	// Consumption keeps baked metadata IDs; it does not request their installer.
	output = run("docker", "image", "inspect", image, "--format", "{{index .Config.Labels \"devcontainer.metadata\"}}")
	if !strings.Contains(string(output), "metadata-only-invalid") {
		t.Fatal("base metadata was modified")
	}
}
