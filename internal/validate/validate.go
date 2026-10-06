package validate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/detect"
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
	Name              string            `json:"name"`
	DockerComposeFile string            `json:"dockerComposeFile"`
	Service           string            `json:"service"`
	WorkspaceFolder   string            `json:"workspaceFolder"`
	ContainerUser     string            `json:"containerUser"`
	RemoteUser        string            `json:"remoteUser"`
	ContainerEnv      map[string]string `json:"containerEnv"`
	Features          map[string]struct {
		Version string `json:"version"`
	} `json:"features"`
	Mounts            []string `json:"mounts"`
	ForwardPorts      []int    `json:"forwardPorts"`
	PostCreateCommand string   `json:"postCreateCommand"`
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
	manifestValid := false
	if err != nil {
		add(Error, relative(root, manifestPath), "required manifest is not readable: %v", err)
	} else {
		manifest, err = config.ReadManifest(manifestFile)
		manifestFile.Close()
		if err != nil {
			add(Error, relative(root, manifestPath), "%v", err)
		} else {
			manifestValid = validateManifest(root, manifest, add)
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
			validateDevcontainer(root, devDir, manifest, document, options.CheckHostMounts, add)
		}
	}

	if manifestValid {
		validateDockerfile(root, devDir, manifest, add)
		validateProjectConventions(root, manifest, add)
		validateCompose(root, devDir, manifest, document, add)
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

func validateManifest(root string, manifest config.Manifest, add func(Severity, string, string, ...any)) bool {
	path := ".devcontainer/projectsetup.json"
	valid := true
	fail := func(format string, args ...any) { valid = false; add(Error, path, format, args...) }
	if manifest.SchemaVersion != config.SchemaVersion {
		fail("unsupported schemaVersion %d; expected %d", manifest.SchemaVersion, config.SchemaVersion)
	}
	if manifest.GeneratedBy != "projectsetup" {
		fail("generatedBy must be %q", "projectsetup")
	}
	if !manifest.Preset.Valid() {
		fail("unsupported preset %q", manifest.Preset)
	}
	if !manifest.Database.Valid() {
		fail("unsupported database %q", manifest.Database)
	}
	if err := config.ValidateProjectName(manifest.ProjectName); err != nil {
		fail("projectName: %v; regenerate with projectsetup init --force --name NAME", err)
	}
	if manifest.LanguageVersion == "" {
		fail("languageVersion is required")
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
	if manifest.Preset.Valid() && !manifest.PackageManager.Supports(manifest.Preset) {
		fail("package manager %q is not supported for preset %q", manifest.PackageManager, manifest.Preset)
	}
	for _, port := range manifest.Ports {
		if port < 1 || port > 65535 {
			fail("port %d is outside the valid range 1-65535", port)
		} else if port < 1024 || port > 20000 {
			add(Warning, path, "port %d is outside dworm's scanned range 1024-20000", port)
		}
	}
	if valid {
		_, err := config.Normalize(config.Input{Root: root, ProjectName: manifest.ProjectName, Preset: manifest.Preset, Database: manifest.Database, AITools: manifest.AITools, PackageManager: manifest.PackageManager, LanguageVersion: manifest.LanguageVersion, Ports: manifest.Ports, SystemPackages: manifest.SystemPackages})
		if err != nil {
			fail("manifest values are invalid: %v", err)
		}
	}
	return valid
}

func validateDevcontainer(root, devDir string, manifest config.Manifest, document devcontainerDocument, checkHost bool, add func(Severity, string, string, ...any)) {
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
	if manifest.Database == config.DatabasePostgres {
		for key, value := range map[string]string{"DB_HOST": "postgres", "PGHOST": "postgres", "PGUSER": "projectsetup", "PGPASSWORD": "projectsetup", "PGDATABASE": manifest.ProjectName} {
			if document.ContainerEnv[key] != value {
				add(Error, path, "containerEnv.%s is %q; expected %q", key, document.ContainerEnv[key], value)
			}
		}
	}
	if document.PostCreateCommand != ".devcontainer/scripts/post-create.sh" {
		add(Error, path, "postCreateCommand must run .devcontainer/scripts/post-create.sh")
	}
	if !equalInts(document.ForwardPorts, manifest.Ports) {
		add(Error, path, "forwardPorts %v do not match manifest ports %v", document.ForwardPorts, manifest.Ports)
	}
	switch manifest.Preset {
	case config.PresetNode:
		if document.Features["ghcr.io/devcontainers/features/node:1"].Version != manifest.LanguageVersion {
			add(Error, path, "Node feature version does not match manifest languageVersion %q", manifest.LanguageVersion)
		}
	case config.PresetRuby:
		if document.Features["ghcr.io/rails/devcontainer/features/ruby:2"].Version != manifest.LanguageVersion {
			add(Error, path, "Ruby feature version does not match manifest languageVersion %q", manifest.LanguageVersion)
		}
	case config.PresetRails:
		if document.Features["ghcr.io/rails/devcontainer/features/ruby:2"].Version != manifest.LanguageVersion {
			add(Error, path, "Rails Ruby feature version does not match manifest languageVersion %q", manifest.LanguageVersion)
		}
	case config.PresetPython:
		if document.Features["ghcr.io/devcontainers/features/python:1"].Version != manifest.LanguageVersion {
			add(Error, path, "Python feature version does not match manifest languageVersion %q", manifest.LanguageVersion)
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
	for _, required := range []string{"/home/vscode/.local/bin", "/usr/bin", "/bin"} {
		if !containsString(containerPath, required) {
			add(Error, path, "containerEnv.PATH must include %s", required)
		}
	}
	if (manifest.Preset == config.PresetRuby || manifest.Preset == config.PresetRails) && !containsString(containerPath, "/home/vscode/.local/share/mise/shims") {
		add(Error, path, "containerEnv.PATH must include /home/vscode/.local/share/mise/shims for Ruby")
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

func validateDockerfile(root, devDir string, manifest config.Manifest, add func(Severity, string, string, ...any)) {
	path := filepath.Join(devDir, "Dockerfile")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	text := string(data)
	if !strings.Contains(text, "USER vscode") || !strings.HasSuffix(strings.TrimSpace(text), "USER vscode") {
		add(Error, relative(root, path), "final effective Dockerfile user must be vscode")
	}
	if !strings.Contains(text, "bash") {
		add(Error, relative(root, path), "image must provide /bin/bash")
	}
	if manifest.Database == config.DatabaseSQLite {
		packages := strings.Fields(text)
		for _, pkg := range []string{"libsqlite3-dev", "sqlite3"} {
			if !containsString(packages, pkg) {
				add(Error, relative(root, path), "SQLite database requires apt package %q", pkg)
			}
		}
	}
}

func validateProjectConventions(root string, manifest config.Manifest, add func(Severity, string, string, ...any)) {
	detected, err := detect.Detect(root)
	if err != nil {
		add(Error, ".", "detect project conventions: %v", err)
		return
	}
	detail, found := detected.Details[manifest.Preset]
	if !found {
		return
	}
	if detail.LanguageVersion != "" && !languageVersionsAgree(manifest.LanguageVersion, detail.LanguageVersion) {
		add(Error, ".devcontainer/projectsetup.json", "languageVersion %q disagrees with detected project version %q", manifest.LanguageVersion, detail.LanguageVersion)
	}
	if len(detail.PackageManagerCandidates) > 1 {
		if containsManager(detail.PackageManagerCandidates, manifest.PackageManager) {
			add(Warning, ".", "multiple package manager lockfiles detected (%s); configured manager is %q", joinManagers(detail.PackageManagerCandidates), manifest.PackageManager)
		} else {
			add(Error, ".devcontainer/projectsetup.json", "packageManager %q does not match detected lockfile managers %s", manifest.PackageManager, joinManagers(detail.PackageManagerCandidates))
		}
	} else if len(detail.PackageManagerCandidates) == 1 && detail.PackageManagerCandidates[0] != manifest.PackageManager {
		add(Error, ".devcontainer/projectsetup.json", "packageManager %q disagrees with detected lockfile manager %q", manifest.PackageManager, detail.PackageManagerCandidates[0])
	}
	if detail.SuggestedDatabase == config.DatabasePostgres && manifest.Database != config.DatabasePostgres {
		add(Warning, ".devcontainer/projectsetup.json", "project configuration appears to require PostgreSQL, but database is %q", manifest.Database)
	}
	for _, warning := range detected.Warnings {
		add(Warning, ".", "%s", warning)
	}
}

func languageVersionsAgree(configured, detected string) bool {
	return configured == detected ||
		strings.HasPrefix(configured, detected+".") ||
		strings.HasPrefix(detected, configured+".")
}

func validateCompose(root, devDir string, manifest config.Manifest, document devcontainerDocument, add func(Severity, string, string, ...any)) {
	path := filepath.Join(devDir, "compose.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	text := string(data)
	wantName := "name: " + manifest.ProjectName
	if !containsString(strings.Split(text, "\n"), wantName) {
		add(Error, relative(root, path), "missing expected Compose configuration %q", wantName)
	}
	checks := []string{"services:", "  " + document.Service + ":", "context: ..", "dockerfile: .devcontainer/Dockerfile", "- ..:/workspaces/" + manifest.ProjectName}
	if manifest.Database == config.DatabasePostgres {
		checks = append(checks, "  postgres:", "POSTGRES_USER: projectsetup", "POSTGRES_PASSWORD: projectsetup", "POSTGRES_DB: '"+manifest.ProjectName+"'", "condition: service_healthy", "postgres-data:/var/lib/postgresql/data")
	} else if strings.Contains(text, "  postgres:") {
		add(Error, relative(root, path), "PostgreSQL service is configured but database is %q", manifest.Database)
	}
	if containsTool(manifest.AITools, config.AIToolCodex) {
		checks = append(checks, "      - type: bind\n        source: \""+config.CodexStateSource+"\"\n        target: /home/vscode/.codex")
	}
	for _, expected := range checks {
		if !strings.Contains(text, expected) {
			add(Error, relative(root, path), "missing expected Compose configuration %q", expected)
		}
	}
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

func joinManagers(managers []config.PackageManager) string {
	values := make([]string, len(managers))
	for i, manager := range managers {
		values[i] = string(manager)
	}
	return strings.Join(values, ", ")
}

func containsManager(managers []config.PackageManager, target config.PackageManager) bool {
	for _, manager := range managers {
		if manager == target {
			return true
		}
	}
	return false
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
