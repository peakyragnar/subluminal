// Package contract contains integration tests for Subluminal contracts.
//
// This file tests BUF-* contracts (buffer management and truncation).
// Reference: Interface-Pack.md §1.10, Contract-Test-Checklist.md BUF-001/002/003/004
package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/subluminal/subluminal/pkg/canonical"
	"github.com/subluminal/subluminal/pkg/testharness"
)

// =============================================================================
// BUF-001: Bounded Inspection - Truncate
// Contract: Shim forwards large payloads successfully; events set
//           preview.truncated=true; preview omitted or "[TRUNCATED]".
// Reference: Interface-Pack.md §1.10, Contract-Test-Checklist.md BUF-001
// =============================================================================

func TestBUF001_BoundedInspectionTruncate(t *testing.T) {
	skipIfNoShim(t)

	h := newShimHarness()

	// Tool that echoes input size (to verify full payload arrived)
	h.AddTool("echo_size", "Echo payload size", func(args map[string]any) (string, error) {
		data, _ := args["data"].(string)
		return string(rune(len(data))), nil
	})

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	h.Initialize()

	// Execute: Create args payload > 1 MiB (MAX_INSPECT_BYTES)
	largeData := strings.Repeat("x", 1024*1024+1) // 1 MiB + 1 byte
	h.CallTool("echo_size", map[string]any{"data": largeData})

	// Assert: Event has preview.truncated=true
	toolCallStarts := h.EventSink.ByType("tool_call_start")
	if len(toolCallStarts) == 0 {
		t.Fatal("BUF-001 FAILED: No tool_call_start events")
	}

	evt := toolCallStarts[0]
	truncated := testharness.GetBool(evt, "call.preview.truncated")
	if !truncated {
		t.Error("BUF-001 FAILED: preview.truncated should be true for payload > 1 MiB\n" +
			"  Per Interface-Pack §1.10, large payloads must set preview.truncated=true")
	}

	// Assert: args_preview is omitted or "[TRUNCATED]"
	argsPreview := testharness.GetString(evt, "call.preview.args_preview")
	if argsPreview != "" && argsPreview != "[TRUNCATED]" {
		t.Errorf("BUF-001 FAILED: args_preview should be omitted or '[TRUNCATED]'\n"+
			"  Per Interface-Pack §1.10, truncated payloads must not include preview\n"+
			"  Got: %q", argsPreview)
	}
}

// =============================================================================
// BUF-002: No OOM on Large Payload
// Contract: Process RSS stable (bounded), no crashes; forwarding completes;
//           events still emitted.
// Reference: Interface-Pack.md §1.10, Contract-Test-Checklist.md BUF-002
// =============================================================================

func TestBUF002_NoOOMOnLargePayload(t *testing.T) {
	skipIfNoShim(t)

	h := newShimHarness()
	h.AddTool("sink", "Sink data", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	h.Initialize()

	// Get initial memory stats
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	// Execute: 50 large calls concurrently
	largeData := strings.Repeat("y", 1024*1024) // 1 MiB each
	var wg sync.WaitGroup
	errors := make(chan error, 50)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := h.CallTool("sink", map[string]any{
				"index": idx,
				"data":  largeData,
			})
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Assert: No errors during calls
	for err := range errors {
		t.Errorf("BUF-002 FAILED: Call failed during stress test: %v", err)
	}

	// Assert: Events were emitted (shim didn't crash)
	toolCallStarts := h.EventSink.ByType("tool_call_start")
	if len(toolCallStarts) < 50 {
		t.Errorf("BUF-002 FAILED: Expected 50 tool_call_start events, got %d\n"+
			"  This may indicate crashes or dropped events", len(toolCallStarts))
	}

	// Assert: Memory stayed bounded (rough check - not exhaustive)
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	// Allow some memory growth but not 50x the payload size
	maxAllowedGrowth := uint64(100 * 1024 * 1024) // 100 MiB
	// Only check growth if memory increased (GC may have freed memory)
	if memAfter.Alloc > memBefore.Alloc {
		growth := memAfter.Alloc - memBefore.Alloc
		if growth > maxAllowedGrowth {
			t.Errorf("BUF-002 FAILED: Excessive memory growth during stress test\n"+
				"  Per Interface-Pack §1.10, shim must handle large payloads without OOM\n"+
				"  Memory growth: %d bytes (max allowed: %d)", growth, maxAllowedGrowth)
		}
	}
}

// =============================================================================
// BUF-003: Forwarding Correctness Under Truncation
// Contract: Upstream receives full payload (size matches); shim did not
//           corrupt stream.
// Reference: Interface-Pack.md §1.10, Contract-Test-Checklist.md BUF-003
// =============================================================================

func TestBUF003_ForwardingCorrectnessUnderTruncation(t *testing.T) {
	skipIfNoShim(t)

	// Use measure-size mode: fakemcp returns {"bytes_received": N} for each call
	h := testharness.NewTestHarness(testharness.HarnessConfig{
		ShimPath:    shimPath,
		MeasureSize: true,
	})
	h.AddTool("measure", "Measure payload size", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	h.Initialize()

	// Execute: Send large payload (2 MiB)
	expectedDataSize := 1024 * 1024 * 2
	largeData := strings.Repeat("z", expectedDataSize)
	resp, err := h.CallTool("measure", map[string]any{"data": largeData})
	if err != nil {
		t.Fatalf("Failed to call tool: %v", err)
	}

	// Parse response to get bytes_received
	wrapped := testharness.WrapResponse(resp)
	if !wrapped.IsSuccess() {
		t.Fatalf("Tool call failed: %s", wrapped.ErrorMessage())
	}

	// Extract the text result (which is JSON: {"bytes_received": N})
	resultText := wrapped.ResultText()
	if resultText == "" {
		t.Fatal("BUF-003 FAILED: No result text returned from measure-size mode")
	}

	// Parse the JSON to get bytes_received
	var resultData map[string]any
	if err := json.Unmarshal([]byte(resultText), &resultData); err != nil {
		t.Fatalf("BUF-003 FAILED: Could not parse result JSON: %v\nResult text: %s", err, resultText)
	}

	bytesReceived, ok := resultData["bytes_received"].(float64)
	if !ok {
		t.Fatalf("BUF-003 FAILED: bytes_received not found or not a number in result: %v", resultData)
	}

	// The bytes_received should be at least the size of the data
	// (it will be slightly larger due to JSON structure: {"data":"..."})
	if int(bytesReceived) < expectedDataSize {
		t.Errorf("BUF-003 FAILED: Upstream received truncated payload\n"+
			"  Per Interface-Pack §1.10, shim MUST forward full traffic even if truncating for inspection\n"+
			"  Expected at least: %d bytes (data size)\n"+
			"  Received: %d bytes", expectedDataSize, int(bytesReceived))
	}
}

// =============================================================================
// BUF-004: Rolling Hash for Truncated Payload
// Contract: args_stream_hash present and matches expected SHA-256 of raw bytes.
// Reference: Interface-Pack.md §1.9.2, Contract-Test-Checklist.md BUF-004
// Priority: P1 (not blocking v0.1)
// =============================================================================

func TestBUF004_RollingHashForTruncatedPayload(t *testing.T) {
	skipIfNoShim(t)

	h := newShimHarness()
	h.AddTool("test_tool", "A test tool", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	h.Initialize()

	// Execute: Large payload that triggers truncation
	largeData := strings.Repeat("w", 1024*1024*2) // 2 MiB
	h.CallTool("test_tool", map[string]any{"data": largeData})

	// Get tool_call_start event
	toolCallStarts := h.EventSink.ByType("tool_call_start")
	if len(toolCallStarts) == 0 {
		t.Fatal("BUF-004 FAILED: No tool_call_start events")
	}

	evt := toolCallStarts[0]

	// Assert: args_stream_hash is present for truncated payload
	streamHash := testharness.GetString(evt, "call.args_stream_hash")
	if streamHash == "" {
		t.Error("BUF-004 FAILED: args_stream_hash is empty\n" +
			"  Per Interface-Pack §1.9.2, truncated payloads should include rolling hash")
	}

	argsHash := testharness.GetString(evt, "call.args_hash")
	if argsHash == "" {
		t.Error("BUF-004 FAILED: args_hash is empty\n" +
			"  Per Interface-Pack §1.9.1, args_hash should be canonical even when previews truncate")
	}

	expectedArgsHash, err := canonical.ArgsHash(map[string]any{"data": largeData})
	if err != nil {
		t.Fatalf("BUF-004 FAILED: Could not compute canonical args_hash: %v", err)
	}
	if argsHash != expectedArgsHash {
		t.Errorf("BUF-004 FAILED: args_hash mismatch\n"+
			"  Expected: %s\n"+
			"  Got:      %s", expectedArgsHash, argsHash)
	}

	argsRaw, err := json.Marshal(map[string]any{"data": largeData})
	if err != nil {
		t.Fatalf("BUF-004 FAILED: Could not marshal args: %v", err)
	}

	expectedStreamHash := sha256Hex(argsRaw)
	if streamHash != expectedStreamHash {
		t.Errorf("BUF-004 FAILED: args_stream_hash mismatch\n"+
			"  Expected: %s\n"+
			"  Got:      %s", expectedStreamHash, streamHash)
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
