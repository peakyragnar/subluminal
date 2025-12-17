// Package event provides event types and JSONL serialization per Interface-Pack §1.1-1.3.
//
// PURPOSE IN SUBLUMINAL:
// Every tool call an agent makes generates a sequence of events:
//   run_start → tool_call_start → tool_call_decision → tool_call_end → run_end
//
// These events are:
//   - Streamed to stdout (JSONL format) for real-time observability
//   - Stored in the ledger (SQLite) for querying and audit
//   - Used by the UI (DVR) to display run timelines
//
// CONTRACT REQUIREMENTS (Interface-Pack.md §1.1-1.3):
// - JSONL format: one JSON object per line, UTF-8, \n terminator
// - Every event has a common envelope with required fields
// - Events are typed (run_start, tool_call_start, etc.)
package event

// Client represents the agent client type.
// Per Interface-Pack §1.3: "claude" | "codex" | "headless" | "custom" | "unknown"
type Client string

const (
	ClientClaude   Client = "claude"
	ClientCodex    Client = "codex"
	ClientHeadless Client = "headless"
	ClientCustom   Client = "custom"
	ClientUnknown  Client = "unknown"
)

// Env represents the execution environment.
// Per Interface-Pack §1.3: "dev" | "ci" | "prod" | "unknown"
type Env string

const (
	EnvDev     Env = "dev"
	EnvCI      Env = "ci"
	EnvProd    Env = "prod"
	EnvUnknown Env = "unknown"
)

// EventType represents the type of event.
// Per Interface-Pack §1.2
type EventType string

const (
	EventTypeRunStart         EventType = "run_start"
	EventTypeToolCallStart    EventType = "tool_call_start"
	EventTypeToolCallDecision EventType = "tool_call_decision"
	EventTypeToolCallEnd      EventType = "tool_call_end"
	EventTypeRunEnd           EventType = "run_end"
)

// Source identifies the producer instance.
// Per Interface-Pack §1.3
type Source struct {
	HostID string `json:"host_id"` // Stable per-machine ID
	ProcID string `json:"proc_id"` // Stable per-process ID
	ShimID string `json:"shim_id"` // Unique per shim instance
}

// Envelope contains the required fields for every event.
// Per Interface-Pack §1.3
type Envelope struct {
	V       string    `json:"v"`        // Interface pack version, e.g. "0.1.0"
	Type    EventType `json:"type"`     // Event type
	TS      string    `json:"ts"`       // RFC3339 timestamp in UTC
	RunID   string    `json:"run_id"`   // Globally unique run identifier
	AgentID string    `json:"agent_id"` // Agent identifier
	Client  Client    `json:"client"`   // Client type
	Env     Env       `json:"env"`      // Execution environment
	Source  Source    `json:"source"`   // Producer instance info
}

// Preview contains truncated previews of args/results.
// Per Interface-Pack §1.5, §1.7
type Preview struct {
	Truncated   bool   `json:"truncated"`              // True if payload was truncated
	ArgsPreview string `json:"args_preview,omitempty"` // Truncated args (tool_call_start)
}

// CallInfo contains tool call metadata.
// Per Interface-Pack §1.5
type CallInfo struct {
	CallID     string  `json:"call_id"`     // Unique within run
	ServerName string  `json:"server_name"` // Exact upstream server name
	ToolName   string  `json:"tool_name"`   // Exact upstream tool name
	Transport  string  `json:"transport"`   // "mcp_stdio" | "mcp_http" | "http" | "unknown"
	ArgsHash   string  `json:"args_hash"`   // SHA-256 of canonical args
	BytesIn    int     `json:"bytes_in"`    // Size of request message
	Preview    Preview `json:"preview"`     // Truncated preview
	Seq        int     `json:"seq"`         // Monotonic call index (starts at 1)
}

// ToolCallStartEvent represents a tool call initiation.
// Per Interface-Pack §1.5
type ToolCallStartEvent struct {
	Envelope
	Call CallInfo `json:"call"`
}
