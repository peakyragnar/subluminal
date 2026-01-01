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

RULES_FILE="$MAIN_REPO/docs/agent-runtime-rules.md"
AGENT_RULES="\
- Make the minimal correct change for the bead
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
LAST_FAIL_SIG=""
REPEAT_FAILS=0

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
$AGENT_RULES

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
      echo "[bead_pr] ERROR: Same failure repeated $REPEAT_FAILS times; stopping"
      echo "[bead_pr] Failure summary:"
      echo "$FAIL_SUMMARY"
      exit 4
    fi

    FAIL_CONTEXT="Previous CI run failed. Summary of failure:

\`\`\`
$FAIL_SUMMARY
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
