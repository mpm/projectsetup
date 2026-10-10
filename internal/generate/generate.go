package generate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/validate"
)

const directoryName = ".devcontainer"

type file struct {
	name string
	data []byte
	mode fs.FileMode
}

// Write stages and validates the complete generated tree before installing it.
func Write(root string, cfg config.Config, force bool) error {
	files, err := render(cfg)
	if err != nil {
		return err
	}

	root, err = filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve project root: %w", err)
	}
	target := filepath.Join(root, directoryName)
	if err := checkDestination(target, force); err != nil {
		return err
	}

	staging, err := os.MkdirTemp(root, ".projectsetup-")
	if err != nil {
		return fmt.Errorf("create generation staging directory: %w", err)
	}
	defer os.RemoveAll(staging)

	for _, generated := range files {
		staged := filepath.Join(staging, filepath.FromSlash(generated.name))
		if err := os.MkdirAll(filepath.Dir(staged), 0o755); err != nil {
			return fmt.Errorf("create parent directory for %s: %w", generated.name, err)
		}
		if err := os.WriteFile(staged, generated.data, generated.mode); err != nil {
			return fmt.Errorf("write generated %s: %w", generated.name, err)
		}
		if err := os.Chmod(staged, generated.mode); err != nil {
			return fmt.Errorf("set generated %s mode: %w", generated.name, err)
		}
	}
	diagnostics := validate.Check(root, validate.Options{DevcontainerDir: staging})
	if count := validate.ErrorCount(diagnostics); count > 0 {
		return fmt.Errorf("validate generated configuration: %d error(s): %s", count, diagnostics[0].Message)
	}
	if err := createHostMountDirectories(cfg); err != nil {
		return err
	}

	// Host sources now exist; reject incompatible fixed IDs/access before replacing
	// project output. The actual root is used even while files live in staging.
	resolved, err := config.Resolve(cfg)
	if err != nil {
		return err
	}
	if resolved.Ownership != nil || len(resolved.Mounts) > 0 {
		diagnostics := validate.Check(root, validate.Options{DevcontainerDir: staging, CheckHostMounts: true})
		if count := validate.ErrorCount(diagnostics); count > 0 {
			return fmt.Errorf("validate host bind access and image ownership: %d error(s): %s", count, diagnostics[0].Message)
		}
	}
	if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
		if err := os.Rename(staging, target); err != nil {
			return fmt.Errorf("install generated directory %q: %w", target, err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect destination %q: %w", target, err)
	}

	backup, err := os.MkdirTemp(root, ".projectsetup-backup-")
	if err != nil {
		return fmt.Errorf("create replacement backup: %w", err)
	}
	if err := os.Remove(backup); err != nil {
		return fmt.Errorf("prepare replacement backup: %w", err)
	}
	if err := os.Rename(target, backup); err != nil {
		return fmt.Errorf("back up existing %q: %w", target, err)
	}
	if err := os.Rename(staging, target); err != nil {
		if restoreErr := os.Rename(backup, target); restoreErr != nil {
			return fmt.Errorf("install generated directory: %w (also failed to restore previous directory: %v)", err, restoreErr)
		}
		return fmt.Errorf("install generated directory: %w", err)
	}
	if err := os.RemoveAll(backup); err != nil {
		return fmt.Errorf("remove replaced generated directory %q: %w", backup, err)
	}
	return nil
}

func render(cfg config.Config) ([]file, error) {
	resolved, err := config.Resolve(cfg)
	if err != nil {
		return nil, fmt.Errorf("resolve %s preset: %w", cfg.Preset, err)
	}
	devcontainer, err := renderDevcontainer(cfg, resolved)
	if err != nil {
		return nil, err
	}
	manifest, err := marshalJSON(config.NewManifest(cfg))
	if err != nil {
		return nil, fmt.Errorf("render projectsetup.json: %w", err)
	}
	dockerfile, err := executeTemplate("Dockerfile.tmpl", templateData(cfg, resolved))
	if err != nil {
		return nil, err
	}
	postCreate, err := executeTemplate("post-create.sh.tmpl", templateData(cfg, resolved))
	if err != nil {
		return nil, err
	}
	aiInstaller, err := templateFiles.ReadFile("templates/install-ai-tools.sh")
	if err != nil {
		return nil, fmt.Errorf("read embedded AI installer: %w", err)
	}
	compose, err := renderCompose(cfg, resolved)
	if err != nil {
		return nil, err
	}
	files := []file{
		{name: "Dockerfile", data: dockerfile, mode: 0o644},
		{name: "compose.yaml", data: compose, mode: 0o644},
		{name: "devcontainer.json", data: devcontainer, mode: 0o644},
		{name: "projectsetup.json", data: manifest, mode: 0o644},
		{name: "scripts/install-ai-tools.sh", data: aiInstaller, mode: 0o755},
		{name: "scripts/post-create.sh", data: postCreate, mode: 0o755},
	}
	// check and upgrade read these copies instead of the local registry.
	for _, definition := range cfg.Definitions {
		files = append(files, file{name: config.PresetsDir + "/" + definition.Name + ".toml", data: definition.Raw, mode: 0o644})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	return files, nil
}

func checkDestination(target string, force bool) error {
	info, err := os.Stat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect destination %q: %w", target, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("refusing to overwrite %q because it is not a directory", target)
	}
	if !force {
		return fmt.Errorf("%q already exists; pass --force to replace a projectsetup-generated directory", target)
	}
	data, err := os.ReadFile(filepath.Join(target, "projectsetup.json"))
	if err != nil {
		return fmt.Errorf("refusing to overwrite %q: cannot read projectsetup.json: %w", target, err)
	}
	var identity struct {
		GeneratedBy string `json:"generatedBy"`
	}
	if err := json.Unmarshal(data, &identity); err != nil || identity.GeneratedBy != config.GeneratedBy {
		return fmt.Errorf("refusing to overwrite %q because it is not recognized as generated by projectsetup", target)
	}
	return checkGeneratedContents(target)
}

func checkGeneratedContents(target string) error {
	allowed := map[string]bool{
		".":                           true,
		"Dockerfile":                  false,
		"compose.yaml":                false,
		"devcontainer.json":           false,
		"projectsetup.json":           false,
		"scripts":                     true,
		"scripts/install-ai-tools.sh": false,
		"scripts/post-create.sh":      false,
		config.PresetsDir:             true,
		// The Dev Container CLI pins the features it installed here when it
		// starts the container. Replacement drops it, and the next start
		// writes it again for the regenerated features.
		"devcontainer-lock.json": false,
	}
	return filepath.WalkDir(target, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("inspect existing generated content %q: %w", current, err)
		}
		relative, err := filepath.Rel(target, current)
		if err != nil {
			return fmt.Errorf("resolve existing generated path %q: %w", current, err)
		}
		relative = filepath.ToSlash(relative)
		wantDirectory, ok := allowed[relative]
		if directory, name := path.Split(relative); directory == config.PresetsDir+"/" && strings.HasSuffix(name, ".toml") {
			wantDirectory, ok = false, true
		}
		if !ok {
			return fmt.Errorf("refusing to overwrite %q because it contains unrelated path %q", target, relative)
		}
		if wantDirectory != entry.IsDir() || (!wantDirectory && !entry.Type().IsRegular()) {
			return fmt.Errorf("refusing to overwrite %q because generated path %q has an unexpected file type", target, relative)
		}
		return nil
	})
}

func createHostMountDirectories(cfg config.Config) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("locate host home directory for AI mounts: %w", err)
	}
	for _, relative := range config.AIHostDirectories(cfg.AITools) {
		path, err := config.AIHostDirectory(relative, home, os.Getenv)
		if err != nil {
			return fmt.Errorf("resolve host mount source: %w", err)
		}
		if err := os.MkdirAll(path, 0o755); err != nil {
			return fmt.Errorf("create host mount source %q: %w", path, err)
		}
	}
	return nil
}

func hostMountDirectories(tools []config.AITool) []string {
	return config.AIHostDirectories(tools)
}

func marshalJSON(value any) ([]byte, error) {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func shellWords(values []config.AITool) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = string(value)
	}
	return strings.Join(parts, " ")
}
