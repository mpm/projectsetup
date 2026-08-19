package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/mpm/projectsetup/internal/doctor"
)

func runDoctor(root string, args []string, stdout, stderr io.Writer, environment doctor.Environment) error {
	flags := flag.NewFlagSet("projectsetup doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("doctor does not accept positional arguments: %s", strings.Join(flags.Args(), " "))
	}

	diagnostics := doctor.Check(root, environment)
	for _, diagnostic := range diagnostics {
		writer := stdout
		if diagnostic.Severity != doctor.Info {
			writer = stderr
		}
		fmt.Fprintf(writer, "%s: %s: %s\n", strings.ToUpper(string(diagnostic.Severity)), diagnostic.Subject, diagnostic.Message)
	}
	if errors := doctor.ErrorCount(diagnostics); errors > 0 {
		return fmt.Errorf("doctor found %d error(s)", errors)
	}
	return nil
}
