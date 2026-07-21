# workflow_ai Capability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `workflow_ai` capability (ATM-efebc0): the brainstorm→clarify→plan→ready cycle over a `stage:*` namespace, task links and plan tracking in the capability's metadata key, five boards, an Annotate cell, and a guide — per spec `docs/superpowers/specs/2026-07-21-workflow-ai-capability-design.md`.

**Architecture:** One new package `internal/capability/workflowai` mirroring `internal/capability/workflow`'s structure (vocabulary / recorder / reporter / annotate / guide / command). Labels carry only what boards select (`stage:*` five values, `wfai:revision` marker); all machine state is one versioned JSON payload under `Meta["workflow_ai"]` written blob-replace through `SetTaskCapabilityMeta`. Every invariant is a paved road maintained by verbs — the store enforces nothing.

**Tech Stack:** Go 1.25, cobra, the in-repo store (`internal/store`), the capability registry (`internal/capability`).

## Global Constraints

- **BLOCKED on ATM-2e64a5 (task metadata column) landing.** Tasks 1–2 compile today; Tasks 3–7 consume its contract: `core.Task.Meta map[string]string`, `core.TaskService.SetTaskCapabilityMeta(id, capability, payload, actor string) error` (empty payload deletes the key), `capability.Cell{Text string; Tone Tone}`, tones `ToneNeutral/ToneOK/ToneAttention/ToneStale`, interface method `Annotate(task core.Task) *Cell`, `Registry.Annotate(capName string, t core.Task) *Cell`. Task 3 opens with a drift check against the landed code.
- `CapabilityName = "workflow_ai"` is the registry name, the command mount, AND the metadata key — never spell the string elsewhere.
- Capability independence: never read or write `status:*`, `priority:*`, `context:*`, or any other capability's labels/metadata.
- Payloads: version field `v: 1`; unknown fields survive every read-modify-write; a payload with no owned-or-unknown fields left encodes to `""` (deletes the key). Verbs FAIL on a malformed payload (never overwrite state they cannot read); only Annotate degrades silently.
- The store enforces nothing: guards, exactly-one-stage, and marker maintenance all live in this package's verbs. Add-before-remove for label swaps (no transactions).
- Actor in tests: `admin@cli:unset`. Test stores are fresh `t.TempDir()` stores — NEVER run a dev build against `~/.config/atm` (schema-changed cache breaks the installed binary); smoke against a copy via `ATM_HOME`.
- Timestamps: `time.RFC3339` UTC; recorders take an injectable `Now func() time.Time` (nil → `time.Now`).
- Markdown files: no hard-wrapped prose (repo convention).

---

### Task 1: Package scaffold — stage constants and the payload codec

**Files:**
- Create: `internal/capability/workflowai/stage.go`
- Create: `internal/capability/workflowai/payload.go`
- Test: `internal/capability/workflowai/payload_test.go`

**Interfaces:**
- Consumes: nothing (pure package; no ATM-2e64a5 dependency).
- Produces: constants `CapabilityName`, `StageNamespace`, `MarkerNamespace`, `MarkerRevision`, `StageNew/StageBrainstormed/StageClarified/StagePlanned/StageImplementable/StageDone`, `PlanKindFile/PlanKindCommit/PlanKindEphemeral`; helpers `firstStageValue(labels []string, code string) string`, `containsString([]string, string) bool`; types `PlanRecord{Kind, Ref, RecordedAt, Actor string}`, `Demotion{At, By, Reason string}`, `Payload` with `DecodePayload(string) (*Payload, error)`, `(*Payload).Encode() (string, error)`, `Plan() *PlanRecord`, `SetPlan(PlanRecord)`, `ClearPlan()`, `RevisionOf() string`, `SetRevisionOf(string)`, `ClearRevisionOf()`, `RelatesTo() []string`, `AddRelatesTo(string) bool`, `RemoveRelatesTo(string) bool`, `SetDemoted(Demotion)`. All later tasks consume these exact names.

- [ ] **Step 1: Write the failing payload tests**

`internal/capability/workflowai/payload_test.go`:

```go
package workflowai

import (
	"encoding/json"
	"testing"
)

func TestPayloadEmptyDecodesAndEncodesEmpty(t *testing.T) {
	pl, err := DecodePayload("")
	if err != nil {
		t.Fatalf("DecodePayload(\"\"): %v", err)
	}
	out, err := pl.Encode()
	if err != nil || out != "" {
		t.Fatalf("Encode = %q, %v; want \"\" (empty payload deletes the key)", out, err)
	}
}

func TestPayloadPlanRoundTrip(t *testing.T) {
	pl, _ := DecodePayload("")
	pl.SetPlan(PlanRecord{Kind: PlanKindFile, Ref: "docs/p.md", RecordedAt: "2026-07-21T00:00:00Z", Actor: "a"})
	out, err := pl.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	back, err := DecodePayload(out)
	if err != nil {
		t.Fatalf("DecodePayload(%q): %v", out, err)
	}
	p := back.Plan()
	if p == nil || p.Kind != PlanKindFile || p.Ref != "docs/p.md" || p.RecordedAt != "2026-07-21T00:00:00Z" || p.Actor != "a" {
		t.Errorf("Plan = %+v", p)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if v, ok := m["v"].(float64); !ok || v != 1 {
		t.Errorf("v = %v, want 1", m["v"])
	}
}

func TestPayloadPreservesUnknownFields(t *testing.T) {
	in := `{"v":1,"future_field":{"x":1},"plan":{"kind":"file","ref":"docs/p.md","recorded_at":"t0","actor":"a"}}`
	pl, err := DecodePayload(in)
	if err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	pl.SetPlan(PlanRecord{Kind: PlanKindCommit, Ref: "abc123", RecordedAt: "t1", Actor: "b"})
	out, _ := pl.Encode()
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := m["future_field"].(map[string]any); !ok {
		t.Errorf("future_field lost: %v", out)
	}
	plan := m["plan"].(map[string]any)
	if plan["kind"] != "commit" || plan["ref"] != "abc123" {
		t.Errorf("plan not updated: %v", plan)
	}
}

func TestPayloadClearLastFieldEncodesEmpty(t *testing.T) {
	pl, _ := DecodePayload("")
	pl.SetPlan(PlanRecord{Kind: PlanKindEphemeral, Ref: "session x", RecordedAt: "t", Actor: "a"})
	pl.ClearPlan()
	if out, _ := pl.Encode(); out != "" {
		t.Errorf("Encode = %q, want \"\" after clearing the only field", out)
	}
}

func TestDecodePayloadMalformed(t *testing.T) {
	if _, err := DecodePayload("not json"); err == nil {
		t.Fatal("DecodePayload of malformed input must error (verbs never overwrite what they cannot read)")
	}
}

func TestRelatesToAddRemove(t *testing.T) {
	pl, _ := DecodePayload("")
	if !pl.AddRelatesTo("ATM-bbbbbb") {
		t.Error("first add should report true")
	}
	if pl.AddRelatesTo("ATM-bbbbbb") {
		t.Error("duplicate add should report false")
	}
	if got := pl.RelatesTo(); len(got) != 1 || got[0] != "ATM-bbbbbb" {
		t.Errorf("RelatesTo = %v", got)
	}
	if pl.RemoveRelatesTo("ATM-cccccc") {
		t.Error("removing an absent link should report false")
	}
	if !pl.RemoveRelatesTo("ATM-bbbbbb") {
		t.Error("removing the present link should report true")
	}
	if out, _ := pl.Encode(); out != "" {
		t.Errorf("Encode = %q, want \"\" after the list emptied (key removed, not [])", out)
	}
}

func TestRevisionOfAndDemoted(t *testing.T) {
	pl, _ := DecodePayload("")
	pl.SetRevisionOf("ATM-aaaaaa")
	pl.SetDemoted(Demotion{At: "t", By: "a", Reason: "plan lost"})
	out, _ := pl.Encode()
	back, _ := DecodePayload(out)
	if back.RevisionOf() != "ATM-aaaaaa" {
		t.Errorf("RevisionOf = %q", back.RevisionOf())
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(out), &m)
	d := m["demoted"].(map[string]any)
	if d["reason"] != "plan lost" || d["at"] != "t" || d["by"] != "a" {
		t.Errorf("demoted = %v", d)
	}
	back.ClearRevisionOf()
	if back.RevisionOf() != "" {
		t.Error("ClearRevisionOf did not clear")
	}
}

func TestFirstStageValue(t *testing.T) {
	labels := []string{"ATM:status:open", "ATM:stage:clarified", "ATM:wfai:revision"}
	if got := firstStageValue(labels, "ATM"); got != StageClarified {
		t.Errorf("firstStageValue = %q, want %q", got, StageClarified)
	}
	if got := firstStageValue([]string{"ATM:status:open"}, "ATM"); got != StageNew {
		t.Errorf("firstStageValue = %q, want StageNew", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/capability/workflowai/ -v 2>&1 | head -20`
Expected: FAIL to compile — `undefined: DecodePayload`, `undefined: PlanRecord`, etc.

- [ ] **Step 3: Write `stage.go`**

```go
// Package workflowai is the AI-native workflow capability: a
// brainstorm→clarify→plan→ready cycle over the stage:* namespace, task
// links and plan tracking in the capability's metadata key, and boards
// over the stage labels. It coexists with the workflow capability as an
// independent view: disjoint namespaces, no interplay. The store enforces
// nothing; every invariant here is a paved road maintained by the verbs
// (docs/superpowers/specs/2026-07-21-workflow-ai-capability-design.md).
package workflowai

import "strings"

// CapabilityName is the stable identifier: the registry name, the command
// mount, and the task metadata key are all this one string.
const CapabilityName = "workflow_ai"

// StageNamespace is the label namespace the stage ladder lives in.
const StageNamespace = "stage"

// MarkerNamespace holds the capability's marker labels.
const MarkerNamespace = "wfai"

// MarkerRevision is the marker value stamped on revision follow-ups so the
// revisions board can select them; the link itself lives in the payload.
const MarkerRevision = "revision"

// Stage values: the ladder new → brainstormed → clarified → planned →
// implementable → done. "New" is the ABSENCE of any stage:* label, not a
// stored label; StageNew is the sentinel guards and reporters use for it.
const (
	StageNew           = ""
	StageBrainstormed  = "brainstormed"
	StageClarified     = "clarified"
	StagePlanned       = "planned"
	StageImplementable = "implementable"
	StageDone          = "done"
)

// Plan locator kinds. Ephemeral is honest: a plan that lives in a
// conversation, unverifiable by construction and always at-risk.
const (
	PlanKindFile      = "file"
	PlanKindCommit    = "commit"
	PlanKindEphemeral = "ephemeral"
)

// firstStageValue returns the task's stage value or StageNew. On a
// hand-edited multi-stage task it reports the lexicographically first
// (the store returns labels sorted); Recorder verbs converge such tasks.
func firstStageValue(labels []string, code string) string {
	prefix := code + ":" + StageNamespace + ":"
	for _, l := range labels {
		if strings.HasPrefix(l, prefix) {
			return strings.TrimPrefix(l, prefix)
		}
	}
	return StageNew
}

func containsString(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Write `payload.go`**

```go
package workflowai

import (
	"encoding/json"
	"fmt"
)

// PlanRecord is the typed plan locator stored in the payload: where the
// task's implementation plan lives. Kind is one of the PlanKind constants.
type PlanRecord struct {
	Kind       string
	Ref        string
	RecordedAt string
	Actor      string
}

// Demotion is the breadcrumb of the most recent demote. The full history
// lives in the event log and the task's comment thread; the payload keeps
// only the latest.
type Demotion struct {
	At, By, Reason string
}

// Payload wraps the capability's JSON object under Meta["workflow_ai"].
// The source of truth is a generic map so UNKNOWN FIELDS SURVIVE every
// read-modify-write: an older binary must never destroy a newer binary's
// state (degrade-never-reject applied to ourselves). Typed accessors read
// and write only the fields this version owns.
type Payload struct {
	raw map[string]any
}

// DecodePayload parses a payload string; "" is a valid empty payload. A
// malformed payload is an ERROR — verbs fail rather than overwrite state
// they cannot read; only Annotate degrades silently.
func DecodePayload(s string) (*Payload, error) {
	if s == "" {
		return &Payload{raw: map[string]any{}}, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, fmt.Errorf("workflow_ai payload is not a JSON object (hand-repair needed): %w", err)
	}
	return &Payload{raw: m}, nil
}

// Encode serializes the payload, stamping the version. A payload with no
// field left besides the version encodes to "" — writing "" through
// SetTaskCapabilityMeta deletes the key, so presence of the key always
// means presence of state. json.Marshal of a map sorts keys: output is
// deterministic.
func (p *Payload) Encode() (string, error) {
	rest := 0
	for k := range p.raw {
		if k != "v" {
			rest++
		}
	}
	if rest == 0 {
		return "", nil
	}
	p.raw["v"] = 1
	b, err := json.Marshal(p.raw)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// Plan returns the recorded plan locator, or nil when none is recorded.
func (p *Payload) Plan() *PlanRecord {
	m, ok := p.raw["plan"].(map[string]any)
	if !ok {
		return nil
	}
	return &PlanRecord{Kind: str(m["kind"]), Ref: str(m["ref"]), RecordedAt: str(m["recorded_at"]), Actor: str(m["actor"])}
}

func (p *Payload) SetPlan(r PlanRecord) {
	p.raw["plan"] = map[string]any{"kind": r.Kind, "ref": r.Ref, "recorded_at": r.RecordedAt, "actor": r.Actor}
}

func (p *Payload) ClearPlan() { delete(p.raw, "plan") }

// RevisionOf returns the parent task ID, or "" when this task is not a
// revision follow-up. At most one parent (spec §3).
func (p *Payload) RevisionOf() string { return str(p.raw["revision_of"]) }

func (p *Payload) SetRevisionOf(id string) { p.raw["revision_of"] = id }

func (p *Payload) ClearRevisionOf() { delete(p.raw, "revision_of") }

// RelatesTo returns the generic related-task list (order preserved).
func (p *Payload) RelatesTo() []string {
	arr, ok := p.raw["relates_to"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		out = append(out, str(v))
	}
	return out
}

// AddRelatesTo appends id, deduplicated; reports whether it was added.
func (p *Payload) AddRelatesTo(id string) bool {
	cur := p.RelatesTo()
	if containsString(cur, id) {
		return false
	}
	next := make([]any, 0, len(cur)+1)
	for _, v := range cur {
		next = append(next, v)
	}
	p.raw["relates_to"] = append(next, id)
	return true
}

// RemoveRelatesTo removes id; reports whether it was present. An emptied
// list removes the key entirely (never a stored []).
func (p *Payload) RemoveRelatesTo(id string) bool {
	cur := p.RelatesTo()
	if !containsString(cur, id) {
		return false
	}
	var next []any
	for _, v := range cur {
		if v != id {
			next = append(next, v)
		}
	}
	if len(next) == 0 {
		delete(p.raw, "relates_to")
	} else {
		p.raw["relates_to"] = next
	}
	return true
}

func (p *Payload) SetDemoted(d Demotion) {
	p.raw["demoted"] = map[string]any{"at": d.At, "by": d.By, "reason": d.Reason}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/capability/workflowai/ -v`
Expected: PASS (8 tests).

- [ ] **Step 6: Commit**

```bash
git add internal/capability/workflowai/
git commit -m "feat(ATM-efebc0): workflowai package scaffold — stage constants and payload codec"
```

---

### Task 2: Vocabulary and boards

**Files:**
- Create: `internal/capability/workflowai/vocabulary.go`
- Test: `internal/capability/workflowai/vocabulary_test.go`

**Interfaces:**
- Consumes: `core.Label`, `core.LabelService` (existing); Task 1 constants.
- Produces: `BoardNewTasks/BoardBrainstormedTasks/BoardPlannedTasks/BoardRevisions/BoardDoneTasks(code string) string`; `Vocabulary(code string) []core.Label`; `Exposed(code string) []core.Label`; `EnsureVocabulary(svc core.LabelService, code, actor string) ([]core.Label, error)`; test helper `newTestStore(t)` used by Tasks 3–6.

- [ ] **Step 1: Write the failing vocabulary tests**

`internal/capability/workflowai/vocabulary_test.go`:

```go
package workflowai

import (
	"path/filepath"
	"testing"

	"atm/internal/core"
	"atm/internal/store"
)

// newTestStore opens a fresh store with project ATM and this capability's
// vocabulary seeded. Every test in this package builds on it; nothing ever
// touches a real store.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "atm"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := s.CreateProject("ATM", "Atm", "admin@cli:unset"); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("ensure vocabulary: %v", err)
	}
	return s
}

func TestEnsureVocabularySeedsStageAndMarkerLabels(t *testing.T) {
	s := newTestStore(t)
	got := map[string]bool{}
	for _, l := range s.LabelList("ATM", "") {
		got[l.Name] = true
	}
	for _, want := range []string{
		"ATM:stage:*", "ATM:stage:brainstormed", "ATM:stage:clarified",
		"ATM:stage:planned", "ATM:stage:implementable", "ATM:stage:done",
		"ATM:wfai:*", "ATM:wfai:revision",
		"ATM:new-tasks", "ATM:brainstormed-tasks", "ATM:planned-tasks",
		"ATM:revisions", "ATM:done-tasks",
	} {
		if !got[want] {
			t.Errorf("missing label %s", want)
		}
	}
}

func TestEnsureVocabularyReturnsTheFiveBoards(t *testing.T) {
	s := newTestStore(t)
	boards, err := EnsureVocabulary(s, "ATM", "admin@cli:unset") // idempotent second run
	if err != nil {
		t.Fatalf("EnsureVocabulary: %v", err)
	}
	if len(boards) != 5 {
		t.Fatalf("boards = %d, want 5", len(boards))
	}
	for _, b := range boards {
		if b.Expr == "" {
			t.Errorf("board %s has empty expr", b.Name)
		}
		if _, err := core.ParseExpr(b.Expr); err != nil {
			t.Errorf("board %s expr %q does not parse: %v", b.Name, b.Expr, err)
		}
	}
}

func TestExposedIsSubsetOfVocabulary(t *testing.T) {
	vocab := map[string]bool{}
	for _, l := range Vocabulary("ATM") {
		vocab[l.Name] = true
	}
	for _, l := range Exposed("ATM") {
		if !vocab[l.Name] {
			t.Errorf("Exposed label %s not in Vocabulary", l.Name)
		}
	}
	if len(Exposed("ATM")) != 7 { // 5 boards + 2 namespace descriptors
		t.Errorf("Exposed = %d entries, want 7", len(Exposed("ATM")))
	}
}

func TestBoardsSelectByStage(t *testing.T) {
	s := newTestStore(t)
	actor := "admin@cli:unset"
	newTask, _ := s.CreateTask("ATM", "fresh", "", nil, actor)
	br, _ := s.CreateTask("ATM", "br", "", []string{"ATM:stage:brainstormed"}, actor)
	pl, _ := s.CreateTask("ATM", "pl", "", []string{"ATM:stage:planned"}, actor)
	rev, _ := s.CreateTask("ATM", "rev", "", []string{"ATM:stage:clarified", "ATM:wfai:revision"}, actor)
	done, _ := s.CreateTask("ATM", "dn", "", []string{"ATM:stage:done"}, actor)

	find := func(board string) map[string]bool {
		out := map[string]bool{}
		for _, tk := range s.ListTasks(store.QueryFilters{Project: "ATM", Labels: []string{board}}) {
			out[tk.ID] = true
		}
		return out
	}
	if got := find(BoardNewTasks("ATM")); !got[newTask.ID] || got[br.ID] {
		t.Errorf("new-tasks = %v", got)
	}
	if got := find(BoardBrainstormedTasks("ATM")); !got[br.ID] || !got[rev.ID] || got[pl.ID] {
		t.Errorf("brainstormed-tasks = %v (want brainstormed OR clarified)", got)
	}
	if got := find(BoardPlannedTasks("ATM")); !got[pl.ID] || got[done.ID] {
		t.Errorf("planned-tasks = %v", got)
	}
	if got := find(BoardRevisions("ATM")); !got[rev.ID] || got[br.ID] || got[done.ID] {
		t.Errorf("revisions = %v", got)
	}
	if got := find(BoardDoneTasks("ATM")); !got[done.ID] || got[pl.ID] {
		t.Errorf("done-tasks = %v", got)
	}
}
```

Note: `store.QueryFilters` — check the actual re-export; workflow's tests use `store.QueryFilters` via `internal/store/query_test.go` style. If the store package does not alias it, use `core.QueryFilters` (the `ListTasks` signature in `internal/core/service.go:18` takes `core.QueryFilters`).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/capability/workflowai/ -run 'Vocabulary|Exposed|Boards' -v 2>&1 | head -10`
Expected: FAIL to compile — `undefined: EnsureVocabulary`, `undefined: BoardNewTasks`.

- [ ] **Step 3: Write `vocabulary.go`**

```go
package workflowai

import "atm/internal/core"

// Board name helpers: callers select boards by name, never by expression.

// BoardNewTasks: tasks not yet brainstormed (no stage:* label at all).
func BoardNewTasks(code string) string { return code + ":new-tasks" }

// BoardBrainstormedTasks: tasks in refinement (brainstormed or clarified).
func BoardBrainstormedTasks(code string) string { return code + ":brainstormed-tasks" }

// BoardPlannedTasks: tasks with a recorded plan (planned or implementable).
func BoardPlannedTasks(code string) string { return code + ":planned-tasks" }

// BoardRevisions: open revision follow-ups of a bigger planned task.
func BoardRevisions(code string) string { return code + ":revisions" }

// BoardDoneTasks: tasks that completed the cycle.
func BoardDoneTasks(code string) string { return code + ":done-tasks" }

func newTasksExpr() string          { return "NOT stage:*" }
func brainstormedTasksExpr() string { return "stage:brainstormed OR stage:clarified" }
func plannedTasksExpr() string      { return "stage:planned OR stage:implementable" }
func revisionsExpr() string         { return "wfai:revision AND NOT stage:done" }
func doneTasksExpr() string         { return "stage:done" }

// vocabulary is the single literal list every contract method derives from:
// stored/namespace labels first (Expr == ""), then the five boards, in seed
// order. Ownership (Vocabulary), ring display (Exposed), and seeding
// (EnsureVocabulary) all read this list, so they cannot diverge.
func vocabulary(code string) []core.Label {
	return []core.Label{
		{Name: code + ":stage:*", Description: "workflow_ai cycle stage; at most one stage label per task, absence means new (not yet brainstormed)"},
		{Name: code + ":stage:brainstormed", Description: "workflow_ai stage: the idea has been brainstormed; requirements explored"},
		{Name: code + ":stage:clarified", Description: "workflow_ai stage: scope and success criteria are settled; ready to plan"},
		{Name: code + ":stage:planned", Description: "workflow_ai stage: a plan locator is recorded in the capability metadata"},
		{Name: code + ":stage:implementable", Description: "workflow_ai stage: planned AND sized for one implementation session; cleared for implementation"},
		{Name: code + ":stage:done", Description: "workflow_ai stage: completed the brainstorm→implement cycle"},
		{Name: code + ":wfai:*", Description: "workflow_ai markers; machine topology lives in the capability metadata, markers only make it board-visible"},
		{Name: code + ":wfai:revision", Description: "revision follow-up of a bigger planned task (revision_of link in workflow_ai metadata)"},
		{Name: BoardNewTasks(code), Description: "tasks not yet brainstormed: no stage label. The workflow_ai intake queue.", Expr: newTasksExpr()},
		{Name: BoardBrainstormedTasks(code), Description: "tasks in refinement: brainstormed or clarified.", Expr: brainstormedTasksExpr()},
		{Name: BoardPlannedTasks(code), Description: "tasks with a recorded plan: planned or implementable.", Expr: plannedTasksExpr()},
		{Name: BoardRevisions(code), Description: "open revision follow-ups of a bigger planned task, still needing refinement.", Expr: revisionsExpr()},
		{Name: BoardDoneTasks(code), Description: "tasks that completed the workflow_ai cycle.", Expr: doneTasksExpr()},
	}
}

// Vocabulary returns every label this capability owns for code. Pure.
func Vocabulary(code string) []core.Label { return vocabulary(code) }

// Exposed returns the labels this capability surfaces in the TUI ring, in
// preferred ring order: the five boards, then the two namespaces it owns.
func Exposed(code string) []core.Label {
	byName := map[string]core.Label{}
	for _, l := range vocabulary(code) {
		byName[l.Name] = l
	}
	names := []string{
		BoardNewTasks(code), BoardBrainstormedTasks(code), BoardPlannedTasks(code),
		BoardRevisions(code), BoardDoneTasks(code),
		code + ":stage:*", code + ":wfai:*",
	}
	out := make([]core.Label, 0, len(names))
	for _, n := range names {
		out = append(out, byName[n])
	}
	return out
}

// EnsureVocabulary seeds this capability's full vocabulary idempotently
// (LabelSeed upserts only when absent, so curated descriptions survive) and
// returns the five board labels it owns.
func EnsureVocabulary(s core.LabelService, code, actor string) ([]core.Label, error) {
	var boards []core.Label
	for _, l := range vocabulary(code) {
		if err := s.LabelSeed(l.Name, l.Description, l.Expr, actor); err != nil {
			return nil, err
		}
		if l.Expr != "" {
			boards = append(boards, l)
		}
	}
	return boards, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/capability/workflowai/ -v`
Expected: PASS. If `TestBoardsSelectByStage` fails on assigning `ATM:stage:brainstormed` at CreateTask (label validation), the vocabulary is seeded by the helper first, so stored labels exist; a failure here means the expr or seed list is wrong — fix those, don't weaken the test.

- [ ] **Step 5: Commit**

```bash
git add internal/capability/workflowai/
git commit -m "feat(ATM-efebc0): workflow_ai vocabulary — stage namespace, revision marker, five boards"
```

---

### Task 3: Stage recorder — the guarded ladder

**PRE-STEP (drift check):** ATM-2e64a5 must be merged before this task. Re-read the landed code and confirm: `core.TaskService` has `SetTaskCapabilityMeta(id, capability, payload, actor string) error`; `core.Task` has `Meta map[string]string`; writing `""` deletes the key. If names differ, update this plan file first, then proceed.

**Files:**
- Create: `internal/capability/workflowai/recorder.go`
- Test: `internal/capability/workflowai/recorder_test.go`

**Interfaces:**
- Consumes: Task 1 payload codec and constants; Task 2 `newTestStore`; ATM-2e64a5 `SetTaskCapabilityMeta` + `Task.Meta`; `core.TaskService`, `core.Comment`, `core.ParseTaskID`.
- Produces: `Service` interface (`core.TaskService` + `CreateComment`); `Recorder{Store Service; Actor string; Now func() time.Time}` with methods `Brainstorm/Clarify/Ready/Done(taskID) (string, error)`, `Plan(taskID, kind, ref string) (string, error)`, `Demote(taskID, reason string) (string, error)`. All return the PRIOR stage value (StageNew for new). Task 7's CLI consumes exactly these.

- [ ] **Step 1: Write the failing recorder tests**

`internal/capability/workflowai/recorder_test.go`:

```go
package workflowai

import (
	"strings"
	"testing"
	"time"

	"atm/internal/store"
)

const testActor = "admin@cli:unset"

func fixedNow() time.Time { return time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC) }

func newRecorder(s *store.Store) *Recorder {
	return &Recorder{Store: s, Actor: testActor, Now: fixedNow}
}

func stageLabelCount(t *testing.T, s *store.Store, id string) int {
	t.Helper()
	tk, err := s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	n := 0
	for _, l := range tk.Labels {
		if strings.HasPrefix(l, "ATM:stage:") {
			n++
		}
	}
	return n
}

func TestLadderHappyPath(t *testing.T) {
	s := newTestStore(t)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	r := newRecorder(s)

	steps := []struct {
		call func() (string, error)
		wantPrior, wantNow string
	}{
		{func() (string, error) { return r.Brainstorm(tk.ID) }, StageNew, StageBrainstormed},
		{func() (string, error) { return r.Clarify(tk.ID) }, StageBrainstormed, StageClarified},
		{func() (string, error) { return r.Plan(tk.ID, PlanKindFile, "docs/p.md") }, StageClarified, StagePlanned},
		{func() (string, error) { return r.Ready(tk.ID) }, StagePlanned, StageImplementable},
		{func() (string, error) { return r.Done(tk.ID) }, StageImplementable, StageDone},
	}
	for i, st := range steps {
		prior, err := st.call()
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if prior != st.wantPrior {
			t.Errorf("step %d prior = %q, want %q", i, prior, st.wantPrior)
		}
		got, _ := (&Reporter{Store: s}).Stage(tk.ID)
		if got != st.wantNow {
			t.Errorf("step %d stage = %q, want %q", i, got, st.wantNow)
		}
		if n := stageLabelCount(t, s, tk.ID); n != 1 {
			t.Errorf("step %d stage label count = %d, want 1", i, n)
		}
	}
}

func TestLadderRejectsSkippedRungs(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)

	cases := []struct {
		name string
		call func() (string, error)
		wantMsg string
	}{
		{"clarify from new", func() (string, error) { return r.Clarify(tk.ID) }, "clarify requires brainstormed"},
		{"plan from new", func() (string, error) { return r.Plan(tk.ID, PlanKindFile, "x") }, "plan requires clarified"},
		{"ready from new", func() (string, error) { return r.Ready(tk.ID) }, "no plan recorded"},
		{"done from new", func() (string, error) { return r.Done(tk.ID) }, "done requires implementable"},
	}
	for _, c := range cases {
		if _, err := c.call(); err == nil || !strings.Contains(err.Error(), c.wantMsg) {
			t.Errorf("%s: err = %v, want containing %q", c.name, err, c.wantMsg)
		}
	}
	// And a rung above: brainstorm on a staged task fails.
	tk2, _ := s.CreateTask("ATM", "t2", "", []string{"ATM:stage:clarified"}, testActor)
	if _, err := r.Brainstorm(tk2.ID); err == nil || !strings.Contains(err.Error(), "brainstorm requires a new task") {
		t.Errorf("brainstorm on clarified: err = %v", err)
	}
}

func TestVerbIsIdempotentNoOp(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:brainstormed"}, testActor)
	prior, err := r.Brainstorm(tk.ID)
	if err != nil {
		t.Fatalf("Brainstorm: %v", err)
	}
	if prior != StageBrainstormed {
		t.Errorf("prior = %q, want %q (no-op signals prior == target)", prior, StageBrainstormed)
	}
}

func TestSwapSelfHealsHandEditedMultiStage(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	// Hand-edited mess: brainstormed AND clarified at once. clarify's
	// predecessor (brainstormed) is present, so it proceeds and heals.
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:brainstormed", "ATM:stage:clarified"}, testActor)
	if _, err := r.Clarify(tk.ID); err != nil {
		t.Fatalf("Clarify: %v", err)
	}
	if n := stageLabelCount(t, s, tk.ID); n != 1 {
		t.Errorf("stage label count = %d, want 1 (self-healed)", n)
	}
	if got, _ := (&Reporter{Store: s}).Stage(tk.ID); got != StageClarified {
		t.Errorf("stage = %q, want %q", got, StageClarified)
	}
}

func TestPlanRecordsLocatorAndTransitions(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:clarified"}, testActor)
	if _, err := r.Plan(tk.ID, PlanKindFile, "docs/superpowers/plans/x.md"); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	got, _ := s.GetTask(tk.ID)
	pl, err := DecodePayload(got.Meta[CapabilityName])
	if err != nil {
		t.Fatalf("payload: %v", err)
	}
	p := pl.Plan()
	if p == nil || p.Kind != PlanKindFile || p.Ref != "docs/superpowers/plans/x.md" || p.Actor != testActor {
		t.Errorf("plan = %+v", p)
	}
	if p.RecordedAt != "2026-07-21T12:00:00Z" {
		t.Errorf("recorded_at = %q (injectable clock not used?)", p.RecordedAt)
	}
}

func TestPlanUpdatesInPlaceFromPlanned(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:clarified"}, testActor)
	_, _ = r.Plan(tk.ID, PlanKindEphemeral, "session 2026-07-20")
	prior, err := r.Plan(tk.ID, PlanKindFile, "docs/p.md") // re-plan: stage unchanged
	if err != nil {
		t.Fatalf("re-plan: %v", err)
	}
	if prior != StagePlanned {
		t.Errorf("prior = %q, want %q (update-in-place signals current stage)", prior, StagePlanned)
	}
	if got, _ := (&Reporter{Store: s}).Stage(tk.ID); got != StagePlanned {
		t.Errorf("stage = %q, want still planned", got)
	}
	got, _ := s.GetTask(tk.ID)
	pl, _ := DecodePayload(got.Meta[CapabilityName])
	if p := pl.Plan(); p == nil || p.Kind != PlanKindFile {
		t.Errorf("plan not updated: %+v", p)
	}
}

func TestPlanRejectsBadKind(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:clarified"}, testActor)
	if _, err := r.Plan(tk.ID, "url", "https://x"); err == nil || !strings.Contains(err.Error(), "invalid plan kind") {
		t.Errorf("err = %v", err)
	}
}

func TestReadyRequiresPlanRecord(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	// Hand-edited to planned WITHOUT a plan record.
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:planned"}, testActor)
	if _, err := r.Ready(tk.ID); err == nil || !strings.Contains(err.Error(), "no plan recorded") {
		t.Errorf("err = %v", err)
	}
}

func TestDemoteClearsStageAndPlanKeepsLinks(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	parent, _ := s.CreateTask("ATM", "parent", "", []string{"ATM:stage:planned"}, testActor)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:clarified"}, testActor)
	_, _ = r.Plan(tk.ID, PlanKindEphemeral, "session x")
	if err := r.LinkRevisionOf(tk.ID, parent.ID); err != nil {
		t.Fatalf("link: %v", err)
	}
	prior, err := r.Demote(tk.ID, "plan lost in session cleanup")
	if err != nil {
		t.Fatalf("Demote: %v", err)
	}
	if prior != StagePlanned {
		t.Errorf("prior = %q, want %q", prior, StagePlanned)
	}
	got, _ := s.GetTask(tk.ID)
	if n := stageLabelCount(t, s, tk.ID); n != 0 {
		t.Errorf("stage labels remain: %v", got.Labels)
	}
	pl, _ := DecodePayload(got.Meta[CapabilityName])
	if pl.Plan() != nil {
		t.Error("plan record survived demote")
	}
	if pl.RevisionOf() != parent.ID {
		t.Error("revision_of link did not survive demote")
	}
	if !containsString(got.Labels, "ATM:wfai:revision") {
		t.Error("revision marker did not survive demote")
	}
	comments, err := s.ListComments(tk.ID)
	if err != nil || len(comments) == 0 || !strings.Contains(comments[len(comments)-1].Body, "plan lost in session cleanup") {
		t.Errorf("demote reason comment missing: %v, %v", comments, err)
	}
}

func TestDemoteRequiresReason(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:brainstormed"}, testActor)
	if _, err := r.Demote(tk.ID, "  "); err == nil || !strings.Contains(err.Error(), "requires --reason") {
		t.Errorf("err = %v", err)
	}
}

func TestDemoteOfNewTaskIsNoOp(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	prior, err := r.Demote(tk.ID, "why not")
	if err != nil {
		t.Fatalf("Demote: %v", err)
	}
	if prior != StageNew {
		t.Errorf("prior = %q, want StageNew", prior)
	}
	got, _ := s.GetTask(tk.ID)
	if got.Meta[CapabilityName] != "" {
		t.Errorf("no-op demote wrote a payload: %q", got.Meta[CapabilityName])
	}
	if comments, _ := s.ListComments(tk.ID); len(comments) != 0 {
		t.Errorf("no-op demote wrote a comment")
	}
}

func TestVerbFailsOnMalformedPayload(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:clarified"}, testActor)
	if err := s.SetTaskCapabilityMeta(tk.ID, CapabilityName, "not json", testActor); err != nil {
		t.Fatalf("seed malformed payload: %v", err)
	}
	if _, err := r.Plan(tk.ID, PlanKindFile, "docs/p.md"); err == nil || !strings.Contains(err.Error(), "hand-repair") {
		t.Errorf("err = %v, want the hand-repair error (never overwrite unreadable state)", err)
	}
}
```

Note: `TestDemoteClearsStageAndPlanKeepsLinks` uses `LinkRevisionOf` from Task 4. If executing tasks strictly in order, comment out the link/marker assertions (the four lines using `parent` and the marker) and re-enable them in Task 4; if executing Tasks 3+4 together, keep as-is.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/capability/workflowai/ -run 'Ladder|Verb|Plan|Ready|Demote|Swap' -v 2>&1 | head -10`
Expected: FAIL to compile — `undefined: Recorder`, `undefined: Reporter`.

- [ ] **Step 3: Write `recorder.go`** (the `Reporter.Stage` referenced by tests arrives in Task 5; to keep this task self-contained, include the minimal `Reporter` struct + `Stage` here and grow it in Task 5)

```go
package workflowai

import (
	"fmt"
	"strings"
	"time"

	"atm/internal/core"
)

// Service is what the recorder needs from the store: task reads/writes,
// the capability metadata mutator (ATM-2e64a5), and comments for the
// demote audit trail. core.Service and *store.Store both satisfy it.
type Service interface {
	core.TaskService
	CreateComment(taskID, body string, labels []string, replyTo, actor string) (*core.Comment, error)
}

// Recorder is the mutating side of the workflow_ai capability. It maintains
// the exactly-one-stage invariant and the payload's plan/link/demotion
// state; the store itself enforces nothing (paved road, not a fence).
type Recorder struct {
	Store Service
	Actor string
	// Now overrides the timestamp source in tests; nil means time.Now.
	Now func() time.Time
}

func (r *Recorder) now() string {
	f := time.Now
	if r.Now != nil {
		f = r.Now
	}
	return f().UTC().Format(time.RFC3339)
}

// taskPayload reads the task and decodes this capability's payload. A
// malformed payload is an error — verbs never overwrite state they cannot
// read (hand-repair via the raw metadata surface instead).
func (r *Recorder) taskPayload(taskID string) (*core.Task, *Payload, error) {
	tk, err := r.Store.GetTask(taskID)
	if err != nil {
		return nil, nil, err
	}
	pl, err := DecodePayload(tk.Meta[CapabilityName])
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", taskID, err)
	}
	return tk, pl, nil
}

func (r *Recorder) writePayload(taskID string, pl *Payload) error {
	s, err := pl.Encode()
	if err != nil {
		return err
	}
	return r.Store.SetTaskCapabilityMeta(taskID, CapabilityName, s, r.Actor)
}

// stageState collects the task's stage labels: full names, values, whether
// target is among them, and the prior value for reporting (first non-target,
// lexicographic — the store returns labels sorted).
func stageState(tk *core.Task, code, target string) (existing []string, vals []string, hasTarget bool, prior string) {
	prefix := code + ":" + StageNamespace + ":"
	prior = StageNew
	for _, l := range tk.Labels {
		if !strings.HasPrefix(l, prefix) {
			continue
		}
		existing = append(existing, l)
		v := strings.TrimPrefix(l, prefix)
		vals = append(vals, v)
		if v == target {
			hasTarget = true
		} else if prior == StageNew {
			prior = v
		}
	}
	return
}

// setStage performs the guarded stage swap for verb: the transition to
// target is allowed only from the stages in from (StageNew means "no stage
// label"). Already exactly at target: idempotent no-op, zero store calls,
// prior == target. On a hand-edited multi-stage task whose set contains an
// allowed from-stage, the swap proceeds and self-heals: add target first,
// then remove every other stage label (no transactions; add-first bounds
// the worst case to a recoverable extra label, re-running converges).
func (r *Recorder) setStage(taskID, verb, target string, from ...string) (string, error) {
	tk, err := r.Store.GetTask(taskID)
	if err != nil {
		return "", err
	}
	code, _, ok := core.ParseTaskID(taskID)
	if !ok {
		return "", fmt.Errorf("invalid task id %q", taskID)
	}
	existing, vals, hasTarget, prior := stageState(tk, code, target)
	if len(existing) == 1 && hasTarget {
		return target, nil
	}
	allowed := false
	for _, f := range from {
		if f == StageNew {
			if len(existing) == 0 {
				allowed = true
			}
		} else if containsString(vals, f) {
			allowed = true
		}
	}
	if !allowed {
		current := "new"
		if len(vals) > 0 {
			current = strings.Join(vals, ", ")
		}
		return prior, fmt.Errorf("cannot %s %s: stage is %s (%s requires %s)", verb, taskID, current, verb, fromWords(from))
	}
	targetLabel := code + ":" + StageNamespace + ":" + target
	if !hasTarget {
		if err := r.Store.TaskLabelAdd(taskID, targetLabel, r.Actor); err != nil {
			return prior, fmt.Errorf("add %s: %w", targetLabel, err)
		}
	}
	for _, l := range existing {
		if l == targetLabel {
			continue
		}
		if err := r.Store.TaskLabelRemove(taskID, l, r.Actor); err != nil {
			return prior, fmt.Errorf("remove %s: %w", l, err)
		}
	}
	return prior, nil
}

func fromWords(from []string) string {
	out := make([]string, len(from))
	for i, f := range from {
		if f == StageNew {
			out[i] = "a new task"
		} else {
			out[i] = f
		}
	}
	return strings.Join(out, " or ")
}

// Brainstorm marks the idea explored: new → brainstormed.
func (r *Recorder) Brainstorm(taskID string) (string, error) {
	return r.setStage(taskID, "brainstorm", StageBrainstormed, StageNew)
}

// Clarify marks scope and success criteria settled: brainstormed → clarified.
func (r *Recorder) Clarify(taskID string) (string, error) {
	return r.setStage(taskID, "clarify", StageClarified, StageBrainstormed)
}

// Plan records the plan locator and, from clarified, advances to planned.
// From planned/implementable it UPDATES the locator in place (stage
// untouched) — a moved plan file or a re-planning pass. The payload is
// written before the label swap: a planned task must never lack a plan
// record; the recoverable direction is a leftover record on a still-
// clarified task. Returns the prior stage (== current stage on update).
func (r *Recorder) Plan(taskID, kind, ref string) (string, error) {
	switch kind {
	case PlanKindFile, PlanKindCommit, PlanKindEphemeral:
	default:
		return "", fmt.Errorf("invalid plan kind %q (want %s, %s or %s)", kind, PlanKindFile, PlanKindCommit, PlanKindEphemeral)
	}
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("plan requires a non-empty --ref")
	}
	tk, pl, err := r.taskPayload(taskID)
	if err != nil {
		return "", err
	}
	code, _, ok := core.ParseTaskID(taskID)
	if !ok {
		return "", fmt.Errorf("invalid task id %q", taskID)
	}
	_, vals, _, _ := stageState(tk, code, StagePlanned)
	update := containsString(vals, StagePlanned) || containsString(vals, StageImplementable)
	if !update && !containsString(vals, StageClarified) {
		current := "new"
		if len(vals) > 0 {
			current = strings.Join(vals, ", ")
		}
		return "", fmt.Errorf("cannot plan %s: stage is %s (plan requires clarified, or planned/implementable to update the locator)", taskID, current)
	}
	pl.SetPlan(PlanRecord{Kind: kind, Ref: ref, RecordedAt: r.now(), Actor: r.Actor})
	if err := r.writePayload(taskID, pl); err != nil {
		return "", err
	}
	if update {
		return firstStageValue(tk.Labels, code), nil
	}
	return r.setStage(taskID, "plan", StagePlanned, StageClarified)
}

// Ready clears the task for implementation: planned → implementable. Guard:
// a plan record must exist — "never implement an unplanned task" starts here.
func (r *Recorder) Ready(taskID string) (string, error) {
	_, pl, err := r.taskPayload(taskID)
	if err != nil {
		return "", err
	}
	if pl.Plan() == nil {
		return "", fmt.Errorf("cannot ready %s: no plan recorded (run `plan` first)", taskID)
	}
	return r.setStage(taskID, "ready", StageImplementable, StagePlanned)
}

// Done closes the cycle: implementable → done.
func (r *Recorder) Done(taskID string) (string, error) {
	return r.setStage(taskID, "done", StageDone, StageImplementable)
}

// Demote resets the task to new from any stage: clears the stage label(s)
// and the plan record, writes the demoted breadcrumb, and appends the
// reason as a task comment (audit trail). Links and the revision marker
// survive — topology is true regardless of stage. A task already new with
// no plan record is a pure no-op. Payload is written first, labels second,
// comment last: every partial-failure state converges on re-run.
func (r *Recorder) Demote(taskID, reason string) (string, error) {
	if strings.TrimSpace(reason) == "" {
		return "", fmt.Errorf("demote requires --reason")
	}
	tk, pl, err := r.taskPayload(taskID)
	if err != nil {
		return "", err
	}
	code, _, ok := core.ParseTaskID(taskID)
	if !ok {
		return "", fmt.Errorf("invalid task id %q", taskID)
	}
	existing, _, _, _ := stageState(tk, code, "\x00none")
	prior := firstStageValue(tk.Labels, code)
	if len(existing) == 0 && pl.Plan() == nil {
		return StageNew, nil
	}
	pl.ClearPlan()
	pl.SetDemoted(Demotion{At: r.now(), By: r.Actor, Reason: reason})
	if err := r.writePayload(taskID, pl); err != nil {
		return prior, err
	}
	for _, l := range existing {
		if err := r.Store.TaskLabelRemove(taskID, l, r.Actor); err != nil {
			return prior, fmt.Errorf("remove %s: %w", l, err)
		}
	}
	if _, err := r.Store.CreateComment(taskID, "workflow_ai: demoted to new — "+reason, nil, "", r.Actor); err != nil {
		return prior, fmt.Errorf("demoted, but recording the reason comment failed: %w", err)
	}
	return prior, nil
}
```

Also create a minimal `reporter.go` (grown in Task 5):

```go
package workflowai

import (
	"fmt"

	"atm/internal/core"
)

// Reporter is the read-only side of the workflow_ai capability. It never
// mutates the store — the reporter reports, the decider demotes.
type Reporter struct {
	Store core.TaskService
}

// Stage returns the task's stage value or StageNew ("") when the task
// carries no stage:* label. On a hand-edited multi-stage task it reports
// the lexicographically first value (store labels sort); Recorder verbs
// converge such tasks.
func (r *Reporter) Stage(taskID string) (string, error) {
	tk, err := r.Store.GetTask(taskID)
	if err != nil {
		return "", err
	}
	code, _, ok := core.ParseTaskID(taskID)
	if !ok {
		return "", fmt.Errorf("invalid task id %q", taskID)
	}
	return firstStageValue(tk.Labels, code), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/capability/workflowai/ -v`
Expected: PASS (with the Task-4-dependent assertions commented out if running strictly in order).

- [ ] **Step 5: Commit**

```bash
git add internal/capability/workflowai/
git commit -m "feat(ATM-efebc0): workflow_ai stage recorder — guarded ladder, plan locator, demote"
```

---

### Task 4: Link recorder — revision_of and relates_to

**Files:**
- Create: `internal/capability/workflowai/links.go`
- Test: `internal/capability/workflowai/links_test.go`

**Interfaces:**
- Consumes: Task 3 `Recorder` (methods attach to it), Task 1 payload accessors.
- Produces: `(*Recorder).LinkRevisionOf(childID, parentID string) error`, `UnlinkRevisionOf(childID, parentID string) error`, `LinkRelatesTo(taskID, otherID string) error`, `UnlinkRelatesTo(taskID, otherID string) error`. Task 7's CLI consumes exactly these.

- [ ] **Step 1: Write the failing link tests**

`internal/capability/workflowai/links_test.go`:

```go
package workflowai

import (
	"strings"
	"testing"
)

func TestLinkRevisionOfStampsMarkerAndPayload(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	parent, _ := s.CreateTask("ATM", "parent", "", []string{"ATM:stage:planned"}, testActor)
	child, _ := s.CreateTask("ATM", "child", "", nil, testActor)
	if err := r.LinkRevisionOf(child.ID, parent.ID); err != nil {
		t.Fatalf("LinkRevisionOf: %v", err)
	}
	got, _ := s.GetTask(child.ID)
	pl, _ := DecodePayload(got.Meta[CapabilityName])
	if pl.RevisionOf() != parent.ID {
		t.Errorf("revision_of = %q", pl.RevisionOf())
	}
	if !containsString(got.Labels, "ATM:wfai:revision") {
		t.Errorf("marker missing: %v", got.Labels)
	}
	// Idempotent for the same parent.
	if err := r.LinkRevisionOf(child.ID, parent.ID); err != nil {
		t.Fatalf("re-link same parent: %v", err)
	}
}

func TestLinkRevisionOfGuards(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	a, _ := s.CreateTask("ATM", "a", "", nil, testActor)
	b, _ := s.CreateTask("ATM", "b", "", nil, testActor)
	c, _ := s.CreateTask("ATM", "c", "", nil, testActor)

	if err := r.LinkRevisionOf(a.ID, a.ID); err == nil || !strings.Contains(err.Error(), "itself") {
		t.Errorf("self-link: %v", err)
	}
	if err := r.LinkRevisionOf(a.ID, "ATM-ffffff"); err == nil {
		t.Error("missing parent must fail")
	}
	if err := r.LinkRevisionOf(a.ID, b.ID); err != nil {
		t.Fatalf("link: %v", err)
	}
	if err := r.LinkRevisionOf(a.ID, c.ID); err == nil || !strings.Contains(err.Error(), "already a revision of") {
		t.Errorf("second parent: %v", err)
	}
	if err := r.LinkRevisionOf(b.ID, a.ID); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Errorf("direct cycle: %v", err)
	}
}

func TestUnlinkRevisionOfClearsMarkerAndPayload(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	parent, _ := s.CreateTask("ATM", "parent", "", nil, testActor)
	child, _ := s.CreateTask("ATM", "child", "", nil, testActor)
	_ = r.LinkRevisionOf(child.ID, parent.ID)

	if err := r.UnlinkRevisionOf(child.ID, "ATM-ffffff"); err == nil || !strings.Contains(err.Error(), "not") {
		t.Errorf("mismatched unlink: %v", err)
	}
	if err := r.UnlinkRevisionOf(child.ID, parent.ID); err != nil {
		t.Fatalf("UnlinkRevisionOf: %v", err)
	}
	got, _ := s.GetTask(child.ID)
	if containsString(got.Labels, "ATM:wfai:revision") {
		t.Error("marker survived unlink")
	}
	if got.Meta[CapabilityName] != "" {
		t.Errorf("payload should be empty after the only field cleared, got %q", got.Meta[CapabilityName])
	}
	if err := r.UnlinkRevisionOf(child.ID, parent.ID); err == nil || !strings.Contains(err.Error(), "no revision_of link") {
		t.Errorf("double unlink: %v", err)
	}
}

func TestRelatesToLinkUnlink(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	a, _ := s.CreateTask("ATM", "a", "", nil, testActor)
	b, _ := s.CreateTask("ATM", "b", "", nil, testActor)
	if err := r.LinkRelatesTo(a.ID, a.ID); err == nil || !strings.Contains(err.Error(), "itself") {
		t.Errorf("self relate: %v", err)
	}
	if err := r.LinkRelatesTo(a.ID, "ATM-ffffff"); err == nil {
		t.Error("missing target must fail")
	}
	if err := r.LinkRelatesTo(a.ID, b.ID); err != nil {
		t.Fatalf("LinkRelatesTo: %v", err)
	}
	if err := r.LinkRelatesTo(a.ID, b.ID); err != nil {
		t.Fatalf("duplicate relate must be a silent no-op: %v", err)
	}
	got, _ := s.GetTask(a.ID)
	pl, _ := DecodePayload(got.Meta[CapabilityName])
	if rt := pl.RelatesTo(); len(rt) != 1 || rt[0] != b.ID {
		t.Errorf("relates_to = %v", rt)
	}
	if err := r.UnlinkRelatesTo(a.ID, "ATM-ffffff"); err == nil || !strings.Contains(err.Error(), "no relates_to link") {
		t.Errorf("unlink absent: %v", err)
	}
	if err := r.UnlinkRelatesTo(a.ID, b.ID); err != nil {
		t.Fatalf("UnlinkRelatesTo: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/capability/workflowai/ -run 'Link|Unlink|Relates' -v 2>&1 | head -10`
Expected: FAIL to compile — `r.LinkRevisionOf undefined`.

- [ ] **Step 3: Write `links.go`**

```go
package workflowai

import (
	"fmt"

	"atm/internal/core"
)

// sameProject validates both IDs and requires one project: links never
// cross project boundaries.
func sameProject(aID, bID string) (code string, err error) {
	aCode, _, ok := core.ParseTaskID(aID)
	if !ok {
		return "", fmt.Errorf("invalid task id %q", aID)
	}
	bCode, _, ok := core.ParseTaskID(bID)
	if !ok {
		return "", fmt.Errorf("invalid task id %q", bID)
	}
	if aCode != bCode {
		return "", fmt.Errorf("cannot link across projects (%s vs %s)", aID, bID)
	}
	if aID == bID {
		return "", fmt.Errorf("cannot link %s to itself", aID)
	}
	return aCode, nil
}

// LinkRevisionOf records child as a revision follow-up of parent: the
// revision_of pointer in child's payload (machine topology) plus the
// wfai:revision marker label (board visibility). At most one parent; a
// direct two-node cycle is rejected, deeper cycles are a non-goal.
// Idempotent for the same parent (and re-ensures the marker).
func (r *Recorder) LinkRevisionOf(childID, parentID string) error {
	code, err := sameProject(childID, parentID)
	if err != nil {
		return err
	}
	tk, pl, err := r.taskPayload(childID)
	if err != nil {
		return err
	}
	if cur := pl.RevisionOf(); cur != "" && cur != parentID {
		return fmt.Errorf("%s is already a revision of %s (unlink first)", childID, cur)
	}
	_, parentPl, err := r.taskPayload(parentID) // also proves the parent exists
	if err != nil {
		return err
	}
	if parentPl.RevisionOf() == childID {
		return fmt.Errorf("cycle: %s is already a revision of %s", parentID, childID)
	}
	if pl.RevisionOf() != parentID {
		pl.SetRevisionOf(parentID)
		if err := r.writePayload(childID, pl); err != nil {
			return err
		}
	}
	marker := code + ":" + MarkerNamespace + ":" + MarkerRevision
	if !containsString(tk.Labels, marker) {
		if err := r.Store.TaskLabelAdd(childID, marker, r.Actor); err != nil {
			return fmt.Errorf("add %s: %w", marker, err)
		}
	}
	return nil
}

// UnlinkRevisionOf removes the revision_of link (parentID must match the
// stored parent — explicit beats accidental) and the marker label.
func (r *Recorder) UnlinkRevisionOf(childID, parentID string) error {
	tk, pl, err := r.taskPayload(childID)
	if err != nil {
		return err
	}
	cur := pl.RevisionOf()
	if cur == "" {
		return fmt.Errorf("%s has no revision_of link", childID)
	}
	if cur != parentID {
		return fmt.Errorf("%s is a revision of %s, not %s", childID, cur, parentID)
	}
	pl.ClearRevisionOf()
	if err := r.writePayload(childID, pl); err != nil {
		return err
	}
	code, _, _ := core.ParseTaskID(childID)
	marker := code + ":" + MarkerNamespace + ":" + MarkerRevision
	if containsString(tk.Labels, marker) {
		if err := r.Store.TaskLabelRemove(childID, marker, r.Actor); err != nil {
			return fmt.Errorf("remove %s: %w", marker, err)
		}
	}
	return nil
}

// LinkRelatesTo records a generic, semantics-free association. Stored
// one-directional on taskID; the links reporter surfaces both directions.
// Duplicate links are a silent no-op.
func (r *Recorder) LinkRelatesTo(taskID, otherID string) error {
	if _, err := sameProject(taskID, otherID); err != nil {
		return err
	}
	_, pl, err := r.taskPayload(taskID)
	if err != nil {
		return err
	}
	if _, err := r.Store.GetTask(otherID); err != nil {
		return err // the target must exist
	}
	if !pl.AddRelatesTo(otherID) {
		return nil
	}
	return r.writePayload(taskID, pl)
}

// UnlinkRelatesTo removes the association; unlinking an absent link is an
// error (it usually means a typo'd ID).
func (r *Recorder) UnlinkRelatesTo(taskID, otherID string) error {
	_, pl, err := r.taskPayload(taskID)
	if err != nil {
		return err
	}
	if !pl.RemoveRelatesTo(otherID) {
		return fmt.Errorf("%s has no relates_to link to %s", taskID, otherID)
	}
	return r.writePayload(taskID, pl)
}
```

- [ ] **Step 4: Run tests to verify they pass** (re-enable the Task 3 demote link assertions if they were commented out)

Run: `go test ./internal/capability/workflowai/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/capability/workflowai/
git commit -m "feat(ATM-efebc0): workflow_ai link verbs — revision_of with marker label, generic relates_to"
```

---

### Task 5: Reporters — plan-staleness check and links view

**Files:**
- Modify: `internal/capability/workflowai/reporter.go` (grow the Task 3 stub)
- Test: `internal/capability/workflowai/reporter_test.go`

**Interfaces:**
- Consumes: Tasks 1–4; `core.QueryFilters`, `ListTasksErr`.
- Produces: `Finding{TaskID, Stage, Detail string}` (json tags `task`, `stage`, `detail`); `PlanVerifier func(kind, ref string) (ok bool, detail string)`; `(*Reporter).PlanCheck(code string, verify PlanVerifier) ([]Finding, int, error)`; `DefaultVerifier(dir string) PlanVerifier`; `TaskLinks{RevisionOf string; RelatesTo, Revisions, RelatedFrom []string}`; `(*Reporter).Links(taskID string) (*TaskLinks, error)`. Task 7's CLI consumes exactly these.

- [ ] **Step 1: Write the failing reporter tests**

`internal/capability/workflowai/reporter_test.go`:

```go
package workflowai

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanCheckFindings(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)

	mkPlanned := func(title, kind, ref string) string {
		tk, _ := s.CreateTask("ATM", title, "", []string{"ATM:stage:clarified"}, testActor)
		if _, err := r.Plan(tk.ID, kind, ref); err != nil {
			t.Fatalf("plan %s: %v", title, err)
		}
		return tk.ID
	}
	good := mkPlanned("good", PlanKindFile, "docs/good.md")
	missing := mkPlanned("missing", PlanKindFile, "docs/gone.md")
	eph := mkPlanned("eph", PlanKindEphemeral, "session 2026-07-14")
	// Hand-edited to planned with NO record at all.
	bare, _ := s.CreateTask("ATM", "bare", "", []string{"ATM:stage:planned"}, testActor)
	// Malformed payload on a planned task.
	bad, _ := s.CreateTask("ATM", "bad", "", []string{"ATM:stage:planned"}, testActor)
	_ = s.SetTaskCapabilityMeta(bad.ID, CapabilityName, "not json", testActor)
	// A new task with no stage is out of scope entirely.
	_, _ = s.CreateTask("ATM", "outofscope", "", nil, testActor)

	verify := func(kind, ref string) (bool, string) {
		if ref == "docs/good.md" {
			return true, ""
		}
		return false, "plan file missing: " + ref
	}
	findings, healthy, err := (&Reporter{Store: s}).PlanCheck("ATM", verify)
	if err != nil {
		t.Fatalf("PlanCheck: %v", err)
	}
	if healthy != 1 {
		t.Errorf("healthy = %d, want 1 (%s)", healthy, good)
	}
	byTask := map[string]string{}
	for _, f := range findings {
		byTask[f.TaskID] = f.Detail
	}
	if len(findings) != 4 {
		t.Errorf("findings = %d (%v), want 4", len(findings), byTask)
	}
	if d := byTask[missing]; !strings.Contains(d, "plan file missing") {
		t.Errorf("missing: %q", d)
	}
	if d := byTask[eph]; !strings.Contains(d, "unverifiable") {
		t.Errorf("ephemeral: %q", d)
	}
	if d := byTask[bare.ID]; !strings.Contains(d, "no plan recorded") {
		t.Errorf("bare: %q", d)
	}
	if d := byTask[bad.ID]; !strings.Contains(d, "unparseable") {
		t.Errorf("bad payload: %q", d)
	}
}

func TestDefaultVerifierFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "p.md"), []byte("# plan"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := DefaultVerifier(dir)
	if ok, _ := v(PlanKindFile, "docs/p.md"); !ok {
		t.Error("existing file reported missing")
	}
	if ok, detail := v(PlanKindFile, "docs/nope.md"); ok || !strings.Contains(detail, "missing") {
		t.Errorf("missing file: ok=%v detail=%q", ok, detail)
	}
}

func TestDefaultVerifierCommit(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f")
	run("commit", "-q", "-m", "c1")
	v := DefaultVerifier(dir)
	if ok, _ := v(PlanKindCommit, "HEAD"); !ok {
		t.Error("HEAD reported unresolvable")
	}
	if ok, detail := v(PlanKindCommit, "deadbeef"); ok || !strings.Contains(detail, "unresolvable") {
		t.Errorf("bogus commit: ok=%v detail=%q", ok, detail)
	}
}

func TestLinksOutboundAndInbound(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	parent, _ := s.CreateTask("ATM", "parent", "", nil, testActor)
	c1, _ := s.CreateTask("ATM", "c1", "", nil, testActor)
	c2, _ := s.CreateTask("ATM", "c2", "", nil, testActor)
	other, _ := s.CreateTask("ATM", "other", "", nil, testActor)
	_ = r.LinkRevisionOf(c1.ID, parent.ID)
	_ = r.LinkRevisionOf(c2.ID, parent.ID)
	_ = r.LinkRelatesTo(other.ID, parent.ID)
	_ = r.LinkRelatesTo(c1.ID, other.ID)

	got, err := (&Reporter{Store: s}).Links(parent.ID)
	if err != nil {
		t.Fatalf("Links: %v", err)
	}
	if got.RevisionOf != "" {
		t.Errorf("RevisionOf = %q", got.RevisionOf)
	}
	if len(got.Revisions) != 2 || !containsString(got.Revisions, c1.ID) || !containsString(got.Revisions, c2.ID) {
		t.Errorf("Revisions = %v", got.Revisions)
	}
	if len(got.RelatedFrom) != 1 || got.RelatedFrom[0] != other.ID {
		t.Errorf("RelatedFrom = %v", got.RelatedFrom)
	}
	gotC1, _ := (&Reporter{Store: s}).Links(c1.ID)
	if gotC1.RevisionOf != parent.ID || len(gotC1.RelatesTo) != 1 || gotC1.RelatesTo[0] != other.ID {
		t.Errorf("c1 links = %+v", gotC1)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/capability/workflowai/ -run 'PlanCheck|Verifier|LinksOut' -v 2>&1 | head -10`
Expected: FAIL to compile — `undefined: DefaultVerifier`, `r.PlanCheck undefined`.

- [ ] **Step 3: Grow `reporter.go`** (append below `Stage`)

```go
// Finding is one at-risk plan in a PlanCheck report.
type Finding struct {
	TaskID string `json:"task"`
	Stage  string `json:"stage"`
	Detail string `json:"detail"`
}

// PlanVerifier checks one locator against the outside world (filesystem,
// git). Split out so PlanCheck stays deterministic in tests; the CLI passes
// DefaultVerifier(cwd). Annotate never verifies — pure over the task.
type PlanVerifier func(kind, ref string) (ok bool, detail string)

// PlanCheck walks the project's planned and implementable tasks and reports
// every plan at risk: unparseable payloads, missing records, ephemeral
// plans (unverifiable by construction), and locators the verifier rejects.
// Healthy plans are counted, not listed. Read-only: the reporter reports,
// the DECIDER demotes — never this code.
func (r *Reporter) PlanCheck(code string, verify PlanVerifier) ([]Finding, int, error) {
	tasks, err := r.Store.ListTasksErr(core.QueryFilters{Project: code, Expr: "stage:planned OR stage:implementable"})
	if err != nil {
		return nil, 0, err
	}
	var findings []Finding
	healthy := 0
	for _, tk := range tasks {
		stage := firstStageValue(tk.Labels, code)
		pl, err := DecodePayload(tk.Meta[CapabilityName])
		if err != nil {
			findings = append(findings, Finding{tk.ID, stage, "payload unparseable (hand-repair needed)"})
			continue
		}
		p := pl.Plan()
		switch {
		case p == nil:
			findings = append(findings, Finding{tk.ID, stage, "no plan recorded"})
		case p.Kind == PlanKindEphemeral:
			findings = append(findings, Finding{tk.ID, stage, "ephemeral plan: " + p.Ref + " (unverifiable)"})
		default:
			if ok, detail := verify(p.Kind, p.Ref); !ok {
				findings = append(findings, Finding{tk.ID, stage, detail})
			} else {
				healthy++
			}
		}
	}
	return findings, healthy, nil
}

// DefaultVerifier verifies locators from dir: file paths by existence,
// commits by resolvability in dir's git repository. Running outside the
// right repository makes commit plans unresolvable — the message says so
// rather than calling the plan missing.
func DefaultVerifier(dir string) PlanVerifier {
	return func(kind, ref string) (bool, string) {
		switch kind {
		case PlanKindFile:
			if _, err := os.Stat(filepath.Join(dir, ref)); err != nil {
				return false, "plan file missing: " + ref
			}
			return true, ""
		case PlanKindCommit:
			if err := exec.Command("git", "-C", dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}").Run(); err != nil {
				return false, "plan commit unresolvable from " + dir + " (missing commit or not a git repository): " + ref
			}
			return true, ""
		}
		return false, "unknown plan kind: " + kind
	}
}

// TaskLinks is the links view for one task: outbound from its own payload,
// inbound computed by scanning the project's workflow_ai payloads (one
// writer per fact — inbound is always derived, never stored).
type TaskLinks struct {
	RevisionOf  string   `json:"revision_of,omitempty"`
	RelatesTo   []string `json:"relates_to,omitempty"`
	Revisions   []string `json:"revisions,omitempty"`
	RelatedFrom []string `json:"related_from,omitempty"`
}

// Links reports taskID's link topology. The task's own malformed payload is
// an error; malformed payloads on OTHER tasks are skipped in the scan
// (PlanCheck is the surface that reports those).
func (r *Reporter) Links(taskID string) (*TaskLinks, error) {
	tk, err := r.Store.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	code, _, ok := core.ParseTaskID(taskID)
	if !ok {
		return nil, fmt.Errorf("invalid task id %q", taskID)
	}
	pl, err := DecodePayload(tk.Meta[CapabilityName])
	if err != nil {
		return nil, fmt.Errorf("%s: %w", taskID, err)
	}
	out := &TaskLinks{RevisionOf: pl.RevisionOf(), RelatesTo: pl.RelatesTo()}
	tasks, err := r.Store.ListTasksErr(core.QueryFilters{Project: code})
	if err != nil {
		return nil, err
	}
	for _, other := range tasks {
		if other.ID == taskID {
			continue
		}
		opl, err := DecodePayload(other.Meta[CapabilityName])
		if err != nil {
			continue
		}
		if opl.RevisionOf() == taskID {
			out.Revisions = append(out.Revisions, other.ID)
		}
		if containsString(opl.RelatesTo(), taskID) {
			out.RelatedFrom = append(out.RelatedFrom, other.ID)
		}
	}
	return out, nil
}
```

Add the imports `os`, `os/exec`, `path/filepath` to `reporter.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/capability/workflowai/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/capability/workflowai/
git commit -m "feat(ATM-efebc0): workflow_ai reporters — plan-staleness check and links view"
```

---

### Task 6: Annotate — the contextual column cell

**PRE-STEP (drift check):** confirm the landed ATM-2e64a5 names: `capability.Cell{Text string; Tone Tone}`, `capability.ToneNeutral/ToneOK/ToneAttention/ToneStale`, interface method `Annotate(task core.Task) *Cell`. Adjust below if drifted.

**Files:**
- Create: `internal/capability/workflowai/annotate.go`
- Test: `internal/capability/workflowai/annotate_test.go`

**Interfaces:**
- Consumes: `capability.Cell`/`Tone` (ATM-2e64a5); Task 1 helpers. `Cap` (the receiver) arrives in Task 7 — declare it here minimally so Annotate compiles: `type Cap struct{}` in `annotate.go`, moved to `command.go` in Task 7 if preferred (keep ONE declaration).
- Produces: `(Cap) Annotate(t core.Task) *capability.Cell` per spec §5. Task 7 relies on `Cap` existing.

- [ ] **Step 1: Write the failing annotate tests**

`internal/capability/workflowai/annotate_test.go`:

```go
package workflowai

import (
	"testing"

	"atm/internal/capability"
	"atm/internal/core"
)

func TestAnnotateTable(t *testing.T) {
	planPayload := func(kind string) string {
		pl, _ := DecodePayload("")
		pl.SetPlan(PlanRecord{Kind: kind, Ref: "r", RecordedAt: "t", Actor: "a"})
		out, _ := pl.Encode()
		return out
	}
	cases := []struct {
		name     string
		labels   []string
		payload  string
		wantNil  bool
		wantText string
		wantTone capability.Tone
	}{
		{"no stage -> nil even with links", nil, `{"v":1,"revision_of":"ATM-aaaaaa"}`, true, "", 0},
		{"brainstormed neutral", []string{"ATM:stage:brainstormed"}, "", false, "brainstormed", capability.ToneNeutral},
		{"clarified neutral", []string{"ATM:stage:clarified"}, "", false, "clarified", capability.ToneNeutral},
		{"done neutral", []string{"ATM:stage:done"}, "", false, "done", capability.ToneNeutral},
		{"planned file neutral", []string{"ATM:stage:planned"}, planPayload(PlanKindFile), false, "planned·file", capability.ToneNeutral},
		{"planned ephemeral attention", []string{"ATM:stage:planned"}, planPayload(PlanKindEphemeral), false, "planned·ephemeral", capability.ToneAttention},
		{"planned no-plan attention", []string{"ATM:stage:planned"}, "", false, "planned·no-plan", capability.ToneAttention},
		{"implementable file ok", []string{"ATM:stage:implementable"}, planPayload(PlanKindFile), false, "implementable·file", capability.ToneOK},
		{"implementable commit ok", []string{"ATM:stage:implementable"}, planPayload(PlanKindCommit), false, "implementable·commit", capability.ToneOK},
		{"implementable ephemeral attention", []string{"ATM:stage:implementable"}, planPayload(PlanKindEphemeral), false, "implementable·ephemeral", capability.ToneAttention},
		{"malformed payload degrades to label-only", []string{"ATM:stage:planned"}, "not json", false, "planned", capability.ToneNeutral},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tk := core.Task{ID: "ATM-1234", Labels: c.labels}
			if c.payload != "" {
				tk.Meta = map[string]string{CapabilityName: c.payload}
			}
			cell := Cap{}.Annotate(tk)
			if c.wantNil {
				if cell != nil {
					t.Fatalf("cell = %+v, want nil", cell)
				}
				return
			}
			if cell == nil {
				t.Fatal("cell = nil")
			}
			if cell.Text != c.wantText || cell.Tone != c.wantTone {
				t.Errorf("cell = {%q, %v}, want {%q, %v}", cell.Text, cell.Tone, c.wantText, c.wantTone)
			}
		})
	}
}
```

Note: `core.Task{ID: "ATM-1234", ...}` — a v1-style numeric ID that `core.ParseTaskID` accepts without a store; if ParseTaskID rejects it, use a hex-style ID `ATM-abc123`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/capability/workflowai/ -run Annotate -v 2>&1 | head -10`
Expected: FAIL to compile — `undefined: Cap`.

- [ ] **Step 3: Write `annotate.go`**

```go
package workflowai

import (
	"atm/internal/capability"
	"atm/internal/core"
)

// Cap is the workflow_ai capability; the full interface lands in command.go
// (Task 7), Annotate lives here with the logic it owns.
type Cap struct{}

// Annotate implements the contextual-column hook: PURE over the task value
// (labels + own payload) — no store, no filesystem; on-disk plan
// verification is PlanCheck's job, which is why ToneStale stays unused
// here. Nil for tasks outside the cycle (no stage label), even when they
// carry links. A malformed payload degrades to the label-only cell — never
// an error, never raw payload on screen.
func (Cap) Annotate(t core.Task) *capability.Cell {
	code, _, ok := core.ParseTaskID(t.ID)
	if !ok {
		return nil
	}
	stage := firstStageValue(t.Labels, code)
	if stage == StageNew {
		return nil
	}
	cell := &capability.Cell{Text: stage, Tone: capability.ToneNeutral}
	if stage != StagePlanned && stage != StageImplementable {
		return cell
	}
	pl, err := DecodePayload(t.Meta[CapabilityName])
	if err != nil {
		return cell
	}
	p := pl.Plan()
	switch {
	case p == nil:
		cell.Text += "·no-plan"
		cell.Tone = capability.ToneAttention
	case p.Kind == PlanKindEphemeral:
		cell.Text += "·ephemeral"
		cell.Tone = capability.ToneAttention
	default:
		cell.Text += "·" + p.Kind
		if stage == StageImplementable {
			cell.Tone = capability.ToneOK
		}
	}
	return cell
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/capability/workflowai/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/capability/workflowai/
git commit -m "feat(ATM-efebc0): workflow_ai Annotate — stage and plan-kind cell with attention tones"
```

---

### Task 7: Capability wiring — guide, command tree, registration, smoke

**Files:**
- Create: `internal/capability/workflowai/guide.md`
- Create: `internal/capability/workflowai/guide.go`
- Create: `internal/capability/workflowai/command.go`
- Test: `internal/capability/workflowai/guide_test.go`
- Modify: `cmd/atm/main.go:19` (register the capability)

**Interfaces:**
- Consumes: everything above; `capability.Env`, `capability.Capability`, cobra.
- Produces: `New() capability.Capability`; `Cap` methods `Name/Summary/Guide/Vocabulary/Exposed/EnsureVocabulary/Command` (+ Task 6's `Annotate`) — the complete interface. Registration order in main.go: `workflow, contextmap, workflowai`.

- [ ] **Step 1: Write the failing guide/contract tests**

`internal/capability/workflowai/guide_test.go`:

```go
package workflowai

import (
	"strings"
	"testing"

	"atm/internal/capability"
)

// The full interface must be satisfied — the packaging design freezes it.
var _ capability.Capability = Cap{}

func TestNameIsTheMetadataKey(t *testing.T) {
	if Cap{}.Name() != CapabilityName || CapabilityName != "workflow_ai" {
		t.Fatalf("Name/CapabilityName mismatch: %q vs %q", Cap{}.Name(), CapabilityName)
	}
}

func TestSummaryIsOneLine(t *testing.T) {
	s := Cap{}.Summary()
	if s == "" || strings.Contains(s, "\n") {
		t.Fatalf("Summary must be one non-empty line, got %q", s)
	}
}

// The guide is the single source of the capability's semantics: every verb,
// the ladder, the invariant, the boards, and the operating doctrine.
func TestGuideCarriesSemantics(t *testing.T) {
	g := Cap{}.Guide()
	for _, want := range []string{
		"atm capability workflow_ai brainstorm",
		"atm capability workflow_ai clarify",
		"atm capability workflow_ai plan",
		"atm capability workflow_ai ready",
		"atm capability workflow_ai done",
		"atm capability workflow_ai demote",
		"atm capability workflow_ai link",
		"atm capability workflow_ai report",
		"atm capability workflow_ai links",
		"atm capability workflow_ai seed",
		"exactly-one-stage", "paved road, not a fence",
		"new-tasks", "brainstormed-tasks", "planned-tasks", "revisions", "done-tasks",
		"stage:implementable", "never implement", "ephemeral",
		"revision_of", "relates_to",
	} {
		if !strings.Contains(g, want) {
			t.Errorf("guide missing %q", want)
		}
	}
}

func TestGuideHasBriefAndAutopilotSections(t *testing.T) {
	g := Cap{}.Guide()
	for _, section := range []string{"\n## Brief\n", "\n## Autopilot\n"} {
		if !strings.Contains(g, section) {
			t.Errorf("guide missing %q section", strings.TrimSpace(section))
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/capability/workflowai/ -run 'Guide|Summary|Name' -v 2>&1 | head -10`
Expected: FAIL to compile — `Cap{}.Name undefined` (interface assertion also fails).

- [ ] **Step 3: Write `guide.md`**

```markdown
# workflow_ai capability — agent guide

The AI-native task cycle: brainstorm → clarify → plan → ready → implement → done, over the `stage:*` namespace, with task links and plan tracking in this capability's metadata key. Fully independent of the `workflow` capability (`status:*`): disjoint namespaces, no interplay — a task may carry both views.

## What it means

Stage verbs — `atm capability workflow_ai brainstorm` / `clarify` / `plan --kind file|commit|ephemeral --ref <ref>` / `ready` / `done` — climb the ladder one rung at a time; each swaps the task's `stage:*` label (adds the target, removes any other), so exactly-one-stage is an invariant the capability maintains. "New" is the absence of any stage label, not a label. `atm capability workflow_ai demote --task X --reason "..."` resets any stage back to new, clears the plan record, and logs the reason as a comment. The store enforces nothing: raw `atm task label add/remove` works. This is a paved road, not a fence.

There is no `implement` verb: implementation is your dev session. The gate is doctrine — **never implement a task whose stage is not `stage:implementable`**; check the stage first and refuse otherwise.

## Vocabulary

Stages (exactly one per task; absence = new):
- `stage:brainstormed` — the idea has been explored.
- `stage:clarified` — scope and success criteria settled.
- `stage:planned` — a plan locator is recorded in this capability's metadata.
- `stage:implementable` — planned AND sized for one implementation session; cleared for implementation.
- `stage:done` — completed the cycle.

Marker:
- `wfai:revision` — this task is a revision follow-up of a bigger planned task; the link itself (`revision_of`) lives in the metadata payload, the marker only makes it board-visible.

Boards (seeded by `atm capability workflow_ai seed` / project create):
- `<CODE>:new-tasks` (`NOT stage:*`) — the intake queue, not yet brainstormed.
- `<CODE>:brainstormed-tasks` (brainstormed or clarified) — in refinement.
- `<CODE>:planned-tasks` (planned or implementable) — has a recorded plan.
- `<CODE>:revisions` (revision marker, not done) — follow-ups still needing refinement.
- `<CODE>:done-tasks` (`stage:done`).

## Links and plans

- `atm capability workflow_ai link --task X --revision-of Y` — X is a refinement follow-up of Y (one parent max). `--relates-to Y` — generic association. `unlink` reverses either. `atm capability workflow_ai links --task X` shows both directions.
- `plan` records WHERE the plan lives: `--kind file --ref docs/...` (repo-relative), `--kind commit --ref <rev>`, or `--kind ephemeral --ref "session ..."` for a plan that lives in a conversation. Record ephemeral plans honestly — they are unverifiable and always at-risk.
- `atm capability workflow_ai report --project <CODE>` — the staleness check: verifies every planned/implementable task's plan (file existence, commit resolvability from the current directory) and lists what is at risk. The reporter never demotes; **you** decide, then `demote --reason`.

## Sizing doctrine

A task is sized to one plan a framework like superpowers can execute in a single session. When a planned task is bigger than that, split it: create follow-up tasks linked with `--revision-of`, each entering the cycle at its own stage. The revisions board is the refinement queue.

## Brief

Walk the human through the ladder and confirm the project will use it as-is: the five stages, absence-as-new, the five boards, the plan-locator kinds, and the two link types (`revision_of`, `relates_to`). The vocabulary is fixed; extra stages are not part of the paved road. Confirm where plans normally live (committed plan docs vs ephemeral sessions) and record that preference in the `stage:planned` label description.

## Autopilot

The mechanical loop, run at session start:
1. `report --project <CODE>`; for each unlocatable or unrecoverable plan, ask the decider (or decide, if you are the decider) and `demote --reason` — replanning is cheaper than implementing against a ghost.
2. Advance tasks whose next rung's evidence exists: brainstormed notes → `brainstorm`; settled scope → `clarify`; a written plan → `plan`; reviewed and sized → `ready`.
3. Never skip rungs; never implement below `stage:implementable`; split oversized planned tasks into `--revision-of` follow-ups.
```

- [ ] **Step 4: Write `guide.go`**

```go
package workflowai

import (
	_ "embed"
)

//go:embed guide.md
var guideText string

// Summary is the capability's one-line description for enumeration surfaces.
func (Cap) Summary() string {
	return "AI-native task cycle (brainstorm→clarify→plan→ready) with links, plan tracking, and stage boards."
}

// Guide is the capability's full agent-facing semantics; `atm capability
// workflow_ai guide` prints it verbatim. Single source: composed surfaces
// only point here.
func (Cap) Guide() string { return guideText }
```

- [ ] **Step 5: Write `command.go`**

```go
package workflowai

import (
	"fmt"
	"os"

	"atm/internal/capability"
	"atm/internal/core"

	"github.com/spf13/cobra"
)

// New returns the capability the composition root registers.
func New() capability.Capability { return Cap{} }

func (Cap) Name() string { return CapabilityName }

// Vocabulary implements capability.Capability (ownership surface).
func (Cap) Vocabulary(code string) []core.Label { return Vocabulary(code) }

// Exposed implements capability.Capability (TUI ring surface).
func (Cap) Exposed(code string) []core.Label { return Exposed(code) }

// EnsureVocabulary implements capability.Capability by delegating to this
// package's vocabulary bootstrap.
func (Cap) EnsureVocabulary(svc core.LabelService, code, actor string) ([]core.Label, error) {
	return EnsureVocabulary(svc, code, actor)
}

func (Cap) Command(env capability.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   CapabilityName,
		Short: "AI-native task cycle: stage ladder, links, plan tracking (the workflow_ai paved road)",
		Long: "The workflow_ai capability climbs tasks through brainstorm → clarify → " +
			"plan → ready → done over the stage:* namespace, one rung at a time; " +
			"exactly-one-stage is an invariant the verbs maintain, and machine state " +
			"(plan locator, links, demotion breadcrumb) lives in this capability's " +
			"metadata key. The store enforces nothing. This is a paved road, not a fence.",
	}
	env.BindActorFlag(cmd)
	cmd.AddCommand(newStageCmd(env, "brainstorm", "Mark the idea brainstormed (new → brainstormed)", (*Recorder).Brainstorm))
	cmd.AddCommand(newStageCmd(env, "clarify", "Mark scope settled (brainstormed → clarified)", (*Recorder).Clarify))
	cmd.AddCommand(newPlanCmd(env))
	cmd.AddCommand(newStageCmd(env, "ready", "Clear for implementation (planned → implementable; requires a plan record)", (*Recorder).Ready))
	cmd.AddCommand(newStageCmd(env, "done", "Close the cycle (implementable → done)", (*Recorder).Done))
	cmd.AddCommand(newDemoteCmd(env))
	cmd.AddCommand(newLinkCmd(env, true))
	cmd.AddCommand(newLinkCmd(env, false))
	cmd.AddCommand(newStageReportCmd(env))
	cmd.AddCommand(newLinksCmd(env))
	cmd.AddCommand(newSeedCmd(env))
	return cmd
}

// stageDisplay renders StageNew as the word humans read.
func stageDisplay(v string) string {
	if v == StageNew {
		return "new"
	}
	return v
}

// runStageVerb is the shared body for the mutating stage verbs: resolve
// task and actor, run the transition, print the transition line, emit the
// updated task JSON. prior == now identifies the idempotent no-op path.
func runStageVerb(env capability.Env, id, legacy string, fn func(*Recorder, string) (string, error)) error {
	taskID, err := env.ResolveTaskID(id, legacy)
	if err != nil {
		return err
	}
	actor, err := env.RequireMutatingActor()
	if err != nil {
		return err
	}
	svc, err := env.OpenService()
	if err != nil {
		return err
	}
	rec := &Recorder{Store: svc, Actor: actor}
	prior, err := fn(rec, taskID)
	if err != nil {
		return err
	}
	t, err := svc.GetTask(taskID)
	if err != nil {
		return err
	}
	now, err := (&Reporter{Store: svc}).Stage(taskID)
	if err != nil {
		return err
	}
	return env.Emit(map[string]any{"task": env.TaskJSON(t)}, func() {
		if prior == now {
			fmt.Fprintf(env.Stdout(), "%s: already %s\n", t.ID, stageDisplay(now))
			return
		}
		fmt.Fprintf(env.Stdout(), "%s: stage %s -> %s\n", t.ID, stageDisplay(prior), stageDisplay(now))
	})
}

func newStageCmd(env capability.Env, use, short string, fn func(*Recorder, string) (string, error)) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStageVerb(env, id, legacy, fn)
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newPlanCmd(env capability.Env) *cobra.Command {
	var id, legacy, kind, ref string
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Record the plan locator (clarified → planned; from planned/implementable updates it in place)",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := env.ResolveTaskID(id, legacy)
			if err != nil {
				return err
			}
			actor, err := env.RequireMutatingActor()
			if err != nil {
				return err
			}
			svc, err := env.OpenService()
			if err != nil {
				return err
			}
			rec := &Recorder{Store: svc, Actor: actor}
			prior, err := rec.Plan(taskID, kind, ref)
			if err != nil {
				return err
			}
			t, err := svc.GetTask(taskID)
			if err != nil {
				return err
			}
			now, err := (&Reporter{Store: svc}).Stage(taskID)
			if err != nil {
				return err
			}
			return env.Emit(map[string]any{"task": env.TaskJSON(t), "plan": map[string]string{"kind": kind, "ref": ref}}, func() {
				if prior == now {
					fmt.Fprintf(env.Stdout(), "%s: plan updated (%s %s); stage %s unchanged\n", t.ID, kind, ref, stageDisplay(now))
					return
				}
				fmt.Fprintf(env.Stdout(), "%s: stage %s -> %s (plan: %s %s)\n", t.ID, stageDisplay(prior), stageDisplay(now), kind, ref)
			})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&kind, "kind", "", "plan locator kind: file|commit|ephemeral")
	cmd.Flags().StringVar(&ref, "ref", "", "plan locator: repo-relative path, git revision, or ephemeral note")
	_ = cmd.MarkFlagRequired("kind")
	_ = cmd.MarkFlagRequired("ref")
	return cmd
}

func newDemoteCmd(env capability.Env) *cobra.Command {
	var id, legacy, reason string
	cmd := &cobra.Command{
		Use:   "demote",
		Short: "Reset the task to new (any stage; clears the plan record, keeps links, logs the reason)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStageVerb(env, id, legacy, func(r *Recorder, tid string) (string, error) {
				return r.Demote(tid, reason)
			})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&reason, "reason", "", "why the task is demoted (recorded as a task comment)")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}

func newLinkCmd(env capability.Env, link bool) *cobra.Command {
	use, short := "link", "Record a task link (exactly one of --revision-of / --relates-to)"
	if !link {
		use, short = "unlink", "Remove a task link (exactly one of --revision-of / --relates-to)"
	}
	var id, legacy, revisionOf, relatesTo string
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := env.ResolveTaskID(id, legacy)
			if err != nil {
				return err
			}
			if (revisionOf == "") == (relatesTo == "") {
				return fmt.Errorf("exactly one of --revision-of or --relates-to is required")
			}
			actor, err := env.RequireMutatingActor()
			if err != nil {
				return err
			}
			svc, err := env.OpenService()
			if err != nil {
				return err
			}
			rec := &Recorder{Store: svc, Actor: actor}
			var verb, desc string
			switch {
			case link && revisionOf != "":
				err, verb, desc = rec.LinkRevisionOf(taskID, revisionOf), "linked", "revision_of "+revisionOf
			case link:
				err, verb, desc = rec.LinkRelatesTo(taskID, relatesTo), "linked", "relates_to "+relatesTo
			case revisionOf != "":
				err, verb, desc = rec.UnlinkRevisionOf(taskID, revisionOf), "unlinked", "revision_of "+revisionOf
			default:
				err, verb, desc = rec.UnlinkRelatesTo(taskID, relatesTo), "unlinked", "relates_to "+relatesTo
			}
			if err != nil {
				return err
			}
			return env.Emit(map[string]any{"task": taskID, verb: desc}, func() {
				fmt.Fprintf(env.Stdout(), "%s: %s %s\n", taskID, verb, desc)
			})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&revisionOf, "revision-of", "", "parent task this task is a revision follow-up of")
	cmd.Flags().StringVar(&relatesTo, "relates-to", "", "related task (generic, semantics-free)")
	return cmd
}

func newStageReportCmd(env capability.Env) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Check every planned/implementable task's plan locator; list what is at risk (read-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := env.OpenService()
			if err != nil {
				return err
			}
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			findings, healthy, err := (&Reporter{Store: svc}).PlanCheck(project, DefaultVerifier(dir))
			if err != nil {
				return err
			}
			return env.Emit(map[string]any{"project": project, "findings": findings, "healthy": healthy}, func() {
				for _, f := range findings {
					fmt.Fprintf(env.Stdout(), "%s\t%s\t%s\n", f.TaskID, f.Stage, f.Detail)
				}
				fmt.Fprintf(env.Stdout(), "%d at risk, %d healthy (verified from %s)\n", len(findings), healthy, dir)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newLinksCmd(env capability.Env) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "links",
		Short: "Show a task's link topology, outbound and inbound (read-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := env.ResolveTaskID(id, legacy)
			if err != nil {
				return err
			}
			svc, err := env.OpenService()
			if err != nil {
				return err
			}
			l, err := (&Reporter{Store: svc}).Links(taskID)
			if err != nil {
				return err
			}
			return env.Emit(map[string]any{"task": taskID, "links": l}, func() {
				if l.RevisionOf != "" {
					fmt.Fprintf(env.Stdout(), "revision_of: %s\n", l.RevisionOf)
				}
				for _, x := range l.RelatesTo {
					fmt.Fprintf(env.Stdout(), "relates_to: %s\n", x)
				}
				for _, x := range l.Revisions {
					fmt.Fprintf(env.Stdout(), "revision: %s\n", x)
				}
				for _, x := range l.RelatedFrom {
					fmt.Fprintf(env.Stdout(), "related_from: %s\n", x)
				}
				if l.RevisionOf == "" && len(l.RelatesTo)+len(l.Revisions)+len(l.RelatedFrom) == 0 {
					fmt.Fprintf(env.Stdout(), "no links\n")
				}
			})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newSeedCmd(env capability.Env) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Ensure the workflow_ai vocabulary and boards exist for a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := env.RequireMutatingActor()
			if err != nil {
				return err
			}
			svc, err := env.OpenService()
			if err != nil {
				return err
			}
			boards, err := EnsureVocabulary(svc, project, actor)
			if err != nil {
				return err
			}
			names := make([]string, 0, len(boards))
			for _, b := range boards {
				names = append(names, b.Name)
			}
			return env.Emit(map[string]any{"project": project, "boards": names}, func() {
				fmt.Fprintf(env.Stdout(), "ensured workflow_ai boards for %s\n", project)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
```

- [ ] **Step 6: Register in `cmd/atm/main.go`**

At line 19, add the import `"atm/internal/capability/workflowai"` and change:

```go
reg := capability.NewRegistry(workflow.New(), contextmap.New())
```

to:

```go
reg := capability.NewRegistry(workflow.New(), contextmap.New(), workflowai.New())
```

- [ ] **Step 7: Run the full test suite and vet**

Run: `gofmt -l internal/capability/workflowai/ && go vet ./internal/capability/workflowai/ ./cmd/... && go test ./...`
Expected: gofmt lists nothing; vet clean; ALL packages pass (registry/TUI tests must not regress from the new registration).

- [ ] **Step 8: Smoke against a store COPY (never `~/.config/atm`)**

```bash
SMOKE=$(mktemp -d)
go build -o "$SMOKE/atm" ./cmd/atm
cp -r ~/.config/atm "$SMOKE/store"
export ATM_HOME="$SMOKE/store"
A="$SMOKE/atm"
$A capability list --project ATM                                  # workflow_ai listed (enabled or available)
$A project capability add --project ATM --name workflow_ai --actor smoke@cli:unset
$A capability workflow_ai guide | head -5
T=$($A task create --project ATM --title "smoke wfai" --actor smoke@cli:unset --output json | python3 -c "import json,sys; print(json.load(sys.stdin)['task']['id'])")
$A capability workflow_ai brainstorm --task $T --actor smoke@cli:unset
$A capability workflow_ai clarify    --task $T --actor smoke@cli:unset
$A capability workflow_ai plan       --task $T --actor smoke@cli:unset --kind ephemeral --ref "smoke session"
$A capability workflow_ai ready     --task $T --actor smoke@cli:unset
$A capability workflow_ai report --project ATM                    # lists $T as ephemeral/at-risk
$A capability workflow_ai demote --task $T --actor smoke@cli:unset --reason "smoke over"
$A task show --task $T                                            # no stage label; metadata presence line
unset ATM_HOME
```

Verify each command's exact flags with `--help` first if any differ (e.g. `task create` flag names); adjust the smoke script, not the code, unless a real defect surfaces.

- [ ] **Step 9: Commit**

```bash
git add internal/capability/workflowai/ cmd/atm/main.go
git commit -m "feat(ATM-efebc0): wire workflow_ai capability — guide, command tree, registration"
```

---

## Final verification

- [ ] `go test ./...` — everything green.
- [ ] Journal completion on the ledger: `atm task comment add --task ATM-efebc0 --body "..."` summarizing what shipped, and label per project convention.
- [ ] Spec cross-check: §1 identity/namespaces (Tasks 1–2), §2 boards (Task 2), §3 payload (Task 1), §4.1 ladder+demote (Task 3), §4.2 links (Task 4), §4.3 reporters (Task 5), §5 Annotate (Task 6), §6 guide, §7 tests throughout, §8 non-goals respected (no auto-demote, no cross-capability reads, no deep cycles, no payload boards).
