package config_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/presets"
)

func TestFixedRuntimeOptionConstraints(t *testing.T) {
	for _, runtime := range presets.RuntimeTools {
		for _, option := range []string{"version", runtime + "_version"} {
			for _, value := range []string{"22", "22.14", "22.14.0", "22.14.1", "2", "latest"} {
				t.Run(runtime+"/"+option+"/"+value, func(t *testing.T) {
					source := fmt.Sprintf(`schema = 2
kind = 'preset'
name = 'fixed'
version = '1.0.0'
description = 'Fixed runtime'
[options.%s]
default = '22.14'
pattern = '[A-Za-z0-9.]+'
[image]
base = 'example:fixed'
[image.preinstalled.tools.%s]
version = '22.14.0'
executable = '/opt/bin/runtime'
path = ['/opt/bin']
`, option, runtime)
					definition, err := presets.Parse([]byte(source), "fixed.toml")
					if err != nil {
						t.Fatal(err)
					}
					registry, err := presets.NewRegistry(definition)
					if err != nil {
						t.Fatal(err)
					}
					_, err = config.Normalize(config.Input{Root: t.TempDir(), ProjectName: "example", Preset: "fixed", Registry: registry, Options: map[string]map[string]string{"fixed": {option: value}}})
					valid := value == "22" || value == "22.14" || value == "22.14.0"
					if valid && err != nil {
						t.Fatal(err)
					}
					if !valid && (err == nil || !strings.Contains(err.Error(), "fixed at")) {
						t.Fatalf("Normalize = %v", err)
					}
				})
			}
		}
	}
}

func TestFixedRuntimeOptionAmbiguityAndDefaults(t *testing.T) {
	for _, tt := range []struct{ name, options, want string }{
		{"no options", "", ""},
		{"other option", "[options.flavor]\ndefault = 'full'\nchoices = ['full', 'small']\n", ""},
		{"ambiguous version", "[options.version]\ndefault = '1.27'\nchoices = ['1.27']\n", "ambiguous"},
		{"named constraints", "[options.go_version]\ndefault = '1.27'\nchoices = ['1.27']\n[options.ruby_version]\ndefault = '3.3'\nchoices = ['3.3']\n", ""},
		{"bad default", "[options.go_version]\ndefault = '1.26'\nchoices = ['1.26']\n", "fixed at"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := sharedDefinition + `[image.preinstalled.tools.ruby]
version = '3.3.7'
executable = '/opt/bin/ruby'
path = ['/opt/bin']
` + tt.options
			definition, err := presets.Parse([]byte(source), "shared.toml")
			if err != nil {
				t.Fatal(err)
			}
			registry, err := presets.NewRegistry(definition)
			if err != nil {
				t.Fatal(err)
			}
			_, err = config.Normalize(config.Input{Root: t.TempDir(), ProjectName: "example", Preset: "shared", Registry: registry})
			if tt.want == "" && err != nil {
				t.Fatal(err)
			}
			if tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)) {
				t.Fatalf("Normalize = %v", err)
			}
		})
	}
}
