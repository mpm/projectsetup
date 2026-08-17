package cli

import (
	"fmt"
	"io"
	"os"
)

const usage = `Usage:
  projectsetup init [flags]
  projectsetup check [--build]
  projectsetup doctor

Commands:
  init    Generate a Dev Container configuration
  check   Validate a generated configuration
  doctor  Diagnose host dependencies and dworm compatibility
`

func Run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		_, err := io.WriteString(stdout, usage)
		return err
	}

	switch args[0] {
	case "help", "-h", "--help":
		_, err := io.WriteString(stdout, usage)
		return err
	case "init":
		root, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("determine current directory: %w", err)
		}
		return runInit(root, args[1:], stdout, stderr)
	case "check":
		return fmt.Errorf("check is not implemented yet")
	case "doctor":
		return fmt.Errorf("doctor is not implemented yet")
	default:
		return fmt.Errorf("unknown command %q; run projectsetup --help for usage", args[0])
	}
}
