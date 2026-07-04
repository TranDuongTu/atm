package cli

import (
	"bytes"
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

func TestStoreLogText(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "c")
	_, _, _ = runArgs(st, "task", "create", "--project", "ATM", "--title", "t", "--actor", "c")
	out := runArgsOut(t, st, "store", "log", "ATM")
	mustContain(t, out, "task.created")
	mustContain(t, out, "project.created")
}

func TestStoreLogJSON(t *testing.T) {
	st := newTestCLI(t)
	st.output = outputJSON
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "c")
	out := runArgsOut(t, st, "store", "log", "ATM")
	mustContain(t, out, `"action": "project.created"`)
}

func TestStoreVerifyClean(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "c")
	out := runArgsOut(t, st, "store", "verify", "ATM")
	mustContain(t, out, "ok")
}

func TestStoreRebuild(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "c")
	out := runArgsOut(t, st, "store", "rebuild")
	mustContain(t, out, "projects")
}
