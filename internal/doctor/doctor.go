package doctor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"projectsetup/internal/config"
)

type Severity string

const (
	Info    Severity = "info"
	Warning Severity = "warning"
	Error   Severity = "error"
)

type Diagnostic struct {
	Severity Severity
	Subject  string
	Message  string
}

type Environment struct {
	GOOS          string
	GOARCH        string
	Getenv        func(string) string
	UserHomeDir   func() (string, error)
	LookPath      func(string) (string, error)
	Run           func(string, ...string) ([]byte, error)
	Stat          func(string) (fs.FileInfo, error)
	ReadFile      func(string) ([]byte, error)
	CheckWritable func(string) error
}

func Check(root string, environment Environment) []Diagnostic {
	environment = withDefaults(environment)
	var diagnostics []Diagnostic
	add := func(severity Severity, subject, format string, args ...any) {
		diagnostics = append(diagnostics, Diagnostic{severity, subject, fmt.Sprintf(format, args...)})
	}

	add(Info, "host", "GOOS=%s GOARCH=%s", environment.GOOS, environment.GOARCH)
	if environment.GOOS == "linux" && environment.GOARCH == "amd64" {
		add(Info, "dworm compatibility", "host is Linux/amd64; dworm's injected endpoint requires a Linux/amd64 glibc container with /bin/bash")
	} else {
		add(Warning, "dworm compatibility", "host is %s/%s; dworm's injected endpoint requires a Linux/amd64 glibc container with /bin/bash", environment.GOOS, environment.GOARCH)
	}
	add(Info, "dworm compatibility", "dworm exec uses direct docker exec; required environment must be in the image or containerEnv")

	dockerPath := checkTool("docker", []string{"--version"}, Error, environment, add)
	if dockerPath != "" {
		if output, err := environment.Run(dockerPath, "info"); err != nil {
			add(Error, "docker daemon", "unavailable: %s", commandError(err, output))
		} else {
			add(Info, "docker daemon", "available")
		}
	}
	checkTool("devcontainer", []string{"--version"}, Error, environment, add)
	checkTool("dworm", []string{"--version"}, Error, environment, add)

	checkSSH(environment, add)
	checkGPG(environment, add)
	checkAIDirectories(root, environment, add)
	return diagnostics
}

func ErrorCount(diagnostics []Diagnostic) int {
	count := 0
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == Error {
			count++
		}
	}
	return count
}

func checkTool(name string, versionArgs []string, missing Severity, environment Environment, add func(Severity, string, string, ...any)) string {
	path, err := environment.LookPath(name)
	if err != nil {
		add(missing, name, "not found in PATH")
		return ""
	}
	output, err := environment.Run(path, versionArgs...)
	if err != nil {
		add(Error, name, "found at %s but version command failed: %s", path, commandError(err, output))
		return path
	}
	add(Info, name, "%s (%s)", path, oneLine(output))
	return path
}

func checkSSH(environment Environment, add func(Severity, string, string, ...any)) {
	path := environment.Getenv("SSH_AUTH_SOCK")
	if path == "" {
		add(Warning, "SSH_AUTH_SOCK", "not set; SSH agent forwarding will be unavailable")
		return
	}
	info, err := environment.Stat(path)
	if err != nil {
		add(Error, "SSH_AUTH_SOCK", "%s is not accessible: %v", path, err)
		return
	}
	if info.Mode()&os.ModeSocket == 0 {
		add(Error, "SSH_AUTH_SOCK", "%s exists but is not a socket", path)
		return
	}
	add(Info, "SSH_AUTH_SOCK", "%s is a socket", path)
}

func checkGPG(environment Environment, add func(Severity, string, string, ...any)) {
	path, err := environment.LookPath("gpgconf")
	if err != nil {
		add(Warning, "GPG agent", "gpgconf not found; agent socket cannot be discovered")
		return
	}
	output, err := environment.Run(path, "--list-dirs", "agent-socket")
	if err != nil {
		add(Warning, "GPG agent", "socket discovery failed: %s", commandError(err, output))
		return
	}
	socket := strings.TrimSpace(string(output))
	if socket == "" {
		add(Warning, "GPG agent", "gpgconf returned an empty agent socket path")
		return
	}
	info, err := environment.Stat(socket)
	if err != nil {
		add(Warning, "GPG agent", "discovered socket %s is not accessible: %v", socket, err)
		return
	}
	if info.Mode()&os.ModeSocket == 0 {
		add(Warning, "GPG agent", "discovered path %s is not a socket", socket)
		return
	}
	add(Info, "GPG agent", "%s is a socket", socket)
}

func checkAIDirectories(root string, environment Environment, add func(Severity, string, string, ...any)) {
	manifestPath := filepath.Join(root, ".devcontainer", "projectsetup.json")
	data, err := environment.ReadFile(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		add(Info, "AI host directories", "not checked; no projectsetup manifest at %s", manifestPath)
		return
	}
	if err != nil {
		add(Warning, "AI host directories", "cannot read %s: %v", manifestPath, err)
		return
	}
	manifest, err := config.ReadManifest(strings.NewReader(string(data)))
	if err != nil {
		add(Warning, "AI host directories", "cannot determine selected tools from %s: %v", manifestPath, err)
		return
	}
	home, err := environment.UserHomeDir()
	if err != nil {
		add(Error, "AI host directories", "cannot locate home directory: %v", err)
		return
	}
	directories := config.AIHostDirectories(manifest.AITools)
	if len(directories) == 0 {
		add(Info, "AI host directories", "no AI tools are selected")
		return
	}
	for _, relative := range directories {
		path := filepath.Join(home, filepath.FromSlash(relative))
		info, err := environment.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			add(Warning, "AI host directory", "%s is missing", path)
			continue
		}
		if err != nil {
			add(Error, "AI host directory", "%s is not accessible: %v", path, err)
			continue
		}
		if !info.IsDir() {
			add(Error, "AI host directory", "%s is not a directory", path)
			continue
		}
		if err := environment.CheckWritable(path); err != nil {
			add(Error, "AI host directory", "%s is not writable: %v", path, err)
			continue
		}
		add(Info, "AI host directory", "%s exists and is writable", path)
	}
}

func withDefaults(environment Environment) Environment {
	if environment.GOOS == "" {
		environment.GOOS = runtime.GOOS
	}
	if environment.GOARCH == "" {
		environment.GOARCH = runtime.GOARCH
	}
	if environment.Getenv == nil {
		environment.Getenv = os.Getenv
	}
	if environment.UserHomeDir == nil {
		environment.UserHomeDir = os.UserHomeDir
	}
	if environment.LookPath == nil {
		environment.LookPath = exec.LookPath
	}
	if environment.Run == nil {
		environment.Run = func(name string, args ...string) ([]byte, error) {
			return exec.Command(name, args...).CombinedOutput()
		}
	}
	if environment.Stat == nil {
		environment.Stat = os.Stat
	}
	if environment.ReadFile == nil {
		environment.ReadFile = os.ReadFile
	}
	if environment.CheckWritable == nil {
		environment.CheckWritable = checkWritable
	}
	return environment
}

func checkWritable(directory string) error {
	file, err := os.CreateTemp(directory, ".projectsetup-doctor-*")
	if err != nil {
		return err
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Remove(name)
}

func commandError(err error, output []byte) string {
	detail := oneLine(output)
	if detail == "" {
		return err.Error()
	}
	return fmt.Sprintf("%v: %s", err, detail)
}

func oneLine(output []byte) string {
	return strings.Join(strings.Fields(string(output)), " ")
}
