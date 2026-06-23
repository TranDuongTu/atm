package cli

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"atm/internal/store"
)

var updateGolden = flag.Bool("update", false, "regenerate golden fixtures")

var tsRe = regexp.MustCompile(`"2\d{3}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z"`)

func normalizeOutput(s string) string {
	out := tsRe.ReplaceAllString(s, `"TIMESTAMP"`)
	out = regexp.MustCompile(`"/[^"]*atm[^"]*/projects"`).ReplaceAllString(out, `"/STORE/projects"`)
	return out
}

type goldenHarness struct {
	t      *testing.T
	st     *cliState
	store  *store.Store
	stdout *bytes.Buffer
	stderr *bytes.Buffer
}

func newGoldenHarness(t *testing.T) *goldenHarness {
	t.Helper()
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
	return &goldenHarness{t: t, st: st, store: s, stdout: buf, stderr: ebuf}
}

func (h *goldenHarness) reset() {
	h.stdout.Reset()
	h.stderr.Reset()
}

func (h *goldenHarness) run(args ...string) (string, string, int) {
	h.reset()
	root := newRootCmdWithState(h.st)
	root.SilenceUsage = true
	root.SilenceErrors = true
	h.st.flags.store = h.store.StorePath()
	h.st.flags.output = outputJSON
	root.SetArgs(args)
	err := root.Execute()
	code := ExitSuccess
	if err != nil {
		code = ExitCodeForError(err)
	}
	return h.stdout.String(), h.stderr.String(), code
}

func (h *goldenHarness) seedScenario1() {
	h.t.Helper()
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "human:alice")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "Agent Tasks Management",
		"--label", "type:epic", "--label", "type:user-story", "--label", "type:impl",
		"--label", "type:bug", "--label", "area:cli", "--label", "area:tui",
		"--label", "kind:convention", "--type-axis", "type", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "PR conventions for bug fixes",
		"--label", "kind:convention", "--label", "type:bug", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Fix claim race",
		"--label", "type:bug", "--label", "area:cli", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Blocked subtask",
		"--label", "type:impl", "--actor", "human:alice")
	h.run("task", "link", "add", "--store", sp, "--id", "ATM-0002", "--type", "blocks", "--target", "ATM-0003",
		"--actor", "human:alice")
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

func TestGoldenNextEmpty(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("init", "--store", h.store.StorePath(), "--actor", "human:alice")
	h.run("project", "create", "--code", "ATM", "--name", "A",
		"--label", "type:impl", "--type-axis", "type", "--actor", "human:alice")
	out, _, code := h.run("task", "next", "--project", "ATM")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	compareGolden(t, "next-empty", out)
	if !strings.Contains(out, `"task": null`) {
		t.Fatalf("expected task null, got %s", out)
	}
	if !strings.Contains(out, `"guide": null`) {
		t.Fatalf("expected guide null, got %s", out)
	}
}

func TestGoldenNextClaimThenNext(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()

	out1, _, code := h.run("task", "next", "--project", "ATM", "--claim", "--actor", "agent:claude-1")
	if code != 0 {
		t.Fatalf("first claim exit = %d", code)
	}
	compareGolden(t, "next-claim-1", out1)
	if !strings.Contains(out1, `"id": "ATM-0002"`) {
		t.Fatalf("expected ATM-0002, got %s", out1)
	}
	if !strings.Contains(out1, `"actor": "agent:claude-1"`) {
		t.Fatalf("expected claim by agent:claude-1, got %s", out1)
	}

	out2, _, code := h.run("task", "next", "--project", "ATM", "--claim", "--actor", "agent:claude-2")
	if code != 0 {
		t.Fatalf("second claim exit = %d", code)
	}
	compareGolden(t, "next-claim-2", out2)
	if strings.Contains(out2, `"id": "ATM-0001"`) {
		t.Fatalf("ATM-0001 is a convention doc and must never be returned by next: %s", out2)
	}

	out3, _, code := h.run("task", "next", "--project", "ATM")
	if code != 0 {
		t.Fatalf("third next exit = %d", code)
	}
	compareGolden(t, "next-claim-3", out3)
	if !strings.Contains(out3, `"task": null`) {
		t.Fatalf("expected null after ATM-0002 claimed and ATM-0003 blocked, got %s", out3)
	}
}

func TestGoldenShowWithContext(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()

	out, _, code := h.run("task", "show", "--id", "ATM-0002", "--with-context")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "show-with-context", out)

	if !strings.Contains(out, `"guide": null`) {
		t.Fatalf("expected guide null, got %s", out)
	}
	if !strings.Contains(out, `"id": "ATM-0001"`) {
		t.Fatalf("expected conventions to contain ATM-0001, got %s", out)
	}
	if !strings.Contains(out, `"matched_labels": [`) {
		t.Fatalf("expected matched_labels, got %s", out)
	}
}

func TestGoldenScenario1Integration(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()

	out, _, code := h.run("task", "next", "--project", "ATM")
	if code != 0 {
		t.Fatalf("next exit = %d", code)
	}
	if !strings.Contains(out, `"id": "ATM-0002"`) {
		t.Fatalf("Scenario 1: next should return ATM-0002, got %s", out)
	}

	outClaim, _, code := h.run("task", "next", "--project", "ATM", "--claim", "--actor", "agent:claude-1")
	if code != 0 {
		t.Fatalf("next --claim exit = %d", code)
	}
	if !strings.Contains(outClaim, `"id": "ATM-0002"`) {
		t.Fatalf("Scenario 1: next --claim should return ATM-0002, got %s", outClaim)
	}
	if !strings.Contains(outClaim, `"actor": "agent:claude-1"`) {
		t.Fatalf("Scenario 1: claim actor wrong, got %s", outClaim)
	}

	outCtx, _, code := h.run("task", "show", "--id", "ATM-0002", "--with-context")
	if code != 0 {
		t.Fatalf("show --with-context exit = %d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(outCtx, `"id": "ATM-0001"`) {
		t.Fatalf("Scenario 1: context.conventions should contain ATM-0001, got %s", outCtx)
	}
	compareGolden(t, "scenario1-show-with-context", outCtx)
}

func TestGoldenDeterminism(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	out1, _, _ := h.run("task", "list", "--project", "ATM")
	out2, _, _ := h.run("task", "list", "--project", "ATM")
	if normalizeOutput(out1) != normalizeOutput(out2) {
		t.Fatal("task list not deterministic across runs")
	}
}

func TestExitCodesNotFound(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("init", "--store", h.store.StorePath(), "--actor", "human:alice")
	h.run("project", "create", "--code", "ATM", "--name", "A", "--label", "type:impl", "--type-axis", "type", "--actor", "human:alice")
	_, _, code := h.run("task", "show", "--id", "ATM-9999")
	if code != ExitNotFound {
		t.Fatalf("expected exit %d, got %d", ExitNotFound, code)
	}
}

func TestExitCodesConflictClaim(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("init", "--store", h.store.StorePath(), "--actor", "human:alice")
	h.run("project", "create", "--code", "ATM", "--name", "A", "--label", "type:impl", "--type-axis", "type", "--actor", "human:alice")
	h.run("task", "create", "--project", "ATM", "--title", "t", "--label", "type:impl", "--actor", "human:alice")
	h.run("task", "claim", "--id", "ATM-0001", "--actor", "agent:claude-1")
	_, _, code := h.run("task", "claim", "--id", "ATM-0001", "--actor", "agent:claude-2")
	if code != ExitConflict {
		t.Fatalf("expected exit %d, got %d", ExitConflict, code)
	}
}

func TestStorePathCommand(t *testing.T) {
	h := newGoldenHarness(t)
	out, _, code := h.run("store", "path")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out, h.store.StorePath()) {
		t.Fatalf("store path = %s want %s", out, h.store.StorePath())
	}
}

func TestVersionCommand(t *testing.T) {
	h := newGoldenHarness(t)
	h.st.flags.output = outputText
	out, _, code := h.run("version")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out, "atm version dev") {
		t.Fatalf("version output = %s", out)
	}
}

func TestSortedKeysSanity(t *testing.T) {
	keys := []string{"task", "guide", "context", "links_out"}
	want := append([]string(nil), keys...)
	sort.Strings(want)
	got := append([]string(nil), keys...)
	sort.Strings(got)
	if fmt.Sprint(want) != fmt.Sprint(got) {
		t.Fatal("sort sanity")
	}
}
