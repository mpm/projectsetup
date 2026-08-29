package detect

import (
	"fmt"
	"os"
	"strings"
)

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
	if version, found, err := versionFile(root, ".ruby-version"); err != nil {
		return PresetResult{}, nil, false, err
	} else if found {
		result.LanguageVersion = version
	}
	return result, nil, true, nil
}
