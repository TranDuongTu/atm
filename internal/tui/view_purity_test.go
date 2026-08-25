package tui

import (
	"testing"

	"atm/internal/capability"
	"atm/internal/capability/scrum"
	"atm/internal/core"
	"atm/internal/store"
)

// readCountingService wraps the real store and counts the reads the render
// path historically leaked (ATM-4c476c): GetProject (freshness probe),
// ListTasks (sqlite query) and ReadLogCached (event-file staleness probe).
type readCountingService struct {
	core.Service
	reads int
}

func (c *readCountingService) GetProject(code string) (*core.Project, error) {
	c.reads++
	return c.Service.GetProject(code)
}

func (c *readCountingService) ListTasks(f core.QueryFilters) []*core.Task {
	c.reads++
	return c.Service.ListTasks(f)
}

func (c *readCountingService) ReadLogCached(code string) ([]core.LogEntry, error) {
	c.reads++
	return c.Service.ReadLogCached(code)
}

// View must be pure formatting over refresh-time snapshots: no store reads
// per frame. The summary pane and events feed read GetProject + ListTasks +
// ReadLogCached on every keystroke before ATM-4c476c.
func TestViewPerformsNoStoreReads(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	svc := &readCountingService{Service: s}
	m, err := NewModel(NewModelOpts{Service: svc, Actor: testActor, Registry: capability.NewRegistry(scrum.New())})
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
	m.focused = paneTasks
	m.SetSize(160, 48)

	if out := m.View(); out == "" {
		t.Fatal("setup: View rendered empty")
	}
	svc.reads = 0
	_ = m.View()
	if svc.reads != 0 {
		t.Fatalf("View performed %d store reads; want 0 (render must consume refresh-time snapshots)", svc.reads)
	}
}

// Selecting a project bypasses refreshAll by design, so the select handler
// must refresh the summary snapshot itself — otherwise the summary pane and
// events feed would render the previous project until the next 10s tick.
func TestSelectProjectRefreshesSummarySnapshot(t *testing.T) {
	m := newTestModel(t)
	if _, err := m.store.CreateProject("AAA", "First", testActor); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := m.store.CreateProject("BBB", "Second", testActor); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := m.store.CreateTask("BBB", "only-bbb-task", "", nil, testActor); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	m.refreshAll()
	m.SetSize(160, 48)

	// Move the cursor to BBB and select it via the key handler ("s").
	m.focused = paneProjects
	for i := 0; i < len(m.projects.list); i++ {
		if r, ok := m.projects.selected(); ok && r.code == "BBB" {
			break
		}
		m.projects.handleKey(keyMsg("down"))
	}
	m.projects.handleKey(keyMsg("s"))
	if m.projectScope != "BBB" {
		t.Fatalf("setup: projectScope = %q, want BBB", m.projectScope)
	}
	project, tasks, _, ok := m.projects.projectSummaryData()
	if !ok || project == nil || project.Code != "BBB" {
		t.Fatalf("summary snapshot project = %+v ok=%v, want BBB", project, ok)
	}
	if len(tasks) != 1 || tasks[0].Title != "only-bbb-task" {
		t.Fatalf("summary snapshot tasks = %v, want the single BBB task", tasks)
	}
}
