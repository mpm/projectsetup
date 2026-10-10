package presets

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Project holds the values available as ${project:NAME} placeholders.
type Project struct {
	Name      string
	Home      string
	Workspace string
}

// Selection names one preset, any add-ons, and option values keyed by
// definition name and then option name. Unset options use their defaults.
type Selection struct {
	Preset  string
	Addons  []string
	Options map[string]map[string]string
	Project Project
}

// Resolved is the merged contribution of the selected definitions with all
// placeholders expanded.
type Resolved struct {
	Base         string
	Preinstalled *Preinstalled
	// PreinstalledPath is separate from installation PATH contributions. The
	// generator's consumption policy determines the final containerEnv.PATH.
	PreinstalledPath []string
	// Apt holds one package group per contributing block, in contribution order.
	Apt      [][]string
	RootRun  []string
	UserRun  []string
	Features map[string]map[string]any
	Path     []string
	Env      map[string]string
	Setup    []string
	Services map[string]Service
	// Options holds the effective option values, including defaults.
	Options map[string]map[string]string
}

// Registry holds definitions by name.
type Registry struct {
	definitions map[string]Definition
}

// NewRegistry validates definitions and rejects duplicate names.
func NewRegistry(definitions ...Definition) (*Registry, error) {
	registry := &Registry{definitions: make(map[string]Definition, len(definitions))}
	var errs []error
	for _, definition := range definitions {
		if problems := definition.problems(); len(problems) > 0 {
			errs = append(errs, fmt.Errorf("definition %q is invalid:\n  %s", definition.Name, strings.Join(problems, "\n  ")))
			continue
		}
		if _, exists := registry.definitions[definition.Name]; exists {
			errs = append(errs, fmt.Errorf("definition %q is defined more than once", definition.Name))
			continue
		}
		registry.definitions[definition.Name] = definition
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return registry, nil
}

// Lookup returns the definition called name.
func (r *Registry) Lookup(name string) (Definition, bool) {
	definition, ok := r.definitions[name]
	return definition, ok
}

// Names returns the sorted names of definitions of kind.
func (r *Registry) Names(kind Kind) []string {
	var names []string
	for name, definition := range r.definitions {
		if definition.Kind == kind {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// Resolve merges the selected preset and add-ons. The preset contributes
// first, then add-ons sorted by name; within a definition, the top-level
// block precedes matching variants in file order.
func (r *Registry) Resolve(selection Selection) (Resolved, error) {
	preset, ok := r.definitions[selection.Preset]
	if !ok || preset.Kind != KindPreset {
		return Resolved{}, fmt.Errorf("unknown preset %q (available: %s)", selection.Preset, strings.Join(r.Names(KindPreset), ", "))
	}
	addons := slices.Clone(selection.Addons)
	slices.Sort(addons)
	addons = slices.Compact(addons)
	definitions := []Definition{preset}
	for _, name := range addons {
		addon, ok := r.definitions[name]
		if !ok || addon.Kind != KindAddon {
			return Resolved{}, fmt.Errorf("unknown add-on %q (available: %s)", name, strings.Join(r.Names(KindAddon), ", "))
		}
		definitions = append(definitions, addon)
	}
	for _, name := range sortedKeys(selection.Options) {
		if name != preset.Name && !slices.Contains(addons, name) {
			return Resolved{}, fmt.Errorf("options are set for %q, which is not selected", name)
		}
	}

	project := map[string]string{
		"home":      selection.Project.Home,
		"name":      selection.Project.Name,
		"workspace": selection.Project.Workspace,
	}
	merger := merger{
		result: Resolved{
			Preinstalled:     preset.Image.Preinstalled.clone(),
			PreinstalledPath: preset.Image.Preinstalled.toolPath(),
			Features:         map[string]map[string]any{},
			Env:              map[string]string{},
			Services:         map[string]Service{},
			Options:          map[string]map[string]string{},
		},
		owners: map[string]string{},
	}
	for _, definition := range definitions {
		values, err := definition.OptionValues(selection.Options[definition.Name])
		if err != nil {
			return Resolved{}, err
		}
		merger.result.Options[definition.Name] = values
		fragments := []Fragment{definition.Fragment}
		for _, variant := range definition.Variants {
			if variant.When.matches(preset.Name, addons, values) {
				fragments = append(fragments, variant.Fragment)
			}
		}
		for _, fragment := range fragments {
			merger.add(definition.Name, fragment.expand(values, project))
		}
	}
	merger.finish()
	if len(merger.errs) > 0 {
		return Resolved{}, errors.Join(merger.errs...)
	}
	return merger.result, nil
}

// OptionValues returns the effective option values: given values, which
// must be valid, and defaults for the rest.
func (d Definition) OptionValues(given map[string]string) (map[string]string, error) {
	values := make(map[string]string, len(d.Options))
	for name, option := range d.Options {
		values[name] = option.Default
	}
	for _, name := range sortedKeys(given) {
		option, ok := d.Options[name]
		if !ok {
			return nil, fmt.Errorf("%s has no option %q (available: %s)", d.Name, name, strings.Join(sortedKeys(d.Options), ", "))
		}
		if err := option.Check(given[name]); err != nil {
			label := d.Name + "." + name
			if option.Description != "" {
				label += " (" + option.Description + ")"
			}
			return nil, fmt.Errorf("option %s: %w", label, err)
		}
		values[name] = given[name]
	}
	return values, nil
}

func (c Condition) matches(preset string, addons []string, options map[string]string) bool {
	if len(c.Preset) > 0 && !slices.Contains(c.Preset, preset) {
		return false
	}
	if slices.Contains(c.NotPreset, preset) {
		return false
	}
	for _, addon := range c.Addon {
		if !slices.Contains(addons, addon) {
			return false
		}
	}
	for name, condition := range c.Option {
		value := options[name]
		if len(condition.In) > 0 && !slices.Contains(condition.In, value) {
			return false
		}
		if condition.Below != nil {
			number, ok := leadingInt(value)
			if !ok || number >= *condition.Below {
				return false
			}
		}
	}
	return true
}

func leadingInt(value string) (int, bool) {
	end := 0
	for end < len(value) && value[end] >= '0' && value[end] <= '9' {
		end++
	}
	number, err := strconv.Atoi(value[:end])
	return number, err == nil
}

// expand returns a copy of f with placeholders replaced.
func (f Fragment) expand(options, project map[string]string) Fragment {
	text := func(value string) string {
		return placeholder.ReplaceAllStringFunc(value, func(match string) string {
			parts := placeholder.FindStringSubmatch(match)
			if parts[1] == "option" {
				return options[parts[2]]
			}
			return project[parts[2]]
		})
	}
	texts := func(values []string) []string {
		result := make([]string, len(values))
		for i, value := range values {
			result[i] = text(value)
		}
		return result
	}
	textMap := func(values map[string]string) map[string]string {
		result := make(map[string]string, len(values))
		for key, value := range values {
			result[key] = text(value)
		}
		return result
	}

	result := Fragment{
		Image: Image{
			Base:    text(f.Image.Base),
			Apt:     slices.Clone(f.Image.Apt),
			RootRun: texts(f.Image.RootRun),
			UserRun: texts(f.Image.UserRun),
		},
		Features:  make(map[string]map[string]any, len(f.Features)),
		Container: Container{Path: texts(f.Container.Path), Env: textMap(f.Container.Env)},
		Setup:     Setup{Script: text(f.Setup.Script)},
		Services:  make(map[string]Service, len(f.Services)),
	}
	for id, values := range f.Features {
		feature := make(map[string]any, len(values))
		for key, value := range values {
			if s, ok := value.(string); ok {
				value = text(s)
			}
			feature[key] = value
		}
		result.Features[id] = feature
	}
	for name, service := range f.Services {
		expanded := Service{
			Image:        text(service.Image),
			Restart:      service.Restart,
			Environment:  textMap(service.Environment),
			Volumes:      textMap(service.Volumes),
			AppDependsOn: service.AppDependsOn,
		}
		if service.Healthcheck != nil {
			check := *service.Healthcheck
			check.Test = texts(check.Test)
			expanded.Healthcheck = &check
		}
		result.Services[name] = expanded
	}
	return result
}

type merger struct {
	result Resolved
	// owners maps "kind:key" to the definition that first contributed it.
	owners map[string]string
	errs   []error
}

// claim records owner for key and reports whether owner may set it. A
// definition may override its own earlier values, but not another's.
func (m *merger) claim(kind, key, owner string) bool {
	id := kind + ":" + key
	if previous, ok := m.owners[id]; ok && previous != owner {
		m.errs = append(m.errs, fmt.Errorf("%s %q is set by both %s and %s", kind, key, previous, owner))
		return false
	}
	m.owners[id] = owner
	return true
}

func (m *merger) add(owner string, f Fragment) {
	if f.Image.Base != "" {
		m.result.Base = f.Image.Base
	}
	if len(f.Image.Apt) > 0 {
		m.result.Apt = append(m.result.Apt, f.Image.Apt)
	}
	m.result.RootRun = append(m.result.RootRun, f.Image.RootRun...)
	m.result.UserRun = append(m.result.UserRun, f.Image.UserRun...)
	for _, id := range sortedKeys(f.Features) {
		if tool := m.result.PreinstalledInstaller(id); tool != "" {
			m.errs = append(m.errs, fmt.Errorf("preinstalled tool %q conflicts with definition %q feature %q; omit its installer when consuming the image", tool, owner, id))
		}
		if !m.claim("feature", id, owner) {
			continue
		}
		if existing, ok := m.result.Features[id]; ok {
			maps.Copy(existing, f.Features[id])
		} else {
			m.result.Features[id] = f.Features[id]
		}
	}
	for _, entry := range f.Container.Path {
		if !slices.Contains(m.result.Path, entry) {
			m.result.Path = append(m.result.Path, entry)
		}
	}
	for _, key := range sortedKeys(f.Container.Env) {
		if m.claim("containerEnv", key, owner) {
			m.result.Env[key] = f.Container.Env[key]
		}
	}
	if strings.TrimSpace(f.Setup.Script) != "" {
		m.result.Setup = append(m.result.Setup, f.Setup.Script)
	}
	for _, name := range sortedKeys(f.Services) {
		if !m.claim("service", name, owner) {
			continue
		}
		m.result.Services[name] = mergeService(m.result.Services[name], f.Services[name])
	}
}

func mergeService(base, override Service) Service {
	if override.Image != "" {
		base.Image = override.Image
	}
	if override.Restart != "" {
		base.Restart = override.Restart
	}
	if override.Healthcheck != nil {
		base.Healthcheck = override.Healthcheck
	}
	if override.AppDependsOn != "" {
		base.AppDependsOn = override.AppDependsOn
	}
	base.Environment = mergeMaps(base.Environment, override.Environment)
	base.Volumes = mergeMaps(base.Volumes, override.Volumes)
	return base
}

func mergeMaps(base, override map[string]string) map[string]string {
	result := make(map[string]string, len(base)+len(override))
	maps.Copy(result, base)
	maps.Copy(result, override)
	return result
}

// finish checks properties that only hold for the merged result.
func (m *merger) finish() {
	for _, entry := range m.result.Path {
		if !strings.HasPrefix(entry, "/") || strings.ContainsAny(entry, ": \t") {
			m.errs = append(m.errs, fmt.Errorf("container.path entry %q must be an absolute path without ':' or whitespace", entry))
		}
	}
	volumes := map[string]string{}
	for _, name := range sortedKeys(m.result.Services) {
		service := m.result.Services[name]
		if service.Image == "" {
			m.errs = append(m.errs, fmt.Errorf("service %q has no image", name))
		}
		for _, volume := range sortedKeys(service.Volumes) {
			if other, ok := volumes[volume]; ok {
				m.errs = append(m.errs, fmt.Errorf("named volume %q is used by both services %q and %q", volume, other, name))
			}
			volumes[volume] = name
		}
	}
}
