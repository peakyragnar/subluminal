#!/usr/bin/env bash
set -euo pipefail

WORKERS="${WORKERS:-2}"
BASE_BRANCH="${BASE_BRANCH:-main}"
MAX_ITERS="${MAX_ITERS:-10}"
CODEX_MODEL="${CODEX_MODEL:-}"
LABELS="${LABELS:-ready,agent-ok}"

ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

WORKTREE_DIR="$ROOT/.worktrees"
mkdir -p "$WORKTREE_DIR"

echo "[pool] Workers: $WORKERS"
echo "[pool] Base branch: $BASE_BRANCH"
echo "[pool] Labels filter: $LABELS"
echo "[pool] Worktree dir: $WORKTREE_DIR"
echo ""

echo "[pool] Fetching latest $BASE_BRANCH..."
git fetch origin "$BASE_BRANCH"

get_ready_issues() {
  gh issue list --label "$LABELS" --state open --json number -q '.[].number' 2>/dev/null | head -20
}

pr_exists() {
  local branch="$1"
  local count
  count="$(gh pr list --head "$branch" --json number 2>/dev/null | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")"
  [[ "$count" -gt 0 ]]
}

READY_ISSUES=()
while IFS= read -r issue_num; do
  if [[ -n "$issue_num" ]]; then
    READY_ISSUES+=("$issue_num")
  fi
done < <(get_ready_issues)

if [[ ${#READY_ISSUES[@]} -eq 0 ]]; then
  echo "[pool] No ready issues found (looking for labels: $LABELS)"
  exit 0
fi

echo "[pool] Found ${#READY_ISSUES[@]} ready issue(s)"
echo ""

JOBS_IDS=()
JOBS_PIDS=()
SPAWNED=0
SKIPPED=0

spawn_worker() {
  local issue_num="$1"
  local branch="issue/$issue_num"
  local wt_path="$WORKTREE_DIR/issue-$issue_num"

  if pr_exists "$branch"; then
    echo "[pool] SKIP #$issue_num: PR already exists for $branch"
    ((SKIPPED++)) || true
    return 1
  fi

  if [[ -d "$wt_path" ]]; then
    echo "[pool] SKIP #$issue_num: Worktree already exists at $wt_path"
    echo "[pool]   (Remove with: git worktree remove $wt_path)"
    ((SKIPPED++)) || true
    return 1
  fi

  echo "[pool] Creating worktree for issue #$issue_num..."
  git worktree add "$wt_path" -b "$branch" "origin/$BASE_BRANCH" 2>/dev/null || {
    git branch -D "$branch" 2>/dev/null || true
    git worktree add "$wt_path" -b "$branch" "origin/$BASE_BRANCH"
  }

  echo "[pool] Spawning worker for issue #$issue_num (worktree: $wt_path)"

  (
    cd "$wt_path"
    MAX_ITERS="$MAX_ITERS" CODEX_MODEL="$CODEX_MODEL" BASE_BRANCH="$BASE_BRANCH" \
      "$ROOT/scripts/issue_pr.sh" "$issue_num"
    exit_code=$?

    if [[ $exit_code -eq 0 ]]; then
      echo "[pool] Cleaning up worktree for issue #$issue_num..."
      cd "$ROOT"
      git worktree remove "$wt_path" 2>/dev/null || true
    fi

    exit $exit_code
  ) &

  JOBS_IDS+=("$issue_num")
  JOBS_PIDS+=("$!")
  ((SPAWNED++)) || true
  return 0
}

wait_for_slot() {
  while [[ ${#JOBS_IDS[@]} -ge $WORKERS ]]; do
    for idx in "${!JOBS_IDS[@]}"; do
      pid="${JOBS_PIDS[$idx]}"
      if ! kill -0 "$pid" 2>/dev/null; then
        wait "$pid" || true
        unset "JOBS_IDS[$idx]"
        unset "JOBS_PIDS[$idx]"
        return 0
      fi
    done
    sleep 1
  done
}

for issue_num in "${READY_ISSUES[@]}"; do
  wait_for_slot
  spawn_worker "$issue_num" || true
done

echo ""
echo "[pool] Waiting for all workers to complete..."
for idx in "${!JOBS_IDS[@]}"; do
  issue_num="${JOBS_IDS[$idx]}"
  pid="${JOBS_PIDS[$idx]}"
  if wait "$pid"; then
    echo "[pool] #$issue_num: SUCCESS"
  else
    exit_code=$?
    echo "[pool] #$issue_num: FAILED (exit $exit_code)"
  fi
done

echo ""
echo "========================================"
echo "[pool] Summary"
echo "========================================"
echo "Ready issues:  ${#READY_ISSUES[@]}"
echo "Spawned:       $SPAWNED"
echo "Skipped:       $SKIPPED"
echo ""
echo "[pool] Done. Review PRs at: gh pr list"
