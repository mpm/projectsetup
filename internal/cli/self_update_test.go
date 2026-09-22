package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/version"
)

func TestSelfUpdateOutsideWorkspace(t *testing.T) {
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	previous := version.Version
	version.Version = "dev"
	t.Cleanup(func() { version.Version = previous })
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"self-update"}, "development builds"},
		{[]string{"self-update", "unexpected"}, "accepts no arguments"},
		{[]string{"self-update", "--unknown"}, "flag provided but not defined"},
		{[]string{"self-update", "--help"}, ""},
	} {
		var stdout, stderr bytes.Buffer
		err := Run(tc.args, strings.NewReader(""), &stdout, &stderr)
		if tc.want == "" {
			if err != nil {
				t.Fatal(err)
			}
		} else if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%v: %v", tc.args, err)
		}
	}
}
