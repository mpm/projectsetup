package config

import (
	"reflect"
	"slices"
	"testing"

	"github.com/mpm/projectsetup/internal/presets"
)

func TestBuiltinDefinitionsMatchPresetEnum(t *testing.T) {
	var want []string
	for _, preset := range Presets() {
		want = append(want, string(preset))
	}
	got := presets.Builtin().Names(presets.KindPreset)
	if !reflect.DeepEqual(sortedStrings(got), sortedStrings(want)) {
		t.Fatalf("built-in preset definitions = %v, want %v", got, want)
	}
	for _, database := range Databases() {
		if addon := database.Addon(); addon != "" {
			if definition, ok := presets.Builtin().Lookup(addon); !ok || definition.Kind != presets.KindAddon {
				t.Errorf("database %q has no built-in add-on %q", database, addon)
			}
		}
	}
}

func TestPresetDefaultsComeFromDefinitions(t *testing.T) {
	tests := []struct {
		preset   Preset
		version  string
		managers []PackageManager
		manager  PackageManager
	}{
		{PresetNode, "26", []PackageManager{PackageManagerNPM, PackageManagerPNPM, PackageManagerYarn}, PackageManagerNPM},
		{PresetRuby, "4.0", []PackageManager{}, ""},
		{PresetRails, "4.0", []PackageManager{}, ""},
		{PresetPython, "3.14", []PackageManager{PackageManagerPip, PackageManagerPoetry, PackageManagerUV}, PackageManagerPip},
		{Preset("missing"), "", []PackageManager{}, ""},
	}
	for _, tt := range tests {
		t.Run(string(tt.preset), func(t *testing.T) {
			if got := DefaultLanguageVersion(tt.preset); got != tt.version {
				t.Errorf("DefaultLanguageVersion() = %q, want %q", got, tt.version)
			}
			if got := PackageManagers(tt.preset); !reflect.DeepEqual(got, tt.managers) {
				t.Errorf("PackageManagers() = %#v, want %#v", got, tt.managers)
			}
			if got := DefaultPackageManager(tt.preset); got != tt.manager {
				t.Errorf("DefaultPackageManager() = %q, want %q", got, tt.manager)
			}
		})
	}
	if got := DefaultPostgresVersion(); got != "18" {
		t.Errorf("DefaultPostgresVersion() = %q, want 18", got)
	}
}

func TestResolveMapsConfigToDefinitions(t *testing.T) {
	cfg, err := Normalize(Input{Root: t.TempDir(), ProjectName: "demo", Preset: PresetPython, PackageManager: PackageManagerUV, LanguageVersion: "3.12", Database: DatabasePostgres, PostgresVersion: "17"})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := Resolve(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]map[string]string{
		"python":   {"version": "3.12", "package_manager": "uv"},
		"postgres": {"version": "17"},
	}
	if !reflect.DeepEqual(resolved.Options, want) {
		t.Errorf("Options = %v, want %v", resolved.Options, want)
	}
	if got := resolved.Services["postgres"].Image; got != "postgres:17-bookworm" {
		t.Errorf("postgres image = %q", got)
	}
	if got := resolved.Env["PGDATABASE"]; got != "demo" {
		t.Errorf("PGDATABASE = %q, want demo", got)
	}
}

func sortedStrings(values []string) []string {
	result := slices.Clone(values)
	slices.Sort(result)
	return result
}
