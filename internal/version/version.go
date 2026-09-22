// Package version provides build and release version information.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// These values are injected into release builds with -ldflags -X.
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// Info returns the complete version description shown by the CLI.
func Info() string {
	return fmt.Sprintf("projectsetup %s (commit: %s, built: %s, %s/%s)",
		Short(), shortCommit(), Date, runtime.GOOS, runtime.GOARCH)
}

// Short returns the release version, or dev for a development build.
func Short() string {
	info, _ := debug.ReadBuildInfo()
	value := buildVersion(Version, info)
	if value == "" {
		return "dev"
	}
	return value
}

// IsDev reports whether this binary was built without release metadata.
func IsDev() bool {
	return Short() == "dev"
}

func buildVersion(value string, info *debug.BuildInfo) string {
	if info != nil {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.modified" && setting.Value == "true" {
				return ""
			}
		}
		if value == "" || value == "dev" {
			value = info.Main.Version
		}
	}
	if strings.Contains(strings.ToLower(value), "dirty") {
		return ""
	}
	if _, err := semver.StrictNewVersion(strings.TrimPrefix(value, "v")); err != nil {
		return ""
	}
	return value
}

func shortCommit() string {
	if len(Commit) > 7 {
		return Commit[:7]
	}
	return Commit
}
