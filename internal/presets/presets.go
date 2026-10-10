// Package presets loads, validates, and resolves preset and add-on
// definitions. A definition only contributes to the generated setup; the core
// that dworm relies on (user, Compose app service, AI tools) stays in Go.
package presets

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// SchemaVersion is the definition file format this build reads.
const SchemaVersion = 1

type Kind string

const (
	KindPreset Kind = "preset"
	KindAddon  Kind = "addon"
)

// Definition is one preset or add-on file.
type Definition struct {
	Schema      int               `toml:"schema"`
	Kind        Kind              `toml:"kind"`
	Name        string            `toml:"name"`
	Version     string            `toml:"version"`
	Description string            `toml:"description"`
	Options     map[string]Option `toml:"options"`
	Fragment
	Variants []Variant `toml:"variant"`
}

// Option is a value the user can set. Values must match Choices or Pattern.
type Option struct {
	Description string   `toml:"description"`
	Default     string   `toml:"default"`
	Choices     []string `toml:"choices"`
	Pattern     string   `toml:"pattern"`
}

// Fragment is what a definition or a matching variant contributes.
type Fragment struct {
	Image     Image                     `toml:"image"`
	Features  map[string]map[string]any `toml:"features"`
	Container Container                 `toml:"container"`
	Setup     Setup                     `toml:"setup"`
	Services  map[string]Service        `toml:"services"`
}

type Image struct {
	Base    string   `toml:"base"`
	Apt     []string `toml:"apt"`
	RootRun []string `toml:"root_run"`
	UserRun []string `toml:"user_run"`
}

type Container struct {
	Path []string          `toml:"path"`
	Env  map[string]string `toml:"env"`
}

type Setup struct {
	Script string `toml:"script"`
}

// Service is a Compose sidecar next to the app service.
type Service struct {
	Image        string            `toml:"image"`
	Restart      string            `toml:"restart"`
	Environment  map[string]string `toml:"environment"`
	Healthcheck  *Healthcheck      `toml:"healthcheck"`
	Volumes      map[string]string `toml:"volumes"`
	AppDependsOn string            `toml:"app_depends_on"`
}

type Healthcheck struct {
	Test     []string `toml:"test"`
	Interval string   `toml:"interval"`
	Timeout  string   `toml:"timeout"`
	Retries  int      `toml:"retries"`
}

// Variant is a fragment applied only when every condition in When matches.
type Variant struct {
	When Condition `toml:"when"`
	Fragment
}

type Condition struct {
	Preset    []string                   `toml:"preset"`
	NotPreset []string                   `toml:"not_preset"`
	Addon     []string                   `toml:"addon"`
	Option    map[string]OptionCondition `toml:"option"`
}

type OptionCondition struct {
	In    []string `toml:"in"`
	Below *int     `toml:"below"`
}

var (
	validName       = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	validOptionName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	validVersion    = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	validEnvName    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	validAptPackage = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9+.-]*$`)
	// validOptionValue keeps substituted values safe inside shell scripts,
	// Dockerfiles, and YAML regardless of an option's own pattern.
	validOptionValue = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
	placeholder      = regexp.MustCompile(`\$\{(option|project):([^}]*)\}`)
)

// ProjectPlaceholders are the names accepted in ${project:NAME}.
var ProjectPlaceholders = []string{"home", "name", "workspace"}

// ReservedEnv are containerEnv keys owned by the core configuration.
var ReservedEnv = []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "HOME", "PATH", "USER"}

// ReservedServices are Compose service names owned by the core configuration.
var ReservedServices = []string{"app"}

var restartPolicies = []string{"", "always", "no", "on-failure", "unless-stopped"}
var dependsOnConditions = []string{"", "service_healthy", "service_started"}

// Parse decodes and validates one definition. origin names the source in
// errors, such as a file path or URL.
func Parse(data []byte, origin string) (Definition, error) {
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var definition Definition
	if err := decoder.Decode(&definition); err != nil {
		var strict *toml.StrictMissingError
		if errors.As(err, &strict) {
			return Definition{}, fmt.Errorf("%s: unknown fields:\n%s", origin, strict.String())
		}
		var decode *toml.DecodeError
		if errors.As(err, &decode) {
			row, column := decode.Position()
			return Definition{}, fmt.Errorf("%s:%d:%d: %v", origin, row, column, err)
		}
		return Definition{}, fmt.Errorf("%s: %w", origin, err)
	}
	if problems := definition.problems(); len(problems) > 0 {
		return Definition{}, fmt.Errorf("%s: invalid definition:\n  %s", origin, strings.Join(problems, "\n  "))
	}
	return definition, nil
}

func (d Definition) problems() []string {
	var problems []string
	fail := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	if d.Schema != SchemaVersion {
		fail("schema is %d; this projectsetup reads schema %d", d.Schema, SchemaVersion)
	}
	if d.Kind != KindPreset && d.Kind != KindAddon {
		fail("kind is %q; expected %q or %q", d.Kind, KindPreset, KindAddon)
	}
	if !validName.MatchString(d.Name) {
		fail("name %q must match %s", d.Name, validName)
	}
	if !validVersion.MatchString(d.Version) {
		fail("version %q must be MAJOR.MINOR.PATCH", d.Version)
	}
	if strings.TrimSpace(d.Description) == "" {
		fail("description is required")
	}

	for _, name := range sortedKeys(d.Options) {
		option := d.Options[name]
		where := "options." + name
		if !validOptionName.MatchString(name) {
			fail("%s: option name must match %s", where, validOptionName)
		}
		if (len(option.Choices) == 0) == (option.Pattern == "") {
			fail("%s: set exactly one of choices or pattern", where)
		}
		if option.Pattern != "" {
			if _, err := regexp.Compile(option.Pattern); err != nil {
				fail("%s: invalid pattern: %v", where, err)
				continue
			}
		}
		seen := map[string]bool{}
		for _, choice := range option.Choices {
			if !validOptionValue.MatchString(choice) {
				fail("%s: choice %q must match %s", where, choice, validOptionValue)
			}
			if seen[choice] {
				fail("%s: choice %q is duplicated", where, choice)
			}
			seen[choice] = true
		}
		if option.Default == "" {
			fail("%s: default is required", where)
		} else if err := option.check(option.Default); err != nil {
			fail("%s: default: %v", where, err)
		}
	}

	if d.Kind == KindPreset && d.Image.Base == "" {
		fail("image.base is required for a preset")
	}
	if d.Kind == KindAddon && d.Image.Base != "" {
		fail("image.base can only be set by a preset")
	}
	problems = append(problems, d.Fragment.problems("", d.Options)...)

	for i, variant := range d.Variants {
		where := fmt.Sprintf("variant[%d]", i)
		if variant.Image.Base != "" {
			fail("%s: image.base cannot be set in a variant", where)
		}
		problems = append(problems, variant.When.problems(where+".when", d.Options)...)
		problems = append(problems, variant.Fragment.problems(where+".", d.Options)...)
	}
	return problems
}

func (f Fragment) problems(prefix string, options map[string]Option) []string {
	var problems []string
	fail := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	text := func(where, value string) {
		if err := checkPlaceholders(value, options); err != nil {
			fail("%s%s: %v", prefix, where, err)
		}
	}

	text("image.base", f.Image.Base)
	for _, pkg := range f.Image.Apt {
		if !validAptPackage.MatchString(pkg) {
			fail("%simage.apt: %q is not a valid apt package name", prefix, pkg)
		}
	}
	for _, run := range []struct {
		field string
		steps []string
	}{{"image.root_run", f.Image.RootRun}, {"image.user_run", f.Image.UserRun}} {
		// Single lines cannot add instructions such as USER after a RUN.
		for _, step := range run.steps {
			if strings.TrimSpace(step) == "" || strings.ContainsAny(step, "\r\n") {
				fail("%s%s: each step must be a single non-empty line", prefix, run.field)
			}
			text(run.field, step)
		}
	}

	for _, id := range sortedKeys(f.Features) {
		where := fmt.Sprintf("features.%q", id)
		if id == "" || strings.ContainsAny(id, " \t\r\n") {
			fail("%s%s: feature ID must be non-empty without whitespace", prefix, where)
		}
		for _, key := range sortedKeys(f.Features[id]) {
			switch value := f.Features[id][key].(type) {
			case string:
				text(where+"."+key, value)
			case bool, int64, float64:
			default:
				fail("%s%s.%s: value must be a string, boolean, or number", prefix, where, key)
			}
		}
	}

	for _, entry := range f.Container.Path {
		if !strings.HasPrefix(entry, "/") && !strings.HasPrefix(entry, "${project:") {
			fail("%scontainer.path: %q must be absolute", prefix, entry)
		}
		text("container.path", entry)
	}
	for _, key := range sortedKeys(f.Container.Env) {
		if !validEnvName.MatchString(key) {
			fail("%scontainer.env: %q is not a valid variable name", prefix, key)
		}
		if slices.Contains(ReservedEnv, key) {
			fail("%scontainer.env: %s is managed by projectsetup", prefix, key)
		}
		text("container.env."+key, f.Container.Env[key])
	}
	text("setup.script", f.Setup.Script)

	for _, name := range sortedKeys(f.Services) {
		service := f.Services[name]
		where := "services." + name
		if !validName.MatchString(name) {
			fail("%s%s: service name must match %s", prefix, where, validName)
		}
		if slices.Contains(ReservedServices, name) {
			fail("%s%s: service name %q is reserved for the app container", prefix, where, name)
		}
		text(where+".image", service.Image)
		if !slices.Contains(restartPolicies, service.Restart) {
			fail("%s%s.restart: %q is not a Compose restart policy", prefix, where, service.Restart)
		}
		if !slices.Contains(dependsOnConditions, service.AppDependsOn) {
			fail("%s%s.app_depends_on: expected service_started or service_healthy", prefix, where)
		}
		for _, key := range sortedKeys(service.Environment) {
			if !validEnvName.MatchString(key) {
				fail("%s%s.environment: %q is not a valid variable name", prefix, where, key)
			}
			text(where+".environment."+key, service.Environment[key])
		}
		for _, volume := range sortedKeys(service.Volumes) {
			if !validName.MatchString(volume) {
				fail("%s%s.volumes: named volume %q must match %s", prefix, where, volume, validName)
			}
			if !strings.HasPrefix(service.Volumes[volume], "/") {
				fail("%s%s.volumes.%s: container path must be absolute", prefix, where, volume)
			}
			text(where+".volumes."+volume, service.Volumes[volume])
		}
		if check := service.Healthcheck; check != nil {
			if len(check.Test) == 0 {
				fail("%s%s.healthcheck.test is required", prefix, where)
			}
			for _, part := range check.Test {
				text(where+".healthcheck.test", part)
			}
			if check.Retries < 0 {
				fail("%s%s.healthcheck.retries must not be negative", prefix, where)
			}
		}
	}
	return problems
}

func (c Condition) problems(where string, options map[string]Option) []string {
	var problems []string
	fail := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if len(c.Preset) == 0 && len(c.NotPreset) == 0 && len(c.Addon) == 0 && len(c.Option) == 0 {
		fail("%s: at least one condition is required", where)
	}
	for _, names := range [][]string{c.Preset, c.NotPreset, c.Addon} {
		for _, name := range names {
			if !validName.MatchString(name) {
				fail("%s: %q is not a valid definition name", where, name)
			}
		}
	}
	for _, name := range sortedKeys(c.Option) {
		if _, ok := options[name]; !ok {
			fail("%s.option.%s: the definition has no option %q", where, name, name)
		}
		if len(c.Option[name].In) == 0 && c.Option[name].Below == nil {
			fail("%s.option.%s: set in or below", where, name)
		}
	}
	return problems
}

func (o Option) check(value string) error {
	if !validOptionValue.MatchString(value) {
		return fmt.Errorf("%q must match %s", value, validOptionValue)
	}
	if len(o.Choices) > 0 {
		if !slices.Contains(o.Choices, value) {
			return fmt.Errorf("%q is not one of %s", value, strings.Join(o.Choices, ", "))
		}
		return nil
	}
	pattern, err := regexp.Compile(`^(?:` + o.Pattern + `)$`)
	if err != nil {
		return fmt.Errorf("invalid pattern: %w", err)
	}
	if !pattern.MatchString(value) {
		return fmt.Errorf("%q does not match %s", value, o.Pattern)
	}
	return nil
}

func checkPlaceholders(value string, options map[string]Option) error {
	for _, match := range placeholder.FindAllStringSubmatch(value, -1) {
		switch match[1] {
		case "option":
			if _, ok := options[match[2]]; !ok {
				return fmt.Errorf("unknown placeholder %s; the definition has no option %q", match[0], match[2])
			}
		case "project":
			if !slices.Contains(ProjectPlaceholders, match[2]) {
				return fmt.Errorf("unknown placeholder %s; use ${project:%s}", match[0], strings.Join(ProjectPlaceholders, "}, ${project:"))
			}
		}
	}
	return nil
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
