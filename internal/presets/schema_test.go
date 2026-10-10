package presets

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

const schemaPath = "../../schema/preset.schema.json"

func loadSchema(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("parse %s: %v", schemaPath, err)
	}
	return schema
}

func TestSchemaAcceptsBuiltinDefinitions(t *testing.T) {
	schema := loadSchema(t)
	for _, info := range Builtin().Infos() {
		definition, _ := Builtin().Lookup(info.Name)
		var document map[string]any
		if err := toml.Unmarshal(definition.Raw, &document); err != nil {
			t.Fatal(err)
		}
		if problems := validateSchema(schema, schema, document, info.Name); len(problems) > 0 {
			t.Errorf("schema rejects built-in %s:\n  %s", info.Name, strings.Join(problems, "\n  "))
		}
	}
}

func TestSchemaRejectsInvalidDefinitions(t *testing.T) {
	schema := loadSchema(t)
	preset := "schema = 1\nkind = \"preset\"\nname = \"x\"\nversion = \"1.0.0\"\ndescription = \"d\"\n[image]\nbase = \"debian\"\n"
	tests := []struct {
		name, source, want string
	}{
		{"unknown field", strings.Replace(preset, "[image]", "colour = \"red\"\n[image]", 1), `x.colour: unknown property`},
		{"unknown nested field", preset + "[setup]\nscripts = \"x\"\n", `x.setup.scripts: unknown property`},
		{"kind", strings.Replace(preset, `"preset"`, `"plugin"`, 1), `x.kind: "plugin" is not one of`},
		{"name", strings.Replace(preset, `name = "x"`, `name = "X"`, 1), `x.name: "X" does not match`},
		{"missing field", strings.Replace(preset, "description = \"d\"\n", "", 1), `x: description is required`},
		{"reserved service", preset + "[services.app]\nimage = \"x\"\n", `x.services: property name "app"`},
		{"variant without when", preset + "[[variant]]\n[variant.setup]\nscript = \"x\"\n", `x.variant[0]: when is required`},
		{"feature value", preset + "[features.\"a\"]\nlist = [1]\n", `x.features.a.list: expected type`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var document map[string]any
			if err := toml.Unmarshal([]byte(tt.source), &document); err != nil {
				t.Fatal(err)
			}
			problems := strings.Join(validateSchema(schema, schema, document, "x"), "\n")
			if !strings.Contains(problems, tt.want) {
				t.Fatalf("problems = %q, want containing %q", problems, tt.want)
			}
		})
	}
}

// TestSchemaMatchesDefinitionFields keeps the schema's properties in step with
// the TOML fields of Definition.
func TestSchemaMatchesDefinitionFields(t *testing.T) {
	schema := loadSchema(t)
	for _, problem := range compareSchemaType(schema, schema, reflect.TypeFor[Definition](), "definition") {
		t.Error(problem)
	}
}

func resolveRef(root, schema map[string]any) map[string]any {
	for {
		ref, ok := schema["$ref"].(string)
		if !ok {
			return schema
		}
		name, found := strings.CutPrefix(ref, "#/$defs/")
		if !found {
			panic("unsupported $ref " + ref)
		}
		schema = root["$defs"].(map[string]any)[name].(map[string]any)
	}
}

func tomlFields(t reflect.Type) map[string]reflect.Type {
	fields := map[string]reflect.Type{}
	for field := range t.Fields() {
		if field.Anonymous {
			maps.Copy(fields, tomlFields(field.Type))
			continue
		}
		if tag := field.Tag.Get("toml"); tag != "" && tag != "-" {
			fields[tag] = field.Type
		}
	}
	return fields
}

func compareSchemaType(root, schema map[string]any, goType reflect.Type, where string) []string {
	schema = resolveRef(root, schema)
	for goType.Kind() == reflect.Pointer {
		goType = goType.Elem()
	}
	switch goType.Kind() {
	case reflect.Struct:
		properties, _ := schema["properties"].(map[string]any)
		fields := tomlFields(goType)
		var problems []string
		if got, want := slices.Sorted(maps.Keys(properties)), slices.Sorted(maps.Keys(fields)); !slices.Equal(got, want) {
			problems = append(problems, fmt.Sprintf("%s: schema properties %v, Go fields %v", where, got, want))
		}
		for _, name := range slices.Sorted(maps.Keys(fields)) {
			if property, ok := properties[name].(map[string]any); ok {
				problems = append(problems, compareSchemaType(root, property, fields[name], where+"."+name)...)
			}
		}
		return problems
	case reflect.Map:
		values, ok := schema["additionalProperties"].(map[string]any)
		if !ok {
			return []string{where + ": schema does not describe map values"}
		}
		return compareSchemaType(root, values, goType.Elem(), where+".*")
	case reflect.Slice:
		items, ok := schema["items"].(map[string]any)
		if !ok {
			return []string{where + ": schema does not describe array items"}
		}
		return compareSchemaType(root, items, goType.Elem(), where+"[]")
	}
	return nil
}

// validateSchema implements the JSON Schema keywords the definition schema
// uses and fails on any other keyword, so it cannot silently pass.
func validateSchema(root, schema map[string]any, value any, where string) []string {
	var problems []string
	fail := func(format string, args ...any) { problems = append(problems, where+": "+fmt.Sprintf(format, args...)) }
	for _, keyword := range slices.Sorted(maps.Keys(schema)) {
		rule := schema[keyword]
		switch keyword {
		case "$schema", "$id", "$defs", "title", "description":
		case "$ref":
			problems = append(problems, validateSchema(root, resolveRef(root, map[string]any{"$ref": rule}), value, where)...)
		case "type":
			types, ok := rule.([]any)
			if !ok {
				types = []any{rule}
			}
			if !slices.ContainsFunc(types, func(name any) bool { return hasType(value, name.(string)) }) {
				fail("expected type %v, got %T", rule, value)
			}
		case "const":
			if !reflect.DeepEqual(jsonNumber(value), rule) {
				fail("%v is not %v", value, rule)
			}
		case "enum":
			if !slices.Contains(rule.([]any), any(value)) {
				fail("%q is not one of %v", value, rule)
			}
		case "pattern":
			if text, ok := value.(string); ok && !regexp.MustCompile(rule.(string)).MatchString(text) {
				fail("%q does not match %s", text, rule)
			}
		case "minLength":
			if text, ok := value.(string); ok && float64(len(text)) < rule.(float64) {
				fail("%q is shorter than %v", text, rule)
			}
		case "minimum":
			if number, ok := jsonNumber(value).(float64); ok && number < rule.(float64) {
				fail("%v is below %v", number, rule)
			}
		case "minItems":
			if items, ok := value.([]any); ok && float64(len(items)) < rule.(float64) {
				fail("needs at least %v items", rule)
			}
		case "items":
			items, _ := value.([]any)
			for i, item := range items {
				problems = append(problems, validateSchema(root, rule.(map[string]any), item, fmt.Sprintf("%s[%d]", where, i))...)
			}
		case "required":
			object, _ := value.(map[string]any)
			for _, name := range rule.([]any) {
				if _, ok := object[name.(string)]; !ok && object != nil {
					fail("%s is required", name)
				}
			}
		case "properties", "additionalProperties", "propertyNames":
			object, _ := value.(map[string]any)
			properties, _ := schema["properties"].(map[string]any)
			for _, name := range slices.Sorted(maps.Keys(object)) {
				switch keyword {
				case "properties":
					if property, ok := properties[name]; ok {
						problems = append(problems, validateSchema(root, property.(map[string]any), object[name], where+"."+name)...)
					}
				case "additionalProperties":
					if _, ok := properties[name]; ok {
						continue
					}
					if rule == false {
						problems = append(problems, where+"."+name+": unknown property")
					} else if extra, ok := rule.(map[string]any); ok {
						problems = append(problems, validateSchema(root, extra, object[name], where+"."+name)...)
					}
				case "propertyNames":
					if len(validateSchema(root, rule.(map[string]any), name, where)) > 0 {
						fail("property name %q is not allowed", name)
					}
				}
			}
		case "not":
			if len(validateSchema(root, rule.(map[string]any), value, where)) == 0 {
				fail("%v is not allowed", value)
			}
		default:
			panic("schema keyword " + keyword + " is not supported by the test validator")
		}
	}
	return problems
}

// jsonNumber converts TOML numbers to the float64 that JSON decoding yields.
func jsonNumber(value any) any {
	switch number := value.(type) {
	case int64:
		return float64(number)
	case float64:
		return number
	}
	return value
}

func hasType(value any, name string) bool {
	switch name {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "integer":
		_, ok := value.(int64)
		return ok
	case "number":
		_, ok := jsonNumber(value).(float64)
		return ok
	}
	panic("unsupported type " + name)
}
