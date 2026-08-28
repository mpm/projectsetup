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
