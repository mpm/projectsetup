package main

import (
	"fmt"
	"os"

	"projectsetup/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "projectsetup:", err)
		os.Exit(1)
	}
}
