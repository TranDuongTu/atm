# Eventsource Core v2 (L0–L2 + D6) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the ATM-0106 core data model — v2 event envelope with content-addressed identity, HLC, hash-DAG, alias minting/resolution, the convergent fold, and the D6 v1→v2 upgrade — as the pure, fully-tested library package `internal/eventsource`.

**Architecture:** A self-contained package of pure functions plus one injectable clock. Events are canonical RFC 8785 bytes (`Event.Raw`) as source of truth with a read-only decoded view; identity is `sha256:` of those bytes; the fold derives state from the event *set* via the maximal-writer rule. The live store is NOT rewired here — that waits for L3 (ATM-0107). The capstone proves `fold(upgrade(v1 log)) == store.Replay` on a real store.

**Tech Stack:** Go 1.25, stdlib `testing`, one new dependency: `github.com/gowebpki/jcs` (RFC 8785).

**Read first:** `docs/eventsource/01-core-data-model.md` (the model — L0/L1/L2/D6), `docs/superpowers/specs/2026-07-14-eventsource-core-v2-design.md` (implementation decisions 1–14; task steps cite them as "spec decision N").

## Global Constraints

- Package `internal/eventsource` MUST NOT import `internal/store`. Only the capstone test (external test package `eventsource_test`) may import both.
- Only new dependency allowed: `github.com/gowebpki/jcs`.
- Every hash flows through `Canonicalize`; every authored event flows through `Parse` (single identity code path).
- The event id is NEVER serialized inside an event (`json:"-"` on `Event.ID` and `Event.Raw`).
- Nothing ever orders by `at` (wall clock); ordering uses `parents` (causality) and `CompareEvents` (HLC total order) only.
- All fold outputs must be deterministic: iterate maps only via sorted keys.
- Run `gofmt -l internal/eventsource` (must print nothing) and `go build ./...` before every commit.
- Markdown prose: single un-wrapped lines (no hard-wrap).
- Commit messages: `feat(ATM-0106): …` / `test(ATM-0106): …` / `docs(ATM-0106): …`.

## File Structure

```
internal/eventsource/
  canon.go + canon_test.go          Task 1
  hlc.go + hlc_test.go              Task 2
  event.go + event_test.go          Task 3   (Subject, Event, Parse, payload accessors, CompareEvents)
  replica.go + replica_test.go      Task 4
  action.go, author.go + author_test.go  Task 5
  dag.go + dag_test.go, helpers_test.go  Task 6
  fold.go + fold_slots_test.go      Task 7   (slot writes, maximal writers)
  fold.go + fold_test.go            Task 8   (Fold, State, resolution rules)
  fold_property_test.go             Task 9
  alias.go + alias_test.go          Task 10
  upgrade.go + upgrade_test.go, testdata/  Task 11
  equivalence_test.go               Task 12  (package eventsource_test)
docs/eventsource/01-core-data-model.md   Task 13 (amendment section)
```

---

### Task 1: Package scaffold + RFC 8785 canonicalization

**Files:**
- Create: `internal/eventsource/canon.go`
- Create: `internal/eventsource/canon_test.go`
- Modify: `go.mod` / `go.sum` (via `go get`)

**Interfaces:**
- Consumes: nothing.
- Produces: `func Canonicalize(raw []byte) ([]byte, error)` — every later task hashes only bytes returned by this function.

- [ ] **Step 1: Add the dependency**

Run from the repository root of the CURRENT checkout (do not `cd` to an absolute path — this plan executes in a worktree, and installing into a different checkout leaves this one unable to compile):

```bash
go get github.com/gowebpki/jcs@latest && go mod tidy
```

- [ ] **Step 2: Write the failing test**

Create `internal/eventsource/canon_test.go`:

```go
package eventsource

import "testing"

// The RFC 8785 example object (§3.2.3): numbers get ES6 shortest-round-trip
// serialization, strings get canonical escapes, keys sort by UTF-16 units.
func TestCanonicalizeRFC8785Vector(t *testing.T) {
	in := []byte(`{
  "numbers": [333333333.33333329, 1E30, 4.50, 2e-3, 0.000000000000000000000000001],
  "string": "\u20ac$\u000F\u000aA'\u0042\u0022\u005c\\\"\/",
  "literals": [null, true, false]
}`)
	want := `{"literals":[null,true,false],"numbers":[333333333.3333333,1e+30,4.5,0.002,1e-27],"string":"€$\u000f\nA'B\"\\\\\"/"}`
	got, err := Canonicalize(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("canonical form:\n got %s\nwant %s", got, want)
	}
}

func TestCanonicalizeSortsKeysAndStripsWhitespace(t *testing.T) {
	got, err := Canonicalize([]byte(`{"b": 2, "a": 1}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"a":1,"b":2}` {
		t.Errorf("got %s", got)
	}
}

func TestCanonicalizeIsIdempotent(t *testing.T) {
	once, err := Canonicalize([]byte(`{"z":[1,2],"a":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	twice, err := Canonicalize(once)
	if err != nil {
		t.Fatal(err)
	}
	if string(once) != string(twice) {
		t.Errorf("not idempotent: %s vs %s", once, twice)
	}
}

func TestCanonicalizeRejectsInvalidJSON(t *testing.T) {
	if _, err := Canonicalize([]byte(`{"unterminated`)); err == nil {
		t.Error("expected error for invalid JSON")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/eventsource/`
Expected: `FAIL` with `undefined: Canonicalize` (build failure).

- [ ] **Step 4: Write minimal implementation**

Create `internal/eventsource/canon.go`:

```go
// Package eventsource implements the ATM v2 distributed event model:
// content-addressed events (L0), stored display aliases (L1), the
// convergent fold (L2), and the one-time v1→v2 upgrade (D6). See
// docs/eventsource/01-core-data-model.md for the model and
// docs/superpowers/specs/2026-07-14-eventsource-core-v2-design.md for the
// implementation decisions.
package eventsource

import "github.com/gowebpki/jcs"

// Canonicalize returns the RFC 8785 (JCS) canonical form of raw JSON.
// Event identity is the SHA-256 of these bytes (L0-1), so every hash in
// the system flows through this one function.
func Canonicalize(raw []byte) ([]byte, error) {
	return jcs.Transform(raw)
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/eventsource/`
Expected: `ok  	atm/internal/eventsource`

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/eventsource/
git commit -m "feat(ATM-0106): eventsource package with RFC 8785 canonicalization"
```

---

### Task 2: Hybrid Logical Clock

**Files:**
- Create: `internal/eventsource/hlc.go`
- Create: `internal/eventsource/hlc_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `type HLC struct{ P, L int64 }` (json `p`/`l`), `func (a HLC) Compare(b HLC) int`, `type Clock`, `func NewClock(now func() int64) *Clock`, `(*Clock).Tick() HLC`, `(*Clock).Observe(h HLC)`.

- [ ] **Step 1: Write the failing test**

Create `internal/eventsource/hlc_test.go`:

```go
package eventsource

import "testing"

func TestHLCCompare(t *testing.T) {
	cases := []struct {
		a, b HLC
		want int
	}{
		{HLC{1, 0}, HLC{2, 0}, -1},
		{HLC{2, 0}, HLC{1, 5}, 1},
		{HLC{1, 1}, HLC{1, 2}, -1},
		{HLC{1, 2}, HLC{1, 2}, 0},
	}
	for _, c := range cases {
		if got := c.a.Compare(c.b); got != c.want {
			t.Errorf("Compare(%v, %v) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestClockTickAdvancesLogicalOnSameMillisecond(t *testing.T) {
	c := NewClock(func() int64 { return 1000 })
	if got := c.Tick(); got != (HLC{1000, 0}) {
		t.Fatalf("first tick = %v", got)
	}
	if got := c.Tick(); got != (HLC{1000, 1}) {
		t.Fatalf("second tick = %v", got)
	}
}

func TestClockTickResetsLogicalOnNewMillisecond(t *testing.T) {
	now := int64(1000)
	c := NewClock(func() int64 { return now })
	c.Tick()
	now = 2000
	if got := c.Tick(); got != (HLC{2000, 0}) {
		t.Fatalf("tick after clock advance = %v", got)
	}
}

func TestClockTickIgnoresWallClockRegression(t *testing.T) {
	now := int64(2000)
	c := NewClock(func() int64 { return now })
	c.Tick()
	now = 1500 // wall clock jumps backwards; stamps must not
	if got := c.Tick(); got != (HLC{2000, 1}) {
		t.Fatalf("tick after regression = %v", got)
	}
}

func TestClockObserve(t *testing.T) {
	// Case p' == local.p == e.p: l = max(l_local, l_e) + 1.
	c := NewClock(func() int64 { return 1000 })
	c.Tick() // local = (1000, 0)
	c.Observe(HLC{1000, 7})
	if got := c.Tick(); got != (HLC{1000, 9}) { // observe set (1000,8); tick bumps to 9
		t.Fatalf("after same-p observe, tick = %v", got)
	}

	// Case p' == e.p (remote ahead): l = e.l + 1.
	c2 := NewClock(func() int64 { return 1000 })
	c2.Observe(HLC{5000, 3})
	if got := c2.Tick(); got != (HLC{5000, 5}) { // observe set (5000,4); tick bumps to 5
		t.Fatalf("after remote-ahead observe, tick = %v", got)
	}

	// Case p' == now (wall clock ahead of both): l = 0.
	c3 := NewClock(func() int64 { return 9000 })
	c3.Observe(HLC{5000, 3})
	if got := c3.Tick(); got != (HLC{9000, 1}) { // observe set (9000,0); tick bumps to 1
		t.Fatalf("after wall-ahead observe, tick = %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/eventsource/`
Expected: `FAIL` with `undefined: HLC` (build failure).

- [ ] **Step 3: Write minimal implementation**

Create `internal/eventsource/hlc.go`:

```go
package eventsource

import (
	"sync"
	"time"
)

// HLC is a Hybrid Logical Clock stamp (D2): physical milliseconds since
// epoch and a logical counter. It is a tiebreak only — causality comes
// from parents, never from stamps, and nothing ever orders by `at`.
type HLC struct {
	P int64 `json:"p"`
	L int64 `json:"l"`
}

// Compare orders two stamps: -1 if a < b, 0 if equal, +1 if a > b.
func (a HLC) Compare(b HLC) int {
	switch {
	case a.P < b.P:
		return -1
	case a.P > b.P:
		return 1
	case a.L < b.L:
		return -1
	case a.L > b.L:
		return 1
	}
	return 0
}

// Clock is a replica's local HLC state. Tick stamps a locally-authored
// event; Observe folds in a received event's stamp so later local stamps
// sort after it.
type Clock struct {
	mu   sync.Mutex
	now  func() int64
	last HLC
}

// NewClock returns a Clock reading physical time in milliseconds from now.
// A nil now uses the wall clock.
func NewClock(now func() int64) *Clock {
	if now == nil {
		now = func() int64 { return time.Now().UnixMilli() }
	}
	return &Clock{now: now}
}

// Tick returns the stamp for a locally-authored event.
func (c *Clock) Tick() HLC {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := max(c.last.P, c.now())
	l := int64(0)
	if p == c.last.P {
		l = c.last.L + 1
	}
	c.last = HLC{P: p, L: l}
	return c.last
}

// Observe advances the clock past a received event's stamp.
func (c *Clock) Observe(h HLC) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := max(c.last.P, h.P, c.now())
	var l int64
	switch {
	case p == c.last.P && p == h.P:
		l = max(c.last.L, h.L) + 1
	case p == c.last.P:
		l = c.last.L + 1
	case p == h.P:
		l = h.L + 1
	}
	c.last = HLC{P: p, L: l}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/eventsource/`
Expected: `ok`

- [ ] **Step 5: Commit**

```bash
git add internal/eventsource/hlc.go internal/eventsource/hlc_test.go
git commit -m "feat(ATM-0106): hybrid logical clock"
```

---

### Task 3: v2 event envelope, Parse, and content-addressed identity

**Files:**
- Create: `internal/eventsource/event.go`
- Create: `internal/eventsource/event_test.go`

**Interfaces:**
- Consumes: `Canonicalize` (Task 1), `HLC` (Task 2).
- Produces: `type Subject struct{ Kind, ID, Name, Code string }`, `type Event` (fields `ID string`, `Raw []byte`, `V int`, `Parents []string`, `HLC HLC`, `Replica string`, `At time.Time`, `Actor string`, `Action string`, `Subject Subject`, `Payload json.RawMessage`), `func Parse(line []byte) (*Event, error)`, `func (e *Event) PayloadString(key string) (string, bool)`, `func (e *Event) PayloadStringOrList(key string) []string`, `func CompareEvents(a, b *Event) int`.

- [ ] **Step 1: Write the failing test**

Create `internal/eventsource/event_test.go`:

```go
package eventsource

import (
	"strings"
	"testing"
)

// A valid v2 root event, deliberately formatted with shuffled keys and
// whitespace: Parse must canonicalize before hashing.
const sampleEvent = `{
  "action": "task.created",
  "v": 2,
  "replica": "r_00000000000000000000000001",
  "hlc": {"l": 0, "p": 1752480000000},
  "at": "2026-07-14T09:12:03Z",
  "actor": "developer@claude:test",
  "subject": {"kind": "task"},
  "parents": [],
  "payload": {"alias": "ATM-7f3a2b", "title": "Fix the cache", "labels": ["ATM:status:open"]}
}`

func TestParseDerivesStableIdentity(t *testing.T) {
	e1, err := Parse([]byte(sampleEvent))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(e1.ID, "sha256:") || len(e1.ID) != len("sha256:")+64 {
		t.Fatalf("id = %q", e1.ID)
	}
	// Reformatting must not change identity: Raw is canonical.
	e2, err := Parse(e1.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if e2.ID != e1.ID {
		t.Errorf("id changed across reformat: %s vs %s", e2.ID, e1.ID)
	}
}

func TestParseDecodesKnownFields(t *testing.T) {
	e, err := Parse([]byte(sampleEvent))
	if err != nil {
		t.Fatal(err)
	}
	if e.V != 2 || e.Replica != "r_00000000000000000000000001" || e.Action != "task.created" {
		t.Errorf("envelope = %+v", e)
	}
	if e.HLC != (HLC{P: 1752480000000, L: 0}) {
		t.Errorf("hlc = %+v", e.HLC)
	}
	if e.Subject.Kind != "task" || e.Subject.ID != "" {
		t.Errorf("subject = %+v", e.Subject)
	}
	if len(e.Parents) != 0 || e.Parents == nil {
		t.Errorf("parents = %#v, want non-nil empty", e.Parents)
	}
	if e.At.Format("2006-01-02") != "2026-07-14" {
		t.Errorf("at = %v", e.At)
	}
}

func TestParsePreservesUnknownFields(t *testing.T) {
	// L0-2: dropping an unknown field would destroy the identity. The raw
	// bytes are the source of truth, so a future field must survive a
	// parse → re-parse round trip with the id intact.
	withFuture := strings.Replace(sampleEvent, `"v": 2,`, `"v": 2, "future_field": {"x": [1, 2.5]},`, 1)
	e1, err := Parse([]byte(withFuture))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(e1.Raw), `"future_field"`) {
		t.Fatal("unknown field dropped from Raw")
	}
	e2, err := Parse(e1.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if e2.ID != e1.ID {
		t.Errorf("identity destroyed by unknown field round trip")
	}
	base, _ := Parse([]byte(sampleEvent))
	if base.ID == e1.ID {
		t.Errorf("different content, same id")
	}
}

func TestParseRejectsMalformedEnvelopes(t *testing.T) {
	bad := []string{
		`{"v":1,"parents":[],"hlc":{"p":1,"l":0},"replica":"r_x","at":"2026-07-14T09:12:03Z","actor":"a","action":"x","subject":{"kind":"task"}}`, // wrong version
		`{"v":2,"hlc":{"p":1,"l":0},"replica":"r_x","at":"2026-07-14T09:12:03Z","actor":"a","action":"x","subject":{"kind":"task"}}`,              // missing parents
		`{"v":2,"parents":[],"hlc":{"p":1,"l":0},"at":"2026-07-14T09:12:03Z","actor":"a","action":"x","subject":{"kind":"task"}}`,                 // missing replica
		`{"v":2,"parents":[],"hlc":{"p":1,"l":0},"replica":"r_x","at":"2026-07-14T09:12:03Z","actor":"a","subject":{"kind":"task"}}`,              // missing action
		`{"v":2,"parents":[],"hlc":{"p":1,"l":0},"replica":"r_x","at":"2026-07-14T09:12:03Z","actor":"a","action":"x","subject":{}}`,              // missing subject.kind
		`not json`,
	}
	for _, b := range bad {
		if _, err := Parse([]byte(b)); err == nil {
			t.Errorf("Parse accepted %s", b)
		}
	}
}

func TestPayloadAccessors(t *testing.T) {
	e, err := Parse([]byte(sampleEvent))
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := e.PayloadString("title"); !ok || s != "Fix the cache" {
		t.Errorf("PayloadString(title) = %q, %v", s, ok)
	}
	if _, ok := e.PayloadString("missing"); ok {
		t.Error("PayloadString(missing) reported ok")
	}
	if got := e.PayloadStringOrList("labels"); len(got) != 1 || got[0] != "ATM:status:open" {
		t.Errorf("PayloadStringOrList(labels) = %v", got)
	}
	if got := e.PayloadStringOrList("title"); len(got) != 1 || got[0] != "Fix the cache" {
		t.Errorf("PayloadStringOrList on a string = %v", got)
	}
	if got := e.PayloadStringOrList("missing"); got != nil {
		t.Errorf("PayloadStringOrList(missing) = %v", got)
	}
}

func TestCompareEventsTotalOrder(t *testing.T) {
	mk := func(p, l int64, replica string) *Event {
		return &Event{ID: "sha256:aaaa", HLC: HLC{P: p, L: l}, Replica: replica}
	}
	a, b := mk(1000, 0, "r_a"), mk(1000, 0, "r_b")
	if CompareEvents(a, b) >= 0 || CompareEvents(b, a) <= 0 {
		t.Error("replica must break HLC ties")
	}
	if CompareEvents(mk(1000, 1, "r_a"), mk(1000, 0, "r_b")) <= 0 {
		t.Error("logical counter must dominate replica")
	}
	// Defensive fourth key (spec decision 11): identical triples — two _v1
	// chains from different upgraded projects — fall back to the id.
	c := &Event{ID: "sha256:bbbb", HLC: HLC{P: 1, L: 2}, Replica: "_v1"}
	d := &Event{ID: "sha256:cccc", HLC: HLC{P: 1, L: 2}, Replica: "_v1"}
	if CompareEvents(c, d) >= 0 || CompareEvents(d, c) <= 0 {
		t.Error("id must break full-triple ties")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/eventsource/`
Expected: `FAIL` with `undefined: Parse` (build failure).

- [ ] **Step 3: Write minimal implementation**

Create `internal/eventsource/event.go`:

```go
package eventsource

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Subject names the entity an event writes. Creation events carry no ID —
// the entity's identity IS the creation event's id, which cannot appear
// inside itself (spec decision 1). Labels are identified by Name (their
// name is their identity, L1); projects keep Code alongside the identity
// for readability.
type Subject struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	Code string `json:"code,omitempty"`
}

// Event is the v2 envelope (L0). Raw holds the canonical RFC 8785 bytes —
// the source of truth, in which unknown fields survive byte-faithfully
// (L0-2). The struct fields are a read-only decoded view and are never
// re-encoded.
type Event struct {
	ID  string `json:"-"` // "sha256:" + hex SHA-256 of Raw; derived, never serialized
	Raw []byte `json:"-"`

	V       int             `json:"v"`
	Parents []string        `json:"parents"`
	HLC     HLC             `json:"hlc"`
	Replica string          `json:"replica"`
	At      time.Time       `json:"at"`
	Actor   string          `json:"actor"`
	Action  string          `json:"action"`
	Subject Subject         `json:"subject"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Parse canonicalizes one serialized v2 event, derives its identity, and
// decodes the known fields. The input may be arbitrarily formatted JSON;
// Raw is always the canonical form, so the id is stable however the event
// was formatted in transit.
func Parse(line []byte) (*Event, error) {
	canon, err := Canonicalize(line)
	if err != nil {
		return nil, fmt.Errorf("eventsource: canonicalize: %w", err)
	}
	e := &Event{}
	if err := json.Unmarshal(canon, e); err != nil {
		return nil, fmt.Errorf("eventsource: decode: %w", err)
	}
	if e.V != 2 {
		return nil, fmt.Errorf("eventsource: unsupported event version %d", e.V)
	}
	if e.Replica == "" || e.Action == "" || e.Subject.Kind == "" {
		return nil, fmt.Errorf("eventsource: event missing replica, action, or subject.kind")
	}
	if e.Parents == nil {
		return nil, fmt.Errorf("eventsource: event missing parents (a root event carries [])")
	}
	e.Raw = canon
	sum := sha256.Sum256(canon)
	e.ID = "sha256:" + hex.EncodeToString(sum[:])
	return e, nil
}

// payloadFields decodes the payload's top-level keys, leaving values raw.
func (e *Event) payloadFields() map[string]json.RawMessage {
	if len(e.Payload) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(e.Payload, &m); err != nil {
		return nil
	}
	return m
}

// PayloadString returns payload[key] when it is a JSON string.
func (e *Event) PayloadString(key string) (string, bool) {
	raw, ok := e.payloadFields()[key]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// PayloadStringOrList returns payload[key] as a list, accepting either a
// single JSON string or an array of strings. The D6 membership delta may
// be either shape; a JSON null yields nil.
func (e *Event) PayloadStringOrList(key string) []string {
	raw, ok := e.payloadFields()[key]
	if !ok {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return []string{s}
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list
	}
	return nil
}

// CompareEvents is the deterministic total order over events (L0-4):
// lexicographic on (hlc.p, hlc.l, replica), with the event id as a final
// defensive tiebreak — two _v1 chains from different upgraded projects can
// collide on the triple after a cross-project merge (spec decision 11).
func CompareEvents(a, b *Event) int {
	if c := a.HLC.Compare(b.HLC); c != 0 {
		return c
	}
	if c := strings.Compare(a.Replica, b.Replica); c != 0 {
		return c
	}
	return strings.Compare(a.ID, b.ID)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/eventsource/`
Expected: `ok`

- [ ] **Step 5: Commit**

```bash
git add internal/eventsource/event.go internal/eventsource/event_test.go
git commit -m "feat(ATM-0106): v2 event envelope, parse, and content-addressed identity"
```

---

### Task 4: Replica identity

**Files:**
- Create: `internal/eventsource/replica.go`
- Create: `internal/eventsource/replica_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `const ReplicaV1 = "_v1"`, `func MintReplicaID(r io.Reader) (string, error)` — `"r_"` + 26 lowercase Crockford base32 chars encoding 128 random bits.

- [ ] **Step 1: Write the failing test**

Create `internal/eventsource/replica_test.go`:

```go
package eventsource

import (
	"bytes"
	"strings"
	"testing"
)

func TestMintReplicaIDShape(t *testing.T) {
	id, err := MintReplicaID(bytes.NewReader(bytes.Repeat([]byte{0xA7}, 16)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id, "r_") || len(id) != 2+26 {
		t.Fatalf("id = %q, want r_ + 26 chars", id)
	}
	const crockford = "0123456789abcdefghjkmnpqrstvwxyz"
	for _, r := range id[2:] {
		if !strings.ContainsRune(crockford, r) {
			t.Fatalf("id %q contains non-Crockford char %q", id, r)
		}
	}
}

func TestMintReplicaIDIsDeterministicPerEntropy(t *testing.T) {
	a, err := MintReplicaID(bytes.NewReader(bytes.Repeat([]byte{0x42}, 16)))
	if err != nil {
		t.Fatal(err)
	}
	b, err := MintReplicaID(bytes.NewReader(bytes.Repeat([]byte{0x42}, 16)))
	if err != nil {
		t.Fatal(err)
	}
	c, err := MintReplicaID(bytes.NewReader(bytes.Repeat([]byte{0x43}, 16)))
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("same entropy, different ids: %s vs %s", a, b)
	}
	if a == c {
		t.Errorf("different entropy, same id: %s", a)
	}
}

func TestMintReplicaIDZeroEntropy(t *testing.T) {
	id, err := MintReplicaID(bytes.NewReader(make([]byte, 16)))
	if err != nil {
		t.Fatal(err)
	}
	if id != "r_"+strings.Repeat("0", 26) {
		t.Errorf("zero entropy id = %q", id)
	}
}

func TestMintReplicaIDShortEntropyFails(t *testing.T) {
	if _, err := MintReplicaID(bytes.NewReader([]byte{1, 2, 3})); err == nil {
		t.Error("expected error for short entropy source")
	}
}

func TestReplicaV1IsReserved(t *testing.T) {
	if ReplicaV1 != "_v1" {
		t.Errorf("ReplicaV1 = %q", ReplicaV1)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/eventsource/`
Expected: `FAIL` with `undefined: MintReplicaID` (build failure).

- [ ] **Step 3: Write minimal implementation**

Create `internal/eventsource/replica.go`:

```go
package eventsource

import (
	"fmt"
	"io"
	"math/big"
)

// ReplicaV1 is the reserved replica id used exclusively by the D6 upgrade
// so that every machine upgrading the same v1 log derives byte-identical
// events. It MUST never be minted for a live replica (L0-3).
const ReplicaV1 = "_v1"

// crockford32 is Crockford base32, lowercase (no i, l, o, u).
const crockford32 = "0123456789abcdefghjkmnpqrstvwxyz"

// MintReplicaID mints a fresh replica id — "r_" + 26 Crockford base32
// characters encoding 128 bits read from r (callers pass crypto/rand's
// Reader; tests pass fixed bytes). A replica id is local state: embedded
// in authored events, never itself synced as content.
func MintReplicaID(r io.Reader) (string, error) {
	var buf [16]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return "", fmt.Errorf("eventsource: mint replica id: %w", err)
	}
	n := new(big.Int).SetBytes(buf[:])
	base := big.NewInt(32)
	mod := new(big.Int)
	out := make([]byte, 26)
	for i := 25; i >= 0; i-- {
		n.DivMod(n, base, mod)
		out[i] = crockford32[mod.Int64()]
	}
	return "r_" + string(out), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/eventsource/`
Expected: `ok`

- [ ] **Step 5: Commit**

```bash
git add internal/eventsource/replica.go internal/eventsource/replica_test.go
git commit -m "feat(ATM-0106): replica id minting"
```

---

### Task 5: Action vocabulary + authoring path

**Files:**
- Create: `internal/eventsource/action.go`
- Create: `internal/eventsource/author.go`
- Create: `internal/eventsource/author_test.go`

**Interfaces:**
- Consumes: `Canonicalize`, `Clock`, `HLC`, `Subject`, `Parse`, `ReplicaV1`.
- Produces: action constants (`ActionProjectCreated` … `ActionTaskRestored`, same strings as `internal/store/log.go:18-34` plus `task.restored`, minus nothing — `task.meta-changed` is deliberately NOT a constant here), `type Draft struct{ At time.Time; Actor, Action string; Subject Subject; Payload map[string]any }`, `func NewEvent(clock *Clock, replica string, parents []string, d Draft) (*Event, error)`.

- [ ] **Step 1: Write the failing test**

Create `internal/eventsource/author_test.go`:

```go
package eventsource

import (
	"strings"
	"testing"
	"time"
)

func fixedClock() *Clock {
	t := int64(1752480000000)
	return NewClock(func() int64 { t++; return t })
}

var testAt = time.Date(2026, 7, 14, 9, 12, 3, 0, time.UTC)

func TestNewEventAuthorsParsableEvent(t *testing.T) {
	e, err := NewEvent(fixedClock(), "r_00000000000000000000000001", nil, Draft{
		At:     testAt,
		Actor:  "developer@claude:test",
		Action: ActionTaskCreated,
		Subject: Subject{Kind: "task"},
		Payload: map[string]any{"alias": "ATM-7f3a2b", "title": "Fix the cache"},
	})
	if err != nil {
		t.Fatal(err)
	}
	reparsed, err := Parse(e.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if reparsed.ID != e.ID {
		t.Errorf("authored event does not round-trip: %s vs %s", reparsed.ID, e.ID)
	}
	if e.Parents == nil || len(e.Parents) != 0 {
		t.Errorf("root event parents = %#v, want []", e.Parents)
	}
	if got, _ := e.PayloadString("title"); got != "Fix the cache" {
		t.Errorf("payload title = %q", got)
	}
}

func TestNewEventIsDeterministic(t *testing.T) {
	mk := func() *Event {
		e, err := NewEvent(fixedClock(), "r_00000000000000000000000001", []string{"sha256:bbb", "sha256:aaa"}, Draft{
			At:      testAt,
			Actor:   "developer@claude:test",
			Action:  ActionTaskTitleChanged,
			Subject: Subject{Kind: "task", ID: "sha256:eee"},
			Payload: map[string]any{"title": "x"},
		})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	a, b := mk(), mk()
	if a.ID != b.ID || string(a.Raw) != string(b.Raw) {
		t.Errorf("same inputs, different events:\n%s\n%s", a.Raw, b.Raw)
	}
}

func TestNewEventSortsAndDedupesParents(t *testing.T) {
	e, err := NewEvent(fixedClock(), "r_00000000000000000000000001", []string{"sha256:bbb", "sha256:aaa", "sha256:bbb"}, Draft{
		At:      testAt,
		Actor:   "a",
		Action:  ActionTaskRemoved,
		Subject: Subject{Kind: "task", ID: "sha256:eee"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Parents) != 2 || e.Parents[0] != "sha256:aaa" || e.Parents[1] != "sha256:bbb" {
		t.Errorf("parents = %v", e.Parents)
	}
}

func TestNewEventOmitsEmptyPayload(t *testing.T) {
	e, err := NewEvent(fixedClock(), "r_00000000000000000000000001", nil, Draft{
		At:      testAt,
		Actor:   "a",
		Action:  ActionTaskRemoved,
		Subject: Subject{Kind: "task", ID: "sha256:eee"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(e.Raw), `"payload"`) {
		t.Errorf("empty payload serialized: %s", e.Raw)
	}
}

func TestNewEventRejectsReservedOrEmptyReplica(t *testing.T) {
	for _, replica := range []string{"", ReplicaV1} {
		_, err := NewEvent(fixedClock(), replica, nil, Draft{
			At: testAt, Actor: "a", Action: ActionTaskRemoved, Subject: Subject{Kind: "task", ID: "sha256:eee"},
		})
		if err == nil {
			t.Errorf("replica %q accepted", replica)
		}
	}
}

func TestNewEventTicksClock(t *testing.T) {
	clock := fixedClock()
	e1, err := NewEvent(clock, "r_00000000000000000000000001", nil, Draft{
		At: testAt, Actor: "a", Action: ActionTaskRemoved, Subject: Subject{Kind: "task", ID: "sha256:eee"},
	})
	if err != nil {
		t.Fatal(err)
	}
	e2, err := NewEvent(clock, "r_00000000000000000000000001", []string{e1.ID}, Draft{
		At: testAt, Actor: "a", Action: ActionTaskRestored, Subject: Subject{Kind: "task", ID: "sha256:eee"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if CompareEvents(e1, e2) >= 0 {
		t.Errorf("later event does not sort after earlier: %v vs %v", e1.HLC, e2.HLC)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/eventsource/`
Expected: `FAIL` with `undefined: NewEvent` (build failure).

- [ ] **Step 3: Write the implementation**

Create `internal/eventsource/action.go`:

```go
package eventsource

// The v2 action vocabulary (the L2 action table). Strings are identical to
// v1 (internal/store/log.go) with two deliberate differences:
// task.restored is new (deletion must not be irreversible, L2-4), and
// task.meta-changed has no constant — it is retired. A v1 instance rides
// through the D6 upgrade and, like any unknown action, participates in
// causality but writes no slots (D5, spec decision 8).
const (
	ActionProjectCreated      = "project.created"
	ActionProjectNameChanged  = "project.name-changed"
	ActionProjectRemoved      = "project.removed"
	ActionTaskCreated         = "task.created"
	ActionTaskTitleChanged    = "task.title-changed"
	ActionTaskDescChanged     = "task.description-changed"
	ActionTaskLabelAdded      = "task.label-added"
	ActionTaskLabelRemoved    = "task.label-removed"
	ActionTaskRemoved         = "task.removed"
	ActionTaskRestored        = "task.restored"
	ActionLabelUpserted       = "label.upserted"
	ActionLabelRemoved        = "label.removed"
	ActionCommentCreated      = "comment.created"
	ActionCommentBodyChanged  = "comment.body-changed"
	ActionCommentLabelAdded   = "comment.label-added"
	ActionCommentLabelRemoved = "comment.label-removed"
	ActionCommentRemoved      = "comment.removed"
)
```

Create `internal/eventsource/author.go`:

```go
package eventsource

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// Draft is the caller-supplied part of a new event. Payload values must
// marshal to JSON; the keys the fold reads per action are defined by
// writesOf in fold.go.
type Draft struct {
	At      time.Time
	Actor   string
	Action  string
	Subject Subject
	Payload map[string]any
}

// NewEvent authors a v2 event: it ticks the clock, sorts and dedupes
// parents (array order must not fork identities, spec decision 12), and
// derives the identity by funnelling through Parse — one code path for
// every id in the system. replica must be a minted id; ReplicaV1 is
// reserved for the D6 upgrade.
func NewEvent(clock *Clock, replica string, parents []string, d Draft) (*Event, error) {
	if replica == "" || replica == ReplicaV1 {
		return nil, fmt.Errorf("eventsource: invalid replica id %q", replica)
	}
	if d.Action == "" || d.Subject.Kind == "" {
		return nil, fmt.Errorf("eventsource: action and subject.kind are required")
	}
	ps := slices.Clone(parents)
	slices.Sort(ps)
	ps = slices.Compact(ps)
	if ps == nil {
		ps = []string{}
	}
	at := d.At
	if at.IsZero() {
		at = time.Now()
	}
	obj := map[string]any{
		"v":       2,
		"parents": ps,
		"hlc":     clock.Tick(),
		"replica": replica,
		"at":      at.UTC().Format(time.RFC3339Nano),
		"actor":   d.Actor,
		"action":  d.Action,
		"subject": d.Subject,
	}
	if len(d.Payload) > 0 {
		obj["payload"] = d.Payload
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("eventsource: marshal draft: %w", err)
	}
	return Parse(raw)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/eventsource/`
Expected: `ok`

- [ ] **Step 5: Commit**

```bash
git add internal/eventsource/action.go internal/eventsource/author.go internal/eventsource/author_test.go
git commit -m "feat(ATM-0106): action vocabulary and event authoring path"
```

---

### Task 6: Hash-DAG — reachability, concurrency, frontier

**Files:**
- Create: `internal/eventsource/dag.go`
- Create: `internal/eventsource/dag_test.go`
- Create: `internal/eventsource/helpers_test.go`

**Interfaces:**
- Consumes: `Event`, `CompareEvents`, `NewEvent` (tests).
- Produces: `func BuildDAG(events []*Event) (*DAG, error)`, `(*DAG).Reaches(anc, desc string) bool` (strict happens-before: `Reaches(x, x) == false`), `(*DAG).Concurrent(a, b string) bool`, `(*DAG).Frontier() []string` (sorted), `(*DAG).Events() []*Event` (deterministic topological order, parents before children), `(*DAG).Get(id string) *Event`. Test helpers `testEvent`, `testClock` for all later tasks.

- [ ] **Step 1: Write the shared test helpers**

Create `internal/eventsource/helpers_test.go`:

```go
package eventsource

import (
	"testing"
	"time"
)

// testClock returns a deterministic Clock advancing one millisecond per
// reading, starting just above start.
func testClock(start int64) *Clock {
	t := start
	return NewClock(func() int64 { t++; return t })
}

// testEvent authors an event for scenario tests, failing the test on error.
func testEvent(t *testing.T, clock *Clock, replica string, parents []string, action string, subj Subject, payload map[string]any) *Event {
	t.Helper()
	e, err := NewEvent(clock, replica, parents, Draft{
		At:      time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC),
		Actor:   "developer@claude:test",
		Action:  action,
		Subject: subj,
		Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

const (
	replicaA = "r_aaaaaaaaaaaaaaaaaaaaaaaaaa"
	replicaB = "r_bbbbbbbbbbbbbbbbbbbbbbbbbb"
)
```

- [ ] **Step 2: Write the failing test**

Create `internal/eventsource/dag_test.go`:

```go
package eventsource

import (
	"slices"
	"testing"
)

// diamond builds: base ← a (replica A), base ← b (replica B), a,b ← tip.
func diamond(t *testing.T) (base, a, b, tip *Event) {
	t.Helper()
	ca, cb := testClock(1000), testClock(2000)
	base = testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"}, map[string]any{"alias": "T-1", "title": "t"})
	a = testEvent(t, ca, replicaA, []string{base.ID}, ActionTaskTitleChanged, Subject{Kind: "task", ID: base.ID}, map[string]any{"title": "from A"})
	b = testEvent(t, cb, replicaB, []string{base.ID}, ActionTaskTitleChanged, Subject{Kind: "task", ID: base.ID}, map[string]any{"title": "from B"})
	tip = testEvent(t, ca, replicaA, []string{a.ID, b.ID}, ActionTaskDescChanged, Subject{Kind: "task", ID: base.ID}, map[string]any{"description": "d"})
	return base, a, b, tip
}

func TestBuildDAGReachability(t *testing.T) {
	base, a, b, tip := diamond(t)
	d, err := BuildDAG([]*Event{tip, b, a, base}) // arrival order shuffled on purpose
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		anc, desc *Event
		want      bool
	}{
		{base, a, true}, {base, b, true}, {base, tip, true},
		{a, tip, true}, {b, tip, true},
		{a, b, false}, {b, a, false},
		{tip, base, false}, {a, base, false},
		{a, a, false}, // strict: an event does not happen-before itself
	} {
		if got := d.Reaches(tc.anc.ID, tc.desc.ID); got != tc.want {
			t.Errorf("Reaches(%s→%s) = %v, want %v", tc.anc.ID[:14], tc.desc.ID[:14], got, tc.want)
		}
	}
	if !d.Concurrent(a.ID, b.ID) || d.Concurrent(a.ID, tip.ID) || d.Concurrent(a.ID, a.ID) {
		t.Error("Concurrent wrong")
	}
}

func TestBuildDAGFrontier(t *testing.T) {
	base, a, b, tip := diamond(t)
	d, err := BuildDAG([]*Event{base, a, b})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{a.ID, b.ID}
	slices.Sort(want)
	if got := d.Frontier(); !slices.Equal(got, want) {
		t.Errorf("frontier = %v, want %v", got, want)
	}
	d2, err := BuildDAG([]*Event{base, a, b, tip})
	if err != nil {
		t.Fatal(err)
	}
	if got := d2.Frontier(); !slices.Equal(got, []string{tip.ID}) {
		t.Errorf("frontier = %v, want [tip]", got)
	}
}

func TestBuildDAGDedupesAndOrdersDeterministically(t *testing.T) {
	base, a, b, tip := diamond(t)
	d1, err := BuildDAG([]*Event{base, a, b, tip, a, base})
	if err != nil {
		t.Fatal(err)
	}
	if len(d1.Events()) != 4 {
		t.Fatalf("dedup failed: %d events", len(d1.Events()))
	}
	d2, err := BuildDAG([]*Event{tip, b, a, base})
	if err != nil {
		t.Fatal(err)
	}
	for i := range d1.Events() {
		if d1.Events()[i].ID != d2.Events()[i].ID {
			t.Fatalf("topo order depends on arrival order at index %d", i)
		}
	}
	// Parents always precede children.
	pos := map[string]int{}
	for i, e := range d1.Events() {
		pos[e.ID] = i
	}
	for _, e := range d1.Events() {
		for _, p := range e.Parents {
			if pos[p] >= pos[e.ID] {
				t.Errorf("parent %s after child %s", p[:14], e.ID[:14])
			}
		}
	}
}

func TestBuildDAGRejectsMissingParent(t *testing.T) {
	base, a, _, _ := diamond(t)
	_ = base
	if _, err := BuildDAG([]*Event{a}); err == nil {
		t.Error("expected error for missing parent")
	}
}

func TestBuildDAGRejectsCycle(t *testing.T) {
	// A parent cycle is impossible for honest hashes; forge one to prove
	// the builder terminates with an error instead of hanging.
	x := &Event{ID: "sha256:x", Parents: []string{"sha256:y"}}
	y := &Event{ID: "sha256:y", Parents: []string{"sha256:x"}}
	if _, err := BuildDAG([]*Event{x, y}); err == nil {
		t.Error("expected error for cycle")
	}
}

func TestDAGGet(t *testing.T) {
	base, a, b, tip := diamond(t)
	d, err := BuildDAG([]*Event{base, a, b, tip})
	if err != nil {
		t.Fatal(err)
	}
	if d.Get(a.ID) != a {
		t.Error("Get returned wrong event")
	}
	if d.Get("sha256:nope") != nil {
		t.Error("Get on unknown id should be nil")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/eventsource/`
Expected: `FAIL` with `undefined: BuildDAG` (build failure).

- [ ] **Step 4: Write the implementation**

Create `internal/eventsource/dag.go`:

```go
package eventsource

import (
	"fmt"
	"slices"
	"sort"
)

// DAG indexes an event set for causal-ancestry queries (L0). Reachability
// is precomputed as ancestor bitsets: O(1) Reaches after O(V·E/64)
// construction — plenty for ATM-scale logs.
type DAG struct {
	events []*Event       // deterministic topological order
	index  map[string]int // event id → position in events
	anc    [][]uint64     // anc[i] = bitset of events[i]'s ancestors
}

// BuildDAG deduplicates events by id, verifies every parent is present,
// fixes a deterministic topological order (Kahn's algorithm, ready set
// ordered by CompareEvents), and computes ancestor bitsets. A missing
// parent or a cycle (impossible for honest hashes) is an error.
func BuildDAG(events []*Event) (*DAG, error) {
	uniq := make([]*Event, 0, len(events))
	byID := make(map[string]*Event, len(events))
	for _, e := range events {
		if byID[e.ID] == nil {
			byID[e.ID] = e
			uniq = append(uniq, e)
		}
	}
	parentsOf := func(e *Event) []string {
		ps := slices.Clone(e.Parents)
		slices.Sort(ps)
		return slices.Compact(ps) // wire events may repeat a parent
	}
	children := map[string][]string{}
	indeg := make(map[string]int, len(uniq))
	for _, e := range uniq {
		ps := parentsOf(e)
		for _, p := range ps {
			if byID[p] == nil {
				return nil, fmt.Errorf("eventsource: event %s references missing parent %s", e.ID, p)
			}
			children[p] = append(children[p], e.ID)
		}
		indeg[e.ID] = len(ps)
	}
	var ready []*Event
	for _, e := range uniq {
		if indeg[e.ID] == 0 {
			ready = append(ready, e)
		}
	}
	d := &DAG{index: make(map[string]int, len(uniq))}
	for len(ready) > 0 {
		sort.Slice(ready, func(i, j int) bool { return CompareEvents(ready[i], ready[j]) < 0 })
		e := ready[0]
		ready = ready[1:]
		d.index[e.ID] = len(d.events)
		d.events = append(d.events, e)
		for _, cid := range children[e.ID] {
			indeg[cid]--
			if indeg[cid] == 0 {
				ready = append(ready, byID[cid])
			}
		}
	}
	if len(d.events) != len(uniq) {
		return nil, fmt.Errorf("eventsource: parent cycle detected")
	}
	words := (len(d.events) + 63) / 64
	d.anc = make([][]uint64, len(d.events))
	for i, e := range d.events {
		set := make([]uint64, words)
		for _, p := range parentsOf(e) {
			pi := d.index[p]
			set[pi/64] |= 1 << (pi % 64)
			for w, v := range d.anc[pi] {
				set[w] |= v
			}
		}
		d.anc[i] = set
	}
	return d, nil
}

// Reaches reports whether anc happens-before desc: anc is reachable from
// desc by following parents. Strict — an event does not reach itself.
func (d *DAG) Reaches(anc, desc string) bool {
	ai, ok := d.index[anc]
	if !ok {
		return false
	}
	di, ok := d.index[desc]
	if !ok {
		return false
	}
	return d.anc[di][ai/64]&(1<<(ai%64)) != 0
}

// Concurrent reports whether two events are causally unrelated — the only
// definition of concurrency in the suite (L0).
func (d *DAG) Concurrent(a, b string) bool {
	return a != b && !d.Reaches(a, b) && !d.Reaches(b, a)
}

// Frontier returns the ids of events that are not a parent of any held
// event — the parents of the next authored event. Sorted ascending.
func (d *DAG) Frontier() []string {
	isParent := map[string]bool{}
	for _, e := range d.events {
		for _, p := range e.Parents {
			isParent[p] = true
		}
	}
	out := make([]string, 0, 1)
	for _, e := range d.events {
		if !isParent[e.ID] {
			out = append(out, e.ID)
		}
	}
	slices.Sort(out)
	return out
}

// Events returns the events in deterministic topological order.
func (d *DAG) Events() []*Event { return d.events }

// Get returns the event with the given id, or nil.
func (d *DAG) Get(id string) *Event {
	i, ok := d.index[id]
	if !ok {
		return nil
	}
	return d.events[i]
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/eventsource/`
Expected: `ok`

- [ ] **Step 6: Commit**

```bash
git add internal/eventsource/dag.go internal/eventsource/dag_test.go internal/eventsource/helpers_test.go
git commit -m "feat(ATM-0106): hash-DAG with ancestor bitsets and frontier"
```

---

### Task 7: Slot writes and the maximal-writer rule

**Files:**
- Create: `internal/eventsource/fold.go` (first half — Task 8 extends this same file)
- Create: `internal/eventsource/fold_slots_test.go`

**Interfaces:**
- Consumes: `Event`, `DAG`, `CompareEvents`, action constants.
- Produces (package-internal, consumed by Task 8): `type slotKey struct{ entity, kind, field string }`, `type slotWrite struct{ slot slotKey; event *Event; value string }`, `func writesOf(e *Event) []slotWrite`, `func collectWrites(d *DAG) map[slotKey][]slotWrite`, `func maximalWriters(d *DAG, ws []slotWrite) []slotWrite`. Exported: `SlotScalar`, `SlotMembership`, `SlotExistence` (strings `"scalar"`, `"membership"`, `"existence"`), `type ContestedSlot struct{ Entity, Kind, Field string; Writers []string }`.

Slot write semantics (the L2 action table, plus spec decisions 6–8):

| Action | Slots written | value |
|---|---|---|
| `project.created` | scalar `(self, "name")` | payload `name` |
| `task.created` | scalar `(self, "title")`, `(self, "description")`; membership `(self, L)` per payload `labels` | payload values; `"add"` |
| `comment.created` | scalar `(self, "body")`; membership per payload `labels` | payload value; `"add"` |
| `*.title/description/body/name-changed` | the one scalar slot named by the action, entity from `subject.id` | the payload field |
| `label.upserted` | scalar `(name, "label.description")` / `(name, "label.expr")` — only for keys present in payload; existence `(name)` | payload values; `"live"` |
| `task/comment.label-added` | membership `(subject.id, L)` per payload `label` (string or list) | `"add"` |
| `task/comment.label-removed` | membership per payload `label` | `"remove"` |
| `project/task/comment.removed`, `label.removed` | existence | `"tombstone"` |
| `task.restored` | existence | `"live"` |
| anything else (incl. `task.meta-changed`) | none — inert but causal | — |

Note: membership actions read ONLY the payload `label` key — never `labels`, which on an upgraded v1 event is the whole-entity snapshot (on a `-removed` event it lists the *remaining* labels and would corrupt the delta). Creation desugar reads `labels` (the initial set) — the two keys coexist by design (spec decision 4).

- [ ] **Step 1: Write the failing test**

Create `internal/eventsource/fold_slots_test.go`:

```go
package eventsource

import (
	"testing"
)

func TestWritesOfActionTable(t *testing.T) {
	c := testClock(1000)
	created := testEvent(t, c, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "t", "labels": []string{"P:x", "P:y"}})
	ws := writesOf(created)
	if len(ws) != 4 { // title, description, membership x2
		t.Fatalf("task.created writes %d slots, want 4: %+v", len(ws), ws)
	}
	if ws[0].slot != (slotKey{created.ID, SlotScalar, "title"}) || ws[0].value != "t" {
		t.Errorf("title write = %+v", ws[0])
	}
	if ws[1].slot != (slotKey{created.ID, SlotScalar, "description"}) || ws[1].value != "" {
		t.Errorf("description write = %+v", ws[1])
	}
	if ws[2].slot != (slotKey{created.ID, SlotMembership, "P:x"}) || ws[2].value != "add" {
		t.Errorf("membership write = %+v", ws[2])
	}

	title := testEvent(t, c, replicaA, []string{created.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"title": "t2"})
	ws = writesOf(title)
	if len(ws) != 1 || ws[0].slot != (slotKey{created.ID, SlotScalar, "title"}) || ws[0].value != "t2" {
		t.Errorf("title-changed writes = %+v", ws)
	}

	// Membership delta reads `label`, never the snapshot `labels`.
	add := testEvent(t, c, replicaA, []string{title.ID}, ActionTaskLabelAdded,
		Subject{Kind: "task", ID: created.ID},
		map[string]any{"label": "P:z", "labels": []string{"P:x", "P:y", "P:z"}})
	ws = writesOf(add)
	if len(ws) != 1 || ws[0].slot != (slotKey{created.ID, SlotMembership, "P:z"}) || ws[0].value != "add" {
		t.Errorf("label-added writes = %+v", ws)
	}

	rm := testEvent(t, c, replicaA, []string{add.ID}, ActionTaskRemoved,
		Subject{Kind: "task", ID: created.ID}, nil)
	ws = writesOf(rm)
	if len(ws) != 1 || ws[0].slot != (slotKey{created.ID, SlotExistence, ""}) || ws[0].value != "tombstone" {
		t.Errorf("removed writes = %+v", ws)
	}

	restore := testEvent(t, c, replicaA, []string{rm.ID}, ActionTaskRestored,
		Subject{Kind: "task", ID: created.ID}, nil)
	ws = writesOf(restore)
	if len(ws) != 1 || ws[0].value != "live" {
		t.Errorf("restored writes = %+v", ws)
	}

	// label.upserted: scalar slots only for keys present, plus existence "live".
	upsert := testEvent(t, c, replicaA, []string{restore.ID}, ActionLabelUpserted,
		Subject{Kind: "label", Name: "P:x"}, map[string]any{"description": "d"})
	ws = writesOf(upsert)
	if len(ws) != 2 {
		t.Fatalf("label.upserted writes = %+v", ws)
	}
	if ws[0].slot != (slotKey{"P:x", SlotScalar, "label.description"}) || ws[0].value != "d" {
		t.Errorf("upsert description write = %+v", ws[0])
	}
	if ws[1].slot != (slotKey{"P:x", SlotExistence, ""}) || ws[1].value != "live" {
		t.Errorf("upsert existence write = %+v", ws[1])
	}

	// Retired/unknown actions are inert.
	meta := testEvent(t, c, replicaA, []string{upsert.ID}, "task.meta-changed",
		Subject{Kind: "task", ID: created.ID}, map[string]any{"next_comment_n": 3})
	if ws := writesOf(meta); len(ws) != 0 {
		t.Errorf("meta-changed should be inert, writes = %+v", ws)
	}
}

func TestMaximalWritersDominatedWriteIsIgnored(t *testing.T) {
	c := testClock(1000)
	created := testEvent(t, c, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "v0"})
	e1 := testEvent(t, c, replicaA, []string{created.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"title": "v1"})
	e2 := testEvent(t, c, replicaA, []string{e1.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"title": "v2"})
	d, err := BuildDAG([]*Event{created, e1, e2})
	if err != nil {
		t.Fatal(err)
	}
	ws := collectWrites(d)[slotKey{created.ID, SlotScalar, "title"}]
	if len(ws) != 3 {
		t.Fatalf("collected %d writers, want 3", len(ws))
	}
	m := maximalWriters(d, ws)
	if len(m) != 1 || m[0].event.ID != e2.ID {
		t.Fatalf("maximal writers = %+v, want just e2", m)
	}
}

func TestMaximalWritersConcurrentWritesAreBothMaximal(t *testing.T) {
	ca, cb := testClock(1000), testClock(2000)
	created := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "v0"})
	a := testEvent(t, ca, replicaA, []string{created.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"title": "from A"})
	b := testEvent(t, cb, replicaB, []string{created.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"title": "from B"})
	d, err := BuildDAG([]*Event{created, a, b})
	if err != nil {
		t.Fatal(err)
	}
	m := maximalWriters(d, collectWrites(d)[slotKey{created.ID, SlotScalar, "title"}])
	if len(m) != 2 {
		t.Fatalf("maximal writers = %d, want 2 (contested)", len(m))
	}
	// Sorted ascending by the HLC total order: the LWW winner is last.
	if m[0].event.ID != a.ID || m[1].event.ID != b.ID {
		t.Errorf("order = [%s, %s], want [a, b]", m[0].event.ID[:14], m[1].event.ID[:14])
	}
}

func TestCollectWritesDedupesPerEventAndSlot(t *testing.T) {
	c := testClock(1000)
	created := testEvent(t, c, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "t", "labels": []string{"P:x", "P:x"}})
	d, err := BuildDAG([]*Event{created})
	if err != nil {
		t.Fatal(err)
	}
	ws := collectWrites(d)[slotKey{created.ID, SlotMembership, "P:x"}]
	if len(ws) != 1 {
		t.Fatalf("duplicate payload label produced %d writes, want 1", len(ws))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/eventsource/`
Expected: `FAIL` with `undefined: writesOf` (build failure).

- [ ] **Step 3: Write the implementation**

Create `internal/eventsource/fold.go` (Task 8 appends to this file):

```go
package eventsource

import (
	"sort"
)

// Slot kinds (L2). Every mutable piece of state is a slot; one rule — the
// maximal-writer rule — governs all of them.
const (
	SlotScalar     = "scalar"
	SlotMembership = "membership"
	SlotExistence  = "existence"
)

// slotKey names one mutable piece of state.
type slotKey struct {
	entity string // entity identity; label name for label entities
	kind   string
	field  string // scalar: field name; membership: label name; existence: ""
}

// slotWrite is one event's write to one slot. value is the written scalar
// value, "add"/"remove" for membership, or "live"/"tombstone" for
// existence.
type slotWrite struct {
	slot  slotKey
	event *Event
	value string
}

// ContestedSlot reports a slot with more than one maximal writer (L2-1).
// Writers are sorted ascending by the HLC total order, so for a scalar
// slot the last writer is the LWW winner. Reported structurally — filtering
// same-outcome noise is board-vocabulary policy, not fold policy (spec
// decision 9). Membership slots of computed labels are inert and never
// reported.
type ContestedSlot struct {
	Entity  string
	Kind    string
	Field   string
	Writers []string
}

// writesOf lists the slot writes an event makes — the L2 action table.
// Unknown actions (including the retired task.meta-changed riding through
// the D6 upgrade) write nothing: they are preserved in the DAG and
// participate in causality, but no rule reads them (D5).
func writesOf(e *Event) []slotWrite {
	str := func(key string) string { s, _ := e.PayloadString(key); return s }
	w := func(entity, kind, field, value string) slotWrite {
		return slotWrite{slot: slotKey{entity, kind, field}, event: e, value: value}
	}
	var out []slotWrite
	switch e.Action {
	case ActionProjectCreated:
		out = append(out, w(e.ID, SlotScalar, "name", str("name")))
	case ActionProjectNameChanged:
		out = append(out, w(e.Subject.ID, SlotScalar, "name", str("name")))
	case ActionTaskCreated:
		out = append(out,
			w(e.ID, SlotScalar, "title", str("title")),
			w(e.ID, SlotScalar, "description", str("description")))
		for _, l := range e.PayloadStringOrList("labels") {
			out = append(out, w(e.ID, SlotMembership, l, "add"))
		}
	case ActionTaskTitleChanged:
		out = append(out, w(e.Subject.ID, SlotScalar, "title", str("title")))
	case ActionTaskDescChanged:
		out = append(out, w(e.Subject.ID, SlotScalar, "description", str("description")))
	case ActionCommentCreated:
		out = append(out, w(e.ID, SlotScalar, "body", str("body")))
		for _, l := range e.PayloadStringOrList("labels") {
			out = append(out, w(e.ID, SlotMembership, l, "add"))
		}
	case ActionCommentBodyChanged:
		out = append(out, w(e.Subject.ID, SlotScalar, "body", str("body")))
	case ActionTaskLabelAdded, ActionCommentLabelAdded:
		for _, l := range e.PayloadStringOrList("label") {
			out = append(out, w(e.Subject.ID, SlotMembership, l, "add"))
		}
	case ActionTaskLabelRemoved, ActionCommentLabelRemoved:
		for _, l := range e.PayloadStringOrList("label") {
			out = append(out, w(e.Subject.ID, SlotMembership, l, "remove"))
		}
	case ActionLabelUpserted:
		name := e.Subject.Name
		fields := e.payloadFields()
		if _, ok := fields["description"]; ok {
			out = append(out, w(name, SlotScalar, "label.description", str("description")))
		}
		if _, ok := fields["expr"]; ok {
			out = append(out, w(name, SlotScalar, "label.expr", str("expr")))
		}
		// An upsert resurrects a removed label (spec decision 6): it
		// writes existence "live", causally dominating any tombstone it
		// observed; concurrent upsert‖remove resolves live (keep beats
		// drop, L2-2).
		out = append(out, w(name, SlotExistence, "", "live"))
	case ActionLabelRemoved:
		out = append(out, w(e.Subject.Name, SlotExistence, "", "tombstone"))
	case ActionProjectRemoved, ActionTaskRemoved, ActionCommentRemoved:
		out = append(out, w(e.Subject.ID, SlotExistence, "", "tombstone"))
	case ActionTaskRestored:
		out = append(out, w(e.Subject.ID, SlotExistence, "", "live"))
	}
	return out
}

// collectWrites groups every event's slot writes by slot, deduplicating a
// same-event double write (e.g. a duplicated payload label) so an event
// can never contest with itself.
func collectWrites(d *DAG) map[slotKey][]slotWrite {
	bySlot := map[slotKey][]slotWrite{}
	seen := map[slotKey]map[string]bool{}
	for _, e := range d.Events() {
		for _, w := range writesOf(e) {
			if w.slot.entity == "" {
				continue // malformed subject: nothing to attach to
			}
			if seen[w.slot] == nil {
				seen[w.slot] = map[string]bool{}
			}
			if seen[w.slot][e.ID] {
				continue
			}
			seen[w.slot][e.ID] = true
			bySlot[w.slot] = append(bySlot[w.slot], w)
		}
	}
	return bySlot
}

// maximalWriters filters a slot's writes to those not causally dominated
// by another write of the same slot — the maximal-writer rule (L2-1). The
// result is sorted ascending by the HLC total order; a slot is contested
// iff more than one write survives.
func maximalWriters(d *DAG, ws []slotWrite) []slotWrite {
	var out []slotWrite
	for i, w := range ws {
		dominated := false
		for j, o := range ws {
			if i != j && d.Reaches(w.event.ID, o.event.ID) {
				dominated = true
				break
			}
		}
		if !dominated {
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool { return CompareEvents(out[i].event, out[j].event) < 0 })
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/eventsource/`
Expected: `ok`

- [ ] **Step 5: Commit**

```bash
git add internal/eventsource/fold.go internal/eventsource/fold_slots_test.go
git commit -m "feat(ATM-0106): slot writes and maximal-writer rule"
```

---

### Task 8: The fold — convergent State with contested slots

**Files:**
- Modify: `internal/eventsource/fold.go` (append to Task 7's file)
- Create: `internal/eventsource/fold_test.go`

**Interfaces:**
- Consumes: Task 7's machinery, `DAG`, `BuildDAG`.
- Produces: `func Fold(d *DAG) *State`, `func FoldEvents(events []*Event) (*State, error)`, `type State struct{ Projects map[string]*ProjectState; Tasks map[string]*TaskState; Comments map[string]*CommentState; Labels map[string]*LabelState; Contested []ContestedSlot; Frontier []string }`, `type EntityMeta struct{ ID, Alias string; Tombstoned bool; CreatedAt time.Time; CreatedBy string; CreatedHLC HLC; CreatedReplica string; UpdatedAt time.Time; UpdatedBy string }`, `type ProjectState struct{ EntityMeta; Code, Name string }`, `type TaskState struct{ EntityMeta; Title, Description string; Labels []string }`, `type CommentState struct{ EntityMeta; TaskRef, ReplyToRef, Body string; Labels []string }`, `type LabelState struct{ Name, Description, Expr string; Tombstoned bool; UpdatedAt time.Time; UpdatedBy string }`, `func (l *LabelState) IsComputed() bool`, `func (s *State) TasksByCreation() []*TaskState`, `func (s *State) CommentsByCreation(taskRef string) []*CommentState`.

Resolution rules (L2-2): scalar = highest `CompareEvents` among maximal writers; membership = present iff any maximal writer is `"add"` (add-wins); existence = live iff any maximal writer is `"live"`, else tombstoned iff any is `"tombstone"`, else live (restore-wins). Entities materialize only from creation events (labels: from their first `label.upserted`); dangling writes are inert (spec decision 10). Membership slots whose label is computed (`Expr` non-empty or name ends `":*"`) are inert and excluded from `Contested` (L2-6, spec decision 9). `UpdatedAt`/`UpdatedBy` come from the HLC-greatest maximal writer across the entity's slots.

- [ ] **Step 1: Write the failing test**

Create `internal/eventsource/fold_test.go`:

```go
package eventsource

import (
	"slices"
	"testing"
	"time"
)

// fold is a shorthand: build the DAG, fold, return state.
func fold(t *testing.T, events ...*Event) *State {
	t.Helper()
	st, err := FoldEvents(events)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestFoldLinearHistory(t *testing.T) {
	c := testClock(1000)
	created := testEvent(t, c, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "v0", "description": "d0", "labels": []string{"P:x"}})
	retitle := testEvent(t, c, replicaA, []string{created.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"title": "v1"})
	st := fold(t, created, retitle)
	task := st.Tasks[created.ID]
	if task == nil {
		t.Fatal("task missing")
	}
	if task.Alias != "T-1" || task.Title != "v1" || task.Description != "d0" || task.Tombstoned {
		t.Errorf("task = %+v", task)
	}
	if !slices.Equal(task.Labels, []string{"P:x"}) {
		t.Errorf("labels = %v", task.Labels)
	}
	if task.CreatedBy != "developer@claude:test" || task.CreatedHLC != created.HLC || task.CreatedReplica != replicaA {
		t.Errorf("creation meta = %+v", task.EntityMeta)
	}
	if len(st.Contested) != 0 {
		t.Errorf("linear history contested: %+v", st.Contested)
	}
	if !slices.Equal(st.Frontier, []string{retitle.ID}) {
		t.Errorf("frontier = %v", st.Frontier)
	}
}

func TestFoldScalarLWWAndContested(t *testing.T) {
	ca, cb := testClock(1000), testClock(2000) // B's stamps are later → B wins LWW
	created := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "v0"})
	a := testEvent(t, ca, replicaA, []string{created.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"title": "from A"})
	b := testEvent(t, cb, replicaB, []string{created.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"title": "from B"})
	st := fold(t, created, a, b)
	if got := st.Tasks[created.ID].Title; got != "from B" {
		t.Errorf("LWW winner = %q, want the higher HLC", got)
	}
	if len(st.Contested) != 1 {
		t.Fatalf("contested = %+v, want exactly the title slot", st.Contested)
	}
	cs := st.Contested[0]
	if cs.Entity != created.ID || cs.Kind != SlotScalar || cs.Field != "title" {
		t.Errorf("contested slot = %+v", cs)
	}
	if !slices.Equal(cs.Writers, []string{a.ID, b.ID}) {
		t.Errorf("writers = %v, want ascending [a, b]", cs.Writers)
	}

	// A resolution write parented on BOTH contested events becomes the
	// unique maximal writer: the slot stops being contested and board
	// membership evaporates (L2-5).
	fix := testEvent(t, ca, replicaA, []string{a.ID, b.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"title": "settled"})
	st = fold(t, created, a, b, fix)
	if st.Tasks[created.ID].Title != "settled" || len(st.Contested) != 0 {
		t.Errorf("resolution did not clear: title=%q contested=%+v", st.Tasks[created.ID].Title, st.Contested)
	}
}

func TestFoldMembershipAddWins(t *testing.T) {
	ca, cb := testClock(1000), testClock(2000)
	created := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "t", "labels": []string{"P:x"}})
	rm := testEvent(t, cb, replicaB, []string{created.ID}, ActionTaskLabelRemoved,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"label": "P:x"})
	add := testEvent(t, ca, replicaA, []string{created.ID}, ActionTaskLabelAdded,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"label": "P:x"})
	st := fold(t, created, rm, add)
	if !slices.Equal(st.Tasks[created.ID].Labels, []string{"P:x"}) {
		t.Errorf("labels = %v, want add-wins", st.Tasks[created.ID].Labels)
	}
	// A remove that OBSERVED the add wins (it dominates).
	rm2 := testEvent(t, cb, replicaB, []string{rm.ID, add.ID}, ActionTaskLabelRemoved,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"label": "P:x"})
	st = fold(t, created, rm, add, rm2)
	if len(st.Tasks[created.ID].Labels) != 0 {
		t.Errorf("labels = %v, want observed remove to win", st.Tasks[created.ID].Labels)
	}
}

func TestFoldTombstoneRestoreAndConcurrentEdit(t *testing.T) {
	ca, cb := testClock(1000), testClock(2000)
	created := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "t"})
	rm := testEvent(t, ca, replicaA, []string{created.ID}, ActionTaskRemoved,
		Subject{Kind: "task", ID: created.ID}, nil)
	edit := testEvent(t, cb, replicaB, []string{created.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"title": "concurrent edit"})
	// D4: a tombstone wins over a concurrent edit.
	st := fold(t, created, rm, edit)
	if !st.Tasks[created.ID].Tombstoned {
		t.Fatal("tombstone should beat concurrent edit")
	}
	// task.restored, authored after observing both, revives the ORIGINAL
	// identity — the concurrent edit's title is there waiting.
	restore := testEvent(t, cb, replicaB, []string{rm.ID, edit.ID}, ActionTaskRestored,
		Subject{Kind: "task", ID: created.ID}, nil)
	st = fold(t, created, rm, edit, restore)
	task := st.Tasks[created.ID]
	if task.Tombstoned || task.Title != "concurrent edit" || task.Alias != "T-1" {
		t.Errorf("restored task = %+v", task)
	}
}

func TestFoldConcurrentRemoveRestoreIsRestoreWinsAndContested(t *testing.T) {
	ca, cb := testClock(1000), testClock(2000)
	created := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "t"})
	rmA := testEvent(t, ca, replicaA, []string{created.ID}, ActionTaskRemoved,
		Subject{Kind: "task", ID: created.ID}, nil)
	rmB := testEvent(t, cb, replicaB, []string{created.ID}, ActionTaskRemoved,
		Subject{Kind: "task", ID: created.ID}, nil)
	restore := testEvent(t, ca, replicaA, []string{rmA.ID}, ActionTaskRestored,
		Subject{Kind: "task", ID: created.ID}, nil)
	st := fold(t, created, rmA, rmB, restore)
	if st.Tasks[created.ID].Tombstoned {
		t.Error("restore-wins: concurrent removed+restored must resolve live")
	}
	found := false
	for _, cs := range st.Contested {
		if cs.Kind == SlotExistence && cs.Entity == created.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("existence slot should be contested: %+v", st.Contested)
	}
}

func TestFoldComputednessWins(t *testing.T) {
	ca, cb := testClock(1000), testClock(2000)
	created := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "t"})
	seed := testEvent(t, ca, replicaA, []string{created.ID}, ActionLabelUpserted,
		Subject{Kind: "label", Name: "P:board"}, map[string]any{"description": "", "expr": ""})
	// Replica A makes the label computed; replica B concurrently assigns it.
	mkBoard := testEvent(t, ca, replicaA, []string{seed.ID}, ActionLabelUpserted,
		Subject{Kind: "label", Name: "P:board"}, map[string]any{"expr": "status:open"})
	assign := testEvent(t, cb, replicaB, []string{seed.ID}, ActionTaskLabelAdded,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"label": "P:board"})
	st := fold(t, created, seed, mkBoard, assign)
	if l := st.Labels["P:board"]; l == nil || l.Expr != "status:open" || !l.IsComputed() {
		t.Fatalf("label = %+v", st.Labels["P:board"])
	}
	if len(st.Tasks[created.ID].Labels) != 0 {
		t.Errorf("computed-ness must win: assignment is inert, got %v", st.Tasks[created.ID].Labels)
	}
	for _, cs := range st.Contested {
		if cs.Kind == SlotMembership && cs.Field == "P:board" {
			t.Errorf("inert membership slot reported contested: %+v", cs)
		}
	}
}

func TestFoldLabelRemoveThenUpsertResurrects(t *testing.T) {
	c := testClock(1000)
	up1 := testEvent(t, c, replicaA, nil, ActionLabelUpserted,
		Subject{Kind: "label", Name: "P:x"}, map[string]any{"description": "first", "expr": ""})
	rm := testEvent(t, c, replicaA, []string{up1.ID}, ActionLabelRemoved,
		Subject{Kind: "label", Name: "P:x"}, nil)
	st := fold(t, up1, rm)
	if !st.Labels["P:x"].Tombstoned {
		t.Fatal("label should be tombstoned after remove")
	}
	up2 := testEvent(t, c, replicaA, []string{rm.ID}, ActionLabelUpserted,
		Subject{Kind: "label", Name: "P:x"}, map[string]any{"description": "second", "expr": ""})
	st = fold(t, up1, rm, up2)
	l := st.Labels["P:x"]
	if l.Tombstoned || l.Description != "second" {
		t.Errorf("re-upsert should resurrect: %+v", l)
	}
}

func TestFoldCommentAttachesToTask(t *testing.T) {
	c := testClock(1000)
	task := testEvent(t, c, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "t"})
	c1 := testEvent(t, c, replicaA, []string{task.ID}, ActionCommentCreated, Subject{Kind: "comment"},
		map[string]any{"alias": "T-1-c1", "task_ref": task.ID, "body": "first"})
	c2 := testEvent(t, c, replicaA, []string{c1.ID}, ActionCommentCreated, Subject{Kind: "comment"},
		map[string]any{"alias": "T-1-c2", "task_ref": task.ID, "reply_to_ref": c1.ID, "body": "reply"})
	st := fold(t, task, c1, c2)
	if got := st.Comments[c1.ID]; got == nil || got.TaskRef != task.ID || got.Body != "first" {
		t.Fatalf("comment 1 = %+v", got)
	}
	if got := st.Comments[c2.ID]; got.ReplyToRef != c1.ID {
		t.Errorf("comment 2 reply ref = %+v", got)
	}
	ordered := st.CommentsByCreation(task.ID)
	if len(ordered) != 2 || ordered[0].ID != c1.ID || ordered[1].ID != c2.ID {
		t.Errorf("CommentsByCreation order wrong")
	}
}

func TestFoldDanglingWritesAreInert(t *testing.T) {
	c := testClock(1000)
	created := testEvent(t, c, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "t"})
	ghostEdit := testEvent(t, c, replicaA, []string{created.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: "sha256:0000000000000000000000000000000000000000000000000000000000000000"},
		map[string]any{"title": "ghost"})
	st := fold(t, created, ghostEdit)
	if len(st.Tasks) != 1 {
		t.Errorf("dangling write materialized an entity: %d tasks", len(st.Tasks))
	}
}

func TestFoldTasksByCreationUsesHLCStamp(t *testing.T) {
	ca, cb := testClock(5000), testClock(1000) // replica B created its task EARLIER
	t1 := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-a", "title": "later"})
	t2 := testEvent(t, cb, replicaB, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-b", "title": "earlier"})
	st := fold(t, t1, t2)
	got := st.TasksByCreation()
	if len(got) != 2 || got[0].ID != t2.ID || got[1].ID != t1.ID {
		t.Errorf("creation order wrong: %v", []string{got[0].Alias, got[1].Alias})
	}
}

func TestFoldUpdatedMetaTracksWinningWriter(t *testing.T) {
	// Author the edit directly (not via testEvent) so its actor and at
	// differ from the creation's — otherwise the assertion is vacuous.
	ca, cb := testClock(1000), testClock(2000)
	created := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "v0"})
	edit, err := NewEvent(cb, replicaB, []string{created.ID}, Draft{
		At:      time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC),
		Actor:   "manager@claude:test",
		Action:  ActionTaskTitleChanged,
		Subject: Subject{Kind: "task", ID: created.ID},
		Payload: map[string]any{"title": "v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	st := fold(t, created, edit)
	task := st.Tasks[created.ID]
	if task.UpdatedBy != "manager@claude:test" || !task.UpdatedAt.Equal(edit.At) {
		t.Errorf("updated meta = %s @ %v", task.UpdatedBy, task.UpdatedAt)
	}
	if task.CreatedBy != created.Actor || !task.CreatedAt.Equal(created.At) {
		t.Errorf("creation meta must not move: %s @ %v", task.CreatedBy, task.CreatedAt)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/eventsource/`
Expected: `FAIL` with `undefined: FoldEvents` (build failure).

- [ ] **Step 3: Write the implementation**

Append to `internal/eventsource/fold.go`:

```go
// State is the fold's output: a pure function of the event set (D4). Two
// replicas holding the same set compute deep-equal State. Nothing below
// consults arrival order, at, or seq.
type State struct {
	Projects  map[string]*ProjectState // by identity
	Tasks     map[string]*TaskState    // by identity
	Comments  map[string]*CommentState // by identity
	Labels    map[string]*LabelState   // by name (a label's name is its identity)
	Contested []ContestedSlot
	Frontier  []string
}

// EntityMeta is the part of an entity every kind shares. Alias is the
// stored display constant from the creation event (L1-2); Tombstoned
// entities remain present so a restore can find them.
type EntityMeta struct {
	ID             string
	Alias          string
	Tombstoned     bool
	CreatedAt      time.Time
	CreatedBy      string
	CreatedHLC     HLC
	CreatedReplica string
	UpdatedAt      time.Time
	UpdatedBy      string
}

type ProjectState struct {
	EntityMeta
	Code string
	Name string
}

type TaskState struct {
	EntityMeta
	Title       string
	Description string
	Labels      []string
}

type CommentState struct {
	EntityMeta
	TaskRef    string
	ReplyToRef string
	Body       string
	Labels     []string
}

type LabelState struct {
	Name        string
	Description string
	Expr        string
	Tombstoned  bool
	UpdatedAt   time.Time
	UpdatedBy   string
}

// isNamespaceName reports whether a label name denotes a namespace label.
// Namespace-ness is a property of the name alone, so it holds even for a
// label with no stored record. Sole definition of the ":*" rule.
func isNamespaceName(name string) bool {
	return strings.HasSuffix(name, ":*")
}

// IsComputed reports whether membership is derived rather than asserted:
// boards (Expr set) and namespace labels. For a computed label every
// membership slot is inert (L2-6). Sole definition of the L2-6 rule — every
// other site delegates here rather than restating it.
func (l *LabelState) IsComputed() bool {
	return l.Expr != "" || isNamespaceName(l.Name)
}

// FoldEvents builds the DAG and folds it.
func FoldEvents(events []*Event) (*State, error) {
	d, err := BuildDAG(events)
	if err != nil {
		return nil, err
	}
	return Fold(d), nil
}

// Fold derives State from the event set. It never blocks, never prompts,
// and never waits for a human (L2-5): contested slots are surfaced in
// State.Contested, and a losing write stays in the log — LWW selects a
// current value, it does not erase history.
func Fold(d *DAG) *State {
	bySlot := collectWrites(d)
	maximal := make(map[slotKey][]slotWrite, len(bySlot))
	for k, ws := range bySlot {
		maximal[k] = maximalWriters(d, ws)
	}

	st := &State{
		Projects: map[string]*ProjectState{},
		Tasks:    map[string]*TaskState{},
		Comments: map[string]*CommentState{},
		Labels:   map[string]*LabelState{},
		Frontier: d.Frontier(),
	}

	// Pass 1 — materialize entities from their creation events (labels:
	// from their first upsert). Dangling writes stay inert (spec decision 10).
	for _, e := range d.Events() {
		switch e.Action {
		case ActionProjectCreated:
			p := &ProjectState{EntityMeta: metaFor(e)}
			p.Code = p.Alias
			p.Name = scalarValue(maximal[slotKey{e.ID, SlotScalar, "name"}])
			p.Tombstoned = tombstoned(maximal[slotKey{e.ID, SlotExistence, ""}])
			st.Projects[e.ID] = p
		case ActionTaskCreated:
			tk := &TaskState{EntityMeta: metaFor(e)}
			tk.Title = scalarValue(maximal[slotKey{e.ID, SlotScalar, "title"}])
			tk.Description = scalarValue(maximal[slotKey{e.ID, SlotScalar, "description"}])
			tk.Tombstoned = tombstoned(maximal[slotKey{e.ID, SlotExistence, ""}])
			st.Tasks[e.ID] = tk
		case ActionCommentCreated:
			cm := &CommentState{EntityMeta: metaFor(e)}
			cm.TaskRef, _ = e.PayloadString("task_ref")
			cm.ReplyToRef, _ = e.PayloadString("reply_to_ref")
			cm.Body = scalarValue(maximal[slotKey{e.ID, SlotScalar, "body"}])
			cm.Tombstoned = tombstoned(maximal[slotKey{e.ID, SlotExistence, ""}])
			st.Comments[e.ID] = cm
		case ActionLabelUpserted:
			name := e.Subject.Name
			if name == "" || st.Labels[name] != nil {
				continue
			}
			l := &LabelState{Name: name}
			l.Description = scalarValue(maximal[slotKey{name, SlotScalar, "label.description"}])
			l.Expr = scalarValue(maximal[slotKey{name, SlotScalar, "label.expr"}])
			l.Tombstoned = tombstoned(maximal[slotKey{name, SlotExistence, ""}])
			l.UpdatedAt = e.At
			l.UpdatedBy = e.Actor
			st.Labels[name] = l
		}
	}

	// A label may be referenced by a membership slot without ever having been
	// upserted, so there may be no LabelState to ask; fall back to the name.
	computed := func(name string) bool {
		if l := st.Labels[name]; l != nil {
			return l.IsComputed()
		}
		return isNamespaceName(name)
	}

	// Pass 2 — membership and contested, iterating slots in sorted order
	// so output is deterministic. Membership slots of computed labels are
	// inert: skipped for both membership AND contested reporting.
	keys := make([]slotKey, 0, len(maximal))
	for k := range maximal {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.entity != b.entity {
			return a.entity < b.entity
		}
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		return a.field < b.field
	})
	for _, k := range keys {
		ws := maximal[k]
		if k.kind == SlotMembership {
			if computed(k.field) {
				continue
			}
			if member(ws) {
				switch {
				case st.Tasks[k.entity] != nil:
					st.Tasks[k.entity].Labels = append(st.Tasks[k.entity].Labels, k.field)
				case st.Comments[k.entity] != nil:
					st.Comments[k.entity].Labels = append(st.Comments[k.entity].Labels, k.field)
				}
			}
		}
		if len(ws) > 1 {
			cs := ContestedSlot{Entity: k.entity, Kind: k.kind, Field: k.field}
			for _, w := range ws {
				cs.Writers = append(cs.Writers, w.event.ID)
			}
			st.Contested = append(st.Contested, cs)
		}
	}

	// Pass 3 — UpdatedAt/UpdatedBy from the HLC-greatest maximal writer
	// across each entity's slots (creation included, so it is the floor).
	// Both loops iterate sorted keys — `keys` from Pass 2, then the entity ids
	// — per the global constraint that fold output never reads map order.
	lastWrite := map[string]*Event{}
	for _, k := range keys {
		for _, w := range maximal[k] {
			if cur := lastWrite[k.entity]; cur == nil || CompareEvents(cur, w.event) < 0 {
				lastWrite[k.entity] = w.event
			}
		}
	}
	entities := make([]string, 0, len(lastWrite))
	for entity := range lastWrite {
		entities = append(entities, entity)
	}
	sort.Strings(entities)
	for _, entity := range entities {
		e := lastWrite[entity]
		switch {
		case st.Projects[entity] != nil:
			st.Projects[entity].UpdatedAt, st.Projects[entity].UpdatedBy = e.At, e.Actor
		case st.Tasks[entity] != nil:
			st.Tasks[entity].UpdatedAt, st.Tasks[entity].UpdatedBy = e.At, e.Actor
		case st.Comments[entity] != nil:
			st.Comments[entity].UpdatedAt, st.Comments[entity].UpdatedBy = e.At, e.Actor
		case st.Labels[entity] != nil:
			st.Labels[entity].UpdatedAt, st.Labels[entity].UpdatedBy = e.At, e.Actor
		}
	}
	return st
}

func metaFor(e *Event) EntityMeta {
	alias, _ := e.PayloadString("alias")
	return EntityMeta{
		ID:             e.ID,
		Alias:          alias,
		CreatedAt:      e.At,
		CreatedBy:      e.Actor,
		CreatedHLC:     e.HLC,
		CreatedReplica: e.Replica,
		UpdatedAt:      e.At,
		UpdatedBy:      e.Actor,
	}
}

// scalarValue resolves a scalar slot: highest HLC among maximal writers
// wins (ws is sorted ascending, so the winner is last).
func scalarValue(ws []slotWrite) string {
	if len(ws) == 0 {
		return ""
	}
	return ws[len(ws)-1].value
}

// tombstoned resolves an existence slot: keep beats drop (L2-2) — any
// maximal "live" (task.restored, label.upserted) means live; otherwise any
// maximal tombstone means tombstoned; no writers means live.
func tombstoned(ws []slotWrite) bool {
	if len(ws) == 0 {
		return false
	}
	for _, w := range ws {
		if w.value == "live" {
			return false
		}
	}
	return true
}

// member resolves a membership slot: add-wins (L2-2). Equivalent to the
// OR-Set read "some add is not a causal ancestor of any remove" — an add
// dominated only by adds survives into the maximal set.
func member(ws []slotWrite) bool {
	for _, w := range ws {
		if w.value == "add" {
			return true
		}
	}
	return false
}

func compareCreation(a, b *EntityMeta) int {
	if c := a.CreatedHLC.Compare(b.CreatedHLC); c != 0 {
		return c
	}
	if c := strings.Compare(a.CreatedReplica, b.CreatedReplica); c != 0 {
		return c
	}
	return strings.Compare(a.ID, b.ID)
}

// TasksByCreation returns all tasks (tombstoned included — callers filter)
// in creation order: the HLC creation stamp, which unlike alias order
// stays meaningful after a merge (L1-3).
func (s *State) TasksByCreation() []*TaskState {
	out := make([]*TaskState, 0, len(s.Tasks))
	for _, t := range s.Tasks {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return compareCreation(&out[i].EntityMeta, &out[j].EntityMeta) < 0 })
	return out
}

// CommentsByCreation returns a task's comments in creation order.
func (s *State) CommentsByCreation(taskRef string) []*CommentState {
	var out []*CommentState
	for _, c := range s.Comments {
		if c.TaskRef == taskRef {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return compareCreation(&out[i].EntityMeta, &out[j].EntityMeta) < 0 })
	return out
}
```

Also extend the import block at the top of `fold.go` to:

```go
import (
	"sort"
	"strings"
	"time"
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/eventsource/`
Expected: `ok` — all Task 7 and Task 8 tests green.

- [ ] **Step 5: Commit**

```bash
git add internal/eventsource/fold.go internal/eventsource/fold_test.go
git commit -m "feat(ATM-0106): the fold - convergent state with contested slots"
```

---

### Task 9: Order-independence property test

**Files:**
- Create: `internal/eventsource/fold_property_test.go`

**Interfaces:**
- Consumes: `FoldEvents`, `NewEvent`, test helpers. Produces nothing — this task is pure verification of D4 (strong eventual consistency): folding any permutation of the same event set, with injected duplicates, yields deep-equal `State`.

- [ ] **Step 1: Write the property test**

Create `internal/eventsource/fold_property_test.go`:

```go
package eventsource

import (
	"encoding/json"
	"math/rand"
	"testing"
)

// buildScenario authors a two-replica history exercising every slot kind:
// creations, concurrent scalar edits, add/remove races, tombstone+restore,
// label upserts, a computed label, and an inert retired action.
func buildScenario(t *testing.T) []*Event {
	t.Helper()
	ca, cb := testClock(1000), testClock(2000)
	var evs []*Event
	add := func(e *Event) *Event { evs = append(evs, e); return e }

	proj := add(testEvent(t, ca, replicaA, nil, ActionProjectCreated,
		Subject{Kind: "project", Code: "P"}, map[string]any{"alias": "P", "name": "proj"}))
	task1 := add(testEvent(t, ca, replicaA, []string{proj.ID}, ActionTaskCreated,
		Subject{Kind: "task"}, map[string]any{"alias": "P-1", "title": "one", "labels": []string{"P:x"}}))
	task2 := add(testEvent(t, cb, replicaB, []string{proj.ID}, ActionTaskCreated,
		Subject{Kind: "task"}, map[string]any{"alias": "P-2", "title": "two"}))
	// Concurrent scalar edits (contested LWW).
	add(testEvent(t, ca, replicaA, []string{task1.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: task1.ID}, map[string]any{"title": "one-A"}))
	add(testEvent(t, cb, replicaB, []string{task1.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: task1.ID}, map[string]any{"title": "one-B"}))
	// Add/remove race on P:x.
	add(testEvent(t, ca, replicaA, []string{task1.ID}, ActionTaskLabelAdded,
		Subject{Kind: "task", ID: task1.ID}, map[string]any{"label": "P:y"}))
	add(testEvent(t, cb, replicaB, []string{task1.ID}, ActionTaskLabelRemoved,
		Subject{Kind: "task", ID: task1.ID}, map[string]any{"label": "P:x"}))
	// Tombstone + causally-later restore on task2.
	rm := add(testEvent(t, ca, replicaA, []string{task2.ID}, ActionTaskRemoved,
		Subject{Kind: "task", ID: task2.ID}, nil))
	add(testEvent(t, cb, replicaB, []string{rm.ID}, ActionTaskRestored,
		Subject{Kind: "task", ID: task2.ID}, nil))
	// Labels: plain + computed, plus a comment.
	up := add(testEvent(t, ca, replicaA, []string{proj.ID}, ActionLabelUpserted,
		Subject{Kind: "label", Name: "P:x"}, map[string]any{"description": "xx", "expr": ""}))
	add(testEvent(t, cb, replicaB, []string{up.ID}, ActionLabelUpserted,
		Subject{Kind: "label", Name: "P:board"}, map[string]any{"description": "", "expr": "x"}))
	cm := add(testEvent(t, cb, replicaB, []string{task1.ID}, ActionCommentCreated,
		Subject{Kind: "comment"}, map[string]any{"alias": "P-1-c1", "task_ref": task1.ID, "body": "hi"}))
	add(testEvent(t, ca, replicaA, []string{cm.ID}, ActionCommentBodyChanged,
		Subject{Kind: "comment", ID: cm.ID}, map[string]any{"body": "edited"}))
	// Retired action riding through: inert but causal.
	add(testEvent(t, ca, replicaA, []string{task1.ID}, "task.meta-changed",
		Subject{Kind: "task", ID: task1.ID}, map[string]any{"next_comment_n": 5}))
	return evs
}

func stateFingerprint(t *testing.T, st *State) string {
	t.Helper()
	// State contains only exported fields and deterministic slices;
	// JSON is a convenient deep-equal witness.
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestFoldIsOrderIndependent(t *testing.T) {
	evs := buildScenario(t)
	base, err := FoldEvents(evs)
	if err != nil {
		t.Fatal(err)
	}
	want := stateFingerprint(t, base)
	if len(base.Contested) == 0 {
		t.Fatal("scenario should produce at least one contested slot")
	}
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 50; i++ {
		shuffled := make([]*Event, len(evs))
		copy(shuffled, evs)
		rng.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		// Inject duplicates: syncing the same event twice must be a no-op.
		shuffled = append(shuffled, shuffled[rng.Intn(len(shuffled))], shuffled[rng.Intn(len(shuffled))])
		st, err := FoldEvents(shuffled)
		if err != nil {
			t.Fatal(err)
		}
		if got := stateFingerprint(t, st); got != want {
			t.Fatalf("permutation %d diverged:\n got %s\nwant %s", i, got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/eventsource/ -run TestFoldIsOrderIndependent -v`
Expected: `PASS`. If it fails, the fold has an arrival-order dependency — a D4 violation to fix in `fold.go`, not in the test. (Likely culprits: map iteration without sorted keys, or a slice appended in event order.)

- [ ] **Step 3: Commit**

```bash
git add internal/eventsource/fold_property_test.go
git commit -m "test(ATM-0106): order-independence property for the fold"
```

---

### Task 10: Alias minting and resolution

**Files:**
- Create: `internal/eventsource/alias.go`
- Create: `internal/eventsource/alias_test.go`

**Interfaces:**
- Consumes: `State`, `TaskState`, `CommentState`.
- Produces: `func MintTaskAlias(projectCode, eventID string, taken func(string) bool) string`, `func MintCommentAlias(taskAlias, eventID string, taken func(string) bool) string`, `type Match struct{ Kind, ID, Alias string }`, `type AmbiguousError struct{ Input string; Matches []Match }` (implements `error`), `var ErrNoMatch = errors.New(...)`, `func (s *State) Resolve(input string) (Match, error)`.

Rules (L1): task alias = `<CODE>-<prefix>` where prefix is the first ≥6 lowercase hex chars of the creation event's hash digest, extended until no *currently held* alias equals it (`taken`); comment alias = `<task-alias>-c<prefix>` with minimum 4. Resolution order: exact alias match → unique identity prefix → `*AmbiguousError` listing candidates / `ErrNoMatch`. Never silently pick one (L1-4). Tombstoned entities still resolve (a restore needs to name its target).

- [ ] **Step 1: Write the failing test**

Create `internal/eventsource/alias_test.go`:

```go
package eventsource

import (
	"errors"
	"testing"
)

const digest = "sha256:7f3a2bc4d5e6f708192a3b4c5d6e7f808192a3b4c5d6e7f808192a3b4c5d6e7f"

func TestMintTaskAlias(t *testing.T) {
	none := func(string) bool { return false }
	if got := MintTaskAlias("ATM", digest, none); got != "ATM-7f3a2b" {
		t.Errorf("alias = %q", got)
	}
	// A collision with a held alias extends the prefix to disambiguate.
	taken := func(a string) bool { return a == "ATM-7f3a2b" }
	if got := MintTaskAlias("ATM", digest, taken); got != "ATM-7f3a2bc" {
		t.Errorf("extended alias = %q", got)
	}
}

func TestMintCommentAlias(t *testing.T) {
	none := func(string) bool { return false }
	if got := MintCommentAlias("ATM-0042", digest, none); got != "ATM-0042-c7f3a" {
		t.Errorf("alias = %q", got)
	}
	taken := func(a string) bool { return a == "ATM-0042-c7f3a" }
	if got := MintCommentAlias("ATM-0042", digest, taken); got != "ATM-0042-c7f3a2" {
		t.Errorf("extended alias = %q", got)
	}
}

func resolveState(t *testing.T) (*State, *Event, *Event) {
	t.Helper()
	ca, cb := testClock(1000), testClock(2000)
	// Two tasks holding the SAME alias — legitimately possible after a
	// cross-project merge (L1: aliases need not be unique).
	t1 := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "ATM-0142", "title": "fix cache"})
	t2 := testEvent(t, cb, replicaB, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "ATM-0142", "title": "add export"})
	st, err := FoldEvents([]*Event{t1, t2})
	if err != nil {
		t.Fatal(err)
	}
	return st, t1, t2
}

func TestResolveExactAliasUnique(t *testing.T) {
	ca := testClock(1000)
	e := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "ATM-0001", "title": "t"})
	st, err := FoldEvents([]*Event{e})
	if err != nil {
		t.Fatal(err)
	}
	m, err := st.Resolve("ATM-0001")
	if err != nil || m.ID != e.ID || m.Kind != "task" {
		t.Errorf("Resolve = %+v, %v", m, err)
	}
}

func TestResolveAmbiguousAliasNeverPicksOne(t *testing.T) {
	st, _, _ := resolveState(t)
	_, err := st.Resolve("ATM-0142")
	var amb *AmbiguousError
	if !errors.As(err, &amb) {
		t.Fatalf("err = %v, want *AmbiguousError", err)
	}
	if len(amb.Matches) != 2 {
		t.Errorf("candidates = %+v", amb.Matches)
	}
}

func TestResolveIdentityPrefix(t *testing.T) {
	st, t1, _ := resolveState(t)
	// A unique prefix of the identity hex resolves, git-style, with or
	// without the sha256: prefix.
	hex := t1.ID[len("sha256:"):]
	for _, in := range []string{hex[:12], "sha256:" + hex[:12], t1.ID} {
		m, err := st.Resolve(in)
		if err != nil || m.ID != t1.ID {
			t.Errorf("Resolve(%q) = %+v, %v", in, m, err)
		}
	}
}

func TestResolveNoMatch(t *testing.T) {
	st, _, _ := resolveState(t)
	if _, err := st.Resolve("ATM-9999"); !errors.Is(err, ErrNoMatch) {
		t.Errorf("err = %v, want ErrNoMatch", err)
	}
}

func TestResolveFindsComments(t *testing.T) {
	ca := testClock(1000)
	task := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "ATM-0001", "title": "t"})
	cm := testEvent(t, ca, replicaA, []string{task.ID}, ActionCommentCreated, Subject{Kind: "comment"},
		map[string]any{"alias": "ATM-0001-c0001", "task_ref": task.ID, "body": "b"})
	st, err := FoldEvents([]*Event{task, cm})
	if err != nil {
		t.Fatal(err)
	}
	m, err := st.Resolve("ATM-0001-c0001")
	if err != nil || m.Kind != "comment" || m.ID != cm.ID {
		t.Errorf("Resolve comment = %+v, %v", m, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/eventsource/`
Expected: `FAIL` with `undefined: MintTaskAlias` (build failure).

- [ ] **Step 3: Write the implementation**

Create `internal/eventsource/alias.go`:

```go
package eventsource

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// MintTaskAlias mints the stored display alias for a new task (L1):
// "<CODE>-" + the first 6 lowercase hex chars of the creation event's
// digest, extended to the shortest length unambiguous among the aliases
// the minting replica currently holds (taken). The alias is stored on the
// creation event, immutable forever, and need not be globally unique —
// local extension only keeps local lookups convenient.
func MintTaskAlias(projectCode, eventID string, taken func(string) bool) string {
	return mintAlias(projectCode+"-", eventID, 6, taken)
}

// MintCommentAlias mints "<task-alias>-c" + ≥4 hex chars: a comment's
// prefix need only disambiguate within its task.
func MintCommentAlias(taskAlias, eventID string, taken func(string) bool) string {
	return mintAlias(taskAlias+"-c", eventID, 4, taken)
}

func mintAlias(prefix, eventID string, minLen int, taken func(string) bool) string {
	hex := strings.TrimPrefix(eventID, "sha256:")
	for n := minLen; n <= len(hex); n++ {
		alias := prefix + hex[:n]
		if !taken(alias) {
			return alias
		}
	}
	return prefix + hex
}

// Match is one resolution candidate.
type Match struct {
	Kind  string // "project", "task", or "comment"
	ID    string
	Alias string
}

// ErrNoMatch reports that an input resolved to nothing.
var ErrNoMatch = errors.New("eventsource: no entity matches")

// AmbiguousError reports an input matching more than one entity. Callers
// print the candidates and let the human disambiguate with an identity
// prefix — never silently pick one (L1-4).
type AmbiguousError struct {
	Input   string
	Matches []Match
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("eventsource: ambiguous %q — %d entities match", e.Input, len(e.Matches))
}

// Resolve maps a user-supplied string to an entity: exact alias match
// first, then unique identity prefix (with or without "sha256:"),
// git-style. Tombstoned entities resolve too — a restore must be able to
// name its target.
func (s *State) Resolve(input string) (Match, error) {
	all := make([]Match, 0, len(s.Projects)+len(s.Tasks)+len(s.Comments))
	for _, p := range s.Projects {
		all = append(all, Match{Kind: "project", ID: p.ID, Alias: p.Alias})
	}
	for _, t := range s.Tasks {
		all = append(all, Match{Kind: "task", ID: t.ID, Alias: t.Alias})
	}
	for _, c := range s.Comments {
		all = append(all, Match{Kind: "comment", ID: c.ID, Alias: c.Alias})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })

	var found []Match
	for _, m := range all {
		if m.Alias == input {
			found = append(found, m)
		}
	}
	if len(found) == 0 {
		hexInput := strings.TrimPrefix(input, "sha256:")
		if hexInput != "" {
			for _, m := range all {
				if strings.HasPrefix(strings.TrimPrefix(m.ID, "sha256:"), hexInput) {
					found = append(found, m)
				}
			}
		}
	}
	switch len(found) {
	case 0:
		return Match{}, fmt.Errorf("%w: %q", ErrNoMatch, input)
	case 1:
		return found[0], nil
	default:
		return Match{}, &AmbiguousError{Input: input, Matches: found}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/eventsource/`
Expected: `ok`

- [ ] **Step 5: Commit**

```bash
git add internal/eventsource/alias.go internal/eventsource/alias_test.go
git commit -m "feat(ATM-0106): alias minting and git-style resolution"
```

---

### Task 11: The D6 upgrade — pure function of the v1 log

**Files:**
- Create: `internal/eventsource/upgrade.go`
- Create: `internal/eventsource/upgrade_test.go`
- Create: `internal/eventsource/testdata/v1-log.jsonl` (fixture, written by hand in Step 1)
- Create: `internal/eventsource/testdata/v2-golden.jsonl` (generated by `-update` in Step 5)

**Interfaces:**
- Consumes: `Parse`, `Canonicalize`, `HLC`, `ReplicaV1`, action constants, `FoldEvents` (tests).
- Produces: `type UpgradeResult struct{ Events []*Event; IdentityByAlias map[string]string }`, `func UpgradeV1(logData []byte) (*UpgradeResult, error)`.

The D6 table (model spec) plus spec decisions 1–5, concretely. For each v1 line, in order:

- envelope: `v: 2`, `replica: "_v1"`, `hlc: {p: at-in-unix-ms, l: seq}`, `parents: [previous upgraded event's id]` (`[]` for the first), `at`/`actor`/`action` carried verbatim.
- v1 `seq` is dropped from the envelope (demoted to a local ordinal; never hashed).
- subject: creation events (`project.created`, `task.created`, `comment.created`) drop `subject.id`; the v1 alias moves to `payload.alias`. Every other task/comment event's `subject.id` becomes the identity of the referenced creation event (looked up by v1 alias). Label subjects (`{kind, name}`) and project subjects (`{kind, code}`) pass through; a non-creation project event gets `subject.id` = the project creation identity.
- payload: carried verbatim (byte-level via `json.RawMessage` round-trip through maps only where keys are added), with these additions:
  - `comment.created`: add `task_ref` (identity of the task, from v1 `task_id`) and `reply_to_ref` (identity of the comment, from v1 `reply_to`, when non-empty). v1 keys stay.
  - `task.label-added`/`-removed`, `comment.label-added`/`-removed`: add `label`, the delta computed by tracking each entity's `labels` list through the linear v1 history (set difference new−old for adds, old−new for removes; a multi-element diff becomes an array). The snapshot `labels` key stays verbatim.
  - `label.upserted`: materialize `description` and `expr` as `""` when absent (v1 replace semantics → v2 per-key semantics, spec decision 5).
  - creation events: `alias` added (from v1 `subject.id` / project code).
- errors: any malformed line, unknown alias reference, duplicate alias creation, or non-monotonic seq aborts the upgrade (`UpgradeV1` returns the error; spec decision 13).
- purity: the function signature admits no clock, no replica id, no randomness — same bytes in, same bytes out, on every machine (D6).

- [ ] **Step 1: Write the v1 fixture**

Create `internal/eventsource/testdata/v1-log.jsonl` — a handcrafted, representative v1 log (one line per event; shown here pretty-printed per line for readability, write as compact JSONL):

```jsonl
{"seq":1,"at":"2026-07-01T10:00:00Z","actor":"admin@cli:test","action":"project.created","subject":{"kind":"project","code":"ATM"},"payload":{"code":"ATM","name":"Agent Tasks Management","next_task_n":1,"created_at":"2026-07-01T10:00:00Z","created_by":"admin@cli:test","updated_at":"2026-07-01T10:00:00Z","updated_by":"admin@cli:test"}}
{"seq":2,"at":"2026-07-01T10:01:00Z","actor":"admin@cli:test","action":"label.upserted","subject":{"kind":"label","name":"ATM:status:open"},"payload":{"name":"ATM:status:open"}}
{"seq":3,"at":"2026-07-01T10:01:00Z","actor":"admin@cli:test","action":"task.created","subject":{"kind":"task","id":"ATM-0001"},"payload":{"id":"ATM-0001","project_code":"ATM","title":"First task","labels":["ATM:status:open"],"created_at":"2026-07-01T10:01:00Z","created_by":"admin@cli:test","updated_at":"2026-07-01T10:01:00Z","updated_by":"admin@cli:test"}}
{"seq":4,"at":"2026-07-01T10:02:00Z","actor":"developer@claude:test","action":"task.title-changed","subject":{"kind":"task","id":"ATM-0001"},"payload":{"id":"ATM-0001","project_code":"ATM","title":"First task, retitled","labels":["ATM:status:open"],"created_at":"2026-07-01T10:01:00Z","created_by":"admin@cli:test","updated_at":"2026-07-01T10:02:00Z","updated_by":"developer@claude:test"}}
{"seq":5,"at":"2026-07-01T10:03:00Z","actor":"developer@claude:test","action":"comment.created","subject":{"kind":"comment","id":"ATM-0001-c0001"},"payload":{"id":"ATM-0001-c0001","task_id":"ATM-0001","body":"a comment","labels":[],"created_at":"2026-07-01T10:03:00Z","created_by":"developer@claude:test","updated_at":"2026-07-01T10:03:00Z","updated_by":"developer@claude:test"}}
{"seq":6,"at":"2026-07-01T10:03:00Z","actor":"developer@claude:test","action":"task.meta-changed","subject":{"kind":"task","id":"ATM-0001"},"payload":{"id":"ATM-0001","project_code":"ATM","title":"First task, retitled","labels":["ATM:status:open"],"next_comment_n":1,"created_at":"2026-07-01T10:01:00Z","created_by":"admin@cli:test","updated_at":"2026-07-01T10:03:00Z","updated_by":"developer@claude:test"}}
{"seq":7,"at":"2026-07-01T10:04:00Z","actor":"developer@claude:test","action":"comment.created","subject":{"kind":"comment","id":"ATM-0001-c0002"},"payload":{"id":"ATM-0001-c0002","task_id":"ATM-0001","reply_to":"ATM-0001-c0001","body":"a reply","labels":[],"created_at":"2026-07-01T10:04:00Z","created_by":"developer@claude:test","updated_at":"2026-07-01T10:04:00Z","updated_by":"developer@claude:test"}}
{"seq":8,"at":"2026-07-01T10:05:00Z","actor":"developer@claude:test","action":"label.upserted","subject":{"kind":"label","name":"ATM:status:done"},"payload":{"name":"ATM:status:done","description":"finished work"}}
{"seq":9,"at":"2026-07-01T10:06:00Z","actor":"developer@claude:test","action":"task.label-added","subject":{"kind":"task","id":"ATM-0001"},"payload":{"id":"ATM-0001","project_code":"ATM","title":"First task, retitled","labels":["ATM:status:done","ATM:status:open"],"next_comment_n":2,"created_at":"2026-07-01T10:01:00Z","created_by":"admin@cli:test","updated_at":"2026-07-01T10:06:00Z","updated_by":"developer@claude:test"}}
{"seq":10,"at":"2026-07-01T10:07:00Z","actor":"developer@claude:test","action":"task.label-removed","subject":{"kind":"task","id":"ATM-0001"},"payload":{"id":"ATM-0001","project_code":"ATM","title":"First task, retitled","labels":["ATM:status:done"],"next_comment_n":2,"created_at":"2026-07-01T10:01:00Z","created_by":"admin@cli:test","updated_at":"2026-07-01T10:07:00Z","updated_by":"developer@claude:test"}}
{"seq":11,"at":"2026-07-01T10:08:00Z","actor":"admin@cli:test","action":"task.created","subject":{"kind":"task","id":"ATM-0002"},"payload":{"id":"ATM-0002","project_code":"ATM","title":"Second task","labels":[],"created_at":"2026-07-01T10:08:00Z","created_by":"admin@cli:test","updated_at":"2026-07-01T10:08:00Z","updated_by":"admin@cli:test"}}
{"seq":12,"at":"2026-07-01T10:09:00Z","actor":"admin@cli:test","action":"task.removed","subject":{"kind":"task","id":"ATM-0002"},"payload":{"id":"ATM-0002","project_code":"ATM","title":"Second task","labels":[],"created_at":"2026-07-01T10:08:00Z","created_by":"admin@cli:test","updated_at":"2026-07-01T10:09:00Z","updated_by":"admin@cli:test"}}
```

This fixture exercises: project creation, label upsert with and without description, task creation with initial labels, retitle, two comments (one a reply), the retired `task.meta-changed`, label add + remove (delta synthesis in both directions), a second task, and a tombstone.

- [ ] **Step 2: Write the failing test**

Create `internal/eventsource/upgrade_test.go`:

```go
package eventsource

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// serializeUpgrade renders upgraded events one canonical line per event —
// the golden file format.
func serializeUpgrade(res *UpgradeResult) []byte {
	var buf bytes.Buffer
	for _, e := range res.Events {
		buf.Write(e.Raw)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

func TestUpgradeV1Golden(t *testing.T) {
	res, err := UpgradeV1(readFixture(t, "v1-log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	got := serializeUpgrade(res)
	golden := filepath.Join("testdata", "v2-golden.jsonl")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("upgrade output differs from golden (run with -update after intentional changes)")
	}
}

func TestUpgradeV1IsPure(t *testing.T) {
	data := readFixture(t, "v1-log.jsonl")
	a, err := UpgradeV1(data)
	if err != nil {
		t.Fatal(err)
	}
	b, err := UpgradeV1(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Events) != len(b.Events) {
		t.Fatal("length differs across runs")
	}
	for i := range a.Events {
		if a.Events[i].ID != b.Events[i].ID {
			t.Fatalf("event %d id differs across runs — upgrade is not pure", i)
		}
	}
}

func TestUpgradeV1Envelope(t *testing.T) {
	res, err := UpgradeV1(readFixture(t, "v1-log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	evs := res.Events
	if len(evs) != 12 {
		t.Fatalf("upgraded %d events, want 12", len(evs))
	}
	for i, e := range evs {
		if e.Replica != ReplicaV1 {
			t.Errorf("event %d replica = %q", i, e.Replica)
		}
		if e.HLC.L != int64(i+1) {
			t.Errorf("event %d hlc.l = %d, want v1 seq %d", i, e.HLC.L, i+1)
		}
		if e.HLC.P != e.At.UnixMilli() {
			t.Errorf("event %d hlc.p = %d, want at-in-ms %d", i, e.HLC.P, e.At.UnixMilli())
		}
		if i == 0 {
			if len(e.Parents) != 0 {
				t.Errorf("first event parents = %v", e.Parents)
			}
		} else if len(e.Parents) != 1 || e.Parents[0] != evs[i-1].ID {
			t.Errorf("event %d parents = %v, want [previous]", i, e.Parents)
		}
	}
}

func TestUpgradeV1IdentityAndAlias(t *testing.T) {
	res, err := UpgradeV1(readFixture(t, "v1-log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	evs := res.Events
	// Creation events: no subject.id, alias in payload.
	taskCreated := evs[2]
	if taskCreated.Action != ActionTaskCreated || taskCreated.Subject.ID != "" {
		t.Fatalf("task.created subject = %+v", taskCreated.Subject)
	}
	if alias, _ := taskCreated.PayloadString("alias"); alias != "ATM-0001" {
		t.Errorf("task alias = %q", alias)
	}
	if res.IdentityByAlias["ATM-0001"] != taskCreated.ID {
		t.Errorf("IdentityByAlias mismatch")
	}
	// Non-creation events: subject.id is the creation identity.
	retitle := evs[3]
	if retitle.Subject.ID != taskCreated.ID {
		t.Errorf("retitle subject.id = %q, want task identity", retitle.Subject.ID)
	}
	// Comment references become identities; v1 keys survive verbatim.
	reply := evs[6]
	if ref, _ := reply.PayloadString("task_ref"); ref != taskCreated.ID {
		t.Errorf("reply task_ref = %q", ref)
	}
	c1 := evs[4]
	if ref, _ := reply.PayloadString("reply_to_ref"); ref != c1.ID {
		t.Errorf("reply reply_to_ref = %q", ref)
	}
	if v1, _ := reply.PayloadString("task_id"); v1 != "ATM-0001" {
		t.Errorf("v1 task_id not preserved: %q", v1)
	}
}

func TestUpgradeV1MembershipDeltas(t *testing.T) {
	res, err := UpgradeV1(readFixture(t, "v1-log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	added := res.Events[8] // seq 9: labels gained ATM:status:done
	if got := added.PayloadStringOrList("label"); !slices.Equal(got, []string{"ATM:status:done"}) {
		t.Errorf("added delta = %v", got)
	}
	removed := res.Events[9] // seq 10: labels lost ATM:status:open
	if got := removed.PayloadStringOrList("label"); !slices.Equal(got, []string{"ATM:status:open"}) {
		t.Errorf("removed delta = %v", got)
	}
}

func TestUpgradeV1LabelUpsertMaterializesEmptyFields(t *testing.T) {
	res, err := UpgradeV1(readFixture(t, "v1-log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	up := res.Events[1] // seq 2: bare {"name": ...} upsert
	if d, ok := up.PayloadString("description"); !ok || d != "" {
		t.Errorf("description = %q, %v — want materialized empty", d, ok)
	}
	if x, ok := up.PayloadString("expr"); !ok || x != "" {
		t.Errorf("expr = %q, %v — want materialized empty", x, ok)
	}
}

func TestUpgradeV1FoldedStateMatchesV1Semantics(t *testing.T) {
	res, err := UpgradeV1(readFixture(t, "v1-log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := FoldEvents(res.Events)
	if err != nil {
		t.Fatal(err)
	}
	t1 := st.Tasks[res.IdentityByAlias["ATM-0001"]]
	if t1 == nil || t1.Title != "First task, retitled" || t1.Tombstoned {
		t.Fatalf("task 1 = %+v", t1)
	}
	if !slices.Equal(t1.Labels, []string{"ATM:status:done"}) {
		t.Errorf("task 1 labels = %v", t1.Labels)
	}
	t2 := st.Tasks[res.IdentityByAlias["ATM-0002"]]
	if t2 == nil || !t2.Tombstoned {
		t.Fatalf("task 2 should exist and be tombstoned: %+v", t2)
	}
	if len(st.CommentsByCreation(t1.ID)) != 2 {
		t.Errorf("comments = %d, want 2", len(st.CommentsByCreation(t1.ID)))
	}
	if len(st.Contested) != 0 {
		t.Errorf("a linear v1 history can never be contested: %+v", st.Contested)
	}
}

func TestUpgradeV1Errors(t *testing.T) {
	cases := map[string]string{
		"malformed line":    "{not json}\n",
		"dangling alias":    `{"seq":1,"at":"2026-07-01T10:00:00Z","actor":"a","action":"task.title-changed","subject":{"kind":"task","id":"ATM-0009"},"payload":{"title":"x"}}` + "\n",
		// A project event whose code has no project.created must abort too —
		// tolerating it would emit an event with an empty subject.id that the
		// fold then silently drops (spec decision 13).
		"dangling project":  `{"seq":1,"at":"2026-07-01T10:00:00Z","actor":"a","action":"project.name-changed","subject":{"kind":"project","code":"ZZZ"},"payload":{"name":"x"}}` + "\n",
		"duplicate creation": `{"seq":1,"at":"2026-07-01T10:00:00Z","actor":"a","action":"task.created","subject":{"kind":"task","id":"ATM-0001"},"payload":{"id":"ATM-0001","title":"a","labels":[]}}` + "\n" + `{"seq":2,"at":"2026-07-01T10:00:01Z","actor":"a","action":"task.created","subject":{"kind":"task","id":"ATM-0001"},"payload":{"id":"ATM-0001","title":"b","labels":[]}}` + "\n",
	}
	for name, log := range cases {
		if _, err := UpgradeV1([]byte(log)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/eventsource/`
Expected: `FAIL` with `undefined: UpgradeV1` (build failure).

- [ ] **Step 4: Write the implementation**

Create `internal/eventsource/upgrade.go`:

```go
package eventsource

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// UpgradeResult is the output of the one-time v1→v2 upgrade.
// IdentityByAlias maps every v1 alias ("ATM-0001", "ATM-0001-c0001",
// project codes) to the identity of its upgraded creation event.
type UpgradeResult struct {
	Events          []*Event
	IdentityByAlias map[string]string
}

// v1Entry is the v1 LogEntry shape (internal/store/log.go), decoded
// loosely: payload stays raw so unknown keys survive verbatim.
type v1Entry struct {
	Seq     int             `json:"seq"`
	At      time.Time       `json:"at"`
	Actor   string          `json:"actor"`
	Action  string          `json:"action"`
	Subject Subject         `json:"subject"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// UpgradeV1 converts a complete v1 log.jsonl into the equivalent v2 event
// chain. It is a PURE function of the log bytes (D6): no clock, no local
// replica id, no randomness — every machine upgrading the same log
// produces byte-identical events, so copies of a store converge trivially.
// It is also strict: any malformed line, dangling reference, or duplicate
// creation aborts, because the upgrade must be lossless or not happen.
func UpgradeV1(logData []byte) (*UpgradeResult, error) {
	res := &UpgradeResult{IdentityByAlias: map[string]string{}}
	prevID := ""
	prevSeq := 0
	// labelsByAlias tracks each entity's label list through the linear v1
	// history, so membership deltas can be synthesized (spec decision 4).
	labelsByAlias := map[string][]string{}

	sc := bufio.NewScanner(bytes.NewReader(logData))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var v1 v1Entry
		if err := json.Unmarshal(line, &v1); err != nil {
			return nil, fmt.Errorf("eventsource: upgrade: line %d: %w", lineNo, err)
		}
		if v1.Seq <= prevSeq {
			return nil, fmt.Errorf("eventsource: upgrade: line %d: seq %d not increasing", lineNo, v1.Seq)
		}
		prevSeq = v1.Seq

		payload := map[string]any{}
		if len(v1.Payload) > 0 {
			dec := json.NewDecoder(bytes.NewReader(v1.Payload))
			dec.UseNumber() // preserve number formatting through the round trip
			if err := dec.Decode(&payload); err != nil {
				return nil, fmt.Errorf("eventsource: upgrade: line %d payload: %w", lineNo, err)
			}
		}

		subject := v1.Subject
		v1Alias := subject.ID // task/comment alias; empty for project/label

		switch v1.Action {
		case ActionProjectCreated:
			payload["alias"] = subject.Code
		case ActionTaskCreated, ActionCommentCreated:
			if v1Alias == "" {
				return nil, fmt.Errorf("eventsource: upgrade: line %d: creation without subject.id", lineNo)
			}
			if _, dup := res.IdentityByAlias[v1Alias]; dup {
				return nil, fmt.Errorf("eventsource: upgrade: line %d: duplicate creation of %q", lineNo, v1Alias)
			}
			payload["alias"] = v1Alias
			subject.ID = ""
			if v1.Action == ActionCommentCreated {
				taskID, _ := payload["task_id"].(string)
				ref, ok := res.IdentityByAlias[taskID]
				if !ok {
					return nil, fmt.Errorf("eventsource: upgrade: line %d: comment references unknown task %q", lineNo, taskID)
				}
				payload["task_ref"] = ref
				if rt, _ := payload["reply_to"].(string); rt != "" {
					rref, ok := res.IdentityByAlias[rt]
					if !ok {
						return nil, fmt.Errorf("eventsource: upgrade: line %d: reply to unknown comment %q", lineNo, rt)
					}
					payload["reply_to_ref"] = rref
				}
			}
		case ActionLabelUpserted:
			// v1 upsert semantics replace the whole record: an absent key
			// means "". Materialize both scalar keys so the v2 per-key
			// slot rule reproduces v1 exactly (spec decision 5).
			if _, ok := payload["description"]; !ok {
				payload["description"] = ""
			}
			if _, ok := payload["expr"]; !ok {
				payload["expr"] = ""
			}
		default:
			// Non-creation task/comment event: subject.id becomes the
			// entity's identity. Project events resolve via the code.
			switch subject.Kind {
			case "task", "comment":
				id, ok := res.IdentityByAlias[v1Alias]
				if !ok {
					return nil, fmt.Errorf("eventsource: upgrade: line %d: %s event for unknown %q", lineNo, v1.Action, v1Alias)
				}
				subject.ID = id
			case "project":
				// Same rule as task/comment: a dangling reference aborts the
				// upgrade (spec decision 13 — lossless or it does not happen).
				// Tolerating the miss would emit an event with an empty
				// subject.id, which collectWrites silently drops.
				id, ok := res.IdentityByAlias[subject.Code]
				if !ok {
					return nil, fmt.Errorf("eventsource: upgrade: line %d: %s event for unknown project %q", lineNo, v1.Action, subject.Code)
				}
				subject.ID = id
			}
		}

		// Membership delta synthesis for label add/remove.
		switch v1.Action {
		case ActionTaskLabelAdded, ActionCommentLabelAdded, ActionTaskLabelRemoved, ActionCommentLabelRemoved:
			newLabels := stringList(payload["labels"])
			oldLabels := labelsByAlias[v1Alias]
			var delta []string
			if v1.Action == ActionTaskLabelAdded || v1.Action == ActionCommentLabelAdded {
				delta = diff(newLabels, oldLabels)
			} else {
				delta = diff(oldLabels, newLabels)
			}
			if len(delta) == 1 {
				payload["label"] = delta[0]
			} else if len(delta) > 1 {
				payload["label"] = delta
			}
			labelsByAlias[v1Alias] = newLabels
		case ActionTaskCreated, ActionCommentCreated:
			labelsByAlias[v1Alias] = stringList(payload["labels"])
		}

		parents := []string{}
		if prevID != "" {
			parents = []string{prevID}
		}
		obj := map[string]any{
			"v":       2,
			"parents": parents,
			"hlc":     HLC{P: v1.At.UnixMilli(), L: int64(v1.Seq)},
			"replica": ReplicaV1,
			"at":      v1.At.UTC().Format(time.RFC3339Nano),
			"actor":   v1.Actor,
			"action":  v1.Action,
			"subject": subject,
		}
		if len(payload) > 0 {
			obj["payload"] = payload
		}
		raw, err := json.Marshal(obj)
		if err != nil {
			return nil, fmt.Errorf("eventsource: upgrade: line %d: %w", lineNo, err)
		}
		e, err := Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("eventsource: upgrade: line %d: %w", lineNo, err)
		}
		res.Events = append(res.Events, e)
		prevID = e.ID
		switch v1.Action {
		case ActionProjectCreated:
			res.IdentityByAlias[subject.Code] = e.ID
		case ActionTaskCreated, ActionCommentCreated:
			res.IdentityByAlias[v1Alias] = e.ID
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("eventsource: upgrade: %w", err)
	}
	return res, nil
}

func stringList(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, x := range list {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// diff returns the elements of a not present in b, sorted.
func diff(a, b []string) []string {
	in := map[string]bool{}
	for _, x := range b {
		in[x] = true
	}
	var out []string
	for _, x := range a {
		if !in[x] {
			out = append(out, x)
		}
	}
	slices.Sort(out)
	return out
}
```

- [ ] **Step 5: Generate the golden file, then verify everything passes**

Run: `go test ./internal/eventsource/ -run TestUpgradeV1Golden -update && go test ./internal/eventsource/`
Expected: golden file written on the first command; `ok` on the second.

Inspect `testdata/v2-golden.jsonl` by eye once: 12 lines, each canonical JSON with `"v":2`, `"replica":"_v1"`, chained parents. Committing it pins every hash — any later change to canonical bytes fails this test loudly.

- [ ] **Step 6: Commit**

```bash
git add internal/eventsource/upgrade.go internal/eventsource/upgrade_test.go internal/eventsource/testdata/
git commit -m "feat(ATM-0106): D6 v1-to-v2 upgrade as a pure function of the log"
```

---

### Task 12: Equivalence capstone — fold(upgrade(v1)) == store.Replay

**Files:**
- Create: `internal/eventsource/equivalence_test.go` (package `eventsource_test` — the ONLY file allowed to import both packages)

**Interfaces:**
- Consumes: `internal/store` (real Store), `eventsource.UpgradeV1`, `eventsource.FoldEvents`.
- Produces: nothing — end-to-end proof that the v2 semantic core reproduces v1 state on a real log.

- [ ] **Step 1: Write the capstone test**

Create `internal/eventsource/equivalence_test.go`:

```go
// Package eventsource_test holds the equivalence capstone: it drives the
// REAL v1 store through a representative session, upgrades the resulting
// log.jsonl with UpgradeV1, folds it, and asserts the folded state matches
// store.Replay. This is the proof that the v2 core is a faithful superset
// of v1 semantics — and the only test allowed to import both packages.
package eventsource_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"atm/internal/eventsource"
	"atm/internal/store"
)

const actor = "admin@cli:test"

func TestFoldOfUpgradeMatchesReplay(t *testing.T) {
	root := t.TempDir()
	s, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(""); err != nil {
		t.Fatal(err)
	}

	// A representative session: every mutation kind the store exposes.
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", actor); err != nil {
		t.Fatal(err)
	}
	t1, err := s.CreateTask("ATM", "First task", "with description", []string{"ATM:status:open"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	t2, err := s.CreateTask("ATM", "Second task", "", nil, actor)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTitle(t1.ID, "First task, retitled", actor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDescription(t2.ID, "added later", actor); err != nil {
		t.Fatal(err)
	}
	c1, err := s.CreateComment(t1.ID, "a comment", []string{"ATM:comment:progress"}, "", actor)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := s.CreateComment(t1.ID, "a reply", nil, c1.ID, actor)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetCommentBody(c2.ID, "a reply, edited", actor); err != nil {
		t.Fatal(err)
	}
	if err := s.TaskLabelAdd(t1.ID, "ATM:status:done", actor); err != nil {
		t.Fatal(err)
	}
	if err := s.TaskLabelRemove(t1.ID, "ATM:status:open", actor); err != nil {
		t.Fatal(err)
	}
	if err := s.LabelAdd("ATM:refactor", "cleanup work", "", actor); err != nil {
		t.Fatal(err)
	}
	t3, err := s.CreateTask("ATM", "Doomed task", "", nil, actor)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveTask(t3.ID, actor); err != nil {
		t.Fatal(err)
	}
	c3, err := s.CreateComment(t2.ID, "doomed comment", nil, "", actor)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveComment(c3.ID, actor); err != nil {
		t.Fatal(err)
	}

	// v1 truth.
	replay, err := s.Replay("ATM")
	if err != nil {
		t.Fatal(err)
	}

	// v2: upgrade the raw log and fold.
	logData, err := os.ReadFile(filepath.Join(root, "projects", "ATM", "log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := eventsource.UpgradeV1(logData)
	if err != nil {
		t.Fatal(err)
	}
	st, err := eventsource.FoldEvents(res.Events)
	if err != nil {
		t.Fatal(err)
	}

	// Project.
	proj := st.Projects[res.IdentityByAlias["ATM"]]
	if proj == nil || proj.Name != replay.Project.Name || proj.Tombstoned {
		t.Errorf("project = %+v, want name %q", proj, replay.Project.Name)
	}

	// Tasks: v1 Replay drops removed tasks; v2 keeps them tombstoned.
	liveTasks := map[string]*eventsource.TaskState{}
	for _, tk := range st.Tasks {
		if !tk.Tombstoned {
			liveTasks[tk.Alias] = tk
		}
	}
	if len(liveTasks) != len(replay.Tasks) {
		t.Fatalf("live tasks = %d, replay = %d", len(liveTasks), len(replay.Tasks))
	}
	for _, want := range replay.Tasks {
		got := liveTasks[want.ID]
		if got == nil {
			t.Fatalf("task %s missing from fold", want.ID)
		}
		if got.Title != want.Title || got.Description != want.Description {
			t.Errorf("task %s: got (%q, %q), want (%q, %q)", want.ID, got.Title, got.Description, want.Title, want.Description)
		}
		gotLabels := slices.Clone(got.Labels)
		slices.Sort(gotLabels)
		wantLabels := slices.Clone(want.Labels)
		slices.Sort(wantLabels)
		if !slices.Equal(gotLabels, wantLabels) {
			t.Errorf("task %s labels: got %v, want %v", want.ID, gotLabels, wantLabels)
		}
		if got.CreatedBy != want.CreatedBy {
			t.Errorf("task %s created_by: got %q, want %q", want.ID, got.CreatedBy, want.CreatedBy)
		}
	}
	// The removed task stays in v2 state, tombstoned — findable for restore.
	if tk := st.Tasks[res.IdentityByAlias[t3.ID]]; tk == nil || !tk.Tombstoned {
		t.Errorf("removed task should remain tombstoned in state: %+v", tk)
	}

	// Comments.
	liveComments := map[string]*eventsource.CommentState{}
	for _, cm := range st.Comments {
		if !cm.Tombstoned {
			liveComments[cm.Alias] = cm
		}
	}
	if len(liveComments) != len(replay.Comments) {
		t.Fatalf("live comments = %d, replay = %d", len(liveComments), len(replay.Comments))
	}
	for _, want := range replay.Comments {
		got := liveComments[want.ID]
		if got == nil {
			t.Fatalf("comment %s missing from fold", want.ID)
		}
		if got.Body != want.Body {
			t.Errorf("comment %s body: got %q, want %q", want.ID, got.Body, want.Body)
		}
		if got.TaskRef != res.IdentityByAlias[want.TaskID] {
			t.Errorf("comment %s task ref mismatch", want.ID)
		}
		if want.ReplyTo != "" && got.ReplyToRef != res.IdentityByAlias[want.ReplyTo] {
			t.Errorf("comment %s reply ref mismatch", want.ID)
		}
		gotLabels := slices.Clone(got.Labels)
		slices.Sort(gotLabels)
		wantLabels := slices.Clone(want.Labels)
		slices.Sort(wantLabels)
		if !slices.Equal(gotLabels, wantLabels) {
			t.Errorf("comment %s labels: got %v, want %v", want.ID, gotLabels, wantLabels)
		}
	}

	// Labels: every replayed label is live in the fold with same fields.
	for _, want := range replay.Labels {
		got := st.Labels[want.Name]
		if got == nil || got.Tombstoned {
			t.Errorf("label %s missing or tombstoned", want.Name)
			continue
		}
		if got.Description != want.Description || got.Expr != want.Expr {
			t.Errorf("label %s: got (%q, %q), want (%q, %q)", want.Name, got.Description, got.Expr, want.Description, want.Expr)
		}
	}

	// A linear v1 history can never be contested.
	if len(st.Contested) != 0 {
		t.Errorf("contested on linear history: %+v", st.Contested)
	}

	// Aliases resolve.
	for _, alias := range []string{t1.ID, t2.ID, c1.ID, c2.ID} {
		m, err := st.Resolve(alias)
		if err != nil || m.ID != res.IdentityByAlias[alias] {
			t.Errorf("Resolve(%q) = %+v, %v", alias, m, err)
		}
	}
}
```

- [ ] **Step 2: Run the capstone**

Run: `go test ./internal/eventsource/ -run TestFoldOfUpgradeMatchesReplay -v`
Expected: `PASS`. Failures here are REAL model/implementation divergences — investigate with superpowers:systematic-debugging; do not weaken assertions to pass. Two known-benign areas if they surface: (a) v1 `CreateProject` may not append `project.created` with the exact payload keys assumed — read the actual log line and adjust only the *reference* (not the assertion strength); (b) label seeding on CreateProject (16 defaults) means `replay.Labels` includes seeded labels — the loop already iterates replay's list, so this is covered.

- [ ] **Step 3: Run the full suite**

Run: `go test ./...`
Expected: all packages `ok` — the new package must not have broken anything (it is unimported by production code, so failures elsewhere indicate an accidental edit).

- [ ] **Step 4: Commit**

```bash
git add internal/eventsource/equivalence_test.go
git commit -m "test(ATM-0106): equivalence capstone - fold(upgrade(v1)) == store.Replay"
```

---

### Task 13: Design-doc amendment + ledger close-out

**Files:**
- Modify: `docs/eventsource/01-core-data-model.md` (append one section before "What this spec does not decide")

**Interfaces:** none — documentation.

- [ ] **Step 1: Append the amendment section**

Insert into `docs/eventsource/01-core-data-model.md`, immediately before the `# What this spec does not decide` heading:

```markdown
# Implementation refinements (ATM-0106 implementation phase)

The reference implementation is `internal/eventsource` (see `docs/superpowers/specs/2026-07-14-eventsource-core-v2-design.md` for the full list of implementation decisions). Two D6 refinements and two L2 clarifications discovered during implementation are recorded here so the spec stays honest:

- **Creation events carry no `subject.id`** — an event cannot contain its own hash. The v1 alias moves from `subject.id` to `payload.alias` during the upgrade; `subject.id` on every non-creation event holds the entity identity as specified.
- **The D6 "carried across unchanged" row is refined**: the upgrade also (a) synthesizes identity references `task_ref`/`reply_to_ref` on `comment.created` from the v1 alias references, which stay in the payload verbatim; (b) synthesizes per-slot membership deltas (`payload.label`) on `*.label-added/-removed` from consecutive v1 snapshots — a pure function of the log, so D6's purity rule is untouched; (c) materializes absent `description`/`expr` keys on `label.upserted` as `""`, converting v1's replace-the-record semantics into v2's write-present-keys slot semantics without changing any outcome.
- **`label.upserted` writes the label's existence slot as "live"** — remove-then-reupsert resurrects a label, matching v1 semantics; concurrent upsert‖remove resolves live (keep beats drop).
- **The HLC total order carries a defensive fourth key** (the event id): two different projects upgraded under `_v1` can collide on `(p, l, replica)` after a cross-project merge; the id keeps the fold deterministic there.
```

- [ ] **Step 2: Verify the suite still passes and commit**

Run: `go test ./... && gofmt -l internal/eventsource`
Expected: all `ok`, no gofmt output.

```bash
git add docs/eventsource/01-core-data-model.md
git commit -m "docs(ATM-0106): record implementation refinements in the core data model spec"
```

- [ ] **Step 3: Update the ATM ledger**

Ask the atm-manager (or comment directly) to record on ATM-0106: implementation of `internal/eventsource` complete — L0 (canonicalization, identity, HLC, replica ids), L1 (alias minting/resolution), L2 (DAG, fold, contested slots), D6 (pure upgrade + golden + equivalence capstone). Store integration deliberately deferred to L3 (ATM-0107).

---

## Verification (whole plan)

After all tasks:

1. `go test ./...` — everything green.
2. `go vet ./internal/eventsource/` — clean.
3. `gofmt -l internal/eventsource` — no output.
4. `git log --oneline` — one commit per task, all prefixed `(ATM-0106)`.
5. Confirm `internal/eventsource` production files do not import `internal/store`: `grep -rn "atm/internal/store" internal/eventsource/*.go | grep -v _test` must print nothing.
