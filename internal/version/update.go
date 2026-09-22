package version

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/creativeprojects/go-selfupdate"
	"github.com/creativeprojects/go-selfupdate/update"
)

const downloadTimeout = 5 * time.Minute

// stableSource prevents fallback to an older release when the latest stable
// release lacks a host asset. It also rejects prerelease tags not marked as such.
type stableSource struct{ selfupdate.Source }

func (s stableSource) ListReleases(ctx context.Context, repo selfupdate.Repository) ([]selfupdate.SourceRelease, error) {
	releases, err := s.Source.ListReleases(ctx, repo)
	if err != nil {
		return nil, err
	}
	var latest selfupdate.SourceRelease
	var latestVersion *semver.Version
	for _, rel := range releases {
		v, err := semver.NewVersion(rel.GetTagName())
		if err != nil || rel.GetDraft() || rel.GetPrerelease() || v.Prerelease() != "" {
			continue
		}
		if latest == nil || v.GreaterThan(latestVersion) {
			latest, latestVersion = rel, v
		}
	}
	if latest == nil {
		return nil, fmt.Errorf("no stable release found at https://github.com/%s/releases", GitHubRepo)
	}
	return []selfupdate.SourceRelease{latest}, nil
}

func newUpdater(source selfupdate.Source, goos, arch string) (*selfupdate.Updater, error) {
	if (goos != "linux" && goos != "darwin") || (arch != "amd64" && arch != "arm64") {
		return nil, fmt.Errorf("self-update does not support %s/%s; supported hosts are Linux/macOS amd64/arm64", goos, arch)
	}
	if source == nil {
		var err error
		source, err = selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
		if err != nil {
			return nil, fmt.Errorf("configure GitHub release discovery: %w", err)
		}
	}
	return selfupdate.NewUpdater(selfupdate.Config{
		Source: stableSource{source}, OS: goos, Arch: arch,
		Validator: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
		// Asset filters replace suffix matching, so include the host explicitly.
		Filters: []string{`^projectsetup_v?[0-9]+\.[0-9]+\.[0-9]+_` + goos + `_` + arch + `\.tar\.gz$`},
	})
}

func discover(ctx context.Context, up *selfupdate.Updater) (*selfupdate.Release, error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	rel, found, err := up.DetectLatest(ctx, selfupdate.ParseSlug(GitHubRepo))
	if err != nil {
		return nil, fmt.Errorf("discover stable projectsetup release (https://github.com/%s/releases): %w", GitHubRepo, err)
	}
	if !found {
		return nil, fmt.Errorf("latest stable projectsetup release has no matching platform archive; check https://github.com/%s/releases", GitHubRepo)
	}
	return rel, nil
}

// UpdateResult identifies the installed version and actual executable destination.
type UpdateResult struct {
	Version     string
	Destination string
	Updated     bool
}

// SelfUpdate updates the running executable, never another executable on PATH.
func SelfUpdate(ctx context.Context) (*UpdateResult, error) {
	if IsDev() {
		return nil, fmt.Errorf("self-update refuses unversioned or dirty development builds; install a tagged release using the installer or go install first")
	}
	up, err := newUpdater(nil, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil, err
	}
	return selfUpdate(ctx, up, Short(), os.Executable)
}

func selfUpdate(ctx context.Context, up *selfupdate.Updater, current string, executable func() (string, error)) (*UpdateResult, error) {
	path, err := executable()
	if err != nil {
		return nil, fmt.Errorf("locate running executable: %w", err)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return nil, fmt.Errorf("resolve executable symlink: %w", err)
	}
	rel, err := discover(ctx, up)
	if err != nil {
		return nil, err
	}
	result := &UpdateResult{Version: current, Destination: path}
	if !rel.GreaterThan(current) {
		return result, nil
	}
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	if err := up.UpdateTo(ctx, rel, path); err != nil {
		if rollback := update.RollbackError(err); rollback != nil {
			return nil, fmt.Errorf("install projectsetup at %s from %s: %v; ROLLBACK FAILED: %v; recover manually from %s or reinstall", path, rel.URL, err, rollback, filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".old"))
		}
		return nil, fmt.Errorf("install projectsetup at %s from %s: %w; check directory permissions and release checksums, or reinstall manually", path, rel.URL, err)
	}
	result.Version, result.Updated = "v"+rel.Version(), true
	return result, nil
}
