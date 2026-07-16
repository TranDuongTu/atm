# Promote eventsource + eventsync to `libs/eventsource` nested module — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move `internal/eventsource` and `internal/eventsync` into a single nested Go module `libs/eventsource` (root package + `sync/` subpackage), stitched into the main build with a committed `go.work`, so the event-log library is import-isolated from the rest of the repo and ready for a future repository split.

**Architecture:** This is ATM-8dbf94 — step 1 of 6 of the modularization refactor whose specification is `docs/architecture/logical-components.md`. It is a **pure, behavior-preserving move**: no signatures change, no logic changes. `eventsource` is already pure (stdlib + `github.com/gowebpki/jcs` only, no internal imports); `eventsync` imports only `eventsource` and its two transports (dir, git-subprocess) are generic. They merge into ONE library module because set-union sync travels with the log format it synchronizes. The nested `go.mod` makes importing any `atm/internal/*` package a **compile error**, enforcing the hygiene by construction. Because it is a refactor with no new behavior, the existing test suites are the safety net — the "test" in each cycle is the current suite staying green, not new failing tests.

**Tech Stack:** Go 1.25.0, Go workspaces (`go.work`), `github.com/gowebpki/jcs`, `modernc.org/sqlite` (main module only). Build/test via `make`.

## Global Constraints

- Go toolchain: **go 1.25.0** (matches root `go.mod`); nested module and `go.work` both declare `go 1.25.0`.
- `libs/eventsource` may import **nothing** from this repo (no `atm/internal/*`). Its only external dependency is `github.com/gowebpki/jcs v1.0.1`.
- No signature, type, or behavior changes anywhere — move + rewire only.
- Preserve git history on moved files: use `git mv`, never delete-and-recreate.
- Nested-module import path is **`atm/libs/eventsource`** (root) and **`atm/libs/eventsource/sync`** (subpackage). The `sync/` directory keeps package name **`eventsync`** (avoids the stdlib `sync` collision), so consumers use the named import `eventsync "atm/libs/eventsource/sync"`.
- The stable out-of-process CLI surface (ATM-0008) must not change: `atm store …` commands behave identically.
- `go.work` and `go.work.sum` are **committed** in this repo (deliberate deviation from the usual convention — CI and the release build run from a fresh clone and must resolve the local module).

---

## File structure after this plan

```
libs/eventsource/                 nested module: module atm/libs/eventsource
  go.mod  go.sum                  require github.com/gowebpki/jcs v1.0.1
  event.go canon.go dag.go fold.go hlc.go replay.go replica.go action.go
  alias.go author.go upgrade.go   + all *_test.go + testdata/   (package eventsource)
  sync/                           import path atm/libs/eventsource/sync
    engine.go target.go dirtarget.go gittarget.go + *_test.go    (package eventsync)
go.work                           use . ; use ./libs/eventsource   (committed)
go.work.sum                       (committed if produced)
go.mod                            + require atm/libs/eventsource v0.0.0
internal/store/*.go               imports rewritten: atm/libs/eventsource[/sync]
internal/cli/store.go             import rewritten: eventsync "atm/libs/eventsource/sync"
Makefile                          PKG := ./... ./libs/eventsource/...
.gitignore                        go.work / go.work.sum lines removed
```

`internal/eventsource/` and `internal/eventsync/` no longer exist after Task 1.

---

## Task 1: Promote the packages into `libs/eventsource` and rewire the main module

Ends with the **whole repo green** (main module + standalone library) and one commit. The main module does not build partway through this task; that is expected — the deliverable is the tree building at the end.

**Files:**
- Create: `libs/eventsource/go.mod`, `libs/eventsource/go.sum`
- Create: `go.work` (repo root), `go.work.sum` (if produced)
- Move (`git mv`): `internal/eventsource/*` → `libs/eventsource/`; `internal/eventsync/*` → `libs/eventsource/sync/`
- Modify: `go.mod` (add require), `.gitignore` (remove go.work lines)
- Modify (import rewrites): all `internal/store/*.go` that import eventsource (22 files), `internal/store/eventsource_sync_e2e_test.go`, `internal/cli/store.go`, and the moved `libs/eventsource/sync/*.go`
- Test (safety net, unchanged): the existing suites under `libs/eventsource/...`, `internal/store/...`, `internal/cli/...`

**Interfaces:**
- Consumes: nothing (first task; no prior tasks).
- Produces: import path `atm/libs/eventsource` (package `eventsource`) and `atm/libs/eventsource/sync` (package `eventsync`). Every exported identifier is unchanged from today's `atm/internal/eventsource` and `atm/internal/eventsync` — e.g. `eventsource.Event`, `eventsource.Replica`, `eventsync.Sync`, `eventsync.Options`, `eventsync.Report`, `eventsync.SelectTarget`, `eventsync.DirTarget`, `eventsync.NewDirTarget`. Consumers in later tasks rely on these names being import-path-relocated only.

---

- [ ] **Step 1: Move the two packages with git (preserve history)**

```bash
cd /home/ttran/projects/scyllas/atm
mkdir -p libs/eventsource/sync
git mv internal/eventsource/* libs/eventsource/
git mv internal/eventsync/*  libs/eventsource/sync/
rmdir internal/eventsource internal/eventsync
```

- [ ] **Step 2: Verify the move landed (including testdata)**

Run:
```bash
ls libs/eventsource/testdata >/dev/null && echo "testdata OK"
ls libs/eventsource/sync/engine.go >/dev/null && echo "sync OK"
test ! -e internal/eventsource && test ! -e internal/eventsync && echo "old dirs gone"
```
Expected: `testdata OK`, `sync OK`, `old dirs gone`.

- [ ] **Step 3: Create the nested module's `go.mod`**

Create `libs/eventsource/go.mod`:
```
module atm/libs/eventsource

go 1.25.0

require github.com/gowebpki/jcs v1.0.1
```

- [ ] **Step 4: Rewrite the intra-library import (sync → its own module root)**

The moved `sync/` files still import the old `atm/internal/eventsource`, and so does
the root package's external test `libs/eventsource/equivalence_test.go` (package
`eventsource_test`, which imports the package by path). Rewrite **both** — scope the
grep to the whole `libs/eventsource` tree, not just `sync/`:
```bash
cd /home/ttran/projects/scyllas/atm
grep -rl '"atm/internal/eventsource"' libs/eventsource \
  | xargs sed -i 's#"atm/internal/eventsource"#"atm/libs/eventsource"#g'
```

- [ ] **Step 5: Generate the nested module's `go.sum` and prove it builds standalone**

Run:
```bash
cd /home/ttran/projects/scyllas/atm/libs/eventsource
go mod tidy
go build ./...
go test ./...
```
Expected: `go mod tidy` writes `go.sum` with the `github.com/gowebpki/jcs v1.0.1` entries; build succeeds; **all tests PASS**. If `go build` reports any `atm/internal/*` import, that is a hygiene violation — stop and fix the offending file (it should not exist; eventsource/eventsync import nothing internal).

- [ ] **Step 6: Confirm the library imports nothing from this repo**

Run:
```bash
cd /home/ttran/projects/scyllas/atm/libs/eventsource
go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./... | grep '^atm/' | sort -u
```
Expected: exactly `atm/libs/eventsource` and `atm/libs/eventsource/sync` — **no `atm/internal/...` line**.

- [ ] **Step 7: Create the root workspace**

Create `go.work` at the repo root:
```
go 1.25.0

use .
use ./libs/eventsource
```

- [ ] **Step 8: Do NOT add a require line — the workspace `use` directive is sufficient**

The main module (`atm`) imports `atm/libs/eventsource`, but you must **not** add
`require atm/libs/eventsource` to the main `go.mod`. A `require` target must have a
dotted first path element (Go treats it as fetchable); a bare `atm/...` path is
rejected with `malformed module path "atm/libs/eventsource": missing dot in first
path element`. In workspace mode the `use ./libs/eventsource` directive already
supplies the module to the build graph — no `require`, no `replace`, no `go.sum`
entry needed for it. If a stray require was added, drop it:
```bash
cd /home/ttran/projects/scyllas/atm
go mod edit -droprequire=atm/libs/eventsource   # only if present; normally a no-op
```
(Consequence: `go mod tidy` on the main module in non-workspace mode would fail on
the dotless import — acceptable here; CI never runs bare `go mod tidy`, and the
release build runs in workspace mode.)

- [ ] **Step 9: Un-ignore the workspace files so they get committed**

Edit `.gitignore` and delete these two lines (under the `# go workspace files` comment):
```
go.work
go.work.sum
```
Leave the `# go workspace files` comment line in place or remove it — cosmetic.

- [ ] **Step 10: Rewrite the `eventsource` imports in `internal/store`**

```bash
cd /home/ttran/projects/scyllas/atm
grep -rl '"atm/internal/eventsource"' internal/store \
  | xargs sed -i 's#"atm/internal/eventsource"#"atm/libs/eventsource"#g'
```

- [ ] **Step 11: Rewrite the `eventsync` imports (named import) in the two consumers**

```bash
cd /home/ttran/projects/scyllas/atm
sed -i 's#"atm/internal/eventsync"#eventsync "atm/libs/eventsource/sync"#' \
  internal/cli/store.go internal/store/eventsource_sync_e2e_test.go
```
Rationale: package name is `eventsync` but the import path ends in `/sync`, so the name must be stated explicitly. All call sites already use the `eventsync.` selector, so nothing else changes.

- [ ] **Step 11b: Fix cross-package testdata relative paths broken by the move**

Two test helpers read `internal/eventsource`'s fixture via a relative path that no
longer resolves after the move (`open ../eventsource/testdata/v1-log.jsonl: no such
file or directory`): `internal/store/eventsource_upgrade_test.go` (helper
`v1RawLogFixture`) and `internal/cli/store_test.go`. From `internal/<pkg>` the new
path is `../../libs/eventsource/testdata/`:
```bash
cd /home/ttran/projects/scyllas/atm
sed -i 's#filepath.Join("\.\.", "eventsource", "testdata", "v1-log.jsonl")#filepath.Join("..", "..", "libs", "eventsource", "testdata", "v1-log.jsonl")#' \
  internal/store/eventsource_upgrade_test.go internal/cli/store_test.go
sed -i 's#internal/eventsource/testdata/v1-log.jsonl#libs/eventsource/testdata/v1-log.jsonl#' \
  internal/store/eventsource_upgrade_test.go   # doc comment
```
Verify none remain: `grep -rn '"eventsource", "testdata"' internal` should show only
the two corrected `..`,`..`,`libs` lines. (grep for the import rewrite in Step 12
will not catch this — it is a filesystem path string, not an import.)

- [ ] **Step 12: Confirm no stale import paths remain**

Run:
```bash
cd /home/ttran/projects/scyllas/atm
grep -rn 'atm/internal/events' internal cmd libs || echo "no stale imports"
```
Expected: `no stale imports`.

- [ ] **Step 13: Format and build the whole workspace**

Run:
```bash
cd /home/ttran/projects/scyllas/atm
gofmt -w internal/cli/store.go internal/store/eventsource_sync_e2e_test.go
go build ./... ./libs/eventsource/...
```
Expected: no output (clean build) from both module patterns.

- [ ] **Step 14: Run the full test suite across both modules**

Run:
```bash
cd /home/ttran/projects/scyllas/atm
go vet ./... ./libs/eventsource/...
go test ./... ./libs/eventsource/...
```
Expected: vet clean; **all tests PASS**, including `internal/store` (event-source projector, sync e2e) and `internal/cli`.

- [ ] **Step 15: Commit**

```bash
cd /home/ttran/projects/scyllas/atm
git add -A
git add -f go.work go.work.sum 2>/dev/null || git add -f go.work
git status --short
git commit -m "refactor(ATM-8dbf94): promote eventsource+eventsync to libs/eventsource nested module

Move internal/eventsource -> libs/eventsource (root pkg) and
internal/eventsync -> libs/eventsource/sync as one nested Go module
(module atm/libs/eventsource), stitched via a committed go.work.
Pure move: no behavior or signature changes. Consumers in
internal/store and internal/cli updated to the new import paths.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```
Expected: `git status --short` shows `go.work` (and `go.work.sum` if present) staged, the moved files as renames, and modified `go.mod`/`.gitignore`/store/cli files. Commit succeeds.

---

## Task 2: Wire the nested module into `make verify`

`go test ./...` never descends into a separate module, so the Makefile's `PKG := ./...` would silently skip every `libs/eventsource` test. This task makes the standard `make` targets cover both modules and proves it.

**Files:**
- Modify: `Makefile:5` (`PKG` definition)

**Interfaces:**
- Consumes: the committed `go.work` from Task 1 (workspace mode makes `./libs/eventsource/...` resolvable from the repo root).
- Produces: `make verify` / `make test` / `make vet` that exercise both modules. No code interface.

---

- [ ] **Step 1: Point `PKG` at both modules**

In `Makefile`, change line 5 from:
```make
PKG := ./...
```
to:
```make
PKG := ./... ./libs/eventsource/...
```
This flows through `test`, `test-race`, `vet`, `fmt`, and `fmt-check`, which all reference `$(PKG)`. The `build`/`install` targets are unaffected (they target `./cmd/atm` explicitly).

- [ ] **Step 2: Prove the library tests now run under `make test`**

Run:
```bash
cd /home/ttran/projects/scyllas/atm
go test ./... ./libs/eventsource/... 2>&1 | grep -E 'libs/eventsource' | head
```
Expected: `ok  atm/libs/eventsource` and `ok  atm/libs/eventsource/sync` lines appear — the library is being tested. (If a package has no tests Go prints `no test files`; the two above do have tests.)

- [ ] **Step 3: Run the full verify gate**

Run:
```bash
cd /home/ttran/projects/scyllas/atm
make verify
```
Expected: `make build` produces `bin/atm`; `make test` runs both modules green; `make scripts-test` passes. Whole gate green.

- [ ] **Step 4: Sanity-check the built binary still works**

Run:
```bash
cd /home/ttran/projects/scyllas/atm
./bin/atm store --help >/dev/null && echo "store cmd OK"
```
Expected: `store cmd OK` (the `atm store remote/sync` surface that consumes `eventsync` still mounts).

- [ ] **Step 5: Commit**

```bash
cd /home/ttran/projects/scyllas/atm
git add Makefile
git commit -m "build(ATM-8dbf94): cover libs/eventsource module in make verify

go test ./... does not recurse into the nested module; extend PKG so
test/vet/fmt exercise libs/eventsource in workspace mode.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```
Expected: commit succeeds.

---

## Acceptance criteria (from ATM-8dbf94)

- [ ] `go build ./... ./libs/eventsource/...` and `go test ./... ./libs/eventsource/...` green from the repo root. *(Task 1 Steps 13–14)*
- [ ] `libs/eventsource` tests pass standalone from that directory. *(Task 1 Step 5)*
- [ ] `libs/eventsource` imports nothing from the main module — no `atm/internal/*`. *(Task 1 Step 6)*
- [ ] `make verify` green with the nested module covered. *(Task 2 Steps 2–3)*
- [ ] `atm store` CLI surface unchanged (ATM-0008 stability). *(Task 2 Step 4)*

## Self-review notes

- **Spec coverage:** The spec (`docs/architecture/logical-components.md`, migration table row "Step 1 / ATM-8dbf94") requires promoting eventsource+eventsync to `libs/eventsource` (root + `sync/`) as a nested module with `go.work`. Covered by Task 1. The two under-specified build-tooling items surfaced during re-review (committed `go.work` vs the existing `.gitignore`; `PKG := ./...` not covering the nested module) are covered by Task 1 Step 9 and Task 2 respectively.
- **Not a new-behavior feature:** the TDD "write a failing test first" cycle does not apply — this is a relocation. Verification is the pre-existing suite staying green plus the import-isolation check (Task 1 Step 6), which is the meaningful new invariant this task establishes.
- **Type consistency:** no types or signatures are introduced or renamed; only import paths change and the `sync/` package gains a named import at its two call sites.
- **Fallback if `go build` (Task 1 Step 13) reports the module is not required:** run `go get atm/libs/eventsource` from the repo root (workspace-aware; adds/repairs the require line against the local module), then re-run the build. The `go mod edit` in Step 8 should make this unnecessary.
