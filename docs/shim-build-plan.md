# Shim Implementation Plan

## Overview

Build the Subluminal shim - an MCP stdio proxy that intercepts tool calls, emits audit events, and (v0.2+) enforces policy.

## Architecture

```
Agent Client (stdin)  →  [SHIM]  →  Upstream MCP Server (subprocess)
                           ↓
                      stderr (JSONL events)
```

The shim:
1. Reads JSON-RPC from stdin (from agent)
2. Spawns upstream MCP server as subprocess
3. Forwards requests to upstream, relays responses back
4. Emits JSONL events to stderr for every tool call

## File Structure

```
cmd/shim/
    main.go              # Entry point, CLI flags, signal handling

pkg/shim/
    shim.go              # Main orchestrator
    config.go            # Configuration types

pkg/core/
    core.go              # Protocol-agnostic enforcement core
    emitter.go           # Async JSONL event emission to stderr
    identity.go          # run_id, agent_id, source generation
    state.go             # Run state (seq counter, call tracking)

pkg/adapter/mcpstdio/
    adapter.go           # MCP stdio adapter
    jsonrpc.go           # JSON-RPC 2.0 types
    proxy.go             # Bidirectional stdio proxy
    process.go           # Upstream process management
```

## Implementation Phases

### Phase 1: Core Infrastructure
**Files:** `cmd/shim/main.go`, `pkg/core/identity.go`, `pkg/core/emitter.go`

1. Create entry point with CLI flags:
   - `--server-name` (required): Server name for events
   - `--` separator, then upstream command + args
   - Env vars: SUB_RUN_ID, SUB_AGENT_ID, SUB_CLIENT, SUB_ENV

2. Identity generation:
   - Generate ULID for run_id if not provided
   - Generate UUIDs for host_id, proc_id, shim_id
   - Read identity from env vars with defaults

3. Async event emitter:
   - Buffered channel (1000 events)
   - Background goroutine writes to stderr
   - Non-blocking Emit() - drop if queue full
   - Uses existing `pkg/event.SerializeEvent()`

### Phase 2: Process Management
**Files:** `pkg/adapter/mcpstdio/process.go`

1. Spawn upstream process:
   - `exec.Command(upstreamCmd, upstreamArgs...)`
   - Get stdin/stdout pipes
   - Start process in new process group

2. Signal forwarding:
   - Catch SIGINT, SIGTERM
   - Forward to upstream process group
   - Wait for clean exit or force kill after timeout

3. EOF handling:
   - When stdin closes, close upstream stdin
   - Terminate cleanly

### Phase 3: JSON-RPC Proxy
**Files:** `pkg/adapter/mcpstdio/jsonrpc.go`, `pkg/adapter/mcpstdio/proxy.go`

1. JSON-RPC types:
   ```go
   type JSONRPCRequest struct {
       JSONRPC string         `json:"jsonrpc"`
       ID      any            `json:"id"`
       Method  string         `json:"method"`
       Params  map[string]any `json:"params,omitempty"`
   }
   ```

2. Bidirectional proxy:
   - Agent→Shim: `bufio.Scanner` on os.Stdin
   - Shim→Upstream: write to upstream stdin pipe
   - Upstream→Shim: `bufio.Scanner` on upstream stdout
   - Shim→Agent: write to os.Stdout

3. Request tracking:
   - Map request ID → call state
   - Match responses to compute latency

### Phase 4: Tool Call Interception
**Files:** `pkg/core/core.go`, `pkg/core/state.go`

1. Detect `tools/call` method:
   - Extract tool_name from params.name
   - Extract args from params.arguments
   - Compute args_hash using `pkg/canonical.ArgsHash()`

2. State tracking:
   - Atomic seq counter (starts at 1)
   - Track active calls by call_id
   - Accumulate summary counts

3. Event emission:
   - `run_start` on first request
   - `tool_call_start` when call detected
   - `tool_call_decision` (v0.1: always ALLOW)
   - `tool_call_end` when response matched
   - `run_end` on shutdown

### Phase 5: Test Harness Fix
**File:** `pkg/testharness/harness.go`

The current harness has a wiring bug (shimStdout used twice). Fix `startWithShim()`:
- Pass upstream command to shim via args: `--upstream-cmd`
- Shim spawns its own fake server process
- OR: Use a socket/pipe approach

Simpler fix: Create a wrapper script that the harness can pass to shim as upstream.

## Critical Files to Modify/Create

| File | Action | Purpose |
|------|--------|---------|
| `cmd/shim/main.go` | Create | Entry point |
| `pkg/core/core.go` | Create | Enforcement core |
| `pkg/core/emitter.go` | Create | Event emission |
| `pkg/core/identity.go` | Create | Identity generation |
| `pkg/core/state.go` | Create | Run state tracking |
| `pkg/adapter/mcpstdio/adapter.go` | Create | MCP adapter |
| `pkg/adapter/mcpstdio/jsonrpc.go` | Create | JSON-RPC types |
| `pkg/adapter/mcpstdio/proxy.go` | Create | Bidirectional proxy |
| `pkg/adapter/mcpstdio/process.go` | Create | Process management |
| `pkg/testharness/harness.go` | Modify | Fix shim wiring |

## Existing Code to Use

| Package | What to Use |
|---------|-------------|
| `pkg/event` | `SerializeEvent()`, all event type structs |
| `pkg/canonical` | `ArgsHash()` for args hashing |

## Contract Tests to Target (P0)

| Test | What It Validates |
|------|-------------------|
| EVT-001 | JSONL single-line format |
| EVT-002 | Required envelope fields |
| EVT-003 | Event ordering (run_start → ... → run_end) |
| EVT-004 | run_id consistent across events |
| EVT-005 | call_id unique, seq monotonic from 1 |
| EVT-006 | Tool/server name preservation |
| EVT-007 | latency_ms present and sane |
| EVT-009 | run_end summary counts correct |
| PROC-001 | SIGINT forwarded to upstream |
| PROC-002 | EOF terminates cleanly |
| HASH-001 | args_hash canonicalization |

## CLI Interface

```bash
# Basic usage
./bin/shim --server-name=git -- git-mcp-server

# With identity
SUB_AGENT_ID=repo-fixer SUB_ENV=ci ./bin/shim --server-name=git -- git-mcp-server

# Test harness usage
./bin/shim --server-name=test -- ./fake-mcp-server
```

## Event Flow

```
[Shim Start]
    ├─→ emit run_start
    │
[tools/call received]
    ├─→ compute args_hash
    ├─→ increment seq
    ├─→ emit tool_call_start
    ├─→ emit tool_call_decision (ALLOW)
    ├─→ forward to upstream
    │
[response received]
    ├─→ calculate latency_ms
    ├─→ emit tool_call_end
    ├─→ relay to agent
    │
[stdin EOF or SIGINT]
    ├─→ emit run_end with summary
    └─→ exit
```

## Build Command

```bash
go build -o bin/shim ./cmd/shim
```

## Success Criteria

1. `go build ./cmd/shim` succeeds
2. `./bin/shim --server-name=test -- cat` starts and handles JSON-RPC
3. Contract tests in `test/contract/evt_test.go` pass
4. Contract tests in `test/contract/proc_test.go` pass
5. `go run ./cmd/teststatus --layer contract` shows EVT-* and PROC-* green
