package presets

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// BindMount is a selected, explicit local-host integration. Sources retain
// localEnv references in snapshots and generated metadata, never host values.
type BindMount struct {
	Source     string `toml:"source" json:"source"`
	Target     string `toml:"target" json:"target"`
	SourceKind string `toml:"source_kind" json:"sourceKind"`
	ReadOnly   *bool  `toml:"read_only" json:"readOnly"`
}

var hostReference = regexp.MustCompile(`\$\{localEnv:([A-Za-z_][A-Za-z0-9_]*)\}`)

func (m BindMount) problems() []string {
	var problems []string
	if m.ReadOnly == nil {
		problems = append(problems, "read_only must explicitly be true or false")
	}
	if !slices.Contains([]string{"directory", "file", "socket"}, m.SourceKind) {
		problems = append(problems, "source_kind must be directory, file, or socket")
	}
	if _, err := ResolveHostSource(m.Source, func(name string) (string, bool) {
		if strings.HasPrefix(m.Source, "${localEnv:"+name+"}") {
			return "/reference", true
		}
		return "reference", true
	}); err != nil {
		problems = append(problems, err.Error())
	}
	if !cleanBindPath(m.Target) {
		problems = append(problems, "target must be a clean absolute Linux path without whitespace, interpolation, comma, colon, or shell syntax")
	}
	for _, value := range []string{m.Source, m.Target} {
		for _, part := range strings.Split(value, "/") {
			if part == ".ssh" || part == ".gitconfig" {
				problems = append(problems, "mount conflicts with dworm credential forwarding")
			}
		}
	}
	return problems
}

func cleanBindPath(value string) bool {
	return strings.HasPrefix(value, "/") && path.Clean(value) == value && !strings.ContainsAny(value, ",:$`~\\") && !strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

// ResolveHostSource performs literal variable replacement only. Missing or
// empty variables fail; defaults, shell expansion and discovery are unsupported.
func ResolveHostSource(source string, lookup func(string) (string, bool)) (string, error) {
	if source == "" {
		return "", fmt.Errorf("source is required")
	}
	var failure error
	resolved := hostReference.ReplaceAllStringFunc(source, func(ref string) string {
		name := hostReference.FindStringSubmatch(ref)[1]
		value, ok := lookup(name)
		if !ok || value == "" {
			failure = fmt.Errorf("source %q requires exported host variable %s; set it before init, check, upgrade, and container startup", source, name)
		}
		return value
	})
	if failure != nil {
		return "", failure
	}
	if !cleanBindPath(resolved) {
		return "", fmt.Errorf("source %q must resolve to a clean absolute path without whitespace, comma, colon, or shell syntax; only literal ${localEnv:NAME} references are supported", source)
	}
	return resolved, nil
}

// DevcontainerMount is shared by generation and validation.
func (m BindMount) DevcontainerMount() string {
	value := "source=" + m.Source + ",target=" + m.Target + ",type=bind"
	if m.ReadOnly != nil && *m.ReadOnly {
		value += ",readonly"
	}
	return value
}

func pathsOverlap(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/") || a == "/" || b == "/"
}

func normalizeMounts(mounts []BindMount, workspace string) ([]BindMount, []error) {
	result := slices.Clone(mounts)
	slices.SortFunc(result, func(a, b BindMount) int { return strings.Compare(a.Target, b.Target) })
	var errs []error
	// Reserve runtime/system roots and every core AI location, even when its tool
	// is not selected, so later --ai changes cannot introduce hidden collisions.
	protected := []string{workspace, "/home/vscode/.ssh", "/home/vscode/.gitconfig", "/home/vscode/.config/opencode", "/home/vscode/.local/share/opencode", "/home/vscode/.opencode", "/home/vscode/.cache/opencode", "/home/vscode/.claude", "/home/vscode/.local/share/claude", "/home/vscode/.codex", "/home/vscode/.local/share/codex", "/home/vscode/.local/bin", "/bin", "/usr", "/etc", "/lib", "/lib64", "/sbin", "/proc", "/sys", "/dev", "/tmp/dworm-ssh-agent.sock"}
	for i, mount := range result {
		for _, p := range protected {
			if p != "" && pathsOverlap(mount.Target, p) {
				errs = append(errs, fmt.Errorf("host mount target %q overlaps core-managed path %q", mount.Target, p))
			}
		}
		for _, previous := range result[:i] {
			if pathsOverlap(mount.Target, previous.Target) {
				errs = append(errs, fmt.Errorf("host mount targets %q and %q overlap", previous.Target, mount.Target))
			}
		}
	}
	return result, errs
}
