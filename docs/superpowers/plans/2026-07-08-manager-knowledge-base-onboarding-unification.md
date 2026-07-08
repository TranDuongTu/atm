# Manager as Knowledge-Base Owner: Onboarding Unification + Ubiquitous Language Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reframe the manager as the owner of each project's knowledge base (ledger + ubiquitous language + context map), unify onboarding into the `atm manager` command tree via a `--onboard` flag, replace the TUI placeholder "bubbles" chart with a real "Ubiquitous Language" chart that reads a per-project `vocabulary.json` written by the manager, and reframe the manager's self-improvement gene as cross-project.

**Architecture:** A new `internal/store/vocabulary.go` adds `Vocabulary` + `GetVocabulary`/`WriteVocabulary` (per-project `vocabulary.json`, lock-guarded). `internal/manager/context_v1.md` is rewritten around the knowledge-base-owner principle with an env-conditional onboarding section activated by `ATM_ONBOARD=1`; `internal/manager/launcher.go` gains `BuildArgvOnboard`. `internal/cli/manager.go` adds the `--onboard` flag and `ATM_ONBOARD=1`; `internal/cli/onboarding.go` + `internal/onboard/` are deleted. `internal/tui/projects.go` renames the third chart box `bubbles` -> `Ubiquitous Language` and renders weighted bubbles from `store.GetVocabulary` (empty state when absent); `renderSampleBubbleCanvas` is deleted. A new `internal/cli/vocabulary.go` adds `atm vocabulary show`/`write` subcommands.

**Tech Stack:** Go 1.22+, cobra CLI, Bubble Tea/lipgloss TUI, `github.com/NimbleMarkets/ntcharts/canvas`, existing `internal/store` + `internal/manager`.

## Global Constraints

- Go module path is `atm` (imports are `atm/internal/...`).
- Store resolution: `--store` > `ATM_HOME` > `~/.config/atm`. Mutating CLI commands require `--actor` or `ATM_ACTOR`. JSON output is deterministic (sorted keys, RFC3339 UTC) via `store.MarshalSorted`/`WriteFileAtomic`.
- Per-project file locking via `s.WithLock(code, fn)`. `vocabulary.json` lives at `$ATM_HOME/projects/<CODE>/vocabulary.json` (use `s.projectDir(code)`), NOT under `personas/`.
- Vocabulary is a derived artifact like `cache.db`: written via `store.WriteVocabulary` (lock-guarded, atomic), NOT appended to `log.jsonl`. No new log action, no `Replay` change.
- Vocabulary weight is a normalized frequency integer 1-10; top-N cap is 12 terms.
- No emojis in code or commits. No comments unless asked. Follow neighboring-file style.
- Manager prompt substitution: `RenderContext` substitutes `<CODE>`,`<PROJECT_NAME>`,`<ATM_BIN>`,`<ACTOR>`,`<RUN_ID>`,`<TIMESTAMP>`; empty values leave the placeholder in place (env-driven generic body for subagent definitions).
- Subagent dispatch inherits `ATM_ROLE=developing` from the parent session; the manager prompt must NOT gate on `ATM_ROLE`. The onboarding section gates on `ATM_ONBOARD` (set by the `--onboard` launcher flag, not by subagent dispatch).
- `ATM_ONBOARD=1` is the env signal that activates the onboarding responsibility in the rendered manager context. It is set only by the `atm manager <host> --project <CODE> --onboard` CLI path.
- Test harnesses: `newGoldenHarness(t)` / `newGoldenHarnessAt(t, storePath)` in `internal/cli/harness_test.go`; `compareGolden(t, name, got)` with `testdata/golden/<name>.json` (regenerate with `-update`). `normalizeOutput`, `normalizeManagerOutput`, `normalizeHome` exist. TUI helpers: `newTestModel(t)`, `newTestModelWithActor(t, actor)`, `seedProject`, `seedTask`, `update`, `mustContain`, `mustNotContain` in `internal/tui/app_test.go`.
- Run `make build && make test` (a.k.a. `make verify`) green before each commit. The gate is `make verify`.

---

## File Structure

- `internal/store/vocabulary.go` (new) — `Vocabulary`/`VocabularyTerm` types, `GetVocabulary(code)`, `WriteVocabulary(code, v)`, `vocabularyPath(code)`.
- `internal/store/vocabulary_test.go` (new) — round-trip, missing-file, malformed-JSON, actor-stamp tests.
- `internal/manager/context_v1.md` (rewrite) — knowledge-base-owner reframe + env-conditional onboarding section + cross-project self-learning gene + vocabulary responsibility.
- `internal/manager/context.go` (modify) — no signature change; the render already substitutes placeholders. (Possibly no code change; the prompt file is the change. Confirm during Task 3.)
- `internal/manager/context_test.go` (modify) — update expected fragments for the reframed prompt (knowledge-base owner, onboarding, vocabulary); assert the onboarding section is present with `ATM_ONBOARD` framing.
- `internal/manager/launcher.go` (modify) — add `BuildArgvOnboard(contextPath string) []string` to the `Launcher` interface and all implementations; add `onboardingMessagePrefix/Suffix` (generalized).
- `internal/manager/launcher_test.go` (modify) — `BuildArgvOnboard` produces `--auto --prompt <msg>` for opencode/ollama.
- `internal/cli/manager.go` (modify) — add `--onboard` flag to `managerOpts` and all agent subcommands; in `runManager`, select `BuildArgvOnboard` + set `ATM_ONBOARD=1` when `--onboard`; add `tmuxLabelOnboarding` window label; remove the onboarding-header label collision.
- `internal/cli/vocabulary.go` (new) — `atm vocabulary` command tree: `show --project <CODE>` and `write --project <CODE> --actor <ACTOR> --terms <json>`.
- `internal/cli/vocabulary_test.go` (new) — `show` empty-state, `show` present, `write` round-trip, `write` malformed-terms, missing `--actor`.
- `internal/cli/root.go` (modify) — register `newVocabularyCmd(st)`; remove `newOnboardingCmd(st)`.
- `internal/cli/onboarding.go` (delete) — superseded by `--onboard` on `atm manager`.
- `internal/cli/tmux.go` (modify) — keep `tmuxLabelOnboarding` constant (now used by `--onboard`); update the doc comment.
- `internal/onboard/` (delete) — `embed.go`, `embed_test.go`, `launcher.go`, `launcher_test.go`, `prompt_opencode_v1.md`. Content absorbed into the manager prompt + `internal/manager/launcher.go`.
- `internal/cli/onboarding_test.go` (delete if present) — superseded by manager `--onboard` tests.
- `internal/cli/manager_test.go` (modify) — add `--onboard` dry-run golden for opencode + ollama; assert `ATM_ONBOARD=1` in env; assert non-onboard dry-run unchanged; regression: no `atm onboarding` command.
- `internal/cli/testdata/golden/manager-onboard-dry-run-opencode.json` (new) — golden for `manager opencode --project FOO --onboard --dry-run`.
- `internal/cli/testdata/golden/manager-onboard-dry-run-ollama.json` (new) — golden for `manager ollama --project FOO --integration opencode --onboard --dry-run`.
- `internal/tui/projects.go` (modify) — `renderBubbleChart` -> `renderUbiquitousLanguageChart(vocab *store.Vocabulary, maxLines int)`; delete `renderSampleBubbleCanvas`; rename "bubbles" title to "Ubiquitous Language"; add `vocab` to `projectSummaryData` return.
- `internal/tui/projects_test.go` (modify) — `renderUbiquitousLanguageChart` empty state + weighted bubbles + deterministic placement.
- `internal/tui/app_test.go` (modify) — `TestSelectedProjectSummaryRendersCharts` and `TestKeywordSummaryDoesNotOpenFormOrConfirm` assert `Ubiquitous Language` + empty state (no `events`/`agents` sample bubbles); `TestRenderSampleBubbleCanvasShowsPlaceholders` deleted.
- `README.md` (modify) — replace the "Onboarding" section with "Manager onboarding" documenting `atm manager <host> --project <CODE> --onboard`; add a "Vocabulary" subsection documenting `atm vocabulary show/write` and the TUI chart.
- `scripts/onboard-smoke.sh` (modify) — replace `atm onboarding ...` calls with `atm manager <host> --project FOO --onboard ...`.
- `docs/superpowers/specs/2026-07-04-tui-project-summary-charts-design.md` (modify) — update Chart 3 description: no longer a placeholder; renamed "Ubiquitous Language"; reads `vocabulary.json`.

---

## Task 1: store vocabulary read/write helpers

**Files:**
- Create: `internal/store/vocabulary.go`
- Test: `internal/store/vocabulary_test.go`

**Interfaces:**
- Consumes: `s.projectDir(code)`, `s.WithLock(code, fn)`, `ReadJSON`, `WriteFileAtomic`, `Now()`, `os.IsNotExist`.
- Produces:
  - `type VocabularyTerm struct { Term string; Weight int }`
  - `type Vocabulary struct { UpdatedAt time.Time; Actor string; Terms []VocabularyTerm }`
  - `func (s *Store) GetVocabulary(code string) (*Vocabulary, error)` — missing file -> `(nil, nil)`; malformed JSON -> error.
  - `func (s *Store) WriteVocabulary(code string, v *Vocabulary) error` — stamps `UpdatedAt = Now()`, requires `v.Actor != ""`, writes under `WithLock(code, ...)`.
  - `func (s *Store) vocabularyPath(code string) string`

- [ ] **Step 1: Write the failing test**

Create `internal/store/vocabulary_test.go`:

```go
package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetVocabularyMissingFileReturnsNilNil(t *testing.T) {
	s := openTempStore(t)
	got, err := s.GetVocabulary("FOO")
	if err != nil || got != nil {
		t.Fatalf("GetVocabulary(missing) = (%v, %v), want (nil, nil)", got, err)
	}
}

func TestWriteVocabularyRoundTrips(t *testing.T) {
	s := openTempStore(t)
	if _, err := s.CreateProject("FOO", "Foo", "tester"); err != nil {
		t.Fatal(err)
	}
	in := &Vocabulary{
		Actor: "opencode-manager",
		Terms: []VocabularyTerm{
			{Term: "labels", Weight: 9},
			{Term: "audit log", Weight: 7},
		},
	}
	if err := s.WriteVocabulary("FOO", in); err != nil {
		t.Fatalf("WriteVocabulary: %v", err)
	}
	got, err := s.GetVocabulary("FOO")
	if err != nil || got == nil {
		t.Fatalf("GetVocabulary after write = (%v, %v), want non-nil", got, err)
	}
	if got.Actor != in.Actor {
		t.Errorf("Actor = %q, want %q", got.Actor, in.Actor)
	}
	if len(got.Terms) != 2 || got.Terms[0].Term != "labels" || got.Terms[0].Weight != 9 {
		t.Errorf("Terms = %#v, want [{labels 9} {audit log 7}]", got.Terms)
	}
	if got.UpdatedAt.IsZero() {
		t.Errorf("UpdatedAt should be stamped by WriteVocabulary")
	}
}

func TestWriteVocabularyRequiresActor(t *testing.T) {
	s := openTempStore(t)
	if _, err := s.CreateProject("FOO", "Foo", "tester"); err != nil {
		t.Fatal(err)
	}
	err := s.WriteVocabulary("FOO", &Vocabulary{Actor: "", Terms: []VocabularyTerm{{Term: "x", Weight: 1}}})
	if err == nil || !strings.Contains(err.Error(), "actor") {
		t.Fatalf("WriteVocabulary(empty actor) err = %v, want actor-required error", err)
	}
}

func TestGetVocabularyMalformedJSONReturnsError(t *testing.T) {
	s := openTempStore(t)
	if _, err := s.CreateProject("FOO", "Foo", "tester"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.projectDir("FOO"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.vocabularyPath("FOO"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetVocabulary("FOO")
	if err == nil {
		t.Fatalf("GetVocabulary(malformed) = (%v, nil), want error", got)
	}
}

// openTempStore builds an initialized store in a temp dir.
func openTempStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(""); err != nil {
		t.Fatal(err)
	}
	return s
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestGetVocabularyMissingFileReturnsNilNil -v`
Expected: FAIL — `vocabulary.go` does not exist (compile error: `GetVocabulary` undefined, `Vocabulary` undefined).

- [ ] **Step 3: Write minimal implementation**

Create `internal/store/vocabulary.go`:

```go
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type VocabularyTerm struct {
	Term   string `json:"term"`
	Weight int    `json:"weight"`
}

type Vocabulary struct {
	UpdatedAt time.Time        `json:"updated_at"`
	Actor     string           `json:"actor"`
	Terms     []VocabularyTerm `json:"terms"`
}

func (s *Store) vocabularyPath(code string) string {
	return filepath.Join(s.projectDir(code), "vocabulary.json")
}

// GetVocabulary reads <store>/projects/<CODE>/vocabulary.json. A missing file
// returns (nil, nil) so callers can treat it as the empty-state case. A
// malformed file returns the decode error.
func (s *Store) GetVocabulary(code string) (*Vocabulary, error) {
	var v Vocabulary
	if err := ReadJSON(s.vocabularyPath(code), &v); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &v, nil
}

// WriteVocabulary stamps UpdatedAt and writes vocabulary.json under the
// project's per-project lock. Actor is required.
func (s *Store) WriteVocabulary(code string, v *Vocabulary) error {
	if v.Actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	v.UpdatedAt = Now()
	return s.WithLock(code, func() error {
		if err := os.MkdirAll(s.projectDir(code), 0o755); err != nil {
			return err
		}
		return WriteFileAtomic(s.vocabularyPath(code), v)
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run 'TestGetVocabulary|TestWriteVocabulary' -v`
Expected: PASS for all four tests.

- [ ] **Step 5: Run full store package tests + commit**

Run: `go test ./internal/store/ -v` (ensure no regressions).
Then:
```bash
git add internal/store/vocabulary.go internal/store/vocabulary_test.go
git commit -m "Add store vocabulary read/write helpers (ATM-0028)"
```

---

## Task 2: `atm vocabulary` CLI subcommands

**Files:**
- Create: `internal/cli/vocabulary.go`
- Test: `internal/cli/vocabulary_test.go`
- Modify: `internal/cli/root.go` (register `newVocabularyCmd`)

**Interfaces:**
- Consumes: `st.openStore()`, `s.GetVocabulary(code)`, `s.WriteVocabulary(code, v)`, `st.resolveActor(true)`, `st.emit`, `ErrNotFound`, `ErrUsage`, `store.IsNotFound`, `store.RFC3339UTC`.
- Produces: `atm vocabulary show --project <CODE>` and `atm vocabulary write --project <CODE> --actor <ACTOR> --terms <json>`.

- [ ] **Step 1: Write the failing test**

Create `internal/cli/vocabulary_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

func TestVocabularyShowEmptyStateJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	out, _, code := h.run("vocabulary", "show", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, `"vocabulary"`) || strings.Contains(out, `"terms": [") {
		t.Fatalf("empty-state show output should have null vocabulary; got:\n%s", out)
	}
}

func TestVocabularyWriteThenShow(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("vocabulary", "write", "--project", "FOO", "--actor", "opencode-manager",
		"--terms", `[{"term":"labels","weight":9},{"term":"audit log","weight":7}]`)
	if code != ExitSuccess {
		t.Fatalf("write exit = %d, want 0", code)
	}
	h.reset()
	out, _, code := h.run("vocabulary", "show", "--project", "FOO")
	if code != ExitSuccess {
		t.Fatalf("show exit = %d, want 0", code)
	}
	if !strings.Contains(out, `"labels"`) || !strings.Contains(out, `"audit log"`) {
		t.Fatalf("show after write missing terms; got:\n%s", out)
	}
}

func TestVocabularyWriteRejectsMalformedTerms(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("vocabulary", "write", "--project", "FOO", "--actor", "x",
		"--terms", `not json`)
	if code != ExitUsage {
		t.Fatalf("malformed terms exit = %d, want %d (usage)", code, ExitUsage)
	}
}

func TestVocabularyWriteRequiresActor(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("vocabulary", "write", "--project", "FOO",
		"--terms", `[{"term":"x","weight":1}]`)
	if code != ExitUsage {
		t.Fatalf("no-actor write exit = %d, want %d (usage)", code, ExitUsage)
	}
}

func TestVocabularyShowMissingProject(t *testing.T) {
	h := newGoldenHarness(t)
	h.reset()
	_, _, code := h.run("vocabulary", "show", "--project", "NOPE")
	if code != ExitNotFound {
		t.Fatalf("show missing project exit = %d, want %d", code, ExitNotFound)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestVocabulary -v`
Expected: FAIL — `newVocabularyCmd` undefined (compile error).

- [ ] **Step 3: Write minimal implementation**

Create `internal/cli/vocabulary.go`:

```go
package cli

import (
	"encoding/json"
	"fmt"

	"atm/internal/store"

	"github.com/spf13/cobra"
)

func newVocabularyCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{Use: "vocabulary", Short: "Project vocabulary (ubiquitous language) commands"}
	cmd.AddCommand(newVocabularyShowCmd(st))
	cmd.AddCommand(newVocabularyWriteCmd(st))
	return cmd
}

func newVocabularyShowCmd(st *cliState) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show a project's vocabulary (ubiquitous language)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if _, err := s.GetProject(project); err != nil {
				return fmt.Errorf("%w: project %s not found", ErrNotFound, project)
			}
			vocab, err := s.GetVocabulary(project)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"vocabulary": vocab}, func() {
				if vocab == nil || len(vocab.Terms) == 0 {
					fmt.Fprintln(st.stdout(), "no vocabulary yet")
					return
				}
				fmt.Fprintf(st.stdout(), "updated: %s  actor: %s\n", store.RFC3339UTC(vocab.UpdatedAt), vocab.Actor)
				for _, term := range vocab.Terms {
					fmt.Fprintf(st.stdout(), "%3d  %s\n", term.Weight, term.Term)
				}
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newVocabularyWriteCmd(st *cliState) *cobra.Command {
	var project, termsJSON string
	cmd := &cobra.Command{
		Use:   "write",
		Short: "Write a project's vocabulary (ubiquitous language)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			var terms []store.VocabularyTerm
			if err := json.Unmarshal([]byte(termsJSON), &terms); err != nil {
				return fmt.Errorf("%w: --terms must be JSON array of {term,weight}: %v", ErrUsage, err)
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if _, err := s.GetProject(project); err != nil {
				return fmt.Errorf("%w: project %s not found", ErrNotFound, project)
			}
			v := &store.Vocabulary{Actor: actor, Terms: terms}
			if err := s.WriteVocabulary(project, v); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{
				"project":   project,
				"terms":     len(terms),
				"updated_at": store.RFC3339UTC(v.UpdatedAt),
			}, func() {
				fmt.Fprintf(st.stdout(), "wrote %d terms to vocabulary for %s\n", len(terms), project)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&termsJSON, "terms", "", `JSON array of {"term":"...","weight":N}`)
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("terms")
	return cmd
}
```

Register in `internal/cli/root.go`: add `root.AddCommand(newVocabularyCmd(st))` after the `newTaskCmd(st)` line.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestVocabulary -v`
Expected: PASS for all five tests.

- [ ] **Step 5: Run full cli package tests + commit**

Run: `go test ./internal/cli/ -v` (ensure no regressions).
Then:
```bash
git add internal/cli/vocabulary.go internal/cli/vocabulary_test.go internal/cli/root.go
git commit -m "Add atm vocabulary show/write CLI (ATM-0028)"
```

---

## Task 3: Reframe manager prompt as knowledge-base owner

**Files:**
- Modify: `internal/manager/context_v1.md` (rewrite)
- Test: `internal/manager/context_test.go` (update expected fragments)

**Interfaces:**
- Consumes: `RenderContext(ContextData)` (unchanged signature), the existing placeholder substitution.
- Produces: a reframed prompt with the knowledge-base-owner role, an env-conditional onboarding section (gated on `ATM_ONBOARD`), a vocabulary responsibility, and a cross-project self-learning gene.

- [ ] **Step 1: Write the failing test**

Update `internal/manager/context_test.go` `TestRenderContextSubstitutesAllPlaceholders` — replace the `want` slice and add a new test for the onboarding framing:

```go
func TestRenderContextSubstitutesAllPlaceholders(t *testing.T) {
	got := RenderContext(ContextData{
		Code:      "ATM",
		Name:      "Agent Tasks Management",
		ATMBin:    "/usr/local/bin/atm",
		Actor:     "opencode-manager",
		RunID:     "ATM-20260706120000-a1b2c3",
		Timestamp: "2026-07-06T12:00:00Z",
	})
	for _, placeholder := range []string{
		"<CODE>", "<PROJECT_NAME>", "<ATM_BIN>", "<ACTOR>",
		"<RUN_ID>", "<TIMESTAMP>",
	} {
		if strings.Contains(got, placeholder) {
			t.Errorf("rendered context still contains %s", placeholder)
		}
	}
	for _, want := range []string{
		"ATM manager session ATM-20260706120000-a1b2c3",
		"Project: `ATM` (`Agent Tasks Management`)",
		"ATM binary: `/usr/local/bin/atm`",
		"Actor: `opencode-manager`",
		"knowledge-base owner",
		"ubiquitous language",
		"vocabulary.json",
		"onboarding",
		"ATM_ONBOARD",
		"needs clarification",
		"cross-project",
		"atm vocabulary write",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered context missing %q", want)
		}
	}
	for _, mustNot := range []string{
		"agent-generated keyword bubbles pending",
	} {
		if strings.Contains(got, mustNot) {
			t.Errorf("rendered context should not contain %q", mustNot)
		}
	}
}

func TestRenderContextOnboardingSectionIsEnvConditional(t *testing.T) {
	got := RenderContext(ContextData{Code: "ATM", ATMBin: "/bin/atm"})
	if !strings.Contains(got, "ATM_ONBOARD") {
		t.Errorf("onboarding section must reference ATM_ONBOARD as its activation signal")
	}
	if !strings.Contains(got, "onboarding") {
		t.Errorf("onboarding responsibility must be present in the prompt")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/manager/ -run TestRenderContext -v`
Expected: FAIL — the current `context_v1.md` lacks `knowledge-base owner`, `ubiquitous language`, `vocabulary.json`, `ATM_ONBOARD`, `cross-project`, `atm vocabulary write`.

- [ ] **Step 3: Rewrite the manager prompt**

Replace the entire contents of `internal/manager/context_v1.md` with:

```markdown
# ATM manager session <RUN_ID>

Project: `<CODE>` (`<PROJECT_NAME>`)
Started: `<TIMESTAMP>`
ATM binary: `<ATM_BIN>`
Actor: `<ACTOR>`

## Role

You are the **knowledge-base owner** for project `<CODE>`
(`<PROJECT_NAME>`). The knowledge base has three parts, and you own and keep
all three coherent:

- **The ledger** — tasks, labels, status, titles, comments. You keep the
  ledger's shape: consistent labels, clear searchable titles, accurate
  status, prioritized work, and comments that capture intent and progress
  rather than chat.
- **The ubiquitous language** — the project's recurring domain terms, mined
  from task titles, descriptions, and comments, persisted to
  `vocabulary.json`. You compute and refresh this vocabulary.
- **The context map** — repo pointers captured during onboarding (agent
  harness setup, structure, document/code pointers, findings, open
  questions). You build this by onboarding repos into the project.

You are a full ATM CLI actor via `<ATM_BIN>` — you create tasks, add
comments, adjust labels, transition status, rewrite titles, split tasks
into subtasks, merge related tasks, and write the project vocabulary. Stamp
every mutating write with actor `<ACTOR>`.

You will later answer inquiries against this knowledge base (search/query is
a future capability; this prompt declares it as your remit so you are
forward-compatible). Today you maintain the knowledge base and surface what
it holds.

## Mode-driven pacing

You run in one of three modes, and the mode sets your pacing:

- **Subagent mode (fast)**: a developing agent dispatched you mid-work with
  a track request. Optimize for a fast, useful ledger write and a short
  confirmation. Do not over-deliberate. Make a reasonable call, write it,
  return. The developing agent is waiting briefly and does not depend on
  your reply.
- **Interactive mode (thorough)**: a human launched you via
  `atm manager <host> --project <CODE>` to consult or steer you. Optimize
  for a thorough review. Dig into the ledger, propose splits/merges, rewrite
  titles for clarity, surface staleness and priority, sum up long
  discussions into structured comments, recompute the vocabulary when asked,
  and ask the human to clarify when something is genuinely ambiguous.
- **Onboarding mode (non-interactive)**: you were launched with `--onboard`
  against a target repo in your current working directory (the
  `ATM_ONBOARD=1` env signal is set). Read the repo, build the context map,
  compute the vocabulary in the same pass, and return. Do not ask the human
  questions; make reasonable judgment calls and proceed.

In all modes you do not ask the developing agent back. In subagent mode, if
a track request is ambiguous, make the most reasonable interpretation,
write that, and optionally leave a short "needs clarification" note on the
task for the human to resolve in an interactive session. In interactive
mode, ask the human directly. In onboarding mode, proceed non-interactively.

## Track pipeline (subagent mode)

A track request arrives as your prompt: an optional advisory hint line of
the form `hint: <word>` followed by a freeform message. The hint is a short
string from this open set: `feature`, `bug`, `design`, `spec`, `chore`,
`investigation`, `decision`, `progress`, `blocker`, `handoff`, `question`,
`vocabulary`. Unknown or missing hints are fine; fall back to interpreting
the freeform message alone.

On receiving a track request, work quickly:

1. Read `ATM_PROJECT`, `ATM_BIN`, `ATM_ACTOR` from your environment. If
   `ATM_PROJECT` is unset, you were loaded outside any ATM session — stay
   silent. Do not gate on `ATM_ROLE`: in subagent mode the env is inherited
   from the developing session and `ATM_ROLE` will be `developing`, not
   `manager`. Being loaded as the `atm-manager` agent is the role signal.
2. Skim the current ledger for the project: open tasks, recent comments,
   labels. Find the task this track call most likely extends.
3. Decide the formal action and write it:
   - Append a progress/comment to an existing open task (the common case).
   - Create a new task if the track call clearly starts a new unit of work.
   - Adjust labels (add priority, transition status) when the hint or
     message signals it (`blocker`, `decision`, etc.).
   - Recompute the vocabulary when the hint is `vocabulary` (see
     Vocabulary responsibility).
   - Split a task into subtasks only when the track call clearly spans
     unrelated work that the original task conflated.
4. Simplify titles and descriptions when you touch a task. Rewrite the
   title so a future agent's semantic search will find it: name the concept,
   not the transient activity. Keep titles short.
5. Sum up discussion into the comment. If the track message is a long
   chat-like dump, distill it into one or two lines of structured progress
   note, not a verbatim copy.
6. If the request is genuinely ambiguous in a way that changes which task
   or action is correct, make your best guess, write it, and add a one-line
   `needs clarification` comment on the task. Do not block.
7. Return a concise confirmation: which task(s) you touched, what action you
   took, and the task ID(s). Do not summarize the track message back.

## Vocabulary responsibility

You own the project's ubiquitous language: the recurring domain nouns and
proper terms mined from task titles, descriptions, and comments. You extract
them using your own language understanding — identify the terms that name
what this project is about, dedupe, rank by frequency, normalize weights to
a 1-10 scale, cap at 12 terms, and write `vocabulary.json`.

Inputs: task titles + descriptions + comments (read via
`<ATM_BIN> task list --project <CODE> --output json` and
`<ATM_BIN> task comment list --task <ID> --output json`). Do not mine the
audit log action strings or persona prompts; the vocabulary is the project's
domain language, not its process substrate.

Output: a weighted term list written via
`<ATM_BIN> vocabulary write --project <CODE> --actor <ACTOR> --terms <json>`
where `<json>` is a JSON array of `{"term":"...","weight":N}` objects. The
CLI stamps `updated_at`; you supply `term` and `weight` only.

Recompute is explicit. Recompute during an onboarding pass, in an
interactive session when the human asks ("recompute the vocabulary"), or on
a subagent track call with a `vocabulary` hint. Do not recompute implicitly
on every touch — the vocabulary is a snapshot, not a live aggregate.

## Onboarding responsibility

When `ATM_ONBOARD=1` is set in your environment, perform onboarding for the
project against the repo in your current working directory. Work
non-interactively; do not ask the human questions.

1. **Orient as an ATM agent first.** Run `<ATM_BIN> conventions` and read
   it. It tells you how ATM projects are organized (the label substrate,
   the first-contact sequence a later agent will follow, the advisory seed
   namespaces). You are building exactly the context that `conventions`
   describes a fresh agent consuming.
2. **Research already-captured knowledge.** Run
   `<ATM_BIN> task list --project <CODE> --output json` and read existing
   tasks' titles, labels, and descriptions. Also run
   `<ATM_BIN> store log <CODE>` to read the project's audit log and observe
   recent activity before reconciling. This project may already have
   context from other repositories; reconcile rather than duplicate.
3. **Explore the repo** breadth-first and budget-bounded. List top-level
   files/dirs; read README; read docs/ if present; sample representative
   source files. Do not read every file. Stop when the obvious surface is
   covered.
4. **Capture findings as ATM tasks**, label-agnostically (match findings to
   label descriptions from `<ATM_BIN> label list --project <CODE> --output json`):
   - Agent harness setup, structure, document/code pointers, findings, open
     questions (each as its own task), no duplication.
   - Cap work-task creation at ~20 per run; further findings go into a
     single aggregate task whose description lists them.
5. **Idempotency.** Before each `<ATM_BIN> task create`, match against
   existing tasks AND what you have created this run, by title and topical
   overlap. Update via `<ATM_BIN> task set-description` and
   `<ATM_BIN> task label add` rather than duplicating. If repos disagree
   about the same thing, prefer a task whose description names the
   disagreement (or a `context:question` task) over silently picking a
   side.
6. **Compute the vocabulary in the same pass.** From the task
   titles/descriptions/comments you just created (plus any pre-existing
   ones), extract the recurring domain nouns/proper terms, dedupe, rank by
   frequency, normalize weights to 1-10, cap at 12, and write
   `vocabulary.json` via `<ATM_BIN> vocabulary write --project <CODE>
   --actor <ACTOR> --terms <json>` (see Vocabulary responsibility).
7. **Summary.** Print a one-paragraph natural-language summary of what you
   created/updated, reconciliations made, and the vocabulary written. This
   is the human's onboarding receipt.

No `<EXISTING_TASKS>` snapshot is embedded in your prompt; the live
`<ATM_BIN> task list --output json` is your reconciliation baseline.

## Ledger hygiene

- Use the project's label conventions consistently. Run
  `<ATM_BIN> label list --project <CODE> --output json` if you are unsure
  which labels exist.
- Status is a label axis (`<CODE>:status:<state>`), not a field. Do not
  invent status values; reuse the project's existing status labels or add a
  new one only when the work genuinely introduces a new state.
- Keep titles concise, accurate, and searchable. A good title names the
  concept ("Refactor label resolver to handle hierarchical prefixes") not
  the moment ("working on labels"). Update titles when work drifts from the
  original framing.
- Comments record intent, progress, decisions, files changed, test
  results, blockers, commit SHAs, and handoff notes. Distill chat-like
  input into structured notes. A track call that says "still working on X,
  hit a snag with Y" becomes `Progress: working on X. Blocker: Y needs
  resolution.`, not a paragraph.
- Surface priority when you see it. If a track call describes a blocker or
  a regression, add the appropriate priority label and name the task in
  your confirmation.

## Interactive mode (human → manager)

When launched as `atm manager <host>`, take your time and review the
project thoroughly. Typical asks:

- "What's stale?" — query open tasks with old `updated_at`, surface them,
  propose cleanup (close, merge, or re-prioritize).
- "What should I work on next?" — rank open tasks by priority/status and
  recent activity.
- "Reconcile labels on ATM-0024." — adjust labels per the human's steering.
- "Recompute the vocabulary." — read task titles/descriptions/comments,
  extract the ubiquitous language, write `vocabulary.json`.
- "Summarize the last session's ledger activity." — read recent comments
  and produce a digest.
- "Split ATM-0017 into subtasks." — break a conflated task into focused
  subtasks with clear titles.
- "Merge ATM-0018 and ATM-0019." — combine related tasks, preserving
  comment history.

Answer in dialogue. Propose writes and execute them when the human agrees.
Ask the human to clarify when something is genuinely ambiguous — that is
what interactive mode is for. Do not write code or modify repo files; you
only touch the ATM ledger and the vocabulary.

## Self-learning & cross-project practices

After every manager session — regardless of mode — log one self-improvement
task before returning. The lens is cross-project: capture common practices
that are reusable across projects, especially how logic is reused through
the label substrate (label conventions that worked, vocabulary-extraction
patterns that generalized, onboarding heuristics that applied across repos).
You suggest best management practices, not just per-project notes.

- Stamp the observation's origin (which project/session surfaced it) but
  frame the improvement as reusable across projects.
- If a task already covers that improvement, add a comment noting the new
  evidence and skip creating a duplicate.
- Otherwise create a new task titled to name the improvement
  ("Manager: <change>"), with `type:chore` and the project's default open
  status, whose description captures: (a) the dynamic observed, (b) the
  proposed change to the manager prompt, a label convention, or a workflow,
  and (c) why it would make this or a future session smoother across
  projects. Stamp it with actor `<ACTOR>`.
- Keep it cheap — one task per session, distilled to a few lines. Do not
  deliberate at length; the value is in the durable record of a real
  observation, not a polished proposal.
- Include the new task's ID in your confirmation so the developing agent
  and the human can see the manager is improving itself in the open, on the
  ledger.

This gene is non-optional. A manager session that does not end with a
self-improvement task logged (or a comment added to an existing one) is
incomplete.

## Commands

`<ATM_BIN>` is the ATM binary path from env (default `atm`); substitute it
in each command below.

- `<ATM_BIN> conventions`
- `<ATM_BIN> label list --project <CODE> --output json`
- `<ATM_BIN> task list --project <CODE> --output json`
- `<ATM_BIN> task show --id <ID> --output json`
- `<ATM_BIN> task create --project <CODE> --title "<title>" --label <CODE>:status:open --actor <ACTOR>`
- `<ATM_BIN> task comment add --task <ID> --body "<progress note>" --actor <ACTOR>`
- `<ATM_BIN> task comment list --task <ID> --output json`
- `<ATM_BIN> task label add --task <ID> --label <LABEL> --actor <ACTOR>`
- `<ATM_BIN> task label remove --task <ID> --label <LABEL> --actor <ACTOR>`
- `<ATM_BIN> task set-title --id <ID> --title "<title>" --actor <ACTOR>`
- `<ATM_BIN> task set-description --id <ID> --description "<desc>" --actor <ACTOR>`
- `<ATM_BIN> task remove --id <ID> --actor <ACTOR>`
- `<ATM_BIN> vocabulary show --project <CODE> --output json`
- `<ATM_BIN> vocabulary write --project <CODE> --actor <ACTOR> --terms <json>`

## Code of conduct

Follow repo instructions, existing skills, harness rules, tool
permissions, and user directions first. ATM is the knowledge base you own;
it is not a workflow that overrides the host's normal rules. If
instructions conflict, preserve the normal agent/repo instruction hierarchy
and use ATM where compatible.
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/manager/ -run TestRenderContext -v`
Expected: PASS for both tests.

- [ ] **Step 5: Run full manager package tests + commit**

Run: `go test ./internal/manager/ -v` (ensure no regressions).
Then:
```bash
git add internal/manager/context_v1.md internal/manager/context_test.go
git commit -m "Reframe manager prompt as knowledge-base owner (ATM-0028)"
```

---

## Task 4: Manager launcher `BuildArgvOnboard`

**Files:**
- Modify: `internal/manager/launcher.go`
- Test: `internal/manager/launcher_test.go`

**Interfaces:**
- Consumes: existing `staticLauncher`, `OllamaLauncher`.
- Produces: `BuildArgvOnboard(contextPath string) []string` on the `Launcher` interface and all implementations; generalized `managerMessagePrefix/Suffix`.

- [ ] **Step 1: Write the failing test**

Add to `internal/manager/launcher_test.go`:

```go
func TestBuildArgvOnboardOpencode(t *testing.T) {
	l, ok := LauncherFor("opencode")
	if !ok {
		t.Fatal("LauncherFor(opencode) not found")
	}
	got := l.BuildArgvOnboard("/tmp/ctx.md")
	if len(got) < 4 || got[0] != "opencode" || got[1] != "--auto" || got[2] != "--prompt" {
		t.Fatalf("opencode BuildArgvOnboard = %v, want [--auto --prompt <msg>]", got)
	}
	if !strings.Contains(got[3], "/tmp/ctx.md") {
		t.Fatalf("opencode onboard prompt message should reference the context path; got %q", got[3])
	}
}

func TestBuildArgvOnboardOllama(t *testing.T) {
	l := OllamaLauncher{Integration: "opencode"}
	got := l.BuildArgvOnboard("/tmp/ctx.md")
	if len(got) < 6 || got[0] != "ollama" || got[1] != "launch" || got[2] != "opencode" || got[3] != "--" {
		t.Fatalf("ollama BuildArgvOnboard = %v, want [ollama launch opencode -- --auto --prompt <msg>]", got)
	}
}
```

Add `"strings"` to the imports if not already present.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/manager/ -run TestBuildArgvOnboard -v`
Expected: FAIL — `BuildArgvOnboard` undefined on the `Launcher` interface (compile error).

- [ ] **Step 3: Write minimal implementation**

Replace `internal/manager/launcher.go` with:

```go
package manager

type Launcher interface {
	Name() string
	NotFoundHint() string
	BuildArgv() []string
	BuildArgvOnboard(contextPath string) []string
}

type staticLauncher struct {
	name string
	hint string
	argv []string
}

func (l staticLauncher) Name() string         { return l.name }
func (l staticLauncher) NotFoundHint() string { return l.hint }
func (l staticLauncher) BuildArgv() []string  { return append([]string(nil), l.argv...) }

func (l staticLauncher) BuildArgvOnboard(contextPath string) []string {
	msg := managerMessagePrefix + contextPath + managerMessageSuffix
	return []string{l.name, "--auto", "--prompt", msg}
}

func LauncherFor(name string) (Launcher, bool) {
	switch name {
	case "opencode":
		return staticLauncher{name: "opencode", hint: "https://opencode.ai", argv: []string{"opencode"}}, true
	case "codex":
		return staticLauncher{name: "codex", hint: "https://developers.openai.com/codex", argv: []string{"codex"}}, true
	case "claude":
		return staticLauncher{name: "claude", hint: "https://code.claude.com", argv: []string{"claude"}}, true
	default:
		return nil, false
	}
}

type OllamaLauncher struct {
	Integration string
}

func (l OllamaLauncher) Name() string         { return "ollama" }
func (l OllamaLauncher) NotFoundHint() string { return "https://ollama.com" }
func (l OllamaLauncher) BuildArgv() []string {
	return []string{"ollama", "launch", l.Integration, "--"}
}

func (l OllamaLauncher) BuildArgvOnboard(contextPath string) []string {
	msg := managerMessagePrefix + contextPath + managerMessageSuffix
	return []string{"ollama", "launch", l.Integration, "--",
		"--auto", "--prompt", msg}
}

const (
	managerMessagePrefix = "Read the manager instructions in the file at "
	managerMessageSuffix = " and follow them exactly."
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/manager/ -run TestBuildArgvOnboard -v`
Expected: PASS for both tests.

- [ ] **Step 5: Run full manager package tests + commit**

Run: `go test ./internal/manager/ -v` (ensure no regressions).
Then:
```bash
git add internal/manager/launcher.go internal/manager/launcher_test.go
git commit -m "Add BuildArgvOnboard to manager launcher (ATM-0028)"
```

---

## Task 5: `--onboard` flag on `atm manager` + delete onboarding

**Files:**
- Modify: `internal/cli/manager.go` (add `--onboard` flag, select `BuildArgvOnboard`, set `ATM_ONBOARD=1`, tmux label)
- Modify: `internal/cli/root.go` (remove `newOnboardingCmd(st)`)
- Delete: `internal/cli/onboarding.go`, `internal/onboard/` (whole package), `internal/cli/onboarding_test.go` (if present)
- Modify: `internal/cli/tmux.go` (doc comment: `tmuxLabelOnboarding` now used by `--onboard`)
- Test: `internal/cli/manager_test.go` (add `--onboard` dry-run golden tests, env assertion)
- Create golden: `internal/cli/testdata/golden/manager-onboard-dry-run-opencode.json`, `...-ollama.json`

**Interfaces:**
- Consumes: `manager.Launcher.BuildArgvOnboard`, `managerEnvValues`, `assembleEnv`, `emitLaunchHeader`/`emitLaunchTail`, `runChild`, `setTmuxWindowLabel`, `tmuxLabelOnboarding`.
- Produces: `managerOpts.Onboard bool`; `runManager` selects `BuildArgvOnboard` + adds `ATM_ONBOARD=1` when `opts.Onboard`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/cli/manager_test.go`:

```go
func TestManagerOnboardOpencodeDryRunJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("manager", "opencode", "--project", "FOO", "--onboard", "--dry-run")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeManagerOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "manager-onboard-dry-run-opencode", got)
}

func TestManagerOnboardOllamaDryRunJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("manager", "ollama", "--project", "FOO", "--integration", "opencode", "--onboard", "--dry-run")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeManagerOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "manager-onboard-dry-run-ollama", got)
}

func TestManagerOnboardEnvHasATMOnboard(t *testing.T) {
	got := managerEnvValues("FOO", "/bin/atm", "opencode-manager", "FOO-RUNID", "/tmp/ctx.md", true)
	joined := strings.Join(gotToSlice(got), "\n")
	if !strings.Contains(joined, "ATM_ONBOARD=1") {
		t.Errorf("onboard env missing ATM_ONBOARD=1; got:\n%s", joined)
	}
}

func TestManagerOnboardArgvUsesAutoPrompt(t *testing.T) {
	l, ok := manager.LauncherFor("opencode")
	if !ok {
		t.Fatal("LauncherFor(opencode) not found")
	}
	argv := l.BuildArgvOnboard("/tmp/ctx.md")
	if argv[1] != "--auto" || argv[2] != "--prompt" {
		t.Fatalf("onboard argv = %v, want --auto --prompt <msg>", argv)
	}
}

func TestOnboardingCommandRemoved(t *testing.T) {
	h := newGoldenHarness(t)
	_, _, code := h.run("onboarding", "opencode", "--project", "FOO", "--dry-run")
	if code == ExitSuccess {
		t.Fatalf("onboarding command should be removed; got exit 0")
	}
}
```

Add `"atm/internal/manager"` to the imports of `manager_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestManagerOnboard|TestOnboardingCommandRemoved' -v`
Expected: FAIL — `--onboard` flag undefined; `managerEnvValues` arity mismatch; `atm onboarding` still registered.

- [ ] **Step 3: Modify `runManager` and add the `--onboard` flag**

In `internal/cli/manager.go`:

a) Add `Onboard bool` to `managerOpts`:
```go
type managerOpts struct {
	Project     string
	Actor       string
	Integration string
	Onboard     bool
	DryRun      bool
	ExtraArgs   []string
}
```

b) Add the flag registration to `newManagerAgentCmd` (after the `--dry-run` flag line, before the `return cmd`):
```go
	cmd.Flags().BoolVar(&opts.Onboard, "onboard", false, "non-interactive onboarding run against cwd (activates ATM_ONBOARD)")
```
And identically in `newManagerOllamaCmd`.

c) Change the `runManager` signature to thread `opts.Onboard` into `managerEnvValues` (or read `opts` directly since it is already a parameter). Modify the call to `managerEnvValues` to pass `opts.Onboard`:

Replace:
```go
	envValues := managerEnvValues(opts.Project, atmBin, opts.Actor, runID, contextPath)
```
with:
```go
	envValues := managerEnvValues(opts.Project, atmBin, opts.Actor, runID, contextPath, opts.Onboard)
```

d) Select the onboard argv when `--onboard` is set. Replace:
```go
	base := l.BuildArgv()
	envArgs := agentEnvArgs(agent, integration)
	argv := appendAgentArgs(base, envArgs, opts.ExtraArgs)
	env := assembleEnv(envValues)
	if err := emitLaunchHeader(st, "manager", opts.Project, runID, contextPath, l.Name(), argv, envValues); err != nil {
		return err
	}
	if opts.DryRun {
		return nil
	}

	exitCode, runErr := runChild(l.Name(), argv, env, l.NotFoundHint())
```
with:
```go
	var base []string
	if opts.Onboard {
		base = l.BuildArgvOnboard(contextPath)
	} else {
		base = l.BuildArgv()
	}
	envArgs := agentEnvArgs(agent, integration)
	argv := appendAgentArgs(base, envArgs, opts.ExtraArgs)
	env := assembleEnv(envValues)
	if opts.Onboard {
		setTmuxWindowLabel(os.Stdout, tmuxLabelOnboarding)
	}
	if err := emitLaunchHeader(st, "manager", opts.Project, runID, contextPath, l.Name(), argv, envValues); err != nil {
		return err
	}
	if opts.DryRun {
		return nil
	}

	exitCode, runErr := runChild(l.Name(), argv, env, l.NotFoundHint())
```

e) Update `managerEnvValues` to accept and conditionally set `ATM_ONBOARD`:
```go
func managerEnvValues(project, atmBin, actor, runID, contextPath string, onboard bool) map[string]string {
	m := map[string]string{
		"ATM_ROLE":         "manager",
		"ATM_PROJECT":      project,
		"ATM_BIN":          atmBin,
		"ATM_ACTOR":        actor,
		"ATM_RUN_ID":       runID,
		"ATM_CONTEXT_FILE": contextPath,
	}
	if onboard {
		m["ATM_ONBOARD"] = "1"
	}
	return m
}
```

f) Fix the existing call site `TestManagerEnvIncludesATMValues` in `manager_test.go` — update the call to pass `false`:
```go
	got := managerEnvValues("FOO", "/bin/atm", "codex-manager", "FOO-RUNID", "/tmp/context.md", false)
```

- [ ] **Step 4: Delete onboarding**

```bash
git rm internal/cli/onboarding.go
git rm -r internal/onboard
```

Check for and delete `internal/cli/onboarding_test.go` if it exists:
```bash
ls internal/cli/onboarding_test.go 2>/dev/null && git rm internal/cli/onboarding_test.go || true
```

- [ ] **Step 5: Remove `newOnboardingCmd` registration**

In `internal/cli/root.go`, delete the line:
```go
	root.AddCommand(newOnboardingCmd(st))
```

- [ ] **Step 6: Regenerate goldens and run tests**

Run: `go test ./internal/cli/ -run 'TestManagerOnboard|TestOnboardingCommandRemoved|TestManagerEnvIncludesATMValues|TestManagerClaudeExtraArgsDryRunJSON|TestManagerOllamaDryRunJSON|TestManagerCodexDryRunJSON' -update`
Then: `go test ./internal/cli/ -run 'TestManagerOnboard|TestOnboardingCommandRemoved|TestManagerEnvIncludesATMValues|TestManager' -v`
Expected: PASS.

Inspect the generated `internal/cli/testdata/golden/manager-onboard-dry-run-opencode.json` — confirm `argv` starts with `["opencode","--auto","--prompt",...]` and `env` contains `"ATM_ONBOARD": "1"`.

- [ ] **Step 7: Run full cli package tests + commit**

Run: `go test ./internal/cli/ -v` (ensure no regressions).
Then:
```bash
git add internal/cli/manager.go internal/cli/manager_test.go internal/cli/root.go internal/cli/tmux.go internal/cli/testdata/golden/manager-onboard-dry-run-opencode.json internal/cli/testdata/golden/manager-onboard-dry-run-ollama.json
git rm -r internal/onboard internal/cli/onboarding.go
git commit -m "Unify onboarding into atm manager --onboard; delete atm onboarding (ATM-0028)"
```

---

## Task 6: TUI Ubiquitous Language chart

**Files:**
- Modify: `internal/tui/projects.go` (rename chart, load vocab, render weighted bubbles, delete `renderSampleBubbleCanvas`)
- Test: `internal/tui/app_test.go` (update `TestSelectedProjectSummaryRendersCharts`, `TestKeywordSummaryDoesNotOpenFormOrConfirm`, delete `TestRenderSampleBubbleCanvasShowsPlaceholders`)
- Test: `internal/tui/projects_test.go` (add `TestRenderUbiquitousLanguageChart`)

**Interfaces:**
- Consumes: `store.GetVocabulary(code)`, `canvas.New`, `canvas.SetStringWithStyle`, `lipgloss.Color`, `chartBoxInnerWidth`, `renderChartBox`, `dashboardLine`.
- Produces:
  - `func (p *projectsModel) renderUbiquitousLanguageChart(vocab *store.Vocabulary, maxLines int) string`
  - `func renderUbiquitousLanguageCanvas(width int, height int, terms []store.VocabularyTerm) string`
  - `projectSummaryData` returns `(*store.Project, []*store.Task, []store.LogEntry, *store.Vocabulary, bool)`

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/projects_test.go`:

```go
func TestRenderUbiquitousLanguageChartEmptyState(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "bug one")
	m.SetSize(80, 24)
	m.projectScope = "ATM"
	m.refreshAll()
	got := m.projects.renderSummary(12)
	mustContain(t, got, "Ubiquitous Language")
	mustContain(t, got, "no vocabulary yet")
	mustNotContain(t, got, "events")
	mustNotContain(t, got, "(agents)")
}

func TestRenderUbiquitousLanguageChartShowsTerms(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "bug one")
	if err := m.store.WriteVocabulary("ATM", &store.Vocabulary{
		Actor: "opencode-manager",
		Terms: []store.VocabularyTerm{
			{Term: "labels", Weight: 9},
			{Term: "audit log", Weight: 7},
			{Term: "persona", Weight: 5},
		},
	}); err != nil {
		t.Fatal(err)
	}
	m.SetSize(80, 24)
	m.projectScope = "ATM"
	m.refreshAll()
	got := m.projects.renderSummary(14)
	mustContain(t, got, "Ubiquitous Language")
	mustContain(t, got, "labels")
	mustContain(t, got, "audit log")
	mustContain(t, got, "persona")
	mustNotContain(t, got, "no vocabulary yet")
}
```

Add `"atm/internal/store"` to imports of `projects_test.go` if not already present.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestRenderUbiquitousLanguage -v`
Expected: FAIL — `renderUbiquitousLanguageChart` undefined; chart still titled "bubbles".

- [ ] **Step 3: Modify `projectSummaryData` to load vocabulary**

In `internal/tui/projects.go`, change the signature of `projectSummaryData` to return the vocabulary:

Replace (around line 787):
```go
func (p *projectsModel) projectSummaryData() (*store.Project, []*store.Task, []store.LogEntry, bool) {
	code := p.m.projectScope
	if code == "" {
		return nil, nil, nil, false
	}
	project, err := p.m.store.GetProject(code)
	if err != nil {
		return nil, nil, nil, false
	}
	tasks := p.m.store.ListTasks(store.QueryFilters{Project: code})
	entries, err := p.m.store.ReadLog(code)
	if err != nil && !store.IsIntegrity(err) {
		return nil, nil, nil, false
	}
	return project, tasks, entries, true
}
```
with:
```go
func (p *projectsModel) projectSummaryData() (*store.Project, []*store.Task, []store.LogEntry, *store.Vocabulary, bool) {
	code := p.m.projectScope
	if code == "" {
		return nil, nil, nil, nil, false
	}
	project, err := p.m.store.GetProject(code)
	if err != nil {
		return nil, nil, nil, nil, false
	}
	tasks := p.m.store.ListTasks(store.QueryFilters{Project: code})
	entries, err := p.m.store.ReadLog(code)
	if err != nil && !store.IsIntegrity(err) {
		return nil, nil, nil, nil, false
	}
	vocab, _ := p.m.store.GetVocabulary(code)
	return project, tasks, entries, vocab, true
}
```

- [ ] **Step 4: Update all `projectSummaryData` call sites**

There is one call site at `internal/tui/projects.go:402`. Replace:
```go
	project, tasks, entries, ok := p.projectSummaryData()
```
with:
```go
	project, tasks, entries, vocab, ok := p.projectSummaryData()
```

Then update the `remaining >= 9` block (around line 427) to pass `vocab`:
```go
	if remaining >= 9 {
		actorH, stripeH, bubblesH := chartBoxHeights(remaining)
		lines = append(lines, p.renderPersonaActivityChart(entries, actorH)...)
		lines = append(lines, strings.Split(p.renderChartBox("activity stripe", p.renderActivityStripeChart(entries, stripeH-2), stripeH), "\n")...)
		lines = append(lines, strings.Split(p.renderUbiquitousLanguageChart(vocab, bubblesH), "\n")...)
		return padToHeight(strings.Join(lines, "\n"), height)
	}
```

- [ ] **Step 5: Replace `renderBubbleChart` and delete `renderSampleBubbleCanvas`**

Delete the entire `renderSampleBubbleCanvas` function (around lines 648-684) and the `renderBubbleChart` function (around lines 686-691). Replace with:

```go
func renderUbiquitousLanguageCanvas(width int, height int, terms []store.VocabularyTerm) string {
	if width < 18 {
		width = 18
	}
	if height < 3 {
		height = 3
	}
	c := canvas.New(width, height)
	colors := []lipgloss.Color{"39", "214", "82", "171", "203", "117"}
	for i, term := range terms {
		if i >= 12 {
			break
		}
		col := colors[i%len(colors)]
		style := lipgloss.NewStyle().Foreground(col).Bold(term.Weight >= 7)
		x := (i * 13) % width
		y := i % height
		c.SetStringWithStyle(canvas.Point{X: x, Y: y}, term.Term, style)
	}
	return c.View()
}

func (p *projectsModel) renderUbiquitousLanguageChart(vocab *store.Vocabulary, maxLines int) string {
	if maxLines < 3 {
		return dashboardLine(p.width, "Ubiquitous Language")
	}
	innerW := chartBoxInnerWidth(p.width)
	innerH := maxLines - 2
	var body string
	if vocab == nil || len(vocab.Terms) == 0 {
		body = p.m.styles.Muted.Render("no vocabulary yet")
	} else {
		body = renderUbiquitousLanguageCanvas(innerW, innerH, vocab.Terms)
	}
	return p.renderChartBox("Ubiquitous Language", body, maxLines)
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestRenderUbiquitousLanguage -v`
Expected: PASS for both tests.

- [ ] **Step 7: Update the existing app tests that referenced "bubbles"**

In `internal/tui/app_test.go`:

a) `TestSelectedProjectSummaryRendersCharts` (around line 800): replace the assertions:
```go
	mustContain(t, body, "bubbles")
	mustContain(t, body, "events")
	mustContain(t, body, "agents")
```
with:
```go
	mustContain(t, body, "Ubiquitous Language")
	mustContain(t, body, "no vocabulary yet")
```

b) `TestKeywordSummaryDoesNotOpenFormOrConfirm` (around line 878): replace:
```go
	mustContain(t, body, "bubbles")
	mustContain(t, body, "events")
	mustContain(t, body, "agents")
```
with:
```go
	mustContain(t, body, "Ubiquitous Language")
```

c) Delete `TestRenderSampleBubbleCanvasShowsPlaceholders` (around line 1092) entirely.

- [ ] **Step 8: Run full TUI tests + commit**

Run: `go test ./internal/tui/ -v` (ensure no regressions).
Then:
```bash
git add internal/tui/projects.go internal/tui/projects_test.go internal/tui/app_test.go
git commit -m "Replace bubbles placeholder with Ubiquitous Language chart (ATM-0028)"
```

---

## Task 7: Docs, smoke script, and spec cross-reference

**Files:**
- Modify: `README.md` (replace Onboarding section, add Vocabulary subsection)
- Modify: `scripts/onboard-smoke.sh` (use `atm manager ... --onboard`)
- Modify: `docs/superpowers/specs/2026-07-04-tui-project-summary-charts-design.md` (Chart 3 no longer a placeholder)

- [ ] **Step 1: Update README.md**

Replace the "## Onboarding" section (lines ~342-381) with:

```markdown
## Manager onboarding

`atm manager <host> --project <CODE> --onboard` launches a non-interactive
agent that explores the current working directory and seeds the existing ATM
project with context tasks AND computes the project vocabulary (ubiquitous
language) in the same pass. The manager is the knowledge-base owner: it
maintains the ledger, the context map, and the vocabulary.

Prerequisite: the project must already exist.

```
atm project create --code FOO --name "Foo"
cd /path/to/repo-to-onboard
atm manager opencode --project FOO --onboard
```

For an ollama-backed agent:

```
atm manager ollama --project FOO --integration opencode --onboard
```

Without `--onboard`, `atm manager <host> --project <CODE>` opens an
interactive human session to consult or steer the manager (ledger hygiene,
vocabulary recompute, splits/merges, staleness review).

Flags (onboard mode):

- `--project <CODE>` (required) — the existing ATM project.
- `--onboard` — non-interactive onboarding run against cwd.
- `--actor <id>` (default `<host>-manager`) — stamped into history.
- `--dry-run` — render the context and print the launcher argv/env without launching.
- `--integration <name>` (ollama only, required) — passed through to `ollama launch`.
- `-- <agent args...>` — everything after `--` is appended verbatim to the host agent's argv.

## Vocabulary (ubiquitous language)

`atm vocabulary` reads and writes a project's ubiquitous language — the
recurring domain terms mined from task titles, descriptions, and comments
by the manager.

```
atm vocabulary show --project FOO
atm vocabulary write --project FOO --actor <ACTOR> --terms '[{"term":"labels","weight":9}]'
```

The TUI's third Projects-pane chart ("Ubiquitous Language") reads
`vocabulary.json` and renders the terms as weighted bubbles. When no
vocabulary has been computed yet, it shows a quiet empty state. Recompute
is explicit: during onboarding, in an interactive manager session, or via
a developing-agent track call with a `vocabulary` hint.
```

- [ ] **Step 2: Update the smoke script**

In `scripts/onboard-smoke.sh`, replace every `atm ... onboarding opencode --project FOO ...` invocation with `atm --store "$store_dir" manager opencode --project FOO --onboard ...`, and the ollama invocation with `atm --store "$store_dir" manager ollama --project FOO --integration opencode --onboard ...`. Remove the `--prompt-version vNoSuch` case (no prompt-version selector on the manager). The prompt-file path changes from `onboarding/*.md` to `manager/*.md`; update the `prompt_file` lookup line accordingly:

```bash
prompt_file="$(ls "$store_dir"/manager/*.md | head -1)"
```

- [ ] **Step 3: Update the charts spec cross-reference**

In `docs/superpowers/specs/2026-07-04-tui-project-summary-charts-design.md`, update the "Chart 3: Sample Bubbles Placeholder" section (lines 154-176). Replace the heading and prose to state that Chart 3 is now "Ubiquitous Language", reads `vocabulary.json` written by the manager, and renders weighted bubbles; the placeholder is no longer a placeholder. Add a pointer to the manager-knowledge-base design doc.

- [ ] **Step 4: Run verify + commit**

Run: `make verify`
Expected: PASS (build + all tests).

Then:
```bash
git add README.md scripts/onboard-smoke.sh docs/superpowers/specs/2026-07-04-tui-project-summary-charts-design.md
git commit -m "Update docs/smoke for manager onboarding + Ubiquitous Language (ATM-0028)"
```

---

## Final verification

- [ ] Run `make verify` once more across the whole tree. Expected: PASS.
- [ ] Confirm no `internal/onboard/` directory remains: `ls internal/onboard 2>/dev/null && echo REMAINS || echo gone`.
- [ ] Confirm no `atm onboarding` command: `./bin/atm onboarding 2>&1 | grep -q "unknown command"` (expected: match).
- [ ] Confirm `atm manager opencode --project ATM --onboard --dry-run` prints argv with `--auto --prompt` and env with `ATM_ONBOARD=1`.