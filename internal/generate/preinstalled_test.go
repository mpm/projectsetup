package generate

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/presets"
)

func TestAbsentAndEmptyPreinstalledPreserveLegacyRendering(t *testing.T) {
	for _, name := range presets.Builtin().Names(presets.KindPreset) {
		t.Run(name, func(t *testing.T) {
			builtin, _ := presets.Builtin().Lookup(name)
			cfg, err := config.Normalize(config.Input{Root: "/tmp/app", Preset: name})
			if err != nil {
				t.Fatal(err)
			}
			legacy, err := render(cfg)
			if err != nil {
				t.Fatal(err)
			}
			for _, contract := range []string{"", "\n[image.preinstalled]\n", "\n[image.preinstalled]\ncore_packages = []\ntools = {}\n"} {
				definition, err := presets.Parse([]byte(strings.Replace(string(builtin.Raw), "schema = 1", "schema = 2", 1)+contract), name+".toml")
				if err != nil {
					t.Fatal(err)
				}
				definition.Source = builtin.Source
				registry, err := presets.NewRegistry(definition)
				if err != nil {
					t.Fatal(err)
				}
				modern, err := config.Normalize(config.Input{Root: "/tmp/app", Preset: name, Registry: registry})
				if err != nil {
					t.Fatal(err)
				}
				files, err := render(modern)
				if err != nil {
					t.Fatal(err)
				}
				if len(files) != len(legacy) {
					t.Fatal("generated tree changed")
				}
				for i, file := range files {
					if file.name != legacy[i].name || file.mode != legacy[i].mode {
						t.Fatal("generated paths/modes changed")
					}
					// These source files necessarily record the explicitly adopted format/hash.
					if file.name == "projectsetup.json" || strings.HasPrefix(file.name, "presets/") {
						continue
					}
					if !bytes.Equal(file.data, legacy[i].data) {
						t.Fatalf("%s changed for absent/empty contract", file.name)
					}
				}
			}
		})
	}
}
