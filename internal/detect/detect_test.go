package detect

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mpm/projectsetup/internal/config"
)

func TestDetect(t *testing.T) {
	tests := []struct {
		name         string
		files        map[string]string
		wantPresets  []config.Preset
		wantVersion  map[config.Preset]string
		wantManagers map[config.Preset][]config.PackageManager
		wantWarnings int
	}{
		{
			name: "node version precedence and ambiguous lockfiles",
			files: map[string]string{
				"package.json": `{"engines":{"node":">=18"}}`,
				".nvmrc":       "v20\n", ".node-version": "22.4.1\n",
				"package-lock.json": "{}", "pnpm-lock.yaml": "lockfileVersion: 9",
			},
			wantPresets:  []config.Preset{config.PresetNode},
			wantVersion:  map[config.Preset]string{config.PresetNode: "22.4.1"},
			wantManagers: map[config.Preset][]config.PackageManager{config.PresetNode: {config.PackageManagerNPM, config.PackageManagerPNPM}},
		},
		{
			name: "ruby gem",
			files: map[string]string{
				"example.gemspec": "Gem::Specification.new do |spec|\nend\n",
				"Gemfile":         `source "https://rubygems.org"`,
				".ruby-version":   "ruby-3.2.6\n",
			},
			wantPresets: []config.Preset{config.PresetRuby},
			wantVersion: map[config.Preset]string{config.PresetRuby: "3.2.6"},
		},
		{
			name: "rails with postgres",
			files: map[string]string{
				"Gemfile":             `source "https://rubygems.org"` + "\n" + `gem "rails"`,
				".ruby-version":       "ruby-3.3.5\n",
				"config/database.yml": "default:\n  adapter: postgresql\n",
			},
			wantPresets: []config.Preset{config.PresetRails},
			wantVersion: map[config.Preset]string{config.PresetRails: "3.3.5"},
		},
		{
			name: "python pyproject and uv",
			files: map[string]string{
				"pyproject.toml": "[project]\nrequires-python = \">=3.12\"\n",
				"uv.lock":        "version = 1\n",
			},
			wantPresets:  []config.Preset{config.PresetPython},
			wantVersion:  map[config.Preset]string{config.PresetPython: "3.12"},
			wantManagers: map[config.Preset][]config.PackageManager{config.PresetPython: {config.PackageManagerUV}},
		},
		{
			name:         "mixed repository and unsupported Pipfile",
			files:        map[string]string{"package.json": "{}", "Pipfile": "[packages]\n"},
			wantPresets:  []config.Preset{config.PresetNode, config.PresetPython},
			wantWarnings: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeFiles(t, root, tt.files)
			got, err := Detect(root)
			if err != nil {
				t.Fatalf("Detect() error = %v", err)
			}
			if !reflect.DeepEqual(got.Presets, tt.wantPresets) {
				t.Errorf("presets = %v, want %v", got.Presets, tt.wantPresets)
			}
			for preset, version := range tt.wantVersion {
				if got.Details[preset].LanguageVersion != version {
					t.Errorf("%s version = %q, want %q", preset, got.Details[preset].LanguageVersion, version)
				}
			}
			for preset, managers := range tt.wantManagers {
				if !reflect.DeepEqual(got.Details[preset].PackageManagerCandidates, managers) {
					t.Errorf("%s managers = %v, want %v", preset, got.Details[preset].PackageManagerCandidates, managers)
				}
			}
			if len(got.Warnings) != tt.wantWarnings {
				t.Errorf("warnings = %v, want %d", got.Warnings, tt.wantWarnings)
			}
		})
	}
}

func TestDetectGenericRubyWithoutRailsSignal(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"Gemfile": `gem "sinatra"`, ".ruby-version": "3.3"})
	got, err := Detect(root)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	want := []config.Preset{config.PresetRuby}
	if !reflect.DeepEqual(got.Presets, want) {
		t.Fatalf("presets = %v, want %v", got.Presets, want)
	}
}

func TestDetectRailsTakesPrecedenceOverRuby(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"example.gemspec":       "Gem::Specification.new do |spec|\nend\n",
		"Gemfile":               `gem "rails"`,
		"config/application.rb": "class Application < Rails::Application; end\n",
	})
	got, err := Detect(root)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	want := []config.Preset{config.PresetRails}
	if !reflect.DeepEqual(got.Presets, want) {
		t.Fatalf("presets = %v, want %v", got.Presets, want)
	}
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create parent for %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}
