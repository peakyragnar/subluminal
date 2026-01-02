package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const defaultTailLimit = 200

func runTail(args []string) int {
	flags := flag.NewFlagSet("tail", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	dbPathFlag := flags.String("db", "", "Path to SQLite ledger database")
	runIDFlag := flags.String("run", "", "Filter by run_id")
	pollFlag := flags.Duration("poll", time.Second, "Polling interval")
	limitFlag := flags.Int("limit", defaultTailLimit, "Rows to scan per poll")

	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "Unexpected args: %s\n", strings.Join(flags.Args(), " "))
		flags.Usage()
		return 2
	}

	if *pollFlag <= 0 {
		fmt.Fprintln(os.Stderr, "Error: --poll must be > 0")
		return 2
	}
	if *limitFlag <= 0 {
		*limitFlag = defaultTailLimit
	}

	dbPath, err := resolveLedgerPath(*dbPathFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	if err := ensureSQLiteAvailable(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := ensureLedgerExists(dbPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	filters := toolCallFilters{RunID: strings.TrimSpace(*runIDFlag)}

	fmt.Fprintln(os.Stdout, toolCallHeader)

	seen := make(map[string]string)
	first := true

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	ticker := time.NewTicker(*pollFlag)
	defer ticker.Stop()

	for {
		rows, err := tailToolCalls(dbPath, filters, *limitFlag)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}

		for _, row := range rows {
			fingerprint := row.fingerprint()
			if !first {
				if prev, ok := seen[row.CallID]; ok && prev == fingerprint {
					continue
				}
			}
			fmt.Fprintln(os.Stdout, row.format())
			seen[row.CallID] = fingerprint
		}
		first = false

		select {
		case <-sigCh:
			return 0
		case <-ticker.C:
		}
	}
}

func tailToolCalls(dbPath string, filters toolCallFilters, limit int) ([]toolCallRow, error) {
	query := buildToolCallQuery(toolCallColumns, filters, true, limit)
	query = fmt.Sprintf("SELECT * FROM (%s) ORDER BY created_at ASC", query)

	output, err := runSQLiteQuery(dbPath, query)
	if err != nil {
		return nil, err
	}
	return parseToolCallRows(output)
}
