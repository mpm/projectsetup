package config

// OptionsSchemaVersion identifies the format of Options. Increment it when a
// field is removed or its meaning changes; adding fields keeps the version.
const OptionsSchemaVersion = 1

// Options lists the values init accepts. It is built from the same tables that
// parsing and normalization use, so it cannot drift from validation.
type Options struct {
	SchemaVersion      int                         `json:"schemaVersion"`
	Presets            []Preset                    `json:"presets"`
	PackageManagers    map[Preset][]PackageManager `json:"packageManagers"`
	Databases          []Database                  `json:"databases"`
	AITools            []AITool                    `json:"aiTools"`
	ProjectNamePattern string                      `json:"projectNamePattern"`
	Defaults           map[Preset]PresetDefaults   `json:"defaults"`
}

// PresetDefaults holds the values Normalize uses for a preset when nothing is
// selected or detected. PackageManager is nil for presets without a choice.
type PresetDefaults struct {
	PackageManager  *PackageManager `json:"packageManager"`
	LanguageVersion string          `json:"languageVersion"`
	Database        Database        `json:"database"`
	PostgresVersion string          `json:"postgresVersion"`
	AITools         []AITool        `json:"aiTools"`
}

// ListOptions returns the accepted init values.
func ListOptions() Options {
	options := Options{
		SchemaVersion:      OptionsSchemaVersion,
		Presets:            Presets(),
		PackageManagers:    make(map[Preset][]PackageManager),
		Databases:          Databases(),
		AITools:            AITools(),
		ProjectNamePattern: ProjectNamePattern,
		Defaults:           make(map[Preset]PresetDefaults),
	}
	for _, preset := range options.Presets {
		options.PackageManagers[preset] = PackageManagers(preset)
		defaults := PresetDefaults{
			LanguageVersion: DefaultLanguageVersion(preset),
			Database:        DefaultDatabase,
			PostgresVersion: DefaultPostgresVersion,
			AITools:         DefaultAITools(),
		}
		if manager := DefaultPackageManager(preset); manager != "" {
			defaults.PackageManager = &manager
		}
		options.Defaults[preset] = defaults
	}
	return options
}
