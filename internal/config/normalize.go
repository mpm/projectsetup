package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var invalidNameCharacters = regexp.MustCompile(`[^a-z0-9._-]+`)
var validSystemPackage = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9+.-]*$`)
var validLanguageVersion = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){0,2}(?:[-+][a-zA-Z0-9.-]+)?$`)

type Input struct {
	Root            string
	ProjectName     string
	Preset          Preset
	Database        Database
	AITools         []AITool
	PackageManager  PackageManager
	LanguageVersion string
	Ports           []int
	SystemPackages  []string
}

func Normalize(input Input) (Config, error) {
	if !input.Preset.Valid() {
		return Config{}, fmt.Errorf("preset is required and must be node, rails, or python")
	}

	name := strings.TrimSpace(input.ProjectName)
	if name == "" {
		root, err := filepath.Abs(input.Root)
		if err != nil {
			return Config{}, fmt.Errorf("resolve project root: %w", err)
		}
		name = filepath.Base(root)
	}
	name = SanitizeName(name)
	if name == "" {
		return Config{}, fmt.Errorf("project name must contain at least one letter or number")
	}

	database := input.Database
	if database == "" {
		database = DatabaseNone
	}
	if !database.Valid() {
		return Config{}, fmt.Errorf("database must be none or postgres")
	}

	tools, err := normalizeAITools(input.AITools)
	if err != nil {
		return Config{}, err
	}

	manager := input.PackageManager
	if manager == "" {
		switch input.Preset {
		case PresetNode:
			manager = PackageManagerNPM
		case PresetPython:
			manager = PackageManagerPip
		}
	}
	if !manager.Supports(input.Preset) {
		return Config{}, fmt.Errorf("package manager %q is not supported for preset %q", manager, input.Preset)
	}

	version := strings.TrimSpace(input.LanguageVersion)
	if version == "" {
		version = DefaultLanguageVersion(input.Preset)
	}
	if !validLanguageVersion.MatchString(version) {
		return Config{}, fmt.Errorf("language version %q must be a numeric version such as 22 or 3.13.1", version)
	}

	ports := append(make([]int, 0, len(input.Ports)), input.Ports...)
	sort.Ints(ports)
	ports = compact(ports)
	for _, port := range ports {
		if port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("port %d is outside the valid range 1-65535", port)
		}
	}

	packages := normalizeStrings(input.SystemPackages)
	for _, pkg := range packages {
		if !validSystemPackage.MatchString(pkg) {
			return Config{}, fmt.Errorf("system package %q is not a valid apt package name", pkg)
		}
	}
	root, err := filepath.Abs(input.Root)
	if err != nil {
		return Config{}, fmt.Errorf("resolve project root: %w", err)
	}

	serviceName := "app"
	composeProjectName := RuntimeProjectName(name)
	return Config{
		SchemaVersion:   SchemaVersion,
		ProjectName:     name,
		Preset:          input.Preset,
		Database:        database,
		AITools:         tools,
		PackageManager:  manager,
		LanguageVersion: version,
		Ports:           ports,
		SystemPackages:  packages,
		Workspace: Workspace{
			HostPath:      root,
			ContainerPath: "/workspaces/" + name,
		},
		Container: Container{
			User:               "vscode",
			Home:               "/home/vscode",
			Name:               composeProjectName + "-" + serviceName,
			ComposeProjectName: composeProjectName,
			ServiceName:        serviceName,
			UseCompose:         database == DatabasePostgres,
		},
	}, nil
}

func SanitizeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = invalidNameCharacters.ReplaceAllString(value, "-")
	return strings.Trim(value, ".-_")
}

// RuntimeProjectName returns a project prefix accepted by Docker Compose.
func RuntimeProjectName(projectName string) string {
	return strings.ReplaceAll(projectName, ".", "-")
}

func DefaultLanguageVersion(preset Preset) string {
	switch preset {
	case PresetNode:
		return "24"
	case PresetRails:
		return "3.3"
	case PresetPython:
		return "3.13"
	default:
		return ""
	}
}

func normalizeAITools(values []AITool) ([]AITool, error) {
	if values == nil {
		return []AITool{AIToolOpenCode}, nil
	}
	seen := make(map[AITool]bool, len(values))
	for _, value := range values {
		if !value.Valid() {
			return nil, fmt.Errorf("unsupported AI tool %q", value)
		}
		seen[value] = true
	}
	result := make([]AITool, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}

func normalizeStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[value] = true
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func compact(values []int) []int {
	if len(values) < 2 {
		return values
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}
