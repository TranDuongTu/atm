#!/bin/sh
# scripts/release.sh — 9-phase release choreography.
# See docs/superpowers/specs/2026-07-06-semver-build-pipeline-design.md.
set -eu

# shellcheck source=_release_lib.sh
. "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/_release_lib.sh"

VERSION=
DRY_RUN=0
NO_EDIT=0
FROM_CI=0
NO_PREFLIGHT_TAG=0
PHASE_ONLY=

for arg in "$@"; do
  case "$arg" in
    VERSION=*) VERSION=${arg#VERSION=} ;;
    DRY_RUN=1) DRY_RUN=1 ;;
    --no-edit) NO_EDIT=1 ;;
    --from-ci) FROM_CI=1 ;;
    --no-preflight-tag) NO_PREFLIGHT_TAG=1 ;;
    --phase=*) PHASE_ONLY=${arg#--phase=} ;;
    *) echo "unknown arg: $arg" >&2; exit 2 ;;
  esac
done

phase_banner() {
  printf '\n=== phase %s: %s ===\n' "$1" "$2"
}

# ---- Phase 1: preflight ----
phase1_preflight() {
  phase_banner 1 "preflight"
  if [ -z "$VERSION" ]; then
    echo "VERSION=vX.Y.Z required" >&2
    exit 2
  fi
  if ! rel_preflight_version "$VERSION"; then
    echo "VERSION does not match semver regex: $VERSION" >&2
    exit 2
  fi
  if [ "$NO_PREFLIGHT_TAG" = 0 ]; then
    if git rev-parse --verify --quiet "refs/tags/$VERSION" >/dev/null; then
      echo "tag already exists: $VERSION" >&2
      exit 2
    fi
  fi
  if [ "$FROM_CI" = 0 ] && [ "$DRY_RUN" = 0 ]; then
    if rel_git_dirty; then
      echo "working tree dirty; commit or stash first" >&2
      exit 2
    fi
  fi
  for tool in go git curl tar sha256sum; do
    if ! command -v "$tool" >/dev/null 2>&1; then
      echo "missing required tool: $tool" >&2
      exit 2
    fi
  done
  echo "preflight ok: version=$VERSION dry_run=$DRY_RUN from_ci=$FROM_CI"
}

# ---- Phase 2: regen version.go ----
phase2_regen_version() {
  phase_banner 2 "regen version.go"
  # REL_COMMIT/REL_DATE pin the values for tests and reproducible builds;
  # when unset, the real git HEAD and current UTC time are used.
  commit=${REL_COMMIT:-$(git rev-parse --short HEAD)}
  date=${REL_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}
  out="internal/version/version.go"
  rel_regen_version_go "$VERSION" "$commit" "$date" "$out"
  if [ "$FROM_CI" = 1 ]; then
    # Verify the committed file matches what we would regenerate.
    tmp=$(mktemp)
    rel_regen_version_go "$VERSION" "$commit" "$date" "$tmp"
    if ! diff -q "$out" "$tmp" >/dev/null; then
      echo "version.go on tag does not match regen output" >&2
      rm -f "$tmp"
      exit 3
    fi
    rm -f "$tmp"
  fi
  echo "regen ok: $out (version=$VERSION commit=$commit date=$date)"
}

# ---- Phases 3-9: stubs (implemented in Tasks 6-7) ----
phase3_changelog()    { phase_banner 3 "changelog (stub)"; echo "TODO phase 3"; }
phase4_commit_tag()   { phase_banner 4 "commit+tag (stub)"; echo "TODO phase 4"; }
phase5_build_matrix() { phase_banner 5 "build matrix (stub)"; echo "TODO phase 5"; }
phase6_tarballs()     { phase_banner 6 "tarballs+SHA256SUMS (stub)"; echo "TODO phase 6"; }
phase7_push()         { phase_banner 7 "push (stub)"; echo "TODO phase 7"; }
phase8_upload()       { phase_banner 8 "upload (stub)"; echo "TODO phase 8"; }
phase9_tail()         { phase_banner 9 "tail (stub)"; echo "TODO phase 9"; }

# ---- Dispatch ----
run_phase() {
  case "$1" in
    1) phase1_preflight ;;
    2) phase2_regen_version ;;
    3) phase3_changelog ;;
    4) phase4_commit_tag ;;
    5) phase5_build_matrix ;;
    6) phase6_tarballs ;;
    7) phase7_push ;;
    8) phase8_upload ;;
    9) phase9_tail ;;
    *) echo "unknown phase: $1" >&2; exit 2 ;;
  esac
}

if [ -n "$PHASE_ONLY" ]; then
  run_phase "$PHASE_ONLY"
  exit 0
fi

run_phase 1
run_phase 2
run_phase 3
[ "$DRY_RUN" = 1 ] || [ "$FROM_CI" = 1 ] || run_phase 4
run_phase 5
run_phase 6
[ "$DRY_RUN" = 1 ] || [ "$FROM_CI" = 1 ] || run_phase 7
[ "$DRY_RUN" = 1 ] || run_phase 8
run_phase 9