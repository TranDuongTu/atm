package cli

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"atm/internal/store"
)

var updateGolden = flag.Bool("update", false, "regenerate golden fixtures")

var tsRe = regexp.MustCompile(`"2\d{3}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z"`)

var storePathRe = regexp.MustCompile(`"/[^"]*/projects"`)

func normalizeOutput(s string) string {
	out := tsRe.ReplaceAllString(s, `"TIMESTAMP"`)
	out = storePathRe.ReplaceAllString(out, `"/STORE/projects"`)
	return out
}

type goldenHarness struct {
	t      *testing.T
	st     *cliState
	store  *store.Store
	stdout *bytes.Buffer
	stderr *bytes.Buffer
	output string
}

func newGoldenHarness(t *testing.T) *goldenHarness {
	t.Helper()
	for _, k := range []string{
		"ATM_ACTOR", "ATM_ROLE", "ATM_PROJECT", "ATM_BIN", "ATM_RUN_ID", "ATM_CONTEXT_FILE", "ATM_AGENT",
		"ATM_OPENCODE_ARGS", "ATM_CODEX_ARGS", "ATM_CLAUDE_ARGS", "ATM_OLLAMA_ARGS",
	} {
		t.Setenv(k, "")
	}
	dir := t.TempDir()
	st := &cliState{flags: globalFlags{output: outputJSON}}
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
	st.flags.store = s.StorePath()
	return &goldenHarness{t: t, st: st, store: s, stdout: buf, stderr: ebuf, output: outputJSON}
}

func newGoldenHarnessAt(t *testing.T, storePath string) *goldenHarness {
	t.Helper()
	for _, k := range []string{
		"ATM_ACTOR", "ATM_ROLE", "ATM_PROJECT", "ATM_BIN", "ATM_RUN_ID", "ATM_CONTEXT_FILE", "ATM_AGENT",
		"ATM_OPENCODE_ARGS", "ATM_CODEX_ARGS", "ATM_CLAUDE_ARGS", "ATM_OLLAMA_ARGS",
	} {
		t.Setenv(k, "")
	}
	st := &cliState{flags: globalFlags{output: outputJSON}}
	buf := &bytes.Buffer{}
	ebuf := &bytes.Buffer{}
	st.out = buf
	st.err = ebuf
	s, err := store.Open(storePath)
	if err != nil {
		t.Fatal(err)
	}
	st.flags.store = s.StorePath()
	return &goldenHarness{t: t, st: st, store: s, stdout: buf, stderr: ebuf, output: outputJSON}
}

func (h *goldenHarness) reset() {
	h.stdout.Reset()
	h.stderr.Reset()
}

func (h *goldenHarness) run(args ...string) (string, string, int) {
	h.reset()
	h.st.flags.actor = ""
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
		// Mirror production Execute(): write the error envelope to stderr
		// in JSON mode so error-case goldens capture the envelope shape.
		if h.output == outputJSON {
			env := NewErrorEnvelopeFromError(err)
			fmt.Fprintln(h.stderr, env.String())
		}
	}
	return h.stdout.String(), h.stderr.String(), code
}

func (h *goldenHarness) seedScenario1() {
	h.t.Helper()
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "admin@cli:unset")
	h.run("label", "add", "--store", sp, "--name", "ATM:type:epic", "--actor", "admin@cli:unset")
	h.run("label", "add", "--store", sp, "--name", "ATM:type:bug", "--description", "Bug fix", "--actor", "admin@cli:unset")
	h.run("label", "add", "--store", sp, "--name", "ATM:status:open", "--actor", "admin@cli:unset")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Fix label reconciliation",
		"--label", "ATM:type:bug", "--label", "ATM:status:open", "--actor", "admin@cli:unset")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Seed index tasks",
		"--label", "ATM:context:agent", "--actor", "admin@cli:unset")
	h.reset()
}

func compareGolden(t *testing.T, name string, got string) {
	t.Helper()
	got = normalizeOutput(got)
	path := filepath.Join("testdata", "golden", name+".json")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s: %v (run with -update to create)", path, err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch %s:\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}
