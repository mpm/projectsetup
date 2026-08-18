package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"projectsetup/internal/validate"
)

func runCheck(root string, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("projectsetup check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	build := flags.Bool("build", false, "build the Dev Container after static validation")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("check does not accept positional arguments: %s", strings.Join(flags.Args(), " "))
	}

	diagnostics := validate.Check(root, validate.Options{CheckHostMounts: true, External: true, Build: *build})
	for _, diagnostic := range diagnostics {
		fmt.Fprintf(stderr, "%s: %s: %s\n", diagnostic.Severity, diagnostic.Path, diagnostic.Message)
	}
	errors := validate.ErrorCount(diagnostics)
	warnings := len(diagnostics) - errors
	if errors > 0 {
		return fmt.Errorf("configuration check failed with %d error(s) and %d warning(s)", errors, warnings)
	}
	_, err := fmt.Fprintf(stdout, "Configuration check passed with %d warning(s).\n", warnings)
	return err
}
