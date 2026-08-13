package cli

import (
	"os"
	"testing"
)

// runChannelErrText runs args against the testCLI harness and returns the
// resulting error's message text (empty on success) plus the exit code.
// testCLI.run (store_test.go) only surfaces the exit code — it never writes
// the error's text into the captured stderr buffer (unlike goldenHarness,
// which does so only in JSON mode) — so the gate and validation tests below,
// which must assert on the actual message, drive root.Execute() directly
// instead of going through it.
func runChannelErrText(t *testing.T, h *testCLI, args ...string) (string, int) {
	t.Helper()
	h.stdout.Reset()
	h.stderr.Reset()
	root := newRootCmdWithState(h.st)
	root.SilenceUsage = true
	root.SilenceErrors = true
	h.st.flags.store = h.store.StorePath()
	h.st.flags.output = h.output
	root.SetArgs(args)
	err := root.Execute()
	if err == nil {
		return "", ExitSuccess
	}
	return err.Error(), ExitCodeForError(err)
}

// TestChannelAddListShowJSON is the round trip: add a channel, then read it
// back through list and show in --output json, asserting the agent
// endpoint's snake_case keys and address contents. newTestCLI's cliState
// carries a nil registry, so `project create` here records no explicit
// capability choice (core.Project.Capabilities stays nil) — the legacy
// "all built-ins enabled" case the gate must allow.
func TestChannelAddListShowJSON(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")

	out := runArgsOut(t, st, "channel", "add", "--project", "ATM", "--name", "specs", "--type", "notion",
		"--purpose", "specs here", "--workspace", "acme", "--database", "abc123", "--actor", "developer@test:unit")
	mustContain(t, out, "created channel specs")

	st.output = outputJSON
	out = runArgsOut(t, st, "channel", "list", "--project", "ATM")
	mustContain(t, out, `"name": "specs"`)
	mustContain(t, out, `"type": "notion"`)
	mustContain(t, out, `"workspace": "acme"`)
	mustContain(t, out, `"database": "abc123"`)

	out = runArgsOut(t, st, "channel", "show", "--project", "ATM", "--name", "specs")
	mustContain(t, out, `"name": "specs"`)
	mustContain(t, out, `"purpose": "specs here"`)
	mustContain(t, out, `"database": "abc123"`)
}

// TestChannelGateWhenCapabilityDisabled proves the capability gate: a
// project whose Capabilities list is explicit and excludes "channel" must
// reject every verb with an error naming the enable command. The fixture is
// built directly through the store (bypassing `project create`) because that
// is the only way to record an explicit, non-nil Capabilities list here —
// see the TestChannelAddListShowJSON comment above.
func TestChannelGateWhenCapabilityDisabled(t *testing.T) {
	st := newTestCLI(t)
	if _, err := st.store.CreateProject("ATM", "x", "admin@cli:unset"); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := st.store.EnableProjectCapability("ATM", "workflow", "admin@cli:unset"); err != nil {
		t.Fatalf("enable workflow: %v", err)
	}

	errText, code := runChannelErrText(t, st, "channel", "list", "--project", "ATM")
	if code == ExitSuccess {
		t.Fatalf("expected the gate to reject list, got success")
	}
	mustContain(t, errText, "atm project capability add --project ATM --name channel")

	errText, code = runChannelErrText(t, st, "channel", "add", "--project", "ATM", "--name", "specs",
		"--type", "notion", "--actor", "developer@test:unit")
	if code == ExitSuccess {
		t.Fatalf("expected the gate to reject add, got success")
	}
	mustContain(t, errText, "atm project capability add --project ATM --name channel")
}

// TestChannelAddRejectsUnknownType proves an invalid --type surfaces the
// store's usage error, which lists the valid types.
func TestChannelAddRejectsUnknownType(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")

	errText, code := runChannelErrText(t, st, "channel", "add", "--project", "ATM", "--name", "x",
		"--type", "slack", "--actor", "developer@test:unit")
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage for an unknown channel type, got %d (%s)", code, errText)
	}
	mustContain(t, errText, "repo")
	mustContain(t, errText, "notion")
}

// TestChannelAddRequiresName proves --name is mandatory.
func TestChannelAddRequiresName(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")

	_, code := runChannelErrText(t, st, "channel", "add", "--project", "ATM", "--type", "repo",
		"--actor", "developer@test:unit")
	if code == ExitSuccess {
		t.Fatal("expected an error when --name is missing")
	}
}

// TestChannelEditChangesPurposeAndAddress proves edit updates both purpose
// and address when both sets of flags are given.
func TestChannelEditChangesPurposeAndAddress(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, _ = runArgs(st, "channel", "add", "--project", "ATM", "--name", "specs", "--type", "notion",
		"--purpose", "old purpose", "--workspace", "acme", "--database", "abc123", "--actor", "developer@test:unit")

	out := runArgsOut(t, st, "channel", "edit", "--project", "ATM", "--name", "specs",
		"--purpose", "new purpose", "--workspace", "acme", "--database", "xyz999", "--actor", "developer@test:unit")
	mustContain(t, out, "specs")

	st.output = outputJSON
	out = runArgsOut(t, st, "channel", "show", "--project", "ATM", "--name", "specs")
	mustContain(t, out, `"purpose": "new purpose"`)
	mustContain(t, out, `"database": "xyz999"`)
}

// TestChannelEditPurposeOnlyLeavesAddressUnchanged proves edit passes a nil
// *core.ChannelAddress (not a zero-valued one) when no address flag was
// changed — cmd.Flags().Changed(...) gates the pointer, so an untouched
// address must survive a purpose-only edit rather than being cleared.
func TestChannelEditPurposeOnlyLeavesAddressUnchanged(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, _ = runArgs(st, "channel", "add", "--project", "ATM", "--name", "specs", "--type", "notion",
		"--purpose", "old purpose", "--workspace", "acme", "--database", "abc123", "--actor", "developer@test:unit")

	_, _, _ = runArgs(st, "channel", "edit", "--project", "ATM", "--name", "specs",
		"--purpose", "new purpose", "--actor", "developer@test:unit")

	st.output = outputJSON
	out := runArgsOut(t, st, "channel", "show", "--project", "ATM", "--name", "specs")
	mustContain(t, out, `"purpose": "new purpose"`)
	mustContain(t, out, `"workspace": "acme"`)
	mustContain(t, out, `"database": "abc123"`)
}

// TestChannelEditOneAddressFlagKeepsSiblings is the untested middle between
// the two edit tests above: naming ONE address flag must overlay that field
// onto the stored address, not replace the whole struct. The address lives
// nowhere else, so a dropped sibling field is unrecoverable through any verb.
func TestChannelEditOneAddressFlagKeepsSiblings(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, _ = runArgs(st, "channel", "add", "--project", "ATM", "--name", "specs", "--type", "notion",
		"--purpose", "specs here", "--workspace", "acme", "--database", "abc123", "--actor", "developer@test:unit")

	_, _, _ = runArgs(st, "channel", "edit", "--project", "ATM", "--name", "specs",
		"--database", "xyz999", "--actor", "developer@test:unit")

	st.output = outputJSON
	out := runArgsOut(t, st, "channel", "show", "--project", "ATM", "--name", "specs")
	mustContain(t, out, `"database": "xyz999"`)
	mustContain(t, out, `"workspace": "acme"`)
}

// TestChannelListTextReportsProbe proves text mode reports the same status as
// the TUI for the same record: a repo channel wired to a directory that has
// since been removed is NOT plain "wired". Both surfaces read core.ChannelStatus.
func TestChannelListTextReportsProbe(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, _ = runArgs(st, "channel", "add", "--project", "ATM", "--name", "code", "--type", "repo",
		"--url", "git@x:y.git", "--actor", "developer@test:unit")

	dir := t.TempDir()
	_, _, _ = runArgs(st, "channel", "wire", "--project", "ATM", "--name", "code", "--path", dir, "--actor", "developer@test:unit")
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove wired dir: %v", err)
	}

	out := runArgsOut(t, st, "channel", "list", "--project", "ATM")
	mustContain(t, out, "path missing")
}

// TestChannelRemove proves remove drops the channel from subsequent lists.
func TestChannelRemove(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, _ = runArgs(st, "channel", "add", "--project", "ATM", "--name", "specs", "--type", "repo",
		"--url", "https://example.com/x.git", "--actor", "developer@test:unit")

	out := runArgsOut(t, st, "channel", "remove", "--project", "ATM", "--name", "specs", "--actor", "developer@test:unit")
	mustContain(t, out, "specs")

	st.output = outputJSON
	out = runArgsOut(t, st, "channel", "list", "--project", "ATM")
	if out != "[]\n" {
		t.Fatalf("expected empty channel list after remove, got %q", out)
	}
}

// TestChannelWireStampAndShowStatus is the tier-2 round trip: wire records
// this machine's local path, stamp records a verification note, and show
// --output json surfaces both plus the local probe (the directory exists).
func TestChannelWireStampAndShowStatus(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, _ = runArgs(st, "channel", "add", "--project", "ATM", "--name", "code", "--type", "repo",
		"--url", "git@x:y.git", "--actor", "developer@test:unit")

	dir := t.TempDir()
	out := runArgsOut(t, st, "channel", "wire", "--project", "ATM", "--name", "code", "--path", dir, "--actor", "developer@test:unit")
	mustContain(t, out, "code")

	out = runArgsOut(t, st, "channel", "stamp", "--project", "ATM", "--name", "code", "--note", "cloned and verified", "--actor", "developer@test:unit")
	mustContain(t, out, "code")

	st.output = outputJSON
	out = runArgsOut(t, st, "channel", "show", "--project", "ATM", "--name", "code")
	mustContain(t, out, `"path": "`+dir+`"`)
	mustContain(t, out, `"note": "cloned and verified"`)
	mustContain(t, out, `"path_exists": true`)
}

// TestChannelWireRequiresPathOrMCPServer proves wire rejects a call with
// neither --path nor --mcp-server: recording an empty wiring is pointless.
func TestChannelWireRequiresPathOrMCPServer(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, _ = runArgs(st, "channel", "add", "--project", "ATM", "--name", "code", "--type", "repo",
		"--url", "git@x:y.git", "--actor", "developer@test:unit")

	_, code := runChannelErrText(t, st, "channel", "wire", "--project", "ATM", "--name", "code", "--actor", "developer@test:unit")
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage when neither --path nor --mcp-server is given, got %d", code)
	}
}

// TestChannelStampRequiresNote proves stamp rejects a call without --note.
func TestChannelStampRequiresNote(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, _ = runArgs(st, "channel", "add", "--project", "ATM", "--name", "code", "--type", "repo",
		"--url", "git@x:y.git", "--actor", "developer@test:unit")

	_, code := runChannelErrText(t, st, "channel", "stamp", "--project", "ATM", "--name", "code", "--actor", "developer@test:unit")
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage without --note, got %d", code)
	}
}

// TestChannelMigrateRepos seeds a legacy repo through the store (the CLI verb
// that used to write it is retired) and proves migrate-repos lifts it into a
// wired repo channel, reporting the migrated count.
func TestChannelMigrateRepos(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	dir := t.TempDir()
	if err := st.store.SetProjectRepo("ATM", "atm", dir, "git@x:atm.git", "admin@cli:unset"); err != nil {
		t.Fatalf("seed legacy repo: %v", err)
	}

	out := runArgsOut(t, st, "channel", "migrate-repos", "--project", "ATM", "--actor", "developer@test:unit")
	mustContain(t, out, "migrated 1")

	st.output = outputJSON
	out = runArgsOut(t, st, "channel", "list", "--project", "ATM")
	mustContain(t, out, `"name": "atm"`)
	mustContain(t, out, `"type": "repo"`)
	mustContain(t, out, `"path": "`+dir+`"`)
}

// TestChannelMigrateReposSkipsTypeCollision proves a legacy repo whose name
// collides with an existing, differently-typed channel is left untouched in
// config.json and reported in `skipped`, not silently discarded.
func TestChannelMigrateReposSkipsTypeCollision(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, _ = runArgs(st, "channel", "add", "--project", "ATM", "--name", "docs", "--type", "notion",
		"--purpose", "specs", "--actor", "developer@test:unit")
	dir := t.TempDir()
	if err := st.store.SetProjectRepo("ATM", "docs", dir, "git@x:docs.git", "admin@cli:unset"); err != nil {
		t.Fatalf("seed legacy repo: %v", err)
	}

	out := runArgsOut(t, st, "channel", "migrate-repos", "--project", "ATM", "--actor", "developer@test:unit")
	mustContain(t, out, "migrated 0")
	mustContain(t, out, "docs")

	st.output = outputJSON
	out = runArgsOut(t, st, "channel", "migrate-repos", "--project", "ATM", "--actor", "developer@test:unit")
	mustContain(t, out, `"skipped"`)
	mustContain(t, out, `"docs"`)

	repos, err := st.store.ProjectRepos("ATM")
	if err != nil || len(repos) != 1 || repos[0].Name != "docs" || repos[0].Path != dir {
		t.Fatalf("legacy repo must survive the collision: %v %v", repos, err)
	}
}

// TestProjectRepoVerbsPointToChannels proves the retired `atm project repo`
// verbs return a pointer error naming the replacement commands instead of a
// cobra unknown-command error.
func TestProjectRepoVerbsPointToChannels(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")

	errText, code := runChannelErrText(t, st, "project", "repo", "list", "--project", "ATM")
	if code == ExitSuccess {
		t.Fatalf("expected the retired verb to fail, got success")
	}
	mustContain(t, errText, "atm channel")
	mustContain(t, errText, "migrate-repos")
}
