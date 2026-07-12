# GitHub Publishing and One-Command Install

## Status

Approved. Implementation on `main` branch.

## Context

ATM currently publishes releases to a self-hosted GitLab instance via `scripts/release.sh`. We want to dual-publish to GitHub (open source) at `github.com/TranDuongTu/atm` and enable a true one-command install for consumers. The install/release scripts already support `FORGE=github` internally — they just need the defaults flipped and GitHub infra wired up.

## Design

### Install script: flip defaults to GitHub

`scripts/install.sh` currently defaults to `FORGE=gitlab` (line 6). Change to `FORGE=github` and hardcode `REPO=TranDuongTu/atm` so the one-command install needs zero env vars:

```bash
curl -fsSL https://raw.githubusercontent.com/TranDuongTu/atm/main/scripts/install.sh | bash
```

The existing GitLab flow still works by setting env vars explicitly:

```bash
curl .../install.sh | FORGE=gitlab REPO=<slug> bash
```

### LICENSE file

Add a `LICENSE` file (MIT). The release tarball packaging in `scripts/release.sh` phase 6 already copies `LICENSE` into tarballs when present.

### GitHub Actions

Two workflows under `.github/workflows/`:

**`verify.yml`** — runs on every push and PR:
- `make verify` (build + test + scripts-test)
- Uses `actions/setup-go@v5` with Go version from `go.mod`

**`release.yml`** — triggers on tag pushes matching `v*`:
- Runs `make release VERSION=${{ github.ref_name }} --from-ci`
- Sets `GITHUB_TOKEN` from secrets for the release upload phase

### Release phase 9: update install command

The tail phase in `scripts/release.sh` prints a sample install command. Update it to reflect the GitHub one-liner.

## Files changed

| File | Change |
|------|--------|
| `scripts/install.sh` | Default `FORGE=github`, `REPO=TranDuongTu/atm` |
| `scripts/release.sh` | Phase 9 install command text updated |
| `LICENSE` | New file (MIT) |
| `.github/workflows/verify.yml` | New file |
| `.github/workflows/release.yml` | New file |
| `README.md` | Add one-command install section |

## Non-goals

- No Go module path rename (stays `atm`). `go install` support is out of scope.
- No Homebrew formula.
- No `.goreleaser.yml` — the existing `scripts/release.sh` pipeline is retained.
- No changes to the existing GitLab remote or flow.
