You don’t need to pick one. You can run a “two-brain” pipeline:
	•	Codex = Control Plane (Founder Lens + planning + decomposition + code review + security review + integration sequencing)
	•	Claude Code = Data Plane (implementation + tests + mechanical edits, enforced by hooks)

That split matches what you observed: Codex tends to be sharper at structured planning/detail, while Claude Code has great “in-repo” ergonomics (hooks, subagents, workflow features).

Below is the parallelized, scalable (10 → 1000+) production loop plus the corrected “single source of truth” setup (yes: CLAUDE.md should literally import AGENTS.md via @AGENTS.md).

⸻

0) Fix the “one source of truth” confusion (and the repo’s current filenames)

You’re right to be confused: if we want one canonical instruction file, then Claude must import it.
	•	Codex reads AGENTS.md automatically and supports a layered “instruction chain” with overrides/fallbacks.
	•	Claude Code automatically reads CLAUDE.md, and CLAUDE.md can import other files using @path/to/import syntax.

Important nuance for your repo

Your repo currently contains Agents.md and Claude.md (capitalized, not all-caps).
To avoid cross-tool surprises:

Best practice: rename them to the canonical filenames:
	•	Agents.md → AGENTS.md
	•	Claude.md → CLAUDE.md

This avoids relying on Codex “fallback filename” config and avoids Claude missing the file on Linux CI. Codex can be configured with fallback filenames if you insist on keeping Agents.md, but canonical names are the lowest-friction path.

⸻

1) The parallelized production loop (Codex planner + Claude implementer) — every step

The core idea

You scale by making the unit of work tiny and isolatable, then running many copies of the same loop:
	•	One task = one worktree + one plan + one report + one reviewer pass
	•	You parallelize only the tasks with minimal file overlap
	•	You integrate in dependency order (merge queue), not by vibes

Artifacts (the “Founder UI”)

For every task <ID> (use Beads ID if enabled, otherwise your own ID), you maintain:
	•	docs/ai/plans/<ID>.md  → the smallest plan (pre-code gate)
	•	docs/ai/reports/<ID>.md → the Founder Report (your primary interface)
	•	Branch: ai/<ID> (in its own worktree)

The report is the thing you read. Agents read diffs.

Your repo already has a docs/ folder and test/contract/ (great for contract-style tests).

⸻

2) Parallelization model that doesn’t devolve into “agent spaghetti”

Roles (team structure)

Codex roles (Control Plane):
	1.	FounderLens Planner (Codex)
	•	applies Musk 5-step to requirements & scope
	•	produces the smallest viable plan (and nothing else)
	2.	Decomposer (Codex)
	•	splits plan into tasks with explicit dependency edges + file ownership
	3.	Code Reviewer (Codex)
	•	reads diffs, maps to plan/spec, finds risk and unnecessary complexity
	4.	Security Reviewer (Codex)
	•	threat-models, checks for sharp edges, unsafe patterns, secret handling
	5.	Integrator (Codex)
	•	orders merges, resolves conflicts, ensures full suite green on integration branch

Claude Code roles (Data Plane):
6. Implementer (Claude Code)
	•	writes tests-first (red/green/prove), implements minimally, updates report

	7.	Fixer (Claude Code)
	•	handles review findings and test failures quickly

Claude can also run subagents, but since you’re using Codex for planning/review, Claude’s job stays “hands on keyboard.”

⸻

3) The per-task production loop (repeat forever)

Step 1 — Create/choose a task ID
	•	If using Beads: task ID is the bead like subl-123 (whatever prefix you choose)
	•	If not: generate something like 2025-12-18-auth-cache or TASK-042

Step 2 — Codex “Founder Lens” produces the smallest plan (NO CODE)

Codex writes/updates: docs/ai/plans/<ID>.md

Founder Lens must apply Musk 5-step:
	1.	Question requirements (who owns each requirement)
	2.	Delete until it breaks (then add back)
	3.	Simplify only after deletion
	4.	Accelerate cycle time
	5.	Automate last

This is your “anti-bloat engine”.

Codex should also decide:
	•	What can be parallelized safely (low overlap)
	•	What must be serialized (shared files / shared APIs)

Step 3 — Codex decomposition into parallel tasks (if needed)

Codex creates a task graph:
	•	Task A, B, C…
	•	For each: owned files, explicit dependencies, and a “definition of done”

If two tasks touch the same core files: don’t parallelize them. Split differently.

Step 4 — Create worktrees (one per task)

For each task <ID>:
	•	branch: ai/<ID>
	•	worktree dir: ../.worktrees/<ID> (or wherever you like)

Now you can run 10–1000 tasks concurrently because they’re physically separated.

Step 5 — Claude Code implementation (in the worktree)

Claude starts in that worktree directory.

Claude must:
	•	Read the plan
	•	Create tests derived from spec/contracts
	•	Run tests and capture RED evidence
	•	Implement minimal change
	•	Run tests and capture GREEN evidence
	•	Prove tests (small mutation / deliberately break then revert) and capture evidence

Step 6 — Codex code review (through your eyes)

Codex reads:
	•	the plan
	•	the report
	•	the diff

Codex writes its review into the report (not just scattered comments):
	•	What changed and why
	•	What to delete/simplify
	•	Risks / edge cases
	•	Spec/test mapping gaps

Step 7 — Codex security review

Same: write into report:
	•	input validation, path handling, command injection, unsafe defaults, secrets
	•	dependency creep
	•	logging of sensitive data

Step 8 — Claude fixes review findings

Claude applies smallest fixes needed, reruns tests, updates report.

Step 9 — Integration merge queue (serialized, always)

Even with 1000 worktrees, you integrate in an ordered queue:
	•	Merge tasks with no dependencies first
	•	Then dependent tasks
	•	After each merge: run full test suite on the integration branch

Step 10 — Final FounderLens summary

Codex writes the “executive summary” into the top-level report:
	•	what shipped
	•	what got deleted
	•	what’s intentionally not done
	•	operational notes (rollout, rollback)

⸻

4) Beads: worth integrating?

I agree it’s worth integrating if you are serious about running parallel agents at scale.

Why:
	•	Beads is explicitly designed as a lightweight, git-backed issue/graph system for agents, improving long-horizon continuity and work discovery by moving from “markdown plan dementia” to a queryable task graph.
	•	The author’s own best-practices explicitly recommend using a plan outside Beads, then importing it into Beads as epics/issues with dependencies and parallelization. That fits your harness perfectly.

Pragmatic integration stance:
	•	Keep plans/reports as human-readable markdown (your Founder UI)
	•	Use Beads as the shared task graph + memory between many agents/worktrees
	•	Run Beads hygiene (bd doctor, bd cleanup, bd sync, bd upgrade) as part of your routine when you scale.

Beads is new and still evolving (the author calls it early/alpha in the intro post), so treat it as an optional accelerator, not a single point of failure.

⸻

5) The corrected scaffolding files (single source of truth, parallel-ready)

File 1: AGENTS.md (canonical — the source of truth)

Put this at repo root (rename your existing Agents.md to this). Your repo already has a Go module + contract test directory, so the commands reflect that lightly.

# AGENTS.md — Agent Harness (Founder-Lens, Parallel, Production-Grade)

This file is the **single source of truth** for all coding agents working in this repo.
- Codex reads AGENTS.md automatically (and supports layered overrides).
- Claude Code should import this file from CLAUDE.md.

## Prime Directive
Ship production-grade code changes that are:
1) correct, 2) minimal, 3) well-tested, 4) secure, 5) explainable to a founder.

**Human (Founder) does NOT read diffs.**
Agents must write a clear report explaining what changed and why.

---

## Founder Lens (Elon Musk 5-Step Algorithm)
Apply this lens before proposing code changes:

1) **Make requirements less dumb**
   - Question every requirement.
   - Every requirement must have a named owner (a person), not a department.
   - If the owner can’t justify it, it’s suspicious.

2) **Delete**
   - Delete parts/process steps until something breaks.
   - If you’re not adding back ~10% of the time, you aren’t deleting enough.

3) **Simplify/Optimize**
   - Only after deletion.
   - Don’t optimize something that shouldn’t exist.

4) **Accelerate cycle time**
   - Speed up only after steps 1–3.

5) **Automate**
   - Last. Never automate “fluff”.

This lens is mandatory for planning and review writeups.

---

## Roles (who does what)
### Codex = Control Plane
- FounderLens Planner: write the smallest plan, apply Founder Lens.
- Decomposer: split into parallel tasks with dependencies + file ownership.
- Code Reviewer: read diffs, enforce simplicity, enforce spec/test mapping.
- Security Reviewer: threat model, flag risky patterns and unsafe defaults.
- Integrator: merge queue ordering + conflict strategy.

### Claude Code = Data Plane
- Implementer: execute an approved plan in a worktree; tests-first; minimal code.
- Fixer: address review findings; stabilize tests; keep changes tight.

---

## Work Unit = Task (parallelizable)
Each task has:
- **Task ID** (prefer a Beads issue ID if enabled; otherwise a slug)
- **Branch**: `ai/<TASK_ID>`
- **Worktree**: separate directory for isolation
- **Plan**: `docs/ai/plans/<TASK_ID>.md`
- **Report**: `docs/ai/reports/<TASK_ID>.md`

---

## Parallelization Rules (how we scale without chaos)
1) **No plan, no code.**
   - A task must have a written plan before any non-doc code edits.

2) **One task owns its files.**
   - Each plan must list the files it intends to touch.
   - If two tasks would touch the same high-churn files, they must be serialized.

3) **Small plans only.**
   - A “small plan” is ≤ 6 steps, each step verifiable.

4) **Merge queue is serialized.**
   - Implementation can be parallel.
   - Integration is ordered, with full tests after each merge.

5) **Reports are the UI.**
   - If it isn’t in the report, it didn’t happen.

---

## The Per-Task Production Loop (repeat forever)
### 0) Create task artifacts
- Create plan file and report file for <TASK_ID>.
- Work in branch `ai/<TASK_ID>` in its own worktree.

### 1) Plan (Codex, FounderLens Planner)
Write `docs/ai/plans/<TASK_ID>.md` with:
- Requirement owners (names)
- What gets deleted / what is explicitly NOT being built
- Smallest plan (≤ 6 steps)
- File ownership list
- Test plan (derived from spec/contracts)

Mark the plan: `Status: APPROVED` when ready to implement.

### 2) Tests first (Claude Implementer)
Tests must be excellent (see below).
Run tests and capture:
- **RED evidence** (tests fail before implementation)
- implement minimal code
- **GREEN evidence** (tests pass)
- **PROVE evidence** (deliberately break or mutate briefly; tests catch it; revert)

All evidence goes into the report.

### 3) Implement minimal code (Claude Implementer)
- Keep changes as small as possible.
- Prefer deletion over addition.
- No new deps without explicit justification in report.

### 4) Code review (Codex Reviewer)
- Read plan + report + diff.
- Write review results into report:
  - correctness risks
  - unnecessary complexity (what to delete)
  - naming/API issues
  - missing tests / missing cases

### 5) Security review (Codex Security Reviewer)
Write into report:
- inputs/outputs, parsing, command execution risk
- secrets handling/logging
- unsafe defaults and privilege boundaries

### 6) Fix & stabilize (Claude Fixer)
- Apply smallest fixes needed.
- Rerun tests, update report evidence.

### 7) Integrate (Codex Integrator)
- Merge to integration branch in dependency order.
- Run full test suite after each merge.

---

## Ensure Tests Are Excellent
Tests must not be vibes. They must prove properties.

How we ensure tests are excellent:

- Tests derived from spec/contract (not invented)
- Tests break first before they pass (RED → GREEN)
- “Prove the test” by breaking the behavior and confirming failure
- Golden values where outputs are known-correct
- Contract checklist mapping tests to requirements
- Mutation testing later (optional future hardening)

Repo hints:
- This is a Go repo (`go.mod` present) and includes `test/contract/`.
- Default test command (unless a task says otherwise): `go test ./...`

---

## Beads (optional, recommended for scale)
If Beads is enabled in this repo, treat it as:
- the shared task graph (dependencies + memory)
- the source of truth for “what’s next”

Suggested hygiene (especially when scaling):
- `bd doctor` regularly
- periodic `bd cleanup` + `bd sync`
- stay current with `bd upgrade`

Plans and reports remain markdown for humans; Beads tracks the work graph.

---

## Default Commands (override per task if needed)
- Tests: `go test ./...`
- Vet: `go vet ./...` (optional)
- Format: `gofmt` on modified `.go` files


⸻

File 2: CLAUDE.md (Claude Code entrypoint that imports AGENTS)

This is the exact “single source of truth” fix you called out.

Claude Code supports importing additional files in CLAUDE.md using @path/to/import syntax.

@AGENTS.md

# Claude Code role in this repo
You are the **Implementer/Fixer** in a parallel agent pipeline.

- Do not invent scope. Execute the approved plan for the current `ai/<TASK_ID>` branch.
- If the plan is missing or not approved, create/update the plan and stop.
- Keep changes minimal. Prefer deletion. Avoid new dependencies.
- Update `docs/ai/reports/<TASK_ID>.md` continuously with:
  - what you changed and why
  - commands you ran
  - test evidence (RED/GREEN/PROVE)
  - any risks or TODOs discovered

Use Claude Code hooks (configured in `.claude/settings.json`) as hard guardrails.


⸻

6) Hooks (parallel-safe guardrails for Claude Code)

Claude Code hooks can deterministically enforce workflow steps (rather than hoping the model “chooses” to comply).
Hooks run at lifecycle events like PreToolUse and Stop, and can block actions (the docs’ file-protection example exits with code 2 to block).
Stop hooks include a stop_hook_active flag to prevent infinite loops.

.claude/settings.json

{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Edit|Write",
        "hooks": [
          {
            "type": "command",
            "command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/plan_gate.py"
          }
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Edit|Write",
        "hooks": [
          {
            "type": "command",
            "command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/format_go.sh"
          }
        ]
      }
    ],
    "Stop": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/stop_report_guard.py"
          }
        ]
      }
    ]
  }
}

.claude/hooks/plan_gate.py

Blocks code edits until the plan exists and is approved. Allows editing the plan/report themselves.

#!/usr/bin/env python3
import json
import os
import re
import subprocess
import sys
from pathlib import Path

def _repo_root() -> Path:
    # Claude Code sets this env var in hooks examples.
    root = os.environ.get("CLAUDE_PROJECT_DIR", "")
    return Path(root) if root else Path.cwd()

def _git_branch(root: Path) -> str:
    try:
        out = subprocess.check_output(
            ["git", "rev-parse", "--abbrev-ref", "HEAD"],
            cwd=str(root),
            text=True,
            stderr=subprocess.DEVNULL,
        ).strip()
        return out
    except Exception:
        return ""

def _task_id_from_branch(branch: str) -> str:
    m = re.match(r"^ai\/(.+)$", branch)
    if m:
        return m.group(1)
    # Fallback: use branch name slug if not following convention.
    return re.sub(r"[^a-zA-Z0-9._-]+", "-", branch)[:64] or "ACTIVE"

def _plan_and_report(root: Path, task_id: str) -> tuple[Path, Path]:
    plan = root / "docs" / "ai" / "plans" / f"{task_id}.md"
    report = root / "docs" / "ai" / "reports" / f"{task_id}.md"
    return plan, report

def _is_doc_file(path: Path, plan: Path, report: Path) -> bool:
    try:
        rp = path.resolve()
    except Exception:
        rp = path
    return rp == plan.resolve() or rp == report.resolve()

def _plan_is_approved(plan_path: Path) -> bool:
    if not plan_path.exists():
        return False
    try:
        text = plan_path.read_text(encoding="utf-8", errors="replace")
    except Exception:
        return False
    return "Status: APPROVED" in text

def main() -> int:
    root = _repo_root()

    data = json.load(sys.stdin)
    tool_input = data.get("tool_input", {})
    file_path = tool_input.get("file_path", "")
    target = (root / file_path).resolve() if file_path else None

    branch = _git_branch(root)
    task_id = _task_id_from_branch(branch)
    plan, report = _plan_and_report(root, task_id)

    # Always allow edits to plan/report (so the agent can unblock itself).
    if target and _is_doc_file(target, plan, report):
        return 0

    # If trying to edit anything else, require approved plan.
    if not _plan_is_approved(plan):
        msg = (
            f"BLOCKED: No approved plan for this task.\n"
            f"- Branch: {branch}\n"
            f"- Expected plan: {plan}\n"
            f"Create/update the plan and include the line `Status: APPROVED`.\n"
            f"Then proceed with code edits."
        )
        print(msg)
        # Exit 2 blocks the tool call (mirrors Claude docs file-protection example behavior).
        return 2

    return 0

if __name__ == "__main__":
    sys.exit(main())

.claude/hooks/format_go.sh

Auto-format changed .go files after edit/write.

#!/usr/bin/env bash
set -euo pipefail

# Claude docs examples show hooks receiving JSON on stdin.
# We'll read file_path and gofmt if it's a .go file.
file_path="$(python3 - <<'PY'
import json, sys
data=json.load(sys.stdin)
print(data.get("tool_input", {}).get("file_path",""))
PY
)"

if [[ "$file_path" == *.go ]]; then
  gofmt -w "$file_path" || true
fi

.claude/hooks/stop_report_guard.py

Forces “report as UI” completion discipline.

#!/usr/bin/env python3
import json
import os
import re
import subprocess
import sys
from pathlib import Path

def _repo_root() -> Path:
    root = os.environ.get("CLAUDE_PROJECT_DIR", "")
    return Path(root) if root else Path.cwd()

def _git_branch(root: Path) -> str:
    try:
        return subprocess.check_output(
            ["git", "rev-parse", "--abbrev-ref", "HEAD"],
            cwd=str(root),
            text=True,
            stderr=subprocess.DEVNULL,
        ).strip()
    except Exception:
        return ""

def _task_id_from_branch(branch: str) -> str:
    m = re.match(r"^ai\/(.+)$", branch)
    if m:
        return m.group(1)
    return re.sub(r"[^a-zA-Z0-9._-]+", "-", branch)[:64] or "ACTIVE"

def _report_path(root: Path, task_id: str) -> Path:
    return root / "docs" / "ai" / "reports" / f"{task_id}.md"

def _report_is_done(report: Path) -> bool:
    if not report.exists():
        return False
    text = report.read_text(encoding="utf-8", errors="replace")
    # Simple, explicit completion marker.
    return "## ✅ Completion Checklist" in text and "[x]" in text

def main() -> int:
    root = _repo_root()
    data = json.load(sys.stdin)

    # Prevent infinite loops: docs explicitly mention stop_hook_active.
    if data.get("stop_hook_active") is True:
        return 0

    branch = _git_branch(root)
    task_id = _task_id_from_branch(branch)
    report = _report_path(root, task_id)

    if not _report_is_done(report):
        print(
            "BLOCKED STOP: Report is not marked complete.\n"
            f"- Expected report: {report}\n"
            "Add a `## ✅ Completion Checklist` section with checked items and include:\n"
            "- RED/GREEN/PROVE test evidence\n"
            "- Commands run\n"
            "- Summary of changes & risks\n"
        )
        # Common pattern: nonzero exit blocks completion; adjust if needed per your hooks behavior.
        return 2

    return 0

if __name__ == "__main__":
    sys.exit(main())

(Stop-hook looping prevention via stop_hook_active is explicitly documented.)

⸻

7) Minimal plan/report templates (so agents always write in the same shape)

docs/ai/plans/_TEMPLATE.md

# Plan: <TASK_ID>
Status: DRAFT
Owner(s): <named humans>

## Founder Lens (Musk 5-step)
- Requirements questioned:
- Deletions:
- Simplifications:
- Cycle-time accelerations:
- Automation (last):

## Smallest Plan (≤ 6 steps)
1.
2.
3.

## File ownership (what this task will touch)
- ...

## Test plan (derived from spec/contract)
- ...

docs/ai/reports/_TEMPLATE.md

# Report: <TASK_ID>

## Executive summary (for founder)
- What changed:
- Why:
- What got deleted:
- Risks / tradeoffs:

## Evidence
### RED (tests fail before fix)
- Command:
- Output snippet:

### GREEN (tests pass after fix)
- Command:
- Output snippet:

### PROVE (break behavior; tests catch it; revert)
- What was changed to prove the test:
- Command/output:

## Code review notes (Codex)
- ...

## Security review notes (Codex)
- ...

## ✅ Completion Checklist
- [ ] Plan approved (`Status: APPROVED`)
- [ ] Tests include spec/contract coverage
- [ ] RED/GREEN/PROVE recorded
- [ ] Code review recorded
- [ ] Security review recorded
- [ ] No unnecessary deps / complexity


⸻

8) How you actually run this in parallel (operational recipe)

A) Start a feature
	1.	Create an integration branch (optional but recommended): feat/<feature>
	2.	Codex writes the high-level plan and decomposes into tasks.

B) Spawn N tasks as worktrees

For each <TASK_ID>:
	•	create ai/<TASK_ID> branch + worktree directory
	•	create plan/report from templates
	•	Codex approves plan (Status: APPROVED)
	•	Claude implements in that worktree

You can literally run 10–1000 terminals/tmux panes. The guardrails keep them from inventing scope.

C) Merge queue

Codex integrator merges tasks back in dependency order (Beads makes the dependency graph explicit).

⸻

The key correction you asked for

Yes: if AGENTS is the single source of truth, then CLAUDE.md should start with @AGENTS.md. Claude Code explicitly supports CLAUDE.md imports using the @path/to/import syntax.

⸻

If you keep running this loop, you’ll notice something very “Google-y” emerging: the quality comes less from genius and more from relentless invariants (small plans, owned files, evidence-driven tests, review + security as mandatory, and reports as the human interface). Codex and Claude just become very fast hands that can’t wiggle out of those invariants.