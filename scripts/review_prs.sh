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

  # Get PRs created by issue_pr.sh (branches starting with issue/)
  # Filter in jq since gh CLI doesn't support glob in --head
  local jq_filter='.[] | select(.headRefName | startswith("issue/")) | .number'

  if [[ -n "$LABELS" ]]; then
    gh pr list --state open --label "$LABELS" --json number,headRefName -q "$jq_filter" 2>/dev/null | head -20
  else
    gh pr list --state open --json number,headRefName -q "$jq_filter" 2>/dev/null | head -20
  fi
}

# Check GitHub Actions status for a PR
# Returns: 0=all passed, 1=some failed, 2=pending/in-progress
check_gh_actions() {
  local pr_num="$1"
  local checks_json
  checks_json="$(gh pr checks "$pr_num" --json name,state,conclusion 2>/dev/null)" || return 2

  local failed pending
  failed="$(echo "$checks_json" | jq -r '[.[] | select(.conclusion == "failure")] | length')"
  pending="$(echo "$checks_json" | jq -r '[.[] | select(.state == "pending" or .state == "in_progress" or .state == "queued")] | length')"

  if [[ "$failed" -gt 0 ]]; then
    return 1
  elif [[ "$pending" -gt 0 ]]; then
    return 2
  else
    return 0
  fi
}

# Wait for GitHub Actions to complete (with timeout)
wait_for_gh_actions() {
  local pr_num="$1"
  local timeout="${2:-300}"  # Default 5 minutes
  local interval=15
  local elapsed=0

  echo "[review] Waiting for GitHub Actions to complete..."

  while [[ $elapsed -lt $timeout ]]; do
    check_gh_actions "$pr_num"
    local status=$?

    if [[ $status -eq 0 ]]; then
      echo "[review] GitHub Actions: all checks passed"
      return 0
    elif [[ $status -eq 1 ]]; then
      echo "[review] GitHub Actions: some checks failed"
      gh pr checks "$pr_num" 2>/dev/null | grep -E "fail|❌" | head -5
      return 1
    fi

    # Still pending
    sleep $interval
    elapsed=$((elapsed + interval))
    echo "[review] GitHub Actions: still running... (${elapsed}s/${timeout}s)"
  done

  echo "[review] GitHub Actions: timeout waiting for checks"
  return 2
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

  # Check GitHub Actions status
  local gh_actions_status=""
  local checks_json
  checks_json="$(gh pr checks "$pr_num" --json name,state,conclusion 2>/dev/null)" || true
  if [[ -n "$checks_json" ]]; then
    local failed_checks
    failed_checks="$(echo "$checks_json" | jq -r '.[] | select(.conclusion == "failure") | "- \(.name): FAILED"' 2>/dev/null)" || true
    if [[ -n "$failed_checks" ]]; then
      gh_actions_status="
## GitHub Actions Status (FAILING)
The following CI checks have FAILED. You MUST investigate and fix these:
$failed_checks

To see full failure logs: gh pr checks $pr_num --web
"
    fi
  fi

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
$gh_actions_status
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

      # Wait for GitHub Actions to complete after pushing
      echo "[review] Waiting for GitHub Actions after push..."
      wait_for_gh_actions "$pr_num" 300
      gh_status=$?

      if [[ $gh_status -eq 0 ]]; then
        echo "[review] PR #$pr_num: Fixes pushed and GitHub Actions passed"
        return 0  # All good, no more review needed
      elif [[ $gh_status -eq 1 ]]; then
        echo "[review] PR #$pr_num: GitHub Actions failed after fixes"
        # Get failed check details for next review round
        gh pr checks "$pr_num" 2>/dev/null | grep -E "fail|❌" | head -5
        return 2  # Signal that more fixes are needed
      else
        echo "[review] PR #$pr_num: Fixes pushed (GitHub Actions still pending/timeout)"
        return 2  # Continue to next round to check again
      fi
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
    # Disable errexit for review_pr since it uses exit codes as signals
    set +e
    review_pr "$pr_num" "$round"
    result=$?
    set -e

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
