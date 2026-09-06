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
