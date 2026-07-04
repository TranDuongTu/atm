# Onboarding v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `atm onboarding opencode` and `atm onboarding ollama --integration <name>`, which render a versioned embedded prompt, write it to `$ATM_HOME/onboarding/<run-id>.md`, and exec the user's agent binary non-interactively to seed an existing ATM project with context tasks.

**Architecture:** ATM is a prompt-renderer + process parent. A library package `internal/onboard` exposes `Render(version, Data)` and a `launcher` interface with two impls (`opencodeLauncher`, `ollamaLauncher`). The CLI layer `internal/cli/onboarding.go` orchestrates: validate project -> snapshot existing tasks -> render prompt -> write prompt file -> print header -> `cmd.Run()` the launcher as a child (inheriting stdio) -> print tail summary. One seed label (`context:question`) is added and existing `context:*` descriptions are refined to be pointer-oriented so the agent self-selects labels from descriptions alone.

**Tech Stack:** Go 1.22+, cobra (existing CLI), `//go:embed` for the prompt asset, `strings.NewReplacer` for substitution, `os/exec` for the child process, `os.Executable` for the ATM binary path, existing `internal/store` and `internal/seed`, `make verify` as the gate.

## Global Constraints

- Follow `docs/superpowers/specs/2026-07-04-onboarding-v1-design.md` exactly.
- Project must pre-exist before onboarding; onboarding never creates a project.
- Agent runs in the cwd under its own permission model; ATM passes no repo path.
- The system prompt is embedded in the binary via `//go:embed`; no `--prompt-file` override.
- The prompt is label-agnostic: it never enumerates label names; agents learn labels from `atm label list` descriptions.
- Idempotency is a prompt instruction backed by the rendered `<EXISTING_TASKS>` snapshot; the agent dedupes by title/topical overlap.
- ATM stays the parent (`cmd.Run()`); it does NOT `exec`-replace; the child inherits stdin/stdout/stderr so the user watches the agent live.
- No second API surface: the agent shells out to the public `atm` CLI only; no MCP/tool-server.
- `ollama --integration <name>` is pass-through: ATM does not validate the integration name.
- No emojis in code or commits.
- Follow existing style in neighboring files (cobra command shape, `cliState` helpers, `emit`, `writeJSON`, golden test pattern).
- Run `make verify` before declaring implementation complete.

---

## File Structure

- Modify `internal/seed/seed.go`: add `context:question`; refine the four `context:*` descriptions to be pointer-oriented.
- Modify `internal/seed/seed_test.go`: bump `TestLabelsCountIs17` to `TestLabelsCountIs18`; keep the non-empty-description test passing against the refined wording.
- Create `internal/onboard/prompt_opencode_v1.md`: the v1 prompt body with `<CODE>`, `<PROJECT_NAME>`, `<ATM_BIN>`, `<ACTOR>`, `<RUN_ID>`, `<TIMESTAMP>`, `<EXISTING_TASKS>` placeholders.
- Create `internal/onboard/embed.go`: `//go:embed` directive, `Latest` const, `Data` struct, `Render(version string, data Data) (string, error)`.
- Create `internal/onboard/embed_test.go`: golden-file render test + unknown-version error test.
- Create `internal/onboard/launcher.go`: `launcher` interface, `opencodeLauncher{}`, `ollamaLauncher{integration string}`, and `buildArgv` impls.
- Create `internal/onboard/launcher_test.go`: argv-shape tests for both launchers + `notFoundHint`.
- Create `internal/cli/onboarding.go`: `newOnboardingCmd`, `newOnboardingOpencodeCmd`, `newOnboardingOllamaCmd`, shared `runOnboarding(st, l launcher, opts onboardingOpts) error`.
- Modify `internal/cli/root.go`: register `newOnboardingCmd(st)` on the root.
- Create `internal/cli/onboarding_test.go`: golden tests for header/tail/dry-run/error cases (no live exec).
- Create `internal/cli/testdata/golden/onboarding-dry-run-opencode.json`, `onboarding-dry-run-ollama.json`, `onboarding-missing-project.json`, `onboarding-unknown-prompt-version.json`: golden fixtures.
- Create `scripts/onboard-smoke.sh`: manual smoke script that runs `--dry-run` against a fixture project and asserts the prompt file + printed argv look right.

---

### Task 1: Seed label changes

**Files:**
- Modify: `internal/seed/seed.go`
- Modify: `internal/seed/seed_test.go`

**Interfaces:**
- Consumes: none.
- Produces: `seed.Labels` now has 18 entries including `context:question`; the four `context:*` descriptions are pointer-oriented. No signature changes — downstream code reads `seed.Labels` by range.

- [ ] **Step 1: Write the failing test**

In `internal/seed/seed_test.go`, rename `TestLabelsCountIs17` to `TestLabelsCountIs18` and update the assertion:

```go
func TestLabelsCountIs18(t *testing.T) {
	if len(Labels) != 18 {
		t.Fatalf("seed.Labels has %d entries, want 18", len(Labels))
	}
}
```

Also add a test asserting `context:question` is present with a non-empty description:

```go
func TestContextQuestionLabelPresent(t *testing.T) {
	for _, l := range Labels {
		if l.Suffix == "context:question" {
			if l.Description == "" {
				t.Errorf("context:question has empty description")
			}
			return
		}
	}
	t.Errorf("context:question label not found in seed.Labels")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/seed/ -run TestLabelsCountIs18 -v`
Expected: FAIL with "seed.Labels has 17 entries, want 18".

Run: `go test ./internal/seed/ -run TestContextQuestionLabelPresent -v`
Expected: FAIL with "context:question label not found in seed.Labels".

- [ ] **Step 3: Modify seed.go**

In `internal/seed/seed.go`, replace the four `context:*` entries and add `context:question`. The final `context:` block (replacing the existing four lines) is:

```go
	{"context:documentation", "the task's description points at a specific document (file path or URL) and summarizes what it covers, so a later agent can decide whether to read it"},
	{"context:repository", "the task's description names a code repository (path or URL), what it contains, and how to work in it; a later agent reads this to orient"},
	{"context:agent", "the task's description captures agent-direction notes for this project: build/test/lint commands, conventions, and gotchas a working agent must know"},
	{"context:fixit", "the task's description flags something that should be reviewed, updated, or altered; not a work item itself, a signal to a human or later agent"},
	{"context:question", "the task's description poses an open question or ambiguity about the project that a human or later agent should clarify; not a defect, not a work item, a gap in understanding"},
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/seed/ -v`
Expected: PASS (all tests including `TestLabelsCountIs18` and `TestContextQuestionLabelPresent`).

- [ ] **Step 5: Verify the full build is still green**

Run: `make verify`
Expected: build + all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/seed/seed.go internal/seed/seed_test.go
git commit -m "seed: add context:question, refine context label descriptions"
```

---

### Task 2: Onboard package — embedded prompt + Render

**Files:**
- Create: `internal/onboard/prompt_opencode_v1.md`
- Create: `internal/onboard/embed.go`
- Create: `internal/onboard/embed_test.go`

**Interfaces:**
- Consumes: none.
- Produces:
  - `package onboard` (library-only, no cobra, no store, no stdout).
  - `var Latest = "v1"` (a string matching the filename suffix).
  - `type Data struct { Code, Name, ATMBin, Actor, RunID, Timestamp, ExistingTasks string }`.
  - `func Render(version string, data Data) (string, error)` — substitutes all placeholders via `strings.NewReplacer`; unknown version -> `fmt.Errorf("%w: unknown prompt version %q", errUnknownVersion, version)`.
  - `var errUnknownVersion = errors.New("unknown prompt version")`.

- [ ] **Step 1: Create the prompt asset**

Create `internal/onboard/prompt_opencode_v1.md` with this exact content (the placeholders are angle-bracketed tokens that `strings.NewReplacer` will substitute; do not add any other angle-bracket tokens to the body):

```markdown
# ATM onboarding run <RUN_ID>

Project: `<CODE>` (`<PROJECT_NAME>`)
Started: `<TIMESTAMP>`
ATM binary: `<ATM_BIN>`
Actor: `<ACTOR>`

## 1. Orient as an ATM agent first

Before exploring the repo, run `<ATM_BIN> conventions` and read it. It tells you how ATM projects are organized (the label substrate, the first-contact sequence a later agent will follow, the advisory seed namespaces). You are building exactly the context that `conventions` describes a fresh agent consuming. Understanding it makes your output useful to the next agent.

## 2. Research already-captured knowledge

Run `<ATM_BIN> task list --project <CODE> --output json` and read the existing tasks' titles, labels, and descriptions. This project may already have context from other repositories (frontend, backend, infra, they may disagree). This is your reconciliation baseline: what is already known, what is already pointer-mapped, where the gaps are. Note any disagreements between existing tasks; you will either update them or flag them as `context:question` rather than silently picking a side.

Existing tasks at onboarding start (t0 snapshot, for audit):

<EXISTING_TASKS>

## 3. Role and goal

You are an onboarding agent for ATM project `<CODE>` (`<PROJECT_NAME>`). Your goal is to build a navigable context map: a later agent landing in this project should be able to query ATM, find pointers to all relevant context, and narrow to specific resources by reading task descriptions. You explore the repository in your current working directory and translate what you find into ATM tasks. You operate non-interactively; do not ask the human questions, make reasonable judgment calls and proceed.

## 4. Tools

Your only persistent side-effect is the `atm` CLI at `<ATM_BIN>`. All mutations go through it. You may freely read files in the cwd. Do not edit repo files, do not `git commit`, do not run long-running services. Stamp every mutating command with `--actor <ACTOR>`.

## 5. Vocabulary

Run `<ATM_BIN> label list --project <CODE> --output json` to learn the available labels and their descriptions. Each label's description is its contract, match findings to labels by reading those descriptions. Use the seeded labels; the label substrate is open but onboarding's contract in v1 is to populate the seeded namespaces, not invent new ones. If a finding genuinely fits no seeded label, describe it in the task description and leave it unlabeled rather than inventing a namespace.

## 6. Idempotency and multi-repo reconciliation

Before each `atm task create`: match against the existing tasks you researched in step 2 AND what you have created this run, by title and topical overlap; if a match exists, update via `atm task set-description` and add any missing labels via `atm task label add` rather than duplicating; if repos disagree about the same thing, prefer a task whose description names the disagreement (or a `context:question` task) over silently picking a side. Re-running onboarding extends and reconciles; it does not duplicate.

## 7. What to capture

Capture these as ATM tasks, label-agnostically. Pick labels by matching each finding to a label description from step 5; do not invent label names.

- Agent harness setup: how an agent should work here. Build/test/lint commands, conventions, gotchas. A later agent reads this to know how to operate.
- Structure: where things live. Top-level layout, source/docs/tests/configs. A later agent reads this to navigate without re-exploring.
- Document and code pointers: notable docs and code locations, each with a path and a one-line statement of what it covers. A later agent searches by label, finds the pointer, reads the description to decide whether to open it.
- Findings: TODO/FIXME clusters, stale docs, broken examples, obvious gaps. A later agent picks these up as work.
- Open questions: anything you could not resolve from the code. An ambiguity, a "why is it this way", a contradiction between repos. Capture each as its own task so a human or later agent can clarify. Do not bury questions inside other tasks' descriptions.
- Do not duplicate: if an existing task (from another repo's onboarding) already covers a finding, update it; do not restate. The context map should converge, not pile up.

## 8. Working method

Breadth-first, budget-bounded. List top-level files/dirs; read README; read docs/ if present; sample representative source files. Do not read every file. Stop when you have covered the obvious surface and produced the context map. Cap work-task creation at ~20 per run; further findings go into a single aggregate task whose description lists them.

## 9. Summary

Before finishing, run `<ATM_BIN> task list --project <CODE> --output json` and print a one-paragraph natural-language summary of what you created/updated, any reconciliations you made against existing tasks, and any judgments worth flagging. This is the human's onboarding receipt.
```

- [ ] **Step 2: Write the failing test**

Create `internal/onboard/embed_test.go`:

```go
package onboard

import (
	"strings"
	"testing"
)

func TestRenderLatestSubstitutesAllPlaceholders(t *testing.T) {
	data := Data{
		Code:          "FOO",
		Name:          "Foo Project",
		ATMBin:        "/usr/local/bin/atm",
		Actor:         "opencode-onboard",
		RunID:         "FOO-20260704121530-a1b2c3",
		Timestamp:     "2026-07-04T12:15:30Z",
		ExistingTasks: "| FOO-0001 | existing task | status:open |",
	}
	got, err := Render(Latest, data)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, placeholder := range []string{
		"<CODE>", "<PROJECT_NAME>", "<ATM_BIN>", "<ACTOR>",
		"<RUN_ID>", "<TIMESTAMP>", "<EXISTING_TASKS>",
	} {
		if strings.Contains(got, placeholder) {
			t.Errorf("rendered output still contains placeholder %q", placeholder)
		}
	}
	for _, want := range []string{
		"FOO", "Foo Project", "/usr/local/bin/atm", "opencode-onboard",
		"FOO-20260704121530-a1b2c3", "2026-07-04T12:15:30Z",
		"| FOO-0001 | existing task | status:open |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered output missing %q", want)
		}
	}
}

func TestRenderUnknownVersionErrors(t *testing.T) {
	if _, err := Render("vNonexistent", Data{}); err == nil {
		t.Fatal("expected error for unknown version, got nil")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/onboard/ -run TestRender -v`
Expected: FAIL (build error: package onboard has no `Render`, `Data`, or `Latest`).

- [ ] **Step 4: Write embed.go**

Create `internal/onboard/embed.go`:

```go
package onboard

import (
	_ "embed"
	"errors"
	"fmt"
	"strings"
)

//go:embed prompt_opencode_v1.md
var promptOpencodeV1 string

// Latest is the prompt version used when --prompt-version is not specified.
// A new prompt version = a new prompt_opencode_v<N>.md file + a bump here.
const Latest = "v1"

// errUnknownVersion is returned by Render when the requested version does not
// match any embedded prompt asset.
var errUnknownVersion = errors.New("unknown prompt version")

// Data carries the values substituted into the prompt template at render time.
type Data struct {
	Code          string
	Name          string
	ATMBin        string
	Actor         string
	RunID         string
	Timestamp     string
	ExistingTasks string // pre-rendered markdown table (or "(none)" if empty)
}

// Render substitutes the placeholders in the prompt template for the requested
// version and returns the rendered markdown. Unknown versions return
// errUnknownVersion (wrapped with the requested version for the CLI to map to
// exit 2).
func Render(version string, data Data) (string, error) {
	var tmpl string
	switch version {
	case "v1":
		tmpl = promptOpencodeV1
	default:
		return "", fmt.Errorf("%w: %q", errUnknownVersion, version)
	}
	if data.ExistingTasks == "" {
		data.ExistingTasks = "(none)"
	}
	replacer := strings.NewReplacer(
		"<CODE>", data.Code,
		"<PROJECT_NAME>", data.Name,
		"<ATM_BIN>", data.ATMBin,
		"<ACTOR>", data.Actor,
		"<RUN_ID>", data.RunID,
		"<TIMESTAMP>", data.Timestamp,
		"<EXISTING_TASKS>", data.ExistingTasks,
	)
	return replacer.Replace(tmpl), nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/onboard/ -v`
Expected: PASS (both tests).

- [ ] **Step 6: Commit**

```bash
git add internal/onboard/prompt_opencode_v1.md internal/onboard/embed.go internal/onboard/embed_test.go
git commit -m "onboard: add embedded v1 prompt and Render"
```

---

### Task 3: Onboard package — launcher interface and impls

**Files:**
- Create: `internal/onboard/launcher.go`
- Create: `internal/onboard/launcher_test.go`

**Interfaces:**
- Consumes: none (pure data).
- Produces:
  - `type Launcher interface { Name() string; NotFoundHint() string; BuildArgv(promptPath, title string) []string }`.
  - `type OpencodeLauncher struct{}`.
  - `type OllamaLauncher struct{ Integration string }`.
  - `func (OpencodeLauncher) Name() string` -> "opencode".
  - `func (OpencodeLauncher) NotFoundHint() string` -> "https://opencode.ai".
  - `func (OpencodeLauncher) BuildArgv(promptPath, title string) []string` -> `["opencode", "run", "-f", promptPath, "--auto", "--title", title]`.
  - `func (OllamaLauncher) Name() string` -> "ollama".
  - `func (OllamaLauncher) NotFoundHint() string` -> "https://ollama.com".
  - `func (l OllamaLauncher) BuildArgv(promptPath, title string) []string` -> `["ollama", "launch", l.Integration, "--", "run", "-f", promptPath, "--auto", "--title", title]`.

- [ ] **Step 1: Write the failing test**

Create `internal/onboard/launcher_test.go`:

```go
package onboard

import (
	"reflect"
	"testing"
)

func TestOpencodeLauncherBuildArgv(t *testing.T) {
	l := OpencodeLauncher{}
	got := l.BuildArgv("/tmp/p.md", "ATM onboarding: FOO (FOO-x)")
	want := []string{"opencode", "run", "-f", "/tmp/p.md", "--auto", "--title", "ATM onboarding: FOO (FOO-x)"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildArgv = %v, want %v", got, want)
	}
}

func TestOpencodeLauncherNameAndHint(t *testing.T) {
	l := OpencodeLauncher{}
	if l.Name() != "opencode" {
		t.Errorf("Name = %q, want opencode", l.Name())
	}
	if l.NotFoundHint() != "https://opencode.ai" {
		t.Errorf("NotFoundHint = %q, want https://opencode.ai", l.NotFoundHint())
	}
}

func TestOllamaLauncherBuildArgv(t *testing.T) {
	l := OllamaLauncher{Integration: "opencode"}
	got := l.BuildArgv("/tmp/p.md", "ATM onboarding: FOO (FOO-x)")
	want := []string{"ollama", "launch", "opencode", "--", "run", "-f", "/tmp/p.md", "--auto", "--title", "ATM onboarding: FOO (FOO-x)"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BuildArgv = %v, want %v", got, want)
	}
}

func TestOllamaLauncherNameAndHint(t *testing.T) {
	l := OllamaLauncher{Integration: "codex"}
	if l.Name() != "ollama" {
		t.Errorf("Name = %q, want ollama", l.Name())
	}
	if l.NotFoundHint() != "https://ollama.com" {
		t.Errorf("NotFoundHint = %q, want https://ollama.com", l.NotFoundHint())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/onboard/ -run TestOpencodeLauncher -v`
Expected: FAIL (build error: undefined `OpencodeLauncher`, `OllamaLauncher`).

- [ ] **Step 3: Write launcher.go**

Create `internal/onboard/launcher.go`:

```go
package onboard

// Launcher builds the exec argv for a non-interactive agent run and provides
// the not-found install hint. Implementations are pure data; the CLI layer
// owns the actual exec.
type Launcher interface {
	// Name is the human-readable launcher name used in headers, tail
	// summaries, and actor defaults (e.g. "opencode", "ollama").
	Name() string
	// NotFoundHint is the install URL printed when the launcher binary is
	// not on PATH.
	NotFoundHint() string
	// BuildArgv returns the full exec argv for the launcher, given the
	// rendered prompt file path and the session title.
	BuildArgv(promptPath, title string) []string
}

// OpencodeLauncher execs `opencode run -f <prompt> --auto --title <title>`.
type OpencodeLauncher struct{}

func (OpencodeLauncher) Name() string         { return "opencode" }
func (OpencodeLauncher) NotFoundHint() string { return "https://opencode.ai" }
func (OpencodeLauncher) BuildArgv(promptPath, title string) []string {
	return []string{"opencode", "run", "-f", promptPath, "--auto", "--title", title}
}

// OllamaLauncher execs `ollama launch <integration> -- run -f <prompt> --auto
// --title <title>`. The `--` separator is ollama launch's documented
// passthrough; ATM does not validate the integration name.
type OllamaLauncher struct {
	Integration string
}

func (OllamaLauncher) Name() string         { return "ollama" }
func (OllamaLauncher) NotFoundHint() string { return "https://ollama.com" }
func (l OllamaLauncher) BuildArgv(promptPath, title string) []string {
	return []string{"ollama", "launch", l.Integration, "--",
		"run", "-f", promptPath, "--auto", "--title", title}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/onboard/ -v`
Expected: PASS (all launcher tests + the embed tests from Task 2).

- [ ] **Step 5: Commit**

```bash
git add internal/onboard/launcher.go internal/onboard/launcher_test.go
git commit -m "onboard: add launcher interface with opencode and ollama impls"
```

---

### Task 4: CLI — onboarding command group (opencode + ollama subcommands)

**Files:**
- Create: `internal/cli/onboarding.go`
- Modify: `internal/cli/root.go:64-72` (the `root.AddCommand` block)

**Interfaces:**
- Consumes:
  - `onboard.Latest`, `onboard.Data`, `onboard.Render`, `onboard.Launcher`, `onboard.OpencodeLauncher`, `onboard.OllamaLauncher` (from Task 2 + Task 3).
  - `cliState.openStore`, `cliState.resolveActor`, `cliState.stdout`, `cliState.stderr`, `cliState.isJSON`, `cliState.emit`, `cliState.flags` (existing).
  - `store.Store.GetProject`, `store.Store.ListTasks`, `store.Store.LabelList`, `store.Store.StorePath`, `store.ErrNotFound` (existing).
  - `ExitNotFound`, `ExitUsage`, `ErrNotFound`, `ErrUsage`, `writeJSON` (existing).
- Produces:
  - `newOnboardingCmd(st *cliState) *cobra.Command` — the group command with two children.
  - `newOnboardingOpencodeCmd(st *cliState) *cobra.Command`.
  - `newOnboardingOllamaCmd(st *cliState) *cobra.Command`.
  - `type onboardingOpts struct { Project, Actor, PromptVersion string; DryRun bool }`.
  - `runOnboarding(st *cliState, l onboard.Launcher, opts onboardingOpts) error` — the shared orchestrator.

- [ ] **Step 1: Write the orchestrator and commands in onboarding.go**

Create `internal/cli/onboarding.go`:

```go
package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"atm/internal/onboard"
	"atm/internal/store"

	"github.com/spf13/cobra"
)

type onboardingOpts struct {
	Project       string
	Actor         string
	PromptVersion string
	DryRun        bool
}

func newOnboardingCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "onboarding",
		Short: "Launch a non-interactive agent to seed an existing project with context",
	}
	cmd.AddCommand(newOnboardingOpencodeCmd(st))
	cmd.AddCommand(newOnboardingOllamaCmd(st))
	return cmd
}

func newOnboardingOpencodeCmd(st *cliState) *cobra.Command {
	var opts onboardingOpts
	cmd := &cobra.Command{
		Use:   "opencode",
		Short: "Onboard via opencode run --auto",
		RunE: func(cmd *cobra.Command, args []string) error {
			l := onboard.OpencodeLauncher{}
			opts.Actor = defaultActor(l.Name(), st, opts.Actor)
			return runOnboarding(st, l, opts)
		},
	}
	addOnboardingFlags(cmd, &opts)
	return cmd
}

func newOnboardingOllamaCmd(st *cliState) *cobra.Command {
	var opts onboardingOpts
	var integration string
	cmd := &cobra.Command{
		Use:   "ollama",
		Short: "Onboard via ollama launch <integration> -- run --auto",
		RunE: func(cmd *cobra.Command, args []string) error {
			l := onboard.OllamaLauncher{Integration: integration}
			opts.Actor = defaultActor(l.Name(), st, opts.Actor)
			return runOnboarding(st, l, opts)
		},
	}
	addOnboardingFlags(cmd, &opts)
	cmd.Flags().StringVar(&integration, "integration", "", "ollama integration name (e.g. opencode, codex, claude)")
	_ = cmd.MarkFlagRequired("integration")
	return cmd
}

func addOnboardingFlags(cmd *cobra.Command, opts *onboardingOpts) {
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project to onboard into (must pre-exist)")
	cmd.Flags().StringVar(&opts.Actor, "actor", "", "actor id stamped into history (default <launcher>-onboard)")
	cmd.Flags().StringVar(&opts.PromptVersion, "prompt-version", "", "embedded prompt version (default latest)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "render + write prompt + print argv; do not launch")
	_ = cmd.MarkFlagRequired("project")
}

// defaultActor returns the explicit actor if set, the global --actor/ATM_ACTOR
// if set, or "<launcher>-onboard" as the final fallback.
func defaultActor(launcherName string, st *cliState, explicit string) string {
	if explicit != "" {
		return explicit
	}
	if st.flags.actor != "" {
		return st.flags.actor
	}
	return launcherName + "-onboard"
}

// runOnboarding validates the project, snapshots existing tasks, renders the
// prompt, writes it to $ATM_HOME/onboarding/<run-id>.md, prints the header,
// and (unless --dry-run) execs the launcher as a child. It prints the tail
// summary after the child exits.
func runOnboarding(st *cliState, l onboard.Launcher, opts onboardingOpts) error {
	s, err := st.openStore()
	if err != nil {
		return err
	}
	p, err := s.GetProject(opts.Project)
	if err != nil {
		return fmt.Errorf("%w: project %s not found; create it first:\n  atm project create --code %s --name \"...\"",
			ErrNotFound, opts.Project, opts.Project)
	}

	version := opts.PromptVersion
	if version == "" {
		version = onboard.Latest
	}

	existing := s.ListTasks(store.QueryFilters{Project: opts.Project})
	snapshot := renderExistingTasksTable(existing)

	atmBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve atm binary: %w", err)
	}

	runID := newRunID(opts.Project)
	title := fmt.Sprintf("ATM onboarding: %s (%s)", opts.Project, runID)
	promptPath := filepath.Join(s.StorePath(), "onboarding", runID+".md")
	if err := os.MkdirAll(filepath.Dir(promptPath), 0o755); err != nil {
		return fmt.Errorf("create onboarding dir: %w", err)
	}

	rendered, err := onboard.Render(version, onboard.Data{
		Code:          p.Code,
		Name:          p.Name,
		ATMBin:        atmBin,
		Actor:         opts.Actor,
		RunID:         runID,
		Timestamp:     time.Now().UTC().Format(store.RFC3339UTC),
		ExistingTasks: snapshot,
	})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if err := os.WriteFile(promptPath, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("write prompt file %s: %w", promptPath, err)
	}

	argv := l.BuildArgv(promptPath, title)
	if err := emitOnboardingHeader(st, l.Name(), opts.Project, runID, version, promptPath, argv); err != nil {
		return err
	}

	if opts.DryRun {
		return nil
	}

	before := len(existing)
	exitCode, runErr := runChild(l, argv)
	after := len(s.ListTasks(store.QueryFilters{Project: opts.Project}))
	if err := emitOnboardingTail(st, opts.Project, runID, version, promptPath, before, after, exitCode); err != nil {
		return err
	}
	if runErr != nil {
		return fmt.Errorf("%s exited: %w", l.Name(), runErr)
	}
	return nil
}

// runChild execs the launcher as a child, inheriting the caller's stdio. It
// returns the process exit code and error.
func runChild(l onboard.Launcher, argv []string) (int, error) {
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		return 0, fmt.Errorf("%s not found on PATH; install: %s", l.Name(), l.NotFoundHint())
	}
	cmd := exec.Command(bin, argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err = cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), err
		}
		return 1, err
	}
	return 0, nil
}

func emitOnboardingHeader(st *cliState, launcherName, project, runID, version, promptPath string, argv []string) error {
	return st.emit(st.stdout(), map[string]any{
		"run_id":         runID,
		"project":        project,
		"agent":          launcherName,
		"prompt_version": version,
		"prompt_path":    promptPath,
		"argv":           argv,
	}, func() {
		fmt.Fprintf(os.Stdout, "onboarding %s  run=%s  agent=%s  prompt-version=%s\n", project, runID, launcherName, version)
		fmt.Fprintf(os.Stdout, "  prompt:  %s\n", promptPath)
		fmt.Fprintf(os.Stdout, "  launching: %s\n", strings.Join(argv, " "))
	})
}

func emitOnboardingTail(st *cliState, project, runID, version, promptPath string, before, after, agentExit int) error {
	return st.emit(st.stdout(), map[string]any{
		"run_id":         runID,
		"project":        project,
		"prompt_version": version,
		"prompt_path":    promptPath,
		"before":         before,
		"after":          after,
		"agent_exit":     agentExit,
	}, func() {
		fmt.Fprintf(os.Stdout, "onboarding %s  run=%s\n", project, runID)
		fmt.Fprintf(os.Stdout, "  prompt:  %s\n", promptPath)
		fmt.Fprintf(os.Stdout, "  before:  %d tasks\n", before)
		fmt.Fprintf(os.Stdout, "  after:   %d tasks\n", after)
		fmt.Fprintf(os.Stdout, "  created: %d   (net)\n", after-before)
		fmt.Fprintf(os.Stdout, "agent exited %d\n", agentExit)
	})
}

func renderExistingTasksTable(tasks []*store.Task) string {
	if len(tasks) == 0 {
		return "(none)"
	}
	var b strings.Builder
	b.WriteString("| ID | Title | Labels |\n")
	b.WriteString("|----|-------|--------|\n")
	for _, t := range tasks {
		labels := strings.Join(t.Labels, ", ")
		if labels == "" {
			labels = "(none)"
		}
		fmt.Fprintf(&b, "| %s | %s | %s |\n", t.ID, t.Title, labels)
	}
	return b.String()
}

func newRunID(code string) string {
	return fmt.Sprintf("%s-%s-%s",
		code,
		time.Now().UTC().Format("20060102150405"),
		shortUUID(),
	)
}

// shortUUID returns a 6-char hex suffix for collision safety in run IDs.
func shortUUID() string {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "000000"
	}
	return fmt.Sprintf("%x", b[:])
}
```

- [ ] **Step 2: Add the crypto/rand import**

The `shortUUID` helper uses `crypto/rand`. Update the import block of `internal/cli/onboarding.go` to include `"crypto/rand"` at the top of the import group (alphabetically before `"fmt"`). The final import block is:

```go
import (
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"atm/internal/onboard"
	"atm/internal/store"

	"github.com/spf13/cobra"
)
```

- [ ] **Step 3: Register the command on root**

In `internal/cli/root.go`, in the `newRootCmdWithState` function's `root.AddCommand(...)` block (currently lines 64-71), add `root.AddCommand(newOnboardingCmd(st))` between `newTaskCmd(st)` and `newTUICmd(st)`. The updated block:

```go
	root.AddCommand(newInitCmd(st))
	root.AddCommand(newStoreCmd(st))
	root.AddCommand(newConventionsCmd(st))
	root.AddCommand(newProjectCmd(st))
	root.AddCommand(newLabelCmd(st))
	root.AddCommand(newTaskCmd(st))
	root.AddCommand(newOnboardingCmd(st))
	root.AddCommand(newTUICmd(st))
	root.AddCommand(newVersionCmd(st))
```

- [ ] **Step 4: Verify the build compiles**

Run: `go build ./...`
Expected: build succeeds.

- [ ] **Step 5: Commit (no tests yet — tests come in Task 5)**

```bash
git add internal/cli/onboarding.go internal/cli/root.go
git commit -m "cli: add onboarding opencode and ollama subcommands"
```

---

### Task 5: CLI — onboarding tests (golden pattern)

**Files:**
- Create: `internal/cli/onboarding_test.go`
- Create: `internal/cli/testdata/golden/onboarding-dry-run-opencode.json`
- Create: `internal/cli/testdata/golden/onboarding-dry-run-ollama.json`
- Create: `internal/cli/testdata/golden/onboarding-missing-project.json`
- Create: `internal/cli/testdata/golden/onboarding-unknown-prompt-version.json`

**Interfaces:**
- Consumes: `newGoldenHarness`, `compareGolden`, `normalizeOutput`, `seedScenario1`, the `goldenHarness.run` helper, `outputJSON`, `outputText` (existing).
- Produces: golden fixtures + a regression net for header/tail/dry-run/error shapes.

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/onboarding_test.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOnboardingOpencodeDryRunJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("onboarding", "opencode", "--project", "FOO", "--dry-run")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	// Normalize the run-id and prompt path (timestamp + uuid + store path are
	// run-specific) before comparing to the golden file.
	got := normalizeOnboardingOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "onboarding-dry-run-opencode", got)
}

func TestOnboardingOllamaDryRunJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("onboarding", "ollama", "--project", "FOO", "--integration", "opencode", "--dry-run")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeOnboardingOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "onboarding-dry-run-ollama", got)
}

func TestOnboardingMissingProject(t *testing.T) {
	h := newGoldenHarness(t)
	h.reset()
	_, stderrStr, code := h.run("onboarding", "opencode", "--project", "NOPE", "--dry-run")
	if code != ExitNotFound {
		t.Fatalf("exit = %d, want %d (not found)", code, ExitNotFound)
	}
	// Error goes to stderr in the existing envelope (JSON mode harness).
	got := normalizeOnboardingOutput(stderrStr, h.store.StorePath())
	compareGolden(t, "onboarding-missing-project", got)
}

func TestOnboardingUnknownPromptVersion(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, stderrStr, code := h.run("onboarding", "opencode", "--project", "FOO", "--prompt-version", "vNoSuch", "--dry-run")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d (usage)", code, ExitUsage)
	}
	got := normalizeOnboardingOutput(stderrStr, h.store.StorePath())
	compareGolden(t, "onboarding-unknown-prompt-version", got)
}

// normalizeOnboardingOutput scrubs run-specific values (run-id, prompt path,
// timestamps) so golden comparison is stable. It reuses normalizeOutput for the
// store-path regex and adds an onboarding-specific run-id regex.
func normalizeOnboardingOutput(s, storePath string) string {
	s = normalizeOutput(s)
	// run-id: <CODE>-<14 digits>-<6 hex>
	runIDRe := regexp.MustCompile(`FOO-\d{14}-[0-9a-f]{6}`)
	s = runIDRe.ReplaceAllString(s, "FOO-RUNID")
	// prompt path: <store>/onboarding/<run-id>.md
	promptPathRe := regexp.MustCompile(strings.ReplaceAll(filepath.ToSlash(storePath), `/`, `\/`) + `/onboarding/FOO-\d{14}-[0-9a-f]{6}\.md`)
	s = promptPathRe.ReplaceAllString(s, "/STORE/onboarding/FOO-RUNID.md")
	return s
}
```

Also add `"regexp"` to the import block of `onboarding_test.go`:

```go
import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)
```

- [ ] **Step 2: Run tests to verify they fail (missing goldens)**

Run: `go test ./internal/cli/ -run TestOnboarding -v`
Expected: FAIL with "missing golden ... (run with -update to create)".

- [ ] **Step 3: Generate the golden fixtures**

Run: `go test ./internal/cli/ -run TestOnboarding -update`
Expected: golden files created under `internal/cli/testdata/golden/`.

Inspect each generated golden file to confirm the shape matches the spec:
- `onboarding-dry-run-opencode.json` — header JSON with `agent: "opencode"`, `argv` ending in `["run","-f","...","--auto","--title","ATM onboarding: FOO (FOO-RUNID)"]`.
- `onboarding-dry-run-ollama.json` — same shape but `agent: "ollama"`, `argv` prefixed `["ollama","launch","opencode","--",...]`.
- `onboarding-missing-project.json` — error envelope with `code: "not-found"`.
- `onboarding-unknown-prompt-version.json` — error envelope with `code: "usage"`.

If any golden's shape is wrong, fix the implementation in `onboarding.go` (not the golden), regenerate with `-update`, and re-inspect.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run TestOnboarding -v`
Expected: PASS (all four tests).

- [ ] **Step 5: Verify the full build is green**

Run: `make verify`
Expected: build + all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/onboarding_test.go internal/cli/testdata/golden/onboarding-*.json
git commit -m "test: golden tests for onboarding dry-run and error cases"
```

---

### Task 6: Smoke script + README section

**Files:**
- Create: `scripts/onboard-smoke.sh`
- Modify: `README.md` (add an Onboarding section)

**Interfaces:**
- Consumes: the `atm` binary built from `make build`.
- Produces: a manual smoke test script and user-facing documentation.

- [ ] **Step 1: Create the smoke script**

Create `scripts/onboard-smoke.sh`:

```bash
#!/usr/bin/env bash
# Manual smoke test for `atm onboarding`. Not run by `make verify`.
# Usage: ./scripts/onboard-smoke.sh /path/to/repo-to-onboard
set -euo pipefail

repo="${1:-.}"
store_dir="$(mktemp -d)"
trap 'rm -rf "$store_dir"' EXIT

echo "## setup: init store + create FOO project"
atm --store "$store_dir" init
atm --store "$store_dir" project create --code FOO --name "Foo" --actor smoke

echo "## dry-run: render prompt + print argv (no launch)"
atm --store "$store_dir" onboarding opencode --project FOO --dry-run

prompt_file="$(ls "$store_dir"/onboarding/*.md | head -1)"
echo "## prompt file: $prompt_file"
echo "## first 20 lines:"
sed -n '1,20p' "$prompt_file"

echo "## ollama dry-run (integration=opencode)"
atm --store "$store_dir" onboarding ollama --project FOO --integration opencode --dry-run

echo "## missing-project error (expect exit 3)"
atm --store "$store_dir" onboarding opencode --project NOPE --dry-run || echo "exit=$?"

echo "## unknown prompt-version (expect exit 2)"
atm --store "$store_dir" onboarding opencode --project FOO --prompt-version vNoSuch --dry-run || echo "exit=$?"

echo "## live run (requires opencode on PATH; runs in '$repo')"
read -r -p "Run live onboarding against '$repo'? [y/N] " ans
if [[ "$ans" == "y" || "$ans" == "Y" ]]; then
  (cd "$repo" && atm --store "$store_dir" onboarding opencode --project FOO)
  echo "## post-run task list:"
  atm --store "$store_dir" task list --project FOO
fi
```

Make it executable:

```bash
chmod +x scripts/onboard-smoke.sh
```

- [ ] **Step 2: Add the README section**

In `README.md`, find the existing section that documents the CLI commands (after the conventions/onboarding area). Append a new `## Onboarding` section:

```markdown
## Onboarding

`atm onboarding` launches a non-interactive agent that explores the current
working directory and seeds an existing ATM project with context tasks. The
agent runs under its own permission model; ATM is the prompt-renderer and
process parent.

Prerequisite: the project must already exist.

```
atm project create --code FOO --name "Foo"
cd /path/to/repo-to-onboard
atm onboarding opencode --project FOO
```

For an ollama-backed agent:

```
atm onboarding ollama --project FOO --integration opencode
```

Flags:

- `--project <CODE>` (required) — the existing ATM project.
- `--actor <id>` (default `<launcher>-onboard`) — stamped into history.
- `--prompt-version <v>` (default latest) — select an embedded prompt version.
- `--dry-run` — render the prompt and print the launcher command without launching.
- `--integration <name>` (ollama only, required) — passed through to `ollama launch`.

Re-running onboarding is idempotent: the agent reads existing tasks and updates
rather than duplicating. Run it per repo to build a multi-repo context map for
a single project.

A smoke script exercises dry-run + error paths against a temp store:

```
./scripts/onboard-smoke.sh /path/to/repo
```
```

- [ ] **Step 3: Verify the build is still green**

Run: `make verify`
Expected: build + all tests pass (the script is not part of `make verify`).

- [ ] **Step 4: Commit**

```bash
git add scripts/onboard-smoke.sh README.md
git commit -m "docs: onboarding smoke script and README section"
```

---

## Self-Review

**1. Spec coverage:**

- Scope (v1): two subcommands — Task 4. Pre-existing project — Task 4 (`runOnboarding` returns `ErrNotFound` if `GetProject` fails). Agent runs in cwd — Task 4 (`cmd.Dir` is not set; child inherits cwd). Idempotent — Task 2 (prompt step 6 + `<EXISTING_TASKS>` snapshot from Task 4). Embedded prompt — Task 2.
- Out of scope (v1): no `codex`/`claude` direct subcommands (none added); no external systems (none added); no MCP (none added); no `--prompt-file` (only `--prompt-version`); no TUI entrypoint (CLI only); no audit log; no GC; no integration validation (pass-through). All confirmed absent in the plan.
- Command surface: both subcommands, all five flags, exec argv shapes — Task 4 + Task 3.
- Execution flow: 10 steps — Task 4 (`runOnboarding` + `runChild` + `emitOnboardingHeader` + `emitOnboardingTail`).
- ATM stays the parent — Task 4 (`cmd.Run()`, not `exec`).
- Idempotency — Task 2 (prompt) + Task 4 (`renderExistingTasksTable` snapshot).
- Prompt logic vs. label logic — Task 2 (prompt is label-agnostic).
- Label-description refinement + `context:question` — Task 1.
- Data model: no new store entities; `$ATM_HOME/onboarding/` — Task 4 (`MkdirAll`). Embedded assets — Task 2.
- CLI command & error handling: all six error cases — Task 4 + Task 5 (missing project, unknown version, launcher not on PATH via `runChild`, launcher non-zero exit, prompt-file write fail, store open fail). Tail + header shapes — Task 4 + Task 5 (golden).
- Testing: unit tests for embed, launcher, CLI (golden), seed — Tasks 1, 2, 3, 5. What is NOT unit-tested (live exec) — Task 6 (smoke script). Verification gate — every task runs `make verify`.
- Rollout: 6 layered commits — one per task. Each independently green.

**2. Placeholder scan:** No "TBD", "TODO", "implement later", "fill in details". Each code step contains the actual code. Each test step contains the actual test. The prompt asset (Task 2 step 1) is the full v1 prompt body, not a sketch. The `context:*` description wording (Task 1 step 3) is the final wording, not "TBD".

**3. Type consistency:**
- `onboard.Data` fields: `Code, Name, ATMBin, Actor, RunID, Timestamp, ExistingTasks` — used identically in Task 2 (definition), Task 4 (caller). Match.
- `onboard.Launcher` interface methods: `Name()`, `NotFoundHint()`, `BuildArgv(promptPath, title string) []string` — used identically in Task 3 (definition), Task 4 (`l.Name()`, `l.BuildArgv(...)`). Match.
- `onboard.OpencodeLauncher{}` and `onboard.OllamaLauncher{Integration: ...}` — constructed in Task 4 with the exact field name `Integration` defined in Task 3. Match.
- `onboardingOpts` fields: `Project, Actor, PromptVersion, DryRun` — used identically in Task 4 across both subcommands and `runOnboarding`. Match.
- `renderExistingTasksTable` takes `[]*store.Task` and reads `t.ID`, `t.Title`, `t.Labels` — these are the existing `store.Task` fields (verified in `internal/store/task.go`). Match.
- `store.QueryFilters{Project: ...}` — matches the existing constructor usage in `internal/cli/task.go`. Match.
- `store.RFC3339UTC` — used in Task 4; exists per the v2 spec's store API surface ("RFC3339UTC/Now"). Match.
- `ErrNotFound`, `ErrUsage`, `ExitNotFound`, `ExitUsage`, `ExitSuccess` — used in Task 4 + Task 5; all exist in `internal/cli/errors.go`. Match.

No issues found. Plan is complete.