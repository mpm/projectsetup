package cli

import (
	"encoding/json"
	"fmt"
	"io"
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
	fmt.Fprintf(&text, "Presets: %s\n", joinChoices(options.Presets, ", "))
	text.WriteString("Package managers (--package-manager):\n")
	for _, preset := range options.Presets {
		managers := options.PackageManagers[preset]
		values := []string{"none"}
		if len(managers) > 0 {
			values = choiceStrings(managers)
			if manager := options.Defaults[preset].PackageManager; manager != nil {
				markDefault(values, string(*manager))
			}
		}
		fmt.Fprintf(&text, "  %s: %s\n", preset, strings.Join(values, ", "))
	}
	text.WriteString("Default language versions:\n")
	for _, preset := range options.Presets {
		fmt.Fprintf(&text, "  %s: %s\n", preset, options.Defaults[preset].LanguageVersion)
	}
	if len(options.Presets) > 0 {
		defaults := options.Defaults[options.Presets[0]]
		databases := choiceStrings(options.Databases)
		markDefault(databases, string(defaults.Database))
		fmt.Fprintf(&text, "Databases (--database): %s\n", strings.Join(databases, ", "))
		fmt.Fprintf(&text, "AI tools (--ai, comma-separated or none): %s; default %s\n", joinChoices(options.AITools, ", "), joinChoices(defaults.AITools, ","))
	}
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
