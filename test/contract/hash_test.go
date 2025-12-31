// Package contract contains integration tests for Subluminal contracts.
//
// This file tests HASH-* contracts (canonicalization and hashing).
// Reference: Interface-Pack.md §1.9, Contract-Test-Checklist.md HASH-001/002
package contract

import (
	"testing"

	"github.com/subluminal/subluminal/pkg/testharness"
)

// =============================================================================
// HASH-001: Canonicalization Equivalence
// Contract: Two argument objects with different key order MUST produce the
//           same args_hash.
// Reference: Interface-Pack.md §1.9.1, Contract-Test-Checklist.md HASH-001
// =============================================================================

func TestHASH001_CanonicalizationEquivalence(t *testing.T) {
	skipIfNoShim(t)

	h := newShimHarness()
	h.AddTool("test_tool", "A test tool", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	h.Initialize()

	// Execute: Call same tool twice with reordered keys
	// Fixture A: keys in order a, b, c
	argsA := map[string]any{
		"a": 1,
		"b": 2,
		"c": 3,
	}

	// Fixture B: keys in order c, b, a (different order, same content)
	argsB := map[string]any{
		"c": 3,
		"b": 2,
		"a": 1,
	}

	h.CallTool("test_tool", argsA)
	h.CallTool("test_tool", argsB)

	// Stop harness to ensure all events are captured before reading
	h.Stop()

	// Get both tool_call_start events
	toolCallStarts := h.EventSink.ByType("tool_call_start")
	if len(toolCallStarts) != 2 {
		t.Fatalf("HASH-001 FAILED: Expected 2 tool_call_start events, got %d", len(toolCallStarts))
	}

	// Assert: args_hash is identical for both calls
	hashA := testharness.GetString(toolCallStarts[0], "call.args_hash")
	hashB := testharness.GetString(toolCallStarts[1], "call.args_hash")

	if hashA == "" || hashB == "" {
		t.Fatal("HASH-001 FAILED: args_hash missing from events")
	}

	if hashA != hashB {
		t.Errorf("HASH-001 FAILED: Different key order produced different hashes\n"+
			"  Per Interface-Pack §1.9.1, canonicalization must produce identical hashes\n"+
			"  Args A (a,b,c order): hash=%s\n"+
			"  Args B (c,b,a order): hash=%s\n"+
			"  These MUST be identical", hashA, hashB)
	}
}

// =============================================================================
// HASH-002: Canonicalization Stability
// Contract: args_hash exactly matches golden value (precomputed) every time.
// Reference: Interface-Pack.md §1.9.1, Contract-Test-Checklist.md HASH-002
// =============================================================================

func TestHASH002_CanonicalizationStability(t *testing.T) {
	skipIfNoShim(t)

	h := newShimHarness()
	h.AddTool("test_tool", "A test tool", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	h.Initialize()

	// Fixed fixture args - must always produce the same hash
	fixedArgs := map[string]any{
		"command": "git push",
		"branch":  "main",
		"force":   false,
	}

	// Golden value: precomputed SHA-256 of canonical JSON
	// Canonical JSON: {"branch":"main","command":"git push","force":false}
	// This is the expected hash that MUST match every time
	goldenHash := "e13c608f6d08e999ce8c39b52fa274a731ccab0e6b3b3e62cd23511b93d7527e"

	// Execute: Call multiple times with same args
	for i := 0; i < 5; i++ {
		h.CallTool("test_tool", fixedArgs)
	}

	// Stop harness to ensure all events are captured before reading
	h.Stop()

	// Assert: All hashes match golden value
	toolCallStarts := h.EventSink.ByType("tool_call_start")
	if len(toolCallStarts) != 5 {
		t.Fatalf("HASH-002 FAILED: Expected 5 tool_call_start events, got %d", len(toolCallStarts))
	}

	for i, evt := range toolCallStarts {
		hash := testharness.GetString(evt, "call.args_hash")

		if hash != goldenHash {
			t.Errorf("HASH-002 FAILED: Call %d hash does not match golden value\n"+
				"  Per Interface-Pack §1.9.1, hash must be stable and match precomputed value\n"+
				"  Expected (golden): %s\n"+
				"  Got:               %s", i+1, goldenHash, hash)
		}
	}
}
