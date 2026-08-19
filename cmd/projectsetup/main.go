package main

import (
	"fmt"
	"os"

	"github.com/mpm/projectsetup/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "projectsetup:", err)
		os.Exit(1)
	}
}
