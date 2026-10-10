package presets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

// Limits for fetched remote files.
const (
	MaxRemoteFileSize = 64 << 10
	MaxIndexEntries   = 64
)

// IndexFile is the file name that marks a URL as an index of definitions.
const IndexFile = "index.toml"

// SourcesFile records remote definitions next to the presets directory.
const SourcesFile = "sources.toml"

// Location is where a remote definition or index is fetched from.
type Location struct {
	URL string
	// Ref is the Git ref of a github: location, empty when not given.
	Ref string
}

var (
	githubName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	githubRef  = regexp.MustCompile(`^[A-Za-z0-9_.][A-Za-z0-9_./-]*$`)
)

// ParseLocation accepts an https:// URL or github:owner/repo[/path][@ref].
// A github path that does not name a .toml file is a directory holding
// index.toml; without a path, the repository root's index.toml is used.
func ParseLocation(spec string) (Location, error) {
	if rest, ok := strings.CutPrefix(spec, "github:"); ok {
		return parseGitHub(spec, rest)
	}
	if err := checkURL(spec); err != nil {
		return Location{}, fmt.Errorf("%w; expected an https:// URL or github:owner/repo[/path][@ref]", err)
	}
	return Location{URL: spec}, nil
}

func parseGitHub(spec, rest string) (Location, error) {
	invalid := func(reason string) error {
		return fmt.Errorf("invalid location %q: %s; expected github:owner/repo[/path][@ref]", spec, reason)
	}
	rest, ref, hasRef := strings.Cut(rest, "@")
	if hasRef && (!githubRef.MatchString(ref) || strings.Contains(ref, "..")) {
		return Location{}, invalid(fmt.Sprintf("ref %q is not a valid Git ref", ref))
	}
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 || !githubName.MatchString(parts[0]) || !githubName.MatchString(parts[1]) {
		return Location{}, invalid("owner and repository are required")
	}
	file := IndexFile
	if len(parts) == 3 {
		file = parts[2]
		if file == "" || path.IsAbs(file) || path.Clean(file) != file || strings.HasPrefix(file, "..") {
			return Location{}, invalid(fmt.Sprintf("path %q must be a clean relative path", file))
		}
		if path.Ext(file) != ".toml" {
			file = path.Join(file, IndexFile)
		}
	}
	urlRef := ref
	if urlRef == "" {
		urlRef = "HEAD"
	}
	escaped := make([]string, 0, 4)
	for _, segment := range strings.Split(path.Join(parts[0], parts[1], urlRef, file), "/") {
		escaped = append(escaped, url.PathEscape(segment))
	}
	return Location{URL: "https://raw.githubusercontent.com/" + strings.Join(escaped, "/"), Ref: ref}, nil
}

func checkURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("invalid URL %q: only https:// URLs are supported", raw)
	}
	if parsed.User != nil {
		return fmt.Errorf("invalid URL %q: credentials in URLs are not supported", raw)
	}
	return nil
}

// Fetcher downloads remote definitions over HTTPS.
type Fetcher struct {
	Client *http.Client
}

// Fetched is a remote definition and the URL it was downloaded from.
type Fetched struct {
	Definition Definition
	URL        string
}

// Fetch downloads the definition at location, or every definition listed
// by an index.toml, and validates each one. Errors from all files are
// reported together.
func (f Fetcher) Fetch(ctx context.Context, location Location) ([]Fetched, error) {
	parsed, err := url.Parse(location.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %w", location.URL, err)
	}
	urls := []string{location.URL}
	if path.Base(parsed.Path) == IndexFile {
		if urls, err = f.index(ctx, parsed); err != nil {
			return nil, err
		}
	}
	var fetched []Fetched
	var errs []error
	names := map[string]string{}
	for _, raw := range urls {
		definition, err := f.FetchDefinition(ctx, raw)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if other, exists := names[definition.Name]; exists {
			errs = append(errs, fmt.Errorf("%s: definition %q is also provided by %s", raw, definition.Name, other))
			continue
		}
		names[definition.Name] = raw
		fetched = append(fetched, Fetched{Definition: definition, URL: raw})
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return fetched, nil
}

// Index lists definition files relative to the index URL.
type Index struct {
	Schema      int      `toml:"schema"`
	Definitions []string `toml:"definitions"`
}

func (f Fetcher) index(ctx context.Context, base *url.URL) ([]string, error) {
	data, err := f.get(ctx, base.String())
	if err != nil {
		return nil, err
	}
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var index Index
	if err := decoder.Decode(&index); err != nil {
		var strict *toml.StrictMissingError
		if errors.As(err, &strict) {
			return nil, fmt.Errorf("%s: parse index: unknown fields:\n%s", base, strict.String())
		}
		return nil, fmt.Errorf("%s: parse index: %w", base, err)
	}
	var problems []string
	if index.Schema != 1 {
		problems = append(problems, fmt.Sprintf("schema is %d; this projectsetup reads index schema 1", index.Schema))
	}
	if len(index.Definitions) == 0 || len(index.Definitions) > MaxIndexEntries {
		problems = append(problems, fmt.Sprintf("definitions must list 1 to %d files", MaxIndexEntries))
	}
	var urls []string
	for _, entry := range index.Definitions {
		reference, err := url.Parse(entry)
		if err != nil || reference.Scheme != "" || reference.Host != "" || reference.RawQuery != "" || reference.Fragment != "" ||
			path.IsAbs(entry) || path.Clean(entry) != entry || strings.HasPrefix(entry, "..") || path.Ext(entry) != ".toml" || path.Base(entry) == IndexFile {
			problems = append(problems, fmt.Sprintf("entry %q must be a clean relative path to a .toml definition", entry))
			continue
		}
		resolved := base.ResolveReference(reference).String()
		if slices.Contains(urls, resolved) {
			problems = append(problems, fmt.Sprintf("entry %q is listed more than once", entry))
			continue
		}
		urls = append(urls, resolved)
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("%s: invalid index:\n  %s", base, strings.Join(problems, "\n  "))
	}
	return urls, nil
}

// FetchDefinition downloads and validates the definition at raw.
func (f Fetcher) FetchDefinition(ctx context.Context, raw string) (Definition, error) {
	data, err := f.get(ctx, raw)
	if err != nil {
		return Definition{}, err
	}
	definition, err := Parse(data, raw)
	if err != nil {
		return Definition{}, err
	}
	if err := CheckStandalone(definition); err != nil {
		return Definition{}, fmt.Errorf("%s: %w", raw, err)
	}
	definition.Source = raw
	return definition, nil
}

// get downloads raw over HTTPS, following only HTTPS redirects, and
// rejects bodies larger than MaxRemoteFileSize.
func (f Fetcher) get(ctx context.Context, raw string) ([]byte, error) {
	if err := checkURL(raw); err != nil {
		return nil, err
	}
	client := http.Client{}
	if f.Client != nil {
		client = *f.Client
	}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return checkURL(request.URL.String())
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", raw, err)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", raw, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: server returned %s", raw, response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, MaxRemoteFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", raw, err)
	}
	if len(data) > MaxRemoteFileSize {
		return nil, fmt.Errorf("fetch %s: file is larger than %d KiB", raw, MaxRemoteFileSize>>10)
	}
	return data, nil
}

// CheckStandalone resolves d with default options: a preset on its own and
// an add-on on top of a minimal preset. It reports rules that only hold
// for resolved results, such as sidecar services without an image.
func CheckStandalone(d Definition) error {
	definitions := []Definition{d}
	selection := Selection{Preset: d.Name, Project: Project{Name: "example", Home: "/home/vscode", Workspace: "/workspaces/example"}}
	if d.Kind == KindAddon {
		name := "standalone"
		if d.Name == name {
			name = "standalone-preset"
		}
		preset, err := Parse([]byte("schema = 1\nkind = \"preset\"\nname = \""+name+"\"\nversion = \"1.0.0\"\ndescription = \"Minimal preset\"\n[image]\nbase = \"debian:trixie\"\n"), "standalone preset")
		if err != nil {
			return err
		}
		definitions = append(definitions, preset)
		selection.Preset, selection.Addons = name, []string{d.Name}
	}
	registry, err := NewRegistry(definitions...)
	if err != nil {
		return err
	}
	if _, err := registry.Resolve(selection); err != nil {
		return fmt.Errorf("resolve with default options: %w", err)
	}
	return nil
}

// Sources lists the remote definitions installed in the user directory.
type Sources struct {
	Definitions []RemoteSource `toml:"definition"`
}

// RemoteSource pins an installed remote definition to its content.
type RemoteSource struct {
	Name    string    `toml:"name"`
	URL     string    `toml:"url"`
	Ref     string    `toml:"ref,omitempty"`
	SHA256  string    `toml:"sha256"`
	Fetched time.Time `toml:"fetched"`
}

// Lookup returns the record for name.
func (s Sources) Lookup(name string) (RemoteSource, bool) {
	for _, source := range s.Definitions {
		if source.Name == name {
			return source, true
		}
	}
	return RemoteSource{}, false
}

// Set adds or replaces the record for source.Name.
func (s *Sources) Set(source RemoteSource) {
	s.Remove(source.Name)
	s.Definitions = append(s.Definitions, source)
	slices.SortFunc(s.Definitions, func(a, b RemoteSource) int { return strings.Compare(a.Name, b.Name) })
}

// Remove drops the record for name.
func (s *Sources) Remove(name string) {
	s.Definitions = slices.DeleteFunc(s.Definitions, func(source RemoteSource) bool { return source.Name == name })
}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// SourcesPath returns the path of sources.toml.
func SourcesPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, SourcesFile), nil
}

// ReadSources reads sources.toml. A missing file records no definitions.
func ReadSources() (Sources, error) {
	file, err := SourcesPath()
	if err != nil {
		return Sources{}, err
	}
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return Sources{}, nil
	}
	if err != nil {
		return Sources{}, fmt.Errorf("read remote definition sources: %w", err)
	}
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var sources Sources
	if err := decoder.Decode(&sources); err != nil {
		return Sources{}, fmt.Errorf("%s: %w", file, err)
	}
	var problems []string
	seen := map[string]bool{}
	for _, source := range sources.Definitions {
		if !validName.MatchString(source.Name) || seen[source.Name] {
			problems = append(problems, fmt.Sprintf("name %q is invalid or recorded more than once", source.Name))
		}
		seen[source.Name] = true
		if err := checkURL(source.URL); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", source.Name, err))
		}
		if !sha256Hex.MatchString(source.SHA256) {
			problems = append(problems, fmt.Sprintf("%s: sha256 must be 64 lowercase hex digits", source.Name))
		}
	}
	if len(problems) > 0 {
		return Sources{}, fmt.Errorf("%s: invalid sources:\n  %s", file, strings.Join(problems, "\n  "))
	}
	return sources, nil
}

// WriteSources atomically replaces sources.toml.
func WriteSources(sources Sources) error {
	file, err := SourcesPath()
	if err != nil {
		return err
	}
	data, err := toml.Marshal(sources)
	if err != nil {
		return fmt.Errorf("encode remote definition sources: %w", err)
	}
	header := "# Remote definitions installed by projectsetup preset add. Do not edit.\n"
	return WriteFileAtomic(file, append([]byte(header), data...))
}

// WriteFileAtomic replaces path with data through a temporary file in the
// same directory, creating the directory when needed.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	temporary, err := os.CreateTemp(dir, ".projectsetup-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chmod(temporary.Name(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(temporary.Name(), path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
