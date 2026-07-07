#!/usr/bin/env bash
# Bootstrap the ATM dogfooding project in the machine-global store (v2).
#
# Idempotent: re-running on an existing store is a no-op (skips the project
# and tasks that already exist by title; project create auto-seeds labels).
# Opt-in: NOT run by `make verify`.
#
# Usage:
#   scripts/dogfood.sh [path-to-atm-binary]
#   ATM_HOME=/tmp/atm-dogfood scripts/dogfood.sh bin/atm
#
# Resolves the store via the standard rule (--store > ATM_HOME > ~/.config/atm).
# Actor is free-form in v2; we use "claude" here.
set -euo pipefail

ATM_BIN="${1:-bin/atm}"
ACTOR="claude"

if [ ! -x "$ATM_BIN" ]; then
  echo "dogfood: atm binary not found at $ATM_BIN (run 'make build' first)" >&2
  exit 1
fi

run() {
  "$ATM_BIN" "$@" --actor "$ACTOR" --output json
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

# 2. create the ATM project if absent (v2 project create takes only code+name)
if ! project_exists ATM; then
  echo "dogfood: creating project ATM"
  run project create --code ATM --name "Agent Tasks Management" >/dev/null
else
  echo "dogfood: project ATM already exists"
fi

# 3. labels are auto-seeded by project create (v2.1: 22 default labels with
#    descriptions). To re-apply defaults after an upgrade, run:
#      atm label seed --project ATM
#    (idempotent; preserves edited descriptions).

# 4. seed tasks (idempotent by title). v2 task create takes --label with the
#    full project-prefixed label name; there is no status field, no claim.
declare -a tasks=(
  "Bootstrap v2 store|ATM:status:open,ATM:type:task,ATM:context:agent"
  "Finish TUI parity with CLI|ATM:status:todo,ATM:type:task"
  "Document v2 conventions in README|ATM:status:todo,ATM:type:task"
  "Add cross-project label search|ATM:status:todo,ATM:type:feature"
)

for entry in "${tasks[@]}"; do
  title="${entry%%|*}"
  labels_csv="${entry#*|}"
  if task_title_exists ATM "$title"; then
    echo "dogfood: task already exists: $title"
    continue
  fi
  echo "dogfood: creating task: $title"
  flags=(--project ATM --title "$title")
  IFS=',' read -ra lbls <<< "$labels_csv"
  for l in "${lbls[@]}"; do
    flags+=(--label "$l")
  done
  run task create "${flags[@]}" >/dev/null
done

echo "dogfood: done"
"$ATM_BIN" task list --project ATM --output json 2>/dev/null \
  | grep -o '"id": "[^"]*"' | head -n 20
