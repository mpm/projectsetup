package config

import (
	"fmt"
	"sort"
)

const SchemaVersion = 1

type Preset string

const (
	PresetNode   Preset = "node"
	PresetRails  Preset = "rails"
	PresetPython Preset = "python"
)

func ParsePreset(value string) (Preset, error) {
	preset := Preset(value)
	if !preset.Valid() {
		return "", fmt.Errorf("unsupported preset %q (expected node, rails, or python)", value)
	}
	return preset, nil
}

func (p Preset) Valid() bool {
	return p == PresetNode || p == PresetRails || p == PresetPython
}

type Database string

const (
	DatabaseNone     Database = "none"
	DatabasePostgres Database = "postgres"
)

func ParseDatabase(value string) (Database, error) {
	database := Database(value)
	if !database.Valid() {
		return "", fmt.Errorf("unsupported database %q (expected none or postgres)", value)
	}
	return database, nil
}

func (d Database) Valid() bool {
	return d == DatabaseNone || d == DatabasePostgres
}

type AITool string

const (
	AIToolOpenCode AITool = "opencode"
	AIToolClaude   AITool = "claude"
)

func ParseAITool(value string) (AITool, error) {
	tool := AITool(value)
	if !tool.Valid() {
		return "", fmt.Errorf("unsupported AI tool %q (expected opencode or claude)", value)
	}
	return tool, nil
}

func (a AITool) Valid() bool {
	return a == AIToolOpenCode || a == AIToolClaude
}

func AIHostDirectories(tools []AITool) []string {
	var directories []string
	for _, tool := range tools {
		switch tool {
		case AIToolOpenCode:
			directories = append(directories, ".config/opencode", ".local/share/opencode", ".opencode", ".cache/opencode")
		case AIToolClaude:
			directories = append(directories, ".claude", ".local/share/claude")
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
	case PresetRails:
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
	User        string
	Home        string
	ServiceName string
	UseCompose  bool
}
