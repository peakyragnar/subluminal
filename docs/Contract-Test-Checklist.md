Here’s the Contract Test Checklist — a compact, “one-page” mapping from each Interface Pack requirement → a concrete test you can automate. This is what you give to parallel coding agents so nobody “interprets” the contracts differently.

I’m labeling tests P0/P1 (P0 = must pass for v0.1 to ship) and tagging the most relevant Track owner (A/B/C/… from the parallelization plan).

⸻

Subluminal Contract Test Checklist v0.1

Format: Each row is a single test case.
Harness assumption: You have (1) a fake MCP tool server (stdio), (2) a shim under test, (3) a minimal “agent client” driver that sends/receives JSON-RPC over stdio, (4) an event sink (stdout JSONL or ledger ingest).

Tip: Maintain a fixtures/ folder with: canonical JSON inputs, expected event JSONL “goldens,” and policy bundles.

Test ID	Priority	Contract Requirement	Track Owner	Setup / Fixture	Stimulus	Expected Assertions
EVT-001	P0	JSONL single-line events (A §1.1)	A,C	Event sink captures emitted events	Run one tool call end-to-end	Every emitted event is exactly 1 line JSON; no multi-line JSON objects
EVT-002	P0	Required envelope fields (A §1.3)	A,C	Minimal policy loaded	Run one tool call	Each event contains: v,type,ts,run_id,agent_id,client,env,source.{host_id,proc_id,shim_id}
EVT-003	P0	Event ordering & completeness (A §1.2)	A,C	Single run with 1 tool call	Execute tool call	Stream contains: run_start → tool_call_start → tool_call_decision → tool_call_end → run_end (in that order)
EVT-004	P0	run_id present everywhere (A §1.3 / Kickoff Contract A)	A,C,E	sub run wrapper sets identity vars	Execute 3 tool calls	All events have the same run_id; no orphan events without run_id
EVT-005	P0	call_id uniqueness per run (A §0.3, §1.5)	A	Make 100 tool calls	Execute loop of calls	All call.call_id distinct; seq is monotonic starting at 1
EVT-006	P0	tool/server name preservation (spec invariant)	A,D	Upstream tool named linear_create_issue	List tools + call tool	Events show exact upstream server_name + tool_name unchanged; no forced namespacing
EVT-007	P0	latency_ms present and sane (A §1.7)	A,C	Tool server sleeps 200ms	Call tool	tool_call_end.latency_ms ≥ 200 and within tolerance; not negative/zero unless truly instant
EVT-008	P0	status/error class taxonomy (A §1.7)	A,C	Tool server returns JSON-RPC error	Call tool	status=ERROR and error.class is one of allowed enums; no raw stack traces in message
EVT-009	P0	run_end summary counts correct (A §1.8)	C	Run with 5 calls (3 OK, 2 blocked)	Execute with policy blocks	summary.calls_total=5, allowed/blocked counts match observed decisions; duration_ms present
HASH-001	P0	Canonicalization equivalence (A §1.9.1)	B,A	Fixture args A & B with reordered keys	Call same tool twice	args_hash identical across both calls
HASH-002	P0	Canonicalization stability	B	Fixed fixture args	Re-run test multiple times	args_hash exactly matches golden value (precomputed) every time
BUF-001	P0	Bounded inspection: truncate (A §1.10)	A,C	Create args payload > 1 MiB	Call tool once	Shim forwards successfully; emitted events set preview.truncated=true; preview omitted or [TRUNCATED]
BUF-002	P0	No OOM on large payload (A §1.10)	A,I	Stress: 50 large calls concurrently	Run calls	Process RSS stable (bounded), no crashes; forwarding completes; events still emitted
BUF-003	P0	Forwarding correctness under truncation	A	Tool echoes input size	Send large payload	Upstream receives full payload (size matches); shim did not corrupt stream
BUF-004	P1	Rolling hash for truncated payload (A §1.9.2)	A,B	Large payload fixture	Call tool	args_stream_hash present and matches expected SHA-256 of raw bytes (golden)
POL-001	P0	Observe mode: never blocks (B §2.1 mode)	B,A	Policy mode = observe	Trigger “deny” rule should exist but ignored	Decision is ALLOW; still logs rules/policy in run_start
POL-002	P0	Allow/Deny ordering (B §2.3 precedence via order)	B	Policy with deny above allow	Call matching tool	Decision is BLOCK; rule_id = deny rule; explain.reason_code correct
POL-003	P0	Budget rule decrements & blocks on exceed (B §2.5 budget)	B,A	Budget limit_calls=3 on tool	Call tool 4 times	First 3 allowed; 4th BLOCK/REJECT_WITH_HINT/TERMINATE per policy; decision cites correct rule_id
POL-004	P0	Token bucket rate limit (THROTTLE) (B §2.5 rate_limit)	B,A	capacity=2, refill slow, on_limit=THROTTLE backoff=500ms	5 rapid calls	Calls after tokens depleted return THROTTLE decision and JSON-RPC throttle error with backoff_ms
POL-005	P0	Breaker: repeat_threshold triggers (B §2.5 breaker)	B,A	repeat_threshold=5/10s	Loop same args_hash call	Breaker trips at threshold; decision is TERMINATE_RUN or BLOCK; emits breaker_trip (optional)
POL-006	P0	Dedupe window blocks duplicate write-like (B §2.5 dedupe)	B,A	dedupe window 60s, key=args_hash	Same write call twice	Second call BLOCK/REJECT_WITH_HINT; explains duplicate; correct rule_id
POL-007	P1	Tag rule applies risk_class (B §2.5 tag)	B	Tag tool as write_like	Call tool	Subsequent rules matching risk_class evaluate as expected
ERR-001	P0	BLOCK uses JSON-RPC error code -32081 (C §3.2.1)	A,B	Deny rule on tool	Call tool	Response is JSON-RPC error with error.code=-32081 and structured error.data.subluminal fields present
ERR-002	P0	THROTTLE uses error code -32082 + backoff_ms	A,B	Rate limit throttling	Call tool fast	JSON-RPC error.code=-32082; subluminal.backoff_ms present and matches decision
ERR-003	P0	REJECT_WITH_HINT uses -32083 + hint object (C §3.2.4)	A,B	Policy uses REJECT_WITH_HINT	Call tool violating rule	JSON-RPC error.code=-32083; subluminal.hint.{hint_text,hint_kind} present; suggested_args valid JSON if present
ERR-004	P0	No secret leakage in error message/data (Secrets §4 + ERR shapes)	A,F	Secret injection enabled	Trigger policy block	Error payload contains no secret substrings (scan for known secret values); previews also clean
SEC-001	P0	Secret injection: agent never sees secrets (Secrets §4)	F,A	Upstream expects env var token	Run tool call	Upstream succeeds using injected token; captured agent-side args do not include token; event previews do not include token
SEC-002	P0	secret_injection event contains metadata only (optional)	F,C	Enable secret_injection events	Start shim	Event includes {inject_as, secret_ref, source, success}; no values present
PROC-001	P0	SIGINT propagates; no zombie shim (Process supervision)	A,I	Start agent + shim + upstream	Send SIGINT to agent	Shim exits; upstream exits; no orphan processes after grace window
PROC-002	P0	EOF on stdin terminates shim + upstream	A,I	Close agent stdin abruptly	Close pipe	Shim exits cleanly; upstream terminated
PROC-003	P1	Upstream crash handled gracefully	A,I	Upstream segfault/exit mid-run	Call tool	Shim emits tool_call_end ERROR with transport/upstream class; run_end status FAILED/TERMINATED; no deadlock
ID-001	P0	Identity env vars applied (§5)	E,A,C	SUB_RUN_ID, SUB_AGENT_ID, etc. set	One call	Events carry correct run_id,agent_id,env,client,principal/workload as per env vars when provided
ID-002	P1	Workload context tolerance (A §1.3.1)	C	Omit principal/workload fields	Run	Consumers (ledger/UI) do not crash; display “unknown” safely
LED-001	P0	Ledger ingestion durability	C	Ledgerd running, WAL enabled	Ingest 10k events	DB not corrupted; run/call counts correct; indexes used (query is fast)
LED-002	P0	Backpressure drops previews not decisions	C,A	Force ingest overload (slow disk)	Burst events	Decision events persist; preview fields may be dropped/marked truncated; no shim blocking
IMP-001	P0	Importer backup + restore correctness	D	Existing Claude/Codex config present	import then restore	After restore, config identical to original (byte compare); import preserves server names
IMP-002	P0	Time-to-first-log < 5 minutes path	D,E,A,C	Fresh install fixture	import → run agent → call tool	From install completion to first tool_call_start observed < 5 minutes on reference machine
ADAPT-001	P0	Adapter provides required fields to core (§7.1)	A1,A2	Mock adapter implementation	Call via adapter	Core receives server_name, tool_name, args, bytes_in, transport; no protocol-specific data leaks
ADAPT-002	P0	Core is protocol-agnostic (§7.2)	A2	Two different adapters, same policy	Same tool call via each	Identical decisions and events regardless of transport; args_hash matches
ADAPT-003	P1	Adapter formats errors correctly (§7.2)	A1	Policy blocks a call	Return error to client	Client receives valid protocol-specific error (JSON-RPC for MCP); error.data.subluminal fields present


⸻

How to use this checklist with parallel coding agents
	•	Put this table in docs/CONTRACT_TESTS.md.
	•	Create a single “contract tests” harness that can be run by all tracks.
	•	Require each PR to:
	1.	declare which tests it touches
	2.	add/modify goldens only via an explicit “contract change” review

Two “gotcha” notes (so you don’t get bitten later)
	•	Make canonicalization a shared library used by shim + policy + ledger to avoid subtle hash disagreements.
	•	Make truncation behavior deterministic (exact [TRUNCATED] string or exact omission rules), otherwise UI/ledger goldens will drift.
