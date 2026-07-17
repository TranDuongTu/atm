package tui

import (
	"strings"
	"testing"
	"time"

	"atm/internal/store"
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
	_, _ = s.CreateProject("ATM", "Acme Task Manager", "admin@cli:test")
	// One task exists before the TUI starts.
	_, _ = s.CreateTask("ATM", "pre-existing", "", []string{"ATM:status:open"}, "admin@cli:test")

	m, err := NewModel(NewModelOpts{Service: s, Actor: "admin@tui:unset"})
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
	if _, err := ext.CreateTask("ATM", "external-task", "", []string{"ATM:status:open"}, "admin@cli:test"); err != nil {
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

// TestInitSchedulesRefreshTick proves Init returns a command, so the periodic
// refresh loop is started on launch.
func TestInitSchedulesRefreshTick(t *testing.T) {
	m := newTestModel(t)
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init returned nil cmd; want a refresh-tick scheduling command")
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
}

func TestRefreshTickIntervalIsTenSeconds(t *testing.T) {
	if refreshTickInterval != 10*time.Second {
		t.Fatalf("refreshTickInterval = %s want 10s", refreshTickInterval)
	}
}

func TestRefreshAllRecordsLastRefreshTime(t *testing.T) {
	m := newTestModel(t)
	old := time.Now().Add(-time.Hour)
	m.lastRefreshAt = old

	m.refreshAll()

	if !m.lastRefreshAt.After(old) {
		t.Fatalf("lastRefreshAt = %v, want after %v", m.lastRefreshAt, old)
	}
}

func TestRefreshAgeLabel(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		last time.Time
		want string
	}{
		{name: "zero", last: time.Time{}, want: "--"},
		{name: "subsecond", last: now.Add(-500 * time.Millisecond), want: "now"},
		{name: "seconds", last: now.Add(-16 * time.Second), want: "16s ago"},
		{name: "minutes", last: now.Add(-2 * time.Minute), want: "2m ago"},
		{name: "hours", last: now.Add(-3 * time.Hour), want: "3h ago"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := refreshAgeLabel(tt.last, now); got != tt.want {
				t.Fatalf("refreshAgeLabel() = %q want %q", got, tt.want)
			}
		})
	}
}

func TestRefreshRecencySegmentFreshShowsCheckOnly(t *testing.T) {
	m := newTestModel(t)
	m.lastRefreshAt = time.Now().Add(-9 * time.Second)

	seg := m.refreshRecencySegment()

	if !strings.Contains(seg, "✓") {
		t.Fatalf("fresh segment missing check icon: %q", seg)
	}
	if strings.Contains(seg, "ago") || strings.Contains(seg, "↻") {
		t.Fatalf("fresh segment should not show age or refresh icon: %q", seg)
	}
	if styleProbe(m.styles.StatusOK) != styleProbe(m.refreshRecencyStyle()) {
		t.Fatalf("fresh segment should use StatusOK style")
	}
}

func TestRefreshRecencySegmentStaleShowsAge(t *testing.T) {
	m := newTestModel(t)
	m.lastRefreshAt = time.Now().Add(-16 * time.Second)

	seg := m.refreshRecencySegment()

	if !strings.Contains(seg, "↻ ") || !strings.Contains(seg, "s ago") {
		t.Fatalf("stale segment should show refresh icon and seconds ago: %q", seg)
	}
}

func TestStatusLineShowsRefreshRecencyRightmost(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.plugins = []plugin{&fakePlugin{}}
	m.lastRefreshAt = time.Now().Add(-9 * time.Second)

	line := m.renderStatusLine()
	trimmed := strings.TrimRight(line, " ")

	if !strings.HasSuffix(trimmed, "✓") {
		t.Fatalf("status line should end with refresh indicator:\n%s", line)
	}
	if strings.Index(line, "# off") > strings.LastIndex(line, "✓") {
		t.Fatalf("plugin dock should render before rightmost refresh indicator:\n%s", line)
	}
}
