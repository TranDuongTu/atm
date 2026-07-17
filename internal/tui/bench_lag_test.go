package tui

import (
	"testing"

	"atm/internal/capability"
	"atm/internal/capability/workflow"
	"atm/internal/store"
)

// BenchmarkView_SelectedProject measures the cost of a full View() render with
// a project selected — the path the user hits on every keystroke. It seeds a
// store with enough tasks/labels/comments to approach the live ATM scale.
func BenchmarkView_SelectedProject(b *testing.B) {
	s, err := store.Open(b.TempDir())
	if err != nil {
		b.Fatalf("store.Open: %v", err)
	}
	if err := s.Init(""); err != nil {
		b.Fatalf("Init: %v", err)
	}
	m, err := NewModel(NewModelOpts{Service: s, Actor: "bench", Registry: capability.NewRegistry(workflow.New())})
	if err != nil {
		b.Fatalf("NewModel: %v", err)
	}
	if _, err := s.CreateProject("ATM", "Acme Task Manager", "bench"); err != nil {
		b.Fatalf("CreateProject: %v", err)
	}
	for i := 0; i < 80; i++ {
		if _, err := s.CreateTask("ATM", taskTitle(i), "", []string{"ATM:status:open", "ATM:type:bug"}, "bench"); err != nil {
			b.Fatalf("CreateTask %d: %v", i, err)
		}
	}
	m.refreshAll()
	m.projectScope = "ATM"
	m.focused = paneTasks
	m.SetSize(140, 48)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

// BenchmarkRefreshAll measures refreshAll — the heavier path run after every
// mutation (and on launch). It surfaces the per-project N+1 task + per-label
// LabelUsage cost in projects.refresh.
func BenchmarkRefreshAll(b *testing.B) {
	s, err := store.Open(b.TempDir())
	if err != nil {
		b.Fatalf("store.Open: %v", err)
	}
	if err := s.Init(""); err != nil {
		b.Fatalf("Init: %v", err)
	}
	m, err := NewModel(NewModelOpts{Service: s, Actor: "bench", Registry: capability.NewRegistry(workflow.New())})
	if err != nil {
		b.Fatalf("NewModel: %v", err)
	}
	if _, err := s.CreateProject("ATM", "Acme Task Manager", "bench"); err != nil {
		b.Fatalf("CreateProject: %v", err)
	}
	for i := 0; i < 80; i++ {
		if _, err := s.CreateTask("ATM", taskTitle(i), "", []string{"ATM:status:open", "ATM:type:bug"}, "bench"); err != nil {
			b.Fatalf("CreateTask %d: %v", i, err)
		}
	}
	m.projectScope = "ATM"
	m.refreshAll()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.refreshAll()
	}
}

// BenchmarkView_NoSelection measures View() with no project selected —
// isolates the baseline render cost from the renderSummary path that only
// runs when a project is selected.
func BenchmarkView_NoSelection(b *testing.B) {
	s, err := store.Open(b.TempDir())
	if err != nil {
		b.Fatalf("store.Open: %v", err)
	}
	if err := s.Init(""); err != nil {
		b.Fatalf("Init: %v", err)
	}
	m, err := NewModel(NewModelOpts{Service: s, Actor: "bench", Registry: capability.NewRegistry(workflow.New())})
	if err != nil {
		b.Fatalf("NewModel: %v", err)
	}
	if _, err := s.CreateProject("ATM", "Acme Task Manager", "bench"); err != nil {
		b.Fatalf("CreateProject: %v", err)
	}
	for i := 0; i < 80; i++ {
		if _, err := s.CreateTask("ATM", taskTitle(i), "", []string{"ATM:status:open", "ATM:type:bug"}, "bench"); err != nil {
			b.Fatalf("CreateTask %d: %v", i, err)
		}
	}
	m.refreshAll()
	m.focused = paneProjects
	m.SetSize(140, 48)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func taskTitle(i int) string {
	titles := []string{
		"lag investigation",
		"refactor cache layer",
		"add benchmark suite",
		"document sync design",
		"fix indexer race",
	}
	return titles[i%len(titles)]
}
