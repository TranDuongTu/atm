#!/usr/bin/env bash
# Manual smoke test for `atm onboarding`. Not run by `make verify`.
# Usage: ./scripts/onboard-smoke.sh /path/to/repo-to-onboard
set -euo pipefail

repo="${1:-.}"
store_dir="$(mktemp -d)"
trap 'rm -rf "$store_dir"' EXIT

echo "## setup: init store + create FOO project"
atm --store "$store_dir" init
atm --store "$store_dir" project create --code FOO --name "Foo" --actor smoke

echo "## dry-run: render prompt + print argv (no launch)"
atm --store "$store_dir" onboarding opencode --project FOO --dry-run

prompt_file="$(ls "$store_dir"/onboarding/*.md | head -1)"
echo "## prompt file: $prompt_file"
echo "## first 20 lines:"
sed -n '1,20p' "$prompt_file"

echo "## ollama dry-run (integration=opencode)"
atm --store "$store_dir" onboarding ollama --project FOO --integration opencode --dry-run

echo "## missing-project error (expect exit 3)"
atm --store "$store_dir" onboarding opencode --project NOPE --dry-run || echo "exit=$?"

echo "## unknown prompt-version (expect exit 2)"
atm --store "$store_dir" onboarding opencode --project FOO --prompt-version vNoSuch --dry-run || echo "exit=$?"

echo "## live run (requires opencode on PATH; runs in '$repo')"
read -r -p "Run live onboarding against '$repo'? [y/N] " ans
if [[ "$ans" == "y" || "$ans" == "Y" ]]; then
  (cd "$repo" && atm --store "$store_dir" onboarding opencode --project FOO)
  echo "## post-run task list:"
  atm --store "$store_dir" task list --project FOO
fi
