package cli

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"projectsetup/internal/config"
	"projectsetup/internal/detect"
	"projectsetup/internal/generate"
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

func runInit(root string, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("projectsetup init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	presetValue := flags.String("preset", "", "node, rails, or python")
	name := flags.String("name", "", "project name")
	databaseValue := flags.String("database", "", "none or postgres")
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

	hasConfigurationFlag := false
	flags.Visit(func(value *flag.Flag) {
		if value.Name != "force" && value.Name != "non-interactive" {
			hasConfigurationFlag = true
		}
	})
	if !*nonInteractive && !hasConfigurationFlag {
		return fmt.Errorf("interactive initialization is not implemented yet; rerun with --non-interactive and explicit flags")
	}

	detected, err := detect.Detect(root)
	if err != nil {
		return err
	}
	for _, warning := range detected.Warnings {
		fmt.Fprintf(stderr, "warning: %s\n", warning)
	}

	preset, err := resolvePreset(*presetValue, detected)
	if err != nil {
		return err
	}
	detail := detected.Details[preset]
	manager, err := resolvePackageManager(*managerValue, preset, detail)
	if err != nil {
		return err
	}
	database, err := resolveDatabase(*databaseValue)
	if err != nil {
		return err
	}
	tools, err := resolveAITools(*aiValue, flags)
	if err != nil {
		return err
	}
	version, err := resolveLanguageVersion(preset, *nodeVersion, *rubyVersion, *pythonVersion, detail.LanguageVersion)
	if err != nil {
		return err
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
	if err := generate.Write(root, cfg, *force); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "Generated .devcontainer for %s (%s).\n", cfg.ProjectName, cfg.Preset)
	return err
}

func resolvePreset(value string, detected detect.Result) (config.Preset, error) {
	if value != "" {
		return config.ParsePreset(value)
	}
	switch len(detected.Presets) {
	case 0:
		return "", fmt.Errorf("could not infer a preset; pass --preset node, rails, or python")
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
	if preset == config.PresetRails {
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
	if preset != config.PresetRails && ruby != "" {
		return "", fmt.Errorf("--ruby-version requires --preset rails")
	}
	if preset != config.PresetPython && python != "" {
		return "", fmt.Errorf("--python-version requires --preset python")
	}
	switch preset {
	case config.PresetNode:
		if node != "" {
			return node, nil
		}
	case config.PresetRails:
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
