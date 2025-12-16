Subluminal Engineering Rules

Document status: Canonical team constitution for v0.x
Applies to: all repos, all contributors (humans + coding agents)

⸻

1) Mission

Subluminal is a vendor-neutral agent tool execution data plane:
	•	intercepts tool calls
	•	enforces local, machine-speed policy
	•	emits an append-only event stream
	•	records an auditable ledger

If we violate any of those properties, we are building a toy.

⸻

2) Non‑negotiables

These are “ship blockers,” not preferences.

2.1 Data plane reliability beats everything
	•	The shim MUST be stable under failure conditions:
	•	upstream crashes
	•	client crashes
	•	large payloads
	•	high concurrency
	•	The shim MUST NOT hang waiting for a human (v0.x).
	•	The shim MUST enforce policy without calling external services in the hot path.

2.2 Bounded memory, always-forward semantics
	•	The shim MUST forward traffic even when inspection/logging is truncated.
	•	The shim MUST NOT buffer unbounded payloads to redact/inspect.
	•	Truncation behavior MUST follow the Interface Pack.

2.3 No zombies
	•	The shim MUST cleanly terminate upstream processes.
	•	SIGINT/SIGTERM and EOF handling MUST work (at least Linux/macOS v0.1).

2.4 Agent must not see secrets
	•	Secrets MUST be injected by Subluminal (spawn-time env injection v0.1).
	•	Secrets MUST NOT appear in:
	•	logs
	•	previews
	•	error payloads
	•	ledger storage

⸻

3) Stable contracts (do not break)

The product is the contracts. Everything else is implementation detail.

3.1 The Interface Pack is the source of truth

All producers/consumers MUST adhere to:
	•	docs/INTERFACE_PACK.md
(Event schema, policy bundle schema, decision/error shapes, hashing, truncation)

3.2 Contract tests define “correct”

All “contract” correctness is encoded in:
	•	docs/CONTRACT_TESTS.md

3.3 Contract changes require an explicit process

If you change any of:
	•	event field names / required fields
	•	decision/action enums
	•	JSON-RPC error codes or error.data schema
	•	canonicalization/hashing semantics
	•	truncation semantics

…you are making a contract change.

Contract change PRs MUST:
	1.	bump Interface Pack version correctly (SemVer: additive=MINOR, breaking=MAJOR)
	2.	update INTERFACE_PACK.md + CONTRACT_TESTS.md
	3.	update any golden fixtures in the same PR
	4.	include “Contract Change” label + require maintainer review

⸻

4) CI gates (what blocks merges)

This repo is governed by CI, not vibes.

4.1 Test tiers
	•	Tier 0: unit tests (fast, deterministic)
	•	Tier 1: contract integration tests (shim ↔ fake MCP server ↔ driver)
	•	Tier 2: concurrency/system tests (nightly)
	•	Tier 3: soak/perf/chaos (nightly/release)

4.2 Merge blocking rules (v0.1 default)

A PR MUST NOT merge unless:
	•	Linux passes Tier 0 + Tier 1
	•	any test tagged SECURITY passes on Linux + macOS
	•	contract goldens are unchanged unless Interface Pack version bump (see §5)

Windows is best-effort in v0.1; escalates later.

4.3 CI policy is codified

See:
	•	docs/CI_GATING_POLICY.md

If there’s conflict, CI_GATING_POLICY.md wins.

⸻

5) Golden files policy (prevents drift)

Goldens exist so parallel contributors don’t quietly diverge.

5.1 Golden updates are restricted

A PR that changes golden fixtures MUST:
	•	include an Interface Pack version bump, or
	•	explicitly justify why the previous golden was non-compliant

5.2 CI should enforce this

We treat “goldens changed without version bump” as failure.

⸻

6) Flakiness policy (fast quarantine, fast fix)

Flaky tests destroy infra teams.
	•	If a P0 test flakes: quarantine immediately
	•	remove from PR gate
	•	run nightly
	•	open P0 issue
	•	Restore to PR gate only after 20 consecutive nightly passes

⸻

7) Module boundaries and ownership (parallel development)

We optimize for parallel work without divergence.

7.1 Tracks are real boundaries

Each contributor (human or coding agent) should own one track at a time:
	•	A1: MCP stdio adapter (protocol handling, process supervision)
	•	A2: core enforcement layer (protocol-agnostic, shared by all adapters)
	•	B: policy engine (stateful enforcement, canonicalization helpers)
	•	C: sub ledgerd (SQLite schema + ingestion + queries)
	•	D: importer/restore (Claude + Codex DX)
	•	E: CLI tools (sub run, sub tail, sub query)
	•	F: secrets (sub secrets + injection mapping)
	•	G: UI (optional)
	•	H: headless/CI mode (v0.1.5)
	•	I: QA harness + contract tests

Note: Track A2 (core) is shared by ALL future adapters (MCP HTTP, Messages API, etc.). Track A1 is the first adapter implementation.

7.2 Cross-cutting changes require coordination

If a PR touches two tracks, it must:
	•	state why the cross-cut is necessary
	•	include affected test IDs from CONTRACT_TESTS.md
	•	tag both track owners for review (or a maintainer if solo)

⸻

8) Naming and user-facing consistency

We ship one coherent tool.
	•	CLI binary is sub
	•	User-visible names: Subluminal
	•	Paths: ~/.subluminal/…
	•	Avoid baking old placeholder prefixes (e.g., gw) into public surfaces.

⸻

9) Security baseline (v0.x)

Minimum security expectations:
	•	no secret values in any logs or stored previews
	•	localhost-only bindings for local services by default
	•	auth token required for any local HTTP API/UI
	•	redact previews by default (opt-in per tool/server later)

Security bugs are treated as P0.

⸻

10) Release discipline

10.1 Release branches

We cut releases only when:
	•	Tier 1 green on Linux + macOS
	•	Tier 2 green at least once in the last 24 hours
	•	no open P0 security issues

10.2 Versioning
	•	sub versions are independent from Interface Pack version
	•	Contract version bumps are governed by the Interface Pack SemVer rules

⸻

11) “What we don’t do” (v0.x guardrails)

To prevent scope creep:
	•	no cloud control plane required for core function
	•	no in-flight approval pause in v0.x (use deterministic errors)
	•	no claims of “full agent governance” without sandbox/egress controls
	•	no breaking contract changes without MAJOR bump and explicit approval

⸻

12) Glossary
	•	Data plane: shim/sidecar hot path that intercepts and enforces locally
	•	Control plane: ledger/UI/policy distribution (must not be on hot path)
	•	Interface Pack: the canonical contract definitions
	•	Contract tests: automated tests that enforce the Interface Pack
	•	Golden fixtures: expected outputs that prevent drift
	•	Reject-with-hint: deterministic “soft failure” allowing agent recovery loop

⸻

Required docs
	•	docs/INTERFACE_PACK.md
	•	docs/CONTRACT_TESTS.md
	•	docs/CI_GATING_POLICY.md

This document is intentionally strict. Infrastructure succeeds by being boringly reliable.