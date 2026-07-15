package cli

import (
	"bytes"
	"io"
	"os"
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
