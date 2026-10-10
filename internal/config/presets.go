package config

import (
	"github.com/mpm/projectsetup/internal/presets"
)

// Option names the built-in definitions use for the values that the
// manifest stores as languageVersion, packageManager, and postgresVersion.
const (
	OptionVersion        = "version"
	OptionPackageManager = "package_manager"
)

func presetOption(preset Preset, name string) (presets.Option, bool) {
	definition, ok := presets.Builtin().Lookup(string(preset))
	if !ok || definition.Kind != presets.KindPreset {
		return presets.Option{}, false
	}
	option, ok := definition.Options[name]
	return option, ok
}

// PackageManagers returns the package managers a preset accepts in display
// order. Presets without a package-manager choice return an empty slice.
func PackageManagers(preset Preset) []PackageManager {
	option, _ := presetOption(preset, OptionPackageManager)
	managers := make([]PackageManager, len(option.Choices))
	for i, choice := range option.Choices {
		managers[i] = PackageManager(choice)
	}
	return managers
}

// DefaultPackageManager returns the package manager used for preset when none
// is selected or detected, or "" when the preset has no package-manager choice.
func DefaultPackageManager(preset Preset) PackageManager {
	option, _ := presetOption(preset, OptionPackageManager)
	return PackageManager(option.Default)
}

// DefaultLanguageVersion returns the language version used for preset when
// none is selected or detected.
func DefaultLanguageVersion(preset Preset) string {
	option, _ := presetOption(preset, OptionVersion)
	return option.Default
}

// DefaultPostgresVersion returns the PostgreSQL major version for new
// projects. Manifests record the version, so changing it never upgrades
// existing data.
func DefaultPostgresVersion() string {
	definition, _ := presets.Builtin().Lookup(DatabasePostgres.Addon())
	return definition.Options[OptionVersion].Default
}

// Addon returns the add-on definition that provides d, or "" for none.
func (d Database) Addon() string {
	switch d {
	case DatabasePostgres, DatabaseSQLite:
		return string(d)
	default:
		return ""
	}
}

// Resolve merges the definitions that cfg selects.
func Resolve(cfg Config) (presets.Resolved, error) {
	preset := string(cfg.Preset)
	options := map[string]map[string]string{preset: {OptionVersion: cfg.LanguageVersion}}
	if cfg.PackageManager != "" {
		options[preset][OptionPackageManager] = string(cfg.PackageManager)
	}
	var addons []string
	if addon := cfg.Database.Addon(); addon != "" {
		addons = append(addons, addon)
		if cfg.PostgresVersion != "" {
			options[addon] = map[string]string{OptionVersion: cfg.PostgresVersion}
		}
	}
	return presets.Builtin().Resolve(presets.Selection{
		Preset:  preset,
		Addons:  addons,
		Options: options,
		Project: presets.Project{
			Name:      cfg.ProjectName,
			Home:      cfg.Container.Home,
			Workspace: cfg.Workspace.ContainerPath,
		},
	})
}
