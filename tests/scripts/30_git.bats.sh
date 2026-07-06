# Tests for rel_git_dirty and rel_regen_version_go (determinism).

test_git_dirty_clean() {
  tmp=$(mktemp -d)
  git init -q "$tmp"
  (cd "$tmp" && git config user.email t@t && git config user.name t && echo hi > f && git add f && git commit -qm x)
  if (cd "$tmp" && rel_git_dirty); then
    fails=$((fails + 1)); echo "FAIL: clean tree reported dirty"
  else
    passes=$((passes + 1))
  fi
  rm -rf "$tmp"
}

test_git_dirty_dirty() {
  tmp=$(mktemp -d)
  git init -q "$tmp"
  (cd "$tmp" && git config user.email t@t && git config user.name t && echo hi > f && git add f && git commit -qm x && echo bye > f)
  if (cd "$tmp" && rel_git_dirty); then
    passes=$((passes + 1))
  else
    fails=$((fails + 1)); echo "FAIL: dirty tree reported clean"
  fi
  rm -rf "$tmp"
}

test_regen_version_deterministic() {
  tmp=$(mktemp -d)
  rel_regen_version_go v0.1.0 abc1234 2026-07-06T00:00:00Z "$tmp/version.go"
  rel_regen_version_go v0.1.0 abc1234 2026-07-06T00:00:00Z "$tmp/version.go"
  if [ -f "$tmp/version.go" ]; then
    passes=$((passes + 1))
  else
    fails=$((fails + 1)); echo "FAIL: regen did not write file"
  fi
  rm -rf "$tmp"
}

test_preflight_version_valid() {
  assert_exit 0 rel_preflight_version v0.1.0
}
test_preflight_version_invalid() {
  assert_exit 1 rel_preflight_version v1.0.0-alpha
}
test_preflight_version_empty() {
  assert_exit 1 rel_preflight_version ""
}

test_git_dirty_clean
test_git_dirty_dirty
test_regen_version_deterministic
test_preflight_version_valid
test_preflight_version_invalid
test_preflight_version_empty