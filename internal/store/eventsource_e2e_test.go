package store

import (
	"atm/internal/store/eventlog"
	"fmt"
	"sort"
	"testing"
)

// TestEventsourceV2EndToEndUpgradeWriteRebuildVerify drives the whole v2
// lifecycle through the public store API: v1 authoring, upgrade, post-cutover
// v2 writes, verify, the log-derived views (sequence probe, history,
// activity, text search), and rebuild. There is no rollback: once a project
// is cut over, it stays v2 forever.
func TestEventsourceV2EndToEndUpgradeWriteRebuildVerify(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("ATM", "x", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "before", "", []string{"ATM:status:open"}, "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "after v2", "", nil, "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if r, err := s.VerifyProject("ATM"); err != nil {
		t.Fatal(err)
	} else if r.Diverged || !r.LogOK || r.Format != string(eventlog.StoreFormatV2) {
		t.Fatalf("verify after v2 write = %#v", r)
	}
	// The whole system runs on v2 now: sequence probe, history, activity
	// entries, and text search all serve from the event file (Task 9b).
	if seq, err := s.LastLogSeq("ATM"); err != nil || seq == 0 {
		t.Fatalf("LastLogSeq on v2 = %d, %v", seq, err)
	}
	if hv := s.History("ATM", Subject{Kind: "project", Code: "ATM"}); len(hv) == 0 {
		t.Fatal("no v2 project history")
	}
	if entries, err := s.ReadLogCached("ATM"); err != nil || len(entries) == 0 {
		t.Fatalf("no v2 activity entries: %d, %v", len(entries), err)
	}
	if hits, _, err := s.Search(SearchParams{Project: "ATM", QueryText: "after v2", K: 5}); err != nil || len(hits) == 0 {
		t.Fatalf("text search found nothing on v2: %v", err)
	}
	if _, err := s.Rebuild(); err != nil {
		t.Fatal(err)
	}
	if tasks := s.ListTasks(QueryFilters{Project: "ATM"}); len(tasks) != 2 {
		t.Fatalf("tasks after rebuild = %d, want 2", len(tasks))
	}
}

// TestEventsourceV2ListOrderFollowsCreationNotAlias pins the display order of a
// v2 project. A v2 alias is a content hash, so id-asc is hash order, not
// creation order -- and a v2 project routinely holds BOTH generations (numeric
// aliases carried over by the upgrade, hash aliases born after cutover). Task
// and comment lists must follow the fold's creation ordinal instead.
func TestEventsourceV2ListOrderFollowsCreationNotAlias(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("ATM", "x", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	// Two v1 tasks, then cut over: the upgraded tasks keep numeric aliases.
	var want []string
	for i := 1; i <= 2; i++ {
		tk, err := s.CreateTask("ATM", fmt.Sprintf("v1 task %d", i), "", nil, "admin@cli:unset")
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, tk.ID)
	}
	// Two comments seeded BEFORE the cutover, so that after it the task carries
	// a mixed-generation thread: v1 numeric aliases (c0001, c0002) alongside the
	// v2 hash aliases minted below. That is the exact shape the CLI transcript
	// showed rendering out of order.
	var wantC []string
	for i := 1; i <= 2; i++ {
		c, err := s.CreateComment(want[0], fmt.Sprintf("v1 note %d", i), nil, "", "admin@cli:unset")
		if err != nil {
			t.Fatal(err)
		}
		wantC = append(wantC, c.ID)
	}
	// Six more hash-aliased tasks. Six random hashes land in creation
	// order by luck once in 720 runs, so an id-asc sort cannot pass this.
	for i := 1; i <= 6; i++ {
		tk, err := s.CreateTask("ATM", fmt.Sprintf("v2 task %d", i), "", nil, "admin@cli:unset")
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, tk.ID)
	}
	var got []string
	for _, tk := range s.ListTasks(QueryFilters{Project: "ATM"}) {
		got = append(got, tk.ID)
	}
	if len(got) != len(want) {
		t.Fatalf("ListTasks = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ListTasks order = %v, want creation order %v", got, want)
		}
	}

	// Same for the comment thread on the upgraded (numeric-aliased) task, which
	// now mixes both alias generations: c0001, c0002 from v1, hash aliases after.
	for i := 1; i <= 6; i++ {
		c, err := s.CreateComment(want[0], fmt.Sprintf("v2 note %d", i), nil, "", "admin@cli:unset")
		if err != nil {
			t.Fatal(err)
		}
		wantC = append(wantC, c.ID)
	}
	// The pre-fix read path ordered a thread by id. A numeric alias always sorts
	// ahead of a hash one (c0001 < c39d9 as text), so the mixed generations alone
	// do NOT discriminate: only the hash aliases' relative order does, and six
	// random hashes land in creation order about once in 720 runs -- an id-asc
	// sort could pass by luck. Append comments until the creation-ordered thread
	// is provably NOT in id order, so that any id-asc read must fail this test.
	for n := 0; ascendingByID(wantC); n++ {
		if n >= 20 {
			t.Fatalf("could not build a thread whose creation order differs from id order: %v", wantC)
		}
		c, err := s.CreateComment(want[0], fmt.Sprintf("v2 tiebreak %d", n), nil, "", "admin@cli:unset")
		if err != nil {
			t.Fatal(err)
		}
		wantC = append(wantC, c.ID)
	}
	comments, err := s.ListComments(want[0])
	if err != nil {
		t.Fatal(err)
	}
	var gotC []string
	for _, c := range comments {
		gotC = append(gotC, c.ID)
	}
	if len(gotC) != len(wantC) {
		t.Fatalf("ListComments = %v, want %v", gotC, wantC)
	}
	for i := range wantC {
		if gotC[i] != wantC[i] {
			t.Fatalf("ListComments order = %v, want creation order %v", gotC, wantC)
		}
	}
}

// ascendingByID reports whether ids (in creation order) also happen to be in
// ascending id order -- i.e. whether an id-asc read would render them the same
// way, which would make an ordering assertion over them vacuous.
func ascendingByID(ids []string) bool {
	return sort.SliceIsSorted(ids, func(i, j int) bool { return ids[i] < ids[j] })
}
