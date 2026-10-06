package config

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const SchemaVersion = 1

type Preset string

const (
	PresetNode   Preset = "node"
	PresetRuby   Preset = "ruby"
	PresetRails  Preset = "rails"
	PresetPython Preset = "python"
)

// Presets returns every supported preset in display order.
func Presets() []Preset {
	return []Preset{PresetNode, PresetRuby, PresetRails, PresetPython}
}

func ParsePreset(value string) (Preset, error) {
	preset := Preset(value)
	if !preset.Valid() {
		return "", fmt.Errorf("unsupported preset %q (expected %s)", value, DescribeChoices(Presets()))
	}
	return preset, nil
}

func (p Preset) Valid() bool {
	return slices.Contains(Presets(), p)
}

type Database string

const (
	DatabaseNone     Database = "none"
	DatabasePostgres Database = "postgres"
	DatabaseSQLite   Database = "sqlite"
)

// DefaultDatabase is used when no database is selected.
const DefaultDatabase = DatabaseNone

// DefaultPostgresVersion is the PostgreSQL major version for new projects.
// Manifests record the version, so changing it never upgrades existing data.
const DefaultPostgresVersion = "18"

// LegacyPostgresVersion is the major version generated before manifests
// recorded postgresVersion.
const LegacyPostgresVersion = "17"

// PostgresImage returns the sidecar image for a PostgreSQL major version.
// Versions before 18 keep the Debian bookworm variant earlier releases
// generated, because changing the image's glibc can change collation order.
func PostgresImage(version string) string {
	if postgresMajor(version) < 18 {
		return "postgres:" + version + "-bookworm"
	}
	return "postgres:" + version + "-trixie"
}

// PostgresDataPath returns where the volume is mounted. Images for 18 and
// later keep data in a version-specific directory below /var/lib/postgresql.
func PostgresDataPath(version string) string {
	if postgresMajor(version) < 18 {
		return "/var/lib/postgresql/data"
	}
	return "/var/lib/postgresql"
}

func postgresMajor(version string) int {
	major, err := strconv.Atoi(version)
	if err != nil {
		return 0
	}
	return major
}

// Databases returns every supported database option in display order. The
// options do not depend on the preset.
func Databases() []Database {
	return []Database{DatabaseNone, DatabasePostgres, DatabaseSQLite}
}

func ParseDatabase(value string) (Database, error) {
	database := Database(value)
	if !database.Valid() {
		return "", fmt.Errorf("unsupported database %q (expected %s)", value, DescribeChoices(Databases()))
	}
	return database, nil
}

func (d Database) Valid() bool {
	return slices.Contains(Databases(), d)
}

type AITool string

const (
	AIToolOpenCode AITool = "opencode"
	AIToolClaude   AITool = "claude"
	AIToolCodex    AITool = "codex"
)

// AITools returns every supported AI tool in display order. Any combination,
// including none, may be selected.
func AITools() []AITool {
	return []AITool{AIToolOpenCode, AIToolClaude, AIToolCodex}
}

// DefaultAITools returns the AI tools used when no selection is made.
func DefaultAITools() []AITool {
	return []AITool{AIToolOpenCode}
}

func ParseAITool(value string) (AITool, error) {
	tool := AITool(value)
	if !tool.Valid() {
		return "", fmt.Errorf("unsupported AI tool %q (expected %s)", value, DescribeChoices(AITools()))
	}
	return tool, nil
}

func (a AITool) Valid() bool {
	return slices.Contains(AITools(), a)
}

func AIHostDirectories(tools []AITool) []string {
	var directories []string
	for _, tool := range tools {
		switch tool {
		case AIToolOpenCode:
			directories = append(directories, ".config/opencode", ".local/share/opencode", ".opencode", ".cache/opencode")
		case AIToolClaude:
			directories = append(directories, ".claude", ".local/share/claude")
		case AIToolCodex:
			directories = append(directories, ".codex", ".local/share/codex")
		}
	}
	sort.Strings(directories)
	return directories
}

type PackageManager string

const (
	PackageManagerNPM    PackageManager = "npm"
	PackageManagerPNPM   PackageManager = "pnpm"
	PackageManagerYarn   PackageManager = "yarn"
	PackageManagerPip    PackageManager = "pip"
	PackageManagerPoetry PackageManager = "poetry"
	PackageManagerUV     PackageManager = "uv"
)

func ParsePackageManager(value string) (PackageManager, error) {
	manager := PackageManager(value)
	if !manager.Valid() {
		return "", fmt.Errorf("unsupported package manager %q", value)
	}
	return manager, nil
}

// PackageManagers returns the package managers a preset accepts in display
// order. Presets without a package-manager choice return an empty slice.
func PackageManagers(preset Preset) []PackageManager {
	switch preset {
	case PresetNode:
		return []PackageManager{PackageManagerNPM, PackageManagerPNPM, PackageManagerYarn}
	case PresetPython:
		return []PackageManager{PackageManagerPip, PackageManagerPoetry, PackageManagerUV}
	default:
		return []PackageManager{}
	}
}

// DefaultPackageManager returns the package manager used for preset when none
// is selected or detected, or "" when the preset has no package-manager choice.
func DefaultPackageManager(preset Preset) PackageManager {
	switch preset {
	case PresetNode:
		return PackageManagerNPM
	case PresetPython:
		return PackageManagerPip
	default:
		return ""
	}
}

func (p PackageManager) Valid() bool {
	for _, preset := range Presets() {
		if slices.Contains(PackageManagers(preset), p) {
			return true
		}
	}
	return false
}

// Supports reports whether p is acceptable for preset. The empty value is
// supported only by valid presets without a package-manager choice.
func (p PackageManager) Supports(preset Preset) bool {
	managers := PackageManagers(preset)
	if p == "" {
		return preset.Valid() && len(managers) == 0
	}
	return slices.Contains(managers, p)
}

// DescribeChoices formats values as "a, b, or c" for messages and help text.
func DescribeChoices[T ~string](values []T) string {
	items := make([]string, len(values))
	for i, value := range values {
		items[i] = string(value)
	}
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " or " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + ", or " + items[len(items)-1]
	}
}

type Config struct {
	SchemaVersion   int
	ProjectName     string
	Preset          Preset
	Database        Database
	AITools         []AITool
	PackageManager  PackageManager
	LanguageVersion string
	PostgresVersion string
	Ports           []int
	SystemPackages  []string
	Workspace       Workspace
	Container       Container
}

type Workspace struct {
	HostPath      string
	ContainerPath string
}

type Container struct {
	User               string
	Home               string
	ComposeProjectName string
	ServiceName        string
}

// CodexStateSource uses Compose interpolation, which supports nested defaults.
const CodexStateSource = "${CODEX_HOME:-${HOME}/.codex}"

func AIHostDirectory(relative, home string, getenv func(string) string) (string, error) {
	if relative == ".codex" {
		if path := getenv("CODEX_HOME"); path != "" {
			if !filepath.IsAbs(path) {
				return "", fmt.Errorf("CODEX_HOME must be an absolute host directory, got %q", path)
			}
			return path, nil
		}
	}
	return filepath.Join(home, filepath.FromSlash(relative)), nil
}
