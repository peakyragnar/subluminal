#!/usr/bin/env bash
set -euo pipefail

WORKERS="${WORKERS:-2}"
BASE_BRANCH="${BASE_BRANCH:-main}"
MAX_ITERS="${MAX_ITERS:-10}"
CODEX_MODEL="${CODEX_MODEL:-}"

ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

WORKTREE_DIR="$ROOT/.worktrees"
mkdir -p "$WORKTREE_DIR"

echo "[pool] Workers: $WORKERS"
echo "[pool] Base branch: $BASE_BRANCH"
echo "[pool] Worktree dir: $WORKTREE_DIR"
echo ""

echo "[pool] Fetching latest $BASE_BRANCH..."
git fetch origin "$BASE_BRANCH"

get_ready_ids() {
  local json_output
  if json_output="$(bd ready --json 2>/dev/null)"; then
    echo "$json_output" | python3 -c "
import sys, json
data = json.load(sys.stdin)
if isinstance(data, list):
    for item in data:
        if 'id' in item:
            print(item['id'])
" 2>/dev/null
    return 0
  fi
  
  bd ready 2>/dev/null | grep -oE 'subluminal-[a-zA-Z0-9]+' | head -20
}

pr_exists() {
  local branch="$1"
  local count
  count="$(gh pr list --head "$branch" --json number 2>/dev/null | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")"
  [[ "$count" -gt 0 ]]
}

mapfile -t READY_IDS < <(get_ready_ids)

if [[ ${#READY_IDS[@]} -eq 0 ]]; then
  echo "[pool] No ready beads found"
  exit 0
fi

echo "[pool] Found ${#READY_IDS[@]} ready bead(s)"
echo ""

declare -A JOBS
SPAWNED=0
SKIPPED=0

spawn_worker() {
  local bead_id="$1"
  local branch="bead/$bead_id"
  local wt_path="$WORKTREE_DIR/$bead_id"

  if pr_exists "$branch"; then
    echo "[pool] SKIP $bead_id: PR already exists for $branch"
    ((SKIPPED++)) || true
    return 1
  fi

  if [[ -d "$wt_path" ]]; then
    echo "[pool] SKIP $bead_id: Worktree already exists at $wt_path"
    echo "[pool]   (Remove with: git worktree remove $wt_path)"
    ((SKIPPED++)) || true
    return 1
  fi

  echo "[pool] Creating worktree for $bead_id..."
  git worktree add "$wt_path" -b "$branch" "origin/$BASE_BRANCH" 2>/dev/null || {
    git branch -D "$branch" 2>/dev/null || true
    git worktree add "$wt_path" -b "$branch" "origin/$BASE_BRANCH"
  }

  echo "[pool] Spawning worker for $bead_id (worktree: $wt_path)"

  (
    cd "$wt_path"
    MAX_ITERS="$MAX_ITERS" CODEX_MODEL="$CODEX_MODEL" BASE_BRANCH="$BASE_BRANCH" \
      "$ROOT/scripts/bead_pr.sh" "$bead_id"
    exit_code=$?

    if [[ $exit_code -eq 0 ]]; then
      echo "[pool] Cleaning up worktree for $bead_id..."
      cd "$ROOT"
      git worktree remove "$wt_path" 2>/dev/null || true
    fi

    exit $exit_code
  ) &

  JOBS["$bead_id"]=$!
  ((SPAWNED++)) || true
  return 0
}

wait_for_slot() {
  while [[ ${#JOBS[@]} -ge $WORKERS ]]; do
    for bead_id in "${!JOBS[@]}"; do
      pid="${JOBS[$bead_id]}"
      if ! kill -0 "$pid" 2>/dev/null; then
        wait "$pid" || true
        unset "JOBS[$bead_id]"
        return 0
      fi
    done
    sleep 1
  done
}

for bead_id in "${READY_IDS[@]}"; do
  wait_for_slot
  spawn_worker "$bead_id" || true
done

echo ""
echo "[pool] Waiting for all workers to complete..."
for bead_id in "${!JOBS[@]}"; do
  pid="${JOBS[$bead_id]}"
  if wait "$pid"; then
    echo "[pool] $bead_id: SUCCESS"
  else
    echo "[pool] $bead_id: FAILED (exit $?)"
  fi
done

echo ""
echo "========================================"
echo "[pool] Summary"
echo "========================================"
echo "Ready beads:   ${#READY_IDS[@]}"
echo "Spawned:       $SPAWNED"
echo "Skipped:       $SKIPPED"
echo ""
echo "[pool] Done. Review PRs at: gh pr list"
