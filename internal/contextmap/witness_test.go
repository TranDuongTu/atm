package contextmap

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// newTestRepo makes a git repo with one committed file and returns its root.
func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg", "a.go"), []byte("package pkg\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-qm", "init")
	return dir
}

func commitFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-qm", "change"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestGitWitnessUnchangedIsOK(t *testing.T) {
	repo := newTestRepo(t)
	r := &Resolver{Repo: repo}
	src := Source{Kind: KindGit, Locator: "pkg"}

	recorded, err := r.Witness(src)
	if err != nil {
		t.Fatalf("Witness: %v", err)
	}
	if recorded == "" {
		t.Fatal("Witness returned empty object id")
	}
	got, err := r.Compare(src, recorded)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if got != VerdictOK {
		t.Errorf("unchanged path: got %v, want %v", got, VerdictOK)
	}
}

func TestGitWitnessChangedIsDrift(t *testing.T) {
	repo := newTestRepo(t)
	r := &Resolver{Repo: repo}
	src := Source{Kind: KindGit, Locator: "pkg"}
	recorded, err := r.Witness(src)
	if err != nil {
		t.Fatalf("Witness: %v", err)
	}

	commitFile(t, repo, "pkg/a.go", "package pkg\n\nfunc New() {}\n")

	got, err := r.Compare(src, recorded)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if got != VerdictDrift {
		t.Errorf("changed path: got %v, want %v", got, VerdictDrift)
	}
}

func TestGitWitnessDeletedIsGone(t *testing.T) {
	repo := newTestRepo(t)
	r := &Resolver{Repo: repo}
	src := Source{Kind: KindGit, Locator: "pkg"}
	recorded, err := r.Witness(src)
	if err != nil {
		t.Fatalf("Witness: %v", err)
	}

	if err := os.RemoveAll(filepath.Join(repo, "pkg")); err != nil {
		t.Fatal(err)
	}
	commitFile(t, repo, "other.txt", "x")

	got, err := r.Compare(src, recorded)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if got != VerdictGone {
		t.Errorf("deleted path: got %v, want %v", got, VerdictGone)
	}
}

func TestFileWitness(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &Resolver{}
	src := Source{Kind: KindFile, Locator: path}

	recorded, err := r.Witness(src)
	if err != nil {
		t.Fatalf("Witness: %v", err)
	}
	if v, err := r.Compare(src, recorded); err != nil || v != VerdictOK {
		t.Fatalf("unchanged file: got %v, %v; want OK", v, err)
	}

	if err := os.WriteFile(path, []byte("goodbye"), 0o644); err != nil {
		t.Fatal(err)
	}
	if v, err := r.Compare(src, recorded); err != nil || v != VerdictDrift {
		t.Fatalf("changed file: got %v, %v; want DRIFT", v, err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if v, err := r.Compare(src, recorded); err != nil || v != VerdictGone {
		t.Fatalf("deleted file: got %v, %v; want GONE", v, err)
	}
}

func TestChangedSince(t *testing.T) {
	repo := newTestRepo(t)
	r := &Resolver{Repo: repo}
	head, err := r.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}

	commitFile(t, repo, "new.txt", "x")

	changed, err := r.ChangedSince(head)
	if err != nil {
		t.Fatalf("ChangedSince: %v", err)
	}
	if len(changed) != 1 || changed[0] != "new.txt" {
		t.Errorf("ChangedSince = %v, want [new.txt]", changed)
	}
}
