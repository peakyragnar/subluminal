package main

import "testing"

func TestBuildToolCallQuery(t *testing.T) {
	filters := toolCallFilters{
		RunID:    "run-1",
		Server:   "server-A",
		Tool:     "tool-B",
		Decision: "ALLOW",
		Status:   "OK",
	}

	query := buildToolCallQuery([]string{"call_id", "run_id"}, filters, true, 25)
	expected := "SELECT call_id, run_id FROM tool_calls WHERE run_id='run-1' AND server_name='server-A' AND tool_name='tool-B' AND decision='ALLOW' AND status='OK' ORDER BY created_at DESC, call_id DESC LIMIT 25"

	if query != expected {
		t.Fatalf("unexpected query:\nexpected: %s\nactual:   %s", expected, query)
	}
}

func TestBuildToolCallQueryAfterCursor(t *testing.T) {
	filters := toolCallFilters{
		RunID:          "run-2",
		AfterCreatedAt: "2024-01-01T00:00:05Z",
		AfterCallID:    "call-9",
	}

	query := buildToolCallQuery([]string{"call_id", "created_at"}, filters, false, 10)
	expected := "SELECT call_id, created_at FROM tool_calls WHERE run_id='run-2' AND (created_at > '2024-01-01T00:00:05Z' OR (created_at = '2024-01-01T00:00:05Z' AND call_id > 'call-9')) ORDER BY created_at ASC, call_id ASC LIMIT 10"

	if query != expected {
		t.Fatalf("unexpected query:\nexpected: %s\nactual:   %s", expected, query)
	}
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
