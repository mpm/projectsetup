package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/doctor"
	"github.com/mpm/projectsetup/internal/generate"
	"github.com/mpm/projectsetup/internal/presets"
	"github.com/mpm/projectsetup/internal/validate"
)

// TestMain keeps tests independent of the user's definitions.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "projectsetup-config-")
	if err != nil {
		panic(err)
	}
	os.Setenv(presets.ConfigDirEnv, dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func readManifest(t *testing.T, root string) config.Manifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".devcontainer", "projectsetup.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := config.ReadManifest(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

// writeSchema1Manifest turns a generated configuration into one written
// before manifest schema 2, which had no definition copies.
func writeSchema1Manifest(t *testing.T, root, manifest string) {
	t.Helper()
	devDir := filepath.Join(root, ".devcontainer")
	if err := os.RemoveAll(filepath.Join(devDir, config.PresetsDir)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(devDir, "projectsetup.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunHelp(t *testing.T) {
	var stdout bytes.Buffer
	if err := Run([]string{"--help"}, &bytes.Buffer{}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "projectsetup init") {
		t.Fatalf("help output does not describe init:\n%s", stdout.String())
	}
}

func TestRunVersion(t *testing.T) {
	var stdout bytes.Buffer
	if err := Run([]string{"--version"}, &bytes.Buffer{}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.HasPrefix(stdout.String(), "projectsetup ") || !strings.Contains(stdout.String(), "commit:") {
		t.Fatalf("version output = %q", stdout.String())
	}
}

func TestRunInitNonInteractive(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"engines":{"node":"22"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := runInit(root, []string{"--non-interactive", "--database", "sqlite", "--ai", "none", "--port", "3000"}, &bytes.Buffer{}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Generated .devcontainer") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".devcontainer", "devcontainer.json")); err != nil {
		t.Fatalf("generated devcontainer.json: %v", err)
	}
	dockerfile, err := os.ReadFile(filepath.Join(root, ".devcontainer", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(dockerfile), "libsqlite3-dev sqlite3") {
		t.Fatalf("generated Dockerfile lacks SQLite packages:\n%s", dockerfile)
	}
}

func TestRunInitDetectsRubyGem(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(root, "example.gemspec"), []byte("Gem::Specification.new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".ruby-version"), []byte("3.2.6\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runInit(root, []string{"--non-interactive", "--ai", "none"}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}
	manifest := readManifest(t, root)
	if manifest.Preset.Name != "ruby" || manifest.Options["ruby"]["version"] != "3.2.6" {
		t.Fatalf("Ruby manifest does not contain detected settings: %+v", manifest)
	}
}

func TestRunInitDetectsRubyVersionFromGemfile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	gemfile := "source 'https://rubygems.org'\n\nruby '3.3.0'\ngemspec\n"
	if err := os.WriteFile(filepath.Join(root, "Gemfile"), []byte(gemfile), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runInit(root, []string{"--non-interactive", "--ai", "none"}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}
	devcontainer, err := os.ReadFile(filepath.Join(root, ".devcontainer", "devcontainer.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(devcontainer), `"version": "3.3.0"`) {
		t.Fatalf("Ruby feature does not use the Gemfile version:\n%s", devcontainer)
	}
}

func TestRunInitAcceptsRubyVersionForRubyPreset(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	err := runInit(root, []string{"--non-interactive", "--preset", "ruby", "--ruby-version", "3.3.7", "--ai", "none"}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("runInit() error = %v", err)
	}
}

func TestRunInitRejectsAmbiguousDetection(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"package.json", "pyproject.toml"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	err := runInit(root, []string{"--non-interactive"}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "multiple project presets detected") {
		t.Fatalf("runInit() error = %v", err)
	}
}

func TestRunUpgradeConvertsLegacyGeneratedConfigurationToCompose(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	cfg, err := config.Normalize(config.Input{
		Root: root, ProjectName: "legacy", Preset: "node",
		Options: map[string]map[string]string{"node": {"package_manager": "npm"}}, Ports: []int{3000}, AITools: []config.AITool{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := generate.Write(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	devDir := filepath.Join(root, ".devcontainer")
	if err := os.Remove(filepath.Join(devDir, "compose.yaml")); err != nil {
		t.Fatal(err)
	}
	legacy := `{"name":"legacy","build":{"dockerfile":"Dockerfile","context":".."},"workspaceFolder":"/workspaces/legacy","containerUser":"vscode","remoteUser":"vscode"}`
	if err := os.WriteFile(filepath.Join(devDir, "devcontainer.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := runUpgrade(root, nil, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("runUpgrade() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Upgraded .devcontainer") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	devcontainer, err := os.ReadFile(filepath.Join(devDir, "devcontainer.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(devcontainer), `"dockerComposeFile": "compose.yaml"`) || strings.Contains(string(devcontainer), `"build"`) {
		t.Fatalf("upgraded devcontainer.json does not use Compose:\n%s", devcontainer)
	}
	if _, err := os.Stat(filepath.Join(devDir, "compose.yaml")); err != nil {
		t.Fatalf("upgraded compose.yaml: %v", err)
	}
	manifest, err := os.ReadFile(filepath.Join(devDir, "projectsetup.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), `"ports": [`) || !strings.Contains(string(manifest), "3000") {
		t.Fatalf("upgrade did not preserve manifest options:\n%s", manifest)
	}
}

func TestRunUpgradeAcceptsDetectedPatchVersionForConfiguredReleaseLine(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	cfg, err := config.Normalize(config.Input{
		Root: root, ProjectName: "example", Preset: "rails", AITools: []config.AITool{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := generate.Write(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string]string{
		"Gemfile":       `gem "rails"`,
		".ruby-version": "4.0.7\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := runUpgrade(root, nil, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runUpgrade() error = %v", err)
	}
}

func TestRunUpgradeRejectsHandWrittenConfiguration(t *testing.T) {
	root := t.TempDir()
	devDir := filepath.Join(root, ".devcontainer")
	if err := os.Mkdir(devDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, manifest := range []string{
		`{"schemaVersion":1,"database":"none","generatedBy":"someone-else"}`,
		`{"schemaVersion":2,"generatedBy":"someone-else"}`,
	} {
		if err := os.WriteFile(filepath.Join(devDir, "projectsetup.json"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		err := runUpgrade(root, nil, &bytes.Buffer{}, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "not recognized") {
			t.Fatalf("runUpgrade(%s) error = %v, want ownership refusal", manifest, err)
		}
	}
}

func TestRunUpgradeRejectsMalformedGeneratedManifest(t *testing.T) {
	root := t.TempDir()
	devDir := filepath.Join(root, ".devcontainer")
	if err := os.Mkdir(devDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"schemaVersion":1,"projectName":"legacy","preset":"node","database":"none","packageManager":"npm","languageVersion":"24","generatedBy":"projectsetup"}`
	if err := os.WriteFile(filepath.Join(devDir, "projectsetup.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runUpgrade(root, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "not normalized") {
		t.Fatalf("runUpgrade() error = %v, want malformed-manifest refusal", err)
	}
}

func TestRunInitRejectsAmbiguousManagers(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"package.json", "package-lock.json", "yarn.lock"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	err := runInit(root, []string{"--non-interactive"}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "multiple values detected for node.package_manager (npm, yarn); pass --set node.package_manager=VALUE") {
		t.Fatalf("runInit() error = %v", err)
	}
}

func TestRunInitInteractiveUsesDetectedDefaults(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"engines":{"node":"22"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package-lock.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err := runInit(root, nil, strings.NewReader("\n\ny\n"), &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("runInit() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Configuration:") || !strings.Contains(stdout.String(), "AI tools: opencode") {
		t.Fatalf("stdout does not contain normalized summary:\n%s", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".devcontainer", "projectsetup.json")); err != nil {
		t.Fatalf("generated projectsetup.json: %v", err)
	}
}

func TestRunInitInteractiveResolvesAmbiguity(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	for _, name := range []string{"package.json", "pyproject.toml", "package-lock.json", "yarn.lock"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Preset, node.package_manager, node.version, add-ons, AI tools, confirmation.
	input := strings.NewReader("node\nyarn\n\nnone\nnone\ny\n")
	if err := runInit(root, nil, input, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}
}

func TestRunInitInteractiveCancellationWritesNothing(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"engines":{"node":"22"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package-lock.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := runInit(root, nil, strings.NewReader("\n\n\n"), &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".devcontainer")); !os.IsNotExist(err) {
		t.Fatalf(".devcontainer exists after cancellation: %v", err)
	}
	if !strings.Contains(stdout.String(), "Initialization cancelled") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestResolveAITools(t *testing.T) {
	for _, value := range []string{"codex", "claude", "codex,opencode", "opencode,claude,codex", "codex,codex", "none"} {
		flags := flag.NewFlagSet("test", flag.ContinueOnError)
		flags.String("ai", "", "")
		if err := flags.Parse([]string{"--ai", value}); err != nil {
			t.Fatal(err)
		}
		if _, err := resolveAITools(value, flags); err != nil {
			t.Errorf("%q: %v", value, err)
		}
	}
	for _, value := range []string{"", "none,codex", "codex,", ",codex", "unknown", "Codex"} {
		if _, err := parseAITools(value, true); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	err := Run([]string{"generate"}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), `unknown command "generate"`) {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunCheckReportsAggregatedFailure(t *testing.T) {
	root := t.TempDir()
	devDir := filepath.Join(root, ".devcontainer")
	if err := os.Mkdir(devDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(devDir, "projectsetup.json"), []byte(`{"schemaVersion":99}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	err := runCheck(root, nil, &bytes.Buffer{}, &stderr)
	if err == nil || !strings.Contains(err.Error(), "configuration check failed") {
		t.Fatalf("runCheck() error = %v", err)
	}
	if !strings.Contains(stderr.String(), "unsupported schemaVersion") || !strings.Contains(stderr.String(), "required generated file") {
		t.Fatalf("stderr does not contain aggregated diagnostics:\n%s", stderr.String())
	}
}

func TestRunDoctorRendersDistinctSeverities(t *testing.T) {
	root := t.TempDir()
	environment := doctor.Environment{
		GOOS:   "linux",
		GOARCH: "amd64",
		Getenv: func(string) string { return "" },
		LookPath: func(string) (string, error) {
			return "", os.ErrNotExist
		},
		ReadFile: func(string) ([]byte, error) { return nil, os.ErrNotExist },
	}
	var stdout, stderr bytes.Buffer
	err := runDoctor(root, nil, &stdout, &stderr, environment)
	if err == nil || !strings.Contains(err.Error(), "doctor found 3 error(s)") {
		t.Fatalf("runDoctor() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "INFO: host: GOOS=linux GOARCH=amd64") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "ERROR: docker: not found") || !strings.Contains(stderr.String(), "WARNING: SSH_AUTH_SOCK:") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestUpgradeReplacesOnlyAISelection(t *testing.T) {
	for _, value := range []string{"codex,opencode,codex", "none"} {
		t.Run(value, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", t.TempDir())
			t.Setenv("CODEX_HOME", "")
			cfg, err := config.Normalize(config.Input{Root: root, ProjectName: "keep-me", Preset: "python", Addons: []string{"postgres"}, Options: map[string]map[string]string{"python": {"version": "3.12", "package_manager": "uv"}}, Ports: []int{8000}, SystemPackages: []string{"make"}})
			if err != nil {
				t.Fatal(err)
			}
			if err := generate.Write(root, cfg, false); err != nil {
				t.Fatal(err)
			}
			if err := runUpgrade(root, []string{"--ai", value}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(root, ".devcontainer/projectsetup.json"))
			if err != nil {
				t.Fatal(err)
			}
			got, err := config.ReadManifest(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			want := config.NewManifest(cfg)
			want.AITools = []config.AITool{config.AIToolCodex, config.AIToolOpenCode}
			if value == "none" {
				want.AITools = []config.AITool{}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("manifest = %#v, want %#v", got, want)
			}
			// Plain upgrades must preserve the new selection, including none.
			if err := runUpgrade(root, nil, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			after, _ := os.ReadFile(filepath.Join(root, ".devcontainer/projectsetup.json"))
			if !bytes.Equal(data, after) {
				t.Fatal("plain upgrade changed manifest")
			}
			if err := runUpgrade(root, []string{"--ai", "codex,none"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
				t.Fatal("accepted invalid selection")
			}
			after, _ = os.ReadFile(filepath.Join(root, ".devcontainer/projectsetup.json"))
			if !bytes.Equal(data, after) {
				t.Fatal("invalid upgrade changed manifest")
			}
		})
	}
}

func writeNodeProject(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"engines":{"node":"22"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

// assertConsistentProjectName verifies that every generated file uses want as
// the project name.
func assertConsistentProjectName(t *testing.T, root, want string) {
	t.Helper()
	devDir := filepath.Join(root, ".devcontainer")
	var devcontainer struct {
		Name            string `json:"name"`
		WorkspaceFolder string `json:"workspaceFolder"`
	}
	data, err := os.ReadFile(filepath.Join(devDir, "devcontainer.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &devcontainer); err != nil {
		t.Fatalf("parse devcontainer.json: %v", err)
	}
	if devcontainer.Name != want || devcontainer.WorkspaceFolder != "/workspaces/"+want {
		t.Errorf("devcontainer.json name = %q, workspaceFolder = %q; want %q", devcontainer.Name, devcontainer.WorkspaceFolder, want)
	}
	file, err := os.Open(filepath.Join(devDir, "projectsetup.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	manifest, err := config.ReadManifest(file)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ProjectName != want {
		t.Errorf("projectsetup.json projectName = %q, want %q", manifest.ProjectName, want)
	}
	compose, err := os.ReadFile(filepath.Join(devDir, "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(compose), "\n")
	var composeNames []string
	for _, line := range lines {
		if strings.HasPrefix(line, "name: ") {
			composeNames = append(composeNames, strings.TrimPrefix(line, "name: "))
		}
	}
	if !reflect.DeepEqual(composeNames, []string{want}) {
		t.Errorf("compose.yaml project names = %q, want [%q]", composeNames, want)
	}
	if !strings.Contains(string(compose), "- ..:/workspaces/"+want+"\n") {
		t.Errorf("compose.yaml does not mount the workspace at /workspaces/%s:\n%s", want, compose)
	}
}

func TestRunInitUsesValidExplicitNameEverywhere(t *testing.T) {
	for _, name := range []string{"my-app", "my_app", "app2", "9lives"} {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "Some.Directory")
			t.Setenv("HOME", t.TempDir())
			writeNodeProject(t, root)
			var stdout bytes.Buffer
			if err := runInit(root, []string{"--non-interactive", "--preset", "node", "--ai", "none", "--database", "postgres", "--name", name}, &bytes.Buffer{}, &stdout, &bytes.Buffer{}); err != nil {
				t.Fatalf("runInit() error = %v", err)
			}
			if !strings.Contains(stdout.String(), "Generated .devcontainer for "+name+" ") {
				t.Errorf("stdout = %q", stdout.String())
			}
			assertConsistentProjectName(t, root, name)
		})
	}
}

func TestRunInitNonInteractiveRejectsInvalidName(t *testing.T) {
	tests := []struct {
		name       string
		suggestion string
	}{
		{name: "My App", suggestion: "my-app"},
		{name: "a.b", suggestion: "a-b"},
		{name: "über", suggestion: "ber"},
		{name: "-dash", suggestion: "dash"},
		{name: "x/y", suggestion: "x-y"},
		{name: "_x", suggestion: "x"},
		{name: "ü"},
		{name: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", t.TempDir())
			writeNodeProject(t, root)
			err := runInit(root, []string{"--non-interactive", "--preset", "node", "--ai", "none", "--name", tt.name}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{})
			if err == nil {
				t.Fatal("runInit() succeeded, want invalid name error")
			}
			message := err.Error()
			if !strings.HasPrefix(message, "--name: ") || !strings.Contains(message, config.ProjectNamePattern) {
				t.Errorf("error %q does not identify --name and the allowed pattern", message)
			}
			if tt.name != "" && !strings.Contains(message, fmt.Sprintf("%q", tt.name)) {
				t.Errorf("error %q does not name the invalid value", message)
			}
			if tt.suggestion != "" && !strings.Contains(message, fmt.Sprintf("use %q instead", tt.suggestion)) {
				t.Errorf("error %q does not suggest %q", message, tt.suggestion)
			}
			if _, err := os.Stat(filepath.Join(root, ".devcontainer")); !os.IsNotExist(err) {
				t.Fatalf(".devcontainer exists after rejected name: %v", err)
			}
		})
	}
}

func TestRunInitNormalizesDirectoryDerivedName(t *testing.T) {
	tests := []struct {
		directory string
		want      string
		wantErr   bool
	}{
		{directory: "plain-app", want: "plain-app"},
		{directory: "a.b", want: "a-b"},
		{directory: "My App", want: "my-app"},
		{directory: "-dash", want: "dash"},
		{directory: "über", want: "ber"},
		{directory: "üü", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.directory, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), tt.directory)
			t.Setenv("HOME", t.TempDir())
			writeNodeProject(t, root)
			err := runInit(root, []string{"--non-interactive", "--preset", "node", "--ai", "none", "--database", "postgres"}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{})
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "--name") || !strings.Contains(err.Error(), config.ProjectNamePattern) {
					t.Fatalf("runInit() error = %v, want actionable name error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("runInit() error = %v", err)
			}
			assertConsistentProjectName(t, root, tt.want)
		})
	}
}

func TestRunInitInteractiveProposesNormalizedName(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		dir   string
		input string
		want  string
	}{
		{name: "invalid flag accepts proposal", args: []string{"--name", "a.b"}, dir: "project", input: "\ny\n", want: "a-b"},
		{name: "invalid flag then invalid answer then custom", args: []string{"--name", "My App"}, dir: "project", input: "Not.Valid\ncustom_name\ny\n", want: "custom_name"},
		{name: "invalid directory accepts proposal", dir: "My.App", input: "\ny\n", want: "my-app"},
		{name: "unusable directory requires answer", dir: "üü", input: "\nchosen\ny\n", want: "chosen"},
		{name: "valid flag is not prompted", args: []string{"--name", "direct"}, dir: "Other Dir", input: "y\n", want: "direct"},
		{name: "valid directory is not prompted", dir: "valid-dir", input: "y\n", want: "valid-dir"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), tt.dir)
			t.Setenv("HOME", t.TempDir())
			writeNodeProject(t, root)
			args := append([]string{"--preset", "node", "--package-manager", "npm", "--database", "none", "--ai", "none", "--node-version", "22"}, tt.args...)
			var stdout bytes.Buffer
			if err := runInit(root, args, strings.NewReader(tt.input), &stdout, &bytes.Buffer{}); err != nil {
				t.Fatalf("runInit() error = %v\nstdout:\n%s", err, stdout.String())
			}
			if !strings.Contains(stdout.String(), "Project: "+tt.want+"\n") {
				t.Errorf("summary does not show project %q:\n%s", tt.want, stdout.String())
			}
			assertConsistentProjectName(t, root, tt.want)
		})
	}
}

func TestRunInitInteractiveRepromptsInvalidName(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	writeNodeProject(t, root)
	args := []string{"--preset", "node", "--package-manager", "npm", "--database", "none", "--ai", "none", "--name", "x/y"}
	var stdout bytes.Buffer
	if err := runInit(root, args, strings.NewReader("bad name\n"), &stdout, &bytes.Buffer{}); err == nil {
		t.Fatal("runInit() succeeded after input ended")
	}
	output := stdout.String()
	for _, want := range []string{`--name: project name "x/y" is invalid`, `Project name [x-y]: `, `project name "bad name" is invalid`, config.ProjectNamePattern} {
		if !strings.Contains(output, want) {
			t.Errorf("stdout does not contain %q:\n%s", want, output)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".devcontainer")); !os.IsNotExist(err) {
		t.Fatalf(".devcontainer exists after aborted prompt: %v", err)
	}
}

// legacyDottedProject simulates a v0.6.0 configuration generated with
// --name a.b, whose manifest and devcontainer.json kept the dot.
func legacyDottedProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	cfg, err := config.Normalize(config.Input{Root: root, ProjectName: "a-b", Preset: "node", AITools: []config.AITool{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := generate.Write(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	devDir := filepath.Join(root, ".devcontainer")
	for name, replacements := range map[string][][2]string{
		"projectsetup.json": {{`"projectName": "a-b"`, `"projectName": "a.b"`}},
		"devcontainer.json": {{`"name": "a-b"`, `"name": "a.b"`}, {"/workspaces/a-b", "/workspaces/a.b"}},
		"compose.yaml":      {{"/workspaces/a-b", "/workspaces/a.b"}},
	} {
		path := filepath.Join(devDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, replacement := range replacements {
			if !strings.Contains(text, replacement[0]) {
				t.Fatalf("%s does not contain %q", name, replacement[0])
			}
			text = strings.ReplaceAll(text, replacement[0], replacement[1])
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestRunUpgradeRejectsLegacyInvalidProjectName(t *testing.T) {
	root := legacyDottedProject(t)
	before, err := os.ReadFile(filepath.Join(root, ".devcontainer", "devcontainer.json"))
	if err != nil {
		t.Fatal(err)
	}
	err = runUpgrade(root, nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("runUpgrade() succeeded, want invalid project name error")
	}
	for _, want := range []string{`"a.b"`, `use "a-b" instead`, "init --force --name"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
	after, err := os.ReadFile(filepath.Join(root, ".devcontainer", "devcontainer.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("upgrade modified files despite rejecting the manifest")
	}
}

func TestCheckReportsLegacyInvalidProjectName(t *testing.T) {
	root := legacyDottedProject(t)
	diagnostics := validate.Check(root, validate.Options{})
	var messages []string
	for _, diagnostic := range diagnostics {
		messages = append(messages, diagnostic.Path+": "+diagnostic.Message)
	}
	joined := strings.Join(messages, "\n")
	if validate.ErrorCount(diagnostics) == 0 || !strings.Contains(joined, `projectName: project name "a.b" is invalid`) || !strings.Contains(joined, `use "a-b" instead`) {
		t.Fatalf("check diagnostics do not report the invalid project name:\n%s", joined)
	}
}

func TestRunInitForceRegeneratesLegacyInvalidProjectName(t *testing.T) {
	root := legacyDottedProject(t)
	if err := runInit(root, []string{"--non-interactive", "--preset", "node", "--ai", "none", "--force", "--name", "a-b"}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}
	assertConsistentProjectName(t, root, "a-b")
}

const wantListOptionsJSON = `{
  "schemaVersion": 2,
  "presets": [
    "node",
    "python",
    "rails",
    "ruby"
  ],
  "addons": [
    "postgres",
    "sqlite"
  ],
  "definitions": {
    "node": {
      "name": "node",
      "kind": "preset",
      "version": "1.0.0",
      "source": "builtin",
      "description": "Node.js with npm, pnpm, or Yarn",
      "options": {
        "package_manager": {
          "description": "Node.js package manager",
          "default": "npm",
          "choices": [
            "npm",
            "pnpm",
            "yarn"
          ]
        },
        "version": {
          "description": "Node.js version",
          "default": "26",
          "pattern": "[0-9]+(\\.[0-9]+){0,2}([-+][a-zA-Z0-9.-]+)?"
        }
      }
    },
    "postgres": {
      "name": "postgres",
      "kind": "addon",
      "version": "1.0.0",
      "source": "builtin",
      "description": "PostgreSQL sidecar with development-only credentials",
      "options": {
        "version": {
          "description": "PostgreSQL major version",
          "default": "18",
          "pattern": "[1-9][0-9]*"
        }
      }
    },
    "python": {
      "name": "python",
      "kind": "preset",
      "version": "1.0.0",
      "source": "builtin",
      "description": "Python with pip, Poetry, or uv",
      "options": {
        "package_manager": {
          "description": "Python package manager",
          "default": "pip",
          "choices": [
            "pip",
            "poetry",
            "uv"
          ]
        },
        "version": {
          "description": "Python version",
          "default": "3.14",
          "pattern": "[0-9]+(\\.[0-9]+){0,2}([-+][a-zA-Z0-9.-]+)?"
        }
      }
    },
    "rails": {
      "name": "rails",
      "kind": "preset",
      "version": "1.0.0",
      "source": "builtin",
      "description": "Ruby on Rails with Bundler, Node, and Active Storage support",
      "options": {
        "version": {
          "description": "Ruby version",
          "default": "4.0",
          "pattern": "[0-9]+(\\.[0-9]+){0,2}([-+][a-zA-Z0-9.-]+)?"
        }
      }
    },
    "ruby": {
      "name": "ruby",
      "kind": "preset",
      "version": "1.0.0",
      "source": "builtin",
      "description": "Ruby with Bundler, for gems and non-Rails applications",
      "options": {
        "version": {
          "description": "Ruby version",
          "default": "4.0",
          "pattern": "[0-9]+(\\.[0-9]+){0,2}([-+][a-zA-Z0-9.-]+)?"
        }
      }
    },
    "sqlite": {
      "name": "sqlite",
      "kind": "addon",
      "version": "1.0.0",
      "source": "builtin",
      "description": "SQLite command-line tool and development headers in the app image",
      "options": {}
    }
  },
  "aiTools": [
    "opencode",
    "claude",
    "codex"
  ],
  "defaultAITools": [
    "opencode"
  ],
  "projectNamePattern": "^[a-z0-9][a-z0-9_-]*$"
}
`

func TestRunInitListOptionsJSON(t *testing.T) {
	// Ambiguous detection must not matter: listing neither detects nor prompts.
	root := t.TempDir()
	for _, name := range []string{"package.json", "pyproject.toml"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"--list-options", "--json"},
		{"--json", "--list-options", "--non-interactive"},
	} {
		var stdout, stderr bytes.Buffer
		if err := runInit(root, args, strings.NewReader(""), &stdout, &stderr); err != nil {
			t.Fatalf("runInit(%v) error = %v", args, err)
		}
		if stdout.String() != wantListOptionsJSON {
			t.Fatalf("runInit(%v) stdout =\n%s\nwant\n%s", args, stdout.String(), wantListOptionsJSON)
		}
		if stderr.Len() != 0 {
			t.Fatalf("runInit(%v) stderr = %q", args, stderr.String())
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("listing options changed the project directory: %v", entries)
	}
}

func TestRunInitListOptionsText(t *testing.T) {
	root := t.TempDir()
	var stdout bytes.Buffer
	if err := runInit(root, []string{"--list-options"}, strings.NewReader(""), &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}
	for _, want := range []string{
		"Presets (--preset): node, python, rails, ruby\n",
		"Add-ons (--addon, repeatable): postgres, sqlite\n",
		"Databases (--database, alias for --addon): none, postgres, sqlite\n",
		"  node.package_manager: Node.js package manager: npm (default), pnpm, yarn\n",
		"  node.version: Node.js version; default 26\n",
		"  python.package_manager: Python package manager: pip (default), poetry, uv\n",
		"  postgres.version: PostgreSQL major version; default 18\n",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout lacks %q:\n%s", want, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "ruby.package_manager") {
		t.Fatalf("stdout lists an option ruby does not have:\n%s", stdout.String())
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatalf("listing options wrote files: %v %v", entries, err)
	}
}

func TestRunInitListOptionsRejectsMisuse(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"--list-options", "--preset", "node"}, "--list-options cannot be combined with --preset"},
		{[]string{"--list-options", "--json", "--force", "--ai", "none"}, "--list-options cannot be combined with --ai, --force"},
		{[]string{"--json", "--non-interactive", "--preset", "node"}, "--json requires --list-options"},
	}
	for _, tt := range tests {
		root := t.TempDir()
		var stdout bytes.Buffer
		err := runInit(root, tt.args, strings.NewReader(""), &stdout, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("runInit(%v) error = %v, want %q", tt.args, err, tt.want)
		}
		if entries, readErr := os.ReadDir(root); readErr != nil || len(entries) != 0 {
			t.Fatalf("runInit(%v) wrote files: %v %v", tt.args, entries, readErr)
		}
	}
}

func TestListOptionsJSONRoundTripsWithInit(t *testing.T) {
	var options config.Options
	if err := json.Unmarshal([]byte(wantListOptionsJSON), &options); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(options, config.ListOptions(presets.Builtin())) {
		t.Fatalf("golden JSON does not match config.ListOptions()")
	}
	t.Setenv("HOME", t.TempDir())
	for _, preset := range options.Presets {
		for _, addon := range append([]string{""}, options.Addons...) {
			args := []string{"--non-interactive", "--preset", preset, "--ai", joinChoices(options.AITools, ",")}
			definitions := []string{preset}
			if addon != "" {
				args = append(args, "--addon", addon)
				definitions = append(definitions, addon)
			}
			// Every listed choice and default must be accepted.
			var settings [][]string
			for _, name := range definitions {
				info := options.Definitions[name]
				for _, option := range slices.Sorted(maps.Keys(info.Options)) {
					details := info.Options[option]
					for _, value := range append([]string{details.Default}, details.Choices...) {
						settings = append(settings, []string{"--set", name + "." + option + "=" + value})
					}
				}
			}
			if len(settings) == 0 {
				settings = [][]string{nil}
			}
			for _, setting := range settings {
				root := t.TempDir()
				if err := runInit(root, append(slices.Clone(args), setting...), strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
					t.Errorf("runInit(%v %v) error = %v", args, setting, err)
				}
			}
		}
	}
	root := t.TempDir()
	err := runInit(root, []string{"--non-interactive", "--preset", "node", "--package-manager", "bun"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("runInit accepted unlisted package manager bun")
	}
}

// writeLegacyPostgresProject generates the PostgreSQL 17 configuration that
// releases before postgresVersion existed produced, with their manifest.
func writeLegacyPostgresProject(t *testing.T, root string) string {
	t.Helper()
	cfg, err := config.Normalize(config.Input{
		Root: root, ProjectName: "legacy-db", Preset: "node", Addons: []string{"postgres"},
		Options: map[string]map[string]string{"postgres": {"version": config.LegacyPostgresVersion}}, AITools: []config.AITool{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := generate.Write(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	writeSchema1Manifest(t, root, `{"schemaVersion":1,"projectName":"legacy-db","preset":"node","database":"postgres","aiTools":[],"packageManager":"npm","languageVersion":"26","ports":[],"systemPackages":[],"generatedBy":"projectsetup"}`)
	return filepath.Join(root, ".devcontainer")
}

func assertPostgresCompose(t *testing.T, devDir, image, dataPath string) {
	t.Helper()
	compose, err := os.ReadFile(filepath.Join(devDir, "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(compose), "image: "+image+"\n") || !strings.Contains(string(compose), "postgres-data:"+dataPath+"\n") {
		t.Fatalf("compose.yaml does not use %s with data at %s:\n%s", image, dataPath, compose)
	}
}

func TestLegacyPostgresProjectKeepsVersion17(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	devDir := writeLegacyPostgresProject(t, root)

	diagnostics := validate.Check(root, validate.Options{})
	if count := validate.ErrorCount(diagnostics); count != 0 {
		t.Fatalf("check on legacy project: errors = %d, diagnostics = %#v", count, diagnostics)
	}
	if err := runUpgrade(root, nil, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("runUpgrade() error = %v", err)
	}
	assertPostgresCompose(t, devDir, "postgres:17-bookworm", "/var/lib/postgresql/data")
	manifest := readManifest(t, root)
	if manifest.SchemaVersion != config.SchemaVersion || manifest.Options["postgres"]["version"] != "17" {
		t.Fatalf("upgrade did not record schema %d with PostgreSQL 17: %+v", config.SchemaVersion, manifest)
	}
	if _, err := os.Stat(filepath.Join(devDir, config.PresetsDir, "postgres.toml")); err != nil {
		t.Fatalf("upgrade did not write definition copies: %v", err)
	}
	if diagnostics := validate.Check(root, validate.Options{}); validate.ErrorCount(diagnostics) != 0 {
		t.Fatalf("check after upgrade: %#v", diagnostics)
	}
}

func TestRunInitPostgresVersion(t *testing.T) {
	tests := []struct {
		name      string
		legacy    bool
		args      []string
		wantImage string
		wantPath  string
		wantErr   string
	}{
		{name: "new project defaults to 18", args: []string{"--database", "postgres"}, wantImage: "postgres:18-trixie", wantPath: "/var/lib/postgresql"},
		{name: "explicit version", args: []string{"--database", "postgres", "--postgres-version", "17"}, wantImage: "postgres:17-bookworm", wantPath: "/var/lib/postgresql/data"},
		{name: "force keeps legacy version", legacy: true, args: []string{"--database", "postgres", "--force"}, wantImage: "postgres:17-bookworm", wantPath: "/var/lib/postgresql/data"},
		{name: "force with explicit version", legacy: true, args: []string{"--database", "postgres", "--force", "--postgres-version", "18"}, wantImage: "postgres:18-trixie", wantPath: "/var/lib/postgresql"},
		{name: "version requires postgres", args: []string{"--database", "none", "--postgres-version", "18"}, wantErr: `requires database "postgres"`},
		{name: "set requires postgres", args: []string{"--set", "postgres.version=18"}, wantErr: `options are set for "postgres", which is not selected`},
		{name: "addon and set", args: []string{"--addon", "postgres", "--set", "postgres.version=17"}, wantImage: "postgres:17-bookworm", wantPath: "/var/lib/postgresql/data"},
		{name: "alias conflicts with set", args: []string{"--addon", "postgres", "--set", "postgres.version=17", "--postgres-version", "18"}, wantErr: "--postgres-version conflicts with postgres.version=17"},
		{name: "invalid version", args: []string{"--database", "postgres", "--postgres-version", "18.1"}, wantErr: "major version"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", t.TempDir())
			if tt.legacy {
				writeLegacyPostgresProject(t, root)
			}
			args := append([]string{"--non-interactive", "--preset", "node", "--name", "legacy-db", "--ai", "none"}, tt.args...)
			err := runInit(root, args, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("runInit() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("runInit() error = %v", err)
			}
			assertPostgresCompose(t, filepath.Join(root, ".devcontainer"), tt.wantImage, tt.wantPath)
		})
	}
}
