#!/usr/bin/env bash
# Bootstrap the ATM dogfooding project in the machine-global store.
#
# Idempotent: re-running on an existing store is a no-op (skips projects and
# tasks that already exist). Opt-in: NOT run by `make verify`.
#
# Usage:
#   scripts/dogfood.sh [path-to-atm-binary]
#   ATM_HOME=/tmp/atm-dogfood scripts/dogfood.sh bin/atm
#
# Resolves the store via the standard rule (--store > ATM_HOME > ~/.config/atm).
set -euo pipefail

ATM_BIN="${1:-bin/atm}"
ACTOR="human:alice"

if [ ! -x "$ATM_BIN" ]; then
  echo "dogfood: atm binary not found at $ATM_BIN (run 'make build' first)" >&2
  exit 1
fi

run() {
  "$ATM_BIN" "$@" --actor "$ACTOR" --output json
}

store_exists() {
  "$ATM_BIN" store path >/dev/null 2>&1
}

project_exists() {
  local code="$1"
  "$ATM_BIN" project show --code "$code" --output json >/dev/null 2>&1
}

task_title_exists() {
  local project="$1" title="$2"
  "$ATM_BIN" task list --project "$project" --output json 2>/dev/null \
    | grep -q "\"title\": \"$title\""
}

echo "dogfood: using store: $("$ATM_BIN" store path)"

# 1. init (idempotent)
"$ATM_BIN" init --actor "$ACTOR" >/dev/null 2>&1 || true

# 2. create the ATM project if absent
if ! project_exists ATM; then
  echo "dogfood: creating project ATM"
  run project create \
    --code ATM --name "Agent Tasks Management" \
    --label type:epic --label type:user-story --label type:impl --label type:bug \
    --label area:cli --label area:tui --label kind:convention \
    --type-axis type >/dev/null
else
  echo "dogfood: project ATM already exists"
fi

# 3. register follow-on tasks (idempotent by title)
followons=(
  "TUI: wire remaining stubbed actions"
  "Add project remove store method + CLI"
  "Add cross-project link support"
  "Auto-refresh TUI"
)

for title in "${followons[@]}"; do
  if task_title_exists ATM "$title"; then
    echo "dogfood: task already exists: $title"
    continue
  fi
  echo "dogfood: creating task: $title"
  run task create --project ATM --title "$title" --label type:impl --label area:cli >/dev/null
done

echo "dogfood: done"
"$ATM_BIN" task list --project ATM --output json | grep -o '"id": "[^"]*"' | head -n 20