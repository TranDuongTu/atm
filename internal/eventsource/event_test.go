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
