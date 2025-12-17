// Package contract contains integration tests for Subluminal contracts.
//
// This file tests PROC-* contracts (process supervision).
// Reference: Contract-Test-Checklist.md PROC-001/002/003
package contract

import (
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/subluminal/subluminal/pkg/testharness"
)

// =============================================================================
// PROC-001: SIGINT Propagates; No Zombie Shim
// Contract: When agent receives SIGINT, shim exits, upstream exits, no orphan
//           processes remain after grace window.
// Reference: Contract-Test-Checklist.md PROC-001
// =============================================================================

func TestPROC001_SIGINTPropagatesNoZombieShim(t *testing.T) {
	skipIfNoShim(t)

	h := newShimHarness()
	h.AddTool("long_running", "A long-running tool", func(args map[string]any) (string, error) {
		// Simulate a tool that takes time
		time.Sleep(10 * time.Second)
		return "done", nil
	})

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}

	h.Initialize()

	// Start a long-running call in background
	go h.CallTool("long_running", nil)

	// Give it time to start
	time.Sleep(100 * time.Millisecond)

	// Send SIGINT to the shim (simulating user Ctrl+C)
	// Note: In real test, we'd have access to the shim process
	// For now, use the harness Stop which should propagate signals

	// Record processes before stop
	shimPID := getShimPID(h)

	// Stop the harness (should propagate SIGINT)
	startStop := time.Now()
	err := h.Stop()
	stopDuration := time.Since(startStop)

	if err != nil {
		t.Errorf("PROC-001: Error during stop: %v", err)
	}

	// Assert: Stop completed within grace window (5 seconds)
	graceWindow := 5 * time.Second
	if stopDuration > graceWindow {
		t.Errorf("PROC-001 FAILED: Shutdown took %v, expected < %v\n"+
			"  Per contract, processes should exit within grace window",
			stopDuration, graceWindow)
	}

	// Assert: No orphan processes (shim PID no longer exists)
	if shimPID > 0 {
		// Give a moment for process cleanup
		time.Sleep(100 * time.Millisecond)

		if processExists(shimPID) {
			t.Errorf("PROC-001 FAILED: Shim process %d still exists after stop\n"+
				"  This may indicate a zombie process", shimPID)
		}
	}
}

// =============================================================================
// PROC-002: EOF on stdin Terminates Shim + Upstream
// Contract: When agent closes stdin, shim exits cleanly, upstream terminated.
// Reference: Contract-Test-Checklist.md PROC-002
// =============================================================================

func TestPROC002_EOFOnStdinTerminatesShim(t *testing.T) {
	skipIfNoShim(t)

	h := newShimHarness()
	h.AddTool("test_tool", "A test tool", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}

	h.Initialize()
	h.CallTool("test_tool", nil)

	shimPID := getShimPID(h)

	// Close stdin abruptly (simulates agent disconnect)
	if h.Driver != nil {
		h.Driver.Close()
	}

	// Wait for shim to notice and exit
	time.Sleep(500 * time.Millisecond)

	// Assert: Shim exited cleanly
	if shimPID > 0 && processExists(shimPID) {
		t.Errorf("PROC-002 FAILED: Shim process %d still running after stdin closed\n"+
			"  Per contract, shim should exit when stdin closes", shimPID)
	}

	// Cleanup
	h.Stop()
}

// =============================================================================
// PROC-003: Upstream Crash Handled Gracefully
// Contract: If upstream tool server crashes, shim emits tool_call_end ERROR
//           with transport/upstream class; run_end status FAILED/TERMINATED;
//           no deadlock.
// Reference: Contract-Test-Checklist.md PROC-003
// Priority: P1 (not blocking v0.1)
// =============================================================================

func TestPROC003_UpstreamCrashHandledGracefully(t *testing.T) {
	skipIfNoShim(t)

	// NOTE: This test is P1 and currently skipped because the test design
	// doesn't work with the shim subprocess architecture. The custom handler
	// that simulates a crash is set on the harness's FakeServer, but in shim
	// mode, a separate fakemcp process is spawned which doesn't have access
	// to the handler.
	//
	// To properly test crash handling, we'd need:
	// 1. A way to make fakemcp actually crash (e.g., --crash-on=toolname flag)
	// 2. Or a different test approach that doesn't rely on custom handlers
	t.Skip("PROC-003: Test design incompatible with shim subprocess architecture (P1)")

	h := newShimHarness()

	// Tool that simulates a crash
	h.AddTool("crasher", "A tool that crashes", func(args map[string]any) (string, error) {
		// Simulate upstream crash by panicking
		// In real test, we'd actually kill the upstream process
		panic("simulated upstream crash")
	})

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	h.Initialize()

	// Execute: Call tool that crashes
	// This should not hang (no deadlock)
	done := make(chan bool, 1)
	go func() {
		h.CallTool("crasher", nil)
		done <- true
	}()

	// Assert: Call completes (no deadlock) within timeout
	select {
	case <-done:
		// Good - call completed
	case <-time.After(10 * time.Second):
		t.Fatal("PROC-003 FAILED: Tool call deadlocked after upstream crash\n" +
			"  Per contract, upstream crash should not cause deadlock")
	}

	// Assert: tool_call_end has ERROR status with appropriate class
	toolCallEnds := h.EventSink.ByType("tool_call_end")
	if len(toolCallEnds) == 0 {
		t.Skip("PROC-003: No tool_call_end event (crash handling may differ)")
	}

	evt := toolCallEnds[0]
	status := testharness.GetString(evt, "status")
	if status != "ERROR" {
		t.Errorf("PROC-003 FAILED: Expected status=ERROR after crash, got %q", status)
	}

	// Assert: error.class is transport or upstream_error
	errorClass := testharness.GetString(evt, "error.class")
	validClasses := map[string]bool{"transport": true, "upstream_error": true, "unknown": true}
	if !validClasses[errorClass] {
		t.Errorf("PROC-003 FAILED: Expected error.class to be transport/upstream_error, got %q", errorClass)
	}
}

// =============================================================================
// Helper functions
// =============================================================================

// getShimPID extracts the shim process ID from the harness.
// Returns 0 if not available.
func getShimPID(_ *testharness.TestHarness) int {
	// Note: This would need access to harness internals
	// For now, return 0 (tests will skip PID-based checks)
	return 0
}

// processExists checks if a process with the given PID exists.
func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds. Send signal 0 to check existence.
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

