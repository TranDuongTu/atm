package contextmap

import (
	"testing"
	"time"

	"atm/internal/store"
)

func TestCheckClassifiesEachPointer(t *testing.T) {
	repo := newTestRepo(t)
	rec, s, actor := newTestRecorder(t, repo)

	clean, _ := s.CreateTask("TST", "Pointer: clean", "", nil, actor)
	drifted, _ := s.CreateTask("TST", "Pointer: drifted", "", nil, actor)
	external, _ := s.CreateTask("TST", "Pointer: jira", "", nil, actor)
	unverified, _ := s.CreateTask("TST", "Pointer: handwritten", "",
		[]string{LabelContextKind("TST", "documentation")}, actor)

	commitFile(t, repo, "clean.txt", "stable\n")
	commitFile(t, repo, "drifty.txt", "before\n")
	if err := rec.Add(clean.ID, "documentation", []Source{{Kind: KindGit, Locator: "clean.txt"}}); err != nil {
		t.Fatalf("Add clean: %v", err)
	}
	if err := rec.Add(drifted.ID, "documentation", []Source{{Kind: KindGit, Locator: "drifty.txt"}}); err != nil {
		t.Fatalf("Add drifted: %v", err)
	}
	if err := rec.Add(external.ID, "documentation", []Source{{Kind: KindExternal, Locator: "jira/TST-1"}}); err != nil {
		t.Fatalf("Add external: %v", err)
	}

	commitFile(t, repo, "drifty.txt", "after\n")

	rep, err := Check(s, &Resolver{Repo: repo}, "TST", "")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	if !containsTask(rep.Drift, drifted.ID) {
		t.Errorf("drifted pointer missing from DRIFT: %+v", rep.Drift)
	}
	if !containsTask(rep.OK, clean.ID) {
		t.Errorf("clean pointer missing from OK: %+v", rep.OK)
	}
	if !containsTask(rep.Age, external.ID) {
		t.Errorf("external pointer missing from AGE: %+v", rep.Age)
	}
	if !containsTask(rep.Unverified, unverified.ID) {
		t.Errorf("unstamped pointer missing from UNVERIFIED: %+v", rep.Unverified)
	}
	// A pointer must land in exactly one bucket.
	if containsTask(rep.OK, drifted.ID) {
		t.Error("drifted pointer also reported OK")
	}
}

func TestCheckReportsNewTerritory(t *testing.T) {
	// A file changed in git that no pointer claims. This is how a repeat run
	// notices the repo grew, without check knowing anything about repo structure.
	repo := newTestRepo(t)
	rec, s, actor := newTestRecorder(t, repo)
	covered, _ := s.CreateTask("TST", "Pointer: pkg", "", nil, actor)
	if err := rec.Add(covered.ID, "documentation", []Source{{Kind: KindGit, Locator: "pkg"}}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	commitFile(t, repo, "pkg/a.go", "package pkg\n\nfunc New() {}\n") // covered -> DRIFT
	commitFile(t, repo, "brand_new.go", "package main\n")             // uncovered -> NEW

	rep, err := Check(s, &Resolver{Repo: repo}, "TST", "")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	if !contains(rep.New, "brand_new.go") {
		t.Errorf("NEW = %v, want it to contain brand_new.go", rep.New)
	}
	for _, p := range rep.New {
		if p == "pkg/a.go" {
			t.Error("pkg/a.go is claimed by a pointer; it must be DRIFT, not NEW")
		}
	}
}

func TestCheckSkipsSupersededPointers(t *testing.T) {
	// Superseded knowledge is history. It must not appear in the worklist.
	repo := newTestRepo(t)
	rec, s, actor := newTestRecorder(t, repo)
	old, _ := s.CreateTask("TST", "Pointer: old", "", nil, actor)
	next, _ := s.CreateTask("TST", "Pointer: new", "", nil, actor)
	if err := rec.Add(old.ID, "documentation", []Source{{Kind: KindGit, Locator: "pkg"}}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitFile(t, repo, "pkg/a.go", "package pkg\n\nfunc New() {}\n")
	if err := rec.Supersede(old.ID, next.ID, "gone"); err != nil {
		t.Fatalf("Supersede: %v", err)
	}

	rep, err := Check(s, &Resolver{Repo: repo}, "TST", "")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if containsTask(rep.Drift, old.ID) {
		t.Error("superseded pointer must not appear in DRIFT")
	}
}

func TestCheckAgeIsMeasuredInDays(t *testing.T) {
	repo := newTestRepo(t)
	rec, s, actor := newTestRecorder(t, repo)
	ext, _ := s.CreateTask("TST", "Pointer: notion", "", nil, actor)
	if err := rec.Add(ext.ID, "documentation", []Source{{Kind: KindExternal, Locator: "notion/arch"}}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	rep, err := Check(s, &Resolver{Repo: repo}, "TST", "")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(rep.Age) != 1 {
		t.Fatalf("AGE = %+v, want 1 finding", rep.Age)
	}
	if rep.Age[0].AgeDays != 0 {
		t.Errorf("just-stamped external: AgeDays = %d, want 0", rep.Age[0].AgeDays)
	}
	_ = time.Now
}

func TestCheckRepoRootPointerCoversAllNewTerritory(t *testing.T) {
	// A whole-repo pointer at "." claims every path in the repo, so no
	// changed path is NEW. Without the whole-repo fix, a path like "pkg/a.go"
	// would be reported as NEW even though "." claims it.
	repo := newTestRepo(t)
	rec, s, actor := newTestRecorder(t, repo)
	whole, _ := s.CreateTask("TST", "Pointer: whole repo", "", nil, actor)
	if err := rec.Add(whole.ID, "documentation", []Source{{Kind: KindGit, Locator: "."}}); err != nil {
		t.Fatalf("Add whole-repo: %v", err)
	}

	commitFile(t, repo, "pkg/a.go", "package pkg\n\nfunc New() {}\n")
	commitFile(t, repo, "brand_new.go", "package main\n")

	rep, err := Check(s, &Resolver{Repo: repo}, "TST", "")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(rep.New) != 0 {
		t.Errorf("NEW = %v, want empty (whole-repo pointer covers everything)", rep.New)
	}
	if !containsTask(rep.OK, whole.ID) || !containsTask(rep.Drift, whole.ID) {
		// Either OK (if content reverted) or DRIFT (content changed) is fine;
		// the point is the pointer is classified, not silently dropped.
		if !containsTask(rep.OK, whole.ID) && !containsTask(rep.Drift, whole.ID) {
			t.Errorf("whole-repo pointer missing from OK and DRIFT: OK=%+v DRIFT=%+v", rep.OK, rep.Drift)
		}
	}
}

func containsTask(fs []Finding, id string) bool {
	for _, f := range fs {
		if f.TaskID == id {
			return true
		}
	}
	return false
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

var _ = store.QueryFilters{}
