package store

import "testing"

func TestLastLogSeqReturnsEventCountForV2(t *testing.T) {
	s := testStore(t)
	_, _ = s.CreateProject("ATM", "x", "admin@cli:unset")
	_, _ = s.UpgradeProjectToV2("ATM")
	before, err := s.LastLogSeq("ATM")
	if err != nil {
		t.Fatal(err)
	}
	want, err := s.v2EventCount("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if before != want || before == 0 {
		t.Fatalf("LastLogSeq = %d, want v2 event count %d", before, want)
	}
	if _, err := s.CreateTask("ATM", "wake the watcher", "", nil, "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	after, err := s.LastLogSeq("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if after <= before {
		t.Fatalf("LastLogSeq did not advance after a v2 append: %d -> %d (Watch and the TUI indexer pane would never wake)", before, after)
	}
}

func TestHistoryRendersV2EventsForTaskAlias(t *testing.T) {
	s := testStore(t)
	_, _ = s.CreateProject("ATM", "x", "admin@cli:unset")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "admin@cli:unset")
	_, _ = s.UpgradeProjectToV2("ATM")
	if err := s.SetTitle(tk.ID, "t2", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	hv := s.History("ATM", Subject{Kind: "task", ID: tk.ID})
	if len(hv) < 2 {
		t.Fatalf("history = %#v, want task.created and task.title-changed", hv)
	}
	seen := map[string]bool{}
	lastSeq := 0
	for _, h := range hv {
		seen[h.Action] = true
		if h.Seq <= lastSeq {
			t.Fatalf("history ordinals not strictly increasing: %#v", hv)
		}
		lastSeq = h.Seq
	}
	if !seen["task.created"] || !seen["task.title-changed"] {
		t.Fatalf("history actions = %#v", hv)
	}
}

func TestReadLogCachedServesActivityShapedV2Entries(t *testing.T) {
	s := testStore(t)
	_, _ = s.CreateProject("ATM", "x", "admin@cli:unset")
	_, _ = s.UpgradeProjectToV2("ATM")
	entries, err := s.ReadLogCached("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no compatibility entries from v2 file")
	}
	for i, e := range entries {
		if e.Seq != i+1 || e.Actor == "" || e.Action == "" || e.At.IsZero() {
			t.Fatalf("entry %d = %#v: activity.Build needs Seq/At/Actor/Action", i, e)
		}
	}
	// Freshness across appends (the actors pane path): a new write must show up.
	n := len(entries)
	if _, err := s.CreateTask("ATM", "new", "", nil, "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	entries, err = s.ReadLogCached("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) <= n {
		t.Fatalf("ReadLogCached snapshot did not refresh: %d -> %d", n, len(entries))
	}
}

func TestTextSearchFindsV2Entities(t *testing.T) {
	s := testStore(t)
	_, _ = s.CreateProject("ATM", "x", "admin@cli:unset")
	_, _ = s.UpgradeProjectToV2("ATM")
	tk, err := s.CreateTask("ATM", "quantum flux capacitor", "", nil, "admin@cli:unset")
	if err != nil {
		t.Fatal(err)
	}
	hits, fallback, err := s.Search(SearchParams{Project: "ATM", QueryText: "quantum capacitor", K: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !fallback || len(hits) == 0 || hits[0].ID != tk.ID {
		t.Fatalf("hits = %#v (fallback=%t), want text hit on %s", hits, fallback, tk.ID)
	}
}

func TestDedupVectorsKeepsLastEntryOnTiedLogSeq(t *testing.T) {
	entries := []VectorEntry{
		{ID: "ATM-abcdef", LogSeq: 3, TextHash: "sha256:old"},
		{ID: "ATM-abcdef", LogSeq: 3, TextHash: "sha256:new"},
	}
	out := dedupVectorsByID(entries)
	if len(out) != 1 || out[0].TextHash != "sha256:new" {
		t.Fatalf("dedup = %#v: a v2 re-embedding reuses the entity's stable creation ordinal, so the LAST entry (append order) must win", out)
	}
}

func TestPendingIndexEnumeratesV2Entities(t *testing.T) {
	s := testStore(t)
	_, _ = s.CreateProject("ATM", "x", "admin@cli:unset")
	_, _ = s.UpgradeProjectToV2("ATM")
	tk, _ := s.CreateTask("ATM", "embed me", "body", nil, "admin@cli:unset")
	pending, err := s.PendingIndex("ATM", "test-model")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range pending {
		if d.ID == tk.ID && d.Kind == "task" {
			found = true
		}
	}
	if !found {
		t.Fatalf("pending = %#v, want the v2-created task (it would otherwise never be embedded)", pending)
	}
}

func TestV2AliasesRoundTripThroughCommentsAndHistory(t *testing.T) {
	// End-to-end closure of Task 2b's grammar decision: both alias
	// generations round-trip through CreateComment (incl. reply
	// validation), GetComment, and History. Lives here because it needs
	// Task 8's v2 mutators AND this task's History branch.
	s := testStore(t)
	if err := s.SetActiveFormat(StoreFormatV2); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject("ATM", "x", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	tk, err := s.CreateTask("ATM", "hash-alias task", "", nil, "admin@cli:unset")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateComment(tk.ID, "first", nil, "", "admin@cli:unset")
	if err != nil {
		t.Fatalf("CreateComment on hash task alias %q: %v", tk.ID, err)
	}
	r, err := s.CreateComment(tk.ID, "reply", nil, c.ID, "admin@cli:unset")
	if err != nil {
		t.Fatalf("reply to hash comment alias %q: %v", c.ID, err)
	}
	if _, err := s.CreateComment(tk.ID, "cross-task", nil, "ATM-ffffff-cffff", "admin@cli:unset"); !IsUsage(err) {
		t.Fatalf("reply-to under a different task alias = %v, want ErrUsage", err)
	}
	if got, err := s.GetComment(r.ID); err != nil || got.ReplyTo != c.ID {
		t.Fatalf("GetComment(%q) = %#v, %v", r.ID, got, err)
	}
	if hv := s.History("ATM", Subject{Kind: "task", ID: tk.ID}); len(hv) == 0 {
		t.Fatal("History must match a v2 hash task alias (subject aliases restored from the fold)")
	}
	if hv := s.History("ATM", Subject{Kind: "comment", ID: c.ID}); len(hv) == 0 {
		t.Fatal("History must match a v2 hash comment alias")
	}
}
