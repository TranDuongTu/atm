package tui

import (
	"strings"
	"testing"
	"time"

	"atm/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// TestRefreshTickSurfacesExternalMutation proves Fix D: after the TUI starts,
// a task created by another process (simulated by a second Store handle to
// the same cache.db + log.jsonl) becomes visible in the Tasks pane once the
// periodic refresh tick fires, without the user pressing any key.
func TestRefreshTickSurfacesExternalMutation(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(""); err != nil {
		t.Fatal(err)
	}
	_, _ = s.CreateProject("ATM", "Acme Task Manager", "ext")
	// One task exists before the TUI starts.
	_, _ = s.CreateTask("ATM", "pre-existing", "", []string{"ATM:status:open"}, "ext")

	m, err := NewModel(NewModelOpts{StorePath: s.StorePath(), Actor: "tui"})
	if err != nil {
		t.Fatal(err)
	}
	m.SetSize(120, 40)
	// Select the project so the Tasks pane is scoped.
	m.projectScope = "ATM"
	m.refreshAll()

	// Sanity: only the pre-existing task is visible.
	if n := len(m.tasks.rows); n != 1 {
		t.Fatalf("before external mutation, tasks pane has %d rows want 1", n)
	}

	// Simulate another process creating a task against the same store root.
	ext, _ := store.Open(s.StorePath())
	if _, err := ext.CreateTask("ATM", "external-task", "", []string{"ATM:status:open"}, "cli"); err != nil {
		t.Fatalf("external CreateTask: %v", err)
	}

	// Without a tick, the TUI still shows only the pre-existing task (the
	// snapshot has not been refreshed).
	if n := len(m.tasks.rows); n != 1 {
		t.Fatalf("external task leaked into TUI without a tick: %d rows", n)
	}

	// Fire the refresh tick. Update must call refreshAll, which re-reads
	// cache.db and surfaces the new task.
	mm, _ := m.Update(refreshTickMsg{})
	out, ok := mm.(*Model)
	if !ok {
		t.Fatalf("Update did not return *Model")
	}
	if n := len(out.tasks.rows); n != 2 {
		t.Fatalf("after refresh tick, tasks pane has %d rows want 2 (external task not surfaced)", n)
	}
	// The new task's title must appear in the rendered Tasks pane.
	body := out.tasks.View()
	if !strings.Contains(body, "external-task") {
		t.Fatalf("external-task not rendered in Tasks pane:\n%s", body)
	}
}

// TestInitSchedulesRefreshTick proves Init returns a command that produces a
// refreshTickMsg (so the periodic refresh loop is started on launch).
func TestInitSchedulesRefreshTick(t *testing.T) {
	m := newTestModel(t)
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init returned nil cmd; want a refresh-tick scheduling command")
	}
	msg := cmd()
	// The command should produce a refreshTickMsg (possibly after a delay).
	if _, ok := msg.(refreshTickMsg); !ok {
		t.Fatalf("Init cmd produced %T, want refreshTickMsg", msg)
	}
}

// TestRefreshTickReSchedules proves that after handling a refreshTickMsg,
// Update returns a command that schedules the next tick, so the loop is
// continuous.
func TestRefreshTickReSchedules(t *testing.T) {
	m := newTestModel(t)
	_, cmd := m.Update(refreshTickMsg{})
	if cmd == nil {
		t.Fatal("Update(refreshTickMsg) returned nil cmd; want next-tick scheduling")
	}
	// The next command should resolve (after a delay) to a refreshTickMsg.
	// Use a short timeout via a helper goroutine so the test doesn't hang.
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		if _, ok := msg.(refreshTickMsg); !ok {
			t.Fatalf("next-tick cmd produced %T, want refreshTickMsg", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("next-tick cmd did not produce a message within 2s")
	}
}