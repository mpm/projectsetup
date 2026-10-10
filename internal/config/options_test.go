package config

import (
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/mpm/projectsetup/internal/presets"
)

func TestListOptionsValuesAreAcceptedByNormalize(t *testing.T) {
	options := ListOptions(presets.Builtin())
	if !reflect.DeepEqual(options.Presets, []string{"node", "python", "rails", "ruby"}) || !reflect.DeepEqual(options.Addons, []string{"go", "postgres", "redis", "rust", "sqlite"}) {
		t.Fatalf("ListOptions() presets = %v, addons = %v", options.Presets, options.Addons)
	}
	for _, preset := range options.Presets {
		cfg, err := Normalize(Input{Root: "/tmp/app", Preset: preset, Addons: options.Addons, AITools: options.AITools})
		if err != nil {
			t.Fatalf("Normalize(%s with every add-on and AI tool) error = %v", preset, err)
		}
		for _, name := range append([]string{preset}, options.Addons...) {
			info := options.Definitions[name]
			for _, option := range slices.Sorted(maps.Keys(info.Options)) {
				details := info.Options[option]
				if got := cfg.Options[name][option]; got != details.Default {
					t.Errorf("%s.%s: Normalize applies %q, listed default %q", name, option, got, details.Default)
				}
				for _, choice := range details.Choices {
					values := map[string]map[string]string{name: {option: choice}}
					if _, err := Normalize(Input{Root: "/tmp/app", Preset: preset, Addons: options.Addons, Options: values}); err != nil {
						t.Errorf("listed choice %s.%s=%s rejected: %v", name, option, choice, err)
					}
				}
			}
		}
	}
	cfg, err := Normalize(Input{Root: "/tmp/app", Preset: "node"})
	if err != nil || !reflect.DeepEqual(cfg.AITools, options.DefaultAITools) {
		t.Errorf("default AI tools = %v, listed %v (%v)", cfg.AITools, options.DefaultAITools, err)
	}
	if !ValidProjectName("example") || ValidProjectName("a.b") || options.ProjectNamePattern != ProjectNamePattern {
		t.Errorf("projectNamePattern = %q", options.ProjectNamePattern)
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
