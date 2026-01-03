package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func runDoctor(args []string) int {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	dbPathFlag := flags.String("db", "", "Path to SQLite ledger database")

	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "Unexpected args: %s\n", strings.Join(flags.Args(), " "))
		flags.Usage()
		return 2
	}

	dbPath, err := resolveLedgerPath(*dbPathFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	ok := true
	sqlitePath, err := exec.LookPath("sqlite3")
	if err != nil {
		fmt.Fprintln(os.Stdout, "sqlite3: missing")
		ok = false
	} else {
		fmt.Fprintf(os.Stdout, "sqlite3: %s\n", sqlitePath)
	}

	if info, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stdout, "ledger db: %s (missing)\n", dbPath)
			ok = false
		} else {
			fmt.Fprintf(os.Stdout, "ledger db: %s (error: %v)\n", dbPath, err)
			ok = false
		}
	} else if info.IsDir() {
		fmt.Fprintf(os.Stdout, "ledger db: %s (is a directory)\n", dbPath)
		ok = false
	} else {
		fmt.Fprintf(os.Stdout, "ledger db: %s\n", dbPath)
	}

	if ok {
		fmt.Fprintln(os.Stdout, "doctor: ok")
		return 0
	}
	fmt.Fprintln(os.Stdout, "doctor: issues found")
	return 1
}
