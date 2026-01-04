// Package mcpstdio implements the MCP stdio adapter.
//
// This file implements the bidirectional proxy that:
// - Reads JSON-RPC from stdin (agent client)
// - Intercepts tools/call requests to emit events
// - Forwards requests to upstream MCP server
// - Reads responses from upstream
// - Emits tool_call_end events
// - Forwards responses to stdout (agent client)
//
// Per Interface-Pack §7:
// - Adapters extract (server_name, tool_name, args) for each tool call
// - Adapters delegate to core for event emission
// - Adapters forward allowed calls
package mcpstdio

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/subluminal/subluminal/pkg/canonical"
	"github.com/subluminal/subluminal/pkg/core"
	"github.com/subluminal/subluminal/pkg/event"
	"github.com/subluminal/subluminal/pkg/policy"
)

const (
	defaultThrottleBackoffMS = 1000
	maxPreviewSize           = 1024
	maxInspectBytes          = 1024 * 1024
	streamHashChunkSize      = 32 * 1024
)

// Proxy handles bidirectional JSON-RPC proxying with event emission.
type Proxy struct {
	// Upstream process
	upstream *UpstreamProcess

	// Event emitter
	emitter *core.Emitter

	// Run state
	state *core.RunState

	// Identity
	identity     core.Identity
	source       core.Source
	serverName   string
	policy       policy.Bundle
	policyTarget policy.SelectorTarget

	// I/O
	agentIn  io.Reader
	agentOut io.Writer

	// Request tracking for response matching
	pendingCalls map[any]*pendingCall
	pendingMu    sync.RWMutex

	// Shutdown coordination
	done      chan struct{}
	closeOnce sync.Once
}

// pendingCall tracks a tool call waiting for response.
type pendingCall struct {
	callID   string
	toolName string
	argsHash string
	startSeq int
}

// NewProxy creates a new bidirectional proxy.
func NewProxy(
	upstream *UpstreamProcess,
	emitter *core.Emitter,
	serverName string,
	identity core.Identity,
	source core.Source,
	agentIn io.Reader,
	agentOut io.Writer,
) *Proxy {
	policyBundle := policy.LoadFromEnv()
	policyTarget := policy.SelectorTarget{
		Env:     string(identity.Env),
		AgentID: identity.AgentID,
		Client:  string(identity.Client),
	}
	return &Proxy{
		upstream:     upstream,
		emitter:      emitter,
		state:        core.NewRunState(),
		identity:     identity,
		source:       source,
		serverName:   serverName,
		policy:       policyBundle,
		policyTarget: policyTarget,
		agentIn:      agentIn,
		agentOut:     agentOut,
		pendingCalls: make(map[any]*pendingCall),
		done:         make(chan struct{}),
	}
}

// Run starts the proxy and blocks until completion.
// Returns when stdin closes OR upstream exits (whichever comes first).
func (p *Proxy) Run() error {
	// Emit run_start
	p.emitRunStart()

	// Start goroutines with individual completion channels
	agentDone := make(chan struct{})
	upstreamDone := make(chan struct{})

	go func() {
		defer close(agentDone)
		p.readFromAgent()
	}()
	go func() {
		defer close(upstreamDone)
		p.readFromUpstream()
	}()

	// Wait for EITHER to complete first
	// The completion path determines whether we need to wait for the other side
	select {
	case <-agentDone:
		// Agent closed stdin - wait for upstream to finish draining responses
		// This ensures all tool_call_end events are emitted before run_end
		<-upstreamDone
	case <-upstreamDone:
		// Upstream exited (normal exit or crash) - don't wait for agent
		// Agent may still have stdin open; we can't block on that
	}

	// Emit run_end (guaranteed to be last for the events we can emit)
	p.emitRunEnd()

	return nil
}

// Stop signals the proxy to shut down.
func (p *Proxy) Stop() {
	p.closeOnce.Do(func() {
		close(p.done)
	})
}

// readFromAgent reads requests from agent stdin and forwards to upstream.
func (p *Proxy) readFromAgent() {
	defer p.upstream.CloseStdin() // Signal EOF to upstream when agent is done

	scanner := bufio.NewScanner(p.agentIn)
	// Increase buffer size for large payloads
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		select {
		case <-p.done:
			return
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Parse request
		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			// Forward malformed requests as-is (let upstream handle errors)
			p.forwardToUpstream(line)
			continue
		}

		// Intercept tools/call
		if IsToolsCall(&req) {
			if !p.interceptToolCall(&req, line) {
				continue
			}
		}

		// Forward to upstream
		p.forwardToUpstream(line)
	}
}

// interceptToolCall processes a tools/call request and emits events.
func (p *Proxy) interceptToolCall(req *JSONRPCRequest, rawLine []byte) bool {
	// Parse params
	toolName, args, argsRaw, err := ParseToolsCallParams(req.Params)
	if err != nil {
		// Can't parse - still forward, just don't emit events
		return true
	}

	argsRaw = normalizeArgsRaw(argsRaw)
	preview, inspectTruncated := buildArgsPreview(argsRaw)

	// Compute args_hash canonically; add args_stream_hash when inspection is truncated.
	argsHash, _ := canonical.ArgsHash(args)
	argsStreamHash := ""
	if inspectTruncated {
		argsStreamHash = hashStreamHex(argsRaw)
	}

	// Generate call_id
	callID := core.GenerateUUID()

	// Start tracking
	callState := p.state.StartCall(callID)

	policyDecision := p.policy.DecideWithContext(policy.DecisionContext{
		ServerName: p.serverName,
		ToolName:   toolName,
		ArgsHash:   argsHash,
		Args:       args,
		Target:     p.policyTarget,
	})
	decisionSummary := redactSecrets(policyDecision.Summary)
	decision := event.Decision{
		Action:   policyDecision.Action,
		RuleID:   policyDecision.RuleID,
		Severity: policyDecision.Severity,
		Explain: event.DecisionExplain{
			Summary:    decisionSummary,
			ReasonCode: policyDecision.ReasonCode,
		},
		BackoffMS: policyDecision.BackoffMS,
		Hint:      sanitizeHint(policyDecision.Hint),
		Policy:    p.policy.Info,
	}
	if decision.Action == event.DecisionThrottle && decision.BackoffMS <= 0 {
		decision.BackoffMS = defaultThrottleBackoffMS
	}

	enforced := p.policy.Mode != event.RunModeObserve
	blocked := enforced && (decision.Action == event.DecisionBlock || decision.Action == event.DecisionTerminateRun)
	throttled := enforced && decision.Action == event.DecisionThrottle
	hinted := enforced && decision.Action == event.DecisionRejectWithHint

	// Track pending call for response matching
	if id, ok := GetRequestID(req); ok && !blocked && !throttled && !hinted {
		p.pendingMu.Lock()
		p.pendingCalls[normalizeID(id)] = &pendingCall{
			callID:   callID,
			toolName: toolName,
			argsHash: argsHash,
			startSeq: callState.Seq,
		}
		p.pendingMu.Unlock()
	}

	// Emit tool_call_start
	p.emitToolCallStart(callID, toolName, argsHash, argsStreamHash, len(rawLine), preview, callState.Seq)

	// Emit tool_call_decision
	p.emitToolCallDecision(callID, toolName, argsHash, decision)

	if blocked || throttled || hinted {
		if blocked || hinted {
			p.state.IncrementBlocked()
		} else {
			p.state.IncrementThrottled()
		}
		p.state.IncrementErrors()
		latencyMS := p.state.EndCall(callID)
		errCode := ErrCodePolicyBlocked
		if throttled {
			errCode = ErrCodePolicyThrottled
		} else if hinted {
			errCode = ErrCodeRejectWithHint
		}
		errDetail := &event.ErrorDetail{
			Class:   "policy_block",
			Message: decision.Explain.Summary,
			Code:    errCode,
		}

		var payload []byte
		if id, ok := GetRequestID(req); ok {
			errData := p.policyErrorData(callID, toolName, argsHash, decision)
			resp := NewErrorResponse(id, errCode, decision.Explain.Summary, errData)
			if p, err := json.Marshal(resp); err == nil {
				payload = p
			}
		}
		bytesOut := len(payload)

		p.emitToolCallEnd(callID, toolName, argsHash, event.CallStatusError, latencyMS, bytesOut, errDetail)

		if payload != nil {
			p.forwardToAgent(payload)
		}
		return false
	}

	// Increment allowed counter
	p.state.IncrementAllowed()
	return true
}

// readFromUpstream reads responses from upstream and forwards to agent.
func (p *Proxy) readFromUpstream() {
	defer p.Stop() // Signal shutdown when upstream exits

	scanner := bufio.NewScanner(p.upstream.Stdout())
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		select {
		case <-p.done:
			return
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Parse response to match with request
		var resp JSONRPCResponse
		sanitizedLine := line
		if err := json.Unmarshal(line, &resp); err == nil && resp.ID != nil {
			if resp.Error != nil {
				resp.Error.Message = redactSecrets(resp.Error.Message)
				if resp.Error.Data != nil {
					resp.Error.Data = sanitizeValue(resp.Error.Data)
				}
				if sanitized, err := json.Marshal(resp); err == nil {
					sanitizedLine = sanitized
				}
			}
			p.matchResponse(&resp, sanitizedLine)
		}

		// Forward to agent
		p.forwardToAgent(sanitizedLine)
	}
}

// matchResponse matches a response to its request and emits tool_call_end.
func (p *Proxy) matchResponse(resp *JSONRPCResponse, rawLine []byte) {
	p.pendingMu.Lock()
	pending, exists := p.pendingCalls[normalizeID(resp.ID)]
	if exists {
		delete(p.pendingCalls, normalizeID(resp.ID))
	}
	p.pendingMu.Unlock()

	if !exists {
		return
	}

	// Calculate latency
	latencyMS := p.state.EndCall(pending.callID)

	// Determine status
	status := event.CallStatusOK
	var errDetail *event.ErrorDetail
	if resp.Error != nil {
		status = event.CallStatusError
		errDetail = &event.ErrorDetail{
			Class:   "upstream_error",
			Message: resp.Error.Message,
			Code:    resp.Error.Code,
		}
		p.state.IncrementErrors()
	}

	// Emit tool_call_end
	p.emitToolCallEnd(pending.callID, pending.toolName, pending.argsHash, status, latencyMS, len(rawLine), errDetail)
}

// forwardToUpstream writes data to the upstream stdin.
func (p *Proxy) forwardToUpstream(data []byte) {
	p.upstream.Stdin().Write(data)
	p.upstream.Stdin().Write([]byte("\n"))
}

// forwardToAgent writes data to the agent stdout.
func (p *Proxy) forwardToAgent(data []byte) {
	p.agentOut.Write(data)
	p.agentOut.Write([]byte("\n"))
}

// Event emission helpers

func (p *Proxy) makeEnvelope(eventType event.EventType) event.Envelope {
	return event.Envelope{
		V:         core.InterfaceVersion,
		Type:      eventType,
		TS:        time.Now().UTC().Format(time.RFC3339Nano),
		RunID:     p.identity.RunID,
		AgentID:   p.identity.AgentID,
		Principal: p.identity.Principal,
		Workload:  p.identity.Workload,
		Client:    p.identity.Client,
		Env:       p.identity.Env,
		Source:    p.source.ToEventSource(),
	}
}

func (p *Proxy) emitRunStart() {
	evt := event.RunStartEvent{
		Envelope: p.makeEnvelope(event.EventTypeRunStart),
		Run: event.RunInfo{
			StartedAt: p.state.StartTime().UTC().Format(time.RFC3339Nano),
			Mode:      p.policy.Mode,
			Policy:    p.policy.Info,
		},
	}
	p.emitter.Emit(evt)
}

func (p *Proxy) emitToolCallStart(callID, toolName, argsHash, argsStreamHash string, bytesIn int, preview event.Preview, seq int) {
	evt := event.ToolCallStartEvent{
		Envelope: p.makeEnvelope(event.EventTypeToolCallStart),
		Call: event.CallInfo{
			CallID:         callID,
			ServerName:     p.serverName,
			ToolName:       toolName,
			Transport:      "mcp_stdio",
			ArgsHash:       argsHash,
			ArgsStreamHash: argsStreamHash,
			BytesIn:        bytesIn,
			Preview:        preview,
			Seq:            seq,
		},
	}
	p.emitter.Emit(evt)
}

func (p *Proxy) policyErrorData(callID, toolName, argsHash string, decision event.Decision) map[string]any {
	subluminal := map[string]any{
		"v":           core.InterfaceVersion,
		"action":      decision.Action,
		"rule_id":     decision.RuleID,
		"reason_code": decision.Explain.ReasonCode,
		"summary":     decision.Explain.Summary,
		"run_id":      p.identity.RunID,
		"call_id":     callID,
		"server_name": p.serverName,
		"tool_name":   toolName,
		"args_hash":   argsHash,
		"policy": map[string]any{
			"policy_id":      decision.Policy.PolicyID,
			"policy_version": decision.Policy.PolicyVersion,
			"policy_hash":    decision.Policy.PolicyHash,
		},
	}

	if decision.Action == event.DecisionThrottle && decision.BackoffMS > 0 {
		subluminal["backoff_ms"] = decision.BackoffMS
	}
	if decision.Action == event.DecisionRejectWithHint {
		hintText := ""
		hintKind := string(event.HintKindOther)
		hint := map[string]any{}
		if decision.Hint != nil {
			hintText = decision.Hint.HintText
			if decision.Hint.HintKind != "" {
				hintKind = string(decision.Hint.HintKind)
			}
			if decision.Hint.SuggestedArgs != nil {
				hint["suggested_args"] = sanitizeValue(decision.Hint.SuggestedArgs)
			}
			if decision.Hint.RetryAdvice != nil {
				hint["retry_advice"] = redactSecrets(*decision.Hint.RetryAdvice)
			}
		}
		if hintText == "" {
			hintText = decision.Explain.Summary
		}
		if hintText == "" {
			hintText = "Rejected with hint"
		}
		hintText = redactSecrets(hintText)
		hint["hint_text"] = hintText
		hint["hint_kind"] = hintKind
		subluminal["hint"] = hint
	}

	return map[string]any{
		"subluminal": subluminal,
	}
}

func (p *Proxy) emitToolCallDecision(callID, toolName, argsHash string, decision event.Decision) {
	evt := event.ToolCallDecisionEvent{
		Envelope: p.makeEnvelope(event.EventTypeToolCallDecision),
		Call: event.CallRef{
			CallID:     callID,
			ServerName: p.serverName,
			ToolName:   toolName,
			ArgsHash:   argsHash,
		},
		Decision: decision,
	}
	p.emitter.EmitSync(evt)
}

func (p *Proxy) emitToolCallEnd(callID, toolName, argsHash string, status event.CallStatus, latencyMS, bytesOut int, errDetail *event.ErrorDetail) {
	evt := event.ToolCallEndEvent{
		Envelope: p.makeEnvelope(event.EventTypeToolCallEnd),
		Call: event.CallRef{
			CallID:     callID,
			ServerName: p.serverName,
			ToolName:   toolName,
			ArgsHash:   argsHash,
		},
		Status:    status,
		LatencyMS: latencyMS,
		BytesOut:  bytesOut,
		Preview: event.ResultPreview{
			Truncated: false,
		},
		Error: errDetail,
	}
	p.emitter.Emit(evt)
}

func (p *Proxy) emitRunEnd() {
	summary := p.state.GetSummary()
	evt := event.RunEndEvent{
		Envelope: p.makeEnvelope(event.EventTypeRunEnd),
		Run: event.RunEndInfo{
			EndedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Status:  event.RunStatusSucceeded,
			Summary: event.RunSummary{
				CallsTotal:     summary.CallsTotal,
				CallsAllowed:   summary.CallsAllowed,
				CallsBlocked:   summary.CallsBlocked,
				CallsThrottled: summary.CallsThrottled,
				ErrorsTotal:    summary.ErrorsTotal,
				DurationMS:     p.state.DurationMS(),
			},
		},
	}
	p.emitter.Emit(evt)
}

// normalizeID normalizes request/response IDs for map key comparison.
// JSON numbers become float64, so we need consistent handling.
func normalizeID(id any) any {
	switch v := id.(type) {
	case float64:
		// Convert to int64 if it's a whole number
		if v == float64(int64(v)) {
			return int64(v)
		}
		return v
	case int:
		return int64(v)
	case int64:
		return v
	default:
		return id
	}
}

func normalizeArgsRaw(argsRaw []byte) []byte {
	trimmed := bytes.TrimSpace(argsRaw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return []byte("{}")
	}
	return argsRaw
}

func buildArgsPreview(argsRaw []byte) (event.Preview, bool) {
	preview := event.Preview{}
	size := len(argsRaw)

	if size > maxInspectBytes {
		preview.Truncated = true
		preview.ArgsPreview = ""
		return preview, true
	}
	if size > maxPreviewSize {
		previewBytes := make([]byte, maxPreviewSize+len("..."))
		copy(previewBytes, argsRaw[:maxPreviewSize])
		copy(previewBytes[maxPreviewSize:], []byte("..."))
		preview.Truncated = true
		preview.ArgsPreview = string(previewBytes)
		return preview, false
	}

	preview.Truncated = false
	preview.ArgsPreview = string(argsRaw)
	return preview, false
}

func hashStreamHex(data []byte) string {
	hasher := sha256.New()
	for offset := 0; offset < len(data); offset += streamHashChunkSize {
		end := offset + streamHashChunkSize
		if end > len(data) {
			end = len(data)
		}
		_, _ = hasher.Write(data[offset:end])
	}
	return hex.EncodeToString(hasher.Sum(nil))
}
