package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func runQuery(args []string) int {
	flags := flag.NewFlagSet("query", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	dbPathFlag := flags.String("db", "", "Path to SQLite ledger database")
	runIDFlag := flags.String("run", "", "Filter by run_id")
	serverFlag := flags.String("server", "", "Filter by server name")
	toolFlag := flags.String("tool", "", "Filter by tool name")
	decisionFlag := flags.String("decision", "", "Filter by decision (ALLOW/BLOCK/THROTTLE/REJECT_WITH_HINT/TERMINATE_RUN)")
	statusFlag := flags.String("status", "", "Filter by status (OK/ERROR/TIMEOUT/CANCELLED)")
	limitFlag := flags.Int("limit", 50, "Max rows to return (0 for no limit)")

	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "Unexpected args: %s\n", strings.Join(flags.Args(), " "))
		flags.Usage()
		return 2
	}

	if *limitFlag < 0 {
		fmt.Fprintln(os.Stderr, "Error: --limit must be >= 0")
		return 2
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

	filters := toolCallFilters{
		RunID:    strings.TrimSpace(*runIDFlag),
		Server:   strings.TrimSpace(*serverFlag),
		Tool:     strings.TrimSpace(*toolFlag),
		Decision: normalizeEnum(*decisionFlag),
		Status:   normalizeEnum(*statusFlag),
	}

	query := buildToolCallQuery(toolCallColumns, filters, true, *limitFlag)
	output, err := runSQLiteQuery(dbPath, query)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	rows, err := parseToolCallRows(output)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(rows) == 0 {
		return 0
	}

	fmt.Fprintln(os.Stdout, toolCallHeader)
	for _, row := range rows {
		fmt.Fprintln(os.Stdout, row.format())
	}

	return 0
}

func normalizeEnum(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return strings.ToUpper(value)
}
