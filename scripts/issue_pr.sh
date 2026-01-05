#!/usr/bin/env bash
set -euo pipefail

ISSUE_NUM="${1:-}"
if [[ -z "$ISSUE_NUM" ]]; then
  echo "Usage: $0 <issue-number>"
  echo "Run this from within a worktree for the issue"
  exit 1
fi

MAX_ITERS="${MAX_ITERS:-10}"
CODEX_MODEL="${CODEX_MODEL:-}"
BASE_BRANCH="${BASE_BRANCH:-main}"

ROOT="$(git rev-parse --show-toplevel)"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MAIN_REPO="$(cd "$SCRIPT_DIR/.." && git rev-parse --show-toplevel 2>/dev/null || echo "$ROOT")"

CACHE_ROOT="${XDG_CACHE_HOME:-$HOME/.cache}/subluminal"
export GOCACHE="$CACHE_ROOT/go-build"
export GOMODCACHE="$CACHE_ROOT/gomod"
mkdir -p "$GOCACHE" "$GOMODCACHE"

LOG_DIR="$MAIN_REPO/.agent/logs"
mkdir -p "$LOG_DIR"
CI_LOG="$LOG_DIR/issue-${ISSUE_NUM}.log"

RULES_FILE="$MAIN_REPO/docs/agent-runtime-rules.md"
AGENT_RULES="\
- Make the minimal correct change for the issue
- Do not modify unrelated files
- Do not push branches or create PRs (the outer script handles that)
- If tests fail, fix the code unless tests are clearly wrong
- Focus on making ./scripts/ci.sh pass
- Stop after 3 repeated failures on the same error and report"

if [[ -f "$RULES_FILE" ]]; then
  AGENT_RULES="$(cat "$RULES_FILE")"
fi

summarize_ci_log() {
  local log_path="$1"
  python3 - "$log_path" <<'PY'
import re
import sys

path = sys.argv[1]
try:
    lines = open(path, errors="ignore").read().splitlines()
except FileNotFoundError:
    lines = []

patterns = [
    r"^--- FAIL:",
    r"^FAIL\b",
    r"^panic:",
    r"^panic ",
    r"^\s*error:",
    r"^\s*Error:",
    r"\bFATAL\b",
    r"\bERROR\b",
    r"undefined:",
    r"cannot find",
    r"not found",
    r"expected",
    r"got",
    r"build failed",
    r"cannot ",
    r"no such file",
]

hits = []
for line in lines:
    if any(re.search(p, line) for p in patterns):
        hits.append(line)

if not hits:
    hits = lines[-40:]

out = []
seen = set()
for line in hits:
    if line not in seen:
        out.append(line)
        seen.add(line)
    if len(out) >= 40:
        break

print("\n".join(out))
PY
}

hash_text() {
  python3 - <<'PY'
import hashlib
import sys

data = sys.stdin.read().encode()
print(hashlib.sha256(data).hexdigest())
PY
}

# Fetch issue details from GitHub
ISSUE_JSON="$(gh issue view "$ISSUE_NUM" --json title,body)"
TITLE="$(echo "$ISSUE_JSON" | python3 -c "import sys,json; print(json.load(sys.stdin).get('title','Issue $ISSUE_NUM'))" 2>/dev/null || echo "Issue #$ISSUE_NUM")"
DESCRIPTION="$(echo "$ISSUE_JSON" | python3 -c "import sys,json; print(json.load(sys.stdin).get('body',''))" 2>/dev/null || echo "")"

echo "[issue_pr] Issue: #$ISSUE_NUM"
echo "[issue_pr] Title: $TITLE"
echo "[issue_pr] Max iterations: $MAX_ITERS"

if [[ -n "$(git status --porcelain)" ]]; then
  echo "[issue_pr] ERROR: Working tree has uncommitted changes"
  git status --short
  exit 1
fi

# Mark issue as in-progress
gh issue edit "$ISSUE_NUM" --add-label "in-progress" --remove-label "ready" 2>/dev/null || true

FAIL_CONTEXT=""
LAST_FAIL_SIG=""
REPEAT_FAILS=0

for ((i=1; i<=MAX_ITERS; i++)); do
  echo ""
  echo "========================================"
  echo "[issue_pr] Iteration $i/$MAX_ITERS"
  echo "========================================"

  PROMPT="You are implementing a task in a software repository.

## Task
Title: $TITLE
Description: $DESCRIPTION

## Rules
$AGENT_RULES

## Current CI Status
$FAIL_CONTEXT

## Instructions
Implement this task. When done, ensure ./scripts/ci.sh passes."

  echo "[issue_pr] Running Codex..."

  CODEX_ARGS=(exec --full-auto)
  if [[ -n "$CODEX_MODEL" ]]; then
    CODEX_ARGS+=(-m "$CODEX_MODEL")
  fi
  CODEX_ARGS+=("$PROMPT")

  codex "${CODEX_ARGS[@]}" || true

  echo ""
  echo "[issue_pr] Running CI..."
  if "$MAIN_REPO/scripts/ci.sh" > "$CI_LOG" 2>&1; then
    echo "[issue_pr] CI PASSED"
    FAIL_CONTEXT=""
    break
  else
    echo "[issue_pr] CI FAILED (see $CI_LOG)"
    FAIL_SUMMARY="$(summarize_ci_log "$CI_LOG")"
    if [[ -z "$FAIL_SUMMARY" ]]; then
      FAIL_SUMMARY="(no CI output captured)"
    fi
    FAIL_SIG="$(printf '%s' "$FAIL_SUMMARY" | hash_text)"

    if [[ "$FAIL_SIG" == "$LAST_FAIL_SIG" ]]; then
      ((REPEAT_FAILS++))
    else
      LAST_FAIL_SIG="$FAIL_SIG"
      REPEAT_FAILS=1
    fi

    if [[ "$REPEAT_FAILS" -ge 3 ]]; then
      echo "[issue_pr] ERROR: Same failure repeated $REPEAT_FAILS times; stopping"
      echo "[issue_pr] Failure summary:"
      echo "$FAIL_SUMMARY"
      # Remove in-progress label on failure
      gh issue edit "$ISSUE_NUM" --remove-label "in-progress" 2>/dev/null || true
      exit 4
    fi

    FAIL_CONTEXT="Previous CI run failed. Summary of failure:

\`\`\`
$FAIL_SUMMARY
\`\`\`

Fix the issues and try again."
  fi

  if [[ "$i" -eq "$MAX_ITERS" ]]; then
    echo "[issue_pr] ERROR: Max iterations reached without passing CI"
    echo "[issue_pr] See logs: $CI_LOG"
    # Remove in-progress label on failure
    gh issue edit "$ISSUE_NUM" --remove-label "in-progress" 2>/dev/null || true
    exit 2
  fi
done

echo ""
echo "[issue_pr] Self-review before commit..."

# Run self-review if SELF_REVIEW is enabled (default: enabled)
SELF_REVIEW="${SELF_REVIEW:-1}"
REVIEW_ROUNDS="${REVIEW_ROUNDS:-2}"

if [[ "$SELF_REVIEW" == "1" ]]; then
  for ((review_round=1; review_round<=REVIEW_ROUNDS; review_round++)); do
    echo "[issue_pr] Review round $review_round/$REVIEW_ROUNDS"

    # Get current diff
    DIFF="$(git diff HEAD)"
    if [[ -z "$DIFF" ]]; then
      DIFF="$(git diff --cached)"
    fi

    if [[ -z "$DIFF" ]]; then
      echo "[issue_pr] No changes to review"
      break
    fi

    REVIEW_PROMPT="You just implemented a task. Now review your own changes for issues.

## Task
$TITLE

## Your Changes
\`\`\`diff
$DIFF
\`\`\`

## Review Checklist
1. Logic errors or edge cases missed
2. Missing error handling
3. Missing test coverage for new code paths
4. Security issues (injection, leaks)
5. Go conventions violations

If you find issues: fix them and run ./scripts/ci.sh
If code looks good: output exactly LGTM"

    CODEX_ARGS=(exec --full-auto)
    if [[ -n "$CODEX_MODEL" ]]; then
      CODEX_ARGS+=(-m "$CODEX_MODEL")
    fi
    CODEX_ARGS+=("$REVIEW_PROMPT")

    REVIEW_OUTPUT="$(codex "${CODEX_ARGS[@]}" 2>&1)" || true

    if echo "$REVIEW_OUTPUT" | grep -q "LGTM"; then
      echo "[issue_pr] Self-review passed"
      break
    fi

    # Check if fixes were made
    if [[ -n "$(git status --porcelain)" ]]; then
      echo "[issue_pr] Review made fixes, re-running CI..."
      if ! "$MAIN_REPO/scripts/ci.sh" > "$CI_LOG" 2>&1; then
        echo "[issue_pr] CI failed after review fixes, reverting..."
        git checkout -- .
        break
      fi
      echo "[issue_pr] CI passed after review fixes"
    else
      break
    fi
  done
fi

echo ""
echo "[issue_pr] Committing changes..."
git add -A

if git diff --cached --quiet; then
  echo "[issue_pr] WARNING: No changes to commit"
  exit 3
fi

git commit -m "$TITLE

Fixes #$ISSUE_NUM"

echo "[issue_pr] Pushing branch..."
BRANCH="$(git rev-parse --abbrev-ref HEAD)"
git push -u origin "$BRANCH"

echo "[issue_pr] Creating PR..."
gh pr create \
  --base "$BASE_BRANCH" \
  --head "$BRANCH" \
  --title "$TITLE" \
  --body "Fixes #$ISSUE_NUM

$DESCRIPTION"

echo ""
echo "[issue_pr] SUCCESS: PR created for issue #$ISSUE_NUM"
