package version

import (
	"context"
	"runtime"
	"time"

	"github.com/creativeprojects/go-selfupdate"
)

const (
	GitHubRepo   = "mpm/projectsetup"
	checkTimeout = 5 * time.Second
)

// CheckResult describes the latest published release relative to this binary.
type CheckResult struct {
	Current         string
	Latest          string
	UpdateAvailable bool
	ReleaseURL      string
}

// CheckForUpdate quietly checks for updates without delaying the main command.
func CheckForUpdate() *CheckResult {
	return CheckForUpdateWithContext(context.Background())
}

// CheckForUpdateWithContext ignores failures and never checks development builds.
func CheckForUpdateWithContext(ctx context.Context) *CheckResult {
	return checkForUpdate(ctx, nil)
}

func checkForUpdate(ctx context.Context, source selfupdate.Source) *CheckResult {
	if IsDev() {
		return nil
	}
	up, err := newUpdater(source, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil
	}
	rel, err := discover(ctx, up)
	if err != nil {
		return nil
	}
	return &CheckResult{Current: Short(), Latest: "v" + rel.Version(), UpdateAvailable: rel.GreaterThan(Short()), ReleaseURL: rel.URL}
}
