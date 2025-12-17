# Plan: Trust-Building TDD Workflow with HASH-001/002

## Meta-Goal: Learning to Operate Coding Agents
This exercise is about **you learning to trust and operate coding agents effectively**. The objective is to build 100% confidence in agent-built code through test-driven verification—the same confidence you'd have if you wrote every line yourself.

By the end of this exercise, you will be able to:
- Direct agents to build features using contract tests as acceptance criteria
- Trust "tests pass = it works" at a gut level
- Scale to parallel agent workflows with confidence

---

## Immediate Goal
Build gut-level confidence that "tests pass = it works" by watching tests catch real bugs, not just pass.

## Source of Truth (Spec Files)
All implementation must conform to these existing specifications:
- `Subliminal-Design.md` - Core architecture and design
- `Interface-Pack.md` - Contracts A, B, C (events, policy, decisions)
- `Engineering_Rules.md` - Non-negotiables and governance
- `Contract-Test-Checklist.md` - 54 test cases with requirements
- `CI-Gating-Policy.md` - Test tiers and gating rules

---

## Why HASH-001/002 (Canonicalization)?
- **Pure function**: No I/O, no side effects, deterministic
- **Foundation**: Used by ALL tracks (shim, policy, ledger)
- **Easy to verify**: Two inputs → compare hashes
- **Easy to break**: Change one character → hash changes
- **P0 contract**: Must pass for v0.1 to ship (per CI-Gating-Policy.md)

---

## Contract Requirements (Interface-Pack.md §1.9.1)
- UTF-8 encoding
- Objects: keys sorted lexicographically by Unicode codepoint
- No insignificant whitespace
- Numbers in minimal decimal form without trailing zeros
- Arrays retain order
- Standard JSON escaping
- `args_hash = SHA-256(canonical_args_bytes)` lowercase hex

---

## Workflow Preferences
- **You run all tests manually** - I give commands, you execute
- **Detailed explanations** - I explain what each test checks and what failures mean

---

## Execution Steps

### Step 0a: Save Plan to Repository ✓
This file.

---

### Step 0b: Git Safety Setup (Before Any Code)
**Goal**: Create backup, then work on main—committing as we succeed.

You run:
```bash
git branch original   # Create backup branch at current state
git branch            # Verify: should show main and original
```

**Branch strategy:**
- `original` = pristine spec files (backup, never touched)
- `main` = where we work and commit as we succeed

**If total failure (need to start over):**
```bash
git checkout main
git reset --hard original
```
This resets main to the pristine spec state.

**On success:**
- Each passing milestone gets committed to main
- `original` stays as historical backup

---

### Step 1: Agent creates Go module and test infrastructure
Agent creates the Go module, directory structure, and empty function stubs.
After this step, you run: `go mod tidy`

### Step 2: Agent writes HASH-001 test (equivalence)
Agent writes a test that asserts:
- `{b:1, a:2}` and `{a:2, b:1}` produce **identical** hashes
- **Why this test exists**: Keys must be sorted for canonical JSON
- **What failure means**: Implementation doesn't sort keys

You run: `go test ./pkg/canonical/... -v`
**Expected**: RED - function returns error/empty (not implemented)

### Step 3: Agent writes HASH-002 test (stability with golden)
Agent writes a test that asserts:
- `ArgsHash(fixture)` equals a **precomputed golden value**
- **Why this test exists**: Ensures determinism and correct SHA-256
- **What failure means**: Hash algorithm wrong, or canonicalization differs

You run: `go test ./pkg/canonical/... -v`
**Expected**: RED - golden mismatch

### Step 4: Agent implements canonicalization
Agent implements `Canonicalize()` and `ArgsHash()` per Interface-Pack §1.9.1.
Agent explains the implementation as it writes.

You run: `go test ./pkg/canonical/... -v`
**Expected**: GREEN - all pass

### Step 5: You break it intentionally
You manually edit `pkg/canonical/canonical.go`:

| Bug to Introduce | Edit | Expected Failure |
|------------------|------|------------------|
| Remove key sorting | Comment out sort line | HASH-001 fails: "hashes not equal" |
| Wrong hash algo | Replace sha256 with sha1 | HASH-002 fails: "golden mismatch" |
| Add whitespace | Add space after colon | HASH-002 fails: "golden mismatch" |

You run: `go test ./pkg/canonical/... -v`
**Expected**: RED - test tells you exactly what's wrong

### Step 6: Agent fixes
You tell agent what broke, agent fixes it.
You run: `go test ./pkg/canonical/... -v`
**Expected**: GREEN

### Step 7: Confidence achieved
You now know:
- Tests fail when code is wrong
- Tests pass when code is correct
- Tests catch bugs you intentionally introduce
- **Therefore**: When tests pass, you can trust it works

---

## Success Criteria

You will have succeeded when you can say:
- "I watched the test fail when the implementation was wrong"
- "I watched the test pass when the implementation was correct"
- "I broke the code and the test caught it"
- "Therefore, when this test passes, I trust it works"

---

## Files to Create

| File | Purpose |
|------|---------|
| `go.mod` | Go module definition |
| `pkg/canonical/canonical.go` | Canonicalization implementation |
| `pkg/canonical/canonical_test.go` | HASH-001, HASH-002 contract tests |
| `testdata/fixtures/canonical/hash_vectors.json` | Test inputs and golden values |

---

## What Comes After This Exercise

Once you trust HASH-001/002 tests, the same pattern applies to:

1. **EVT-001/002**: Event schema tests (JSONL format, required fields)
2. **ERR-001**: Error shape tests (JSON-RPC codes)
3. **PROC-001**: Process lifecycle tests (signal handling)

Eventually, you can **parallelize development** by:
- Having multiple agents work on different tracks (A, B, C, etc.)
- Each track has its own contract tests
- When all contract tests pass, tracks integrate cleanly
- You trust the integration because you trust the tests

This is how you scale from "read every line" to "tests pass = ship it".
