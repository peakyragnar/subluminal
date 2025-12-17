# 2-Agent Parallel Build Plan (NEXT PHASE)

**Status:** SAVED FOR LATER - Fix test infrastructure issues first

---

## User Preferences
- **Agents:** 2 parallel
- **Mode:** Test-driven (controlled by tests, not accepting every change)
- **Goal:** Build 2 things at once, verify by making skipped tests pass

---

## The Plan: Importer + Policy Engine

### Agent 1: Importer (completes v0.1)
**Goal:** Enable IMP-001, IMP-002 tests
**Why:** Required for v0.1 milestone - "install → import → first log < 5 minutes"

**Deliverables:**
- `cmd/sub/main.go` - CLI entry point
- `cmd/sub/import.go` - `sub import claude|codex` command
- `cmd/sub/restore.go` - `sub restore claude|codex` command
- `pkg/importer/claude.go` - Claude MCP config detection/rewriting
- `pkg/importer/codex.go` - Codex MCP config detection/rewriting
- `pkg/importer/backup.go` - Backup/restore logic

**Tests to make pass:**
- IMP-001: Backup + restore correctness
- IMP-002: Time-to-first-log < 5 minutes path

**Success criteria:**
```bash
# These should work:
./bin/sub import claude   # Rewrites Claude MCP config to route through shim
./bin/sub restore claude  # Restores original config
go test ./test/contract/... -run 'TestIMP'  # Both tests pass
```

---

### Agent 2: Policy Engine Foundation (v0.2 start)
**Goal:** Enable POL-002 through POL-007, ERR-001 through ERR-004 tests
**Why:** Enables 12 tests, core product value ("guardrails")

**Deliverables:**
- `pkg/policy/types.go` - Policy bundle types (per Interface-Pack §2)
- `pkg/policy/loader.go` - Load/compile YAML policy bundles
- `pkg/policy/engine.go` - Rule evaluation engine
- `pkg/policy/matcher.go` - Server/tool/args glob/regex matching
- `pkg/policy/decision.go` - Decision types (ALLOW/BLOCK/THROTTLE/REJECT_WITH_HINT)
- Integration into `pkg/adapter/mcpstdio/proxy.go` - Apply policy decisions

**Tests to make pass:**
- POL-002: Allow/Deny ordering
- POL-003: Budget rule decrements & blocks
- POL-004: Token bucket rate limit (THROTTLE)
- POL-005: Breaker repeat_threshold triggers
- POL-006: Dedupe window blocks duplicate
- ERR-001: BLOCK uses JSON-RPC error -32081
- ERR-002: THROTTLE uses error -32082 + backoff_ms
- ERR-003: REJECT_WITH_HINT uses -32083 + hint object
- ERR-004: No secret leakage in errors
- ADAPT-003: Adapter formats errors correctly

**Success criteria:**
```bash
# All policy tests pass:
go test ./test/contract/... -run 'Test(POL|ERR|ADAPT003)'
```

---

## Files to Read First

**Agent 1 (Importer) should read:**
- `docs/Subliminal-Design.md` §13 (Importer requirements)
- `test/contract/remaining_test.go` (IMP-001, IMP-002 test stubs)
- Claude MCP config location: `~/.config/claude-code/` or similar

**Agent 2 (Policy Engine) should read:**
- `docs/Interface-Pack.md` §2 (Policy Bundle Schema)
- `docs/Interface-Pack.md` §3 (Decision & Error Shapes)
- `test/contract/pol_test.go` (POL-002 through POL-007)
- `test/contract/err_test.go` (ERR-001 through ERR-004)
- `pkg/adapter/mcpstdio/proxy.go` (where to integrate policy decisions)

---

## Expected Outcome

After both agents complete:
- **Before:** 27 pass, 17 skip (after BUF-003 fix)
- **After:** 39 pass, 5 skip

**Final 5 skips (all legitimate):**
- LED-001, LED-002: Need ledger component (future)
- BUF-004: P1 feature (explicitly deferred)
- ADAPT-002: Needs multiple adapters (future)
- SEC-002: Optional secret_injection events
