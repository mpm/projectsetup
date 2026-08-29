package detect

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var gemfileRubyVersion = regexp.MustCompile(`(?m)^\s*ruby\s*(?:\(\s*)?["'](?:ruby-)?(v?[0-9]+(?:\.[0-9]+){1,2}(?:[-+][0-9A-Za-z.-]+)?)["']`)

func detectRuby(root string) (PresetResult, []string, bool, error) {
	signals, err := existingFiles(root, "Gemfile", "Gemfile.lock", ".ruby-version")
	if err != nil {
		return PresetResult{}, nil, false, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return PresetResult{}, nil, false, fmt.Errorf("inspect Ruby project files: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".gemspec") {
			signals = append(signals, entry.Name())
		}
	}
	if len(signals) == 0 {
		return PresetResult{}, nil, false, nil
	}

	result := PresetResult{Signals: signals}
	if version, found, err := detectRubyVersion(root); err != nil {
		return PresetResult{}, nil, false, err
	} else if found {
		result.LanguageVersion = version
	}
	return result, nil, true, nil
}

func detectRubyVersion(root string) (string, bool, error) {
	if version, found, err := versionFile(root, ".ruby-version"); err != nil || found {
		return version, found, err
	}
	gemfile, found, err := readOptional(root, "Gemfile")
	if err != nil || !found {
		return "", false, err
	}
	match := gemfileRubyVersion.FindSubmatch(gemfile)
	if len(match) != 2 {
		return "", false, nil
	}
	return cleanVersion(string(match[1])), true, nil
}
