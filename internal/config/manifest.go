package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"github.com/mpm/projectsetup/internal/presets"
)

// SchemaVersion is the manifest schema this version writes. ReadManifest
// also reads schema 1.
const SchemaVersion = 2

const GeneratedBy = "projectsetup"

// PresetsDir is the directory below .devcontainer that holds copies of the
// definitions a project was generated from.
const PresetsDir = "presets"

type Manifest struct {
	SchemaVersion  int                          `json:"schemaVersion"`
	ProjectName    string                       `json:"projectName"`
	Preset         presets.Ref                  `json:"preset"`
	Addons         []presets.Ref                `json:"addons"`
	Options        map[string]map[string]string `json:"options"`
	PostgresImage  PostgresImageRef             `json:"postgresImage,omitempty"`
	AITools        []AITool                     `json:"aiTools"`
	Ports          []int                        `json:"ports"`
	SystemPackages []string                     `json:"systemPackages"`
	GeneratedBy    string                       `json:"generatedBy"`
}

// manifestV1 is the schema written before definitions were recorded.
type manifestV1 struct {
	SchemaVersion   int      `json:"schemaVersion"`
	ProjectName     string   `json:"projectName"`
	Preset          string   `json:"preset"`
	Database        string   `json:"database"`
	AITools         []AITool `json:"aiTools"`
	PackageManager  string   `json:"packageManager,omitempty"`
	LanguageVersion string   `json:"languageVersion"`
	PostgresVersion string   `json:"postgresVersion,omitempty"`
	Ports           []int    `json:"ports"`
	SystemPackages  []string `json:"systemPackages"`
	GeneratedBy     string   `json:"generatedBy"`
}

func NewManifest(config Config) Manifest {
	manifest := Manifest{
		PostgresImage:  config.PostgresImage,
		SchemaVersion:  SchemaVersion,
		ProjectName:    config.ProjectName,
		Addons:         []presets.Ref{},
		Options:        map[string]map[string]string{},
		AITools:        append(make([]AITool, 0, len(config.AITools)), config.AITools...),
		Ports:          append(make([]int, 0, len(config.Ports)), config.Ports...),
		SystemPackages: append(make([]string, 0, len(config.SystemPackages)), config.SystemPackages...),
		GeneratedBy:    GeneratedBy,
	}
	for _, definition := range config.Definitions {
		if definition.Name == config.Preset {
			manifest.Preset = definition.Ref()
		} else {
			manifest.Addons = append(manifest.Addons, definition.Ref())
		}
	}
	for name, values := range config.Options {
		if len(values) > 0 {
			manifest.Options[name] = values
		}
	}
	return manifest
}

// Input returns the normalization input that reproduces the manifest.
func (m Manifest) Input(root string, registry *presets.Registry) Input {
	input := Input{
		PostgresImage:  m.PostgresImage,
		Root:           root,
		Registry:       registry,
		ProjectName:    m.ProjectName,
		Preset:         m.Preset.Name,
		Options:        m.Options,
		AITools:        m.AITools,
		Ports:          m.Ports,
		SystemPackages: m.SystemPackages,
	}
	for _, addon := range m.Addons {
		input.Addons = append(input.Addons, addon.Name)
	}
	return input
}

// Refs returns the preset and add-on references.
func (m Manifest) Refs() []presets.Ref {
	return append([]presets.Ref{m.Preset}, m.Addons...)
}

// ReadManifest parses a schema 1 or 2 manifest. A schema 1 manifest is
// converted to the schema 2 fields, keeps SchemaVersion 1, and refers to
// built-in definitions without a version or digest.
func ReadManifest(reader io.Reader) (Manifest, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return Manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var header struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&header); err != nil {
		return Manifest{}, fmt.Errorf("parse manifest: %w", err)
	}
	switch header.SchemaVersion {
	case 1:
		var legacy manifestV1
		if err := decodeStrict(data, &legacy); err != nil {
			return Manifest{}, err
		}
		return legacy.convert()
	case SchemaVersion:
		var manifest Manifest
		if err := decodeStrict(data, &manifest); err != nil {
			return Manifest{}, err
		}
		return manifest, nil
	default:
		return Manifest{}, fmt.Errorf("unsupported schemaVersion %d; this projectsetup reads schemaVersion 1 and %d", header.SchemaVersion, SchemaVersion)
	}
}

func decodeStrict(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("parse manifest: multiple JSON values")
		}
		return fmt.Errorf("parse manifest: %w", err)
	}
	return nil
}

func (legacy manifestV1) convert() (Manifest, error) {
	builtin := func(name string) presets.Ref { return presets.Ref{Name: name, Source: presets.SourceBuiltin} }
	manifest := Manifest{
		SchemaVersion:  1,
		ProjectName:    legacy.ProjectName,
		Preset:         builtin(legacy.Preset),
		Addons:         []presets.Ref{},
		Options:        map[string]map[string]string{},
		AITools:        legacy.AITools,
		Ports:          legacy.Ports,
		SystemPackages: legacy.SystemPackages,
		GeneratedBy:    legacy.GeneratedBy,
	}
	options := map[string]string{OptionVersion: legacy.LanguageVersion}
	if legacy.PackageManager != "" {
		options[OptionPackageManager] = legacy.PackageManager
	}
	manifest.Options[legacy.Preset] = options
	switch legacy.Database {
	case "none":
	case "postgres":
		version := legacy.PostgresVersion
		if version == "" {
			version = LegacyPostgresVersion
		}
		manifest.Addons = append(manifest.Addons, builtin("postgres"))
		manifest.Options["postgres"] = map[string]string{OptionVersion: version}
	case "sqlite":
		manifest.Addons = append(manifest.Addons, builtin("sqlite"))
	default:
		return Manifest{}, fmt.Errorf("parse manifest: unsupported database %q (expected %s)", legacy.Database, DescribeChoices(Databases))
	}
	if legacy.PostgresVersion != "" && legacy.Database != "postgres" {
		return Manifest{}, fmt.Errorf("parse manifest: postgresVersion requires database %q", "postgres")
	}
	return manifest, nil
}

// SameSelection reports whether a and b select the same definitions and
// values. Definition versions, sources, and digests are compared only when
// both manifests record them, which schema 1 manifests do not.
func SameSelection(a, b Manifest) bool {
	left, right := a.Refs(), b.Refs()
	if a.SchemaVersion == 1 || b.SchemaVersion == 1 {
		for i := range left {
			left[i] = presets.Ref{Name: left[i].Name}
		}
		for i := range right {
			right[i] = presets.Ref{Name: right[i].Name}
		}
	}
	a.SchemaVersion, b.SchemaVersion = 0, 0
	a.Preset, b.Preset = presets.Ref{}, presets.Ref{}
	a.Addons, b.Addons = nil, nil
	return slices.Equal(left, right) && jsonEqual(a, b)
}

func jsonEqual(a, b any) bool {
	left, errLeft := json.Marshal(a)
	right, errRight := json.Marshal(b)
	return errLeft == nil && errRight == nil && bytes.Equal(left, right)
}
