// Package policy tests for stateful policy evaluation.
//
// These tests verify that budget, rate limiting, breaker, and dedupe
// state is properly maintained across multiple Decide() calls.
package policy

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/subluminal/subluminal/pkg/event"
)

// =============================================================================
// Budget Tracking Tests (POL-003)
// =============================================================================

func TestBudget_CallsAreCountedCorrectly(t *testing.T) {
	// Create a bundle with a budget rule: limit 3 calls
	bundle := Bundle{
		Mode: event.RunModeGuardrails,
		Info: event.PolicyInfo{
			PolicyID:      "test",
			PolicyVersion: "1.0.0",
		},
		Rules: []Rule{
			{
				RuleID: "budget-rule",
				Kind:   "budget",
				Match: Match{
					ToolName: &NameMatch{Glob: []string{"test_tool"}},
				},
				Effect: Effect{
					ReasonCode: "BUDGET_EXCEEDED",
					Message:    "Budget exceeded",
					Budget: &BudgetEffect{
						Scope:      "tool",
						LimitCalls: intPtr(3),
						OnExceed:   event.DecisionBlock,
					},
				},
			},
		},
		breakerState: make(map[string][]time.Time),
		budgets:      newBudgetState(),
		rateLimit:    newRateLimitState(),
		dedupe:       newDedupeCache(),
	}

	// Call 1-3 should be ALLOW
	for i := 1; i <= 3; i++ {
		decision := bundle.Decide("server", "test_tool", "hash123")
		if decision.Action != event.DecisionAllow {
			t.Errorf("Call %d: expected ALLOW, got %s", i, decision.Action)
		}
		t.Logf("Call %d: action=%s", i, decision.Action)
	}

	// Call 4 should be BLOCK
	decision := bundle.Decide("server", "test_tool", "hash123")
	if decision.Action != event.DecisionBlock {
		t.Errorf("Call 4: expected BLOCK, got %s (budget should be exceeded)", decision.Action)
	}
	t.Logf("Call 4: action=%s, reason=%s", decision.Action, decision.ReasonCode)
}

func TestBudget_StatePersistedAcrossCalls(t *testing.T) {
	// This test verifies the state object is shared, not recreated
	bundle := Bundle{
		Mode: event.RunModeGuardrails,
		Rules: []Rule{
			{
				RuleID: "budget-test",
				Kind:   "budget",
				Match: Match{
					ToolName: &NameMatch{Glob: []string{"*"}},
				},
				Effect: Effect{
					Budget: &BudgetEffect{
						Scope:      "tool",
						LimitCalls: intPtr(2),
						OnExceed:   event.DecisionBlock,
					},
				},
			},
		},
		budgets: newBudgetState(),
	}

	// Verify budgets state exists
	if bundle.budgets == nil {
		t.Fatal("budgets state is nil before first call")
	}

	// First call
	d1 := bundle.Decide("s", "tool1", "h1")
	t.Logf("After call 1: budgets=%+v", bundle.budgets.calls)

	// Verify budgets state still exists and has data
	if bundle.budgets == nil {
		t.Fatal("budgets state became nil after first call")
	}
	if len(bundle.budgets.calls) == 0 {
		t.Error("budgets.calls is empty after first call - state not persisted")
	}

	// Second call
	d2 := bundle.Decide("s", "tool1", "h1")
	t.Logf("After call 2: budgets=%+v", bundle.budgets.calls)

	// Third call should be blocked
	d3 := bundle.Decide("s", "tool1", "h1")
	t.Logf("After call 3: budgets=%+v, action=%s", bundle.budgets.calls, d3.Action)

	if d1.Action != event.DecisionAllow {
		t.Errorf("Call 1 should be ALLOW, got %s", d1.Action)
	}
	if d2.Action != event.DecisionAllow {
		t.Errorf("Call 2 should be ALLOW, got %s", d2.Action)
	}
	if d3.Action != event.DecisionBlock {
		t.Errorf("Call 3 should be BLOCK, got %s", d3.Action)
	}
}

func TestBudget_LoadFromEnvPreservesState(t *testing.T) {
	// Set up policy JSON in environment
	policyJSON := `{
		"mode": "guardrails",
		"policy_id": "env-test",
		"policy_version": "1.0.0",
		"rules": [{
			"rule_id": "budget-env-test",
			"kind": "budget",
			"match": {"tool_name": {"glob": ["env_tool"]}},
			"effect": {
				"budget": {
					"scope": "tool",
					"limit_calls": 2,
					"on_exceed": "BLOCK"
				}
			}
		}]
	}`

	os.Setenv("SUB_POLICY_JSON", policyJSON)
	defer os.Unsetenv("SUB_POLICY_JSON")

	bundle := LoadFromEnv()

	t.Logf("Loaded bundle: mode=%s, rules=%d, budgets=%v",
		bundle.Mode, len(bundle.Rules), bundle.budgets != nil)

	if bundle.Mode != event.RunModeGuardrails {
		t.Errorf("Expected guardrails mode, got %s", bundle.Mode)
	}
	if bundle.budgets == nil {
		t.Fatal("budgets state is nil after LoadFromEnv")
	}

	// Make calls and verify budget is tracked
	d1 := bundle.Decide("s", "env_tool", "h")
	d2 := bundle.Decide("s", "env_tool", "h")
	d3 := bundle.Decide("s", "env_tool", "h")

	t.Logf("Decisions: d1=%s, d2=%s, d3=%s", d1.Action, d2.Action, d3.Action)

	if d1.Action != event.DecisionAllow || d2.Action != event.DecisionAllow {
		t.Error("First two calls should be ALLOW")
	}
	if d3.Action != event.DecisionBlock {
		t.Errorf("Third call should be BLOCK, got %s", d3.Action)
	}
}

// =============================================================================
// Rate Limiting Tests (POL-004)
// =============================================================================

func TestRateLimit_TokenBucketDepletes(t *testing.T) {
	bundle := Bundle{
		Mode: event.RunModeGuardrails,
		Rules: []Rule{
			{
				RuleID: "rate-limit-test",
				Kind:   "rate_limit",
				Match: Match{
					ToolName: &NameMatch{Glob: []string{"rate_tool"}},
				},
				Effect: Effect{
					RateLimit: &RateLimitEffect{
						Scope:             "tool",
						Capacity:          2,
						RefillTokens:      1,
						RefillPeriodMS:    60000, // 1 minute
						CostTokensPerCall: 1,
						OnLimit:           event.DecisionThrottle,
						BackoffMS:         500,
					},
				},
			},
		},
		rateLimit: newRateLimitState(),
	}

	// First 2 calls should succeed (capacity=2)
	d1 := bundle.Decide("s", "rate_tool", "h")
	d2 := bundle.Decide("s", "rate_tool", "h")

	t.Logf("d1=%s, d2=%s", d1.Action, d2.Action)

	if d1.Action != event.DecisionAllow {
		t.Errorf("Call 1 should be ALLOW, got %s", d1.Action)
	}
	if d2.Action != event.DecisionAllow {
		t.Errorf("Call 2 should be ALLOW, got %s", d2.Action)
	}

	// Call 3 should be throttled (no tokens left, no refill yet)
	d3 := bundle.Decide("s", "rate_tool", "h")
	t.Logf("d3=%s, backoff=%d", d3.Action, d3.BackoffMS)

	if d3.Action != event.DecisionThrottle {
		t.Errorf("Call 3 should be THROTTLE, got %s", d3.Action)
	}
	if d3.BackoffMS <= 0 {
		t.Errorf("THROTTLE should have backoff_ms > 0, got %d", d3.BackoffMS)
	}
}

func TestRateLimit_RejectWithHintUsesEffectHint(t *testing.T) {
	bundle := Bundle{
		Mode: event.RunModeGuardrails,
		Rules: []Rule{
			{
				RuleID: "rate-limit-hint",
				Kind:   "rate_limit",
				Match: Match{
					ToolName: &NameMatch{Glob: []string{"rate_hint_tool"}},
				},
				Effect: Effect{
					Hint: &event.Hint{
						HintText: "Use suggested args",
						HintKind: event.HintKindArgFix,
						SuggestedArgs: map[string]any{
							"mode":  "safe",
							"limit": 5,
						},
					},
					RateLimit: &RateLimitEffect{
						Scope:             "tool",
						Capacity:          0,
						RefillTokens:      0,
						RefillPeriodMS:    0,
						CostTokensPerCall: 1,
						OnLimit:           event.DecisionRejectWithHint,
						HintText:          "Rate limit exceeded",
					},
				},
			},
		},
		rateLimit: newRateLimitState(),
	}

	decision := bundle.Decide("s", "rate_hint_tool", "h")
	if decision.Action != event.DecisionRejectWithHint {
		t.Fatalf("Expected REJECT_WITH_HINT, got %s", decision.Action)
	}
	if decision.Hint == nil {
		t.Fatal("Expected hint to be set for REJECT_WITH_HINT decision")
	}
	if decision.Hint.HintText != "Use suggested args" {
		t.Errorf("Expected hint_text %q, got %q", "Use suggested args", decision.Hint.HintText)
	}
	if decision.Hint.HintKind != event.HintKindArgFix {
		t.Errorf("Expected hint_kind %q, got %q", event.HintKindArgFix, decision.Hint.HintKind)
	}
	if decision.Hint.SuggestedArgs == nil {
		t.Fatal("Expected suggested_args to be present")
	}
	if mode, ok := decision.Hint.SuggestedArgs["mode"].(string); !ok || mode != "safe" {
		t.Errorf("Expected suggested_args.mode %q, got %v", "safe", decision.Hint.SuggestedArgs["mode"])
	}
	if limit, ok := decision.Hint.SuggestedArgs["limit"].(int); !ok || limit != 5 {
		t.Errorf("Expected suggested_args.limit %d, got %v", 5, decision.Hint.SuggestedArgs["limit"])
	}
}

func TestRateLimit_StatePersistedAcrossCalls(t *testing.T) {
	bundle := Bundle{
		Mode: event.RunModeGuardrails,
		Rules: []Rule{
			{
				RuleID: "rate-persist-test",
				Kind:   "rate_limit",
				Match: Match{
					ToolName: &NameMatch{Glob: []string{"*"}},
				},
				Effect: Effect{
					RateLimit: &RateLimitEffect{
						Scope:             "tool",
						Capacity:          1,
						RefillTokens:      0, // No refill
						RefillPeriodMS:    60000,
						CostTokensPerCall: 1,
						OnLimit:           event.DecisionThrottle,
						BackoffMS:         100,
					},
				},
			},
		},
		rateLimit: newRateLimitState(),
	}

	// First call uses the only token
	d1 := bundle.Decide("s", "tool", "h")
	if d1.Action != event.DecisionAllow {
		t.Errorf("Call 1 should be ALLOW, got %s", d1.Action)
	}

	// Verify state exists
	if bundle.rateLimit == nil {
		t.Fatal("rateLimit state is nil after first call")
	}
	if len(bundle.rateLimit.buckets) == 0 {
		t.Error("rateLimit.buckets is empty - state not persisted")
	}

	// Second call should be throttled
	d2 := bundle.Decide("s", "tool", "h")
	if d2.Action != event.DecisionThrottle {
		t.Errorf("Call 2 should be THROTTLE, got %s", d2.Action)
	}
}

// =============================================================================
// Breaker Tests (POL-005)
// =============================================================================

func TestBreaker_TripsOnRepeatThreshold(t *testing.T) {
	bundle := Bundle{
		Mode: event.RunModeGuardrails,
		Rules: []Rule{
			{
				RuleID: "breaker-test",
				Kind:   "breaker",
				Match: Match{
					ToolName: &NameMatch{Glob: []string{"repeat_tool"}},
				},
				Effect: Effect{
					Breaker: &BreakerEffect{
						Scope:           "tool",
						RepeatThreshold: 3,
						RepeatWindowMS:  10000, // 10 seconds
						OnTrip:          "BLOCK",
					},
				},
			},
		},
		breakerState: make(map[string][]time.Time),
	}

	// Same args_hash for all calls
	argsHash := "same_hash"

	// First 2 calls should be ALLOW (under threshold)
	d1 := bundle.Decide("s", "repeat_tool", argsHash)
	d2 := bundle.Decide("s", "repeat_tool", argsHash)

	t.Logf("d1=%s, d2=%s", d1.Action, d2.Action)

	// Note: breaker counts AFTER adding current call, so:
	// Call 1: count=1, 1 < 3, ALLOW
	// Call 2: count=2, 2 < 3, ALLOW
	// Call 3: count=3, 3 >= 3, BLOCK

	if d1.Action != event.DecisionAllow {
		t.Errorf("Call 1 should be ALLOW, got %s", d1.Action)
	}
	if d2.Action != event.DecisionAllow {
		t.Errorf("Call 2 should be ALLOW, got %s", d2.Action)
	}

	// Third call should trip the breaker
	d3 := bundle.Decide("s", "repeat_tool", argsHash)
	t.Logf("d3=%s, reason=%s", d3.Action, d3.ReasonCode)

	if d3.Action != event.DecisionBlock {
		t.Errorf("Call 3 should be BLOCK (breaker tripped), got %s", d3.Action)
	}
}

func TestBreaker_StatePersistedAcrossCalls(t *testing.T) {
	bundle := Bundle{
		Mode: event.RunModeGuardrails,
		Rules: []Rule{
			{
				RuleID: "breaker-persist",
				Kind:   "breaker",
				Match: Match{
					ToolName: &NameMatch{Glob: []string{"*"}},
				},
				Effect: Effect{
					Breaker: &BreakerEffect{
						Scope:           "tool",
						RepeatThreshold: 2,
						RepeatWindowMS:  10000,
						OnTrip:          "BLOCK",
					},
				},
			},
		},
		breakerState: make(map[string][]time.Time),
	}

	hash := "test_hash"
	d1 := bundle.Decide("s", "tool", hash)

	// Verify state exists
	if bundle.breakerState == nil {
		t.Fatal("breakerState is nil after first call")
	}
	if len(bundle.breakerState) == 0 {
		t.Error("breakerState is empty - state not persisted")
	}

	t.Logf("After call 1: breakerState has %d entries", len(bundle.breakerState))

	d2 := bundle.Decide("s", "tool", hash)

	if d1.Action != event.DecisionAllow {
		t.Errorf("Call 1 should be ALLOW, got %s", d1.Action)
	}
	if d2.Action != event.DecisionBlock {
		t.Errorf("Call 2 should be BLOCK (breaker tripped), got %s", d2.Action)
	}
}

// =============================================================================
// Dedupe Tests (POL-006)
// =============================================================================

func TestDedupe_BlocksDuplicateWithinWindow(t *testing.T) {
	bundle := Bundle{
		Mode: event.RunModeGuardrails,
		Rules: []Rule{
			{
				RuleID: "dedupe-test",
				Kind:   "dedupe",
				Match: Match{
					ToolName: &NameMatch{Glob: []string{"write_tool"}},
				},
				Effect: Effect{
					ReasonCode: "DEDUPE_DUPLICATE",
					Dedupe: &DedupeEffect{
						Scope:       "tool",
						WindowMS:    60000, // 1 minute
						Key:         "args_hash",
						OnDuplicate: event.DecisionBlock,
					},
				},
			},
		},
		dedupe: newDedupeCache(),
	}

	argsHash := "write_hash"

	// First call should be ALLOW
	d1 := bundle.Decide("s", "write_tool", argsHash)
	t.Logf("d1=%s", d1.Action)

	if d1.Action != event.DecisionAllow {
		t.Errorf("Call 1 should be ALLOW, got %s", d1.Action)
	}

	// Second call with same args_hash should be BLOCK (duplicate)
	d2 := bundle.Decide("s", "write_tool", argsHash)
	t.Logf("d2=%s, reason=%s", d2.Action, d2.ReasonCode)

	if d2.Action != event.DecisionBlock {
		t.Errorf("Call 2 should be BLOCK (duplicate), got %s", d2.Action)
	}
}

func TestDedupe_RejectWithHintUsesEffectHint(t *testing.T) {
	bundle := Bundle{
		Mode: event.RunModeGuardrails,
		Rules: []Rule{
			{
				RuleID: "dedupe-hint",
				Kind:   "dedupe",
				Match: Match{
					ToolName: &NameMatch{Glob: []string{"dup_hint_tool"}},
				},
				Effect: Effect{
					Hint: &event.Hint{
						HintText: "Use suggested args",
						HintKind: event.HintKindArgFix,
						SuggestedArgs: map[string]any{
							"mode":  "safe",
							"limit": 5,
						},
					},
					Dedupe: &DedupeEffect{
						Scope:       "tool",
						WindowMS:    60000,
						Key:         "args_hash",
						OnDuplicate: event.DecisionRejectWithHint,
						HintText:    "Duplicate call blocked",
					},
				},
			},
		},
		dedupe: newDedupeCache(),
	}

	argsHash := "dup_hash"
	first := bundle.Decide("s", "dup_hint_tool", argsHash)
	if first.Action != event.DecisionAllow {
		t.Fatalf("Call 1 should be ALLOW, got %s", first.Action)
	}

	decision := bundle.Decide("s", "dup_hint_tool", argsHash)
	if decision.Action != event.DecisionRejectWithHint {
		t.Fatalf("Expected REJECT_WITH_HINT, got %s", decision.Action)
	}
	if decision.Hint == nil {
		t.Fatal("Expected hint to be set for REJECT_WITH_HINT decision")
	}
	if decision.Hint.HintText != "Use suggested args" {
		t.Errorf("Expected hint_text %q, got %q", "Use suggested args", decision.Hint.HintText)
	}
	if decision.Hint.HintKind != event.HintKindArgFix {
		t.Errorf("Expected hint_kind %q, got %q", event.HintKindArgFix, decision.Hint.HintKind)
	}
	if decision.Hint.SuggestedArgs == nil {
		t.Fatal("Expected suggested_args to be present")
	}
	if mode, ok := decision.Hint.SuggestedArgs["mode"].(string); !ok || mode != "safe" {
		t.Errorf("Expected suggested_args.mode %q, got %v", "safe", decision.Hint.SuggestedArgs["mode"])
	}
	if limit, ok := decision.Hint.SuggestedArgs["limit"].(int); !ok || limit != 5 {
		t.Errorf("Expected suggested_args.limit %d, got %v", 5, decision.Hint.SuggestedArgs["limit"])
	}
}

func TestDedupe_DifferentArgsHashAllowed(t *testing.T) {
	bundle := Bundle{
		Mode: event.RunModeGuardrails,
		Rules: []Rule{
			{
				RuleID: "dedupe-diff",
				Kind:   "dedupe",
				Match: Match{
					ToolName: &NameMatch{Glob: []string{"*"}},
				},
				Effect: Effect{
					Dedupe: &DedupeEffect{
						Scope:       "tool",
						WindowMS:    60000,
						Key:         "args_hash",
						OnDuplicate: event.DecisionBlock,
					},
				},
			},
		},
		dedupe: newDedupeCache(),
	}

	// Different args_hash for each call
	d1 := bundle.Decide("s", "tool", "hash1")
	d2 := bundle.Decide("s", "tool", "hash2")
	d3 := bundle.Decide("s", "tool", "hash3")

	if d1.Action != event.DecisionAllow {
		t.Errorf("Call 1 should be ALLOW, got %s", d1.Action)
	}
	if d2.Action != event.DecisionAllow {
		t.Errorf("Call 2 should be ALLOW (different hash), got %s", d2.Action)
	}
	if d3.Action != event.DecisionAllow {
		t.Errorf("Call 3 should be ALLOW (different hash), got %s", d3.Action)
	}
}

func TestDedupe_StatePersistedAcrossCalls(t *testing.T) {
	bundle := Bundle{
		Mode: event.RunModeGuardrails,
		Rules: []Rule{
			{
				RuleID: "dedupe-persist",
				Kind:   "dedupe",
				Match: Match{
					ToolName: &NameMatch{Glob: []string{"*"}},
				},
				Effect: Effect{
					Dedupe: &DedupeEffect{
						Scope:       "tool",
						WindowMS:    60000,
						Key:         "args_hash",
						OnDuplicate: event.DecisionBlock,
					},
				},
			},
		},
		dedupe: newDedupeCache(),
	}

	hash := "persist_hash"
	d1 := bundle.Decide("s", "tool", hash)

	// Verify state exists
	if bundle.dedupe == nil {
		t.Fatal("dedupe state is nil after first call")
	}
	if len(bundle.dedupe.entries) == 0 {
		t.Error("dedupe.entries is empty - state not persisted")
	}

	t.Logf("After call 1: dedupe has %d entries", len(bundle.dedupe.entries))

	d2 := bundle.Decide("s", "tool", hash)

	if d1.Action != event.DecisionAllow {
		t.Errorf("Call 1 should be ALLOW, got %s", d1.Action)
	}
	if d2.Action != event.DecisionBlock {
		t.Errorf("Call 2 should be BLOCK (duplicate), got %s", d2.Action)
	}
}

// =============================================================================
// JSON Parsing Tests
// =============================================================================

func TestJSONParsing_BudgetEffect(t *testing.T) {
	policyJSON := `{
		"mode": "guardrails",
		"policy_id": "json-test",
		"policy_version": "1.0.0",
		"rules": [{
			"rule_id": "budget-json",
			"kind": "budget",
			"match": {"tool_name": {"glob": ["*"]}},
			"effect": {
				"reason_code": "BUDGET_EXCEEDED",
				"message": "Budget exceeded",
				"budget": {
					"scope": "tool",
					"limit_calls": 5,
					"on_exceed": "BLOCK"
				}
			}
		}]
	}`

	var parsed rawBundle
	if err := json.Unmarshal([]byte(policyJSON), &parsed); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if len(parsed.Rules) != 1 {
		t.Fatalf("Expected 1 rule, got %d", len(parsed.Rules))
	}

	rule := parsed.Rules[0]
	if rule.Effect.Budget == nil {
		t.Fatal("Budget effect is nil - JSON parsing failed")
	}
	if rule.Effect.Budget.LimitCalls == nil {
		t.Fatal("LimitCalls is nil - JSON parsing failed")
	}
	if *rule.Effect.Budget.LimitCalls != 5 {
		t.Errorf("LimitCalls should be 5, got %d", *rule.Effect.Budget.LimitCalls)
	}
	if rule.Effect.Budget.Scope != "tool" {
		t.Errorf("Scope should be 'tool', got %s", rule.Effect.Budget.Scope)
	}

	t.Logf("Parsed budget: scope=%s, limit=%d, on_exceed=%s",
		rule.Effect.Budget.Scope, *rule.Effect.Budget.LimitCalls, rule.Effect.Budget.OnExceed)
}

func TestJSONParsing_RateLimitEffect(t *testing.T) {
	policyJSON := `{
		"mode": "guardrails",
		"rules": [{
			"rule_id": "rate-json",
			"kind": "rate_limit",
			"match": {"tool_name": {"glob": ["*"]}},
			"effect": {
				"rate_limit": {
					"scope": "tool",
					"capacity": 10,
					"refill_tokens": 2,
					"refill_period_ms": 1000,
					"cost_tokens_per_call": 1,
					"on_limit": "THROTTLE",
					"backoff_ms": 500
				}
			}
		}]
	}`

	var parsed rawBundle
	if err := json.Unmarshal([]byte(policyJSON), &parsed); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	rule := parsed.Rules[0]
	if rule.Effect.RateLimit == nil {
		t.Fatal("RateLimit effect is nil")
	}

	rl := rule.Effect.RateLimit
	if rl.Capacity != 10 {
		t.Errorf("Capacity should be 10, got %d", rl.Capacity)
	}
	if rl.BackoffMS != 500 {
		t.Errorf("BackoffMS should be 500, got %d", rl.BackoffMS)
	}

	t.Logf("Parsed rate_limit: capacity=%d, refill=%d/%dms, backoff=%d",
		rl.Capacity, rl.RefillTokens, rl.RefillPeriodMS, rl.BackoffMS)
}

// =============================================================================
// Helpers
// =============================================================================

func intPtr(i int) *int {
	return &i
}
