package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/subluminal/subluminal/pkg/event"
)

func TestBuildExportEventsSummary(t *testing.T) {
	runRow, runInfo, calls := exportFixture(t)

	events, err := buildExportEvents(runRow, runInfo, calls)
	if err != nil {
		t.Fatalf("build export events: %v", err)
	}

	if len(events) != 8 {
		t.Fatalf("expected 8 events, got %d", len(events))
	}

	start, ok := events[0].(event.RunStartEvent)
	if !ok {
		t.Fatalf("expected run_start event, got %T", events[0])
	}
	if start.Run.StartedAt == "" {
		t.Fatal("expected run_start started_at to be set")
	}

	firstCall, ok := events[1].(event.ToolCallStartEvent)
	if !ok {
		t.Fatalf("expected first tool_call_start event, got %T", events[1])
	}
	if firstCall.Call.Seq != 1 {
		t.Fatalf("expected first call seq=1, got %d", firstCall.Call.Seq)
	}

	end, ok := events[len(events)-1].(event.RunEndEvent)
	if !ok {
		t.Fatalf("expected run_end event, got %T", events[len(events)-1])
	}
	if end.Run.Status != event.RunStatus("SUCCEEDED") {
		t.Fatalf("expected run_end status SUCCEEDED, got %q", end.Run.Status)
	}
	if end.Run.Summary.CallsTotal != 2 {
		t.Fatalf("expected calls_total=2, got %d", end.Run.Summary.CallsTotal)
	}
	if end.Run.Summary.CallsAllowed != 1 {
		t.Fatalf("expected calls_allowed=1, got %d", end.Run.Summary.CallsAllowed)
	}
	if end.Run.Summary.CallsBlocked != 1 {
		t.Fatalf("expected calls_blocked=1, got %d", end.Run.Summary.CallsBlocked)
	}
	if end.Run.Summary.ErrorsTotal != 1 {
		t.Fatalf("expected errors_total=1, got %d", end.Run.Summary.ErrorsTotal)
	}
	if end.Run.Summary.DurationMS != 2000 {
		t.Fatalf("expected duration_ms=2000, got %d", end.Run.Summary.DurationMS)
	}
}

func TestWriteExportOutputJSONL(t *testing.T) {
	events := exportEventsFixture(t)

	var buf bytes.Buffer
	if err := writeExportFormat(&buf, exportFormatJSONL, events); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != len(events) {
		t.Fatalf("expected %d jsonl lines, got %d", len(events), len(lines))
	}

	first := parseLineJSON(t, lines[0])
	last := parseLineJSON(t, lines[len(lines)-1])

	if first["type"] != "run_start" {
		t.Fatalf("expected first event run_start, got %v", first["type"])
	}
	if last["type"] != "run_end" {
		t.Fatalf("expected last event run_end, got %v", last["type"])
	}
}

func TestWriteExportOutputJSON(t *testing.T) {
	events := exportEventsFixture(t)

	var buf bytes.Buffer
	if err := writeExportFormat(&buf, exportFormatJSON, events); err != nil {
		t.Fatalf("write json: %v", err)
	}

	var payload []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("parse json array: %v", err)
	}
	if len(payload) != len(events) {
		t.Fatalf("expected %d json items, got %d", len(events), len(payload))
	}
	if payload[0]["type"] != "run_start" {
		t.Fatalf("expected first event run_start, got %v", payload[0]["type"])
	}
	if payload[len(payload)-1]["type"] != "run_end" {
		t.Fatalf("expected last event run_end, got %v", payload[len(payload)-1]["type"])
	}
}

func TestWriteExportOutputFile(t *testing.T) {
	events := exportEventsFixture(t)

	outputPath := filepath.Join(t.TempDir(), "export.jsonl")
	if err := writeExportOutput(exportFormatJSONL, outputPath, events); err != nil {
		t.Fatalf("write output file: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != len(events) {
		t.Fatalf("expected %d lines in file, got %d", len(events), len(lines))
	}
}

func TestParseLastRunID(t *testing.T) {
	runID, err := parseLastRunID("run-42\n")
	if err != nil {
		t.Fatalf("parse last run id: %v", err)
	}
	if runID != "run-42" {
		t.Fatalf("expected run-42, got %s", runID)
	}
}

func exportFixture(t *testing.T) (exportRunRow, event.RunInfo, []exportToolCallRow) {
	t.Helper()

	runInfo := event.RunInfo{
		StartedAt: "2024-01-01T00:00:00Z",
		Mode:      event.RunModeObserve,
		Policy: event.PolicyInfo{
			PolicyID:      "policy-1",
			PolicyVersion: "1",
			PolicyHash:    "hash",
		},
	}

	meta, err := json.Marshal(runInfo)
	if err != nil {
		t.Fatalf("marshal run info: %v", err)
	}

	runRow := exportRunRow{
		RunID:        "run-1",
		AgentID:      "agent-1",
		Client:       "codex",
		Env:          "ci",
		StartedAt:    "2024-01-01T00:00:00Z",
		EndedAt:      "2024-01-01T00:00:02Z",
		Status:       "SUCCEEDED",
		MetadataJSON: string(meta),
	}

	calls := []exportToolCallRow{
		{
			CallID:           "call-1",
			CreatedAt:        "2024-01-01T00:00:01Z",
			RunID:            "run-1",
			ServerName:       "server-a",
			ToolName:         "tool-a",
			ArgsHash:         "hash-1",
			Decision:         "ALLOW",
			Status:           "OK",
			LatencyMS:        10,
			BytesIn:          120,
			BytesOut:         64,
			PreviewTruncated: false,
			ArgsPreview:      `{"hello":"world"}`,
			ResultPreview:    `{"ok":true}`,
		},
		{
			CallID:           "call-2",
			CreatedAt:        "2024-01-01T00:00:02Z",
			RunID:            "run-1",
			ServerName:       "server-b",
			ToolName:         "tool-b",
			ArgsHash:         "hash-2",
			Decision:         "BLOCK",
			RuleID:           "rule-1",
			Status:           "ERROR",
			LatencyMS:        20,
			BytesIn:          90,
			BytesOut:         0,
			PreviewTruncated: true,
			HintText:         "blocked",
			SuggestedArgs: map[string]any{
				"fix": "value",
			},
		},
	}

	return runRow, runInfo, calls
}

func exportEventsFixture(t *testing.T) []any {
	t.Helper()

	runRow, runInfo, calls := exportFixture(t)
	events, err := buildExportEvents(runRow, runInfo, calls)
	if err != nil {
		t.Fatalf("build export events: %v", err)
	}
	return events
}

func parseLineJSON(t *testing.T, line string) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		t.Fatalf("parse json line: %v", err)
	}
	return payload
}
