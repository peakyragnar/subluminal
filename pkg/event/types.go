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

// =============================================================================
// run_start event types (Interface-Pack §1.4)
// =============================================================================

// RunMode represents the enforcement mode.
// Per Interface-Pack §1.4: "observe" | "guardrails" | "control"
type RunMode string

const (
	RunModeObserve    RunMode = "observe"
	RunModeGuardrails RunMode = "guardrails"
	RunModeControl    RunMode = "control"
)

// PolicyInfo contains policy metadata for events.
// Per Interface-Pack §1.4, §1.6
type PolicyInfo struct {
	PolicyID      string `json:"policy_id"`
	PolicyVersion string `json:"policy_version"`
	PolicyHash    string `json:"policy_hash"`
}

// RunInfo contains run metadata.
// Per Interface-Pack §1.4
type RunInfo struct {
	StartedAt string     `json:"started_at"` // RFC3339 timestamp
	Mode      RunMode    `json:"mode"`
	Policy    PolicyInfo `json:"policy"`
}

// RunStartEvent represents the beginning of a run.
// Per Interface-Pack §1.4
type RunStartEvent struct {
	Envelope
	Run RunInfo `json:"run"`
}

// =============================================================================
// tool_call_decision event types (Interface-Pack §1.6)
// =============================================================================

// DecisionAction represents the enforcement decision.
// Per Interface-Pack §1.6
type DecisionAction string

const (
	DecisionAllow          DecisionAction = "ALLOW"
	DecisionBlock          DecisionAction = "BLOCK"
	DecisionThrottle       DecisionAction = "THROTTLE"
	DecisionRejectWithHint DecisionAction = "REJECT_WITH_HINT"
	DecisionTerminateRun   DecisionAction = "TERMINATE_RUN"
)

// Severity represents the severity level.
// Per Interface-Pack §1.6
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarn     Severity = "warn"
	SeverityCritical Severity = "critical"
)

// DecisionExplain contains human-readable explanation.
// Per Interface-Pack §1.6
type DecisionExplain struct {
	Summary    string `json:"summary"`
	ReasonCode string `json:"reason_code"`
}

// CallRef contains minimal call identification for decision/end events.
// Per Interface-Pack §1.6, §1.7
type CallRef struct {
	CallID     string `json:"call_id"`
	ServerName string `json:"server_name"`
	ToolName   string `json:"tool_name"`
	ArgsHash   string `json:"args_hash"`
}

// Decision contains the enforcement decision.
// Per Interface-Pack §1.6
type Decision struct {
	Action   DecisionAction  `json:"action"`
	RuleID   *string         `json:"rule_id"` // Nullable
	Severity Severity        `json:"severity"`
	Explain  DecisionExplain `json:"explain"`
	Policy   PolicyInfo      `json:"policy"`
}

// ToolCallDecisionEvent represents an enforcement decision.
// Per Interface-Pack §1.6
type ToolCallDecisionEvent struct {
	Envelope
	Call     CallRef  `json:"call"`
	Decision Decision `json:"decision"`
}

// =============================================================================
// tool_call_end event types (Interface-Pack §1.7)
// =============================================================================

// CallStatus represents the outcome of a tool call.
// Per Interface-Pack §1.7
type CallStatus string

const (
	CallStatusOK        CallStatus = "OK"
	CallStatusError     CallStatus = "ERROR"
	CallStatusTimeout   CallStatus = "TIMEOUT"
	CallStatusCancelled CallStatus = "CANCELLED"
)

// ResultPreview contains truncated result preview.
// Per Interface-Pack §1.7
type ResultPreview struct {
	Truncated     bool   `json:"truncated"`
	ResultPreview string `json:"result_preview,omitempty"`
}

// ErrorDetail contains optional error information.
// Per Interface-Pack §1.7
type ErrorDetail struct {
	Class     string `json:"class"`              // "upstream_error" | "policy_block" | "timeout" | "transport" | "unknown"
	Message   string `json:"message"`            // Safe, no secrets
	Code      any    `json:"code,omitempty"`     // Upstream code if known (string or int)
	Retryable bool   `json:"retryable,omitempty"`
}

// ToolCallEndEvent represents tool call completion.
// Per Interface-Pack §1.7
type ToolCallEndEvent struct {
	Envelope
	Call      CallRef       `json:"call"`
	Status    CallStatus    `json:"status"`
	LatencyMS int           `json:"latency_ms"`
	BytesOut  int           `json:"bytes_out"`
	Preview   ResultPreview `json:"preview"`
	Error     *ErrorDetail  `json:"error,omitempty"` // Only if status != OK
}

// =============================================================================
// run_end event types (Interface-Pack §1.8)
// =============================================================================

// RunStatus represents the final run outcome.
// Per Interface-Pack §1.8
type RunStatus string

const (
	RunStatusSucceeded  RunStatus = "SUCCEEDED"
	RunStatusFailed     RunStatus = "FAILED"
	RunStatusTerminated RunStatus = "TERMINATED"
	RunStatusCancelled  RunStatus = "CANCELLED"
)

// RunSummary contains aggregate counts for a run.
// Per Interface-Pack §1.8
type RunSummary struct {
	CallsTotal     int `json:"calls_total"`
	CallsAllowed   int `json:"calls_allowed"`
	CallsBlocked   int `json:"calls_blocked"`
	CallsThrottled int `json:"calls_throttled"`
	ErrorsTotal    int `json:"errors_total"`
	DurationMS     int `json:"duration_ms"`
}

// RunEndInfo contains run completion metadata.
// Per Interface-Pack §1.8
type RunEndInfo struct {
	EndedAt string     `json:"ended_at"` // RFC3339 timestamp
	Status  RunStatus  `json:"status"`
	Summary RunSummary `json:"summary"`
}

// RunEndEvent represents the end of a run.
// Per Interface-Pack §1.8
type RunEndEvent struct {
	Envelope
	Run RunEndInfo `json:"run"`
}
