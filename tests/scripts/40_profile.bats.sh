# Tests for the profile-artifact helpers: rel_profile_dirs,
# rel_profile_artifact_name, rel_profile_manifest_field.

test_profile_dirs_lists_scrumban() {
  got=$(rel_profile_dirs | tr '\n' ' ')
  assert_eq "$got" "profiles/scrumban " "profile dirs"
}

# The artifact is named from the PROFILE's own manifest, never from the atm
# release tag: the same scrumban@1.0.0 ships across several atm releases, and
# naming it per tag would invent versions nobody authored.
test_profile_artifact_name() {
  assert_eq "$(rel_profile_artifact_name scrumban 1.0.0)" "scrumban-1.0.0.atmprofile" "artifact name"
}

test_profile_manifest_field_reads_name() {
  assert_eq "$(rel_profile_manifest_field "$REPO_ROOT/profiles/scrumban" name)" "scrumban" "manifest name"
}

test_profile_manifest_field_reads_semver_version() {
  version=$(rel_profile_manifest_field "$REPO_ROOT/profiles/scrumban" version)
  case "$version" in
    [0-9]*.[0-9]*.[0-9]*) got=semver ;;
    *) got="not-semver: $version" ;;
  esac
  assert_eq "$got" "semver" "manifest version is semver"
}

# Every dir rel_profile_dirs names must actually be readable. A release that
# discovers a missing manifest in phase 6 has already tagged and pushed.
test_profile_dirs_all_have_manifests() {
  missing=""
  for d in $(rel_profile_dirs); do
    [ -f "$REPO_ROOT/$d/manifest.yaml" ] || missing="$missing $d"
  done
  assert_eq "$missing" "" "every profile dir has a manifest"
}

test_profile_dirs_lists_scrumban
test_profile_artifact_name
test_profile_manifest_field_reads_name
test_profile_manifest_field_reads_semver_version
test_profile_dirs_all_have_manifests
