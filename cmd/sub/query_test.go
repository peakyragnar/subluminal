package main

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBuildToolCallQuery(t *testing.T) {
	filters := toolCallFilters{
		RunID:    "run-1",
		Server:   "server-A",
		Tool:     "tool-B",
		Decision: "ALLOW",
		Status:   "OK",
	}

	query := buildToolCallQuery([]string{"call_id", "run_id"}, filters, true, 25, 0)
	expectedSQL := "SELECT call_id, run_id FROM tool_calls WHERE run_id = :run_id AND server_name = :server_name AND tool_name = :tool_name AND decision = :decision AND status = :status ORDER BY created_at DESC, call_id DESC LIMIT :limit"
	expectedParams := []sqliteParam{
		{Name: ":run_id", Value: "run-1"},
		{Name: ":server_name", Value: "server-A"},
		{Name: ":tool_name", Value: "tool-B"},
		{Name: ":decision", Value: "ALLOW"},
		{Name: ":status", Value: "OK"},
		{Name: ":limit", Value: "25", IsNumeric: true},
	}

	if query.SQL != expectedSQL {
		t.Fatalf("unexpected query:\nexpected: %s\nactual:   %s", expectedSQL, query.SQL)
	}
	if !reflect.DeepEqual(query.Params, expectedParams) {
		t.Fatalf("unexpected params:\nexpected: %#v\nactual:   %#v", expectedParams, query.Params)
	}
}

func TestBuildToolCallQueryAfterCursor(t *testing.T) {
	filters := toolCallFilters{
		RunID:          "run-2",
		AfterCreatedAt: "2024-01-01T00:00:05Z",
		AfterCallID:    "call-9",
	}

	query := buildToolCallQuery([]string{"call_id", "created_at"}, filters, false, 10, 0)
	expectedSQL := "SELECT call_id, created_at FROM tool_calls WHERE run_id = :run_id AND (created_at > :after_created_at OR (created_at = :after_created_at AND call_id > :after_call_id)) ORDER BY created_at ASC, call_id ASC LIMIT :limit"
	expectedParams := []sqliteParam{
		{Name: ":run_id", Value: "run-2"},
		{Name: ":after_created_at", Value: "2024-01-01T00:00:05Z"},
		{Name: ":after_call_id", Value: "call-9"},
		{Name: ":limit", Value: "10", IsNumeric: true},
	}

	if query.SQL != expectedSQL {
		t.Fatalf("unexpected query:\nexpected: %s\nactual:   %s", expectedSQL, query.SQL)
	}
	if !reflect.DeepEqual(query.Params, expectedParams) {
		t.Fatalf("unexpected params:\nexpected: %#v\nactual:   %#v", expectedParams, query.Params)
	}
}

func TestBuildToolCallQueryToolGlob(t *testing.T) {
	filters := toolCallFilters{
		Tool: "git_*",
	}

	query := buildToolCallQuery([]string{"call_id"}, filters, true, 0, 0)
	expectedSQL := "SELECT call_id FROM tool_calls WHERE tool_name GLOB :tool_name ORDER BY created_at DESC, call_id DESC"
	expectedParams := []sqliteParam{
		{Name: ":tool_name", Value: "git_*"},
	}

	if query.SQL != expectedSQL {
		t.Fatalf("unexpected query:\nexpected: %s\nactual:   %s", expectedSQL, query.SQL)
	}
	if !reflect.DeepEqual(query.Params, expectedParams) {
		t.Fatalf("unexpected params:\nexpected: %#v\nactual:   %#v", expectedParams, query.Params)
	}
}

func TestBuildToolCallQueryOffsetWithoutLimit(t *testing.T) {
	query := buildToolCallQuery([]string{"call_id"}, toolCallFilters{}, true, 0, 20)
	expectedSQL := "SELECT call_id FROM tool_calls ORDER BY created_at DESC, call_id DESC LIMIT -1 OFFSET :offset"
	expectedParams := []sqliteParam{
		{Name: ":offset", Value: "20", IsNumeric: true},
	}

	if query.SQL != expectedSQL {
		t.Fatalf("unexpected query:\nexpected: %s\nactual:   %s", expectedSQL, query.SQL)
	}
	if !reflect.DeepEqual(query.Params, expectedParams) {
		t.Fatalf("unexpected params:\nexpected: %#v\nactual:   %#v", expectedParams, query.Params)
	}
}

func TestParseSinceArgDuration(t *testing.T) {
	now := time.Date(2024, 2, 10, 12, 0, 0, 0, time.UTC)
	got, err := parseSinceArg("1h", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := now.Add(-1 * time.Hour).Format(time.RFC3339Nano)
	if got != expected {
		t.Fatalf("unexpected timestamp:\nexpected: %s\nactual:   %s", expected, got)
	}
}

func TestParseSinceArgTimestamp(t *testing.T) {
	now := time.Date(2024, 2, 10, 12, 0, 0, 0, time.UTC)
	got, err := parseSinceArg("2024-02-10T11:00:00Z", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := time.Date(2024, 2, 10, 11, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	if got != expected {
		t.Fatalf("unexpected timestamp:\nexpected: %s\nactual:   %s", expected, got)
	}
}

func TestParseSinceArgInvalid(t *testing.T) {
	now := time.Date(2024, 2, 10, 12, 0, 0, 0, time.UTC)
	if _, err := parseSinceArg("bogus", now); err == nil {
		t.Fatalf("expected error for invalid since arg")
	}
}

func BenchmarkToolCallQuery10k(b *testing.B) {
	if err := ensureSQLiteAvailable(); err != nil {
		b.Skipf("sqlite3 not available: %v", err)
	}

	dbPath := filepath.Join(b.TempDir(), "ledger.db")
	if err := seedToolCallBenchmarkDB(dbPath, 10000); err != nil {
		b.Fatalf("seed db: %v", err)
	}

	filters := toolCallFilters{
		Decision:       "BLOCK",
		Tool:           "git_*",
		SinceCreatedAt: "2024-01-01T00:00:30Z",
	}
	query := buildToolCallQuery(toolCallColumns, filters, true, 100, 0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := runSQLiteQuery(dbPath, query.SQL, query.Params); err != nil {
			b.Fatalf("query failed: %v", err)
		}
	}
}

func seedToolCallBenchmarkDB(dbPath string, rows int) error {
	var builder strings.Builder
	builder.WriteString("BEGIN;\n")
	builder.WriteString(`CREATE TABLE tool_calls (
		call_id TEXT PRIMARY KEY,
		run_id TEXT,
		server_name TEXT,
		tool_name TEXT,
		decision TEXT,
		status TEXT,
		latency_ms INTEGER,
		bytes_in INTEGER,
		bytes_out INTEGER,
		created_at TEXT
	);` + "\n")
	builder.WriteString("CREATE INDEX idx_tool_calls_run_created ON tool_calls(run_id, created_at);\n")
	builder.WriteString("CREATE INDEX idx_tool_calls_tool ON tool_calls(server_name, tool_name);\n")
	builder.WriteString("CREATE INDEX idx_tool_calls_decision_status ON tool_calls(decision, status);\n")

	for i := 0; i < rows; i++ {
		decision := "ALLOW"
		if i%3 == 0 {
			decision = "BLOCK"
		}
		tool := "git_status"
		if i%5 == 0 {
			tool = "http_get"
		}
		createdAt := fmt.Sprintf("2024-01-01T00:00:%02dZ", i%60)
		fmt.Fprintf(&builder,
			"INSERT INTO tool_calls (call_id, run_id, server_name, tool_name, decision, status, latency_ms, bytes_in, bytes_out, created_at) "+
				"VALUES ('call-%d', 'run-1', 'server', '%s', '%s', 'OK', 5, 100, 200, '%s');\n",
			i,
			tool,
			decision,
			createdAt,
		)
	}

	builder.WriteString("COMMIT;\n")
	_, err := runSQLiteQuery(dbPath, builder.String(), nil)
	return err
}

func TestApplyToolCallRowsEmitsUpdates(t *testing.T) {
	seen := make(map[string]string)

	base := toolCallRow{
		CallID:     "call-1",
		CreatedAt:  "2024-01-01T00:00:01Z",
		RunID:      "run-1",
		ServerName: "server",
		ToolName:   "tool",
		Decision:   "",
		Status:     "",
		LatencyMS:  "0",
		BytesIn:    "10",
		BytesOut:   "0",
	}
	updated := base
	updated.Decision = "ALLOW"
	updated.Status = "OK"

	first := applyToolCallRows([]toolCallRow{base}, seen, true)
	if len(first) != 1 {
		t.Fatalf("expected first row, got %d", len(first))
	}

	second := applyToolCallRows([]toolCallRow{updated}, seen, false)
	if len(second) != 1 {
		t.Fatalf("expected updated row, got %d", len(second))
	}
	if second[0].Decision != "ALLOW" || second[0].Status != "OK" {
		t.Fatalf("unexpected updated row values: decision=%s status=%s", second[0].Decision, second[0].Status)
	}
}

func TestFetchNewToolCallsPaginates(t *testing.T) {
	rows := []toolCallRow{
		{CallID: "call-1", CreatedAt: "2024-01-01T00:00:01Z"},
		{CallID: "call-2", CreatedAt: "2024-01-01T00:00:02Z"},
		{CallID: "call-3", CreatedAt: "2024-01-01T00:00:03Z"},
		{CallID: "call-4", CreatedAt: "2024-01-01T00:00:04Z"},
		{CallID: "call-5", CreatedAt: "2024-01-01T00:00:05Z"},
	}

	fetch := func(filters toolCallFilters, limit int) ([]toolCallRow, error) {
		filtered := make([]toolCallRow, 0, len(rows))
		for _, row := range rows {
			if filters.AfterCreatedAt != "" {
				if row.CreatedAt < filters.AfterCreatedAt {
					continue
				}
				if row.CreatedAt == filters.AfterCreatedAt && row.CallID <= filters.AfterCallID {
					continue
				}
			}
			filtered = append(filtered, row)
		}
		if limit > 0 && len(filtered) > limit {
			filtered = filtered[:limit]
		}
		return filtered, nil
	}

	out, lastCreatedAt, lastCallID, err := fetchNewToolCalls(toolCallFilters{}, 2, "", "", fetch)
	if err != nil {
		t.Fatalf("unexpected fetch error: %v", err)
	}
	if len(out) != len(rows) {
		t.Fatalf("expected %d rows, got %d", len(rows), len(out))
	}
	if lastCreatedAt != rows[len(rows)-1].CreatedAt || lastCallID != rows[len(rows)-1].CallID {
		t.Fatalf("unexpected cursor: %s/%s", lastCreatedAt, lastCallID)
	}
}
