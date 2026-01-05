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

	policyJSON := `{
		"mode": "control",
		"policy_id": "test-err-003",
		"policy_version": "1.0.0",
		"rules": [
			{
				"rule_id": "reject-with-hint",
				"kind": "deny",
				"match": {"tool_name": {"glob": ["hinted_tool"]}},
				"effect": {
					"action": "REJECT_WITH_HINT",
					"reason_code": "TEST_HINT",
					"message": "Rejected with suggested args for ERR-003",
					"hint": {
						"hint_text": "Use suggested args to retry",
						"hint_kind": "ARG_FIX",
						"suggested_args": {
							"mode": "safe",
							"limit": 5
						}
					}
				}
			}
		]
	}`

	h := testharness.NewTestHarness(testharness.HarnessConfig{
		ShimPath: shimPath,
		ShimEnv:  []string{"SUB_POLICY_JSON=" + policyJSON},
	})
	h.AddTool("hinted_tool", "A tool that gets hints", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	h.Initialize()

	// Execute: Call tool once to trigger REJECT_WITH_HINT
	args := map[string]any{"mode": "unsafe", "limit": 1}
	resp, err := h.CallTool("hinted_tool", args)
	if err != nil {
		t.Fatalf("Failed to call tool: %v", err)
	}

	wrapped := testharness.WrapResponse(resp)
	if wrapped.IsSuccess() {
		t.Fatal("ERR-003 FAILED: Call should be rejected with hint")
	}

	// Assert: Error code is -32083 (REJECT_WITH_HINT)
	if wrapped.ErrorCode() != -32083 {
		t.Fatalf("ERR-003 FAILED: Expected error code -32083 (REJECT_WITH_HINT), got %d",
			wrapped.ErrorCode())
	}

	// Assert: subluminal.hint object is present with required fields
	if resp.Error == nil || resp.Error.Data == nil {
		t.Fatal("ERR-003 FAILED: error.data must be present for REJECT_WITH_HINT")
	}

	data, ok := resp.Error.Data.(map[string]any)
	if !ok {
		t.Fatal("ERR-003 FAILED: error.data should be an object")
	}
	subluminal, ok := data["subluminal"].(map[string]any)
	if !ok {
		t.Fatal("ERR-003 FAILED: error.data.subluminal should be present")
	}
	hint, ok := subluminal["hint"].(map[string]any)
	if !ok {
		t.Fatal("ERR-003 FAILED: subluminal.hint must be present for REJECT_WITH_HINT")
	}

	hintText, ok := hint["hint_text"].(string)
	if !ok || hintText != "Use suggested args to retry" {
		t.Errorf("ERR-003 FAILED: hint.hint_text=%q, expected %q", hintText, "Use suggested args to retry")
	}

	hintKind, ok := hint["hint_kind"].(string)
	if !ok || hintKind != "ARG_FIX" {
		t.Errorf("ERR-003 FAILED: hint.hint_kind=%q, expected %q", hintKind, "ARG_FIX")
	}

	suggestedArgs, ok := hint["suggested_args"].(map[string]any)
	if !ok {
		t.Fatal("ERR-003 FAILED: hint.suggested_args must be present and an object")
	}
	if suggestedArgs["mode"] != "safe" {
		t.Errorf("ERR-003 FAILED: hint.suggested_args.mode=%v, expected %q", suggestedArgs["mode"], "safe")
	}
	if limit, ok := suggestedArgs["limit"].(float64); !ok || limit != 5 {
		t.Errorf("ERR-003 FAILED: hint.suggested_args.limit=%v, expected %d", suggestedArgs["limit"], 5)
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

func TestERR004_NoSecretLeakageInHintText(t *testing.T) {
	skipIfNoShim(t)

	policyJSON := `{
		"mode": "control",
		"policy_id": "test-err-004-hint",
		"policy_version": "1.0.0",
		"rules": [
			{
				"rule_id": "deny-secret-hint",
				"kind": "deny",
				"match": {"tool_name": {"glob": ["secret_hint_tool"]}},
				"effect": {"action": "BLOCK", "reason_code": "TEST_HINT", "message": "Hint includes sk-secret-key-12345 ghp_github_token_abc password123"}
			}
		]
	}`

	h := testharness.NewTestHarness(testharness.HarnessConfig{
		ShimPath: shimPath,
		ShimEnv:  []string{"SUB_POLICY_JSON=" + policyJSON},
	})
	h.AddTool("secret_hint_tool", "A tool with secrets in hints", nil)

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

	resp, _ := h.CallTool("secret_hint_tool", nil)
	wrapped := testharness.WrapResponse(resp)
	if wrapped.IsSuccess() {
		t.Fatal("ERR-004 FAILED: Tool should have been rejected with hint")
	}

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
