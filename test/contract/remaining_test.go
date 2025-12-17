// Package contract contains integration tests for Subluminal contracts.
//
// This file tests remaining contracts: ID-*, LED-*, IMP-*, ADAPT-*
// Reference: Contract-Test-Checklist.md
package contract

import (
	"testing"

	"github.com/subluminal/subluminal/pkg/testharness"
)

// =============================================================================
// ID-001: Identity Env Vars Applied
// Contract: SUB_RUN_ID, SUB_AGENT_ID, SUB_ENV, SUB_CLIENT, etc. are read from
//           environment and stamped into events.
// Reference: Interface-Pack.md §5, Contract-Test-Checklist.md ID-001
// =============================================================================

func TestID001_IdentityEnvVarsApplied(t *testing.T) {
	skipIfNoShim(t)

	// Note: This test requires env vars to be set before starting shim:
	// SUB_RUN_ID, SUB_AGENT_ID, SUB_ENV, SUB_CLIENT

	h := testharness.NewTestHarness(testharness.HarnessConfig{
		ShimPath: shimPath,
		ShimEnv: []string{
			"SUB_RUN_ID=test-run-id-12345",
			"SUB_AGENT_ID=test-agent-id-67890",
			"SUB_ENV=ci",
			"SUB_CLIENT=claude",
		},
	})
	h.AddTool("test_tool", "A test tool", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	h.Initialize()
	h.CallTool("test_tool", nil)

	// Assert: Events carry values from env vars
	events := h.Events()
	if len(events) == 0 {
		t.Fatal("ID-001 FAILED: No events captured")
	}

	for _, evt := range events {
		// Check run_id matches env var
		runID := testharness.GetString(evt, "run_id")
		if runID != "test-run-id-12345" {
			t.Errorf("ID-001 FAILED: Event run_id=%q, expected 'test-run-id-12345'\n"+
				"  Per Interface-Pack §5, SUB_RUN_ID should be applied", runID)
		}

		// Check agent_id matches env var
		agentID := testharness.GetString(evt, "agent_id")
		if agentID != "test-agent-id-67890" {
			t.Errorf("ID-001 FAILED: Event agent_id=%q, expected 'test-agent-id-67890'\n"+
				"  Per Interface-Pack §5, SUB_AGENT_ID should be applied", agentID)
		}

		// Check env matches
		env := testharness.GetString(evt, "env")
		if env != "ci" {
			t.Errorf("ID-001 FAILED: Event env=%q, expected 'ci'\n"+
				"  Per Interface-Pack §5, SUB_ENV should be applied", env)
		}

		// Check client matches
		client := testharness.GetString(evt, "client")
		if client != "claude" {
			t.Errorf("ID-001 FAILED: Event client=%q, expected 'claude'\n"+
				"  Per Interface-Pack §5, SUB_CLIENT should be applied", client)
		}
	}
}

// =============================================================================
// ID-002: Workload Context Tolerance
// Contract: Consumers (ledger/UI) do not crash when workload fields are missing;
//           display "unknown" safely.
// Reference: Interface-Pack.md §1.3.1, Contract-Test-Checklist.md ID-002
// Priority: P1 (not blocking v0.1)
// =============================================================================

func TestID002_WorkloadContextTolerance(t *testing.T) {
	skipIfNoShim(t)

	// Note: This test verifies the shim doesn't crash when workload is omitted

	h := newShimHarness()
	// Don't set any workload-related env vars

	h.AddTool("test_tool", "A test tool", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	// Execute: Run without workload context
	h.Initialize()
	h.CallTool("test_tool", nil)

	// Assert: Shim didn't crash, events were emitted
	events := h.Events()
	if len(events) == 0 {
		t.Fatal("ID-002 FAILED: No events captured - shim may have crashed")
	}

	// Assert: Events can be processed (no crash-inducing missing fields)
	// Consumer tolerance is really about ledger/UI, but we can verify
	// the events are well-formed even without workload
	for _, evt := range events {
		// Required fields should still be present
		if !testharness.HasField(evt, "run_id") {
			t.Error("ID-002 FAILED: Required field run_id missing")
		}
		if !testharness.HasField(evt, "type") {
			t.Error("ID-002 FAILED: Required field type missing")
		}
	}
}

// =============================================================================
// LED-001: Ledger Ingestion Durability
// Contract: Events ingested to ledger survive; run/call counts correct;
//           indexes used (query is fast).
// Reference: Contract-Test-Checklist.md LED-001
// =============================================================================

func TestLED001_LedgerIngestionDurability(t *testing.T) {
	t.Skip("LED-001: Requires ledger component (not yet implemented)")

	// This test would:
	// 1. Start ledgerd with WAL enabled
	// 2. Ingest 10k events
	// 3. Verify DB not corrupted
	// 4. Verify run/call counts correct
	// 5. Verify queries use indexes (are fast)
}

// =============================================================================
// LED-002: Backpressure Drops Previews Not Decisions
// Contract: Under backpressure, decision events persist; preview fields may
//           be dropped; no shim blocking.
// Reference: Contract-Test-Checklist.md LED-002
// =============================================================================

func TestLED002_BackpressureDropsPreviewsNotDecisions(t *testing.T) {
	t.Skip("LED-002: Requires ledger component with backpressure simulation")

	// This test would:
	// 1. Force ingest overload (slow disk simulation)
	// 2. Burst events at high rate
	// 3. Verify decision events persist
	// 4. Verify preview fields may be dropped/truncated
	// 5. Verify shim doesn't block
}

// =============================================================================
// IMP-001: Importer Backup + Restore Correctness
// Contract: After import, config can be restored to original state.
// Reference: Contract-Test-Checklist.md IMP-001
// =============================================================================

func TestIMP001_ImporterBackupRestoreCorrectness(t *testing.T) {
	t.Skip("IMP-001: Requires importer component (not yet implemented)")

	// This test would:
	// 1. Have existing Claude/Codex config
	// 2. Run import
	// 3. Run restore
	// 4. Verify config identical to original (byte compare)
	// 5. Verify import preserves server names
}

// =============================================================================
// IMP-002: Time-to-First-Log < 5 Minutes Path
// Contract: From fresh install to first tool_call_start observed < 5 minutes.
// Reference: Contract-Test-Checklist.md IMP-002
// =============================================================================

func TestIMP002_TimeToFirstLogUnder5Minutes(t *testing.T) {
	t.Skip("IMP-002: Requires fresh install simulation (not yet implemented)")

	// This test would:
	// 1. Start with fresh install fixture
	// 2. Run import → agent → call tool
	// 3. Verify first tool_call_start within 5 minutes
}

// =============================================================================
// ADAPT-001: Adapter Provides Required Fields to Core
// Contract: Adapter sends server_name, tool_name, args, bytes_in, transport
//           to core; no protocol-specific data leaks.
// Reference: Interface-Pack.md §7.1, Contract-Test-Checklist.md ADAPT-001
// =============================================================================

func TestADAPT001_AdapterProvidesRequiredFieldsToCore(t *testing.T) {
	skipIfNoShim(t)

	h := newShimHarness()
	h.AddTool("adapter_test", "Test adapter fields", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	h.Initialize()
	h.CallTool("adapter_test", map[string]any{"key": "value"})

	// Assert: tool_call_start has all required adapter fields
	toolCallStarts := h.EventSink.ByType("tool_call_start")
	if len(toolCallStarts) == 0 {
		t.Fatal("ADAPT-001 FAILED: No tool_call_start events")
	}

	evt := toolCallStarts[0]

	// Check required fields from adapter (per §7.1)
	requiredFields := []string{
		"call.server_name",
		"call.tool_name",
		"call.args_hash", // Core computes from args
		"call.bytes_in",
		"call.transport",
	}

	for _, field := range requiredFields {
		if !testharness.HasField(evt, field) {
			t.Errorf("ADAPT-001 FAILED: Missing required field %q\n"+
				"  Per Interface-Pack §7.1, adapter must provide this to core", field)
		}
	}

	// Assert: transport is a known value
	transport := testharness.GetString(evt, "call.transport")
	validTransports := map[string]bool{
		"mcp_stdio": true, "mcp_http": true, "http": true, "messages_api": true, "unknown": true,
	}
	if !validTransports[transport] {
		t.Errorf("ADAPT-001 FAILED: Unknown transport %q", transport)
	}
}

// =============================================================================
// ADAPT-002: Core is Protocol-Agnostic
// Contract: Same tool call via different adapters produces identical decisions
//           and events; args_hash matches.
// Reference: Interface-Pack.md §7.2, Contract-Test-Checklist.md ADAPT-002
// =============================================================================

func TestADAPT002_CoreIsProtocolAgnostic(t *testing.T) {
	t.Skip("ADAPT-002: Requires multiple adapter implementations to compare")

	// This test would:
	// 1. Call same tool via MCP stdio adapter
	// 2. Call same tool via MCP HTTP adapter
	// 3. Verify identical decisions
	// 4. Verify identical args_hash
}

// =============================================================================
// ADAPT-003: Adapter Formats Errors Correctly
// Contract: When policy blocks, client receives valid protocol-specific error
//           (JSON-RPC for MCP) with error.data.subluminal fields.
// Reference: Interface-Pack.md §7.2, Contract-Test-Checklist.md ADAPT-003
// Priority: P1 (not blocking v0.1)
// =============================================================================

func TestADAPT003_AdapterFormatsErrorsCorrectly(t *testing.T) {
	skipIfNoShim(t)

	// Note: Requires policy that blocks calls

	h := newShimHarness()
	h.AddTool("blocked_tool", "A blocked tool", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	h.Initialize()
	resp, _ := h.CallTool("blocked_tool", nil)

	wrapped := testharness.WrapResponse(resp)
	if wrapped.IsSuccess() {
		t.Skip("ADAPT-003: Tool not blocked - needs policy configuration")
	}

	// Assert: Error is valid JSON-RPC format
	if resp.Error == nil {
		t.Fatal("ADAPT-003 FAILED: Response should have error field")
	}

	// Assert: Error code is a Subluminal code
	subluminalCodes := map[int]bool{
		-32081: true, // POLICY_BLOCKED
		-32082: true, // POLICY_THROTTLED
		-32083: true, // REJECT_WITH_HINT
		-32084: true, // RUN_TERMINATED
	}
	if !subluminalCodes[resp.Error.Code] {
		t.Logf("ADAPT-003: Error code %d is not a Subluminal policy code (may be upstream error)", resp.Error.Code)
	}

	// Assert: error.data.subluminal present if policy error
	if resp.Error.Data != nil {
		data, ok := resp.Error.Data.(map[string]any)
		if ok {
			if _, exists := data["subluminal"]; !exists && subluminalCodes[resp.Error.Code] {
				t.Error("ADAPT-003 FAILED: Policy error should have error.data.subluminal")
			}
		}
	}
}

