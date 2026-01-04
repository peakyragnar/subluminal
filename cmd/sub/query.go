package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

func runQuery(args []string) int {
	flags := flag.NewFlagSet("query", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	dbPathFlag := flags.String("db", "", "Path to SQLite ledger database")
	runIDFlag := flags.String("run", "", "Filter by run_id")
	serverFlag := flags.String("server", "", "Filter by server name")
	toolFlag := flags.String("tool", "", "Filter by tool name (supports glob patterns like git_*)")
	decisionFlag := flags.String("decision", "", "Filter by decision (ALLOW/BLOCK/THROTTLE/REJECT_WITH_HINT/TERMINATE_RUN)")
	statusFlag := flags.String("status", "", "Filter by status (OK/ERROR/TIMEOUT/CANCELLED)")
	sinceFlag := flags.String("since", "", "Filter by created_at since (duration like 1h or RFC3339 timestamp)")
	limitFlag := flags.Int("limit", 50, "Max rows to return (0 for no limit)")
	offsetFlag := flags.Int("offset", 0, "Rows to skip before returning results")
	explainFlag := flags.Bool("explain", false, "Show query plan on stderr")

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
	if *offsetFlag < 0 {
		fmt.Fprintln(os.Stderr, "Error: --offset must be >= 0")
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

	sinceCreatedAt := ""
	if strings.TrimSpace(*sinceFlag) != "" {
		value, err := parseSinceArg(*sinceFlag, time.Now().UTC())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 2
		}
		sinceCreatedAt = value
	}

	filters := toolCallFilters{
		RunID:          strings.TrimSpace(*runIDFlag),
		Server:         strings.TrimSpace(*serverFlag),
		Tool:           strings.TrimSpace(*toolFlag),
		Decision:       normalizeEnum(*decisionFlag),
		Status:         normalizeEnum(*statusFlag),
		SinceCreatedAt: sinceCreatedAt,
	}

	query := buildToolCallQuery(toolCallColumns, filters, true, *limitFlag, *offsetFlag)
	if *explainFlag {
		if err := explainToolCallQuery(dbPath, query); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}

	output, err := runSQLiteQuery(dbPath, query.SQL, query.Params)
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

func parseSinceArg(value string, now time.Time) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}

	if duration, err := time.ParseDuration(value); err == nil {
		if duration < 0 {
			return "", fmt.Errorf("--since must be a positive duration")
		}
		return now.Add(-duration).UTC().Format(time.RFC3339Nano), nil
	}

	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC().Format(time.RFC3339Nano), nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC().Format(time.RFC3339Nano), nil
	}

	return "", fmt.Errorf("invalid --since value: %s", value)
}

func explainToolCallQuery(dbPath string, query sqliteQuery) error {
	planSQL := "EXPLAIN QUERY PLAN " + query.SQL
	output, err := runSQLiteQuery(dbPath, planSQL, query.Params)
	if err != nil {
		return err
	}

	output = strings.TrimSpace(output)
	if output == "" {
		return nil
	}

	fmt.Fprintln(os.Stderr, "Query plan:")
	for _, line := range strings.Split(output, "\n") {
		fmt.Fprintln(os.Stderr, line)
	}
	return nil
}
