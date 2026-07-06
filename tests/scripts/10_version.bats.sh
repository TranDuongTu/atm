# Tests for rel_version_validate, rel_version_strip_v, rel_next_rc.

test_version_validate_accepts() {
  for v in v0.1.0 v1.2.3 v0.1.0-rc.0 v10.20.30-rc.42; do
    assert_exit 0 rel_version_validate "$v"
  done
}
test_version_validate_rejects() {
  for v in v1.0.0-alpha v1.0 1.0.0 v01.02.03 v0.1.0-rc v0.1.0-rc.0a ""; do
    assert_exit 1 rel_version_validate "$v"
  done
}
test_version_strip_v() {
  assert_eq "$(rel_version_strip_v v0.1.0)" "0.1.0" "strip v"
  assert_eq "$(rel_version_strip_v 0.1.0)" "0.1.0" "no v to strip"
}
test_next_rc_first() {
  assert_eq "$(rel_next_rc v0.1.0)" "v0.1.0-rc.0" "first rc"
}
test_next_rc_increment() {
  assert_eq "$(rel_next_rc v0.1.0-rc.3)" "v0.1.0-rc.4" "increment rc"
}

test_version_validate_accepts
test_version_validate_rejects
test_version_strip_v
test_next_rc_first
test_next_rc_increment