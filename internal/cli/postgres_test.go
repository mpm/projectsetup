package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/presets"
	"github.com/mpm/projectsetup/internal/validate"
)

func TestExactPostgresRegeneration(t *testing.T) {
	for _, major := range []string{"17", "18"} {
		t.Run(major, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", t.TempDir())
			ref := "postgres:" + major + ".1@sha256:" + strings.Repeat("c", 64)
			args := []string{"--non-interactive", "--preset", "node", "--name", "exact-db", "--ai", "none", "--addon", "postgres"}
			if err := runInit(root, append(append([]string{}, args...), "--postgres-version", major, "--postgres-image", ref), &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			path := "/var/lib/postgresql"
			if major == "17" {
				path += "/data"
			}
			snapshotPath := filepath.Join(root, ".devcontainer", "presets", "postgres.toml")
			snapshot, err := os.ReadFile(snapshotPath)
			if err != nil {
				t.Fatal(err)
			}
			// Ordinary upgrade must use only the project snapshots, even when loading
			// the global registry would fail. Neither check nor upgrade resolves a tag.
			broken := t.TempDir()
			t.Setenv(presets.ConfigDirEnv, broken)
			if err := os.MkdirAll(filepath.Join(broken, "presets"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(broken, "presets", "bad.toml"), []byte("invalid"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := runUpgrade(root, nil, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			assertPostgresCompose(t, filepath.Join(root, ".devcontainer"), ref, path)
			if m := readManifest(t, root); string(m.PostgresImage) != ref || m.Options["postgres"]["version"] != major {
				t.Fatalf("upgrade lost pin: %+v", m)
			}
			current, err := os.ReadFile(snapshotPath)
			if err != nil || !bytes.Equal(snapshot, current) {
				t.Fatalf("snapshot changed: %v", err)
			}
			t.Setenv(presets.ConfigDirEnv, t.TempDir())
			if err := runInit(root, append(append([]string{}, args...), "--force"), &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			assertPostgresCompose(t, filepath.Join(root, ".devcontainer"), ref, path)
			report := validate.Check(root, validate.Options{})
			if validate.ErrorCount(report) != 0 {
				t.Fatalf("check failed: %+v", report)
			}
			// A contradictory explicit major must fail without changing generated data.
			before, err := os.ReadFile(filepath.Join(root, ".devcontainer", "projectsetup.json"))
			if err != nil {
				t.Fatal(err)
			}
			err = runInit(root, append(append([]string{}, args...), "--force", "--postgres-version", "19"), &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), "declares major") {
				t.Fatalf("inconsistent force error=%v", err)
			}
			after, err := os.ReadFile(filepath.Join(root, ".devcontainer", "projectsetup.json"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("failed force changed manifest")
			}
			replacement := "postgres:" + major + ".2"
			if err := runInit(root, append(append([]string{}, args...), "--force", "--postgres-image", replacement), &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			assertPostgresCompose(t, filepath.Join(root, ".devcontainer"), replacement, path)
			// Explicitly empty image restores the snapshot's default image, preserving
			// the selected major. Removing the addon must also remove the override.
			if err := runInit(root, append(append([]string{}, args...), "--force", "--postgres-image", ""), &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			if m := readManifest(t, root); m.PostgresImage != "" || m.Options["postgres"]["version"] != major {
				t.Fatalf("clear pin=%+v", m)
			}
			if err := runInit(root, append(append([]string{}, args...), "--force", "--postgres-image", replacement), &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			if err := runInit(root, []string{"--non-interactive", "--preset", "node", "--name", "exact-db", "--ai", "none", "--force"}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			if m := readManifest(t, root); m.PostgresImage != "" {
				t.Fatal("reference retained without addon")
			}
		})
	}
}

func TestExactPostgresCLIRejectsInvalidSelection(t *testing.T) {
	for _, args := range [][]string{
		{"--postgres-image", "postgres:18.1"},
		{"--addon", "postgres", "--postgres-image", "postgres:17.1"},
		{"--addon", "postgres", "--postgres-image", "postgres:18.1@sha256:bad"},
	} {
		root := t.TempDir()
		err := runInit(root, append([]string{"--non-interactive", "--preset", "node", "--name", "db", "--ai", "none"}, args...), &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "postgresImage") {
			t.Fatalf("args=%v error=%v", args, err)
		}
		if _, err := os.Stat(filepath.Join(root, ".devcontainer")); !os.IsNotExist(err) {
			t.Fatal("invalid selection wrote configuration")
		}
	}
	// Legacy schema 1 cannot acquire the new field silently.
	_, err := config.ReadManifest(strings.NewReader(`{"schemaVersion":1,"postgresImage":"postgres:18.1"}`))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("legacy accepted new field: %v", err)
	}
}
