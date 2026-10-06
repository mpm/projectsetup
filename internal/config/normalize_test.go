package config

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name    string
		input   Input
		want    Config
		wantErr bool
	}{
		{
			name: "node defaults and deterministic sets",
			input: Input{
				Root:           "/tmp/My Project",
				Preset:         PresetNode,
				Ports:          []int{3000, 1024, 3000},
				SystemPackages: []string{"libpq-dev", "curl", "libpq-dev"},
			},
			want: Config{
				SchemaVersion: 1, ProjectName: "my-project", Preset: PresetNode,
				Database: DatabaseNone, AITools: []AITool{AIToolOpenCode},
				PackageManager: PackageManagerNPM, LanguageVersion: "24",
				Ports: []int{1024, 3000}, SystemPackages: []string{"curl", "libpq-dev"},
				Workspace: Workspace{HostPath: "/tmp/My Project", ContainerPath: "/workspaces/my-project"},
				Container: Container{User: "vscode", Home: "/home/vscode", ComposeProjectName: "my-project", ServiceName: "app"},
			},
		},
		{
			name: "postgres configuration",
			input: Input{Root: "/tmp/api", Preset: PresetPython, Database: DatabasePostgres,
				AITools: []AITool{}, PackageManager: PackageManagerUV},
			want: Config{
				SchemaVersion: 1, ProjectName: "api", Preset: PresetPython,
				Database: DatabasePostgres, AITools: []AITool{},
				PackageManager: PackageManagerUV, LanguageVersion: "3.13",
				Ports: []int{}, SystemPackages: []string{},
				Workspace: Workspace{HostPath: "/tmp/api", ContainerPath: "/workspaces/api"},
				Container: Container{User: "vscode", Home: "/home/vscode", ComposeProjectName: "api", ServiceName: "app"},
			},
		},
		{
			name:  "ruby defaults",
			input: Input{Root: "/tmp/gem", Preset: PresetRuby, AITools: []AITool{}},
			want: Config{
				SchemaVersion: 1, ProjectName: "gem", Preset: PresetRuby,
				Database: DatabaseNone, AITools: []AITool{},
				LanguageVersion: "3.3", Ports: []int{}, SystemPackages: []string{},
				Workspace: Workspace{HostPath: "/tmp/gem", ContainerPath: "/workspaces/gem"},
				Container: Container{User: "vscode", Home: "/home/vscode", ComposeProjectName: "gem", ServiceName: "app"},
			},
		},
		{
			name:  "sqlite configuration",
			input: Input{Root: "/tmp/app", Preset: PresetRails, Database: DatabaseSQLite, AITools: []AITool{}},
			want: Config{
				SchemaVersion: 1, ProjectName: "app", Preset: PresetRails,
				Database: DatabaseSQLite, AITools: []AITool{},
				LanguageVersion: "3.3", Ports: []int{}, SystemPackages: []string{},
				Workspace: Workspace{HostPath: "/tmp/app", ContainerPath: "/workspaces/app"},
				Container: Container{User: "vscode", Home: "/home/vscode", ComposeProjectName: "app", ServiceName: "app"},
			},
		},
		{name: "rejects incompatible manager", input: Input{Root: "/tmp/api", Preset: PresetPython, PackageManager: PackageManagerNPM}, wantErr: true},
		{name: "rejects manager for ruby", input: Input{Root: "/tmp/gem", Preset: PresetRuby, PackageManager: PackageManagerNPM}, wantErr: true},
		{name: "rejects invalid port", input: Input{Root: "/tmp/api", Preset: PresetPython, Ports: []int{70000}}, wantErr: true},
		{name: "rejects unsafe system package", input: Input{Root: "/tmp/api", Preset: PresetPython, SystemPackages: []string{"curl; false"}}, wantErr: true},
		{name: "rejects unsafe language version", input: Input{Root: "/tmp/api", Preset: PresetRails, LanguageVersion: "3.3\nRUN false"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Normalize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Normalize() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Normalize() = %#v, want %#v", got, tt.want)
			}
		})
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
			tt.input.Preset = PresetNode
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
