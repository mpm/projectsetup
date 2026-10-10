package validate

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/presets"
)

func TestReportedRuntimeVersions(t *testing.T) {
	for _, tt := range []struct{ tool, release, output string }{
		{"node", "22.14.0", "v22.14.0\n"},
		{"ruby", "3.4.1", "ruby 3.4.1 (2025-01-01 revision abc) [x86_64-linux]"},
		{"python", "3.14.0rc1", "Python 3.14.0rc1\n"},
		{"go", "1.27.2", "go version go1.27.2 linux/amd64"},
		{"rust", "1.99.0", "rustc 1.99.0 (abcdef 2026-01-01)"},
		{"gh", "2.67.0", "gh version 2.67.0 (2025-02-11)\nhttps://github.com/cli/cli/releases/tag/v2.67.0"},
		{"npm", "10.9.2", "10.9.2\n"},
		{"pnpm", "10.1.0", "10.1.0\n"},
		{"yarn", "4.6.0", "4.6.0\n"},
		{"bundler", "2.6.2", "Bundler version 2.6.2"},
		{"pip", "25.0", "pip 25.0 from /usr/lib/python (python 3.13)"},
		{"uv", "0.6.0", "uv 0.6.0 (abcdef 2025-01-01)"},
		{"poetry", "2.1.0", "Poetry (version 2.1.0)"},
		{"cargo", "1.99.0", "cargo 1.99.0 (abcdef 2026-01-01)"},
	} {
		t.Run(tt.tool, func(t *testing.T) {
			pattern := versionPatterns[tt.tool]
			if err := checkReportedVersion(tt.tool, tt.release, []byte(tt.output), pattern); err != nil {
				t.Fatal(err)
			}
			for _, output := range []string{tt.output, "unrelated " + tt.release, "", "garbage"} {
				if err := checkReportedVersion(tt.tool, "0.0.0", []byte(output), pattern); err == nil {
					t.Fatalf("invalid/mismatched output accepted: %q", output)
				}
			}
		})
	}
}

type runtimeRunner struct {
	metadataRunner
	wrongVersion    bool
	missingTool     string
	finalFailure    string
	finalInspection []byte
	finalMerged     []byte
	built           bool
}

func (r *runtimeRunner) LookPath(name string) (string, error) {
	if name == r.missingTool {
		return "", exec.ErrNotFound
	}
	return name, nil
}

func (r *runtimeRunner) Run(name string, args ...string) ([]byte, error) {
	output, err := r.metadataRunner.Run(name, args...)
	if err != nil {
		return output, err
	}
	if name == "devcontainer" && args[0] == "build" {
		r.built = true
		return []byte(`{"imageName":"built-image"}`), nil
	}
	if r.built {
		command := strings.Join(append([]string{name}, args...), " ")
		if r.finalFailure != "" && strings.HasPrefix(command, r.finalFailure) {
			return []byte("final probe failure"), errors.New("failed")
		}
		if name == "docker" && args[0] == "image" && r.finalInspection != nil {
			return r.finalInspection, nil
		}
		if name == "devcontainer" && args[0] == "read-configuration" && r.finalMerged != nil {
			return r.finalMerged, nil
		}
	}
	if name == "docker" && args[0] == "exec" && args[len(args)-1] == "--version" {
		if r.wrongVersion {
			return []byte("v22.14.1\n"), nil
		}
		return []byte("v22.14.0\n"), nil
	}
	return output, nil
}

func runtimeContract() *presets.Preinstalled {
	return &presets.Preinstalled{Tools: map[string]presets.InstalledTool{"node": {Version: "22.14.0", Executable: "/opt/node/bin/node", Path: []string{"/opt/node/bin"}}}}
}

func TestRuntimeProbeFailuresAndIsolation(t *testing.T) {
	for _, tt := range []struct {
		name, failure, expected string
		wrong, custom, cleanup  bool
	}{
		{name: "valid", cleanup: true},
		{name: "create failure", failure: "docker create", expected: "create isolated"},
		{name: "start failure", failure: "docker start", expected: "start isolated", cleanup: true},
		{name: "core failure", failure: "docker exec --user vscode --env BASH_ENV= --env ENV= owned-probe /bin/bash", expected: "core prerequisites", cleanup: true},
		{name: "absolute exec failure", failure: "docker exec --user vscode owned-probe /opt/node/bin/node", expected: "command", cleanup: true},
		{name: "lookup exec failure", failure: "docker exec --user vscode owned-probe node", expected: "command", cleanup: true},
		{name: "wrong release", wrong: true, expected: "differs from declared", cleanup: true},
		{name: "custom tool", custom: true, expected: "no supported version probe", cleanup: true},
		{name: "cleanup failure", failure: "docker rm", expected: "docker rm --force --volumes owned-probe", cleanup: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := &runtimeRunner{metadataRunner: metadataRunner{failure: tt.failure}, wrongVersion: tt.wrong}
			contract := runtimeContract()
			if tt.custom {
				contract.Tools["team-cli"] = presets.InstalledTool{Version: "release_1", Executable: "/usr/bin/team", Path: []string{"/usr/bin"}}
			}
			var diagnostics []Diagnostic
			validateRuntime("built-image", contract, devcontainerDocument{ContainerEnv: map[string]string{"PATH": "/opt/node/bin:/usr/bin", "TEAM": "literal value"}}, r, func(s Severity, p, f string, args ...any) {
				diagnostics = append(diagnostics, Diagnostic{s, p, fmt.Sprintf(f, args...)})
			})
			if tt.expected == "" && len(diagnostics) != 0 || tt.expected != "" && !strings.Contains(fmt.Sprint(diagnostics), tt.expected) {
				t.Fatalf("diagnostics=%v", diagnostics)
			}
			commands := strings.Join(r.commands, "\n")
			if strings.Contains(commands, "docker rm --force --volumes owned-probe") != tt.cleanup {
				t.Fatalf("cleanup: %s", commands)
			}
			if !strings.Contains(commands, "--network none --no-healthcheck --user vscode --workdir /home/vscode") || !strings.Contains(commands, "--entrypoint /bin/sleep built-image infinity") || !strings.Contains(commands, "--env PATH=/opt/node/bin:/usr/bin") || strings.Contains(commands, "--mount") || strings.Contains(commands, "post-create") {
				t.Fatalf("probe isolation: %s", commands)
			}
			if tt.expected == "" && (!strings.Contains(commands, "owned-probe /opt/node/bin/node --version") || !strings.Contains(commands, "owned-probe node --version")) {
				t.Fatalf("missing absolute or lookup version execution: %s", commands)
			}
		})
	}
}

func TestRuntimeExecutionGates(t *testing.T) {
	want, merged := validMetadataDocument()
	for _, tt := range []struct {
		name, failure, missingTool, finalFailure string
		finalInspection, finalMerged             []byte
	}{
		{name: "valid"},
		{name: "missing Docker", missingTool: "docker"},
		{name: "missing Dev Container CLI", missingTool: "devcontainer"},
		{name: "compose failure", failure: "docker compose"},
		{name: "configuration failure", failure: "devcontainer read-configuration"},
		{name: "build failure", failure: "devcontainer build"},
		{name: "inspect failure", failure: "docker image inspect"},
		{name: "merge failure", failure: "devcontainer read-configuration --workspace-folder /project --config"},
		{name: "metadata cleanup failure", failure: "docker rm --volumes"},
		{name: "final inspect failure", finalFailure: "docker image inspect"},
		{name: "final merge failure", finalFailure: "devcontainer read-configuration"},
		{name: "final cleanup failure", finalFailure: "docker rm --volumes"},
		{name: "final root user", finalInspection: []byte(`[{"Config":{"User":"root"}}]`)},
		{name: "final wrong home", finalInspection: []byte(`[{"Config":{"User":"vscode","Env":["HOME=/root"]}}]`)},
		{name: "final volume", finalInspection: []byte(`[{"Config":{"User":"vscode","Volumes":{"/project":{}}}}]`)},
		{name: "final malformed merge", finalMerged: []byte(`{}`)},
		{name: "final inherited setup", finalMerged: []byte(strings.Replace(string(merged), want.PostCreateCommand, "npm ci", 1))},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := &runtimeRunner{metadataRunner: metadataRunner{merged: merged, failure: tt.failure}, missingTool: tt.missingTool, finalFailure: tt.finalFailure, finalInspection: tt.finalInspection, finalMerged: tt.finalMerged}
			var diagnostics []Diagnostic
			validateExternal("/project", "/project/.devcontainer", true, true, presets.Resolved{Base: "shared:fixed", Preinstalled: runtimeContract()}, want, Options{Runner: r, Runtime: true}, func(s Severity, p, f string, a ...any) {
				diagnostics = append(diagnostics, Diagnostic{s, p, fmt.Sprintf(f, a...)})
			})
			executed := strings.Contains(strings.Join(r.commands, "\n"), "docker start")
			if executed != (tt.name == "valid") || (tt.name != "valid" && ErrorCount(diagnostics) == 0) {
				t.Fatalf("diagnostics=%v commands=%v", diagnostics, r.commands)
			}
		})
	}
}

func TestRuntimeAggregatesFailures(t *testing.T) {
	r := &runtimeRunner{metadataRunner: metadataRunner{failure: "docker exec --user vscode --env BASH_ENV= --env ENV= owned-probe /bin/bash --noprofile --norc -c " + coreRuntimeProbe}, wrongVersion: true}
	contract := runtimeContract()
	contract.Tools["ruby"] = presets.InstalledTool{Version: "3.4.1", Executable: "/opt/ruby/bin/ruby", Path: []string{"/opt/ruby/bin"}}
	var diagnostics []Diagnostic
	validateRuntime("built-image", contract, devcontainerDocument{}, r, func(s Severity, p, f string, args ...any) {
		diagnostics = append(diagnostics, Diagnostic{s, p, fmt.Sprintf(f, args...)})
	})
	for _, expected := range []string{"core prerequisites", `tool "node"`, `tool "ruby"`} {
		if !strings.Contains(fmt.Sprint(diagnostics), expected) {
			t.Fatalf("missing independent failure %q: %v", expected, diagnostics)
		}
	}
	if !strings.Contains(strings.Join(r.commands, "\n"), "docker rm --force --volumes owned-probe") {
		t.Fatal("failed probes were not cleaned up")
	}
}

func TestBuildOnlyDoesNotVerifyCapabilities(t *testing.T) {
	for _, contract := range []*presets.Preinstalled{nil, {}, {CorePackages: []presets.CorePackage{"bash"}}, runtimeContract()} {
		want, merged := validMetadataDocument()
		r := &runtimeRunner{metadataRunner: metadataRunner{merged: merged}}
		var diagnostics []Diagnostic
		validateExternal("/project", "/project/.devcontainer", true, true, presets.Resolved{Base: "shared:fixed", Preinstalled: contract}, want, Options{Runner: r}, func(s Severity, p, f string, a ...any) {
			diagnostics = append(diagnostics, Diagnostic{s, p, fmt.Sprintf(f, a...)})
		})
		if ErrorCount(diagnostics) != 0 || strings.Contains(strings.Join(r.commands, "\n"), "docker start") {
			t.Fatalf("build-only changed behavior: %v %v", diagnostics, r.commands)
		}
		claims := (presets.Resolved{Preinstalled: contract}).ConsumesPreinstalledImage()
		if claims && !strings.Contains(fmt.Sprint(diagnostics), "do not verify installed capabilities") {
			t.Fatalf("build success incorrectly implies verification: %v", diagnostics)
		}
		if !claims && len(diagnostics) != 0 {
			t.Fatalf("legacy/empty build gained diagnostics: %v", diagnostics)
		}
	}
}

// Exercise the shell's executable identity check, including a different command
// shadowing the claimed binary and a broken symlink. No Docker is required.
func TestRuntimeLookupShell(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is unavailable")
	}
	for _, name := range []string{"valid", "symlink", "shadowed", "missing", "broken", "nonexecutable"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			claimed := filepath.Join(root, "claimed")
			lookup := filepath.Join(root, "tool-command")
			if err := os.WriteFile(claimed, []byte("#!/bin/bash\nexit 0\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "valid":
				lookup = claimed
			case "symlink":
				if err := os.Symlink(claimed, lookup); err != nil {
					t.Fatal(err)
				}
			case "shadowed":
				if err := os.WriteFile(lookup, []byte("#!/bin/bash\nexit 0\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			case "broken":
				if err := os.Remove(claimed); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(claimed, lookup); err != nil {
					t.Fatal(err)
				}
			case "nonexecutable":
				lookup = claimed
				if err := os.Chmod(claimed, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(bash, "--noprofile", "--norc", "-c", toolLookupProbe, "test", claimed, filepath.Base(lookup))
			cmd.Env = []string{"PATH=" + root}
			output, err := cmd.CombinedOutput()
			if (err == nil) != (name == "valid" || name == "symlink") {
				t.Fatalf("lookup result: %v %s", err, output)
			}
		})
	}
}
