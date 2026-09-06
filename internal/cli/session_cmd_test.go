package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"atm/internal/runtime"
)

// seedSessions plants one run of each liveness: pid 1 is always alive, and
// 2147483647 is above the pid ceiling so it never is.
func seedSessions(t *testing.T, h *goldenHarness) *runtime.Registry {
	t.Helper()
	reg := runtime.Open(h.store.StorePath())
	live := runtime.Record{RunID: "ATM-20260905080000-live00", Project: "ATM", Task: "ATM-1", Persona: "developer", Checklist: "code-it",
		Agent: "claude", Model: "fable-5.1", Actor: "developer@claude:fable-5.1", LauncherPID: 1,
		StartedAt: "2026-09-05T08:00:00Z", Surface: runtime.Surface{Kind: "tmux", TmuxSocket: "/s", TmuxPane: "%30"}}
	lost := runtime.Record{RunID: "ATM-20260905070000-lost00", Project: "ATM", Task: "ATM-1", Persona: "reviewer", Agent: "codex",
		Actor: "reviewer@codex:unset", LauncherPID: 2147483647, StartedAt: "2026-09-05T07:00:00Z", Surface: runtime.Surface{Kind: "terminal", TTY: "/dev/pts/9"}}
	ended := runtime.Record{RunID: "ATM-20260905060000-ended0", Project: "OTHER", Persona: "manager", Agent: "claude",
		Actor: "manager@claude:unset", LauncherPID: 1, StartedAt: "2026-09-05T06:00:00Z", EndedAt: "2026-09-05T06:30:00Z", Surface: runtime.Surface{Kind: "herdr", HerdrPane: "p7"}}
	for _, r := range []runtime.Record{live, lost, ended} {
		if err := reg.Create(r); err != nil {
			t.Fatal(err)
		}
	}
	_ = reg.SetStatus("ATM-20260905080000-live00", runtime.Status{State: runtime.StateBlocked, Text: "permission: Bash(rm)", Source: "hook"}, nil)
	return reg
}

func TestSessionListJSONAndFilters(t *testing.T) {
	h := newGoldenHarness(t)
	seedSessions(t, h)
	out, stderr, code := h.run("session", "list", "--output", "json")
	if code != ExitSuccess {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	var got struct {
		Sessions []map[string]any `json:"sessions"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out)
	}
	if len(got.Sessions) != 3 {
		t.Fatalf("sessions = %d, want 3", len(got.Sessions))
	}
	// Blocked floats to the top, then live, lost, ended.
	if got.Sessions[0]["run_id"] != "ATM-20260905080000-live00" || got.Sessions[0]["liveness"] != "live" || got.Sessions[0]["state"] != "blocked" {
		t.Fatalf("first row = %v", got.Sessions[0])
	}
	if got.Sessions[1]["liveness"] != "lost" || got.Sessions[2]["liveness"] != "ended" {
		t.Fatalf("order = %v %v", got.Sessions[1]["liveness"], got.Sessions[2]["liveness"])
	}
	out, _, _ = h.run("session", "list", "--project", "OTHER", "--output", "json")
	if strings.Count(out, `"run_id"`) != 1 || !strings.Contains(out, "ended0") {
		t.Fatalf("--project filter: %s", out)
	}
	out, _, _ = h.run("session", "list", "--task", "ATM-1", "--live", "--output", "json")
	if strings.Count(out, `"run_id"`) != 1 || !strings.Contains(out, "live00") {
		t.Fatalf("--task --live filter: %s", out)
	}
}

func TestSessionListText(t *testing.T) {
	h := newGoldenHarness(t)
	seedSessions(t, h)
	// The golden harness defaults to --output json, so the text renderer has
	// to be asked for by name.
	out, _, code := h.run("session", "list", "--output", "text")
	if code != ExitSuccess {
		t.Fatalf("exit=%d", code)
	}
	for _, want := range []string{"STATE", "RUN", "TASK", "SURFACE", "blocked", "live00", "tmux %30", "lost", "ended", "herdr p7", "permission: Bash(rm)"} {
		if !strings.Contains(out, want) {
			t.Errorf("text list missing %q:\n%s", want, out)
		}
	}
}

func TestSessionShow(t *testing.T) {
	h := newGoldenHarness(t)
	seedSessions(t, h)
	out, _, code := h.run("session", "show", "ATM-20260905080000-live00", "--output", "json")
	if code != ExitSuccess || !strings.Contains(out, `"surface": "tmux %30"`) || !strings.Contains(out, `"state": "blocked"`) {
		t.Fatalf("show: exit=%d\n%s", code, out)
	}
	_, stderr, code := h.run("session", "show", "ATM-00000000000000-nope00")
	if code == ExitSuccess || !strings.Contains(stderr, "not found") {
		t.Fatalf("unknown run must fail: exit=%d stderr=%s", code, stderr)
	}
}

func TestSessionStatusFromEnvRunID(t *testing.T) {
	h := newGoldenHarness(t)
	reg := seedSessions(t, h)
	t.Setenv("ATM_RUN_ID", "ATM-20260905080000-live00")
	if _, stderr, code := h.run("session", "status", "--state", "watching", "--text", "polling journal",
		"--watch-channel", "journal", "--last-poll", "2026-09-05T08:03:00Z", "--items", "2"); code != ExitSuccess {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	rec, _ := reg.Get("ATM-20260905080000-live00")
	if rec.Status.State != runtime.StateWatching || rec.Status.Text != "polling journal" || rec.Status.Source != "hook" {
		t.Fatalf("status = %+v", rec.Status)
	}
	if rec.Watch == nil || rec.Watch.Channel != "journal" || rec.Watch.ItemsSeen != 2 || rec.Watch.LastPollAt != "2026-09-05T08:03:00Z" {
		t.Fatalf("watch = %+v", rec.Watch)
	}
}

func TestSessionStatusValidation(t *testing.T) {
	h := newGoldenHarness(t)
	seedSessions(t, h)
	t.Setenv("ATM_RUN_ID", "")
	if _, stderr, code := h.run("session", "status", "--state", "idle"); code == ExitSuccess || !strings.Contains(stderr, "--run") {
		t.Fatalf("no run id must be a usage error: exit=%d stderr=%s", code, stderr)
	}
	if _, stderr, code := h.run("session", "status", "--run", "ATM-20260905080000-live00", "--state", "napping"); code == ExitSuccess || !strings.Contains(stderr, "napping") {
		t.Fatalf("bad state must be refused: exit=%d stderr=%s", code, stderr)
	}
	if _, stderr, code := h.run("session", "status", "--run", "ATM-00000000000000-nope00", "--state", "idle"); code == ExitSuccess || !strings.Contains(stderr, "not found") {
		t.Fatalf("unknown run must fail: exit=%d stderr=%s", code, stderr)
	}
	// An ended record still accepts a status write (readers keep showing it ended).
	if _, stderr, code := h.run("session", "status", "--run", "ATM-20260905060000-ended0", "--state", "idle"); code != ExitSuccess {
		t.Fatalf("ended record must accept status: %s", stderr)
	}
}

func TestSessionPrune(t *testing.T) {
	h := newGoldenHarness(t)
	reg := seedSessions(t, h)
	// Seed stamps are fixed dates, so the default 24h window depends on the
	// wall clock; assert --all, which is clock-independent.
	out, _, code := h.run("session", "prune", "--all", "--output", "json")
	if code != ExitSuccess || !strings.Contains(out, "lost00") || !strings.Contains(out, "ended0") || strings.Contains(out, "live00") {
		t.Fatalf("prune --all: exit=%d\n%s", code, out)
	}
	if entries, _ := reg.List(); len(entries) != 1 || entries[0].Record.RunID != "ATM-20260905080000-live00" {
		t.Fatalf("only the live record must remain: %+v", entries)
	}
	if _, stderr, code := h.run("session", "prune", "--older-than", "bogus"); code == ExitSuccess || !strings.Contains(stderr, "older-than") {
		t.Fatalf("bad duration must be a usage error: %s", stderr)
	}
}
