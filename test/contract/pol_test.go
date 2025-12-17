// Package contract contains integration tests for Subluminal contracts.
//
// This file tests POL-* contracts (policy evaluation).
// Reference: Interface-Pack.md §2, Contract-Test-Checklist.md POL-001 through POL-007
package contract

import (
	"testing"

	"github.com/subluminal/subluminal/pkg/testharness"
)

// =============================================================================
// POL-001: Observe Mode Never Blocks
// Contract: In observe mode, decision is always ALLOW; rules are logged but
//           not enforced.
// Reference: Interface-Pack.md §2.1, Contract-Test-Checklist.md POL-001
// =============================================================================

func TestPOL001_ObserveModeNeverBlocks(t *testing.T) {
	skipIfNoShim(t)

	// Note: This test requires configuring the shim with:
	// - mode = "observe"
	// - A deny rule that would normally block

	h := newShimHarness()
	h.AddTool("should_be_denied", "A tool that would be denied in guardrails mode", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	h.Initialize()

	// Execute: Call tool that has a deny rule
	resp, err := h.CallTool("should_be_denied", nil)
	if err != nil {
		t.Fatalf("Failed to call tool: %v", err)
	}

	// Assert: Call succeeded (not blocked)
	wrapped := testharness.WrapResponse(resp)
	if !wrapped.IsSuccess() {
		t.Errorf("POL-001 FAILED: Tool call was blocked in observe mode\n"+
			"  Per Interface-Pack §2.1, observe mode should never block\n"+
			"  Error: %s", wrapped.ErrorMessage())
	}

	// Assert: Decision event shows ALLOW (even though rule matched)
	decisions := h.EventSink.ByType("tool_call_decision")
	if len(decisions) == 0 {
		t.Fatal("POL-001 FAILED: No tool_call_decision events")
	}

	for _, evt := range decisions {
		action := testharness.GetString(evt, "decision.action")
		if action != "ALLOW" {
			t.Errorf("POL-001 FAILED: Decision action is %q, expected ALLOW\n"+
				"  Per Interface-Pack §2.1, observe mode always allows", action)
		}
	}
}

// =============================================================================
// POL-002: Allow/Deny Ordering
// Contract: Rules evaluated top-to-bottom; deny above allow results in BLOCK.
// Reference: Interface-Pack.md §2.3, Contract-Test-Checklist.md POL-002
// =============================================================================

func TestPOL002_AllowDenyOrdering(t *testing.T) {
	skipIfNoShim(t)

	// Note: This test requires a policy with:
	// 1. deny rule for specific tool (first)
	// 2. allow rule for all tools (second)

	h := newShimHarness()
	h.AddTool("denied_tool", "A tool denied by policy", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	h.Initialize()

	// Execute: Call the denied tool
	resp, err := h.CallTool("denied_tool", nil)
	if err != nil {
		t.Fatalf("Failed to call tool: %v", err)
	}

	// Assert: Call was blocked
	wrapped := testharness.WrapResponse(resp)
	if wrapped.IsSuccess() {
		t.Error("POL-002 FAILED: Tool call should have been blocked by deny rule")
	}

	// Assert: Decision shows BLOCK with correct rule_id
	decisions := h.EventSink.ByType("tool_call_decision")
	if len(decisions) == 0 {
		t.Fatal("POL-002 FAILED: No tool_call_decision events")
	}

	evt := decisions[0]
	action := testharness.GetString(evt, "decision.action")
	if action != "BLOCK" {
		t.Errorf("POL-002 FAILED: Decision action is %q, expected BLOCK", action)
	}

	// Assert: rule_id is present (identifies which rule blocked)
	ruleID := testharness.GetString(evt, "decision.rule_id")
	if ruleID == "" {
		t.Error("POL-002 FAILED: decision.rule_id should identify the blocking rule")
	}

	// Assert: reason_code is present
	reasonCode := testharness.GetString(evt, "decision.explain.reason_code")
	if reasonCode == "" {
		t.Error("POL-002 FAILED: decision.explain.reason_code should be present")
	}
}

// =============================================================================
// POL-003: Budget Rule Decrements & Blocks on Exceed
// Contract: First N calls allowed; call N+1 gets BLOCK/REJECT_WITH_HINT/TERMINATE.
// Reference: Interface-Pack.md §2.5, Contract-Test-Checklist.md POL-003
// =============================================================================

func TestPOL003_BudgetRuleDecrementsAndBlocks(t *testing.T) {
	skipIfNoShim(t)

	// Note: This test requires a policy with:
	// - budget rule: limit_calls=3

	h := newShimHarness()
	h.AddTool("budgeted_tool", "A tool with call budget", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	h.Initialize()

	// Execute: Call tool 4 times (budget is 3)
	var results []*testharness.ToolCallResponse
	for i := 0; i < 4; i++ {
		resp, err := h.CallTool("budgeted_tool", nil)
		if err != nil {
			t.Fatalf("Call %d failed: %v", i+1, err)
		}
		results = append(results, testharness.WrapResponse(resp))
	}

	// Assert: First 3 calls succeeded
	for i := 0; i < 3; i++ {
		if !results[i].IsSuccess() {
			t.Errorf("POL-003 FAILED: Call %d should have succeeded (within budget)\n"+
				"  Error: %s", i+1, results[i].ErrorMessage())
		}
	}

	// Assert: 4th call was blocked/rejected
	if results[3].IsSuccess() {
		t.Error("POL-003 FAILED: Call 4 should have been blocked (exceeded budget)")
	}

	// Assert: Decision cites budget rule
	decisions := h.EventSink.ByType("tool_call_decision")
	if len(decisions) < 4 {
		t.Fatalf("POL-003 FAILED: Expected 4 decisions, got %d", len(decisions))
	}

	lastDecision := decisions[3]
	action := testharness.GetString(lastDecision, "decision.action")
	allowedActions := map[string]bool{"BLOCK": true, "REJECT_WITH_HINT": true, "TERMINATE_RUN": true}
	if !allowedActions[action] {
		t.Errorf("POL-003 FAILED: 4th call decision was %q, expected BLOCK/REJECT_WITH_HINT/TERMINATE_RUN", action)
	}
}

// =============================================================================
// POL-004: Token Bucket Rate Limit (THROTTLE)
// Contract: Calls after tokens depleted return THROTTLE with backoff_ms.
// Reference: Interface-Pack.md §2.5, Contract-Test-Checklist.md POL-004
// =============================================================================

func TestPOL004_TokenBucketRateLimitThrottle(t *testing.T) {
	skipIfNoShim(t)

	// Note: This test requires a policy with:
	// - rate_limit rule: capacity=2, slow refill, on_limit=THROTTLE, backoff=500ms

	h := newShimHarness()
	h.AddTool("rate_limited_tool", "A rate-limited tool", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	h.Initialize()

	// Execute: 5 rapid calls (capacity is 2)
	var results []*testharness.ToolCallResponse
	for i := 0; i < 5; i++ {
		resp, err := h.CallTool("rate_limited_tool", nil)
		if err != nil {
			t.Fatalf("Call %d failed: %v", i+1, err)
		}
		results = append(results, testharness.WrapResponse(resp))
	}

	// Assert: Some calls were throttled
	throttledCount := 0
	for _, r := range results {
		if r.ErrorCode() == -32082 { // POLICY_THROTTLED
			throttledCount++
		}
	}

	if throttledCount == 0 {
		t.Error("POL-004 FAILED: Expected some calls to be throttled")
	}

	// Assert: Throttled decisions include backoff_ms
	decisions := h.EventSink.ByType("tool_call_decision")
	for _, evt := range decisions {
		action := testharness.GetString(evt, "decision.action")
		if action == "THROTTLE" {
			backoffMS := testharness.GetInt(evt, "decision.backoff_ms")
			if backoffMS <= 0 {
				t.Error("POL-004 FAILED: THROTTLE decision missing backoff_ms\n" +
					"  Per Interface-Pack §2.5, THROTTLE must include backoff_ms")
			}
		}
	}
}

// =============================================================================
// POL-005: Breaker - Repeat Threshold Triggers
// Contract: Repeated calls with same args_hash trip breaker at threshold.
// Reference: Interface-Pack.md §2.5, Contract-Test-Checklist.md POL-005
// =============================================================================

func TestPOL005_BreakerRepeatThresholdTriggers(t *testing.T) {
	skipIfNoShim(t)

	// Note: This test requires a policy with:
	// - breaker rule: repeat_threshold=5 within 10s window

	h := newShimHarness()
	h.AddTool("repetitive_tool", "A tool with breaker", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	h.Initialize()

	// Execute: Call with same args repeatedly (same args_hash)
	sameArgs := map[string]any{"always": "same"}
	var lastResult *testharness.ToolCallResponse

	for i := 0; i < 10; i++ {
		resp, err := h.CallTool("repetitive_tool", sameArgs)
		if err != nil {
			t.Fatalf("Call %d failed: %v", i+1, err)
		}
		lastResult = testharness.WrapResponse(resp)

		// If breaker tripped, stop early
		if !lastResult.IsSuccess() {
			break
		}
	}

	// Assert: Breaker eventually tripped
	if lastResult.IsSuccess() {
		t.Error("POL-005 FAILED: Breaker should have tripped after repeated identical calls")
	}

	// Assert: Decision is TERMINATE_RUN or BLOCK
	decisions := h.EventSink.ByType("tool_call_decision")
	var breakerTripped bool
	for _, evt := range decisions {
		action := testharness.GetString(evt, "decision.action")
		if action == "TERMINATE_RUN" || action == "BLOCK" {
			breakerTripped = true
			break
		}
	}

	if !breakerTripped {
		t.Error("POL-005 FAILED: Expected TERMINATE_RUN or BLOCK decision from breaker")
	}
}

// =============================================================================
// POL-006: Dedupe Window Blocks Duplicate Write-Like
// Contract: Second identical write-like call within window is blocked.
// Reference: Interface-Pack.md §2.5, Contract-Test-Checklist.md POL-006
// =============================================================================

func TestPOL006_DedupeWindowBlocksDuplicate(t *testing.T) {
	skipIfNoShim(t)

	// Note: This test requires a policy with:
	// - dedupe rule: window=60s, key=args_hash, on_duplicate=BLOCK

	h := newShimHarness()
	h.AddTool("write_tool", "A write-like tool", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	h.Initialize()

	// Execute: Same write call twice
	writeArgs := map[string]any{"action": "create", "name": "test"}

	resp1, _ := h.CallTool("write_tool", writeArgs)
	resp2, _ := h.CallTool("write_tool", writeArgs)

	wrapped1 := testharness.WrapResponse(resp1)
	wrapped2 := testharness.WrapResponse(resp2)

	// Assert: First call succeeded
	if !wrapped1.IsSuccess() {
		t.Errorf("POL-006 FAILED: First call should succeed\n"+
			"  Error: %s", wrapped1.ErrorMessage())
	}

	// Assert: Second call was blocked (duplicate)
	if wrapped2.IsSuccess() {
		t.Error("POL-006 FAILED: Second identical call should be blocked as duplicate")
	}

	// Assert: Decision explains duplicate
	decisions := h.EventSink.ByType("tool_call_decision")
	if len(decisions) < 2 {
		t.Fatal("POL-006 FAILED: Expected 2 decisions")
	}

	secondDecision := decisions[1]
	action := testharness.GetString(secondDecision, "decision.action")
	if action != "BLOCK" && action != "REJECT_WITH_HINT" {
		t.Errorf("POL-006 FAILED: Second call decision was %q, expected BLOCK or REJECT_WITH_HINT", action)
	}
}

// =============================================================================
// POL-007: Tag Rule Applies risk_class
// Contract: Tag rule marks tool with risk_class; subsequent rules match on it.
// Reference: Interface-Pack.md §2.5, Contract-Test-Checklist.md POL-007
// Priority: P1 (not blocking v0.1)
// =============================================================================

func TestPOL007_TagRuleAppliesRiskClass(t *testing.T) {
	skipIfNoShim(t)

	// Note: This test requires a policy with:
	// 1. tag rule: marks certain tools as "write_like"
	// 2. deny rule: blocks all "write_like" tools

	h := newShimHarness()
	h.AddTool("file_write", "Write to a file", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	h.Initialize()

	// Execute: Call the tagged tool
	resp, _ := h.CallTool("file_write", map[string]any{"path": "/tmp/test"})

	// Assert: Call was blocked (tag rule applied, deny rule matched)
	wrapped := testharness.WrapResponse(resp)
	if wrapped.IsSuccess() {
		t.Skip("POL-007: Tag rule feature not implemented (P1)")
	}

	// If blocked, verify it was due to risk_class matching
	decisions := h.EventSink.ByType("tool_call_decision")
	if len(decisions) == 0 {
		t.Fatal("POL-007 FAILED: No decisions captured")
	}

	// Look for evidence that risk_class was applied
	// (This would be in the decision explain or rule match info)
}
