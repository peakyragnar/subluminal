# GitHub Issues Workflow

This project uses GitHub Issues for task tracking, integrated with the `gh` CLI and automation scripts.

## Overview

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Create    │────▶│   Label:    │────▶│    Work     │────▶│  PR Merges  │
│   Issue     │     │   ready     │     │   (manual   │     │  "Fixes #N" │
│             │     │             │     │  or agent)  │     │  auto-close │
└─────────────┘     └─────────────┘     └─────────────┘     └─────────────┘
```

## Labels

| Label | Color | Meaning |
|-------|-------|---------|
| `ready` | Green | Unblocked, well-defined, ready to work |
| `in-progress` | Yellow | Currently being worked on |
| `agent-ok` | Blue | Safe for autonomous agent to tackle |
| `P0` | Red | Critical priority |
| `P1` | Orange | High priority |
| `P2` | Yellow | Medium priority |
| `P3` | Light blue | Low priority |

### When to Use Each Label

**`ready`**: Apply when an issue has:
- Clear requirements
- No blocking dependencies
- Enough context for someone to start immediately

**`agent-ok`**: Apply when an issue is:
- Well-scoped (single feature/fix)
- Has clear acceptance criteria
- Won't require human judgment calls
- Safe to implement autonomously

**`in-progress`**: Applied automatically by scripts, or manually when you start work.

## Manual Workflow

### Find Work

```bash
# All ready issues
gh issue list --label ready

# Ready issues assigned to you
gh issue list --label ready --assignee @me

# Ready issues by priority
gh issue list --label ready,P0
gh issue list --label ready,P1
```

### View Issue Details

```bash
gh issue view 42
```

### Claim an Issue

```bash
gh issue edit 42 --add-label in-progress --remove-label ready
```

### Create an Issue

```bash
# Interactive
gh issue create

# One-liner
gh issue create --title "Add retry logic to API client" --label "task,P2,ready"
```

### Complete an Issue

Create a PR with "Fixes #N" in the title or body:

```bash
gh pr create --title "Add retry logic to API client" --body "Fixes #42"
```

When the PR merges, issue #42 automatically closes.

### Close Without PR

```bash
gh issue close 42 --reason completed
gh issue close 42 --reason "not planned"
```

## Agentic Workflow

Two scripts automate issue-to-PR workflows using Codex (or similar agents).

### Single Issue: `issue_pr.sh`

Processes one issue in the current worktree:

```bash
./scripts/issue_pr.sh 42
```

**What it does:**
1. Fetches issue title and body from GitHub
2. Labels issue as `in-progress`
3. Runs Codex with issue context
4. Runs CI (`./scripts/ci.sh`)
5. If CI fails, feeds error summary back to Codex (up to MAX_ITERS times)
6. **Self-review**: Agent reviews its own changes, fixes issues found
7. On success: commits, pushes, creates PR with "Fixes #42"
8. On failure: removes `in-progress` label, exits with error

**Environment variables:**
- `MAX_ITERS` - Max agent iterations (default: 10)
- `CODEX_MODEL` - Model to use (optional)
- `BASE_BRANCH` - Target branch for PR (default: main)
- `SELF_REVIEW` - Enable self-review before commit (default: 1)
- `REVIEW_ROUNDS` - Max self-review iterations (default: 2)

### Parallel Pool: `agent_pool.sh`

Processes all `ready` + `agent-ok` issues in parallel:

```bash
./scripts/agent_pool.sh
```

**What it does:**
1. Queries GitHub for issues with both `ready` and `agent-ok` labels
2. For each issue:
   - Skips if PR already exists for that branch
   - Creates a git worktree at `.worktrees/issue-N/`
   - Spawns `issue_pr.sh` in background
3. Runs up to WORKERS in parallel
4. Cleans up successful worktrees
5. Reports summary

**Environment variables:**
- `WORKERS` - Parallel worker count (default: 2)
- `LABELS` - Label filter (default: "ready,agent-ok")
- `MAX_ITERS` - Passed to issue_pr.sh (default: 10)
- `CODEX_MODEL` - Passed to issue_pr.sh (optional)
- `BASE_BRANCH` - Target branch (default: main)

**Example:**
```bash
# Run 4 workers, max 5 iterations each
WORKERS=4 MAX_ITERS=5 ./scripts/agent_pool.sh
```

## Workflow Diagram

```
                                    ┌──────────────────┐
                                    │  GitHub Issues   │
                                    │  (source of      │
                                    │   truth)         │
                                    └────────┬─────────┘
                                             │
                     ┌───────────────────────┼───────────────────────┐
                     │                       │                       │
                     ▼                       ▼                       ▼
            ┌────────────────┐     ┌────────────────┐     ┌────────────────┐
            │  Manual Work   │     │  issue_pr.sh   │     │ agent_pool.sh  │
            │  (human)       │     │  (single)      │     │ (parallel)     │
            └───────┬────────┘     └───────┬────────┘     └───────┬────────┘
                    │                      │                      │
                    │                      │     ┌────────────────┤
                    │                      │     │                │
                    │                      ▼     ▼                ▼
                    │              ┌─────────────────────────────────┐
                    │              │  Git Worktrees                  │
                    │              │  .worktrees/issue-N/            │
                    │              └───────────────┬─────────────────┘
                    │                              │
                    │                              ▼
                    │              ┌─────────────────────────────────┐
                    │              │  Codex Agent Loop               │
                    │              │  1. Read issue                  │
                    │              │  2. Implement                   │
                    │              │  3. Run CI                      │
                    │              │  4. Fix if needed               │
                    │              │  5. Repeat until pass           │
                    │              └───────────────┬─────────────────┘
                    │                              │
                    ▼                              ▼
            ┌─────────────────────────────────────────────────────────┐
            │                    Pull Request                         │
            │                    "Fixes #N"                           │
            └─────────────────────────────────────────────────────────┘
                                       │
                                       ▼
            ┌─────────────────────────────────────────────────────────┐
            │                    Merge → Auto-close                   │
            └─────────────────────────────────────────────────────────┘
```

### PR Reviewer: `review_prs.sh`

Reviews and fixes open PRs:

```bash
# Review all open PRs from issue_pr.sh
./scripts/review_prs.sh

# Review specific PR
./scripts/review_prs.sh --pr=45

# Limit review rounds
./scripts/review_prs.sh --max-rounds=2
```

**What it does:**
1. Lists open PRs (branches starting with `issue/`)
2. For each PR: checks out branch, runs Codex review
3. If issues found: fixes them, runs CI, pushes
4. Repeats until LGTM or max rounds reached
5. Returns to original branch

**Environment variables:**
- `MAX_ROUNDS` - Max review iterations per PR (default: 3)
- `CODEX_MODEL` - Model to use (optional)
- `LABELS` - Filter PRs by label (optional)

**Typical flow:**
```bash
# After agent_pool.sh creates PRs
./scripts/review_prs.sh

# Or review as PRs come in
./scripts/review_prs.sh --pr=45
```

## Best Practices

### For Issue Creation

1. **Write clear titles** - Should describe the outcome, not the problem
   - Good: "Add retry logic to HTTP client"
   - Bad: "HTTP sometimes fails"

2. **Include acceptance criteria** - What does "done" look like?

3. **Keep scope small** - One issue = one PR = one logical change

4. **Link related issues** - Use "Depends on #N" or "Related to #N"

### For Labeling

1. **Only label `ready`** when truly unblocked
2. **Only label `agent-ok`** for well-defined, safe tasks
3. **Use priorities sparingly** - Not everything is P0

### For Agents

1. **Review agent PRs** before merging
2. **Start with low-risk issues** to calibrate agent quality
3. **Monitor worktree disk usage** - Clean up `.worktrees/` periodically

## Troubleshooting

### Worktree Already Exists

```bash
# List worktrees
git worktree list

# Remove a specific worktree
git worktree remove .worktrees/issue-42

# Remove all worktrees
rm -rf .worktrees && git worktree prune
```

### Branch Already Exists

```bash
# Delete local branch
git branch -D issue/42

# Delete remote branch
git push origin --delete issue/42
```

### Agent Stuck in Loop

The script stops after 3 identical failures. Check logs at:
```
.agent/logs/issue-42.log
```

## Quick Reference

```bash
# Find ready work
gh issue list --label ready

# Claim issue
gh issue edit 42 --add-label in-progress --remove-label ready

# Create issue
gh issue create --title "..." --label "task,P2,ready"

# View issue
gh issue view 42

# Run single agent
./scripts/issue_pr.sh 42

# Run agent pool
WORKERS=2 ./scripts/agent_pool.sh

# List worktrees
git worktree list

# Clean up worktrees
git worktree remove .worktrees/issue-42
```
