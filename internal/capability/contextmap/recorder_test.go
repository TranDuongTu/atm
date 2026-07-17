package contextmap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"atm/internal/store"
)

func newTestRecorder(t *testing.T, repo string) (*Recorder, *store.Store, string) {
	t.Helper()
	s, actor := newTestStore(t)
	return &Recorder{Store: s, Resolver: &Resolver{Repo: repo}, Actor: actor}, s, actor
}

func TestAddStampsAndLabels(t *testing.T) {
	repo := newTestRepo(t)
	rec, s, actor := newTestRecorder(t, repo)
	task, err := s.CreateTask("TST", "Code pointer: pkg", "", nil, actor)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	src := Source{Kind: KindGit, Locator: "pkg"}
	if err := rec.Add(task.ID, "documentation", []Source{src}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, err := s.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if !hasLabel(got.Labels, LabelContextKind("TST", "documentation")) {
		t.Errorf("kind label not applied: %v", got.Labels)
	}

	stamp, ok, err := LatestStamp(s, task.ID, "TST")
	if err != nil || !ok {
		t.Fatalf("LatestStamp: ok=%v err=%v", ok, err)
	}
	if len(stamp.Witnesses) != 1 || stamp.Witnesses[0].Source != src {
		t.Fatalf("stamp sources = %+v, want [%v]", stamp.Witnesses, src)
	}
	if stamp.Witnesses[0].Value == "" {
		t.Error("git source recorded with no witness")
	}
	if stamp.Head == "" {
		t.Error("stamp recorded no HEAD")
	}
}

func TestStampAppendsRatherThanReplaces(t *testing.T) {
	// Freshness history is the point: each re-stamp leaves the previous one
	// behind, so the thread records every revision at which this was verified.
	repo := newTestRepo(t)
	rec, s, actor := newTestRecorder(t, repo)
	task, _ := s.CreateTask("TST", "Code pointer: pkg", "", nil, actor)
	if err := rec.Add(task.ID, "documentation", []Source{{Kind: KindGit, Locator: "pkg"}}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitFile(t, repo, "pkg/a.go", "package pkg\n\nfunc New() {}\n")
	if err := rec.Stamp(task.ID); err != nil {
		t.Fatalf("Stamp: %v", err)
	}

	comments, err := s.ListComments(task.ID)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	n := 0
	for _, c := range comments {
		if hasLabel(c.Labels, LabelProvenance("TST")) {
			n++
		}
	}
	if n != 2 {
		t.Errorf("provenance comments = %d, want 2 (add + stamp)", n)
	}
}

func TestRetargetKeepsTaskAndRecordsNewSources(t *testing.T) {
	repo := newTestRepo(t)
	rec, s, actor := newTestRecorder(t, repo)
	task, _ := s.CreateTask("TST", "Code pointer: pkg", "", nil, actor)
	if err := rec.Add(task.ID, "documentation", []Source{{Kind: KindGit, Locator: "pkg"}}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	commitFile(t, repo, "moved.go", "package moved\n")
	newSrc := Source{Kind: KindGit, Locator: "moved.go"}
	if err := rec.Retarget(task.ID, []Source{newSrc}); err != nil {
		t.Fatalf("Retarget: %v", err)
	}

	stamp, ok, err := LatestStamp(s, task.ID, "TST")
	if err != nil || !ok {
		t.Fatalf("LatestStamp: ok=%v err=%v", ok, err)
	}
	if len(stamp.Witnesses) != 1 || stamp.Witnesses[0].Source != newSrc {
		t.Errorf("sources = %+v, want [%v]", stamp.Witnesses, newSrc)
	}
	if _, err := s.GetTask(task.ID); err != nil {
		t.Errorf("task must survive a retarget: %v", err)
	}
}

func TestSupersedeLabelsOldAndKeepsHistory(t *testing.T) {
	repo := newTestRepo(t)
	rec, s, actor := newTestRecorder(t, repo)
	old, _ := s.CreateTask("TST", "Doc pointer: old", "the old thing", nil, actor)
	replacement, _ := s.CreateTask("TST", "Doc pointer: new", "", nil, actor)
	if err := rec.Add(old.ID, "documentation", []Source{{Kind: KindGit, Locator: "pkg"}}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := rec.Supersede(old.ID, replacement.ID, "renderer moved"); err != nil {
		t.Fatalf("Supersede: %v", err)
	}

	got, err := s.GetTask(old.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if !hasLabel(got.Labels, LabelSuperseded("TST")) {
		t.Errorf("superseded label not applied: %v", got.Labels)
	}
	// It keeps its KIND -- lifecycle composes with kind, it does not replace it.
	if !hasLabel(got.Labels, LabelContextKind("TST", "documentation")) {
		t.Errorf("kind label was removed: %v", got.Labels)
	}
	if !strings.Contains(got.Description, replacement.ID) {
		t.Errorf("description does not name the successor: %q", got.Description)
	}
	if !strings.Contains(got.Description, "the old thing") {
		t.Errorf("original narrative was destroyed: %q", got.Description)
	}
}

// TestSupersededTaskLeavesCurrentBoard proves the "CLI returns the latest"
// requirement: it is a board, and it needs no code of its own.
func TestSupersededTaskLeavesCurrentBoard(t *testing.T) {
	repo := newTestRepo(t)
	rec, s, actor := newTestRecorder(t, repo)
	keep, _ := s.CreateTask("TST", "Doc pointer: keep", "", nil, actor)
	drop, _ := s.CreateTask("TST", "Doc pointer: drop", "", nil, actor)
	replacement, _ := s.CreateTask("TST", "Doc pointer: new", "", nil, actor)
	for _, id := range []string{keep.ID, drop.ID} {
		if err := rec.Add(id, "documentation", []Source{{Kind: KindGit, Locator: "pkg"}}); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	if err := rec.Supersede(drop.ID, replacement.ID, "obsolete"); err != nil {
		t.Fatalf("Supersede: %v", err)
	}

	tasks, err := s.ListTasksErr(store.QueryFilters{
		Project: "TST",
		Labels:  []string{BoardCurrent("TST")},
	})
	if err != nil {
		t.Fatalf("ListTasksErr: %v", err)
	}
	ids := map[string]bool{}
	for _, tk := range tasks {
		ids[tk.ID] = true
	}
	if !ids[keep.ID] {
		t.Errorf("current board must contain the live pointer %s", keep.ID)
	}
	if ids[drop.ID] {
		t.Errorf("current board must not contain the superseded pointer %s", drop.ID)
	}
}

func TestStampRefusesWhenSourceIsGone(t *testing.T) {
	// stamp's contract is "the subject is unchanged in meaning". If the
	// subject is gone, stamp must refuse -- the manager must retarget (if it
	// moved) or supersede (if it died). A silent empty witness would lie that
	// the pointer was re-verified.
	repo := newTestRepo(t)
	rec, s, actor := newTestRecorder(t, repo)
	task, _ := s.CreateTask("TST", "Code pointer: gone", "", nil, actor)
	if err := rec.Add(task.ID, "documentation", []Source{{Kind: KindGit, Locator: "pkg"}}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Delete the source directory and commit. The pointer's subject is gone.
	if err := os.RemoveAll(filepath.Join(repo, "pkg")); err != nil {
		t.Fatal(err)
	}
	commitFile(t, repo, "other.txt", "x")

	err := rec.Stamp(task.ID)
	if err == nil {
		t.Fatal("Stamp on a gone source: want error, got nil")
	}
	if !strings.Contains(err.Error(), "gone") {
		t.Errorf("Stamp error = %q, want it to mention 'gone'", err.Error())
	}
}

func hasLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}
