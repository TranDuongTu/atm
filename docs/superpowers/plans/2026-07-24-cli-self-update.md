# ATM CLI Self-Update Implementation Plan

> **For agentic workers:** Follow this plan task-by-task. Keep ATM-0119 comments
> updated as each task lands. Do not implement unless ATM-0119 is
> `ATM:stage:planned`.

**Goal:** Add `atm update`, an explicit GitHub self-update command that
downloads a verified release tarball for the current platform and atomically
replaces the running binary.

**Spec:** `docs/superpowers/specs/2026-07-24-cli-self-update-design.md`.

**Architecture:** New `internal/update` package for all updater behavior; thin
Cobra wiring in `internal/cli/update.go`. Tests use fake release clients and
temp files, not network calls.

**Global Constraints**

- V1 is GitHub-only against `TranDuongTu/atm`.
- Public CLI flags: only `--version <tag>`.
- No `--dry-run`, no sudo, no `PREFIX`.
- Target defaults to `os.Executable()`.
- Verify `SHA256SUMS` before replacing anything.
- Replacement must leave the old binary untouched on every pre-rename failure.
- Use explicit git paths; do not stage unrelated benchmark files currently
  dirty in `internal/tui/`.
- Verify with `make verify` before marking implementation done.

---

### Task 1: Package skeleton, platform mapping, and version decision

Create `internal/update` with `Options`, `Result`, `Release`, `Asset`,
`ReleaseClient`, platform mapping, asset naming, and version equality helpers.
Write tests first for supported/unsupported platforms, asset naming, and
`dev`/normalized-version behavior.

### Task 2: Release resolution and GitHub client

Add a fake-client-tested `Run` path for latest vs pinned releases, already
current no-op, missing selected tarball, and missing `SHA256SUMS`. Add
`GitHubClient` tested with `httptest.Server`.

### Task 3: Download, checksum, and archive extraction

Add SHA256SUMS parser/verifier and tar.gz extraction. Tests cover valid sums,
missing lines, malformed hashes, mismatches, missing `atm`, symlink/directory
`atm`, and invalid archives.

### Task 4: Atomic binary replacement

Add same-directory temp write, mode `0755`, rename over target, cleanup on
failure, and injectable replacement seam. Tests cover success and pre-rename
failure preserving old bytes.

### Task 5: Cobra command and CLI tests

Add `internal/cli/update.go`, mount `atm update`, bind `--version`, add a CLI
runner seam, and test help, flag passing, success text, no-op text, JSON output,
and runner errors.

### Task 6: Docs, smoke, and final verification

Update user-facing docs if needed, run `make build`, `./bin/atm update --help`,
and `make verify`. Record files changed, tests run, deviations, and verification
result on ATM-0119.

