package presets

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseLocation(t *testing.T) {
	tests := []struct {
		spec string
		want Location
		err  string
	}{
		{spec: "https://example.com/presets/redis.toml", want: Location{URL: "https://example.com/presets/redis.toml"}},
		{spec: "github:acme/presets", want: Location{URL: "https://raw.githubusercontent.com/acme/presets/HEAD/index.toml"}},
		{spec: "github:acme/presets/addons/redis.toml@v1.2", want: Location{URL: "https://raw.githubusercontent.com/acme/presets/v1.2/addons/redis.toml", Ref: "v1.2"}},
		{spec: "github:acme/presets/addons@release/2", want: Location{URL: "https://raw.githubusercontent.com/acme/presets/release/2/addons/index.toml", Ref: "release/2"}},
		{spec: "http://example.com/redis.toml", err: "only https:// URLs are supported"},
		{spec: "https://user:secret@example.com/redis.toml", err: "credentials in URLs are not supported"},
		{spec: "redis.toml", err: "expected an https:// URL or github:"},
		{spec: "github:acme", err: "owner and repository are required"},
		{spec: "github:acme/presets/../secrets.toml", err: "must be a clean relative path"},
		{spec: "github:acme/presets/a//b.toml", err: "must be a clean relative path"},
		{spec: "github:acme/presets@-x", err: "is not a valid Git ref"},
		{spec: "github:acme/presets@a..b", err: "is not a valid Git ref"},
	}
	for _, tt := range tests {
		got, err := ParseLocation(tt.spec)
		if tt.err != "" {
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("ParseLocation(%q) error = %v, want containing %q", tt.spec, err, tt.err)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("ParseLocation(%q) = %+v, %v; want %+v", tt.spec, got, err, tt.want)
		}
	}
}

// serve starts an HTTPS server with the given files and returns a fetcher
// that trusts it and the server's base URL.
func serve(t *testing.T, files map[string]string) (Fetcher, string) {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if target, ok := strings.CutPrefix(r.URL.Path, "/redirect"); ok {
			http.Redirect(w, r, target, http.StatusFound)
			return
		}
		content, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(content))
	}))
	t.Cleanup(server.Close)
	return Fetcher{Client: server.Client()}, server.URL
}

func TestFetchSingleDefinition(t *testing.T) {
	fetcher, base := serve(t, map[string]string{"/redis.toml": definitionText("addon", "redis")})
	fetched, err := fetcher.Fetch(context.Background(), Location{URL: base + "/redis.toml"})
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched) != 1 || fetched[0].URL != base+"/redis.toml" || fetched[0].Definition.Name != "redis" || fetched[0].Definition.Source != base+"/redis.toml" {
		t.Fatalf("Fetch() = %+v", fetched)
	}
}

func TestFetchIndex(t *testing.T) {
	fetcher, base := serve(t, map[string]string{
		"/set/index.toml":        "schema = 1\ndefinitions = [\"redis.toml\", \"langs/custom.toml\"]\n",
		"/set/redis.toml":        definitionText("addon", "redis"),
		"/set/langs/custom.toml": definitionText("preset", "custom"),
	})
	fetched, err := fetcher.Fetch(context.Background(), Location{URL: base + "/set/index.toml"})
	if err != nil {
		t.Fatal(err)
	}
	var urls []string
	for _, item := range fetched {
		urls = append(urls, item.URL)
	}
	if want := []string{base + "/set/redis.toml", base + "/set/langs/custom.toml"}; !reflect.DeepEqual(urls, want) {
		t.Fatalf("URLs = %v, want %v", urls, want)
	}
}

func TestFetchRejectsInvalidRemoteFiles(t *testing.T) {
	invalidAddon := definitionText("addon", "cache") + "[services.cache]\nrestart = \"always\"\n"
	fetcher, base := serve(t, map[string]string{
		"/large.toml":         definitionText("addon", "large") + "# " + strings.Repeat("x", MaxRemoteFileSize) + "\n",
		"/broken.toml":        "schema = ",
		"/cache.toml":         invalidAddon,
		"/redis.toml":         definitionText("addon", "redis"),
		"/bad/index.toml":     "schema = 1\ndefinitions = [\"../redis.toml\", \"https://example.com/x.toml\", \"a.toml\", \"a.toml\", \"index.toml\"]\n",
		"/empty/index.toml":   "schema = 2\ndefinitions = []\n",
		"/unknown/index.toml": "schema = 1\nfiles = []\n",
		"/dup/index.toml":     "schema = 1\ndefinitions = [\"a.toml\", \"b.toml\", \"missing.toml\"]\n",
		"/dup/a.toml":         definitionText("addon", "redis"),
		"/dup/b.toml":         definitionText("addon", "redis"),
	})
	tests := []struct {
		path string
		want []string
	}{
		{"/missing.toml", []string{"404 Not Found"}},
		{"/large.toml", []string{"larger than 64 KiB"}},
		{"/broken.toml", []string{"/broken.toml:1"}},
		{"/cache.toml", []string{`service "cache" has no image`}},
		{"/redirect" + "http://example.com/redis.toml", []string{"only https:// URLs are supported"}},
		{"/bad/index.toml", []string{`entry "../redis.toml"`, `entry "https://example.com/x.toml"`, `entry "a.toml" is listed more than once`, `entry "index.toml"`}},
		{"/empty/index.toml", []string{"schema is 2", "definitions must list 1 to 64 files"}},
		{"/unknown/index.toml", []string{"parse index", "files"}},
		{"/dup/index.toml", []string{`definition "redis" is also provided by`, "missing.toml: server returned 404"}},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			_, err := fetcher.Fetch(context.Background(), Location{URL: base + tt.path})
			if err == nil {
				t.Fatal("Fetch() succeeded")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error does not contain %q:\n%v", want, err)
				}
			}
		})
	}
	// An HTTPS redirect is followed.
	if _, err := fetcher.Fetch(context.Background(), Location{URL: base + "/redirect/redis.toml"}); err != nil {
		t.Fatalf("Fetch() through HTTPS redirect: %v", err)
	}
}

func TestCheckStandalone(t *testing.T) {
	for _, name := range []string{"node", "rails", "postgres", "sqlite"} {
		definition, _ := Builtin().Lookup(name)
		if err := CheckStandalone(definition); err != nil {
			t.Errorf("CheckStandalone(%s) = %v", name, err)
		}
	}
	standalone, err := Parse([]byte(definitionText("addon", "standalone")), "standalone.toml")
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckStandalone(standalone); err != nil {
		t.Errorf("CheckStandalone(standalone) = %v", err)
	}
}

func TestSourcesRoundTripAndPinning(t *testing.T) {
	config := t.TempDir()
	t.Setenv(ConfigDirEnv, config)
	dir := filepath.Join(config, "presets")
	if sources, err := ReadSources(); err != nil || len(sources.Definitions) != 0 {
		t.Fatalf("ReadSources() without file = %+v, %v", sources, err)
	}
	text := definitionText("addon", "redis")
	writeDefinition(t, dir, "redis.toml", text)
	redis, _ := Parse([]byte(text), "redis.toml")
	fetched := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	var sources Sources
	sources.Set(RemoteSource{Name: "redis", URL: "https://example.com/redis.toml", SHA256: redis.SHA256(), Fetched: fetched})
	sources.Set(RemoteSource{Name: "go", URL: "https://example.com/go.toml", Ref: "v1", SHA256: strings.Repeat("0", 64), Fetched: fetched})
	if err := WriteSources(sources); err != nil {
		t.Fatal(err)
	}
	read, err := ReadSources()
	if err != nil || !reflect.DeepEqual(read, sources) || read.Definitions[0].Name != "go" {
		t.Fatalf("ReadSources() = %+v, %v; want %+v", read, err, sources)
	}

	// go.toml is recorded but missing.
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), `remote definition "go" recorded in sources.toml is missing`) {
		t.Fatalf("Load() with missing remote definition: %v", err)
	}
	sources.Remove("go")
	if err := WriteSources(sources); err != nil {
		t.Fatal(err)
	}
	registry, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded, _ := registry.Lookup("redis"); loaded.Source != "https://example.com/redis.toml" {
		t.Fatalf("remote source = %q", loaded.Source)
	}

	writeDefinition(t, dir, "redis.toml", text+"# edited\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "remote definitions cannot be edited in place, run projectsetup preset update redis") {
		t.Fatalf("Load() with edited remote definition: %v", err)
	}
}

func TestReadSourcesRejectsInvalidRecords(t *testing.T) {
	config := t.TempDir()
	t.Setenv(ConfigDirEnv, config)
	content := `[[definition]]
name = "Redis"
url = "http://example.com/redis.toml"
sha256 = "abc"
fetched = 2026-10-10T12:00:00Z

[[definition]]
name = "Redis"
url = "https://example.com/redis.toml"
sha256 = "` + strings.Repeat("a", 64) + `"
fetched = 2026-10-10T12:00:00Z
`
	if err := os.WriteFile(filepath.Join(config, SourcesFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadSources()
	if err == nil {
		t.Fatal("ReadSources() succeeded")
	}
	for _, want := range []string{`name "Redis" is invalid`, "only https:// URLs", "64 lowercase hex digits"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not contain %q:\n%v", want, err)
		}
	}
}
