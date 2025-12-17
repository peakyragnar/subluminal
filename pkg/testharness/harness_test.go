// Package testharness tests verify the test infrastructure works correctly.
//
// These tests run in "direct" mode (no shim) to verify:
//   - FakeMCPServer responds correctly
//   - AgentDriver sends/receives correctly
//   - EventSink captures events correctly
//   - TestHarness ties everything together
//
// Once these pass, we can trust the harness to test the real shim.
package testharness

import (
	"bytes"
	"strings"
	"testing"
)

// =============================================================================
// FakeMCPServer Tests
// =============================================================================

func TestFakeMCPServer_Initialize(t *testing.T) {
	// Setup: create server with pipes
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")
	output := &bytes.Buffer{}

	server := NewFakeMCPServer()
	server.Run(input, output)

	// Verify response
	resp := output.String()
	if !strings.Contains(resp, `"jsonrpc":"2.0"`) {
		t.Errorf("Expected JSON-RPC 2.0 response, got: %s", resp)
	}
	if !strings.Contains(resp, `"id":1`) {
		t.Errorf("Expected id:1 in response, got: %s", resp)
	}
	if !strings.Contains(resp, `"protocolVersion"`) {
		t.Errorf("Expected protocolVersion in response, got: %s", resp)
	}
}

func TestFakeMCPServer_ToolsList(t *testing.T) {
	// Setup: server with one tool
	server := NewFakeMCPServer()
	server.AddTool("git_push", "Push to git", nil)

	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n")
	output := &bytes.Buffer{}

	server.Run(input, output)

	// Verify tool is listed
	resp := output.String()
	if !strings.Contains(resp, `"git_push"`) {
		t.Errorf("Expected git_push in tools list, got: %s", resp)
	}
}

func TestFakeMCPServer_ToolCall(t *testing.T) {
	// Setup: server with handler
	server := NewFakeMCPServer()
	server.AddTool("echo", "Echo input", func(args map[string]any) (string, error) {
		msg, _ := args["message"].(string)
		return "echo: " + msg, nil
	})

	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"message":"hello"}}}` + "\n")
	output := &bytes.Buffer{}

	server.Run(input, output)

	// Verify response contains echoed message
	resp := output.String()
	if !strings.Contains(resp, "echo: hello") {
		t.Errorf("Expected 'echo: hello' in response, got: %s", resp)
	}

	// Verify call was recorded
	calls := server.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("Expected 1 call recorded, got %d", len(calls))
	}
	if calls[0].Name != "echo" {
		t.Errorf("Expected call to 'echo', got: %s", calls[0].Name)
	}
}

func TestFakeMCPServer_UnknownMethod(t *testing.T) {
	server := NewFakeMCPServer()

	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"unknown/method","params":{}}` + "\n")
	output := &bytes.Buffer{}

	server.Run(input, output)

	// Verify error response
	resp := output.String()
	if !strings.Contains(resp, `"error"`) {
		t.Errorf("Expected error response, got: %s", resp)
	}
	if !strings.Contains(resp, "-32601") {
		t.Errorf("Expected error code -32601 (method not found), got: %s", resp)
	}
}

// =============================================================================
// EventSink Tests
// =============================================================================

func TestEventSink_CaptureEvents(t *testing.T) {
	sink := NewEventSink()

	// Simulate JSONL input
	input := strings.NewReader(
		`{"type":"run_start","run_id":"run1","ts":"2025-01-01T00:00:00Z"}` + "\n" +
			`{"type":"tool_call_start","run_id":"run1","call":{"call_id":"c1"}}` + "\n" +
			`{"type":"run_end","run_id":"run1"}` + "\n",
	)

	sink.Capture(input)

	// Verify count
	if sink.Count() != 3 {
		t.Fatalf("Expected 3 events, got %d", sink.Count())
	}

	// Verify types
	types := sink.Types()
	expected := []string{"run_start", "tool_call_start", "run_end"}
	for i, exp := range expected {
		if types[i] != exp {
			t.Errorf("Event %d: expected type %q, got %q", i, exp, types[i])
		}
	}
}

func TestEventSink_ByType(t *testing.T) {
	sink := NewEventSink()

	input := strings.NewReader(
		`{"type":"run_start","run_id":"run1"}` + "\n" +
			`{"type":"tool_call_start","run_id":"run1"}` + "\n" +
			`{"type":"tool_call_start","run_id":"run1"}` + "\n" +
			`{"type":"run_end","run_id":"run1"}` + "\n",
	)

	sink.Capture(input)

	// Filter by type
	toolCalls := sink.ByType("tool_call_start")
	if len(toolCalls) != 2 {
		t.Errorf("Expected 2 tool_call_start events, got %d", len(toolCalls))
	}
}

func TestEventSink_FieldExtraction(t *testing.T) {
	sink := NewEventSink()

	input := strings.NewReader(
		`{"type":"run_start","run_id":"test123","source":{"host_id":"h1","proc_id":"p1"}}` + "\n",
	)

	sink.Capture(input)

	event := sink.First()
	if event == nil {
		t.Fatal("Expected at least one event")
	}

	// Test GetString
	runID := GetString(*event, "run_id")
	if runID != "test123" {
		t.Errorf("Expected run_id 'test123', got %q", runID)
	}

	// Test nested field
	hostID := GetString(*event, "source.host_id")
	if hostID != "h1" {
		t.Errorf("Expected source.host_id 'h1', got %q", hostID)
	}

	// Test HasField
	if !HasField(*event, "run_id") {
		t.Error("Expected HasField('run_id') to be true")
	}
	if HasField(*event, "nonexistent") {
		t.Error("Expected HasField('nonexistent') to be false")
	}
}

func TestEventSink_AssertEventOrder(t *testing.T) {
	sink := NewEventSink()

	input := strings.NewReader(
		`{"type":"run_start"}` + "\n" +
			`{"type":"tool_call_start"}` + "\n" +
			`{"type":"tool_call_decision"}` + "\n" +
			`{"type":"tool_call_end"}` + "\n" +
			`{"type":"run_end"}` + "\n",
	)

	sink.Capture(input)

	// Should pass - correct order
	err := sink.AssertEventOrder("run_start", "tool_call_start", "tool_call_decision", "tool_call_end", "run_end")
	if err != nil {
		t.Errorf("Expected order assertion to pass, got: %v", err)
	}

	// Should pass - subset in order
	err = sink.AssertEventOrder("run_start", "run_end")
	if err != nil {
		t.Errorf("Expected subset order assertion to pass, got: %v", err)
	}

	// Should fail - wrong order
	err = sink.AssertEventOrder("run_end", "run_start")
	if err == nil {
		t.Error("Expected order assertion to fail for wrong order")
	}
}

func TestEventSink_AssertAllHaveField(t *testing.T) {
	sink := NewEventSink()

	input := strings.NewReader(
		`{"type":"run_start","run_id":"r1"}` + "\n" +
			`{"type":"tool_call_start","run_id":"r1"}` + "\n" +
			`{"type":"run_end","run_id":"r1"}` + "\n",
	)

	sink.Capture(input)

	// Should pass - all have run_id
	err := sink.AssertAllHaveField("run_id")
	if err != nil {
		t.Errorf("Expected all-have-field assertion to pass, got: %v", err)
	}

	// Should fail - not all have "missing"
	err = sink.AssertAllHaveField("missing")
	if err == nil {
		t.Error("Expected all-have-field assertion to fail for missing field")
	}
}

func TestEventSink_AssertFieldConsistent(t *testing.T) {
	sink := NewEventSink()

	// All same run_id
	input := strings.NewReader(
		`{"type":"run_start","run_id":"same"}` + "\n" +
			`{"type":"tool_call_start","run_id":"same"}` + "\n" +
			`{"type":"run_end","run_id":"same"}` + "\n",
	)

	sink.Capture(input)

	err := sink.AssertFieldConsistent("run_id")
	if err != nil {
		t.Errorf("Expected consistent field assertion to pass, got: %v", err)
	}
}

func TestEventSink_AssertFieldConsistent_Fails(t *testing.T) {
	sink := NewEventSink()

	// Different run_ids
	input := strings.NewReader(
		`{"type":"run_start","run_id":"one"}` + "\n" +
			`{"type":"run_end","run_id":"two"}` + "\n",
	)

	sink.Capture(input)

	err := sink.AssertFieldConsistent("run_id")
	if err == nil {
		t.Error("Expected consistent field assertion to fail for different values")
	}
}

// =============================================================================
// TestHarness Integration Tests (Direct Mode)
// =============================================================================

func TestHarness_DirectMode_ToolCall(t *testing.T) {
	h := NewDirectHarness()

	// Add a tool
	h.AddTool("greet", "Say hello", func(args map[string]any) (string, error) {
		name, _ := args["name"].(string)
		return "Hello, " + name + "!", nil
	})

	// Start harness
	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	// Initialize
	if err := h.Initialize(); err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	// Make tool call
	resp, err := h.CallTool("greet", map[string]any{"name": "World"})
	if err != nil {
		t.Fatalf("Failed to call tool: %v", err)
	}

	// Verify response
	wrapped := WrapResponse(resp)
	if !wrapped.IsSuccess() {
		t.Errorf("Expected success, got error: %s", wrapped.ErrorMessage())
	}

	result := wrapped.ResultText()
	if result != "Hello, World!" {
		t.Errorf("Expected 'Hello, World!', got: %s", result)
	}
}

func TestHarness_DirectMode_MultipleCalls(t *testing.T) {
	h := NewDirectHarness()

	callCount := 0
	h.AddTool("counter", "Count calls", func(args map[string]any) (string, error) {
		callCount++
		return "ok", nil
	})

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start: %v", err)
	}
	defer h.Stop()

	h.Initialize()

	// Make multiple calls
	for i := 0; i < 5; i++ {
		_, err := h.CallTool("counter", nil)
		if err != nil {
			t.Fatalf("Call %d failed: %v", i, err)
		}
	}

	// Verify all calls were received
	if callCount != 5 {
		t.Errorf("Expected 5 calls, handler received %d", callCount)
	}

	// Verify fake server recorded all calls
	calls := h.FakeServer.GetCalls()
	if len(calls) != 5 {
		t.Errorf("Expected 5 recorded calls, got %d", len(calls))
	}
}

// =============================================================================
// RunTest Helper Tests
// =============================================================================

func TestRunDirectTest_Success(t *testing.T) {
	err := RunDirectTest(func(h *TestHarness) error {
		h.AddTool("ping", "Ping", func(args map[string]any) (string, error) {
			return "pong", nil
		})

		h.Initialize()

		resp, err := h.CallTool("ping", nil)
		if err != nil {
			return err
		}

		if WrapResponse(resp).ResultText() != "pong" {
			return &testError{"expected pong"}
		}
		return nil
	})

	if err != nil {
		t.Errorf("RunDirectTest failed: %v", err)
	}
}

// testError is a simple error for tests
type testError struct {
	msg string
}

func (e *testError) Error() string { return e.msg }
