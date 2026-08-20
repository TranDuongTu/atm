package workflowrpi

import (
	"strings"
	"testing"

	"atm/internal/store"
)

func countPrefix(t *testing.T, s *store.Store, id, prefix string) int {
	t.Helper()
	tk, err := s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	n := 0
	for _, l := range tk.Labels {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

func TestProductTransitionSetsLaneAndStatus(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "idea", "", nil, testActor)
	prior, err := r.Product(tk.ID, ProductClarified)
	if err != nil {
		t.Fatalf("Product: %v", err)
	}
	if prior != "" {
		t.Errorf("prior = %q, want empty backlog lane", prior)
	}
	got, _ := s.GetTask(tk.ID)
	for _, want := range []string{"ATM:rpi:product", "ATM:rpi-product:clarified"} {
		if !containsString(got.Labels, want) {
			t.Errorf("missing %s in %v", want, got.Labels)
		}
	}
	if n := countPrefix(t, s, tk.ID, "ATM:rpi:"); n != 1 {
		t.Errorf("rpi lane labels = %d, want 1", n)
	}
}

func TestPipelineRequiresProductParent(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	plain, _ := s.CreateTask("ATM", "plain", "", nil, testActor)
	work, _ := s.CreateTask("ATM", "work", "", nil, testActor)
	if _, err := r.Pipeline(work.ID, plain.ID, DevPlanned); err == nil || !strings.Contains(err.Error(), "product parent") {
		t.Fatalf("Pipeline with non-product parent err = %v", err)
	}
	product, _ := s.CreateTask("ATM", "product", "", []string{"ATM:rpi:product"}, testActor)
	if _, err := r.Pipeline(work.ID, product.ID, DevPlanned); err != nil {
		t.Fatalf("Pipeline: %v", err)
	}
	got, _ := s.GetTask(work.ID)
	pl, _ := DecodePayload(got.Meta[CapabilityName])
	if pl.ProductOf() != product.ID {
		t.Errorf("product_of = %q", pl.ProductOf())
	}
	if !containsString(got.Labels, "ATM:rpi:pipeline") || !containsString(got.Labels, "ATM:rpi-dev:planned") {
		t.Errorf("pipeline labels = %v", got.Labels)
	}
}

func TestRejectSetsReasonAndCoveredBy(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	cover, _ := s.CreateTask("ATM", "cover", "", []string{"ATM:rpi:product"}, testActor)
	tk, _ := s.CreateTask("ATM", "dup", "", nil, testActor)
	if _, err := r.Reject(tk.ID, RejectCoveredBy, cover.ID); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	got, _ := s.GetTask(tk.ID)
	pl, _ := DecodePayload(got.Meta[CapabilityName])
	if !containsString(got.Labels, "ATM:rpi:reject") || !containsString(got.Labels, "ATM:rpi-reject:covered-by") {
		t.Errorf("reject labels = %v", got.Labels)
	}
	if pl.CoveredBy() != cover.ID {
		t.Errorf("covered_by = %q", pl.CoveredBy())
	}
}

func TestReleaseClearsRPIOnlyAndComments(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	product, _ := s.CreateTask("ATM", "product", "", []string{"ATM:rpi:product"}, testActor)
	work, _ := s.CreateTask("ATM", "work", "", []string{"ATM:status:open"}, testActor)
	_, _ = r.Pipeline(work.ID, product.ID, DevPlanned)
	prior, err := r.Release(work.ID, "return to intake")
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if prior != LanePipeline {
		t.Errorf("prior = %q, want pipeline", prior)
	}
	got, _ := s.GetTask(work.ID)
	for _, l := range got.Labels {
		if strings.Contains(l, ":rpi") {
			t.Errorf("RPI label survived release: %s", l)
		}
	}
	if !containsString(got.Labels, "ATM:status:open") {
		t.Errorf("non-RPI label removed: %v", got.Labels)
	}
	if got.Meta[CapabilityName] != "" {
		t.Errorf("RPI payload survived release: %q", got.Meta[CapabilityName])
	}
	comments, _ := s.ListComments(work.ID)
	if len(comments) == 0 || !strings.Contains(comments[len(comments)-1].Body, "return to intake") {
		t.Errorf("release reason comment missing: %v", comments)
	}
}

func TestRecorderFailsOnMalformedPayload(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "bad", "", nil, testActor)
	if err := s.SetTaskCapabilityMeta(tk.ID, CapabilityName, "not json", testActor); err != nil {
		t.Fatalf("seed malformed payload: %v", err)
	}
	if _, err := r.Product(tk.ID, ProductClarified); err == nil || !strings.Contains(err.Error(), "hand-repair") {
		t.Errorf("err = %v, want hand-repair", err)
	}
}
