package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/mpm/projectsetup/internal/version"
)

func runSelfUpdate(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("self-update", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("self-update accepts no arguments")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	result, err := version.SelfUpdate(ctx)
	if err != nil {
		return err
	}
	if result.Updated {
		_, err = fmt.Fprintf(stdout, "Installed projectsetup %s at %s. The next invocation uses this update.\n", result.Version, result.Destination)
	} else {
		_, err = fmt.Fprintf(stdout, "projectsetup %s at %s is current or newer than the latest stable release.\n", result.Version, result.Destination)
	}
	return err
}
