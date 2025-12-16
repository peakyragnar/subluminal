Absolutely — here’s the CI Gating Policy layer. This is the “how we keep Subluminal from drifting into chaos” document: what’s blocking on PRs, what’s allowed to fail, what runs nightly, and how contract changes are handled without accidentally breaking every parallel track.

⸻

Subluminal CI Gating Policy v0.1

Purpose: Prevent contract drift, ensure cross-platform stability, and keep “data plane reliability” ahead of “UI polish.”

This policy is designed to be used with the Interface Pack + Contract Test Checklist you already have.

⸻

1) Core principle: Gating aligns to the data plane invariants

Subluminal only wins if:
	•	the shim is stable
	•	it never leaks secrets
	•	it never leaves zombies
	•	it doesn’t OOM on large payloads
	•	contract output stays stable

So gating is weighted heavily toward:
	•	shim pass-through correctness
	•	bounded inspection
	•	signal propagation
	•	deterministic event schema
	•	secret injection behavior

UI and “nice-to-haves” can be non-gating early.

⸻

2) Test tiers

We split tests into 4 tiers. The tier determines when and how tests run.

Tier 0 — Fast Unit (PR blocking)
	•	Pure unit tests (policy engine evaluation, canonicalization, diff logic)
	•	No processes spawned
	•	No network

Budget: < 60s

Tier 1 — Contract Integration (PR blocking)
	•	Shim ↔ fake MCP server ↔ driver client
	•	Emits JSONL events; optionally ingests ledger
	•	Covers the “do not break” contracts (event schema, error shapes, truncation)

Budget: < 5 minutes

Tier 2 — System / Concurrency (Nightly blocking, PR informational)
	•	50–100 concurrent runs
	•	large payload tests
	•	disk pressure tests for ledger ingestion

Budget: 10–30 minutes (nightly)

Tier 3 — Performance / Soak / Chaos (Nightly + Release)
	•	stress for 1–6 hours
	•	kill -9, crash upstream, intermittent tool errors
	•	memory leak checks

Budget: nightly / weekly / release

⸻

3) OS gating matrix

Cross-platform is important, but early gating must be realistic.

Required for PR merge
	•	Linux (primary): must pass Tier 0 + Tier 1.
	•	macOS (secondary): should pass Tier 0 + Tier 1; may be temporarily “allowed failure” only for non-P0 tests during v0.1.

Best-effort early
	•	Windows: Tier 0 required; Tier 1 best-effort until v0.2.
Reason: signal propagation/process-group semantics differ, and you don’t want Windows flakiness to block your core iteration early.

Rule: if a test is flaky on Windows but stable on Linux, Linux wins for gating until explicitly elevated.

⸻

4) What blocks a PR vs what only warns

We align to your checklist P0/P1 and convert to gating rules.

Blocking criteria (PR must not merge)

A PR cannot merge unless:
	•	all Tier 0 tests pass (Linux)
	•	all Tier 1 tests pass (Linux)
	•	any test tagged SECURITY passes (Linux + macOS)

Warning criteria (PR can merge with warnings)
	•	Tier 2 failures (nightly) open an issue automatically and post a comment on the last merged PR that introduced regression
	•	Windows Tier 1 failures during v0.1 do not block but must be tracked

⸻

5) Gating by milestone (what becomes required when)

This is the most important part: you don’t gate on tests for features you haven’t shipped.

v0.1 (Desktop Observe + Importer) — PR Blocking Test Set

Must pass on Linux (Tier 0 + Tier 1):

Event Contract
	•	EVT-001 JSONL single-line
	•	EVT-002 required envelope fields
	•	EVT-003 ordering/completeness
	•	EVT-004 run_id everywhere
	•	EVT-005 call_id uniqueness + seq monotonic
	•	EVT-006 tool/server name preservation
	•	EVT-007 latency_ms sanity
	•	EVT-008 status/error class taxonomy
	•	EVT-009 run_end summary correctness

Canonicalization
	•	HASH-001 canonicalization equivalence
	•	HASH-002 stability against golden

Bounded buffer / large payload safety
	•	BUF-001 truncation contract (must forward + truncated previews)
	•	BUF-003 forwarding correctness under truncation
	•	BUF-002 (concurrency large payload) is Tier 2 nightly in v0.1, not PR blocking

Error shape
	•	ERR-001 BLOCK error shape + code -32081
	•	ERR-002 THROTTLE code -32082 is optional in v0.1 unless you implement throttling
	•	ERR-003 REJECT_WITH_HINT not required until v0.3

Secrets
	•	SEC-001 secret injection “agent never sees secrets”
	•	ERR-004 no secret leakage in errors/logs

Process lifecycle
	•	PROC-001 SIGINT propagation (Linux/macOS)
	•	PROC-002 EOF shutdown behavior

Identity
	•	ID-001 identity env vars applied

Ledger
	•	LED-001 ingestion durability (if ledgerd is part of v0.1)
	•	If you choose “stdout only” initially: replace LED-001 with a stdout capture test that validates event validity

Importer
	•	IMP-001 backup + restore correctness
	•	IMP-002 time-to-first-log path

Allowed to fail / warn (v0.1)
	•	Windows Tier 1 process tests (signals) — warn only
	•	PROC-003 upstream crash handling — P1 until v0.2
	•	BUF-004 rolling hash — P1 until v0.2
	•	Any UI tests — non-gating

⸻

v0.1.5 (Headless CI proof) — Merge Blocker for “release branch”

You can keep PR gating light, but for cutting a v0.1.5 release branch:

Must pass (Linux):
	•	BUF-002 concurrency large payload (50+ concurrent)
	•	LED-002 backpressure drops previews not decisions (if ledger used under load)
	•	A new test: HDLS-001 “50 concurrent headless runs complete; policy strict mode enforced”

Additional operational checks
	•	artifact export produces a trace bundle per run (even if minimal)

⸻

v0.2 (Guardrails: budgets, rate limits, breakers, dedupe)

Once guardrails are a feature, stateful enforcement must be gated.

New blocking tests (Linux):
	•	POL-003 budgets decrement + block on exceed
	•	POL-004 token bucket throttles deterministically
	•	POL-005 breaker trips on repeats / error windows
	•	POL-006 dedupe window blocks duplicates
	•	LED-002 backpressure behavior becomes P0 if ledger used
	•	PROC-003 upstream crash handled gracefully becomes P0
	•	SEC-002 secret_injection metadata event (if you emit it) must contain no secret values

Windows escalation
	•	Windows Tier 1 becomes PR blocking for non-signal tests
	•	Signal tests remain best-effort until you implement Windows-friendly semantics explicitly

⸻

v0.3 (Control Mode: Reject-with-Hint moat)

REJECT_WITH_HINT becomes gating because it’s a contract promise.

Blocking tests (Linux + macOS):
	•	ERR-003 reject-with-hint error code -32083 + hint payload correctness
	•	EVT: hint_issued event emitted (if you adopt it)
	•	A new test: HINT-001 “agent retry loop can recover using suggested_args”
	•	This can be simulated with a driver that retries automatically on hint

Governance-as-code becomes gating
	•	sub policy diff outputs stable severity classifications
	•	A new test: POL-DIFF-001 “diff shows critical when destructive tool is newly allowed”

⸻

6) Contract changes and “golden” updates policy

This prevents the biggest multi-agent failure: everyone updates fields casually.

6.1 What counts as a contract change

Any change to:
	•	event field names or required fields
	•	decision/action enums
	•	JSON-RPC error codes or error.data shape
	•	canonicalization rules
	•	truncation behavior semantics

…is a contract change.

6.2 Contract change workflow (required)

A PR that changes contracts must:
	1.	bump Interface Pack version appropriately:
	•	additive only → MINOR
	•	breaking → MAJOR (avoid this early)
	2.	update docs/INTERFACE_PACK.md and docs/CONTRACT_TESTS.md
	3.	update goldens only in that PR
	4.	include a “Contract Change” label and require reviewer approval (human)

6.3 Golden updates (normal workflow)

Goldens may be updated only when:
	•	contract version bump is present, or
	•	the golden was wrong and the fix is demonstrably compliant with current contract (rare)

Any PR that changes goldens without a contract bump should fail CI (simple check: “golden files changed but Interface Pack version unchanged”).

⸻

7) CI artifacts (what must be uploaded on failure)

When Tier 1 tests fail, CI must attach:
	•	the raw JSONL event stream
	•	the shim stderr/stdout logs
	•	the policy bundle used
	•	(if ledger used) the SQLite db file
	•	“repro command” string (printed by harness)

This makes debugging 10× faster and reduces “it works on my machine” drift.

⸻

8) Flakiness policy (practical and strict)

Flakiness kills infra products, but you also can’t stall forever.

Flaky test definition

A test is flaky if it fails intermittently without code changes.

Flaky test rules
	•	If a P0 test flakes: it is immediately quarantined:
	•	removed from PR gating
	•	runs nightly
	•	tracked as a P0 bug to fix within 48–72 hours
	•	After it passes 20 consecutive nightly runs, it is reintroduced to PR gating.

This prevents CI from becoming a random number generator while maintaining pressure to fix flakiness quickly.

⸻

9) Performance budgets (non-blocking until v0.2)

Start measuring early, but don’t block v0.1 on perf until you have baselines.

Metrics to record (Tier 3)
	•	shim overhead latency p50/p95/p99 per call (target: +<5ms p95 for typical calls)
	•	memory growth over long runs (no unbounded growth)
	•	ledger ingest throughput

When performance becomes gating
	•	v0.2: a “perf regression guardrail” becomes warning-level (not blocking)
	•	v1: perf becomes blocking for release branches

⸻

10) PR checklist (human process that saves you)

Every PR must declare:
	•	which track it touches (A/B/C/…)
	•	which contract tests it affects
	•	whether it changes Interface Pack contracts (yes/no)
	•	whether it changes goldens (yes/no)
	•	whether it introduces new policy rules or enforcement behavior

This reduces accidental contract breakage by parallel agents.

⸻

Quick summary (what you tell the team)
	•	Linux Tier 0+1 blocks merges.
	•	v0.1 gates on: events, hashing, truncation, secrets, process cleanup, importer.
	•	v0.2 gates on: budgets/rate/breakers/dedupe.
	•	v0.3 gates on: reject-with-hint + policy diff.
	•	Goldens can’t change unless the Interface Pack version changes or it’s a clear compliance fix.
	•	Flaky tests get quarantined fast, then reinstated after proven stable.

