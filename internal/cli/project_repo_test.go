package cli

import (
	"path/filepath"
	"testing"
)

func TestProjectRepoAddListRemoveRoundTrip(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	dir := t.TempDir()
	out := runArgsOut(t, st, "project", "repo", "add", "main", dir, "--project", "ATM", "--actor", "admin@cli:unset")
	mustContain(t, out, "main")
	mustContain(t, out, dir)

	out = runArgsOut(t, st, "project", "repo", "list", "--project", "ATM")
	if out != "main\t"+dir+"\n" {
		t.Fatalf("unexpected list output: %q", out)
	}

	out = runArgsOut(t, st, "project", "repo", "remove", "main", "--project", "ATM", "--actor", "admin@cli:unset")
	mustContain(t, out, "main")

	out = runArgsOut(t, st, "project", "repo", "list", "--project", "ATM")
	if out != "" {
		t.Fatalf("expected empty list after remove, got %q", out)
	}
}

func TestProjectRepoAddWithURL(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	dir := t.TempDir()
	out := runArgsOut(t, st, "project", "repo", "add", "main", dir, "--url", "https://example.com/atm.git", "--project", "ATM", "--actor", "admin@cli:unset")
	mustContain(t, out, "main")
	out = runArgsOut(t, st, "project", "repo", "list", "--project", "ATM")
	mustContain(t, out, "https://example.com/atm.git")
}

func TestProjectRepoListJSON(t *testing.T) {
	st := newTestCLI(t)
	st.output = outputJSON
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	dir := t.TempDir()
	_, _, _ = runArgs(st, "project", "repo", "add", "main", dir, "--url", "https://example.com/atm.git", "--project", "ATM", "--actor", "admin@cli:unset")
	out := runArgsOut(t, st, "project", "repo", "list", "--project", "ATM")
	mustContain(t, out, `"name": "main"`)
	mustContain(t, out, `"path": "`+dir+`"`)
	mustContain(t, out, `"url": "https://example.com/atm.git"`)
}

func TestProjectRepoAddRequiresProject(t *testing.T) {
	st := newTestCLI(t)
	dir := t.TempDir()
	_, _, code := runArgs(st, "project", "repo", "add", "main", dir, "--actor", "admin@cli:unset")
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage without --project, got %d", code)
	}
}

func TestProjectRepoRemoveRequiresProject(t *testing.T) {
	st := newTestCLI(t)
	_, _, code := runArgs(st, "project", "repo", "remove", "main", "--actor", "admin@cli:unset")
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage without --project, got %d", code)
	}
}

func TestProjectRepoRemoveUnknownNotFound(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, code := runArgs(st, "project", "repo", "remove", "nope", "--project", "ATM", "--actor", "admin@cli:unset")
	if code != ExitNotFound {
		t.Fatalf("expected ExitNotFound removing unknown repo, got %d", code)
	}
}

func TestProjectRepoAddRejectsNonexistentPath(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	_, _, code := runArgs(st, "project", "repo", "add", "main", missing, "--project", "ATM", "--actor", "admin@cli:unset")
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage for non-existent path, got %d", code)
	}
}