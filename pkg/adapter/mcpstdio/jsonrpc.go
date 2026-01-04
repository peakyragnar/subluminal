// Package mcpstdio implements the MCP stdio adapter.
//
// This file defines JSON-RPC 2.0 types used by the MCP protocol.
// The shim parses incoming requests, forwards them to upstream,
// and relays responses back.
//
// JSON-RPC 2.0 Spec: https://www.jsonrpc.org/specification
package mcpstdio

import "encoding/json"

// JSONRPCRequest represents a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"` // Can be string, number, or null for notifications
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id"`
	Result  any           `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Common JSON-RPC error codes
const (
	// Standard JSON-RPC errors
	ErrCodeParseError     = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternalError  = -32603

	// Subluminal-specific error codes (per Interface-Pack §3.2.1)
	ErrCodePolicyBlocked   = -32081
	ErrCodePolicyThrottled = -32082
	ErrCodeRejectWithHint  = -32083
	ErrCodeRunTerminated   = -32084
)

// ToolsCallParams represents the params for a tools/call request.
type ToolsCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// ParseToolsCallParams extracts tool name and arguments from a tools/call request.
// Returns the tool name, arguments, raw arguments bytes, and any error.
func ParseToolsCallParams(params json.RawMessage) (string, map[string]any, json.RawMessage, error) {
	var raw struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments,omitempty"`
	}
	if err := json.Unmarshal(params, &raw); err != nil {
		return "", nil, nil, err
	}

	var args map[string]any
	if len(raw.Arguments) > 0 {
		if err := json.Unmarshal(raw.Arguments, &args); err != nil {
			return "", nil, nil, err
		}
	}
	if args == nil {
		args = make(map[string]any)
	}

	return raw.Name, args, raw.Arguments, nil
}

// IsToolsCall returns true if the request is a tools/call method.
func IsToolsCall(req *JSONRPCRequest) bool {
	return req.Method == "tools/call"
}

// IsNotification returns true if the request is a notification (no ID).
func IsNotification(req *JSONRPCRequest) bool {
	return req.ID == nil
}

// GetRequestID extracts the request ID as a comparable value.
// Returns the ID and true if present, or nil and false if notification.
func GetRequestID(req *JSONRPCRequest) (any, bool) {
	if req.ID == nil {
		return nil, false
	}
	return req.ID, true
}

// NewErrorResponse creates a JSON-RPC error response.
func NewErrorResponse(id any, code int, message string, data any) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}
