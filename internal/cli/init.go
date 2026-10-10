package cli

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/detect"
	"github.com/mpm/projectsetup/internal/generate"
	"github.com/mpm/projectsetup/internal/presets"
)

type repeatedStrings []string

func (values *repeatedStrings) String() string { return strings.Join(*values, ",") }

func (values *repeatedStrings) Set(value string) error {
	*values = append(*values, value)
	return nil
}

type repeatedPorts []int

func (ports *repeatedPorts) String() string {
	values := make([]string, len(*ports))
	for i, port := range *ports {
		values[i] = strconv.Itoa(port)
	}
	return strings.Join(values, ",")
}

func (ports *repeatedPorts) Set(value string) error {
	port, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("port must be an integer: %q", value)
	}
	*ports = append(*ports, port)
	return nil
}

func runInit(root string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("projectsetup init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	presetValue := flags.String("preset", "", "preset definition (projectsetup preset list)")
	name := flags.String("name", "", "project name")
	var addonValues, settings repeatedStrings
	flags.Var(&addonValues, "addon", "add-on definition (repeatable)")
	flags.Var(&settings, "set", "option value as [DEFINITION.]OPTION=VALUE; DEFINITION defaults to the preset (repeatable)")
	databaseValue := flags.String("database", "", "alias for --addon: "+config.DescribeChoices(config.Databases))
	aiValue := flags.String("ai", "", "comma-separated "+joinChoices(config.AITools(), ", ")+"; or none")
	nodeVersion := flags.String("node-version", "", "alias for --set node.version=VERSION")
	rubyVersion := flags.String("ruby-version", "", "alias for --set ruby.version=VERSION or rails.version=VERSION")
	pythonVersion := flags.String("python-version", "", "alias for --set python.version=VERSION")
	managerValue := flags.String("package-manager", "", "alias for --set package_manager=VALUE")
	postgresImage := flags.String("postgres-image", "", "literal PostgreSQL image tag with optional sha256 digest; major must match --postgres-version (--force preserves it)")
	postgresVersion := flags.String("postgres-version", "", "alias for --set postgres.version=MAJOR (--force keeps the existing version)")
	var ports repeatedPorts
	var systemPackages repeatedStrings
	flags.Var(&ports, "port", "forwarded application port (repeatable)")
	flags.Var(&systemPackages, "system-package", "additional apt package (repeatable)")
	nonInteractive := flags.Bool("non-interactive", false, "fail instead of prompting for missing values")
	force := flags.Bool("force", false, "replace an existing projectsetup-generated directory")
	listOptions := flags.Bool("list-options", false, "list accepted presets, add-ons, options, and AI tools without generating files")
	jsonOutput := flags.Bool("json", false, "with --list-options, print the listing as JSON")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("init does not accept positional arguments: %s", strings.Join(flags.Args(), " "))
	}

	provided := make(map[string]bool)
	var generationFlags []string
	flags.Visit(func(value *flag.Flag) {
		provided[value.Name] = true
		switch value.Name {
		case "list-options", "json", "non-interactive":
		default:
			generationFlags = append(generationFlags, "--"+value.Name)
		}
	})
	if *listOptions && len(generationFlags) > 0 {
		return fmt.Errorf("--list-options cannot be combined with %s", strings.Join(generationFlags, ", "))
	}
	if *jsonOutput && !*listOptions {
		return fmt.Errorf("--json requires --list-options")
	}
	registry, err := presets.Load()
	if err != nil {
		return fmt.Errorf("load preset definitions: %w", err)
	}
	if *listOptions {
		return writeOptions(stdout, config.ListOptions(registry), *jsonOutput)
	}

	detected, err := detect.Detect(root, registry)
	if err != nil {
		return err
	}
	for _, warning := range detected.Warnings {
		fmt.Fprintf(stderr, "warning: %s\n", warning)
	}

	var prompt *prompter
	if !*nonInteractive {
		prompt = newPrompter(stdin, stdout)
	}

	projectName, err := chooseProjectName(root, *name, provided["name"], prompt)
	if err != nil {
		return err
	}
	preset, err := choosePreset(*presetValue, detected, registry, prompt)
	if err != nil {
		return err
	}
	options, err := parseSettings(preset, settings)
	if err != nil {
		return err
	}
	for _, alias := range []struct {
		flag, value string
		presets     []string
		option      string
	}{
		{"--node-version", *nodeVersion, []string{"node"}, config.OptionVersion},
		{"--ruby-version", *rubyVersion, []string{"ruby", "rails"}, config.OptionVersion},
		{"--python-version", *pythonVersion, []string{"python"}, config.OptionVersion},
		{"--package-manager", *managerValue, nil, config.OptionPackageManager},
	} {
		if alias.value == "" {
			continue
		}
		if alias.presets != nil && !slices.Contains(alias.presets, preset) {
			return fmt.Errorf("%s requires --preset %s", alias.flag, strings.Join(alias.presets, " or "))
		}
		if err := setOption(options, preset, alias.option, alias.value, alias.flag); err != nil {
			return err
		}
	}

	definition, _ := registry.Lookup(preset)
	if err := choosePresetOptions(definition, detected.Details[preset], options, prompt); err != nil {
		return err
	}

	addons, err := chooseAddons(addonValues, *databaseValue, provided["addon"] || provided["database"], detected.Details[preset], registry, prompt)
	if err != nil {
		return err
	}
	if *postgresVersion != "" {
		if !slices.Contains(addons, "postgres") {
			return fmt.Errorf("--postgres-version requires database %q; pass --database postgres or --addon postgres", "postgres")
		}
		if err := setOption(options, "postgres", config.OptionVersion, *postgresVersion, "--postgres-version"); err != nil {
			return err
		}
	}
	if *force {
		previousImage := keepAddonOptions(root, addons, options)
		if !provided["postgres-image"] {
			*postgresImage = string(previousImage)
		}
	}
	tools, err := chooseAITools(*aiValue, provided["ai"], prompt)
	if err != nil {
		return err
	}

	cfg, err := config.Normalize(config.Input{
		PostgresImage:  config.PostgresImageRef(*postgresImage),
		Root:           root,
		Registry:       registry,
		ProjectName:    projectName,
		Preset:         preset,
		Addons:         addons,
		Options:        options,
		AITools:        tools,
		Ports:          ports,
		SystemPackages: systemPackages,
	})
	if err != nil {
		return fmt.Errorf("normalize initialization options: %w", err)
	}
	if prompt != nil {
		printConfigSummary(stdout, cfg)
		confirmed, err := prompt.confirm("Generate .devcontainer with this configuration?")
		if err != nil {
			return err
		}
		if !confirmed {
			_, err = io.WriteString(stdout, "Initialization cancelled; no files written.\n")
			return err
		}
	}
	if err := generate.Write(root, cfg, *force); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "Generated .devcontainer for %s (%s).\n", cfg.ProjectName, cfg.Preset)
	return err
}

// parseSettings reads --set values. A setting without a definition prefix
// applies to the preset.
func parseSettings(preset string, settings []string) (map[string]map[string]string, error) {
	options := map[string]map[string]string{}
	for _, setting := range settings {
		key, value, ok := strings.Cut(setting, "=")
		definition, option, qualified := strings.Cut(key, ".")
		if !qualified {
			definition, option = preset, key
		}
		if !ok || definition == "" || option == "" {
			return nil, fmt.Errorf("--set %q must have the form [DEFINITION.]OPTION=VALUE", setting)
		}
		if err := setOption(options, definition, option, value, "--set "+setting); err != nil {
			return nil, err
		}
	}
	return options, nil
}

// setOption records value and rejects a different value for the same option.
func setOption(options map[string]map[string]string, definition, option, value, source string) error {
	if previous, ok := options[definition][option]; ok && previous != value {
		return fmt.Errorf("%s conflicts with %s.%s=%s set earlier", source, definition, option, previous)
	}
	if options[definition] == nil {
		options[definition] = map[string]string{}
	}
	options[definition][option] = value
	return nil
}

type prompter struct {
	scanner *bufio.Scanner
	output  io.Writer
}

func newPrompter(input io.Reader, output io.Writer) *prompter {
	return &prompter{scanner: bufio.NewScanner(input), output: output}
}

func (p *prompter) ask(label, defaultValue string) (string, error) {
	if defaultValue == "" {
		fmt.Fprintf(p.output, "%s: ", label)
	} else {
		fmt.Fprintf(p.output, "%s [%s]: ", label, defaultValue)
	}
	if !p.scanner.Scan() {
		if err := p.scanner.Err(); err != nil {
			return "", fmt.Errorf("read response for %s: %w", label, err)
		}
		return "", fmt.Errorf("read response for %s: input ended", label)
	}
	value := strings.TrimSpace(p.scanner.Text())
	if value == "" {
		value = defaultValue
	}
	return value, nil
}

func (p *prompter) choice(label string, choices []string, defaultValue string) (string, error) {
	for {
		value, err := p.ask(fmt.Sprintf("%s (%s)", label, strings.Join(choices, "/")), defaultValue)
		if err != nil {
			return "", err
		}
		for _, choice := range choices {
			if value == choice {
				return value, nil
			}
		}
		fmt.Fprintf(p.output, "Please choose one of: %s.\n", strings.Join(choices, ", "))
	}
}

// option asks for an option value until it is valid.
func (p *prompter) option(label string, option presets.Option, defaultValue string) (string, error) {
	if len(option.Choices) > 0 {
		return p.choice(label, option.Choices, defaultValue)
	}
	for {
		value, err := p.ask(label, defaultValue)
		if err != nil {
			return "", err
		}
		if err := option.Check(value); err != nil {
			fmt.Fprintf(p.output, "%s: %v.\n", label, err)
			continue
		}
		return value, nil
	}
}

func (p *prompter) confirm(label string) (bool, error) {
	for {
		value, err := p.ask(label+" (y/N)", "n")
		if err != nil {
			return false, err
		}
		switch strings.ToLower(value) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintln(p.output, "Please answer yes or no.")
		}
	}
}

func (p *prompter) projectName(defaultValue string) (string, error) {
	for {
		value, err := p.ask("Project name", defaultValue)
		if err != nil {
			return "", err
		}
		if err := config.ValidateProjectName(value); err != nil {
			fmt.Fprintf(p.output, "%v.\n", err)
			continue
		}
		return value, nil
	}
}

// chooseProjectName returns the single project name used for every generated
// file. An explicit --name must be valid as-is; non-interactive mode rejects
// invalid values, and interactive mode proposes the normalized name instead.
// Directory-derived names are normalized, and interactive mode asks for
// confirmation when normalization changed them.
func chooseProjectName(root, value string, wasProvided bool, prompt *prompter) (string, error) {
	if wasProvided {
		err := config.ValidateProjectName(value)
		if err == nil {
			return value, nil
		}
		if prompt == nil {
			return "", fmt.Errorf("--name: %w", err)
		}
		fmt.Fprintf(prompt.output, "--name: %v.\n", err)
		return prompt.projectName(config.SanitizeName(value))
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	base := filepath.Base(absolute)
	if config.ValidProjectName(base) {
		return base, nil
	}
	if prompt == nil {
		name, err := config.DefaultProjectName(root)
		if err != nil {
			return "", fmt.Errorf("%w; pass --name NAME", err)
		}
		return name, nil
	}
	fmt.Fprintf(prompt.output, "Directory name %q is not a valid project name; it must match %s.\n", base, config.ProjectNamePattern)
	return prompt.projectName(config.SanitizeName(base))
}

func choosePreset(value string, detected detect.Result, registry *presets.Registry, prompt *prompter) (string, error) {
	if value != "" || prompt == nil {
		return resolvePreset(value, detected, registry)
	}
	if len(detected.Presets) == 1 {
		return detected.Presets[0], nil
	}
	return prompt.choice("Preset", registry.Names(presets.KindPreset), "")
}

// choosePresetOptions fills unset preset options from detection. A single
// detected value is used; several are ambiguous. Interactive mode asks for
// every remaining option, and non-interactive mode leaves it at its default.
func choosePresetOptions(definition presets.Definition, detail detect.PresetResult, options map[string]map[string]string, prompt *prompter) error {
	for _, name := range slices.Sorted(maps.Keys(definition.Options)) {
		if _, ok := options[definition.Name][name]; ok {
			continue
		}
		option := definition.Options[name]
		detected := detail.Options[name]
		value := ""
		switch {
		case len(detected) == 1:
			value = detected[0]
		case len(detected) > 1 && prompt == nil:
			return fmt.Errorf("multiple values detected for %s.%s (%s); pass --set %s.%s=VALUE", definition.Name, name, strings.Join(detected, ", "), definition.Name, name)
		case prompt != nil:
			defaultValue := option.Default
			if len(detected) > 1 {
				defaultValue = ""
			}
			var err error
			if value, err = prompt.option(definition.Name+"."+name, option, defaultValue); err != nil {
				return err
			}
		default:
			continue
		}
		if err := setOption(options, definition.Name, name, value, "detected value"); err != nil {
			return err
		}
	}
	return nil
}

// chooseAddons combines --addon and the --database alias. Without either,
// interactive mode asks and offers detected suggestions as the default;
// non-interactive mode selects none.
func chooseAddons(values []string, database string, wasProvided bool, detail detect.PresetResult, registry *presets.Registry, prompt *prompter) ([]string, error) {
	addons := slices.Clone(values)
	if database != "" {
		if !slices.Contains(config.Databases, database) {
			return nil, fmt.Errorf("unsupported database %q (expected %s)", database, config.DescribeChoices(config.Databases))
		}
		if database != "none" {
			addons = append(addons, database)
		}
	}
	if wasProvided || prompt == nil {
		return addons, nil
	}
	available := registry.Names(presets.KindAddon)
	defaultValue := "none"
	if len(detail.SuggestedAddons) > 0 {
		defaultValue = strings.Join(detail.SuggestedAddons, ",")
	}
	for {
		value, err := prompt.ask(fmt.Sprintf("Add-ons (%s; comma-separated or none)", strings.Join(available, ", ")), defaultValue)
		if err != nil {
			return nil, err
		}
		if value == "none" {
			return nil, nil
		}
		selected := strings.Split(value, ",")
		valid := true
		for i, name := range selected {
			selected[i] = strings.TrimSpace(name)
			if !slices.Contains(available, selected[i]) {
				valid = false
			}
		}
		if valid {
			return selected, nil
		}
		fmt.Fprintf(prompt.output, "Please choose from: %s, or none.\n", strings.Join(available, ", "))
	}
}

func chooseAITools(value string, wasProvided bool, prompt *prompter) ([]config.AITool, error) {
	if wasProvided || prompt == nil {
		return parseAITools(value, wasProvided)
	}
	selected, err := prompt.choice("AI tools", []string{"opencode", "opencode,claude", "opencode,codex", "opencode,claude,codex", "claude", "codex", "claude,codex", "none"}, "opencode")
	if err != nil {
		return nil, err
	}
	return parseAITools(selected, true)
}

// keepAddonOptions carries option values of add-ons that the existing
// generated manifest also selected into options when they are not set, so
// regenerating with --force keeps, for example, a PostgreSQL data volume
// readable. Its explicit image reference is returned when postgres remains
// selected. Unusable manifests are ignored.
func keepAddonOptions(root string, addons []string, options map[string]map[string]string) config.PostgresImageRef {
	file, err := os.Open(filepath.Join(root, ".devcontainer", "projectsetup.json"))
	if err != nil {
		return ""
	}
	defer file.Close()
	manifest, err := config.ReadManifest(file)
	if err != nil || manifest.GeneratedBy != config.GeneratedBy {
		return ""
	}
	for _, ref := range manifest.Addons {
		if !slices.Contains(addons, ref.Name) {
			continue
		}
		for option, value := range manifest.Options[ref.Name] {
			if _, ok := options[ref.Name][option]; !ok {
				setOption(options, ref.Name, option, value, "existing manifest")
			}
		}
	}
	if slices.Contains(addons, "postgres") {
		return manifest.PostgresImage
	}
	return ""
}

func printConfigSummary(output io.Writer, cfg config.Config) {
	if cfg.PostgresImage != "" {
		fmt.Fprintf(output, "PostgreSQL image: %s\n", cfg.PostgresImage)
	}
	tools := make([]string, len(cfg.AITools))
	for i, tool := range cfg.AITools {
		tools[i] = string(tool)
	}
	if len(tools) == 0 {
		tools = []string{"none"}
	}
	var definitions []string
	for _, definition := range cfg.Definitions {
		definitions = append(definitions, fmt.Sprintf("%s %s (%s)", definition.Name, definition.Version, definition.Source))
	}
	addons := "none"
	if len(definitions) > 1 {
		addons = strings.Join(definitions[1:], ", ")
	}
	var options []string
	for _, definition := range slices.Sorted(maps.Keys(cfg.Options)) {
		for _, name := range slices.Sorted(maps.Keys(cfg.Options[definition])) {
			options = append(options, fmt.Sprintf("%s.%s=%s", definition, name, cfg.Options[definition][name]))
		}
	}
	if len(options) == 0 {
		options = []string{"none"}
	}
	fmt.Fprintf(output, "\nConfiguration:\n  Project: %s\n  Preset: %s\n  Add-ons: %s\n  Options: %s\n  AI tools: %s\n  Ports: %v\n  System packages: %s\n\n",
		cfg.ProjectName, definitions[0], addons, strings.Join(options, ", "), strings.Join(tools, ", "), cfg.Ports, strings.Join(cfg.SystemPackages, ", "))
}

func resolvePreset(value string, detected detect.Result, registry *presets.Registry) (string, error) {
	if value != "" {
		if definition, ok := registry.Lookup(value); !ok || definition.Kind != presets.KindPreset {
			return "", fmt.Errorf("unsupported preset %q (expected %s)", value, config.DescribeChoices(registry.Names(presets.KindPreset)))
		}
		return value, nil
	}
	switch len(detected.Presets) {
	case 0:
		return "", fmt.Errorf("could not infer a preset; pass --preset %s", config.DescribeChoices(registry.Names(presets.KindPreset)))
	case 1:
		return detected.Presets[0], nil
	default:
		return "", fmt.Errorf("multiple project presets detected (%s); pass --preset explicitly", strings.Join(detected.Presets, ", "))
	}
}

func resolveAITools(value string, flags *flag.FlagSet) ([]config.AITool, error) {
	provided := false
	flags.Visit(func(item *flag.Flag) {
		if item.Name == "ai" {
			provided = true
		}
	})
	return parseAITools(value, provided)
}

func parseAITools(value string, provided bool) ([]config.AITool, error) {
	if !provided {
		return nil, nil
	}
	if value == "none" {
		return []config.AITool{}, nil
	}
	var tools []config.AITool
	for _, part := range strings.Split(value, ",") {
		tool, err := config.ParseAITool(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("--ai: %w; use comma-separated %s, or none alone", err, joinChoices(config.AITools(), ", "))
		}
		tools = append(tools, tool)
	}
	return tools, nil
}
