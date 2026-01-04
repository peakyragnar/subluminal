// Package contract contains integration tests for Subluminal contracts.
//
// This file tests remaining contracts: ID-*, LED-*, IMP-*, ADAPT-*
// Reference: Contract-Test-Checklist.md
package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/subluminal/subluminal/pkg/canonical"
	"github.com/subluminal/subluminal/pkg/core"
	"github.com/subluminal/subluminal/pkg/event"
	"github.com/subluminal/subluminal/pkg/importer"
	"github.com/subluminal/subluminal/pkg/ledger"
	"github.com/subluminal/subluminal/pkg/policy"
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
			"SUB_PRINCIPAL=test-user@example.com",
			`SUB_WORKLOAD={"repo":"subluminal","labels":{"team":"core"}}`,
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

		// Check principal matches
		principal := testharness.GetString(evt, "principal")
		if principal != "test-user@example.com" {
			t.Errorf("ID-001 FAILED: Event principal=%q, expected 'test-user@example.com'\n"+
				"  Per Interface-Pack §5, SUB_PRINCIPAL should be applied", principal)
		}

		// Check workload matches
		workloadRepo := testharness.GetString(evt, "workload.repo")
		if workloadRepo != "subluminal" {
			t.Errorf("ID-001 FAILED: Event workload.repo=%q, expected 'subluminal'\n"+
				"  Per Interface-Pack §5, SUB_WORKLOAD should be applied", workloadRepo)
		}
		workloadTeam := testharness.GetString(evt, "workload.labels.team")
		if workloadTeam != "core" {
			t.Errorf("ID-001 FAILED: Event workload.labels.team=%q, expected 'core'\n"+
				"  Per Interface-Pack §5, SUB_WORKLOAD should be applied", workloadTeam)
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
	const (
		runCount    = 2
		callsPerRun = 1666
	)

	dbPath := filepath.Join(t.TempDir(), "ledger.db")
	events := buildLedgerEvents(t, runCount, callsPerRun)

	if err := ledger.IngestJSONL(bytes.NewReader(events), dbPath); err != nil {
		t.Fatalf("LED-001 FAILED: ingest error: %v", err)
	}

	journalMode := strings.ToLower(sqliteQuery(t, dbPath, "PRAGMA journal_mode;"))
	if journalMode != "wal" {
		t.Fatalf("LED-001 FAILED: journal_mode=%q, expected WAL", journalMode)
	}

	integrity := strings.ToLower(sqliteQuery(t, dbPath, "PRAGMA integrity_check;"))
	if integrity != "ok" {
		t.Fatalf("LED-001 FAILED: integrity_check=%q, expected ok", integrity)
	}

	expectedRuns := fmt.Sprintf("%d", runCount)
	if actual := sqliteQuery(t, dbPath, "SELECT COUNT(*) FROM runs;"); actual != expectedRuns {
		t.Fatalf("LED-001 FAILED: run count=%s, expected %s", actual, expectedRuns)
	}

	expectedCalls := fmt.Sprintf("%d", runCount*callsPerRun)
	if actual := sqliteQuery(t, dbPath, "SELECT COUNT(*) FROM tool_calls;"); actual != expectedCalls {
		t.Fatalf("LED-001 FAILED: call count=%s, expected %s", actual, expectedCalls)
	}

	expectedPreviews := fmt.Sprintf("%d", runCount*callsPerRun)
	if actual := sqliteQuery(t, dbPath, "SELECT COUNT(*) FROM previews;"); actual != expectedPreviews {
		t.Fatalf("LED-001 FAILED: preview count=%s, expected %s", actual, expectedPreviews)
	}

	expectedHints := fmt.Sprintf("%d", runCount*callsPerRun)
	if actual := sqliteQuery(t, dbPath, "SELECT COUNT(*) FROM hints;"); actual != expectedHints {
		t.Fatalf("LED-001 FAILED: hint count=%s, expected %s", actual, expectedHints)
	}

	expectedPolicies := fmt.Sprintf("%d", runCount)
	if actual := sqliteQuery(t, dbPath, "SELECT COUNT(*) FROM policy_versions;"); actual != expectedPolicies {
		t.Fatalf("LED-001 FAILED: policy version count=%s, expected %s", actual, expectedPolicies)
	}
	if actual := sqliteQuery(t, dbPath, "SELECT COUNT(*) FROM policy_versions WHERE mode IS NULL OR mode = '';"); actual != "0" {
		t.Fatalf("LED-001 FAILED: policy version mode count=%s, expected 0", actual)
	}

	plan := sqliteQuery(t, dbPath, "EXPLAIN QUERY PLAN SELECT * FROM tool_calls WHERE run_id='run-1' ORDER BY created_at LIMIT 5;")
	if !strings.Contains(plan, "idx_tool_calls_run_created") {
		t.Fatalf("LED-001 FAILED: expected index usage, plan=%q", plan)
	}
}

// =============================================================================
// LED-002: Backpressure Drops Previews Not Decisions
// Contract: Under backpressure, decision events persist; preview fields may
//           be dropped; no shim blocking.
// Reference: Contract-Test-Checklist.md LED-002
// =============================================================================

func TestLED002_BackpressureDropsPreviewsNotDecisions(t *testing.T) {
	release := make(chan struct{})
	writer := newBlockingWriter(release)

	emitter := core.NewEmitterWithOptions(writer, core.EmitterOptions{
		BufferSize:           8,
		PreviewDropThreshold: 4,
	})
	emitter.Start()

	const totalCalls = 6
	emitDone := make(chan struct{})
	go func() {
		defer close(emitDone)
		for i := 0; i < totalCalls; i++ {
			callID := fmt.Sprintf("call-%d", i)
			emitter.Emit(makeStartEvent(callID, i+1))
			emitter.EmitSync(makeDecisionEvent(callID))
			emitter.Emit(makeEndEvent(callID))
		}
	}()

	select {
	case <-emitDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("LED-002 FAILED: emitter blocked under backpressure")
	}

	close(release)
	emitter.Close()

	sink := testharness.NewEventSink()
	if err := sink.Capture(bytes.NewReader(writer.Bytes())); err != nil {
		t.Fatalf("LED-002 FAILED: event capture error: %v", err)
	}
	if errors := sink.Errors(); len(errors) > 0 {
		t.Fatalf("LED-002 FAILED: event parse errors: %v", errors)
	}

	decisions := sink.ByType(string(event.EventTypeToolCallDecision))
	if len(decisions) != totalCalls {
		t.Fatalf("LED-002 FAILED: expected %d decision events, got %d", totalCalls, len(decisions))
	}

	var previewDropped bool
	for _, evt := range sink.ByType(string(event.EventTypeToolCallStart)) {
		truncated := testharness.GetBool(evt, "call.preview.truncated")
		argsPreview := testharness.GetString(evt, "call.preview.args_preview")
		if truncated && argsPreview == "" {
			previewDropped = true
			break
		}
	}
	if !previewDropped {
		for _, evt := range sink.ByType(string(event.EventTypeToolCallEnd)) {
			truncated := testharness.GetBool(evt, "preview.truncated")
			resultPreview := testharness.GetString(evt, "preview.result_preview")
			if truncated && resultPreview == "" {
				previewDropped = true
				break
			}
		}
	}
	if !previewDropped {
		t.Error("LED-002 FAILED: expected preview fields to be dropped under backpressure")
	}
}

func TestLED002_DecisionsBlockWhenQueueFull(t *testing.T) {
	release := make(chan struct{})
	writer := newBlockingWriter(release)

	emitter := core.NewEmitterWithOptions(writer, core.EmitterOptions{
		BufferSize:           2,
		PreviewDropThreshold: 1,
	})
	emitter.Start()

	const totalDecisions = 4
	emitDone := make(chan struct{})
	go func() {
		defer close(emitDone)
		for i := 0; i < totalDecisions; i++ {
			callID := fmt.Sprintf("decision-%d", i)
			emitter.EmitSync(makeDecisionEvent(callID))
		}
	}()

	select {
	case <-emitDone:
		t.Fatal("LED-002 FAILED: decisions dropped instead of waiting for queue space")
	case <-time.After(200 * time.Millisecond):
	}

	close(release)

	select {
	case <-emitDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("LED-002 FAILED: decisions did not drain after release")
	}

	emitter.Close()

	sink := testharness.NewEventSink()
	if err := sink.Capture(bytes.NewReader(writer.Bytes())); err != nil {
		t.Fatalf("LED-002 FAILED: event capture error: %v", err)
	}
	if errors := sink.Errors(); len(errors) > 0 {
		t.Fatalf("LED-002 FAILED: event parse errors: %v", errors)
	}

	decisions := sink.ByType(string(event.EventTypeToolCallDecision))
	if len(decisions) != totalDecisions {
		t.Fatalf("LED-002 FAILED: expected %d decision events, got %d", totalDecisions, len(decisions))
	}
}

type blockingWriter struct {
	release <-chan struct{}
	mu      sync.Mutex
	buf     bytes.Buffer
}

func newBlockingWriter(release <-chan struct{}) *blockingWriter {
	return &blockingWriter{release: release}
}

func (w *blockingWriter) Write(p []byte) (int, error) {
	<-w.release
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *blockingWriter) Bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buf.Bytes()...)
}

func makeEnvelope(eventType event.EventType) event.Envelope {
	return event.Envelope{
		V:       core.InterfaceVersion,
		Type:    eventType,
		TS:      time.Now().UTC().Format(time.RFC3339Nano),
		RunID:   "run-led-002",
		AgentID: "agent-led-002",
		Client:  event.ClientCodex,
		Env:     event.EnvCI,
		Source: event.Source{
			HostID: "host-led-002",
			ProcID: "proc-led-002",
			ShimID: "shim-led-002",
		},
	}
}

func makeCallRef(callID string) event.CallRef {
	return event.CallRef{
		CallID:     callID,
		ServerName: "server-led-002",
		ToolName:   "tool-led-002",
		ArgsHash:   "hash-led-002",
	}
}

func makeStartEvent(callID string, seq int) event.ToolCallStartEvent {
	return event.ToolCallStartEvent{
		Envelope: makeEnvelope(event.EventTypeToolCallStart),
		Call: event.CallInfo{
			CallID:     callID,
			ServerName: "server-led-002",
			ToolName:   "tool-led-002",
			Transport:  "mcp_stdio",
			ArgsHash:   "hash-led-002",
			BytesIn:    42,
			Preview: event.Preview{
				Truncated:   false,
				ArgsPreview: "args-preview-" + callID,
			},
			Seq: seq,
		},
	}
}

func makeDecisionEvent(callID string) event.ToolCallDecisionEvent {
	return event.ToolCallDecisionEvent{
		Envelope: makeEnvelope(event.EventTypeToolCallDecision),
		Call:     makeCallRef(callID),
		Decision: event.Decision{
			Action:   event.DecisionAllow,
			Severity: event.SeverityInfo,
			Explain: event.DecisionExplain{
				Summary:    "allowed",
				ReasonCode: "ALLOW",
			},
			Policy: event.PolicyInfo{
				PolicyID:      "policy-led-002",
				PolicyVersion: "0.1.0",
				PolicyHash:    "hash-led-002",
			},
		},
	}
}

func makeEndEvent(callID string) event.ToolCallEndEvent {
	return event.ToolCallEndEvent{
		Envelope:  makeEnvelope(event.EventTypeToolCallEnd),
		Call:      makeCallRef(callID),
		Status:    event.CallStatusOK,
		LatencyMS: 5,
		BytesOut:  24,
		Preview: event.ResultPreview{
			Truncated:     false,
			ResultPreview: "result-preview-" + callID,
		},
	}
}

// =============================================================================
// IMP-001: Importer Backup + Restore Correctness
// Contract: After import, config can be restored to original state.
// Reference: Contract-Test-Checklist.md IMP-001
// =============================================================================

func TestIMP001_ImporterBackupRestoreCorrectness(t *testing.T) {
	clients := []importer.Client{importer.ClientClaude, importer.ClientCodex}
	for _, client := range clients {
		t.Run(string(client), func(t *testing.T) {
			tempDir := t.TempDir()
			configPath := filepath.Join(tempDir, "mcp.json")

			config := map[string]any{
				"mcpServers": map[string]any{
					"alpha": map[string]any{
						"command": "/usr/bin/alpha",
						"args":    []string{"--flag", "value"},
					},
					"beta": map[string]any{
						"command": "/usr/bin/beta",
						"args":    []string{"--opt"},
						"env": map[string]any{
							"FOO": "bar",
						},
					},
				},
				"other": map[string]any{
					"feature": true,
				},
			}

			original := writeTestConfig(t, configPath, config)

			result, err := importer.Import(importer.Options{
				Client:     client,
				ConfigPath: configPath,
				ShimPath:   shimPath,
			})
			if err != nil {
				t.Fatalf("IMP-001 FAILED: import error: %v", err)
			}

			backupBytes := readFile(t, result.BackupPath)
			if !bytes.Equal(backupBytes, original) {
				t.Fatalf("IMP-001 FAILED: backup does not match original config")
			}

			servers := readMCPServers(t, configPath)
			if _, ok := servers["alpha"]; !ok {
				t.Fatalf("IMP-001 FAILED: server name alpha missing after import")
			}
			if _, ok := servers["beta"]; !ok {
				t.Fatalf("IMP-001 FAILED: server name beta missing after import")
			}

			assertServerRewrite(t, servers["alpha"], "alpha", shimPath, "/usr/bin/alpha", []string{"--flag", "value"})
			assertServerRewrite(t, servers["beta"], "beta", shimPath, "/usr/bin/beta", []string{"--opt"})

			env, ok := servers["beta"]["env"].(map[string]any)
			if !ok || env["FOO"] != "bar" {
				t.Fatalf("IMP-001 FAILED: server env field not preserved")
			}

			if _, err := importer.Restore(importer.Options{
				Client:     client,
				ConfigPath: configPath,
			}); err != nil {
				t.Fatalf("IMP-001 FAILED: restore error: %v", err)
			}

			restored := readFile(t, configPath)
			if !bytes.Equal(restored, original) {
				t.Fatalf("IMP-001 FAILED: restored config does not match original")
			}

			editedConfig := map[string]any{
				"mcpServers": map[string]any{
					"alpha": map[string]any{
						"command": "/usr/bin/alpha",
						"args":    []string{"--flag", "value"},
					},
					"beta": map[string]any{
						"command": "/usr/bin/beta",
						"args":    []string{"--opt"},
						"env": map[string]any{
							"FOO": "baz",
						},
					},
					"gamma": map[string]any{
						"command": "/usr/bin/gamma",
						"args":    []string{"--new"},
					},
				},
				"other": map[string]any{
					"feature": false,
					"edition": "second",
				},
			}

			edited := writeTestConfig(t, configPath, editedConfig)

			result, err = importer.Import(importer.Options{
				Client:     client,
				ConfigPath: configPath,
				ShimPath:   shimPath,
			})
			if err != nil {
				t.Fatalf("IMP-001 FAILED: second import error: %v", err)
			}

			backupBytes = readFile(t, result.BackupPath)
			if !bytes.Equal(backupBytes, edited) {
				t.Fatalf("IMP-001 FAILED: backup not refreshed after second import")
			}

			if _, err := importer.Restore(importer.Options{
				Client:     client,
				ConfigPath: configPath,
			}); err != nil {
				t.Fatalf("IMP-001 FAILED: second restore error: %v", err)
			}

			restored = readFile(t, configPath)
			if !bytes.Equal(restored, edited) {
				t.Fatalf("IMP-001 FAILED: restored config does not match latest backup")
			}
		})
	}
}

// =============================================================================
// IMP-002: Time-to-First-Log < 5 Minutes Path
// Contract: From fresh install to first tool_call_start observed < 5 minutes.
// Reference: Contract-Test-Checklist.md IMP-002
// =============================================================================

func TestIMP002_TimeToFirstLogUnder5Minutes(t *testing.T) {
	skipIfNoShim(t)

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.json")
	fakeMCPPath := getFakeMCPPath()

	config := map[string]any{
		"mcpServers": map[string]any{
			"test": map[string]any{
				"command": fakeMCPPath,
				"args":    []string{"--tools=test_tool"},
			},
		},
	}

	start := time.Now()
	writeTestConfig(t, configPath, config)

	if _, err := importer.Import(importer.Options{
		Client:     importer.ClientClaude,
		ConfigPath: configPath,
		ShimPath:   shimPath,
	}); err != nil {
		t.Fatalf("IMP-002 FAILED: import error: %v", err)
	}

	servers := readMCPServers(t, configPath)
	server, ok := servers["test"]
	if !ok {
		t.Fatalf("IMP-002 FAILED: server name test missing after import")
	}

	command := getStringField(t, server, "command")
	args := getStringSlice(t, server["args"])

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = os.Environ()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("IMP-002 FAILED: stdin pipe error: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("IMP-002 FAILED: stdout pipe error: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("IMP-002 FAILED: stderr pipe error: %v", err)
	}

	sink := testharness.NewEventSink()
	go sink.Capture(stderr)

	if err := cmd.Start(); err != nil {
		t.Fatalf("IMP-002 FAILED: start shim error: %v", err)
	}

	defer func() {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Signal(os.Interrupt)
		}
		done := make(chan error, 1)
		go func() {
			done <- cmd.Wait()
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		}
	}()

	driver := testharness.NewAgentDriver(stdin, stdout)
	driver.StartResponseReader()

	if _, err := driver.Initialize(); err != nil {
		t.Fatalf("IMP-002 FAILED: initialize error: %v", err)
	}

	if _, err := driver.CallTool("test_tool", nil); err != nil {
		t.Fatalf("IMP-002 FAILED: tool call error: %v", err)
	}

	if _, err := waitForEventType(sink, "tool_call_start", 30*time.Second); err != nil {
		t.Fatalf("IMP-002 FAILED: %v", err)
	}

	elapsed := time.Since(start)
	if elapsed > 5*time.Minute {
		t.Fatalf("IMP-002 FAILED: time-to-first-log %s exceeds 5 minutes", elapsed)
	}
}

// =============================================================================
// ADAPT-001: Adapter Tool Call Start Consistency
// Contract: Same tool call via different adapters produces identical
//           tool_call_start content.
// Reference: Interface-Pack.md §7.4, Contract-Test-Checklist.md ADAPT-001
// =============================================================================

func TestADAPT001_AdapterToolCallStartConsistent(t *testing.T) {
	skipIfNoShim(t)

	toolName := "adapter_test"
	args := map[string]any{
		"alpha": "one",
		"beta": map[string]any{
			"z": 2,
			"a": 1,
		},
	}

	shimResult, mockResult := adapterResultsForCall(t, toolName, args, "")

	assertStartSnapshotComplete(t, "shim", shimResult.Start)
	assertStartSnapshotComplete(t, "mock", mockResult.Start)

	shimStart := normalizeStartSnapshot(t, shimResult.Start)
	mockStart := normalizeStartSnapshot(t, mockResult.Start)

	if !reflect.DeepEqual(shimStart, mockStart) {
		t.Fatalf("ADAPT-001 FAILED: tool_call_start mismatch across adapters\n  shim=%+v\n  mock=%+v",
			shimStart, mockStart)
	}
}

// =============================================================================
// ADAPT-002: Core is Protocol-Agnostic
// Contract: Same tool call via different adapters produces identical decisions
//           and events; args_hash matches.
// Reference: Interface-Pack.md §7.2, Contract-Test-Checklist.md ADAPT-002
// =============================================================================

func TestADAPT002_CoreIsProtocolAgnostic(t *testing.T) {
	skipIfNoShim(t)

	toolName := "adapter_test"
	args := map[string]any{
		"z": "last",
		"a": 1,
		"nested": map[string]any{
			"b": true,
			"a": false,
		},
	}

	shimResult, mockResult := adapterResultsForCall(t, toolName, args, "")

	if shimResult.Start.ArgsHash == "" || mockResult.Start.ArgsHash == "" {
		t.Fatal("ADAPT-002 FAILED: args_hash missing from tool_call_start")
	}
	if shimResult.Start.ArgsHash != mockResult.Start.ArgsHash {
		t.Fatalf("ADAPT-002 FAILED: args_hash mismatch across adapters\n  shim=%s\n  mock=%s",
			shimResult.Start.ArgsHash, mockResult.Start.ArgsHash)
	}
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

	policyJSON := `{
		"mode": "guardrails",
		"policy_id": "test-adapt-003",
		"policy_version": "1.0.0",
		"rules": [
			{
				"rule_id": "deny-blocked-tool",
				"kind": "deny",
				"match": {"tool_name": {"glob": ["blocked_tool"]}},
				"effect": {"action": "BLOCK", "reason_code": "TEST_BLOCK", "message": "Blocked for ADAPT-003 test"}
			}
		]
	}`

	toolName := "blocked_tool"

	shimResult, mockResult := adapterResultsForCall(t, toolName, nil, policyJSON)

	cases := []struct {
		name string
		resp *testharness.JSONRPCResponse
	}{
		{name: "shim", resp: shimResult.Response},
		{name: "mock", resp: mockResult.Response},
	}

	for _, tc := range cases {
		if tc.resp == nil {
			t.Fatalf("ADAPT-003 FAILED: %s adapter did not return a response", tc.name)
		}

		wrapped := testharness.WrapResponse(tc.resp)
		if wrapped.IsSuccess() {
			t.Fatalf("ADAPT-003 FAILED: %s adapter should have been blocked by policy", tc.name)
		}

		if tc.resp.Error == nil {
			t.Fatalf("ADAPT-003 FAILED: %s adapter response missing error field", tc.name)
		}

		subluminalCodes := map[int]bool{
			-32081: true, // POLICY_BLOCKED
			-32082: true, // POLICY_THROTTLED
			-32083: true, // REJECT_WITH_HINT
			-32084: true, // RUN_TERMINATED
		}
		if !subluminalCodes[tc.resp.Error.Code] {
			t.Errorf("ADAPT-003 FAILED: %s adapter error code %d is not a Subluminal policy code",
				tc.name, tc.resp.Error.Code)
		}

		if subluminalCodes[tc.resp.Error.Code] && !hasSubluminalData(tc.resp.Error.Data) {
			t.Errorf("ADAPT-003 FAILED: %s adapter policy error missing error.data.subluminal", tc.name)
		}
	}
}

type toolCallStartSnapshot struct {
	ServerName       string
	ToolName         string
	Transport        string
	ArgsHash         string
	BytesIn          int
	Seq              int
	PreviewTruncated bool
	PreviewArgs      string
}

type adapterCallResult struct {
	Start    toolCallStartSnapshot
	Response *testharness.JSONRPCResponse
}

func adapterResultsForCall(t *testing.T, toolName string, args map[string]any, policyJSON string) (adapterCallResult, adapterCallResult) {
	t.Helper()

	rawRequest := buildToolsCallRaw(t, 2, toolName, args)
	shimResult := callShimAdapter(t, toolName, args, policyJSON)
	mockResult := callMockAdapter(t, toolName, args, rawRequest, int64(2), policyJSON)

	return shimResult, mockResult
}

func callShimAdapter(t *testing.T, toolName string, args map[string]any, policyJSON string) adapterCallResult {
	t.Helper()

	env := []string{"SUB_POLICY_JSON=" + policyJSON}

	h := testharness.NewTestHarness(testharness.HarnessConfig{
		ShimPath: shimPath,
		ShimEnv:  env,
	})
	h.AddTool(toolName, "Adapter contract tool", nil)

	if err := h.Start(); err != nil {
		t.Fatalf("Failed to start harness: %v", err)
	}
	defer h.Stop()

	if err := h.Initialize(); err != nil {
		t.Fatalf("Failed to initialize harness: %v", err)
	}

	resp, err := h.CallTool(toolName, args)
	if err != nil {
		t.Fatalf("Failed to call tool: %v", err)
	}

	if !h.EventSink.WaitForTypeCount(string(event.EventTypeToolCallStart), 1, 5*time.Second) {
		t.Fatalf("ADAPT: tool_call_start event not captured in time")
	}

	if err := h.Stop(); err != nil {
		t.Fatalf("Failed to stop harness: %v", err)
	}

	toolCallStarts := h.EventSink.ByType(string(event.EventTypeToolCallStart))
	if len(toolCallStarts) == 0 {
		t.Fatal("ADAPT: No tool_call_start events")
	}

	return adapterCallResult{
		Start:    snapshotFromCapturedStart(t, toolCallStarts[0]),
		Response: resp,
	}
}

func callMockAdapter(t *testing.T, toolName string, args map[string]any, rawRequest []byte, requestID any, policyJSON string) adapterCallResult {
	t.Helper()

	adapter := testharness.NewMockAdapter(testharness.MockAdapterConfig{
		ServerName: "test",
		Transport:  "mcp_stdio",
		Identity: core.Identity{
			RunID:   "mock-run",
			AgentID: "mock-agent",
			Client:  event.ClientClaude,
			Env:     event.EnvCI,
		},
		Source: core.Source{
			HostID: "mock-host",
			ProcID: "mock-proc",
			ShimID: "mock-shim",
		},
		Policy: policyBundleFromJSON(t, policyJSON),
	})

	result, err := adapter.HandleToolCall(testharness.MockToolCall{
		ToolName:  toolName,
		Args:      args,
		BytesIn:   len(rawRequest),
		RequestID: requestID,
	})
	if err != nil {
		t.Fatalf("Failed to handle mock tool call: %v", err)
	}

	return adapterCallResult{
		Start:    snapshotFromStartEvent(result.StartEvent),
		Response: result.Response,
	}
}

func snapshotFromCapturedStart(t *testing.T, evt testharness.CapturedEvent) toolCallStartSnapshot {
	t.Helper()

	return toolCallStartSnapshot{
		ServerName:       testharness.GetString(evt, "call.server_name"),
		ToolName:         testharness.GetString(evt, "call.tool_name"),
		Transport:        testharness.GetString(evt, "call.transport"),
		ArgsHash:         testharness.GetString(evt, "call.args_hash"),
		BytesIn:          testharness.GetInt(evt, "call.bytes_in"),
		Seq:              testharness.GetInt(evt, "call.seq"),
		PreviewTruncated: testharness.GetBool(evt, "call.preview.truncated"),
		PreviewArgs:      testharness.GetString(evt, "call.preview.args_preview"),
	}
}

func snapshotFromStartEvent(evt event.ToolCallStartEvent) toolCallStartSnapshot {
	return toolCallStartSnapshot{
		ServerName:       evt.Call.ServerName,
		ToolName:         evt.Call.ToolName,
		Transport:        evt.Call.Transport,
		ArgsHash:         evt.Call.ArgsHash,
		BytesIn:          evt.Call.BytesIn,
		Seq:              evt.Call.Seq,
		PreviewTruncated: evt.Call.Preview.Truncated,
		PreviewArgs:      evt.Call.Preview.ArgsPreview,
	}
}

func normalizeStartSnapshot(t *testing.T, start toolCallStartSnapshot) toolCallStartSnapshot {
	t.Helper()

	if start.PreviewTruncated || start.PreviewArgs == "" {
		start.PreviewArgs = ""
		return start
	}

	var payload any
	if err := json.Unmarshal([]byte(start.PreviewArgs), &payload); err != nil {
		t.Fatalf("ADAPT-001 FAILED: preview args is not valid JSON: %v", err)
	}

	canonicalBytes, err := canonical.Canonicalize(payload)
	if err != nil {
		t.Fatalf("ADAPT-001 FAILED: canonicalize preview args: %v", err)
	}

	start.PreviewArgs = string(canonicalBytes)
	return start
}

func buildToolsCallRaw(t *testing.T, id int64, toolName string, args map[string]any) []byte {
	t.Helper()

	if args == nil {
		args = map[string]any{}
	}

	params := map[string]any{
		"name":      toolName,
		"arguments": args,
	}
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Failed to marshal params: %v", err)
	}

	req := testharness.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "tools/call",
		Params:  paramsBytes,
	}

	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal JSON-RPC request: %v", err)
	}

	return raw
}

func policyBundleFromJSON(t *testing.T, policyJSON string) policy.Bundle {
	t.Helper()

	if strings.TrimSpace(policyJSON) == "" {
		return policy.DefaultBundle()
	}

	spec, err := policy.ParseBundle([]byte(policyJSON))
	if err != nil {
		t.Fatalf("Failed to parse policy bundle: %v", err)
	}

	compiled, err := policy.CompileBundle(spec)
	if err != nil {
		t.Fatalf("Failed to compile policy bundle: %v", err)
	}

	return compiled.Bundle
}

func assertStartSnapshotComplete(t *testing.T, name string, start toolCallStartSnapshot) {
	t.Helper()

	if start.ServerName == "" {
		t.Fatalf("ADAPT-001 FAILED: %s adapter missing server_name", name)
	}
	if start.ToolName == "" {
		t.Fatalf("ADAPT-001 FAILED: %s adapter missing tool_name", name)
	}
	if start.ArgsHash == "" {
		t.Fatalf("ADAPT-001 FAILED: %s adapter missing args_hash", name)
	}
	if start.Transport == "" {
		t.Fatalf("ADAPT-001 FAILED: %s adapter missing transport", name)
	}
	if start.BytesIn <= 0 {
		t.Fatalf("ADAPT-001 FAILED: %s adapter bytes_in=%d, expected >0", name, start.BytesIn)
	}
	if start.Seq <= 0 {
		t.Fatalf("ADAPT-001 FAILED: %s adapter seq=%d, expected >0", name, start.Seq)
	}

	validTransports := map[string]bool{
		"mcp_stdio": true, "mcp_http": true, "http": true, "messages_api": true, "unknown": true,
	}
	if !validTransports[start.Transport] {
		t.Fatalf("ADAPT-001 FAILED: %s adapter unknown transport %q", name, start.Transport)
	}
}

func hasSubluminalData(data any) bool {
	if data == nil {
		return false
	}

	payload, ok := data.(map[string]any)
	if !ok {
		return false
	}
	_, exists := payload["subluminal"]
	return exists
}

func writeTestConfig(t *testing.T, path string, config map[string]any) []byte {
	t.Helper()
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}
	return data
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read %s: %v", path, err)
	}
	return data
}

func readMCPServers(t *testing.T, path string) map[string]map[string]any {
	t.Helper()
	raw := readFile(t, path)

	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("Failed to parse config JSON: %v", err)
	}

	rawServers, ok := root["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("Config missing mcpServers object")
	}

	servers := make(map[string]map[string]any)
	for name, rawServer := range rawServers {
		server, ok := rawServer.(map[string]any)
		if !ok {
			t.Fatalf("Server %s is not an object", name)
		}
		servers[name] = server
	}
	return servers
}

func getStringField(t *testing.T, server map[string]any, field string) string {
	t.Helper()
	value, ok := server[field].(string)
	if !ok {
		t.Fatalf("Server field %s missing or not a string", field)
	}
	return value
}

func getStringSlice(t *testing.T, raw any) []string {
	t.Helper()

	switch values := raw.(type) {
	case []string:
		return append([]string{}, values...)
	case []any:
		out := make([]string, 0, len(values))
		for i, item := range values {
			text, ok := item.(string)
			if !ok {
				t.Fatalf("Args[%d] is not a string", i)
			}
			out = append(out, text)
		}
		return out
	default:
		t.Fatalf("Args field not a string slice")
		return nil
	}
}

func assertServerRewrite(t *testing.T, server map[string]any, name, shimPath, upstream string, upstreamArgs []string) {
	t.Helper()
	command := getStringField(t, server, "command")
	if command != shimPath {
		t.Fatalf("Server %s command=%q, expected %q", name, command, shimPath)
	}

	args := getStringSlice(t, server["args"])
	expected := append([]string{"--server-name=" + name, "--", upstream}, upstreamArgs...)
	if !stringSlicesEqual(args, expected) {
		t.Fatalf("Server %s args=%v, expected %v", name, args, expected)
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func waitForEventType(sink *testharness.EventSink, eventType string, timeout time.Duration) (*testharness.CapturedEvent, error) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	deadline := time.After(timeout)
	for {
		if evt := sink.FirstOfType(eventType); evt != nil {
			return evt, nil
		}
		select {
		case <-deadline:
			return nil, fmt.Errorf("timeout waiting for %s event", eventType)
		case <-ticker.C:
		}
	}
}

func getFakeMCPPath() string {
	if p := os.Getenv("SUBLUMINAL_FAKEMCP_PATH"); p != "" {
		return p
	}
	return filepath.Join(findRepoRoot(), "bin", "fakemcp")
}

func buildLedgerEvents(t *testing.T, runCount, callsPerRun int) []byte {
	t.Helper()

	var buf bytes.Buffer
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	seq := 0
	nextTS := func() string {
		ts := start.Add(time.Duration(seq) * time.Second).Format(time.RFC3339Nano)
		seq++
		return ts
	}
	source := event.Source{
		HostID: "host-1",
		ProcID: "proc-1",
		ShimID: "shim-1",
	}

	for runIndex := 0; runIndex < runCount; runIndex++ {
		runID := fmt.Sprintf("run-%d", runIndex+1)
		policyID := fmt.Sprintf("policy-%d", runIndex+1)
		policyHash := fmt.Sprintf("hash-%d", runIndex+1)
		ts := nextTS()
		runStart := event.RunStartEvent{
			Envelope: event.Envelope{
				V:       "0.1.0",
				Type:    event.EventTypeRunStart,
				TS:      ts,
				RunID:   runID,
				AgentID: "agent-1",
				Client:  event.ClientClaude,
				Env:     event.EnvCI,
				Source:  source,
			},
			Run: event.RunInfo{
				StartedAt: ts,
				Mode:      event.RunModeObserve,
				Policy: event.PolicyInfo{
					PolicyID:      policyID,
					PolicyVersion: "1",
					PolicyHash:    policyHash,
				},
			},
		}
		appendEvent(t, &buf, runStart)

		for callIndex := 0; callIndex < callsPerRun; callIndex++ {
			callID := fmt.Sprintf("call-%d-%d", runIndex+1, callIndex+1)
			argsHash := fmt.Sprintf("hash-%d-%d", runIndex+1, callIndex+1)

			ts = nextTS()
			startEvent := event.ToolCallStartEvent{
				Envelope: event.Envelope{
					V:       "0.1.0",
					Type:    event.EventTypeToolCallStart,
					TS:      ts,
					RunID:   runID,
					AgentID: "agent-1",
					Client:  event.ClientClaude,
					Env:     event.EnvCI,
					Source:  source,
				},
				Call: event.CallInfo{
					CallID:     callID,
					ServerName: "server",
					ToolName:   "tool",
					Transport:  "mcp_stdio",
					ArgsHash:   argsHash,
					BytesIn:    128,
					Preview: event.Preview{
						Truncated:   false,
						ArgsPreview: fmt.Sprintf("args-preview-%s", callID),
					},
					Seq: callIndex + 1,
				},
			}
			appendEvent(t, &buf, startEvent)

			ts = nextTS()
			decisionEvent := event.ToolCallDecisionEvent{
				Envelope: event.Envelope{
					V:       "0.1.0",
					Type:    event.EventTypeToolCallDecision,
					TS:      ts,
					RunID:   runID,
					AgentID: "agent-1",
					Client:  event.ClientClaude,
					Env:     event.EnvCI,
					Source:  source,
				},
				Call: event.CallRef{
					CallID:     callID,
					ServerName: "server",
					ToolName:   "tool",
					ArgsHash:   argsHash,
				},
				Decision: event.Decision{
					Action:   event.DecisionAllow,
					RuleID:   nil,
					Severity: event.SeverityInfo,
					Explain: event.DecisionExplain{
						Summary:    "allowed",
						ReasonCode: "ALLOW",
					},
					Policy: event.PolicyInfo{
						PolicyID:      policyID,
						PolicyVersion: "1",
						PolicyHash:    policyHash,
					},
					Hint: &event.Hint{
						HintText: "hint for " + callID,
						SuggestedArgs: map[string]any{
							"call_id": callID,
						},
						HintKind: event.HintKindArgFix,
					},
				},
			}
			appendEvent(t, &buf, decisionEvent)

			ts = nextTS()
			endEvent := event.ToolCallEndEvent{
				Envelope: event.Envelope{
					V:       "0.1.0",
					Type:    event.EventTypeToolCallEnd,
					TS:      ts,
					RunID:   runID,
					AgentID: "agent-1",
					Client:  event.ClientClaude,
					Env:     event.EnvCI,
					Source:  source,
				},
				Call: event.CallRef{
					CallID:     callID,
					ServerName: "server",
					ToolName:   "tool",
					ArgsHash:   argsHash,
				},
				Status:    event.CallStatusOK,
				LatencyMS: 4,
				BytesOut:  256,
				Preview: event.ResultPreview{
					Truncated:     false,
					ResultPreview: fmt.Sprintf("result-preview-%s", callID),
				},
			}
			appendEvent(t, &buf, endEvent)
		}

		ts = nextTS()
		runEnd := event.RunEndEvent{
			Envelope: event.Envelope{
				V:       "0.1.0",
				Type:    event.EventTypeRunEnd,
				TS:      ts,
				RunID:   runID,
				AgentID: "agent-1",
				Client:  event.ClientClaude,
				Env:     event.EnvCI,
				Source:  source,
			},
			Run: event.RunEndInfo{
				EndedAt: ts,
				Status:  event.RunStatusSucceeded,
				Summary: event.RunSummary{
					CallsTotal:   callsPerRun,
					CallsAllowed: callsPerRun,
					DurationMS:   callsPerRun * 10,
				},
			},
		}
		appendEvent(t, &buf, runEnd)
	}

	return buf.Bytes()
}

func appendEvent(t *testing.T, buf *bytes.Buffer, evt any) {
	t.Helper()
	data, err := event.SerializeEvent(evt)
	if err != nil {
		t.Fatalf("LED-001 FAILED: serialize error: %v", err)
	}
	if _, err := buf.Write(data); err != nil {
		t.Fatalf("LED-001 FAILED: buffer write error: %v", err)
	}
}

func sqliteQuery(t *testing.T, dbPath, query string) string {
	t.Helper()
	cmd := exec.Command("sqlite3", "-batch", dbPath, query)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("LED-001 FAILED: sqlite3 error: %v (%s)", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output))
}
