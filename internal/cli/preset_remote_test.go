package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mpm/projectsetup/internal/presets"
	"github.com/mpm/projectsetup/internal/validate"
)

const (
	remotePreset = "schema = 1\nkind = \"preset\"\nname = \"custom\"\nversion = \"1.0.0\"\ndescription = \"Remote preset\"\n\n[image]\nbase = \"mcr.microsoft.com/devcontainers/base:ubuntu-24.04\"\n"
	remoteAddon  = "schema = 1\nkind = \"addon\"\nname = \"redis\"\nversion = \"1.0.0\"\ndescription = \"Remote add-on\"\n\n[container.env]\nREDIS_URL = \"redis://redis:6379\"\n"
)

// remoteServer serves files over HTTPS to the preset commands. Tests change
// files between commands.
func remoteServer(t *testing.T, files map[string]string) string {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(content))
	}))
	t.Cleanup(server.Close)
	client, now := presetHTTPClient, presetNow
	presetHTTPClient = server.Client()
	presetNow = func() time.Time { return time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { presetHTTPClient, presetNow = client, now })
	return server.URL
}

func runPresetInput(t *testing.T, input string, args ...string) (string, error) {
	t.Helper()
	var stdout bytes.Buffer
	err := Run(append([]string{"preset"}, args...), strings.NewReader(input), &stdout, &bytes.Buffer{})
	return stdout.String(), err
}

func TestRemoteDefinitionLifecycle(t *testing.T) {
	userDir := useConfigDir(t)
	t.Setenv("HOME", t.TempDir())
	files := map[string]string{
		"/set/index.toml":        "schema = 1\ndefinitions = [\"custom.toml\", \"addons/redis.toml\"]\n",
		"/set/custom.toml":       remotePreset,
		"/set/addons/redis.toml": remoteAddon,
	}
	base := remoteServer(t, files)
	redisURL := base + "/set/addons/redis.toml"

	stdout, err := runPresetInput(t, "n\n", "add", base+"/set/index.toml")
	if err == nil || !strings.Contains(err.Error(), "installation cancelled") {
		t.Fatalf("declined add: %v", err)
	}
	if !strings.Contains(stdout, "==> addon redis 1.0.0 from "+redisURL) || !strings.Contains(stdout, `REDIS_URL = "redis://redis:6379"`) || !strings.Contains(stdout, "forwarded SSH agent") {
		t.Fatalf("add did not show the definitions:\n%s", stdout)
	}
	if entries, _ := os.ReadDir(filepath.Dir(userDir)); len(entries) != 0 {
		t.Fatalf("declined add wrote %v", entries)
	}

	if _, err := runPresetInput(t, "y\n", "add", base+"/set/index.toml"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := runPresetInput(t, "", "add", "--yes", redisURL); err == nil || !strings.Contains(err.Error(), `"redis" is already installed from `+redisURL) {
		t.Fatalf("second add: %v", err)
	}
	stdout, err = runPresetInput(t, "", "list")
	if err != nil || !strings.Contains(stdout, redisURL) {
		t.Fatalf("list = %q, %v", stdout, err)
	}

	// Projects record the URL as source and check without the network.
	root := t.TempDir()
	if err := runInit(root, []string{"--non-interactive", "--preset", "custom", "--addon", "redis", "--ai", "none"}, &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("init: %v", err)
	}
	manifest := readManifest(t, root)
	if manifest.Preset.Source != base+"/set/custom.toml" || len(manifest.Addons) != 1 || manifest.Addons[0].Source != redisURL {
		t.Fatalf("manifest = %+v", manifest)
	}
	if diagnostics := validate.Check(root, validate.Options{}); validate.ErrorCount(diagnostics) != 0 {
		t.Fatalf("check: %#v", diagnostics)
	}

	stdout, err = runPresetInput(t, "", "update")
	if err != nil || !strings.Contains(stdout, "custom 1.0.0 is up to date.") || !strings.Contains(stdout, "redis 1.0.0 is up to date.") {
		t.Fatalf("update without changes = %q, %v", stdout, err)
	}

	installed := filepath.Join(userDir, "redis.toml")
	files["/set/addons/redis.toml"] = strings.Replace(remoteAddon, `version = "1.0.0"`, `version = "1.1.0"`, 1)
	stdout, err = runPresetInput(t, "n\n", "update", "redis")
	if err == nil || !strings.Contains(err.Error(), "update cancelled") {
		t.Fatalf("declined update: %v", err)
	}
	wantDiff := "--- " + installed + "\n+++ " + redisURL + "\n@@ -1,7 +1,7 @@\n schema = 1\n kind = \"addon\"\n name = \"redis\"\n-version = \"1.0.0\"\n+version = \"1.1.0\"\n"
	if !strings.Contains(stdout, wantDiff) {
		t.Fatalf("update diff:\n%s\nwant containing:\n%s", stdout, wantDiff)
	}
	if data, _ := os.ReadFile(installed); string(data) != remoteAddon {
		t.Fatalf("declined update changed %s", installed)
	}

	// A failing definition prevents every update.
	delete(files, "/set/custom.toml")
	if _, err := runPresetInput(t, "", "update", "--yes"); err == nil || !strings.Contains(err.Error(), "nothing was installed") || !strings.Contains(err.Error(), "404") {
		t.Fatalf("update with missing remote file: %v", err)
	}
	if data, _ := os.ReadFile(installed); string(data) != remoteAddon {
		t.Fatalf("failed update changed %s", installed)
	}

	stdout, err = runPresetInput(t, "", "update", "--yes", "redis")
	if err != nil || !strings.Contains(stdout, "Updated redis to 1.1.0.") {
		t.Fatalf("update = %q, %v", stdout, err)
	}
	registry, err := presets.Load()
	if err != nil {
		t.Fatalf("Load() after update: %v", err)
	}
	if redis, _ := registry.Lookup("redis"); redis.Version != "1.1.0" || redis.Source != redisURL {
		t.Fatalf("redis = %+v", redis.Ref())
	}
	sources, err := presets.ReadSources()
	if source, _ := sources.Lookup("redis"); err != nil || source.Fetched != presetNow() {
		t.Fatalf("sources = %+v, %v", sources, err)
	}

	// A remote definition edited in place is restored by update.
	editFile(t, installed, "redis://redis:6379", "redis://other:6379")
	if _, _, err := runPresetCommand(t, "list"); err == nil || !strings.Contains(err.Error(), "cannot be edited in place") {
		t.Fatalf("list with edited remote definition: %v", err)
	}
	if _, err := runPresetInput(t, "", "update", "--yes", "redis"); err != nil {
		t.Fatalf("restore: %v", err)
	}

	if stdout, err = runPresetInput(t, "", "remove", "redis"); err != nil || !strings.Contains(stdout, "Removed redis.") {
		t.Fatalf("remove = %q, %v", stdout, err)
	}
	if _, err := os.Stat(installed); !os.IsNotExist(err) {
		t.Fatalf("remove kept %s: %v", installed, err)
	}
	if sources, _ := presets.ReadSources(); len(sources.Definitions) != 1 || sources.Definitions[0].Name != "custom" {
		t.Fatalf("sources after remove = %+v", sources)
	}
	// The generated project keeps working from its copy.
	if diagnostics := validate.Check(root, validate.Options{}); validate.ErrorCount(diagnostics) != 0 {
		t.Fatalf("check after remove: %#v", diagnostics)
	}
}

func TestRemoteCommandsRejectInvalidRequests(t *testing.T) {
	userDir := useConfigDir(t)
	base := remoteServer(t, map[string]string{
		"/node.toml":  strings.Replace(remotePreset, `name = "custom"`, `name = "node"`, 1),
		"/redis.toml": remoteAddon,
	})
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "redis.toml"), []byte(remoteAddon), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"add", "--yes", base + "/node.toml"}, `"node" is a built-in definition name`},
		{[]string{"add", "--yes", base + "/redis.toml"}, "user definition " + filepath.Join(userDir, "redis.toml") + " already exists"},
		{[]string{"add", "--yes", "http://example.com/redis.toml"}, "only https:// URLs are supported"},
		{[]string{"add", "--yes"}, "expects 1 argument(s), got 0"},
		{[]string{"update", "a", "b"}, "expects at most 1 argument(s), got 2"},
		{[]string{"update", "node"}, `"node" is a built-in definition`},
		{[]string{"update", "redis"}, `"redis" is not a remote definition`},
		{[]string{"remove", "redis"}, `"redis" is not a remote definition`},
	}
	for _, tt := range tests {
		_, err := runPresetInput(t, "", tt.args...)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("preset %v error = %v, want containing %q", tt.args, err, tt.want)
		}
	}
	if stdout, err := runPresetInput(t, "", "update"); err != nil || stdout != "No remote definitions are installed.\n" {
		t.Fatalf("update without remote definitions = %q, %v", stdout, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(userDir), presets.SourcesFile)); !os.IsNotExist(err) {
		t.Fatalf("rejected requests wrote sources.toml: %v", err)
	}
}

func TestUnifiedDiff(t *testing.T) {
	lines := func(n int) []string {
		var out []string
		for i := 1; i <= n; i++ {
			out = append(out, "line "+string(rune('a'+i-1)))
		}
		return out
	}
	text := func(lines []string) string { return strings.Join(lines, "\n") + "\n" }
	original := lines(12)
	changed := append([]string{}, original...)
	changed[1] = "changed b"
	changed[10] = "changed k"
	tests := []struct {
		name     string
		from, to string
		want     string
	}{
		{"equal", text(original), text(original), ""},
		{"new file", "", "a\nb\n", "--- old\n+++ new\n@@ -0,0 +1,2 @@\n+a\n+b\n"},
		{"two hunks", text(original), text(changed), "--- old\n+++ new\n" +
			"@@ -1,5 +1,5 @@\n line a\n-line b\n+changed b\n line c\n line d\n line e\n" +
			"@@ -8,5 +8,5 @@\n line h\n line i\n line j\n-line k\n+changed k\n line l\n"},
		{"insertion", "a\nc\n", "a\nb\nc\n", "--- old\n+++ new\n@@ -1,2 +1,3 @@\n a\n+b\n c\n"},
	}
	for _, tt := range tests {
		if got := unifiedDiff("old", "new", tt.from, tt.to); got != tt.want {
			t.Errorf("%s: unifiedDiff() =\n%s\nwant\n%s", tt.name, got, tt.want)
		}
	}
}
