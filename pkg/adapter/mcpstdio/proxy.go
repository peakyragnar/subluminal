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
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/subluminal/subluminal/pkg/canonical"
	"github.com/subluminal/subluminal/pkg/core"
	"github.com/subluminal/subluminal/pkg/event"
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
	identity   core.Identity
	source     core.Source
	serverName string

	// I/O
	agentIn  io.Reader
	agentOut io.Writer

	// Request tracking for response matching
	pendingCalls map[any]*pendingCall
	pendingMu    sync.RWMutex

	// Shutdown coordination
	done      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
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
	return &Proxy{
		upstream:     upstream,
		emitter:      emitter,
		state:        core.NewRunState(),
		identity:     identity,
		source:       source,
		serverName:   serverName,
		agentIn:      agentIn,
		agentOut:     agentOut,
		pendingCalls: make(map[any]*pendingCall),
		done:         make(chan struct{}),
	}
}

// Run starts the proxy and blocks until completion.
// Returns when stdin closes or an error occurs.
func (p *Proxy) Run() error {
	// Emit run_start
	p.emitRunStart()

	// Start goroutines
	p.wg.Add(2)
	go p.readFromAgent()
	go p.readFromUpstream()

	// Wait for completion
	p.wg.Wait()

	// Emit run_end
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
	defer p.wg.Done()
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
			p.interceptToolCall(&req, line)
		}

		// Forward to upstream
		p.forwardToUpstream(line)
	}
}

// interceptToolCall processes a tools/call request and emits events.
func (p *Proxy) interceptToolCall(req *JSONRPCRequest, rawLine []byte) {
	// Parse params
	toolName, args, err := ParseToolsCallParams(req.Params)
	if err != nil {
		// Can't parse - still forward, just don't emit events
		return
	}

	// Compute args_hash
	argsHash, _ := canonical.ArgsHash(args)

	// Generate call_id
	callID := core.GenerateUUID()

	// Start tracking
	callState := p.state.StartCall(callID)

	// Track pending call for response matching
	if id, ok := GetRequestID(req); ok {
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
	p.emitToolCallStart(callID, toolName, argsHash, len(rawLine), args, callState.Seq)

	// Emit tool_call_decision (v0.1: always ALLOW)
	p.emitToolCallDecision(callID, toolName, argsHash)

	// Increment allowed counter
	p.state.IncrementAllowed()
}

// readFromUpstream reads responses from upstream and forwards to agent.
func (p *Proxy) readFromUpstream() {
	defer p.wg.Done()
	defer p.Stop() // Signal shutdown when upstream exits (fixes hang on upstream crash)

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
		if err := json.Unmarshal(line, &resp); err == nil && resp.ID != nil {
			p.matchResponse(&resp, line)
		}

		// Forward to agent
		p.forwardToAgent(line)
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
		V:       core.InterfaceVersion,
		Type:    eventType,
		TS:      time.Now().UTC().Format(time.RFC3339Nano),
		RunID:   p.identity.RunID,
		AgentID: p.identity.AgentID,
		Client:  p.identity.Client,
		Env:     p.identity.Env,
		Source:  p.source.ToEventSource(),
	}
}

func (p *Proxy) emitRunStart() {
	evt := event.RunStartEvent{
		Envelope: p.makeEnvelope(event.EventTypeRunStart),
		Run: event.RunInfo{
			StartedAt: p.state.StartTime().UTC().Format(time.RFC3339Nano),
			Mode:      event.RunModeObserve, // v0.1: observe only
			Policy: event.PolicyInfo{
				PolicyID:      "default",
				PolicyVersion: "0.1.0",
				PolicyHash:    "none",
			},
		},
	}
	p.emitter.Emit(evt)
}

func (p *Proxy) emitToolCallStart(callID, toolName, argsHash string, bytesIn int, args map[string]any, seq int) {
	// Create preview (truncated args)
	// Per Interface-Pack §1.10:
	// - For small payloads: include full preview
	// - For medium payloads (>1KB but <1MiB): include truncated preview with "..."
	// - For large payloads (>1MiB): omit preview entirely, set truncated=true
	const maxPreviewSize = 1024
	const maxInspectBytes = 1024 * 1024 // 1 MiB

	argsPreview := ""
	truncated := false

	if args != nil {
		if b, err := json.Marshal(args); err == nil {
			originalLen := len(b)
			if originalLen > maxInspectBytes {
				// Very large payload - omit preview
				argsPreview = ""
				truncated = true
			} else if originalLen > maxPreviewSize {
				// Medium payload - truncate with "..."
				argsPreview = string(b[:maxPreviewSize]) + "..."
				truncated = true
			} else {
				// Small payload - full preview
				argsPreview = string(b)
				truncated = false
			}
		}
	}

	evt := event.ToolCallStartEvent{
		Envelope: p.makeEnvelope(event.EventTypeToolCallStart),
		Call: event.CallInfo{
			CallID:     callID,
			ServerName: p.serverName,
			ToolName:   toolName,
			Transport:  "mcp_stdio",
			ArgsHash:   argsHash,
			BytesIn:    bytesIn,
			Preview: event.Preview{
				Truncated:   truncated,
				ArgsPreview: argsPreview,
			},
			Seq: seq,
		},
	}
	p.emitter.Emit(evt)
}

func (p *Proxy) emitToolCallDecision(callID, toolName, argsHash string) {
	evt := event.ToolCallDecisionEvent{
		Envelope: p.makeEnvelope(event.EventTypeToolCallDecision),
		Call: event.CallRef{
			CallID:     callID,
			ServerName: p.serverName,
			ToolName:   toolName,
			ArgsHash:   argsHash,
		},
		Decision: event.Decision{
			Action:   event.DecisionAllow,
			RuleID:   nil, // No rule triggered
			Severity: event.SeverityInfo,
			Explain: event.DecisionExplain{
				Summary:    "Allowed by default policy",
				ReasonCode: "DEFAULT_ALLOW",
			},
			Policy: event.PolicyInfo{
				PolicyID:      "default",
				PolicyVersion: "0.1.0",
				PolicyHash:    "none",
			},
		},
	}
	p.emitter.Emit(evt)
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
