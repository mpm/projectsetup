package validate

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/presets"
)

func TestMergedMetadata(t *testing.T) {
	want := devcontainerDocument{ContainerUser: "vscode", RemoteUser: "vscode", ContainerEnv: map[string]string{"PATH": "/opt/node/bin:/usr/bin", "TEAM": "project"}, Features: map[string]map[string]any{}, PostCreateCommand: ".devcontainer/scripts/post-create.sh"}
	for _, tt := range []struct {
		name    string
		mutate  func(map[string]any)
		failure string
	}{
		{"valid", func(m map[string]any) {}, ""},
		{"invalid command array", func(m map[string]any) { m["postCreateCommands"] = want.PostCreateCommand }, "invalid"},
		{"empty hooks", func(m map[string]any) { m["onCreateCommands"] = []any{map[string]any{"0": ""}} }, ""},
		{"duplicate project setup", func(m map[string]any) {
			m["postCreateCommands"] = []string{want.PostCreateCommand, want.PostCreateCommand}
		}, "only"},
		{"inherited dependency setup", func(m map[string]any) { m["onCreateCommands"] = []string{"npm ci"} }, "onCreateCommands"},
		{"inherited server", func(m map[string]any) { m["postStartCommands"] = []any{[]string{"npm", "start"}} }, "postStartCommands"},
		{"inherited root", func(m map[string]any) { m["remoteUser"] = "root" }, "remoteUser"},
		{"inherited UID policy", func(m map[string]any) { m["updateRemoteUserUID"] = false }, "UID adjustment"},
		{"wrong path", func(m map[string]any) { m["containerEnv"] = map[string]string{"PATH": "/wrong"} }, "containerEnv.PATH"},
		{"wrong home", func(m map[string]any) { m["containerEnv"].(map[string]string)["HOME"] = "/root" }, "containerEnv.HOME"},
		{"remote path drift", func(m map[string]any) { m["remoteEnv"] = map[string]string{"PATH": "/wrong"} }, "remoteEnv.PATH"},
		{"remote inherited home", func(m map[string]any) { m["remoteEnv"] = map[string]any{"HOME": nil} }, "remoteEnv.HOME"},
		{"remote passthrough", func(m map[string]any) { m["remoteEnv"] = map[string]string{"PATH": "${containerEnv:PATH}"} }, ""},
		{"reintroduced installer", func(m map[string]any) {
			m["features"] = map[string]any{"ghcr.io/devcontainers/features/node:1": map[string]any{}}
		}, "feature requests"},
		{"credential mount", func(m map[string]any) { m["mounts"] = []string{"source=/host/.ssh,target=/home/vscode/.ssh,type=bind"} }, "mounts"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := map[string]any{"containerUser": "vscode", "remoteUser": "vscode", "containerEnv": map[string]string{"PATH": want.ContainerEnv["PATH"], "TEAM": "project"}, "features": want.Features, "postCreateCommands": []string{want.PostCreateCommand}, "onCreateCommands": []string{}, "updateContentCommands": []string{}, "postStartCommands": []string{}, "postAttachCommands": []string{}}
			tt.mutate(m)
			data, _ := json.Marshal(map[string]any{"mergedConfiguration": m})
			var failures []string
			validateMergedMetadata(data, want, func(format string, args ...any) { failures = append(failures, fmt.Sprintf(format, args...)) })
			if tt.failure == "" && len(failures) != 0 || tt.failure != "" && !strings.Contains(strings.Join(failures, "\n"), tt.failure) {
				t.Fatalf("failures = %v, want %q", failures, tt.failure)
			}
		})
	}
}

type metadataRunner struct {
	commands   []string
	failure    string
	merged     []byte
	inspection []byte
}

func (r *metadataRunner) LookPath(name string) (string, error) { return name, nil }
func (r *metadataRunner) Run(name string, args ...string) ([]byte, error) {
	command := strings.Join(append([]string{name}, args...), " ")
	r.commands = append(r.commands, command)
	if strings.HasPrefix(command, r.failure) && r.failure != "" {
		return []byte("probe failure"), errors.New("failed")
	}
	switch {
	case strings.HasPrefix(command, "docker image inspect"):
		if r.inspection != nil {
			return r.inspection, nil
		}
		return []byte(`[{"Config":{"User":"vscode","Env":["HOME=/home/vscode"]}}]`), nil
	case strings.HasPrefix(command, "docker create"):
		return []byte("owned-probe\n"), nil
	case strings.HasPrefix(command, "devcontainer read"):
		return r.merged, nil
	}
	return nil, nil
}

func TestBuiltMetadataProbeFailuresAndCleanup(t *testing.T) {
	for _, tt := range []struct {
		name, build, failure, expected string
		cleanup                        bool
	}{
		{"missing build result", `{}`, "", "imageName", false},
		{"invalid build result", `bad`, "", "imageName", false},
		{"inspect failure", `{"imageName":["built-image"]}`, "docker image inspect", "inspect image", false},
		{"create failure", `{"imageName":["built-image"]}`, "docker create", "create stopped", false},
		{"merge failure", `{"imageName":["built-image"]}`, "devcontainer read", "effective built metadata", true},
		{"invalid merged output", `{"imageName":["built-image"]}`, "", "mergedConfiguration", true},
		{"cleanup failure", `{"imageName":["built-image"]}`, "docker rm", "remove stopped", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := &metadataRunner{failure: tt.failure}
			var failures []string
			validateBuiltMetadata("/project", "/project/.devcontainer", []byte(tt.build), devcontainerDocument{}, r, func(_ Severity, _, format string, args ...any) {
				failures = append(failures, fmt.Sprintf(format, args...))
			})
			if !strings.Contains(strings.Join(failures, "\n"), tt.expected) {
				t.Fatalf("failures = %v", failures)
			}
			cleaned := false
			for _, cmd := range r.commands {
				if cmd == "docker rm --volumes owned-probe" {
					cleaned = true
				}
				if strings.Contains(cmd, " start ") || strings.Contains(cmd, " up ") {
					t.Fatalf("probe started: %s", cmd)
				}
			}
			if cleaned != tt.cleanup {
				t.Fatalf("commands = %v, cleanup want %v", r.commands, tt.cleanup)
			}
		})
	}
}

func validMetadataDocument() (devcontainerDocument, []byte) {
	want := devcontainerDocument{ContainerUser: "vscode", RemoteUser: "vscode", ContainerEnv: map[string]string{"PATH": "/usr/bin"}, Features: map[string]map[string]any{}, PostCreateCommand: ".devcontainer/scripts/post-create.sh"}
	merged, _ := json.Marshal(map[string]any{"mergedConfiguration": map[string]any{"containerUser": "vscode", "remoteUser": "vscode", "containerEnv": want.ContainerEnv, "features": want.Features, "onCreateCommands": []string{}, "updateContentCommands": []string{}, "postCreateCommands": []string{want.PostCreateCommand}, "postStartCommands": []string{}, "postAttachCommands": []string{}}})
	return want, merged
}

func TestBaseMetadataContract(t *testing.T) {
	for _, tt := range []struct{ name, label, failure string }{
		{"absent", "", ""},
		{"empty array", "[]", ""},
		{"legacy object", `{"id":"ghcr.io/devcontainers/features/node:1"}`, ""},
		{"baked features", `[{"id":"ghcr.io/devcontainers/features/node@sha256:abc"},{"id":"example.com/opaque:2","customizations":{"vscode":{"extensions":["example.extension"]}}}]`, ""},
		{"empty lifecycle", `[{"onCreateCommand":{},"postCreateCommand":"","mounts":[]}]`, ""},
		{"malformed", `{broken`, "invalid devcontainer.metadata"},
		{"scalar", `"wrong"`, "invalid devcontainer.metadata"},
		{"null", `null`, "not null"},
		{"null entry", `[null]`, "must be an object"},
		{"setup string", `[{"postCreateCommand":"bundle install"}]`, "postCreateCommand"},
		{"setup argv", `[{"onCreateCommand":["npm","ci"]}]`, "onCreateCommand"},
		{"setup parallel", `[{"updateContentCommand":{"dependencies":"uv sync"}}]`, "updateContentCommand"},
		{"same project hook", `[{"postCreateCommand":".devcontainer/scripts/post-create.sh"}]`, "postCreateCommand"},
		{"host initialize", `[{"initializeCommand":"touch /host"}]`, "initializeCommand"},
		{"attach hook", `[{"postAttachCommand":"echo hello"}]`, "postAttachCommand"},
		{"server", `[{"postStartCommand":"npm start"}]`, "postStartCommand"},
		{"credential mount", `[{"mounts":["source=/host/.ssh,target=/home/vscode/.ssh,type=bind"]}]`, "mounts"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			want, merged := validMetadataDocument()
			inspection, _ := json.Marshal([]any{map[string]any{"Config": map[string]any{"Labels": map[string]string{"devcontainer.metadata": tt.label}}}})
			r := &metadataRunner{inspection: inspection, merged: merged}
			var diagnostics []Diagnostic
			valid := validateBaseMetadata("/project", "/project/.devcontainer", "shared:fixed", want, r, func(severity Severity, path, format string, args ...any) {
				diagnostics = append(diagnostics, Diagnostic{severity, path, fmt.Sprintf(format, args...)})
			})
			if tt.failure == "" && (!valid || len(diagnostics) > 0) || tt.failure != "" && (valid || !strings.Contains(fmt.Sprint(diagnostics), tt.failure)) {
				t.Fatalf("valid=%v diagnostics=%v, want %q", valid, diagnostics, tt.failure)
			}
			if tt.failure != "" && len(r.commands) != 1 {
				t.Fatalf("unsafe base probed or built: %v", r.commands)
			}
		})
	}
}

func TestMetadataInspectionGatesAndLegacy(t *testing.T) {
	for _, tt := range []struct {
		name     string
		contract *presets.Preinstalled
		build    bool
		inspect  bool
	}{
		{"legacy", nil, true, false},
		{"empty", &presets.Preinstalled{}, true, false},
		{"static shared", &presets.Preinstalled{CorePackages: []presets.CorePackage{"bash"}}, false, false},
		{"build shared", &presets.Preinstalled{CorePackages: []presets.CorePackage{"bash"}}, true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			want, merged := validMetadataDocument()
			r := &metadataRunner{merged: merged, inspection: []byte(`[{"Config":{"Labels":{"devcontainer.metadata":"[{\"postCreateCommand\":\"npm ci\"}]"}}}]`)}
			var diagnostics []Diagnostic
			validateExternal("/project", "/project/.devcontainer", false, tt.build, presets.Resolved{Base: "shared:fixed", Preinstalled: tt.contract}, want, Options{Runner: r}, func(severity Severity, path, format string, args ...any) {
				diagnostics = append(diagnostics, Diagnostic{severity, path, fmt.Sprintf(format, args...)})
			})
			inspected, built := false, false
			for _, cmd := range r.commands {
				if strings.HasPrefix(cmd, "docker image inspect") {
					inspected = true
				}
				if strings.HasPrefix(cmd, "devcontainer build") {
					built = true
				}
			}
			if inspected != tt.inspect || built != (tt.build && !tt.inspect) {
				t.Fatalf("commands=%v", r.commands)
			}
			if tt.inspect && ErrorCount(diagnostics) == 0 {
				t.Fatalf("unsafe base accepted: %v", diagnostics)
			}
			if !tt.inspect && ErrorCount(diagnostics) != 0 {
				t.Fatalf("legacy/static check failed: %v", diagnostics)
			}
			if tt.name == "static shared" && (len(diagnostics) == 0 || diagnostics[0].Severity != Warning) {
				t.Fatalf("missing uninspected metadata warning: %v", diagnostics)
			}
		})
	}
}

func TestBuiltImageUserHomeAndResultForms(t *testing.T) {
	for _, tt := range []struct{ name, build, inspection, failure string }{
		{"compose string", `{"imageName":"built-image"}`, `[{"Config":{"User":"vscode","Env":["HOME=/home/vscode"]}}]`, ""},
		{"image array", `{"imageName":["built-image"]}`, `[{"Config":{"User":"vscode"}}]`, ""},
		{"wrong user", `{"imageName":"built-image"}`, `[{"Config":{"User":"root"}}]`, "effective user"},
		{"wrong home", `{"imageName":"built-image"}`, `[{"Config":{"User":"vscode","Env":["HOME=/root"]}}]`, "HOME"},
		{"wrong USER env", `{"imageName":"built-image"}`, `[{"Config":{"User":"vscode","Env":["USER=root"]}}]`, "USER"},
		{"invalid inspect", `{"imageName":"built-image"}`, `{}`, "inspection document"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			want, merged := validMetadataDocument()
			r := &metadataRunner{merged: merged, inspection: []byte(tt.inspection)}
			var failures []string
			validateBuiltMetadata("/project", "/project/.devcontainer", []byte(tt.build), want, r, func(_ Severity, _, format string, args ...any) {
				failures = append(failures, fmt.Sprintf(format, args...))
			})
			if tt.failure == "" && len(failures) != 0 || tt.failure != "" && !strings.Contains(strings.Join(failures, "\n"), tt.failure) {
				t.Fatalf("failures=%v", failures)
			}
			for _, cmd := range r.commands {
				if strings.HasPrefix(cmd, "docker create") && (!strings.Contains(cmd, "devcontainer.local_folder=/project") || !strings.Contains(cmd, "devcontainer.config_file=/project/.devcontainer/devcontainer.json")) {
					t.Fatalf("final probe lacks project identity labels: %s", cmd)
				}
			}
		})
	}
}

func TestMergedAIMountInterpolation(t *testing.T) {
	t.Setenv("HOME", "/test-host")
	want, data := validMetadataDocument()
	want.Mounts = []string{"source=${localEnv:HOME}/.opencode,target=/home/vscode/.opencode,type=bind"}
	var result map[string]map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	result["mergedConfiguration"]["mounts"] = []string{"source=/test-host/.opencode,target=/home/vscode/.opencode,type=bind"}
	data, _ = json.Marshal(result)
	validateMergedMetadata(data, want, func(format string, args ...any) { t.Errorf(format, args...) })
}

func TestBaseDockerVolumesRejectedBeforeProbe(t *testing.T) {
	want, merged := validMetadataDocument()
	r := &metadataRunner{inspection: []byte(`[{"Config":{"Volumes":{"/project":{}}}}]`), merged: merged}
	valid := validateBaseMetadata("/project", "/project/.devcontainer", "shared:fixed", want, r, func(_ Severity, _, format string, args ...any) {
		if !strings.Contains(fmt.Sprintf(format, args...), "VOLUME") {
			t.Errorf(format, args...)
		}
	})
	if valid || len(r.commands) != 1 {
		t.Fatalf("valid=%v commands=%v", valid, r.commands)
	}
}

func TestComposeFailurePreventsBuildAndMetadataProbes(t *testing.T) {
	want, merged := validMetadataDocument()
	r := &metadataRunner{merged: merged, failure: "docker compose"}
	var diagnostics []Diagnostic
	validateExternal("/project", "/project/.devcontainer", true, true, presets.Resolved{Preinstalled: &presets.Preinstalled{CorePackages: []presets.CorePackage{"bash"}}}, want, Options{Runner: r}, func(severity Severity, path, format string, args ...any) {
		diagnostics = append(diagnostics, Diagnostic{severity, path, fmt.Sprintf(format, args...)})
	})
	if ErrorCount(diagnostics) == 0 {
		t.Fatal("missing compose failure")
	}
	for _, cmd := range r.commands {
		if strings.HasPrefix(cmd, "devcontainer build") || strings.HasPrefix(cmd, "docker create") || strings.HasPrefix(cmd, "docker image inspect") {
			t.Fatalf("build/probe ran after compose failure: %v", r.commands)
		}
	}
}
