package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mpm/projectsetup/internal/presets"
)

func validateHostIntegrations(mounts []presets.BindMount, ownership *presets.Ownership, add func(Severity, string, string, ...any)) {
	if len(mounts) == 0 {
		return
	}
	const file = ".devcontainer/devcontainer.json"
	if problem := hostIntegrationPlatformProblem(runtime.GOOS, os.Getenv("DOCKER_HOST"), os.Getenv("DOCKER_CONTEXT")); problem != "" {
		add(Error, file, "%s", problem)
		return
	}
	if os.Getenv("DOCKER_CONTEXT") == "" && os.Getenv("DOCKER_HOST") == "" {
		if err := hostIntegrationDockerConfig(os.Getenv("DOCKER_CONFIG")); err != nil {
			add(Error, file, "%v", err)
			return
		}
	}
	uid, gid := os.Getuid(), os.Getgid()
	if ownership != nil {
		uid, gid = ownership.UID, ownership.GID
	}
	for _, mount := range mounts {
		source, err := presets.ResolveHostSource(mount.Source, os.LookupEnv)
		if err != nil {
			add(Error, file, "resolve host integration source %q for target %q: %v", mount.Source, mount.Target, err)
			continue
		}
		if err := protectedHostSource(source); err != nil {
			add(Error, file, "host integration source %q for target %q: %v", source, mount.Target, err)
			continue
		}
		info, err := os.Stat(source)
		if err != nil {
			add(Error, file, "host integration source %q for target %q must already exist as a %s: %v; projectsetup does not create integration sources", source, mount.Target, mount.SourceKind, err)
			continue
		}
		valid := false
		switch mount.SourceKind {
		case "directory":
			valid = info.IsDir()
		case "file":
			valid = info.Mode().IsRegular()
		case "socket":
			valid = info.Mode()&os.ModeSocket != 0
		}
		if !valid {
			add(Error, file, "host integration source %q for target %q must be a %s; found %s", source, mount.Target, mount.SourceKind, info.Mode().Type())
			continue
		}
		readOnly := mount.ReadOnly != nil && *mount.ReadOnly
		if err := hostIntegrationAccess(source, mount.SourceKind, readOnly, uid, gid); err != nil {
			add(Error, file, "host integration source %q for target %q is inaccessible to container vscode IDs %d:%d: %v; repair host permissions or select a matching image; projectsetup never changes source ownership", source, mount.Target, uid, gid, err)
		}
	}
}

func hostIntegrationPlatformProblem(goos, dockerHost, dockerContext string) string {
	if goos != "linux" {
		return "explicit host integrations require Linux and a local Docker daemon; disable the integration on this host"
	}
	if dockerContext != "" && dockerContext != "default" {
		return "explicit host integrations cannot verify Docker context " + fmt.Sprintf("%q", dockerContext) + "; select the local default context"
	}
	if dockerHost != "" && !strings.HasPrefix(dockerHost, "unix:///") {
		return "explicit host integrations require a local unix Docker socket; remote DOCKER_HOST is unsupported"
	}
	return ""
}

func protectedHostSource(source string) error {
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		return nil
	} // The existence check reports missing sources.
	paths := []string{filepath.Clean(source), resolved}
	for _, path := range paths {
		for ancestor := path; ; ancestor = filepath.Dir(ancestor) {
			if name := filepath.Base(ancestor); name == ".ssh" || name == ".gitconfig" {
				return fmt.Errorf("mounting host .ssh or .gitconfig conflicts with dworm credential forwarding")
			}
			if filepath.Dir(ancestor) == ancestor {
				break
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, name := range []string{".ssh", ".gitconfig"} {
			protected, err := filepath.EvalSymlinks(filepath.Join(home, name))
			if err != nil {
				continue
			}
			rel, err := filepath.Rel(protected, resolved)
			inside := err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
			ancestor, ancestorErr := filepath.Rel(resolved, protected)
			exposes := ancestorErr == nil && ancestor != ".." && !strings.HasPrefix(ancestor, ".."+string(filepath.Separator))
			if inside || exposes {
				return fmt.Errorf("resolved source exposes host %s and conflicts with dworm credential forwarding", name)
			}
		}
	}
	return nil
}

// Docker's selected context may live in its config instead of the environment.
// Nondefault contexts are rejected rather than guessing a daemon's filesystem.
func hostIntegrationDockerConfig(directory string) error {
	if directory == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve Docker config for host integrations: %w", err)
		}
		directory = filepath.Join(home, ".docker")
	}
	path := filepath.Join(directory, "config.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read Docker config %q for host integrations: %w", path, err)
	}
	var config struct {
		CurrentContext string `json:"currentContext"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("parse Docker config %q for host integrations: %w", path, err)
	}
	if config.CurrentContext != "" && config.CurrentContext != "default" {
		return fmt.Errorf("explicit host integrations require the local default Docker context; Docker config %q selects %q", path, config.CurrentContext)
	}
	return nil
}
