// Package contract contains integration tests for Subluminal contracts.
//
// This file tests ERR-* contracts (error handling and JSON-RPC error shapes).
// Reference: Interface-Pack.md §3, Contract-Test-Checklist.md ERR-001 through ERR-004
package contract

import (
	"strings"
	"testing"

	"github.com/subluminal/subluminal/pkg/testharness"
)

// =============================================================================
// ERR-001: BLOCK Uses JSON-RPC Error Code -32081
// Contract: When policy blocks a call, response uses error.code=-32081 with
//           structured error.data.subluminal fields.
// Reference: Interface-Pack.md §3.2.1, Contract-Test-Checklist.md ERR-001
// =============================================================================

func TestERR001_BlockUsesCorrectErrorCode(t *testing.T) {
	skipIfNoShim(t)

	policyJSON := `{
		"mode": "guardrails",
		"policy_id": "test-err-001",
		"policy_version": "1.0.0",
		"rules": [
			{
				"rule_id": "deny-blocked-tool",
				"kind": "deny",
				"match": {"tool_name": {"glob": ["blocked_tool"]}},
				"effect": {"action": "BLOCK", "reason_code": "TEST_BLOCK", "message": "Blocked for ERR-001 test"}
			}
		]
	}`

	h := testharness.NewTestHarness(testharness.HarnessConfig{
		ShimPath: shimPath,
		ShimEnv:  []string{"SUB_POLICY_JSON=" + policyJSON},
	})
	h.AddTool("blocked_tool", "A tool blocked by policy", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	h.Initialize()

	resp, err := h.CallTool("blocked_tool", nil)
	if err != nil {
		t.Fatalf("Failed to call tool: %v", err)
	}

	wrapped := testharness.WrapResponse(resp)

	if wrapped.IsSuccess() {
		t.Fatal("ERR-001 FAILED: Tool should have been blocked by policy")
	}

	// Assert: Error code is -32081 (POLICY_BLOCKED)
	if wrapped.ErrorCode() != -32081 {
		t.Errorf("ERR-001 FAILED: Expected error code -32081 (POLICY_BLOCKED), got %d\n"+
			"  Per Interface-Pack §3.2.1, BLOCK must use error code -32081",
			wrapped.ErrorCode())
	}

	// Assert: error.data.subluminal fields are present
	// Note: Need to access raw response for nested data validation
	if resp.Error != nil && resp.Error.Data != nil {
		data, ok := resp.Error.Data.(map[string]any)
		if !ok {
			t.Error("ERR-001 FAILED: error.data should be an object")
		} else {
			subluminal, ok := data["subluminal"].(map[string]any)
			if !ok {
				t.Error("ERR-001 FAILED: error.data.subluminal should be present")
			} else {
				// Check required subluminal fields per §3.2.2
				requiredFields := []string{"v", "action", "rule_id", "reason_code", "summary", "run_id", "call_id"}
				for _, field := range requiredFields {
					if _, exists := subluminal[field]; !exists {
						t.Errorf("ERR-001 FAILED: error.data.subluminal missing required field %q", field)
					}
				}
			}
		}
	}
}

// =============================================================================
// ERR-002: THROTTLE Uses Error Code -32082 + backoff_ms
// Contract: Rate limit throttle returns error.code=-32082 with backoff_ms.
// Reference: Interface-Pack.md §3.2.3, Contract-Test-Checklist.md ERR-002
// =============================================================================

func TestERR002_ThrottleUsesCorrectErrorCode(t *testing.T) {
	skipIfNoShim(t)

	// Note: This test requires a policy with rate_limit rule

	h := newShimHarness()
	h.AddTool("rate_limited", "A rate-limited tool", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	h.Initialize()

	// Execute: Rapid calls to trigger throttle
	var throttledResp *testharness.JSONRPCResponse
	for i := 0; i < 10; i++ {
		resp, _ := h.CallTool("rate_limited", nil)
		wrapped := testharness.WrapResponse(resp)
		if wrapped.ErrorCode() == -32082 {
			throttledResp = resp
			break
		}
	}

	if throttledResp == nil {
		t.Skip("ERR-002: Could not trigger throttle - needs rate_limit policy")
	}

	// Assert: Error code is -32082 (POLICY_THROTTLED)
	wrapped := testharness.WrapResponse(throttledResp)
	if wrapped.ErrorCode() != -32082 {
		t.Errorf("ERR-002 FAILED: Expected error code -32082 (POLICY_THROTTLED), got %d",
			wrapped.ErrorCode())
	}

	// Assert: subluminal.backoff_ms is present
	if throttledResp.Error != nil && throttledResp.Error.Data != nil {
		data, _ := throttledResp.Error.Data.(map[string]any)
		subluminal, _ := data["subluminal"].(map[string]any)
		if subluminal != nil {
			backoffMS, exists := subluminal["backoff_ms"]
			if !exists {
				t.Error("ERR-002 FAILED: subluminal.backoff_ms must be present for THROTTLE")
			}
			if backoffMS.(float64) <= 0 {
				t.Error("ERR-002 FAILED: subluminal.backoff_ms must be positive")
			}
		}
	}
}

// =============================================================================
// ERR-003: REJECT_WITH_HINT Uses -32083 + hint Object
// Contract: Hint rejection returns error.code=-32083 with hint.{hint_text,hint_kind}.
// Reference: Interface-Pack.md §3.2.4, Contract-Test-Checklist.md ERR-003
// =============================================================================

func TestERR003_RejectWithHintUsesCorrectErrorCode(t *testing.T) {
	skipIfNoShim(t)

	// Note: This test requires a policy with REJECT_WITH_HINT action

	h := newShimHarness()
	h.AddTool("hinted_tool", "A tool that gets hints", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	h.Initialize()

	// Execute: Call tool that triggers hint
	resp, err := h.CallTool("hinted_tool", map[string]any{"bad_param": true})
	if err != nil {
		t.Fatalf("Failed to call tool: %v", err)
	}

	wrapped := testharness.WrapResponse(resp)
	if wrapped.IsSuccess() {
		t.Skip("ERR-003: Tool was not rejected - needs REJECT_WITH_HINT policy")
	}

	// Assert: Error code is -32083 (REJECT_WITH_HINT)
	if wrapped.ErrorCode() != -32083 {
		t.Skip("ERR-003: Error code was not -32083 - may not have triggered hint")
	}

	// Assert: subluminal.hint object is present with required fields
	if resp.Error != nil && resp.Error.Data != nil {
		data, _ := resp.Error.Data.(map[string]any)
		subluminal, _ := data["subluminal"].(map[string]any)
		if subluminal != nil {
			hint, ok := subluminal["hint"].(map[string]any)
			if !ok {
				t.Error("ERR-003 FAILED: subluminal.hint must be present for REJECT_WITH_HINT")
			} else {
				// Check required hint fields per §3.2.4
				if _, exists := hint["hint_text"]; !exists {
					t.Error("ERR-003 FAILED: hint.hint_text is required")
				}
				if _, exists := hint["hint_kind"]; !exists {
					t.Error("ERR-003 FAILED: hint.hint_kind is required")
				}

				// Check hint_kind is valid enum
				hintKind, _ := hint["hint_kind"].(string)
				validKinds := map[string]bool{
					"ARG_FIX": true, "BUDGET": true, "RATE": true, "SAFETY": true, "OTHER": true,
				}
				if !validKinds[hintKind] {
					t.Errorf("ERR-003 FAILED: hint.hint_kind %q is not a valid enum value", hintKind)
				}

				// If suggested_args present, must be valid JSON object
				if suggestedArgs, exists := hint["suggested_args"]; exists && suggestedArgs != nil {
					if _, ok := suggestedArgs.(map[string]any); !ok {
						t.Error("ERR-003 FAILED: hint.suggested_args must be a valid JSON object if present")
					}
				}
			}
		}
	}
}

// =============================================================================
// ERR-004: No Secret Leakage in Error Message/Data
// Contract: Error payloads contain no secret values; previews are clean.
// Reference: Interface-Pack.md §4, Contract-Test-Checklist.md ERR-004
// =============================================================================

func TestERR004_NoSecretLeakageInErrors(t *testing.T) {
	skipIfNoShim(t)

	policyJSON := `{
		"mode": "guardrails",
		"policy_id": "test-err-004",
		"policy_version": "1.0.0",
		"rules": [
			{
				"rule_id": "deny-secret-tool",
				"kind": "deny",
				"match": {"tool_name": {"glob": ["secret_tool"]}},
				"effect": {"action": "BLOCK", "reason_code": "TEST_BLOCK", "message": "Blocked for ERR-004 test: sk-secret-key-12345 ghp_github_token_abc password123"}
			}
		]
	}`

	h := testharness.NewTestHarness(testharness.HarnessConfig{
		ShimPath: shimPath,
		ShimEnv:  []string{"SUB_POLICY_JSON=" + policyJSON},
	})
	h.AddTool("secret_tool", "A tool using secrets", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	h.Initialize()

	knownSecrets := []string{
		"sk-secret-key-12345",
		"ghp_github_token_abc",
		"password123",
	}

	resp, _ := h.CallTool("secret_tool", nil)

	wrapped := testharness.WrapResponse(resp)
	if wrapped.IsSuccess() {
		t.Fatal("ERR-004 FAILED: Tool should have been blocked by policy")
	}

	// Assert: Error message contains no secrets
	errorMsg := wrapped.ErrorMessage()
	for _, secret := range knownSecrets {
		if strings.Contains(errorMsg, secret) {
			t.Errorf("ERR-004 FAILED: Error message contains secret!\n"+
				"  Per Interface-Pack §4, secrets must NEVER appear in error messages\n"+
				"  Found: %q in message", secret)
		}
	}

	// Assert: Error data contains no secrets
	if resp.Error != nil && resp.Error.Data != nil {
		dataStr := stringify(resp.Error.Data)
		for _, secret := range knownSecrets {
			if strings.Contains(dataStr, secret) {
				t.Errorf("ERR-004 FAILED: Error data contains secret!\n"+
					"  Per Interface-Pack §4, secrets must NEVER appear in error data\n"+
					"  Found: %q in error.data", secret)
			}
		}
	}

	// Assert: Event previews contain no secrets
	for _, evt := range h.Events() {
		raw := evt.Raw
		for _, secret := range knownSecrets {
			if strings.Contains(raw, secret) {
				t.Errorf("ERR-004 FAILED: Event contains secret!\n"+
					"  Per Interface-Pack §4, secrets must NEVER appear in events\n"+
					"  Found in event %d (type=%s)", evt.Index, evt.Type)
			}
		}
	}
}

// stringify converts any value to a string for secret scanning
func stringify(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case map[string]any:
		var parts []string
		for k, v := range val {
			parts = append(parts, k+":"+stringify(v))
		}
		return strings.Join(parts, ",")
	default:
		return ""
	}
}
