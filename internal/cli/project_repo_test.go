package cli

import "testing"

// `atm project repo add|list|remove` are retired: repo dispatch targets
// moved to `atm channel` (add/wire, or migrate-repos for a one-shot lift of
// what's already recorded). The verbs stay mounted — so old muscle memory
// gets a pointer, not a cobra unknown-command error — but every call fails
// unconditionally. The behavior these tests used to exercise (round trip,
// URL, JSON, --project validation, not-found, nonexistent-path rejection)
// still lives at the store level (internal/store/config_test.go, via
// SetProjectRepo/ProjectRepos/RemoveProjectRepo, which migrate-repos still
// calls) and at the CLI level through the new channel verbs
// (TestChannelWireStampAndShowStatus, TestChannelMigrateRepos* in
// channel_test.go). What remains here is the retirement contract itself:
// each verb must return the pointer error, unconditionally, regardless of
// its arguments.

func TestProjectRepoAddIsRetired(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	dir := t.TempDir()

	errText, code := runChannelErrText(t, st, "project", "repo", "add", "main", dir, "--project", "ATM", "--actor", "admin@cli:unset")
	if code == ExitSuccess {
		t.Fatalf("expected the retired verb to fail, got success")
	}
	mustContain(t, errText, "atm channel")
	mustContain(t, errText, "migrate-repos")
}

func TestProjectRepoListIsRetired(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")

	errText, code := runChannelErrText(t, st, "project", "repo", "list", "--project", "ATM")
	if code == ExitSuccess {
		t.Fatalf("expected the retired verb to fail, got success")
	}
	mustContain(t, errText, "atm channel")
	mustContain(t, errText, "migrate-repos")
}

func TestProjectRepoRemoveIsRetired(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")

	errText, code := runChannelErrText(t, st, "project", "repo", "remove", "main", "--project", "ATM", "--actor", "admin@cli:unset")
	if code == ExitSuccess {
		t.Fatalf("expected the retired verb to fail, got success")
	}
	mustContain(t, errText, "atm channel")
	mustContain(t, errText, "migrate-repos")
}
