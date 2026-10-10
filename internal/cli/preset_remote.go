package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/mpm/projectsetup/internal/presets"
)

// Remote definitions are fetched with presetHTTPClient and stamped with
// presetNow; tests replace both.
var (
	presetHTTPClient = &http.Client{Timeout: 30 * time.Second}
	presetNow        = time.Now
)

const remoteWarning = "Definitions run their shell code in a container that mounts your AI tool credentials and receives your forwarded SSH agent. Install only definitions you trust.\n"

func presetFetcher() presets.Fetcher {
	return presets.Fetcher{Client: presetHTTPClient}
}

func runPresetAdd(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("preset add", flag.ContinueOnError)
	yes := flags.Bool("yes", false, "install without confirmation")
	specs, err := parsePresetFlags(flags, args, 1)
	if err != nil {
		return err
	}
	location, err := presets.ParseLocation(specs[0])
	if err != nil {
		return err
	}
	sources, err := presets.ReadSources()
	if err != nil {
		return err
	}
	dir, err := presets.UserDir()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fetched, err := presetFetcher().Fetch(ctx, location)
	if err != nil {
		return fmt.Errorf("fetch definitions from %s: %w", specs[0], err)
	}

	var errs []error
	for _, item := range fetched {
		name := item.Definition.Name
		path := filepath.Join(dir, name+".toml")
		if _, ok := presets.Builtin().Lookup(name); ok {
			errs = append(errs, fmt.Errorf("%s: %q is a built-in definition name", item.URL, name))
		} else if source, ok := sources.Lookup(name); ok {
			errs = append(errs, fmt.Errorf("%s: %q is already installed from %s; run projectsetup preset update %s", item.URL, name, source.URL, name))
		} else if _, err := os.Lstat(path); err == nil {
			errs = append(errs, fmt.Errorf("%s: user definition %s already exists; remove it or rename the remote definition", item.URL, path))
		} else if !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("inspect %s: %w", path, err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	for _, item := range fetched {
		definition := item.Definition
		fmt.Fprintf(stdout, "==> %s %s %s from %s\n", definition.Kind, definition.Name, definition.Version, item.URL)
		stdout.Write(definition.Raw)
		if !bytes.HasSuffix(definition.Raw, []byte("\n")) {
			fmt.Fprintln(stdout)
		}
		fmt.Fprintln(stdout)
	}
	io.WriteString(stdout, remoteWarning)
	if !*yes {
		confirmed, err := newPrompter(stdin, stdout).confirm(fmt.Sprintf("Install %d definition(s)?", len(fetched)))
		if err != nil {
			return err
		}
		if !confirmed {
			return errors.New("installation cancelled")
		}
	}

	now := presetNow().UTC().Truncate(time.Second)
	for _, item := range fetched {
		sources.Set(presets.RemoteSource{Name: item.Definition.Name, URL: item.URL, Ref: location.Ref, SHA256: item.Definition.SHA256(), Fetched: now})
	}
	if err := installRemote(sources, dir, fetched); err != nil {
		return err
	}
	for _, item := range fetched {
		fmt.Fprintf(stdout, "Installed %s %s.\n", item.Definition.Name, item.Definition.Version)
	}
	return nil
}

// installRemote records sources first and then writes the definitions, so
// an interruption leaves a recorded definition that Load reports as missing
// or changed, never an unpinned one.
func installRemote(sources presets.Sources, dir string, fetched []presets.Fetched) error {
	if err := presets.WriteSources(sources); err != nil {
		return err
	}
	for _, item := range fetched {
		if err := presets.WriteFileAtomic(filepath.Join(dir, item.Definition.Name+".toml"), item.Definition.Raw); err != nil {
			return err
		}
	}
	return nil
}

func runPresetUpdate(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("preset update", flag.ContinueOnError)
	yes := flags.Bool("yes", false, "install updates without confirmation")
	names, err := parsePresetArgs(flags, args, 1)
	if err != nil {
		return err
	}
	sources, err := presets.ReadSources()
	if err != nil {
		return err
	}
	targets := sources.Definitions
	if len(names) == 1 {
		source, err := lookupRemote(sources, names[0])
		if err != nil {
			return err
		}
		targets = []presets.RemoteSource{source}
	}
	if len(targets) == 0 {
		_, err := fmt.Fprintln(stdout, "No remote definitions are installed.")
		return err
	}
	dir, err := presets.UserDir()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var changed []presets.Fetched
	var errs []error
	for _, source := range targets {
		definition, err := presetFetcher().FetchDefinition(ctx, source.URL)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if definition.Name != source.Name {
			errs = append(errs, fmt.Errorf("%s: now defines %q instead of %q; run projectsetup preset remove %s and add it again", source.URL, definition.Name, source.Name, source.Name))
			continue
		}
		path := filepath.Join(dir, source.Name+".toml")
		current, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("read %s: %w", path, err))
			continue
		}
		if bytes.Equal(current, definition.Raw) && definition.SHA256() == source.SHA256 {
			fmt.Fprintf(stdout, "%s %s is up to date.\n", definition.Name, definition.Version)
			continue
		}
		fmt.Fprint(stdout, unifiedDiff(path, source.URL, string(current), string(definition.Raw)))
		changed = append(changed, presets.Fetched{Definition: definition, URL: source.URL})
	}
	if len(errs) > 0 {
		return fmt.Errorf("update remote definitions; nothing was installed: %w", errors.Join(errs...))
	}
	if len(changed) == 0 {
		return nil
	}
	io.WriteString(stdout, remoteWarning)
	if !*yes {
		confirmed, err := newPrompter(stdin, stdout).confirm(fmt.Sprintf("Install %d update(s)?", len(changed)))
		if err != nil {
			return err
		}
		if !confirmed {
			return errors.New("update cancelled")
		}
	}

	now := presetNow().UTC().Truncate(time.Second)
	for _, item := range changed {
		source, _ := sources.Lookup(item.Definition.Name)
		source.SHA256, source.Fetched = item.Definition.SHA256(), now
		sources.Set(source)
	}
	if err := installRemote(sources, dir, changed); err != nil {
		return err
	}
	for _, item := range changed {
		fmt.Fprintf(stdout, "Updated %s to %s. Run projectsetup upgrade --refresh-presets in projects that should use it.\n", item.Definition.Name, item.Definition.Version)
	}
	return nil
}

func runPresetRemove(args []string, stdout, stderr io.Writer) error {
	names, err := parsePresetFlags(flag.NewFlagSet("preset remove", flag.ContinueOnError), args, 1)
	if err != nil {
		return err
	}
	sources, err := presets.ReadSources()
	if err != nil {
		return err
	}
	source, err := lookupRemote(sources, names[0])
	if err != nil {
		return err
	}
	dir, err := presets.UserDir()
	if err != nil {
		return err
	}
	// Remove the file first, so an interruption leaves a recorded but
	// missing definition rather than an unpinned one.
	path := filepath.Join(dir, source.Name+".toml")
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	sources.Remove(source.Name)
	if err := presets.WriteSources(sources); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "Removed %s. Generated projects keep their copy in .devcontainer/presets.\n", source.Name)
	return err
}

// lookupRemote returns the sources.toml record for name, or an error that
// explains why name is not a remote definition.
func lookupRemote(sources presets.Sources, name string) (presets.RemoteSource, error) {
	if source, ok := sources.Lookup(name); ok {
		return source, nil
	}
	if _, ok := presets.Builtin().Lookup(name); ok {
		return presets.RemoteSource{}, fmt.Errorf("%q is a built-in definition; it changes with projectsetup itself (projectsetup self-update)", name)
	}
	return presets.RemoteSource{}, fmt.Errorf("%q is not a remote definition installed with projectsetup preset add; run projectsetup preset list", name)
}
