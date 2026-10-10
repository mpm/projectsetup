package detect

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/presets"
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
			name: "ruby version from Gemfile",
			files: map[string]string{
				"Gemfile": "source 'https://rubygems.org'\n\nruby '3.3.0'\ngem 'sinatra'\n",
			},
			wantPresets: []config.Preset{config.PresetRuby},
			wantVersion: map[config.Preset]string{config.PresetRuby: "3.3.0"},
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
			name: "rails version from Gemfile",
			files: map[string]string{
				"Gemfile": "ruby(\"3.3.1\")\ngem \"rails\"\n",
			},
			wantPresets: []config.Preset{config.PresetRails},
			wantVersion: map[config.Preset]string{config.PresetRails: "3.3.1"},
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

// TestDetectDetails pins complete detection results, including signals,
// suggestions, and warnings, for every supported rule.
func TestDetectDetails(t *testing.T) {
	pg := config.DatabasePostgres
	tests := []struct {
		name     string
		files    map[string]string
		want     map[config.Preset]PresetResult
		warnings []string
	}{
		{"empty directory", map[string]string{"README.md": "x"}, map[config.Preset]PresetResult{}, nil},
		{"node manifest only", map[string]string{"package.json": "{}"},
			map[config.Preset]PresetResult{config.PresetNode: {Signals: []string{"package.json"}}}, nil},
		{"node engines", map[string]string{"package.json": `{"engines":{"node":"^20.1"}}`},
			map[config.Preset]PresetResult{config.PresetNode: {Signals: []string{"package.json"}, LanguageVersion: "20.1"}}, nil},
		{"node empty .node-version falls back to .nvmrc", map[string]string{"package.json": "{}", ".node-version": "\n", ".nvmrc": "v22\n"},
			map[config.Preset]PresetResult{config.PresetNode: {Signals: []string{"package.json", ".node-version", ".nvmrc"}, LanguageVersion: "22"}}, nil},
		{"node version file skips invalid package.json", map[string]string{"package.json": "{", ".nvmrc": "24"},
			map[config.Preset]PresetResult{config.PresetNode: {Signals: []string{"package.json", ".nvmrc"}, LanguageVersion: "24"}}, nil},
		{"node yarn", map[string]string{"package.json": "{}", "yarn.lock": ""},
			map[config.Preset]PresetResult{config.PresetNode: {Signals: []string{"package.json", "yarn.lock"}, PackageManagerCandidates: []config.PackageManager{config.PackageManagerYarn}}}, nil},
		{"node all lockfiles", map[string]string{"package.json": "{}", "yarn.lock": "", "pnpm-lock.yaml": "", "package-lock.json": ""},
			map[config.Preset]PresetResult{config.PresetNode: {Signals: []string{"package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock"}, PackageManagerCandidates: []config.PackageManager{config.PackageManagerNPM, config.PackageManagerPNPM, config.PackageManagerYarn}}}, nil},
		{"node lockfile without manifest", map[string]string{"package-lock.json": "{}"}, map[config.Preset]PresetResult{}, nil},
		{"ruby lockfile only", map[string]string{"Gemfile.lock": ""},
			map[config.Preset]PresetResult{config.PresetRuby: {Signals: []string{"Gemfile.lock"}}}, nil},
		{"ruby gemspecs", map[string]string{"b.gemspec": "", "a.gemspec": "", "nested/c.gemspec": ""},
			map[config.Preset]PresetResult{config.PresetRuby: {Signals: []string{"a.gemspec", "b.gemspec"}}}, nil},
		{"ruby Gemfile version prefixes", map[string]string{"Gemfile": "ruby \"ruby-3.2.1\"\n"},
			map[config.Preset]PresetResult{config.PresetRuby: {Signals: []string{"Gemfile"}, LanguageVersion: "3.2.1"}}, nil},
		{"ruby Gemfile v prefix", map[string]string{"Gemfile": "  ruby 'v3.4.0-preview1'\n"},
			map[config.Preset]PresetResult{config.PresetRuby: {Signals: []string{"Gemfile"}, LanguageVersion: "3.4.0-preview1"}}, nil},
		{"ruby .ruby-version wins over Gemfile", map[string]string{"Gemfile": "ruby '3.3.0'\n", ".ruby-version": "3.4.1"},
			map[config.Preset]PresetResult{config.PresetRuby: {Signals: []string{"Gemfile", ".ruby-version"}, LanguageVersion: "3.4.1"}}, nil},
		{"rails from bin/rails", map[string]string{"bin/rails": ""},
			map[config.Preset]PresetResult{config.PresetRails: {Signals: []string{"bin/rails"}}}, nil},
		{"rails signals and postgis", map[string]string{"Gemfile": "gem 'rails', '~> 8.0'\n", "Gemfile.lock": "", ".ruby-version": "3.3.6", "config/application.rb": "", "config/database.yml": "development:\n  adapter: postgis # spatial\n"},
			map[config.Preset]PresetResult{config.PresetRails: {Signals: []string{"Gemfile", "config/application.rb", ".ruby-version", "Gemfile.lock"}, LanguageVersion: "3.3.6", SuggestedDatabase: pg}}, nil},
		{"rails mysql", map[string]string{"bin/rails": "", "config/database.yml": "default:\n  adapter: mysql2\n"},
			map[config.Preset]PresetResult{config.PresetRails: {Signals: []string{"bin/rails"}}}, nil},
		{"rails gem must be a gem line", map[string]string{"Gemfile": "# gem 'rails'\ngem 'railties'\n"},
			map[config.Preset]PresetResult{config.PresetRuby: {Signals: []string{"Gemfile"}}}, nil},
		{"python requirements", map[string]string{"requirements-dev.txt": ""},
			map[config.Preset]PresetResult{config.PresetPython: {Signals: []string{"requirements-dev.txt"}, PackageManagerCandidates: []config.PackageManager{config.PackageManagerPip}}}, nil},
		{"python poetry metadata", map[string]string{"pyproject.toml": "[tool.poetry]\nname = \"x\"\n"},
			map[config.Preset]PresetResult{config.PresetPython: {Signals: []string{"pyproject.toml"}, PackageManagerCandidates: []config.PackageManager{config.PackageManagerPoetry}}}, nil},
		{"python ambiguous lockfiles", map[string]string{"poetry.lock": "", "uv.lock": "", "requirements.txt": ""},
			map[config.Preset]PresetResult{config.PresetPython: {Signals: []string{"requirements.txt", "poetry.lock", "uv.lock"}, PackageManagerCandidates: []config.PackageManager{config.PackageManagerPip, config.PackageManagerPoetry, config.PackageManagerUV}}}, nil},
		{"python version precedence", map[string]string{".python-version": "3.13.2\n", "pyproject.toml": "requires-python = \">=3.11\"\n"},
			map[config.Preset]PresetResult{config.PresetPython: {Signals: []string{"pyproject.toml", ".python-version"}, LanguageVersion: "3.13.2"}}, nil},
		{"python compatible release", map[string]string{"pyproject.toml": "requires-python = '~=3.11.4'\n"},
			map[config.Preset]PresetResult{config.PresetPython: {Signals: []string{"pyproject.toml"}, LanguageVersion: "3.11.4"}}, nil},
		{"python major only", map[string]string{"pyproject.toml": "requires-python = \">=3\"\n"},
			map[config.Preset]PresetResult{config.PresetPython: {Signals: []string{"pyproject.toml"}}}, nil},
		{"python Pipfile", map[string]string{"Pipfile": ""},
			map[config.Preset]PresetResult{config.PresetPython: {Signals: []string{"Pipfile"}}},
			[]string{"Pipfile detected, but Pipenv is not supported; select pip, poetry, or uv"}},
		{"node and rails", map[string]string{"package.json": "{}", "bin/rails": "", "Gemfile": ""},
			map[config.Preset]PresetResult{
				config.PresetNode:  {Signals: []string{"package.json"}},
				config.PresetRails: {Signals: []string{"Gemfile", "bin/rails"}},
			}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeFiles(t, root, tt.files)
			got, err := Detect(root)
			if err != nil {
				t.Fatalf("Detect() error = %v", err)
			}
			if !reflect.DeepEqual(got.Details, tt.want) {
				t.Errorf("Details =\n%#v\nwant\n%#v", got.Details, tt.want)
			}
			if !reflect.DeepEqual(got.Warnings, tt.warnings) {
				t.Errorf("Warnings = %#v, want %#v", got.Warnings, tt.warnings)
			}
		})
	}
}

func TestDetectReportsUnreadablePackageJSON(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"package.json": "{"})
	if _, err := Detect(root); err == nil || !strings.Contains(err.Error(), "package.json") {
		t.Fatalf("Detect() error = %v, want package.json parse error", err)
	}
}

func TestBuiltinsMatchDefinitionSchema(t *testing.T) {
	var names []string
	for name := range builtins {
		names = append(names, name)
	}
	slices.Sort(names)
	want := slices.Sorted(slices.Values(presets.DetectBuiltins))
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("detection builtins = %v, want %v", names, want)
	}
}
