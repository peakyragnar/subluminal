# Beads Automation Plan

**Status:** Canonical workflow for autonomous agent development  
**Scope:** Reusable across any repository with Beads + Codex CLI

---

## Overview

This document defines a fully automated workflow for turning Beads issues into merged PRs using Codex CLI as the implementation engine. The system is designed for solo developers who want agents to grind through work queues while they review PRs.

**Core loop:**
1. Run `worker_pool.sh` → Codex works on "ready" beads → PRs created (beads stay open)
2. Review + merge PRs in GitHub
3. `bd close <id>` after merge
4. Rerun `worker_pool.sh` → dependent beads now "ready"
5. Repeat

---

## Design Decisions

### Beads Mode: Stealth (Local Only)
- Beads is a **personal operator queue**, not a shared artifact
- `.beads/issues.jsonl` is gitignored (stealth mode)
- No `bd sync` to remote; no sync branch
- Issue state lives only on your machine

### Dependency Semantics: Merged = Ready
- If bead B depends on bead A, B cannot start until A is **merged to `main`**
- "Closed" means "merged" — you close beads after merging their PRs
- `bd ready` only returns beads whose blockers are all closed
- Worker pool respects this strictly; no speculative work

### Automation: Fully Automatic to PR
- When CI passes, scripts **push branch + create PR automatically**
- You review PRs in GitHub (not in Beads)
- No human approval gates before PR creation

### CI Validation: Host-Based (No Containers)
- `scripts/ci.sh` runs the exact same steps as GitHub Actions
- No Docker, no `act`, no containers
- Determinism comes from matching GHA steps locally

### Parallelism: One Worktree Per Bead
- Each bead runs in its own git worktree
- Branch naming: `bead/<id>` (e.g., `bead/subluminal-td8`)
- Worktree location: `.worktrees/<id>/`
- Enables safe parallel execution without file conflicts

### PR Deduplication
- Before spawning a worker, check: `gh pr list --head bead/<id>`
- If PR already exists, skip that bead
- Prevents duplicates when rerunning pool before merging

### Data Extraction: JSON Preferred
- Use `bd show <id> --json` for parsing title/description
- PR title = bead title
- PR body = bead description
- Fall back to text parsing only if JSON unavailable

---

## File Structure

```
scripts/
  ci.sh              # Repo-specific: mirrors GitHub Actions exactly
  bead_pr.sh         # Generic: implements one bead, creates PR
  worker_pool.sh     # Generic: spawns N workers for ready beads

.worktrees/          # Gitignored; holds per-bead worktrees
  <bead-id>/         # One worktree per active bead

.agent/              # Gitignored; logs and state
  logs/              # CI logs per bead
```

---

## Scripts Specification

### 1. `scripts/ci.sh` (Repo-Specific)

**Purpose:** Single source of truth for "does CI pass?" — must exactly mirror GitHub Actions.

**Contract:**
- Exit 0 = pass
- Exit non-zero = fail
- All output to stdout/stderr (captured by caller)

**Example for Go repo (this repo):**
```bash
#!/usr/bin/env bash
set -euo pipefail

echo "=== CI: Building binaries ==="
go build -o bin/shim ./cmd/shim
go build -o bin/fakemcp ./cmd/fakemcp

echo "=== CI: Running tests ==="
go test -v ./...

echo "=== CI: All checks passed ==="
```

**Example for Node/TS repo:**
```bash
#!/usr/bin/env bash
set -euo pipefail

npm ci
npm run lint
npm test
npm run build
```

**Example for Python repo:**
```bash
#!/usr/bin/env bash
set -euo pipefail

pip install -r requirements.txt -r requirements-dev.txt
ruff check .
pytest -v
```

---

### 2. `scripts/bead_pr.sh` (Generic)

**Purpose:** Take a single bead ID, implement it via Codex, iterate until CI green, create PR.

**Inputs:**
| Variable | Default | Description |
|----------|---------|-------------|
| `$1` | (required) | Bead ID to implement |
| `MAX_ITERS` | `10` | Max edit→CI cycles before giving up |
| `CODEX_MODEL` | (codex default) | Model override for Codex |

**Algorithm:**
```
1. Parse bead: bd show <id> --json → extract title, description
2. Verify clean state: no uncommitted changes in worktree
3. Loop up to MAX_ITERS:
   a. Run Codex with prompt derived from bead title + description
   b. Run scripts/ci.sh, capture output
   c. If exit 0: break (success)
   d. If exit non-zero: feed CI log tail back to Codex as context
4. If success:
   a. git add -A && git commit -m "<bead title>"
   b. git push -u origin bead/<id>
   c. gh pr create --base main --head bead/<id> \
        --title "<title>" --body "<description>"
5. Exit 0 on PR created, non-zero on failure
```

**Codex invocation:**
```bash
codex exec \
  --full-auto \
  -C "$WORKTREE_DIR" \
  "$PROMPT"
```

Where `--full-auto` = `-a on-request --sandbox workspace-write` (safe defaults for unattended execution).

---

### 3. `scripts/worker_pool.sh` (Generic)

**Purpose:** Spawn up to N parallel workers for ready beads. Run once (no polling).

**Inputs:**
| Variable | Default | Description |
|----------|---------|-------------|
| `WORKERS` | `2` | Max parallel workers |
| `BASE_BRANCH` | `main` | Branch to create worktrees from |

**Algorithm:**
```
1. Ensure main is up-to-date: git fetch origin main
2. Get ready beads: bd ready --json → list of IDs
3. For each ready bead ID:
   a. Skip if PR exists: gh pr list --head bead/<id> --json number
   b. Skip if worktree already exists (stale)
   c. Create worktree: git worktree add .worktrees/<id> -b bead/<id> origin/main
   d. Spawn worker (background): scripts/bead_pr.sh <id> in that worktree
   e. Track job count; wait if at WORKERS limit
4. Wait for all workers to complete
5. Report results: which beads got PRs, which failed
6. Clean up completed worktrees (optional)
```

**Worktree lifecycle:**
- Created when bead is picked
- Removed after PR is created (or on failure)
- `.worktrees/` is gitignored

---

## Beads CLI Reference

Commands used by automation:

| Command | Purpose |
|---------|---------|
| `bd ready` | List beads with no unmet dependencies |
| `bd ready --json` | Same, JSON output |
| `bd show <id>` | Display bead details |
| `bd show <id> --json` | Same, JSON output (preferred) |
| `bd update <id> --status in_progress` | Mark bead as being worked |
| `bd close <id>` | Mark bead as done (you do this after merge) |

**JSON schema for `bd show --json`:**
```json
{
  "id": "subluminal-td8",
  "title": "v0.2 Policy Engine (Guardrails)",
  "description": "Token buckets, budgets, breakers...",
  "status": "open",
  "priority": 0,
  "issue_type": "epic",
  "dependencies": [...]
}
```

---

## GitHub CLI Reference

Commands used by automation:

| Command | Purpose |
|---------|---------|
| `gh pr list --head bead/<id> --json number` | Check if PR exists for branch |
| `gh pr create --base main --head bead/<id> --title "..." --body "..."` | Create PR |
| `gh auth status` | Verify authentication |

---

## Codex CLI Reference

Non-interactive execution for automation:

```bash
# Basic non-interactive run
codex exec "Implement feature X"

# With full-auto (recommended for automation)
codex exec --full-auto "Implement feature X"

# With explicit settings
codex exec \
  --sandbox workspace-write \
  -a on-request \
  -m o3 \
  -C /path/to/worktree \
  "Implement feature X"
```

**Key flags:**
| Flag | Purpose |
|------|---------|
| `--full-auto` | Alias for `-a on-request --sandbox workspace-write` |
| `-a on-request` | Model decides when to ask for approval (rarely) |
| `--sandbox workspace-write` | Allow writes only in workspace |
| `-C <dir>` | Run in specified directory |
| `-m <model>` | Override model |
| `--json` | JSONL event output |
| `-o <file>` | Write last message to file |

---

## Prompt Engineering

The prompt sent to Codex should be structured for success:

```
You are implementing a task in a software repository.

## Task
Title: <bead title>
Description: <bead description>

## Rules
- Make the minimal correct change
- Do NOT push branches or create PRs (the outer script handles that)
- Do NOT modify unrelated files
- Run tests via ./scripts/ci.sh before declaring done
- If tests fail, fix the code (not the tests) unless tests are wrong

## Current CI Status
<if this is a retry, include tail of previous CI failure log>

## Instructions
Implement this task. When done, the outer script will run CI automatically.
```

---

## Operator Workflow

### Initial Setup (Once Per Repo)

```bash
# 1. Install prerequisites
brew install gh         # GitHub CLI
npm i -g @openai/codex  # Codex CLI (or your preferred method)
# Install bd (beads) per https://github.com/steveyegge/beads

# 2. Authenticate
gh auth login
codex login  # if needed

# 3. Initialize Beads (stealth mode)
bd init --stealth

# 4. Create scripts
mkdir -p scripts
# Create scripts/ci.sh (repo-specific)
# Copy scripts/bead_pr.sh (generic)
# Copy scripts/worker_pool.sh (generic)
chmod +x scripts/*.sh

# 5. Update .gitignore
echo ".worktrees/" >> .gitignore
echo ".agent/" >> .gitignore
```

### Daily Operation

```bash
# 1. Plan work into Beads
bd create "Implement feature X"
bd create "Add tests for feature X"
bd dep add <tests-id> <feature-id>  # tests blocked by feature

# 2. Run the worker pool
./scripts/worker_pool.sh

# 3. Review PRs in GitHub
# (PRs appear for all "ready" beads that didn't already have PRs)

# 4. Merge PRs you approve

# 5. Close merged beads
bd close <id>  # repeat for each merged bead

# 6. Rerun pool for newly-ready beads
./scripts/worker_pool.sh

# 7. Repeat until done
```

### Monitoring Progress

```bash
# See what's ready to work
bd ready

# See all open beads
bd list --status open

# See a specific bead
bd show <id>

# See what PRs exist
gh pr list
```

---

## Failure Modes and Recovery

### CI Never Passes (MAX_ITERS Exceeded)
- Worker exits non-zero
- Worktree is left in place for debugging
- Check `.agent/logs/<id>.log` for CI output history
- Manually fix or adjust the bead description and rerun

### Codex Produces Bad Code
- CI catches it (that's the point)
- If Codex thrashes: reduce MAX_ITERS, inspect logs, improve prompt
- Consider breaking the bead into smaller pieces

### Merge Conflicts
- Can happen if two beads touch same files
- Beads deps should prevent this; if not, add deps
- Manual resolution required

### Stale Worktrees
- If pool crashes, worktrees may remain
- Clean up: `git worktree remove .worktrees/<id>`
- Or: `git worktree prune`

### PR Already Exists
- Worker skips that bead (by design)
- If you want to regenerate: close the PR first, delete the branch

---

## Adapting to Other Repos

This workflow is repo-agnostic. To use in a new repo:

1. **Create `scripts/ci.sh`** that mirrors your GitHub Actions
2. **Copy `scripts/bead_pr.sh`** unchanged (it's generic)
3. **Copy `scripts/worker_pool.sh`** unchanged (it's generic)
4. **Initialize Beads:** `bd init --stealth`
5. **Add to `.gitignore`:** `.worktrees/` and `.agent/`

The only repo-specific file is `scripts/ci.sh`.

---

## Configuration Reference

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `WORKERS` | `2` | Parallel workers in pool |
| `MAX_ITERS` | `10` | Max CI retries per bead |
| `BASE_BRANCH` | `main` | Branch to create worktrees from |
| `CODEX_MODEL` | (codex default) | Model for Codex |

### Files

| Path | Purpose |
|------|---------|
| `scripts/ci.sh` | Repo-specific CI script |
| `scripts/bead_pr.sh` | Generic bead→PR script |
| `scripts/worker_pool.sh` | Generic worker pool |
| `.worktrees/` | Per-bead worktrees (gitignored) |
| `.agent/logs/` | CI logs per bead (gitignored) |
| `.beads/` | Beads database (stealth = gitignored) |

---

## Summary

| What | How |
|------|-----|
| Work queue | Beads (`bd ready`) |
| Implementation | Codex CLI (`codex exec --full-auto`) |
| Validation | `scripts/ci.sh` (must match GHA) |
| Parallelism | Git worktrees (one per bead) |
| Output | GitHub PRs |
| Dependency gate | "closed" = "merged" |
| Human involvement | Review + merge PRs, then `bd close` |

This is the simplest possible system that gives you:
- Parallel agent work without conflicts
- Dependency-respecting execution order
- PRs as the review interface
- No containers, no polling, no complexity
