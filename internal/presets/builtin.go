package presets

import (
	"embed"
	"fmt"
	"path"
	"strings"
	"sync"
)

//go:embed builtin/*.toml
var builtinFiles embed.FS

var builtin = sync.OnceValues(func() (*Registry, error) {
	entries, err := builtinFiles.ReadDir("builtin")
	if err != nil {
		return nil, fmt.Errorf("read embedded definitions: %w", err)
	}
	var definitions []Definition
	for _, entry := range entries {
		name := path.Join("builtin", entry.Name())
		data, err := builtinFiles.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read embedded definition %s: %w", name, err)
		}
		definition, err := Parse(data, name)
		if err != nil {
			return nil, err
		}
		if want := strings.TrimSuffix(entry.Name(), ".toml"); definition.Name != want {
			return nil, fmt.Errorf("%s: name is %q; expected %q to match the file name", name, definition.Name, want)
		}
		definition.Source = SourceBuiltin
		definitions = append(definitions, definition)
	}
	return NewRegistry(definitions...)
})

// Builtin returns the definitions embedded in the binary. It panics if they
// are invalid, which tests rule out.
func Builtin() *Registry {
	registry, err := builtin()
	if err != nil {
		panic(err)
	}
	return registry
}
