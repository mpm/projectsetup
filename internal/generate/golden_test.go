package generate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/mpm/projectsetup/internal/config"
)

type goldenCase struct {
	name  string
	input config.Input
}

var goldenCases = []goldenCase{
	{
		name: "node-opencode",
		input: config.Input{
			ProjectName: "node-app", Preset: config.PresetNode,
			PackageManager: config.PackageManagerNPM, LanguageVersion: "22",
		},
	},
	{
		name: "node-claude-postgres",
		input: config.Input{
			ProjectName: "node-postgres", Preset: config.PresetNode,
			PackageManager: config.PackageManagerPNPM, LanguageVersion: "20",
			Database: config.DatabasePostgres,
			AITools:  []config.AITool{config.AIToolOpenCode, config.AIToolClaude},
			Ports:    []int{5173},
		},
	},
	{
		name: "rails-claude-postgres",
		input: config.Input{
			ProjectName: "rails-postgres", Preset: config.PresetRails,
			LanguageVersion: "3.3.6", Database: config.DatabasePostgres,
			AITools: []config.AITool{config.AIToolOpenCode, config.AIToolClaude},
			Ports:   []int{3000},
		},
	},
	{
		name: "rails-opencode",
		input: config.Input{
			ProjectName: "rails-app", Preset: config.PresetRails,
			LanguageVersion: "3.3.6",
		},
	},
	{
		name: "python-pip",
		input: config.Input{
			ProjectName: "python-pip", Preset: config.PresetPython,
			PackageManager: config.PackageManagerPip, LanguageVersion: "3.13",
		},
	},
	{
		name: "python-uv-claude-postgres",
		input: config.Input{
			ProjectName: "python-uv", Preset: config.PresetPython,
			PackageManager: config.PackageManagerUV, LanguageVersion: "3.12",
			Database: config.DatabasePostgres,
			AITools:  []config.AITool{config.AIToolOpenCode, config.AIToolClaude},
			Ports:    []int{8000}, SystemPackages: []string{"build-essential"},
		},
	},
	{
		name: "python-poetry",
		input: config.Input{
			ProjectName: "python-poetry", Preset: config.PresetPython,
			PackageManager: config.PackageManagerPoetry, LanguageVersion: "3.11",
		},
	},
}

func TestGoldenTrees(t *testing.T) {
	for _, tt := range goldenCases {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", t.TempDir())
			tt.input.Root = root
			cfg, err := config.Normalize(tt.input)
			if err != nil {
				t.Fatalf("Normalize() error = %v", err)
			}
			if err := Write(root, cfg, false); err != nil {
				t.Fatalf("Write() error = %v", err)
			}

			actual := filepath.Join(root, directoryName)
			expected := filepath.Join("testdata", "golden", tt.name)
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := replaceTree(expected, actual); err != nil {
					t.Fatalf("update golden tree: %v", err)
				}
			}
			compareTrees(t, expected, actual)

			devcontainer, err := os.ReadFile(filepath.Join(actual, "devcontainer.json"))
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]any
			if err := json.Unmarshal(devcontainer, &document); err != nil {
				t.Fatalf("parse generated devcontainer.json: %v", err)
			}
		})
	}
}

func TestGoldenComposeConfigurations(t *testing.T) {
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Skip("docker is not installed")
	}
	if output, err := exec.Command(docker, "compose", "version").CombinedOutput(); err != nil {
		t.Skipf("docker compose is not available: %v: %s", err, output)
	}

	for _, name := range []string{"node-claude-postgres", "rails-claude-postgres", "python-uv-claude-postgres"} {
		t.Run(name, func(t *testing.T) {
			compose := filepath.Join("testdata", "golden", name, "compose.yaml")
			command := exec.Command(docker, "compose", "-f", compose, "config")
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("docker compose config: %v\n%s", err, output)
			}
		})
	}
}

func TestGoldenDevcontainerConfigurations(t *testing.T) {
	devcontainer, err := exec.LookPath("devcontainer")
	if err != nil {
		t.Skip("devcontainer is not installed")
	}
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Skip("docker is not installed; devcontainer read-configuration requires it")
	}
	daemonContext, daemonCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer daemonCancel()
	if output, err := exec.CommandContext(daemonContext, docker, "info").CombinedOutput(); err != nil {
		t.Skipf("Docker daemon is unavailable: %v: %s", err, output)
	}

	for _, name := range []string{"node-opencode", "rails-claude-postgres", "python-uv-claude-postgres"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			root := t.TempDir()
			fixture := filepath.Join("testdata", "golden", name)
			if err := replaceTree(filepath.Join(root, directoryName), fixture); err != nil {
				t.Fatalf("prepare fixture workspace: %v", err)
			}
			command := exec.CommandContext(ctx, devcontainer, "read-configuration", "--workspace-folder", root, "--log-level", "trace")
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("devcontainer read-configuration: %v\n%s", err, output)
			}
		})
	}
}

func TestBuildPresetFixtures(t *testing.T) {
	if os.Getenv("PROJECTSETUP_BUILD_TESTS") != "1" {
		t.Skip("set PROJECTSETUP_BUILD_TESTS=1 to build one fixture per preset")
	}
	devcontainer, err := exec.LookPath("devcontainer")
	if err != nil {
		t.Fatal("devcontainer is not installed")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("docker is not installed")
	}

	for _, name := range []string{"node-opencode", "rails-opencode", "python-pip"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := replaceTree(filepath.Join(root, directoryName), filepath.Join("testdata", "golden", name)); err != nil {
				t.Fatalf("prepare fixture workspace: %v", err)
			}
			command := exec.Command(devcontainer, "build", "--workspace-folder", root)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("devcontainer build: %v\n%s", err, output)
			}
		})
	}
}

func TestSmokePresetFixtures(t *testing.T) {
	if os.Getenv("PROJECTSETUP_SMOKE_TESTS") != "1" {
		t.Skip("set PROJECTSETUP_SMOKE_TESTS=1 to create and smoke-test one container per preset")
	}
	devcontainer, err := exec.LookPath("devcontainer")
	if err != nil {
		t.Fatal("devcontainer is not installed")
	}
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Fatal("docker is not installed")
	}

	for _, name := range []string{"node-opencode", "rails-opencode", "python-pip"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			home := t.TempDir()
			if err := replaceTree(filepath.Join(root, directoryName), filepath.Join("testdata", "golden", name)); err != nil {
				t.Fatalf("prepare fixture workspace: %v", err)
			}
			for _, relative := range config.AIHostDirectories([]config.AITool{config.AIToolOpenCode}) {
				if err := os.MkdirAll(filepath.Join(home, relative), 0o755); err != nil {
					t.Fatalf("prepare clean AI host directory: %v", err)
				}
			}

			up := exec.Command(devcontainer, "up", "--workspace-folder", root)
			up.Env = append(os.Environ(), "HOME="+home)
			var stderr bytes.Buffer
			up.Stderr = &stderr
			output, err := up.Output()
			var result struct {
				ContainerID string `json:"containerId"`
			}
			parseErr := json.Unmarshal(output, &result)
			if result.ContainerID != "" {
				t.Cleanup(func() {
					if output, err := exec.Command(docker, "rm", "-f", result.ContainerID).CombinedOutput(); err != nil {
						t.Errorf("remove smoke-test container: %v\n%s", err, output)
					}
				})
			}
			if err != nil {
				t.Fatalf("devcontainer up: %v\n%s\n%s", err, output, stderr.Bytes())
			}
			if parseErr != nil || result.ContainerID == "" {
				t.Fatalf("parse devcontainer up output: %v\n%s", parseErr, output)
			}

			smoke := exec.Command(devcontainer, "exec", "--workspace-folder", root, "bash", "-lc", `test "$(id -un)" = vscode && test "$HOME" = /home/vscode && test -w "$HOME" && command -v opencode >/dev/null && opencode --version >/dev/null`)
			smoke.Env = append(os.Environ(), "HOME="+home)
			if output, err := smoke.CombinedOutput(); err != nil {
				t.Fatalf("smoke test container: %v\n%s", err, output)
			}
		})
	}
}

type treeFile struct {
	data []byte
	mode fs.FileMode
}

func compareTrees(t *testing.T, expectedRoot, actualRoot string) {
	t.Helper()
	expected := readTree(t, expectedRoot)
	actual := readTree(t, actualRoot)

	paths := make([]string, 0, len(expected)+len(actual))
	seen := make(map[string]bool, len(expected)+len(actual))
	for path := range expected {
		seen[path] = true
		paths = append(paths, path)
	}
	for path := range actual {
		if !seen[path] {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)

	for _, path := range paths {
		want, wantOK := expected[path]
		got, gotOK := actual[path]
		if !wantOK {
			t.Errorf("unexpected generated file %s", path)
			continue
		}
		if !gotOK {
			t.Errorf("missing generated file %s", path)
			continue
		}
		if want.mode != got.mode {
			t.Errorf("%s mode = %o, want %o", path, got.mode, want.mode)
		}
		if !bytes.Equal(want.data, got.data) {
			t.Errorf("%s content differs from golden fixture; run UPDATE_GOLDEN=1 go test ./internal/generate", path)
		}
	}
}

func readTree(t *testing.T, root string) map[string]treeFile {
	t.Helper()
	result := make(map[string]treeFile)
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[filepath.ToSlash(relative)] = treeFile{data: data, mode: info.Mode().Perm()}
		return nil
	}); err != nil {
		t.Fatalf("read tree %s: %v", root, err)
	}
	return result
}

func replaceTree(destination, source string) error {
	if err := os.RemoveAll(destination); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !entry.Type().IsRegular() {
			return errors.New("golden source contains a non-regular file: " + relative)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}
