package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/presets"
)

func TestHostIDPolicy(t *testing.T) {
	contract := &presets.Ownership{Mode: "fixed", UID: 1001, GID: 1002}
	for _, tt := range []struct {
		host     string
		uid, gid int
		valid    bool
	}{
		{"linux", 1001, 1002, true}, {"linux", 1000, 1002, false}, {"linux", 1001, 1000, false},
		{"darwin", 501, 20, true}, {"windows", 1001, 1002, false},
	} {
		if problem := hostIDProblem(tt.host, tt.uid, tt.gid, contract); (problem == "") != tt.valid {
			t.Errorf("%+v: %s", tt, problem)
		}
	}
}

func TestHostDirectoryAccess(t *testing.T) {
	source := t.TempDir()
	if err := hostDirectoryAccess(source); err != nil {
		t.Fatal(err)
	}
	if err := hostDirectoryAccess(filepath.Join(source, "missing")); err == nil {
		t.Fatal("missing source accepted")
	}
	file := filepath.Join(source, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := hostDirectoryAccess(file); err == nil {
		t.Fatal("file source accepted")
	}
	if os.Getuid() == 0 {
		t.Skip("root bypasses DAC")
	}
	if err := os.Chmod(source, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(source, 0700) })
	if err := hostDirectoryAccess(source); err == nil {
		t.Fatal("unwritable source accepted")
	}
}

func TestFixedMergedMetadataUIDPolicy(t *testing.T) {
	fixed := false
	want := devcontainerDocument{UpdateRemoteUserUID: &fixed, ContainerUser: "vscode", RemoteUser: "vscode", Features: map[string]map[string]any{}, PostCreateCommand: "setup"}
	for _, value := range []any{false, true, nil, "false"} {
		m := map[string]any{"containerUser": "vscode", "remoteUser": "vscode", "features": want.Features, "postCreateCommands": []string{"setup"}, "onCreateCommands": []string{}, "updateContentCommands": []string{}, "postStartCommands": []string{}, "postAttachCommands": []string{}}
		if value != nil {
			m["updateRemoteUserUID"] = value
		}
		data, _ := json.Marshal(map[string]any{"mergedConfiguration": m})
		var failures []string
		validateMergedMetadata(data, want, func(f string, args ...any) { failures = append(failures, fmt.Sprintf(f, args...)) })
		if (len(failures) == 0) != (value == false) {
			t.Errorf("%v: %v", value, failures)
		}
	}
}

func TestOwnershipOnlyRuntimeProbe(t *testing.T) {
	for _, failure := range []string{"", "docker exec --user vscode --env BASH_ENV= --env ENV= owned-probe /bin/bash --noprofile --norc -c set -eu; test"} {
		r := &runtimeRunner{metadataRunner: metadataRunner{failure: failure}}
		var failures []string
		validateRuntime("built-image", nil, &presets.Ownership{Mode: "fixed", UID: 1001, GID: 1002}, devcontainerDocument{}, r, func(s Severity, p, f string, args ...any) { failures = append(failures, fmt.Sprintf(f, args...)) })
		joined := strings.Join(r.commands, "\n")
		if !strings.Contains(joined, `test "$(id -u)" = 1001; test "$(id -g)" = 1002`) {
			t.Fatalf("missing ID probe: %s", joined)
		}
		if !strings.Contains(joined, "docker rm --force --volumes owned-probe") {
			t.Fatal("missing cleanup")
		}
		if (len(failures) == 0) != (failure == "") {
			t.Errorf("failures: %v", failures)
		}
	}
}
