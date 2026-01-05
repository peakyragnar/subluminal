package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/peakyragnar/subluminal/pkg/core"
)

var version = "dev"

func runVersion(args []string) int {
	flags := flag.NewFlagSet("version", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "Usage: sub version")
		return 2
	}

	fmt.Fprintf(os.Stdout, "sub %s\n", version)
	fmt.Fprintf(os.Stdout, "interface-pack %s\n", core.InterfaceVersion)
	return 0
}
