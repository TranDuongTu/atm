#!/bin/sh
# tests/scripts/install-smoke.sh — sandbox smoke for scripts/install.sh.
# Builds a fake dist/ via release.sh DRY_RUN, serves it over http, and runs
# install.sh against a temp PREFIX with FORGE=local. No network.
set -eu

REPO_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$REPO_ROOT"

# release.sh DRY_RUN regenerates internal/version/version.go (phase 2) and
# creates CHANGELOG.md (phase 3) without committing. Snapshot them so the trap
# can restore the working tree and leave no smoke residue behind.
vg_path="internal/version/version.go"
vg_snap=$(mktemp)
cp "$vg_path" "$vg_snap"
cl_had=0; [ -f CHANGELOG.md ] && cl_had=1
if [ "$cl_had" = 1 ]; then
  cl_snap=$(mktemp)
  cp CHANGELOG.md "$cl_snap"
fi

scripts/release.sh VERSION=v0.0.0-smoke DRY_RUN=1 --no-edit --no-preflight-tag >/dev/null

port=18099
tmp=$(mktemp -d)
cd dist
python3 -m http.server "$port" >/dev/null 2>&1 &
http_pid=$!
cd "$REPO_ROOT"
trap 'kill $http_pid 2>/dev/null || true; rm -rf "$tmp"; cp "$vg_snap" "$vg_path"; rm -f "$vg_snap"; if [ "$cl_had" = 1 ]; then cp "$cl_snap" CHANGELOG.md; rm -f "$cl_snap"; else rm -f CHANGELOG.md; fi' EXIT
sleep 0.5

PREFIX="$tmp/bin"
mkdir -p "$PREFIX"
PORT="$port" FORGE=local REPO=unused VERSION=v0.0.0-smoke PREFIX="$PREFIX" \
  scripts/install.sh

if [ ! -x "$PREFIX/atm" ]; then
  echo "FAIL: atm not installed at $PREFIX/atm" >&2
  exit 1
fi
out=$("$PREFIX/atm" version)
case "$out" in
  *v0.0.0-smoke*) echo "install-smoke OK: $out" ;;
  *) echo "FAIL: version mismatch: $out" >&2; exit 1 ;;
esac
