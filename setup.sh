#!/usr/bin/env bash
# setup.sh — one-time bootstrap for the ATM repo.
# Installs the Spec Kit `specify` CLI at a pinned version.
set -euo pipefail

SPEC_KIT_VERSION="v0.11.5"
SPEC_KIT_REPO="https://github.com/github/spec-kit.git"

command -v uv >/dev/null 2>&1 || {
  echo "error: 'uv' is required. Install it first: https://docs.astral.sh/uv/"
  exit 1
}

echo "Installing specify-cli @ ${SPEC_KIT_VERSION} ..."
uv tool install specify-cli --from "git+${SPEC_KIT_REPO}@${SPEC_KIT_VERSION}"

echo
echo "Done. Verify with:  specify --version"
echo "Next, initialize specs in this repo with:  specify init"