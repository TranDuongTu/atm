# Release Process

This document describes how the `atm` binary is versioned, built, and published. For the full design rationale, see [docs/superpowers/specs/2026-07-06-semver-build-pipeline-design.md](docs/superpowers/specs/2026-07-06-semver-build-pipeline-design.md).

## Version model

Version data lives in two Go files under `internal/version/`:

- `version.go` — the three package vars `Version`, `Commit`, `Date`. This file is **regenerated** by the release script on each release commit and overridden at build time via ldflags for dev builds. Defaults: `Version="dev"`, `Commit=""`, `Date=""`.
- `formatters.go` — the static `Info()`, `FormatText()`, `EmitJSON()` helpers (never regenerated).

`make build` injects the three vars via ldflags when `.git/` exists and falls back to the committed defaults on a tarball (no-git) build:

```sh
git describe --tags --dirty --always   # -> Version
git rev-parse --short HEAD              # -> Commit
date -u +%Y-%m-%dT%H:%M:%SZ            # -> Date
```

`atm version` prints text or JSON (`--output json`):

```
atm v0.1.0 (commit: abc1234, built: 2026-07-06T13:45:03Z, linux/amd64)
```

## Versioning policy

- **Semantic versioning** (`vMAJOR.MINOR.PATCH`), starting at `v0.1.0`.
- **Pre-releases** are `-rc.N` only (`v0.1.0-rc.0`, `v0.1.0-rc.1`, ...). The release regex `^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-rc\.(0|[1-9]\d*))?$` rejects `alpha`/`beta`/`-rc` without a number, leading zeros, and missing `v`.
- `0.x → 1.0` graduation is a separate future decision.

## Components

| File | Role |
|------|------|
| `scripts/_release_lib.sh` | Pure, sourceable POSIX sh helpers (`rel_*`). Unit-tested by `tests/scripts/`. |
| `scripts/release.sh` | 9-phase release orchestrator. The source of truth. |
| `scripts/install.sh` | Forge-agnostic consumer installer (GitLab / GitHub / local). |
| `scripts/INSTALL.txt.tmpl` | Template baked into each tarball (`__VERSION__`/`__COMMIT__`/`__DATE__` substituted). |
| `Makefile` | Thin wrappers (`release`, `release-upload`, `release-smoke`, `install-smoke`, `version-bump`, `install-release`, `dist`). |
| `.gitlab-ci.yml` | Inert CI wrapper (valid YAML; activates once the GitLab migration lands). Runs `make verify` on every push and `make release VERSION=$CI_COMMIT_TAG --from-ci` on tag pushes. |

The release script is the source of truth; `.gitlab-ci.yml` and the Makefile are callers over it.

## Build matrix

Four static binaries, `CGO_ENABLED=0`:

| GOOS | GOARCH |
|------|--------|
| linux | amd64 |
| linux | arm64 |
| darwin | amd64 |
| darwin | arm64 |

No Windows. Each target is packaged as a `.tar.gz` containing `atm`, `README.md`, `INSTALL.txt`, and `LICENSE` (when present). A single `SHA256SUMS` file lists all four tarball hashes (two-space separator). No GPG signing.

## Cutting a release

```sh
make release VERSION=v0.1.0
```

This runs `scripts/release.sh` through all 9 phases:

| Phase | Action | Skipped under |
|-------|--------|---------------|
| 1. preflight | validate `VERSION` regex, tag-doesn't-exist, clean tree, required tools (`go git curl tar sha256sum`) | — |
| 2. regen version.go | overwrite `internal/version/version.go` with the release vars | — |
| 3. changelog | draft `dist/CHANGELOG.draft.md` from `git log` grouped by dir (tui/cli/store/docs/misc); open `$EDITOR` to curate (block-until-non-empty, 3 empty-save abort); prepend to `CHANGELOG.md` | — |
| 4. commit + tag | `git add version.go CHANGELOG.md`, commit `release vX.Y.Z`, annotated tag | `DRY_RUN=1` or `--from-ci` |
| 5. build matrix | cross-compile 4 binaries with ldflags; host-arch binary gets a runtime `version` smoke; others get `strings \| grep` | — |
| 6. tarballs | stage + `tar` each target; write `dist/SHA256SUMS` + `dist/LATEST` | — |
| 7. push | `git push origin <branch>` + `git push origin <tag>` | `DRY_RUN=1` or `--from-ci` |
| 8. upload | `curl` to GitLab Releases API (or GitHub); uploads artifacts + creates release with the curated changelog as description | `DRY_RUN=1` |
| 9. tail | print artifacts, `SHA256SUMS`, one-line install command, ATM comment reminder | — |

### Flags & env

| Flag / env | Effect |
|------------|--------|
| `VERSION=vX.Y.Z` | required |
| `DRY_RUN=1` | stop after `dist/` (skip phases 4, 7, 8) |
| `--no-edit` | skip the changelog editor loop (no non-empty enforcement) |
| `--from-ci` | CI mode: skip dirty-tree check (phase 1) and phases 4 + 7 (commit/push already done by the tag push that triggered CI) |
| `--no-preflight-tag` | smoke/dev escape hatch: bypass both the version regex and the tag-exists check |
| `--phase=N` | run only phase N (used by `release-upload` and `version-bump`) |
| `REL_DATE` / `REL_COMMIT` | pin the `Date`/`Commit` values (reproducible builds, tests) |
| `FAIL_UPLOAD=1` | mock a 401 from the forge API in phase 8 (exits 42) — used by the idempotency test |
| `FORGE` | `gitlab` (default) / `github` / `local` (install.sh) |
| `GITLAB_TOKEN` / `GITHUB_TOKEN` | forge auth for upload (phase 8 exits 6 if absent on a real release) |

### Atomicity & recovery

There is no auto-rollback. Failures leave a local checkpoint:

- **Before phase 4:** nothing committed — restore `version.go` with `git checkout internal/version/version.go`.
- **Phase 4 failure (commit/tag):** amend or `git reset` the partial commit.
- **Phase 5–6 failure (build/tarball):** `dist/` is gitignored; re-run after fixing.
- **Phase 7 failure (push):** re-push.
- **Phase 8 failure (upload):** the commit + tag are already pushed; retry upload independently with `make release-upload VERSION=vX.Y.Z` (re-runs phase 8 only). Phase 8 tolerates a missing `dist/CHANGELOG.draft.md` (falls back to `git log`) so retry works on a fresh checkout at the tag.

## Installing (consumers)

One-line install from a forge:

```sh
curl -fsSL <raw-host>/scripts/install.sh | FORGE=gitlab REPO=<slug> VERSION=v0.1.0 bash
```

`VERSION` defaults to `latest`; `PREFIX` defaults to `/usr/local/bin` (auto-sudo if not root). The script maps `uname -s`/`uname -m` to one of the 4 targets, downloads the tarball + `SHA256SUMS`, verifies, installs, and runs `atm version` as a smoke test.

For a local sandbox test (no network):

```sh
make install-smoke
```

## Smoke tests (operator confidence)

```sh
make release-smoke    # DRY_RUN end-to-end through dist/ (4 tarballs, no git state)
make install-smoke    # sandbox install against a local http server
make verify           # build + Go tests + scripts-test (26 sh assertions)
FAIL_UPLOAD=1 scripts/release.sh VERSION=v0.0.0-smoke --phase=8 --no-preflight-tag
                      # idempotency mock: exits 42, no partial commit
REL_DATE=2026-01-02T03:04:05Z REL_COMMIT=deadbeef make version-bump  # deterministic regen
```

## CI (`.gitlab-ci.yml`)

Inert until the GitLab migration lands. When active:

- **verify** stage: `make verify` on every push.
- **release** stage: runs only on tag pushes matching the semver regex; `make release VERSION=$CI_COMMIT_TAG --from-ci`; `dist/` artifacts expire in 30 days.

## Fast-follow (tracked, not blocking)

- `scripts/install.sh` GitLab `download_url` path is broken at runtime (single-quoted printf format string — `$(...)` is literal). The `FORGE=local` smoke path works; the GitLab path needs a fix before the first real GitLab release. GitHub and local paths are unaffected.
