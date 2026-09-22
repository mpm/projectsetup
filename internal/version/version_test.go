package version

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestInfo(t *testing.T) {
	originalVersion, originalCommit, originalDate := Version, Commit, Date
	t.Cleanup(func() { Version, Commit, Date = originalVersion, originalCommit, originalDate })
	Version, Commit, Date = "v1.2.3", "1234567890", "2026-08-28T12:00:00Z"
	for _, want := range []string{"projectsetup v1.2.3", "commit: 1234567", "built: 2026-08-28T12:00:00Z"} {
		if !strings.Contains(Info(), want) {
			t.Fatalf("Info() = %q, want %q", Info(), want)
		}
	}
}

func TestBuildVersion(t *testing.T) {
	for _, tc := range []struct {
		name, injected, module string
		dirty                  bool
		want                   string
	}{
		{"release", "v1.2.3", "", false, "v1.2.3"},
		{"go install", "dev", "v1.2.3", false, "v1.2.3"},
		{"local", "dev", "(devel)", false, ""},
		{"empty", "", "", false, ""},
		{"invalid", "broken", "", false, ""},
		{"dirty tag", "v1.2.3-dirty", "", false, ""},
		{"dirty vcs", "v1.2.3", "", true, ""},
		{"prerelease", "v1.2.3-rc.1", "", false, "v1.2.3-rc.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &debug.BuildInfo{Main: debug.Module{Version: tc.module}}
			if tc.dirty {
				info.Settings = []debug.BuildSetting{{Key: "vcs.modified", Value: "true"}}
			}
			if got := buildVersion(tc.injected, info); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
