package config

import (
	"reflect"
	"testing"
)

func TestResolveMapsConfigToDefinitions(t *testing.T) {
	cfg, err := Normalize(Input{Root: t.TempDir(), ProjectName: "demo", Preset: "python", Addons: []string{"postgres"}, Options: map[string]map[string]string{"python": {"package_manager": "uv", "version": "3.12"}, "postgres": {"version": "17"}}})
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

func TestBuiltinAliasesNameBuiltinDefinitions(t *testing.T) {
	cfg, err := Normalize(Input{Root: "/tmp/app", Preset: "python", Addons: Databases[1:]})
	if err != nil {
		t.Fatalf("--database values are not built-in add-ons: %v", err)
	}
	for _, preset := range []string{"node", "python", "rails", "ruby"} {
		cfg, err := Normalize(Input{Root: "/tmp/app", Preset: preset})
		if err != nil || cfg.Options[preset][OptionVersion] == "" {
			t.Errorf("preset %s has no %s option: %v", preset, OptionVersion, err)
		}
	}
	if cfg.Options["postgres"][OptionVersion] != "18" {
		t.Errorf("postgres default version = %q, want 18", cfg.Options["postgres"][OptionVersion])
	}
}
