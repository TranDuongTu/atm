package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"atm/internal/runtime"
)

func seedSessions(t *testing.T, m *Model) {
	t.Helper()
	m.sessions.reg = runtime.Open(m.store.StorePath()).WithPIDCheck(func(pid int) bool { return pid == 100 })
	ctx := runtime.ContextPath(m.store.StorePath(), "ATM", "ATM-20260905080000-live00")
	_ = os.MkdirAll(filepath.Dir(ctx), 0o755)
	_ = os.WriteFile(ctx, []byte("# Session prompt\n\nHello **agent**.\n"), 0o644)
	recs := []runtime.Record{
		{RunID: "ATM-20260905080000-live00", Project: "ATM", Task: "ATM-1", Persona: "developer", Checklist: "code-it", Agent: "claude", Actor: "developer@claude:x",
			LauncherPID: 100, StartedAt: "2026-09-05T08:00:00Z", ContextPath: ctx, Surface: runtime.Surface{Kind: "tmux", TmuxSocket: "/s", TmuxPane: "%30"}},
		{RunID: "ATM-20260905079000-live01", Project: "ATM", Task: "ATM-1", Persona: "reviewer", Agent: "codex", Actor: "reviewer@codex:x",
			LauncherPID: 100, StartedAt: "2026-09-05T07:50:00Z", Surface: runtime.Surface{Kind: "terminal", TTY: "/dev/pts/9"}},
		{RunID: "ATM-20260905070000-lost00", Project: "ATM", Persona: "manager", Agent: "claude", Actor: "manager@claude:x",
			LauncherPID: 200, StartedAt: "2026-09-05T07:00:00Z", Surface: runtime.Surface{Kind: "herdr", HerdrPane: "p7"}},
	}
	for _, r := range recs {
		if err := m.sessions.reg.Create(r); err != nil {
			t.Fatal(err)
		}
	}
	_ = m.sessions.reg.SetStatus("ATM-20260905079000-live01", runtime.Status{State: runtime.StateBlocked, Text: "permission: Bash(rm -rf)", Source: "hook"}, nil)
	m.refreshAll()
}

func TestSessionsOverlayListsBlockedFirst(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(140, 40)
	seedSessions(t, m)
	m.handleKey(keyMsg("R"))
	if !m.sessions.open {
		t.Fatal("R must open the sessions overlay")
	}
	// lipgloss styles each cell, so assert on the plain text.
	view := stripANSI(m.sessions.renderOverlay())
	for _, want := range []string{"blocked", "live01", "live00", "lost", "lost00", "tmux %30", "herdr p7", "permission: Bash(rm -rf)", "ATM-1"} {
		if !strings.Contains(view, want) {
			t.Errorf("overlay missing %q:\n%s", want, view)
		}
	}
	if strings.Index(view, "live01") > strings.Index(view, "live00") {
		t.Fatalf("blocked session must sort first:\n%s", view)
	}
	m.handleKey(keyMsg("esc"))
	if m.sessions.open {
		t.Fatal("esc must close")
	}
}

func TestSessionsOverlayEnterFocusesSurface(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(140, 40)
	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	seedSessions(t, m)
	m.handleKey(keyMsg("R"))
	m.handleKey(keyMsg("j")) // cursor 1 -> live00 (tmux)
	m.handleKey(keyMsg("enter"))
	if len(fd.focused) != 1 || fd.focused[0].TmuxPane != "%30" {
		t.Fatalf("enter must call Focus with the row's surface, got %+v", fd.focused)
	}
	if !strings.Contains(m.toastMsg, "live00") {
		t.Fatalf("toast must name the run, got %q", m.toastMsg)
	}
}

func TestSessionsOverlayUnsupportedSurfaceShowsLocator(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(140, 40)
	m.dispatcher = &fakeDispatcher{}
	seedSessions(t, m)
	m.handleKey(keyMsg("R")) // cursor 0 -> live01, a bare tty
	m.handleKey(keyMsg("enter"))
	if !strings.Contains(m.toastMsg, "/dev/pts/9") || !strings.Contains(m.toastMsg, "no remote focus") {
		t.Fatalf("toast must show the locator and say why, got %q", m.toastMsg)
	}
}

func TestSessionsOverlayViewPrompt(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(140, 40)
	seedSessions(t, m)
	m.handleKey(keyMsg("R"))
	m.handleKey(keyMsg("j"))
	m.handleKey(keyMsg("v"))
	// renderMarkdown styles per span, so the prompt text is only contiguous
	// once the escape sequences are stripped.
	view := stripANSI(m.sessions.renderOverlay())
	if !strings.Contains(view, "Session prompt") || !strings.Contains(view, "Hello") {
		t.Fatalf("v must render the run's context file:\n%s", view)
	}
	m.handleKey(keyMsg("esc"))
	if !m.sessions.open || m.sessions.prompt {
		t.Fatal("esc from the prompt view returns to the list, not the workspace")
	}
}

func TestSessionsOverlayPrune(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(140, 40)
	seedSessions(t, m)
	m.handleKey(keyMsg("R"))
	m.handleKey(keyMsg("p"))
	// p uses the default rules against the wall clock; whatever it removes,
	// the toast reports the outcome.
	if !strings.Contains(m.toastMsg, "pruned") {
		t.Fatalf("toast = %q", m.toastMsg)
	}
}

func TestSessionsCountsForStatusBar(t *testing.T) {
	m := newTestModel(t)
	seedSessions(t, m)
	live, blocked := m.sessions.counts()
	if live != 2 || blocked != 1 {
		t.Fatalf("counts = %d live, %d blocked; want 2, 1", live, blocked)
	}
	if got := m.sessions.forTask("ATM-1"); len(got) != 2 {
		t.Fatalf("forTask = %d, want 2", len(got))
	}
}

func TestTaskDetailShowsSessionsBlock(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(140, 40)
	_, _ = m.store.CreateProject("ATM", "Acme", testActor)
	tk, _ := m.store.CreateTask("ATM", "Monitor agents", "watch them", nil, testActor)
	m.projectScope = "ATM"
	seedSessions(t, m)
	// Re-point the two task-bound records at the real task id.
	for _, id := range []string{"ATM-20260905080000-live00", "ATM-20260905079000-live01"} {
		rec, _ := m.sessions.reg.Get(id)
		_ = os.Remove(filepath.Join(m.sessions.reg.Dir(), id+".json"))
		rec.Task = tk.ID
		_ = m.sessions.reg.Create(*rec)
	}
	m.refreshAll()
	m.focused = paneTasks
	m.tasks.openDetail(tk.ID)
	view := stripANSI(m.tasks.renderDrillModal())
	for _, want := range []string{"SESSIONS  2", "live00", "live01", "blocked", "tmux %30", "s sessions"} {
		if !strings.Contains(view, want) {
			t.Errorf("detail missing %q:\n%s", want, view)
		}
	}
	m.handleKey(keyMsg("s"))
	if !m.sessions.open || m.sessions.taskFilter != tk.ID {
		t.Fatalf("s must open the sessions overlay filtered to the task: open=%v filter=%q", m.sessions.open, m.sessions.taskFilter)
	}
	if len(m.sessions.rows()) != 2 {
		t.Fatalf("filtered rows = %d, want 2", len(m.sessions.rows()))
	}
}

func TestTaskDetailWithoutSessionsHasNoBlock(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(140, 40)
	_, _ = m.store.CreateProject("ATM", "Acme", testActor)
	tk, _ := m.store.CreateTask("ATM", "Quiet task", "nobody ran it", nil, testActor)
	m.projectScope = "ATM"
	m.focused = paneTasks
	m.tasks.openDetail(tk.ID)
	if strings.Contains(stripANSI(m.tasks.renderDrillModal()), "SESSIONS") {
		t.Fatal("a task with no runs shows no SESSIONS block")
	}
}

func TestStatusLineShowsSessionCounts(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(160, 40)
	if strings.Contains(stripANSI(m.renderStatusLine()), "live") {
		t.Fatal("no sessions -> no segment")
	}
	seedSessions(t, m)
	line := stripANSI(m.renderStatusLine())
	if !strings.Contains(line, "2 live") || !strings.Contains(line, "1 blocked") {
		t.Fatalf("status line = %q", line)
	}
}
