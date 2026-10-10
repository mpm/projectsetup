package config_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/presets"
)

const sharedDefinition = `schema = 2
kind = "preset"
name = "shared"
version = "1.0.0"
description = "Fixed image contract"
[image]
base = "registry.example/team/toolchain@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
[image.preinstalled]
core_packages = ["git", "curl"]
[image.preinstalled.tools.go]
version = "1.27.2"
executable = "/opt/go/bin/go"
path = ["/opt/go/bin"]
`

func TestNormalizePreinstalledAndSnapshotRoundTrip(t *testing.T) {
	definition, err := presets.Parse([]byte(sharedDefinition), "shared.toml")
	if err != nil {
		t.Fatal(err)
	}
	definition.Source = presets.SourceUser
	sqlite, _ := presets.Builtin().Lookup("sqlite")
	registry, err := presets.NewRegistry(definition, sqlite)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Normalize(config.Input{Root: "/tmp/app", Registry: registry, Preset: "shared", Addons: []string{"sqlite"}})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := config.Resolve(cfg)
	if err != nil || !reflect.DeepEqual(resolved.Preinstalled, definition.Image.Preinstalled) {
		t.Fatalf("resolved = %+v, %v", resolved, err)
	}
	manifest := config.NewManifest(cfg)
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != 2 || bytes.Contains(data, []byte("preinstalled")) || bytes.Contains(data, []byte("definitionSchema")) || len(manifest.Options) != 0 {
		t.Fatalf("manifest = %s", data)
	}
	read, err := config.ReadManifest(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, d := range cfg.Definitions {
		if err := os.WriteFile(filepath.Join(dir, d.Name+".toml"), d.Raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	copies, err := presets.LoadProject(dir, []presets.Ref{read.Preset, read.Addons[0]})
	if err != nil {
		t.Fatal(err)
	}
	again, err := config.Normalize(read.Input("/tmp/app", copies))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg, again) || !config.SameSelection(manifest, config.NewManifest(again)) {
		t.Fatal("snapshot/manifest round trip changed configuration")
	}
	for _, d := range again.Definitions {
		original, _ := registry.Lookup(d.Name)
		if !bytes.Equal(d.Raw, original.Raw) || d.Ref() != original.Ref() {
			t.Fatal("snapshot bytes/hash changed")
		}
	}
	// A changed claim cannot be adopted through an ordinary snapshot load.
	if err := os.WriteFile(filepath.Join(dir, "shared.toml"), []byte(strings.Replace(sharedDefinition, "1.27.2", "1.27.3", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := presets.LoadProject(dir, []presets.Ref{read.Preset, read.Addons[0]}); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("changed snapshot error = %v", err)
	}
}

func TestNormalizeRejectsPreinstalledInstallerAndEditableClaims(t *testing.T) {
	definition, err := presets.Parse([]byte(sharedDefinition), "shared.toml")
	if err != nil {
		t.Fatal(err)
	}
	goAddon, _ := presets.Builtin().Lookup("go")
	registry, err := presets.NewRegistry(definition, goAddon)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		input config.Input
		want  string
	}{
		{config.Input{Addons: []string{"go"}}, `preinstalled tool "go" conflicts with definition "go"`},
		{config.Input{Options: map[string]map[string]string{"shared": {"version": "1.27.3"}}}, `has no option "version"`},
	} {
		tt.input.Root, tt.input.Preset, tt.input.Registry = "/tmp/app", "shared", registry
		if _, err := config.Normalize(tt.input); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("error = %v, want %q", err, tt.want)
		}
	}
}
