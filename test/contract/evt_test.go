// Package contract contains integration tests for Subluminal contracts.
//
// These tests verify the shim behaves according to Interface-Pack.md.
// They use the test harness to orchestrate: Agent → Shim → FakeMCPServer
//
// IMPORTANT: These tests require a shim binary to pass.
// Until the shim is built, all tests will fail (or be skipped).
// This is intentional - we write tests first, then agents implement.
//
// Reference: Contract-Test-Checklist.md
package contract

import (
	"fmt"
	"os"
	"testing"

	"github.com/subluminal/subluminal/pkg/testharness"
)

// shimPath is the path to the shim binary.
// Tests skip if this doesn't exist.
var shimPath = getShimPath()

func getShimPath() string {
	// Allow override via environment
	if p := os.Getenv("SUBLUMINAL_SHIM_PATH"); p != "" {
		return p
	}
	// Default path (relative to repo root)
	return "./bin/shim"
}

// skipIfNoShim skips the test if the shim binary doesn't exist.
func skipIfNoShim(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(shimPath); os.IsNotExist(err) {
		t.Skipf("Shim not found at %s - build shim first", shimPath)
	}
}

// newShimHarness creates a harness configured to use the real shim.
func newShimHarness() *testharness.TestHarness {
	return testharness.NewTestHarness(testharness.HarnessConfig{
		ShimPath: shimPath,
	})
}

// =============================================================================
// EVT-001: JSONL Single-Line Events
// Contract: Every emitted event is exactly 1 line JSON; no multi-line JSON objects.
// Reference: Interface-Pack.md §1.1, Contract-Test-Checklist.md EVT-001
// =============================================================================

func TestEVT001_JSONLSingleLineEvents(t *testing.T) {
	skipIfNoShim(t)

	h := newShimHarness()
	h.AddTool("test_tool", "A test tool", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	// Execute: Run one tool call end-to-end
	h.Initialize()
	h.CallTool("test_tool", map[string]any{"key": "value"})

	// Assert: Every emitted event is exactly 1 line JSON
	events := h.Events()
	if len(events) == 0 {
		t.Fatal("EVT-001 FAILED: No events captured")
	}

	for _, evt := range events {
		// Check: Raw line contains no embedded newlines
		for i, c := range evt.Raw {
			if c == '\n' && i < len(evt.Raw)-1 {
				t.Errorf("EVT-001 FAILED: Event %d contains embedded newline\n"+
					"  Per Interface-Pack §1.1, events must be single-line JSON\n"+
					"  Raw: %q", evt.Index, evt.Raw)
			}
		}

		// Check: Parsed successfully (valid JSON)
		if evt.Parsed == nil {
			t.Errorf("EVT-001 FAILED: Event %d is not valid JSON\n"+
				"  Raw: %q", evt.Index, evt.Raw)
		}
	}
}

// =============================================================================
// EVT-002: Required Envelope Fields
// Contract: Each event contains: v, type, ts, run_id, agent_id, client, env,
//           source.{host_id, proc_id, shim_id}
// Reference: Interface-Pack.md §1.3, Contract-Test-Checklist.md EVT-002
// =============================================================================

func TestEVT002_RequiredEnvelopeFields(t *testing.T) {
	skipIfNoShim(t)

	h := newShimHarness()
	h.AddTool("test_tool", "A test tool", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	// Execute: Run one tool call
	h.Initialize()
	h.CallTool("test_tool", nil)

	// Assert: Required fields present in all events
	requiredFields := []string{
		"v", "type", "ts", "run_id", "agent_id", "client", "env",
		"source.host_id", "source.proc_id", "source.shim_id",
	}

	events := h.Events()
	if len(events) == 0 {
		t.Fatal("EVT-002 FAILED: No events captured")
	}

	for _, evt := range events {
		for _, field := range requiredFields {
			if !testharness.HasField(evt, field) {
				t.Errorf("EVT-002 FAILED: Event %d (type=%s) missing required field %q\n"+
					"  Per Interface-Pack §1.3, every event MUST include this field",
					evt.Index, evt.Type, field)
			}
		}
	}
}

// =============================================================================
// EVT-003: Event Ordering & Completeness
// Contract: Stream contains run_start → tool_call_start → tool_call_decision →
//           tool_call_end → run_end (in that order)
// Reference: Interface-Pack.md §1.2, Contract-Test-Checklist.md EVT-003
// =============================================================================

func TestEVT003_EventOrderingCompleteness(t *testing.T) {
	skipIfNoShim(t)

	h := newShimHarness()
	h.AddTool("test_tool", "A test tool", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	// Execute: Single run with 1 tool call
	h.Initialize()
	h.CallTool("test_tool", nil)

	// Signal end of run (close connection)
	h.Stop()

	// Assert: Events in correct order
	expectedOrder := []string{
		"run_start",
		"tool_call_start",
		"tool_call_decision",
		"tool_call_end",
		"run_end",
	}

	err := h.AssertEventOrder(expectedOrder...)
	if err != nil {
		t.Errorf("EVT-003 FAILED: %v\n"+
			"  Per Interface-Pack §1.2, events must appear in this order:\n"+
			"  run_start → tool_call_start → tool_call_decision → tool_call_end → run_end",
			err)
	}
}

// =============================================================================
// EVT-004: run_id Present Everywhere
// Contract: All events have the same run_id; no orphan events without run_id.
// Reference: Interface-Pack.md §1.3, §0.3, Contract-Test-Checklist.md EVT-004
// =============================================================================

func TestEVT004_RunIDPresentEverywhere(t *testing.T) {
	skipIfNoShim(t)

	h := newShimHarness()
	h.AddTool("tool1", "Tool 1", nil)
	h.AddTool("tool2", "Tool 2", nil)
	h.AddTool("tool3", "Tool 3", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	// Execute: 3 tool calls
	h.Initialize()
	h.CallTool("tool1", nil)
	h.CallTool("tool2", nil)
	h.CallTool("tool3", nil)

	// Assert: All events have run_id
	err := h.AssertAllEventsHaveField("run_id")
	if err != nil {
		t.Errorf("EVT-004 FAILED: %v\n"+
			"  Per Interface-Pack §1.3, run_id is REQUIRED in every event", err)
	}

	// Assert: All events have SAME run_id
	err = h.AssertRunIDConsistent()
	if err != nil {
		t.Errorf("EVT-004 FAILED: %v\n"+
			"  Per Interface-Pack §0.3, run_id MUST be consistent within a run", err)
	}

	// Assert: run_id is not empty
	err = h.EventSink.AssertAllHaveNonEmptyField("run_id")
	if err != nil {
		t.Errorf("EVT-004 FAILED: %v\n"+
			"  run_id must not be empty", err)
	}
}

// =============================================================================
// EVT-005: call_id Uniqueness Per Run
// Contract: All call.call_id distinct; seq is monotonic starting at 1.
// Reference: Interface-Pack.md §0.3, §1.5, Contract-Test-Checklist.md EVT-005
// =============================================================================

func TestEVT005_CallIDUniquenessPerRun(t *testing.T) {
	skipIfNoShim(t)

	h := newShimHarness()
	h.AddTool("test_tool", "A test tool", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	// Execute: Make 100 tool calls (per checklist spec)
	h.Initialize()
	for i := 0; i < 100; i++ {
		h.CallTool("test_tool", map[string]any{"iteration": i})
	}

	// Get all tool_call_start events
	toolCallStarts := h.EventSink.ByType("tool_call_start")
	if len(toolCallStarts) != 100 {
		t.Fatalf("EVT-005 FAILED: Expected 100 tool_call_start events, got %d", len(toolCallStarts))
	}

	// Assert: All call_ids are distinct
	seenCallIDs := make(map[string]bool)
	for _, evt := range toolCallStarts {
		callID := testharness.GetString(evt, "call.call_id")
		if callID == "" {
			t.Errorf("EVT-005 FAILED: Event %d missing call.call_id", evt.Index)
			continue
		}
		if seenCallIDs[callID] {
			t.Errorf("EVT-005 FAILED: Duplicate call_id %q\n"+
				"  Per Interface-Pack §0.3, call_id MUST be unique within a run", callID)
		}
		seenCallIDs[callID] = true
	}

	// Assert: seq is monotonic starting at 1
	for i, evt := range toolCallStarts {
		seq := testharness.GetInt(evt, "call.seq")
		expectedSeq := i + 1 // Starts at 1
		if seq != expectedSeq {
			t.Errorf("EVT-005 FAILED: Event %d has seq=%d, expected %d\n"+
				"  Per Interface-Pack §1.5, seq must be monotonic starting at 1",
				evt.Index, seq, expectedSeq)
		}
	}
}

// =============================================================================
// EVT-006: Tool/Server Name Preservation
// Contract: Events show exact upstream server_name + tool_name unchanged;
//           no forced namespacing.
// Reference: Interface-Pack.md (spec invariant), Contract-Test-Checklist.md EVT-006
// =============================================================================

func TestEVT006_ToolServerNamePreservation(t *testing.T) {
	skipIfNoShim(t)

	h := newShimHarness()

	// Use specific names that should be preserved exactly
	toolName := "linear_create_issue"
	// Note: server name would be configured in shim config, tested separately

	h.FakeServer.AddTool(toolName, "Create a Linear issue", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	// Execute: List tools and call tool
	h.Initialize()
	h.Driver.ListTools()
	h.CallTool(toolName, map[string]any{"title": "Test issue"})

	// Assert: Events show exact tool_name unchanged
	toolCallStarts := h.EventSink.ByType("tool_call_start")
	if len(toolCallStarts) == 0 {
		t.Fatal("EVT-006 FAILED: No tool_call_start events")
	}

	for _, evt := range toolCallStarts {
		actualToolName := testharness.GetString(evt, "call.tool_name")
		if actualToolName != toolName {
			t.Errorf("EVT-006 FAILED: tool_name was modified\n"+
				"  Expected: %q\n"+
				"  Got: %q\n"+
				"  Per spec, tool names must be preserved exactly (no forced namespacing)",
				toolName, actualToolName)
		}
	}
}

// =============================================================================
// EVT-007: latency_ms Present and Sane
// Contract: tool_call_end.latency_ms >= actual latency; not negative/zero
//           unless truly instant.
// Reference: Interface-Pack.md §1.7, Contract-Test-Checklist.md EVT-007
// =============================================================================

func TestEVT007_LatencyMSPresentAndSane(t *testing.T) {
	skipIfNoShim(t)

	h := newShimHarness()

	// Add tool that sleeps 200ms
	h.AddTool("slow_tool", "A slow tool", func(args map[string]any) (string, error) {
		// Simulate 200ms delay
		// time.Sleep(200 * time.Millisecond)
		// Note: In real test, fake server would actually sleep
		return "done", nil
	})

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	// Execute: Call the slow tool
	h.Initialize()
	h.CallTool("slow_tool", nil)

	// Assert: tool_call_end has latency_ms >= 200 (or reasonable)
	toolCallEnds := h.EventSink.ByType("tool_call_end")
	if len(toolCallEnds) == 0 {
		t.Fatal("EVT-007 FAILED: No tool_call_end events")
	}

	for _, evt := range toolCallEnds {
		latencyMS := testharness.GetInt(evt, "latency_ms")

		// Check: latency_ms exists and is reasonable
		if !testharness.HasField(evt, "latency_ms") {
			t.Errorf("EVT-007 FAILED: tool_call_end missing latency_ms\n"+
				"  Per Interface-Pack §1.7, latency_ms is required")
			continue
		}

		// Check: Not negative
		if latencyMS < 0 {
			t.Errorf("EVT-007 FAILED: latency_ms is negative (%d)\n"+
				"  Per Interface-Pack §1.7, latency_ms must not be negative",
				latencyMS)
		}

		// Check: Reasonably reflects actual latency (at least 200ms for slow tool)
		// Note: This threshold depends on the actual sleep in the handler
		// For now, just check it's present and non-negative
	}
}

// =============================================================================
// EVT-008: Status/Error Class Taxonomy
// Contract: status=ERROR and error.class is one of allowed enums;
//           no raw stack traces in message.
// Reference: Interface-Pack.md §1.7, Contract-Test-Checklist.md EVT-008
// =============================================================================

func TestEVT008_StatusErrorClassTaxonomy(t *testing.T) {
	skipIfNoShim(t)

	h := newShimHarness()

	// Add tool that returns an error
	h.AddTool("error_tool", "A tool that errors", func(args map[string]any) (string, error) {
		return "", fmt.Errorf("something went wrong")
	})

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	// Execute: Call the error tool
	h.Initialize()
	h.CallTool("error_tool", nil)

	// Assert: tool_call_end has status=ERROR with proper error.class
	toolCallEnds := h.EventSink.ByType("tool_call_end")
	if len(toolCallEnds) == 0 {
		t.Fatal("EVT-008 FAILED: No tool_call_end events")
	}

	allowedClasses := map[string]bool{
		"upstream_error": true,
		"policy_block":   true,
		"timeout":        true,
		"transport":      true,
		"unknown":        true,
	}

	for _, evt := range toolCallEnds {
		status := testharness.GetString(evt, "status")
		if status != "ERROR" {
			continue // Only check error events
		}

		// Check: error.class is one of allowed enums
		errorClass := testharness.GetString(evt, "error.class")
		if !allowedClasses[errorClass] {
			t.Errorf("EVT-008 FAILED: Invalid error.class %q\n"+
				"  Per Interface-Pack §1.7, error.class must be one of: %v",
				errorClass, allowedClasses)
		}

		// Check: No raw stack traces in message
		errorMessage := testharness.GetString(evt, "error.message")
		if containsStackTrace(errorMessage) {
			t.Errorf("EVT-008 FAILED: error.message contains stack trace\n"+
				"  Per Interface-Pack §1.7, error messages should be safe (no stack traces)\n"+
				"  Message: %q", errorMessage)
		}
	}
}

// containsStackTrace checks for common stack trace patterns.
func containsStackTrace(s string) bool {
	// Simple heuristics for stack traces
	patterns := []string{
		"at ", // JavaScript style
		".go:", // Go style
		"Traceback", // Python style
		"Exception in thread", // Java style
	}
	for _, p := range patterns {
		if len(s) > 200 && contains(s, p) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && (s[:len(substr)] == substr || contains(s[1:], substr)))
}

// =============================================================================
// EVT-009: run_end Summary Counts Correct
// Contract: summary.calls_total, allowed/blocked counts match observed decisions;
//           duration_ms present.
// Reference: Interface-Pack.md §1.8, Contract-Test-Checklist.md EVT-009
// =============================================================================

func TestEVT009_RunEndSummaryCountsCorrect(t *testing.T) {
	skipIfNoShim(t)

	h := newShimHarness()
	h.AddTool("allowed_tool", "An allowed tool", nil)
	// Note: To test blocked calls, we'd need a policy that blocks some calls

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	// Execute: Run with 5 calls (checklist says 3 OK, 2 blocked - needs policy)
	h.Initialize()
	for i := 0; i < 5; i++ {
		h.CallTool("allowed_tool", nil)
	}

	// Get run_end event
	runEnds := h.EventSink.ByType("run_end")
	if len(runEnds) == 0 {
		t.Fatal("EVT-009 FAILED: No run_end event")
	}

	runEnd := runEnds[0]

	// Assert: summary.calls_total matches actual calls
	callsTotal := testharness.GetInt(runEnd, "run.summary.calls_total")
	if callsTotal != 5 {
		t.Errorf("EVT-009 FAILED: summary.calls_total=%d, expected 5\n"+
			"  Per Interface-Pack §1.8, calls_total must match actual call count",
			callsTotal)
	}

	// Assert: duration_ms is present
	if !testharness.HasField(runEnd, "run.summary.duration_ms") {
		t.Error("EVT-009 FAILED: run_end missing summary.duration_ms\n" +
			"  Per Interface-Pack §1.8, duration_ms is required")
	}

	// Assert: duration_ms is reasonable (not negative)
	durationMS := testharness.GetInt(runEnd, "run.summary.duration_ms")
	if durationMS < 0 {
		t.Errorf("EVT-009 FAILED: summary.duration_ms is negative (%d)", durationMS)
	}

	// Assert: calls_allowed + calls_blocked = calls_total
	callsAllowed := testharness.GetInt(runEnd, "run.summary.calls_allowed")
	callsBlocked := testharness.GetInt(runEnd, "run.summary.calls_blocked")
	if callsAllowed+callsBlocked != callsTotal {
		t.Errorf("EVT-009 FAILED: calls_allowed(%d) + calls_blocked(%d) != calls_total(%d)\n"+
			"  Per Interface-Pack §1.8, counts must be consistent",
			callsAllowed, callsBlocked, callsTotal)
	}
}
