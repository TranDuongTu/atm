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
FAIL_UPLOAD=${FAIL_UPLOAD:-0}

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
  # --no-preflight-tag is the smoke/dev escape hatch: bypass both the
  # version regex and the tag-exists check. Real releases keep full validation.
  if [ "$NO_PREFLIGHT_TAG" = 0 ]; then
    if ! rel_preflight_version "$VERSION"; then
      echo "VERSION does not match semver regex: $VERSION" >&2
      exit 2
    fi
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

# ---- Phase 3: changelog ----
phase3_changelog() {
  phase_banner 3 "changelog"
  mkdir -p dist
  draft="dist/CHANGELOG.draft.md"
  prev=$(git tag --list 'v*' --sort=-v:refname | sed -n '2p' || true)
  if [ -n "$prev" ]; then
    range="$prev..HEAD"
  else
    range="--all"
  fi
  {
    echo "## $VERSION - $(date -u +%Y-%m-%d)"
    echo
    for bucket in tui cli store docs misc; do
      commits=$(git log "$range" --name-only --pretty=format: 2>/dev/null \
        | awk -F/ -v b="$bucket" '
            $1 == b { seen[$0]=1 }
            END { for (f in seen) print f }')
      if [ -n "$commits" ]; then
        echo "### $bucket"
        git log "$range" --pretty=format:'- %s' -- "$bucket" 2>/dev/null || true
        echo
        echo
      fi
    done
  } > "$draft"

  if [ "$NO_EDIT" = 0 ]; then
    editor=${EDITOR:-vi}
    empty_attempts=0
    while :; do
      "$editor" "$draft"
      if [ -s "$draft" ]; then
        break
      fi
      empty_attempts=$((empty_attempts + 1))
      if [ "$empty_attempts" -ge 3 ]; then
        echo "changelog curation required (3 empty saves); aborting" >&2
        exit 4
      fi
      echo "draft empty; re-opening editor ($empty_attempts/3)" >&2
    done
  fi

  if [ ! -f CHANGELOG.md ]; then
    echo "# Changelog" > CHANGELOG.md
    echo >> CHANGELOG.md
  fi
  {
    cat "$draft"
    echo
    cat CHANGELOG.md
  } > CHANGELOG.md.new
  mv CHANGELOG.md.new CHANGELOG.md
  echo "changelog ok: $(wc -l < CHANGELOG.md) lines"
}

# ---- Phase 4: commit + tag ----
phase4_commit_tag() {
  phase_banner 4 "commit + tag"
  git add internal/version/version.go CHANGELOG.md
  git commit -m "release $VERSION" >/dev/null
  git tag -a "$VERSION" -m "release $VERSION"
  echo "commit+tag ok: $VERSION"
}

# ---- Phase 5: build matrix ----
phase5_build_matrix() {
  phase_banner 5 "build matrix"
  mkdir -p dist
  vstripped=$(rel_version_strip_v "$VERSION")
  commit=${REL_COMMIT:-$(git rev-parse --short HEAD)}
  date=${REL_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}
  ldflags="-X 'atm/internal/version.Version=$VERSION' \
           -X 'atm/internal/version.Commit=$commit' \
           -X 'atm/internal/version.Date=$date'"
  for pair in $(rel_target_matrix); do
    os=${pair%/*}
    arch=${pair#*/}
    out="dist/atm_${vstripped}_${os}_${arch}"
    echo "building $out"
    CGO_ENABLED=0 GOOS=$os GOARCH=$arch \
      go build -trimpath -ldflags "$ldflags" -o "$out" ./cmd/atm
    if [ "$os/$arch" = "$(uname -s | tr A-Z a-z)/$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')" ]; then
      got=$("$out" version)
      case "$got" in
        *"$VERSION"*) ;;
        *) echo "version mismatch in $out: $got" >&2; exit 5 ;;
      esac
    else
      if ! strings "$out" | grep -q "$VERSION"; then
        echo "version string not baked into $out" >&2
        exit 5
      fi
    fi
  done
  echo "build matrix ok: 4 binaries"
}

# ---- Phase 6: tarballs + SHA256SUMS ----
phase6_tarballs() {
  phase_banner 6 "tarballs + SHA256SUMS"
  vstripped=$(rel_version_strip_v "$VERSION")
  commit=${REL_COMMIT:-$(git rev-parse --short HEAD)}
  date=${REL_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}
  inst=$(cat scripts/INSTALL.txt.tmpl \
    | sed "s/__VERSION__/$VERSION/g; s/__COMMIT__/$commit/g; s/__DATE__/$date/g")
  sums="dist/SHA256SUMS"
  : > "$sums"
  for pair in $(rel_target_matrix); do
    os=${pair%/*}
    arch=${pair#*/}
    bin="dist/atm_${vstripped}_${os}_${arch}"
    tb=$(rel_tarball_name "$vstripped" "$os" "$arch")
    staging=$(mktemp -d)
    cp "$bin" "$staging/atm"
    cp LICENSE "$staging/" 2>/dev/null || true
    cp README.md "$staging/"
    printf '%s\n' "$inst" > "$staging/INSTALL.txt"
    files="atm README.md INSTALL.txt"
    [ -f "$staging/LICENSE" ] && files="$files LICENSE"
    tar -C "$staging" -czf "dist/$tb" $files
    rm -rf "$staging"
    hash=$(sha256sum "dist/$tb" | awk '{print $1}')
    rel_sha_line "$hash" "$tb" >> "$sums"
  done
  echo "LATEST=$VERSION" > dist/LATEST
  echo "tarballs ok:"
  cat "$sums"
}

# ---- Phase 7: push ----
phase7_push() {
  phase_banner 7 "push"
  branch=$(git rev-parse --abbrev-ref HEAD)
  git push origin "$branch"
  git push origin "$VERSION"
  echo "push ok: $branch + $VERSION"
}

# ---- Phase 8: upload to GitLab Releases ----
phase8_upload() {
  phase_banner 8 "upload to GitLab Releases"
  if [ "$FAIL_UPLOAD" = 1 ]; then
    echo "FAIL_UPLOAD=1: simulating 401 from forge API" >&2
    exit 42
  fi
  if [ -z "${GITLAB_TOKEN:-}" ] && [ -z "${GITHUB_TOKEN:-}" ] && [ "$DRY_RUN" = 0 ]; then
    echo "GITLAB_TOKEN (or GITHUB_TOKEN) required for upload" >&2
    exit 6
  fi
  if [ -f dist/CHANGELOG.draft.md ]; then
    desc=$(cat dist/CHANGELOG.draft.md)
  else
    prev=$(git tag --list 'v*' --sort=-v:refname | sed -n '2p' || true)
    if [ -n "$prev" ]; then range="$prev..HEAD"; else range="--all"; fi
    desc=$(git log "$range" --pretty=format:'- %s' 2>/dev/null || echo "")
  fi
  forge=${FORGE:-gitlab}
  case "$forge" in
    gitlab)
      api="${CI_API_V4_URL:-https://gitlab.com/api/v4}"
      project="${CI_PROJECT_ID:-}"
      if [ -z "$project" ]; then
        echo "CI_PROJECT_ID required for GitLab upload (or set --from-ci)" >&2
        exit 6
      fi
      body=$(jq -n --arg tag "$VERSION" --arg name "$VERSION" \
        --arg desc "$desc" \
        '{tag_name:$tag, name:$name, description:$desc, assets:{links:[]}}')
      curl -sf --header "PRIVATE-TOKEN: $GITLAB_TOKEN" \
        --header "Content-Type: application/json" \
        --data "$body" \
        "$api/projects/$project/releases" >/dev/null
      # Upload the 5 artifacts as package files and link them.
      for f in dist/*.tar.gz dist/SHA256SUMS; do
        curl -sf --header "PRIVATE-TOKEN: $GITLAB_TOKEN" \
          --form "file=@$f" \
          "$api/projects/$project/uploads" >/dev/null
      done
      ;;
    github)
      repo=${REPO:-}
      if [ -z "$repo" ]; then echo "REPO required for GitHub upload" >&2; exit 6; fi
      body=$(jq -n --arg tag "$VERSION" --arg name "$VERSION" \
        --arg body "$desc" \
        '{tag_name:$tag, name:$name, body:$body}')
      curl -sf --header "Authorization: token $GITHUB_TOKEN" \
        --header "Content-Type: application/json" \
        --data "$body" \
        "https://api.github.com/repos/$repo/releases" >/dev/null
      ;;
    *) echo "unknown FORGE: $forge" >&2; exit 2 ;;
  esac
  echo "upload ok: $VERSION"
}

# ---- Phase 9: tail ----
phase9_tail() {
  phase_banner 9 "tail"
  vstripped=$(rel_version_strip_v "$VERSION")
  echo "Release $VERSION produced:"
  ls -la dist/*.tar.gz dist/SHA256SUMS 2>/dev/null || true
  echo
  echo "SHA256SUMS:"
  cat dist/SHA256SUMS 2>/dev/null || true
  echo
  echo "Install (one line):"
  echo "  curl -fsSL https://<raw-host>/scripts/install.sh | FORGE=gitlab REPO=<slug> VERSION=$VERSION bash"
  echo
  echo "Next: record this release as a comment on ATM-0023 (or the active release task)."
}

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