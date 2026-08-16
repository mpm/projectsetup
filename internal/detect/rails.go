package detect

import (
	"regexp"

	"projectsetup/internal/config"
)

var railsGem = regexp.MustCompile(`(?m)^\s*gem\s+["']rails["']`)
var postgresAdapter = regexp.MustCompile(`(?m)^\s*adapter:\s*(postgresql|postgis)\s*(?:#.*)?$`)

func detectRails(root string) (PresetResult, []string, bool, error) {
	signals, err := existingFiles(root, "Gemfile", "bin/rails", "config/application.rb", ".ruby-version", "Gemfile.lock")
	if err != nil {
		return PresetResult{}, nil, false, err
	}
	gemfile, hasGemfile, err := readOptional(root, "Gemfile")
	if err != nil {
		return PresetResult{}, nil, false, err
	}
	found := contains(signals, "bin/rails") || contains(signals, "config/application.rb") || (hasGemfile && railsGem.Match(gemfile))
	if !found {
		return PresetResult{}, nil, false, nil
	}

	result := PresetResult{Signals: signals}
	if version, found, err := versionFile(root, ".ruby-version"); err != nil {
		return PresetResult{}, nil, false, err
	} else if found {
		result.LanguageVersion = version
	}
	databaseConfig, hasDatabaseConfig, err := readOptional(root, "config/database.yml")
	if err != nil {
		return PresetResult{}, nil, false, err
	}
	if hasDatabaseConfig && postgresAdapter.Match(databaseConfig) {
		result.SuggestedDatabase = config.DatabasePostgres
	}
	return result, nil, true, nil
}
