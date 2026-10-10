package presets

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Definition sources other than the URL of a remote definition, which is
// recorded in sources.toml.
const (
	SourceBuiltin = "builtin"
	SourceUser    = "user"
)

// ConfigDirEnv overrides the projectsetup configuration directory.
const ConfigDirEnv = "PROJECTSETUP_CONFIG_DIR"

// Ref identifies the exact definition file a project was generated from.
type Ref struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Source  string `json:"source"`
	SHA256  string `json:"sha256"`
}

// Ref returns the manifest reference for d.
func (d Definition) Ref() Ref {
	return Ref{Name: d.Name, Version: d.Version, Source: d.Source, SHA256: d.SHA256()}
}

// SHA256 returns the hex digest of the definition file.
func (d Definition) SHA256() string {
	sum := sha256.Sum256(d.Raw)
	return hex.EncodeToString(sum[:])
}

// Info describes a definition for listings.
type Info struct {
	DefinitionSchema int                   `json:"definitionSchema"`
	Preinstalled     *Preinstalled         `json:"preinstalled,omitempty"`
	Name             string                `json:"name"`
	Kind             Kind                  `json:"kind"`
	Version          string                `json:"version"`
	Source           string                `json:"source"`
	Description      string                `json:"description"`
	Options          map[string]OptionInfo `json:"options"`
}

type OptionInfo struct {
	Description string   `json:"description"`
	Default     string   `json:"default"`
	Choices     []string `json:"choices,omitempty"`
	Pattern     string   `json:"pattern,omitempty"`
}

// Info returns the listing entry for d.
func (d Definition) Info() Info {
	info := Info{DefinitionSchema: d.Schema, Preinstalled: d.Image.Preinstalled.clone(), Name: d.Name, Kind: d.Kind, Version: d.Version, Source: d.Source, Description: d.Description, Options: map[string]OptionInfo{}}
	for name, option := range d.Options {
		info.Options[name] = OptionInfo{Description: option.Description, Default: option.Default, Choices: option.Choices, Pattern: option.Pattern}
	}
	return info
}

// ConfigDir returns the projectsetup configuration directory:
// $PROJECTSETUP_CONFIG_DIR, or projectsetup below os.UserConfigDir().
func ConfigDir() (string, error) {
	if dir := os.Getenv(ConfigDirEnv); dir != "" {
		return dir, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user configuration directory: %w; set %s", err, ConfigDirEnv)
	}
	return filepath.Join(dir, "projectsetup"), nil
}

// UserDir returns the directory that holds user definitions.
func UserDir() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "presets"), nil
}

// Load returns the built-in definitions together with the user definitions.
// A user definition cannot reuse a built-in name.
func Load() (*Registry, error) {
	dir, err := UserDir()
	if err != nil {
		return nil, err
	}
	user, err := readDir(dir, SourceUser)
	var errs []error
	if err != nil {
		errs = append(errs, err)
	}
	user, err = pinRemote(dir, user)
	if err != nil {
		errs = append(errs, err)
	}
	builtin := Builtin()
	definitions := builtin.all()
	for _, definition := range user {
		if _, ok := builtin.Lookup(definition.Name); ok {
			errs = append(errs, fmt.Errorf("%s: %q is a built-in definition name; rename the user definition (preset eject %s --as NEW copies a built-in for editing)", filepath.Join(dir, definition.Name+".toml"), definition.Name, definition.Name))
			continue
		}
		definitions = append(definitions, definition)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return NewRegistry(definitions...)
}

// pinRemote marks the user definitions recorded in sources.toml as remote,
// with their URL as source, and requires them to match the recorded digest.
func pinRemote(dir string, user []Definition) ([]Definition, error) {
	sources, err := ReadSources()
	if err != nil {
		return user, err
	}
	var errs []error
	found := map[string]bool{}
	for i, definition := range user {
		source, ok := sources.Lookup(definition.Name)
		if !ok {
			continue
		}
		found[definition.Name] = true
		if got := definition.SHA256(); got != source.SHA256 {
			errs = append(errs, fmt.Errorf("%s: sha256 is %s but %s records %s for %s; remote definitions cannot be edited in place, run projectsetup preset update %s to restore it or preset remove %s", filepath.Join(dir, definition.Name+".toml"), got, SourcesFile, source.SHA256, source.URL, definition.Name, definition.Name))
			continue
		}
		user[i].Source = source.URL
	}
	for _, source := range sources.Definitions {
		if !found[source.Name] {
			errs = append(errs, fmt.Errorf("%s: remote definition %q recorded in %s is missing; run projectsetup preset update %s or preset remove %s", filepath.Join(dir, source.Name+".toml"), source.Name, SourcesFile, source.Name, source.Name))
		}
	}
	return user, errors.Join(errs...)
}

// readDir parses every NAME.toml in dir. A missing directory holds no
// definitions. Errors from all files are reported together.
func readDir(dir, source string) ([]Definition, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read definitions directory %q: %w", dir, err)
	}
	var definitions []Definition
	var errs []error
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".toml" {
			continue
		}
		definition, err := ReadFile(filepath.Join(dir, entry.Name()), source)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		definitions = append(definitions, definition)
	}
	return definitions, errors.Join(errs...)
}

// ReadFile parses the definition at path, which must be named NAME.toml.
func ReadFile(path, source string) (Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Definition{}, fmt.Errorf("read definition: %w", err)
	}
	definition, err := Parse(data, path)
	if err != nil {
		return Definition{}, err
	}
	if want := strings.TrimSuffix(filepath.Base(path), ".toml"); definition.Name != want {
		return Definition{}, fmt.Errorf("%s: name is %q; expected %q to match the file name", path, definition.Name, want)
	}
	definition.Source = source
	return definition, nil
}

// LoadProject reads the definition copies in a generated project's presets
// directory. Every copy must match its manifest reference, and the directory
// must not contain other definitions.
func LoadProject(dir string, refs []Ref) (*Registry, error) {
	var definitions []Definition
	var errs []error
	expected := map[string]bool{}
	for _, ref := range refs {
		expected[ref.Name+".toml"] = true
		path := filepath.Join(dir, ref.Name+".toml")
		definition, err := ReadFile(path, ref.Source)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if got := definition.SHA256(); got != ref.SHA256 {
			errs = append(errs, fmt.Errorf("%s: sha256 is %s but the manifest records %s; do not edit project copies, change the user definition and run projectsetup upgrade --refresh-presets", path, got, ref.SHA256))
			continue
		}
		if definition.Version != ref.Version {
			errs = append(errs, fmt.Errorf("%s: version is %s but the manifest records %s", path, definition.Version, ref.Version))
			continue
		}
		definitions = append(definitions, definition)
	}
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, fmt.Errorf("read project definitions %q: %w", dir, err))
	}
	for _, entry := range entries {
		if !expected[entry.Name()] {
			errs = append(errs, fmt.Errorf("%s: not a definition recorded in the manifest", filepath.Join(dir, entry.Name())))
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return NewRegistry(definitions...)
}

// all returns every definition sorted by name.
func (r *Registry) all() []Definition {
	names := make([]string, 0, len(r.definitions))
	for name := range r.definitions {
		names = append(names, name)
	}
	sort.Strings(names)
	definitions := make([]Definition, len(names))
	for i, name := range names {
		definitions[i] = r.definitions[name]
	}
	return definitions
}

// Infos returns listing entries for every definition: presets, then
// add-ons, each sorted by name.
func (r *Registry) Infos() []Info {
	var infos []Info
	for _, kind := range []Kind{KindPreset, KindAddon} {
		for _, name := range r.Names(kind) {
			infos = append(infos, r.definitions[name].Info())
		}
	}
	return infos
}
