package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/mpm/projectsetup/internal/presets"
)

const presetUsage = `Usage:
  projectsetup preset list [--json]
  projectsetup preset show NAME
  projectsetup preset validate FILE
  projectsetup preset eject NAME --as NEW
`

func runPreset(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		_, err := io.WriteString(stderr, presetUsage)
		if err != nil {
			return err
		}
		return errors.New("preset requires a subcommand")
	}
	switch args[0] {
	case "list":
		return runPresetList(args[1:], stdout, stderr)
	case "show":
		return runPresetShow(args[1:], stdout, stderr)
	case "validate":
		return runPresetValidate(args[1:], stdout, stderr)
	case "eject":
		return runPresetEject(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		_, err := io.WriteString(stdout, presetUsage)
		return err
	default:
		return fmt.Errorf("unknown preset subcommand %q; run projectsetup preset --help for usage", args[0])
	}
}

// parsePresetFlags parses flags that may follow the positional arguments
// and returns the positional arguments.
func parsePresetFlags(flags *flag.FlagSet, args []string, positional int) ([]string, error) {
	flags.SetOutput(io.Discard)
	var names []string
	for {
		if err := flags.Parse(args); err != nil {
			return nil, err
		}
		args = flags.Args()
		if len(args) == 0 {
			break
		}
		names = append(names, args[0])
		args = args[1:]
	}
	if len(names) != positional {
		return nil, fmt.Errorf("%s expects %d argument(s), got %d", flags.Name(), positional, len(names))
	}
	return names, nil
}

func runPresetList(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("preset list", flag.ContinueOnError)
	asJSON := flags.Bool("json", false, "print the listing as JSON")
	if _, err := parsePresetFlags(flags, args, 0); err != nil {
		return err
	}
	registry, err := presets.Load()
	if err != nil {
		return fmt.Errorf("load preset definitions: %w", err)
	}
	infos := registry.Infos()
	if *asJSON {
		data, err := json.MarshalIndent(infos, "", "  ")
		if err != nil {
			return fmt.Errorf("encode definitions: %w", err)
		}
		_, err = stdout.Write(append(data, '\n'))
		return err
	}
	writer := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "NAME\tKIND\tVERSION\tSOURCE\tDESCRIPTION")
	for _, info := range infos {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n", info.Name, info.Kind, info.Version, info.Source, info.Description)
	}
	return writer.Flush()
}

func runPresetShow(args []string, stdout, stderr io.Writer) error {
	names, err := parsePresetFlags(flag.NewFlagSet("preset show", flag.ContinueOnError), args, 1)
	if err != nil {
		return err
	}
	registry, err := presets.Load()
	if err != nil {
		return fmt.Errorf("load preset definitions: %w", err)
	}
	definition, ok := registry.Lookup(names[0])
	if !ok {
		return fmt.Errorf("unknown definition %q; run projectsetup preset list", names[0])
	}
	_, err = stdout.Write(definition.Raw)
	return err
}

func runPresetValidate(args []string, stdout, stderr io.Writer) error {
	paths, err := parsePresetFlags(flag.NewFlagSet("preset validate", flag.ContinueOnError), args, 1)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		return fmt.Errorf("read definition: %w", err)
	}
	definition, err := presets.Parse(data, paths[0])
	if err != nil {
		return err
	}
	// A preset must also resolve on its own with default options, which
	// checks rules that only hold for merged results.
	if definition.Kind == presets.KindPreset {
		registry, err := presets.NewRegistry(definition)
		if err != nil {
			return err
		}
		if _, err := registry.Resolve(presets.Selection{Preset: definition.Name, Project: presets.Project{Name: "example", Home: "/home/vscode", Workspace: "/workspaces/example"}}); err != nil {
			return fmt.Errorf("%s: resolve with default options: %w", paths[0], err)
		}
	}
	if want := strings.TrimSuffix(filepath.Base(paths[0]), ".toml"); want != definition.Name {
		fmt.Fprintf(stderr, "warning: %s: name %q does not match the file name; save it as %s.toml in the user definition directory\n", paths[0], definition.Name, definition.Name)
	}
	if _, ok := presets.Builtin().Lookup(definition.Name); ok {
		fmt.Fprintf(stderr, "warning: %s: %q is a built-in definition name; user definitions must use another name\n", paths[0], definition.Name)
	}
	_, err = fmt.Fprintf(stdout, "%s: valid %s %s %s\n", paths[0], definition.Kind, definition.Name, definition.Version)
	return err
}

var definitionName = regexp.MustCompile(`(?m)^name\s*=\s*"([^"]*)"[ \t]*(#.*)?$`)

func runPresetEject(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("preset eject", flag.ContinueOnError)
	newName := flags.String("as", "", "name of the user definition to create")
	names, err := parsePresetFlags(flags, args, 1)
	if err != nil {
		return err
	}
	if *newName == "" {
		return errors.New("preset eject requires --as NEW")
	}
	registry, err := presets.Load()
	if err != nil {
		return fmt.Errorf("load preset definitions: %w", err)
	}
	definition, ok := registry.Lookup(names[0])
	if !ok {
		return fmt.Errorf("unknown definition %q; run projectsetup preset list", names[0])
	}
	if _, exists := registry.Lookup(*newName); exists {
		return fmt.Errorf("definition %q already exists; choose another name", *newName)
	}
	matches := definitionName.FindAllSubmatchIndex(definition.Raw, -1)
	if len(matches) != 1 {
		return fmt.Errorf("cannot find the name line of definition %q", definition.Name)
	}
	start, end := matches[0][2], matches[0][3]
	data := append(append(append([]byte{}, definition.Raw[:start]...), *newName...), definition.Raw[end:]...)

	dir, err := presets.UserDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, *newName+".toml")
	if _, err := presets.Parse(data, path); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create user definition directory: %w", err)
	}
	if err := writeNewFile(path, data); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "Copied %s to %s. Edit it, then select it with --preset or --addon %s.\n", definition.Name, path, *newName)
	return err
}

// writeNewFile writes data to path through a temporary file and refuses to
// replace an existing file.
func writeNewFile(path string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".projectsetup-*.toml")
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chmod(temporary.Name(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	// Link fails when path exists, unlike Rename.
	if err := os.Link(temporary.Name(), path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s already exists; remove it or choose another name", path)
		}
		return fmt.Errorf("install %s: %w", path, err)
	}
	return nil
}
