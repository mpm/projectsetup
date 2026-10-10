package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/mpm/projectsetup/internal/config"
)

// writeOptions prints the values init accepts, either as indented JSON or as a
// short human-readable listing.
func writeOptions(output io.Writer, options config.Options, asJSON bool) error {
	if asJSON {
		data, err := json.MarshalIndent(options, "", "  ")
		if err != nil {
			return fmt.Errorf("encode init options: %w", err)
		}
		_, err = output.Write(append(data, '\n'))
		return err
	}

	var text strings.Builder
	fmt.Fprintf(&text, "Presets (--preset): %s\n", strings.Join(options.Presets, ", "))
	fmt.Fprintf(&text, "Add-ons (--addon, repeatable): %s\n", strings.Join(options.Addons, ", "))
	fmt.Fprintf(&text, "Databases (--database, alias for --addon): %s\n", strings.Join(config.Databases, ", "))
	text.WriteString("Options (--set [DEFINITION.]OPTION=VALUE):\n")
	for _, name := range append(slices.Clone(options.Presets), options.Addons...) {
		info := options.Definitions[name]
		for _, option := range slices.Sorted(maps.Keys(info.Options)) {
			details := info.Options[option]
			if len(details.Choices) > 0 {
				values := slices.Clone(details.Choices)
				markDefault(values, details.Default)
				fmt.Fprintf(&text, "  %s.%s: %s: %s\n", name, option, details.Description, strings.Join(values, ", "))
			} else {
				fmt.Fprintf(&text, "  %s.%s: %s; default %s\n", name, option, details.Description, details.Default)
			}
		}
	}
	text.WriteString("Aliases: --node-version, --ruby-version, --python-version, --package-manager, and --postgres-version set the matching option\n")
	fmt.Fprintf(&text, "AI tools (--ai, comma-separated or none): %s; default %s\n", joinChoices(options.AITools, ", "), joinChoices(options.DefaultAITools, ","))
	fmt.Fprintf(&text, "Project name pattern (--name): %s\n", options.ProjectNamePattern)
	_, err := io.WriteString(output, text.String())
	return err
}

func markDefault(values []string, defaultValue string) {
	for i, value := range values {
		if value == defaultValue {
			values[i] = value + " (default)"
		}
	}
}

func choiceStrings[T ~string](values []T) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = string(value)
	}
	return result
}

func joinChoices[T ~string](values []T, separator string) string {
	return strings.Join(choiceStrings(values), separator)
}
