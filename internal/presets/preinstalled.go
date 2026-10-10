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
var runtimeReleaseLine = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

var knownTools = []string{"node", "ruby", "python", "go", "rust", "gh"}
var movingVersions = []string{"latest", "lts", "stable", "nightly"}

// RuntimeTools have runtime version meanings independent of preset names.
var RuntimeTools = []string{"node", "ruby", "python", "go", "rust"}

// RuntimeVersionsAgree retains release-line/patch compatibility, but does not
// interpret ranges or moving selectors as versions.
func RuntimeVersionsAgree(configured, detected string) bool {
	if (!installedVersion.MatchString(configured) && !runtimeReleaseLine.MatchString(configured)) ||
		(!installedVersion.MatchString(detected) && !runtimeReleaseLine.MatchString(detected)) {
		return false
	}
	return configured == detected ||
		(runtimeReleaseLine.MatchString(detected) && strings.HasPrefix(configured, detected+".")) ||
		(runtimeReleaseLine.MatchString(configured) && strings.HasPrefix(detected, configured+"."))
}

// fixedRuntimeOptionProblems treats conventional runtime options as constraints,
// never selectors for the fixed base. Other options retain their own semantics.
func (d Definition) fixedRuntimeOptionProblems(values map[string]string) []error {
	if d.Image.Preinstalled == nil {
		return nil
	}
	var runtimes []string
	for _, name := range RuntimeTools {
		if _, ok := d.Image.Preinstalled.Tools[name]; ok {
			runtimes = append(runtimes, name)
		}
	}
	var problems []error
	check := func(option, runtime string) {
		if value, ok := values[option]; ok {
			installed := d.Image.Preinstalled.Tools[runtime].Version
			if !RuntimeVersionsAgree(installed, value) {
				problems = append(problems, fmt.Errorf("option %s.%s is %q but image.preinstalled.tools.%s.version is fixed at %q; select a different image/preset to change the runtime", d.Name, option, value, runtime, installed))
			}
		}
	}
	for _, runtime := range runtimes {
		check(runtime+"_version", runtime)
	}
	if _, ok := values["version"]; ok {
		switch len(runtimes) {
		case 0:
		case 1:
			check("version", runtimes[0])
		default:
			problems = append(problems, fmt.Errorf("option %s.version is ambiguous for multiple preinstalled runtimes; omit it or use TOOL_version compatibility constraints", d.Name))
		}
	}
	return problems
}

// CoreAptPackages returns only core prerequisites not supplied by the image,
// in the historical installation order. Project contributions stay separate.
func (r Resolved) CoreAptPackages() []string {
	var result []string
	for _, pkg := range []CorePackage{CoreBash, CoreCACertificates, CoreCurl, CoreGit, CoreGnuPG, CoreSudo} {
		if r.Preinstalled == nil || !slices.Contains(r.Preinstalled.CorePackages, pkg) {
			result = append(result, string(pkg))
		}
	}
	return result
}

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

// PreinstalledInstaller returns the declared tool that a known feature would
// reinstall, recognizing repository identity independently of tag/digest.
func (r Resolved) PreinstalledInstaller(id string) string {
	if r.Preinstalled != nil {
		tool := installerTool(id)
		if _, declared := r.Preinstalled.Tools[tool]; declared {
			return tool
		}
	}
	return ""
}

// DevcontainerFeatures includes the core GitHub CLI request only when the
// image does not declare its equivalent. Definition installers remain explicit.
func (r Resolved) DevcontainerFeatures() map[string]map[string]any {
	result := make(map[string]map[string]any, len(r.Features)+1)
	const githubCLI = "ghcr.io/devcontainers/features/github-cli:1"
	if r.PreinstalledInstaller(githubCLI) == "" {
		result[githubCLI] = map[string]any{}
	}
	for id, options := range r.Features {
		result[id] = options
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
