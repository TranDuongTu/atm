package workflowrpi

import (
	"strings"
	"testing"

	"atm/internal/store"
)

// newRecorder builds the mutating side over a test store. It lives here
// because the link tests are the first to need it; the recorder's own tests
// reuse it.
func newRecorder(s *store.Store) *Recorder {
	return &Recorder{Store: s, Actor: testActor}
}

func TestProductOfLinkStoresPayload(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	product, _ := s.CreateTask("ATM", "product", "", []string{"ATM:rpi:product"}, testActor)
	pipeline, _ := s.CreateTask("ATM", "pipeline", "", nil, testActor)
	if err := r.SetProductOf(pipeline.ID, product.ID); err != nil {
		t.Fatalf("SetProductOf: %v", err)
	}
	got, _ := s.GetTask(pipeline.ID)
	pl, _ := DecodePayload(got.Meta[CapabilityName])
	if pl.ProductOf() != product.ID {
		t.Errorf("product_of = %q", pl.ProductOf())
	}
	if err := r.SetProductOf(pipeline.ID, product.ID); err != nil {
		t.Fatalf("same product_of must be idempotent: %v", err)
	}
}

func TestProductOfGuards(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	a, _ := s.CreateTask("ATM", "a", "", []string{"ATM:rpi:product"}, testActor)
	b, _ := s.CreateTask("ATM", "b", "", nil, testActor)
	c, _ := s.CreateTask("ATM", "c", "", []string{"ATM:rpi:product"}, testActor)
	if err := r.SetProductOf(a.ID, a.ID); err == nil || !strings.Contains(err.Error(), "itself") {
		t.Errorf("self product_of: %v", err)
	}
	if err := r.SetProductOf(b.ID, "ATM-ffffff"); err == nil {
		t.Error("missing product target must fail")
	}
	if err := r.SetProductOf(b.ID, a.ID); err != nil {
		t.Fatalf("set product: %v", err)
	}
	if err := r.SetProductOf(b.ID, c.ID); err == nil || !strings.Contains(err.Error(), "already linked to product") {
		t.Errorf("second product_of: %v", err)
	}
}

func TestDependsOnLinkUnlink(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	a, _ := s.CreateTask("ATM", "a", "", nil, testActor)
	b, _ := s.CreateTask("ATM", "b", "", nil, testActor)
	if err := r.LinkDependsOn(a.ID, a.ID); err == nil || !strings.Contains(err.Error(), "itself") {
		t.Errorf("self depends_on: %v", err)
	}
	if err := r.LinkDependsOn(a.ID, "ATM-ffffff"); err == nil {
		t.Error("missing target must fail")
	}
	if err := r.LinkDependsOn(a.ID, b.ID); err != nil {
		t.Fatalf("LinkDependsOn: %v", err)
	}
	if err := r.LinkDependsOn(a.ID, b.ID); err != nil {
		t.Fatalf("duplicate depends_on must be a silent no-op: %v", err)
	}
	got, _ := s.GetTask(a.ID)
	pl, _ := DecodePayload(got.Meta[CapabilityName])
	if ds := pl.DependsOn(); len(ds) != 1 || ds[0] != b.ID {
		t.Errorf("depends_on = %v", ds)
	}
	if err := r.UnlinkDependsOn(a.ID, b.ID); err != nil {
		t.Fatalf("UnlinkDependsOn: %v", err)
	}
}

func TestRelatesToAndCoveredBy(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	a, _ := s.CreateTask("ATM", "a", "", nil, testActor)
	b, _ := s.CreateTask("ATM", "b", "", nil, testActor)
	if err := r.LinkRelatesTo(a.ID, b.ID); err != nil {
		t.Fatalf("LinkRelatesTo: %v", err)
	}
	if err := r.SetCoveredBy(a.ID, b.ID); err != nil {
		t.Fatalf("SetCoveredBy: %v", err)
	}
	got, _ := s.GetTask(a.ID)
	pl, _ := DecodePayload(got.Meta[CapabilityName])
	if rt := pl.RelatesTo(); len(rt) != 1 || rt[0] != b.ID {
		t.Errorf("relates_to = %v", rt)
	}
	if pl.CoveredBy() != b.ID {
		t.Errorf("covered_by = %q", pl.CoveredBy())
	}
}
