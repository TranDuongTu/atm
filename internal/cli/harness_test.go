package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"atm/internal/capability"
	"atm/internal/capability/contextmap"
	"atm/internal/capability/workflow"
	"atm/internal/core"
	"atm/internal/store"
)

// deterministicSeam returns a fixed byte stream. A born-v2 store mints more
// than one replica id (StoreInstanceID + ReplicaID, each 16 bytes via
// io.ReadFull), so a finite 16-byte reader would exhaust after the first mint
// and the second would draw non-reproducible bytes. This reader is INFINITE and
// reproducible: a fresh instance (one per store.Open) always yields the same
// deterministic sequence, so two seeded stores mint identical ids and goldens
// are byte-stable across processes. The counter advances so consecutive 16-byte
// mints differ (StoreInstanceID != ReplicaID), matching production's shape.
type deterministicSeam struct{ b byte }

func (d *deterministicSeam) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = d.b
		d.b++
	}
	return len(p), nil
}

// deterministicSeamOpts returns the fixed determinism seams (clock, replica
// entropy, and now) used by the golden harness so that when goldens are
// regenerated for v2 they are reproducible. Call it fresh per store.Open since
// the entropy reader carries per-store consumption state.
func deterministicSeamOpts() []store.Option {
	var n int64 = 1_752_480_000_000
	return []store.Option{
		store.WithClock(func() int64 { n++; return n }),
		store.WithReplicaEntropy(&deterministicSeam{}),
		store.WithNow(func() time.Time { return time.Date(2026, 7, 14, 9, 12, 3, 0, time.UTC) }),
	}
}

// storeOpeners builds the injected OpenService/OpenAdmin constructors the test
// harnesses wire into cliState, standing in for cmd/atm's composition root. Both
// share the one determinism seam set (opts) so the v2 events authored INSIDE
// command execution (openStore/openAdmin are called per command, not on the
// harness's own handle) mint reproducible hex aliases. Each returns an explicit
// nil on error so a typed-nil *store.Store never hides in the interface.
func storeOpeners(opts ...store.Option) (func(string) (core.Service, error), func(string) (core.StorageAdmin, error)) {
	openService := func(p string) (core.Service, error) {
		s, err := store.Open(store.ResolveStorePath(p), opts...)
		if err != nil {
			return nil, err
		}
		return s, nil
	}
	openAdmin := func(p string) (core.StorageAdmin, error) {
		s, err := store.Open(store.ResolveStorePath(p), opts...)
		if err != nil {
			return nil, err
		}
		return s, nil
	}
	return openService, openAdmin
}

var updateGolden = flag.Bool("update", false, "regenerate golden fixtures")

// testRegistry mirrors cmd/atm's production registry so golden tests
// exercise the same command surface the binary ships. Keep the two in sync:
// a capability registered in main.go but not here is invisible to goldens.
func testRegistry() *capability.Registry {
	return capability.NewRegistry(workflow.New(), contextmap.New())
}

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
	// openService is the harness's seeded service opener (from storeOpeners).
	// run() reuses it to build the mount's OpenService so pre-parse project
	// resolution reads the SAME seeded store the commands write to.
	openService func(string) (core.Service, error)
}

// mountDeps builds the Deps the per-run mount consumes. Its OpenService
// resolves an empty --store path to the harness's real temp store: most
// harness runs carry no --store arg, and a plain open("") would resolve the
// default home, fail GetProject, and degrade-open (mount everything) — masking
// a correct gate. Registry is a FRESH full registry so every run re-narrows
// from the full set, never from an already-narrowed h.st.registry.
func (h *goldenHarness) mountDeps() Deps {
	return Deps{
		Registry: testRegistry(),
		OpenService: func(p string) (core.Service, error) {
			if p == "" {
				p = h.store.StorePath()
			}
			return h.openService(p)
		},
	}
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
	// One shared seam set threaded through BOTH the harness's own handle and
	// every store the CLI commands open (via the injected openers): v2 events
	// are authored inside command execution through openStore/openAdmin, so the
	// seams must reach there or the minted hex aliases draw from the real wall
	// clock / crypto-rand and are not reproducible.
	opts := deterministicSeamOpts()
	openService, openAdmin := storeOpeners(opts...)
	st := &cliState{flags: globalFlags{output: outputJSON}, registry: testRegistry(), fullRegistry: testRegistry(), openServiceFn: openService, openAdminFn: openAdmin}
	buf := &bytes.Buffer{}
	ebuf := &bytes.Buffer{}
	st.out = buf
	st.err = ebuf
	s, err := store.Open(dir, opts...)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(""); err != nil {
		t.Fatal(err)
	}
	st.flags.store = s.StorePath()
	return &goldenHarness{t: t, st: st, store: s, stdout: buf, stderr: ebuf, output: outputJSON, openService: openService}
}

func newGoldenHarnessAt(t *testing.T, storePath string) *goldenHarness {
	t.Helper()
	for _, k := range []string{
		"ATM_ACTOR", "ATM_ROLE", "ATM_PROJECT", "ATM_BIN", "ATM_RUN_ID", "ATM_CONTEXT_FILE", "ATM_AGENT",
		"ATM_OPENCODE_ARGS", "ATM_CODEX_ARGS", "ATM_CLAUDE_ARGS", "ATM_OLLAMA_ARGS",
	} {
		t.Setenv(k, "")
	}
	opts := deterministicSeamOpts()
	openService, openAdmin := storeOpeners(opts...)
	st := &cliState{flags: globalFlags{output: outputJSON}, registry: testRegistry(), fullRegistry: testRegistry(), openServiceFn: openService, openAdminFn: openAdmin}
	buf := &bytes.Buffer{}
	ebuf := &bytes.Buffer{}
	st.out = buf
	st.err = ebuf
	s, err := store.Open(storePath, opts...)
	if err != nil {
		t.Fatal(err)
	}
	st.flags.store = s.StorePath()
	return &goldenHarness{t: t, st: st, store: s, stdout: buf, stderr: ebuf, output: outputJSON, openService: openService}
}

func (h *goldenHarness) reset() {
	h.stdout.Reset()
	h.stderr.Reset()
}

func (h *goldenHarness) run(args ...string) (string, string, int) {
	h.reset()
	h.st.flags.actor = ""
	// Route every run through the SAME pre-parse mount path production uses.
	// Re-narrow from a fresh full registry (deps.Registry); the env getter
	// returns "" so goldens never see the invoking shell's ATM_PROJECT.
	// fullRegistry keeps the un-narrowed set for capability-management commands.
	deps := h.mountDeps()
	h.st.fullRegistry = deps.Registry
	h.st.registry = mountRegistry(deps, args, func(string) string { return "" })
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

// seedScenario1 seeds the standard golden-test fixture: project ATM, three
// labels, and two tasks. It returns the two created tasks' ids (in creation
// order) so callers reference them dynamically instead of hardcoding the v1
// sequential form ("ATM-0001"/"ATM-0002") — the ids stay whatever the store
// actually minted, v1 today and v2 hex after the born-v2 cutover.
func (h *goldenHarness) seedScenario1() (task1ID, task2ID string) {
	h.t.Helper()
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "admin@cli:unset")
	h.run("label", "add", "--store", sp, "--name", "ATM:type:epic", "--actor", "admin@cli:unset")
	h.run("label", "add", "--store", sp, "--name", "ATM:type:bug", "--description", "Bug fix", "--actor", "admin@cli:unset")
	h.run("label", "add", "--store", sp, "--name", "ATM:status:open", "--actor", "admin@cli:unset")
	out1, _, _ := h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Fix label reconciliation",
		"--label", "ATM:type:bug", "--label", "ATM:status:open", "--actor", "admin@cli:unset")
	out2, _, _ := h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Seed index tasks",
		"--label", "ATM:context:agent", "--actor", "admin@cli:unset")
	h.reset()
	return taskIDFromCreateJSON(h.t, out1), taskIDFromCreateJSON(h.t, out2)
}

// taskIDFromCreateJSON extracts the "task.id" field from a `task create` JSON
// envelope, so tests can capture a just-created task's id instead of
// hardcoding it.
func taskIDFromCreateJSON(t *testing.T, out string) string {
	t.Helper()
	var env struct {
		Task struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("parse task id from create output %q: %v", out, err)
	}
	if env.Task.ID == "" {
		t.Fatalf("no task id in create output: %q", out)
	}
	return env.Task.ID
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
