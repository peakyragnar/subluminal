// Package testharness provides test infrastructure for contract testing.
//
// This file implements a mock adapter for contract-level comparisons.
package testharness

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/subluminal/subluminal/pkg/canonical"
	"github.com/subluminal/subluminal/pkg/core"
	"github.com/subluminal/subluminal/pkg/event"
	"github.com/subluminal/subluminal/pkg/policy"
)

const (
	policyErrCodeBlocked   = -32081
	policyErrCodeThrottled = -32082
	policyErrCodeHinted    = -32083
)

// MockAdapterConfig configures a mock adapter instance.
type MockAdapterConfig struct {
	ServerName   string
	Transport    string
	Identity     core.Identity
	Source       core.Source
	Policy       policy.Bundle
	PolicyTarget policy.SelectorTarget
	Now          func() time.Time
}

// MockAdapter simulates an adapter for contract tests.
type MockAdapter struct {
	serverName   string
	transport    string
	identity     core.Identity
	source       core.Source
	policy       policy.Bundle
	policyTarget policy.SelectorTarget
	now          func() time.Time
	seq          int
	callCount    int
}

// MockToolCall represents a tools/call request for the mock adapter.
type MockToolCall struct {
	ToolName  string
	Args      map[string]any
	BytesIn   int
	RequestID any
}

// MockToolCallResult captures the adapter output for a tool call.
type MockToolCallResult struct {
	StartEvent event.ToolCallStartEvent
	Decision   policy.Decision
	Response   *JSONRPCResponse
}

// NewMockAdapter creates a configured mock adapter instance.
func NewMockAdapter(config MockAdapterConfig) *MockAdapter {
	serverName := config.ServerName
	if serverName == "" {
		serverName = "test"
	}
	transport := config.Transport
	if transport == "" {
		transport = "mcp_stdio"
	}
	identity := normalizeMockIdentity(config.Identity)
	source := normalizeMockSource(config.Source)
	policyBundle := config.Policy
	if isZeroPolicyBundle(policyBundle) {
		policyBundle = policy.DefaultBundle()
	}
	policyTarget := config.PolicyTarget
	if isZeroSelectorTarget(policyTarget) {
		policyTarget = policy.SelectorTarget{
			Env:     string(identity.Env),
			AgentID: identity.AgentID,
			Client:  string(identity.Client),
		}
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}

	return &MockAdapter{
		serverName:   serverName,
		transport:    transport,
		identity:     identity,
		source:       source,
		policy:       policyBundle,
		policyTarget: policyTarget,
		now:          now,
	}
}

// HandleToolCall processes a tool call and returns the start event and response.
func (m *MockAdapter) HandleToolCall(call MockToolCall) (*MockToolCallResult, error) {
	args := call.Args
	if args == nil {
		args = map[string]any{}
	}

	argsHash, err := canonical.ArgsHash(args)
	if err != nil {
		return nil, err
	}

	m.seq++
	m.callCount++
	callID := fmt.Sprintf("mock-call-%d", m.callCount)

	start := event.ToolCallStartEvent{
		Envelope: m.makeEnvelope(event.EventTypeToolCallStart),
		Call: event.CallInfo{
			CallID:     callID,
			ServerName: m.serverName,
			ToolName:   call.ToolName,
			Transport:  m.transport,
			ArgsHash:   argsHash,
			BytesIn:    call.BytesIn,
			Preview:    previewFromArgs(args),
			Seq:        m.seq,
		},
	}

	decision := m.policy.DecideWithContext(policy.DecisionContext{
		ServerName: m.serverName,
		ToolName:   call.ToolName,
		ArgsHash:   argsHash,
		Args:       args,
		Target:     m.policyTarget,
	})

	response := m.policyErrorResponse(call.RequestID, call.ToolName, argsHash, callID, decision)

	return &MockToolCallResult{
		StartEvent: start,
		Decision:   decision,
		Response:   response,
	}, nil
}

func (m *MockAdapter) makeEnvelope(eventType event.EventType) event.Envelope {
	return event.Envelope{
		V:         core.InterfaceVersion,
		Type:      eventType,
		TS:        m.now().UTC().Format(time.RFC3339Nano),
		RunID:     m.identity.RunID,
		AgentID:   m.identity.AgentID,
		Principal: m.identity.Principal,
		Workload:  m.identity.Workload,
		Client:    m.identity.Client,
		Env:       m.identity.Env,
		Source:    m.source.ToEventSource(),
	}
}

func (m *MockAdapter) policyErrorResponse(id any, toolName, argsHash, callID string, decision policy.Decision) *JSONRPCResponse {
	if id == nil {
		return nil
	}

	enforced := m.policy.Mode != event.RunModeObserve
	blocked := enforced && (decision.Action == event.DecisionBlock || decision.Action == event.DecisionTerminateRun)
	throttled := enforced && decision.Action == event.DecisionThrottle
	hinted := enforced && decision.Action == event.DecisionRejectWithHint

	if !blocked && !throttled && !hinted {
		return nil
	}

	code := policyErrCodeBlocked
	if throttled {
		code = policyErrCodeThrottled
	} else if hinted {
		code = policyErrCodeHinted
	}

	message := decision.Summary
	if message == "" {
		message = defaultPolicyMessage(decision.Action)
	}

	data := map[string]any{
		"subluminal": map[string]any{
			"action":      decision.Action,
			"reason_code": decision.ReasonCode,
			"summary":     message,
			"run_id":      m.identity.RunID,
			"call_id":     callID,
			"server_name": m.serverName,
			"tool_name":   toolName,
			"args_hash":   argsHash,
		},
	}

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

func previewFromArgs(args map[string]any) event.Preview {
	const maxPreviewSize = 1024
	const maxInspectBytes = 1024 * 1024

	preview := event.Preview{}
	if args == nil {
		return preview
	}

	b, err := json.Marshal(args)
	if err != nil {
		return preview
	}

	originalLen := len(b)
	if originalLen > maxInspectBytes {
		preview.Truncated = true
		return preview
	}
	if originalLen > maxPreviewSize {
		preview.Truncated = true
		preview.ArgsPreview = string(b[:maxPreviewSize]) + "..."
		return preview
	}

	preview.Truncated = false
	preview.ArgsPreview = string(b)
	return preview
}

func defaultPolicyMessage(action event.DecisionAction) string {
	switch action {
	case event.DecisionThrottle:
		return "Policy throttled"
	case event.DecisionRejectWithHint:
		return "Rejected with hint"
	case event.DecisionTerminateRun:
		return "Run terminated"
	default:
		return "Policy blocked"
	}
}

func normalizeMockIdentity(id core.Identity) core.Identity {
	if id.RunID == "" {
		id.RunID = "mock-run"
	}
	if id.AgentID == "" {
		id.AgentID = "mock-agent"
	}
	if id.Client == "" {
		id.Client = event.ClientUnknown
	}
	if id.Env == "" {
		id.Env = event.EnvUnknown
	}
	return id
}

func normalizeMockSource(src core.Source) core.Source {
	if src.HostID == "" {
		src.HostID = "mock-host"
	}
	if src.ProcID == "" {
		src.ProcID = "mock-proc"
	}
	if src.ShimID == "" {
		src.ShimID = "mock-shim"
	}
	return src
}

func isZeroPolicyBundle(bundle policy.Bundle) bool {
	return bundle.Mode == "" &&
		bundle.Info.PolicyID == "" &&
		bundle.Info.PolicyVersion == "" &&
		bundle.Info.PolicyHash == "" &&
		len(bundle.Rules) == 0
}

func isZeroSelectorTarget(target policy.SelectorTarget) bool {
	return target.Env == "" &&
		target.AgentID == "" &&
		target.Client == "" &&
		target.Workload.Namespace == "" &&
		target.Workload.ServiceAccount == "" &&
		target.Workload.Repo == "" &&
		target.Workload.Branch == "" &&
		len(target.Workload.Labels) == 0
}
