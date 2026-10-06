package config

import (
	"reflect"
	"slices"
	"testing"
)

func TestListOptionsValuesAreAcceptedByNormalize(t *testing.T) {
	options := ListOptions()
	if len(options.Presets) == 0 || len(options.Databases) == 0 || len(options.AITools) == 0 {
		t.Fatalf("ListOptions() returned empty lists: %#v", options)
	}
	for _, preset := range options.Presets {
		if _, err := ParsePreset(string(preset)); err != nil {
			t.Errorf("listed preset %q rejected: %v", preset, err)
		}
		managers, ok := options.PackageManagers[preset]
		if !ok || managers == nil {
			t.Errorf("packageManagers[%q] missing; presets without a choice must list an empty slice", preset)
		}
		for _, manager := range managers {
			if _, err := ParsePackageManager(string(manager)); err != nil {
				t.Errorf("listed package manager %q rejected: %v", manager, err)
			}
			if _, err := Normalize(Input{Root: "/tmp/app", Preset: preset, PackageManager: manager}); err != nil {
				t.Errorf("Normalize(%s, %s) error = %v", preset, manager, err)
			}
		}
		for _, database := range options.Databases {
			if _, err := ParseDatabase(string(database)); err != nil {
				t.Errorf("listed database %q rejected: %v", database, err)
			}
			if _, err := Normalize(Input{Root: "/tmp/app", Preset: preset, Database: database}); err != nil {
				t.Errorf("Normalize(%s, database %s) error = %v", preset, database, err)
			}
		}
		for _, tool := range options.AITools {
			if _, err := ParseAITool(string(tool)); err != nil {
				t.Errorf("listed AI tool %q rejected: %v", tool, err)
			}
		}
		if _, err := Normalize(Input{Root: "/tmp/app", Preset: preset, AITools: options.AITools}); err != nil {
			t.Errorf("Normalize(%s, all AI tools) error = %v", preset, err)
		}

		defaults, ok := options.Defaults[preset]
		if !ok {
			t.Fatalf("defaults[%q] missing", preset)
		}
		cfg, err := Normalize(Input{Root: "/tmp/app", Preset: preset})
		if err != nil {
			t.Fatalf("Normalize(%s defaults) error = %v", preset, err)
		}
		var wantManager PackageManager
		if defaults.PackageManager != nil {
			wantManager = *defaults.PackageManager
			if !slices.Contains(managers, wantManager) {
				t.Errorf("default package manager %q for %s is not listed in %v", wantManager, preset, managers)
			}
		}
		if cfg.PackageManager != wantManager || cfg.LanguageVersion != defaults.LanguageVersion ||
			cfg.Database != defaults.Database || !reflect.DeepEqual(cfg.AITools, defaults.AITools) {
			t.Errorf("defaults[%s] = %+v; Normalize applies manager %q, version %q, database %q, tools %v",
				preset, defaults, cfg.PackageManager, cfg.LanguageVersion, cfg.Database, cfg.AITools)
		}
	}
	if !ValidProjectName("example") || ValidProjectName("a.b") || options.ProjectNamePattern != ProjectNamePattern {
		t.Errorf("projectNamePattern = %q", options.ProjectNamePattern)
	}
}

func TestListOptionsExcludesRejectedValues(t *testing.T) {
	options := ListOptions()
	for _, preset := range options.Presets {
		listed := options.PackageManagers[preset]
		for _, other := range options.Presets {
			for _, manager := range options.PackageManagers[other] {
				_, err := Normalize(Input{Root: "/tmp/app", Preset: preset, PackageManager: manager})
				if accepted := err == nil; accepted != slices.Contains(listed, manager) {
					t.Errorf("preset %s manager %s: accepted = %v, listed = %v", preset, manager, accepted, listed)
				}
			}
		}
		if slices.Contains(listed, "bun") {
			t.Errorf("bun listed for %s", preset)
		}
	}
	for _, value := range []string{"bun", "bundler", "pipenv"} {
		if _, err := ParsePackageManager(value); err == nil {
			t.Errorf("ParsePackageManager(%q) accepted an unlisted value", value)
		}
	}
	if _, err := ParseDatabase("mysql"); err == nil {
		t.Error("ParseDatabase accepted mysql")
	}
	if _, err := ParseAITool("aider"); err == nil {
		t.Error("ParseAITool accepted aider")
	}
}

func TestDescribeChoices(t *testing.T) {
	tests := []struct {
		values []string
		want   string
	}{
		{nil, ""},
		{[]string{"a"}, "a"},
		{[]string{"a", "b"}, "a or b"},
		{[]string{"a", "b", "c"}, "a, b, or c"},
	}
	for _, tt := range tests {
		if got := DescribeChoices(tt.values); got != tt.want {
			t.Errorf("DescribeChoices(%v) = %q, want %q", tt.values, got, tt.want)
		}
	}
}
