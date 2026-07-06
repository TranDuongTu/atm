# Semver Build & Release Pipeline — Design Spec

**Status:** Approved (user review 2026-07-06; ready for implementation plan)
**Date:** 2026-07-06
**Depends on:** `2026-07-02-tasks-management-v2-design.md` (verify gate), repo `AGENTS.md` (stable+versioned API surface, no emojis, `make verify` gate, follow neighboring style)

## Driver

Today the `atm` binary advertises no version. `internal/cli/root.go:112` prints the
hardcoded string `atm version dev`. The Makefile has no release targets. No CI
configuration exists (`.github/`, `.gitea/`, `.woodpecker/`, `.gitlab-ci.yml` all
absent). No git tags exist yet — this is a greenfield release convention.

The repo's forge is migrating from a self-hosted Gitea instance at
`git.ttran.synology.me` to **GitLab**. The GitLab infra is not yet ready to run
CI against. We need a release pipeline that works *today*, on a developer
laptop, and that the future GitLab CI can wrap as a thin caller rather than
reimplement. The pipeline must produce:

- A deterministic version string baked into every binary (human-readable text +
  machine-readable JSON) so `atm version` is the source of truth at runtime.
- A reproducible cross-compile matrix producing tarballed binaries with
  checksums that consumers can install with a one-line shell command.
- A semver policy that constrains tag shape so future automation and tooling
  never need to second-guess what a release tag means.
- A changelog that is curated, not auto-published, so the release notes reflect
  intent rather than raw commit noise.

The design holds to v2's philosophy: the release pipeline has no intrinsic
workflow knowledge of the application, the manual `scripts/release.sh` is the
source of truth, and any future CI runner is just another caller invoking the
same script with `--from-ci`.

## Scope (v1)

- A generated, committed `internal/version/version.go` package that holds
  `Version`, `Commit`, `Date` package vars (defaults `dev` / `""` / `""`).
- ldflags injection in `make build` so dev builds always reflect
  `git describe --tags --dirty --always` + commit SHA + build date, with a
  tarball/no-git fallback to the committed `version.go` values.
- A rewritten `atm version` subcommand: deterministic JSON (`{version,commit,
  date,os,arch}`) and a text formatter that strips empty commit/date so a
  no-git build prints `atm dev (linux/amd64)`.
- `scripts/_release_lib.sh` — pure, sourceable POSIX sh helpers (version
  validation, target matrix, tarball naming, SHA line emission, version.go
  regeneration, changelog draft generation).
- `scripts/release.sh` — the 9-phase release choreography (preflight → regen
  version.go → changelog → commit+tag → build matrix → tarballs+SHA256SUMS →
  push → upload → tail), with `DRY_RUN=1`, `--no-edit`, `--from-ci`, and
  `FAIL_UPLOAD=1` mock hooks.
- `scripts/install.sh` — forge-agnostic consumer installer (`FORGE=gitlab|
  github|local`, `REPO=<slug>`, `VERSION=<tag-or-latest>`, `PREFIX=<path>`)
  that fetches, verifies, installs, and smokes one binary.
- `Makefile` targets: `release`, `release-upload`, `release-smoke`,
  `install-smoke`, `scripts-test`, `version-bump`, `install-release`,
  `dist`. All thin wrappers over `scripts/release.sh` subcommands.
- `.gitlab-ci.yml` — inert until the GitLab migration lands; specifies the
  `golang:1.22-alpine` image, `verify` job, `release` job on tags only, and
  `release --from-ci` invocation. No secrets to manage (`GITLAB_TOKEN` is
  CI-injected).
- `CHANGELOG.md` — created by the first release; one section per release
  appended at the top, identical body to the GitLab Release description.
- An `INSTALL.txt` bundled into every tarball with the one-line install
  command for that exact release.
- A POSIX-sh test harness under `tests/scripts/` covering `_release_lib.sh`
  pure functions, wired into `make verify` via `make scripts-test`.
- `internal/version/version_test.go` covering the formatter, ldflags override
  (via subprocess build), and empty-commit fallback.
- `make release-smoke` and `make install-smoke` targets (out of the default
  `verify` gate) exercising `release.sh DRY_RUN=1` end-to-end and
  `install.sh` against a local sandbox respectively.

## Out of scope (v1)

- Windows binaries (matrix is linux/{amd64,arm64} + darwin/{amd64,arm64} only).
- GPG signing of checksums or binaries.
- Package-manager formulas (`.deb`, `.rpm`, Homebrew tap, AUR).
- Manpages or shell-completion files in the tarball.
- A `latest` symlink or channel abstraction on the forge — `install.sh`
  resolves "latest" by querying the forge's releases endpoint directly.
- alpha/beta pre-release tags — only `-rc.N` is permitted by the version
  regex. `0.x → 1.0` graduation is a future design event.
- Auto-rollback of a partially published release. Failures after phase 4
  (commit+tag) leave a local checkpoint for a human to amend or `git reset`.
- A release-event task type in ATM. Releases are recorded as comments on
  ATM-0023 and via git tags only; ATM does not model releases as first-class
  entities.
- A second API surface. The release script shells out to `git`, `go`, `curl`,
  and the forge's REST API only; it does not call `atm` itself.

## Decisions (locked)

1. **Version source of truth** — committed `internal/version/version.go` is
   regenerated by the release script at phase 2 and committed in the release
   commit. The git tag is canonical at release time. `make build` injects
   `Version`/`Commit`/`Date` via ldflags as a *runtime override* of the
   committed defaults; non-release dev builds always inject
   `$(git describe --tags --dirty --always)` + commit + date so a developer
   never sees a stale `dev`.
2. **Dev build injection** — `make build` always passes
   `-ldflags "-X 'atm/internal/version.Version=$(git describe ...)' -X
   'atm/internal/version.Commit=$(git rev-parse --short HEAD)' -X
   'atm/internal/version.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)'"` and
   `-trimpath`. Tarball builds (no `.git/`) fall back to the committed
   `version.go` defaults; `make build` detects the absence of `.git/` and
   skips ldflags injection rather than failing.
3. **Release runner** — GitLab CI / GitLab Releases is the target. Until the
   migration lands, the manual `scripts/release.sh` + `make release` is the
   supported path. The script is the source of truth; `.gitlab-ci.yml` is a
   thin wrapper that re-invokes `scripts/release.sh --from-ci`.
4. **Cross-compile matrix** — exactly four targets:
   `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`. No Windows.
   All builds use `CGO_ENABLED=0` for static binaries.
5. **Packaging** — one `.tar.gz` per target containing `atm`, `LICENSE`,
   `README.md`, and `INSTALL.txt`. One `SHA256SUMS` file covering all four
   tarballs. No GPG signature file.
6. **Release flow** — `make release VERSION=vX.Y.Z`:
   (1) preflight (clean tree, version regex, tag absence, forge env)
   → (2) regenerate `internal/version/version.go`
   → (3) generate changelog draft grouped by directory touched
   (tui/cli/store/docs/misc) and require non-empty curation via `$EDITOR`
   (or `--no-edit` to accept the draft verbatim)
   → (4) commit `release vX.Y.Z` + annotated tag `vX.Y.Z`
   → (5) build matrix via ldflags
   → (6) tarballs + `SHA256SUMS` into `dist/`
   → (7) push commit + tag
   → (8) upload `dist/*` to GitLab Releases API
   → (9) tail (print release URL + next steps).
   `DRY_RUN=1` skips phases 4, 7, 8. `--from-ci` skips 4 and 7 (CI runs on
   the tag commit already pushed).
7. **Changelog** — `scripts/release.sh` produces a `git log` draft grouped by
   top-level directory touched (`tui`, `cli`, `store`, `docs`, `misc`), opens
   it in `$EDITOR`, and blocks until the saved buffer is non-empty. The same
   body is both prepended to `CHANGELOG.md` and used as the GitLab Release
   description. `--no-edit` accepts the draft verbatim (used by `release-smoke`
   and CI non-interactive runs).
8. **`atm version` output** — text:
   `atm vX.Y.Z (commit: <short>, built: <RFC3339UTC>, <GOOS>/<GOARCH>)`.
   When `Commit` or `Date` is empty, the parenthetical is trimmed so a no-git
   build prints `atm dev (linux/amd64)`. JSON:
   `{"version":"...","commit":"...","date":"...","os":"...","arch":"..."}`
   with deterministic key order via the existing `st.emit()` strict-sort
   path used by other JSON-emitting subcommands.
9. **Semver policy** — first release is `v0.1.0`. Pre-releases are `-rc.N`
   only; the version regex `^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-rc\.
   (0|[1-9]\d*))?$` rejects alpha/beta. `0.x → 1.0` graduation is a future
   design event recorded as a separate spec.
10. **Atomicity** — `DRY_RUN=1` stops after phase 6 (dist/ produced, no
    commit/tag/push/upload). Failures leave a local checkpoint; there is no
    auto-rollback. `make release-upload VERSION=vX.Y.Z` re-runs phase 8
    independently for retry. Phase 4 failure mid-release is the one place
    recovery needs human action (`git reset --hard`, amend, or `git tag -d`).

## Architecture & component layout

```
internal/version/
  version.go          # generated defaults (Version="dev", Commit="", Date="")
                      # + FormatText(map)string + EmitJSON(map)string exported
  version_test.go     # formatter + ldflags override + empty-commit fallback

scripts/
  _release_lib.sh     # pure, sourceable POSIX sh helpers
  release.sh          # 9-phase release choreography; sources _release_lib.sh
  install.sh          # forge-agnostic consumer installer

tests/scripts/
  runner.sh           # harness: sources _release_lib.sh, runs assertions
  10_version.bats.sh  # version_validate / parse_version / next_rc
  20_matrix.bats.sh   # target_matrix / tarball_name / sha_line_for
  30_git.bats.sh      # git_dirty / regen_version_determinism

Makefile              # +release, +release-upload, +release-smoke,
                      # +install-smoke, +scripts-test, +version-bump,
                      # +install-release, +dist; verify now depends on scripts-test

.gitlab-ci.yml        # inert until migration; verify job + release job (tags only)

.gitignore            # +dist/, +*.tar.gz

CHANGELOG.md          # created on first release; one section per release

INSTALL.txt           # template baked into each tarball (per-release version line)
```

The component layering is deliberately shallow: `release.sh` is the
orchestrator, `_release_lib.sh` is the testable pure-function layer, and the
Makefile is a thin menu over both. No Go code is imported by the release
machinery; the script shells out to `go build` and `git` only.

## Version data model

`internal/version/version.go` (generated, committed):

```go
package version

// Version is the canonical version string. Default "dev"; overridden at build
// time via -ldflags "-X 'atm/internal/version.Version=...'". Regenerated by
// scripts/release.sh on each release commit.
var Version = "dev"

// Commit is the short SHA at build time. Empty when built from a tarball with
// no .git; the text formatter trims it from output in that case.
var Commit = ""

// Date is the RFC3339-UTC build timestamp. Empty when built from a tarball.
var Date = ""
```

The file is regenerated by `scripts/release.sh` phase 2 via a heredoc that
substitutes the three values from `VERSION`, `git rev-parse --short HEAD`, and
`date -u +%Y-%m-%dT%H:%M:%SZ`. Regeneration is deterministic: running it twice
on the same inputs and `git diff` shows no change (asserted by E6).

`internal/version` also exports two formatters:

- `FormatText(map[string]string) string` — produces
  `atm <version> (commit: <short>, built: <date>, <os>/<arch>)`, trimming the
  `commit:` and `built:` segments when their values are empty, and trimming
  the whole parenthetical to just `<os>/<arch>` when both are empty.
- `EmitJSON(map[string]any) string` — produces the strict-ordered JSON object
  via the existing `st.emit()` strict-sort path used by other JSON-emitting
  subcommands.

`internal/cli/root.go`'s `version` subcommand is rewritten to call
`version.FormatText` / `version.EmitJSON` with a map built from
`version.Version`, `version.Commit`, `version.Date`, `runtime.GOOS`,
`runtime.GOARCH`. The `--output json` flag selects JSON; default remains text.

ldflags injection in `make build`:

```make
GO_LDFLAGS := -trimpath
ifneq ($(wildcard .git/),)
  GO_LDFLAGS += -X 'atm/internal/version.Version=$(shell git describe --tags --dirty --always)' \
                -X 'atm/internal/version.Commit=$(shell git rev-parse --short HEAD)' \
                -X 'atm/internal/version.Date=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)'
endif
build:
	@mkdir -p $(BIN)
	$(GO) build $(GO_LDFLAGS_ARGS) -o $(BINARY) ./cmd/atm
```

The `.git/` guard means a `make build` inside a release tarball silently falls
back to the committed `version.go` values; a `make build` inside a developer
clone always reflects the working tree state.

## Release flow choreography

`scripts/release.sh VERSION=vX.Y.Z` runs nine phases. Each phase logs a banner
and exits non-zero on failure. State between phases is carried in shell
variables only; no temp files persist past phase 9 (except `dist/` in
`DRY_RUN=1`).

| # | Phase | DRY_RUN | --from-ci | Side effects |
|---|-------|---------|-----------|--------------|
| 1 | preflight | runs | runs | none |
| 2 | regen `version.go` | runs | runs | `internal/version/version.go` modified |
| 3 | changelog draft + curate | runs | runs | `CHANGELOG.md` modified |
| 4 | commit `release vX.Y.Z` + tag | **skipped** | **skipped** | git commit + annotated tag |
| 5 | build matrix | runs | runs | `dist/atm_<v>_<os>_<arch>` (4 binaries) |
| 6 | tarballs + `SHA256SUMS` | runs | runs | `dist/*.tar.gz` + `dist/SHA256SUMS` |
| 7 | push commit + tag | **skipped** | **skipped** | `git push --follow-tags` |
| 8 | upload to GitLab Releases | **skipped** | runs | REST POST to forge |
| 9 | tail (print URLs + next steps) | runs | runs | none |

### Phase 1 — preflight

- Working tree must be clean (`git status --porcelain` empty) unless `--from-ci`
  (CI runs on the freshly-cloned tag).
- `VERSION` env var must be set and match the semver regex (decision 9).
- Tag `vX.Y.Z` must not already exist (`git rev-parse "$TAG"` fails) unless
  `--from-ci` (CI runs on the existing tag).
- `GITLAB_TOKEN` (or `GITHUB_TOKEN`) must be set for phase 8; warn-continue if
  absent under `DRY_RUN=1`.
- `go`, `git`, `curl`, `tar`, `sha256sum` present in `PATH`.
- A `preflight_failed` exit prints the offending check and the remediation
  hint; no state is mutated.

### Phase 2 — regenerate `version.go`

- Re-emits `internal/version/version.go` with `Version="$VERSION"`,
  `Commit=$(git rev-parse --short HEAD)`, `Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)`.
- Asserts idempotency: regen twice, `git diff --quiet` after second regen.
- Under `--from-ci`, the file is already committed on the tag; phase 2 is a
  no-op verification (regen + diff, fail if mismatch).

### Phase 3 — changelog

- `git log v<previous-tag>..HEAD --name-only --pretty=format:` piped through
  `awk` to bucket by top-level directory: `tui`, `cli`, `store`, `docs`,
  `misc`. Each bucket lists its commits' one-line subjects.
- If no previous tag exists (first release), the range is `--all`.
- Draft written to `dist/CHANGELOG.draft.md`.
- `$EDITOR` invoked on the draft unless `--no-edit`. Block-until-non-empty:
  on editor exit, the draft file must contain at least one non-blank,
  non-comment line; otherwise re-open the editor. Three consecutive empty
  saves abort with "changelog curation required".
- The curated body is prepended to `CHANGELOG.md` (creating the file on first
  release) under a `## vX.Y.Z — <date>` header, and stashed for phase 8 to
  reuse as the GitLab Release description.

### Phase 4 — commit + tag

- `git add internal/version/version.go CHANGELOG.md`
- `git commit -m "release vX.Y.Z"`
- `git tag -a "$TAG" -m "release $TAG"` (annotated tag, message mirrors the
  changelog header).
- Skipped under `DRY_RUN=1` and `--from-ci`.

### Phase 5 — build matrix

- For each `(GOOS, GOARCH)` in `linux/amd64 linux/arm64 darwin/amd64
  darwin/arm64`:
  `CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags
  "-X 'atm/internal/version.Version=$VERSION' -X
  'atm/internal/version.Commit=$COMMIT' -X
  'atm/internal/version.Date=$DATE'" -o dist/atm_${VERSION#v}_${os}_${arch}
  ./cmd/atm`
- The dist filename strips the leading `v` from the version (so
  `atm_0.1.0_linux_amd64`, not `atm_v0.1.0_linux_amd64`); the in-binary
  `Version` string keeps the `v` prefix to match `git describe` output.
- After each build, `./dist/atm_<v>_<os>_<arch> version` is exec'd (where
  executable — skipped for non-host targets via `--check-strings` fallback:
  `strings <binary> | grep -q "$VERSION"`) to assert the version string is
  baked in.

### Phase 6 — tarballs + SHA256SUMS

- Each target binary is tarred with `LICENSE`, `README.md`, and a
  per-release `INSTALL.txt` rendered from the template with `$VERSION`,
  `$COMMIT`, `$DATE` substituted. Tarball name:
  `atm_${VERSION#v}_${os}_${arch}.tar.gz`.
- `SHA256SUMS` is emitted in the `sha256sum` format
  (`<hash>  <filename>` two-space separator) covering all four tarballs.
- The `dist/` directory is gitignored (`.gitignore` adds `dist/` and
  `*.tar.gz`).

### Phase 7 — push

- `git push origin "$BRANCH"` and `git push origin "$TAG"`.
- Skipped under `DRY_RUN=1` and `--from-ci` (CI is *on* the pushed tag).

### Phase 8 — upload

- GitLab Releases API: `POST /api/v4/projects/:id/releases` with JSON body
  `{ "tag_name": "$TAG", "name": "$TAG", "description": "<changelog body>",
  "assets": { "links": [...] }}`. The four tarballs and `SHA256SUMS` are
  uploaded as package assets and linked.
- `FAIL_UPLOAD=1` env mock (used by E6): forces the API call to return 401,
  assert script exits 42 without committing partial state (no rollback
  needed — phases 4 and 7 are independent of phase 8).
- `make release-upload VERSION=vX.Y.Z` re-runs phases 1 (preflight, tag
  present) and 8 only, for retry after a transient failure.

### Phase 9 — tail

- Prints the GitLab Release URL, the SHA256SUMS content, the one-line
  `curl | bash` install command, and a "next ATM comment" reminder to
  record the release on ATM-0023.

## Makefile targets

New targets, all thin wrappers over `scripts/release.sh` subcommands:

```make
.PHONY: release release-upload release-smoke install-smoke scripts-test \
        version-bump install-release dist

## release: cut a release. Required: VERSION=vX.Y.Z. Optional: DRY_RUN=1, --no-edit, --from-ci.
release:
	@test -n "$(VERSION)" || { echo "VERSION=vX.Y.Z required"; exit 2; }
	scripts/release.sh VERSION=$(VERSION) $(RELEASE_ARGS)

## release-upload: retry the GitLab upload phase for an existing tag.
release-upload:
	@test -n "$(VERSION)" || { echo "VERSION=vX.Y.Z required"; exit 2; }
	scripts/release.sh VERSION=$(VERSION) --phase=upload

## release-smoke: end-to-end DRY_RUN=1 release through dist/.
release-smoke:
	scripts/release.sh VERSION=v0.0.0-smoke DRY_RUN=1 --no-edit --no-preflight-tag

## install-smoke: install.sh against a local http server serving dist/.
install-smoke:
	tests/scripts/install-smoke.sh

## scripts-test: POSIX sh unit tests for scripts/_release_lib.sh.
scripts-test:
	tests/scripts/runner.sh

## version-bump: regenerate internal/version/version.go from git state (no commit).
version-bump:
	scripts/release.sh --phase=regen-version

## install-release: convenience wrapper around scripts/install.sh.
install-release:
	scripts/install.sh FORGE=$(or $(FORGE),gitlab) REPO=$(or $(REPO),$(DEFAULT_REPO)) VERSION=$(VERSION)

## dist: alias for release-smoke (produces dist/ without committing).
dist: release-smoke

## verify: now includes scripts-test.
verify: build test scripts-test
```

`make verify` gains a `scripts-test` prerequisite so shell-helper regressions
fail the default gate. `release-smoke` and `install-smoke` are *not* in
`verify` — too slow and network-touching for the default gate.

## .gitlab-ci.yml (inert until migration)

```yaml
image: golang:1.22-alpine

stages: [verify, release]

verify:
  stage: verify
  script: [make verify]

release:
  stage: release
  rules:
    - if: $CI_COMMIT_TAG =~ /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-rc\.(0|[1-9]\d*))?$/
  variables:
    GIT_STRATEGY: clone
  script:
    - make release VERSION=$CI_COMMIT_TAG --from-ci
  artifacts:
    paths: [dist/]
    expire_in: 30 days
```

No secrets are checked into the file. `GITLAB_TOKEN` is injected via the
project's CI/CD variables UI when the migration lands. Until then this file
is inert (no runners tagged to it).

## Consumer install — scripts/install.sh

Forge-agnostic via env vars:

| Env | Required | Default | Purpose |
|-----|----------|---------|---------|
| `FORGE` | no | `gitlab` | `gitlab` / `github` / `local`. `local` short-circuits to `http://localhost:$PORT` for `install-smoke`. |
| `REPO` | yes (gitlab/github) | — | Project slug, e.g. `scyllas/atm`. |
| `VERSION` | no | latest | Pin to a tag, or `latest` to query the forge's releases endpoint. |
| `PREFIX` | no | `/usr/local/bin` | Install directory. Auto-`sudo` if not root and `PREFIX` is not user-writable. |
| `PORT` | no | `8000` | Used only when `FORGE=local`. |

Behavior:

1. Map `uname -s`/`uname -m` to one of the four targets (Darwin/x86_64 →
   darwin/amd64, etc.). Unknown architecture exits 3 with a helpful list.
2. Resolve `VERSION`: if `latest`, query `GET
   /api/v4/projects/<enc>/releases` (GitLab) or `/releases/latest`
   (GitHub). For `FORGE=local`, fetch `dist/LATEST` (a one-line file written
   by `release.sh` phase 6).
3. Download `atm_<v>_<os>_<arch>.tar.gz` + `SHA256SUMS` to a temp dir.
4. `sha256sum -c SHA256SUMS --ignore-missing` for the downloaded tarball.
   Mismatch exits 4.
5. Extract `atm` to `$PREFIX/atm`. Auto-`sudo` if `$PREFIX` is not writable
   by the current user.
6. Run `$PREFIX/atm version` as a smoke test. Print the version string.
7. Print the one-line reinstall command with the resolved `VERSION` pinned.

The single-command UX:

```sh
curl -fsSL https://<raw-host>/scripts/install.sh | FORGE=gitlab REPO=scyllas/atm bash
```

`VERSION` can be appended to pin: `... | VERSION=v0.1.0 bash`.

## Testing & verification of release machinery

The release pipeline *is* code, so it gets the same `make verify` discipline.

**E1. POSIX-sh unit tests for `scripts/_release_lib.sh`.** Pure functions
(`version_validate`, `parse_version`, `next_rc`, `target_matrix`,
`tarball_name`, `sha_line_for`, `git_dirty`, `regen_version_determinism`)
live in `_release_lib.sh` so they can be sourced and tested without side
effects. Harness: `tests/scripts/runner.sh` — a plain POSIX sh loop that
sources `_release_lib.sh`, calls each fn with input/expected pairs from
`tests/scripts/*.bats.sh`, and fails on mismatch. No `bats` dependency (keep
the toolchain Go-only). Wired into `make verify` via `make scripts-test`.

**E2. `internal/version` Go tests.** `version_test.go` covers:
- `FormatText` with full map → full text output.
- `FormatText` with empty `Commit`/`Date` → trimmed parenthetical.
- `FormatText` with `Version="dev"` + empty commit/date → `atm dev (linux/amd64)`.
- `EmitJSON` key order is strictly `version,commit,date,os,arch`.
- ldflags override: a subprocess test that runs
  `go build -ldflags "-X ...Version=test-v ..." -o /tmp/... && /tmp/... version`
  and asserts `test-v` appears in output. Skipped if `go` not on PATH.

**E3. `make release-smoke`.** Runs
`scripts/release.sh VERSION=v0.0.0-smoke DRY_RUN=1 --no-edit --no-preflight-tag`
end-to-end through phase 6. Asserts:
- `dist/atm_0.0.0-smoke_linux_amd64.tar.gz` exists (and the other 3).
- `dist/SHA256SUMS` has exactly 4 lines.
- `dist/atm_0.0.0-smoke_linux_amd64/atm version` prints `v0.0.0-smoke` for
  the host target.
Out of the default `verify` gate. Documented to run on every PR touching
`scripts/` or `internal/version/`.

**E4. `make install-smoke`.** `tests/scripts/install-smoke.sh`:
- Starts `python3 -m http.server $PORT` in a temp dir containing a pre-built
  `dist/` (from `release-smoke` or a fixture).
- Runs `scripts/install.sh FORGE=local REPO=unused PORT=$PORT
  PREFIX=$(mktemp -d)/bin VERSION=v0.0.0-smoke`.
- Asserts `$PREFIX/bin/atm version` prints the right string.
- No network. Out of the default `verify` gate.

**E5. ATM task test plan (this spec's acceptance).** The implementation plan
derived from this spec lists, per plan step, which of E1–E4 it satisfies.
`make verify` must pass after every code-changing plan step. A throwaway
`$TMPDIR/release-repo` is used by `release-smoke` to validate tag-then-build
ordering without polluting real history.

**E6. Idempotency & failure injection.**
- `version.go` regen determinism: `version-bump` run twice, `git diff
  --quiet` succeeds after the second run.
- `FAIL_UPLOAD=1` env mock: phase 8 simulates a GitLab 401. Assert script
  exits 42 *without* having committed partial state (phases 4/7 already
  ran; phase 8 is independent — retry with `make release-upload
  VERSION=$TAG`).

## Acceptance criteria

- `make verify` passes with the new `scripts-test` prerequisite.
- `make release-smoke` produces `dist/` with 4 tarballs + `SHA256SUMS` and
  no commit, tag, push, or upload side effects.
- `make install-smoke` installs `atm` into a sandbox `PREFIX` and `atm
  version` prints the smoke version.
- `atm version` (default text) and `atm version --output json` both reflect
  ldflags-injected values from a dev build, and the committed `version.go`
  defaults from a tarball build.
- The version regex rejects `v1.0.0-alpha`, `v1.0`, `1.0.0`, `v01.02.03`,
  and accepts `v0.1.0`, `v1.2.3`, `v0.1.0-rc.0`, `v10.20.30-rc.42`.
- `.gitlab-ci.yml` is syntactically valid (passes
  `python3 -c "import yaml,sys; yaml.safe_load(open('.gitlab-ci.yml'))"`)
  even though no runner serves it yet.
- The first real release (`make release VERSION=v0.1.0`) is a *separate*
  task spawned from the implementation plan, not part of this spec.

## Resolved review questions (user-approved 2026-07-06)

1. **`INSTALL.txt` template content** — minimal: the one-line `curl | bash`
   install command for this exact release + a 4-line checksum-verification
   snippet. No manual install steps or upgrade notes; the same file is
   bundled into every tarball with `VERSION`/`COMMIT`/`DATE` substituted.
2. **`CHANGELOG.md` first-release header** — minimal: `# Changelog` header
   followed immediately by the first release section (`## vX.Y.Z — <date>`).
   No project blurb or design-spec link; the README already serves that role.
3. **`make release` default behavior** — interactive `$EDITOR` curation is
   the default; `--no-edit` is the override that accepts the git-log draft
   verbatim (used by `release-smoke` and CI non-interactive `--from-ci` runs).
4. **`release-smoke` version string** — `v0.0.0-smoke` (rejected by the
   semver regex) is used, with `--no-preflight-tag` bypassing the regex
   check. This keeps smoke output visually distinct from any real release.

## Transition

On user approval of this spec:
1. Self-review pass — re-read the spec end-to-end against the brainstorm
   checkpoint (ATM-0023-c0005) to confirm every locked decision is reflected.
2. Commit the spec on a feature branch (`spec/semver-build-pipeline`).
3. Record the commit SHA on ATM-0023 as a comment.
4. Invoke the `writing-plans` skill to produce the implementation plan at
   `docs/superpowers/plans/2026-07-06-semver-build-pipeline-plan.md`.
5. Then transition to the `executing-plans` skill for the actual
   implementation, which spawns the first concrete code task
   (`internal/version` + tests) as a separate ATM task.
