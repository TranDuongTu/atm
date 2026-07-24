package tui

import (
	"testing"

	"atm/internal/capability"
	"atm/internal/capability/workflow"
	"atm/internal/core"
	"atm/internal/store"
)

// countingService wraps the real store and counts GetProject calls, so tests
// can pin how many freshness-probed point reads a refresh performs.
type countingService struct {
	core.Service
	getProject int
}

func (c *countingService) GetProject(code string) (*core.Project, error) {
	c.getProject++
	return c.Service.GetProject(code)
}

// A tasks refresh must resolve the capability registry once per refresh, not
// once per task row: regFor -> GetProject runs a cache freshness probe, and
// per-row calls made refreshAll O(rows) probes (ATM-4c476c).
func TestTasksRefreshResolvesRegistryOncePerRefresh(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	svc := &countingService{Service: s}
	m, err := NewModel(NewModelOpts{Service: svc, Actor: testActor, Registry: capability.NewRegistry(workflow.New())})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	if _, err := s.CreateProject("ATM", "Acme", testActor); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, err := s.CreateTask("ATM", "task", "", []string{"ATM:status:open"}, testActor); err != nil {
			t.Fatalf("CreateTask: %v", err)
		}
	}
	m.projectScope = "ATM"
	m.refreshAll()
	if len(m.tasks.rows)+groupRowCount(m.tasks.groups) != 5 {
		t.Fatalf("setup: expected 5 task rows, got rows=%d groups=%d", len(m.tasks.rows), groupRowCount(m.tasks.groups))
	}

	svc.getProject = 0
	m.tasks.refresh()
	if svc.getProject > 1 {
		t.Fatalf("tasks refresh called GetProject %d times for 5 rows; want at most 1", svc.getProject)
	}
}

func groupRowCount(gs []taskGroup) int {
	n := 0
	for _, g := range gs {
		n += len(g.rows) + groupRowCount(g.subgroups)
	}
	return n
}
