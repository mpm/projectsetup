package presets

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeDefinition(t *testing.T, dir, file, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, file)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func definitionText(kind, name string) string {
	text := "schema = 1\nkind = \"" + kind + "\"\nname = \"" + name + "\"\nversion = \"1.0.0\"\ndescription = \"Test\"\n"
	if kind == "preset" {
		text += "[image]\nbase = \"debian:trixie\"\n"
	}
	return text
}

func TestConfigDir(t *testing.T) {
	t.Setenv(ConfigDirEnv, "/custom/projectsetup")
	if dir, err := UserDir(); err != nil || dir != "/custom/projectsetup/presets" {
		t.Fatalf("UserDir() = %q, %v", dir, err)
	}
	t.Setenv(ConfigDirEnv, "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if dir, err := ConfigDir(); err != nil || dir != "/xdg/projectsetup" {
		t.Fatalf("ConfigDir() = %q, %v", dir, err)
	}
}

func TestLoadAddsUserDefinitions(t *testing.T) {
	config := t.TempDir()
	t.Setenv(ConfigDirEnv, config)
	dir := filepath.Join(config, "presets")
	writeDefinition(t, dir, "custom.toml", definitionText("preset", "custom"))
	writeDefinition(t, dir, "redis.toml", definitionText("addon", "redis"))
	writeDefinition(t, dir, "README.md", "not a definition")
	if err := os.Mkdir(filepath.Join(dir, "drafts"), 0o755); err != nil {
		t.Fatal(err)
	}

	registry, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := registry.Names(KindPreset); !reflect.DeepEqual(got, []string{"custom", "node", "python", "rails", "ruby"}) {
		t.Errorf("presets = %v", got)
	}
	if got := registry.Names(KindAddon); !reflect.DeepEqual(got, []string{"postgres", "redis", "sqlite"}) {
		t.Errorf("add-ons = %v", got)
	}
	custom, _ := registry.Lookup("custom")
	node, _ := registry.Lookup("node")
	if custom.Source != SourceUser || node.Source != SourceBuiltin {
		t.Errorf("sources = %q, %q", custom.Source, node.Source)
	}
	if string(custom.Raw) != definitionText("preset", "custom") {
		t.Errorf("Raw = %q", custom.Raw)
	}
}

func TestLoadWithoutUserDirectory(t *testing.T) {
	t.Setenv(ConfigDirEnv, filepath.Join(t.TempDir(), "missing"))
	registry, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(registry.Names(KindPreset), Builtin().Names(KindPreset)) {
		t.Fatalf("presets = %v", registry.Names(KindPreset))
	}
}

func TestLoadRejectsInvalidUserDefinitions(t *testing.T) {
	config := t.TempDir()
	t.Setenv(ConfigDirEnv, config)
	dir := filepath.Join(config, "presets")
	writeDefinition(t, dir, "node.toml", definitionText("preset", "node"))
	writeDefinition(t, dir, "other.toml", definitionText("addon", "renamed"))
	writeDefinition(t, dir, "broken.toml", "schema = ")
	_, err := Load()
	if err == nil {
		t.Fatal("Load() succeeded")
	}
	for _, want := range []string{
		`node.toml: "node" is a built-in definition name; rename the user definition (preset eject node --as NEW`,
		`other.toml: name is "renamed"; expected "other" to match the file name`,
		"broken.toml:1",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not contain %q:\n%v", want, err)
		}
	}
}

func TestLoadProjectVerifiesCopies(t *testing.T) {
	node, _ := Builtin().Lookup("node")
	postgres, _ := Builtin().Lookup("postgres")
	refs := []Ref{node.Ref(), postgres.Ref()}
	setup := func(t *testing.T) string {
		dir := t.TempDir()
		writeDefinition(t, dir, "node.toml", string(node.Raw))
		writeDefinition(t, dir, "postgres.toml", string(postgres.Raw))
		return dir
	}

	registry, err := LoadProject(setup(t), refs)
	if err != nil {
		t.Fatalf("LoadProject() error = %v", err)
	}
	if loaded, _ := registry.Lookup("postgres"); loaded.Source != SourceBuiltin || loaded.SHA256() != postgres.SHA256() {
		t.Fatalf("loaded %+v", loaded.Ref())
	}

	tests := []struct {
		name   string
		change func(t *testing.T, dir string) []Ref
		want   string
	}{
		{"edited copy", func(t *testing.T, dir string) []Ref {
			writeDefinition(t, dir, "node.toml", string(node.Raw)+"\n")
			return refs
		}, "node.toml: sha256 is"},
		{"missing copy", func(t *testing.T, dir string) []Ref {
			os.Remove(filepath.Join(dir, "postgres.toml"))
			return refs
		}, "postgres.toml: no such file"},
		{"unrecorded copy", func(t *testing.T, dir string) []Ref {
			writeDefinition(t, dir, "sqlite.toml", definitionText("addon", "sqlite"))
			return refs
		}, "sqlite.toml: not a definition recorded in the manifest"},
		{"version mismatch", func(t *testing.T, dir string) []Ref {
			changed := []Ref{node.Ref(), postgres.Ref()}
			changed[0].Version = "0.9.0"
			return changed
		}, "version is 1.0.0 but the manifest records 0.9.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := setup(t)
			_, err := LoadProject(dir, tt.change(t, dir))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadProject() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestDefinitionInfo(t *testing.T) {
	node, _ := Builtin().Lookup("node")
	info := node.Info()
	if info.Name != "node" || info.Kind != KindPreset || info.Source != SourceBuiltin || info.Options["package_manager"].Default != "npm" {
		t.Fatalf("Info() = %+v", info)
	}
	infos := Builtin().Infos()
	var names []string
	for _, info := range infos {
		names = append(names, info.Name)
	}
	if !reflect.DeepEqual(names, []string{"node", "python", "rails", "ruby", "postgres", "sqlite"}) {
		t.Fatalf("Infos() order = %v", names)
	}
}
