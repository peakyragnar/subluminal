Below is the Subluminal Interface Pack — the “do-not-break” contracts your parallel coding agents should implement against so you don’t get divergence.

This is intentionally written like an RFC: MUST / SHOULD / MAY language, stable field names, and explicit semantics.

⸻

Subluminal Interface Pack v0.1.0

Contracts: A) Event Stream, B) Policy Bundle, C) Decision & Error Shapes
Audience: engineers + parallel coding agents implementing data plane, ledger, UI, importer, and policy tooling.

0) Compatibility rules

0.1 Versioning
	•	This pack uses semantic versioning MAJOR.MINOR.PATCH.
	•	PATCH: clarifications only, no breaking changes.
	•	MINOR: additive fields/events/rules only (backwards compatible).
	•	MAJOR: any breaking rename/semantic change.

0.2 Backward compatibility
	•	Event consumers MUST ignore unknown fields.
	•	Policy compilers MUST error on unknown required rule types (to avoid silent misconfig).
	•	Default behavior for missing optional fields MUST be defined (see below).

0.3 IDs
	•	All IDs are case-sensitive.
	•	run_id MUST be globally unique per run. Format is arbitrary but SHOULD be ULID/UUIDv7-style for time sorting.
	•	call_id MUST be unique within a run and SHOULD be globally unique.

⸻

1) Contract A — Event Stream Schema

1.1 Event transport: JSONL

Event stream format in v0.x is JSONL:
	•	One JSON object per line.
	•	UTF‑8 encoding.
	•	Newline \n terminator.
	•	Event producers MUST NOT emit multi-line JSON.

Reason: trivially streamable to stdout, files, sockets, and scalable collectors.

1.2 Event types (minimum set)

Producers MUST emit the following event types:
	1.	run_start
	2.	tool_call_start
	3.	tool_call_decision
	4.	tool_call_end
	5.	run_end

Optional types (v0.x+):
	•	hint_issued
	•	policy_loaded
	•	secret_injection (metadata only; never values)
	•	shim_health (heartbeat)
	•	breaker_trip

1.3 Common event envelope (required fields)

Every event MUST include:
	•	v (string): interface pack version, e.g. "0.1.0"
	•	type (string): one of the event types
	•	ts (string): ISO 8601 / RFC3339 timestamp in UTC (e.g. 2025-12-15T12:34:56.789Z)
	•	run_id (string)
	•	agent_id (string)
	•	client (string): "claude" | "codex" | "headless" | "custom" | "unknown"
	•	env (string): "dev" | "ci" | "prod" | "unknown"
	•	source (object): identifies producer instance
	•	host_id (string): stable per-machine id (random UUID OK)
	•	proc_id (string): stable per-process id (UUID OK)
	•	shim_id (string): unique per shim instance

1.3.1 Identity/workload context (recommended fields)

Events SHOULD include:
	•	principal (string): who initiated the run (user/service)
	•	workload (object):
	•	Desktop examples:
	•	user (string)
	•	repo_path (string)
	•	CI examples:
	•	ci_provider (string)
	•	job_id (string)
	•	repo (string)
	•	branch (string)
	•	K8s examples:
	•	namespace (string)
	•	service_account (string)
	•	pod (string)
	•	labels (object<string,string>)

Consumers MUST tolerate missing workload.

⸻

1.4 run_start event

Purpose: marks beginning of a run and declares metadata.

Required:
	•	Common envelope fields
	•	run (object):
	•	started_at (string RFC3339) — may match ts
	•	mode (string): "observe" | "guardrails" | "control"
	•	policy (object):
	•	policy_id (string)
	•	policy_version (string)
	•	policy_hash (string) — hash of compiled snapshot content

Recommended:
	•	run.meta (object) freeform:
	•	issue_id, repo, task_type, etc.

⸻

1.5 tool_call_start event

Purpose: tool call is initiated.

Required:
	•	Common envelope fields
	•	call (object):
	•	call_id (string)
	•	server_name (string)
	•	tool_name (string)
	•	transport (string): "mcp_stdio" | "mcp_http" | "http" | "unknown"
	•	args_hash (string) — see §1.9 canonicalization/hashing
	•	bytes_in (integer) — size of request message observed at shim boundary
	•	preview (object):
	•	truncated (bool)
	•	args_preview (string, optional, redacted/truncated)
	•	seq (integer) — monotonically increasing call index within run (starts at 1)

Optional:
	•	call.tags (array) e.g. ["write_like"], ["read_like"]
	•	call.timeout_ms (integer) if known

⸻

1.6 tool_call_decision event

Purpose: records enforcement decision.

Required:
	•	Common envelope fields
	•	call object with:
	•	call_id, server_name, tool_name, args_hash
	•	decision (object):
	•	action (string):
"ALLOW" | "BLOCK" | "THROTTLE" | "REJECT_WITH_HINT" | "TERMINATE_RUN"
	•	rule_id (string, nullable) — id of rule that triggered (or null)
	•	severity (string): "info" | "warn" | "critical"
	•	explain (object):
	•	summary (string) human readable
	•	reason_code (string) stable enum-like string (e.g. "BUDGET_EXCEEDED", "DENYLIST_MATCH")
	•	policy (object):
	•	policy_id, policy_version, policy_hash

Decision-specific fields:
	•	If THROTTLE:
	•	decision.backoff_ms (integer) required
	•	If REJECT_WITH_HINT:
	•	decision.hint (object) required (see §3.3)
	•	If TERMINATE_RUN:
	•	decision.terminate (object) required:
	•	terminate_code (string)
	•	terminate_message (string)

⸻

1.7 tool_call_end event

Purpose: tool call completed (success or error).

Required:
	•	Common envelope fields
	•	call object with:
	•	call_id, server_name, tool_name, args_hash
	•	status (string): "OK" | "ERROR" | "TIMEOUT" | "CANCELLED"
	•	latency_ms (integer)
	•	bytes_out (integer)
	•	preview (object):
	•	truncated (bool)
	•	result_preview (string, optional, redacted/truncated)

Optional error detail:
	•	error (object) if status != OK:
	•	class (string): "upstream_error" | "policy_block" | "timeout" | "transport" | "unknown"
	•	message (string) — safe, no secrets
	•	code (string|int) — upstream code if known
	•	retryable (bool)

⸻

1.8 run_end event

Required:
	•	Common envelope fields
	•	run (object):
	•	ended_at (string RFC3339)
	•	status (string): "SUCCEEDED" | "FAILED" | "TERMINATED" | "CANCELLED"
	•	summary (object):
	•	calls_total (integer)
	•	calls_allowed (integer)
	•	calls_blocked (integer)
	•	calls_throttled (integer)
	•	errors_total (integer)
	•	duration_ms (integer)

⸻

1.9 Canonicalization and hashing (MUST)

To avoid divergence between tracks, hashing rules are strict.

1.9.1 args_hash

args_hash MUST be computed from canonical JSON bytes of the tool call arguments object.

Canonical JSON rules:
	•	UTF‑8.
	•	Objects: keys sorted lexicographically by Unicode codepoint.
	•	No insignificant whitespace.
	•	Numbers MUST be represented in the minimal decimal form without trailing zeros (policy engine may instead hash the exact raw JSON bytes if canonicalization is too heavy — but then it must be consistent across producer/consumer. For v0.1, canonicalization is required.)
	•	Arrays retain order.
	•	Strings use standard JSON escaping.

args_hash = SHA-256(canonical_args_bytes) and encoded as lowercase hex.

1.9.2 Rolling hash for oversized payloads

When payload inspection is truncated (see §1.10), producer SHOULD compute:
	•	args_stream_hash / result_stream_hash as SHA-256 over the raw bytes observed, computed incrementally.

⸻

1.10 Bounded inspection: truncation contract (MUST)

To prevent OOM and latency spikes:

Config defaults (v0.1):
	•	MAX_INSPECT_BYTES = 1_048_576 (1 MiB) per message
	•	MAX_PREVIEW_BYTES = 16_384 (16 KiB) per preview string

Rules:
	•	Producers MUST forward full traffic even if they cannot inspect/log it fully.
	•	If message size > MAX_INSPECT_BYTES:
	•	preview.truncated MUST be true
	•	args_preview / result_preview MUST be omitted or set to "[TRUNCATED]"
	•	metadata MUST still be emitted (bytes_in/bytes_out, hashes if available)

⸻

2) Contract B — Policy Bundle Schema

Policy bundles are authored in YAML or JSON. They compile into a policy snapshot used by shims.

2.1 Policy bundle top-level

Required:
	•	policy_id (string) — stable identifier
	•	version (string) — semantic version or timestamp version
	•	mode (string): "observe" | "guardrails" | "control"
	•	defaults (object):
	•	decision_on_error (string): "ALLOW" | "BLOCK"
(If policy evaluation fails, what happens?)
	•	fail_open_read_tools (bool) — optional; default false
	•	selectors (object) — who this policy applies to (see §2.2)
	•	rules (array) — ordered list of rules (see §2.3)

Recommended:
	•	description (string)
	•	owner (string)
	•	created_at (string RFC3339)

2.2 Policy selectors (targeting)

Policy applies if selectors match identity envelope.

Supported selector fields:
	•	env (array) e.g. ["dev","ci"]
	•	agent_id (array) exact match
	•	client (array)
	•	workload (object) with optional matchers:
	•	namespace (array)
	•	service_account (array)
	•	labels (object<string,string>) exact match on keys
	•	repo (array)
	•	branch (array)

Selector semantics:
	•	If a selector field is omitted, it matches all.
	•	Arrays are OR within a field.
	•	All specified fields are AND together.

⸻

2.3 Rule schema (ordered evaluation)

Rules are evaluated top-to-bottom. First matching rule that emits a decisive action wins, unless rule is of “accumulator” type (budgets/rate) which always updates state.

Required per rule:
	•	rule_id (string) — unique within policy
	•	kind (string): "allow" | "deny" | "budget" | "rate_limit" | "breaker" | "dedupe" | "tag"
	•	enabled (bool)
	•	match (object) — see §2.4
	•	effect (object) — see §2.5
	•	severity (string): "info" | "warn" | "critical"

Optional:
	•	description (string)

Rule precedence guidance
	•	deny SHOULD override allow when both match; easiest is ordering: put denies before allows.
	•	Budgets/rate/breakers MUST run regardless of allow/deny outcomes if enabled (state accumulation), but their enforcement occurs when they trigger.

⸻

2.4 Rule match object

Match fields (all optional, ANDed if present):
	•	server_name (object):
	•	glob (array) e.g. ["git*", "linear"]
	•	regex (array)
	•	tool_name (object):
	•	glob (array)
	•	regex (array)
	•	risk_class (array) e.g. ["read_like","write_like","network_like"]
	•	args (object) — simple predicates:
	•	has_keys (array)
	•	key_equals (object<string, string|number|bool>)
	•	key_in (object<string, array>)
	•	numeric_range (object<string, {min?:number, max?:number}>)
	•	time (object) — optional future:
	•	utc_hours etc.

Notes:
	•	Matchers MUST be “cheap” (constant-time-ish).
	•	Deep JSONPath is out of scope v0.x; keep predicates simple.

⸻

2.5 Rule effect object

allow / deny
	•	effect.action (string): "ALLOW" or "BLOCK"
	•	effect.reason_code (string)
	•	effect.message (string)

budget

Budgets apply to counters in the data plane (locally stateful).
	•	effect.budget (object):
	•	scope (string): "run" | "tool" | "server_tool"
	•	limit_calls (integer, optional)
	•	limit_cost_units (integer, optional)
	•	cost_units_per_call (integer, default 1)
	•	on_exceed (string): "BLOCK" | "REJECT_WITH_HINT" | "TERMINATE_RUN"
	•	hint_text (string, optional)

rate_limit (token bucket)
	•	effect.rate_limit (object):
	•	scope (string): "run" | "tool" | "server_tool"
	•	capacity (integer) — max tokens
	•	refill_tokens (integer)
	•	refill_period_ms (integer)
	•	cost_tokens_per_call (integer, default 1)
	•	on_limit (string): "THROTTLE" | "BLOCK" | "REJECT_WITH_HINT"
	•	backoff_ms (integer, required if on_limit=THROTTLE)
	•	hint_text (string, optional)

breaker
	•	effect.breaker (object):
	•	scope (string): "run" | "tool" | "server_tool"
	•	error_threshold (integer) within window_ms
	•	window_ms (integer)
	•	repeat_threshold (integer) within repeat_window_ms
	•	repeat_window_ms (integer)
	•	on_trip (string): "TERMINATE_RUN" | "BLOCK" | "REJECT_WITH_HINT"
	•	terminate_code (string, optional)
	•	hint_text (string, optional)

dedupe (write-like actions)
	•	effect.dedupe (object):
	•	scope (string): "run" | "tool" | "server_tool"
	•	window_ms (integer)
	•	key (string): "args_hash" | "custom"
(v0.1 supports args_hash only)
	•	on_duplicate (string): "BLOCK" | "REJECT_WITH_HINT"
	•	hint_text (string, optional)

tag

Used to tag tools as read/write risk classes.
	•	effect.tag (object):
	•	add_risk_class (array)

⸻

2.6 Policy snapshot (compiled)

Shims DO NOT evaluate raw YAML on every call. They load a compiled snapshot.

Snapshot MUST include:
	•	policy_id
	•	version
	•	policy_hash (SHA-256 over canonical snapshot bytes)
	•	compiled matcher representations (regex precompiled, globs normalized)
	•	default values expanded

Snapshot MUST be reloadable without restarting shim (desktop hot reload is desired; may be v0.2).

⸻

3) Contract C — Decision & Error Shapes

This contract matters because it prevents “blocked = agent melts down.”

3.1 Decisions (data plane internal output)

Decision object MUST include:
	•	action (ALLOW/BLOCK/THROTTLE/REJECT_WITH_HINT/TERMINATE_RUN)
	•	rule_id (nullable)
	•	severity
	•	reason_code
	•	summary

Decision MAY include:
	•	backoff_ms (THROTTLE)
	•	hint (REJECT_WITH_HINT)
	•	terminate (TERMINATE_RUN)

⸻

3.2 JSON-RPC error for BLOCK / THROTTLE / REJECT_WITH_HINT

When the shim blocks or hints, it must respond in a deterministic, machine-parseable way.

3.2.1 Error shape

For a tool call that is denied or hinted, the shim MUST respond with a JSON-RPC error object:
	•	error.code (int): reserved Subluminal codes
	•	error.message (string): short human-readable
	•	error.data (object): structured payload (below)

Reserved error codes:
	•	-32081 = POLICY_BLOCKED
	•	-32082 = POLICY_THROTTLED
	•	-32083 = REJECT_WITH_HINT
	•	-32084 = RUN_TERMINATED

The exact numbers can change only on MAJOR version.

3.2.2 error.data fields (required)
	•	subluminal (object):
	•	v (string) interface version
	•	action (string) matches decision action
	•	rule_id (string|null)
	•	reason_code (string)
	•	summary (string)
	•	run_id (string)
	•	call_id (string)
	•	server_name (string)
	•	tool_name (string)
	•	args_hash (string)
	•	policy (object):
	•	policy_id, policy_version, policy_hash

3.2.3 THROTTLE fields

If action=THROTTLE:
	•	subluminal.backoff_ms (int) required
	•	subluminal.retry_advice (string) recommended

3.2.4 REJECT_WITH_HINT fields (the moat)

If action=REJECT_WITH_HINT:
	•	subluminal.hint (object) required:
	•	hint_text (string)
	•	suggested_args (object|null) — must be valid JSON object if present
	•	retry_advice (string|null)
	•	hint_kind (string): "ARG_FIX" | "BUDGET" | "RATE" | "SAFETY" | "OTHER"

This enables the agent to self-correct rather than crash.

⸻

3.3 Hint event schema (optional but recommended)

If REJECT_WITH_HINT is used, producers SHOULD emit hint_issued:

Required:
	•	common envelope
	•	call.call_id
	•	hint object as above

⸻

4) Secrets & Injection Interface (v0.1 contract)

This is critical to prevent “agent has the keys.”

4.1 Secret bindings config (per server)

Each server shim MUST support “secret bindings” at spawn time:
	•	server_name
	•	secret_bindings (array):
	•	inject_as (string): env var name in upstream tool server
	•	secret_ref (string): reference name (e.g. "github_token")
	•	source (string): "env" | "keychain" | "file" (v0.1 supports env; others later)
	•	redact (bool): default true

Rules:
	•	The shim MUST NOT log secret values.
	•	The shim MUST NOT expose secret values via previews.
	•	A secret_injection event MAY be emitted with metadata only:
	•	{inject_as, secret_ref, source, success:true/false}

⸻

5) Run/Agent identity env vars (desktop/CI minimum)

To standardize identity across tools, sub run and headless runners SHOULD set:
	•	SUB_RUN_ID
	•	SUB_AGENT_ID
	•	SUB_ENV (dev|ci|prod)
	•	SUB_CLIENT (claude|codex|headless|custom)
	•	SUB_PRINCIPAL (optional)

Shims MUST read these if present and stamp events accordingly.

⸻

6) Acceptance test vectors (contract compliance)

Every track should implement these tests using the same inputs:

6.1 Canonicalization equivalence

Two argument objects with different key order MUST produce the same args_hash.

6.2 Large payload truncation

A payload of size > MAX_INSPECT_BYTES MUST:
	•	be forwarded without corruption
	•	produce events with preview.truncated=true
	•	not OOM

6.3 Signal propagation

Killing the agent client MUST terminate shim and upstream tool server within a defined grace window.

6.4 Secret non-leakage

Injected secrets MUST NOT appear in:
	•	event previews
	•	error messages
	•	stored ledger fields

⸻

7) Adapter Contract (Protocol Abstraction)

This contract defines the boundary between protocol-specific adapters and the protocol-agnostic core layer.

7.1 ToolCallSource Interface

Every adapter MUST provide the core layer with the following for each tool call:

Input (adapter → core):
	•	server_name (string): exact configured server name
	•	tool_name (string): exact upstream tool name
	•	args (JSON object): tool call arguments
	•	bytes_in (integer): size of request message
	•	transport (string): adapter identifier (e.g., "mcp_stdio", "mcp_http", "messages_api")
	•	call_context (object): adapter-specific metadata (optional)

Output (core → adapter):
	•	decision (string): ALLOW | BLOCK | THROTTLE | REJECT_WITH_HINT | TERMINATE_RUN
	•	decision_payload (object): contains error details, hints, backoff info as applicable
	•	If ALLOW: adapter proceeds with forwarding
	•	If not ALLOW: adapter formats protocol-specific error response

7.2 Adapter Responsibilities Matrix

| Responsibility              | Adapter | Core |
|-----------------------------|---------|------|
| Parse protocol messages     | ✓       |      |
| Extract tool call data      | ✓       |      |
| Policy evaluation           |         | ✓    |
| Event emission              |         | ✓    |
| Format error responses      | ✓       |      |
| Forward allowed calls       | ✓       |      |
| Process supervision         | ✓       |      |
| State tracking (budgets)    |         | ✓    |
| Secret injection            |         | ✓    |

7.3 Known Adapters

| Adapter      | Transport Value   | Status  | Notes                          |
|--------------|-------------------|---------|--------------------------------|
| MCP stdio    | "mcp_stdio"       | v0.1    | Primary adapter                |
| MCP HTTP     | "mcp_http"        | Planned | Streamable HTTP transport      |
| Messages API | "messages_api"    | Planned | Anthropic API wrapper          |
| HTTP proxy   | "http"            | Future  | Generic HTTP tool calls        |

7.4 Adapter Compliance

All adapters MUST:
	•	Produce identical events for identical tool calls (regardless of transport)
	•	Use the shared canonicalization library for args_hash computation
	•	Not make policy decisions (delegate to core)
	•	Handle transport-specific process lifecycle (signals, EOF, connections)

Adapters MUST NOT:
	•	Modify event schema
	•	Bypass core layer for enforcement
	•	Invent new decision types

⸻

What to hand to parallel coding agents

This pack is the canonical source. Each agent should:
	•	implement their track
	•	validate against §6 acceptance vectors
	•	never invent field names (only extend via MINOR proposal)

If you’d like, I can also produce a “Contract Test Checklist” as a one-page table mapping each contract requirement to a concrete test case, so your agent swarm doesn’t interpret things differently.