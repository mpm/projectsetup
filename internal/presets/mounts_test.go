package presets

import (
	toml "github.com/pelletier/go-toml/v2"
	"strings"
	"testing"
)

const mountDefinition = `schema = 2
kind = "addon"
name = "wayland"
version = "1.0.0"
description = "Explicit Wayland socket forwarding"
[[container.mounts]]
source = "${localEnv:XDG_RUNTIME_DIR}/${localEnv:WAYLAND_DISPLAY}"
target = "/run/host-wayland/wayland-0"
source_kind = "socket"
read_only = true
[container.env]
WAYLAND_DISPLAY = "/run/host-wayland/wayland-0"
`

func TestBindMountParserAndEditor(t *testing.T) {
	for _, tt := range []struct {
		name, old, new string
		valid          bool
	}{
		{"valid", "", "", true},
		{"legacy", "schema = 2", "schema = 1", false},
		{"explicit mode", "read_only = true", "", false},
		{"kind", "source_kind = \"socket\"", "source_kind = \"pipe\"", false},
		{"unknown", "read_only = true", "read_only = true\nextra = 1", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw := mountDefinition
			if tt.old != "" {
				raw = strings.Replace(raw, tt.old, tt.new, 1)
			}
			_, err := Parse([]byte(raw), "test")
			if (err == nil) != tt.valid {
				t.Fatalf("Parse error=%v", err)
			}
			var doc map[string]any
			if err := toml.Unmarshal([]byte(raw), &doc); err != nil {
				t.Fatal(err)
			}
			schema := loadSchema(t)
			problems := validateSchema(schema, schema, doc, "test")
			if (len(problems) == 0) != tt.valid {
				t.Fatalf("editor: %v", problems)
			}
		})
	}
	for _, value := range []string{"/tmp/../sock", "$(echo /tmp)", "${localEnv:NOT_SET:-default}", "${option:source}", "/tmp/a,b", "/home/a/.ssh/key"} {
		raw := strings.Replace(mountDefinition, "${localEnv:XDG_RUNTIME_DIR}/${localEnv:WAYLAND_DISPLAY}", value, 1)
		if _, err := Parse([]byte(raw), "test"); err == nil {
			t.Errorf("accepted source %s", value)
		}
	}
}

func TestResolveHostSource(t *testing.T) {
	lookup := func(key string) (string, bool) {
		v, ok := map[string]string{"XDG_RUNTIME_DIR": "/run/user/1000", "WAYLAND_DISPLAY": "wayland-0"}[key]
		return v, ok
	}
	source := "${localEnv:XDG_RUNTIME_DIR}/${localEnv:WAYLAND_DISPLAY}"
	got, err := ResolveHostSource(source, lookup)
	if err != nil || got != "/run/user/1000/wayland-0" {
		t.Fatalf("%s %v", got, err)
	}
	for _, source := range []string{"${localEnv:MISSING}/sock", "${localEnv:XDG_RUNTIME_DIR}/../sock", "/tmp/${HOME}", "relative"} {
		if _, err := ResolveHostSource(source, lookup); err == nil {
			t.Errorf("accepted %s", source)
		}
	}
}

func TestBindMountMergeConflicts(t *testing.T) {
	addon, err := Parse([]byte(mountDefinition), "test")
	if err != nil {
		t.Fatal(err)
	}
	base, _ := Builtin().Lookup("node")
	for _, target := range []string{"/workspaces/app", "/workspaces", "/home/vscode", "/home/vscode/.codex", "/home/vscode/.local/share/codex/nested", "/usr/local/lib", "/tmp/dworm-ssh-agent.sock"} {
		d := addon
		d.Container.Mounts = append([]BindMount(nil), addon.Container.Mounts...)
		d.Container.Mounts[0].Target = target
		r, err := NewRegistry(base, d)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.Resolve(Selection{Preset: "node", Addons: []string{"wayland"}, Project: Project{Workspace: "/workspaces/app"}}); err == nil {
			t.Errorf("accepted protected target %s", target)
		}
	}
	r, _ := NewRegistry(base, addon)
	resolved, err := r.Resolve(Selection{Preset: "node", Addons: []string{"wayland"}, Project: Project{Workspace: "/workspaces/app"}})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Mounts[0].Source != addon.Container.Mounts[0].Source {
		t.Fatal("source references expanded")
	}
	duplicate := addon
	duplicate.Name = "other"
	duplicate.Raw = nil
	r, _ = NewRegistry(base, addon, duplicate)
	if _, err := r.Resolve(Selection{Preset: "node", Addons: []string{"wayland", "other"}}); err == nil {
		t.Fatal("duplicate targets accepted")
	}
	a := addon.Container.Mounts[0]
	b := a
	b.Target = "/run/a"
	sorted, errs := normalizeMounts([]BindMount{a, b}, "/workspaces/app")
	if len(errs) != 0 || sorted[0].Target != "/run/a" {
		t.Fatalf("%v %v", sorted, errs)
	}
	b.Target = a.Target + "/child"
	if _, errs := normalizeMounts([]BindMount{a, b}, ""); len(errs) == 0 {
		t.Fatal("nested targets accepted")
	}
}

func TestBindMountVariantsAndPreset(t *testing.T) {
	raw := strings.Replace(mountDefinition, `kind = "addon"`, `kind = "preset"`, 1) + "\n[image]\nbase = \"debian:bookworm\"\n"
	if _, err := Parse([]byte(raw), "test"); err != nil {
		t.Fatal(err)
	}
	variant := `schema = 2
kind = "addon"
name = "display"
version = "1.0.0"
description = "Conditional socket"
[[variant]]
when = { preset = ["node"] }
[[variant.container.mounts]]
source = "/run/user/1000/wayland-0"
target = "/run/host-wayland/wayland-0"
source_kind = "socket"
read_only = false
`
	addon, err := Parse([]byte(variant), "test")
	if err != nil {
		t.Fatal(err)
	}
	node, _ := Builtin().Lookup("node")
	python, _ := Builtin().Lookup("python")
	registry, err := NewRegistry(node, python, addon)
	if err != nil {
		t.Fatal(err)
	}
	for _, preset := range []string{"node", "python"} {
		resolved, err := registry.Resolve(Selection{Preset: preset, Addons: []string{"display"}})
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if preset == "node" {
			want = 1
		}
		if len(resolved.Mounts) != want {
			t.Errorf("%s mounts = %v", preset, resolved.Mounts)
		}
	}
	if _, err := Parse([]byte(strings.Replace(variant, "schema = 2", "schema = 1", 1)), "test"); err == nil {
		t.Fatal("legacy variant mounts accepted")
	}
	empty := "schema = 1\nkind = \"addon\"\nname = \"empty\"\nversion = \"1.0.0\"\ndescription = \"empty\"\n[container]\nmounts = []\n"
	if _, err := Parse([]byte(empty), "test"); err == nil {
		t.Fatal("legacy empty declaration accepted")
	}
}
