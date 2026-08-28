// Package version provides build and release version information.
package version

import (
	"fmt"
	"runtime"
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
	if IsDev() {
		return "dev"
	}
	return Version
}

// IsDev reports whether this binary was built without release metadata.
func IsDev() bool {
	return Version == "" || Version == "dev"
}

func shortCommit() string {
	if len(Commit) > 7 {
		return Commit[:7]
	}
	return Commit
}
