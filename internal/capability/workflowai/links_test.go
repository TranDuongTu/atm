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
