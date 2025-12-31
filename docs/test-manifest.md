# Test Manifest

This document captures the COMPLETE test suite for Subluminal - every test that must exist for agentic coding confidence.

## Test Layers

```
┌─────────────────────────────────────────────────────────────────┐
│                 LAYER 4: Integration Tests                       │
│    (Cross-feature: Policy+Shim, Importer+Shim, E2E pipeline)    │
│                  19 tests - NOT BUILT YET                        │
└─────────────────────────────────────────────────────────────────┘
                              ↑
┌─────────────────────────────────────────────────────────────────┐
│                    LAYER 3: Component Tests                      │
│         (Ledger, Importer - require full system)                │
│                     4 tests - NOT BUILT YET                      │
└─────────────────────────────────────────────────────────────────┘
                              ↑
┌─────────────────────────────────────────────────────────────────┐
│                 LAYER 2: Contract Tests                          │
│      (Full shim integration - Agent → Shim → Server)            │
│              40 tests implemented, 18 skipped                    │
└─────────────────────────────────────────────────────────────────┘
                              ↑
┌─────────────────────────────────────────────────────────────────┐
│                    LAYER 1: Unit Tests                           │
│         (Fast, isolated - no shim binary needed)                │
│                      3 test files                                │
└─────────────────────────────────────────────────────────────────┘
```

## Quick Status

```bash
# Run all unit tests (fast, no dependencies)
go test ./pkg/...

# Run contract tests (requires ./bin/shim)
go test ./test/contract/...

# Full status report
go run ./cmd/teststatus
```

---

## Layer 1: Unit Tests

These run fast, need no external binaries, and validate core algorithms.

### pkg/canonical/canonical_test.go

| Test | Description | Status |
|------|-------------|--------|
| TestCanonicalize_KeyOrdering | Object keys sorted lexicographically | ✓ |
| TestCanonicalize_NestedObjects | Nested object key ordering | ✓ |
| TestCanonicalize_Arrays | Array order preserved | ✓ |
| TestCanonicalize_Numbers | Minimal decimal notation | ✓ |
| TestCanonicalize_Strings | Proper JSON escaping | ✓ |
| TestCanonicalize_UTF8 | Unicode handling | ✓ |
| TestCanonicalize_NoWhitespace | No extraneous whitespace | ✓ |
| TestArgsHash_Equivalence | Same hash for reordered keys | ✓ |
| TestArgsHash_GoldenValue | Matches precomputed golden hash | ✓ |

**Golden Value:**
```
Input:  {"branch":"main","command":"git push","force":false}
Hash:   43258cff783fe7036d8a43033f830adfc60ec037382473548ac742b888292777
```

### pkg/event/serialize_test.go

| Test | Description | Status |
|------|-------------|--------|
| TestSerialize_SingleLine | Each event is exactly one JSON line | ✓ |
| TestSerialize_RequiredFields | All envelope fields present | ✓ |
| TestSerialize_RunID | run_id present in all events | ✓ |
| TestSerialize_CallID | call_id unique and monotonic | ✓ |
| TestSerialize_Timestamp | RFC3339 format | ✓ |

### pkg/testharness/harness_test.go

| Test | Description | Status |
|------|-------------|--------|
| TestHarness_DirectMode | Harness works without shim | ✓ |
| TestHarness_ToolRegistration | Can register and call tools | ✓ |
| TestEventSink_Parsing | EventSink parses JSONL correctly | ✓ |
| TestAgentDriver_RoundTrip | Request/response works | ✓ |

---

## Layer 2: Contract Tests

These validate the shim against Interface-Pack.md. Each test maps to a spec section.

### Events (EVT) - 9 tests

| ID | Priority | Spec | Description | Status |
|----|----------|------|-------------|--------|
| EVT-001 | P0 | §1.1 | JSONL single-line events | Implemented |
| EVT-002 | P0 | §1.3 | Required envelope fields | Implemented |
| EVT-003 | P0 | §1.2 | Event ordering & completeness | Implemented |
| EVT-004 | P0 | §1.3 | run_id present everywhere | Implemented |
| EVT-005 | P0 | §0.3 | call_id uniqueness per run | Implemented |
| EVT-006 | P0 | - | tool/server name preservation | Implemented |
| EVT-007 | P0 | §1.7 | latency_ms present and sane | Implemented |
| EVT-008 | P0 | §1.7 | status/error class taxonomy | Implemented |
| EVT-009 | P0 | §1.8 | run_end summary counts correct | Implemented |

### Hashing (HASH) - 2 tests

| ID | Priority | Spec | Description | Status |
|----|----------|------|-------------|--------|
| HASH-001 | P0 | §1.9.1 | Canonicalization equivalence | Implemented |
| HASH-002 | P0 | §1.9.1 | Canonicalization stability (golden) | Implemented |

### Buffering (BUF) - 4 tests

| ID | Priority | Spec | Description | Status |
|----|----------|------|-------------|--------|
| BUF-001 | P0 | §1.10 | Bounded inspection: truncate | Implemented |
| BUF-002 | P0 | §1.10 | No OOM on large payload | Implemented |
| BUF-003 | P0 | §1.10 | Forwarding correctness under truncation | Implemented |
| BUF-004 | P1 | §1.9.2 | Rolling hash for truncated payload | Implemented |

### Policy (POL) - 7 tests

| ID | Priority | Spec | Description | Status |
|----|----------|------|-------------|--------|
| POL-001 | P0 | §2.1 | Observe mode: never blocks | Implemented |
| POL-002 | P0 | §2.3 | Allow/Deny ordering | Implemented |
| POL-003 | P0 | §2.5 | Budget rule decrements & blocks | Implemented |
| POL-004 | P0 | §2.5 | Token bucket rate limit (THROTTLE) | Implemented |
| POL-005 | P0 | §2.5 | Breaker: repeat_threshold triggers | Implemented |
| POL-006 | P0 | §2.5 | Dedupe window blocks duplicate | Implemented |
| POL-007 | P1 | §2.5 | Tag rule applies risk_class | Implemented |

### Errors (ERR) - 4 tests

| ID | Priority | Spec | Description | Status |
|----|----------|------|-------------|--------|
| ERR-001 | P0 | §3.2.1 | BLOCK uses JSON-RPC error -32081 | Implemented |
| ERR-002 | P0 | §3.2.2 | THROTTLE uses error -32082 + backoff_ms | Implemented |
| ERR-003 | P0 | §3.2.4 | REJECT_WITH_HINT uses -32083 | Implemented |
| ERR-004 | P0 | §4 | No secret leakage in errors | Implemented |

### Secrets (SEC) - 2 tests

| ID | Priority | Spec | Description | Status |
|----|----------|------|-------------|--------|
| SEC-001 | P0 | §4 | Agent never sees injected secrets | Implemented |
| SEC-002 | P1 | §4 | secret_injection event metadata only | Implemented |

### Process (PROC) - 3 tests

| ID | Priority | Spec | Description | Status |
|----|----------|------|-------------|--------|
| PROC-001 | P0 | - | SIGINT propagates; no zombie | Implemented |
| PROC-002 | P0 | - | EOF on stdin terminates cleanly | Implemented |
| PROC-003 | P1 | - | Upstream crash handled gracefully | Implemented |

### Identity (ID) - 2 tests

| ID | Priority | Spec | Description | Status |
|----|----------|------|-------------|--------|
| ID-001 | P0 | §5 | Identity env vars applied | Implemented |
| ID-002 | P1 | §1.3.1 | Workload context tolerance | Implemented |

### Ledger (LED) - 2 tests

| ID | Priority | Spec | Description | Status |
|----|----------|------|-------------|--------|
| LED-001 | P0 | - | Ledger ingestion durability | **SKIPPED** - needs ledger |
| LED-002 | P0 | - | Backpressure drops previews not decisions | **SKIPPED** - needs ledger |

### Importer (IMP) - 2 tests

| ID | Priority | Spec | Description | Status |
|----|----------|------|-------------|--------|
| IMP-001 | P0 | - | Backup + restore correctness | **SKIPPED** - needs importer |
| IMP-002 | P0 | - | Time-to-first-log < 5 minutes | **SKIPPED** - needs importer |

### Adapter (ADAPT) - 3 tests

| ID | Priority | Spec | Description | Status |
|----|----------|------|-------------|--------|
| ADAPT-001 | P0 | §7.1 | Adapter provides required fields | Implemented |
| ADAPT-002 | P0 | §7.2 | Core is protocol-agnostic | **SKIPPED** - needs 2+ adapters |
| ADAPT-003 | P1 | §7.2 | Adapter formats errors correctly | Implemented |

---

## Layer 3: Component Tests (NOT YET BUILT)

These require full system components that don't exist yet.

| Component | Tests | Blocked By |
|-----------|-------|------------|
| Ledger | LED-001, LED-002 | `ledgerd` binary not built |
| Importer | IMP-001, IMP-002 | `sub import` command not built |
| Multi-Adapter | ADAPT-002 | Only MCP stdio adapter exists |

---

## Layer 4: Integration Tests (NOT YET BUILT)

These test cross-feature interactions. Each validates that two or more components work correctly together.

### Policy + Shim Integration (POL-INT) - 3 tests

| ID | Priority | Description | Blocked By |
|----|----------|-------------|------------|
| POL-INT-001 | P0 | Policy BLOCK decision produces correct JSON-RPC error AND tool_call_decision event | Policy engine |
| POL-INT-002 | P0 | Policy THROTTLE returns backoff_ms in error AND events reflect throttle count | Policy engine |
| POL-INT-003 | P0 | Policy REJECT_WITH_HINT returns hint object AND hint_issued event emitted | Policy engine |

**What these catch:** Shim might apply policy correctly but emit wrong events, or vice versa.

### Policy State Interactions (POL-STATE) - 4 tests

| ID | Priority | Description | Blocked By |
|----|----------|-------------|------------|
| POL-STATE-001 | P0 | Budget exhaustion + breaker: both accumulate independently, first to trigger wins | Policy engine |
| POL-STATE-002 | P0 | Rate limit + budget: rate limit delays, budget counts actual calls (not attempts) | Policy engine |
| POL-STATE-003 | P1 | Dedupe + budget: dedupe block doesn't decrement budget counter | Policy engine |
| POL-STATE-004 | P1 | Multiple rules match same call: evaluation order is deterministic | Policy engine |

**What these catch:** State corruption when multiple policy rules fire on the same call.

### Importer + Shim Integration (IMP-INT) - 3 tests

| ID | Priority | Description | Blocked By |
|----|----------|-------------|------------|
| IMP-INT-001 | P0 | After `sub import claude`, MCP server entries route through shim correctly | Importer |
| IMP-INT-002 | P0 | Imported config preserves all original server names in events | Importer |
| IMP-INT-003 | P0 | `sub restore` returns to original config; shim no longer in path | Importer |

**What these catch:** Config rewrite might look correct but shim doesn't actually intercept.

### Ledger + Events Integration (LED-INT) - 3 tests

| ID | Priority | Description | Blocked By |
|----|----------|-------------|------------|
| LED-INT-001 | P0 | Events emitted by shim are queryable in ledger with correct fields | Ledger |
| LED-INT-002 | P0 | Policy decisions (BLOCK/THROTTLE/HINT) stored with rule_id and reason_code | Ledger + Policy |
| LED-INT-003 | P1 | Large payload events stored with truncated previews, full metadata intact | Ledger |

**What these catch:** Event schema mismatch between producer (shim) and consumer (ledger).

### End-to-End Pipeline (E2E) - 4 tests

| ID | Priority | Description | Blocked By |
|----|----------|-------------|------------|
| E2E-001 | P0 | Full path: import → tool call → event → ledger → query returns correct data | All components |
| E2E-002 | P0 | Full path with policy block: import → blocked call → error to agent → decision in ledger | All components |
| E2E-003 | P0 | `sub tail` shows live events during active tool calls | CLI + Ledger |
| E2E-004 | P1 | Multiple concurrent runs maintain isolated state and separate run_ids in ledger | All components |

**What these catch:** Components work in isolation but fail when connected.

### Secret Flow Integration (SEC-INT) - 2 tests

| ID | Priority | Description | Blocked By |
|----|----------|-------------|------------|
| SEC-INT-001 | P0 | Secret injected at shim → used by upstream → NOT in events → NOT in ledger | Ledger |
| SEC-INT-002 | P0 | Secret in policy error hint text is redacted before reaching agent AND ledger | Policy + Ledger |

**What these catch:** Secret leakage at integration boundaries (not just within one component).

---

### Integration Test Summary

| Category | Total | Priority P0 | Priority P1 |
|----------|-------|-------------|-------------|
| POL-INT | 3 | 3 | 0 |
| POL-STATE | 4 | 2 | 2 |
| IMP-INT | 3 | 3 | 0 |
| LED-INT | 3 | 2 | 1 |
| E2E | 4 | 3 | 1 |
| SEC-INT | 2 | 2 | 0 |
| **TOTAL** | **19** | **15** | **4** |

---

### When to Build Integration Tests

Integration tests should be built AFTER the component they depend on:

| Phase | Components Ready | Integration Tests Unblocked |
|-------|------------------|----------------------------|
| After Policy Engine | Shim + Policy | POL-INT-*, POL-STATE-* |
| After Importer | Shim + Importer | IMP-INT-* |
| After Ledger | Shim + Ledger | LED-INT-* |
| After All | All components | E2E-*, SEC-INT-* |

---

## Summary Counts

| Category | Total | Implemented | Passing | Blocked |
|----------|-------|-------------|---------|---------|
| Unit Tests (Layer 1) | 35 | 35 | 35 | 0 |
| Contract Tests P0 (Layer 2) | 35 | 35 | 20 | 4 |
| Contract Tests P1 (Layer 2) | 5 | 5 | 2 | 0 |
| Component Tests (Layer 3) | 4 | 0 | 0 | 4 |
| Integration Tests P0 (Layer 4) | 15 | 0 | 0 | 15 |
| Integration Tests P1 (Layer 4) | 4 | 0 | 0 | 4 |
| **TOTAL** | **98** | **75** | **57** | **27** |

**Current status (as of reset):**
- Unit tests: All passing
- Contract tests: 22/40 passing (18 skipped - need policy engine, importer, ledger)
- Component tests: Not implemented (blocked by missing components)
- Integration tests: Not implemented (blocked by missing components)

---

## Running Tests

### Full Suite
```bash
# Layer 1: Unit tests (always run, fast)
go test -v ./pkg/...

# Layer 2: Contract tests (skips if no shim)
go test -v ./test/contract/...

# Status report
go run ./cmd/teststatus
```

### CI Gate (P0 only)
```bash
# Must pass for v0.1 release
go test ./pkg/...
go test ./test/contract/... -run 'Test(EVT|HASH|BUF|POL|ERR|SEC|PROC|ID|ADAPT)0'

# Layer 4: Integration tests (when implemented)
go test ./test/integration/...
```

### Quick Smoke Test
```bash
go test ./pkg/canonical/... -run TestArgsHash_GoldenValue
```

---

## For Agentic Coding

### Before Starting Work
```bash
go run ./cmd/teststatus -no-run  # What's implemented?
go test ./pkg/...                 # Unit tests pass?
```

### After Agent Completes Task
```bash
go test ./pkg/...                # Unit tests still pass?
go run ./cmd/teststatus          # Full integration status
```

### Agent Confidence Checklist
- [ ] Unit tests in pkg/* pass
- [ ] Contract test functions exist for my changes
- [ ] Golden values match (if touching canonicalization)
- [ ] No new skips introduced
- [ ] teststatus shows progress (not regression)

---

## What Needs To Be Built

### To Unblock Contract Tests (Layer 2)
1. ~~Build the shim binary (`./bin/shim`)~~ ✓ DONE
2. Build policy engine → unblocks POL-002 through POL-007, ERR-001 through ERR-004, ADAPT-003
3. Build importer → unblocks IMP-001, IMP-002
4. Build ledger → unblocks LED-001, LED-002

### To Unblock Component Tests (Layer 3)
1. Build `ledgerd` → unblocks LED-001, LED-002
2. Build `sub import` → unblocks IMP-001, IMP-002
3. Build second adapter (HTTP?) → unblocks ADAPT-002

### To Unblock Integration Tests (Layer 4)
1. Build policy engine → unblocks POL-INT-*, POL-STATE-*
2. Build importer → unblocks IMP-INT-*
3. Build ledger → unblocks LED-INT-*
4. Build all components → unblocks E2E-*, SEC-INT-*

### To Complete the Suite
1. All P0 tests passing across all layers
2. Golden value fixtures in testdata/fixtures/
3. Integration test file: test/integration/*_test.go
4. Mutation testing framework (future)
