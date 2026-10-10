package config_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/presets"
)

func TestReadManifestRejectsUnknownFields(t *testing.T) {
	for _, manifest := range []string{`{"schemaVersion":1,"unexpected":true}`, `{"schemaVersion":2,"database":"none"}`} {
		_, err := config.ReadManifest(strings.NewReader(manifest))
		if err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("ReadManifest(%s) error = %v", manifest, err)
		}
	}
}

func TestReadManifestRejectsTrailingJSON(t *testing.T) {
	_, err := config.ReadManifest(strings.NewReader(`{"schemaVersion":2} {}`))
	if err == nil || !strings.Contains(err.Error(), "multiple JSON values") {
		t.Fatalf("ReadManifest() error = %v", err)
	}
}

func TestReadManifestRejectsUnsupportedSchema(t *testing.T) {
	_, err := config.ReadManifest(strings.NewReader(`{"schemaVersion":3}`))
	if err == nil || !strings.Contains(err.Error(), "unsupported schemaVersion 3") {
		t.Fatalf("ReadManifest() error = %v", err)
	}
}

func TestReadManifestConvertsSchema1(t *testing.T) {
	builtin := func(name string) presets.Ref { return presets.Ref{Name: name, Source: presets.SourceBuiltin} }
	tests := []struct {
		name     string
		manifest string
		addons   []presets.Ref
		options  map[string]map[string]string
		wantErr  string
	}{
		{
			name:     "legacy postgres pins version 17",
			manifest: `{"preset":"node","database":"postgres","packageManager":"npm","languageVersion":"22"}`,
			addons:   []presets.Ref{builtin("postgres")},
			options:  map[string]map[string]string{"node": {"package_manager": "npm", "version": "22"}, "postgres": {"version": config.LegacyPostgresVersion}},
		},
		{
			name:     "recorded postgres",
			manifest: `{"preset":"python","database":"postgres","packageManager":"uv","languageVersion":"3.12","postgresVersion":"18"}`,
			addons:   []presets.Ref{builtin("postgres")},
			options:  map[string]map[string]string{"python": {"package_manager": "uv", "version": "3.12"}, "postgres": {"version": "18"}},
		},
		{
			name:     "sqlite without package manager",
			manifest: `{"preset":"rails","database":"sqlite","languageVersion":"3.3"}`,
			addons:   []presets.Ref{builtin("sqlite")},
			options:  map[string]map[string]string{"rails": {"version": "3.3"}},
		},
		{
			name:     "no database",
			manifest: `{"preset":"ruby","database":"none","languageVersion":"3.3"}`,
			addons:   []presets.Ref{},
			options:  map[string]map[string]string{"ruby": {"version": "3.3"}},
		},
		{name: "unknown database", manifest: `{"preset":"ruby","database":"mysql"}`, wantErr: `unsupported database "mysql"`},
		{name: "version without postgres", manifest: `{"preset":"ruby","database":"none","postgresVersion":"18"}`, wantErr: "postgresVersion requires"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest, err := config.ReadManifest(strings.NewReader(`{"schemaVersion":1,` + strings.TrimPrefix(tt.manifest, "{")))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ReadManifest() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if manifest.SchemaVersion != 1 || manifest.Preset.Source != presets.SourceBuiltin {
				t.Errorf("manifest = %+v", manifest)
			}
			if !reflect.DeepEqual(manifest.Addons, tt.addons) || !reflect.DeepEqual(manifest.Options, tt.options) {
				t.Fatalf("addons = %v, options = %v; want %v, %v", manifest.Addons, manifest.Options, tt.addons, tt.options)
			}
		})
	}
}

func TestManifestRoundTripsConfig(t *testing.T) {
	cfg, err := config.Normalize(config.Input{Root: "/tmp/app", Preset: "node", Addons: []string{"postgres"}, Ports: []int{3000}})
	if err != nil {
		t.Fatal(err)
	}
	manifest := config.NewManifest(cfg)
	node, _ := presets.Builtin().Lookup("node")
	if manifest.SchemaVersion != config.SchemaVersion || manifest.Preset != node.Ref() || len(manifest.Addons) != 1 || manifest.Addons[0].Name != "postgres" {
		t.Fatalf("NewManifest() = %+v", manifest)
	}
	if manifest.Preset.Version != "1.0.0" || manifest.Preset.Source != presets.SourceBuiltin || len(manifest.Preset.SHA256) != 64 {
		t.Fatalf("preset ref = %+v", manifest.Preset)
	}
	again, err := config.Normalize(manifest.Input("/tmp/app", nil))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again, cfg) || !config.SameSelection(config.NewManifest(again), manifest) {
		t.Fatalf("round trip changed the configuration")
	}
	changed := manifest
	changed.Options = map[string]map[string]string{"node": {"package_manager": "npm", "version": "26"}, "postgres": {"version": "17"}}
	if config.SameSelection(changed, manifest) {
		t.Fatal("SameSelection ignored an option change")
	}
	changed = manifest
	changed.Preset.SHA256 = "other"
	if config.SameSelection(changed, manifest) {
		t.Fatal("SameSelection ignored a definition digest change")
	}
	changed.SchemaVersion = 1
	if !config.SameSelection(changed, manifest) {
		t.Fatal("SameSelection compared digests that a schema 1 manifest does not record")
	}
}
