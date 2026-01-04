package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/subluminal/subluminal/pkg/core"
	"github.com/subluminal/subluminal/pkg/event"
)

const (
	exportFormatJSONL = "jsonl"
	exportFormatJSON  = "json"
)

const (
	exportRunFieldCount      = 8
	exportToolCallFieldCount = 17
)

type exportRunRow struct {
	RunID        string
	AgentID      string
	Client       string
	Env          string
	StartedAt    string
	EndedAt      string
	Status       string
	MetadataJSON string
}

type exportToolCallRow struct {
	CallID           string
	CreatedAt        string
	RunID            string
	ServerName       string
	ToolName         string
	ArgsHash         string
	Decision         string
	RuleID           string
	Status           string
	LatencyMS        int
	BytesIn          int
	BytesOut         int
	PreviewTruncated bool
	ArgsPreview      string
	ResultPreview    string
	HintText         string
	SuggestedArgs    map[string]any
}

func runExport(args []string) int {
	if len(args) == 0 {
		exportUsage()
		return 2
	}

	switch args[0] {
	case "run":
		return runExportRun(args[1:])
	case "-h", "--help", "help":
		exportUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown export command: %s\n", args[0])
		exportUsage()
		return 2
	}
}

func exportUsage() {
	fmt.Fprintln(os.Stderr, "Usage: sub export <command> [options]")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  run <run_id> [--format=jsonl|json] [--output=PATH] [--db=PATH]")
	fmt.Fprintln(os.Stderr, "  run --last [--format=jsonl|json] [--output=PATH] [--db=PATH]")
}

func runExportRun(args []string) int {
	flags := flag.NewFlagSet("export run", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	dbPathFlag := flags.String("db", "", "Path to SQLite ledger database")
	formatFlag := flags.String("format", exportFormatJSONL, "Output format (jsonl|json)")
	outputFlag := flags.String("output", "", "Write output to file (default stdout)")
	lastFlag := flags.Bool("last", false, "Export most recent run")

	if err := flags.Parse(args); err != nil {
		return 2
	}

	if *lastFlag && flags.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "Error: --last cannot be used with run_id")
		return 2
	}
	if !*lastFlag && flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "Usage: sub export run <run_id> [options]")
		return 2
	}

	format := normalizeExportFormat(*formatFlag)
	if format == "" {
		format = exportFormatJSONL
	}
	if format != exportFormatJSONL && format != exportFormatJSON {
		fmt.Fprintf(os.Stderr, "Error: unsupported format %q\n", *formatFlag)
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

	runID := ""
	if *lastFlag {
		runID, err = fetchLastRunID(dbPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	} else {
		runID = strings.TrimSpace(flags.Arg(0))
		if runID == "" {
			fmt.Fprintln(os.Stderr, "Error: run_id is required")
			return 2
		}
	}

	runRow, runInfo, calls, err := loadExportRun(dbPath, runID)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	events, err := buildExportEvents(runRow, runInfo, calls)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if err := writeExportOutput(format, *outputFlag, events); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	return 0
}

func normalizeExportFormat(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func fetchLastRunID(dbPath string) (string, error) {
	query := "SELECT run_id FROM runs ORDER BY started_at DESC LIMIT 1"
	output, err := runSQLiteQuery(dbPath, query)
	if err != nil {
		return "", err
	}
	return parseLastRunID(output)
}

func parseLastRunID(output string) (string, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return "", fmt.Errorf("no runs found")
	}
	lines := strings.Split(output, "\n")
	runID := strings.TrimSpace(strings.TrimRight(lines[0], "\r"))
	if runID == "" {
		return "", fmt.Errorf("no runs found")
	}
	return runID, nil
}

func loadExportRun(dbPath, runID string) (exportRunRow, event.RunInfo, []exportToolCallRow, error) {
	runQuery := buildExportRunQuery(runID)
	runOutput, err := runSQLiteQuery(dbPath, runQuery)
	if err != nil {
		return exportRunRow{}, event.RunInfo{}, nil, err
	}

	runRow, err := parseExportRunRow(runOutput)
	if err != nil {
		return exportRunRow{}, event.RunInfo{}, nil, err
	}

	runInfo, err := parseRunInfo(runRow)
	if err != nil {
		return exportRunRow{}, event.RunInfo{}, nil, err
	}

	callQuery := buildExportToolCallQuery(runID)
	callOutput, err := runSQLiteQuery(dbPath, callQuery)
	if err != nil {
		return exportRunRow{}, event.RunInfo{}, nil, err
	}

	calls, err := parseExportToolCallRows(callOutput)
	if err != nil {
		return exportRunRow{}, event.RunInfo{}, nil, err
	}

	return runRow, runInfo, calls, nil
}

func buildExportRunQuery(runID string) string {
	return fmt.Sprintf("SELECT run_id, agent_id, client, env, started_at, ended_at, status, metadata_json FROM runs WHERE run_id=%s LIMIT 1", sqlText(runID))
}

func buildExportToolCallQuery(runID string) string {
	return fmt.Sprintf("SELECT tc.call_id, tc.created_at, tc.run_id, tc.server_name, tc.tool_name, tc.args_hash, tc.decision, tc.rule_id, tc.status, tc.latency_ms, tc.bytes_in, tc.bytes_out, tc.preview_truncated, p.args_preview, p.result_preview, h.hint_text, h.suggested_args_json FROM tool_calls tc LEFT JOIN previews p ON p.call_id = tc.call_id LEFT JOIN hints h ON h.call_id = tc.call_id WHERE tc.run_id=%s ORDER BY tc.created_at ASC, tc.call_id ASC", sqlText(runID))
}

func parseExportRunRow(output string) (exportRunRow, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return exportRunRow{}, fmt.Errorf("run not found")
	}

	lines := strings.Split(output, "\n")
	line := strings.TrimRight(lines[0], "\r")
	fields := strings.Split(line, sqliteSeparator)
	if len(fields) < exportRunFieldCount {
		return exportRunRow{}, fmt.Errorf("expected %d columns, got %d", exportRunFieldCount, len(fields))
	}

	return exportRunRow{
		RunID:        fields[0],
		AgentID:      fields[1],
		Client:       fields[2],
		Env:          fields[3],
		StartedAt:    fields[4],
		EndedAt:      fields[5],
		Status:       fields[6],
		MetadataJSON: fields[7],
	}, nil
}

func parseRunInfo(row exportRunRow) (event.RunInfo, error) {
	var info event.RunInfo
	if strings.TrimSpace(row.MetadataJSON) != "" {
		if err := json.Unmarshal([]byte(row.MetadataJSON), &info); err != nil {
			return event.RunInfo{}, fmt.Errorf("parse run metadata: %w", err)
		}
	}
	if info.StartedAt == "" {
		info.StartedAt = row.StartedAt
	}
	return info, nil
}

func parseExportToolCallRows(output string) ([]exportToolCallRow, error) {
	output = strings.Trim(output, "\r\n")
	if output == "" {
		return nil, nil
	}

	lines := strings.Split(output, "\n")
	rows := make([]exportToolCallRow, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		fields := strings.Split(line, sqliteSeparator)
		if len(fields) < exportToolCallFieldCount {
			return nil, fmt.Errorf("expected %d columns, got %d", exportToolCallFieldCount, len(fields))
		}
		row, err := parseExportToolCallRow(fields)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}

	return rows, nil
}

func parseExportToolCallRow(fields []string) (exportToolCallRow, error) {
	latencyMS, err := parseExportInt("latency_ms", fields[9])
	if err != nil {
		return exportToolCallRow{}, err
	}
	bytesIn, err := parseExportInt("bytes_in", fields[10])
	if err != nil {
		return exportToolCallRow{}, err
	}
	bytesOut, err := parseExportInt("bytes_out", fields[11])
	if err != nil {
		return exportToolCallRow{}, err
	}
	previewTruncated, err := parseExportBool("preview_truncated", fields[12])
	if err != nil {
		return exportToolCallRow{}, err
	}
	suggestedArgs, err := parseSuggestedArgs(fields[16])
	if err != nil {
		return exportToolCallRow{}, err
	}

	return exportToolCallRow{
		CallID:           fields[0],
		CreatedAt:        fields[1],
		RunID:            fields[2],
		ServerName:       fields[3],
		ToolName:         fields[4],
		ArgsHash:         fields[5],
		Decision:         fields[6],
		RuleID:           fields[7],
		Status:           fields[8],
		LatencyMS:        latencyMS,
		BytesIn:          bytesIn,
		BytesOut:         bytesOut,
		PreviewTruncated: previewTruncated,
		ArgsPreview:      fields[13],
		ResultPreview:    fields[14],
		HintText:         fields[15],
		SuggestedArgs:    suggestedArgs,
	}, nil
}

func parseExportInt(fieldName, value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", fieldName, err)
	}
	return parsed, nil
}

func parseExportBool(fieldName, value string) (bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return false, nil
	}
	if value == "1" {
		return true, nil
	}
	if value == "0" {
		return false, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return false, fmt.Errorf("%s: %w", fieldName, err)
	}
	return parsed != 0, nil
}

func parseSuggestedArgs(value string) (map[string]any, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(value), &payload); err != nil {
		return nil, fmt.Errorf("suggested_args_json: %w", err)
	}
	if len(payload) == 0 {
		return nil, nil
	}
	return payload, nil
}

func buildExportEvents(runRow exportRunRow, runInfo event.RunInfo, calls []exportToolCallRow) ([]any, error) {
	if runInfo.StartedAt == "" {
		runInfo.StartedAt = runRow.StartedAt
	}

	sort.Slice(calls, func(i, j int) bool {
		if calls[i].CreatedAt == calls[j].CreatedAt {
			return calls[i].CallID < calls[j].CallID
		}
		return calls[i].CreatedAt < calls[j].CreatedAt
	})

	startTS := firstNonEmpty(runInfo.StartedAt, runRow.StartedAt)
	if startTS == "" && len(calls) > 0 {
		startTS = calls[0].CreatedAt
	}

	events := make([]any, 0, 2+len(calls)*3)
	events = append(events, event.RunStartEvent{
		Envelope: makeExportEnvelope(runRow, event.EventTypeRunStart, startTS),
		Run:      runInfo,
	})

	seq := 1
	for _, call := range calls {
		callTS := firstNonEmpty(call.CreatedAt, startTS)

		startEvent := event.ToolCallStartEvent{
			Envelope: makeExportEnvelope(runRow, event.EventTypeToolCallStart, callTS),
			Call: event.CallInfo{
				CallID:     call.CallID,
				ServerName: call.ServerName,
				ToolName:   call.ToolName,
				Transport:  "unknown",
				ArgsHash:   call.ArgsHash,
				BytesIn:    call.BytesIn,
				Preview: event.Preview{
					Truncated:   call.PreviewTruncated,
					ArgsPreview: call.ArgsPreview,
				},
				Seq: seq,
			},
		}
		events = append(events, startEvent)
		seq++

		if strings.TrimSpace(call.Decision) != "" {
			decisionEvent := event.ToolCallDecisionEvent{
				Envelope: makeExportEnvelope(runRow, event.EventTypeToolCallDecision, callTS),
				Call: event.CallRef{
					CallID:     call.CallID,
					ServerName: call.ServerName,
					ToolName:   call.ToolName,
					ArgsHash:   call.ArgsHash,
				},
				Decision: buildExportDecision(call, runInfo.Policy),
			}
			events = append(events, decisionEvent)
		}

		if strings.TrimSpace(call.Status) != "" {
			endTS := computeToolCallEndTS(callTS, call.LatencyMS)
			endEvent := event.ToolCallEndEvent{
				Envelope: makeExportEnvelope(runRow, event.EventTypeToolCallEnd, endTS),
				Call: event.CallRef{
					CallID:     call.CallID,
					ServerName: call.ServerName,
					ToolName:   call.ToolName,
					ArgsHash:   call.ArgsHash,
				},
				Status:    event.CallStatus(call.Status),
				LatencyMS: call.LatencyMS,
				BytesOut:  call.BytesOut,
				Preview: event.ResultPreview{
					Truncated:     call.PreviewTruncated,
					ResultPreview: call.ResultPreview,
				},
			}
			events = append(events, endEvent)
		}
	}

	if strings.TrimSpace(runRow.Status) == "" || strings.TrimSpace(runRow.EndedAt) == "" {
		return events, nil
	}

	endTS := firstNonEmpty(runRow.EndedAt, startTS)
	summary := buildExportSummary(calls, startTS, endTS)
	runEnd := event.RunEndEvent{
		Envelope: makeExportEnvelope(runRow, event.EventTypeRunEnd, endTS),
		Run: event.RunEndInfo{
			EndedAt: endTS,
			Status:  event.RunStatus(runRow.Status),
			Summary: summary,
		},
	}
	events = append(events, runEnd)

	return events, nil
}

func buildExportDecision(call exportToolCallRow, policy event.PolicyInfo) event.Decision {
	var ruleID *string
	if strings.TrimSpace(call.RuleID) != "" {
		rule := call.RuleID
		ruleID = &rule
	}

	var hint *event.Hint
	if strings.TrimSpace(call.HintText) != "" || len(call.SuggestedArgs) > 0 {
		hint = &event.Hint{
			HintText:      call.HintText,
			SuggestedArgs: call.SuggestedArgs,
			HintKind:      event.HintKindOther,
		}
	}

	return event.Decision{
		Action:   event.DecisionAction(call.Decision),
		RuleID:   ruleID,
		Severity: event.SeverityInfo,
		Explain:  event.DecisionExplain{},
		Policy:   policy,
		Hint:     hint,
	}
}

func makeExportEnvelope(runRow exportRunRow, eventType event.EventType, timestamp string) event.Envelope {
	return event.Envelope{
		V:       core.InterfaceVersion,
		Type:    eventType,
		TS:      timestamp,
		RunID:   runRow.RunID,
		AgentID: runRow.AgentID,
		Client:  normalizeExportClient(runRow.Client),
		Env:     normalizeExportEnv(runRow.Env),
		Source:  event.Source{},
	}
}

func normalizeExportClient(value string) event.Client {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "claude":
		return event.ClientClaude
	case "codex":
		return event.ClientCodex
	case "headless":
		return event.ClientHeadless
	case "custom":
		return event.ClientCustom
	case "unknown", "":
		return event.ClientUnknown
	default:
		return event.Client(value)
	}
}

func normalizeExportEnv(value string) event.Env {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "dev":
		return event.EnvDev
	case "ci":
		return event.EnvCI
	case "prod":
		return event.EnvProd
	case "unknown", "":
		return event.EnvUnknown
	default:
		return event.Env(value)
	}
}

func buildExportSummary(calls []exportToolCallRow, startedAt, endedAt string) event.RunSummary {
	summary := event.RunSummary{}
	for _, call := range calls {
		switch strings.ToUpper(strings.TrimSpace(call.Decision)) {
		case string(event.DecisionAllow):
			summary.CallsTotal++
			summary.CallsAllowed++
		case string(event.DecisionBlock), string(event.DecisionRejectWithHint), string(event.DecisionTerminateRun):
			summary.CallsTotal++
			summary.CallsBlocked++
		case string(event.DecisionThrottle):
			summary.CallsTotal++
			summary.CallsThrottled++
		}

		status := strings.ToUpper(strings.TrimSpace(call.Status))
		if status != "" && status != string(event.CallStatusOK) {
			summary.ErrorsTotal++
		}
	}

	summary.DurationMS = computeDurationMS(startedAt, endedAt)
	return summary
}

func computeToolCallEndTS(startedAt string, latencyMS int) string {
	if latencyMS <= 0 {
		return startedAt
	}
	startTime, err := parseTimestamp(startedAt)
	if err != nil {
		return startedAt
	}
	return startTime.Add(time.Duration(latencyMS) * time.Millisecond).Format(time.RFC3339Nano)
}

func computeDurationMS(startedAt, endedAt string) int {
	startTime, err := parseTimestamp(startedAt)
	if err != nil {
		return 0
	}
	endTime, err := parseTimestamp(endedAt)
	if err != nil {
		return 0
	}
	if endTime.Before(startTime) {
		return 0
	}
	return int(endTime.Sub(startTime).Milliseconds())
}

func parseTimestamp(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("timestamp is empty")
	}
	if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return ts, nil
	}
	return time.Parse(time.RFC3339, value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func writeExportOutput(format string, outputPath string, events []any) error {
	writer := io.Writer(os.Stdout)
	var file *os.File
	if strings.TrimSpace(outputPath) != "" {
		var err error
		file, err = os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("open output file: %w", err)
		}
		defer file.Close()
		writer = file
	}

	return writeExportFormat(writer, format, events)
}

func writeExportFormat(w io.Writer, format string, events []any) error {
	switch format {
	case exportFormatJSONL:
		for _, evt := range events {
			data, err := event.SerializeEvent(evt)
			if err != nil {
				return fmt.Errorf("serialize event: %w", err)
			}
			if _, err := w.Write(data); err != nil {
				return fmt.Errorf("write jsonl: %w", err)
			}
		}
		return nil
	case exportFormatJSON:
		data, err := json.Marshal(events)
		if err != nil {
			return fmt.Errorf("serialize json: %w", err)
		}
		if _, err := w.Write(data); err != nil {
			return fmt.Errorf("write json: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}
