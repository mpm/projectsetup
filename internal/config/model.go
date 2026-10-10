package config

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/mpm/projectsetup/internal/presets"
)

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

// ContainerPath is shared by generation and static checks. It works for direct,
// non-login docker exec and preserves legacy rendering when no tools are claimed.
func ContainerPath(home string, resolved presets.Resolved) string {
	paths := []string{home + "/.local/bin", home + "/.opencode/bin"}
	paths = append(paths, resolved.PreinstalledPath...)
	paths = append(paths, resolved.Path...)
	paths = append(paths, "/usr/local/sbin", "/usr/local/bin", "/usr/sbin", "/usr/bin", "/sbin", "/bin")
	if resolved.Preinstalled != nil && len(resolved.Preinstalled.Tools) > 0 {
		unique := make([]string, 0, len(paths))
		for _, entry := range paths {
			if !slices.Contains(unique, entry) {
				unique = append(unique, entry)
			}
		}
		paths = unique
	}
	return strings.Join(paths, ":")
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

// Config is the normalized configuration. Preset and Addons name
// definitions validated against a registry; Definitions holds them in
// contribution order, the preset first and then add-ons sorted by name.
type Config struct {
	PostgresImage PostgresImageRef
	ProjectName   string
	Preset        string
	Addons        []string
	// Options holds the effective value of every option of every selected
	// definition, keyed by definition name and then option name.
	Options        map[string]map[string]string
	Definitions    []presets.Definition
	AITools        []AITool
	Ports          []int
	SystemPackages []string
	Workspace      Workspace
	Container      Container
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
