package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ProjectNamePattern matches project names that are used as-is for the Compose
// project, the Dev Container name, the manifest, and the workspace folder.
const ProjectNamePattern = `^[a-z0-9][a-z0-9_-]*$`

var validProjectName = regexp.MustCompile(ProjectNamePattern)
var invalidNameCharacters = regexp.MustCompile(`[^a-z0-9_-]+`)
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
		return Config{}, fmt.Errorf("preset is required and must be %s", DescribeChoices(Presets()))
	}

	name := input.ProjectName
	if name == "" {
		derived, err := DefaultProjectName(input.Root)
		if err != nil {
			return Config{}, err
		}
		name = derived
	} else if err := ValidateProjectName(name); err != nil {
		return Config{}, err
	}

	database := input.Database
	if database == "" {
		database = DefaultDatabase
	}
	if !database.Valid() {
		return Config{}, fmt.Errorf("database must be %s", DescribeChoices(Databases()))
	}

	tools, err := normalizeAITools(input.AITools)
	if err != nil {
		return Config{}, err
	}

	manager := input.PackageManager
	if manager == "" {
		manager = DefaultPackageManager(input.Preset)
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
			ComposeProjectName: name,
			ServiceName:        serviceName,
		},
	}, nil
}

// ValidProjectName reports whether name can be used unchanged as the project name.
func ValidProjectName(name string) bool {
	return validProjectName.MatchString(name)
}

// ValidateProjectName returns an actionable error when name cannot be used
// unchanged, suggesting the normalized alternative when one exists.
func ValidateProjectName(name string) error {
	if ValidProjectName(name) {
		return nil
	}
	rule := fmt.Sprintf("it must match %s (lowercase letters, digits, '-' and '_', starting with a letter or digit)", ProjectNamePattern)
	if name == "" {
		return errors.New("project name is required; " + rule)
	}
	message := fmt.Sprintf("project name %q is invalid; %s", name, rule)
	if suggestion := SanitizeName(name); suggestion != "" {
		message += fmt.Sprintf("; use %q instead", suggestion)
	}
	return errors.New(message)
}

// SanitizeName converts value into a valid project name, or returns "" when
// nothing usable remains. Valid names are returned unchanged.
func SanitizeName(value string) string {
	if ValidProjectName(value) {
		return value
	}
	value = strings.ToLower(strings.TrimSpace(value))
	value = invalidNameCharacters.ReplaceAllString(value, "-")
	return strings.Trim(value, "-_")
}

// DefaultProjectName derives a valid project name from the project directory.
func DefaultProjectName(root string) (string, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	base := filepath.Base(absolute)
	name := SanitizeName(base)
	if name == "" {
		return "", fmt.Errorf("cannot derive a project name from directory %q; provide a name matching %s", base, ProjectNamePattern)
	}
	return name, nil
}

func DefaultLanguageVersion(preset Preset) string {
	switch preset {
	case PresetNode:
		return "24"
	case PresetRuby, PresetRails:
		return "3.3"
	case PresetPython:
		return "3.13"
	default:
		return ""
	}
}

func normalizeAITools(values []AITool) ([]AITool, error) {
	if values == nil {
		return DefaultAITools(), nil
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
