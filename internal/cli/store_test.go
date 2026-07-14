package cli

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"atm/internal/store"

	_ "modernc.org/sqlite"
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

func TestStoreVerifyExitsNonzeroOnDivergence(t *testing.T) {
	st := newTestCLI(t)
	_, _ = st.store.CreateProject("ATM", "x", "admin@cli:unset")
	tk, _ := st.store.CreateTask("ATM", "t", "", nil, "admin@cli:unset")
	_ = st.store.SetTitle(tk.ID, "changed", "admin@cli:unset")
	// Stomp the task cache row back to seq 1 (stale) so verify detects divergence.
	db, err := sql.Open("sqlite", filepath.Join(st.store.StorePath(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE tasks SET log_seq = 1 WHERE id = ?`, tk.ID); err != nil {
		t.Fatal(err)
	}
	_, _, code := runArgs(st, "store", "verify")
	if code != 5 {
		t.Fatalf("store verify exit code = %d, want 5 (integrity divergence)", code)
	}
}

func TestStoreUpgradeProjectAndRollback(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	out := runArgsOut(t, st, "store", "upgrade", "--project", "ATM")
	mustContain(t, out, "upgraded\tATM\tv2")
	out = runArgsOut(t, st, "store", "verify", "ATM")
	mustContain(t, out, "format: v2")
	out = runArgsOut(t, st, "store", "rollback", "--project", "ATM", "--to", "v1")
	mustContain(t, out, "rolled back\tATM\tv1")
}

func TestStoreUpgradeAll(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, _ = runArgs(st, "project", "create", "--code", "DOC", "--name", "docs", "--actor", "admin@cli:unset")
	out := runArgsOut(t, st, "store", "upgrade", "--all")
	mustContain(t, out, "upgraded\tATM\tv2")
	mustContain(t, out, "upgraded\tDOC\tv2")
	mustContain(t, out, "active format: v2")
}

func TestStoreSetFormat(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	// v2 refused while ATM lacks an explicit entry (legacy v1 project).
	// runArgs returns (stdout, stderr, exit code) — assert on the exit code.
	_, stderr, code := runArgs(st, "store", "set-format", "--format", "v2")
	if code == ExitSuccess {
		t.Fatalf("set-format v2 must refuse with entry-less projects; stderr=%s", stderr)
	}
	_ = runArgsOut(t, st, "store", "upgrade", "--all")
	out := runArgsOut(t, st, "store", "set-format", "--format", "v1")
	mustContain(t, out, "active format: v1")
	out = runArgsOut(t, st, "store", "set-format", "--format", "v2")
	mustContain(t, out, "active format: v2")
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
