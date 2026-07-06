# Tests for rel_target_matrix, rel_tarball_name, rel_sha_line.

test_target_matrix_count() {
  got=$(rel_target_matrix | wc -w)
  assert_eq "$got" "4" "matrix has 4 targets"
}
test_target_matrix_content() {
  got=$(rel_target_matrix | tr '\n' ' ')
  assert_eq "$got" "linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 " "matrix content"
}
test_tarball_name() {
  assert_eq "$(rel_tarball_name 0.1.0 linux amd64)" "atm_0.1.0_linux_amd64.tar.gz" "tarball name"
}
test_sha_line() {
  assert_eq "$(rel_sha_line abc123 atm_0.1.0_linux_amd64.tar.gz)" "abc123  atm_0.1.0_linux_amd64.tar.gz" "sha line"
}

test_target_matrix_count
test_target_matrix_content
test_tarball_name
test_sha_line