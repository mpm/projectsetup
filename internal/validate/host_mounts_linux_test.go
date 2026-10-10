//go:build linux

package validate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHostIntegrationAccessModes(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(filepath.Dir(directory), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "file")
	if err := os.WriteFile(file, nil, 0644); err != nil {
		t.Fatal(err)
	}
	// Deliberately unrelated numeric IDs make these tests independent of root,
	// supplementary host groups, and the account running the tests.
	const uid, gid = 123456, 123456
	for _, tc := range []struct {
		kind    string
		ro, bad bool
	}{{"file", true, false}, {"file", false, true}, {"socket", true, true}} {
		if err := hostIntegrationAccess(file, tc.kind, tc.ro, uid, gid); (err != nil) != tc.bad {
			t.Errorf("%+v: %v", tc, err)
		}
	}
	if err := os.Chmod(directory, 0777); err != nil {
		t.Fatal(err)
	}
	if err := hostIntegrationAccess(directory, "directory", false, uid, gid); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0744); err != nil {
		t.Fatal(err)
	}
	if err := hostIntegrationAccess(directory, "directory", true, uid, gid); err == nil {
		t.Fatal("directory without search accepted")
	}
	if err := hostIntegrationAccess(file, "file", true, uid, gid); err == nil {
		t.Fatal("ancestor without search accepted")
	}
}
