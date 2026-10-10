package config

import "github.com/mpm/projectsetup/internal/presets"

// OptionsSchemaVersion identifies the format of Options. Increment it when a
// field is removed or its meaning changes; adding fields keeps the version.
const OptionsSchemaVersion = 2

// Options lists the values init accepts. It is built from the registry that
// normalization validates against, so it cannot drift from validation.
type Options struct {
	SchemaVersion      int                     `json:"schemaVersion"`
	Presets            []string                `json:"presets"`
	Addons             []string                `json:"addons"`
	Definitions        map[string]presets.Info `json:"definitions"`
	AITools            []AITool                `json:"aiTools"`
	DefaultAITools     []AITool                `json:"defaultAITools"`
	ProjectNamePattern string                  `json:"projectNamePattern"`
}

// ListOptions returns the accepted init values for registry.
func ListOptions(registry *presets.Registry) Options {
	options := Options{
		SchemaVersion:      OptionsSchemaVersion,
		Presets:            registry.Names(presets.KindPreset),
		Addons:             registry.Names(presets.KindAddon),
		Definitions:        map[string]presets.Info{},
		AITools:            AITools(),
		DefaultAITools:     DefaultAITools(),
		ProjectNamePattern: ProjectNamePattern,
	}
	for _, info := range registry.Infos() {
		options.Definitions[info.Name] = info
	}
	return options
}
