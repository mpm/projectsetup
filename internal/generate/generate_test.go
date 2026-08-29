package generate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/config"
)

func TestWriteGeneratesSupportedPresets(t *testing.T) {
	tests := []struct {
		name     string
		preset   config.Preset
		manager  config.PackageManager
		database config.Database
		wantText string
	}{
		{name: "node", preset: config.PresetNode, manager: config.PackageManagerPNPM, wantText: "pnpm install --frozen-lockfile"},
		{name: "ruby", preset: config.PresetRuby, wantText: "bundle install"},
		{name: "rails postgres", preset: config.PresetRails, database: config.DatabasePostgres, wantText: "bin/setup --skip-server"},
		{name: "rails sqlite", preset: config.PresetRails, database: config.DatabaseSQLite, wantText: "bin/setup --skip-server"},
		{name: "python", preset: config.PresetPython, manager: config.PackageManagerUV, wantText: "uv sync --frozen"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			home := t.TempDir()
			t.Setenv("HOME", home)
			cfg, err := config.Normalize(config.Input{
				Root: root, Preset: tt.preset, PackageManager: tt.manager,
				Database: tt.database, AITools: []config.AITool{config.AIToolOpenCode, config.AIToolClaude},
			})
			if err != nil {
				t.Fatalf("Normalize() error = %v", err)
			}
			if err := Write(root, cfg, false); err != nil {
				t.Fatalf("Write() error = %v", err)
			}

			devcontainer := readGenerated(t, root, "devcontainer.json")
			if !json.Valid(devcontainer) {
				t.Fatal("devcontainer.json is invalid JSON")
			}
			if !strings.Contains(string(devcontainer), `"containerUser": "vscode"`) ||
				!strings.Contains(string(devcontainer), `"CLAUDE_CONFIG_DIR": "/home/vscode/.claude"`) {
				t.Fatalf("devcontainer.json lacks shared user or Claude settings:\n%s", devcontainer)
			}
			dockerfile := readGenerated(t, root, "Dockerfile")
			if tt.preset == config.PresetRails {
				if !strings.Contains(string(dockerfile), "FROM mcr.microsoft.com/devcontainers/base:ubuntu-24.04") ||
					!strings.Contains(string(devcontainer), `"ghcr.io/rails/devcontainer/features/ruby:2"`) ||
					!strings.Contains(string(devcontainer), `"version": "3.3"`) ||
					!strings.Contains(string(devcontainer), "/home/vscode/.local/share/mise/shims") ||
					!strings.Contains(string(devcontainer), "ghcr.io/rails/devcontainer/features/activestorage") {
					t.Fatalf("Rails output lacks the official Ruby feature or required settings:\n%s\n%s", dockerfile, devcontainer)
				}
				if tt.database == config.DatabasePostgres && !strings.Contains(string(devcontainer), "ghcr.io/rails/devcontainer/features/postgres-client") {
					t.Fatalf("Rails PostgreSQL output lacks the PostgreSQL client feature:\n%s", devcontainer)
				}
			}
			if tt.preset == config.PresetRuby {
				if !strings.Contains(string(devcontainer), `"ghcr.io/rails/devcontainer/features/ruby:2"`) ||
					!strings.Contains(string(devcontainer), `"version": "3.3"`) ||
					!strings.Contains(string(devcontainer), "/home/vscode/.local/share/mise/shims") ||
					strings.Contains(string(devcontainer), "features/activestorage") ||
					strings.Contains(string(devcontainer), "features/node:1") {
					t.Fatalf("Ruby output has missing runtime settings or Rails extras:\n%s", devcontainer)
				}
			}
			postCreate := readGenerated(t, root, "scripts/post-create.sh")
			if !strings.Contains(string(postCreate), tt.wantText) {
				t.Fatalf("post-create.sh does not contain %q:\n%s", tt.wantText, postCreate)
			}
			for _, script := range []string{"scripts/install-ai-tools.sh", "scripts/post-create.sh"} {
				path := filepath.Join(root, directoryName, script)
				info, err := os.Stat(path)
				if err != nil {
					t.Fatalf("stat %s: %v", script, err)
				}
				if info.Mode().Perm() != 0o755 {
					t.Errorf("%s mode = %o, want 755", script, info.Mode().Perm())
				}
				if output, err := exec.Command("bash", "-n", path).CombinedOutput(); err != nil {
					t.Errorf("bash -n %s: %v\n%s", script, err, output)
				}
			}
			compose := readGenerated(t, root, "compose.yaml")
			if tt.database == config.DatabasePostgres {
				if !strings.Contains(string(compose), "condition: service_healthy") || !strings.Contains(string(compose), "postgres:17-bookworm") {
					t.Fatalf("compose.yaml lacks PostgreSQL health dependency:\n%s", compose)
				}
			} else if strings.Contains(string(compose), "postgres:") || strings.Contains(string(compose), "postgres-data") {
				t.Fatalf("compose.yaml contains an unselected PostgreSQL service:\n%s", compose)
			}
			if tt.database == config.DatabaseSQLite {
				if !strings.Contains(string(dockerfile), "libsqlite3-dev sqlite3") {
					t.Fatalf("Dockerfile lacks SQLite packages:\n%s", dockerfile)
				}
				if strings.Contains(string(devcontainer), `"DB_HOST"`) || strings.Contains(string(compose), "sqlite:") {
					t.Fatalf("SQLite output contains sidecar configuration:\n%s\n%s", devcontainer, compose)
				}
			}
			for _, relative := range hostMountDirectories(cfg.AITools) {
				if info, err := os.Stat(filepath.Join(home, relative)); err != nil || !info.IsDir() {
					t.Errorf("host mount directory %s was not created", relative)
				}
			}
		})
	}
}

func TestWriteOnlyReplacesRecognizedDirectoryWithForce(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	cfg, err := config.Normalize(config.Input{Root: root, Preset: config.PresetNode})
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	if err := Write(root, cfg, false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("second Write() error = %v, want --force guidance", err)
	}
	if err := Write(root, cfg, true); err != nil {
		t.Fatalf("forced Write() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, directoryName, "notes.txt"), []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(root, cfg, true); err == nil || !strings.Contains(err.Error(), "unrelated path") {
		t.Fatalf("Write() error = %v, want unrelated-file refusal", err)
	}

	otherRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(otherRoot, directoryName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherRoot, directoryName, "projectsetup.json"), []byte(`{"generatedBy":"someone-else"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	otherCfg, err := config.Normalize(config.Input{Root: otherRoot, Preset: config.PresetNode})
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(otherRoot, otherCfg, true); err == nil || !strings.Contains(err.Error(), "not recognized") {
		t.Fatalf("Write() error = %v, want unrecognized-directory refusal", err)
	}
}

func TestAIInstallerInstallsClaudeFromCleanState(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	bin := t.TempDir()
	installer := filepath.Join(root, "install-ai-tools.sh")
	data, err := templateFiles.ReadFile("templates/install-ai-tools.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installer, data, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeCurl := `#!/usr/bin/env bash
printf '%s\n' 'mkdir -p "$HOME/.local/share/claude/versions"' 'printf "#!/usr/bin/env bash\\necho 1.0.0\\n" > "$HOME/.local/share/claude/versions/1.0.0"' 'chmod +x "$HOME/.local/share/claude/versions/1.0.0"'
`
	if err := os.WriteFile(filepath.Join(bin, "curl"), []byte(fakeCurl), 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", installer, "claude")
	command.Env = append(os.Environ(), "HOME="+home, "PATH="+bin+":"+filepath.Join(home, ".local/bin")+":"+os.Getenv("PATH"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("install-ai-tools.sh claude: %v\n%s", err, output)
	}
	if target, err := os.Readlink(filepath.Join(home, ".local/bin/claude")); err != nil || target != filepath.Join(home, ".local/share/claude/versions/1.0.0") {
		t.Fatalf("Claude link target = %q, error = %v", target, err)
	}
}

func TestPostCreateSkipsMissingDependencyFiles(t *testing.T) {
	tests := []struct {
		preset  config.Preset
		manager config.PackageManager
	}{
		{preset: config.PresetNode, manager: config.PackageManagerNPM},
		{preset: config.PresetRuby},
		{preset: config.PresetPython, manager: config.PackageManagerPip},
	}
	for _, tt := range tests {
		t.Run(string(tt.preset), func(t *testing.T) {
			root := t.TempDir()
			home := t.TempDir()
			t.Setenv("HOME", home)
			cfg, err := config.Normalize(config.Input{Root: root, Preset: tt.preset, PackageManager: tt.manager, AITools: []config.AITool{}})
			if err != nil {
				t.Fatal(err)
			}
			if err := Write(root, cfg, false); err != nil {
				t.Fatal(err)
			}
			if tt.preset == config.PresetNode {
				if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			command := exec.Command("bash", filepath.Join(root, directoryName, "scripts/post-create.sh"))
			command.Dir = root
			command.Env = append(os.Environ(), "HOME="+home)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("post-create.sh: %v\n%s", err, output)
			}
		})
	}
}

func readGenerated(t *testing.T, root, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, directoryName, filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}
