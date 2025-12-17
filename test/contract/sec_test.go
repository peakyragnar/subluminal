// Package contract contains integration tests for Subluminal contracts.
//
// This file tests SEC-* contracts (secrets and injection).
// Reference: Interface-Pack.md §4, Contract-Test-Checklist.md SEC-001/002
package contract

import (
	"strings"
	"testing"

	"github.com/subluminal/subluminal/pkg/testharness"
)

// =============================================================================
// SEC-001: Secret Injection - Agent Never Sees Secrets
// Contract: Upstream tool receives injected secrets; agent-side args and
//           event previews do not include secret values.
// Reference: Interface-Pack.md §4, Contract-Test-Checklist.md SEC-001
// =============================================================================

func TestSEC001_SecretInjectionAgentNeverSeesSecrets(t *testing.T) {
	skipIfNoShim(t)

	// Note: This test requires:
	// - Secret bindings configured for the tool server
	// - Upstream tool that uses the injected env var

	h := newShimHarness()

	// Tool simulates using an injected secret
	h.AddTool("api_call", "Make an API call", func(args map[string]any) (string, error) {
		// In real test, upstream would read from env var injected by shim
		// Simulates: token := os.Getenv("API_TOKEN")
		return "api call succeeded", nil
	})

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	h.Initialize()

	// Known secret value (would be configured in secret bindings)
	secretValue := "sk-secret-api-key-xyz123"

	// Execute: Call tool that uses secret
	resp, err := h.CallTool("api_call", map[string]any{
		"endpoint": "/users",
		// Note: Agent does NOT pass the secret - shim injects it
	})
	if err != nil {
		t.Fatalf("Failed to call tool: %v", err)
	}

	// Assert: Call succeeded (upstream got the token)
	wrapped := testharness.WrapResponse(resp)
	if !wrapped.IsSuccess() {
		t.Errorf("SEC-001 FAILED: API call should succeed with injected token\n"+
			"  Error: %s", wrapped.ErrorMessage())
	}

	// Assert: Agent-side call args do NOT include secret
	toolCallStarts := h.EventSink.ByType("tool_call_start")
	for _, evt := range toolCallStarts {
		// Check args_preview doesn't contain secret
		argsPreview := testharness.GetString(evt, "call.preview.args_preview")
		if strings.Contains(argsPreview, secretValue) {
			t.Errorf("SEC-001 FAILED: args_preview contains secret value!\n"+
				"  Per Interface-Pack §4, agent must NEVER see secrets")
		}

		// Check entire event doesn't contain secret
		if strings.Contains(evt.Raw, secretValue) {
			t.Errorf("SEC-001 FAILED: Event contains secret value!\n"+
				"  Per Interface-Pack §4, secrets must NEVER appear in events")
		}
	}

	// Assert: No events contain the secret
	for _, evt := range h.Events() {
		if strings.Contains(evt.Raw, secretValue) {
			t.Errorf("SEC-001 FAILED: Event %d (type=%s) contains secret value!\n"+
				"  Per Interface-Pack §4, secrets must NEVER be logged",
				evt.Index, evt.Type)
		}
	}
}

// =============================================================================
// SEC-002: secret_injection Event Contains Metadata Only
// Contract: secret_injection event includes {inject_as, secret_ref, source, success}
//           but NO actual secret values.
// Reference: Interface-Pack.md §4, Contract-Test-Checklist.md SEC-002
// =============================================================================

func TestSEC002_SecretInjectionEventMetadataOnly(t *testing.T) {
	skipIfNoShim(t)

	// Note: This test requires secret_injection events to be enabled

	h := newShimHarness()
	h.AddTool("secret_tool", "A tool using secrets", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	h.Initialize()
	h.CallTool("secret_tool", nil)

	// Look for secret_injection event
	injectionEvents := h.EventSink.ByType("secret_injection")
	if len(injectionEvents) == 0 {
		t.Skip("SEC-002: No secret_injection events emitted (feature may not be enabled)")
	}

	// Known secret values that must NOT appear
	knownSecrets := []string{
		"sk-secret-key-12345",
		"ghp_github_token_abc",
	}

	for _, evt := range injectionEvents {
		// Assert: Required metadata fields are present
		requiredFields := []string{"inject_as", "secret_ref", "source", "success"}
		for _, field := range requiredFields {
			if !testharness.HasField(evt, field) {
				t.Errorf("SEC-002 FAILED: secret_injection event missing required field %q\n"+
					"  Per Interface-Pack §4, event must include: %v", field, requiredFields)
			}
		}

		// Assert: No actual secret values present
		for _, secret := range knownSecrets {
			if strings.Contains(evt.Raw, secret) {
				t.Errorf("SEC-002 FAILED: secret_injection event contains actual secret value!\n"+
					"  Per Interface-Pack §4, only metadata allowed, no values")
			}
		}

		// Assert: No field named "value" or "secret_value"
		if testharness.HasField(evt, "value") || testharness.HasField(evt, "secret_value") {
			t.Error("SEC-002 FAILED: secret_injection event should not have 'value' field\n" +
				"  Per Interface-Pack §4, only metadata, never values")
		}
	}
}
