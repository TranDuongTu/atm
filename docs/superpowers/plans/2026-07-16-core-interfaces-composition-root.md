# Core Interfaces + Composition Root (ATM-b9d83a) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move ATM's domain types and pure helpers into `internal/core`, define the service interfaces adapters consume (satisfied structurally by `*store.Store`), put the TUI on the interface only, move wiring to a real composition root in `cmd/atm`, and delete the `cli→tui` and `version→store` backwards edges.

**Architecture:** Behavior-preserving refactor per `docs/superpowers/specs/2026-07-16-core-interfaces-composition-root-design.md` (read it first) and `docs/architecture/logical-components.md`. Types move to core; `internal/store` keeps *type aliases* (`type Task = core.Task`) so store internals and all CLI call-sites compile unchanged. Interfaces are role-segregated + one `Service` composite. Storage-format admin methods (`Verify`, `Rebuild`, `Upgrade*`, `PruneProjectV1`, `SetActiveFormat`, `ReadV2LogForDisplay`) and their report types stay in store — core must never learn persistence is event-sourced.

**Tech Stack:** Go 1.22+, cobra (cli), Bubble Tea (tui). Verify gate: `make verify` (build + `go test ./... ./libs/eventsource/...` + scripts-test).

**Ledger:** ATM task `ATM-b9d83a` (status already `in-progress`). Comment progress there as tasks land, actor `developer@claude:fable-5`.

## Global Constraints

- `internal/core` imports **nothing** from this repository and nothing outside the Go standard library. (`context` is stdlib and allowed.)
- Zero behavior change: no golden output, error message, or exit-code change anywhere. Error sentinels move by **variable aliasing** (`var ErrNotFound = core.ErrNotFound`) so `errors.Is` identity is preserved.
- No interface method or core type may name an event, replica, HLC, projector, or v1/v2 store format.
- Every task ends with `make verify` green and a commit. No emojis in code or commits.
- Commit messages: `refactor(ATM-b9d83a): <what>` (or `test(...)`, `docs(...)`).
- All symbol references below were verified against `main` @ `71966ba`. Locate declarations **by symbol name**, not by line number — earlier tasks shift lines in the same files.

---

### Task 1: Pure helpers + error sentinels → core

**Files:**
- Create: `internal/core/errors.go`, `internal/core/errors_test.go`
- Create: `internal/core/convention.go`, `internal/core/convention_test.go`
- Modify: `internal/core/label.go` (add `IsNamespaceName`)
- Create: `internal/store/core_aliases.go`
- Modify: `internal/store/store.go` (delete moved decls), `internal/store/log.go` (delete `ErrIntegrity` + `IsIntegrity`), `internal/store/persona.go` (delete `ValidatePersonaName` + `personaNameRe`)

**Interfaces:**
- Produces: `core.ErrNotFound/ErrConflict/ErrIntegrity`, `core.IsNotFound/IsConflict/IsIntegrity(err error) bool`, `core.Now() time.Time`, `core.RFC3339UTC(t time.Time) string`, `core.TaskIDRe`, `core.ParseTaskID(id string) (code string, n int, ok bool)`, `core.CommentIDRe`, `core.ParseCommentID(id string) (code string, taskN, commentN int, ok bool)`, `core.ValidatePersonaName(name string) error`, `core.IsNamespaceName(name string) bool`. Store re-exports all of them under the old names; later tasks rely on both spellings.

**Note on the ID-parsing family:** the whole family (`TaskIDRe`, `ParseTaskID`, `CommentIDRe`, `ParseCommentID`, and their shared private helper `numericOrZero`) moves to core **together**, and `numericOrZero` exists in exactly ONE place afterwards. Do not copy it. `ParseTaskID` is pure regex parsing in the same family as `ParseCommentID` and is called only from inside store (`task.go`, `comment.go`, `query.go`), which reaches it through the alias.

- [ ] **Step 1: Write failing core tests**

`internal/core/errors_test.go`:

```go
package core

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorPredicatesMatchWrapped(t *testing.T) {
	cases := []struct {
		err  error
		pred func(error) bool
	}{
		{fmt.Errorf("task %q: %w", "ATM-1", ErrNotFound), IsNotFound},
		{fmt.Errorf("stale: %w", ErrConflict), IsConflict},
		{fmt.Errorf("log: %w", ErrIntegrity), IsIntegrity},
		{fmt.Errorf("bad flag: %w", ErrUsage), IsUsage},
	}
	for i, c := range cases {
		if !c.pred(c.err) {
			t.Fatalf("case %d: predicate did not match wrapped sentinel", i)
		}
		if c.pred(errors.New("other")) {
			t.Fatalf("case %d: predicate matched unrelated error", i)
		}
	}
}

// The CLI maps ErrUsage to exit code 2 (cli/errors.go CodeForError). This
// pins both the wrap and the exact message the TUI persona form renders.
func TestValidatePersonaNameWrapsErrUsage(t *testing.T) {
	err := ValidatePersonaName("Bad Name")
	if !IsUsage(err) {
		t.Fatal("must wrap ErrUsage so the CLI maps it to exit 2")
	}
	want := `usage: invalid persona name "Bad Name" (want ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$)`
	if err.Error() != want {
		t.Fatalf("message drift:\n got %q\nwant %q", err.Error(), want)
	}
}
```

`internal/core/convention_test.go`:

```go
package core

import (
	"testing"
	"time"
)

func TestRFC3339UTC(t *testing.T) {
	in := time.Date(2026, 7, 16, 10, 30, 0, 0, time.FixedZone("X", 7*3600))
	if got, want := RFC3339UTC(in), "2026-07-16T03:30:00Z"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestParseTaskID(t *testing.T) {
	if code, n, ok := ParseTaskID("ATM-0001"); !ok || code != "ATM" || n != 1 {
		t.Fatalf("v1 id: got %q %d %v", code, n, ok)
	}
	if code, n, ok := ParseTaskID("ATM-7f3a2b"); !ok || code != "ATM" || n != 0 {
		t.Fatalf("v2 alias: got %q %d %v", code, n, ok)
	}
	if _, _, ok := ParseTaskID("ATM-7F3A2B"); ok {
		t.Fatal("uppercase hex must not parse")
	}
}

func TestParseCommentID(t *testing.T) {
	code, taskN, commentN, ok := ParseCommentID("ATM-0001-c0002")
	if !ok || code != "ATM" || taskN != 1 || commentN != 2 {
		t.Fatalf("v1 id: got %q %d %d %v", code, taskN, commentN, ok)
	}
	code, taskN, commentN, ok = ParseCommentID("ATM-7f3a2b-c9e1d")
	if !ok || code != "ATM" || taskN != 0 || commentN != 0 {
		t.Fatalf("v2 alias: got %q %d %d %v", code, taskN, commentN, ok)
	}
	if _, _, _, ok := ParseCommentID("nope"); ok {
		t.Fatal("junk id parsed")
	}
}

func TestValidatePersonaName(t *testing.T) {
	if err := ValidatePersonaName("dev-agent2"); err != nil {
		t.Fatalf("valid name rejected: %v", err)
	}
	for _, bad := range []string{"", "-x", "x-", "UPPER", "a b"} {
		if err := ValidatePersonaName(bad); err == nil {
			t.Fatalf("invalid name %q accepted", bad)
		}
	}
}

func TestIsNamespaceName(t *testing.T) {
	if !IsNamespaceName("ATM:status:*") || IsNamespaceName("ATM:status:open") {
		t.Fatal("IsNamespaceName wrong")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/core/`
Expected: FAIL — `undefined: ErrNotFound`, `undefined: RFC3339UTC`, etc.

- [ ] **Step 3: Create the core files (move implementations verbatim)**

`internal/core/errors.go` — new file; the sentinel messages are copied **exactly** from `store/store.go` (`ErrNotFound`, `ErrConflict`) and `store/log.go` (`ErrIntegrity`):

```go
package core

import "errors"

// Domain error kinds. Adapters classify failures with the Is* predicates;
// the store wraps these sentinels so errors.Is matches across layers.
var (
	ErrNotFound  = errors.New("not found")
	ErrConflict  = errors.New("conflict")
	ErrIntegrity = errors.New("integrity")
	ErrUsage     = errors.New("usage")
)

func IsNotFound(err error) bool  { return errors.Is(err, ErrNotFound) }
func IsConflict(err error) bool  { return errors.Is(err, ErrConflict) }
func IsIntegrity(err error) bool { return errors.Is(err, ErrIntegrity) }
func IsUsage(err error) bool     { return errors.Is(err, ErrUsage) }
```

**Why `ErrUsage` is here** (this supersedes an earlier draft that kept it in store): core owns the vocabulary rules — `ValidatePersonaName` now, `ValidateLabelName`/`ValidateProjectCode` later — and every one of them expresses a violation by wrapping `ErrUsage`, so core cannot own vocabulary rules without owning that error kind. `cli/errors.go`'s `CodeForError` gates exit code 2 on `errors.Is(err, store.ErrUsage)`; the store alias holds the same pointer, so identity — and the exit code — is preserved. All 38 of store's `fmt.Errorf("%w: ...", ErrUsage, ...)` sites keep compiling untouched.

`internal/core/convention.go` — new file. Move these declarations **verbatim** from `internal/store/store.go`: `RFC3339UTC`, `Now` (the package-level function, NOT the `(s *Store) Now` method — that stays), `TaskIDRe`, `ParseTaskID`, `CommentIDRe`, `ParseCommentID` (each with its doc comment), the shared private helper `numericOrZero` (moved, NOT copied — delete it from store), and from `internal/store/persona.go`: `personaNameRe`, `ValidatePersonaName`. File skeleton:

```go
package core

import (
	"fmt"
	"regexp"
	"time"
)

// ... RFC3339UTC, Now, TaskIDRe, ParseTaskID, CommentIDRe, ParseCommentID,
// numericOrZero, personaNameRe, ValidatePersonaName — bodies moved verbatim.
```

Note `RenderCommentID`/`RenderTaskID` stay in store — they are not referenced by this task's move set; leave them alone.

(If `ValidatePersonaName` uses `fmt`, keep the import; drop any import the moved bodies don't need — `go vet` will tell you.)

Append to `internal/core/label.go` (moved verbatim from `store/store.go`):

```go
// IsNamespaceName reports whether name is a namespace label (e.g. "ATM:status:*"),
// whose membership is every label sharing its prefix.
func IsNamespaceName(name string) bool { return strings.HasSuffix(name, ":*") }
```

- [ ] **Step 4: Run core tests to verify they pass**

Run: `go test ./internal/core/`
Expected: PASS

- [ ] **Step 5: Delete moved decls from store; add the alias file**

Delete from `internal/store/store.go`: `RFC3339UTC`, package-level `Now`, `TaskIDRe`, `ParseTaskID`, `CommentIDRe`, `ParseCommentID`, `numericOrZero`, `IsNamespaceName`, and the whole `var (...)` sentinel block (`ErrNotFound`/`ErrConflict`/`ErrUsage`) plus `IsNotFound`/`IsConflict`/`IsUsage`. Delete from `internal/store/log.go`: `var ErrIntegrity ...` and `func IsIntegrity ...`. Delete from `internal/store/persona.go`: `personaNameRe`, `ValidatePersonaName`.

Create `internal/store/core_aliases.go`:

```go
package store

// Temporary re-exports of symbols that moved to internal/core in refactor
// step 4 (ATM-b9d83a). They keep store's internals and the CLI compiling
// unchanged while the adapters migrate; step 6 (ATM-3b873c) removes them.

import "atm/internal/core"

var (
	ErrNotFound  = core.ErrNotFound
	ErrConflict  = core.ErrConflict
	ErrIntegrity = core.ErrIntegrity
	ErrUsage     = core.ErrUsage
)

var (
	IsNotFound          = core.IsNotFound
	IsConflict          = core.IsConflict
	IsIntegrity         = core.IsIntegrity
	IsUsage             = core.IsUsage
	Now                 = core.Now
	RFC3339UTC          = core.RFC3339UTC
	TaskIDRe            = core.TaskIDRe
	ParseTaskID         = core.ParseTaskID
	CommentIDRe         = core.CommentIDRe
	ParseCommentID      = core.ParseCommentID
	ValidatePersonaName = core.ValidatePersonaName
	IsNamespaceName     = core.IsNamespaceName
)
```

Note: `IsNamespaceName` is referenced by `store/types.go` (`Label.IsComputed`) and `ParseTaskID` by `store/task.go`, `store/comment.go`, `store/query.go` — the function-value aliases keep every call site compiling, and `store/id_alias_test.go` keeps passing unchanged.

- [ ] **Step 6: Verify**

Run: `make verify`
Expected: PASS (build, all tests both modules, scripts-test). If any store test asserted the *package* of a sentinel, fix the test's import, never the message text.

- [ ] **Step 7: Commit**

```bash
git add internal/core internal/store
git commit -m "refactor(ATM-b9d83a): move error kinds and pure convention helpers to core"
```

---

### Task 2: Board-expression AST → core (renamed `Expr`)

**Files:**
- Create: `internal/core/expr.go`, `internal/core/expr_test.go`
- Delete: `internal/store/expr.go`, `internal/store/expr_test.go`
- Modify: `internal/store/core_aliases.go`

**Interfaces:**
- Produces: `core.Expr` (interface), `core.ExprAtom/ExprNot/ExprAnd/ExprOr` (nodes), `core.ParseExpr(src string) (Expr, error)`, `core.Atoms(n Expr) []string`. Store re-exports old names (`Node`, `AtomNode`, …). Renamed because `core.Node[T]` (facet tree, step 3) already owns the `Node` identifier.

- [ ] **Step 1: Move the file with renames**

```bash
git mv internal/store/expr.go internal/core/expr.go
git mv internal/store/expr_test.go internal/core/expr_test.go
sed -i -e 's/^package store/package core/' \
  -e 's/\bAtomNode\b/ExprAtom/g' -e 's/\bNotNode\b/ExprNot/g' \
  -e 's/\bAndNode\b/ExprAnd/g' -e 's/\bOrNode\b/ExprOr/g' \
  -e 's/\bNode\b/Expr/g' -e 's/\bisNode\b/isExpr/g' \
  internal/core/expr.go internal/core/expr_test.go
```

Then eyeball both files: the `Node`→`Expr` sed is ordered AFTER the composite names so `AtomNode` never becomes `AtomExpr`; confirm no comment text was mangled and the interface reads `type Expr interface{ isExpr() }`.

- [ ] **Step 2: Re-export the old names from store**

Append to `internal/store/core_aliases.go`:

```go
// Board-expression AST (renamed on the move: Node -> Expr).
type Node = core.Expr
type AtomNode = core.ExprAtom
type NotNode = core.ExprNot
type AndNode = core.ExprAnd
type OrNode = core.ExprOr

var (
	ParseExpr = core.ParseExpr
	Atoms     = core.Atoms
)
```

`store/query.go` and `store/label.go` use these names (including type switches over `*AtomNode` etc.); aliases are identical types, so they compile unchanged.

- [ ] **Step 3: Verify**

Run: `make verify`
Expected: PASS. `go test ./internal/core/ -run TestParseExpr -v` (or whatever the moved test names are) runs the AST tests from their new home.

- [ ] **Step 4: Commit**

```bash
git add internal/core internal/store
git commit -m "refactor(ATM-b9d83a): move board-expression AST to core, renamed Node->Expr"
```

---

### Task 3: Domain + read-model types → core

**Files:**
- Create: `internal/core/types.go`, `internal/core/types_test.go`, `internal/core/query.go`, `internal/core/activity.go`, `internal/core/config.go`, `internal/core/index.go`
- Modify: `internal/store/types.go` (empties out — delete the file), `internal/store/query.go`, `internal/store/log.go`, `internal/store/pins.go`, `internal/store/vocabulary.go`, `internal/store/config.go`, `internal/store/agents.go`, `internal/store/persona.go`, `internal/store/label.go`, `internal/store/search.go`, `internal/store/indexer.go`, `internal/store/vectors.go`, `internal/store/core_aliases.go`

**Interfaces:**
- Produces (all moved verbatim, same field sets, same struct tags): `core.Task`, `core.Label` (+ method `IsComputed`), `core.Comment`, `core.Project`, `core.Persona`, `core.LabelRemoveResult`, `core.QueryFilters`, `core.LabelGroup`, `core.LogEntry`, `core.Subject`, `core.HistoryView`, `core.Pins`, `core.Vocabulary`, `core.VocabularyTerm`, `core.EmbeddingConfig`, `core.ProjectConfig`, `core.AgentsConfig`, `core.SearchParams`, `core.Hit`, `core.IndexResult`, `core.EmbedFunc`, `core.ProgressFunc`, `core.VectorMeta`. Store re-exports every old name.
- NOT moved (persistence vocabulary, stay in store): `StoreFormat`, `StoreMeta`, `VerifyReport`, `CacheCheck`, `VectorIndexInfo`, `RebuildReport`, `UpgradeReport`, `PruneReport`, `V2LogView`, `IndexDoc`, `VectorEntry`, `Option`.

- [ ] **Step 1: Write the one failing behavior test**

`internal/core/types_test.go` (the only moved type with logic is `Label.IsComputed`):

```go
package core

import "testing"

func TestLabelIsComputed(t *testing.T) {
	if !(Label{Name: "ATM:done", Expr: "a AND b"}).IsComputed() {
		t.Fatal("board (Expr set) must be computed")
	}
	if !(Label{Name: "ATM:status:*"}).IsComputed() {
		t.Fatal("namespace label must be computed")
	}
	if (Label{Name: "ATM:status:open"}).IsComputed() {
		t.Fatal("plain label must not be computed")
	}
}
```

Run: `go test ./internal/core/ -run TestLabelIsComputed` — Expected: FAIL `undefined: Label`.

- [ ] **Step 2: Move the type blocks**

Move **verbatim** (declaration + doc comment + struct tags), grouped as:

- `internal/core/types.go` ← from `store/types.go`: `Label` (+ `IsComputed` method), `Project`, `Task`, `Comment` (that empties `store/types.go` — `git rm` it); from `store/persona.go`: `Persona`; from `store/label.go`: `LabelRemoveResult`.
- `internal/core/query.go` ← from `store/query.go`: `QueryFilters`, `LabelGroup`.
- `internal/core/activity.go` ← from `store/log.go`: `LogEntry`, `Subject`, `HistoryView`.
- `internal/core/config.go` ← from `store/config.go`: `EmbeddingConfig`, `ProjectConfig`; from `store/agents.go`: `AgentsConfig`; from `store/pins.go`: `Pins`; from `store/vocabulary.go`: `VocabularyTerm`, `Vocabulary`.
- `internal/core/index.go` ← from `store/search.go`: `Hit`, `SearchParams`; from `store/indexer.go`: `IndexResult`, `EmbedFunc`, `ProgressFunc`; from `store/vectors.go`: `VectorMeta`.

Each core file starts `package core` and imports only what the moved bodies need (`time`, `encoding/json` for `LogEntry.Payload`).

- [ ] **Step 3: Alias the old names in store**

Append to `internal/store/core_aliases.go`:

```go
// Domain and read-model types.
type Task = core.Task
type Label = core.Label
type Comment = core.Comment
type Project = core.Project
type Persona = core.Persona
type LabelRemoveResult = core.LabelRemoveResult
type QueryFilters = core.QueryFilters
type LabelGroup = core.LabelGroup
type LogEntry = core.LogEntry
type Subject = core.Subject
type HistoryView = core.HistoryView
type Pins = core.Pins
type Vocabulary = core.Vocabulary
type VocabularyTerm = core.VocabularyTerm
type EmbeddingConfig = core.EmbeddingConfig
type ProjectConfig = core.ProjectConfig
type AgentsConfig = core.AgentsConfig
type SearchParams = core.SearchParams
type Hit = core.Hit
type IndexResult = core.IndexResult
type EmbedFunc = core.EmbedFunc
type ProgressFunc = core.ProgressFunc
type VectorMeta = core.VectorMeta
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/core/ -run TestLabelIsComputed` — Expected: PASS.
Run: `make verify` — Expected: PASS with zero golden drift (aliases are identical types; JSON tags moved with the structs).

- [ ] **Step 5: Commit**

```bash
git add internal/core internal/store
git commit -m "refactor(ATM-b9d83a): move domain and read-model types to core behind store aliases"
```

---

### Task 4: Service interfaces + repository skeleton in core

**Files:**
- Create: `internal/core/service.go`, `internal/core/repository.go`
- Modify: `internal/store/store.go` (conformance assertion)

**Interfaces:**
- Consumes: every type Task 1-3 put in core.
- Produces: the role interfaces + `core.Service` composite below — the exact seam Tasks 5-6 program against. `*store.Store` must satisfy `core.Service` with **no store changes** beyond the assertion.

- [ ] **Step 1: Write `internal/core/service.go`**

Complete file (signatures transcribed from `*Store` methods; if the compiler disagrees with a signature here, the store method is authoritative — fix the interface, never the store):

```go
package core

import (
	"context"
	"time"
)

// The role interfaces below are the service seam of the hexagonal
// architecture (docs/architecture/logical-components.md): adapters consume
// them, internal/store satisfies them structurally. They cover exactly the
// union of what internal/tui and internal/cli invoke on the store today,
// minus the storage-format admin surface, which deliberately stays on the
// concrete store (core never knows persistence is event-sourced).

type TaskService interface {
	CreateTask(projectCode, title, description string, labels []string, actor string) (*Task, error)
	GetTask(id string) (*Task, error)
	ListTasks(filters QueryFilters) []*Task
	ListTasksErr(filters QueryFilters) ([]*Task, error)
	GroupTasks(filters QueryFilters) ([]LabelGroup, []*Task)
	GroupTasksErr(filters QueryFilters) ([]LabelGroup, []*Task, error)
	SetTitle(id, title, actor string) error
	SetDescription(id, description, actor string) error
	TaskLabelAdd(id, label, actor string) error
	TaskLabelRemove(id, label, actor string) error
	RemoveTask(id, actor string) error
}

type ProjectService interface {
	CreateProject(code, name, actor string) (*Project, error)
	GetProject(code string) (*Project, error)
	ListProjects() []*Project
	ProjectCodes() ([]string, error)
	SetProjectName(code, name, actor string) error
	RemoveProject(code, actor string) error
	GetProjectConfig(code string) (*ProjectConfig, error)
	ProjectRemotes(code string) (map[string]string, error)
	SetProjectRemote(code, name, url, actor string) error
	RemoveProjectRemote(code, name, actor string) error
}

type LabelService interface {
	LabelAdd(name, description, expr, actor string) error
	LabelList(project, namespace string) []Label
	LabelShow(name string) (Label, error)
	LabelRemove(name, actor string) (*LabelRemoveResult, error)
	LabelUsageGrouped(projectCode string) (map[string]int, error)
	SeedLabels(code, actor string) error
}

type CommentService interface {
	CreateComment(taskID, body string, labels []string, replyTo, actor string) (*Comment, error)
	GetComment(id string) (*Comment, error)
	ListComments(taskID string) ([]*Comment, error)
	SetCommentBody(id, body, actor string) error
	RemoveComment(id, actor string) error
	CommentLabelAdd(id, label, actor string) error
	CommentLabelRemove(id, label, actor string) error
}

type PersonaService interface {
	CreatePersona(name, prompt, description, actor string) (*Persona, error)
	GetPersona(name string) (*Persona, error)
	ListPersonas() []*Persona
	EditPersona(name string, prompt, description *string, actor string) (*Persona, error)
	RemovePersona(name string) error
}

type VocabularyService interface {
	GetVocabulary(code string) (*Vocabulary, error)
	WriteVocabulary(code string, v *Vocabulary) error
}

type ActivityService interface {
	ReadLogCached(code string) ([]LogEntry, error)
	LastLogSeq(code string) (int, error)
	History(code string, subject Subject) []HistoryView
	HistoryE(code string, subject Subject) ([]HistoryView, error)
	AppendInquiry(code, query string, citedIDs []string) error
}

type IndexService interface {
	ReindexOnce(ctx context.Context, code string, embed EmbedFunc, log ProgressFunc) (IndexResult, error)
	Watch(ctx context.Context, code string, embed EmbedFunc, log ProgressFunc) error
	ListVectorModels(code string) ([]string, error)
	VectorMeta(code, slug string) (*VectorMeta, error)
	DropVectors(code, slug string) error
	SetEmbeddingConfig(code string, cfg EmbeddingConfig, actor string) error
	Search(p SearchParams) (hits []Hit, fallbackUsed bool, err error)
}

type PinService interface {
	GetPins(code string) (*Pins, error)
	WritePins(code string, p *Pins) error
}

type AgentService interface {
	GetAgentsConfig() (AgentsConfig, error)
	SetSelectedAgent(name, actor string) error
	SetAgentArgs(name string, args []string, actor string) error
}

type MaintenanceService interface {
	Init(storePath string) error
	StorePath() string
	Now() time.Time
}

// Service is the composite the composition root injects into adapters.
type Service interface {
	TaskService
	ProjectService
	LabelService
	CommentService
	PersonaService
	VocabularyService
	ActivityService
	IndexService
	PinService
	AgentService
	MaintenanceService
}
```

- [ ] **Step 2: Write `internal/core/repository.go`**

Complete file:

```go
package core

// Repository interfaces are the persistence seam DECLARED by refactor step 4
// and IMPLEMENTED by step 6 (ATM-3b873c), which carves the store's event-log
// write-engine behind them. Nothing consumes them yet; step 6 may refine the
// shapes when the carve is studied. They mirror what the store's cache layer
// provides today, in domain terms only.

type TaskRepository interface {
	PutTask(t *Task) error
	GetTask(id string) (*Task, error)
	ListTasksForProject(code string) ([]*Task, error)
	DeleteTask(id string) error
}

type LabelRepository interface {
	PutLabel(l Label) error
	GetLabel(name string) (Label, error)
	ListLabels(project string) ([]Label, error)
	DeleteLabel(name string) error
}

type ProjectRepository interface {
	PutProject(p *Project) error
	GetProject(code string) (*Project, error)
	ListProjects() ([]*Project, error)
	DeleteProject(code string) error
}

type CommentRepository interface {
	PutComment(c *Comment) error
	GetComment(id string) (*Comment, error)
	ListCommentsForTask(taskID string) ([]*Comment, error)
	DeleteComment(id string) error
}
```

- [ ] **Step 3: Add the conformance assertion (this is the test)**

In `internal/store/store.go`, directly under `type Store struct { ... }`:

```go
// Store satisfies core's service seam structurally (refactor step 4).
var _ core.Service = (*Store)(nil)
```

(`internal/store/store.go` must import `atm/internal/core` — it may already via other code; add if missing.)

- [ ] **Step 4: Build to verify conformance, then full verify**

Run: `go build ./...`
Expected: compiles. If it fails with "*Store does not implement core.Service", the interface signature is wrong — fix `service.go` to match the store method exactly.
Run: `make verify` — Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core internal/store
git commit -m "refactor(ATM-b9d83a): define core service role interfaces and repository skeleton"
```

---

### Task 5: TUI production files consume core, not store

**Files:**
- Modify: every non-test file in `internal/tui/` that references `store.` — at `71966ba` that is: `app.go`, `board_editor.go`, `projects.go`, `tasks.go`, `tasks_list.go`, `tasks_detail.go`, `tasks_grouping.go`, `tasks_mutations.go`, `comments.go`, `labels.go`, `indexer.go`, `actors.go`, `plugin.go` (confirm with the grep in Step 1).

**Interfaces:**
- Consumes: `core.Service` and the core types/helpers from Tasks 1-4.
- Produces: `internal/tui` non-test files whose ONLY store references left are `store.ResolveStorePath` + `store.Open` in `app.go`'s `NewModel` (removed by Task 6). Everything else is `core.*`.

- [ ] **Step 1: Enumerate actual references**

Run: `grep -rn 'store\.' internal/tui --include='*.go' | grep -v _test`
Every hit must be covered by the mapping in Step 2 (or be `app.go`'s `ResolveStorePath`/`Open`, which stay until Task 6). If a symbol appears that the mapping misses, it moved in Tasks 1-3 — use its `core.` name; if it did NOT move, stop and re-check against the spec's exclusion list before proceeding.

- [ ] **Step 2: Apply the symbol map**

Type/receiver changes:

| Old | New |
|---|---|
| `store *store.Store` field in `Model` (`app.go`) | `store core.Service` |
| `store *store.Store` field in `boardEditor` + `newBoardEditor(s *store.Store, ...)` (`board_editor.go`) | `core.Service` |
| `listTaskIDs(s *store.Store, ...)` (`projects.go`) | `listTaskIDs(s core.Service, ...)` |
| `store.Task`, `store.Label`, `store.Comment`, `store.Project` | `core.Task`, `core.Label`, `core.Comment`, `core.Project` |
| `store.QueryFilters`, `store.LogEntry`, `store.Subject`, `store.Pins`, `store.Vocabulary`, `store.VocabularyTerm`, `store.EmbeddingConfig`, `store.EmbedFunc`, `store.VectorMeta` | same name under `core.` |
| `store.Node`, `store.ParseExpr` | `core.Expr`, `core.ParseExpr` |
| `store.Now`, `store.RFC3339UTC`, `store.ParseCommentID`, `store.ValidatePersonaName`, `store.IsNamespaceName`, `store.IsConflict`, `store.IsIntegrity` | same name under `core.` |

Method calls (`m.store.GetTask(...)` etc.) are untouched — the receiver type changed, the method set didn't. After editing each file, remove its `"atm/internal/store"` import (except `app.go`) and add `"atm/internal/core"` where missing. `goimports`/compiler errors are the checklist.

- [ ] **Step 3: Verify**

Run: `go build ./... && go test ./internal/tui/`
Expected: PASS — test files still import store and pass `*store.Store` into models; it satisfies `core.Service`.
Run: `grep -rln '"atm/internal/store"' internal/tui --include='*.go' | grep -v _test`
Expected: exactly one line, `internal/tui/app.go`.
Run: `make verify` — Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/tui
git commit -m "refactor(ATM-b9d83a): TUI consumes core.Service and core types"
```

---

### Task 6: Composition root in cmd/atm; cli loses the tui import

**Files:**
- Modify: `internal/tui/run.go`, `internal/tui/app.go` (NewModel signature)
- Modify: `internal/cli/root.go`
- Modify: `cmd/atm/main.go`
- Modify (test call sites): `internal/tui/bench_lag_test.go`, `internal/tui/refresh_tick_test.go`, `internal/tui/labels_test.go`, `internal/tui/app_test.go` (+ any other test the compiler flags)

**Interfaces:**
- Consumes: `core.Service` (Task 4), the TUI flip (Task 5).
- Produces: `tui.Run(svc core.Service, actor string) error`; `tui.NewModelOpts{Service core.Service; Actor string}`; `cli.Deps{RunTUI func(storePath, actor string) error}`; `cli.Execute(deps cli.Deps) int`. After this task `internal/tui` non-test files have zero store imports and `internal/cli` has zero tui imports.

- [ ] **Step 1: Change the TUI entry points**

`internal/tui/run.go` — full new content:

```go
package tui

import (
	"atm/internal/core"

	"github.com/charmbracelet/bubbletea"
)

// Run launches the Bubble Tea TUI over an already-opened store, with the
// given free-form actor id. The composition root (cmd/atm) resolves the
// store path and opens the concrete store; Run auto-inits it if absent,
// builds the root Model, and runs the program until the user quits.
func Run(svc core.Service, actor string) error {
	m, err := NewModel(NewModelOpts{Service: svc, Actor: actor})
	if err != nil {
		return err
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}
```

`internal/tui/app.go` — replace `NewModelOpts` and the top of `NewModel`:

```go
// NewModelOpts are the inputs to NewModel.
type NewModelOpts struct {
	Service core.Service
	Actor   string
}

// NewModel builds the root Model over an opened store (auto-initing the
// store directory if absent) with all its sub-models initialized.
func NewModel(opts NewModelOpts) (*Model, error) {
	s := opts.Service
	if _, statErr := os.Stat(s.StorePath()); statErr != nil {
		if err := s.Init(""); err != nil {
			return nil, err
		}
	}
	// ... rest of NewModel unchanged (actor defaulting, theme, sub-models),
	// with the local variable s now a core.Service.
}
```

Delete the `store.ResolveStorePath`/`store.Open` lines and remove `"atm/internal/store"` from `app.go` imports.

- [ ] **Step 2: Inject the runner into cli**

`internal/cli/root.go`:
- Delete the `"atm/internal/tui"` import.
- Add the exported dependency struct next to the `tuiRunner` type:

```go
// Deps are the composition-root-provided dependencies (wired by cmd/atm).
// RunTUI launches the interactive TUI for the given store path and actor.
type Deps struct {
	RunTUI func(storePath, actor string) error
}
```

- In `launchTUI`, replace the default:

```go
	run := s.runTUI
	if run == nil {
		return fmt.Errorf("tui runner not wired (composition root must set Deps.RunTUI)")
	}
```

- Change `Execute` to accept the deps:

```go
func Execute(deps Deps) int {
	st := &cliState{runTUI: deps.RunTUI}
	root := newRootCmdWithState(st)
	// ... rest unchanged
}
```

- [ ] **Step 3: Wire the composition root**

`cmd/atm/main.go` — full new content:

```go
package main

import (
	"os"

	"atm/internal/cli"
	"atm/internal/store"
	"atm/internal/tui"
)

// main is the composition root: it constructs the concrete store and hands
// the adapters their dependencies. No domain or presentation logic here.
func main() {
	runTUI := func(storePath, actor string) error {
		root := store.ResolveStorePath(storePath)
		s, err := store.Open(root)
		if err != nil {
			return err
		}
		return tui.Run(s, actor)
	}
	os.Exit(cli.Execute(cli.Deps{RunTUI: runTUI}))
}
```

- [ ] **Step 4: Fix TUI test call sites**

The compiler lists them: every `NewModel(NewModelOpts{StorePath: s.StorePath(), Actor: ...})` becomes `NewModel(NewModelOpts{Service: s, Actor: ...})` — each of these tests already holds an open `*store.Store` named `s`; where one instead re-opens by path (e.g. `refresh_tick_test.go`'s second handle), keep the `store.Open` and pass the handle. Test files importing store is allowed (spec: the import rule binds production files).

- [ ] **Step 5: Verify the boundary and behavior**

Run: `grep -rln '"atm/internal/store"' internal/tui --include='*.go' | grep -v _test` — Expected: no output.
Run: `grep -rln '"atm/internal/tui"' internal/cli` — Expected: no output.
Run: `make verify` — Expected: PASS.
Smoke: `make build && ./bin/atm version && ./bin/atm task show --task ATM-b9d83a` — Expected: normal output (proves Execute wiring); TUI launch itself is covered by `app_test.go`.

- [ ] **Step 6: Commit**

```bash
git add internal/tui internal/cli cmd/atm
git commit -m "refactor(ATM-b9d83a): composition root in cmd/atm; cli drops tui import; tui.Run takes core.Service"
```

---

### Task 7: internal/version becomes a pure leaf

**Files:**
- Modify: `internal/version/formatters.go`

**Interfaces:**
- Produces: `version.EmitJSON(info map[string]any) string` — same behavior, no store dependency. Existing tests `TestEmitJSONKeyOrder` / `TestEmitJSONDeterministicContent` pin the bytes (sorted keys, two-space indent, no trailing newline).

- [ ] **Step 1: Run the pinning tests first**

Run: `go test ./internal/version/ -run TestEmitJSON -v`
Expected: PASS (they pass today; they are the contract for the rewrite).

- [ ] **Step 2: Rewrite EmitJSON without store**

In `internal/version/formatters.go`, drop `"atm/internal/store"` from imports, add `"encoding/json"`, and replace `EmitJSON`:

```go
// EmitJSON renders the deterministic JSON object: encoding/json marshals
// map keys in sorted order (arch, commit, date, os, version), two-space
// indent, no HTML escaping, no trailing newline — byte-identical to the
// store-backed marshaller this replaced.
func EmitJSON(info map[string]any) string {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(info); err != nil {
		return "{}\n"
	}
	return strings.TrimSuffix(buf.String(), "\n")
}
```

- [ ] **Step 3: Verify**

Run: `go test ./internal/version/ -v` — Expected: PASS, including the ldflags build test.
Run: `grep -rn "atm/internal" internal/version/*.go | grep -v _test` — Expected: no output.
Run: `make verify` — Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/version
git commit -m "refactor(ATM-b9d83a): version emits its own sorted JSON; store edge removed"
```

---

### Task 8: Import-boundary test

**Files:**
- Create: `tests/arch/imports_test.go`

**Interfaces:**
- Consumes: nothing from the codebase — it parses source files.
- Produces: CI enforcement of the import-rules table for the packages this step touched.

- [ ] **Step 1: Write the test**

Complete file:

```go
// Package arch enforces the import-rules table of
// docs/architecture/logical-components.md for the packages refactor step 4
// (ATM-b9d83a) put on their target boundaries. A change that violates one of
// these rules is wrong even if it compiles and every other test passes.
package arch

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// internalImports returns the "atm/internal/..." and "atm/libs/..." import
// paths of every non-test .go file in dir (relative to the repo root).
func internalImports(t *testing.T, dir string) map[string][]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no .go files under %s — directory moved?", dir)
	}
	out := map[string][]string{}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, imp := range src.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if strings.HasPrefix(p, "atm/internal/") || strings.HasPrefix(p, "atm/libs/") {
				out[f] = append(out[f], p)
			}
		}
	}
	return out
}

func TestCoreIsAPureLeaf(t *testing.T) {
	for f, imps := range internalImports(t, "internal/core") {
		t.Errorf("%s imports %v; internal/core may import nothing from this repository", f, imps)
	}
}

func TestVersionImportsNoInternalPackage(t *testing.T) {
	for f, imps := range internalImports(t, "internal/version") {
		t.Errorf("%s imports %v; internal/version must be a pure leaf", f, imps)
	}
}

func TestTUIImportsOnlyCore(t *testing.T) {
	for f, imps := range internalImports(t, "internal/tui") {
		for _, p := range imps {
			if p != "atm/internal/core" {
				t.Errorf("%s imports %q; internal/tui production files may import only atm/internal/core", f, p)
			}
		}
	}
}

func TestCLIDoesNotImportTUI(t *testing.T) {
	for f, imps := range internalImports(t, "internal/cli") {
		for _, p := range imps {
			if p == "atm/internal/tui" {
				t.Errorf("%s imports the tui package; the runner seam (Deps.RunTUI) is the only allowed edge", f)
			}
		}
	}
}
```

- [ ] **Step 2: Run it — must pass on the finished tree**

Run: `go test ./tests/arch/ -v`
Expected: PASS (4 tests).

- [ ] **Step 3: Prove it bites**

Temporarily add `_ "atm/internal/store"` to `internal/tui/styles.go` imports, run `go test ./tests/arch/ -run TestTUIImportsOnlyCore` — Expected: FAIL naming `styles.go`. Revert the edit (`git checkout -- internal/tui/styles.go`) and re-run — Expected: PASS.

- [ ] **Step 4: Full verify and commit**

Run: `make verify` — Expected: PASS (the new package is inside `./...`).

```bash
git add tests/arch
git commit -m "test(ATM-b9d83a): enforce the import-rules table for core, tui, cli, version"
```

---

### Task 9: Ledger close-out and integration

**Files:**
- None in the repo beyond what earlier tasks committed (spec/plan already committed). ATM ledger updates only.

- [ ] **Step 1: Full gate on the final tree**

Run: `make verify`
Expected: PASS. Also re-run the boundary greps from Task 6 Step 5 — all clean.

- [ ] **Step 2: Acceptance-criteria checklist against ATM-b9d83a**

- Build and tests green — from Step 1.
- `internal/tui` production imports only core — `tests/arch` proves it.
- `internal/version` imports no internal package — `tests/arch` proves it.
- Import-rules table holds — `tests/arch` covers tui/cli/version/core rows.

- [ ] **Step 3: Record completion in the ledger**

```bash
atm task comment add --task ATM-b9d83a --actor "developer@claude:fable-5" --label "ATM:comment:progress" \
  --body "Step 4 implemented: <commit range>. Domain types + pure helpers moved to core behind store aliases; core.Service role interfaces (61 methods, storage-format admin excluded by design) satisfied structurally by *store.Store; TUI on core.Service only; composition root in cmd/atm (cli.Deps{RunTUI}); version is a pure leaf; tests/arch enforces the import table. make verify green."
atm task comment add --task ATM-9eb7dc --actor "developer@claude:fable-5" --label "ATM:comment:progress" \
  --body "Step 4 of 6 (ATM-b9d83a) done — see task for details. Step 5 (ATM-08db6e, capability registry) and step 6 (ATM-3b873c, write-engine carve) are now unblocked."
```

Set status: `atm task label add --task ATM-b9d83a --actor "developer@claude:fable-5" --label "ATM:status:done"` and remove `ATM:status:in-progress` (same flags as add, `label remove`).

- [ ] **Step 4: Integration**

Use the superpowers:finishing-a-development-branch skill to merge/close out the working branch (the executor session decides branch mechanics; work happens on a worktree branch per superpowers:using-git-worktrees).
