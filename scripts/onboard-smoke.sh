#!/usr/bin/env bash
# Manual smoke test for `atm manager --onboard`. Not run by `make verify`.
# Usage: ./scripts/onboard-smoke.sh /path/to/repo-to-onboard
set -euo pipefail

repo="${1:-.}"
store_dir="$(mktemp -d)"
trap 'rm -rf "$store_dir"' EXIT

echo "## setup: init store + create FOO project"
atm --store "$store_dir" init
atm --store "$store_dir" project create --code FOO --name "Foo" --actor smoke

echo "## dry-run: render prompt + print argv (no launch)"
atm --store "$store_dir" manager opencode --project FOO --onboard --dry-run

prompt_file="$(ls "$store_dir"/manager/*.md | head -1)"
echo "## prompt file: $prompt_file"
echo "## first 20 lines:"
sed -n '1,20p' "$prompt_file"

echo "## ollama dry-run (integration=opencode)"
atm --store "$store_dir" manager ollama --project FOO --integration opencode --onboard --dry-run

echo "## missing-project error (expect exit 3)"
atm --store "$store_dir" manager opencode --project NOPE --onboard --dry-run || echo "exit=$?"

echo "## live run (requires opencode on PATH; runs in '$repo')"
read -r -p "Run live onboarding against '$repo'? [y/N] " ans
if [[ "$ans" == "y" || "$ans" == "Y" ]]; then
  (cd "$repo" && atm --store "$store_dir" manager opencode --project FOO --onboard)
  echo "## post-run task list:"
  atm --store "$store_dir" task list --project FOO
fi
