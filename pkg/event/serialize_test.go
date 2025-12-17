package event_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/subluminal/subluminal/pkg/event"
)

// =============================================================================
// EVT-001: JSONL Single-Line Events
// Contract: Every emitted event is exactly 1 line JSON; no multi-line JSON objects.
// Reference: Interface-Pack.md §1.1, Contract-Test-Checklist.md EVT-001
// =============================================================================

func TestEVT001_SingleLineJSON(t *testing.T) {
	// Create a valid tool_call_start event
	evt := event.ToolCallStartEvent{
		Envelope: event.Envelope{
			V:       "0.1.0",
			Type:    event.EventTypeToolCallStart,
			TS:      "2025-01-15T12:00:00.000Z",
			RunID:   "run_123",
			AgentID: "agent_456",
			Client:  event.ClientClaude,
			Env:     event.EnvDev,
			Source: event.Source{
				HostID: "host_abc",
				ProcID: "proc_def",
				ShimID: "shim_ghi",
			},
		},
		Call: event.CallInfo{
			CallID:     "call_001",
			ServerName: "git",
			ToolName:   "git_push",
			Transport:  "mcp_stdio",
			ArgsHash:   "abc123def456",
			BytesIn:    256,
			Preview: event.Preview{
				Truncated:   false,
				ArgsPreview: `{"branch":"main"}`,
			},
			Seq: 1,
		},
	}

	output, err := event.SerializeEvent(evt)
	if err != nil {
		t.Fatalf("SerializeEvent returned error: %v", err)
	}

	// Check 1: Output is valid JSON
	// Trim the trailing newline for JSON validation
	jsonPart := bytes.TrimSuffix(output, []byte("\n"))
	if !json.Valid(jsonPart) {
		t.Errorf("EVT-001 FAILED: Output is not valid JSON\n"+
			"  Output: %s", string(output))
	}

	// Check 2: No embedded newlines (before the terminator)
	// Count newlines - should be exactly 1 at the end
	newlineCount := bytes.Count(output, []byte("\n"))
	if newlineCount != 1 {
		t.Errorf("EVT-001 FAILED: Expected exactly 1 newline, got %d\n"+
			"  This means either multi-line JSON or wrong terminator\n"+
			"  Output: %q", newlineCount, string(output))
	}

	// Check 3: Ends with exactly \n (not \r\n or other)
	if len(output) == 0 {
		t.Fatalf("EVT-001 FAILED: Output is empty")
	}
	if output[len(output)-1] != '\n' {
		t.Errorf("EVT-001 FAILED: Output does not end with \\n\n"+
			"  Last byte: %q", output[len(output)-1])
	}

	// Check 4: No \r before \n (Windows line ending)
	if bytes.Contains(output, []byte("\r\n")) {
		t.Errorf("EVT-001 FAILED: Output contains Windows line ending \\r\\n\n"+
			"  JSONL requires Unix line ending \\n only")
	}
}

func TestEVT001_NoMultiLineJSON(t *testing.T) {
	// Even with nested objects and arrays, output must be single line
	evt := event.ToolCallStartEvent{
		Envelope: event.Envelope{
			V:       "0.1.0",
			Type:    event.EventTypeToolCallStart,
			TS:      "2025-01-15T12:00:00.000Z",
			RunID:   "run_with_nested_data",
			AgentID: "agent_nested",
			Client:  event.ClientCodex,
			Env:     event.EnvCI,
			Source: event.Source{
				HostID: "host_1",
				ProcID: "proc_2",
				ShimID: "shim_3",
			},
		},
		Call: event.CallInfo{
			CallID:     "call_nested",
			ServerName: "complex_server",
			ToolName:   "complex_tool",
			Transport:  "mcp_http",
			ArgsHash:   "nested_hash_value",
			BytesIn:    1024,
			Preview: event.Preview{
				Truncated:   true,
				ArgsPreview: "[TRUNCATED]",
			},
			Seq: 42,
		},
	}

	output, err := event.SerializeEvent(evt)
	if err != nil {
		t.Fatalf("SerializeEvent returned error: %v", err)
	}

	// Split by newline - should result in exactly 2 parts:
	// [0] = the JSON line
	// [1] = empty string after trailing newline
	parts := bytes.Split(output, []byte("\n"))
	if len(parts) != 2 || len(parts[1]) != 0 {
		t.Errorf("EVT-001 FAILED: Output is not single-line JSONL\n"+
			"  Expected: 1 JSON line + trailing newline\n"+
			"  Got %d parts: %v", len(parts), parts)
	}
}

func TestEVT001_UTF8Encoding(t *testing.T) {
	// Events with unicode must still be single-line valid JSON
	evt := event.ToolCallStartEvent{
		Envelope: event.Envelope{
			V:       "0.1.0",
			Type:    event.EventTypeToolCallStart,
			TS:      "2025-01-15T12:00:00.000Z",
			RunID:   "run_unicode_日本語",
			AgentID: "agent_emoji_🚀",
			Client:  event.ClientCustom,
			Env:     event.EnvProd,
			Source: event.Source{
				HostID: "host_中文",
				ProcID: "proc_한글",
				ShimID: "shim_🔥",
			},
		},
		Call: event.CallInfo{
			CallID:     "call_unicode",
			ServerName: "server_世界",
			ToolName:   "tool_مرحبا",
			Transport:  "mcp_stdio",
			ArgsHash:   "unicode_hash",
			BytesIn:    512,
			Preview: event.Preview{
				Truncated:   false,
				ArgsPreview: `{"greeting":"こんにちは"}`,
			},
			Seq: 1,
		},
	}

	output, err := event.SerializeEvent(evt)
	if err != nil {
		t.Fatalf("SerializeEvent returned error: %v", err)
	}

	// Must still be valid JSON
	jsonPart := bytes.TrimSuffix(output, []byte("\n"))
	if !json.Valid(jsonPart) {
		t.Errorf("EVT-001 FAILED: Unicode event is not valid JSON\n"+
			"  Output: %s", string(output))
	}

	// Must still be single line
	if bytes.Count(output, []byte("\n")) != 1 {
		t.Errorf("EVT-001 FAILED: Unicode event is not single-line")
	}
}

// =============================================================================
// EVT-002: Required Envelope Fields
// Contract: Each event contains: v, type, ts, run_id, agent_id, client, env,
//           source.{host_id, proc_id, shim_id}
// Reference: Interface-Pack.md §1.3, Contract-Test-Checklist.md EVT-002
// =============================================================================

func TestEVT002_RequiredEnvelopeFields(t *testing.T) {
	evt := event.ToolCallStartEvent{
		Envelope: event.Envelope{
			V:       "0.1.0",
			Type:    event.EventTypeToolCallStart,
			TS:      "2025-01-15T12:00:00.000Z",
			RunID:   "run_required_fields",
			AgentID: "agent_required",
			Client:  event.ClientClaude,
			Env:     event.EnvDev,
			Source: event.Source{
				HostID: "host_required",
				ProcID: "proc_required",
				ShimID: "shim_required",
			},
		},
		Call: event.CallInfo{
			CallID:     "call_req",
			ServerName: "server",
			ToolName:   "tool",
			Transport:  "mcp_stdio",
			ArgsHash:   "hash",
			BytesIn:    100,
			Seq:        1,
		},
	}

	output, err := event.SerializeEvent(evt)
	if err != nil {
		t.Fatalf("SerializeEvent returned error: %v", err)
	}

	// Parse the JSON output to verify fields
	jsonPart := bytes.TrimSuffix(output, []byte("\n"))
	var parsed map[string]any
	if err := json.Unmarshal(jsonPart, &parsed); err != nil {
		t.Fatalf("Failed to parse output as JSON: %v", err)
	}

	// Check all required envelope fields per Interface-Pack §1.3
	requiredFields := []string{"v", "type", "ts", "run_id", "agent_id", "client", "env", "source"}
	for _, field := range requiredFields {
		if _, exists := parsed[field]; !exists {
			t.Errorf("EVT-002 FAILED: Missing required field %q\n"+
				"  Per Interface-Pack §1.3, every event MUST include this field", field)
		}
	}

	// Check source sub-fields
	source, ok := parsed["source"].(map[string]any)
	if !ok {
		t.Fatalf("EVT-002 FAILED: 'source' is not an object")
	}

	sourceFields := []string{"host_id", "proc_id", "shim_id"}
	for _, field := range sourceFields {
		if _, exists := source[field]; !exists {
			t.Errorf("EVT-002 FAILED: Missing required source field %q\n"+
				"  Per Interface-Pack §1.3, source MUST include this field", field)
		}
	}
}

func TestEVT002_FieldValues(t *testing.T) {
	// Verify that field values match what was set
	evt := event.ToolCallStartEvent{
		Envelope: event.Envelope{
			V:       "0.1.0",
			Type:    event.EventTypeToolCallStart,
			TS:      "2025-01-15T12:00:00.000Z",
			RunID:   "run_value_check",
			AgentID: "agent_value_check",
			Client:  event.ClientCodex,
			Env:     event.EnvCI,
			Source: event.Source{
				HostID: "host_val",
				ProcID: "proc_val",
				ShimID: "shim_val",
			},
		},
		Call: event.CallInfo{
			CallID:     "call_val",
			ServerName: "srv",
			ToolName:   "tl",
			Transport:  "mcp_stdio",
			ArgsHash:   "h",
			BytesIn:    1,
			Seq:        1,
		},
	}

	output, err := event.SerializeEvent(evt)
	if err != nil {
		t.Fatalf("SerializeEvent returned error: %v", err)
	}

	jsonPart := bytes.TrimSuffix(output, []byte("\n"))
	var parsed map[string]any
	if err := json.Unmarshal(jsonPart, &parsed); err != nil {
		t.Fatalf("Failed to parse output: %v", err)
	}

	// Verify envelope values
	checks := map[string]any{
		"v":        "0.1.0",
		"type":     "tool_call_start",
		"ts":       "2025-01-15T12:00:00.000Z",
		"run_id":   "run_value_check",
		"agent_id": "agent_value_check",
		"client":   "codex",
		"env":      "ci",
	}

	for field, expected := range checks {
		if got := parsed[field]; got != expected {
			t.Errorf("EVT-002 FAILED: Field %q has wrong value\n"+
				"  Expected: %v\n"+
				"  Got:      %v", field, expected, got)
		}
	}

	// Verify source values
	source := parsed["source"].(map[string]any)
	sourceChecks := map[string]string{
		"host_id": "host_val",
		"proc_id": "proc_val",
		"shim_id": "shim_val",
	}
	for field, expected := range sourceChecks {
		if got := source[field]; got != expected {
			t.Errorf("EVT-002 FAILED: source.%s has wrong value\n"+
				"  Expected: %v\n"+
				"  Got:      %v", field, expected, got)
		}
	}
}

func TestEVT002_CallFields(t *testing.T) {
	// Verify tool_call_start specific fields (call object)
	evt := event.ToolCallStartEvent{
		Envelope: event.Envelope{
			V:       "0.1.0",
			Type:    event.EventTypeToolCallStart,
			TS:      "2025-01-15T12:00:00.000Z",
			RunID:   "run_call_check",
			AgentID: "agent_call",
			Client:  event.ClientClaude,
			Env:     event.EnvDev,
			Source: event.Source{
				HostID: "h",
				ProcID: "p",
				ShimID: "s",
			},
		},
		Call: event.CallInfo{
			CallID:     "call_specific",
			ServerName: "git_server",
			ToolName:   "git_push",
			Transport:  "mcp_stdio",
			ArgsHash:   "abc123hash",
			BytesIn:    512,
			Preview: event.Preview{
				Truncated:   true,
				ArgsPreview: "[TRUNCATED]",
			},
			Seq: 7,
		},
	}

	output, err := event.SerializeEvent(evt)
	if err != nil {
		t.Fatalf("SerializeEvent returned error: %v", err)
	}

	jsonPart := bytes.TrimSuffix(output, []byte("\n"))
	var parsed map[string]any
	if err := json.Unmarshal(jsonPart, &parsed); err != nil {
		t.Fatalf("Failed to parse output: %v", err)
	}

	// Check call object exists
	call, ok := parsed["call"].(map[string]any)
	if !ok {
		t.Fatalf("EVT-002 FAILED: 'call' is not an object or missing")
	}

	// Per Interface-Pack §1.5, tool_call_start requires these call fields
	callRequiredFields := []string{"call_id", "server_name", "tool_name", "transport", "args_hash", "bytes_in", "seq"}
	for _, field := range callRequiredFields {
		if _, exists := call[field]; !exists {
			t.Errorf("EVT-002 FAILED: Missing required call field %q\n"+
				"  Per Interface-Pack §1.5, call MUST include this field", field)
		}
	}

	// Verify specific values
	if call["call_id"] != "call_specific" {
		t.Errorf("EVT-002 FAILED: call.call_id wrong value")
	}
	if call["server_name"] != "git_server" {
		t.Errorf("EVT-002 FAILED: call.server_name wrong value")
	}
	if call["tool_name"] != "git_push" {
		t.Errorf("EVT-002 FAILED: call.tool_name wrong value")
	}
	if call["args_hash"] != "abc123hash" {
		t.Errorf("EVT-002 FAILED: call.args_hash wrong value")
	}
	// JSON numbers are float64
	if call["seq"] != float64(7) {
		t.Errorf("EVT-002 FAILED: call.seq wrong value, got %v", call["seq"])
	}
}
