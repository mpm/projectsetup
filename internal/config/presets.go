package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mpm/projectsetup/internal/presets"
)

// Option names the built-in presets use for the values that schema 1
// manifests stored as languageVersion and packageManager, and that the
// alias flags set.
const (
	OptionVersion        = "version"
	OptionPackageManager = "package_manager"
)

// Databases are the values schema 1 manifests stored as database and that
// --database accepts. Each value other than none names the add-on that
// provides it.
var Databases = []string{"none", "postgres", "sqlite"}

// LegacyPostgresVersion is the major version generated before manifests
// recorded postgresVersion.
const LegacyPostgresVersion = "17"

// Resolve merges the definitions that cfg selects.
func Resolve(cfg Config) (presets.Resolved, error) {
	registry, err := presets.NewRegistry(cfg.Definitions...)
	if err != nil {
		return presets.Resolved{}, err
	}
	resolved, err := registry.Resolve(presets.Selection{
		Preset:  cfg.Preset,
		Addons:  cfg.Addons,
		Options: cfg.Options,
		Project: presets.Project{
			Name:      cfg.ProjectName,
			Home:      cfg.Container.Home,
			Workspace: cfg.Workspace.ContainerPath,
		},
	})
	if err != nil {
		return presets.Resolved{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return presets.Resolved{}, fmt.Errorf("locate host home directory for core mounts: %w", err)
	}
	coreTargets := []string{cfg.Workspace.HostPath, WorktreeRootPath(home)}
	for _, mount := range resolved.Mounts {
		for _, target := range coreTargets {
			if pathsOverlap(mount.Target, target) {
				return presets.Resolved{}, fmt.Errorf("host mount target %q overlaps core-managed path %q", mount.Target, target)
			}
		}
	}
	if cfg.PostgresImage != "" {
		if err := validatePostgresImage(cfg); err != nil {
			return presets.Resolved{}, err
		}
		service, ok := resolved.Services["postgres"]
		if !ok {
			return presets.Resolved{}, fmt.Errorf("postgresImage requires the built-in postgres service")
		}
		service.Image = string(cfg.PostgresImage)
		resolved.Services["postgres"] = service
	}
	return resolved, nil
}

func pathsOverlap(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	return a == b || a == string(filepath.Separator) || b == string(filepath.Separator) ||
		strings.HasPrefix(a, b+string(filepath.Separator)) || strings.HasPrefix(b, a+string(filepath.Separator))
}
