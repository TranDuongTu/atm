package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"atm/internal/store"
)

// Minimal test harness for the store subcommands. The package's golden harness
// defaults to JSON output and is oriented around fixture comparison; these
// tests need both text and JSON modes plus substring assertions, so a small
// standalone harness is cleaner than bending goldenHarness to fit.

type testCLI struct {
	t      *testing.T
	st     *cliState
	store  *store.Store
	stdout *bytes.Buffer
	stderr *bytes.Buffer
	output string
}

func newTestCLI(t *testing.T) *testCLI {
	t.Helper()
	dir := t.TempDir()
	st := &cliState{flags: globalFlags{output: outputText}}
	buf := &bytes.Buffer{}
	ebuf := &bytes.Buffer{}
	st.out = buf
	st.err = ebuf
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(""); err != nil {
		t.Fatal(err)
	}
	return &testCLI{t: t, st: st, store: s, stdout: buf, stderr: ebuf, output: outputText}
}

func (h *testCLI) run(args ...string) (string, string, int) {
	h.stdout.Reset()
	h.stderr.Reset()
	root := newRootCmdWithState(h.st)
	root.SilenceUsage = true
	root.SilenceErrors = true
	h.st.flags.store = h.store.StorePath()
	h.st.flags.output = h.output
	root.SetArgs(args)
	err := root.Execute()
	code := ExitSuccess
	if err != nil {
		code = ExitCodeForError(err)
	}
	return h.stdout.String(), h.stderr.String(), code
}

func runArgs(h *testCLI, args ...string) (string, string, int) {
	return h.run(args...)
}

func runArgsOut(t *testing.T, h *testCLI, args ...string) string {
	t.Helper()
	out, stderr, code := h.run(args...)
	if code != ExitSuccess {
		t.Fatalf("run %v: exit=%d stderr=%s", args, code, stderr)
	}
	return out
}

func mustContain(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Fatalf("missing %q in:\n%s", sub, s)
	}
}

func mustNotContain(t *testing.T, s, sub string) {
	t.Helper()
	if strings.Contains(s, sub) {
		t.Fatalf("unexpected %q in:\n%s", sub, s)
	}
}

func hasLineWithPrefix(s, prefix string) bool {
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

func TestStoreLogText(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, _ = runArgs(st, "task", "create", "--project", "ATM", "--title", "t", "--actor", "admin@cli:unset")
	out := runArgsOut(t, st, "store", "log", "ATM")
	mustContain(t, out, "task.created")
	mustContain(t, out, "project.created")
}

func TestStoreLogJSON(t *testing.T) {
	st := newTestCLI(t)
	st.output = outputJSON
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	out := runArgsOut(t, st, "store", "log", "ATM")
	mustContain(t, out, `"action": "project.created"`)
}

func TestStoreVerifyClean(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	out := runArgsOut(t, st, "store", "verify", "ATM")
	mustContain(t, out, "ok")
}

func TestStoreRebuild(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	out := runArgsOut(t, st, "store", "rebuild")
	mustContain(t, out, "projects")
}
func TestStoreLogFromToFilter(t *testing.T) {
	st := newTestCLI(t)
	_, _ = st.store.CreateProject("ATM", "x", "admin@cli:unset")
	// Generate six task events: project.created=1, then tasks 2..6.
	for i := 0; i < 5; i++ {
		if _, err := st.store.CreateTask("ATM", "t", "", nil, "admin@cli:unset"); err != nil {
			t.Fatal(err)
		}
	}
	out := runArgsOut(t, st, "store", "log", "ATM", "--from", "3", "--to", "5")
	if !hasLineWithPrefix(out, "3\t") {
		t.Fatalf("missing seq 3 in:\n%s", out)
	}
	if !hasLineWithPrefix(out, "4\t") {
		t.Fatalf("missing seq 4 in:\n%s", out)
	}
	if !hasLineWithPrefix(out, "5\t") {
		t.Fatalf("missing seq 5 in:\n%s", out)
	}
	if hasLineWithPrefix(out, "1\t") {
		t.Fatalf("unexpected seq 1 in:\n%s", out)
	}
	if hasLineWithPrefix(out, "2\t") {
		t.Fatalf("unexpected seq 2 in:\n%s", out)
	}
	if hasLineWithPrefix(out, "6\t") {
		t.Fatalf("unexpected seq 6 in:\n%s", out)
	}
}

func TestStoreLogShowsV2EventsForV2ActiveProject(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	// Born v2: no upgrade needed — the project's log surface is already v2.
	out := runArgsOut(t, st, "store", "log", "ATM")
	mustContain(t, out, "project.created")
	mustContain(t, out, "sha256:")
}

// runArgsStdoutOut is runArgsOut for the commands that render their TEXT output
// straight to os.Stdout instead of cliState.stdout() (project list, task comment
// show, ...): it swaps in a pipe for the duration of the run and returns both
// sinks concatenated. The golden harness never needed this because it drives
// those commands in JSON mode.
func runArgsStdoutOut(t *testing.T, h *testCLI, args ...string) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	var captured bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&captured, r)
		close(done)
	}()
	out, stderr, code := h.run(args...)
	os.Stdout = old
	_ = w.Close()
	<-done
	_ = r.Close()
	if code != ExitSuccess {
		t.Fatalf("run %v: exit=%d stderr=%s", args, code, stderr)
	}
	return out + captured.String()
}

// plantV1LogCLI writes the shared v1 fixture (project code "ATM") directly
// into a fresh store's project directory, bypassing the (now v2-only) public
// Create* API, mirroring internal/store's plantV1Project idiom for this
// package's black-box CLI harness.
func plantV1LogCLI(t *testing.T, s *store.Store, code string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "eventsource", "testdata", "v1-log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.StorePath(), "projects", code)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "log.jsonl"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStorePruneV1SkipsBornV2(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	out := runArgsOut(t, st, "store", "prune-v1", "--project", "ATM")
	mustContain(t, out, "skipped\tATM\tborn v2 (no v1 log)")
	if _, err := os.Stat(filepath.Join(st.store.StorePath(), "projects", "ATM")); err != nil {
		t.Fatalf("project dir missing: %v", err)
	}
}

func TestStorePruneV1SkipsV1Active(t *testing.T) {
	st := newTestCLI(t)
	plantV1LogCLI(t, st.store, "ATM")
	out := runArgsOut(t, st, "store", "prune-v1", "--project", "ATM")
	mustContain(t, out, "skipped\tATM\tnot v2-active")
	if _, err := os.Stat(filepath.Join(st.store.StorePath(), "projects", "ATM", "log.jsonl")); err != nil {
		t.Fatalf("v1-active log.jsonl should survive: %v", err)
	}
}

func TestStorePruneV1ArchivesUpgradedProjectByDefault(t *testing.T) {
	st := newTestCLI(t)
	plantV1LogCLI(t, st.store, "ATM")
	if _, err := st.store.UpgradeProjectToV2("ATM"); err != nil {
		t.Fatal(err)
	}
	out := runArgsOut(t, st, "store", "prune-v1", "--project", "ATM")
	mustContain(t, out, "pruned\tATM\tarchived")
	logPath := filepath.Join(st.store.StorePath(), "projects", "ATM", "log.jsonl")
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("log.jsonl should be archived away, stat err=%v", err)
	}
}

func TestStorePruneV1DeleteRemoves(t *testing.T) {
	st := newTestCLI(t)
	plantV1LogCLI(t, st.store, "ATM")
	if _, err := st.store.UpgradeProjectToV2("ATM"); err != nil {
		t.Fatal(err)
	}
	out := runArgsOut(t, st, "store", "prune-v1", "--project", "ATM", "--delete")
	mustContain(t, out, "pruned\tATM\tdeleted")
	logPath := filepath.Join(st.store.StorePath(), "projects", "ATM", "log.jsonl")
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("log.jsonl should be deleted, stat err=%v", err)
	}
}

func TestStorePruneV1AllFlag(t *testing.T) {
	st := newTestCLI(t)
	plantV1LogCLI(t, st.store, "ATM")
	if _, err := st.store.UpgradeProjectToV2("ATM"); err != nil {
		t.Fatal(err)
	}
	_, _, _ = runArgs(st, "project", "create", "--code", "BVX", "--name", "y", "--actor", "admin@cli:unset")
	out := runArgsOut(t, st, "store", "prune-v1", "--all")
	mustContain(t, out, "pruned\tATM\tarchived")
	mustContain(t, out, "skipped\tBVX\tborn v2 (no v1 log)")
}

// TestStorePruneV1AllReportsSuccessesBeforeFailing pins the fix to the
// mid-batch partial-progress bug: `--all` used to `return err` on the first
// per-project failure, discarding the reports for projects it had already
// (durably) pruned earlier in the same loop. "AAA" sorts before "ZZZ"
// (ProjectCodes is sorted), so AAA is pruned successfully before the loop
// reaches ZZZ, whose corrupted v2 event file makes PruneProjectV1 refuse.
// AAA's success must still be reported even though the command as a whole
// fails.
func TestStorePruneV1AllReportsSuccessesBeforeFailing(t *testing.T) {
	st := newTestCLI(t)
	plantV1LogCLI(t, st.store, "AAA")
	if _, err := st.store.UpgradeProjectToV2("AAA"); err != nil {
		t.Fatal(err)
	}
	plantV1LogCLI(t, st.store, "ZZZ")
	if _, err := st.store.UpgradeProjectToV2("ZZZ"); err != nil {
		t.Fatal(err)
	}
	// Corrupt ZZZ's v2 event file so VerifyProject reports Diverged, which
	// PruneProjectV1 refuses to prune past (see prune.go's verify-clean guard).
	zzzEvents := filepath.Join(st.store.StorePath(), "projects", "ZZZ", "events.v2.jsonl")
	if err := os.WriteFile(zzzEvents, []byte("{not-json}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, code := runArgs(st, "store", "prune-v1", "--all")
	if code == ExitSuccess {
		t.Fatalf("expected a non-zero exit for ZZZ's verify failure, got success:\n%s", out)
	}
	mustContain(t, out, "pruned\tAAA\tarchived")
}

func TestStorePruneV1RequiresExactlyOneSelector(t *testing.T) {
	st := newTestCLI(t)
	_, _, code := runArgs(st, "store", "prune-v1")
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage with neither --project nor --all, got exit=%d", code)
	}
}

func TestCommentShowAcceptsV2HashAliases(t *testing.T) {
	// Regression for the cli/comment.go project-code derivation: the relaxed
	// ParseCommentID (Task 2b) must yield the code for a v2 comment alias, or
	// `task comment show` dies before reaching the store — and the store's own
	// v2 read path must then answer a hash alias by id.
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	tk, err := st.store.CreateTask("ATM", "hash task", "", nil, "admin@cli:unset")
	if err != nil {
		t.Fatal(err)
	}
	c, err := st.store.CreateComment(tk.ID, "hash comment body", nil, "", "admin@cli:unset")
	if err != nil {
		t.Fatal(err)
	}
	out := runArgsStdoutOut(t, st, "task", "comment", "show", "--id", c.ID)
	mustContain(t, out, c.ID)
	mustContain(t, out, "hash comment body")
}

func TestStoreRemoteAddListRemoveRoundTrip(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	out := runArgsOut(t, st, "store", "remote", "add", "origin", "https://example.com/atm.git", "--project", "ATM", "--actor", "admin@cli:unset")
	mustContain(t, out, "origin")

	out = runArgsOut(t, st, "store", "remote", "list", "--project", "ATM")
	if out != "origin\thttps://example.com/atm.git\n" {
		t.Fatalf("unexpected list output: %q", out)
	}

	out = runArgsOut(t, st, "store", "remote", "remove", "origin", "--project", "ATM", "--actor", "admin@cli:unset")
	mustContain(t, out, "origin")

	out = runArgsOut(t, st, "store", "remote", "list", "--project", "ATM")
	if out != "" {
		t.Fatalf("expected empty list after remove, got %q", out)
	}
}

func TestStoreRemoteAddRequiresProject(t *testing.T) {
	st := newTestCLI(t)
	_, _, code := runArgs(st, "store", "remote", "add", "origin", "https://example.com/atm.git", "--actor", "admin@cli:unset")
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage without --project, got %d", code)
	}
}

func TestStoreRemoteRemoveRequiresProject(t *testing.T) {
	st := newTestCLI(t)
	_, _, code := runArgs(st, "store", "remote", "remove", "origin", "--actor", "admin@cli:unset")
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage without --project, got %d", code)
	}
}

func TestStoreRemoteRemoveUnknownNotFound(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, code := runArgs(st, "store", "remote", "remove", "nope", "--project", "ATM", "--actor", "admin@cli:unset")
	if code != ExitNotFound {
		t.Fatalf("expected ExitNotFound removing unknown remote, got %d", code)
	}
}

func TestStoreRemoteListJSON(t *testing.T) {
	st := newTestCLI(t)
	st.output = outputJSON
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, _ = runArgs(st, "store", "remote", "add", "origin", "https://example.com/atm.git", "--project", "ATM", "--actor", "admin@cli:unset")
	out := runArgsOut(t, st, "store", "remote", "list", "--project", "ATM")
	mustContain(t, out, `"project": "ATM"`)
	mustContain(t, out, `"name": "origin"`)
	mustContain(t, out, `"url": "https://example.com/atm.git"`)
}

func TestStoreRemoteListAllProjects(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, _ = runArgs(st, "project", "create", "--code", "BVX", "--name", "y", "--actor", "admin@cli:unset")
	_, _, _ = runArgs(st, "store", "remote", "add", "origin", "https://example.com/atm.git", "--project", "ATM", "--actor", "admin@cli:unset")
	out := runArgsOut(t, st, "store", "remote", "list")
	if out != "ATM\torigin\thttps://example.com/atm.git\n" {
		t.Fatalf("unexpected all-projects list output: %q", out)
	}
}
