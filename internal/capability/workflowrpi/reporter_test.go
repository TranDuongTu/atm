package workflowrpi

import (
	"strings"
	"testing"
)

func TestLinksOutboundAndInbound(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	product, _ := s.CreateTask("ATM", "product", "", []string{"ATM:rpi:product"}, testActor)
	work1, _ := s.CreateTask("ATM", "work1", "", nil, testActor)
	work2, _ := s.CreateTask("ATM", "work2", "", nil, testActor)
	dep, _ := s.CreateTask("ATM", "dep", "", nil, testActor)
	_ = r.SetProductOf(work1.ID, product.ID)
	_ = r.SetProductOf(work2.ID, product.ID)
	_ = r.LinkDependsOn(work1.ID, dep.ID)
	_ = r.LinkRelatesTo(work2.ID, dep.ID)
	_ = r.SetCoveredBy(work2.ID, dep.ID)

	got, err := (&Reporter{Store: s}).Links(product.ID)
	if err != nil {
		t.Fatalf("Links(product): %v", err)
	}
	if len(got.PipelineChildren) != 2 || !containsString(got.PipelineChildren, work1.ID) || !containsString(got.PipelineChildren, work2.ID) {
		t.Errorf("PipelineChildren = %v", got.PipelineChildren)
	}
	depLinks, err := (&Reporter{Store: s}).Links(dep.ID)
	if err != nil {
		t.Fatalf("Links(dep): %v", err)
	}
	if len(depLinks.Dependents) != 1 || depLinks.Dependents[0] != work1.ID {
		t.Errorf("Dependents = %v", depLinks.Dependents)
	}
	if len(depLinks.RelatedFrom) != 1 || depLinks.RelatedFrom[0] != work2.ID {
		t.Errorf("RelatedFrom = %v", depLinks.RelatedFrom)
	}
	if len(depLinks.Covered) != 1 || depLinks.Covered[0] != work2.ID {
		t.Errorf("Covered = %v", depLinks.Covered)
	}
}

func TestReportFindsAtRiskTasks(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	product, _ := s.CreateTask("ATM", "product", "", []string{"ATM:rpi:product", "ATM:rpi-product:clarified"}, testActor)
	work, _ := s.CreateTask("ATM", "work", "", nil, testActor)
	_, _ = r.Pipeline(work.ID, product.ID, DevPlanned)
	orphan, _ := s.CreateTask("ATM", "orphan", "", []string{"ATM:rpi:pipeline", "ATM:rpi-dev:planned"}, testActor)
	bad, _ := s.CreateTask("ATM", "bad", "", []string{"ATM:rpi:pipeline"}, testActor)
	_ = s.SetTaskCapabilityMeta(bad.ID, CapabilityName, "not json", testActor)
	backlog, _ := s.CreateTask("ATM", "backlog", "", nil, testActor)

	rep, err := (&Reporter{Store: s}).Report("ATM")
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if rep.Backlog != 1 {
		t.Errorf("Backlog = %d, want 1 (%s)", rep.Backlog, backlog.ID)
	}
	byTask := map[string]string{}
	for _, f := range rep.Findings {
		byTask[f.TaskID] = f.Detail
	}
	if !strings.Contains(byTask[orphan.ID], "missing product_of") {
		t.Errorf("orphan finding = %q", byTask[orphan.ID])
	}
	if !strings.Contains(byTask[bad.ID], "payload unparseable") {
		t.Errorf("bad payload finding = %q", byTask[bad.ID])
	}
}
