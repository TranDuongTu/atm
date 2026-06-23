package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func seedGuideStore(t *testing.T) *Store {
	t.Helper()
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "Agent Tasks", "type", []Label{
		{Name: "type:impl"}, {Name: "kind:convention"},
	}, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "Convention doc", "", []string{"kind:convention"}, "human:alice")
	_, _ = s.CreateTask("ATM", "Impl task", "", []string{"type:impl"}, "human:alice")
	return s
}

func TestGuideSectionAddDuplicate(t *testing.T) {
	s := seedGuideStore(t)
	if err := s.GuideSectionAdd("ATM", "conventions", "human:alice"); err != nil {
		t.Fatal(err)
	}
	err := s.GuideSectionAdd("ATM", "conventions", "human:alice")
	if !IsConflict(err) {
		t.Fatalf("expected conflict, got %v", err)
	}
	g, _ := s.GuideGet("ATM")
	if len(g.Sections) != 1 {
		t.Fatalf("sections = %d want 1", len(g.Sections))
	}
	if g.UpdatedBy != "human:alice" {
		t.Fatalf("updated_by = %q", g.UpdatedBy)
	}
	p, _ := s.GetProject("ATM")
	if len(p.History) != 1 {
		t.Fatalf("project history = %d want 1", len(p.History))
	}
	if p.History[0].Action != "guide-updated" {
		t.Fatalf("history action = %q", p.History[0].Action)
	}
	if p.NextHistoryN != 1 {
		t.Fatalf("next_history_n = %d want 1", p.NextHistoryN)
	}
}

func TestGuideSectionRenameRemoveMove(t *testing.T) {
	s := seedGuideStore(t)
	_ = s.GuideSectionAdd("ATM", "conventions", "human:alice")
	_ = s.GuideSectionAdd("ATM", "testing", "human:alice")
	if err := s.GuideSectionRename("ATM", "conventions", "convs", "human:alice"); err != nil {
		t.Fatal(err)
	}
	g, _ := s.GuideGet("ATM")
	if g.Sections[0].Name != "convs" {
		t.Fatalf("rename failed: %s", g.Sections[0].Name)
	}
	if err := s.GuideSectionRemove("ATM", "testing", "human:alice"); err != nil {
		t.Fatal(err)
	}
	g, _ = s.GuideGet("ATM")
	if len(g.Sections) != 1 || g.Sections[0].Name != "convs" {
		t.Fatalf("after remove: %v", g.Sections)
	}
	_ = s.GuideSectionAdd("ATM", "zeta", "human:alice")
	_ = s.GuideSectionAdd("ATM", "alpha", "human:alice")
	if err := s.GuideSectionMove("ATM", "zeta", "alpha", "human:alice"); err != nil {
		t.Fatal(err)
	}
	g, _ = s.GuideGet("ATM")
	zetaIdx, alphaIdx := -1, -1
	for i, sec := range g.Sections {
		if sec.Name == "zeta" {
			zetaIdx = i
		}
		if sec.Name == "alpha" {
			alphaIdx = i
		}
	}
	if zetaIdx < 0 || alphaIdx < 0 || zetaIdx+1 != alphaIdx {
		t.Fatalf("zeta should be immediately before alpha: zeta=%d alpha=%d", zetaIdx, alphaIdx)
	}
	if err := s.GuideSectionMove("ATM", "alpha", "", "human:alice"); err != nil {
		t.Fatal(err)
	}
	g, _ = s.GuideGet("ATM")
	if g.Sections[len(g.Sections)-1].Name != "alpha" {
		t.Fatalf("move-to-end failed: %s", g.Sections[len(g.Sections)-1].Name)
	}
}

func TestGuideSectionRenameNotFound(t *testing.T) {
	s := seedGuideStore(t)
	err := s.GuideSectionRename("ATM", "nope", "x", "human:alice")
	if !IsNotFound(err) {
		t.Fatalf("expected not-found, got %v", err)
	}
}

func TestGuideRefAddTaskValidates(t *testing.T) {
	s := seedGuideStore(t)
	_ = s.GuideSectionAdd("ATM", "conventions", "human:alice")
	if err := s.GuideRefAdd("ATM", "conventions", "task", "ATM-0001", "human:alice"); err != nil {
		t.Fatalf("valid task ref: %v", err)
	}
	if err := s.GuideRefAdd("ATM", "conventions", "task", "ATM-9999", "human:alice"); !IsNotFound(err) {
		t.Fatalf("missing task should be not-found, got %v", err)
	}
	if err := s.GuideRefAdd("ATM", "conventions", "task", "BAD-ID", "human:alice"); !IsUsage(err) {
		t.Fatalf("bad task id should be usage, got %v", err)
	}
	if err := s.GuideRefAdd("ATM", "conventions", "task", "ATM-0001", "human:alice"); !IsConflict(err) {
		t.Fatalf("duplicate ref should be conflict, got %v", err)
	}
}

func TestGuideRefAddFileAbsPath(t *testing.T) {
	s := seedGuideStore(t)
	_ = s.GuideSectionAdd("ATM", "docs", "human:alice")
	if err := s.GuideRefAdd("ATM", "docs", "file", "relative/path.md", "human:alice"); !IsUsage(err) {
		t.Fatalf("relative file should be usage, got %v", err)
	}
	if err := s.GuideRefAdd("ATM", "docs", "file", "/abs/path.md", "human:alice"); err != nil {
		t.Fatalf("abs file ref: %v", err)
	}
}

func TestGuideRefAddBadKind(t *testing.T) {
	s := seedGuideStore(t)
	_ = s.GuideSectionAdd("ATM", "s", "human:alice")
	if err := s.GuideRefAdd("ATM", "s", "blob", "x", "human:alice"); !IsUsage(err) {
		t.Fatalf("bad kind should be usage, got %v", err)
	}
}

func TestGuideRefRemoveMove(t *testing.T) {
	s := seedGuideStore(t)
	_ = s.GuideSectionAdd("ATM", "conventions", "human:alice")
	_ = s.GuideRefAdd("ATM", "conventions", "task", "ATM-0001", "human:alice")
	_ = s.GuideRefAdd("ATM", "conventions", "task", "ATM-0002", "human:alice")
	if err := s.GuideRefMove("ATM", "conventions", "task", "ATM-0002", "ATM-0001", "human:alice"); err != nil {
		t.Fatal(err)
	}
	g, _ := s.GuideGet("ATM")
	if g.Sections[0].Refs[0].Target != "ATM-0002" {
		t.Fatalf("move order = %s", g.Sections[0].Refs[0].Target)
	}
	if err := s.GuideRefRemove("ATM", "conventions", "task", "ATM-0001", "human:alice"); err != nil {
		t.Fatal(err)
	}
	g, _ = s.GuideGet("ATM")
	if len(g.Sections[0].Refs) != 1 || g.Sections[0].Refs[0].Target != "ATM-0002" {
		t.Fatalf("after remove: %v", g.Sections[0].Refs)
	}
}

func TestGuideSetFreshness(t *testing.T) {
	s := seedGuideStore(t)
	if err := s.GuideSetFreshness("ATM", "720h", "human:alice"); err != nil {
		t.Fatal(err)
	}
	p, _ := s.GetProject("ATM")
	if p.GuideFreshnessThreshold != "720h" {
		t.Fatalf("threshold = %q", p.GuideFreshnessThreshold)
	}
	if err := s.GuideSetFreshness("ATM", "unset", "human:alice"); err != nil {
		t.Fatal(err)
	}
	p, _ = s.GetProject("ATM")
	if p.GuideFreshnessThreshold != "" {
		t.Fatalf("threshold = %q want empty", p.GuideFreshnessThreshold)
	}
	if err := s.GuideSetFreshness("ATM", "not-a-duration", "human:alice"); !IsUsage(err) {
		t.Fatalf("bad duration should be usage, got %v", err)
	}
	if err := s.GuideSetFreshness("ATM", "-1h", "human:alice"); !IsUsage(err) {
		t.Fatalf("negative duration should be usage, got %v", err)
	}
}

func TestGuideStatusFreshStaleMissingUnknown(t *testing.T) {
	s := seedGuideStore(t)
	_ = s.GuideSectionAdd("ATM", "conventions", "human:alice")
	_ = s.GuideSectionAdd("ATM", "empty", "human:alice")
	_ = s.GuideRefAdd("ATM", "conventions", "task", "ATM-0001", "human:alice")
	_ = s.GuideRefAdd("ATM", "conventions", "task", "ATM-0002", "human:alice")
	tmpFile := filepath.Join(t.TempDir(), "DOC.md")
	if err := os.WriteFile(tmpFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = s.GuideRefAdd("ATM", "conventions", "file", tmpFile, "human:alice")
	_ = s.GuideRefAdd("ATM", "conventions", "file", "/does/not/exist.md", "human:alice")

	res, err := s.GuideStatus("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if res.Coverage.TotalSections != 2 {
		t.Fatalf("total_sections = %d", res.Coverage.TotalSections)
	}
	if res.Coverage.TotalRefs != 4 {
		t.Fatalf("total_refs = %d", res.Coverage.TotalRefs)
	}
	if len(res.Coverage.EmptySections) != 1 || res.Coverage.EmptySections[0] != "empty" {
		t.Fatalf("empty_sections = %v", res.Coverage.EmptySections)
	}
	for _, f := range res.Freshness {
		if f.Kind == "task" && f.State != "unknown" {
			t.Fatalf("with threshold unset, task ref state should be unknown, got %v", f)
		}
		if f.Kind == "file" && f.Target == tmpFile && f.State != "present" {
			t.Fatalf("present file = %s", f.State)
		}
		if f.Kind == "file" && f.Target == "/does/not/exist.md" && f.State != "missing" {
			t.Fatalf("missing file = %s", f.State)
		}
	}

	_ = s.GuideSetFreshness("ATM", "720h", "human:alice")
	res, _ = s.GuideStatus("ATM")
	for _, f := range res.Freshness {
		if f.Kind == "task" && (f.State != "fresh" && f.State != "stale") {
			t.Fatalf("task state with threshold = %s", f.State)
		}
	}

	tOld := time.Now().UTC().Add(-2000 * time.Hour)
	task2, _ := s.GetTask("ATM-0002")
	task2.UpdatedAt = tOld
	if err := WriteJSON(s.taskPath("ATM-0002"), task2); err != nil {
		t.Fatal(err)
	}
	res, _ = s.GuideStatus("ATM")
	var sawStale bool
	for _, f := range res.Freshness {
		if f.Target == "ATM-0002" && f.State == "stale" {
			sawStale = true
		}
	}
	if !sawStale {
		t.Fatalf("expected ATM-0002 stale, got %+v", res.Freshness)
	}

	_ = os.Remove(s.taskPath("ATM-0002"))
	res, _ = s.GuideStatus("ATM")
	var sawMissingTask bool
	for _, f := range res.Freshness {
		if f.Target == "ATM-0002" && f.State == "missing" {
			sawMissingTask = true
		}
	}
	if !sawMissingTask {
		t.Fatalf("expected ATM-0002 missing after delete, got %+v", res.Freshness)
	}
}
