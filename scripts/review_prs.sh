#!/usr/bin/env bash
set -euo pipefail

# Review and fix open PRs using Codex
# Usage: ./scripts/review_prs.sh [--pr=NUM] [--max-rounds=N]

MAX_ROUNDS="${MAX_ROUNDS:-3}"
CODEX_MODEL="${CODEX_MODEL:-}"
SPECIFIC_PR=""
LABELS="${LABELS:-}"

# Parse arguments
for arg in "$@"; do
  case $arg in
    --pr=*)
      SPECIFIC_PR="${arg#*=}"
      ;;
    --max-rounds=*)
      MAX_ROUNDS="${arg#*=}"
      ;;
    --labels=*)
      LABELS="${arg#*=}"
      ;;
  esac
done

ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

CACHE_ROOT="${XDG_CACHE_HOME:-$HOME/.cache}/subluminal"
export GOCACHE="$CACHE_ROOT/go-build"
export GOMODCACHE="$CACHE_ROOT/gomod"
mkdir -p "$GOCACHE" "$GOMODCACHE"

LOG_DIR="$ROOT/.agent/logs"
mkdir -p "$LOG_DIR"

echo "[review] Max review rounds: $MAX_ROUNDS"
echo ""

get_open_prs() {
  if [[ -n "$SPECIFIC_PR" ]]; then
    echo "$SPECIFIC_PR"
    return
  fi

  local label_filter=""
  if [[ -n "$LABELS" ]]; then
    label_filter="--label $LABELS"
  fi

  # Get PRs created by issue_pr.sh (branches starting with issue/)
  gh pr list --state open --head "issue/*" $label_filter --json number -q '.[].number' 2>/dev/null | head -20
}

review_pr() {
  local pr_num="$1"
  local round="$2"

  echo "[review] PR #$pr_num - Round $round/$MAX_ROUNDS"

  # Get PR details
  local pr_json
  pr_json="$(gh pr view "$pr_num" --json title,body,headRefName,baseRefName,changedFiles)"
  local title=$(echo "$pr_json" | jq -r '.title')
  local branch=$(echo "$pr_json" | jq -r '.headRefName')
  local base=$(echo "$pr_json" | jq -r '.baseRefName')

  echo "[review] Title: $title"
  echo "[review] Branch: $branch"

  # Get the diff
  local diff
  diff="$(gh pr diff "$pr_num" 2>/dev/null || echo "")"

  if [[ -z "$diff" ]]; then
    echo "[review] No diff found, skipping"
    return 1
  fi

  # Get changed files list
  local files
  files="$(gh pr view "$pr_num" --json files -q '.files[].path' | tr '\n' ' ')"

  # Build review prompt
  local prompt="You are reviewing a pull request.

## PR Title
$title

## Changed Files
$files

## Diff
\`\`\`diff
$diff
\`\`\`

## Instructions
Review this PR for:
1. **Correctness**: Logic errors, edge cases, potential bugs
2. **Style**: Go conventions, naming, code organization
3. **Tests**: Missing test coverage for new code
4. **Security**: Potential vulnerabilities (injection, leaks, etc.)
5. **Performance**: Obvious inefficiencies

If you find issues that should be fixed:
- Fix them directly in the code
- Run ./scripts/ci.sh to verify your fixes pass

If the code looks good with no issues:
- Output exactly: LGTM

Focus on issues that matter. Ignore nitpicks like comment formatting unless they're misleading."

  # Checkout the PR branch
  echo "[review] Checking out branch..."
  git fetch origin "$branch" 2>/dev/null
  git checkout "$branch" 2>/dev/null || git checkout -b "$branch" "origin/$branch" 2>/dev/null
  git pull origin "$branch" 2>/dev/null || true

  # Run codex review
  echo "[review] Running Codex review..."

  local review_log="$LOG_DIR/review-pr-${pr_num}-round-${round}.log"

  CODEX_ARGS=(exec --full-auto)
  if [[ -n "$CODEX_MODEL" ]]; then
    CODEX_ARGS+=(-m "$CODEX_MODEL")
  fi
  CODEX_ARGS+=("$prompt")

  local output
  output="$(codex "${CODEX_ARGS[@]}" 2>&1 | tee "$review_log")" || true

  # Check if LGTM
  if echo "$output" | grep -q "LGTM"; then
    echo "[review] PR #$pr_num: LGTM - no issues found"
    return 0
  fi

  # Check if any changes were made
  if [[ -n "$(git status --porcelain)" ]]; then
    echo "[review] Changes detected, running CI..."

    if "$ROOT/scripts/ci.sh" > "$LOG_DIR/review-ci-${pr_num}.log" 2>&1; then
      echo "[review] CI passed, committing fixes..."
      git add -A
      git commit -m "Address review feedback (round $round)

🤖 Generated with [Claude Code](https://claude.com/claude-code)

Co-Authored-By: Codex <noreply@openai.com>"

      echo "[review] Pushing fixes..."
      git push origin "$branch"

      echo "[review] PR #$pr_num: Fixes pushed"
      return 2  # Signal that fixes were made
    else
      echo "[review] CI failed after review fixes"
      echo "[review] See: $LOG_DIR/review-ci-${pr_num}.log"
      git checkout -- . 2>/dev/null || true  # Revert changes
      return 1
    fi
  else
    echo "[review] No changes made by reviewer"
    return 0
  fi
}

# Main loop
PR_NUMS=()
while IFS= read -r pr_num; do
  if [[ -n "$pr_num" ]]; then
    PR_NUMS+=("$pr_num")
  fi
done < <(get_open_prs)

if [[ ${#PR_NUMS[@]} -eq 0 ]]; then
  echo "[review] No open PRs found to review"
  exit 0
fi

echo "[review] Found ${#PR_NUMS[@]} PR(s) to review"
echo ""

# Save current branch to return to
ORIGINAL_BRANCH="$(git rev-parse --abbrev-ref HEAD)"

REVIEWED=0
FIXED=0
FAILED=0

for pr_num in "${PR_NUMS[@]}"; do
  echo "========================================"
  echo "[review] Processing PR #$pr_num"
  echo "========================================"

  for ((round=1; round<=MAX_ROUNDS; round++)); do
    review_pr "$pr_num" "$round"
    result=$?

    if [[ $result -eq 0 ]]; then
      # LGTM or no changes
      ((REVIEWED++)) || true
      break
    elif [[ $result -eq 2 ]]; then
      # Fixes pushed, continue reviewing
      ((FIXED++)) || true
      if [[ $round -eq $MAX_ROUNDS ]]; then
        echo "[review] Max rounds reached for PR #$pr_num"
      fi
    else
      # Error
      ((FAILED++)) || true
      break
    fi
  done

  echo ""
done

# Return to original branch
git checkout "$ORIGINAL_BRANCH" 2>/dev/null || true

echo "========================================"
echo "[review] Summary"
echo "========================================"
echo "PRs processed: ${#PR_NUMS[@]}"
echo "Clean (LGTM):  $REVIEWED"
echo "Fixed:         $FIXED"
echo "Failed:        $FAILED"
