package cli

import (
	"errors"
	"fmt"
	"io"
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

func Run(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		_, err := io.WriteString(stdout, usage)
		return err
	}

	switch args[0] {
	case "help", "-h", "--help":
		_, err := io.WriteString(stdout, usage)
		return err
	case "init":
		return errors.New("init generation is not implemented yet")
	case "check":
		return errors.New("check is not implemented yet")
	case "doctor":
		return errors.New("doctor is not implemented yet")
	default:
		return fmt.Errorf("unknown command %q; run projectsetup --help for usage", args[0])
	}
}
