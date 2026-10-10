package presets

import (
	toml "github.com/pelletier/go-toml/v2"
	"strings"
	"testing"
)

const fixedOwnershipDefinition = `schema = 2
kind = "preset"
name = "fixed"
version = "1.0.0"
description = "Fixed IDs"
[image]
base = "team/toolchain:fixed"
[image.ownership]
mode = "fixed"
uid = 1001
gid = 1002
`

func TestOwnershipContract(t *testing.T) {
	for _, tt := range []struct {
		name, raw string
		valid     bool
	}{
		{"fixed", fixedOwnershipDefinition, true},
		{"schema 1", strings.Replace(fixedOwnershipDefinition, "schema = 2", "schema = 1", 1), false},
		{"addon", strings.Replace(strings.Replace(fixedOwnershipDefinition, `kind = "preset"`, `kind = "addon"`, 1), `base = "team/toolchain:fixed"`, "", 1), false},
		{"root uid", strings.Replace(fixedOwnershipDefinition, "uid = 1001", "uid = 0", 1), false},
		{"negative gid", strings.Replace(fixedOwnershipDefinition, "gid = 1002", "gid = -1", 1), false},
		{"large uid", strings.Replace(fixedOwnershipDefinition, "uid = 1001", "uid = 2147483648", 1), false},
		{"missing gid", strings.Replace(fixedOwnershipDefinition, "gid = 1002", "", 1), false},
		{"portable mode", strings.Replace(fixedOwnershipDefinition, `mode = "fixed"`, `mode = "portable"`, 1), false},
		{"empty", strings.Split(fixedOwnershipDefinition, "mode =")[0], false},
		{"unknown", fixedOwnershipDefinition + "user = \"other\"\n", false},
		{"variant", strings.Replace(fixedOwnershipDefinition, "[image.ownership]", "[[variant]]\n[variant.when]\npreset = [\"fixed\"]\n[variant.image.ownership]", 1), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			definition, err := Parse([]byte(tt.raw), tt.name)
			if (err == nil) != tt.valid {
				t.Fatalf("Parse error = %v", err)
			}
			var document map[string]any
			if err := toml.Unmarshal([]byte(tt.raw), &document); err != nil {
				t.Fatal(err)
			}
			schema := loadSchema(t)
			if problems := validateSchema(schema, schema, document, "definition"); (len(problems) == 0) != tt.valid {
				t.Fatalf("editor schema problems = %v", problems)
			}
			if !tt.valid {
				return
			}
			registry, err := NewRegistry(definition)
			if err != nil {
				t.Fatal(err)
			}
			result, err := registry.Resolve(Selection{Preset: "fixed"})
			if err != nil {
				t.Fatal(err)
			}
			result.Ownership.UID = 99
			if definition.Image.Ownership.UID != 1001 {
				t.Fatal("resolution mutated source")
			}
			info := definition.Info()
			info.Ownership.GID = 99
			if definition.Image.Ownership.GID != 1002 {
				t.Fatal("listing mutated source")
			}
		})
	}
}
