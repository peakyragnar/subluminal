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
	expected := "SELECT call_id, run_id FROM tool_calls WHERE run_id='run-1' AND server_name='server-A' AND tool_name='tool-B' AND decision='ALLOW' AND status='OK' ORDER BY created_at DESC LIMIT 25"

	if query != expected {
		t.Fatalf("unexpected query:\nexpected: %s\nactual:   %s", expected, query)
	}
}
