#!/usr/bin/env bash
# setup.sh - one-time sanity check for the ATM repo.
set -euo pipefail

missing=0

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1"
    missing=1
  fi
}

require go
require make

if [[ "$missing" -ne 0 ]]; then
  exit 1
fi

echo "ATM repo prerequisites look ready."
echo "Specs and plans live in the Notion channel (ATM - Superpowers page)."
echo "Use Superpowers brainstorming/planning for new design work."
echo "Verify changes with: make verify"
