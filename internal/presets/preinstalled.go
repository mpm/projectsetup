package presets

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// CorePackage identifies a prerequisite from the core apt package set.
type CorePackage string

const (
	CoreBash           CorePackage = "bash"
	CoreCACertificates CorePackage = "ca-certificates"
	CoreCurl           CorePackage = "curl"
	CoreGit            CorePackage = "git"
	CoreGnuPG          CorePackage = "gnupg"
	CoreSudo           CorePackage = "sudo"
)

// Preinstalled describes fixed capabilities of the preset's base artifact.
// It is kept separate from contributions that install tools in project layers.
type Preinstalled struct {
	CorePackages []CorePackage            `toml:"core_packages" json:"corePackages,omitempty"`
	Tools        map[string]InstalledTool `toml:"tools" json:"tools,omitempty"`
}

type InstalledTool struct {
	Version    string   `toml:"version" json:"version"`
	Executable string   `toml:"executable" json:"executable"`
	Path       []string `toml:"path" json:"path"`
}

var installedVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9]+(?:[.-][A-Za-z0-9]+)*)?(?:\+[A-Za-z0-9]+(?:[.-][A-Za-z0-9]+)*)?$`)

var knownTools = []string{"node", "ruby", "python", "go", "rust", "gh"}
var movingVersions = []string{"latest", "lts", "stable", "nightly"}

func literalLinuxPath(value string) bool {
	return path.IsAbs(value) && path.Clean(value) == value &&
		!strings.ContainsAny(value, ":$`~\\") &&
		!strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

func (p *Preinstalled) problems(where string) []string {
	var problems []string
	fail := func(field, message string) { problems = append(problems, where+field+": "+message) }
	seen := map[CorePackage]bool{}
	for _, pkg := range p.CorePackages {
		if !slices.Contains([]CorePackage{CoreBash, CoreCACertificates, CoreCurl, CoreGit, CoreGnuPG, CoreSudo}, pkg) {
			fail(".core_packages", fmt.Sprintf("unknown core package %q", pkg))
		}
		if seen[pkg] {
			fail(".core_packages", fmt.Sprintf("duplicate core package %q", pkg))
		}
		seen[pkg] = true
	}
	for _, name := range sortedKeys(p.Tools) {
		tool := p.Tools[name]
		field := ".tools." + name
		if !validName.MatchString(name) {
			fail(field, "tool name must match "+validName.String())
		}
		if slices.Contains([]string{"opencode", "claude", "codex"}, name) {
			fail(field, "AI tools are managed by projectsetup's shared installer and host mounts")
		}
		if !validOptionValue.MatchString(tool.Version) || slices.Contains(movingVersions, strings.ToLower(tool.Version)) {
			fail(field+".version", "requires a literal concrete release token, not a moving selector or expansion")
		} else if slices.Contains(knownTools, name) && !installedVersion.MatchString(tool.Version) {
			fail(field+".version", "requires MAJOR.MINOR.PATCH with optional prerelease/build suffix")
		}
		if !literalLinuxPath(tool.Executable) || tool.Executable == "/" {
			fail(field+".executable", "requires a clean absolute Linux executable path without whitespace, control characters, ':' or expansion markers")
		}
		if len(tool.Path) == 0 {
			fail(field+".path", "requires at least one directory")
		}
		for _, entry := range tool.Path {
			if !literalLinuxPath(entry) {
				fail(field+".path", fmt.Sprintf("%q must be a clean absolute Linux path without whitespace, control characters, ':' or expansion markers", entry))
			}
		}
		if !slices.Contains(tool.Path, path.Dir(tool.Executable)) {
			fail(field+".path", "must include the executable's parent directory")
		}
	}
	return problems
}

func (p *Preinstalled) clone() *Preinstalled {
	if p == nil {
		return nil
	}
	result := &Preinstalled{CorePackages: slices.Clone(p.CorePackages)}
	if p.Tools != nil {
		result.Tools = make(map[string]InstalledTool, len(p.Tools))
		for name, tool := range p.Tools {
			tool.Path = slices.Clone(tool.Path)
			result.Tools[name] = tool
		}
	}
	return result
}

// toolPath orders tool keys deterministically, preserving each declared list.
func (p *Preinstalled) toolPath() []string {
	var result []string
	if p == nil {
		return result
	}
	for _, name := range sortedKeys(p.Tools) {
		for _, entry := range p.Tools[name].Path {
			if !slices.Contains(result, entry) {
				result = append(result, entry)
			}
		}
	}
	return result
}

// installerTool recognizes repository identity independently of tag/digest.
func installerTool(id string) string {
	repository, _, _ := strings.Cut(id, "@")
	if colon := strings.LastIndex(repository, ":"); colon > strings.LastIndex(repository, "/") {
		repository = repository[:colon]
	}
	switch repository {
	case "ghcr.io/devcontainers/features/node":
		return "node"
	case "ghcr.io/devcontainers/features/python":
		return "python"
	case "ghcr.io/devcontainers/features/go":
		return "go"
	case "ghcr.io/devcontainers/features/rust":
		return "rust"
	case "ghcr.io/devcontainers/features/github-cli":
		return "gh"
	case "ghcr.io/rails/devcontainer/features/ruby":
		return "ruby"
	}
	return ""
}
