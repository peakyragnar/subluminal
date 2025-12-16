Yes — gw was just a placeholder prefix because we were calling the system a “gateway”/“gateway toolkit” (data-plane gateway + control-plane governance). With the product name Subluminal, we should rename the CLI and all command surfaces so engineering doesn’t bake “gateway” into everything.

Recommended naming:
	•	Binary name: sub (fast to type)
	•	Project/product name in docs/UI: Subluminal
	•	Config/dirs: ~/.subluminal/…
	•	“gw-*” tools become: sub <subcommand> (Unix-y), or separate binaries with sub-<tool> if you really want Lego executables.

For a solo/busy team and parallel agents, the simplest is one binary with subcommands (easy packaging), but keep the conceptual modularity in the codebase.

⸻

Subluminal v1.0 Engineering Specification

Subtitle: Semantic firewall + DVR for agent tool execution. Local-first wedge, cloud-scale core.

This doc is designed to be handed directly to an engineering team (or multiple coding agents) and built in parallel.

⸻

0) One-sentence definition

Subluminal is the vendor-neutral data plane that intercepts agent tool execution, enforces machine-speed guardrails locally, and records an auditable ledger of what the agent actually did.

⸻

1) North Star and Success Criteria

North Star (v0.1)

A developer runs:
	1.	sub import claude or sub import codex
	2.	runs an agent normally
	3.	sees live tool-call logs in < 5 minutes
	4.	with zero tool renaming and no orphan processes

“Not a toy” proof (v0.1.5)

We can run 50–100 headless agent jobs concurrently in CI/containers with:
	•	Guardrails enabled (budgets / breakers)
	•	Deterministic logs per run
	•	No panics, no corrupted ledgers, no runaway loops

⸻

2) Product Philosophy: Unix Lego Blocks, Not a Platform

Subluminal is a primitive, not an ecosystem.
	•	You can install just the shim.
	•	Or just the ledger viewer.
	•	Or the full pipeline.
	•	Integrations are optional and based on stable interfaces: event stream + policy bundles.

Invariant bet: tool execution boundaries will always exist, even if protocols change.

⸻

3) Architecture Overview

Core split: Data Plane vs Control Plane
	•	Data Plane: runs in the hot path; must be fast and resilient
Intercepts tool calls, applies local policy, injects secrets, emits events.
	•	Control Plane: never required in the hot path
Stores ledger, provides UI/search, distributes policy snapshots (later).

Architectural graph (high level)

         ┌─────────────────────────────────────────────┐
         │               Agent Client                  │
         │  Claude Code / Codex CLI / Headless Runner  │
         └───────────────────┬─────────────────────────┘
                             │ (MCP server entries)
                     ┌───────┴────────┐
                     │                │      ... one per tool server entry
                     ▼                ▼
           ┌────────────────┐  ┌────────────────┐
           │ sub shim stdio │  │ sub shim stdio │
           │  (server=git)  │  │ (server=linear)│
           └───────┬────────┘  └───────┬────────┘
                   │                   │
                   ▼                   ▼
        ┌──────────────────┐   ┌──────────────────┐
        │ Upstream MCP tool │   │ Upstream MCP tool │
        │ server (git)      │   │ server (linear)   │
        └──────────────────┘   └──────────────────┘

                   │                   │
                   │ async events      │ async events
                   └─────────┬─────────┘
                             ▼
               ┌────────────────────────────┐
               │     sub ledgerd (local)    │
               │  - SQLite ledger (WAL)     │
               │  - query API + WS stream   │
               │  - optional UI server      │
               └───────────┬────────────────┘
                           ▼
                  ┌─────────────────┐
                  │ sub ui (optional)│
                  │ DVR + search     │
                  └─────────────────┘

Key design choice: shim-per-server preserves server identity and tool names by default.

⸻

4) Deployment Profiles (Desktop → CI → K8s)

Profile 1: Desktop “Dev Loop / Time-Travel Debugging”

Goal: fast iteration + visibility
Components: shims + local ledger + optional UI
Policy: local policy.yaml in repo; hot reload

What “Replay” means in v0.x:
	•	v0.1–v0.2: replay trace viewing + diff policies against past runs (“would this have been blocked?”)
	•	later: simulated re-execution with recorded tool responses (optional)

Profile 2: CI/Container “Gatekeeper / Regression”

Goal: enforce strict rules and catch regressions
Components: shims (headless) + stdout JSONL + artifact bundles
Policy: strict mode; violations fail job
Key value: proves safety at scale without UI

Profile 3: Kubernetes Sidecar “Fleet Firewall / Kill Switch”

Goal: compliance + blast radius + emergency stop
Components: sidecar data plane + central collector + optional SaaS control plane
Policy: signed snapshots cached locally; configurable fail-open/closed by tool risk class

⸻

5) Stable Contracts (Do Not Break)

These contracts are the real product. Everything else can change.

Contract A: Event Schema (append-only)

Subluminal emits events (JSONL initially) with stable fields.

Required fields:
	•	ts (RFC3339 or epoch millis)
	•	run_id (correlates a job/run)
	•	call_id (unique per tool call)
	•	client (claude|codex|headless|custom)
	•	agent_id (stable identity for fleet use; see §8)
	•	server_name (exact configured server name)
	•	tool_name (exact upstream tool name)
	•	args_hash (hash of canonical args)
	•	decision (ALLOW|BLOCK|THROTTLE|REJECT_WITH_HINT|TERMINATE_RUN)
	•	rule_id (when decision != ALLOW)
	•	status (OK|ERROR|TIMEOUT|CANCELLED)
	•	latency_ms
	•	bytes_in / bytes_out (optional but recommended)
	•	preview_truncated (bool)

Optional preview fields (redacted by default):
	•	args_preview
	•	result_preview

Contract B: Policy Bundle Format

Policies are versioned bundles (YAML/JSON) compiled into a snapshot the data plane can evaluate locally.

Contract C: Deterministic Enforcement & Error Shape
	•	No “in-flight pause” in v0.x.
	•	Control behavior uses REJECT_WITH_HINT as a deterministic, fast JSON-RPC error with structured data.

⸻

6) Subluminal CLI Surface (Spec)

Single binary

sub is the user-facing entrypoint.

P0 commands (v0.1)
	•	sub import claude
	•	sub import codex
	•	sub restore claude|codex
	•	sub run -- <agent command…> (tags a run_id and sets identity env)
	•	sub tail [--run <id>]
	•	sub query … (basic search)
	•	sub ledgerd (start local daemon)
	•	sub version, sub doctor

Next commands (v0.2+)
	•	sub policy lint|compile|diff|explain
	•	sub secrets add|get|list|remove
	•	sub export run <id>

⸻

7) Data Plane: sub shim (Hot Path)

7.1 Responsibilities

Each shim instance:
	1.	speaks MCP transport to the client (stdio first)
	2.	proxies to upstream tool server
	3.	identifies tool calls (tools/call) and applies policy
	4.	injects secrets into upstream server environment (spawn-time injection)
	5.	emits events asynchronously
	6.	guarantees clean process shutdown (no zombies)
	7.	never buffers large payloads unboundedly

7.2 Must preserve semantics
	•	tool names must match upstream tools by default
	•	server identity preserved (shim is configured per server)

7.3 Stateful enforcement in the data plane (required for machine-speed)

The data plane maintains bounded, local state per (run_id, agent_id, server_name, tool_name):
	•	token bucket state (rate)
	•	counters (budget)
	•	error windows (breaker)
	•	repeated-call detection (loop guard)
	•	dedupe cache for write-like tools

Design principle: locally stateful, externally stateless
No database lookups, no control plane RPC in the hot path.

7.4 Secret Injection (Security “God Mode”)

Goal: agent never sees real secrets.

v0.1 injection pattern
	•	Shim spawns upstream tool server process.
	•	Shim injects secrets as environment variables into the child process.
	•	Agent only sends placeholder tokens or no tokens at all.

Configuration mapping
	•	For each server:
	•	secret_bindings: map of env var name → secret reference
	•	secret reference sources:
	•	v0.1: shell env passthrough
	•	v0.2+: OS keychain via sub secrets

Logging
	•	never log injected values
	•	redact patterns in previews
	•	store only hashes where necessary

7.5 Process Supervision & Signal Propagation (Zombie prevention)

Shim must implement:
	•	SIGINT/SIGTERM handling
	•	forward signals to upstream tool server process group
	•	EOF behavior
	•	if stdin closes, terminate upstream and exit
	•	Parent death detection
	•	if agent client process ends and pipes close, shim exits
	•	No orphan child processes
	•	upstream must be terminated if shim exits unexpectedly

This is P0: leaving zombie shims is an uninstall trigger.

7.6 Streaming vs Buffering: Bounded Inspection

Never buffer entire payloads to inspect/redact.

Defaults (configurable):
	•	MAX_INSPECT_BYTES = 1MB per message
	•	MAX_PREVIEW_BYTES = 16KB stored/displayed
	•	If a message exceeds MAX_INSPECT_BYTES:
	•	forward raw bytes normally
	•	log metadata only:
	•	size
	•	rolling hash (incremental)
	•	[TRUNCATED] previews

For Streamable HTTP/SSE (later):
	•	never buffer full streams
	•	capture boundaries + previews + hashes only

7.7 Adapter Architecture (Protocol Abstraction)

The shim separates into two layers to support multiple protocols:

         ┌─────────────────────────────────────────┐
         │           ADAPTER LAYER                 │
         │  (Protocol-specific, swappable)         │
         │                                         │
         │  ┌───────────┐ ┌───────────┐ ┌───────┐  │
         │  │MCP stdio  │ │MCP HTTP   │ │Future │  │
         │  └─────┬─────┘ └─────┬─────┘ └───┬───┘  │
         └────────┼─────────────┼───────────┼──────┘
                  └─────────────┼───────────┘
                                ▼
         ┌─────────────────────────────────────────┐
         │           CORE LAYER                    │
         │  (Protocol-agnostic, shared)            │
         │                                         │
         │  - Policy engine                        │
         │  - Event emission                       │
         │  - Secret injection                     │
         │  - State tracking (budgets, rates)      │
         │  - Decision logic                       │
         └─────────────────────────────────────────┘

Adapters MUST:
	•	Parse protocol-specific messages
	•	Extract (server_name, tool_name, args) tuples
	•	Forward to core layer for enforcement
	•	Return protocol-specific responses (including errors)
	•	Handle process supervision for their transport (signals, EOF for stdio)

Core layer MUST:
	•	Be completely protocol-agnostic
	•	Work only with abstract tool call representations
	•	Emit events in the standard schema
	•	Apply policy without knowing transport details

Known adapters:
	•	MCP stdio (v0.1)
	•	MCP HTTP (planned)
	•	Messages API (planned)
	•	HTTP proxy (future)

This separation ensures that adding new protocols requires only a new adapter, not changes to enforcement logic.

⸻

8) Agent Identity & Policy Targeting (Fleet-scale requirement)

8.1 Identity Envelope (required fields)

Every run and call must carry:
	•	agent_id (logical stable identity; e.g., “repo-fixer”, “ci-agent”, “prod-sweeper”)
	•	run_id (unique per run/job)
	•	principal (who initiated: user/service)
	•	environment (dev|ci|prod)
	•	workload metadata:
	•	Desktop: local user + repo path
	•	CI: pipeline/job id + repo + branch
	•	K8s: namespace + service account + pod labels

8.2 How identity is established
	•	Desktop: sub run sets env vars and run_id
	•	CI: runner sets run_id and environment automatically
	•	K8s: sidecar derives identity from downward API / service account / labels

8.3 Policy targeting selectors

Policy bundles select by:
	•	environment
	•	agent_id
	•	server/tool patterns
	•	risk class (read-like vs write-like)
	•	workload labels

This enables:
	•	“prod agents cannot call write tools”
	•	“this team’s agents have lower budgets”
	•	“emergency stop: disable tool X globally”

⸻

9) Policy Engine (Core Library)

9.1 Modes
	•	Observe: allow all, log
	•	Guardrails: enforce budgets/rate/breakers/allowlists without prompts
	•	Control: REJECT_WITH_HINT + “promote hint to rule”

9.2 Rule Types

Minimum viable set:

Access control
	•	allow/deny by:
	•	server_name glob/regex
	•	tool_name glob/regex
	•	argument predicates (simple: key exists, enum allowed, numeric range)

Budgets (per run / per tool)
	•	max calls
	•	max “cost units”
	•	max write actions

Rate limits (token bucket)
	•	tokens per time window
	•	per tool/per server/per run

Circuit breakers
	•	error_count threshold within time window
	•	repeated_call threshold for same (tool, args_hash) in window
	•	latency threshold (optional)

Dedupe windows (write-like actions)
	•	block repeated write-like calls within N seconds unless explicitly overridden

9.3 Decision outputs
	•	ALLOW
	•	BLOCK
	•	THROTTLE (with backoff)
	•	TERMINATE_RUN
	•	REJECT_WITH_HINT (structured error)

9.4 Governance-as-Code tooling

sub policy diff <old> <new> must output:
	•	what changed
	•	severity classification
	•	human readable summary + machine readable JSON

This is what makes Subluminal “Terraform-like”: policy changes are reviewable and CI-enforceable.

⸻

10) REJECT_WITH_HINT (Moat Feature)

10.1 Purpose

Convert “blocked” into an agent recovery loop.

10.2 Behavior

When a call is blocked in Control mode, return immediately with JSON-RPC error containing:
	•	gateway_decision: "REJECT_WITH_HINT"
	•	rule_id
	•	call_id
	•	reason (enum)
	•	hint_text
	•	suggested_args (optional corrected args)
	•	retry_advice (e.g. backoff, batch requests, reduce scope)

No hanging requests, no approval pauses.

10.3 Promote hint to rule

Operator can promote a hint into:
	•	a session-scoped allow rule
	•	or a persistent policy change (reviewed via diff)

⸻

11) Ledger: sub ledgerd + SQLite

11.1 Local ledger goals
	•	single-writer, append-only
	•	durable, queryable
	•	safe under concurrent shim writers

11.2 Storage
	•	SQLite WAL mode
	•	local path: ~/.subluminal/ledger.db

11.3 Schema (minimum)

Tables:
	•	runs
	•	run_id, agent_id, client, env, started_at, ended_at, status, metadata_json
	•	tool_calls
	•	call_id, run_id, server_name, tool_name, args_hash, decision, rule_id, status,
latency_ms, bytes_in, bytes_out, preview_truncated, created_at
	•	previews
	•	call_id, args_preview, result_preview, redaction_flags
	•	hints
	•	call_id, hint_text, suggested_args_json, created_at
	•	policy_versions
	•	policy_id, version, mode, rules_hash, rules_json, created_at

Indexes:
	•	(run_id, created_at)
	•	(server_name, tool_name)
	•	(decision, status)
	•	(args_hash) for dedupe investigations

11.4 Event ingestion

Two supported ingest methods:
	•	Desktop: unix domain socket (preferred) or localhost HTTP
	•	CI: stdout JSONL + optional sub ingest later
	•	K8s: HTTP/gRPC collector (later)

Ledgerd must be robust to bursts: ring buffer + backpressure strategy (drop previews first, never drop decision events).

⸻

12) UI (Optional, not required for headless)

MVP UI surfaces
	•	Live stream (DVR)
	•	Run list (scoreboard)
	•	Run detail timeline
	•	Call detail
	•	Filters: status/decision/tool/server
	•	Policy viewer (later)
	•	Hints view (v0.3)

UI must not block shipping. CLI tail/query is required.

⸻

13) Importer (P0: Adoption)

Requirements

sub import claude and sub import codex must:
	•	detect existing MCP server configs
	•	create a backup copy
	•	rewrite server entries to route through shims
	•	preserve server names
	•	print restore instructions

Also provide:
	•	sub restore claude|codex

Time-to-first-call depends on importer.

⸻

14) Known Boundary & Loophole: Code Execution

Tool-call governance is not full execution governance if the agent can run arbitrary code.

Spec statement (explicit)
	•	v0.x guarantees apply only to calls that pass through Subluminal shims.
	•	To prevent bypass via code execution:
	•	Desktop: recommend running agent inside a sandboxed container for sensitive workflows
	•	CI/K8s: enforce egress and filesystem mounts at container/runtime level

Roadmap (optional modules)
	•	container runtime hooks
	•	eBPF syscall telemetry (Linux-first)
	•	“exec boundary adapter” for common interpreters (later)

This prevents security overclaims.

⸻

15) Technical Choices (with rationale)

Language

Recommendation: Go for v0.x (shim + ledgerd)
	•	fast iteration
	•	strong concurrency primitives (channels)
	•	static binaries, easy packaging
	•	good cross-platform support for CLI tooling

When Rust makes sense
	•	if data plane latency becomes a hard requirement at massive scale
	•	if you want memory safety guarantees in hot path
You can still keep policy engine logic portable (shared spec + test vectors).

Storage
	•	SQLite WAL for desktop + CI artifacts (v0.x)
	•	ClickHouse or similar for fleet analytics (v1+)
	•	Postgres for control plane metadata (v1+)

Event encoding
	•	JSONL initially (human-friendly, simple)
	•	Protobuf later for high-volume fleets

Config formats
	•	policy.yaml for policy bundles (human-friendly)
	•	config.toml for system config (stable, explicit types)

IPC
	•	Unix domain sockets where available
	•	localhost HTTP fallback (Windows)

Hashing
	•	SHA-256 for args_hash (safe default)
	•	optional BLAKE3 later for speed

Packaging
	•	single binary distribution
	•	Docker image for headless/CI and sidecar modes
	•	future: homebrew/scoop installers

⸻

16) Parallel Build Plan (Designed for multiple coding agents)

Below is the parallelization map you can hand to a “coding agents swarm.” Each track has:
	•	inputs
	•	outputs
	•	interfaces
	•	acceptance tests

Track A1 — MCP stdio Adapter (Protocol handling)

Owner: Agent A1
Inputs: MCP protocol spec, core layer interface
Outputs: MCP stdio adapter that extracts tool calls and formats responses
Acceptance tests:
	•	parses MCP stdio JSON-RPC correctly
	•	extracts (server_name, tool_name, args) from tools/call
	•	formats JSON-RPC errors correctly for blocked calls
	•	handles process supervision (SIGINT/SIGTERM, EOF)
	•	forwards signals to upstream and exits cleanly (no zombie)

Track A2 — Core Enforcement Layer (Protocol-agnostic)

Owner: Agent A2
Inputs: event schema, policy snapshot format, secret binding format, adapter interface
Outputs: protocol-agnostic enforcement module shared by ALL adapters
Acceptance tests:
	•	receives abstract tool call → applies policy → returns decision
	•	protocol-agnostic (tested with mock adapter)
	•	emits correct events regardless of transport
	•	bounded inspect: large payload doesn't OOM; previews truncated
	•	enforces allow/deny deterministically

Note: Track A2 (Core) is shared by all future adapters. Track A1 is the first adapter.

Track B — Policy Engine Library (Stateful enforcement)

Owner: Agent B
Inputs: policy bundle schema, decision schema
Outputs: pure evaluation module + test vectors
Acceptance tests:
	•	token bucket works (rate)
	•	budgets decrement correctly (per run / per tool)
	•	breakers trigger on repeated calls and error windows
	•	dedupe window blocks repeated write-like calls
	•	produces decision trace for explainability

Track C — sub ledgerd + SQLite schema

Owner: Agent C
Inputs: event schema, ingestion transport choice
Outputs: durable local ledger; query endpoints
Acceptance tests:
	•	can ingest 10k events without corruption
	•	can query by run_id, tool, decision
	•	WAL enabled; safe concurrent ingestion
	•	never blocks shim: ingestion backpressure drops previews before dropping decision events

Track D — Importer/Restore for Claude + Codex (DX P0)

Owner: Agent D
Inputs: desired CLI UX; shim launch contract
Outputs: import + restore operations
Acceptance tests:
	•	import rewrites configs and preserves server names
	•	backup created and restore returns system to exact previous state
	•	“first tool call logged” path demonstrated end-to-end

Track E — CLI Operator Tools (sub tail, sub query, sub run)

Owner: Agent E
Inputs: ledger schema + query endpoints; identity envelope
Outputs: CLI UX for human-on-the-loop operations
Acceptance tests:
	•	sub run stamps run_id + identity env vars
	•	sub tail streams live events
	•	sub query filters by decision/status/tool/server quickly

Track F — Secrets (sub secrets) + Injection mapping

Owner: Agent F
Inputs: secret binding spec; shim spawn contract
Outputs: v0.1 env passthrough + v0.2 keychain integration plan
Acceptance tests:
	•	injected env vars present in upstream tool server but not visible in agent logs
	•	secrets never written to ledger previews
	•	placeholder auth scheme documented for tools that require headers (future)

Track G — UI (Optional)

Owner: Agent G
Inputs: WS/live stream + ledger query endpoints
Outputs: DVR UI + run view + call detail + filters
Acceptance tests:
	•	handles 10k events smoothly (virtualized list)
	•	renders truncated previews clearly
	•	shows decision/rule_id/hints

Track H — CI/Container Headless Mode (v0.1.5)

Owner: Agent H
Inputs: shim + policy + JSONL output; run_id contract
Outputs: container mode that runs many concurrent jobs
Acceptance tests:
	•	run 50+ concurrent runs with guardrails
	•	outputs trace bundles as CI artifacts
	•	strict mode fails job on violations

Track I — QA Harness + Torture Tests

Owner: Agent I
Inputs: contracts A/B/C above
Outputs: deterministic test harness and regression suite
Acceptance tests:
	•	large payload streaming tests
	•	signal propagation tests
	•	crash recovery tests
	•	loop detection tests
	•	secret leakage tests (ensure none in logs)

⸻

17) Milestones & Backlog (Milestone-based)

Milestone 0 — Repo + Contracts (1–2 days)
	•	finalize event schema (Contract A)
	•	finalize policy schema (Contract B)
	•	finalize decision/error schema (Contract C)
	•	publish “Interface Pack” doc for parallel tracks

Milestone 1 — v0.1 Desktop Observe (1–2 weeks)
	•	shim stdio pass-through + bounded logging + signal supervision
	•	ledgerd + SQLite + tail/query
	•	importer/restore for Claude and Codex
	•	run wrapper with run_id stamping

Exit criteria
	•	install → import → first log < 5 minutes
	•	no zombies
	•	no OOM on large payloads
	•	tool naming preserved
	•	secrets not leaked (even if only env injection v0.1)

Milestone 1.5 — Headless CI/Container proof (1 week)
	•	strict policy mode
	•	stdout JSONL streaming
	•	artifact export

Exit criteria
	•	50 concurrent jobs stable
	•	policy violations fail jobs
	•	trace artifacts generated

Milestone 2 — Guardrails (2–3 weeks)
	•	token buckets, budgets, breakers, dedupe windows
	•	policy diff + explain
	•	secret broker improvements

Milestone 3 — Control (Reject-with-Hint) (2–4 weeks)
	•	REJECT_WITH_HINT return format
	•	hint ledger + hint UI
	•	“promote hint to rule” workflow

Milestone 4 — K8s sidecar prototype (future)
	•	sidecar packaging
	•	policy snapshot caching
	•	remote collector

⸻

18) What Subluminal enables (operator reality)

With Desktop + CI + Sidecar profiles, Subluminal becomes:
	•	your personal “agent ops” cockpit (run 50 repos, triage exceptions)
	•	a team governance layer (policy-as-code diff in PRs)
	•	a production fleet firewall (kill-switch + local enforcement)

And because the core is the data plane + stable contracts, you’re not betting your company on MCP staying trendy.

⸻

Final naming cleanup

Since the product is Subluminal, here’s a clean CLI style:
	•	sub import …
	•	sub run …
	•	sub shim …
	•	sub ledgerd
	•	sub tail
	•	sub query
	•	sub policy …
	•	sub secrets …

Keep the word “gateway” in internal architecture docs if useful, but not in user-facing commands. It’ll age better.

⸻

If you want, I can also output a one-page “Interface Pack” (event schema fields + policy schema + decision/error schema) as a standalone doc to feed directly into parallel coding agents so they don’t diverge on contracts.