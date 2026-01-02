package ledger

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/subluminal/subluminal/pkg/event"
)

const maxLineBytes = 10 * 1024 * 1024

// IngestJSONL reads JSONL events from r and writes them into the SQLite ledger.
func IngestJSONL(r io.Reader, dbPath string) error {
	if strings.TrimSpace(dbPath) == "" {
		return fmt.Errorf("db path is required")
	}
	if err := ensureDir(dbPath); err != nil {
		return err
	}
	if _, err := exec.LookPath("sqlite3"); err != nil {
		return fmt.Errorf("sqlite3 not found: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "ledgerd-*.sql")
	if err != nil {
		return fmt.Errorf("create temp sql: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	writer := bufio.NewWriter(tmpFile)
	if err := writeSchema(writer); err != nil {
		return err
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var base struct {
			Type event.EventType `json:"type"`
		}
		if err := json.Unmarshal(line, &base); err != nil {
			return fmt.Errorf("line %d: %w", lineNum, err)
		}
		if base.Type == "" {
			return fmt.Errorf("line %d: missing event type", lineNum)
		}

		switch base.Type {
		case event.EventTypeRunStart:
			var evt event.RunStartEvent
			if err := json.Unmarshal(line, &evt); err != nil {
				return fmt.Errorf("line %d: %w", lineNum, err)
			}
			if err := writeRunStart(writer, evt); err != nil {
				return err
			}
		case event.EventTypeRunEnd:
			var evt event.RunEndEvent
			if err := json.Unmarshal(line, &evt); err != nil {
				return fmt.Errorf("line %d: %w", lineNum, err)
			}
			if err := writeRunEnd(writer, evt); err != nil {
				return err
			}
		case event.EventTypeToolCallStart:
			var evt event.ToolCallStartEvent
			if err := json.Unmarshal(line, &evt); err != nil {
				return fmt.Errorf("line %d: %w", lineNum, err)
			}
			if err := writeToolCallStart(writer, evt); err != nil {
				return err
			}
		case event.EventTypeToolCallDecision:
			var evt event.ToolCallDecisionEvent
			if err := json.Unmarshal(line, &evt); err != nil {
				return fmt.Errorf("line %d: %w", lineNum, err)
			}
			if err := writeToolCallDecision(writer, evt); err != nil {
				return err
			}
		case event.EventTypeToolCallEnd:
			var evt event.ToolCallEndEvent
			if err := json.Unmarshal(line, &evt); err != nil {
				return fmt.Errorf("line %d: %w", lineNum, err)
			}
			if err := writeToolCallEnd(writer, evt); err != nil {
				return err
			}
		default:
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}
	if err := writeLine(writer, "COMMIT;"); err != nil {
		return err
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	return runSQLite(dbPath, tmpFile.Name())
}

func ensureDir(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}
	return nil
}

func writeSchema(w *bufio.Writer) error {
	statements := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=NORMAL;",
		`CREATE TABLE IF NOT EXISTS runs (
			run_id TEXT PRIMARY KEY,
			agent_id TEXT,
			client TEXT,
			env TEXT,
			started_at TEXT,
			ended_at TEXT,
			status TEXT,
			metadata_json TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS tool_calls (
			call_id TEXT PRIMARY KEY,
			run_id TEXT,
			server_name TEXT,
			tool_name TEXT,
			args_hash TEXT,
			decision TEXT,
			rule_id TEXT,
			status TEXT,
			latency_ms INTEGER,
			bytes_in INTEGER,
			bytes_out INTEGER,
			preview_truncated INTEGER,
			created_at TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS previews (
			call_id TEXT PRIMARY KEY,
			args_preview TEXT,
			result_preview TEXT,
			redaction_flags TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS hints (
			call_id TEXT PRIMARY KEY,
			hint_text TEXT,
			suggested_args_json TEXT,
			created_at TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS policy_versions (
			policy_id TEXT,
			version TEXT,
			mode TEXT,
			rules_hash TEXT,
			rules_json TEXT,
			created_at TEXT,
			PRIMARY KEY (policy_id, version)
		);`,
		"CREATE INDEX IF NOT EXISTS idx_tool_calls_run_created ON tool_calls(run_id, created_at);",
		"CREATE INDEX IF NOT EXISTS idx_tool_calls_tool ON tool_calls(server_name, tool_name);",
		"CREATE INDEX IF NOT EXISTS idx_tool_calls_decision_status ON tool_calls(decision, status);",
		"CREATE INDEX IF NOT EXISTS idx_tool_calls_args_hash ON tool_calls(args_hash);",
		"BEGIN;",
	}

	for _, stmt := range statements {
		if err := writeLine(w, stmt); err != nil {
			return err
		}
	}
	return nil
}

func writeRunStart(w *bufio.Writer, evt event.RunStartEvent) error {
	metadata, err := marshalJSON(evt.Run)
	if err != nil {
		return err
	}
	stmt := fmt.Sprintf(
		"INSERT INTO runs (run_id, agent_id, client, env, started_at, metadata_json) VALUES (%s, %s, %s, %s, %s, %s) "+
			"ON CONFLICT(run_id) DO UPDATE SET agent_id=excluded.agent_id, client=excluded.client, env=excluded.env, started_at=excluded.started_at, metadata_json=excluded.metadata_json;",
		sqlText(evt.RunID),
		sqlText(evt.AgentID),
		sqlText(string(evt.Client)),
		sqlText(string(evt.Env)),
		sqlText(evt.Run.StartedAt),
		sqlText(metadata),
	)
	if err := writeLine(w, stmt); err != nil {
		return err
	}
	return writePolicyVersion(w, evt.Run.Policy, string(evt.Run.Mode), evt.Run.StartedAt)
}

func writeRunEnd(w *bufio.Writer, evt event.RunEndEvent) error {
	stmt := fmt.Sprintf(
		"INSERT INTO runs (run_id, ended_at, status) VALUES (%s, %s, %s) "+
			"ON CONFLICT(run_id) DO UPDATE SET ended_at=excluded.ended_at, status=excluded.status;",
		sqlText(evt.RunID),
		sqlText(evt.Run.EndedAt),
		sqlText(string(evt.Run.Status)),
	)
	return writeLine(w, stmt)
}

func writeToolCallStart(w *bufio.Writer, evt event.ToolCallStartEvent) error {
	stmt := fmt.Sprintf(
		"INSERT INTO tool_calls (call_id, run_id, server_name, tool_name, args_hash, bytes_in, preview_truncated, created_at) "+
			"VALUES (%s, %s, %s, %s, %s, %d, %s, %s) "+
			"ON CONFLICT(call_id) DO UPDATE SET run_id=excluded.run_id, server_name=excluded.server_name, tool_name=excluded.tool_name, args_hash=excluded.args_hash, bytes_in=excluded.bytes_in, preview_truncated=CASE WHEN excluded.preview_truncated=1 OR tool_calls.preview_truncated=1 THEN 1 ELSE 0 END, created_at=excluded.created_at;",
		sqlText(evt.Call.CallID),
		sqlText(evt.RunID),
		sqlText(evt.Call.ServerName),
		sqlText(evt.Call.ToolName),
		sqlText(evt.Call.ArgsHash),
		evt.Call.BytesIn,
		sqlBool(evt.Call.Preview.Truncated),
		sqlText(evt.TS),
	)
	if err := writeLine(w, stmt); err != nil {
		return err
	}
	return writePreviewArgs(w, evt.Call.CallID, evt.Call.Preview)
}

func writeToolCallDecision(w *bufio.Writer, evt event.ToolCallDecisionEvent) error {
	ruleID := ""
	if evt.Decision.RuleID != nil {
		ruleID = *evt.Decision.RuleID
	}
	stmt := fmt.Sprintf(
		"INSERT INTO tool_calls (call_id, run_id, decision, rule_id, created_at) VALUES (%s, %s, %s, %s, %s) "+
			"ON CONFLICT(call_id) DO UPDATE SET decision=excluded.decision, rule_id=excluded.rule_id;",
		sqlText(evt.Call.CallID),
		sqlText(evt.RunID),
		sqlText(string(evt.Decision.Action)),
		sqlText(ruleID),
		sqlText(evt.TS),
	)
	if err := writeLine(w, stmt); err != nil {
		return err
	}
	if err := writePolicyVersion(w, evt.Decision.Policy, "", evt.TS); err != nil {
		return err
	}
	if evt.Decision.Hint == nil {
		return nil
	}
	return writeHint(w, evt.Call.CallID, *evt.Decision.Hint, evt.TS)
}

func writeToolCallEnd(w *bufio.Writer, evt event.ToolCallEndEvent) error {
	stmt := fmt.Sprintf(
		"INSERT INTO tool_calls (call_id, run_id, status, latency_ms, bytes_out, preview_truncated, created_at) VALUES (%s, %s, %s, %d, %d, %s, %s) "+
			"ON CONFLICT(call_id) DO UPDATE SET status=excluded.status, latency_ms=excluded.latency_ms, bytes_out=excluded.bytes_out, preview_truncated=CASE WHEN excluded.preview_truncated=1 OR tool_calls.preview_truncated=1 THEN 1 ELSE 0 END;",
		sqlText(evt.Call.CallID),
		sqlText(evt.RunID),
		sqlText(string(evt.Status)),
		evt.LatencyMS,
		evt.BytesOut,
		sqlBool(evt.Preview.Truncated),
		sqlText(evt.TS),
	)
	if err := writeLine(w, stmt); err != nil {
		return err
	}
	return writePreviewResult(w, evt.Call.CallID, evt.Preview)
}

func writePreviewArgs(w *bufio.Writer, callID string, preview event.Preview) error {
	stmt := fmt.Sprintf(
		"INSERT INTO previews (call_id, args_preview, redaction_flags) VALUES (%s, %s, %s) "+
			"ON CONFLICT(call_id) DO UPDATE SET args_preview=excluded.args_preview;",
		sqlText(callID),
		sqlText(preview.ArgsPreview),
		sqlText(""),
	)
	return writeLine(w, stmt)
}

func writePreviewResult(w *bufio.Writer, callID string, preview event.ResultPreview) error {
	stmt := fmt.Sprintf(
		"INSERT INTO previews (call_id, result_preview) VALUES (%s, %s) "+
			"ON CONFLICT(call_id) DO UPDATE SET result_preview=excluded.result_preview;",
		sqlText(callID),
		sqlText(preview.ResultPreview),
	)
	return writeLine(w, stmt)
}

func writeHint(w *bufio.Writer, callID string, hint event.Hint, createdAt string) error {
	suggested := ""
	if len(hint.SuggestedArgs) > 0 {
		var err error
		suggested, err = marshalJSON(hint.SuggestedArgs)
		if err != nil {
			return err
		}
	}
	stmt := fmt.Sprintf(
		"INSERT INTO hints (call_id, hint_text, suggested_args_json, created_at) VALUES (%s, %s, %s, %s) "+
			"ON CONFLICT(call_id) DO UPDATE SET hint_text=excluded.hint_text, suggested_args_json=excluded.suggested_args_json, created_at=excluded.created_at;",
		sqlText(callID),
		sqlText(hint.HintText),
		sqlText(suggested),
		sqlText(createdAt),
	)
	return writeLine(w, stmt)
}

func writePolicyVersion(w *bufio.Writer, policy event.PolicyInfo, mode string, createdAt string) error {
	policyID := strings.TrimSpace(policy.PolicyID)
	version := strings.TrimSpace(policy.PolicyVersion)
	if policyID == "" || version == "" {
		return nil
	}
	stmt := fmt.Sprintf(
		"INSERT INTO policy_versions (policy_id, version, mode, rules_hash, rules_json, created_at) VALUES (%s, %s, %s, %s, %s, %s) "+
			"ON CONFLICT(policy_id, version) DO UPDATE SET "+
			"mode=COALESCE(excluded.mode, policy_versions.mode), "+
			"rules_hash=COALESCE(excluded.rules_hash, policy_versions.rules_hash), "+
			"rules_json=COALESCE(excluded.rules_json, policy_versions.rules_json), "+
			"created_at=COALESCE(policy_versions.created_at, excluded.created_at);",
		sqlText(policyID),
		sqlText(version),
		sqlText(mode),
		sqlText(policy.PolicyHash),
		sqlText(""),
		sqlText(createdAt),
	)
	return writeLine(w, stmt)
}

func runSQLite(dbPath, sqlPath string) error {
	cmd := exec.Command("sqlite3", "-batch", "-bail", dbPath)
	sqlFile, err := os.Open(sqlPath)
	if err != nil {
		return fmt.Errorf("open sql: %w", err)
	}
	defer sqlFile.Close()
	cmd.Stdin = sqlFile

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sqlite3 failed: %w: %s", err, strings.TrimSpace(output.String()))
	}
	return nil
}

func writeLine(w *bufio.Writer, line string) error {
	if _, err := w.WriteString(line); err != nil {
		return err
	}
	if err := w.WriteByte('\n'); err != nil {
		return err
	}
	return nil
}

func marshalJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal metadata: %w", err)
	}
	return string(data), nil
}

func sqlText(value string) string {
	if value == "" {
		return "NULL"
	}
	escaped := strings.ReplaceAll(value, "'", "''")
	return "'" + escaped + "'"
}

func sqlBool(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
