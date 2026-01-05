// Package testharness provides test infrastructure for contract testing.
//
// This file implements a fake MCP (Model Context Protocol) server for testing.
// MCP uses JSON-RPC 2.0 over stdio to communicate between agents and tool servers.
//
// WHY THIS EXISTS:
// To test the shim (the thing we're building), we need a predictable tool server.
// Real tool servers are unpredictable. This fake lets us:
//   - Control exactly what tools are available
//   - Control exactly what responses come back
//   - Inject errors, delays, and edge cases
//   - Run tests without external dependencies
package testharness

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// =============================================================================
// JSON-RPC 2.0 Types
// These are the wire format for MCP communication.
// =============================================================================

// JSONRPCRequest is an incoming request from the agent/shim.
// Example: {"jsonrpc":"2.0","id":1,"method":"tools/call","params":{...}}
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"` // Always "2.0"
	ID      any             `json:"id"`      // Request ID (number or string)
	Method  string          `json:"method"`  // e.g., "tools/list", "tools/call"
	Params  json.RawMessage `json:"params"`  // Method-specific parameters
}

// JSONRPCResponse is our response back to the agent/shim.
// Example: {"jsonrpc":"2.0","id":1,"result":{...}}
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`          // Always "2.0"
	ID      any           `json:"id"`               // Matches request ID
	Result  any           `json:"result,omitempty"` // Success payload
	Error   *JSONRPCError `json:"error,omitempty"`  // Error payload (mutually exclusive with Result)
}

// JSONRPCError is the error object when something goes wrong.
// Example: {"code":-32601,"message":"Method not found"}
type JSONRPCError struct {
	Code    int    `json:"code"`           // Error code (negative numbers)
	Message string `json:"message"`        // Human-readable message
	Data    any    `json:"data,omitempty"` // Optional additional data
}

// =============================================================================
// MCP-Specific Types
// These are the payloads inside JSON-RPC for MCP protocol.
// =============================================================================

// Tool represents a tool that the fake server exposes.
type Tool struct {
	Name        string         `json:"name"`        // e.g., "git_push"
	Description string         `json:"description"` // What the tool does
	InputSchema map[string]any `json:"inputSchema"` // JSON Schema for arguments
}

// ToolCallParams is the params for a "tools/call" request.
type ToolCallParams struct {
	Name      string         `json:"name"`      // Which tool to call
	Arguments map[string]any `json:"arguments"` // The arguments
}

// ToolCallResult is the result of a successful tool call.
type ToolCallResult struct {
	Content []ToolContent `json:"content"` // The output
}

// ToolContent is one piece of output from a tool.
type ToolContent struct {
	Type string `json:"type"` // "text" or "image" etc.
	Text string `json:"text"` // The actual content
}

// =============================================================================
// FakeMCPServer
// The main struct that runs the fake server.
// =============================================================================

// ToolHandler is a function that handles a tool call.
// It receives the arguments and returns either a result or an error.
type ToolHandler func(args map[string]any) (string, error)

// FakeMCPServer simulates an MCP tool server for testing.
type FakeMCPServer struct {
	// Tools is the list of tools this server exposes.
	Tools []Tool

	// Handlers maps tool name -> handler function.
	// If no handler exists for a tool, returns a default "ok" response.
	Handlers map[string]ToolHandler

	// DelayMS adds artificial delay to responses (for latency testing).
	DelayMS int

	// mu protects concurrent access.
	mu sync.Mutex

	// calls records all tool calls received (for assertions).
	calls []ToolCallParams

	// RequireEnv lists env vars that must be present for calls to succeed.
	RequireEnv []string
}

// NewFakeMCPServer creates a new fake server with no tools.
// Add tools with AddTool().
func NewFakeMCPServer() *FakeMCPServer {
	return &FakeMCPServer{
		Tools:    []Tool{},
		Handlers: make(map[string]ToolHandler),
		calls:    []ToolCallParams{},
	}
}

// AddTool registers a tool with an optional handler.
// If handler is nil, the tool returns "ok" for any call.
func (s *FakeMCPServer) AddTool(name, description string, handler ToolHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Tools = append(s.Tools, Tool{
		Name:        name,
		Description: description,
		InputSchema: map[string]any{"type": "object"}, // Accept any args
	})

	if handler != nil {
		s.Handlers[name] = handler
	}
}

// GetCalls returns all tool calls received (for test assertions).
func (s *FakeMCPServer) GetCalls() []ToolCallParams {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ToolCallParams{}, s.calls...) // Return a copy
}

// Run starts the server, reading from r and writing to w.
// This blocks until r is closed (EOF).
// Typically: Run(os.Stdin, os.Stdout)
func (s *FakeMCPServer) Run(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	// Increase buffer size to handle large payloads (up to 10 MiB)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()

		// Parse the JSON-RPC request
		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			// Invalid JSON - send parse error
			s.writeError(w, nil, -32700, "Parse error", err.Error())
			continue
		}

		// Handle the request based on method
		resp := s.handleRequest(&req)

		// Write response as single line JSON
		respBytes, _ := json.Marshal(resp)
		fmt.Fprintf(w, "%s\n", respBytes)
	}

	return scanner.Err()
}

// handleRequest routes the request to the appropriate handler.
func (s *FakeMCPServer) handleRequest(req *JSONRPCRequest) *JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	default:
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32601,
				Message: "Method not found",
			},
		}
	}
}

// handleInitialize responds to the MCP initialization handshake.
func (s *FakeMCPServer) handleInitialize(req *JSONRPCRequest) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "fake-mcp-server",
				"version": "1.0.0",
			},
		},
	}
}

// handleToolsList returns the list of available tools.
func (s *FakeMCPServer) handleToolsList(req *JSONRPCRequest) *JSONRPCResponse {
	s.mu.Lock()
	tools := s.Tools
	s.mu.Unlock()

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"tools": tools,
		},
	}
}

// handleToolsCall executes a tool and returns the result.
func (s *FakeMCPServer) handleToolsCall(req *JSONRPCRequest) *JSONRPCResponse {
	if missing := missingEnv(s.RequireEnv); len(missing) > 0 {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32002,
				Message: fmt.Sprintf("missing required env vars: %s", strings.Join(missing, ",")),
			},
		}
	}

	// Parse the call params
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32602,
				Message: "Invalid params",
				Data:    err.Error(),
			},
		}
	}

	// Record the call for test assertions
	s.mu.Lock()
	s.calls = append(s.calls, params)
	handler := s.Handlers[params.Name]
	s.mu.Unlock()

	// Execute the handler (or default)
	var resultText string
	var err error

	if handler != nil {
		resultText, err = handler(params.Arguments)
	} else {
		resultText = "ok" // Default response
	}

	// Return error if handler failed
	if err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32000,
				Message: err.Error(),
			},
		}
	}

	// Return success
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: ToolCallResult{
			Content: []ToolContent{
				{Type: "text", Text: resultText},
			},
		},
	}
}

func missingEnv(required []string) []string {
	if len(required) == 0 {
		return nil
	}
	var missing []string
	for _, name := range required {
		if name == "" {
			continue
		}
		if value, ok := os.LookupEnv(name); !ok || value == "" {
			missing = append(missing, name)
		}
	}
	return missing
}

// writeError is a helper to write an error response.
func (s *FakeMCPServer) writeError(w io.Writer, id any, code int, message, data string) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
	respBytes, _ := json.Marshal(resp)
	fmt.Fprintf(w, "%s\n", respBytes)
}
