#!/usr/bin/env bash
set -euo pipefail

BEAD_ID="${1:-}"
if [[ -z "$BEAD_ID" ]]; then
  echo "Usage: $0 <bead-id>"
  echo "Run this from within a worktree for the bead"
  exit 1
fi

MAX_ITERS="${MAX_ITERS:-10}"
CODEX_MODEL="${CODEX_MODEL:-}"
BASE_BRANCH="${BASE_BRANCH:-main}"

ROOT="$(git rev-parse --show-toplevel)"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MAIN_REPO="$(cd "$SCRIPT_DIR/.." && git rev-parse --show-toplevel 2>/dev/null || echo "$ROOT")"

LOG_DIR="$MAIN_REPO/.agent/logs"
mkdir -p "$LOG_DIR"
CI_LOG="$LOG_DIR/${BEAD_ID}.log"

get_bead_field() {
  local id="$1"
  local field="$2"
  local default="$3"
  
  local json_output
  if json_output="$(bd show "$id" --json 2>/dev/null)"; then
    local value
    value="$(echo "$json_output" | python3 -c "
import sys, json
data = json.load(sys.stdin)
if isinstance(data, list) and len(data) > 0:
    print(data[0].get('$field', '$default'))
elif isinstance(data, dict):
    print(data.get('$field', '$default'))
else:
    print('$default')
" 2>/dev/null)"
    if [[ -n "$value" ]]; then
      echo "$value"
      return 0
    fi
  fi
  echo "$default"
}

TITLE="$(get_bead_field "$BEAD_ID" "title" "$BEAD_ID")"
DESCRIPTION="$(get_bead_field "$BEAD_ID" "description" "")"

echo "[bead_pr] Bead: $BEAD_ID"
echo "[bead_pr] Title: $TITLE"
echo "[bead_pr] Max iterations: $MAX_ITERS"

if [[ -n "$(git status --porcelain)" ]]; then
  echo "[bead_pr] ERROR: Working tree has uncommitted changes"
  git status --short
  exit 1
fi

FAIL_CONTEXT=""

for ((i=1; i<=MAX_ITERS; i++)); do
  echo ""
  echo "========================================"
  echo "[bead_pr] Iteration $i/$MAX_ITERS"
  echo "========================================"

  PROMPT="You are implementing a task in a software repository.

## Task
Title: $TITLE
Description: $DESCRIPTION

## Rules
- Make the minimal correct change
- Do NOT push branches or create PRs (the outer script handles that)
- Do NOT modify unrelated files
- If tests fail, fix the code (not the tests) unless tests are clearly wrong
- Focus on making ./scripts/ci.sh pass

## Current CI Status
$FAIL_CONTEXT

## Instructions
Implement this task. When done, ensure ./scripts/ci.sh passes."

  echo "[bead_pr] Running Codex..."

  CODEX_ARGS=(exec --full-auto)
  if [[ -n "$CODEX_MODEL" ]]; then
    CODEX_ARGS+=(-m "$CODEX_MODEL")
  fi
  CODEX_ARGS+=("$PROMPT")

  codex "${CODEX_ARGS[@]}" || true

  echo ""
  echo "[bead_pr] Running CI..."
  if "$MAIN_REPO/scripts/ci.sh" > "$CI_LOG" 2>&1; then
    echo "[bead_pr] CI PASSED"
    FAIL_CONTEXT=""
    break
  else
    echo "[bead_pr] CI FAILED (see $CI_LOG)"
    FAIL_CONTEXT="Previous CI run failed. Last 100 lines of output:

\`\`\`
$(tail -n 100 "$CI_LOG")
\`\`\`

Fix the issues and try again."
  fi

  if [[ "$i" -eq "$MAX_ITERS" ]]; then
    echo "[bead_pr] ERROR: Max iterations reached without passing CI"
    echo "[bead_pr] See logs: $CI_LOG"
    exit 2
  fi
done

echo ""
echo "[bead_pr] Committing changes..."
git add -A

if git diff --cached --quiet; then
  echo "[bead_pr] WARNING: No changes to commit"
  exit 3
fi

git commit -m "$TITLE"

echo "[bead_pr] Pushing branch..."
BRANCH="$(git rev-parse --abbrev-ref HEAD)"
git push -u origin "$BRANCH"

echo "[bead_pr] Creating PR..."
gh pr create \
  --base "$BASE_BRANCH" \
  --head "$BRANCH" \
  --title "$TITLE" \
  --body "$DESCRIPTION"

echo ""
echo "[bead_pr] SUCCESS: PR created for $BEAD_ID"
