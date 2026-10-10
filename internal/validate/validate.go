package validate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/detect"
	"github.com/mpm/projectsetup/internal/presets"
	yaml "go.yaml.in/yaml/v3"
)

type Severity string

const (
	Error   Severity = "error"
	Warning Severity = "warning"
)

type Diagnostic struct {
	Severity Severity
	Path     string
	Message  string
}

type Options struct {
	DevcontainerDir string
	CheckHostMounts bool
	External        bool
	Build           bool
	Runner          Runner
}

type Runner interface {
	LookPath(string) (string, error)
	Run(string, ...string) ([]byte, error)
}

type commandRunner struct{}

func (commandRunner) LookPath(name string) (string, error) { return exec.LookPath(name) }
func (commandRunner) Run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

type devcontainerDocument struct {
	Name              string                    `json:"name"`
	DockerComposeFile string                    `json:"dockerComposeFile"`
	Service           string                    `json:"service"`
	WorkspaceFolder   string                    `json:"workspaceFolder"`
	ContainerUser     string                    `json:"containerUser"`
	RemoteUser        string                    `json:"remoteUser"`
	ContainerEnv      map[string]string         `json:"containerEnv"`
	Features          map[string]map[string]any `json:"features"`
	Mounts            []string                  `json:"mounts"`
	ForwardPorts      []int                     `json:"forwardPorts"`
	PostCreateCommand string                    `json:"postCreateCommand"`
}

func Check(root string, options Options) []Diagnostic {
	root, err := filepath.Abs(root)
	if err != nil {
		return []Diagnostic{{Error, ".", fmt.Sprintf("resolve project root: %v", err)}}
	}
	devDir := options.DevcontainerDir
	if devDir == "" {
		devDir = filepath.Join(root, ".devcontainer")
	}
	if options.Runner == nil {
		options.Runner = commandRunner{}
	}

	var diagnostics []Diagnostic
	add := func(severity Severity, path, format string, args ...any) {
		diagnostics = append(diagnostics, Diagnostic{severity, path, fmt.Sprintf(format, args...)})
	}

	manifestPath := filepath.Join(devDir, "projectsetup.json")
	manifestFile, err := os.Open(manifestPath)
	var manifest config.Manifest
	var cfg config.Config
	var resolved presets.Resolved
	manifestValid := false
	if err != nil {
		add(Error, relative(root, manifestPath), "required manifest is not readable: %v", err)
	} else {
		manifest, err = config.ReadManifest(manifestFile)
		manifestFile.Close()
		if err != nil {
			add(Error, relative(root, manifestPath), "%v", err)
		} else {
			cfg, manifestValid = validateManifest(root, devDir, manifest, add)
		}
	}
	if manifestValid {
		if resolved, err = config.Resolve(cfg); err != nil {
			add(Error, relative(root, manifestPath), "resolve preset definitions: %v", err)
			manifestValid = false
		}
	}

	required := []string{"Dockerfile", "compose.yaml", "devcontainer.json", "scripts/install-ai-tools.sh", "scripts/post-create.sh"}
	for _, name := range required {
		path := filepath.Join(devDir, filepath.FromSlash(name))
		info, err := os.Stat(path)
		if err != nil {
			add(Error, relative(root, path), "required generated file is missing or unreadable: %v", err)
			continue
		}
		if !info.Mode().IsRegular() {
			add(Error, relative(root, path), "required generated path is not a regular file")
		}
	}
	for _, name := range []string{"scripts/install-ai-tools.sh", "scripts/post-create.sh"} {
		path := filepath.Join(devDir, filepath.FromSlash(name))
		if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0o111 == 0 {
			add(Error, relative(root, path), "generated script is not executable")
		}
	}

	var document devcontainerDocument
	devcontainerPath := filepath.Join(devDir, "devcontainer.json")
	data, err := os.ReadFile(devcontainerPath)
	if err == nil {
		if err := json.Unmarshal(data, &document); err != nil {
			add(Error, relative(root, devcontainerPath), "parse JSON: %v", err)
		} else if manifestValid {
			validateDevcontainer(root, devDir, manifest, resolved, document, options.CheckHostMounts, add)
		}
	}

	if manifestValid {
		validateDockerfile(root, devDir, cfg, resolved, add)
		validateProjectConventions(root, cfg, add)
		validateCompose(root, devDir, cfg, resolved, add)
	}
	if options.External {
		build := options.Build && ErrorCount(diagnostics) == 0
		validateExternal(root, devDir, manifestValid, build, options, add)
	}

	sort.SliceStable(diagnostics, func(i, j int) bool {
		if diagnostics[i].Severity != diagnostics[j].Severity {
			return diagnostics[i].Severity < diagnostics[j].Severity
		}
		if diagnostics[i].Path != diagnostics[j].Path {
			return diagnostics[i].Path < diagnostics[j].Path
		}
		return diagnostics[i].Message < diagnostics[j].Message
	})
	return diagnostics
}

// validateManifest returns the normalized configuration and whether the
// manifest is valid. Schema 1 manifests use the built-in definitions; later
// schemas use the definition copies next to the manifest.
func validateManifest(root, devDir string, manifest config.Manifest, add func(Severity, string, string, ...any)) (config.Config, bool) {
	path := ".devcontainer/projectsetup.json"
	valid := true
	fail := func(format string, args ...any) { valid = false; add(Error, path, format, args...) }
	if manifest.GeneratedBy != config.GeneratedBy {
		fail("generatedBy must be %q", config.GeneratedBy)
	}
	if err := config.ValidateProjectName(manifest.ProjectName); err != nil {
		fail("projectName: %v; regenerate with projectsetup init --force --name NAME", err)
	}
	if manifest.Preset.Name == "" {
		fail("preset is required")
	}
	if manifest.Addons == nil {
		fail("addons is required; use an empty array when no add-ons are selected")
	}
	if manifest.Options == nil {
		fail("options is required; use an empty object when no definition has options")
	}
	if manifest.AITools == nil {
		fail("aiTools is required; use an empty array to disable AI tools")
	}
	if manifest.Ports == nil {
		fail("ports is required; use an empty array when no ports are configured")
	}
	if manifest.SystemPackages == nil {
		fail("systemPackages is required; use an empty array when no packages are configured")
	}
	seenTools := map[config.AITool]bool{}
	for _, tool := range manifest.AITools {
		if !tool.Valid() {
			fail("unsupported AI tool %q", tool)
		}
		if seenTools[tool] {
			fail("AI tool %q is duplicated", tool)
		}
		seenTools[tool] = true
	}
	for _, port := range manifest.Ports {
		if port < 1 || port > 65535 {
			fail("port %d is outside the valid range 1-65535", port)
		} else if port < 1024 || port > 20000 {
			add(Warning, path, "port %d is outside dworm's scanned range 1024-20000", port)
		}
	}
	if !valid {
		return config.Config{}, false
	}
	registry := presets.Builtin()
	if manifest.SchemaVersion != 1 {
		var err error
		presetsDir := filepath.Join(devDir, config.PresetsDir)
		if registry, err = presets.LoadProject(presetsDir, manifest.Refs()); err != nil {
			add(Error, relative(root, presetsDir), "load recorded definitions: %v", err)
			return config.Config{}, false
		}
	}
	cfg, err := config.Normalize(manifest.Input(root, registry))
	if err != nil {
		fail("manifest values are invalid: %v", err)
		return config.Config{}, false
	}
	if !config.SameSelection(config.NewManifest(cfg), manifest) {
		fail("manifest values are not normalized; every option of the selected definitions must be recorded; regenerate with projectsetup init --force")
		return config.Config{}, false
	}
	return cfg, true
}

func validateDevcontainer(root, devDir string, manifest config.Manifest, resolved presets.Resolved, document devcontainerDocument, checkHost bool, add func(Severity, string, string, ...any)) {
	path := relative(root, filepath.Join(devDir, "devcontainer.json"))
	wantWorkspace := "/workspaces/" + manifest.ProjectName
	if document.Name != manifest.ProjectName {
		add(Error, path, "name is %q; expected project name %q", document.Name, manifest.ProjectName)
	}
	if document.WorkspaceFolder != wantWorkspace {
		add(Error, path, "workspaceFolder is %q; expected %q", document.WorkspaceFolder, wantWorkspace)
	}
	if document.ContainerUser != "vscode" || document.RemoteUser != "vscode" {
		add(Error, path, "containerUser and remoteUser must both be %q", "vscode")
	}
	if document.DockerComposeFile != "compose.yaml" || document.Service != "app" {
		add(Error, path, "configuration must use compose.yaml service app")
	}
	for _, key := range sortedKeys(resolved.Env) {
		if document.ContainerEnv[key] != resolved.Env[key] {
			add(Error, path, "containerEnv.%s is %q; expected %q", key, document.ContainerEnv[key], resolved.Env[key])
		}
	}
	if document.PostCreateCommand != ".devcontainer/scripts/post-create.sh" {
		add(Error, path, "postCreateCommand must run .devcontainer/scripts/post-create.sh")
	}
	if !equalInts(document.ForwardPorts, manifest.Ports) {
		add(Error, path, "forwardPorts %v do not match manifest ports %v", document.ForwardPorts, manifest.Ports)
	}
	expectedFeatures := resolved.DevcontainerFeatures()
	for _, id := range sortedKeys(expectedFeatures) {
		actual, ok := document.Features[id]
		if !ok {
			add(Error, path, "required feature %q is missing", id)
			continue
		}
		for _, key := range sortedKeys(expectedFeatures[id]) {
			want := jsonValue(expectedFeatures[id][key])
			if !reflect.DeepEqual(actual[key], want) {
				add(Error, path, "feature %q option %s is %s; expected %s", id, key, jsonText(actual[key]), jsonText(want))
			}
		}
	}
	for _, id := range sortedKeys(document.Features) {
		if tool := resolved.PreinstalledInstaller(id); tool != "" {
			add(Error, path, "feature %q reinstalls preinstalled tool %q; remove its installer when consuming the image", id, tool)
		}
	}
	for _, expected := range expectedAIMounts(manifest.AITools) {
		if !containsString(document.Mounts, expected) {
			add(Error, path, "selected AI tool mount %q is missing", expected)
		}
	}
	if containsTool(manifest.AITools, config.AIToolClaude) && document.ContainerEnv["CLAUDE_CONFIG_DIR"] != "/home/vscode/.claude" {
		add(Error, path, "CLAUDE_CONFIG_DIR must be /home/vscode/.claude when Claude is selected")
	}
	if containsTool(manifest.AITools, config.AIToolCodex) && document.ContainerEnv["CODEX_HOME"] != "/home/vscode/.codex" {
		add(Error, path, "CODEX_HOME must be /home/vscode/.codex when Codex is selected")
	}
	for _, mount := range document.Mounts {
		lower := strings.ToLower(mount)
		if strings.Contains(lower, "/.ssh") || strings.Contains(lower, "/.gitconfig") {
			add(Error, path, "mount %q conflicts with dworm credential forwarding", mount)
		}
	}
	containerPath := strings.Split(document.ContainerEnv["PATH"], ":")
	if resolved.Preinstalled != nil && len(resolved.Preinstalled.Tools) > 0 {
		if want := config.ContainerPath("/home/vscode", resolved); document.ContainerEnv["PATH"] != want {
			add(Error, path, "containerEnv.PATH must match the ordered, deduplicated image tool and definition paths; expected %q", want)
		}
	}
	for _, required := range append([]string{"/home/vscode/.local/bin", "/usr/bin", "/bin"}, resolved.Path...) {
		if !containsString(containerPath, required) {
			add(Error, path, "containerEnv.PATH must include %s", required)
		}
	}
	if checkHost {
		home, err := os.UserHomeDir()
		if err != nil {
			add(Error, path, "locate host home directory for bind mounts: %v", err)
		} else {
			if containsTool(manifest.AITools, config.AIToolCodex) {
				hostPath, err := config.AIHostDirectory(".codex", home, os.Getenv)
				if err != nil {
					add(Error, path, "resolve Codex host mount: %v", err)
				} else if info, err := os.Stat(hostPath); err != nil || !info.IsDir() {
					add(Error, path, "host bind-mount source %q is not an existing directory", hostPath)
				}
			}
			for _, mount := range document.Mounts {
				source := mountField(mount, "source")
				if strings.HasPrefix(source, "${localEnv:HOME}/") {
					hostPath := filepath.Join(home, filepath.FromSlash(strings.TrimPrefix(source, "${localEnv:HOME}/")))
					if info, err := os.Stat(hostPath); err != nil || !info.IsDir() {
						add(Error, path, "host bind-mount source %q is not an existing directory", hostPath)
					}
				}
			}
		}
	}
}

func validateDockerfile(root, devDir string, cfg config.Config, resolved presets.Resolved, add func(Severity, string, string, ...any)) {
	path := filepath.Join(devDir, "Dockerfile")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	text := string(data)
	finalUser := ""
	for _, line := range strings.Split(text, "\n") {
		if user, ok := strings.CutPrefix(strings.TrimSpace(line), "USER "); ok {
			finalUser = strings.TrimSpace(user)
		}
	}
	if finalUser != "vscode" {
		add(Error, relative(root, path), "final effective Dockerfile user must be vscode")
	}
	if slices.Contains(resolved.CoreAptPackages(), "bash") && !strings.Contains(text, "bash") {
		add(Error, relative(root, path), "image must provide /bin/bash")
	}
	packages := strings.Fields(text)
	for _, pkg := range resolved.CoreAptPackages() {
		if !containsString(packages, pkg) {
			add(Error, relative(root, path), "core apt package %q is missing and is not declared preinstalled", pkg)
		}
	}
	for _, group := range resolved.Apt {
		for _, pkg := range group {
			if !containsString(packages, pkg) {
				add(Error, relative(root, path), "apt package %q required by the selected definitions is missing", pkg)
			}
		}
	}
	for _, pkg := range cfg.SystemPackages {
		if !containsString(packages, pkg) {
			add(Error, relative(root, path), "explicit system package %q is missing", pkg)
		}
	}
}

// validateProjectConventions compares the configured preset options with
// the values the preset's detection rules read from the project.
func validateProjectConventions(root string, cfg config.Config, add func(Severity, string, string, ...any)) {
	const manifestPath = ".devcontainer/projectsetup.json"
	registry, err := presets.NewRegistry(cfg.Definitions...)
	if err != nil {
		add(Error, manifestPath, "load selected definitions: %v", err)
		return
	}
	detected, err := detect.Detect(root, registry)
	if err != nil {
		add(Error, ".", "detect project conventions: %v", err)
		return
	}
	detail, found := detected.Details[cfg.Preset]
	if !found {
		return
	}
	definition := cfg.Definitions[0]
	for _, name := range sortedKeys(detail.Options) {
		values := detail.Options[name]
		configured := cfg.Options[cfg.Preset][name]
		field := "options." + cfg.Preset + "." + name
		if len(definition.Options[name].Detect.Sources) > 0 {
			if !languageVersionsAgree(configured, values[0]) {
				add(Error, manifestPath, "%s is %q but the project specifies %q", field, configured, values[0])
			}
			continue
		}
		switch {
		case len(values) > 1 && slices.Contains(values, configured):
			add(Warning, ".", "project files match several values for %s (%s); configured value is %q", field, strings.Join(values, ", "), configured)
		case len(values) > 1:
			add(Error, manifestPath, "%s is %q but project files match %s", field, configured, strings.Join(values, ", "))
		case values[0] != configured:
			add(Error, manifestPath, "%s is %q but project files match %q", field, configured, values[0])
		}
	}
	for _, addon := range detail.SuggestedAddons {
		if !slices.Contains(cfg.Addons, addon) {
			add(Warning, manifestPath, "project configuration suggests add-on %q, which is not selected", addon)
		}
	}
	for _, warning := range detected.Warnings {
		add(Warning, ".", "%s", warning)
	}
}

// languageVersionsAgree accepts a configured release line for a detected
// patch version and the reverse.
func languageVersionsAgree(configured, detected string) bool {
	return configured == detected ||
		strings.HasPrefix(configured, detected+".") ||
		strings.HasPrefix(detected, configured+".")
}

type composeDocument struct {
	Name     string                    `yaml:"name"`
	Services map[string]composeService `yaml:"services"`
	Volumes  map[string]any            `yaml:"volumes"`
}

type composeService struct {
	Image string `yaml:"image"`
	Build struct {
		Context    string `yaml:"context"`
		Dockerfile string `yaml:"dockerfile"`
	} `yaml:"build"`
	Environment map[string]string `yaml:"environment"`
	Healthcheck struct {
		Test []string `yaml:"test"`
	} `yaml:"healthcheck"`
	// Volumes holds short-syntax strings and long-syntax mappings.
	Volumes   []any `yaml:"volumes"`
	DependsOn map[string]struct {
		Condition string `yaml:"condition"`
	} `yaml:"depends_on"`
}

// validateCompose parses compose.yaml rather than matching text, so files
// written by earlier releases with different formatting still pass.
func validateCompose(root, devDir string, cfg config.Config, resolved presets.Resolved, add func(Severity, string, string, ...any)) {
	path := filepath.Join(devDir, "compose.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	file := relative(root, path)
	var document composeDocument
	if err := yaml.Unmarshal(data, &document); err != nil {
		add(Error, file, "parse YAML: %v", err)
		return
	}
	if document.Name != cfg.Container.ComposeProjectName {
		add(Error, file, "Compose project name is %q; expected %q", document.Name, cfg.Container.ComposeProjectName)
	}
	appName := cfg.Container.ServiceName
	app, ok := document.Services[appName]
	if !ok {
		add(Error, file, "Compose service %q is missing", appName)
	} else {
		if app.Build.Context != ".." || app.Build.Dockerfile != ".devcontainer/Dockerfile" {
			add(Error, file, "service %s must build context .. with dockerfile .devcontainer/Dockerfile", appName)
		}
		if workspace := "..:" + cfg.Workspace.ContainerPath; !containsVolume(app.Volumes, workspace) {
			add(Error, file, "service %s must mount the project with volume %q", appName, workspace)
		}
		if containsTool(cfg.AITools, config.AIToolCodex) && !containsBind(app.Volumes, config.CodexStateSource, cfg.Container.Home+"/.codex") {
			add(Error, file, "service %s must bind-mount %q to %s/.codex when Codex is selected", appName, config.CodexStateSource, cfg.Container.Home)
		}
	}
	for _, name := range sortedKeys(document.Services) {
		if _, ok := resolved.Services[name]; !ok && name != appName {
			add(Error, file, "Compose service %q is not provided by the selected definitions", name)
		}
	}
	for _, name := range sortedKeys(resolved.Services) {
		want := resolved.Services[name]
		actual, ok := document.Services[name]
		if !ok {
			add(Error, file, "Compose service %q required by the selected definitions is missing", name)
			continue
		}
		if actual.Image != want.Image {
			add(Error, file, "service %s image is %q; expected %q", name, actual.Image, want.Image)
		}
		for _, key := range sortedKeys(want.Environment) {
			if actual.Environment[key] != want.Environment[key] {
				add(Error, file, "service %s environment %s is %q; expected %q", name, key, actual.Environment[key], want.Environment[key])
			}
		}
		if want.Healthcheck != nil && !slices.Equal(actual.Healthcheck.Test, want.Healthcheck.Test) {
			add(Error, file, "service %s healthcheck test is %q; expected %q", name, actual.Healthcheck.Test, want.Healthcheck.Test)
		}
		for _, volume := range sortedKeys(want.Volumes) {
			if mount := volume + ":" + want.Volumes[volume]; !containsVolume(actual.Volumes, mount) {
				add(Error, file, "service %s must mount volume %q", name, mount)
			}
			if _, ok := document.Volumes[volume]; !ok {
				add(Error, file, "named volume %q is not declared", volume)
			}
		}
		if want.AppDependsOn != "" && app.DependsOn[name].Condition != want.AppDependsOn {
			add(Error, file, "service %s must depend on %s with condition %s", appName, name, want.AppDependsOn)
		}
	}
}

func containsVolume(volumes []any, want string) bool {
	for _, volume := range volumes {
		if volume == want {
			return true
		}
	}
	return false
}

func containsBind(volumes []any, source, target string) bool {
	for _, volume := range volumes {
		mount, ok := volume.(map[string]any)
		if ok && mount["type"] == "bind" && mount["source"] == source && mount["target"] == target {
			return true
		}
	}
	return false
}

// jsonValue converts a definition value to the type encoding/json decodes.
func jsonValue(value any) any {
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var result any
	if err := json.Unmarshal(data, &result); err != nil {
		return value
	}
	return result
}

func jsonText(value any) string {
	if value == nil {
		return "missing"
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func validateExternal(root, devDir string, compose, build bool, options Options, add func(Severity, string, string, ...any)) {
	if compose {
		if _, err := options.Runner.LookPath("docker"); err != nil {
			add(Warning, ".devcontainer/compose.yaml", "docker is not installed; skipped docker compose config")
		} else if output, err := options.Runner.Run("docker", "compose", "-f", filepath.Join(devDir, "compose.yaml"), "config"); err != nil {
			add(Error, ".devcontainer/compose.yaml", "docker compose config failed: %s", commandFailure(err, output))
		}
	}
	if _, err := options.Runner.LookPath("devcontainer"); err != nil {
		severity := Warning
		if build {
			severity = Error
		}
		add(severity, ".devcontainer/devcontainer.json", "devcontainer CLI is not installed; skipped configuration validation%s", map[bool]string{true: " and requested build", false: ""}[build])
		return
	}
	if output, err := options.Runner.Run("devcontainer", "read-configuration", "--workspace-folder", root); err != nil {
		add(Error, ".devcontainer/devcontainer.json", "devcontainer read-configuration failed: %s", commandFailure(err, output))
		return
	}
	if build {
		if output, err := options.Runner.Run("devcontainer", "build", "--workspace-folder", root); err != nil {
			add(Error, ".devcontainer/devcontainer.json", "devcontainer build failed: %s", commandFailure(err, output))
		}
	}
}

func ErrorCount(diagnostics []Diagnostic) int {
	count := 0
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == Error {
			count++
		}
	}
	return count
}

func relative(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

func mountField(mount, key string) string {
	for _, field := range strings.Split(mount, ",") {
		name, value, ok := strings.Cut(field, "=")
		if ok && name == key {
			return value
		}
	}
	return ""
}

func expectedAIMounts(tools []config.AITool) []string {
	var mounts []string
	for _, tool := range tools {
		switch tool {
		case config.AIToolOpenCode:
			mounts = append(mounts,
				"source=${localEnv:HOME}/.config/opencode,target=/home/vscode/.config/opencode,type=bind",
				"source=${localEnv:HOME}/.local/share/opencode,target=/home/vscode/.local/share/opencode,type=bind",
				"source=${localEnv:HOME}/.opencode,target=/home/vscode/.opencode,type=bind",
				"source=${localEnv:HOME}/.cache/opencode,target=/home/vscode/.cache/opencode,type=bind",
			)
		case config.AIToolCodex:
			mounts = append(mounts,
				"source=${localEnv:HOME}/.local/share/codex,target=/home/vscode/.local/share/codex,type=bind")
		case config.AIToolClaude:
			mounts = append(mounts,
				"source=${localEnv:HOME}/.claude,target=/home/vscode/.claude,type=bind",
				"source=${localEnv:HOME}/.local/share/claude,target=/home/vscode/.local/share/claude,type=bind",
			)
		}
	}
	return mounts
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsTool(tools []config.AITool, target config.AITool) bool {
	for _, tool := range tools {
		if tool == target {
			return true
		}
	}
	return false
}

func equalInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func commandFailure(err error, output []byte) string {
	message := strings.TrimSpace(string(bytes.TrimSpace(output)))
	if len(message) > 1000 {
		message = message[:1000] + "..."
	}
	if message == "" {
		return err.Error()
	}
	return fmt.Sprintf("%v: %s", err, message)
}
