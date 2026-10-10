package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/generate"
	"github.com/mpm/projectsetup/internal/presets"
)

func TestListOptionsPreinstalledFromUserRegistry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(presets.ConfigDirEnv, dir)
	if err := os.Mkdir(filepath.Join(dir, "presets"), 0o755); err != nil {
		t.Fatal(err)
	}
	source := `schema = 2
kind = "preset"
name = "shared"
version = "1.0.0"
description = "Shared toolchain"
[image]
base = "debian:trixie"
[image.preinstalled]
core_packages = ["git"]
[image.preinstalled.tools.node]
version = "22.14.0"
executable = "/opt/node/bin/node"
path = ["/opt/node/bin"]
`
	if err := os.WriteFile(filepath.Join(dir, "presets", "shared.toml"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	var output bytes.Buffer
	if err := runInit(root, []string{"--list-options", "--json"}, strings.NewReader(""), &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var listing config.Options
	if err := json.Unmarshal(output.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	info := listing.Definitions["shared"]
	if listing.SchemaVersion != 2 || info.DefinitionSchema != 2 || info.Preinstalled == nil || info.Preinstalled.Tools["node"].Version != "22.14.0" || len(info.Options) != 0 {
		t.Fatalf("listing = %s", output.Bytes())
	}
	output.Reset()
	if err := runInit(root, []string{"--list-options"}, strings.NewReader(""), &output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"shared image declarations (fixed, not --set options)", "core packages: git", "node: 22.14.0; executable /opt/node/bin/node; PATH /opt/node/bin"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %s; want %s", output.String(), want)
		}
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatalf("listing changed root: %v, %v", entries, err)
	}
	// Generate the snapshot, then change the registry. Ordinary upgrades must
	// retain the original claims; only explicit refresh adopts the new artifact.
	t.Setenv("HOME", t.TempDir())
	registry, err := presets.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Normalize(config.Input{Root: root, ProjectName: "app", Registry: registry, Preset: "shared", AITools: []config.AITool{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := generate.Write(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(source, "22.14.0", "22.14.1", 1)
	if err := os.WriteFile(filepath.Join(dir, "presets", "shared.toml"), []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(root, ".devcontainer", "presets", "shared.toml")
	manifest := filepath.Join(root, ".devcontainer", "projectsetup.json")
	before, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := runUpgrade(root, nil, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(snapshot)
	if err != nil || string(data) != source {
		t.Fatalf("ordinary upgrade changed snapshot: %s, %v", data, err)
	}
	after, err := os.ReadFile(manifest)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("ordinary upgrade changed manifest: %s, %v", after, err)
	}
	if err := runUpgrade(root, []string{"--refresh-presets"}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(snapshot)
	if err != nil || string(data) != changed {
		t.Fatalf("refresh did not adopt snapshot: %s, %v", data, err)
	}
	// Refresh must validate the original snapshot before using registry data.
	if err := os.WriteFile(snapshot, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runUpgrade(root, []string{"--refresh-presets"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("tampered refresh error = %v", err)
	}
}
