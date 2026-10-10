package validate

import (
	"fmt"
	"os"
	"runtime"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/presets"
)

// Fixed IDs deliberately require the Linux caller's primary IDs. Host access
// probes diagnose local bind sources; they cannot prove Docker Desktop mapping,
// remote daemon access, container supplementary groups, or every nested ACL.
func validateHostOwnership(root string, ownership *presets.Ownership, tools []config.AITool, add func(Severity, string, string, ...any)) {
	if problem := hostIDProblem(runtime.GOOS, os.Getuid(), os.Getgid(), ownership); problem != "" {
		add(Error, ".devcontainer/projectsetup.json", "%s", problem)
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return
	}
	paths := []string{root}
	home, err := os.UserHomeDir()
	if err != nil {
		add(Error, ".devcontainer/devcontainer.json", "resolve host ownership sources: %v", err)
		return
	}
	for _, directory := range config.AIHostDirectories(tools) {
		source, err := config.AIHostDirectory(directory, home, os.Getenv)
		if err != nil {
			add(Error, ".devcontainer/devcontainer.json", "resolve host source %s: %v", directory, err)
			continue
		}
		paths = append(paths, source)
	}
	for _, source := range paths {
		if err := hostDirectoryAccess(source); err != nil {
			add(Error, source, "host bind source %q needs write/search access for fixed container IDs %d:%d: %v; repair this source on the host or select a matching image; projectsetup never recursively changes host ownership", source, ownership.UID, ownership.GID, err)
		}
	}
	if runtime.GOOS == "darwin" {
		add(Warning, ".devcontainer/projectsetup.json", "fixed IDs on macOS use Docker Desktop bind sharing, not host numeric ID equality; verify container write access at startup; remote Docker daemons are unsupported")
	}
}

func hostIDProblem(goos string, uid, gid int, ownership *presets.Ownership) string {
	switch goos {
	case "linux":
		if uid != ownership.UID || gid != ownership.GID {
			return fmt.Sprintf("Linux host IDs %d:%d differ from image.ownership fixed vscode IDs %d:%d; select/build a matching image or use a portable preset; do not renumber populated toolchains or recursively chown bind sources", uid, gid, ownership.UID, ownership.GID)
		}
	case "darwin":
	default:
		return "fixed image.ownership is supported on Linux and macOS Docker Desktop only; use a portable preset on this host"
	}
	return ""
}
