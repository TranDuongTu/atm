# Computed Labels (Boards) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a label carry a boolean expression over other labels, so a named "board" (`next-sprint`, `release-v1.0.0`, `untriaged`) is just a label whose membership is computed rather than asserted.

**Architecture:** `Label` gains one optional `Expr` field. Three label kinds are discriminated mechanically with no namespace special-cased: **stored** (`ATM:status:open`, tasks assert it), **namespace** (`ATM:status:*`, name ends in `:*`, expression implicit in the name, emergent, expandable into members), and **board** (`ATM:next-sprint`, `Expr` set, authored, no members). Because a board *is* a label, the whole existing query surface gets boards for free — `--label ATM:next-sprint` needs no new flag. The `label.upserted` log event already marshals the entire `Label` struct, so no new log action is needed.

**Tech Stack:** Go, SQLite (`cache.db`, derived/rebuildable), Bubble Tea TUI.

**Spec:** `docs/superpowers/specs/2026-07-13-computed-labels-boards-design.md`
**Ledger:** ATM-0115 (feature). Merge semantics recorded at ATM-0105-c0004.

## Global Constraints

- **Actor stamp:** every store mutation in a test or CLI path uses an actor of the form `persona@agent:model`. Tests use the existing `testActor` constant.
- **No namespace is special-cased in the store.** `atm conventions` forbids it. `status`, `board`, `priority` get no privileged treatment anywhere in `internal/store`.
- **Computed labels are never stored on tasks.** A task's `Labels` may contain only stored labels.
- **The log is the sole source of truth.** `cache.db` stays derived and rebuildable; never migrate data *into* the log to serve the cache.
- **Label name grammar (new):** `^[A-Z]{3,6}:[a-z0-9][a-z0-9-]*(:([a-z0-9][a-z0-9-]*|\*))?$`
- **Run `make lint test` before every commit.** `make lint` is `vet` + `fmt-check`.

## Invariants (from the spec — each has a task)

| | Rule | Task |
|---|---|---|
| I1 | Computed labels are never stored on tasks; on merge, computed-ness wins and a stored assignment is inert | 5 |
| I2 | Cycles rejected at write **and** depth-guarded at read (merge can synthesize a cycle no replica wrote) | 3, 5 |
| I3 | A board name may not collide with a namespace name (`ATM:status` vs `ATM:status:*`) | 5 |
| I4 | Namespaces stay emergent — a descriptor is optional metadata, never a gate | 6 |
| I5 | A board cannot be a facet (it has no members) | 4 |

---

### Task 1: `Label.Expr` — field, name grammar, cache column

Adds the storage substrate. Nothing computes yet; this task only proves an expression string survives write → log → replay → cache rebuild.

**Files:**
- Modify: `internal/store/types.go:5-9` (Label struct)
- Modify: `internal/store/store.go:63-70` (`labelRe`, `ValidateLabelName`)
- Modify: `internal/store/cache.go:46-50` (schema), `:371-397` (upsert/get), `:401-428` (list)
- Test: `internal/store/label_test.go`, `internal/store/store_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `Label{Name, Description, Expr, LogSeq}`; `func IsNamespaceName(name string) bool`; `func (l Label) IsComputed() bool`.

- [ ] **Step 1: Write the failing tests**

In `internal/store/store_test.go`:

```go
func TestValidateLabelNameAcceptsNamespaceAndBoardNames(t *testing.T) {
	ok := []string{"ATM:stale", "ATM:next-sprint", "ATM:status:open", "ATM:status:*"}
	for _, n := range ok {
		if err := ValidateLabelName(n); err != nil {
			t.Errorf("ValidateLabelName(%q) = %v, want nil", n, err)
		}
	}
	bad := []string{"ATM:status:open:*", "ATM:*", "ATM:Status:open", "atm:status:open", "ATM"}
	for _, n := range bad {
		if err := ValidateLabelName(n); err == nil {
			t.Errorf("ValidateLabelName(%q) = nil, want error", n)
		}
	}
}

func TestIsNamespaceName(t *testing.T) {
	if !IsNamespaceName("ATM:status:*") {
		t.Error("ATM:status:* should be a namespace name")
	}
	if IsNamespaceName("ATM:status:open") || IsNamespaceName("ATM:next-sprint") {
		t.Error("non-:* names are not namespace names")
	}
}
```

In `internal/store/label_test.go`:

```go
func TestLabelExprSurvivesReplayAndRebuild(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	if err := s.LabelAdd("ATM:next-sprint", "the sprint board", "status:open AND sprint:next", testActor); err != nil {
		t.Fatalf("LabelAdd: %v", err)
	}
	if _, err := s.Rebuild(); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	got, err := s.LabelShow("ATM:next-sprint")
	if err != nil {
		t.Fatalf("LabelShow: %v", err)
	}
	if got.Expr != "status:open AND sprint:next" {
		t.Fatalf("Expr = %q, want it to survive rebuild", got.Expr)
	}
	if !got.IsComputed() {
		t.Error("label with an Expr must report IsComputed")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/store/ -run 'TestValidateLabelName|TestIsNamespaceName|TestLabelExpr' -v`
Expected: FAIL — `ValidateLabelName` rejects `ATM:status:*`; `IsNamespaceName` undefined; `LabelAdd` takes 3 args not 4.

- [ ] **Step 3: Add the field and the helpers**

`internal/store/types.go` — add `Expr` to `Label`:

```go
type Label struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Expr, when non-empty, makes this a computed label (a "board"): its
	// membership is derived by evaluating the expression over other labels
	// rather than asserted by tasks. See docs/superpowers/specs/2026-07-13-computed-labels-boards-design.md
	Expr   string `json:"expr,omitempty"`
	LogSeq int    `json:"log_seq,omitempty"`
}

// IsComputed reports whether membership is derived rather than asserted.
// True for boards (Expr set) and for namespace labels (name ends in ":*",
// whose expression is the prefix pattern implicit in the name).
func (l Label) IsComputed() bool { return l.Expr != "" || IsNamespaceName(l.Name) }
```

`internal/store/store.go` — widen the grammar and add the namespace predicate:

```go
var labelRe = regexp.MustCompile(`^[A-Z]{3,6}:[a-z0-9][a-z0-9-]*(:([a-z0-9][a-z0-9-]*|\*))?$`)

func ValidateLabelName(name string) error {
	if !labelRe.MatchString(name) {
		return fmt.Errorf("invalid label %q (want ^[A-Z]{3,6}:[a-z0-9][a-z0-9-]*(:([a-z0-9][a-z0-9-]*|\\*))?$)", name)
	}
	return nil
}

// IsNamespaceName reports whether name is a namespace label (e.g. "ATM:status:*"),
// whose membership is every label sharing its prefix.
func IsNamespaceName(name string) bool { return strings.HasSuffix(name, ":*") }
```

Keep the existing `labelRe` declaration site; replace only the pattern. Ensure `strings` is imported in `store.go`.

- [ ] **Step 4: Thread `expr` through the cache**

`internal/store/cache.go` — schema (line ~46):

```sql
CREATE TABLE IF NOT EXISTS labels (
	name TEXT PRIMARY KEY,
	description TEXT NOT NULL DEFAULT '',
	expr TEXT NOT NULL DEFAULT '',
	log_seq INTEGER NOT NULL DEFAULT 0
);
```

`CREATE TABLE IF NOT EXISTS` will not add the column to an existing `cache.db`, and there is no schema-version marker. Add a guarded migration immediately after the schema is executed (in the same function that runs `schemaSQL`):

```go
// The labels.expr column was added after the initial schema. CREATE TABLE
// IF NOT EXISTS will not add it to an existing cache.db, and the schema
// carries no version marker, so ALTER unconditionally and swallow the
// "duplicate column" error. cache.db is derived and rebuildable, so the
// worst case is always recoverable by deleting it and replaying the log.
if _, err := db.Exec(`ALTER TABLE labels ADD COLUMN expr TEXT NOT NULL DEFAULT ''`); err != nil &&
	!strings.Contains(err.Error(), "duplicate column name") {
	return err
}
```

Then thread `expr` through the three label cache functions:

```go
func cacheUpsertLabel(db *sql.DB, l Label) error {
	_, err := db.Exec(`INSERT INTO labels (name, description, expr, log_seq) VALUES (?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET description=excluded.description, expr=excluded.expr, log_seq=excluded.log_seq`,
		l.Name, l.Description, l.Expr, l.LogSeq)
	return err
}

func cacheGetLabel(db *sql.DB, name string) (Label, bool, error) {
	var l Label
	err := db.QueryRow(`SELECT name, description, expr, log_seq FROM labels WHERE name = ?`, name).
		Scan(&l.Name, &l.Description, &l.Expr, &l.LogSeq)
	if err == sql.ErrNoRows {
		return Label{}, false, nil
	}
	if err != nil {
		return Label{}, false, err
	}
	return l, true, nil
}
```

In `cacheListLabels`, change the SELECT list to `SELECT name, description, expr, log_seq FROM labels WHERE 1=1` and the scan to `rows.Scan(&l.Name, &l.Description, &l.Expr, &l.LogSeq)`.

No change is needed in `internal/store/log.go` — `label.upserted` marshals the whole `Label` (`label.go:45-51`) and replay unmarshals it whole (`log.go:380-389`), so `Expr` round-trips for free.

- [ ] **Step 5: Give `LabelAdd` and `LabelSeed` an `expr` parameter**

`internal/store/label.go` — change the signature and preserve-on-empty logic. Mirror exactly what the existing code does for `description`:

```go
func (s *Store) LabelAdd(name, description, expr, actor string) error {
	// ... unchanged validation ...
	return s.WithLock(code, func() error {
		l := Label{Name: name, Description: description, Expr: expr}
		if description == "" || expr == "" {
			if existing, ok, err := cacheGetLabel(db, name); err != nil {
				return err
			} else if ok {
				if description == "" {
					l.Description = existing.Description
				}
				if expr == "" {
					l.Expr = existing.Expr
				}
			}
		}
		// ... unchanged append + cacheUpsertLabel(db, l) ...
	})
}
```

`LabelSeed` gains an `expr` parameter too, set on the `Label` literal in both places it constructs one. `seedLabelsLocked` passes `l.Expr` from the seed entry (added in Task 6; pass `""` for now).

Update every existing caller to pass `""` for `expr`. Find them with:
`grep -rn "LabelAdd(\|LabelSeed(" --include=*.go .`

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestValidateLabelName|TestIsNamespaceName|TestLabelExpr' -v`
Expected: PASS

Run: `make lint test`
Expected: all green (existing callers updated in Step 5).

- [ ] **Step 7: Commit**

```bash
git add internal/store/
git commit -m "feat(store): add Label.Expr and widen label name grammar for namespaces"
```

---

### Task 2: Expression parser

A self-contained lexer + recursive-descent parser producing an AST. No store access — pure function from string to AST. This is the one piece a reviewer can reject entirely without touching anything else.

**Files:**
- Create: `internal/store/expr.go`
- Test: `internal/store/expr_test.go`

**Interfaces:**
- Consumes: nothing (pure).
- Produces:
  - `type Node interface{ isNode() }`
  - `type AtomNode struct{ Name string }` — a bare label name, project prefix *omitted* (e.g. `status:open`, `status:*`, `next-sprint`)
  - `type NotNode struct{ X Node }`
  - `type AndNode struct{ L, R Node }`
  - `type OrNode struct{ L, R Node }`
  - `func ParseExpr(src string) (Node, error)`
  - `func Atoms(n Node) []string` — every atom name in the tree, deduped, sorted. Task 3 and Task 5 both need this.

- [ ] **Step 1: Write the failing test**

`internal/store/expr_test.go`:

```go
package store

import "testing"

func TestParseExprPrecedenceAndAtoms(t *testing.T) {
	// NOT binds tighter than AND; AND binds tighter than OR.
	n, err := ParseExpr("a OR b AND NOT c")
	if err != nil {
		t.Fatalf("ParseExpr: %v", err)
	}
	or, ok := n.(*OrNode)
	if !ok {
		t.Fatalf("root = %T, want *OrNode (OR is lowest precedence)", n)
	}
	if _, ok := or.R.(*AndNode); !ok {
		t.Fatalf("or.R = %T, want *AndNode", or.R)
	}
	got := Atoms(n)
	want := []string{"a", "b", "c"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("Atoms = %v, want %v", got, want)
	}
}

func TestParseExprParensOverridePrecedence(t *testing.T) {
	n, err := ParseExpr("(a OR b) AND c")
	if err != nil {
		t.Fatalf("ParseExpr: %v", err)
	}
	if _, ok := n.(*AndNode); !ok {
		t.Fatalf("root = %T, want *AndNode", n)
	}
}

func TestParseExprAtomForms(t *testing.T) {
	// stored label, namespace predicate, board reference
	for _, src := range []string{"status:open", "status:*", "next-sprint"} {
		n, err := ParseExpr(src)
		if err != nil {
			t.Fatalf("ParseExpr(%q): %v", src, err)
		}
		a, ok := n.(*AtomNode)
		if !ok || a.Name != src {
			t.Fatalf("ParseExpr(%q) = %#v, want AtomNode{%q}", src, n, src)
		}
	}
}

func TestParseExprRejectsMalformed(t *testing.T) {
	bad := []string{"", "  ", "AND a", "a AND", "(a", "a)", "a b", "NOT", "a OR OR b"}
	for _, src := range bad {
		if _, err := ParseExpr(src); err == nil {
			t.Errorf("ParseExpr(%q) = nil error, want error", src)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/store/ -run TestParseExpr -v`
Expected: FAIL — `undefined: ParseExpr`, `undefined: OrNode`, …

- [ ] **Step 3: Implement the parser**

`internal/store/expr.go`:

```go
package store

import (
	"fmt"
	"sort"
	"strings"
)

// Node is one node of a parsed board expression. The grammar, lowest
// precedence first:
//
//	or   := and ("OR" and)*
//	and  := not ("AND" not)*
//	not  := "NOT" not | atom
//	atom := NAME | "(" or ")"
//
// NAME is a label name with the project prefix omitted: a stored label
// ("status:open"), a namespace predicate ("status:*"), or a board
// reference ("next-sprint"). Which one it is, is decided at resolve time
// by looking the name up — not by its syntax. See resolve.go.
type Node interface{ isNode() }

type AtomNode struct{ Name string }
type NotNode struct{ X Node }
type AndNode struct{ L, R Node }
type OrNode struct{ L, R Node }

func (*AtomNode) isNode() {}
func (*NotNode) isNode()  {}
func (*AndNode) isNode()  {}
func (*OrNode) isNode()   {}

// ParseExpr parses a board expression. Operators are case-sensitive
// (AND/OR/NOT) so they cannot collide with label names, which the label
// grammar constrains to lowercase.
func ParseExpr(src string) (Node, error) {
	toks, err := lexExpr(src)
	if err != nil {
		return nil, err
	}
	p := &exprParser{toks: toks}
	n, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if !p.done() {
		return nil, fmt.Errorf("unexpected %q after expression", p.peek())
	}
	return n, nil
}

// Atoms returns every atom name in the tree, deduped and sorted.
func Atoms(n Node) []string {
	seen := map[string]bool{}
	var walk func(Node)
	walk = func(n Node) {
		switch t := n.(type) {
		case *AtomNode:
			seen[t.Name] = true
		case *NotNode:
			walk(t.X)
		case *AndNode:
			walk(t.L)
			walk(t.R)
		case *OrNode:
			walk(t.L)
			walk(t.R)
		}
	}
	walk(n)
	out := make([]string, 0, len(seen))
	for a := range seen {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

func lexExpr(src string) ([]string, error) {
	var toks []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	for _, r := range src {
		switch {
		case r == '(' || r == ')':
			flush()
			toks = append(toks, string(r))
		case r == ' ' || r == '\t' || r == '\n':
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	if len(toks) == 0 {
		return nil, fmt.Errorf("empty expression")
	}
	return toks, nil
}

type exprParser struct {
	toks []string
	i    int
}

func (p *exprParser) done() bool  { return p.i >= len(p.toks) }
func (p *exprParser) peek() string {
	if p.done() {
		return ""
	}
	return p.toks[p.i]
}
func (p *exprParser) next() string {
	t := p.peek()
	p.i++
	return t
}

func (p *exprParser) parseOr() (Node, error) {
	l, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek() == "OR" {
		p.next()
		r, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		l = &OrNode{L: l, R: r}
	}
	return l, nil
}

func (p *exprParser) parseAnd() (Node, error) {
	l, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.peek() == "AND" {
		p.next()
		r, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		l = &AndNode{L: l, R: r}
	}
	return l, nil
}

func (p *exprParser) parseNot() (Node, error) {
	if p.peek() == "NOT" {
		p.next()
		x, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &NotNode{X: x}, nil
	}
	return p.parseAtom()
}

func (p *exprParser) parseAtom() (Node, error) {
	if p.done() {
		return nil, fmt.Errorf("unexpected end of expression")
	}
	t := p.next()
	if t == "(" {
		n, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.peek() != ")" {
			return nil, fmt.Errorf("missing closing paren")
		}
		p.next()
		return n, nil
	}
	if t == ")" || t == "AND" || t == "OR" || t == "NOT" {
		return nil, fmt.Errorf("unexpected %q", t)
	}
	return &AtomNode{Name: t}, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/store/ -run TestParseExpr -v`
Expected: PASS (all four)

- [ ] **Step 5: Commit**

```bash
git add internal/store/expr.go internal/store/expr_test.go
git commit -m "feat(store): add board expression parser (AND/OR/NOT/parens)"
```

---

### Task 3: Resolver — evaluate an expression against a task, with the cycle guard

Turns an AST into a predicate. The visited-set guard is the load-bearing piece: a cycle can arrive from a *merge* even though the write path rejects one (see I2 / ATM-0105-c0004), so it must be caught here, not only at write time.

**Files:**
- Create: `internal/store/resolve.go`
- Test: `internal/store/resolve_test.go`

**Interfaces:**
- Consumes: `Node`, `AtomNode`, `NotNode`, `AndNode`, `OrNode`, `ParseExpr` (Task 2); `Label.Expr`, `IsNamespaceName` (Task 1).
- Produces:
  - `type labelSet map[string]Label` — every label in a project, keyed by full name.
  - `func newResolver(code string, labels []Label) *resolver`
  - `func (r *resolver) Matches(t *Task, n Node) (bool, error)`
  - `var ErrCyclicExpr = errors.New("cyclic board expression")`

- [ ] **Step 1: Write the failing test**

`internal/store/resolve_test.go`:

```go
package store

import (
	"errors"
	"testing"
)

func resolverFor(labels ...Label) *resolver {
	return newResolver("ATM", labels)
}

func TestResolverAtomForms(t *testing.T) {
	r := resolverFor(
		Label{Name: "ATM:status:open"},
		Label{Name: "ATM:sprint:next"},
	)
	task := &Task{ID: "ATM-0001", Labels: []string{"ATM:status:open", "ATM:sprint:next"}}

	cases := map[string]bool{
		"status:open":            true,  // stored label, present
		"status:done":            false, // stored label, absent
		"status:*":               true,  // namespace predicate: has SOME status label
		"priority:*":             false, // namespace predicate: has NO priority label
		"NOT priority:*":         true,  // "unprioritized"
		"status:open AND NOT priority:*": true,
		"status:done OR sprint:next":     true,
	}
	for src, want := range cases {
		n, err := ParseExpr(src)
		if err != nil {
			t.Fatalf("ParseExpr(%q): %v", src, err)
		}
		got, err := r.Matches(task, n)
		if err != nil {
			t.Fatalf("Matches(%q): %v", src, err)
		}
		if got != want {
			t.Errorf("Matches(%q) = %v, want %v", src, got, want)
		}
	}
}

func TestResolverComposesBoards(t *testing.T) {
	// release-blockers references release-v1.0.0, which is itself a board.
	r := resolverFor(
		Label{Name: "ATM:release-v1.0.0", Expr: "release:v1-0-0 AND NOT status:done"},
		Label{Name: "ATM:release-blockers", Expr: "release-v1.0.0 AND priority:high"},
	)
	blocker := &Task{ID: "ATM-0001", Labels: []string{"ATM:release:v1-0-0", "ATM:priority:high"}}
	shipped := &Task{ID: "ATM-0002", Labels: []string{"ATM:release:v1-0-0", "ATM:priority:high", "ATM:status:done"}}

	n, _ := ParseExpr("release-blockers")
	if got, err := r.Matches(blocker, n); err != nil || !got {
		t.Errorf("blocker: got %v (err %v), want true", got, err)
	}
	if got, err := r.Matches(shipped, n); err != nil || got {
		t.Errorf("shipped: got %v (err %v), want false — status:done excludes it", got, err)
	}
}

// A cycle cannot be produced through the write path (Task 5 rejects it), but
// a MERGE can synthesize one that no replica ever wrote: replica A points
// board a at b while replica B points b at a. See ATM-0105-c0004. So the
// resolver must catch it rather than recursing forever.
func TestResolverRejectsMergeInducedCycle(t *testing.T) {
	r := resolverFor(
		Label{Name: "ATM:a", Expr: "b"},
		Label{Name: "ATM:b", Expr: "a"},
	)
	n, _ := ParseExpr("a")
	_, err := r.Matches(&Task{ID: "ATM-0001"}, n)
	if !errors.Is(err, ErrCyclicExpr) {
		t.Fatalf("err = %v, want ErrCyclicExpr", err)
	}
}

func TestResolverUnknownAtomIsNotAMatch(t *testing.T) {
	// An atom naming no live label is simply absent — not an error. A label
	// removed while a board still references it must not break the board.
	r := resolverFor()
	n, _ := ParseExpr("ghost")
	got, err := r.Matches(&Task{ID: "ATM-0001"}, n)
	if err != nil || got {
		t.Fatalf("got %v (err %v), want false, nil", got, err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/store/ -run TestResolver -v`
Expected: FAIL — `undefined: newResolver`, `undefined: ErrCyclicExpr`

- [ ] **Step 3: Implement the resolver**

`internal/store/resolve.go`:

```go
package store

import (
	"errors"
	"fmt"
	"strings"
)

// ErrCyclicExpr is returned when board references form a cycle. Write-time
// validation rejects cycles (see LabelAdd), but a MERGE can synthesize one
// that no replica ever wrote — replica A points board a at b while replica B
// points b at a, both writes individually valid. See ATM-0105-c0004 and
// docs/eventsource/00-architecture.md D4. The guard below is what keeps that
// case from recursing forever.
var ErrCyclicExpr = errors.New("cyclic board expression")

// resolver evaluates board expressions against tasks for one project. It is
// built once per query and holds a memo, so a board referenced by several
// other boards is parsed once.
type resolver struct {
	code   string
	labels map[string]Label // full name -> label
	parsed map[string]Node  // full name -> parsed Expr (memo)
}

func newResolver(code string, labels []Label) *resolver {
	m := make(map[string]Label, len(labels))
	for _, l := range labels {
		m[l.Name] = l
	}
	return &resolver{code: code, labels: m, parsed: map[string]Node{}}
}

// qualify turns a bare atom name ("status:open") into a full label name
// ("ATM:status:open"). Atoms in an expression omit the project prefix.
func (r *resolver) qualify(atom string) string { return r.code + ":" + atom }

// Matches reports whether t satisfies n.
func (r *resolver) Matches(t *Task, n Node) (bool, error) {
	return r.eval(t, n, map[string]bool{})
}

func (r *resolver) eval(t *Task, n Node, visiting map[string]bool) (bool, error) {
	switch node := n.(type) {
	case *NotNode:
		v, err := r.eval(t, node.X, visiting)
		return !v, err
	case *AndNode:
		l, err := r.eval(t, node.L, visiting)
		if err != nil || !l {
			return false, err
		}
		return r.eval(t, node.R, visiting)
	case *OrNode:
		l, err := r.eval(t, node.L, visiting)
		if err != nil || l {
			return l, err
		}
		return r.eval(t, node.R, visiting)
	case *AtomNode:
		return r.evalAtom(t, node.Name, visiting)
	}
	return false, fmt.Errorf("unknown expression node %T", n)
}

// evalAtom resolves an atom BY LOOKUP, not by syntax. That is what lets a
// bare-tag stored label ("stale") and a board ("next-sprint") share one name
// space unambiguously: whichever the live label record says it is, it is.
func (r *resolver) evalAtom(t *Task, atom string, visiting map[string]bool) (bool, error) {
	full := r.qualify(atom)

	// Namespace predicate: task carries ANY label in the namespace.
	if IsNamespaceName(full) {
		prefix := strings.TrimSuffix(full, "*")
		for _, l := range t.Labels {
			if strings.HasPrefix(l, prefix) {
				return true, nil
			}
		}
		return false, nil
	}

	// Board: recurse into its expression, guarding against cycles.
	if l, ok := r.labels[full]; ok && l.Expr != "" {
		if visiting[full] {
			return false, fmt.Errorf("%w: %s", ErrCyclicExpr, full)
		}
		visiting[full] = true
		defer delete(visiting, full)

		n, ok := r.parsed[full]
		if !ok {
			var err error
			if n, err = ParseExpr(l.Expr); err != nil {
				return false, fmt.Errorf("board %s: %w", full, err)
			}
			r.parsed[full] = n
		}
		return r.eval(t, n, visiting)
	}

	// Stored label (or an atom naming no live label, which is simply absent).
	for _, l := range t.Labels {
		if l == full {
			return true, nil
		}
	}
	return false, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/store/ -run TestResolver -v`
Expected: PASS (all four)

- [ ] **Step 5: Commit**

```bash
git add internal/store/resolve.go internal/store/resolve_test.go
git commit -m "feat(store): resolve board expressions with a merge-cycle guard"
```

---

### Task 4: Query engine — `--expr` filtering, boards as `--label` values, I5

Wires the resolver into `ListTasks`/`GroupTasks`. Backward compatibility is the whole risk here: `--label a --label b` must keep meaning `a AND b`, and a bare wildcard in `Labels` must keep *not* restricting (`query.go:10-11` — `TestListTasksIgnoresWildcardTokensForScoping` guards this).

**Files:**
- Modify: `internal/store/query.go:8-12` (`QueryFilters`), `:19-55` (`ListTasks`), `:57-101` (`GroupTasks`)
- Test: `internal/store/query_test.go`

**Interfaces:**
- Consumes: `newResolver`, `(*resolver).Matches`, `ErrCyclicExpr` (Task 3); `ParseExpr` (Task 2); `Store.LabelList` (Task 1).
- Produces: `QueryFilters{Project, Labels, Expr}`; `ErrBoardNotAFacet`.

- [ ] **Step 1: Write the failing test**

Append to `internal/store/query_test.go`:

```go
func TestListTasksByExpr(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_, _ = s.CreateTask("ATM", "a", "", []string{"ATM:status:open", "ATM:priority:high"}, testActor)
	_, _ = s.CreateTask("ATM", "b", "", []string{"ATM:status:open"}, testActor)
	_, _ = s.CreateTask("ATM", "c", "", []string{"ATM:status:done", "ATM:priority:high"}, testActor)

	got := s.ListTasks(QueryFilters{Project: "ATM", Expr: "status:open AND priority:high"})
	if len(got) != 1 || got[0].Title != "a" {
		t.Fatalf("got %v, want [a]", got)
	}
}

func TestListTasksByExprNotNamespaceFindsUntriaged(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_, _ = s.CreateTask("ATM", "triaged", "", []string{"ATM:status:open"}, testActor)
	_, _ = s.CreateTask("ATM", "untriaged", "", []string{"ATM:type:bug"}, testActor)

	got := s.ListTasks(QueryFilters{Project: "ATM", Expr: "NOT status:*"})
	if len(got) != 1 || got[0].Title != "untriaged" {
		t.Fatalf("got %v, want [untriaged]", got)
	}
}

func TestListTasksByBoardUsedAsLabelValue(t *testing.T) {
	// The payoff: a board IS a label, so --label works with no new flag.
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_ = s.LabelAdd("ATM:next-sprint", "sprint board", "status:open AND sprint:next", testActor)
	_, _ = s.CreateTask("ATM", "in", "", []string{"ATM:status:open", "ATM:sprint:next"}, testActor)
	_, _ = s.CreateTask("ATM", "out", "", []string{"ATM:status:done", "ATM:sprint:next"}, testActor)

	got := s.ListTasks(QueryFilters{Project: "ATM", Labels: []string{"ATM:next-sprint"}})
	if len(got) != 1 || got[0].Title != "in" {
		t.Fatalf("got %v, want [in]", got)
	}
}

func TestGroupTasksRejectsBoardAsFacet(t *testing.T) {
	// I5: a board has no members, so faceting by one is meaningless.
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_ = s.LabelAdd("ATM:next-sprint", "sprint board", "status:open", testActor)
	_, _, err := s.GroupTasksErr(QueryFilters{Project: "ATM", Labels: []string{"ATM:next-sprint:*"}})
	if err == nil {
		t.Fatal("faceting by a board must error")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/store/ -run 'TestListTasksBy|TestGroupTasksRejects' -v`
Expected: FAIL — `unknown field Expr in QueryFilters`; `s.GroupTasksErr undefined`

- [ ] **Step 3: Extend `QueryFilters` and `ListTasks`**

`internal/store/query.go`:

```go
type QueryFilters struct {
	Project string
	// Labels AND-intersect; full label names. A name may be a board — a
	// computed label — in which case its expression is evaluated. Suffix
	// wildcards (e.g. "ATM:status:*", "ATM:*") declare facets and do NOT
	// restrict; see GroupTasks.
	Labels []string
	// Expr is an ad-hoc board expression (AND/OR/NOT/parens over bare label
	// names). Empty means no expression filter. ANDs with Labels.
	Expr string
}
```

`ListTasks` builds one resolver per call and evaluates. Restricting tokens that name a *computed* label go through the resolver; plain stored labels keep the existing fast path.

```go
func (s *Store) ListTasks(filters QueryFilters) []*Task {
	out, _ := s.listTasksErr(filters)
	return out
}

// listTasksErr is ListTasks plus the error, for callers that must surface a
// bad or cyclic expression instead of silently returning nothing.
func (s *Store) listTasksErr(filters QueryFilters) ([]*Task, error) {
	var codes []string
	if filters.Project != "" {
		codes = []string{filters.Project}
	} else {
		for _, p := range s.ListProjects() {
			codes = append(codes, p.Code)
		}
	}
	restricting := restrictingTokens(filters.Labels)
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	var out []*Task
	for _, code := range codes {
		tasks, err := cacheListTasksForProject(db, code)
		if err != nil {
			continue
		}
		r := newResolver(code, s.LabelList(code, ""))

		// Each restricting token becomes an atom; they AND together. A token
		// naming a board resolves through its expression, which is what makes
		// `--label ATM:next-sprint` work with no new flag.
		var nodes []Node
		for _, tok := range restricting {
			nodes = append(nodes, &AtomNode{Name: strings.TrimPrefix(tok, code+":")})
		}
		if filters.Expr != "" {
			n, err := ParseExpr(filters.Expr)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, n)
		}
		for _, t := range tasks {
			ok := true
			for _, n := range nodes {
				m, err := r.Matches(t, n)
				if err != nil {
					return nil, err
				}
				if !m {
					ok = false
					break
				}
			}
			if ok {
				out = append(out, t)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ci, ni, _ := ParseTaskID(out[i].ID)
		cj, nj, _ := ParseTaskID(out[j].ID)
		if ci != cj {
			return ci < cj
		}
		return ni < nj
	})
	return out, nil
}
```

Delete `taskMatchesLabels` — the resolver subsumes it. Keep `restrictingTokens`, `wildcardTokens`, `isWildcard`, and `labelMatchesWildcard` unchanged; `GroupTasks` still needs them.

- [ ] **Step 4: Add I5 to `GroupTasks`**

Keep `GroupTasks` as-is for compatibility and add an error-returning sibling. A wildcard token whose *base* names a board is rejected:

```go
var ErrBoardNotAFacet = errors.New("a board has no members and cannot be a facet")

func (s *Store) GroupTasks(filters QueryFilters) ([]LabelGroup, []*Task) {
	g, o, _ := s.GroupTasksErr(filters)
	return g, o
}

func (s *Store) GroupTasksErr(filters QueryFilters) ([]LabelGroup, []*Task, error) {
	// I5: faceting by a board is meaningless — it has no members.
	for _, w := range wildcardTokens(filters.Labels) {
		base := strings.TrimSuffix(w, ":*")
		if l, err := s.LabelShow(base); err == nil && l.Expr != "" {
			return nil, nil, fmt.Errorf("%w: %s", ErrBoardNotAFacet, base)
		}
	}
	inScope, err := s.listTasksErr(filters)
	if err != nil {
		return nil, nil, err
	}
	// ... existing bucketing body, unchanged, operating on inScope ...
}
```

Add `"errors"`, `"fmt"`, and `"strings"` to the imports of `query.go` as needed.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestListTasks|TestGroupTasks' -v`
Expected: PASS — including the pre-existing `TestListTasksANDIntersectsExactLabels` and `TestListTasksIgnoresWildcardTokensForScoping`, which must not regress.

Run: `make lint test`
Expected: all green.

- [ ] **Step 6: Commit**

```bash
git add internal/store/query.go internal/store/query_test.go
git commit -m "feat(store): filter tasks by board expression; boards work as --label values"
```

---

### Task 5: Write-path invariants — I1, I2, I3

The three rules that keep the substrate honest. Each is a rejection at write time with a clear error.

**Files:**
- Modify: `internal/store/label.go:20-58` (`LabelAdd`)
- Modify: `internal/store/task.go` (task label validation — find it with `grep -n "ValidateLabelName" internal/store/task.go`)
- Test: `internal/store/label_test.go`, `internal/store/task_test.go`

**Interfaces:**
- Consumes: `ParseExpr`, `Atoms` (Task 2); `ErrCyclicExpr` (Task 3); `Label.IsComputed`, `IsNamespaceName` (Task 1).
- Produces: `ErrComputedLabelOnTask`, `ErrBoardNameCollision`.

- [ ] **Step 1: Write the failing tests**

`internal/store/label_test.go`:

```go
func TestLabelAddRejectsInvalidExpr(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	if err := s.LabelAdd("ATM:broken", "d", "status:open AND", testActor); err == nil {
		t.Fatal("malformed expression must be rejected at write time")
	}
}

// I2, write half. (The read half — a cycle arriving from a merge — is
// guarded in resolve.go and tested by TestResolverRejectsMergeInducedCycle.)
func TestLabelAddRejectsCycle(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	if err := s.LabelAdd("ATM:a", "d", "status:open", testActor); err != nil {
		t.Fatalf("seed a: %v", err)
	}
	if err := s.LabelAdd("ATM:b", "d", "a", testActor); err != nil {
		t.Fatalf("seed b: %v", err)
	}
	// Now point a at b -> a -> b -> a.
	err := s.LabelAdd("ATM:a", "d", "b", testActor)
	if !errors.Is(err, ErrCyclicExpr) {
		t.Fatalf("err = %v, want ErrCyclicExpr", err)
	}
}

func TestLabelAddRejectsSelfReference(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	err := s.LabelAdd("ATM:loop", "d", "loop", testActor)
	if !errors.Is(err, ErrCyclicExpr) {
		t.Fatalf("err = %v, want ErrCyclicExpr", err)
	}
}

// I3: ATM:status and ATM:status:* are distinct strings but both display as
// "status" in the Boards pane.
func TestLabelAddRejectsBoardNameCollidingWithNamespace(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_, _ = s.CreateTask("ATM", "t", "", []string{"ATM:status:open"}, testActor)
	err := s.LabelAdd("ATM:status", "d", "priority:high", testActor)
	if !errors.Is(err, ErrBoardNameCollision) {
		t.Fatalf("err = %v, want ErrBoardNameCollision", err)
	}
}
```

`internal/store/task_test.go`:

```go
// I1: computed labels are never stored on tasks.
func TestCreateTaskRejectsComputedLabel(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_ = s.LabelAdd("ATM:next-sprint", "board", "status:open", testActor)

	if _, err := s.CreateTask("ATM", "t", "", []string{"ATM:next-sprint"}, testActor); !errors.Is(err, ErrComputedLabelOnTask) {
		t.Fatalf("board on task: err = %v, want ErrComputedLabelOnTask", err)
	}
	if _, err := s.CreateTask("ATM", "t2", "", []string{"ATM:status:*"}, testActor); !errors.Is(err, ErrComputedLabelOnTask) {
		t.Fatalf("namespace on task: err = %v, want ErrComputedLabelOnTask", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/store/ -run 'TestLabelAddRejects|TestCreateTaskRejectsComputed' -v`
Expected: FAIL — `undefined: ErrBoardNameCollision`, `undefined: ErrComputedLabelOnTask`; cycles currently accepted.

- [ ] **Step 3: Implement the three guards**

In `internal/store/label.go`, add the errors and a validation helper, then call it from `LabelAdd` after the existing `ValidateLabelName` / actor / project checks and *before* `WithLock`:

```go
var (
	// ErrComputedLabelOnTask: a task may only carry stored labels. A computed
	// label (a board, or a ":*" namespace) is derived, so asserting it on a
	// task would make the label mean two things at once — see conventions
	// rule 5, "one label, one meaning".
	ErrComputedLabelOnTask = errors.New("computed labels cannot be assigned to a task")
	// ErrBoardNameCollision: ATM:status and ATM:status:* are distinct strings
	// but both render as "status" in the Boards pane.
	ErrBoardNameCollision = errors.New("board name collides with a namespace name")
)

// validateExpr parses expr, rejects a name collision, and walks the board
// reference graph to reject cycles. Called on the write path. It is NOT the
// only cycle defence: a merge can synthesize a cycle no replica wrote, which
// is why resolve.go carries a visited-set guard too. See ATM-0105-c0004.
func (s *Store) validateExpr(name, expr string) error {
	code := labelProject(name)

	// I3 — a board may not shadow a namespace.
	for _, l := range s.LabelList(code, "") {
		if IsNamespaceName(l.Name) && strings.TrimSuffix(l.Name, ":*") == name {
			return fmt.Errorf("%w: %s vs %s", ErrBoardNameCollision, name, l.Name)
		}
	}

	n, err := ParseExpr(expr)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}

	// I2 (write half) — walk references depth-first from this label.
	live := map[string]Label{}
	for _, l := range s.LabelList(code, "") {
		live[l.Name] = l
	}
	live[name] = Label{Name: name, Expr: expr} // the label as it WOULD be

	visiting := map[string]bool{}
	var walk func(full string, node Node) error
	walk = func(full string, node Node) error {
		for _, atom := range Atoms(node) {
			ref := code + ":" + atom
			l, ok := live[ref]
			if !ok || l.Expr == "" {
				continue // stored label or namespace — a leaf, cannot cycle
			}
			if visiting[ref] {
				return fmt.Errorf("%w: %s", ErrCyclicExpr, ref)
			}
			visiting[ref] = true
			sub, err := ParseExpr(l.Expr)
			if err != nil {
				return fmt.Errorf("board %s: %w", ref, err)
			}
			if err := walk(ref, sub); err != nil {
				return err
			}
			delete(visiting, ref)
		}
		return nil
	}
	visiting[name] = true
	return walk(name, n)
}
```

Call it from `LabelAdd`, guarded so a plain (non-computed) label is unaffected:

```go
if expr != "" {
	if err := s.validateExpr(name, expr); err != nil {
		return err
	}
}
```

For **I1**, find where `CreateTask` validates supplied labels (`grep -n "ValidateLabelName\|labelProjectExistsLocked" internal/store/task.go`) and reject computed ones alongside the existing checks. The same guard belongs on the task-label-add path:

```go
// I1 — a task may only carry stored labels.
if IsNamespaceName(l) {
	return fmt.Errorf("%w: %s", ErrComputedLabelOnTask, l)
}
if lb, ok, err := cacheGetLabel(db, l); err != nil {
	return err
} else if ok && lb.Expr != "" {
	return fmt.Errorf("%w: %s", ErrComputedLabelOnTask, l)
}
```

Use `cacheGetLabel` rather than `s.LabelShow` on any path already holding the project lock — `LabelShow` does not take the lock, but keep the pattern consistent with the surrounding locked code and mirror `labelProjectExistsLocked`'s comment about re-entrancy.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestLabelAdd|TestCreateTask' -v`
Expected: PASS

Run: `make lint test`
Expected: all green.

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat(store): enforce board invariants (no computed labels on tasks, no cycles, no name collisions)"
```

---

### Task 6: Seed namespace descriptors

Gives namespaces a meaning record for the first time. Because `atm label seed` re-applies idempotently and adds defaults introduced in a release, existing projects back-fill with **no migration step** (I4: descriptors are optional metadata, not a gate — an undescribed namespace still works).

**Files:**
- Modify: `internal/seed/seed.go:17-49`
- Modify: `internal/store/label.go:98-135` (`SeedLabels`, `seedLabelsLocked` — pass `l.Expr`)
- Test: `internal/seed/seed_test.go`, `internal/store/label_test.go`

**Interfaces:**
- Consumes: `LabelSeed(name, description, expr, actor)` (Task 1).
- Produces: `seed.Label{Suffix, Description, Expr}`.

- [ ] **Step 1: Write the failing test**

`internal/store/label_test.go`:

```go
func TestSeedAddsNamespaceDescriptors(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	l, err := s.LabelShow("ATM:status:*")
	if err != nil {
		t.Fatalf("namespace descriptor must be seeded: %v", err)
	}
	if l.Description == "" {
		t.Error("seeded namespace descriptor must carry a description")
	}
	if !l.IsComputed() {
		t.Error("a namespace label is computed")
	}
}

// I4 — a namespace with no descriptor still works; the descriptor is
// optional metadata, never a gate.
func TestUnseededNamespaceStillUsable(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_, err := s.CreateTask("ATM", "t", "", []string{"ATM:sprint:next"}, testActor)
	if err != nil {
		t.Fatalf("using an undescribed namespace must work: %v", err)
	}
	got := s.ListTasks(QueryFilters{Project: "ATM", Expr: "sprint:*"})
	if len(got) != 1 {
		t.Fatalf("got %d tasks, want 1", len(got))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/store/ -run 'TestSeedAddsNamespace|TestUnseededNamespace' -v`
Expected: FAIL — `LabelShow("ATM:status:*")` returns `ErrNotFound`.

- [ ] **Step 3: Add `Expr` to the seed type and the namespace descriptors**

`internal/seed/seed.go`:

```go
type Label struct {
	Suffix      string
	Description string
	// Expr, when non-empty, seeds a computed label (a board). No default
	// board is seeded today; the field exists so the seed set can carry one.
	Expr string
}
```

Every existing entry keeps two fields; Go zero-fills `Expr`. Add the namespace descriptors to `Labels` — these are the labels whose *names* end in `:*`, so their expression is implicit and `Expr` stays empty:

```go
	{Suffix: "status:*", Description: "lifecycle state of a task; exactly one status label should be present"},
	{Suffix: "priority:*", Description: "optional urgency ranking; absent means default priority"},
	{Suffix: "context:*", Description: "index tasks whose description is the payload: agent directions, repos, docs, questions"},
	{Suffix: "comment:*", Description: "the kinds of narrative an agent writes on a task"},
```

Convert the existing positional entries (`{"status:open", "..."}`) to keyed form in the same edit so the struct stays readable with three fields.

Do **not** seed `type:*` — `type` is invented on demand (see the package comment at `seed.go:33-35`), so it should correctly surface as undescribed in the Boards pane until a human or agent explains it. That is the `⚠` signal working as designed.

`internal/store/label.go` — pass the expression through both seed paths:

```go
// in SeedLabels
if err := s.LabelSeed(full, l.Description, l.Expr, actor); err != nil {

// in seedLabelsLocked
Payload: mustMarshal(Label{Name: full, Description: l.Description, Expr: l.Expr}),
// ...
if err := cacheUpsertLabel(db, Label{Name: full, Description: l.Description, Expr: l.Expr, LogSeq: entry.Seq}); err != nil {
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/seed/ ./internal/store/ -run 'TestSeed|TestUnseeded' -v`
Expected: PASS

Run: `make lint test`
Expected: all green.

- [ ] **Step 5: Verify the back-fill on the real store**

Run: `atm label seed --project ATM && atm label list --project ATM | grep ':\*'`
Expected: `ATM:status:*`, `ATM:priority:*`, `ATM:context:*`, `ATM:comment:*` listed with descriptions; no `ATM:type:*`.

- [ ] **Step 6: Commit**

```bash
git add internal/seed/ internal/store/
git commit -m "feat(seed): give namespaces a meaning record via :* descriptors"
```

---

### Task 7: CLI — `atm label add --expr`, `atm task list --expr`

**Files:**
- Modify: `internal/cli/label.go`
- Modify: `internal/cli/task.go` (find the `list` subcommand: `grep -n "func.*taskList\|\"list\"" internal/cli/task.go`)
- Test: `internal/cli/label_test.go`, `internal/cli/task_test.go`

**Interfaces:**
- Consumes: `Store.LabelAdd(name, description, expr, actor)` (Task 5); `QueryFilters.Expr` (Task 4).
- Produces: nothing downstream.

- [ ] **Step 1: Write the failing test**

Follow the existing harness in `internal/cli/label_test.go` (see `harness_test.go` for how a CLI invocation is driven).

```go
func TestLabelAddWithExprCreatesBoard(t *testing.T) {
	h := newCLIHarness(t)
	h.run("project", "create", "--code", "ATM", "--name", "x")
	h.run("label", "add", "--project", "ATM", "--name", "ATM:next-sprint",
		"--description", "the sprint board", "--expr", "status:open AND sprint:next")

	out := h.run("label", "show", "--name", "ATM:next-sprint")
	if !strings.Contains(out, "status:open AND sprint:next") {
		t.Fatalf("label show must render the expression; got:\n%s", out)
	}
}

func TestLabelAddRejectsBadExpr(t *testing.T) {
	h := newCLIHarness(t)
	h.run("project", "create", "--code", "ATM", "--name", "x")
	_, err := h.runErr("label", "add", "--project", "ATM", "--name", "ATM:bad",
		"--description", "d", "--expr", "status:open AND")
	if err == nil {
		t.Fatal("a malformed expression must fail the command")
	}
}

func TestTaskListWithExpr(t *testing.T) {
	h := newCLIHarness(t)
	h.run("project", "create", "--code", "ATM", "--name", "x")
	h.run("task", "create", "--project", "ATM", "--title", "a", "--label", "ATM:status:open")
	h.run("task", "create", "--project", "ATM", "--title", "b", "--label", "ATM:status:done")

	out := h.run("task", "list", "--project", "ATM", "--expr", "NOT status:done")
	if !strings.Contains(out, "a") || strings.Contains(out, "b") {
		t.Fatalf("--expr must filter; got:\n%s", out)
	}
}
```

Match the harness's actual helper names — read `internal/cli/harness_test.go` first and adapt.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestLabelAddWith|TestLabelAddRejects|TestTaskListWithExpr' -v`
Expected: FAIL — `unknown flag: --expr`

- [ ] **Step 3: Add the flags**

In `internal/cli/label.go`, register `--expr` on the `add` subcommand and pass it to `LabelAdd`. Help text:

```
--expr string   board expression over labels (AND/OR/NOT/parens), e.g.
                'status:open AND (priority:high OR priority:critical)'.
                A label with an expression is a board: its membership is
                computed, and it cannot be assigned to a task.
```

Render `Expr` in `label show` and mark computed rows in `label list`.

In `internal/cli/task.go`, register `--expr` on `list` and set `QueryFilters.Expr`. Surface a parse or cycle error as a command error — do not silently return an empty list.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cli/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/
git commit -m "feat(cli): add --expr to label add and task list"
```

---

### Task 8: TUI — Boards pane (read-only)

The Labels pane becomes the Boards pane: a **flat** list of computed labels — boards and namespaces intermixed and indistinguishable by design. Boards select straight to tasks; namespaces expand into their member labels, preserving ATM-0111's drill-down as a component. Undescribed rows carry `⚠`, which is how the human curation loop (`atm conventions` rule 6) survives the revamp with no second pane.

Read `internal/tui/labels.go` and `internal/tui/labels_test.go` in full before starting — this task reshapes an existing pane rather than adding one.

**Files:**
- Modify: `internal/tui/labels.go`
- Modify: `internal/tui/keymap.go`, `internal/tui/help.go` (pane name and keys)
- Test: `internal/tui/labels_test.go`

**Interfaces:**
- Consumes: `Store.LabelList` (returns `Label` with `Expr`), `Label.IsComputed`, `IsNamespaceName`, `QueryFilters{Labels}` with a board name (Task 4).
- Produces: nothing downstream.

- [ ] **Step 1: Write the failing test**

```go
func TestBoardsPaneListsComputedLabelsFlat(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_ = s.LabelAdd("ATM:next-sprint", "the sprint board", "status:open", testActor)

	m := newBoardsModel(s, "ATM")
	m.refresh()

	rows := m.rowNames()
	// Boards and namespaces sit in ONE flat list, indistinguishable by design.
	if !contains(rows, "next-sprint") {
		t.Errorf("board missing from rows: %v", rows)
	}
	if !contains(rows, "status") {
		t.Errorf("namespace missing from rows: %v", rows)
	}
	// A board is not a namespace, so it must not appear as one.
	if contains(rows, "next-sprint:*") {
		t.Errorf("a board must not render as a namespace: %v", rows)
	}
}

func TestBoardsPaneFlagsUndescribedRows(t *testing.T) {
	// An agent invents a namespace without describing it -> the human's
	// review signal (conventions rule 6) appears in the pane automatically.
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_, _ = s.CreateTask("ATM", "t", "", []string{"ATM:sprint:next"}, testActor)

	m := newBoardsModel(s, "ATM")
	m.refresh()

	row := m.row("sprint")
	if !row.NeedsDescription {
		t.Error("an undescribed namespace must be flagged for human reconciliation")
	}
	row = m.row("status")
	if row.NeedsDescription {
		t.Error("a seeded namespace has a description and must not be flagged")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run TestBoardsPane -v`
Expected: FAIL — `undefined: newBoardsModel`

- [ ] **Step 3: Build the pane**

Rename `labelsModel` → `boardsModel` in `internal/tui/labels.go` (keep the file name; the pane's content changed, not its home). A row is:

```go
// boardRow is one row of the Boards pane: a computed label. Boards and
// namespaces render identically on purpose — the user should not have to
// know which is which. The only difference is that a namespace can expand.
type boardRow struct {
	Name             string // display name: "next-sprint" or "status"
	FullName         string // "ATM:next-sprint" or "ATM:status:*"
	Description      string
	Expr             string // empty for a namespace
	Count            int
	Expandable       bool // true for namespaces (they have members)
	NeedsDescription bool // renders the ⚠ — conventions rule 6
}
```

`refresh()` builds rows from `LabelList(code, "")`:
- every label with `Expr != ""` → a board row (`Name` = the segment after the project code; `Expandable: false`)
- every *emergent* namespace (derive from stored label names, as the pane does today) → a namespace row, joined to its `ATM:<ns>:*` descriptor label if one exists (that is where `Description` comes from; missing descriptor → `NeedsDescription: true`, `Description: ""`)

`Count` comes from the existing usage path (`Store.LabelUsageGrouped`) for namespaces, and from `len(ListTasks(QueryFilters{Project: code, Labels: []string{FullName}}))` for boards.

Selecting a board row filters the Tasks pane by `QueryFilters{Labels: []string{row.FullName}}` — no new query path, because a board is a label. Selecting/expanding a namespace row keeps ATM-0111's existing drill-down behaviour verbatim.

Render a board whose expression is invalid or cyclic (from `ListTasks` returning an error) as `⚠ broken` rather than as an empty list — an empty board and a broken board must not look the same.

Update the pane's title to `BOARDS` in `help.go` and any keymap label.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -v`
Expected: PASS — including the existing labels-pane tests, adapted to the new names.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/
git commit -m "feat(tui): turn the Labels pane into a flat Boards pane"
```

---

### Task 9: TUI — board editor with live validation

`[n]ew` / `[e]dit` open a form with name, description, and expression. The expression is parsed **as the user types**: an invalid or cyclic expression cannot be saved, and a valid one shows a live match count. The live count is what makes the expression language usable by someone who does not know the syntax.

**Files:**
- Modify: `internal/tui/labels.go` (key handling), `internal/tui/form.go` (the form)
- Test: `internal/tui/labels_test.go`

**Interfaces:**
- Consumes: `ParseExpr` (Task 2), `Store.LabelAdd(name, description, expr, actor)` (Task 5), `Store.ListTasks` (Task 4).
- Produces: nothing downstream.

- [ ] **Step 1: Write the failing test**

```go
func TestBoardEditorLiveValidation(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_, _ = s.CreateTask("ATM", "a", "", []string{"ATM:status:open"}, testActor)
	_, _ = s.CreateTask("ATM", "b", "", []string{"ATM:status:done"}, testActor)

	ed := newBoardEditor(s, "ATM")
	ed.SetExpr("status:open")
	if !ed.Valid() {
		t.Fatalf("valid expression reported invalid: %v", ed.Err())
	}
	if ed.MatchCount() != 1 {
		t.Errorf("MatchCount = %d, want 1", ed.MatchCount())
	}

	ed.SetExpr("status:open AND")
	if ed.Valid() {
		t.Error("malformed expression must be invalid")
	}
	if ed.CanSave() {
		t.Error("an invalid expression must not be saveable")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run TestBoardEditor -v`
Expected: FAIL — `undefined: newBoardEditor`

- [ ] **Step 3: Implement the editor**

Add to `internal/tui/labels.go` (or a new `internal/tui/board_editor.go` if `labels.go` is already large — check its length first and split if so):

```go
// boardEditor backs the [n]ew/[e]dit form. It re-validates on every
// keystroke so the user sees a match count as they type, and cannot save an
// expression that does not parse. This is what makes the expression language
// usable by someone who does not know its syntax.
type boardEditor struct {
	store *store.Store
	code  string

	Name, Description, expr string

	parsed store.Node
	err    error
	count  int
}

func newBoardEditor(s *store.Store, code string) *boardEditor {
	return &boardEditor{store: s, code: code}
}

func (e *boardEditor) SetExpr(src string) {
	e.expr = src
	e.parsed, e.err = store.ParseExpr(src)
	e.count = 0
	if e.err != nil {
		return
	}
	// A cyclic or otherwise unresolvable expression surfaces here, because
	// ListTasks evaluates it.
	tasks, err := e.store.ListTasksErr(store.QueryFilters{Project: e.code, Expr: src})
	if err != nil {
		e.err = err
		return
	}
	e.count = len(tasks)
}

func (e *boardEditor) Valid() bool      { return e.err == nil && e.expr != "" }
func (e *boardEditor) Err() error       { return e.err }
func (e *boardEditor) MatchCount() int  { return e.count }
func (e *boardEditor) CanSave() bool    { return e.Valid() && e.Name != "" }

func (e *boardEditor) Save(actor string) error {
	return e.store.LabelAdd(e.code+":"+e.Name, e.Description, e.expr, actor)
}
```

This needs `listTasksErr` exported as `ListTasksErr` — do that in `internal/store/query.go` (Task 4 created it unexported; export it and keep `ListTasks` as the swallowing wrapper).

Wire `[n]` and `[e]` in the pane's key handling to open the form, and render, below the expression field, either `✓ N tasks` or `✗ <error>`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -v`
Expected: PASS

Run: `make lint test`
Expected: all green.

- [ ] **Step 5: Drive the real TUI**

Run: `atm` and open the Boards pane. Create a board with `[n]`: type `status:open AND NOT type:chore`, watch the match count update as you type, break it (`status:open AND`) and confirm it will not save. Save it, confirm it appears in the flat list and filters the Tasks pane when selected.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/ internal/store/query.go
git commit -m "feat(tui): add a live-validated board editor to the Boards pane"
```

---

### Task 10: Documentation

**Files:**
- Modify: `internal/cli/conventions.go` (the `atm conventions` text)
- Modify: `README.md`
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Update `atm conventions`**

It currently says labels are the query substrate and that "the Labels tab is the human's review surface." Both statements need updating, and the code-of-conduct gains the board rules. Add a section:

```
## Boards (computed labels)

A label may carry an expression over other labels. Such a label is a BOARD:
its membership is computed, not asserted. `ATM:next-sprint` with the
expression `status:open AND sprint:next` matches every open task in the next
sprint, and nothing carries that label directly.

Three kinds of label:
- stored     ATM:status:open   tasks assert it
- namespace  ATM:status:*      any label with that prefix; emergent; describable
- board      ATM:next-sprint   has an expression; computed; cannot be assigned

Expressions use AND / OR / NOT / parentheses over bare label names (the
project prefix is implied). Boards may reference other boards. `NOT status:*`
means "carries no status label" — i.e. untriaged.

Agents: author boards for the groupings your project actually needs, and give
each one a description — the description is what the next agent reads to know
what the board means. Boards are how a human asks for "the release board"
without knowing the label vocabulary.

The Boards tab is the human's review surface. A namespace with no description
shows a warning there: an agent introduced it but did not explain why.
```

- [ ] **Step 2: Update `README.md`**

Add a short "Boards" subsection after the label description, with the `atm label add --expr` example and one `atm task list --label ATM:next-sprint` example.

- [ ] **Step 3: Update `CHANGELOG.md`**

Follow the existing entry format. Note the label-name grammar widening (`:*` now legal) and that `atm label seed` back-fills namespace descriptors into existing projects.

- [ ] **Step 4: Verify**

Run: `make lint test && atm conventions | head -40`
Expected: green; the Boards section renders.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/conventions.go README.md CHANGELOG.md
git commit -m "docs: document boards (computed labels)"
```

---

## Self-Review

**Spec coverage** — every section maps to a task: `Label.Expr` + name grammar + cache column → 1; grammar → 2; three atom forms, lookup-not-syntax resolution, board composition → 3; wildcard's two readings, `--expr`, boards as `--label` values → 4; I1/I2/I3 → 5; I5 → 4; I4 + seed → 6; CLI → 7; Boards pane, flat list, `⚠` → 8; live-validated authoring → 9; conventions/README/CHANGELOG → 10. The spec's "no new log action" finding is realized in Task 1 Step 4 (nothing to do in `log.go`).

**Type consistency** — `LabelAdd(name, description, expr, actor)` is used identically in Tasks 1, 5, 6, 7, 9. `newResolver(code, labels)` / `(*resolver).Matches(t, n)` are consistent between Tasks 3 and 4. `ParseExpr` / `Atoms` / `Node` are consistent between Tasks 2, 3, 5, 9. `listTasksErr` is created unexported in Task 4 and exported as `ListTasksErr` in Task 9 — this is called out explicitly in Task 9 Step 3 rather than left implicit.

**Known coupling** — Task 9 requires an export from Task 4's file. Task 8 renames `labelsModel` → `boardsModel`, which Task 9 then extends. Run 8 before 9.
