package detect

import (
	"regexp"

	"projectsetup/internal/config"
)

var requiresPython = regexp.MustCompile(`(?m)^\s*requires-python\s*=\s*["'][^"']*?(\d+(?:\.\d+){1,2})`)
var poetryMetadata = regexp.MustCompile(`(?m)^\s*\[tool\.poetry\]\s*$`)

func detectPython(root string) (PresetResult, []string, bool, error) {
	signals, err := existingFiles(root, "pyproject.toml", "requirements.txt", "requirements-dev.txt", "poetry.lock", "uv.lock", "Pipfile", ".python-version")
	if err != nil {
		return PresetResult{}, nil, false, err
	}
	if len(signals) == 0 {
		return PresetResult{}, nil, false, nil
	}

	result := PresetResult{Signals: signals}
	if version, found, err := versionFile(root, ".python-version"); err != nil {
		return PresetResult{}, nil, false, err
	} else if found {
		result.LanguageVersion = version
	} else if pyproject, found, err := readOptional(root, "pyproject.toml"); err != nil {
		return PresetResult{}, nil, false, err
	} else if found {
		match := requiresPython.FindSubmatch(pyproject)
		if len(match) == 2 {
			result.LanguageVersion = string(match[1])
		}
	}

	if contains(signals, "uv.lock") {
		result.PackageManagerCandidates = append(result.PackageManagerCandidates, config.PackageManagerUV)
	}
	if contains(signals, "poetry.lock") {
		result.PackageManagerCandidates = append(result.PackageManagerCandidates, config.PackageManagerPoetry)
	} else if pyproject, found, err := readOptional(root, "pyproject.toml"); err != nil {
		return PresetResult{}, nil, false, err
	} else if found && poetryMetadata.Match(pyproject) {
		result.PackageManagerCandidates = append(result.PackageManagerCandidates, config.PackageManagerPoetry)
	}
	if contains(signals, "requirements.txt") || contains(signals, "requirements-dev.txt") {
		result.PackageManagerCandidates = append(result.PackageManagerCandidates, config.PackageManagerPip)
	}
	result.PackageManagerCandidates = sortedManagers(result.PackageManagerCandidates)

	var warnings []string
	if contains(signals, "Pipfile") {
		warnings = append(warnings, "Pipfile detected, but Pipenv is not supported; select pip, poetry, or uv")
	}
	return result, warnings, true, nil
}
