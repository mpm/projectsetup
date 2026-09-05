package config

import (
	"reflect"
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
	tests := map[string]string{
		"My API":              "my-api",
		"  example.com_web  ": "example.com_web",
		"---":                 "",
	}
	for input, want := range tests {
		if got := SanitizeName(input); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRuntimeProjectName(t *testing.T) {
	if got, want := RuntimeProjectName("example.com_web"), "example-com_web"; got != want {
		t.Errorf("RuntimeProjectName() = %q, want %q", got, want)
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
