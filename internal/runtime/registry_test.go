package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixedClock(s string) func() time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return func() time.Time { return t }
}

func alivePIDs(pids ...int) func(int) bool {
	set := map[int]bool{}
	for _, p := range pids {
		set[p] = true
	}
	return func(p int) bool { return set[p] }
}

func newReg(t *testing.T) (*Registry, string) {
	t.Helper()
	store := t.TempDir()
	r := Open(store).WithClock(fixedClock("2026-09-05T08:00:00Z")).WithPIDCheck(alivePIDs(100))
	return r, store
}

func sample(runID string, pid int) Record {
	return Record{RunID: runID, Project: "ATM", Task: "ATM-9339a7", Persona: "developer",
		Checklist: "superpowers-task-cycle", Agent: "claude", Model: "fable-5.1",
		Actor: "developer@claude:fable-5.1", LauncherPID: pid,
		Surface: Surface{Kind: "tmux", TmuxSocket: "/tmp/tmux-1000/default", TmuxPane: "%30"}}
}

func TestCreateWritesFileWithDefaults(t *testing.T) {
	r, store := newReg(t)
	if err := r.Create(sample("ATM-20260905080000-aaaaaa", 100)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store, "runtime", "sessions", "ATM-20260905080000-aaaaaa.json")); err != nil {
		t.Fatalf("record file missing: %v", err)
	}
	rec, err := r.Get("ATM-20260905080000-aaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Schema != Schema || rec.StartedAt != "2026-09-05T08:00:00Z" || rec.Status.State != StateWorking || rec.Status.Source != "launcher" || rec.Status.At != "2026-09-05T08:00:00Z" {
		t.Fatalf("defaults not applied: %+v", rec)
	}
}

func TestCreateRejectsBadRunID(t *testing.T) {
	r, _ := newReg(t)
	for _, id := range []string{"", "../x", "a/b", "has space"} {
		if err := r.Create(sample(id, 100)); err == nil {
			t.Errorf("Create(%q) must fail", id)
		}
	}
}

func TestGetNotFound(t *testing.T) {
	r, _ := newReg(t)
	if _, err := r.Get("ATM-20260905080000-ffffff"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestSetStatusAndWatch(t *testing.T) {
	r, _ := newReg(t)
	_ = r.Create(sample("ATM-20260905080000-aaaaaa", 100))
	err := r.SetStatus("ATM-20260905080000-aaaaaa", Status{State: StateWatching, Text: "polling journal", Source: "hook"},
		&Watch{Channel: "journal", LastPollAt: "2026-09-05T08:03:00Z", ItemsSeen: 2})
	if err != nil {
		t.Fatal(err)
	}
	rec, _ := r.Get("ATM-20260905080000-aaaaaa")
	if rec.Status.State != StateWatching || rec.Status.At != "2026-09-05T08:00:00Z" || rec.Watch == nil || rec.Watch.Channel != "journal" || rec.Watch.ItemsSeen != 2 {
		t.Fatalf("status not applied: %+v watch=%+v", rec.Status, rec.Watch)
	}
	if rec.Persona != "developer" || rec.LauncherPID != 100 {
		t.Fatalf("identity fields must survive a status write: %+v", rec)
	}
	// A status write without watch flags leaves the watch block alone.
	_ = r.SetStatus("ATM-20260905080000-aaaaaa", Status{State: StateIdle, Source: "hook"}, nil)
	rec, _ = r.Get("ATM-20260905080000-aaaaaa")
	if rec.Watch == nil || rec.Status.State != StateIdle {
		t.Fatalf("watch dropped or state not updated: %+v", rec)
	}
	if err := r.SetStatus("ATM-20260905080000-ffffff", Status{State: StateIdle}, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown run must be ErrNotFound, got %v", err)
	}
}

func TestEndAndLiveness(t *testing.T) {
	r, _ := newReg(t)
	_ = r.Create(sample("ATM-20260905080000-aaaaaa", 100)) // pid alive → live
	_ = r.Create(sample("ATM-20260905080000-bbbbbb", 200)) // pid dead → lost
	_ = r.Create(sample("ATM-20260905080000-cccccc", 100))
	if err := r.End("ATM-20260905080000-cccccc", 3); err != nil {
		t.Fatal(err)
	}
	c, _ := r.Get("ATM-20260905080000-cccccc")
	if c.EndedAt != "2026-09-05T08:00:00Z" || c.ExitCode == nil || *c.ExitCode != 3 || c.Status.State != StateEnded {
		t.Fatalf("End not applied: %+v", c)
	}
	// A late hook must not revive an ended record's ended_at.
	_ = r.SetStatus("ATM-20260905080000-cccccc", Status{State: StateIdle, Source: "hook"}, nil)
	c, _ = r.Get("ATM-20260905080000-cccccc")
	if c.EndedAt == "" || r.Liveness(*c) != LiveEnded {
		t.Fatalf("ended record revived: %+v", c)
	}
	entries, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Liveness{}
	for _, e := range entries {
		got[e.Record.RunID] = e.Liveness
	}
	want := map[string]Liveness{"ATM-20260905080000-aaaaaa": LiveLive, "ATM-20260905080000-bbbbbb": LiveLost, "ATM-20260905080000-cccccc": LiveEnded}
	for id, l := range want {
		if got[id] != l {
			t.Errorf("%s liveness = %q, want %q", id, got[id], l)
		}
	}
}

func TestListTreatsNewerSchemaAsUnreadable(t *testing.T) {
	r, store := newReg(t)
	dir := filepath.Join(store, "runtime", "sessions")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "ATM-20260905080000-999999.json"), []byte(`{"schema":99,"run_id":"ATM-20260905080000-999999"}`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "ATM-20260905080000-888888.json"), []byte(`{not json`), 0o644)
	entries, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	for _, e := range entries {
		if e.Liveness != LiveUnreadable || e.Record.RunID == "" {
			t.Errorf("entry %+v must be unreadable and keep its run id from the filename", e)
		}
	}
	if _, err := r.Get("ATM-20260905080000-999999"); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("Get newer schema err = %v, want ErrUnreadable", err)
	}
}

func TestListEmptyDirIsNotAnError(t *testing.T) {
	r, _ := newReg(t)
	entries, err := r.List()
	if err != nil || len(entries) != 0 {
		t.Fatalf("List on missing dir = %v, %v", entries, err)
	}
}
