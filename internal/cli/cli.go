package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/mpm/projectsetup/internal/doctor"
	"github.com/mpm/projectsetup/internal/version"
)

const usage = `Usage:
  projectsetup init [flags]
  projectsetup upgrade
  projectsetup check [--build]
  projectsetup doctor
  projectsetup version

Commands:
  init    Generate a Dev Container configuration
  upgrade Regenerate an existing projectsetup configuration
  check   Validate a generated configuration
  doctor  Diagnose host dependencies and dworm compatibility
  version Show version and build information
`

func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		_, err := io.WriteString(stdout, usage)
		return err
	}

	switch args[0] {
	case "help", "-h", "--help":
		_, err := io.WriteString(stdout, usage)
		return err
	case "version", "-v", "--version":
		_, err := fmt.Fprintln(stdout, version.Info())
		return err
	case "init":
		root, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("determine current directory: %w", err)
		}
		return runInit(root, args[1:], stdin, stdout, stderr)
	case "check":
		root, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("determine current directory: %w", err)
		}
		return runCheck(root, args[1:], stdout, stderr)
	case "upgrade":
		root, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("determine current directory: %w", err)
		}
		return runUpgrade(root, args[1:], stdout, stderr)
	case "doctor":
		root, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("determine current directory: %w", err)
		}
		return runDoctor(root, args[1:], stdout, stderr, doctor.Environment{})
	default:
		return fmt.Errorf("unknown command %q; run projectsetup --help for usage", args[0])
	}
}
