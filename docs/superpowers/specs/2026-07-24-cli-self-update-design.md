# ATM CLI Self-Update Design

**Status:** Approved for planning after ATM-0119 brainstorming.
**Task:** ATM-0119.
**Plan:** `docs/superpowers/plans/2026-07-24-cli-self-update.md`.

## Driver

ATM already has a one-command installer in `scripts/install.sh`, but installed
users still have to re-run that script manually when they want a newer release.
The requested `atm update` action is an explicit, in-place binary self-update:
fetch the requested ATM release for the current platform, verify it, and replace
the running binary path.

This is not a store migration command and not a general task-edit/update verb.
It is the same user-facing idea as `gh` or `rustup` self-update workflows:

```sh
atm update
atm update --version v1.2.11
```

The task description was written before the current installer facts were
rechecked. `scripts/install.sh` now defaults to GitHub and does verify
`SHA256SUMS`, so the Go updater must also verify `SHA256SUMS`.

## Decisions

1. **Command name is `atm update`.** No public `self-update` alias in v1.
2. **V1 release source is GitHub only.** Defaults match the current public
   installer: repo `TranDuongTu/atm`, latest release by default.
3. **`--version <tag>` is the only public selector flag.** There is no
   `--dry-run`, no public `--repo`, and no public `--api-base`.
4. **Target path is the running executable.** The updater uses `os.Executable()`
   as the default target and replaces that path. It does not read `PREFIX` and
   does not invoke `sudo`.
5. **Checksum verification is mandatory.** The updater downloads the selected
   tarball plus `SHA256SUMS`, finds the line for the selected asset name, hashes
   the downloaded bytes, and refuses to install on mismatch or missing checksum.
6. **Already-up-to-date is explicit.** If the resolved release tag matches
   `internal/version.Version` after normalizing a leading `v`, the command
   exits 0 with a clear no-op message.
7. **`dev` builds may update.** A binary with `version.Version == "dev"` is
   never reported as up to date.

## User Experience

`atm update`:

1. Resolves the latest GitHub release for `TranDuongTu/atm`.
2. Maps `runtime.GOOS` / `runtime.GOARCH` to the release target matrix:
   `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`.
3. Selects asset `atm_<version-without-v>_<os>_<arch>.tar.gz`.
4. Downloads that tarball and `SHA256SUMS`.
5. Verifies the tarball hash against the exact asset name.
6. Extracts the `atm` binary from the archive.
7. Replaces the running executable path atomically.
8. Prints `updated atm <old> -> <new>`.

`atm update --version v1.2.11` uses the tag endpoint for that version instead
of the latest-release endpoint. The tag should be accepted with or without a
leading `v`, but all user-facing output uses the release tag returned by GitHub.

`atm update` when already current exits 0 and prints:

```text
atm is already up to date: v1.2.11
```

Failure exits non-zero and leaves the old binary untouched. Important failures:
unsupported platform, release not found, matching asset not found, `SHA256SUMS`
not found, checksum missing or mismatched, archive does not contain an `atm`
regular file, target path is not writable, or final rename fails.

## Architecture

Add a new package:

```text
internal/update/
  update.go        orchestration and public Options/Result
  github.go        GitHub release client
  platform.go      GOOS/GOARCH to release asset target
  checksum.go      SHA256SUMS parser/verifier
  archive.go       tar.gz extraction
  replace.go       atomic binary replacement
```

`internal/update` depends on `internal/version` for the current version string,
but it must not depend on Cobra or `internal/cli`.

The CLI remains thin:

```text
internal/cli/update.go
```

It binds `atm update`, accepts `--version`, calls `update.Run`, and renders the
result to stdout/stderr. It follows the existing `version` command pattern in
`internal/cli/root.go`.

## Non-goals

- No store-format migrations.
- No TUI update surface.
- No automatic/background update checks.
- No Windows support in v1.
- No sudo or privilege escalation.
- No GitLab/local/dist support in the Go command.
- No public repo/API override flags.
- No signature verification beyond `SHA256SUMS`.
- No shared installer/updater library extracted in this task.

## Test Strategy

Use ordinary Go tests with fake clients and temp directories:

- Platform mapping accepts the four release targets and rejects unsupported
  OS/arch pairs.
- Asset naming strips a leading `v` from the tarball version segment.
- Latest and pinned-version resolution call the expected release-client path.
- `dev` current version is not treated as already up to date.
- Normalized equal versions produce an explicit no-op result.
- Missing tarball asset, missing `SHA256SUMS`, missing checksum line, and
  checksum mismatch all fail before replacement.
- Archive extraction rejects tarballs without an `atm` regular file.
- Atomic replacement writes the new binary and preserves the old binary on
  pre-rename failures.
- CLI tests prove `atm update --help`, `--version`, successful update output,
  and already-up-to-date output through an injected runner seam.

The repository gate remains `make verify`.

