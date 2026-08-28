package main

import (
	"fmt"
	"os"

	"github.com/mpm/projectsetup/internal/cli"
	"github.com/mpm/projectsetup/internal/version"
)

func main() {
	updateCh := make(chan *version.CheckResult, 1)
	go func() {
		updateCh <- version.CheckForUpdate()
	}()

	err := cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	select {
	case result := <-updateCh:
		if result != nil && result.UpdateAvailable {
			fmt.Fprintf(os.Stderr, "\nA new version of projectsetup is available: %s (current: %s)\n", result.Latest, result.Current)
			fmt.Fprintf(os.Stderr, "Download: %s\n", result.ReleaseURL)
		}
	default:
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "projectsetup:", err)
		os.Exit(1)
	}
}
