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
	lastCreatedAt := ""
	lastCallID := ""

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	ticker := time.NewTicker(*pollFlag)
	defer ticker.Stop()

	for {
		recentRows, err := tailToolCallWindow(dbPath, filters, *limitFlag)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}

		rowsToPrint := applyToolCallRows(recentRows, seen, first)

		if !first {
			paged := filters
			paged.AfterCreatedAt = lastCreatedAt
			paged.AfterCallID = lastCallID
			newRows, err := tailToolCalls(dbPath, paged, *limitFlag)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			rowsToPrint = append(rowsToPrint, applyToolCallRows(newRows, seen, false)...)
			if len(newRows) > 0 {
				lastRow := newRows[len(newRows)-1]
				if isAfterCursor(lastCreatedAt, lastCallID, lastRow) {
					lastCreatedAt = lastRow.CreatedAt
					lastCallID = lastRow.CallID
				}
			}
		}

		if len(recentRows) > 0 {
			lastRow := recentRows[len(recentRows)-1]
			if isAfterCursor(lastCreatedAt, lastCallID, lastRow) {
				lastCreatedAt = lastRow.CreatedAt
				lastCallID = lastRow.CallID
			}
		}

		for _, row := range rowsToPrint {
			fmt.Fprintln(os.Stdout, row.format())
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
	query := buildToolCallQuery(toolCallColumns, filters, false, limit)

	output, err := runSQLiteQuery(dbPath, query)
	if err != nil {
		return nil, err
	}
	return parseToolCallRows(output)
}

func tailToolCallWindow(dbPath string, filters toolCallFilters, limit int) ([]toolCallRow, error) {
	query := buildToolCallQuery(toolCallColumns, filters, true, limit)
	query = fmt.Sprintf("SELECT * FROM (%s) ORDER BY created_at ASC, call_id ASC", query)

	output, err := runSQLiteQuery(dbPath, query)
	if err != nil {
		return nil, err
	}
	return parseToolCallRows(output)
}

func applyToolCallRows(rows []toolCallRow, seen map[string]string, first bool) []toolCallRow {
	out := make([]toolCallRow, 0, len(rows))
	for _, row := range rows {
		fingerprint := row.fingerprint()
		if !first {
			if prev, ok := seen[row.CallID]; ok && prev == fingerprint {
				continue
			}
		}
		out = append(out, row)
		seen[row.CallID] = fingerprint
	}
	return out
}

func isAfterCursor(lastCreatedAt, lastCallID string, row toolCallRow) bool {
	if lastCreatedAt == "" {
		return true
	}
	if row.CreatedAt > lastCreatedAt {
		return true
	}
	if row.CreatedAt == lastCreatedAt && row.CallID > lastCallID {
		return true
	}
	return false
}
