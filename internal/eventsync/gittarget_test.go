package eventsync

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// initBareRepo creates a fresh bare repo in a temp dir and seeds it with
// one empty commit on "main" (pushed via a scratch clone) so it has a
// real HEAD/branch for GitTarget to clone against, matching how a shared
// remote actually looks before anyone has synced a project into it.
func initBareRepo(t *testing.T) string {
	t.Helper()

	bare := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, "", "init", "--bare", bare)

	scratch := t.TempDir()
	runGit(t, "", "clone", bare, scratch)
	runGit(t, scratch, "checkout", "-b", "main")
	runGit(t, scratch, "commit", "--allow-empty", "-m", "init")
	runGit(t, scratch, "push", "origin", "main")

	return bare
}

// runGit runs git with a pinned, non-interactive author identity so tests
// don't depend on the environment's global git config.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=atm-test", "GIT_AUTHOR_EMAIL=atm-test@localhost",
		"GIT_COMMITTER_NAME=atm-test", "GIT_COMMITTER_EMAIL=atm-test@localhost",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}
}

func TestGitFetchAbsent(t *testing.T) {
	skipIfNoGit(t)
	remote := initBareRepo(t)
	target := NewGitTarget(t.TempDir(), remote, ".atm")

	snap, err := target.Fetch(context.Background(), "PRJ")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !snap.Absent {
		t.Errorf("Absent = false, want true")
	}
}

func TestGitPublishThenFetchRoundTrip(t *testing.T) {
	skipIfNoGit(t)
	remote := initBareRepo(t)
	clock := fixedClock()
	root := mustProject(t, clock, replicaA, "PRJ")
	task := mustTask(t, clock, replicaA, []string{root.ID}, "task")

	ctx := context.Background()
	target := NewGitTarget(t.TempDir(), remote, ".atm")
	if err := target.Publish(ctx, "PRJ", rawOf(root, task), nil); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// A fresh cache dir proves the round trip goes through the remote,
	// not through target's own local clone.
	other := NewGitTarget(t.TempDir(), remote, ".atm")
	snap, err := other.Fetch(ctx, "PRJ")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Absent {
		t.Fatalf("Absent = true, want false")
	}
	if len(snap.Events) != 2 || snap.Events[0].ID != root.ID || snap.Events[1].ID != task.ID {
		t.Errorf("Events = %v, want [%s, %s]", rawEventIDs(snap.Events), root.ID, task.ID)
	}
	wantDigest := SetDigest([]string{root.ID, task.ID})
	if snap.Digest != wantDigest {
		t.Errorf("Digest = %q, want %q", snap.Digest, wantDigest)
	}
	head, ok := snap.State.(string)
	if !ok || head == "" {
		t.Errorf("State = %#v, want a non-empty head commit string", snap.State)
	}
}

func TestGitPublishWritesGitattributes(t *testing.T) {
	skipIfNoGit(t)
	remote := initBareRepo(t)
	clock := fixedClock()
	root := mustProject(t, clock, replicaA, "PRJ")

	target := NewGitTarget(t.TempDir(), remote, ".atm")
	if err := target.Publish(context.Background(), "PRJ", rawOf(root), nil); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(target.workdir, ".atm", ".gitattributes"))
	if err != nil {
		t.Fatalf("ReadFile .gitattributes: %v", err)
	}
	// Patterns in a .gitattributes file are relative to the directory it
	// lives in, i.e. <subpath>/, so the entry names the path from there.
	want := "PRJ/events.v2.jsonl merge=union"
	if !strings.Contains(string(data), want) {
		t.Errorf(".gitattributes = %q, want it to contain %q", data, want)
	}
}

func TestGitNonFastForwardRetryUnions(t *testing.T) {
	skipIfNoGit(t)
	remote := initBareRepo(t)
	clock := fixedClock()
	root := mustProject(t, clock, replicaA, "PRJ")
	e1 := mustTask(t, clock, replicaA, []string{root.ID}, "e1")
	e2 := mustTask(t, clock, replicaB, []string{root.ID}, "e2")

	ctx := context.Background()
	a := NewGitTarget(t.TempDir(), remote, ".atm")
	b := NewGitTarget(t.TempDir(), remote, ".atm")

	// Seed both replicas from the same base commit, then race two
	// concurrent publishers against it. Whichever wins the push forces
	// the other through the non-fast-forward retry path; either way the
	// two event sets must end up unioned on the remote, never one
	// silently dropped.
	if err := a.Publish(ctx, "PRJ", rawOf(root), nil); err != nil {
		t.Fatalf("seed Publish: %v", err)
	}
	if _, err := b.Fetch(ctx, "PRJ"); err != nil {
		t.Fatalf("B seed Fetch: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); errs <- a.Publish(ctx, "PRJ", rawOf(e1), nil) }()
	go func() { defer wg.Done(); errs <- b.Publish(ctx, "PRJ", rawOf(e2), nil) }()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Publish: %v", err)
		}
	}

	final := NewGitTarget(t.TempDir(), remote, ".atm")
	snap, err := final.Fetch(ctx, "PRJ")
	if err != nil {
		t.Fatalf("final Fetch: %v", err)
	}
	got := make(map[string]bool, len(snap.Events))
	for _, ev := range snap.Events {
		got[ev.ID] = true
	}
	for _, id := range []string{root.ID, e1.ID, e2.ID} {
		if !got[id] {
			t.Errorf("missing event %s from union after concurrent publish", id)
		}
	}
	if len(snap.Events) != 3 {
		t.Errorf("Events count = %d, want 3 (deduped union)", len(snap.Events))
	}
}

func TestGitPublishRetryExhaustionErrors(t *testing.T) {
	skipIfNoGit(t)
	remote := initBareRepo(t)
	clock := fixedClock()
	root := mustProject(t, clock, replicaA, "PRJ")

	ctx := context.Background()
	target := NewGitTarget(t.TempDir(), remote, ".atm")

	// Force the clone to happen while the remote is still writable, then
	// strip write permission from the whole bare repo so every push in
	// the retry loop fails the same way.
	if _, err := target.Fetch(ctx, "PRJ"); err != nil {
		t.Fatalf("seed Fetch: %v", err)
	}
	chmodTree(t, remote, 0o555, 0o444)
	t.Cleanup(func() { chmodTree(t, remote, 0o755, 0o644) })

	err := target.Publish(ctx, "PRJ", rawOf(root), nil)
	if err == nil {
		t.Fatal("Publish: want error, got nil")
	}
	if !strings.Contains(err.Error(), "retry") {
		t.Errorf("error = %q, want it to mention retry", err)
	}
}

func TestGitMissingBinaryError(t *testing.T) {
	skipIfNoGit(t)
	t.Setenv("PATH", "")

	target := NewGitTarget(t.TempDir(), "file:///nonexistent.git", ".atm")
	_, err := target.Fetch(context.Background(), "PRJ")
	if err == nil {
		t.Fatal("Fetch: want error, got nil")
	}
	if !strings.Contains(err.Error(), "git binary not found") {
		t.Errorf("error = %q, want it to mention that the git binary was not found", err)
	}
}

// chmodTree sets dirMode on every directory and fileMode on every file
// under root, used to make a bare repo read-only (or restore it) without
// depending on running as a user that write permission wouldn't stop.
func chmodTree(t *testing.T, root string, dirMode, fileMode os.FileMode) {
	t.Helper()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return os.Chmod(path, dirMode)
		}
		return os.Chmod(path, fileMode)
	})
	if err != nil {
		t.Fatalf("chmodTree %s: %v", root, err)
	}
}
