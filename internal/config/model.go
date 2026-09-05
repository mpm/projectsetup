package config

import (
	"fmt"
	"path/filepath"
	"sort"
)

const SchemaVersion = 1

type Preset string

const (
	PresetNode   Preset = "node"
	PresetRuby   Preset = "ruby"
	PresetRails  Preset = "rails"
	PresetPython Preset = "python"
)

func ParsePreset(value string) (Preset, error) {
	preset := Preset(value)
	if !preset.Valid() {
		return "", fmt.Errorf("unsupported preset %q (expected node, ruby, rails, or python)", value)
	}
	return preset, nil
}

func (p Preset) Valid() bool {
	return p == PresetNode || p == PresetRuby || p == PresetRails || p == PresetPython
}

type Database string

const (
	DatabaseNone     Database = "none"
	DatabasePostgres Database = "postgres"
	DatabaseSQLite   Database = "sqlite"
)

func ParseDatabase(value string) (Database, error) {
	database := Database(value)
	if !database.Valid() {
		return "", fmt.Errorf("unsupported database %q (expected none, postgres, or sqlite)", value)
	}
	return database, nil
}

func (d Database) Valid() bool {
	return d == DatabaseNone || d == DatabasePostgres || d == DatabaseSQLite
}

type AITool string

const (
	AIToolOpenCode AITool = "opencode"
	AIToolClaude   AITool = "claude"
	AIToolCodex    AITool = "codex"
)

func ParseAITool(value string) (AITool, error) {
	tool := AITool(value)
	if !tool.Valid() {
		return "", fmt.Errorf("unsupported AI tool %q (expected opencode, claude, or codex)", value)
	}
	return tool, nil
}

func (a AITool) Valid() bool {
	return a == AIToolOpenCode || a == AIToolClaude || a == AIToolCodex
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

func (p PackageManager) Valid() bool {
	switch p {
	case PackageManagerNPM, PackageManagerPNPM, PackageManagerYarn,
		PackageManagerPip, PackageManagerPoetry, PackageManagerUV:
		return true
	default:
		return false
	}
}

func (p PackageManager) Supports(preset Preset) bool {
	switch preset {
	case PresetNode:
		return p == PackageManagerNPM || p == PackageManagerPNPM || p == PackageManagerYarn
	case PresetPython:
		return p == PackageManagerPip || p == PackageManagerPoetry || p == PackageManagerUV
	case PresetRuby, PresetRails:
		return p == ""
	default:
		return false
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
