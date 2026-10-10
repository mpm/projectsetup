// Package detect evaluates the detection rules of preset definitions against
// a project directory.
package detect

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/mpm/projectsetup/internal/presets"
)

type PresetResult struct {
	Signals []string
	// Options holds the detected values of options with detection rules:
	// one value from value sources, or every matching choice. Options
	// without a detected value have no entry.
	Options map[string][]string
	// SuggestedAddons are offered as defaults, never added silently.
	SuggestedAddons []string
}

type Result struct {
	Presets  []string
	Details  map[string]PresetResult
	Warnings []string
}

// builtins read values that a regular expression cannot read reliably. Their
// names are listed in presets.DetectBuiltins.
var builtins = map[string]func(*project) (string, error){
	"package-json-engines": packageJSONEngines,
}

// Detect evaluates the detection rules of every preset in registry.
func Detect(root string, registry *presets.Registry) (Result, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Result{}, fmt.Errorf("resolve project root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return Result{}, fmt.Errorf("inspect project root %q: %w", root, err)
	}
	if !info.IsDir() {
		return Result{}, fmt.Errorf("project root %q is not a directory", root)
	}

	project := &project{root: root, files: map[string]projectFile{}}
	result := Result{Details: make(map[string]PresetResult)}
	warnings := map[string][]string{}
	var superseded []string
	for _, name := range registry.Names(presets.KindPreset) {
		definition, _ := registry.Lookup(name)
		detail, presetWarnings, found, err := detectPreset(project, definition)
		if err != nil {
			return Result{}, err
		}
		if !found {
			continue
		}
		result.Presets = append(result.Presets, name)
		result.Details[name] = detail
		warnings[name] = presetWarnings
		superseded = append(superseded, definition.Detect.Supersedes...)
	}
	// A more specific preset, such as Rails, replaces the generic one it
	// supersedes so that the project is not reported as ambiguous.
	result.Presets = slices.DeleteFunc(result.Presets, func(preset string) bool {
		return slices.Contains(superseded, preset)
	})
	for _, preset := range superseded {
		delete(result.Details, preset)
	}
	for _, preset := range result.Presets {
		result.Warnings = append(result.Warnings, warnings[preset]...)
	}
	sort.Strings(result.Warnings)
	return result, nil
}

func detectPreset(p *project, definition presets.Definition) (PresetResult, []string, bool, error) {
	rules := definition.Detect
	var signals []string
	for _, signal := range rules.Signals {
		names, err := p.glob(signal)
		if err != nil {
			return PresetResult{}, nil, false, err
		}
		signals = append(signals, names...)
	}
	found := len(signals) > 0
	if len(rules.Match) > 0 {
		var err error
		if found, err = p.matchesAny(rules.Match); err != nil {
			return PresetResult{}, nil, false, err
		}
	}
	if !found {
		return PresetResult{}, nil, false, nil
	}

	result := PresetResult{Signals: signals}
	for _, name := range slices.Sorted(maps.Keys(definition.Options)) {
		values, err := detectOption(p, definition.Options[name])
		if err != nil {
			return PresetResult{}, nil, false, err
		}
		if len(values) > 0 {
			if result.Options == nil {
				result.Options = map[string][]string{}
			}
			result.Options[name] = values
		}
	}
	for _, suggestion := range rules.Suggest {
		matched, err := p.matchesAny(suggestion.Match)
		if err != nil {
			return PresetResult{}, nil, false, err
		}
		if matched && !slices.Contains(result.SuggestedAddons, suggestion.Addon) {
			result.SuggestedAddons = append(result.SuggestedAddons, suggestion.Addon)
		}
	}
	var warnings []string
	for _, warning := range rules.Warnings {
		matched, err := p.matchesAny(warning.Match)
		if err != nil {
			return PresetResult{}, nil, false, err
		}
		if matched {
			warnings = append(warnings, warning.Message)
		}
	}
	return result, warnings, true, nil
}

// detectOption returns the value of the first source that yields one, or
// every choice with a matching file in declaration order.
func detectOption(p *project, option presets.Option) ([]string, error) {
	for _, source := range option.Detect.Sources {
		value, err := p.value(source)
		if err != nil {
			return nil, err
		}
		if value != "" {
			return []string{value}, nil
		}
	}
	var candidates []string
	for _, choice := range option.Choices {
		matched, err := p.matchesAny(option.Detect.Choices[choice])
		if err != nil {
			return nil, err
		}
		if matched {
			candidates = append(candidates, choice)
		}
	}
	return candidates, nil
}

// project reads files below root once and caches their contents.
type project struct {
	root  string
	files map[string]projectFile
}

type projectFile struct {
	data  []byte
	found bool
	err   error
}

// glob returns the regular files matching name, which may be a glob without
// a directory part.
func (p *project) glob(name string) ([]string, error) {
	if !strings.ContainsAny(name, "*?[") {
		found, err := p.exists(name)
		if err != nil || !found {
			return nil, err
		}
		return []string{name}, nil
	}
	matches, err := filepath.Glob(filepath.Join(p.root, name))
	if err != nil {
		return nil, fmt.Errorf("match %s: %w", name, err)
	}
	var names []string
	for _, match := range matches {
		relative := filepath.Base(match)
		if found, err := p.exists(relative); err != nil {
			return nil, err
		} else if found {
			names = append(names, relative)
		}
	}
	return names, nil
}

func (p *project) exists(name string) (bool, error) {
	info, err := os.Stat(filepath.Join(p.root, filepath.FromSlash(name)))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect %s: %w", name, err)
	}
	return !info.IsDir(), nil
}

func (p *project) read(name string) ([]byte, bool, error) {
	if file, ok := p.files[name]; ok {
		return file.data, file.found, file.err
	}
	var file projectFile
	if file.found, file.err = p.exists(name); file.found {
		if file.data, file.err = os.ReadFile(filepath.Join(p.root, filepath.FromSlash(name))); file.err != nil {
			file.found = false
			file.err = fmt.Errorf("read %s: %w", name, file.err)
		}
	}
	p.files[name] = file
	return file.data, file.found, file.err
}

func (p *project) matchesAny(matches []presets.FileMatch) (bool, error) {
	for _, match := range matches {
		matched, err := p.matches(match)
		if err != nil || matched {
			return matched, err
		}
	}
	return false, nil
}

func (p *project) matches(match presets.FileMatch) (bool, error) {
	names, err := p.glob(match.File)
	if err != nil || match.Pattern == "" {
		return len(names) > 0, err
	}
	pattern, err := regexp.Compile(match.Pattern)
	if err != nil {
		return false, fmt.Errorf("compile pattern for %s: %w", match.File, err)
	}
	for _, name := range names {
		data, _, err := p.read(name)
		if err != nil {
			return false, err
		}
		if pattern.Match(data) {
			return true, nil
		}
	}
	return false, nil
}

func (p *project) value(source presets.ValueSource) (string, error) {
	if source.Builtin != "" {
		builtin, ok := builtins[source.Builtin]
		if !ok {
			return "", fmt.Errorf("unknown detection builtin %q", source.Builtin)
		}
		return builtin(p)
	}
	data, found, err := p.read(source.File)
	if err != nil || !found {
		return "", err
	}
	if source.Pattern == "" {
		return cleanVersion(string(data)), nil
	}
	pattern, err := regexp.Compile(source.Pattern)
	if err != nil {
		return "", fmt.Errorf("compile pattern for %s: %w", source.File, err)
	}
	if match := pattern.FindSubmatch(data); len(match) > 1 {
		return string(match[1]), nil
	}
	return "", nil
}

var nodeEngineVersion = regexp.MustCompile(`\d+(?:\.\d+){0,2}`)

func packageJSONEngines(p *project) (string, error) {
	data, found, err := p.read("package.json")
	if err != nil || !found {
		return "", err
	}
	var manifest struct {
		Engines struct {
			Node string `json:"node"`
		} `json:"engines"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", fmt.Errorf("parse package.json: %w", err)
	}
	return nodeEngineVersion.FindString(manifest.Engines.Node), nil
}

// cleanVersion reads a version file such as .ruby-version, which may hold a
// "v" or "ruby-" prefix and trailing text.
func cleanVersion(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "v")
	value = strings.TrimPrefix(value, "ruby-")
	fields := strings.Fields(value)
	if len(fields) > 0 {
		return fields[0]
	}
	return ""
}
