// Package testharness provides test infrastructure for contract testing.
//
// This file implements an agent driver that simulates an agent making tool calls.
//
// WHY THIS EXISTS:
// The shim intercepts communication between an agent and tool servers.
// To test the shim, we need to simulate an agent sending requests.
// This driver:
//   - Sends JSON-RPC requests (like a real agent would)
//   - Reads JSON-RPC responses
//   - Provides a simple API for tests: driver.CallTool("git_push", args)
package testharness

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// =============================================================================
// AgentDriver simulates an agent making tool calls.
// =============================================================================

// AgentDriver sends requests and receives responses over stdio.
type AgentDriver struct {
	// stdin is where we write requests (to the shim).
	stdin io.Writer

	// stdout is where we read responses (from the shim).
	stdout io.Reader

	// scanner reads lines from stdout.
	scanner *bufio.Scanner

	// nextID generates unique request IDs.
	nextID atomic.Int64

	// mu protects concurrent access.
	mu sync.Mutex

	// responses stores responses by request ID for async matching.
	responses map[int64]chan *JSONRPCResponse
	respMu    sync.Mutex
}

// NewAgentDriver creates a driver connected to the given stdin/stdout.
// Typically: NewAgentDriver(shimStdin, shimStdout)
func NewAgentDriver(stdin io.Writer, stdout io.Reader) *AgentDriver {
	d := &AgentDriver{
		stdin:     stdin,
		stdout:    stdout,
		scanner:   bufio.NewScanner(stdout),
		responses: make(map[int64]chan *JSONRPCResponse),
	}
	return d
}

// StartResponseReader begins reading responses in a goroutine.
// Call this before making any requests.
func (d *AgentDriver) StartResponseReader() {
	go d.readResponses()
}

// readResponses continuously reads from stdout and routes responses.
func (d *AgentDriver) readResponses() {
	for d.scanner.Scan() {
		line := d.scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var resp JSONRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue // Skip malformed responses
		}

		// Route to waiting caller by ID
		if id, ok := resp.ID.(float64); ok {
			d.respMu.Lock()
			if ch, exists := d.responses[int64(id)]; exists {
				ch <- &resp
				delete(d.responses, int64(id))
			}
			d.respMu.Unlock()
		}
	}
}

// =============================================================================
// High-level API for tests
// =============================================================================

// Initialize sends the MCP initialization handshake.
// Must be called first before any tool calls.
func (d *AgentDriver) Initialize() (*JSONRPCResponse, error) {
	return d.sendRequest("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "test-agent",
			"version": "1.0.0",
		},
	})
}

// ListTools requests the available tools from the server.
func (d *AgentDriver) ListTools() (*JSONRPCResponse, error) {
	return d.sendRequest("tools/list", map[string]any{})
}

// CallTool invokes a tool with the given arguments.
// This is the main method tests will use.
//
// Example:
//
//	resp, err := driver.CallTool("git_push", map[string]any{"branch": "main"})
func (d *AgentDriver) CallTool(toolName string, args map[string]any) (*JSONRPCResponse, error) {
	if args == nil {
		args = map[string]any{}
	}
	return d.sendRequest("tools/call", map[string]any{
		"name":      toolName,
		"arguments": args,
	})
}

// CallToolRaw sends a tools/call with raw params (for edge case testing).
func (d *AgentDriver) CallToolRaw(params map[string]any) (*JSONRPCResponse, error) {
	return d.sendRequest("tools/call", params)
}

// =============================================================================
// Response inspection helpers
// =============================================================================

// ToolCallResponse wraps a response with helper methods.
type ToolCallResponse struct {
	*JSONRPCResponse
}

// IsSuccess returns true if the call succeeded (no error).
func (r *ToolCallResponse) IsSuccess() bool {
	return r.Error == nil
}

// IsError returns true if the call returned an error.
func (r *ToolCallResponse) IsError() bool {
	return r.Error != nil
}

// ErrorCode returns the error code, or 0 if no error.
func (r *ToolCallResponse) ErrorCode() int {
	if r.Error == nil {
		return 0
	}
	return r.Error.Code
}

// ErrorMessage returns the error message, or "" if no error.
func (r *ToolCallResponse) ErrorMessage() string {
	if r.Error == nil {
		return ""
	}
	return r.Error.Message
}

// ResultText extracts the text content from a successful tool call.
// Returns "" if not found or error response.
func (r *ToolCallResponse) ResultText() string {
	if r.Error != nil || r.Result == nil {
		return ""
	}

	// Result is typically: {"content": [{"type": "text", "text": "..."}]}
	result, ok := r.Result.(map[string]any)
	if !ok {
		return ""
	}

	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		return ""
	}

	first, ok := content[0].(map[string]any)
	if !ok {
		return ""
	}

	text, _ := first["text"].(string)
	return text
}

// WrapResponse wraps a raw response for easier inspection.
func WrapResponse(resp *JSONRPCResponse) *ToolCallResponse {
	return &ToolCallResponse{resp}
}

// =============================================================================
// Low-level request handling
// =============================================================================

// sendRequest sends a JSON-RPC request and waits for the response.
func (d *AgentDriver) sendRequest(method string, params any) (*JSONRPCResponse, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Generate unique ID
	id := d.nextID.Add(1)

	// Create response channel
	respCh := make(chan *JSONRPCResponse, 1)
	d.respMu.Lock()
	d.responses[id] = respCh
	d.respMu.Unlock()

	// Build request
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
	}

	// Marshal params
	if params != nil {
		paramsBytes, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
		req.Params = paramsBytes
	}

	// Send request
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if _, err := fmt.Fprintf(d.stdin, "%s\n", reqBytes); err != nil {
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	// Wait for response
	resp := <-respCh
	return resp, nil
}

// SendRaw sends a raw JSON line (for malformed request testing).
func (d *AgentDriver) SendRaw(jsonLine string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := fmt.Fprintf(d.stdin, "%s\n", jsonLine)
	return err
}

// Close signals we're done (closes stdin to the shim).
func (d *AgentDriver) Close() error {
	if closer, ok := d.stdin.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}
