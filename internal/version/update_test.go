package version

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/creativeprojects/go-selfupdate"
)

const fixtureBinary = "#!/bin/sh\necho 'projectsetup v1.3.0'\n"

type releaseFixture struct {
	source    selfupdate.Source
	url       string
	downloads atomic.Int32
}

// Exercise the real GitHub source, checksum validator, and archive handling over HTTP.
func newFixture(t *testing.T, failure string) *releaseFixture {
	t.Helper()
	f := &releaseFixture{}
	assets := []map[string]any{}
	data := map[string][]byte{}
	var checksums strings.Builder
	for i, host := range []string{"linux_amd64", "linux_arm64", "darwin_amd64", "darwin_arm64"} {
		name := "projectsetup_v1.3.0_" + host + ".tar.gz"
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tarball := tar.NewWriter(gz)
		for _, entry := range []struct{ name, body string }{{"README.md", "fixture"}, {"projectsetup", fixtureBinary}} {
			if err := tarball.WriteHeader(&tar.Header{Name: strings.TrimSuffix(name, ".tar.gz") + "/" + entry.name, Mode: 0755, Size: int64(len(entry.body))}); err != nil {
				t.Fatal(err)
			}
			if _, err := tarball.Write([]byte(entry.body)); err != nil {
				t.Fatal(err)
			}
		}
		if err := tarball.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		id := fmt.Sprint(i + 1)
		data[id] = buf.Bytes()
		hash := sha256.Sum256(buf.Bytes())
		if failure == "mismatch" {
			hash = sha256.Sum256([]byte("wrong"))
		}
		if failure != "missing entry" {
			fmt.Fprintf(&checksums, "%x  %s\n", hash, name)
		}
		if failure != "missing asset" {
			assets = append(assets, map[string]any{"id": i + 1, "name": name})
		}
	}
	if failure != "missing checksums" {
		assets = append(assets, map[string]any{"id": 10, "name": "checksums.txt"})
	}
	data["10"] = []byte(checksums.String())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failure == "network" {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/releases") {
			w.Header().Set("Content-Type", "application/json")
			// Include higher draft, marked and unmarked prereleases; API order isn't version order.
			releases := []map[string]any{}
			for _, tag := range []string{"v9.0.0", "v8.0.0-beta.1", "v7.0.0-rc.1", "v1.3.0", "v1.2.0"} {
				if failure == "no stable release" && (tag == "v1.3.0" || tag == "v1.2.0") {
					continue
				}
				releaseAssets := assets
				if failure == "latest missing asset" && tag == "v1.3.0" {
					releaseAssets = nil
				}
				releases = append(releases, map[string]any{"tag_name": tag, "draft": tag == "v9.0.0", "prerelease": tag == "v8.0.0-beta.1", "html_url": f.url + "/tag/" + tag, "assets": releaseAssets})
			}
			_ = json.NewEncoder(w).Encode(releases)
			return
		}
		f.downloads.Add(1)
		if failure == "slow download" {
			<-r.Context().Done()
			return
		}
		if failure == "download" {
			http.Error(w, "download failed", 500)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		body, ok := data[filepath.Base(r.URL.Path)]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	f.url = server.URL
	var err error
	f.source, err = selfupdate.NewGitHubSource(selfupdate.GitHubConfig{EnterpriseBaseURL: server.URL + "/"})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func fixtureUpdater(t *testing.T, f *releaseFixture, goos, arch string) *selfupdate.Updater {
	t.Helper()
	up, err := newUpdater(f.source, goos, arch)
	if err != nil {
		t.Fatal(err)
	}
	return up
}

func fixturePath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "projectsetup")
	if err := os.WriteFile(path, []byte("old executable"), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSupportedAssets(t *testing.T) {
	f := newFixture(t, "")
	for _, goos := range []string{"linux", "darwin"} {
		for _, arch := range []string{"amd64", "arm64"} {
			t.Run(goos+"/"+arch, func(t *testing.T) {
				up := fixtureUpdater(t, f, goos, arch)
				rel, err := discover(context.Background(), up)
				if err != nil {
					t.Fatal(err)
				}
				if rel.Version() != "1.3.0" || rel.AssetName != "projectsetup_v1.3.0_"+goos+"_"+arch+".tar.gz" {
					t.Fatalf("wrong release: %+v", rel)
				}
				path := fixturePath(t)
				result, err := selfUpdate(context.Background(), up, "v1.2.3", func() (string, error) { return path, nil })
				if err != nil {
					t.Fatal(err)
				}
				if !result.Updated || result.Version != "v1.3.0" || result.Destination != path {
					t.Fatalf("result: %+v", result)
				}
				body, err := os.ReadFile(path)
				if err != nil || string(body) != fixtureBinary {
					t.Fatalf("replacement: %q, %v", body, err)
				}
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != 0755 {
					t.Fatalf("executable mode: %v, %v", info, err)
				}
			})
		}
	}
}

func TestNoDowngradeOrReplacement(t *testing.T) {
	for _, current := range []string{"v1.3.0", "v2.0.0", "v1.3.0+custom"} {
		t.Run(current, func(t *testing.T) {
			f := newFixture(t, "")
			path := fixturePath(t)
			result, err := selfUpdate(context.Background(), fixtureUpdater(t, f, "linux", "amd64"), current, func() (string, error) { return path, nil })
			if err != nil || result.Updated || result.Version != current {
				t.Fatalf("%+v, %v", result, err)
			}
			assertPreserved(t, path)
			if f.downloads.Load() != 0 {
				t.Fatal("no-op downloaded assets")
			}
		})
	}
}

func assertPreserved(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "old executable" {
		t.Fatalf("old executable changed: %q, %v", data, err)
	}
}

func TestUpdateFailuresPreserveExecutable(t *testing.T) {
	for _, failure := range []string{"missing checksums", "missing entry", "mismatch", "missing asset", "latest missing asset", "no stable release", "network", "download", "unwritable", "replacement"} {
		t.Run(failure, func(t *testing.T) {
			f := newFixture(t, failure)
			path := fixturePath(t)
			if failure == "unwritable" {
				if os.Geteuid() == 0 {
					t.Skip("root bypasses directory permissions")
				}
				if err := os.Chmod(filepath.Dir(path), 0555); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(filepath.Dir(path), 0755) })
			}
			if failure == "replacement" {
				// A nonempty backup directory forces the first rename to fail.
				backup := filepath.Join(filepath.Dir(path), ".projectsetup.old")
				if err := os.Mkdir(backup, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(backup, "keep"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := selfUpdate(context.Background(), fixtureUpdater(t, f, "linux", "amd64"), "v1.2.3", func() (string, error) { return path, nil })
			if err == nil {
				t.Fatal("expected failure")
			}
			if failure == "unwritable" || failure == "replacement" || failure == "mismatch" || failure == "download" || failure == "missing entry" {
				if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), f.url+"/tag/v1.3.0") {
					t.Fatalf("missing error context: %v", err)
				}
			}
			assertPreserved(t, path)
		})
	}
}

func TestCancelledDiscovery(t *testing.T) {
	f := newFixture(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := discover(ctx, fixtureUpdater(t, f, "linux", "amd64")); err == nil {
		t.Fatal("expected cancellation")
	}
}

func TestDownloadDeadline(t *testing.T) {
	f := newFixture(t, "slow download")
	path := fixturePath(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := selfUpdate(ctx, fixtureUpdater(t, f, "linux", "amd64"), "v1.2.3", func() (string, error) { return path, nil })
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("expected deadline error: %v", err)
	}
	assertPreserved(t, path)
}

func TestResolveSymlinkDestination(t *testing.T) {
	f := newFixture(t, "")
	path := fixturePath(t)
	link := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	result, err := selfUpdate(context.Background(), fixtureUpdater(t, f, "linux", "amd64"), "v1.2.3", func() (string, error) { return link, nil })
	if err != nil || result.Destination != path {
		t.Fatalf("result: %+v, %v", result, err)
	}
	if target, err := os.Readlink(link); err != nil || target != path {
		t.Fatalf("symlink changed: %q, %v", target, err)
	}
}

func TestUnsupportedHost(t *testing.T) {
	for _, host := range [][2]string{{"windows", "amd64"}, {"linux", "386"}} {
		if _, err := newUpdater(nil, host[0], host[1]); err == nil {
			t.Fatal("accepted unsupported host")
		}
	}
}

func TestNotifications(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })
	Version = "v1.2.3"
	f := newFixture(t, "")
	result := checkForUpdate(context.Background(), f.source)
	if result == nil || !result.UpdateAvailable || result.Latest != "v1.3.0" {
		t.Fatalf("result: %+v", result)
	}
	broken := newFixture(t, "network")
	if checkForUpdate(context.Background(), broken.source) != nil {
		t.Fatal("network failure wasn't quiet")
	}
	Version = "dev"
	if checkForUpdate(context.Background(), nil) != nil {
		t.Fatal("development check wasn't skipped")
	}
	if _, err := SelfUpdate(context.Background()); err == nil {
		t.Fatal("development update wasn't refused")
	}
}

func TestSymlinkedRunningExecutable(t *testing.T) {
	if os.Getenv("PROJECTSETUP_UPDATE_FIXTURE") != "" {
		source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{EnterpriseBaseURL: os.Getenv("PROJECTSETUP_UPDATE_FIXTURE")})
		if err != nil {
			t.Fatal(err)
		}
		up, err := newUpdater(source, runtime.GOOS, runtime.GOARCH)
		if err != nil {
			t.Fatal(err)
		}
		result, err := selfUpdate(context.Background(), up, "v1.2.3", os.Executable)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println(result.Destination)
		return
	}
	f := newFixture(t, "")
	path := fixturePath(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "projectsetup-link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, link, "-test.run=^TestSymlinkedRunningExecutable$")
	cmd.Dir = t.TempDir() // no project configuration or Docker required
	cmd.Env = append(os.Environ(), "PROJECTSETUP_UPDATE_FIXTURE="+f.url+"/")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("running replacement: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), path) {
		t.Fatalf("wrong destination: %s", output)
	}
	if target, err := os.Readlink(link); err != nil || target != path {
		t.Fatalf("symlink changed: %q, %v", target, err)
	}
	output, err = exec.CommandContext(ctx, link, "--version").CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "projectsetup v1.3.0" {
		t.Fatalf("installed fixture: %s, %v", output, err)
	}
}
