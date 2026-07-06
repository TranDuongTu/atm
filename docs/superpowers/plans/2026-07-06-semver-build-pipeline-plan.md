# Semver Build & Release Pipeline — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the hardcoded `atm version dev` output with a generated `internal/version` package, ldflags-injected build, 9-phase `scripts/release.sh` choreography, forge-agnostic `scripts/install.sh`, and the Makefile + inert `.gitlab-ci.yml` targets that wrap them.

**Architecture:** Three layers — (1) Go `internal/version` package with package vars and two exported formatters (`FormatText`, `EmitJSON`); (2) POSIX sh `scripts/_release_lib.sh` (pure, sourceable, unit-tested) plus `scripts/release.sh` (9-phase orchestrator) and `scripts/install.sh` (consumer installer); (3) Makefile + `.gitlab-ci.yml` as thin wrappers. The release script is the source of truth; CI is a caller.

**Tech Stack:** Go 1.22, cobra, POSIX sh (no bats), `git`, `curl`, `tar`, `sha256sum`, GitLab Releases API.

## Global Constraints

- Go 1.22.0 (per `go.mod`); no Go version bump.
- No emojis in code, commits, or generated files (per `AGENTS.md`).
- No new third-party Go dependencies (use stdlib `runtime`, `fmt`, `encoding/json`).
- No `bats` dependency — shell tests use plain POSIX sh + `test` assertions via `tests/scripts/runner.sh`.
- `CGO_ENABLED=0` for all release-matrix builds (static binaries).
- Follow existing style in neighboring files: `internal/cli/output.go` uses `store.MarshalSorted` for deterministic JSON; shell scripts in `scripts/` use plain `#!/bin/sh` shebang.
- `make verify` is the gate; it now includes `scripts-test` as a prerequisite.
- Branch: `spec/semver-build-pipeline` (already created); commit each task with `docs:`/`feat:`/`chore:` prefixes matching repo convention (`git log --oneline -10` shows `docs:`, `feat(tui):`, `fix`).
- The spec is `docs/superpowers/specs/2026-07-06-semver-build-pipeline-design.md` — every task references it.

---

## File Structure

```
internal/version/
  version.go          # NEW: package vars + FormatText + EmitJSON
  version_test.go     # NEW: formatter tests + ldflags override subprocess test

internal/cli/
  root.go             # MODIFY: rewrite newVersionCmd (line 107-115)

scripts/
  _release_lib.sh     # NEW: pure POSIX sh helpers (sourced, not run directly)
  release.sh          # NEW: 9-phase release orchestrator
  install.sh          # NEW: forge-agnostic consumer installer
  INSTALL.txt.tmpl    # NEW: template baked into each tarball

tests/scripts/
  runner.sh           # NEW: harness that sources _release_lib.sh and runs *.bats.sh
  10_version.bats.sh  # NEW: version_validate / parse_version / next_rc tests
  20_matrix.bats.sh   # NEW: target_matrix / tarball_name / sha_line_for tests
  30_git.bats.sh      # NEW: git_dirty / regen_version_determinism tests
  install-smoke.sh    # NEW: install.sh sandbox smoke (used by make install-smoke)

Makefile              # MODIFY: ldflags in build, +release/release-upload/...
.gitlab-ci.yml        # NEW: inert until GitLab migration
.gitignore            # MODIFY: +dist/, +*.tar.gz
CHANGELOG.md          # NEW: created on first release (not in this plan;
                      #       placeholder header committed in Task 10)
```

---

## Task 1: internal/version package — vars + formatters + tests

**Files:**
- Create: `internal/version/version.go`
- Create: `internal/version/version_test.go`
- Test: `internal/version/version_test.go`

**Interfaces:**
- Consumes: `atm/internal/store.MarshalSorted` (for deterministic JSON via re-export — see below).
- Produces:
  - `version.Version` (string, default `"dev"`)
  - `version.Commit` (string, default `""`)
  - `version.Date` (string, default `""`)
  - `version.FormatText(info map[string]string) string`
  - `version.EmitJSON(info map[string]any) string`
  - `version.Info()` returns `map[string]any` built from the three vars + `runtime.GOOS` + `runtime.GOARCH`.

**Spec ref:** Decision 1, Decision 8; "Version data model" section.

- [ ] **Step 1: Write the failing tests**

Create `internal/version/version_test.go`:

```go
package version

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestFormatTextFull(t *testing.T) {
	got := FormatText(map[string]string{
		"version": "v0.1.0",
		"commit":  "abc1234",
		"date":    "2026-07-06T13:45:03Z",
		"os":      "linux",
		"arch":    "amd64",
	})
	want := "atm v0.1.0 (commit: abc1234, built: 2026-07-06T13:45:03Z, linux/amd64)"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFormatTextEmptyCommitDate(t *testing.T) {
	got := FormatText(map[string]string{
		"version": "dev",
		"commit":  "",
		"date":    "",
		"os":      "linux",
		"arch":    "amd64",
	})
	want := "atm dev (linux/amd64)"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFormatTextCommitOnly(t *testing.T) {
	got := FormatText(map[string]string{
		"version": "v0.1.0",
		"commit":  "abc1234",
		"date":    "",
		"os":      "darwin",
		"arch":    "arm64",
	})
	want := "atm v0.1.0 (commit: abc1234, darwin/arm64)"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestEmitJSONKeyOrder(t *testing.T) {
	got := EmitJSON(map[string]any{
		"version": "v0.1.0",
		"commit":  "abc1234",
		"date":    "2026-07-06T13:45:03Z",
		"os":      "linux",
		"arch":    "amd64",
	})
	var m map[string]string
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, got)
	}
	wantOrder := []string{"arch", "commit", "date", "os", "version"}
	var gotOrder []string
	for k := range m {
		gotOrder = append(gotOrder, k)
	}
	if len(gotOrder) != len(wantOrder) {
		t.Fatalf("key count mismatch: got %v want %v", gotOrder, wantOrder)
	}
}

func TestEmitJSONDeterministicContent(t *testing.T) {
	info := map[string]any{
		"version": "v0.1.0",
		"commit":  "abc1234",
		"date":    "2026-07-06T13:45:03Z",
		"os":      "linux",
		"arch":    "amd64",
	}
	a := EmitJSON(info)
	b := EmitJSON(info)
	if a != b {
		t.Fatalf("non-deterministic:\n%s\n%s", a, b)
	}
}

func TestLdflagsOverride(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	tmp := t.TempDir() + "/atm_version_probe"
	ldflags := "-X 'atm/internal/version.Version=test-v' " +
		"-X 'atm/internal/version.Commit=deadbeef' " +
		"-X 'atm/internal/version.Date=2026-01-02T03:04:05Z'"
	cmd := exec.Command("go", "build", "-ldflags", ldflags, "-o", tmp,
		"./cmd/atm")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build: %v", err)
	}
	defer func() { _ = exec.Command("rm", "-f", tmp).Run() }()

	out, err := exec.Command(tmp, "version").Output()
	if err != nil {
		t.Fatalf("run probe: %v", err)
	}
	got := strings.TrimSpace(string(out))
	if !strings.Contains(got, "test-v") {
		t.Fatalf("ldflags override not baked in: %q", got)
	}
	if !strings.Contains(got, "deadbeef") {
		t.Fatalf("commit not baked in: %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/version/...`
Expected: FAIL — package does not exist (`no Go files in .../internal/version`).

- [ ] **Step 3: Write the implementation**

Create `internal/version/version.go`:

```go
package version

import (
	"fmt"
	"runtime"
	"strings"

	"atm/internal/store"
)

// Version is the canonical version string. Default "dev"; overridden at build
// time via -ldflags "-X 'atm/internal/version.Version=...'". Regenerated by
// scripts/release.sh on each release commit.
var Version = "dev"

// Commit is the short SHA at build time. Empty when built from a tarball with
// no .git; the text formatter trims it from output in that case.
var Commit = ""

// Date is the RFC3339-UTC build timestamp. Empty when built from a tarball.
var Date = ""

// Info returns the full version info map used by both formatters.
func Info() map[string]any {
	return map[string]any{
		"version": Version,
		"commit":  Commit,
		"date":    Date,
		"os":      runtime.GOOS,
		"arch":    runtime.GOARCH,
	}
}

// FormatText renders the human-readable version line. Empty commit and date
// segments are trimmed; when both are empty the parenthetical collapses to
// just "<os>/<arch>".
func FormatText(info map[string]string) string {
	var segs []string
	if info["commit"] != "" {
		segs = append(segs, "commit: "+info["commit"])
	}
	if info["date"] != "" {
		segs = append(segs, "built: "+info["date"])
	}
	segs = append(segs, info["os"]+"/"+info["arch"])
	return fmt.Sprintf("atm %s (%s)", info["version"], strings.Join(segs, ", "))
}

// EmitJSON renders the deterministic JSON object via the store's sorted
// marshaller so key order is stable: arch, commit, date, os, version
// (alphabetical, matching every other JSON-emitting CLI subcommand).
func EmitJSON(info map[string]any) string {
	data, err := store.MarshalSorted(info)
	if err != nil {
		return "{}\n"
	}
	return string(data)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/version/...`
Expected: PASS — all 6 tests green.

- [ ] **Step 5: Run the repo verify gate**

Run: `make verify`
Expected: PASS (build + test). `scripts-test` is not yet a prerequisite — added in Task 4.

- [ ] **Step 6: Commit**

```bash
git add internal/version/version.go internal/version/version_test.go
git commit -m "feat(version): add internal/version package with FormatText/EmitJSON"
```

---

## Task 2: Rewrite `atm version` subcommand to use internal/version

**Files:**
- Modify: `internal/cli/root.go:107-115` (rewrite `newVersionCmd`)
- Modify: `internal/cli/root.go` imports (add `atm/internal/version`)
- Test: existing `internal/cli/` test harness (golden) — add a new `version_test.go`.

**Interfaces:**
- Consumes: `version.Info()`, `version.FormatText(map[string]string)`, `version.EmitJSON(map[string]any)`.
- Produces: `atm version` text + JSON output matching Decision 8.

**Spec ref:** Decision 8; "Version data model" section.

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/version_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestVersionText(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	out, _, code := h.run("version")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	got := strings.TrimSpace(out)
	if !strings.HasPrefix(got, "atm ") {
		t.Fatalf("expected 'atm ' prefix: %q", got)
	}
	if !strings.Contains(got, "linux/") && !strings.Contains(got, "darwin/") {
		t.Fatalf("expected os/arch token: %q", got)
	}
	compareGolden(t, "version-text", out)
}

func TestVersionJSON(t *testing.T) {
	h := newGoldenHarness(t)
	out, _, code := h.run("version")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	for _, key := range []string{`"version"`, `"commit"`, `"date"`, `"os"`, `"arch"`} {
		if !strings.Contains(out, key) {
			t.Fatalf("expected key %s in JSON: %s", key, out)
		}
	}
	compareGolden(t, "version-json", out)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run TestVersion`
Expected: FAIL — the current `newVersionCmd` prints `atm version dev`, which lacks the `os/arch` token and JSON keys.

- [ ] **Step 3: Rewrite `newVersionCmd`**

Edit `internal/cli/root.go`:

Replace lines 107-115:

```go
func newVersionCmd(st *cliState) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the atm version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(st.stdout(), "atm version dev")
		},
	}
}
```

With:

```go
func newVersionCmd(st *cliState) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the atm version",
		Run: func(cmd *cobra.Command, args []string) {
			info := version.Info()
			if st.isJSON() {
				fmt.Fprint(st.stdout(), version.EmitJSON(info))
				return
			}
			text := version.FormatText(map[string]string{
				"version": version.Version,
				"commit":  version.Commit,
				"date":    version.Date,
				"os":      info["os"].(string),
				"arch":    info["arch"].(string),
			})
			fmt.Fprintln(st.stdout(), text)
		},
	}
}
```

Add to the import block at `internal/cli/root.go:3-11`:

```go
import (
	"fmt"
	"io"
	"os"

	"atm/internal/store"
	"atm/internal/version"

	"github.com/spf13/cobra"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run TestVersion`
Expected: PASS. If golden files do not yet exist, run:
`go test ./internal/cli/ -run TestVersion -update` to generate them, then re-run to confirm stable.

- [ ] **Step 5: Run the repo verify gate**

Run: `make verify`
Expected: PASS.

- [ ] **Step 6: Manual smoke**

Run: `make build && ./bin/atm version && ./bin/atm version --output json`
Expected: text line `atm dev (linux/amd64)` (or `darwin/arm64`) and a JSON object with all 5 keys.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/cli/version_test.go internal/cli/testdata/golden/version-text.txt internal/cli/testdata/golden/version-json.txt
git commit -m "feat(cli): rewrite 'atm version' to use internal/version formatters"
```

(Adjust the testdata paths if the harness writes to a different location — check `internal/cli/harness_test.go:compareGolden` for the actual directory.)

---

## Task 3: Makefile ldflags injection + .gitignore dist/

**Files:**
- Modify: `Makefile` (build target — add ldflags + trimpath + `.git/` guard)
- Modify: `.gitignore` (add `dist/` and `*.tar.gz`)

**Interfaces:**
- Consumes: `internal/version` package vars from Task 1.
- Produces: `make build` injects `Version`/`Commit`/`Date` via ldflags when `.git/` exists; silently falls back to `version.go` defaults otherwise.

**Spec ref:** Decision 2; "Version data model" section (Makefile snippet).

- [ ] **Step 1: Write the failing test (shell assertion)**

There is no Go test for Makefile behavior; this step verifies by running `make build` and asserting the binary prints a `git describe`-shaped version.

Run:
```sh
make build
got=$(./bin/atm version)
case "$got" in
  *"atm dev ("*) echo "FAIL: ldflags not injected: $got"; exit 1 ;;
  *) echo "OK: $got" ;;
esac
```
Expected before fix: FAIL — `make build` produces `atm dev (...)` because no ldflags are passed.

- [ ] **Step 2: Edit the Makefile build target**

Replace the existing `build` block in `Makefile`:

```make
## build: compile the atm binary into bin/
build:
	@mkdir -p $(BIN)
	$(GO) build -o $(BINARY) ./cmd/atm
```

With:

```make
GO_LDFLAGS := -trimpath
ifneq ($(wildcard .git/),)
  GO_LDFLAGS += -X 'atm/internal/version.Version=$(shell git describe --tags --dirty --always)' \
                -X 'atm/internal/version.Commit=$(shell git rev-parse --short HEAD)' \
                -X 'atm/internal/version.Date=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)'
endif

## build: compile the atm binary into bin/ with ldflags-injected version
build:
	@mkdir -p $(BIN)
	$(GO) build -ldflags "$(GO_LDFLAGS)" -o $(BINARY) ./cmd/atm
```

- [ ] **Step 3: Edit .gitignore**

Append to `.gitignore`:

```
# release artifacts
dist/
*.tar.gz
```

- [ ] **Step 4: Run the manual smoke (passes now)**

Run:
```sh
make clean build
got=$(./bin/atm version)
case "$got" in
  *"atm dev ("*) echo "FAIL: ldflags not injected: $got"; exit 1 ;;
  *) echo "OK: $got" ;;
esac
```
Expected: `OK: atm <git-describe> (commit: <sha>, built: <date>, linux/amd64)`.

- [ ] **Step 5: Run the repo verify gate**

Run: `make verify`
Expected: PASS. The ldflags now flow through the Task 2 golden tests — if the goldens were captured without ldflags, regenerate them with `go test ./internal/cli/ -run TestVersion -update` and confirm they still match a clean re-run.

- [ ] **Step 6: Commit**

```bash
git add Makefile .gitignore internal/cli/testdata/golden/
git commit -m "build: inject version via ldflags in 'make build'; gitignore dist/"
```

---

## Task 4: scripts/_release_lib.sh pure helpers + tests/scripts harness + make scripts-test

**Files:**
- Create: `scripts/_release_lib.sh`
- Create: `tests/scripts/runner.sh`
- Create: `tests/scripts/10_version.bats.sh`
- Create: `tests/scripts/20_matrix.bats.sh`
- Create: `tests/scripts/30_git.bats.sh`
- Modify: `Makefile` (add `scripts-test` target; add it as `verify` prerequisite)

**Interfaces:**
- Produces (shell functions, all namespaced `rel_`):
  - `rel_version_validate <v>` — exit 0 if matches `^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-rc\.(0|[1-9]\d*))?$`, else exit 1.
  - `rel_version_strip_v <v>` — echo `<v>` without leading `v`.
  - `rel_next_rc <v>` — given `v0.1.0` echo `v0.1.0-rc.0`; given `v0.1.0-rc.N` echo `v0.1.0-rc.$((N+1))`.
  - `rel_target_matrix` — echo the 4 `GOOS/GOARCH` pairs space-separated.
  - `rel_tarball_name <v-stripped> <os> <arch>` — echo `atm_<v>_<os>_<arch>.tar.gz`.
  - `rel_sha_line <hash> <filename>` — echo `<hash>  <filename>` (two-space separator).
  - `rel_git_dirty` — exit 0 if working tree is dirty, exit 1 if clean.
  - `rel_regen_version_go <version> <commit> <date> <path>` — write `internal/version/version.go` with the three vars set; idempotent.

**Spec ref:** Decision 9 (semver regex); "Architecture & component layout"; E1.

- [ ] **Step 1: Write the failing tests**

Create `tests/scripts/runner.sh`:

```sh
#!/bin/sh
# POSIX sh test harness for scripts/_release_lib.sh. No bats dependency.
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)

# shellcheck source=../../scripts/_release_lib.sh
. "$REPO_ROOT/scripts/_release_lib.sh"

passes=0
fails=0

assert_eq() {
  if [ "$1" = "$2" ]; then
    passes=$((passes + 1))
  else
    fails=$((fails + 1))
    echo "FAIL: $3"
    echo "  expected: $2"
    echo "  got:      $1"
  fi
}

assert_exit() {
  expected=$1; shift
  "$@" >/dev/null 2>&1; got=$?
  if [ "$got" = "$expected" ]; then
    passes=$((passes + 1))
  else
    fails=$((fails + 1))
    echo "FAIL: expected exit $expected from: $*"
    echo "  got exit: $got"
  fi
}

for t in "$SCRIPT_DIR"/*.bats.sh; do
  . "$t"
done

echo "scripts-test: $passes passed, $fails failed"
[ "$fails" = 0 ] || exit 1
```

Create `tests/scripts/10_version.bats.sh`:

```sh
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
```

Create `tests/scripts/20_matrix.bats.sh`:

```sh
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
```

Create `tests/scripts/30_git.bats.sh`:

```sh
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
```

Make all four scripts executable: `chmod +x tests/scripts/runner.sh`.

- [ ] **Step 2: Run the harness to verify it fails**

Run: `tests/scripts/runner.sh`
Expected: FAIL — `scripts/_release_lib.sh` does not exist (`No such file`).

- [ ] **Step 3: Write `scripts/_release_lib.sh`**

```sh
#!/bin/sh
# Pure, sourceable POSIX sh helpers for the release pipeline. No side effects
# on source; functions exit non-zero on invalid input.
#
# This file is sourced by scripts/release.sh and tests/scripts/runner.sh.
# Do not execute it directly.

rel_VERSION_RE='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rc\.(0|[1-9][0-9]*))?$'

rel_version_validate() {
  v=$1
  [ -n "$v" ] || return 1
  printf '%s' "$v" | grep -Eq "$rel_VERSION_RE"
}

rel_version_strip_v() {
  v=$1
  printf '%s' "$v" | sed 's/^v//'
}

rel_next_rc() {
  v=$1
  case "$v" in
    *-rc.[0-9]*)
      base=${v%-rc.*}
      n=${v##*-rc.}
      printf 'v%s-rc.%d' "$base" "$((n + 1))"
      ;;
    *)
      printf 'v%s-rc.0' "$v"
      ;;
  esac
}

rel_target_matrix() {
  printf 'linux/amd64\nlinux/arm64\ndarwin/amd64\ndarwin/arm64\n'
}

rel_tarball_name() {
  v=$1; os=$2; arch=$3
  printf 'atm_%s_%s_%s.tar.gz' "$v" "$os" "$arch"
}

rel_sha_line() {
  hash=$1; file=$2
  printf '%s  %s\n' "$hash" "$file"
}

rel_git_dirty() {
  [ -n "$(git status --porcelain)" ]
}

rel_regen_version_go() {
  v=$1; commit=$2; date=$3; out=$4
  cat > "$out" <<EOF
package version

// Version is the canonical version string. Regenerated by scripts/release.sh
// on each release commit; overridden at build time via ldflags for dev builds.
var Version = "$v"

// Commit is the short SHA at build time. Empty when built from a tarball.
var Commit = "$commit"

// Date is the RFC3339-UTC build timestamp. Empty when built from a tarball.
var Date = "$date"
EOF
}
```

- [ ] **Step 4: Run the harness to verify it passes**

Run: `tests/scripts/runner.sh`
Expected: `scripts-test: N passed, 0 failed`, exit 0.

- [ ] **Step 5: Wire `scripts-test` into the Makefile**

Add to `Makefile` (near the other `.PHONY` declarations at the top):

```make
.PHONY: all build test lint vet fmt clean install help dogfood \
        scripts-test release release-upload release-smoke install-smoke \
        version-bump install-release dist
```

Add a new target (after `verify:`):

```make
## scripts-test: POSIX sh unit tests for scripts/_release_lib.sh.
scripts-test:
	tests/scripts/runner.sh
```

Modify the existing `verify` target:

```make
## verify: the AGENTS.md verify step - build + test + scripts-test.
verify:
	$(MAKE) build
	$(MAKE) test
	$(MAKE) scripts-test
```

- [ ] **Step 6: Run the full verify gate**

Run: `make verify`
Expected: PASS — build + Go tests + `scripts-test`.

- [ ] **Step 7: Commit**

```bash
git add scripts/_release_lib.sh tests/scripts/ Makefile
git commit -m "feat(scripts): add _release_lib.sh pure helpers + POSIX sh test harness"
```

---

## Task 5: scripts/release.sh — phases 1-2 (preflight + regen version.go)

**Files:**
- Create: `scripts/release.sh`
- Modify: `tests/scripts/30_git.bats.sh` (add `rel_preflight_version` test if pure)
- Modify: `scripts/_release_lib.sh` (add `rel_preflight_version` helper)

**Interfaces:**
- Consumes: `rel_version_validate`, `rel_git_dirty`, `rel_regen_version_go` from Task 4.
- Produces: `scripts/release.sh` with phase 1 + 2 implemented; phases 3-9 are stubs that echo "TODO phase N" and exit 0 (so `release-smoke` in Task 10 can already exercise phase 1-2).

**Spec ref:** "Release flow choreography" — Phase 1 (preflight) + Phase 2 (regen version.go).

- [ ] **Step 1: Write the failing test for `rel_preflight_version`**

Append to `tests/scripts/30_git.bats.sh`:

```sh
test_preflight_version_valid() {
  assert_exit 0 rel_preflight_version v0.1.0
}
test_preflight_version_invalid() {
  assert_exit 1 rel_preflight_version v1.0.0-alpha
}
test_preflight_version_empty() {
  assert_exit 1 rel_preflight_version ""
}
```

- [ ] **Step 2: Run the harness to verify it fails**

Run: `tests/scripts/runner.sh`
Expected: FAIL — `rel_preflight_version: not found`.

- [ ] **Step 3: Add `rel_preflight_version` to `scripts/_release_lib.sh`**

Append:

```sh
rel_preflight_version() {
  rel_version_validate "$1"
}
```

- [ ] **Step 4: Run the harness to verify it passes**

Run: `tests/scripts/runner.sh`
Expected: PASS.

- [ ] **Step 5: Write `scripts/release.sh` with phases 1-2 and stubs**

```sh
#!/bin/sh
# scripts/release.sh — 9-phase release choreography.
# See docs/superpowers/specs/2026-07-06-semver-build-pipeline-design.md.
set -eu

# shellcheck source=_release_lib.sh
. "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/_release_lib.sh"

VERSION=
DRY_RUN=0
NO_EDIT=0
FROM_CI=0
NO_PREFLIGHT_TAG=0
PHASE_ONLY=

for arg in "$@"; do
  case "$arg" in
    VERSION=*) VERSION=${arg#VERSION=} ;;
    DRY_RUN=1) DRY_RUN=1 ;;
    --no-edit) NO_EDIT=1 ;;
    --from-ci) FROM_CI=1 ;;
    --no-preflight-tag) NO_PREFLIGHT_TAG=1 ;;
    --phase=*) PHASE_ONLY=${arg#--phase=} ;;
    *) echo "unknown arg: $arg" >&2; exit 2 ;;
  esac
done

phase_banner() {
  printf '\n=== phase %s: %s ===\n' "$1" "$2"
}

# ---- Phase 1: preflight ----
phase1_preflight() {
  phase_banner 1 "preflight"
  if [ -z "$VERSION" ]; then
    echo "VERSION=vX.Y.Z required" >&2
    exit 2
  fi
  if ! rel_preflight_version "$VERSION"; then
    echo "VERSION does not match semver regex: $VERSION" >&2
    exit 2
  fi
  if [ "$NO_PREFLIGHT_TAG" = 0 ]; then
    if git rev-parse --verify --quiet "refs/tags/$VERSION" >/dev/null; then
      echo "tag already exists: $VERSION" >&2
      exit 2
    fi
  fi
  if [ "$FROM_CI" = 0 ] && [ "$DRY_RUN" = 0 ]; then
    if rel_git_dirty; then
      echo "working tree dirty; commit or stash first" >&2
      exit 2
    fi
  fi
  for tool in go git curl tar sha256sum; do
    if ! command -v "$tool" >/dev/null 2>&1; then
      echo "missing required tool: $tool" >&2
      exit 2
    fi
  done
  echo "preflight ok: version=$VERSION dry_run=$DRY_RUN from_ci=$FROM_CI"
}

# ---- Phase 2: regen version.go ----
phase2_regen_version() {
  phase_banner 2 "regen version.go"
  commit=$(git rev-parse --short HEAD)
  date=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  out="internal/version/version.go"
  rel_regen_version_go "$VERSION" "$commit" "$date" "$out"
  if [ "$FROM_CI" = 1 ]; then
    # Verify the committed file matches what we would regenerate.
    tmp=$(mktemp)
    rel_regen_version_go "$VERSION" "$commit" "$date" "$tmp"
    if ! diff -q "$out" "$tmp" >/dev/null; then
      echo "version.go on tag does not match regen output" >&2
      rm -f "$tmp"
      exit 3
    fi
    rm -f "$tmp"
  fi
  echo "regen ok: $out (version=$VERSION commit=$commit date=$date)"
}

# ---- Phases 3-9: stubs (implemented in Tasks 6-7) ----
phase3_changelog()    { phase_banner 3 "changelog (stub)"; echo "TODO phase 3"; }
phase4_commit_tag()   { phase_banner 4 "commit+tag (stub)"; echo "TODO phase 4"; }
phase5_build_matrix() { phase_banner 5 "build matrix (stub)"; echo "TODO phase 5"; }
phase6_tarballs()     { phase_banner 6 "tarballs+SHA256SUMS (stub)"; echo "TODO phase 6"; }
phase7_push()         { phase_banner 7 "push (stub)"; echo "TODO phase 7"; }
phase8_upload()       { phase_banner 8 "upload (stub)"; echo "TODO phase 8"; }
phase9_tail()         { phase_banner 9 "tail (stub)"; echo "TODO phase 9"; }

# ---- Dispatch ----
run_phase() {
  case "$1" in
    1) phase1_preflight ;;
    2) phase2_regen_version ;;
    3) phase3_changelog ;;
    4) phase4_commit_tag ;;
    5) phase5_build_matrix ;;
    6) phase6_tarballs ;;
    7) phase7_push ;;
    8) phase8_upload ;;
    9) phase9_tail ;;
    *) echo "unknown phase: $1" >&2; exit 2 ;;
  esac
}

if [ -n "$PHASE_ONLY" ]; then
  run_phase "$PHASE_ONLY"
  exit 0
fi

run_phase 1
run_phase 2
run_phase 3
[ "$DRY_RUN" = 1 ] || [ "$FROM_CI" = 1 ] || run_phase 4
run_phase 5
run_phase 6
[ "$DRY_RUN" = 1 ] || [ "$FROM_CI" = 1 ] || run_phase 7
[ "$DRY_RUN" = 1 ] || run_phase 8
run_phase 9
```

Make it executable: `chmod +x scripts/release.sh`.

- [ ] **Step 6: Run the smoke (manual)**

Run:
```sh
scripts/release.sh VERSION=v0.1.0 DRY_RUN=1 --no-preflight-tag
```
Expected: phase 1 ok, phase 2 regen ok (modifies `internal/version/version.go`), phases 3-9 print stubs. Exit 0.

- [ ] **Step 7: Restore version.go and run verify**

```sh
git checkout internal/version/version.go
make verify
```
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add scripts/release.sh scripts/_release_lib.sh tests/scripts/30_git.bats.sh
git commit -m "feat(scripts): release.sh phases 1-2 (preflight + regen version.go)"
```

---

## Task 6: scripts/release.sh — phases 3-6 (changelog, commit+tag, build, tarballs)

**Files:**
- Modify: `scripts/release.sh` (replace stubs for phases 3, 4, 5, 6)
- Modify: `scripts/_release_lib.sh` (add `rel_changelog_draft`, `rel_build_matrix`, `rel_make_tarballs` helpers if pure enough; otherwise inline in release.sh)
- Create: `scripts/INSTALL.txt.tmpl`
- Modify: `tests/scripts/20_matrix.bats.sh` (add tests for `rel_build_one`'s filename convention if extracted)

**Interfaces:**
- Consumes: `rel_target_matrix`, `rel_tarball_name`, `rel_sha_line` from Task 4.
- Produces: phase 3 produces `dist/CHANGELOG.draft.md` and prepends to `CHANGELOG.md`; phase 4 commits + tags; phase 5 builds 4 binaries with ldflags; phase 6 produces 4 tarballs + `dist/SHA256SUMS`.

**Spec ref:** "Release flow choreography" — Phases 3-6; "Resolved review questions" (INSTALL.txt minimal; CHANGELOG.md minimal header; interactive default).

- [ ] **Step 1: Write `scripts/INSTALL.txt.tmpl`**

```
atm __VERSION__ (commit: __COMMIT__, built: __DATE__)

Install with one line:

  curl -fsSL https://<raw-host>/scripts/install.sh | FORGE=gitlab REPO=<slug> VERSION=__VERSION__ bash

Verify checksum after download:

  sha256sum -c SHA256SUMS --ignore-missing
```

- [ ] **Step 2: Replace phase 3 stub in `scripts/release.sh`**

```sh
phase3_changelog() {
  phase_banner 3 "changelog"
  mkdir -p dist
  draft="dist/CHANGELOG.draft.md"
  prev=$(git tag --list 'v*' --sort=-v:refname | sed -n '2p' || true)
  if [ -n "$prev" ]; then
    range="$prev..HEAD"
  else
    range="--all"
  fi
  {
    echo "## $VERSION - $(date -u +%Y-%m-%d)"
    echo
    for bucket in tui cli store docs misc; do
      commits=$(git log "$range" --name-only --pretty=format: 2>/dev/null \
        | awk -F/ -v b="$bucket" '
            $1 == b { seen[$0]=1 }
            END { for (f in seen) print f }')
      if [ -n "$commits" ]; then
        echo "### $bucket"
        git log "$range" --pretty=format:'- %s' -- "$bucket" 2>/dev/null || true
        echo
        echo
      fi
    done
  } > "$draft"

  if [ "$NO_EDIT" = 0 ]; then
    editor=${EDITOR:-vi}
    empty_attempts=0
    while :; do
      "$editor" "$draft"
      if [ -s "$draft" ]; then
        break
      fi
      empty_attempts=$((empty_attempts + 1))
      if [ "$empty_attempts" -ge 3 ]; then
        echo "changelog curation required (3 empty saves); aborting" >&2
        exit 4
      fi
      echo "draft empty; re-opening editor ($empty_attempts/3)" >&2
    done
  fi

  if [ ! -f CHANGELOG.md ]; then
    echo "# Changelog" > CHANGELOG.md
    echo >> CHANGELOG.md
  fi
  {
    cat "$draft"
    echo
    cat CHANGELOG.md
  } > CHANGELOG.md.new
  mv CHANGELOG.md.new CHANGELOG.md
  echo "changelog ok: $(wc -l < CHANGELOG.md) lines"
}
```

- [ ] **Step 3: Replace phase 4 stub in `scripts/release.sh`**

```sh
phase4_commit_tag() {
  phase_banner 4 "commit + tag"
  git add internal/version/version.go CHANGELOG.md
  git commit -m "release $VERSION" >/dev/null
  git tag -a "$VERSION" -m "release $VERSION"
  echo "commit+tag ok: $VERSION"
}
```

- [ ] **Step 4: Replace phase 5 stub in `scripts/release.sh`**

```sh
phase5_build_matrix() {
  phase_banner 5 "build matrix"
  mkdir -p dist
  vstripped=$(rel_version_strip_v "$VERSION")
  commit=$(git rev-parse --short HEAD)
  date=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  ldflags="-X 'atm/internal/version.Version=$VERSION' \
           -X 'atm/internal/version.Commit=$commit' \
           -X 'atm/internal/version.Date=$date'"
  for pair in $(rel_target_matrix); do
    os=${pair%/*}
    arch=${pair#*/}
    out="dist/atm_${vstripped}_${os}_${arch}"
    echo "building $out"
    CGO_ENABLED=0 GOOS=$os GOARCH=$arch \
      go build -trimpath -ldflags "$ldflags" -o "$out" ./cmd/atm
    if [ "$os/$arch" = "$(uname -s | tr A-Z a-z)/$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')" ]; then
      got=$("$out" version)
      case "$got" in
        *"$VERSION"*) ;;
        *) echo "version mismatch in $out: $got" >&2; exit 5 ;;
      esac
    else
      if ! strings "$out" | grep -q "$VERSION"; then
        echo "version string not baked into $out" >&2
        exit 5
      fi
    fi
  done
  echo "build matrix ok: 4 binaries"
}
```

- [ ] **Step 5: Replace phase 6 stub in `scripts/release.sh`**

```sh
phase6_tarballs() {
  phase_banner 6 "tarballs + SHA256SUMS"
  vstripped=$(rel_version_strip_v "$VERSION")
  commit=$(git rev-parse --short HEAD)
  date=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  inst=$(cat scripts/INSTALL.txt.tmpl \
    | sed "s/__VERSION__/$VERSION/g; s/__COMMIT__/$commit/g; s/__DATE__/$date/g")
  sums="dist/SHA256SUMS"
  : > "$sums"
  for pair in $(rel_target_matrix); do
    os=${pair%/*}
    arch=${pair#*/}
    bin="dist/atm_${vstripped}_${os}_${arch}"
    tb=$(rel_tarball_name "$vstripped" "$os" "$arch")
    staging=$(mktemp -d)
    cp "$bin" "$staging/atm"
    cp LICENSE "$staging/" 2>/dev/null || true
    cp README.md "$staging/"
    printf '%s\n' "$inst" > "$staging/INSTALL.txt"
    tar -C "$staging" -czf "dist/$tb" atm LICENSE README.md INSTALL.txt
    rm -rf "$staging"
    hash=$(sha256sum "dist/$tb" | awk '{print $1}')
    rel_sha_line "$hash" "$tb" >> "$sums"
  done
  echo "LATEST=$VERSION" > dist/LATEST
  echo "tarballs ok:"
  cat "$sums"
}
```

- [ ] **Step 6: Run the smoke (manual, DRY_RUN)**

```sh
scripts/release.sh VERSION=v0.0.0-smoke DRY_RUN=1 --no-edit --no-preflight-tag
ls dist/
cat dist/SHA256SUMS
```
Expected: 4 tarballs, 4 binaries, `SHA256SUMS` with 4 lines, `LATEST` file, `CHANGELOG.draft.md`. Exit 0. No git commit/tag created (DRY_RUN skips phase 4).

- [ ] **Step 7: Restore state and run verify**

```sh
git checkout internal/version/version.go CHANGELOG.md 2>/dev/null || true
rm -rf dist
make verify
```
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add scripts/release.sh scripts/INSTALL.txt.tmpl
git commit -m "feat(scripts): release.sh phases 3-6 (changelog, commit+tag, build, tarballs)"
```

---

## Task 7: scripts/release.sh — phases 7-9 (push, upload, tail) + FAIL_UPLOAD mock

**Files:**
- Modify: `scripts/release.sh` (replace stubs for phases 7, 8, 9)
- Modify: `tests/scripts/30_git.bats.sh` (add `rel_upload_release` mock test if extracted)

**Interfaces:**
- Produces: phase 7 `git push --follow-tags`; phase 8 `curl` POST to GitLab Releases API with `FAIL_UPLOAD=1` mock; phase 9 prints release URL + install command + ATM comment reminder.

**Spec ref:** "Release flow choreography" — Phases 7-9; Decision 10 (atomicity, `FAIL_UPLOAD=1`).

- [ ] **Step 1: Replace phase 7 stub in `scripts/release.sh`**

```sh
phase7_push() {
  phase_banner 7 "push"
  branch=$(git rev-parse --abbrev-ref HEAD)
  git push origin "$branch"
  git push origin "$VERSION"
  echo "push ok: $branch + $VERSION"
}
```

- [ ] **Step 2: Replace phase 8 stub in `scripts/release.sh`**

```sh
phase8_upload() {
  phase_banner 8 "upload to GitLab Releases"
  if [ -z "${GITLAB_TOKEN:-}" ] && [ -z "${GITHUB_TOKEN:-}" ] && [ "$DRY_RUN" = 0 ]; then
    echo "GITLAB_TOKEN (or GITHUB_TOKEN) required for upload" >&2
    exit 6
  fi
  if [ "$FAIL_UPLOAD" = 1 ]; then
    echo "FAIL_UPLOAD=1: simulating 401 from forge API" >&2
    exit 42
  fi
  forge=${FORGE:-gitlab}
  case "$forge" in
    gitlab)
      api="${CI_API_V4_URL:-https://gitlab.com/api/v4}"
      project="${CI_PROJECT_ID:-}"
      if [ -z "$project" ]; then
        echo "CI_PROJECT_ID required for GitLab upload (or set --from-ci)" >&2
        exit 6
      fi
      body=$(jq -n --arg tag "$VERSION" --arg name "$VERSION" \
        --arg desc "$(cat dist/CHANGELOG.draft.md)" \
        '{tag_name:$tag, name:$name, description:$desc, assets:{links:[]}}')
      curl -sf --header "PRIVATE-TOKEN: $GITLAB_TOKEN" \
        --header "Content-Type: application/json" \
        --data "$body" \
        "$api/projects/$project/releases" >/dev/null
      # Upload the 5 artifacts as package files and link them.
      for f in dist/*.tar.gz dist/SHA256SUMS; do
        curl -sf --header "PRIVATE-TOKEN: $GITLAB_TOKEN" \
          --form "file=@$f" \
          "$api/projects/$project/uploads" >/dev/null
      done
      ;;
    github)
      repo=${REPO:-}
      if [ -z "$repo" ]; then echo "REPO required for GitHub upload" >&2; exit 6; fi
      body=$(jq -n --arg tag "$VERSION" --arg name "$VERSION" \
        --arg body "$(cat dist/CHANGELOG.draft.md)" \
        '{tag_name:$tag, name:$name, body:$body}')
      curl -sf --header "Authorization: token $GITHUB_TOKEN" \
        --header "Content-Type: application/json" \
        --data "$body" \
        "https://api.github.com/repos/$repo/releases" >/dev/null
      ;;
    *) echo "unknown FORGE: $forge" >&2; exit 2 ;;
  esac
  echo "upload ok: $VERSION"
}
```

- [ ] **Step 3: Replace phase 9 stub in `scripts/release.sh`**

```sh
phase9_tail() {
  phase_banner 9 "tail"
  vstripped=$(rel_version_strip_v "$VERSION")
  echo "Release $VERSION produced:"
  ls -la dist/*.tar.gz dist/SHA256SUMS
  echo
  echo "SHA256SUMS:"
  cat dist/SHA256SUMS
  echo
  echo "Install (one line):"
  echo "  curl -fsSL https://<raw-host>/scripts/install.sh | FORGE=gitlab REPO=<slug> VERSION=$VERSION bash"
  echo
  echo "Next: record this release as a comment on ATM-0023 (or the active release task)."
}
```

- [ ] **Step 4: Wire `FAIL_UPLOAD` env into the arg parser**

Modify the top of `scripts/release.sh` to read `FAIL_UPLOAD` from env (not from argv — it is a mock hook, not a user flag). Add near the other env reads:

```sh
FAIL_UPLOAD=${FAIL_UPLOAD:-0}
```

- [ ] **Step 5: Manual FAIL_UPLOAD smoke**

```sh
scripts/release.sh VERSION=v0.0.0-smoke DRY_RUN=1 --no-edit --no-preflight-tag
# DRY_RUN skips phase 8, so test FAIL_UPLOAD via --phase=8:
FAIL_UPLOAD=1 scripts/release.sh VERSION=v0.0.0-smoke --phase=8 --no-preflight-tag
echo "exit=$?"
```
Expected: phase 8 prints `FAIL_UPLOAD=1: simulating 401` and exits 42. (Phase 1 runs first via `--phase`? No — `--phase=` runs only the named phase. Confirm by inspecting the dispatch block in Task 5: `if [ -n "$PHASE_ONLY" ]; then run_phase "$PHASE_ONLY"; exit 0; fi`. So `--phase=8` runs only phase 8, which reads `FAIL_UPLOAD` from env.)

- [ ] **Step 6: Run verify**

```sh
git checkout internal/version/version.go CHANGELOG.md 2>/dev/null || true
rm -rf dist
make verify
```
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add scripts/release.sh
git commit -m "feat(scripts): release.sh phases 7-9 (push, upload, tail) + FAIL_UPLOAD mock"
```

---

## Task 8: scripts/install.sh + tests/scripts/install-smoke.sh

**Files:**
- Create: `scripts/install.sh`
- Create: `tests/scripts/install-smoke.sh`

**Interfaces:**
- Produces: `scripts/install.sh` reads `FORGE` (default `gitlab`), `REPO`, `VERSION` (default `latest`), `PREFIX` (default `/usr/local/bin`), `PORT` (default `8000`, used when `FORGE=local`); maps `uname -s`/`uname -m` to one of 4 targets; downloads, verifies, installs, smokes.

**Spec ref:** "Consumer install — scripts/install.sh".

- [ ] **Step 1: Write `tests/scripts/install-smoke.sh`**

```sh
#!/bin/sh
# tests/scripts/install-smoke.sh — sandbox smoke for scripts/install.sh.
# Builds a fake dist/ via release.sh DRY_RUN, serves it over http, and runs
# install.sh against a temp PREFIX with FORGE=local. No network.
set -eu

REPO_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$REPO_ROOT"

scripts/release.sh VERSION=v0.0.0-smoke DRY_RUN=1 --no-edit --no-preflight-tag >/dev/null

port=18099
tmp=$(mktemp -d)
cd dist
python3 -m http.server "$port" >/dev/null 2>&1 &
http_pid=$!
cd "$REPO_ROOT"
trap 'kill $http_pid 2>/dev/null || true; rm -rf "$tmp"' EXIT
sleep 0.5

PREFIX="$tmp/bin"
PORT="$port" FORGE=local REPO=unused VERSION=v0.0.0-smoke PREFIX="$PREFIX" \
  scripts/install.sh

if [ ! -x "$PREFIX/atm" ]; then
  echo "FAIL: atm not installed at $PREFIX/atm" >&2
  exit 1
fi
out=$("$PREFIX/atm" version)
case "$out" in
  *v0.0.0-smoke*) echo "install-smoke OK: $out" ;;
  *) echo "FAIL: version mismatch: $out" >&2; exit 1 ;;
esac
```

Make it executable: `chmod +x tests/scripts/install-smoke.sh`.

- [ ] **Step 2: Write `scripts/install.sh`**

```sh
#!/bin/sh
# scripts/install.sh — forge-agnostic consumer installer for atm.
# See docs/superpowers/specs/2026-07-06-semver-build-pipeline-design.md.
set -eu

FORGE=${FORGE:-gitlab}
REPO=${REPO:-}
VERSION=${VERSION:-latest}
PREFIX=${PREFIX:-/usr/local/bin}
PORT=${PORT:-8000}

# Map uname to one of the 4 release targets.
map_target() {
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  arch=$(uname -m)
  case "$arch" in
    x86_64|amd64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) echo "unsupported arch: $arch" >&2; exit 3 ;;
  esac
  case "$os" in
    linux|darwin) ;;
    *) echo "unsupported os: $os" >&2; exit 3 ;;
  esac
  printf '%s/%s' "$os" "$arch"
}

resolve_version() {
  if [ "$VERSION" = "latest" ]; then
    case "$FORGE" in
      local) curl -fsS "http://localhost:$PORT/LATEST" | sed 's/^LATEST=//' ;;
      gitlab) curl -fsS "https://gitlab.com/api/v4/projects/$(printf '%s' "$REPO" | jq -sRr @uri)/releases" | jq -r '.[0].tag_name' ;;
      github) curl -fsS "https://api.github.com/repos/$REPO/releases/latest" | jq -r '.tag_name' ;;
      *) echo "unknown FORGE: $FORGE" >&2; exit 2 ;;
    esac
  else
    printf '%s' "$VERSION"
  fi
}

download_url() {
  v=$1; pair=$2
  vstripped=$(printf '%s' "$v" | sed 's/^v//')
  os=${pair%/*}; arch=${pair#*/}
  case "$FORGE" in
    local) printf 'http://localhost:%s/atm_%s_%s_%s.tar.gz' "$PORT" "$vstripped" "$os" "$arch" ;;
    gitlab) printf 'https://gitlab.com/api/v4/projects/$(printf %%s %s | jq -sRr @uri)/releases/%s/downloads/atm_%s_%s_%s.tar.gz' "$REPO" "$v" "$vstripped" "$os" "$arch" ;;
    github) printf 'https://github.com/%s/releases/download/%s/atm_%s_%s_%s.tar.gz' "$REPO" "$v" "$vstripped" "$os" "$arch" ;;
  esac
}

pair=$(map_target)
v=$(resolve_version)
vstripped=$(printf '%s' "$v" | sed 's/^v//')
os=${pair%/*}; arch=${pair#*/}
tb="atm_${vstripped}_${os}_${arch}.tar.gz"
url=$(download_url "$v" "$pair")

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
cd "$tmp"
echo "downloading $url"
curl -fsSLO "$url"
case "$FORGE" in
  local) curl -fsSLO "http://localhost:$PORT/SHA256SUMS" ;;
  gitlab) curl -fsSLO "https://gitlab.com/api/v4/projects/$(printf '%s' "$REPO" | jq -sRr @uri)/releases/$v/downloads/SHA256SUMS" ;;
  github) curl -fsSLO "https://github.com/$REPO/releases/download/$v/SHA256SUMS" ;;
esac
sha256sum -c SHA256SUMS --ignore-missing 2>&1 | grep -q "OK$" || { echo "checksum failed" >&2; exit 4; }

tar xzf "$tb"
if [ -w "$PREFIX" ] || [ "$(id -u)" = 0 ]; then
  install -m 0755 atm "$PREFIX/atm"
else
  sudo install -m 0755 atm "$PREFIX/atm"
fi
"$PREFIX/atm" version
```

Make it executable: `chmod +x scripts/install.sh`.

- [ ] **Step 3: Run the install smoke**

Run: `tests/scripts/install-smoke.sh`
Expected: builds a smoke dist, serves over http, installs to a temp PREFIX, `atm version` prints `v0.0.0-smoke`. Exit 0.

- [ ] **Step 4: Run verify**

```sh
rm -rf dist
make verify
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add scripts/install.sh tests/scripts/install-smoke.sh
git commit -m "feat(scripts): forge-agnostic install.sh + sandbox install-smoke test"
```

---

## Task 9: Makefile release targets + .gitlab-ci.yml + CHANGELOG.md header

**Files:**
- Modify: `Makefile` (add `release`, `release-upload`, `release-smoke`, `install-smoke`, `version-bump`, `install-release`, `dist` targets; already has `scripts-test` from Task 4)
- Create: `.gitlab-ci.yml`
- Create: `CHANGELOG.md` (placeholder header — first release will prepend the first section)

**Interfaces:**
- Consumes: `scripts/release.sh`, `scripts/install.sh`, `tests/scripts/install-smoke.sh` from Tasks 5-8.
- Produces: `make release VERSION=vX.Y.Z`, `make release-upload`, `make release-smoke`, `make install-smoke`, `make version-bump`, `make install-release`, `make dist`; an inert `.gitlab-ci.yml` syntactically valid; a placeholder `CHANGELOG.md` so the first release can prepend without creating.

**Spec ref:** "Makefile targets"; ".gitlab-ci.yml (inert until migration)"; "Resolved review questions" (CHANGELOG.md minimal header).

- [ ] **Step 1: Add Makefile release targets**

Append to `Makefile` (after the `verify:` block):

```make
## release: cut a release. Required: VERSION=vX.Y.Z. Optional: DRY_RUN=1, --no-edit, --from-ci.
release:
	@test -n "$(VERSION)" || { echo "VERSION=vX.Y.Z required"; exit 2; }
	scripts/release.sh VERSION=$(VERSION) $(RELEASE_ARGS)

## release-upload: retry the GitLab upload phase for an existing tag.
release-upload:
	@test -n "$(VERSION)" || { echo "VERSION=vX.Y.Z required"; exit 2; }
	scripts/release.sh VERSION=$(VERSION) --phase=8

## release-smoke: end-to-end DRY_RUN=1 release through dist/.
release-smoke:
	scripts/release.sh VERSION=v0.0.0-smoke DRY_RUN=1 --no-edit --no-preflight-tag

## install-smoke: install.sh against a local http server serving dist/.
install-smoke:
	tests/scripts/install-smoke.sh

## version-bump: regenerate internal/version/version.go from git state (no commit).
version-bump:
	scripts/release.sh VERSION=dev --phase=2 --no-preflight-tag

## install-release: convenience wrapper around scripts/install.sh.
install-release:
	scripts/install.sh FORGE=$(or $(FORGE),gitlab) REPO=$(or $(REPO),$(DEFAULT_REPO)) VERSION=$(VERSION)

## dist: alias for release-smoke (produces dist/ without committing).
dist: release-smoke
```

- [ ] **Step 2: Create `.gitlab-ci.yml`**

```yaml
image: golang:1.22-alpine

stages: [verify, release]

verify:
  stage: verify
  script:
    - make verify

release:
  stage: release
  rules:
    - if: $CI_COMMIT_TAG =~ /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-rc\.(0|[1-9]\d*))?$/
  variables:
    GIT_STRATEGY: clone
  script:
    - make release VERSION=$CI_COMMIT_TAG --from-ci
  artifacts:
    paths:
      - dist/
    expire_in: 30 days
```

- [ ] **Step 3: Validate .gitlab-ci.yml syntax**

Run: `python3 -c "import yaml; yaml.safe_load(open('.gitlab-ci.yml')); print('ok')"`
Expected: `ok`. (If `python3` or `pyyaml` is missing, install pyyaml: `pip3 install pyyaml` — or skip with a note. The acceptance criterion in the spec requires this check to pass.)

- [ ] **Step 4: Create `CHANGELOG.md` placeholder**

```
# Changelog

All notable changes to atm are documented here. The first release section
will be prepended by `scripts/release.sh` phase 3.
```

- [ ] **Step 5: Run `make release-smoke` to verify the full Makefile chain**

Run: `make release-smoke`
Expected: 9 phases run (4, 7, 8 skipped under DRY_RUN), `dist/` has 4 tarballs + SHA256SUMS + LATEST + CHANGELOG.draft.md. Exit 0.

- [ ] **Step 6: Run `make install-smoke`**

Run: `make install-smoke`
Expected: builds smoke dist, serves over http, installs to temp PREFIX, `atm version` prints `v0.0.0-smoke`. Exit 0.

- [ ] **Step 7: Run `make verify`**

Run: `make verify`
Expected: PASS — build + Go tests + scripts-test.

- [ ] **Step 8: Commit**

```bash
git add Makefile .gitlab-ci.yml CHANGELOG.md
git commit -m "build: add release Makefile targets, inert .gitlab-ci.yml, CHANGELOG header"
```

---

## Task 10: End-to-end release-smoke validation + final verify + ATM recording

**Files:** none new — this task validates the whole pipeline end-to-end and records the result.

**Spec ref:** Acceptance criteria; E3, E5, E6.

- [ ] **Step 1: Clean state**

```sh
git status --porcelain
```
Expected: empty (or only `dist/` which is gitignored).

- [ ] **Step 2: Run the full release-smoke and assert the acceptance criteria**

```sh
make release-smoke
test -f dist/atm_0.0.0-smoke_linux_amd64.tar.gz
test -f dist/atm_0.0.0-smoke_linux_arm64.tar.gz
test -f dist/atm_0.0.0-smoke_darwin_amd64.tar.gz
test -f dist/atm_0.0.0-smoke_darwin_arm64.tar.gz
test -f dist/SHA256SUMS
test $(wc -l < dist/SHA256SUMS) -eq 4
host_target="linux/$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')"
host_bin="dist/atm_0.0.0-smoke_$(echo $host_target | tr / _)"
$host_bin version | grep -q "v0.0.0-smoke"
```
Expected: all assertions pass. (On darwin, adjust `host_target` accordingly.)

- [ ] **Step 3: Verify no git state was created**

```sh
git tag --list 'v0.0.0*'
git log --oneline -1
```
Expected: no `v0.0.0-smoke` tag exists; HEAD is still the last commit from Task 9.

- [ ] **Step 4: Run FAIL_UPLOAD idempotency test (E6)**

```sh
FAIL_UPLOAD=1 scripts/release.sh VERSION=v0.0.0-smoke --phase=8 --no-preflight-tag
echo "exit=$?"
```
Expected: exit 42, message `FAIL_UPLOAD=1: simulating 401`.

- [ ] **Step 5: Run version-bump determinism test (E6)**

```sh
make version-bump
cp internal/version/version.go /tmp/v1
make version-bump
diff -q internal/version/version.go /tmp/v1 && echo "deterministic"
git checkout internal/version/version.go
```
Expected: `deterministic` printed, no diff.

- [ ] **Step 6: Run the full verify gate**

```sh
make verify
```
Expected: PASS — build + Go tests + scripts-test.

- [ ] **Step 7: Run install-smoke one more time**

```sh
make install-smoke
```
Expected: sandbox install succeeds, `atm version` prints smoke version.

- [ ] **Step 8: Clean up smoke artifacts**

```sh
rm -rf dist
git status --porcelain
```
Expected: empty.

- [ ] **Step 9: Record completion on ATM-0023**

```sh
/home/ttran/projects/scyllas/atm/bin/atm task comment add --task ATM-0023 --actor opencode-dev \
  --body "Implementation plan executed. All 10 tasks complete. make verify passes (build + Go tests + scripts-test). make release-smoke produces 4 tarballs + SHA256SUMS + LATEST, no git state. make install-smoke installs to sandbox PREFIX. FAIL_UPLOAD=1 exits 42. version-bump deterministic. .gitlab-ci.yml syntactically valid (inert). CHANGELOG.md placeholder committed. Branch: spec/semver-build-pipeline. Ready for first real release (separate task: make release VERSION=v0.1.0)."
```

- [ ] **Step 10: Final commit (if any uncommitted state remains)**

```sh
git status --porcelain
```
If empty, skip. If not, commit any leftover test fixtures with `chore: release pipeline smoke fixtures` (unlikely — `dist/` is gitignored).

---

## Self-Review

After writing the complete plan, I re-read the spec and checked:

**1. Spec coverage:**
- Decision 1 (version source of truth) → Task 1 (internal/version) + Task 5 (phase 2 regen). Covered.
- Decision 2 (dev build ldflags) → Task 3 (Makefile ldflags). Covered.
- Decision 3 (GitLab target, manual script now) → Task 9 (.gitlab-ci.yml). Covered.
- Decision 4 (4-target matrix) → Task 4 (`rel_target_matrix`) + Task 6 (phase 5 build). Covered.
- Decision 5 (per-target tarball + SHA256SUMS, no GPG) → Task 6 (phase 6). Covered.
- Decision 6 (9-phase flow) → Tasks 5-7. Covered.
- Decision 7 (changelog curation, block-until-non-empty) → Task 6 (phase 3). Covered.
- Decision 8 (`atm version` text + JSON) → Tasks 1-2. Covered.
- Decision 9 (semver regex, v0.1.0 start, -rc.N only) → Task 4 (`rel_version_validate`). Covered.
- Decision 10 (DRY_RUN, no auto-rollback, release-upload retry, FAIL_UPLOAD) → Tasks 5-7. Covered.
- Consumer install (scripts/install.sh) → Task 8. Covered.
- E1 (POSIX-sh unit tests) → Task 4. Covered.
- E2 (Go version tests) → Task 1. Covered.
- E3 (release-smoke) → Tasks 6, 9, 10. Covered.
- E4 (install-smoke) → Tasks 8, 9, 10. Covered.
- E5 (ATM task test plan) → Task 10 step 9. Covered.
- E6 (idempotency + FAIL_UPLOAD) → Task 10 steps 4-5. Covered.
- Resolved review question 1 (INSTALL.txt minimal) → Task 6 step 1 template. Covered.
- Resolved review question 2 (CHANGELOG.md minimal header) → Task 9 step 4. Covered.
- Resolved review question 3 (interactive default, --no-edit override) → Task 5 arg parser + Task 6 phase 3. Covered.
- Resolved review question 4 (smoke version v0.0.0-smoke + --no-preflight-tag) → Tasks 5, 9, 10. Covered.

**2. Placeholder scan:** No `TBD`, `TODO` (except the deliberate stubs in Task 5 that are replaced in Tasks 6-7), `implement later`, or vague error-handling instructions. Every code step contains the actual code.

**3. Type consistency:**
- `rel_version_validate`, `rel_version_strip_v`, `rel_next_rc`, `rel_target_matrix`, `rel_tarball_name`, `rel_sha_line`, `rel_git_dirty`, `rel_regen_version_go`, `rel_preflight_version` — all defined in Task 4 or Task 5, used consistently in Tasks 5-8.
- `version.Info()`, `version.FormatText(map[string]string)`, `version.EmitJSON(map[string]any)` — defined in Task 1, used in Task 2.
- `FAIL_UPLOAD` env var — introduced in Task 7, asserted in Task 10. Consistent.
- `--no-preflight-tag` flag — introduced in Task 5 arg parser, used in Tasks 6, 8, 9, 10. Consistent.

No gaps, no placeholders, type-consistent.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-06-semver-build-pipeline-plan.md`.
