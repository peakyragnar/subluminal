Contract A: Event Stream Schema

  Purpose: Defines how tool call activity is logged as a stream of events.

  Format

  - JSONL (JSON Lines) — one JSON object per line, UTF-8, newline-terminated
  - Trivially streamable to stdout, files, sockets, or collectors

  Required Event Types (lifecycle of a tool call)

  | Event              | When Emitted                                                                     |
  |--------------------|----------------------------------------------------------------------------------|
  | run_start          | Beginning of an agent run; declares metadata, mode, policy                       |
  | tool_call_start    | Tool call initiated; includes args_hash, server_name, tool_name, preview         |
  | tool_call_decision | Enforcement decision made: ALLOW/BLOCK/THROTTLE/REJECT_WITH_HINT/TERMINATE_RUN   |
  | tool_call_end      | Tool call completed; includes status (OK/ERROR/TIMEOUT), latency, result preview |
  | run_end            | Run finished; includes summary stats (calls allowed/blocked, duration)           |

   What is CI (Continuous Integration)?

  CI is an automated gatekeeper for your codebase. Every time you push code to GitHub, CI automatically:

  1. Checks out your code on a fresh machine
  2. Runs your tests (go test ./... in your case)
  3. Reports GREEN (pass) or RED (fail) directly on the PR/commit

  Why this matters for you:
  - You've been running go test ./... manually — CI does this automatically on every push
  - When you spawn parallel agents later, you just watch for the GREEN checkmark instead of running tests yourself
  - Prevents broken code from ever reaching main branch

  What is GitHub Actions?

  GitHub Actions is GitHub's built-in CI system. It works via:

  1. A YAML file in .github/workflows/ that defines what to run
  2. Triggers (e.g., "on every push" or "on pull request")
  3. Runners — GitHub provides free Linux/macOS/Windows VMs that execute your tests