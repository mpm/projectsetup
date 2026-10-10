package validate

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/mpm/projectsetup/internal/presets"
)

// Parsers are deliberately keyed by tool identity, not a generic search for a
// version substring. Custom tools remain declarations, not verified releases.
var versionPatterns = map[string]*regexp.Regexp{
	"node":    regexp.MustCompile(`^v([^\s]+)\s*$`),
	"ruby":    regexp.MustCompile(`^ruby ([^\s]+) `),
	"python":  regexp.MustCompile(`^Python ([^\s]+)\s*$`),
	"go":      regexp.MustCompile(`^go version go([^\s]+) `),
	"rust":    regexp.MustCompile(`^rustc ([^\s]+) `),
	"gh":      regexp.MustCompile(`^gh version ([^\s]+) `),
	"npm":     regexp.MustCompile(`^([^\s]+)\s*$`),
	"pnpm":    regexp.MustCompile(`^([^\s]+)\s*$`),
	"yarn":    regexp.MustCompile(`^([^\s]+)\s*$`),
	"bundler": regexp.MustCompile(`^Bundler version ([^\s]+)\s*$`),
	"pip":     regexp.MustCompile(`^pip ([^\s]+) from `),
	"uv":      regexp.MustCompile(`^uv ([^\s]+)(?:\s|$)`),
	"poetry":  regexp.MustCompile(`^Poetry \(version ([^\s)]+)\)\s*$`),
	"cargo":   regexp.MustCompile(`^cargo ([^\s]+) `),
}

const coreRuntimeProbe = `set -euo pipefail
trap 'printf "Failed prerequisite: %s\n" "$BASH_COMMAND" >&2' ERR
test "$(id -un)" = vscode
test "$(id -u)" != 0
test "$HOME" = /home/vscode
test "$(getent passwd vscode | cut -d: -f6)" = /home/vscode
test -x /bin/bash
. /etc/os-release
case "$ID" in debian|ubuntu) ;; *) exit 1 ;; esac
getconf GNU_LIBC_VERSION | /bin/grep -q '^glibc '
for package in bash ca-certificates curl git gnupg sudo; do
  test "$(dpkg-query -W -f='${Status}' "$package")" = 'install ok installed'
done
test -s /etc/ssl/certs/ca-certificates.crt
openssl x509 -in /etc/ssl/certs/ca-certificates.crt -noout >/dev/null
openssl crl2pkcs7 -nocrl -certfile /etc/ssl/certs/ca-certificates.crt | openssl pkcs7 -print_certs -noout >/dev/null
curl --version >/dev/null
git --version >/dev/null
gpg --version >/dev/null
gpg-agent --version >/dev/null
sudo --version >/dev/null
probe=$(mktemp /home/vscode/.projectsetup-runtime.XXXXXX)
trap 'rm -f "$probe"' EXIT
printf 'writable\n' > "$probe"
`

const toolLookupProbe = `set -euo pipefail
trap 'printf "Failed executable/PATH check: %s\n" "$BASH_COMMAND" >&2' ERR
test -x "$1"
found=$(command -v -- "$2")
test "$found" -ef "$1"
`

// The caller has already validated base and final CLI-merged metadata. This
// owned container has no project/credential mounts, no network or image startup
// command, and is never brought up through Dev Container lifecycle execution.
func validateRuntime(image string, contract *presets.Preinstalled, ownership *presets.Ownership, document devcontainerDocument, runner Runner, add func(Severity, string, string, ...any)) {
	fail := func(format string, args ...any) {
		add(Error, ".devcontainer/Dockerfile", "runtime verification: "+format, args...)
	}
	args := []string{"create", "--network", "none", "--no-healthcheck", "--user", "vscode", "--workdir", "/home/vscode"}
	for _, key := range sortedKeys(document.ContainerEnv) {
		args = append(args, "--env", key+"="+document.ContainerEnv[key])
	}
	args = append(args, "--entrypoint", "/bin/sleep", image, "infinity")
	output, err := runner.Run("docker", args...)
	if err != nil {
		fail("create isolated probe for %q: %s", image, commandFailure(err, output))
		return
	}
	id := strings.TrimSpace(string(output))
	if id == "" || strings.ContainsAny(id, " \t\r\n") {
		fail("create isolated probe: Docker returned no valid container ID")
		return
	}
	defer func() {
		if output, err := runner.Run("docker", "rm", "--force", "--volumes", id); err != nil {
			fail("remove isolated probe %s: %s; remove it with docker rm --force --volumes %s", id, commandFailure(err, output), id)
		}
	}()
	if output, err := runner.Run("docker", "start", id); err != nil {
		fail("start isolated probe %s: %s", id, commandFailure(err, output))
		return
	}
	execute := func(command ...string) ([]byte, error) {
		args := []string{"exec", "--user", "vscode"}
		if command[0] == "/bin/bash" {
			// --norc does not suppress BASH_ENV. Inspection shells must not
			// execute an inherited initialization file; direct tool probes keep
			// the image/generated environment unchanged.
			args = append(args, "--env", "BASH_ENV=", "--env", "ENV=")
		}
		return runner.Run("docker", append(append(args, id), command...)...)
	}
	if output, err := execute("/bin/bash", "--noprofile", "--norc", "-c", coreRuntimeProbe); err != nil {
		fail("core prerequisites/user/home probe in %q failed: %s; repair the image's Debian/Ubuntu glibc prerequisites and writable vscode home", image, commandFailure(err, output))
	}
	if ownership != nil {
		probe := fmt.Sprintf(`set -eu; test "$(id -u)" = %d; test "$(id -g)" = %d`, ownership.UID, ownership.GID)
		if output, err := execute("/bin/bash", "--noprofile", "--norc", "-c", probe); err != nil {
			fail("vscode UID/GID differs from fixed contract %d:%d: %s; rebuild the base with configured IDs before installing toolchains", ownership.UID, ownership.GID, commandFailure(err, output))
		}
	}
	if contract == nil {
		return
	}
	for _, key := range sortedKeys(contract.Tools) {
		tool := contract.Tools[key]
		command := path.Base(tool.Executable)
		if output, err := execute("/bin/bash", "--noprofile", "--norc", "-c", toolLookupProbe, "projectsetup-probe", tool.Executable, command); err != nil {
			fail("tool %q executable %q or PATH lookup %q failed: %s; repair the executable or declared PATH", key, tool.Executable, command, commandFailure(err, output))
			continue
		}
		pattern, supported := versionPatterns[key]
		if !supported {
			fail("tool %q at %q has no supported version probe; executable/PATH checks alone cannot verify declared release %q", key, tool.Executable, tool.Version)
			continue
		}
		flag := "--version"
		if key == "go" {
			flag = "version"
		}
		// Check both the absolute declaration and the actual direct-exec lookup.
		for _, executable := range []string{tool.Executable, command} {
			output, err := execute(executable, flag)
			if err != nil {
				fail("tool %q command %q failed: %s", key, executable, commandFailure(err, output))
				continue
			}
			if err := checkReportedVersion(key, tool.Version, output, pattern); err != nil {
				fail("tool %q command %q: %v; repair the artifact or select a matching fixed declaration", key, executable, err)
			}
		}
	}
}

func checkReportedVersion(tool, expected string, output []byte, pattern *regexp.Regexp) error {
	match := pattern.FindStringSubmatch(strings.TrimSpace(string(output)))
	if len(match) != 2 {
		return fmt.Errorf("unrecognized %s version output (expected release %q)", tool, expected)
	}
	if match[1] != expected {
		return fmt.Errorf("reported release %q differs from declared release %q", match[1], expected)
	}
	return nil
}
