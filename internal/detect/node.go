package detect

import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/mpm/projectsetup/internal/config"
)

var nodeEngineVersion = regexp.MustCompile(`\d+(?:\.\d+){0,2}`)

type packageJSON struct {
	Engines struct {
		Node string `json:"node"`
	} `json:"engines"`
}

func detectNode(root string) (PresetResult, []string, bool, error) {
	signals, err := existingFiles(root, "package.json", ".node-version", ".nvmrc", "package-lock.json", "pnpm-lock.yaml", "yarn.lock")
	if err != nil {
		return PresetResult{}, nil, false, err
	}
	packageData, hasPackage, err := readOptional(root, "package.json")
	if err != nil {
		return PresetResult{}, nil, false, err
	}
	if !hasPackage {
		return PresetResult{}, nil, false, nil
	}

	result := PresetResult{Signals: signals}
	if version, found, err := versionFile(root, ".node-version"); err != nil {
		return PresetResult{}, nil, false, err
	} else if found {
		result.LanguageVersion = version
	} else if version, found, err := versionFile(root, ".nvmrc"); err != nil {
		return PresetResult{}, nil, false, err
	} else if found {
		result.LanguageVersion = version
	} else {
		var manifest packageJSON
		if err := json.Unmarshal(packageData, &manifest); err != nil {
			return PresetResult{}, nil, false, fmt.Errorf("parse package.json: %w", err)
		}
		result.LanguageVersion = nodeEngineVersion.FindString(manifest.Engines.Node)
	}

	for file, manager := range map[string]config.PackageManager{
		"package-lock.json": config.PackageManagerNPM,
		"pnpm-lock.yaml":    config.PackageManagerPNPM,
		"yarn.lock":         config.PackageManagerYarn,
	} {
		if contains(signals, file) {
			result.PackageManagerCandidates = append(result.PackageManagerCandidates, manager)
		}
	}
	result.PackageManagerCandidates = sortedManagers(result.PackageManagerCandidates)
	return result, nil, true, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
