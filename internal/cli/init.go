package cli

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/detect"
	"github.com/mpm/projectsetup/internal/generate"
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
	presetValue := flags.String("preset", "", "node, ruby, rails, or python")
	name := flags.String("name", "", "project name")
	databaseValue := flags.String("database", "", "none, postgres, or sqlite")
	aiValue := flags.String("ai", "", "opencode, opencode,claude, or none")
	nodeVersion := flags.String("node-version", "", "Node version")
	rubyVersion := flags.String("ruby-version", "", "Ruby version")
	pythonVersion := flags.String("python-version", "", "Python version")
	managerValue := flags.String("package-manager", "", "project package manager")
	var ports repeatedPorts
	var systemPackages repeatedStrings
	flags.Var(&ports, "port", "forwarded application port (repeatable)")
	flags.Var(&systemPackages, "system-package", "additional apt package (repeatable)")
	nonInteractive := flags.Bool("non-interactive", false, "fail instead of prompting for missing values")
	force := flags.Bool("force", false, "replace an existing projectsetup-generated directory")
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
	flags.Visit(func(value *flag.Flag) {
		provided[value.Name] = true
	})

	detected, err := detect.Detect(root)
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

	preset, err := choosePreset(*presetValue, detected, prompt)
	if err != nil {
		return err
	}
	detail := detected.Details[preset]
	manager, err := choosePackageManager(*managerValue, preset, detail, prompt)
	if err != nil {
		return err
	}
	database, err := chooseDatabase(*databaseValue, provided["database"], detail, prompt)
	if err != nil {
		return err
	}
	tools, err := chooseAITools(*aiValue, provided["ai"], prompt)
	if err != nil {
		return err
	}
	version, err := resolveLanguageVersion(preset, *nodeVersion, *rubyVersion, *pythonVersion, detail.LanguageVersion)
	if err != nil {
		return err
	}
	if prompt != nil && version == "" {
		version, err = prompt.ask("Language version", config.DefaultLanguageVersion(preset))
		if err != nil {
			return err
		}
	}

	cfg, err := config.Normalize(config.Input{
		Root:            root,
		ProjectName:     *name,
		Preset:          preset,
		Database:        database,
		AITools:         tools,
		PackageManager:  manager,
		LanguageVersion: version,
		Ports:           ports,
		SystemPackages:  systemPackages,
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

func choosePreset(value string, detected detect.Result, prompt *prompter) (config.Preset, error) {
	if value != "" || prompt == nil {
		return resolvePreset(value, detected)
	}
	if len(detected.Presets) == 1 {
		return detected.Presets[0], nil
	}
	defaultValue := ""
	choices := []string{"node", "ruby", "rails", "python"}
	selected, err := prompt.choice("Preset", choices, defaultValue)
	if err != nil {
		return "", err
	}
	return config.ParsePreset(selected)
}

func choosePackageManager(value string, preset config.Preset, detail detect.PresetResult, prompt *prompter) (config.PackageManager, error) {
	if value != "" || prompt == nil || preset == config.PresetRuby || preset == config.PresetRails {
		return resolvePackageManager(value, preset, detail)
	}
	if len(detail.PackageManagerCandidates) == 1 {
		return detail.PackageManagerCandidates[0], nil
	}
	var choices []string
	defaultValue := ""
	switch preset {
	case config.PresetNode:
		choices = []string{"npm", "pnpm", "yarn"}
		defaultValue = "npm"
	case config.PresetPython:
		choices = []string{"pip", "poetry", "uv"}
		defaultValue = "pip"
	}
	if len(detail.PackageManagerCandidates) > 1 {
		defaultValue = ""
	}
	selected, err := prompt.choice("Package manager", choices, defaultValue)
	if err != nil {
		return "", err
	}
	return config.ParsePackageManager(selected)
}

func chooseDatabase(value string, wasProvided bool, detail detect.PresetResult, prompt *prompter) (config.Database, error) {
	if wasProvided || prompt == nil {
		return resolveDatabase(value)
	}
	defaultValue := string(config.DatabaseNone)
	if detail.SuggestedDatabase == config.DatabasePostgres {
		defaultValue = string(config.DatabasePostgres)
	}
	selected, err := prompt.choice("Database", []string{"none", "postgres", "sqlite"}, defaultValue)
	if err != nil {
		return "", err
	}
	return config.ParseDatabase(selected)
}

func chooseAITools(value string, wasProvided bool, prompt *prompter) ([]config.AITool, error) {
	if wasProvided || prompt == nil {
		return parseAITools(value, wasProvided)
	}
	selected, err := prompt.choice("AI tools", []string{"opencode", "opencode,claude", "none"}, "opencode")
	if err != nil {
		return nil, err
	}
	return parseAITools(selected, true)
}

func printConfigSummary(output io.Writer, cfg config.Config) {
	manager := string(cfg.PackageManager)
	if manager == "" {
		manager = "none"
	}
	tools := make([]string, len(cfg.AITools))
	for i, tool := range cfg.AITools {
		tools[i] = string(tool)
	}
	if len(tools) == 0 {
		tools = []string{"none"}
	}
	fmt.Fprintf(output, "\nConfiguration:\n  Project: %s\n  Preset: %s\n  Language version: %s\n  Package manager: %s\n  Database: %s\n  AI tools: %s\n  Ports: %v\n  System packages: %s\n\n",
		cfg.ProjectName, cfg.Preset, cfg.LanguageVersion, manager, cfg.Database, strings.Join(tools, ", "), cfg.Ports, strings.Join(cfg.SystemPackages, ", "))
}

func resolvePreset(value string, detected detect.Result) (config.Preset, error) {
	if value != "" {
		return config.ParsePreset(value)
	}
	switch len(detected.Presets) {
	case 0:
		return "", fmt.Errorf("could not infer a preset; pass --preset node, ruby, rails, or python")
	case 1:
		return detected.Presets[0], nil
	default:
		return "", fmt.Errorf("multiple project presets detected (%s); pass --preset explicitly", joinPresets(detected.Presets))
	}
}

func resolvePackageManager(value string, preset config.Preset, detail detect.PresetResult) (config.PackageManager, error) {
	if value != "" {
		return config.ParsePackageManager(value)
	}
	if len(detail.PackageManagerCandidates) > 1 {
		values := make([]string, len(detail.PackageManagerCandidates))
		for i, manager := range detail.PackageManagerCandidates {
			values[i] = string(manager)
		}
		return "", fmt.Errorf("multiple package managers detected (%s); pass --package-manager explicitly", strings.Join(values, ", "))
	}
	if len(detail.PackageManagerCandidates) == 1 {
		return detail.PackageManagerCandidates[0], nil
	}
	if preset == config.PresetRuby || preset == config.PresetRails {
		return "", nil
	}
	return "", nil
}

func resolveDatabase(value string) (config.Database, error) {
	if value == "" {
		return config.DatabaseNone, nil
	}
	return config.ParseDatabase(value)
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
	if value == "opencode" {
		return []config.AITool{config.AIToolOpenCode}, nil
	}
	if value == "opencode,claude" {
		return []config.AITool{config.AIToolOpenCode, config.AIToolClaude}, nil
	}
	return nil, fmt.Errorf("--ai must be opencode, opencode,claude, or none")
}

func resolveLanguageVersion(preset config.Preset, node, ruby, python, detected string) (string, error) {
	if preset != config.PresetNode && node != "" {
		return "", fmt.Errorf("--node-version requires --preset node")
	}
	if preset != config.PresetRuby && preset != config.PresetRails && ruby != "" {
		return "", fmt.Errorf("--ruby-version requires --preset ruby or rails")
	}
	if preset != config.PresetPython && python != "" {
		return "", fmt.Errorf("--python-version requires --preset python")
	}
	switch preset {
	case config.PresetNode:
		if node != "" {
			return node, nil
		}
	case config.PresetRuby, config.PresetRails:
		if ruby != "" {
			return ruby, nil
		}
	case config.PresetPython:
		if python != "" {
			return python, nil
		}
	}
	return detected, nil
}

func joinPresets(presets []config.Preset) string {
	values := make([]string, len(presets))
	for i, preset := range presets {
		values[i] = string(preset)
	}
	return strings.Join(values, ", ")
}
