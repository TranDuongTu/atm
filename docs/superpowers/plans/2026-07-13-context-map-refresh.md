# Living Context Map Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `atm context` — a capability command that records where each `context:*` pointer came from, reports which pointers have drifted from reality, and lets the manager refresh the map cheaply enough to do it every session.

**Architecture:** A new `internal/contextmap` package holds all the logic; `internal/cli/context.go` is a thin cobra wrapper. Pointers record kinded, witnessed sources in a `comment:provenance` body that only this package parses. Four recorder verbs mutate (`add`, `stamp`, `retarget`, `supersede`); one reporter verb (`check`) is strictly read-only. The capability ensures its own labels and the `context-current` board on first use, so it needs no seeding. **No change to the stable store API** — `LabelSeed`, `CreateComment`, `ListComments`, `TaskLabelAdd`, `SetDescription` already cover every write.

**Tech Stack:** Go 1.25, cobra, existing `internal/store` API. Git is invoked by shelling out to `git` (`os/exec`), matching `internal/cli/launcher_shared.go`; **no new module dependencies.**

## Global Constraints

- Go 1.25.0 (`go.mod`). No new dependencies in `go.mod` — git via `os/exec`, HTTP via `net/http`.
- The package is named `contextmap`, **never** `context` — that shadows the stdlib package and will make every file that imports both unreadable.
- **No emojis in code or commit messages** (AGENTS.md).
- Verify gate before declaring any task done: `make verify` (= `make build && make test`). AGENTS.md requires it.
- JSON output must stay deterministic: sorted keys, RFC3339 UTC timestamps (`internal/cli/determinism_test.go` enforces this repo-wide).
- Mutating CLI commands require `--actor` or `ATM_ACTOR`; use `st.resolveActor(true)`. Read-only commands use `st.resolveActor(false)`.
- Exit codes: 0 ok, 1 generic, 2 usage (`ErrUsage`), 3 not-found, 4 conflict.
- **`check` must never mutate the store.** This is a design invariant, not a preference, and Task 8 tests it byte-for-byte.
- All new labels created by this capability carry a description. A label with no description is a defect in this codebase (it surfaces as a warning in the Boards pane).

---

### Task 1: Kinded source locators

A **source** is a kinded locator: `git:internal/store`, `file:/etc/hosts`, `url:https://x.dev/a`, `external:jira/ATM-441`. This task is the pure parse/format layer everything else builds on.

**Files:**
- Create: `internal/contextmap/source.go`
- Test: `internal/contextmap/source_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Kind string` with consts `KindGit = "git"`, `KindFile = "file"`, `KindURL = "url"`, `KindExternal = "external"`
  - `type Source struct { Kind Kind; Locator string }`
  - `func ParseSource(s string) (Source, error)`
  - `func (s Source) String() string`
  - `func (s Source) Provable() bool` — true for git/file/url, false for external

- [ ] **Step 1: Write the failing test**

```go
package contextmap

import "testing"

func TestParseSource(t *testing.T) {
	tests := []struct {
		in      string
		want    Source
		wantErr bool
	}{
		{in: "git:internal/store", want: Source{Kind: KindGit, Locator: "internal/store"}},
		{in: "file:/etc/hosts", want: Source{Kind: KindFile, Locator: "/etc/hosts"}},
		{in: "url:https://go.dev/doc", want: Source{Kind: KindURL, Locator: "https://go.dev/doc"}},
		{in: "external:jira/ATM-441", want: Source{Kind: KindExternal, Locator: "jira/ATM-441"}},
		{in: "internal/store", wantErr: true},  // no kind prefix
		{in: "svn:trunk", wantErr: true},       // unknown kind
		{in: "git:", wantErr: true},            // empty locator
		{in: "", wantErr: true},
	}
	for _, tt := range tests {
		got, err := ParseSource(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseSource(%q): want error, got %v", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParseSource(%q): %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("ParseSource(%q) = %+v, want %+v", tt.in, got, tt.want)
		}
		if rt := got.String(); rt != tt.in {
			t.Errorf("round-trip: String() = %q, want %q", rt, tt.in)
		}
	}
}

func TestSourceProvable(t *testing.T) {
	for _, tt := range []struct {
		src  Source
		want bool
	}{
		{Source{Kind: KindGit, Locator: "a"}, true},
		{Source{Kind: KindFile, Locator: "a"}, true},
		{Source{Kind: KindURL, Locator: "a"}, true},
		{Source{Kind: KindExternal, Locator: "a"}, false},
	} {
		if got := tt.src.Provable(); got != tt.want {
			t.Errorf("%v.Provable() = %v, want %v", tt.src, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/contextmap/ -run TestParseSource -v`
Expected: FAIL — build error, `undefined: ParseSource`.

- [ ] **Step 3: Write minimal implementation**

```go
// Package contextmap implements the atm context capability: it records where
// each context:* pointer came from, and reports which pointers have drifted
// from reality. It owns its slice of the label substrate -- the context kinds,
// the knowledge lifecycle namespace, the provenance comment kind, and the
// context-current board -- and ensures that vocabulary exists before using it.
//
// See docs/architecture/label-substrate-and-capabilities.md for the pattern,
// and docs/superpowers/specs/2026-07-13-context-map-refresh-design.md.
package contextmap

import (
	"fmt"
	"strings"
)

// Kind classifies a source by how -- and whether -- it can be witnessed.
type Kind string

const (
	KindGit      Kind = "git"      // path in the repo; witnessed by git object id
	KindFile     Kind = "file"     // path outside the repo; witnessed by content hash
	KindURL      Kind = "url"      // fetched over HTTP; witnessed by body hash
	KindExternal Kind = "external" // Jira, Notion, ...; NOT witnessable, aged only
)

// Source is a kinded locator: the thing a context pointer was derived from.
type Source struct {
	Kind    Kind
	Locator string
}

// Provable reports whether this kind of source can be witnessed locally. When
// false, drift is undetectable and check reports age instead. ATM speaks no
// third-party API, so external sources are never provable.
func (s Source) Provable() bool { return s.Kind != KindExternal }

func (s Source) String() string { return string(s.Kind) + ":" + s.Locator }

// ParseSource parses a kinded locator such as "git:internal/store".
func ParseSource(s string) (Source, error) {
	kindStr, locator, ok := strings.Cut(s, ":")
	if !ok || locator == "" {
		return Source{}, fmt.Errorf("source %q: want <kind>:<locator>, e.g. git:internal/store", s)
	}
	kind := Kind(kindStr)
	switch kind {
	case KindGit, KindFile, KindURL, KindExternal:
	default:
		return Source{}, fmt.Errorf("source %q: unknown kind %q (want git, file, url, or external)", s, kindStr)
	}
	return Source{Kind: kind, Locator: locator}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/contextmap/ -v`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
git add internal/contextmap/source.go internal/contextmap/source_test.go
git commit -m "feat(context): kinded source locators"
```

---

### Task 2: Provenance stamp encoding

The provenance stamp is the capability's private format. It lives in a comment body and **nothing outside this package ever parses it** — that privacy is what lets the format change later without touching a prompt.

**Files:**
- Create: `internal/contextmap/provenance.go`
- Test: `internal/contextmap/provenance_test.go`

**Interfaces:**
- Consumes: `Source`, `ParseSource` (Task 1).
- Produces:
  - `type Witness struct { Source Source; Value string }` — `Value` is the recorded witness (git object id, content hash, ETag, external version token, or `""` when none)
  - `type Stamp struct { Version int; At time.Time; Head string; Witnesses []Witness }` — `Head` is the repo HEAD commit at stamp time (`""` when not in a repo)
  - `func MarshalStamp(s Stamp) (string, error)`
  - `func UnmarshalStamp(body string) (Stamp, error)`
  - `const StampVersion = 1`

- [ ] **Step 1: Write the failing test**

```go
package contextmap

import (
	"testing"
	"time"
)

func TestStampRoundTrip(t *testing.T) {
	want := Stamp{
		Version: StampVersion,
		At:      time.Date(2026, 7, 13, 9, 30, 0, 0, time.UTC),
		Head:    "d1f8cc4",
		Witnesses: []Witness{
			{Source: Source{Kind: KindGit, Locator: "internal/store"}, Value: "a3f9b1"},
			{Source: Source{Kind: KindExternal, Locator: "jira/ATM-441"}, Value: ""},
		},
	}
	body, err := MarshalStamp(want)
	if err != nil {
		t.Fatalf("MarshalStamp: %v", err)
	}
	got, err := UnmarshalStamp(body)
	if err != nil {
		t.Fatalf("UnmarshalStamp: %v", err)
	}
	if !got.At.Equal(want.At) || got.Head != want.Head || got.Version != want.Version {
		t.Errorf("scalar mismatch: got %+v, want %+v", got, want)
	}
	if len(got.Witnesses) != len(want.Witnesses) {
		t.Fatalf("witnesses: got %d, want %d", len(got.Witnesses), len(want.Witnesses))
	}
	for i := range want.Witnesses {
		if got.Witnesses[i] != want.Witnesses[i] {
			t.Errorf("witness %d: got %+v, want %+v", i, got.Witnesses[i], want.Witnesses[i])
		}
	}
}

func TestUnmarshalStampRejectsGarbage(t *testing.T) {
	// A human hand-wrote prose into a provenance comment. Report it as
	// unreadable; never panic, never "repair" it.
	for _, body := range []string{"", "not json", `{"v":99,"sources":[]}`} {
		if _, err := UnmarshalStamp(body); err == nil {
			t.Errorf("UnmarshalStamp(%q): want error, got nil", body)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/contextmap/ -run TestStamp -v`
Expected: FAIL — `undefined: Stamp`.

- [ ] **Step 3: Write minimal implementation**

```go
package contextmap

import (
	"encoding/json"
	"fmt"
	"time"
)

// StampVersion is the provenance body schema version. Because the body is
// written and read only by this package, bumping it requires no change to any
// prompt, skill, or agent.
const StampVersion = 1

// Witness is a source plus the evidence recorded for it at stamp time. Value
// is empty when the source is not provable (external) or the agent supplied
// no version token.
type Witness struct {
	Source Source
	Value  string
}

// Stamp is one provenance record: what a pointer was derived from, and the
// evidence for each source, at a moment in time.
type Stamp struct {
	Version   int
	At        time.Time
	Head      string // repo HEAD commit at stamp time; empty when not in a repo
	Witnesses []Witness
}

// wire is the on-disk shape. Kept separate from Stamp so the exported type can
// evolve independently of the serialized format.
type wire struct {
	V       int          `json:"v"`
	At      time.Time    `json:"at"`
	Head    string       `json:"head,omitempty"`
	Sources []wireSource `json:"sources"`
}

type wireSource struct {
	Source  string `json:"source"`            // kinded locator, e.g. "git:internal/store"
	Witness string `json:"witness,omitempty"` // empty for unprovable sources
}

func MarshalStamp(s Stamp) (string, error) {
	w := wire{V: StampVersion, At: s.At.UTC(), Head: s.Head}
	w.Sources = make([]wireSource, 0, len(s.Witnesses))
	for _, wit := range s.Witnesses {
		w.Sources = append(w.Sources, wireSource{Source: wit.Source.String(), Witness: wit.Value})
	}
	b, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal provenance: %w", err)
	}
	return string(b), nil
}

func UnmarshalStamp(body string) (Stamp, error) {
	var w wire
	if err := json.Unmarshal([]byte(body), &w); err != nil {
		return Stamp{}, fmt.Errorf("parse provenance: %w", err)
	}
	if w.V != StampVersion {
		return Stamp{}, fmt.Errorf("parse provenance: unsupported version %d (want %d)", w.V, StampVersion)
	}
	s := Stamp{Version: w.V, At: w.At, Head: w.Head}
	s.Witnesses = make([]Witness, 0, len(w.Sources))
	for _, ws := range w.Sources {
		src, err := ParseSource(ws.Source)
		if err != nil {
			return Stamp{}, fmt.Errorf("parse provenance: %w", err)
		}
		s.Witnesses = append(s.Witnesses, Witness{Source: src, Value: ws.Witness})
	}
	return s, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/contextmap/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/contextmap/provenance.go internal/contextmap/provenance_test.go
git commit -m "feat(context): provenance stamp encoding"
```

---

### Task 3: Vocabulary bootstrap

The capability ensures its own labels and its board exist before using them. This is what removes the seeding dependency: every verb works in a project created five minutes ago, or one whose human curated the labels differently.

**Files:**
- Create: `internal/contextmap/vocabulary.go`
- Test: `internal/contextmap/vocabulary_test.go`

**Interfaces:**
- Consumes: `store.Store` (`LabelSeed(name, description, expr, actor string) error` — already idempotent: it upserts only when the label is absent, preserving existing descriptions).
- Produces:
  - `func EnsureVocabulary(s *store.Store, code, actor string) error`
  - `func LabelSuperseded(code string) string` → `"<CODE>:knowledge:superseded"`
  - `func LabelProvenance(code string) string` → `"<CODE>:comment:provenance"`
  - `func LabelContextKind(code, kind string) string` → `"<CODE>:context:<kind>"`
  - `func BoardCurrent(code string) string` → `"<CODE>:context-current"`

- [ ] **Step 1: Write the failing test**

```go
package contextmap

import (
	"testing"

	"atm/internal/store"
)

// newTestStore opens a store in a temp dir with one project. Mirrors the
// existing helpers in internal/store/*_test.go.
func newTestStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	const actor = "manager@claude:opus-4.8"
	if _, err := s.CreateProject("TST", "Test", actor); err != nil {
		t.Fatalf("create project: %v", err)
	}
	return s, actor
}

func TestEnsureVocabularyCreatesLabelsAndBoard(t *testing.T) {
	s, actor := newTestStore(t)
	if err := EnsureVocabulary(s, "TST", actor); err != nil {
		t.Fatalf("EnsureVocabulary: %v", err)
	}

	// Every label this capability owns must exist AND carry a description --
	// a label without one is a defect that warns in the Boards pane.
	for _, name := range []string{
		LabelSuperseded("TST"),
		LabelProvenance("TST"),
		LabelContextKind("TST", "documentation"),
	} {
		l, err := s.LabelShow(name)
		if err != nil {
			t.Fatalf("LabelShow(%q): %v", name, err)
		}
		if l.Description == "" {
			t.Errorf("label %q has no description", name)
		}
	}

	board, err := s.LabelShow(BoardCurrent("TST"))
	if err != nil {
		t.Fatalf("LabelShow(board): %v", err)
	}
	if board.Expr == "" {
		t.Error("context-current must be a board (Expr set), got a stored label")
	}
	if board.Description == "" {
		t.Error("board has no description")
	}
}

func TestEnsureVocabularyIsIdempotent(t *testing.T) {
	s, actor := newTestStore(t)
	if err := EnsureVocabulary(s, "TST", actor); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := EnsureVocabulary(s, "TST", actor); err != nil {
		t.Fatalf("second: %v", err)
	}
}

func TestEnsureVocabularyPreservesHumanDescription(t *testing.T) {
	// A human curated the description. The capability must not clobber it:
	// paved road, not fence.
	s, actor := newTestStore(t)
	name := LabelSuperseded("TST")
	if err := s.LabelAdd(name, "my own wording", "", actor); err != nil {
		t.Fatalf("LabelAdd: %v", err)
	}
	if err := EnsureVocabulary(s, "TST", actor); err != nil {
		t.Fatalf("EnsureVocabulary: %v", err)
	}
	l, err := s.LabelShow(name)
	if err != nil {
		t.Fatalf("LabelShow: %v", err)
	}
	if l.Description != "my own wording" {
		t.Errorf("description clobbered: got %q", l.Description)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/contextmap/ -run TestEnsureVocabulary -v`
Expected: FAIL — `undefined: EnsureVocabulary`.

- [ ] **Step 3: Write minimal implementation**

```go
package contextmap

import "atm/internal/store"

// ContextKinds are the pointer kinds this capability recognizes. They match the
// seeded context:* labels, but EnsureVocabulary does not assume seeding ran.
var ContextKinds = []string{"agent", "repository", "documentation", "question"}

func LabelSuperseded(code string) string { return code + ":knowledge:superseded" }
func LabelProvenance(code string) string { return code + ":comment:provenance" }
func LabelContextKind(code, kind string) string {
	return code + ":context:" + kind
}
func BoardCurrent(code string) string { return code + ":context-current" }

// currentExpr computes membership of the context-current board: every context
// pointer that has not been superseded. Absence of the lifecycle label means
// current, so a human hand-writing a context task need not know the namespace
// exists.
func currentExpr() string { return "context:* AND NOT knowledge:superseded" }

// EnsureVocabulary creates the labels and the board this capability uses, with
// descriptions, if they are absent. Idempotent, and it never overwrites a
// description a human already curated (store.LabelSeed upserts only when the
// label is absent).
//
// This is what makes the capability self-bootstrapping: it works in any
// project, whether or not `atm label seed` ever ran.
func EnsureVocabulary(s *store.Store, code, actor string) error {
	type lbl struct{ name, desc, expr string }
	want := []lbl{
		{code + ":knowledge:*", "lifecycle of a piece of recorded knowledge; absence means current", ""},
		{LabelSuperseded(code), "this context pointer is obsolete; its successor is named in the description. Kept for history -- it retains its kind, narrative, and provenance stamps. Applied by `atm context supersede`.", ""},
		{LabelProvenance(code), "task comment kind: a machine-written provenance stamp recording what a context pointer was derived from, and the evidence, at a moment in time. Written and read only by `atm context` -- do not hand-edit.", ""},
		{BoardCurrent(code), "every context pointer that has not been superseded: the project's current knowledge. Agents read this board rather than the raw context:* namespace, so a query always returns the latest.", currentExpr()},
	}
	for _, kind := range ContextKinds {
		want = append(want, lbl{LabelContextKind(code, kind), "context pointer kind: " + kind, ""})
	}
	for _, l := range want {
		if err := s.LabelSeed(l.name, l.desc, l.expr, actor); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/contextmap/ -v`
Expected: PASS (all three vocabulary tests).

- [ ] **Step 5: Commit**

```bash
git add internal/contextmap/vocabulary.go internal/contextmap/vocabulary_test.go
git commit -m "feat(context): self-bootstrapping vocabulary and context-current board"
```

---

### Task 4: Git and file witnesses

`git rev-parse HEAD:<path>` returns the object id of a blob **or a tree**, so one call witnesses a file and a directory alike, and it changes exactly when the content under that path changes. That is the whole drift mechanism.

**Files:**
- Create: `internal/contextmap/witness.go`
- Test: `internal/contextmap/witness_test.go`

**Interfaces:**
- Consumes: `Source`, `Kind` (Task 1).
- Produces:
  - `type Verdict string` with consts `VerdictOK`, `VerdictDrift`, `VerdictGone`, `VerdictSkipped`, `VerdictUnwitnessable`
  - `type Resolver struct { Repo string; HTTP *http.Client }` — `Repo` is the repo root; a zero `HTTP` disables URL fetching
  - `func (r *Resolver) Witness(src Source) (value string, err error)` — current evidence for a source
  - `func (r *Resolver) Compare(src Source, recorded string) (Verdict, error)`
  - `func (r *Resolver) Head() (string, error)` — current HEAD commit, `""` outside a repo
  - `func (r *Resolver) ChangedSince(rev string) ([]string, error)` — repo-relative paths changed between `rev` and HEAD

- [ ] **Step 1: Write the failing test**

```go
package contextmap

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// newTestRepo makes a git repo with one committed file and returns its root.
func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg", "a.go"), []byte("package pkg\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-qm", "init")
	return dir
}

func commitFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-qm", "change"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestGitWitnessUnchangedIsOK(t *testing.T) {
	repo := newTestRepo(t)
	r := &Resolver{Repo: repo}
	src := Source{Kind: KindGit, Locator: "pkg"}

	recorded, err := r.Witness(src)
	if err != nil {
		t.Fatalf("Witness: %v", err)
	}
	if recorded == "" {
		t.Fatal("Witness returned empty object id")
	}
	got, err := r.Compare(src, recorded)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if got != VerdictOK {
		t.Errorf("unchanged path: got %v, want %v", got, VerdictOK)
	}
}

func TestGitWitnessChangedIsDrift(t *testing.T) {
	repo := newTestRepo(t)
	r := &Resolver{Repo: repo}
	src := Source{Kind: KindGit, Locator: "pkg"}
	recorded, err := r.Witness(src)
	if err != nil {
		t.Fatalf("Witness: %v", err)
	}

	commitFile(t, repo, "pkg/a.go", "package pkg\n\nfunc New() {}\n")

	got, err := r.Compare(src, recorded)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if got != VerdictDrift {
		t.Errorf("changed path: got %v, want %v", got, VerdictDrift)
	}
}

func TestGitWitnessDeletedIsGone(t *testing.T) {
	repo := newTestRepo(t)
	r := &Resolver{Repo: repo}
	src := Source{Kind: KindGit, Locator: "pkg"}
	recorded, err := r.Witness(src)
	if err != nil {
		t.Fatalf("Witness: %v", err)
	}

	if err := os.RemoveAll(filepath.Join(repo, "pkg")); err != nil {
		t.Fatal(err)
	}
	commitFile(t, repo, "other.txt", "x")

	got, err := r.Compare(src, recorded)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if got != VerdictGone {
		t.Errorf("deleted path: got %v, want %v", got, VerdictGone)
	}
}

func TestFileWitness(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &Resolver{}
	src := Source{Kind: KindFile, Locator: path}

	recorded, err := r.Witness(src)
	if err != nil {
		t.Fatalf("Witness: %v", err)
	}
	if v, err := r.Compare(src, recorded); err != nil || v != VerdictOK {
		t.Fatalf("unchanged file: got %v, %v; want OK", v, err)
	}

	if err := os.WriteFile(path, []byte("goodbye"), 0o644); err != nil {
		t.Fatal(err)
	}
	if v, err := r.Compare(src, recorded); err != nil || v != VerdictDrift {
		t.Fatalf("changed file: got %v, %v; want DRIFT", v, err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if v, err := r.Compare(src, recorded); err != nil || v != VerdictGone {
		t.Fatalf("deleted file: got %v, %v; want GONE", v, err)
	}
}

func TestChangedSince(t *testing.T) {
	repo := newTestRepo(t)
	r := &Resolver{Repo: repo}
	head, err := r.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}

	commitFile(t, repo, "new.txt", "x")

	changed, err := r.ChangedSince(head)
	if err != nil {
		t.Fatalf("ChangedSince: %v", err)
	}
	if len(changed) != 1 || changed[0] != "new.txt" {
		t.Errorf("ChangedSince = %v, want [new.txt]", changed)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/contextmap/ -run 'Witness|ChangedSince' -v`
Expected: FAIL — `undefined: Resolver`.

- [ ] **Step 3: Write minimal implementation**

```go
package contextmap

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

// Verdict is what check can honestly say about one source.
type Verdict string

const (
	VerdictOK            Verdict = "OK"      // witnessed, and unchanged
	VerdictDrift         Verdict = "DRIFT"   // witnessed, and the content changed
	VerdictGone          Verdict = "GONE"    // the subject moved or was deleted
	VerdictSkipped       Verdict = "SKIPPED" // could not witness right now (offline)
	VerdictUnwitnessable Verdict = "AGE"     // external: drift is undetectable; age is all we have
)

// Resolver witnesses sources against the real world. Repo is the git repo root;
// HTTP, when nil, disables URL fetching so check degrades to SKIPPED rather
// than failing.
type Resolver struct {
	Repo string
	HTTP *http.Client
}

// errGone marks a subject that is no longer there.
var errGone = errors.New("subject gone")

// Witness returns the current evidence for a source: a git object id, a content
// hash, or an HTTP body hash. External sources have no local witness and return
// "" -- their freshness is judged by age alone.
func (r *Resolver) Witness(src Source) (string, error) {
	switch src.Kind {
	case KindGit:
		return r.gitObject(src.Locator)
	case KindFile:
		b, err := os.ReadFile(src.Locator)
		if err != nil {
			if os.IsNotExist(err) {
				return "", errGone
			}
			return "", fmt.Errorf("read %s: %w", src.Locator, err)
		}
		return hashBytes(b), nil
	case KindURL:
		if r.HTTP == nil {
			return "", nil // caller maps this to SKIPPED
		}
		resp, err := r.HTTP.Get(src.Locator)
		if err != nil {
			return "", nil // offline: SKIPPED, not a failure
		}
		defer resp.Body.Close()
		if etag := resp.Header.Get("ETag"); etag != "" {
			return etag, nil
		}
		var sb strings.Builder
		h := sha256.New()
		if _, err := copyTo(h, resp.Body); err != nil {
			return "", nil
		}
		sb.WriteString(hex.EncodeToString(h.Sum(nil)))
		return sb.String(), nil
	case KindExternal:
		return "", nil
	}
	return "", fmt.Errorf("witness: unknown kind %q", src.Kind)
}

// Compare witnesses a source now and judges it against what was recorded.
func (r *Resolver) Compare(src Source, recorded string) (Verdict, error) {
	if !src.Provable() {
		return VerdictUnwitnessable, nil
	}
	now, err := r.Witness(src)
	if err != nil {
		if errors.Is(err, errGone) {
			return VerdictGone, nil
		}
		return "", err
	}
	if now == "" {
		return VerdictSkipped, nil // could not witness (e.g. offline URL)
	}
	if now == recorded {
		return VerdictOK, nil
	}
	return VerdictDrift, nil
}

// Head returns the current HEAD commit, or "" outside a git repo.
func (r *Resolver) Head() (string, error) {
	out, err := r.git("rev-parse", "HEAD")
	if err != nil {
		return "", nil
	}
	return out, nil
}

// ChangedSince lists repo-relative paths that changed between rev and HEAD.
func (r *Resolver) ChangedSince(rev string) ([]string, error) {
	if rev == "" {
		return nil, nil
	}
	out, err := r.git("diff", "--name-only", rev+"..HEAD")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// gitObject returns the object id of the blob or tree at path in HEAD. One call
// witnesses a file and a directory alike: the id changes exactly when the
// content under that path changes.
func (r *Resolver) gitObject(path string) (string, error) {
	out, err := r.git("rev-parse", "HEAD:"+path)
	if err != nil {
		return "", errGone // git exits non-zero when the path is not in HEAD
	}
	return out, nil
}

func (r *Resolver) git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if r.Repo != "" {
		cmd.Dir = r.Repo
	}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
```

Add the small `copyTo` helper in the same file (kept separate so the `io` import is obvious):

```go
// copyTo is io.Copy, named locally to keep the import list small and explicit.
func copyTo(dst io.Writer, src io.Reader) (int64, error) { return io.Copy(dst, src) }
```

...and add `"io"` to the import block.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/contextmap/ -v`
Expected: PASS (all five witness tests, plus Tasks 1-3).

- [ ] **Step 5: Commit**

```bash
git add internal/contextmap/witness.go internal/contextmap/witness_test.go
git commit -m "feat(context): git, file, and url witnesses"
```

---

### Task 5: Recorder verbs (add, stamp, retarget, supersede)

The four mutating verbs. Each ensures the vocabulary first, so none of them needs a seeded project.

**Files:**
- Create: `internal/contextmap/recorder.go`
- Test: `internal/contextmap/recorder_test.go`

**Interfaces:**
- Consumes: `EnsureVocabulary`, `LabelContextKind`, `LabelProvenance`, `LabelSuperseded` (Task 3); `Resolver`, `Witness` (Task 4); `MarshalStamp` (Task 2).
- Produces:
  - `type Recorder struct { Store *store.Store; Resolver *Resolver; Actor string }`
  - `func (rec *Recorder) Add(taskID, kind string, sources []Source) error`
  - `func (rec *Recorder) Stamp(taskID string) error` — re-witness the sources on the task's newest stamp
  - `func (rec *Recorder) Retarget(taskID string, sources []Source) error`
  - `func (rec *Recorder) Supersede(taskID, byID, reason string) error`
  - `func LatestStamp(s *store.Store, taskID, code string) (Stamp, bool, error)` — newest provenance comment; `false` when the pointer was never stamped

- [ ] **Step 1: Write the failing test**

```go
package contextmap

import (
	"strings"
	"testing"

	"atm/internal/store"
)

func newRecorder(t *testing.T, repo string) (*Recorder, *store.Store, string) {
	t.Helper()
	s, actor := newTestStore(t)
	return &Recorder{Store: s, Resolver: &Resolver{Repo: repo}, Actor: actor}, s, actor
}

func TestAddStampsAndLabels(t *testing.T) {
	repo := newTestRepo(t)
	rec, s, actor := newRecorder(t, repo)
	task, err := s.CreateTask("TST", "Code pointer: pkg", "", nil, actor)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	src := Source{Kind: KindGit, Locator: "pkg"}
	if err := rec.Add(task.ID, "documentation", []Source{src}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, err := s.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if !hasLabel(got.Labels, LabelContextKind("TST", "documentation")) {
		t.Errorf("kind label not applied: %v", got.Labels)
	}

	stamp, ok, err := LatestStamp(s, task.ID, "TST")
	if err != nil || !ok {
		t.Fatalf("LatestStamp: ok=%v err=%v", ok, err)
	}
	if len(stamp.Witnesses) != 1 || stamp.Witnesses[0].Source != src {
		t.Fatalf("stamp sources = %+v, want [%v]", stamp.Witnesses, src)
	}
	if stamp.Witnesses[0].Value == "" {
		t.Error("git source recorded with no witness")
	}
	if stamp.Head == "" {
		t.Error("stamp recorded no HEAD")
	}
}

func TestStampAppendsRatherThanReplaces(t *testing.T) {
	// Freshness history is the point: each re-stamp leaves the previous one
	// behind, so the thread records every revision at which this was verified.
	repo := newTestRepo(t)
	rec, s, actor := newRecorder(t, repo)
	task, _ := s.CreateTask("TST", "Code pointer: pkg", "", nil, actor)
	if err := rec.Add(task.ID, "documentation", []Source{{Kind: KindGit, Locator: "pkg"}}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitFile(t, repo, "pkg/a.go", "package pkg\n\nfunc New() {}\n")
	if err := rec.Stamp(task.ID); err != nil {
		t.Fatalf("Stamp: %v", err)
	}

	comments, err := s.ListComments(task.ID)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	n := 0
	for _, c := range comments {
		if hasLabel(c.Labels, LabelProvenance("TST")) {
			n++
		}
	}
	if n != 2 {
		t.Errorf("provenance comments = %d, want 2 (add + stamp)", n)
	}
}

func TestRetargetKeepsTaskAndRecordsNewSources(t *testing.T) {
	repo := newTestRepo(t)
	rec, s, actor := newRecorder(t, repo)
	task, _ := s.CreateTask("TST", "Code pointer: pkg", "", nil, actor)
	if err := rec.Add(task.ID, "documentation", []Source{{Kind: KindGit, Locator: "pkg"}}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	commitFile(t, repo, "moved.go", "package moved\n")
	newSrc := Source{Kind: KindGit, Locator: "moved.go"}
	if err := rec.Retarget(task.ID, []Source{newSrc}); err != nil {
		t.Fatalf("Retarget: %v", err)
	}

	stamp, ok, err := LatestStamp(s, task.ID, "TST")
	if err != nil || !ok {
		t.Fatalf("LatestStamp: ok=%v err=%v", ok, err)
	}
	if len(stamp.Witnesses) != 1 || stamp.Witnesses[0].Source != newSrc {
		t.Errorf("sources = %+v, want [%v]", stamp.Witnesses, newSrc)
	}
	if _, err := s.GetTask(task.ID); err != nil {
		t.Errorf("task must survive a retarget: %v", err)
	}
}

func TestSupersedeLabelsOldAndKeepsHistory(t *testing.T) {
	repo := newTestRepo(t)
	rec, s, actor := newRecorder(t, repo)
	old, _ := s.CreateTask("TST", "Doc pointer: old", "the old thing", nil, actor)
	replacement, _ := s.CreateTask("TST", "Doc pointer: new", "", nil, actor)
	if err := rec.Add(old.ID, "documentation", []Source{{Kind: KindGit, Locator: "pkg"}}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := rec.Supersede(old.ID, replacement.ID, "renderer moved"); err != nil {
		t.Fatalf("Supersede: %v", err)
	}

	got, err := s.GetTask(old.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if !hasLabel(got.Labels, LabelSuperseded("TST")) {
		t.Errorf("superseded label not applied: %v", got.Labels)
	}
	// It keeps its KIND -- lifecycle composes with kind, it does not replace it.
	if !hasLabel(got.Labels, LabelContextKind("TST", "documentation")) {
		t.Errorf("kind label was removed: %v", got.Labels)
	}
	if !strings.Contains(got.Description, replacement.ID) {
		t.Errorf("description does not name the successor: %q", got.Description)
	}
	if !strings.Contains(got.Description, "the old thing") {
		t.Errorf("original narrative was destroyed: %q", got.Description)
	}
}

// TestSupersededTaskLeavesCurrentBoard proves the "CLI returns the latest"
// requirement: it is a board, and it needs no code of its own.
func TestSupersededTaskLeavesCurrentBoard(t *testing.T) {
	repo := newTestRepo(t)
	rec, s, actor := newRecorder(t, repo)
	keep, _ := s.CreateTask("TST", "Doc pointer: keep", "", nil, actor)
	drop, _ := s.CreateTask("TST", "Doc pointer: drop", "", nil, actor)
	replacement, _ := s.CreateTask("TST", "Doc pointer: new", "", nil, actor)
	for _, id := range []string{keep.ID, drop.ID} {
		if err := rec.Add(id, "documentation", []Source{{Kind: KindGit, Locator: "pkg"}}); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	if err := rec.Supersede(drop.ID, replacement.ID, "obsolete"); err != nil {
		t.Fatalf("Supersede: %v", err)
	}

	tasks, err := s.ListTasksErr(store.QueryFilters{
		Project: "TST",
		Labels:  []string{BoardCurrent("TST")},
	})
	if err != nil {
		t.Fatalf("ListTasksErr: %v", err)
	}
	ids := map[string]bool{}
	for _, tk := range tasks {
		ids[tk.ID] = true
	}
	if !ids[keep.ID] {
		t.Errorf("current board must contain the live pointer %s", keep.ID)
	}
	if ids[drop.ID] {
		t.Errorf("current board must not contain the superseded pointer %s", drop.ID)
	}
}

func hasLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/contextmap/ -run 'TestAdd|TestStamp|TestRetarget|TestSupersede' -v`
Expected: FAIL — `undefined: Recorder`.

- [ ] **Step 3: Write minimal implementation**

```go
package contextmap

import (
	"fmt"
	"strings"
	"time"

	"atm/internal/store"
)

// Recorder holds the four mutating verbs. Each ensures the capability's
// vocabulary before using it, so none requires a seeded project.
type Recorder struct {
	Store    *store.Store
	Resolver *Resolver
	Actor    string
}

func projectOf(taskID string) string {
	code, _, _ := strings.Cut(taskID, "-")
	return code
}

// Add turns a task into a context pointer: it applies the kind label and writes
// the first provenance stamp.
func (rec *Recorder) Add(taskID, kind string, sources []Source) error {
	code := projectOf(taskID)
	if err := EnsureVocabulary(rec.Store, code, rec.Actor); err != nil {
		return err
	}
	if err := rec.Store.TaskLabelAdd(taskID, LabelContextKind(code, kind), rec.Actor); err != nil {
		return err
	}
	return rec.writeStamp(taskID, code, sources)
}

// Stamp re-witnesses the sources already recorded on the task: the subject is
// unchanged in meaning, so record fresh evidence for it.
func (rec *Recorder) Stamp(taskID string) error {
	code := projectOf(taskID)
	prev, ok, err := LatestStamp(rec.Store, taskID, code)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s has no provenance to re-stamp; use `atm context add` first", taskID)
	}
	sources := make([]Source, 0, len(prev.Witnesses))
	for _, w := range prev.Witnesses {
		sources = append(sources, w.Source)
	}
	return rec.writeStamp(taskID, code, sources)
}

// Retarget records new sources for a pointer whose subject survived but moved.
// The task ID is stable, so anything referencing it keeps working.
func (rec *Recorder) Retarget(taskID string, sources []Source) error {
	code := projectOf(taskID)
	if err := EnsureVocabulary(rec.Store, code, rec.Actor); err != nil {
		return err
	}
	return rec.writeStamp(taskID, code, sources)
}

// Supersede retires a pointer whose subject died or was replaced. The task keeps
// its kind, its narrative, and its provenance history -- it is simply no longer
// current, so it drops out of the context-current board.
func (rec *Recorder) Supersede(taskID, byID, reason string) error {
	code := projectOf(taskID)
	if err := EnsureVocabulary(rec.Store, code, rec.Actor); err != nil {
		return err
	}
	if _, err := rec.Store.GetTask(byID); err != nil {
		return fmt.Errorf("successor %s: %w", byID, err)
	}
	t, err := rec.Store.GetTask(taskID)
	if err != nil {
		return err
	}
	note := fmt.Sprintf("SUPERSEDED BY %s", byID)
	if reason != "" {
		note += ": " + reason
	}
	desc := t.Description
	if desc != "" {
		desc += "\n\n"
	}
	if err := rec.Store.SetDescription(taskID, desc+note, rec.Actor); err != nil {
		return err
	}
	return rec.Store.TaskLabelAdd(taskID, LabelSuperseded(code), rec.Actor)
}

// writeStamp witnesses each source now and appends a provenance comment. It
// appends rather than replaces, so the thread keeps the full freshness history.
func (rec *Recorder) writeStamp(taskID, code string, sources []Source) error {
	if len(sources) == 0 {
		return fmt.Errorf("%s: at least one --source is required", taskID)
	}
	head, err := rec.Resolver.Head()
	if err != nil {
		return err
	}
	stamp := Stamp{Version: StampVersion, At: time.Now().UTC(), Head: head}
	for _, src := range sources {
		value, err := rec.Resolver.Witness(src)
		if err != nil && !isGone(err) {
			return fmt.Errorf("witness %s: %w", src, err)
		}
		stamp.Witnesses = append(stamp.Witnesses, Witness{Source: src, Value: value})
	}
	body, err := MarshalStamp(stamp)
	if err != nil {
		return err
	}
	_, err = rec.Store.CreateComment(taskID, body, []string{LabelProvenance(code)}, "", rec.Actor)
	return err
}

func isGone(err error) bool { return err != nil && strings.Contains(err.Error(), "subject gone") }

// LatestStamp returns the newest provenance stamp on a task. ok is false when
// the pointer was never stamped -- check reports that as UNVERIFIED, never as an
// error: a human may have written the pointer by hand.
func LatestStamp(s *store.Store, taskID, code string) (Stamp, bool, error) {
	comments, err := s.ListComments(taskID)
	if err != nil {
		return Stamp{}, false, err
	}
	want := LabelProvenance(code)
	var newest Stamp
	found := false
	for _, c := range comments {
		if !hasLabelIn(c.Labels, want) {
			continue
		}
		st, err := UnmarshalStamp(c.Body)
		if err != nil {
			continue // unreadable stamp: treat as absent, never fail
		}
		if !found || st.At.After(newest.At) {
			newest, found = st, true
		}
	}
	return newest, found, nil
}

func hasLabelIn(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/contextmap/ -v`
Expected: PASS (all recorder tests).

- [ ] **Step 5: Commit**

```bash
git add internal/contextmap/recorder.go internal/contextmap/recorder_test.go
git commit -m "feat(context): recorder verbs add, stamp, retarget, supersede"
```

---

### Task 6: The check reporter

The read-only half. It assembles the worklist the manager works from, and it **must not touch the store** — Task 8 proves that byte-for-byte.

**Files:**
- Create: `internal/contextmap/check.go`
- Test: `internal/contextmap/check_test.go`

**Interfaces:**
- Consumes: `Resolver`, `Verdict` (Task 4); `LatestStamp` (Task 5); `BoardCurrent`, `LabelProvenance` (Task 3).
- Produces:
  - `type Finding struct { TaskID string; Title string; Source Source; Verdict Verdict; Detail string; AgeDays int }`
  - `type Report struct { Drift []Finding; Age []Finding; Unverified []Finding; OK []Finding; Skipped []Finding; New []string; Since string }`
  - `func Check(s *store.Store, r *Resolver, code string, since string) (Report, error)` — `since` empty means "default to the newest stamp in the project"

- [ ] **Step 1: Write the failing test**

```go
package contextmap

import (
	"testing"
	"time"

	"atm/internal/store"
)

func TestCheckClassifiesEachPointer(t *testing.T) {
	repo := newTestRepo(t)
	rec, s, actor := newRecorder(t, repo)

	clean, _ := s.CreateTask("TST", "Pointer: clean", "", nil, actor)
	drifted, _ := s.CreateTask("TST", "Pointer: drifted", "", nil, actor)
	external, _ := s.CreateTask("TST", "Pointer: jira", "", nil, actor)
	unverified, _ := s.CreateTask("TST", "Pointer: handwritten", "",
		[]string{LabelContextKind("TST", "documentation")}, actor)

	commitFile(t, repo, "clean.txt", "stable\n")
	commitFile(t, repo, "drifty.txt", "before\n")
	if err := rec.Add(clean.ID, "documentation", []Source{{Kind: KindGit, Locator: "clean.txt"}}); err != nil {
		t.Fatalf("Add clean: %v", err)
	}
	if err := rec.Add(drifted.ID, "documentation", []Source{{Kind: KindGit, Locator: "drifty.txt"}}); err != nil {
		t.Fatalf("Add drifted: %v", err)
	}
	if err := rec.Add(external.ID, "documentation", []Source{{Kind: KindExternal, Locator: "jira/TST-1"}}); err != nil {
		t.Fatalf("Add external: %v", err)
	}

	commitFile(t, repo, "drifty.txt", "after\n")

	rep, err := Check(s, &Resolver{Repo: repo}, "TST", "")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	if !containsTask(rep.Drift, drifted.ID) {
		t.Errorf("drifted pointer missing from DRIFT: %+v", rep.Drift)
	}
	if !containsTask(rep.OK, clean.ID) {
		t.Errorf("clean pointer missing from OK: %+v", rep.OK)
	}
	if !containsTask(rep.Age, external.ID) {
		t.Errorf("external pointer missing from AGE: %+v", rep.Age)
	}
	if !containsTask(rep.Unverified, unverified.ID) {
		t.Errorf("unstamped pointer missing from UNVERIFIED: %+v", rep.Unverified)
	}
	// A pointer must land in exactly one bucket.
	if containsTask(rep.OK, drifted.ID) {
		t.Error("drifted pointer also reported OK")
	}
}

func TestCheckReportsNewTerritory(t *testing.T) {
	// A file changed in git that no pointer claims. This is how a repeat run
	// notices the repo grew, without check knowing anything about repo structure.
	repo := newTestRepo(t)
	rec, s, actor := newRecorder(t, repo)
	covered, _ := s.CreateTask("TST", "Pointer: pkg", "", nil, actor)
	if err := rec.Add(covered.ID, "documentation", []Source{{Kind: KindGit, Locator: "pkg"}}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	commitFile(t, repo, "pkg/a.go", "package pkg\n\nfunc New() {}\n") // covered -> DRIFT
	commitFile(t, repo, "brand_new.go", "package main\n")             // uncovered -> NEW

	rep, err := Check(s, &Resolver{Repo: repo}, "TST", "")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	if !contains(rep.New, "brand_new.go") {
		t.Errorf("NEW = %v, want it to contain brand_new.go", rep.New)
	}
	for _, p := range rep.New {
		if p == "pkg/a.go" {
			t.Error("pkg/a.go is claimed by a pointer; it must be DRIFT, not NEW")
		}
	}
}

func TestCheckSkipsSupersededPointers(t *testing.T) {
	// Superseded knowledge is history. It must not appear in the worklist.
	repo := newTestRepo(t)
	rec, s, actor := newRecorder(t, repo)
	old, _ := s.CreateTask("TST", "Pointer: old", "", nil, actor)
	next, _ := s.CreateTask("TST", "Pointer: new", "", nil, actor)
	if err := rec.Add(old.ID, "documentation", []Source{{Kind: KindGit, Locator: "pkg"}}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitFile(t, repo, "pkg/a.go", "package pkg\n\nfunc New() {}\n")
	if err := rec.Supersede(old.ID, next.ID, "gone"); err != nil {
		t.Fatalf("Supersede: %v", err)
	}

	rep, err := Check(s, &Resolver{Repo: repo}, "TST", "")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if containsTask(rep.Drift, old.ID) {
		t.Error("superseded pointer must not appear in DRIFT")
	}
}

func TestCheckAgeIsMeasuredInDays(t *testing.T) {
	repo := newTestRepo(t)
	rec, s, actor := newRecorder(t, repo)
	ext, _ := s.CreateTask("TST", "Pointer: notion", "", nil, actor)
	if err := rec.Add(ext.ID, "documentation", []Source{{Kind: KindExternal, Locator: "notion/arch"}}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	rep, err := Check(s, &Resolver{Repo: repo}, "TST", "")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(rep.Age) != 1 {
		t.Fatalf("AGE = %+v, want 1 finding", rep.Age)
	}
	if rep.Age[0].AgeDays != 0 {
		t.Errorf("just-stamped external: AgeDays = %d, want 0", rep.Age[0].AgeDays)
	}
	_ = time.Now
}

func containsTask(fs []Finding, id string) bool {
	for _, f := range fs {
		if f.TaskID == id {
			return true
		}
	}
	return false
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

var _ = store.QueryFilters{}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/contextmap/ -run TestCheck -v`
Expected: FAIL — `undefined: Check`.

- [ ] **Step 3: Write minimal implementation**

```go
package contextmap

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"atm/internal/store"
)

// Finding is one pointer's verdict on one source.
type Finding struct {
	TaskID  string
	Title   string
	Source  Source
	Verdict Verdict
	Detail  string
	AgeDays int // set for AGE findings
}

// Report is the worklist a manager session works from.
type Report struct {
	Drift      []Finding
	Age        []Finding
	Unverified []Finding
	OK         []Finding
	Skipped    []Finding
	New        []string // repo paths changed in git that no pointer claims
	Since      string   // the revision NEW was computed from
}

// Check compares every current context pointer against reality and reports what
// it finds. It is STRICTLY READ-ONLY: it mutates nothing, ever. Deciding what a
// drift means -- and acting on it -- belongs to the manager.
//
// since bounds the NEW-territory scan; when empty it defaults to the HEAD
// recorded on the most recent stamp in the project, so no watermark needs
// storing anywhere.
func Check(s *store.Store, r *Resolver, code, since string) (Report, error) {
	tasks, err := s.ListTasksErr(store.QueryFilters{
		Project: code,
		Labels:  []string{BoardCurrent(code)},
	})
	if err != nil {
		return Report{}, err
	}

	var rep Report
	covered := map[string]bool{}
	newestStampHead := ""
	var newestAt time.Time

	for _, t := range tasks {
		stamp, ok, err := LatestStamp(s, t.ID, code)
		if err != nil {
			return Report{}, err
		}
		if !ok {
			rep.Unverified = append(rep.Unverified, Finding{
				TaskID: t.ID, Title: t.Title, Verdict: "UNVERIFIED",
				Detail: "never stamped",
			})
			continue
		}
		if stamp.At.After(newestAt) {
			newestAt, newestStampHead = stamp.At, stamp.Head
		}
		for _, w := range stamp.Witnesses {
			if w.Source.Kind == KindGit {
				covered[w.Source.Locator] = true
			}
			f := Finding{TaskID: t.ID, Title: t.Title, Source: w.Source}
			verdict, err := r.Compare(w.Source, w.Value)
			if err != nil {
				return Report{}, err
			}
			f.Verdict = verdict
			switch verdict {
			case VerdictOK:
				rep.OK = append(rep.OK, f)
			case VerdictDrift:
				f.Detail = "content changed since verified"
				rep.Drift = append(rep.Drift, f)
			case VerdictGone:
				// Moved or deleted subjects are still the manager's drift
				// worklist -- they need a retarget or a supersede.
				f.Verdict = VerdictDrift
				f.Detail = "path moved or deleted"
				rep.Drift = append(rep.Drift, f)
			case VerdictSkipped:
				f.Detail = "could not witness (offline?)"
				rep.Skipped = append(rep.Skipped, f)
			case VerdictUnwitnessable:
				f.AgeDays = int(time.Since(stamp.At).Hours() / 24)
				f.Detail = fmt.Sprintf("%dd since verified; re-verify by hand", f.AgeDays)
				rep.Age = append(rep.Age, f)
			}
		}
	}

	rep.Since = since
	if rep.Since == "" {
		rep.Since = newestStampHead
	}
	changed, err := r.ChangedSince(rep.Since)
	if err != nil {
		return Report{}, err
	}
	for _, p := range changed {
		if !isCovered(p, covered) {
			rep.New = append(rep.New, p)
		}
	}
	sort.Strings(rep.New)
	return rep, nil
}

// isCovered reports whether a changed path falls under any pointer's git
// source. A pointer at "internal/store" covers "internal/store/task.go".
func isCovered(p string, covered map[string]bool) bool {
	for c := range covered {
		if p == c || strings.HasPrefix(p, c+"/") {
			return true
		}
	}
	// A pointer at a directory also covers files added directly beneath it.
	return covered[path.Dir(p)]
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/contextmap/ -v`
Expected: PASS (all check tests).

- [ ] **Step 5: Commit**

```bash
git add internal/contextmap/check.go internal/contextmap/check_test.go
git commit -m "feat(context): read-only check reporter"
```

---

### Task 7: CLI surface

Wire the five verbs into cobra. This is the layer the manager actually calls, and it is the reason the manager never sees a label string.

**Files:**
- Create: `internal/cli/context.go`
- Create: `internal/cli/context_test.go`
- Modify: `internal/cli/root.go` — register the command alongside the others

**Interfaces:**
- Consumes: `contextmap.Recorder`, `contextmap.Check`, `contextmap.ParseSource`, `contextmap.Report`.
- Produces: `func newContextCmd(st *cliState) *cobra.Command`

- [ ] **Step 1: Write the failing test**

The existing harness is `goldenHarness` (`internal/cli/harness_test.go:36`): `newGoldenHarness(t)` opens a store in a temp dir, and `h.run(args...)` returns `(stdout, stderr, exitCode)` — note it returns an **exit code, not an error**, and defaults to JSON output (`h.output = outputJSON`; set `h.output = outputText` for text assertions).

Two things the harness does not have and this task needs: a git repo to run `check` against, and a store digest. Add both to `internal/cli/context_test.go` (not to `harness_test.go` — they are only used here):

```go
package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// chdirRepo creates a git repo with one committed file and chdirs into it for
// the duration of the test. `atm context` resolves its repo from the working
// directory, which is the repo the manager is running in.
func chdirRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg", "a.go"), []byte("package pkg\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "init")
	t.Chdir(dir) // Go 1.24+; restores the old cwd at test end
	return dir
}

func commitInRepo(t *testing.T, dir, rel, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-qm", "change"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// storeDigest hashes every file under the store root, so a byte-level change
// anywhere in the ledger changes the digest.
func storeDigest(t *testing.T, root string) string {
	t.Helper()
	var paths []string
	if err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			paths = append(paths, p)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		h.Write([]byte(p))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// setup makes a harness with project TST, inside a git repo, with an actor set.
func setup(t *testing.T) (*goldenHarness, string) {
	t.Helper()
	repo := chdirRepo(t)
	h := newGoldenHarness(t)
	h.output = outputText
	const actor = "manager@claude:opus-4.8"
	if _, _, code := h.run("project", "create", "--code", "TST", "--name", "Test", "--actor", actor); code != ExitSuccess {
		t.Fatalf("project create: exit %d", code)
	}
	return h, repo
}

func TestContextAddThenCheckReportsOK(t *testing.T) {
	h, _ := setup(t)
	const actor = "manager@claude:opus-4.8"
	h.run("task", "create", "--project", "TST", "--title", "Pointer: pkg", "--actor", actor)
	if _, errOut, code := h.run("context", "add", "--task", "TST-0001",
		"--kind", "documentation", "--source", "git:pkg", "--actor", actor); code != ExitSuccess {
		t.Fatalf("context add: exit %d: %s", code, errOut)
	}

	out, _, code := h.run("context", "check", "--project", "TST")
	if code != ExitSuccess {
		t.Fatalf("context check: exit %d", code)
	}
	if !strings.Contains(out, "OK (1)") {
		t.Errorf("want OK (1) in report:\n%s", out)
	}
	if strings.Contains(out, "DRIFT") {
		t.Errorf("nothing changed; check must not report drift:\n%s", out)
	}
}

func TestContextCheckIsReadOnly(t *testing.T) {
	// The design invariant: check reports, it never decides. Prove the store is
	// byte-identical after a check run that finds drift, age, and unverified
	// pointers all at once.
	h, repo := setup(t)
	const actor = "manager@claude:opus-4.8"
	h.run("task", "create", "--project", "TST", "--title", "Pointer: pkg", "--actor", actor)
	h.run("task", "create", "--project", "TST", "--title", "Pointer: handwritten", "--actor", actor)
	h.run("task", "label", "add", "--task", "TST-0002", "--label", "TST:context:documentation", "--actor", actor)
	h.run("context", "add", "--task", "TST-0001", "--kind", "documentation",
		"--source", "git:pkg", "--source", "external:jira/TST-9", "--actor", actor)
	commitInRepo(t, repo, "pkg/a.go", "package pkg\n\nfunc New() {}\n") // force DRIFT

	root := h.store.StorePath()
	before := storeDigest(t, root)
	out, _, code := h.run("context", "check", "--project", "TST")
	if code != ExitSuccess {
		t.Fatalf("context check: exit %d", code)
	}
	after := storeDigest(t, root)

	if before != after {
		t.Fatalf("check mutated the store\nreport was:\n%s", out)
	}
	// And it really did have all three things to report.
	for _, want := range []string{"DRIFT", "AGE", "UNVERIFIED"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %s section:\n%s", want, out)
		}
	}
}

func TestContextAddRequiresActor(t *testing.T) {
	h, _ := setup(t)
	h.run("task", "create", "--project", "TST", "--title", "x", "--actor", "manager@claude:opus-4.8")
	if _, _, code := h.run("context", "add", "--task", "TST-0001",
		"--kind", "documentation", "--source", "git:pkg"); code == ExitSuccess {
		t.Error("mutating command must require --actor or ATM_ACTOR")
	}
}

func TestContextRejectsUnkindedSource(t *testing.T) {
	h, _ := setup(t)
	const actor = "manager@claude:opus-4.8"
	h.run("task", "create", "--project", "TST", "--title", "x", "--actor", actor)
	_, _, code := h.run("context", "add", "--task", "TST-0001",
		"--kind", "documentation", "--source", "pkg", "--actor", actor)
	if code != ExitUsage {
		t.Errorf("bare path without a kind prefix must be a usage error, got exit %d", code)
	}
}

func TestContextWorksWithoutSeededLabels(t *testing.T) {
	// The capability bootstraps its own vocabulary. project create seeds the
	// default labels, but context-current and knowledge:superseded are NOT among
	// them -- so this proves the capability ensured them itself.
	h, _ := setup(t)
	const actor = "manager@claude:opus-4.8"
	h.run("task", "create", "--project", "TST", "--title", "Pointer: pkg", "--actor", actor)
	h.run("context", "add", "--task", "TST-0001", "--kind", "documentation",
		"--source", "git:pkg", "--actor", actor)

	out, _, _ := h.run("label", "list", "--project", "TST")
	for _, want := range []string{"TST:context-current", "TST:comment:provenance"} {
		if !strings.Contains(out, want) {
			t.Errorf("%s was not ensured by the capability:\n%s", want, out)
		}
	}
}
```

**Note for the implementer:** read `internal/cli/harness_test.go` before starting. Do not add a second harness — `goldenHarness` is the one. Check the real flag names for `project create` and `task label add` with `./bin/atm <cmd> --help` and correct the calls above if they differ; the assertions are what matter, not the exact flags.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestContext -v`
Expected: FAIL — `unknown command "context"`.

- [ ] **Step 3: Write minimal implementation**

```go
package cli

import (
	"fmt"
	"os"

	"atm/internal/contextmap"

	"github.com/spf13/cobra"
)

func newContextCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Record and verify the project's context map",
		Long: "Record where each context pointer came from, and report which pointers have " +
			"drifted from reality.\n\n" +
			"add/stamp/retarget/supersede record; check only reports -- it never marks anything " +
			"stale. A changed file is not a wrong pointer: that judgement is yours.",
	}
	bindActorFlag(cmd, st)
	cmd.AddCommand(newContextAddCmd(st))
	cmd.AddCommand(newContextStampCmd(st))
	cmd.AddCommand(newContextRetargetCmd(st))
	cmd.AddCommand(newContextSupersedeCmd(st))
	cmd.AddCommand(newContextCheckCmd(st))
	return cmd
}

// recorder builds a Recorder rooted at the current working directory, which is
// the repo the manager is running in.
func (st *cliState) recorder(actor string) (*contextmap.Recorder, error) {
	s, err := st.openStore()
	if err != nil {
		return nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return &contextmap.Recorder{
		Store:    s,
		Resolver: &contextmap.Resolver{Repo: cwd},
		Actor:    actor,
	}, nil
}

func parseSources(raw []string) ([]contextmap.Source, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: at least one --source is required", ErrUsage)
	}
	out := make([]contextmap.Source, 0, len(raw))
	for _, r := range raw {
		src, err := contextmap.ParseSource(r)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUsage, err)
		}
		out = append(out, src)
	}
	return out, nil
}

func newContextAddCmd(st *cliState) *cobra.Command {
	var task, kind string
	var sources []string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Make a task a context pointer and stamp its provenance",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			srcs, err := parseSources(sources)
			if err != nil {
				return err
			}
			rec, err := st.recorder(actor)
			if err != nil {
				return err
			}
			if err := rec.Add(task, kind, srcs); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": task, "kind": kind}, func() {
				fmt.Fprintf(st.stdout(), "stamped %s as context:%s\n", task, kind)
			})
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "task id")
	cmd.Flags().StringVar(&kind, "kind", "", "pointer kind: agent, repository, documentation, or question")
	cmd.Flags().StringArrayVar(&sources, "source", nil,
		"kinded locator this pointer was derived from, repeatable: git:<path>, file:<path>, url:<url>, external:<system>/<id>")
	_ = cmd.MarkFlagRequired("task")
	_ = cmd.MarkFlagRequired("kind")
	return cmd
}

func newContextStampCmd(st *cliState) *cobra.Command {
	var task string
	cmd := &cobra.Command{
		Use:   "stamp",
		Short: "Re-verify a pointer: its subject is unchanged in meaning",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			rec, err := st.recorder(actor)
			if err != nil {
				return err
			}
			if err := rec.Stamp(task); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": task}, func() {
				fmt.Fprintf(st.stdout(), "re-stamped %s\n", task)
			})
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "task id")
	_ = cmd.MarkFlagRequired("task")
	return cmd
}

func newContextRetargetCmd(st *cliState) *cobra.Command {
	var task string
	var sources []string
	cmd := &cobra.Command{
		Use:   "retarget",
		Short: "Point at new sources: the subject survived, but moved",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			srcs, err := parseSources(sources)
			if err != nil {
				return err
			}
			rec, err := st.recorder(actor)
			if err != nil {
				return err
			}
			if err := rec.Retarget(task, srcs); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": task}, func() {
				fmt.Fprintf(st.stdout(), "retargeted %s\n", task)
			})
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "task id")
	cmd.Flags().StringArrayVar(&sources, "source", nil, "new kinded locator, repeatable")
	_ = cmd.MarkFlagRequired("task")
	return cmd
}

func newContextSupersedeCmd(st *cliState) *cobra.Command {
	var task, by, reason string
	cmd := &cobra.Command{
		Use:   "supersede",
		Short: "Retire a pointer whose subject died; history is kept",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			rec, err := st.recorder(actor)
			if err != nil {
				return err
			}
			if err := rec.Supersede(task, by, reason); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": task, "by": by}, func() {
				fmt.Fprintf(st.stdout(), "superseded %s by %s\n", task, by)
			})
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "task id to retire")
	cmd.Flags().StringVar(&by, "by", "", "task id that replaces it")
	cmd.Flags().StringVar(&reason, "reason", "", "why it was superseded")
	_ = cmd.MarkFlagRequired("task")
	_ = cmd.MarkFlagRequired("by")
	return cmd
}

func newContextCheckCmd(st *cliState) *cobra.Command {
	var project, since string
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Report which pointers drifted (read-only; mutates nothing)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := st.resolveActor(false); err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			rep, err := contextmap.Check(s, &contextmap.Resolver{Repo: cwd}, project, since)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), reportToJSON(rep), func() { printReport(st, rep) })
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "ATM project code")
	cmd.Flags().StringVar(&since, "since", "", "revision to scan for new territory (default: the newest stamp in the project)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func reportToJSON(rep contextmap.Report) map[string]any {
	find := func(fs []contextmap.Finding) []map[string]any {
		out := make([]map[string]any, 0, len(fs))
		for _, f := range fs {
			m := map[string]any{
				"task":    f.TaskID,
				"title":   f.Title,
				"source":  f.Source.String(),
				"verdict": string(f.Verdict),
				"detail":  f.Detail,
			}
			if f.AgeDays > 0 {
				m["age_days"] = f.AgeDays
			}
			out = append(out, m)
		}
		return out
	}
	return map[string]any{
		"drift":      find(rep.Drift),
		"age":        find(rep.Age),
		"unverified": find(rep.Unverified),
		"skipped":    find(rep.Skipped),
		"ok":         find(rep.OK),
		"new":        rep.New,
		"since":      rep.Since,
	}
}

func printReport(st *cliState, rep contextmap.Report) {
	w := st.stdout()
	section := func(name string, fs []contextmap.Finding, gloss string) {
		if len(fs) == 0 {
			return
		}
		fmt.Fprintf(w, "\n%s (%d)\t%s\n", name, len(fs), gloss)
		for _, f := range fs {
			fmt.Fprintf(w, "  %s\t%s\t%s\n", f.TaskID, f.Source, f.Detail)
		}
	}
	if rep.Since != "" {
		fmt.Fprintf(w, "  (new territory since %s)\n", rep.Since)
	}
	section("DRIFT", rep.Drift, "provable content change")
	if len(rep.New) > 0 {
		fmt.Fprintf(w, "\nNEW (%d)\tchanged in git, claimed by no pointer\n", len(rep.New))
		for _, p := range rep.New {
			fmt.Fprintf(w, "  %s\n", p)
		}
	}
	section("AGE", rep.Age, "unprovable; re-verify by hand")
	section("UNVERIFIED", rep.Unverified, "no provenance stamp")
	section("SKIPPED", rep.Skipped, "could not witness")
	fmt.Fprintf(w, "\nOK (%d)\n", len(rep.OK))
}
```

Then register it. In `internal/cli/root.go`, alongside the other `AddCommand` calls (e.g. next to `newLabelCmd(st)`):

```go
	root.AddCommand(newContextCmd(st))
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run TestContext -v && make verify`
Expected: PASS. `make verify` must be clean — it is the AGENTS.md gate.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/context.go internal/cli/context_test.go internal/cli/root.go internal/cli/harness_test.go
git commit -m "feat(context): atm context CLI surface"
```

---

### Task 8: The mapping track — onboarding becomes repeatable

Rename the manager's `--onboarding` action to `--mapping` and teach the prompt the verify → discover → close loop. `--onboarding` survives as a hidden deprecated alias, per the pattern in `internal/cli/task.go` (`--task` canonical / `--id` deprecated) recorded in ATM-0113.

**Files:**
- Modify: `internal/cli/manager.go:15-39` (opts + action consts), `:174-207` (flag binding + validation)
- Modify: `internal/manager/context_v1.md:24` (the Onboarding role)
- Test: `internal/cli/manager_test.go`

**Interfaces:**
- Consumes: existing `managerOpts`, `managerAction`, `validateManagerAction`.
- Produces: `managerActionMapping managerAction = "mapping"` replacing `managerActionOnboarding`.

- [ ] **Step 1: Write the failing test**

Add to `internal/cli/manager_test.go`:

```go
func TestMappingActionResolves(t *testing.T) {
	got, err := validateManagerAction(managerOpts{Mapping: true})
	if err != nil {
		t.Fatalf("validateManagerAction: %v", err)
	}
	if got != managerActionMapping {
		t.Errorf("got %q, want %q", got, managerActionMapping)
	}
}

func TestOnboardingAliasStillResolves(t *testing.T) {
	// Deprecated, hidden, but never hard-broken: the flag is on a stable CLI
	// surface. See ATM-0113.
	got, err := validateManagerAction(managerOpts{Onboarding: true})
	if err != nil {
		t.Fatalf("validateManagerAction: %v", err)
	}
	if got != managerActionMapping {
		t.Errorf("--onboarding must resolve to %q, got %q", managerActionMapping, got)
	}
}

func TestMappingAndOnboardingTogetherIsOneAction(t *testing.T) {
	// Both names for the same action must not count as two selections.
	if _, err := validateManagerAction(managerOpts{Mapping: true, Onboarding: true}); err != nil {
		t.Errorf("alias + canonical must be accepted as one action, got %v", err)
	}
}

func TestNoActionIsUsageError(t *testing.T) {
	if _, err := validateManagerAction(managerOpts{}); err == nil {
		t.Error("want usage error when no action is selected")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestMapping|TestOnboardingAlias|TestNoAction' -v`
Expected: FAIL — `unknown field Mapping`, `undefined: managerActionMapping`.

- [ ] **Step 3: Write minimal implementation**

In `internal/cli/manager.go`, add `Mapping bool` to `managerOpts` (keep `Onboarding bool`), and replace the action const:

```go
	managerActionMapping managerAction = "mapping"
```

Delete `managerActionOnboarding`. Then update the flag binding:

```go
func bindManagerActionFlags(cmd *cobra.Command, opts *managerOpts) {
	cmd.Flags().BoolVar(&opts.Planning, "planning", false, "review backlog readiness, blocked work, and in-flight work")
	cmd.Flags().BoolVar(&opts.Grooming, "grooming", false, "prioritize and shape the backlog")
	cmd.Flags().BoolVar(&opts.Tracking, "tracking", false, "curate progress, decisions, questions, and handoffs")
	cmd.Flags().BoolVar(&opts.Asking, "asking", false, "answer project questions grounded in ledger IDs")
	cmd.Flags().BoolVar(&opts.Glossary, "glossary", false, "maintain shared project language")
	cmd.Flags().BoolVar(&opts.Mapping, "mapping", false, "reconcile the project's context map against the repo: verify drifted pointers, discover new territory")

	// Deprecated alias. The action was named --onboarding when it was believed to
	// be a first-contact ceremony; it is now a repeatable refresh. Never hard-break
	// a flag on a stable CLI surface (ATM-0113).
	cmd.Flags().BoolVar(&opts.Onboarding, "onboarding", false, "")
	_ = cmd.Flags().MarkDeprecated("onboarding", "use --mapping")
	_ = cmd.Flags().MarkHidden("onboarding")
}
```

And in `validateManagerAction`, replace the `Onboarding` branch so the alias collapses into one selection:

```go
	if opts.Mapping || opts.Onboarding {
		selected = append(selected, managerActionMapping)
	}
	if len(selected) != 1 {
		return "", fmt.Errorf("%w: choose exactly one manager action: --planning, --grooming, --tracking, --asking, --glossary, or --mapping", ErrUsage)
	}
```

Then rewrite the role in `internal/manager/context_v1.md`. Replace line 24 (`- **Onboarding** — ...`) with:

```markdown
- **Mapping** — reconcile the project's context map against reality. Repeatable, and meant to be run often; the first run in a fresh repo is just the case where there is nothing yet to verify.

  1. **Verify.** Run `<ATM_BIN> context check --project <CODE>`. Work the report:
     - `DRIFT` — read the pointer's description against the actual change. If the description still tells the truth, `<ATM_BIN> context stamp --task <ID>`. If the subject survived but moved, `<ATM_BIN> context retarget --task <ID> --source <kinded-locator>`. If the subject died or was replaced, create the successor and `<ATM_BIN> context supersede --task <ID> --by <NEW-ID> --reason "..."`.
     - `AGE` — an external source (Jira, Notion) that nothing can witness locally. Re-read it with your own tools, then `stamp`.
     - `UNVERIFIED` — a pointer someone wrote by hand. Read it, confirm it is true, then `<ATM_BIN> context add --task <ID> --kind <kind> --source <kinded-locator>`.
  2. **Discover.** Work the `NEW` list: territory that changed in git and that no pointer claims. For each thing worth knowing, create a task and `<ATM_BIN> context add` it. Ignore what is not worth a pointer -- that is a judgement, and it is yours.
  3. **Close.** Everything reported is now stamped, retargeted, superseded, or deliberately ignored.

  `check` never marks anything stale: a changed file is not a wrong pointer. It tells you where to look; you decide what it means.
```

**Do not** teach the prompt any label names. The verbs are the interface — that is the whole point of the capability pattern (`docs/architecture/label-substrate-and-capabilities.md`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ ./internal/manager/ -v && make verify`
Expected: PASS. Note: `internal/manager/context_test.go` asserts on prompt fragments — if it asserts the word "Onboarding", update that assertion to "Mapping". (This is exactly the ATM-0114 bug class; expect one or two such assertions.)

- [ ] **Step 5: Commit**

```bash
git add internal/cli/manager.go internal/cli/manager_test.go internal/manager/context_v1.md internal/manager/context_test.go
git commit -m "feat(context): repeatable mapping track replaces one-shot onboarding"
```

---

### Task 9: Teach `atm conventions` the current-knowledge board

Agents currently read the raw `context:*` namespace in the first-contact sequence, so they see superseded knowledge. Point them at the board instead. Without this task, nothing an agent actually reads changes, and the whole feature is invisible.

**Files:**
- Modify: `internal/cli/conventions.go` (the first-contact sequence and the JSON mirror at `:150`)
- Test: `internal/cli/conventions_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestConventionsPointAtCurrentKnowledgeBoard(t *testing.T) {
	out := renderConventions() // use whatever the existing test file already calls
	if !strings.Contains(out, "context-current") {
		t.Error("first-contact sequence must send agents to the context-current board, " +
			"not the raw context:* namespace -- otherwise they read superseded knowledge")
	}
	if !strings.Contains(out, "atm context check") {
		t.Error("conventions must mention `atm context check` so an agent can find the capability")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestConventions -v`
Expected: FAIL — the string is absent.

- [ ] **Step 3: Write minimal implementation**

In `internal/cli/conventions.go`, in the "Agent first-contact sequence", change the context-reading steps to read the board, and add a short section. The existing steps 3 and 4 read:

```
3. `atm task list --project <CODE> --label <CODE>:context:agent` — get agent directions...
4. `atm task list --project <CODE> --label <CODE>:context:repository` / `:context:documentation` ...
```

Replace with:

```
3. `atm task list --project <CODE> --label <CODE>:context-current` — the project's current knowledge: agent directions, repository pointers, and documentation pointers that have not been superseded. Read this board rather than the raw `context:*` namespace; membership is computed, so it is always the latest. Narrow by kind with an extra `--label <CODE>:context:agent`.
```

Renumber the following steps. Then add, after the Boards section:

```
## The context map

Context pointers record what they were derived from, so drift can be detected. `atm context check --project <CODE>` reports which pointers have gone stale against the repo (DRIFT), which name external systems nobody has re-read lately (AGE), and which were never verified (UNVERIFIED). It is read-only — it tells you where to look, never what it means.

Record a pointer with `atm context add`; re-verify one with `atm context stamp`; repoint a subject that moved with `atm context retarget`; retire one whose subject died with `atm context supersede`. These verbs own the context vocabulary — do not hand-assign the labels or hand-edit a provenance comment.
```

Mirror the same text into the JSON structure around `internal/cli/conventions.go:150` — that file keeps a text form and a JSON form in sync, and `conventions_test.go` checks both.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -v && make verify`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/conventions.go internal/cli/conventions_test.go
git commit -m "feat(context): point the first-contact sequence at current knowledge"
```

---

### Task 10: Migrate this repo's own context map

Dogfood it. ATM's ledger has sixteen context pointers and none of them is stamped; ATM-0014 points at `internal/onboard/`, a package that no longer exists. Until they are stamped, `check` reports all sixteen as UNVERIFIED and the feature proves nothing.

This task is **operational, not code** — it runs the tool against the real ledger.

**Files:** none. This task mutates the ATM store, not the repo.

- [ ] **Step 1: See the starting state**

```bash
make build
./bin/atm context check --project ATM
```

Expected: sixteen `UNVERIFIED` findings, no DRIFT (nothing is stamped yet, so nothing can have drifted).

- [ ] **Step 2: Stamp every pointer that is still true**

For each of ATM-0001 through ATM-0016, read the task (`./bin/atm task show --task <ID>`), find the path or URL its description names, confirm the subject still exists, and stamp it. For example:

```bash
export ATM_ACTOR="developer@claude:opus-4.8"
./bin/atm context add --task ATM-0007 --kind documentation --source git:internal/store
./bin/atm context add --task ATM-0009 --kind documentation --source git:internal/tui
./bin/atm context add --task ATM-0002 --kind repository    --source git:.
```

Use the kind the task already carries. Do not invent new kinds.

- [ ] **Step 3: Supersede what has actually died**

ATM-0014 ("Doc pointer: onboarding prompt template (internal/onboard/prompt_opencode_v1.md)") names a package that no longer exists — the renderer now lives at `internal/manager/context_v1.md`. Create the successor and retire the old pointer:

```bash
./bin/atm task create --project ATM \
  --title "Doc pointer: manager context template (internal/manager/context_v1.md)" \
  --description "The manager session prompt template. RenderContext (internal/manager/context.go) substitutes <CODE>, <PERSONA_BLOCK>, <ACTION_BLOCK> and friends into it. Read this to see what a manager session is actually told to do."
# note the new ID, e.g. ATM-0116
./bin/atm context add --task ATM-0116 --kind documentation --source git:internal/manager/context_v1.md
./bin/atm context supersede --task ATM-0014 --by ATM-0116 \
  --reason "internal/onboard was removed; the renderer now lives in internal/manager"
```

- [ ] **Step 4: Confirm the map is clean**

```bash
./bin/atm context check --project ATM
```

Expected: every pointer `OK`; no `UNVERIFIED`; ATM-0014 absent from the report entirely (it is superseded, so it is no longer on the `context-current` board). Any `NEW` entries are real: decide whether each deserves a pointer, and add the ones that do.

- [ ] **Step 5: Record the result on the task**

```bash
./bin/atm task comment add --task ATM-0085 --label ATM:comment:progress \
  --actor "developer@claude:opus-4.8" \
  --body "Migrated ATM's own context map: stamped ATM-0001..ATM-0016, superseded ATM-0014 (internal/onboard is gone; successor points at internal/manager/context_v1.md). check now reports a clean map. The capability is dogfooded."
```

---

## Self-Review

**Spec coverage:**

| Spec section | Task |
|---|---|
| Capability-command principle (ensures vocabulary, intent verbs, owns formats) | 3, 5, 7 |
| Five verbs, one read-only | 5, 6, 7 |
| Witness model: git/file provable, url opportunistic, external aged | 4, 6 |
| Provenance in comments, append-only history | 2, 5 |
| `UNVERIFIED` degradation, never an error | 5 (`LatestStamp` swallows unreadable stamps), 6 |
| Lifecycle namespace composes with kind | 5 (`TestSupersedeLabelsOldAndKeepsHistory`) |
| `context-current` board, ensured on first use | 3, 5 (`TestSupersededTaskLeavesCurrentBoard`) |
| First-contact sequence reads the board | 9 |
| No new persistent state (no watermark) | 6 (`since` defaults to newest stamp's HEAD) |
| No repo-structure knowledge; git arithmetic only | 6 (`TestCheckReportsNewTerritory`) |
| `--onboarding` → `--mapping`, hidden deprecated alias | 8 |
| verify → discover → close prompt track | 8 |
| Testing: check purity, vocabulary bootstrap, witness kinds, offline, board, flag alias | 7, 3, 4, 4, 5, 8 |

Two spec requirements are covered only implicitly, so calling them out: **offline degradation** is tested at the unit level via `Resolver.HTTP == nil` (Task 4's `Witness` returns `""` → `VerdictSkipped`); there is no integration test that unplugs the network, and there should not be one. And **"external re-verified with the agent's own tools"** is a prompt instruction (Task 8), not code — there is nothing to test.

**Placeholder scan:** none. Every code step carries complete code; the one place the plan defers to the implementer's reading (`harness_test.go` shape in Task 7) names the exact file and the exact helpers to add.

**Type consistency:** `Source`/`Kind`/`Witness`/`Stamp`/`Verdict`/`Finding`/`Report`/`Resolver`/`Recorder` are each defined once and used with the same signature downstream. `LatestStamp(s, taskID, code)` keeps its three-arg form in Tasks 5 and 6. `Check(s, r, code, since)` matches its Task 7 call site. `EnsureVocabulary(s, code, actor)` matches all four recorder call sites.

**One risk worth naming:** Task 10 mutates the real ATM store, and `supersede` on ATM-0014 is not trivially reversible (it edits a description). Run Task 10 only after Tasks 1-9 are merged and `make verify` is green.
