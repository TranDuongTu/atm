package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// endedAt registers a record that started and ended at the given stamp.
func endedAt(t *testing.T, r *Registry, store, id, stamp string) {
	t.Helper()
	rec := sample(id, 100)
	rec.StartedAt = stamp
	rec.EndedAt = stamp
	rec.ContextPath = ContextPath(store, "ATM", id)
	_ = os.MkdirAll(filepath.Dir(rec.ContextPath), 0o755)
	_ = os.WriteFile(rec.ContextPath, []byte("# prompt\n"), 0o644)
	if err := r.Create(rec); err != nil {
		t.Fatal(err)
	}
}

func TestPruneNeverRemovesLive(t *testing.T) {
	r, store := newReg(t)
	rec := sample("ATM-20260101000000-aaaaaa", 100) // pid alive
	rec.StartedAt = "2026-01-01T00:00:00Z"          // months old
	rec.ContextPath = ContextPath(store, "ATM", rec.RunID)
	_ = os.MkdirAll(filepath.Dir(rec.ContextPath), 0o755)
	_ = os.WriteFile(rec.ContextPath, []byte("x"), 0o644)
	_ = r.Create(rec)
	res, err := r.Prune(PruneOptions{All: true})
	if err != nil || len(res.Removed) != 0 {
		t.Fatalf("live record pruned: %+v err=%v", res, err)
	}
	if _, err := os.Stat(rec.ContextPath); err != nil {
		t.Fatal("live record's prompt must survive prune")
	}
}

func TestPruneByAge(t *testing.T) {
	r, store := newReg(t)                                                     // clock 2026-09-05T08:00:00Z
	endedAt(t, r, store, "ATM-20260904070000-old000", "2026-09-04T07:00:00Z") // 25h → gone
	endedAt(t, r, store, "ATM-20260905070000-new000", "2026-09-05T07:00:00Z") // 1h → stays
	res, err := r.Prune(PruneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Removed) != 1 || res.Removed[0] != "ATM-20260904070000-old000" {
		t.Fatalf("Removed = %v", res.Removed)
	}
	if _, err := os.Stat(ContextPath(store, "ATM", "ATM-20260904070000-old000")); !os.IsNotExist(err) {
		t.Fatal("pruned record's prompt must be deleted with it")
	}
	if _, err := r.Get("ATM-20260905070000-new000"); err != nil {
		t.Fatal("recent record must survive")
	}
}

func TestPruneLostByAge(t *testing.T) {
	r, _ := newReg(t)
	rec := sample("ATM-20260904070000-lost00", 200) // pid dead, never ended
	rec.StartedAt = "2026-09-04T07:00:00Z"
	_ = r.Create(rec)
	res, _ := r.Prune(PruneOptions{})
	if len(res.Removed) != 1 {
		t.Fatalf("lost record older than 24h must be pruned: %+v", res)
	}
}

func TestPruneCap(t *testing.T) {
	r, store := newReg(t)
	for i := 0; i < 60; i++ {
		stamp := fmt.Sprintf("2026-09-05T07:%02d:00Z", i) // all within 24h
		endedAt(t, r, store, fmt.Sprintf("ATM-20260905070000-%06d", i), stamp)
	}
	res, err := r.Prune(PruneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Removed) != 10 {
		t.Fatalf("Removed = %d, want 10 (60 non-live minus DefaultKeep 50)", len(res.Removed))
	}
	// Oldest go first.
	for _, id := range res.Removed {
		if id > "ATM-20260905070000-000009" {
			t.Errorf("removed a newer record before an older one: %s", id)
		}
	}
	entries, _ := r.List()
	if len(entries) != 50 {
		t.Fatalf("remaining = %d, want 50", len(entries))
	}
	res, _ = r.Prune(PruneOptions{Keep: 5})
	if len(res.Removed) != 45 {
		t.Fatalf("Keep override: removed %d, want 45", len(res.Removed))
	}
}

func TestPruneOrphanSweep(t *testing.T) {
	r, store := newReg(t)
	endedAt(t, r, store, "ATM-20260905070000-keep00", "2026-09-05T07:00:00Z")
	orphan := ContextPath(store, "ATM", "ATM-20260905060000-orphan")
	_ = os.WriteFile(orphan, []byte("stale prompt"), 0o644)
	storeOrphan := ContextPath(store, "", "atm-20260905060000-orphan")
	_ = os.MkdirAll(filepath.Dir(storeOrphan), 0o755)
	_ = os.WriteFile(storeOrphan, []byte("stale prompt"), 0o644)
	res, err := r.Prune(PruneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Orphans) != 2 {
		t.Fatalf("Orphans = %v, want both orphan prompts", res.Orphans)
	}
	for _, p := range []string{orphan, storeOrphan} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("orphan %s must be deleted", p)
		}
	}
	if _, err := os.Stat(ContextPath(store, "ATM", "ATM-20260905070000-keep00")); err != nil {
		t.Fatal("a record's own prompt is not an orphan")
	}
}

func TestPruneAllAndUnreadableSurvive(t *testing.T) {
	r, store := newReg(t)
	endedAt(t, r, store, "ATM-20260905070000-a00000", "2026-09-05T07:00:00Z")
	dir := r.Dir()
	_ = os.WriteFile(filepath.Join(dir, "ATM-20260905070000-newer0.json"), []byte(`{"schema":99,"run_id":"ATM-20260905070000-newer0"}`), 0o644)
	res, _ := r.Prune(PruneOptions{All: true})
	if len(res.Removed) != 1 || res.Removed[0] != "ATM-20260905070000-a00000" {
		t.Fatalf("All must remove every readable non-live record and no unreadable one: %v", res.Removed)
	}
	if _, err := os.Stat(filepath.Join(dir, "ATM-20260905070000-newer0.json")); err != nil {
		t.Fatal("unreadable record must survive prune")
	}
}
