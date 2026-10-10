package config

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/presets"
)

func TestPostgresImageReference(t *testing.T) {
	digest := "@sha256:" + strings.Repeat("a", 64)
	for _, tc := range []struct {
		ref, major string
		valid      bool
	}{
		{"postgres:17.6-bookworm", "17", true},
		{"postgres:18.1-trixie" + digest, "18", true},
		{"docker.io/library/postgres:18" + digest, "18", true},
		{"localhost:5000/team/postgres:18.0", "18", true},
		{"Registry--Mirror.example:5000/team/postgres:18.1", "18", true},
		{"[2001:db8::1]:5000/team/postgres:18.1", "18", true},
		{"registry.example/team__one/pg--mirror:18.1-trixie", "18", true},
		{"postgres:17.6-bookworm" + digest, "18", false},
		{"postgres:18.1-trixie" + digest, "17", false},
		{"postgres:latest", "18", false},
		{"postgres" + digest, "18", false},
		{"postgres:018.1", "18", false},
		{"postgres:18.01", "18", false},
		{"postgres:18.1.2", "18", false},
		{"postgres:18.1@sha256:abc", "18", false},
		{"postgres:18.1@sha256:" + strings.Repeat("A", 64), "18", false},
		{"postgres:18.1@md5:" + strings.Repeat("a", 64), "18", false},
		{"https://docker.io/postgres:18.1", "18", false},
		{"registry..example/postgres:18.1", "18", false},
		{"team//postgres:18.1", "18", false},
		{"/postgres:18.1", "18", false},
		{"[::::]:5000/team/postgres:18.1", "18", false},
		{"postgres:18.1$(id)", "18", false},
		{"postgres:${VERSION}", "18", false},
		{"postgres:18.1\n", "18", false},
		{"postgres:18-" + strings.Repeat("a", 126), "18", false},
	} {
		t.Run(tc.ref, func(t *testing.T) {
			err := PostgresImageRef(tc.ref).Validate(tc.major)
			if (err == nil) != tc.valid {
				t.Fatalf("Validate(%q)=%v, valid=%v", tc.major, err, tc.valid)
			}
		})
	}
}

func TestPostgresImageNormalizationAndManifest(t *testing.T) {
	for _, major := range []string{"17", "18"} {
		t.Run(major, func(t *testing.T) {
			ref := PostgresImageRef("postgres:" + major + ".1@sha256:" + strings.Repeat("b", 64))
			cfg, err := Normalize(Input{Root: t.TempDir(), ProjectName: "db", Preset: "node", Addons: []string{"postgres"}, Options: map[string]map[string]string{"postgres": {"version": major}}, PostgresImage: ref})
			if err != nil {
				t.Fatal(err)
			}
			resolved, err := Resolve(cfg)
			if err != nil {
				t.Fatal(err)
			}
			wantPath := "/var/lib/postgresql"
			if major == "17" {
				wantPath += "/data"
			}
			service := resolved.Services["postgres"]
			if service.Image != string(ref) || service.Volumes["postgres-data"] != wantPath {
				t.Fatalf("service=%+v", service)
			}
			manifest := NewManifest(cfg)
			data, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := ReadManifest(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			registry, err := presets.NewRegistry(cfg.Definitions...)
			if err != nil {
				t.Fatal(err)
			}
			roundtrip, err := Normalize(parsed.Input(cfg.Workspace.HostPath, registry))
			if err != nil {
				t.Fatal(err)
			}
			if !SameSelection(NewManifest(roundtrip), manifest) {
				t.Fatal("reference changed during manifest roundtrip")
			}
			builtin, _ := presets.Builtin().Lookup("postgres")
			if cfg.Definitions[1].Ref() != builtin.Ref() {
				t.Fatal("built-in snapshot changed")
			}
			// Reproduce the previous schema-2 decoder: strict unknown-field parsing
			// rejects the addition instead of regenerating while discarding the pin.
			var old struct {
				SchemaVersion  json.RawMessage `json:"schemaVersion"`
				ProjectName    json.RawMessage `json:"projectName"`
				Preset         json.RawMessage `json:"preset"`
				Addons         json.RawMessage `json:"addons"`
				Options        json.RawMessage `json:"options"`
				AITools        json.RawMessage `json:"aiTools"`
				Ports          json.RawMessage `json:"ports"`
				SystemPackages json.RawMessage `json:"systemPackages"`
				GeneratedBy    json.RawMessage `json:"generatedBy"`
			}
			if err := decodeStrict(data, &old); err == nil || !strings.Contains(err.Error(), `unknown field "postgresImage"`) {
				t.Fatalf("previous strict manifest decoder accepted pin: %v", err)
			}
			cfg.PostgresImage = ""
			oldData, err := json.Marshal(NewManifest(cfg))
			if err != nil {
				t.Fatal(err)
			}
			if err := decodeStrict(oldData, &old); err != nil {
				t.Fatalf("previous decoder rejected unpinned manifest: %v", err)
			}

		})
	}
}

func TestPostgresImageSelectionFailures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input Input
		want  string
	}{
		{"unselected", Input{Preset: "node", PostgresImage: "postgres:18.1"}, "requires --addon postgres"},
		{"mismatch", Input{Preset: "node", Addons: []string{"postgres"}, PostgresImage: "postgres:17.6"}, "declares major 17 but postgres.version is 18"},
		{"generic option remains restricted", Input{Preset: "node", Addons: []string{"postgres"}, Options: map[string]map[string]string{"postgres": {"version": "postgres:18.1"}}}, "option postgres.version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.input.Root = t.TempDir()
			tc.input.ProjectName = "db"
			_, err := Normalize(tc.input)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want %s", err, tc.want)
			}
		})
	}
	builtin := presets.Builtin()
	node, _ := builtin.Lookup("node")
	postgres, _ := builtin.Lookup("postgres")
	postgres.Source = "user"
	registry, err := presets.NewRegistry(node, postgres)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Normalize(Input{Root: t.TempDir(), ProjectName: "db", Preset: "node", Addons: []string{"postgres"}, Registry: registry, PostgresImage: "postgres:18.1"})
	if err == nil || !strings.Contains(err.Error(), "only to the built-in") {
		t.Fatalf("custom source accepted: %v", err)
	}
}
