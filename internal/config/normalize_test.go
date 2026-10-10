package config

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/presets"
)

func definitions(names ...string) []presets.Definition {
	result := make([]presets.Definition, len(names))
	for i, name := range names {
		result[i], _ = presets.Builtin().Lookup(name)
	}
	return result
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		name    string
		input   Input
		want    Config
		wantErr string
	}{
		{
			name: "node defaults and deterministic sets",
			input: Input{
				Root:           "/tmp/My Project",
				Preset:         "node",
				Ports:          []int{3000, 1024, 3000},
				SystemPackages: []string{"libpq-dev", "curl", "libpq-dev"},
			},
			want: Config{
				ProjectName: "my-project", Preset: "node", Addons: []string{},
				Options:     map[string]map[string]string{"node": {"package_manager": "npm", "version": "26"}},
				Definitions: definitions("node"),
				AITools:     []AITool{AIToolOpenCode},
				Ports:       []int{1024, 3000}, SystemPackages: []string{"curl", "libpq-dev"},
				Workspace: Workspace{HostPath: "/tmp/My Project", ContainerPath: "/workspaces/my-project"},
				Container: Container{User: "vscode", Home: "/home/vscode", ComposeProjectName: "my-project", ServiceName: "app"},
			},
		},
		{
			name: "add-ons are sorted and deduplicated",
			input: Input{Root: "/tmp/api", Preset: "python", Addons: []string{"sqlite", "postgres", "sqlite"},
				AITools: []AITool{}, Options: map[string]map[string]string{"python": {"package_manager": "uv"}, "postgres": {"version": " 17 "}}},
			want: Config{
				ProjectName: "api", Preset: "python", Addons: []string{"postgres", "sqlite"},
				Options: map[string]map[string]string{
					"python":   {"package_manager": "uv", "version": "3.14"},
					"postgres": {"version": "17"},
				},
				Definitions: definitions("python", "postgres", "sqlite"),
				AITools:     []AITool{}, Ports: []int{}, SystemPackages: []string{},
				Workspace: Workspace{HostPath: "/tmp/api", ContainerPath: "/workspaces/api"},
				Container: Container{User: "vscode", Home: "/home/vscode", ComposeProjectName: "api", ServiceName: "app"},
			},
		},
		{
			name:  "ruby defaults",
			input: Input{Root: "/tmp/gem", Preset: "ruby", AITools: []AITool{}},
			want: Config{
				ProjectName: "gem", Preset: "ruby", Addons: []string{},
				Options:     map[string]map[string]string{"ruby": {"version": "4.0"}},
				Definitions: definitions("ruby"),
				AITools:     []AITool{}, Ports: []int{}, SystemPackages: []string{},
				Workspace: Workspace{HostPath: "/tmp/gem", ContainerPath: "/workspaces/gem"},
				Container: Container{User: "vscode", Home: "/home/vscode", ComposeProjectName: "gem", ServiceName: "app"},
			},
		},
		{name: "rejects missing preset", input: Input{Root: "/tmp/api"}, wantErr: "preset is required"},
		{name: "rejects unknown preset", input: Input{Root: "/tmp/api", Preset: "go"}, wantErr: `unsupported preset "go"`},
		{name: "rejects add-on as preset", input: Input{Root: "/tmp/api", Preset: "postgres"}, wantErr: `unsupported preset "postgres"`},
		{name: "rejects preset as add-on", input: Input{Root: "/tmp/api", Preset: "node", Addons: []string{"ruby"}}, wantErr: `unsupported add-on "ruby"`},
		{name: "rejects incompatible manager", input: Input{Root: "/tmp/api", Preset: "python", Options: map[string]map[string]string{"python": {"package_manager": "npm"}}}, wantErr: `"npm" is not one of pip, poetry, uv`},
		{name: "rejects manager for ruby", input: Input{Root: "/tmp/gem", Preset: "ruby", Options: map[string]map[string]string{"ruby": {"package_manager": "npm"}}}, wantErr: `ruby has no option "package_manager"`},
		{name: "rejects options for unselected definition", input: Input{Root: "/tmp/app", Preset: "node", Options: map[string]map[string]string{"postgres": {"version": "18"}}}, wantErr: `options are set for "postgres", which is not selected`},
		{name: "rejects invalid postgres version", input: Input{Root: "/tmp/app", Preset: "node", Addons: []string{"postgres"}, Options: map[string]map[string]string{"postgres": {"version": "18.1"}}}, wantErr: `option postgres.version (PostgreSQL major version): "18.1" does not match`},
		{name: "rejects invalid port", input: Input{Root: "/tmp/api", Preset: "python", Ports: []int{70000}}, wantErr: "outside the valid range"},
		{name: "rejects unsafe system package", input: Input{Root: "/tmp/api", Preset: "python", SystemPackages: []string{"curl; false"}}, wantErr: "not a valid apt package"},
		{name: "rejects unsafe language version", input: Input{Root: "/tmp/api", Preset: "rails", Options: map[string]map[string]string{"rails": {"version": "3.3\nRUN false"}}}, wantErr: "option rails.version"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Normalize(tt.input)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Normalize() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Normalize() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Normalize() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestNormalizeUsesGivenRegistry(t *testing.T) {
	custom, err := presets.Parse([]byte("schema = 1\nkind = \"preset\"\nname = \"custom\"\nversion = \"1.2.3\"\ndescription = \"d\"\n[image]\nbase = \"debian\"\n"), "custom.toml")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := presets.NewRegistry(custom)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Normalize(Input{Root: "/tmp/app", Registry: registry, Preset: "custom"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Preset != "custom" || len(cfg.Options) != 0 {
		t.Fatalf("Normalize() = %+v", cfg)
	}
	if _, err := Normalize(Input{Root: "/tmp/app", Registry: registry, Preset: "node"}); err == nil || !strings.Contains(err.Error(), "expected custom") {
		t.Fatalf("Normalize(node) error = %v", err)
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"my-app", "my-app"},
		{"my_app_", "my_app_"},
		{"0app", "0app"},
		{"My API", "my-api"},
		{"  example.com_web  ", "example-com_web"},
		{"a.b", "a-b"},
		{"über", "ber"},
		{"-dash", "dash"},
		{"x/y", "x-y"},
		{"_lead", "lead"},
		{"---", ""},
		{"ü", ""},
	}
	for _, tt := range tests {
		got := SanitizeName(tt.input)
		if got != tt.want {
			t.Errorf("SanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
		}
		if got != "" && !ValidProjectName(got) {
			t.Errorf("SanitizeName(%q) = %q is not a valid project name", tt.input, got)
		}
	}
}

func TestValidateProjectName(t *testing.T) {
	tests := []struct {
		input      string
		valid      bool
		suggestion string
	}{
		{input: "my-app", valid: true},
		{input: "my_app", valid: true},
		{input: "app2", valid: true},
		{input: "9lives", valid: true},
		{input: "trailing-", valid: true},
		{input: "My App", suggestion: "my-app"},
		{input: "a.b", suggestion: "a-b"},
		{input: "über", suggestion: "ber"},
		{input: "-dash", suggestion: "dash"},
		{input: "x/y", suggestion: "x-y"},
		{input: "_under", suggestion: "under"},
		{input: " app", suggestion: "app"},
		{input: "ü"},
		{input: ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := ValidateProjectName(tt.input)
			if ValidProjectName(tt.input) != tt.valid {
				t.Fatalf("ValidProjectName(%q) = %v, want %v", tt.input, !tt.valid, tt.valid)
			}
			if tt.valid {
				if err != nil {
					t.Fatalf("ValidateProjectName(%q) error = %v", tt.input, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateProjectName(%q) succeeded, want error", tt.input)
			}
			if !strings.Contains(err.Error(), ProjectNamePattern) {
				t.Errorf("error %q does not name the allowed pattern", err)
			}
			if tt.input != "" && !strings.Contains(err.Error(), fmt.Sprintf("%q", tt.input)) {
				t.Errorf("error %q does not name the invalid value", err)
			}
			hasSuggestion := strings.Contains(err.Error(), "instead")
			if tt.suggestion == "" && hasSuggestion {
				t.Errorf("error %q suggests an alternative, want none", err)
			}
			if tt.suggestion != "" && !strings.Contains(err.Error(), fmt.Sprintf("use %q instead", tt.suggestion)) {
				t.Errorf("error %q does not suggest %q", err, tt.suggestion)
			}
		})
	}
}

func TestNormalizeProjectNameIsConsistent(t *testing.T) {
	tests := []struct {
		name    string
		input   Input
		want    string
		wantErr bool
	}{
		{name: "explicit valid name", input: Input{Root: "/tmp/whatever", ProjectName: "my_app-2"}, want: "my_app-2"},
		{name: "derived from directory", input: Input{Root: "/tmp/my-app"}, want: "my-app"},
		{name: "derived directory with dots is normalized", input: Input{Root: "/tmp/a.b"}, want: "a-b"},
		{name: "derived directory with spaces is normalized", input: Input{Root: "/tmp/My App"}, want: "my-app"},
		{name: "derived directory without usable characters", input: Input{Root: "/tmp/üü"}, wantErr: true},
		{name: "explicit name with dot is rejected", input: Input{Root: "/tmp/x", ProjectName: "a.b"}, wantErr: true},
		{name: "explicit name with space is rejected", input: Input{Root: "/tmp/x", ProjectName: "My App"}, wantErr: true},
		{name: "explicit name with umlaut is rejected", input: Input{Root: "/tmp/x", ProjectName: "über"}, wantErr: true},
		{name: "explicit name with leading dash is rejected", input: Input{Root: "/tmp/x", ProjectName: "-dash"}, wantErr: true},
		{name: "explicit name with slash is rejected", input: Input{Root: "/tmp/x", ProjectName: "x/y"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.input.Preset = "node"
			got, err := Normalize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Normalize() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.ProjectName != tt.want || got.Container.ComposeProjectName != tt.want || got.Workspace.ContainerPath != "/workspaces/"+tt.want {
				t.Fatalf("names = %q, %q, %q; want %q everywhere", got.ProjectName, got.Container.ComposeProjectName, got.Workspace.ContainerPath, tt.want)
			}
			if manifest := NewManifest(got); manifest.ProjectName != tt.want {
				t.Fatalf("manifest projectName = %q, want %q", manifest.ProjectName, tt.want)
			}
		})
	}
}

func TestNormalizeAITools(t *testing.T) {
	for _, tt := range []struct {
		input, want []AITool
		fail        bool
	}{
		{nil, []AITool{AIToolOpenCode}, false},
		{[]AITool{}, []AITool{}, false},
		{[]AITool{AIToolOpenCode, AIToolCodex, AIToolClaude, AIToolCodex}, []AITool{AIToolClaude, AIToolCodex, AIToolOpenCode}, false},
		{[]AITool{AIToolCodex}, []AITool{AIToolCodex}, false},
		{[]AITool{"bogus"}, nil, true},
	} {
		got, err := normalizeAITools(tt.input)
		if (err != nil) != tt.fail || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%v: got %v, %v", tt.input, got, err)
		}
	}
}

func TestCodexHostDirectory(t *testing.T) {
	for _, tt := range []struct {
		override, want string
		fail           bool
	}{
		{"", "/host/.codex", false}, {"/custom/codex home", "/custom/codex home", false}, {"relative", "", true},
	} {
		got, err := AIHostDirectory(".codex", "/host", func(string) string { return tt.override })
		if (err != nil) != tt.fail || got != tt.want {
			t.Errorf("%q: %q, %v", tt.override, got, err)
		}
	}
	got, err := AIHostDirectory(".local/share/codex", "/host", func(string) string { return "/custom/state" })
	if err != nil || got != "/host/.local/share/codex" {
		t.Fatalf("installation directory = %q, %v", got, err)
	}
}
