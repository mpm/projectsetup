package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/mpm/projectsetup/internal/presets"
)

// ProjectNamePattern matches project names that are used as-is for the Compose
// project, the Dev Container name, the manifest, and the workspace folder.
const ProjectNamePattern = `^[a-z0-9][a-z0-9_-]*$`

var validProjectName = regexp.MustCompile(ProjectNamePattern)
var invalidNameCharacters = regexp.MustCompile(`[^a-z0-9_-]+`)
var validSystemPackage = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9+.-]*$`)

type Input struct {
	PostgresImage PostgresImageRef
	Root          string
	// Registry provides the definitions; nil means the built-in definitions.
	Registry    *presets.Registry
	ProjectName string
	Preset      string
	Addons      []string
	// Options holds explicitly set values keyed by definition and option
	// name. Unset options use their defaults.
	Options        map[string]map[string]string
	AITools        []AITool
	Ports          []int
	SystemPackages []string
}

func Normalize(input Input) (Config, error) {
	registry := input.Registry
	if registry == nil {
		registry = presets.Builtin()
	}
	definitions, err := selectDefinitions(registry, input.Preset, input.Addons)
	if err != nil {
		return Config{}, err
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

	tools, err := normalizeAITools(input.AITools)
	if err != nil {
		return Config{}, err
	}

	options, err := normalizeOptions(definitions, input.Options)
	if err != nil {
		return Config{}, err
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

	addons := make([]string, 0, len(definitions)-1)
	for _, definition := range definitions[1:] {
		addons = append(addons, definition.Name)
	}
	cfg := Config{
		PostgresImage:  input.PostgresImage,
		ProjectName:    name,
		Preset:         definitions[0].Name,
		Addons:         addons,
		Options:        options,
		Definitions:    definitions,
		AITools:        tools,
		Ports:          ports,
		SystemPackages: packages,
		Workspace: Workspace{
			HostPath:      root,
			ContainerPath: "/workspaces/" + name,
		},
		Container: Container{
			User:               "vscode",
			Home:               "/home/vscode",
			ComposeProjectName: name,
			ServiceName:        "app",
		},
	}
	// Resolve before rendering so invalid selected installer combinations fail
	// during normalization, including matching variants and mixed schemas.
	if _, err := Resolve(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// selectDefinitions returns the preset followed by the add-ons sorted by
// name without duplicates.
func selectDefinitions(registry *presets.Registry, preset string, addons []string) ([]presets.Definition, error) {
	presetNames := registry.Names(presets.KindPreset)
	if preset == "" {
		return nil, fmt.Errorf("preset is required and must be %s", DescribeChoices(presetNames))
	}
	definition, ok := registry.Lookup(preset)
	if !ok || definition.Kind != presets.KindPreset {
		return nil, fmt.Errorf("unsupported preset %q (expected %s)", preset, DescribeChoices(presetNames))
	}
	definitions := []presets.Definition{definition}
	names := slices.Clone(addons)
	slices.Sort(names)
	for _, name := range slices.Compact(names) {
		addon, ok := registry.Lookup(name)
		if !ok || addon.Kind != presets.KindAddon {
			return nil, fmt.Errorf("unsupported add-on %q (expected %s)", name, DescribeChoices(registry.Names(presets.KindAddon)))
		}
		definitions = append(definitions, addon)
	}
	return definitions, nil
}

// normalizeOptions returns the effective values of every option of the
// selected definitions. Definitions without options have no entry.
func normalizeOptions(definitions []presets.Definition, given map[string]map[string]string) (map[string]map[string]string, error) {
	for _, name := range sortedKeys(given) {
		if !slices.ContainsFunc(definitions, func(definition presets.Definition) bool { return definition.Name == name }) {
			return nil, fmt.Errorf("options are set for %q, which is not selected", name)
		}
	}
	result := map[string]map[string]string{}
	for _, definition := range definitions {
		values := map[string]string{}
		for option, value := range given[definition.Name] {
			values[option] = strings.TrimSpace(value)
		}
		effective, err := definition.OptionValues(values)
		if err != nil {
			return nil, err
		}
		if len(effective) > 0 {
			result[definition.Name] = effective
		}
	}
	return result, nil
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

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
