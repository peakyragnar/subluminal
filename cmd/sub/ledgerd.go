package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/subluminal/subluminal/pkg/ledger"
)

func runLedgerd(args []string) int {
	flags := flag.NewFlagSet("ledgerd", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	dbPath := flags.String("db", "", "Path to SQLite ledger database")

	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *dbPath == "" {
		fmt.Fprintln(os.Stderr, "Error: --db is required")
		flags.Usage()
		return 2
	}

	if err := ledger.IngestJSONL(os.Stdin, *dbPath); err != nil {
		fmt.Fprintf(os.Stderr, "ledgerd error: %v\n", err)
		return 1
	}
	return 0
}
