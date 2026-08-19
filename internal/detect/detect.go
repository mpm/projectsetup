package detect

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mpm/projectsetup/internal/config"
)

type PresetResult struct {
	Signals                  []string
	LanguageVersion          string
	PackageManagerCandidates []config.PackageManager
	SuggestedDatabase        config.Database
}

type Result struct {
	Presets  []config.Preset
	Details  map[config.Preset]PresetResult
	Warnings []string
}

func (r Result) AmbiguousPreset() bool {
	return len(r.Presets) > 1
}

func (p PresetResult) AmbiguousPackageManager() bool {
	return len(p.PackageManagerCandidates) > 1
}

func Detect(root string) (Result, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Result{}, fmt.Errorf("resolve project root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return Result{}, fmt.Errorf("inspect project root %q: %w", root, err)
	}
	if !info.IsDir() {
		return Result{}, fmt.Errorf("project root %q is not a directory", root)
	}

	result := Result{Details: make(map[config.Preset]PresetResult)}
	detectors := []struct {
		preset config.Preset
		fn     func(string) (PresetResult, []string, bool, error)
	}{
		{config.PresetNode, detectNode},
		{config.PresetRails, detectRails},
		{config.PresetPython, detectPython},
	}
	for _, detector := range detectors {
		detail, warnings, found, err := detector.fn(root)
		if err != nil {
			return Result{}, err
		}
		result.Warnings = append(result.Warnings, warnings...)
		if found {
			result.Presets = append(result.Presets, detector.preset)
			result.Details[detector.preset] = detail
		}
	}
	sort.Slice(result.Presets, func(i, j int) bool { return result.Presets[i] < result.Presets[j] })
	sort.Strings(result.Warnings)
	return result, nil
}

func existingFiles(root string, names ...string) ([]string, error) {
	var result []string
	for _, name := range names {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(name)))
		if err == nil {
			if !info.IsDir() {
				result = append(result, name)
			}
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect %s: %w", name, err)
		}
	}
	return result, nil
}

func readOptional(root, name string) ([]byte, bool, error) {
	path := filepath.Join(root, filepath.FromSlash(name))
	data, err := os.ReadFile(path)
	if err == nil {
		return data, true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("read %s: %w", name, err)
}

func versionFile(root, name string) (string, bool, error) {
	data, found, err := readOptional(root, name)
	if err != nil || !found {
		return "", found, err
	}
	version := cleanVersion(string(data))
	return version, version != "", nil
}

func cleanVersion(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "v")
	value = strings.TrimPrefix(value, "ruby-")
	fields := strings.Fields(value)
	if len(fields) > 0 {
		return fields[0]
	}
	return ""
}

func sortedManagers(values []config.PackageManager) []config.PackageManager {
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return values
}
