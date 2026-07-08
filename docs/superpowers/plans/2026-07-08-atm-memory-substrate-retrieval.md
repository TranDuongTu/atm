# ATM as Memory Substrate: Retrieval Surface + Indexing Model + Manager Cognition Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the retrieve side of ATM-as-memory-substrate by adding a direct CLI semantic search (`atm search`) over per-project per-model vector indexes, a manager indexing runtime mode (`atm manager <host> --index`) that uses the host's embedding capability, a manager inquiry capability (`hint: question`), and a recall-measurement loop (`atm eval`) — without adding new memory kinds and without ATM ever calling a model.

**Architecture:** Vectors and eval artifacts are per-project derived files under `$ATM_HOME/projects/<CODE>/vectors/<model-slug>.jsonl` and `$ATM_HOME/projects/<CODE>/eval/`, decoupled from `cache.db`. The host agent (manager session or developing agent's host) computes embeddings; ATM stores vectors via a batched CLI verb and runs cosine search + text fallback in pure Go. A new `--index` flag on `atm manager` sets `ATM_INDEX=1` (parallel to `--onboard`/`ATM_ONBOARD=1`) to activate an indexing responsibility section in the one manager prompt. Recall measurement uses an eval set + `atm eval run` computing recall@k/precision@k offline.

**Tech Stack:** Go 1.22+, cobra CLI, existing `internal/store` (per-project files, `WithLock`, `WriteFileAtomic`, `ReadJSON`, `LastLogSeq`), existing `internal/manager` (`RenderContext`, `Launcher`, `BuildArgv`/`BuildArgvOnboard`), existing `internal/cli` (`cliState`, `emit`, `resolveActor`, golden harness).

## Global Constraints

- Go module path is `atm` (imports are `atm/internal/...`).
- Store resolution: `--store` > `ATM_HOME` > `~/.config/atm`. Mutating CLI commands require `--actor` or `ATM_ACTOR`. JSON output is deterministic (sorted keys, RFC3339 UTC) via `store.MarshalSorted`/`WriteFileAtomic`.
- Per-project file locking via `s.WithLock(code, fn)`. Vector/eval files live under `s.projectDir(code)` (`$ATM_HOME/projects/<CODE>/`), NOT under `personas/` or `cache.db`.
- Vectors and eval artifacts are derived (like `vocabulary.json`): written via store helpers (lock-guarded, atomic), NOT appended to `log.jsonl`. No new log action, no `Replay` change, no entity schema change.
- ATM the binary NEVER calls a model. Embeddings come from the host agent's embedding capability; the host passes vectors to ATM via CLI flags. Search reads local vector files in pure Go.
- Per-project, per-model vector files: `vectors/<model-slug>.jsonl`. A host only ever searches its own model's index (space-mismatch can't happen). Model-slug = host's embedding model name, lowercased, non-alphanumeric chars replaced with hyphens (e.g. `nomic-embed-text`, `text-embedding-3-small`).
- `--index` and `--onboard` are mutually exclusive on `atm manager`. `--index` sets `ATM_INDEX=1` and uses the interactive `BuildArgv` (NOT `BuildArgvOnboard`).
- No emojis in code or commits. No comments unless asked. Follow neighboring-file style.
- Manager prompt substitution: `RenderContext` substitutes `<CODE>`,`<PROJECT_NAME>`,`<ATM_BIN>`,`<ACTOR>`,`<RUN_ID>`,`<TIMESTAMP>`; empty values leave the placeholder in place.
- The manager prompt gates indexing on `ATM_INDEX` (set by the `--index` launcher flag), NOT on `ATM_ROLE`. Inquiry/evaluation/write-side-representation sections are mode-agnostic (present in all renders).
- Test harness: `newGoldenHarness(t)` / `newGoldenHarnessAt(t, storePath)` in `internal/cli/harness_test.go`; `compareGolden(t, name, got)` with `testdata/golden/<name>.json` (regenerate with `-update`). `normalizeOutput`, `normalizeHome` exist. Store tests use temp dirs via `t.TempDir()` + `store.Open`.
- Run `make build && make test` (a.k.a. `make verify`) green before each commit. The gate is `make verify`.

---

## File Structure

- `internal/store/vectors.go` (new) — `VectorEntry`/`VectorMeta` types, path helpers, `ReadVectors`, `WriteVectorBatch`, `VectorMeta`, `DropVectors`, `ListVectorModels`.
- `internal/store/vectors_test.go` (new) — round-trip, append, stale detection, drop, list, lock/mkdir, model-mismatch/dim-mismatch rejection.
- `internal/store/eval.go` (new) — `EvalEntry`/`EvalRun`/`EvalQueryResult` types, `ReadEvalSet`, `AppendEvalEntries`, `RecordEvalRun`, `ListEvalRuns`, `GetEvalRun`.
- `internal/store/eval_test.go` (new) — append, run record write/list/get, skipped entries.
- `internal/store/search.go` (new) — `Hit`/`SearchParams` types, `Search` (cosine + text fallback), `cosineSimilarity`, `textSearch`.
- `internal/store/search_test.go` (new) — cosine ranking, kind filter, stale skip, threshold→fallback, text fallback ranking, empty index→fallback, dim-mismatch rejection.
- `internal/store/store.go` (modify) — add `vectorsDir`, `vectorPath`, `vectorMetaPath`, `evalDir`, `evalSetPath`, `evalRunPath` helpers.
- `internal/store/rebuild.go` (modify) — drop `vectors/` and `eval/` dirs per project in `Rebuild`.
- `internal/store/verify.go` (modify) — info-level `VectorIndex`/`EvalSet` entries in `VerifyReport` (absence is not an error).
- `internal/cli/search.go` (new) — `atm search` command: `--model`, `--query-vector`, `--kind`, `--k`, `--threshold`, positional query text.
- `internal/cli/search_test.go` (new) — golden text+JSON for semantic, text-fallback, empty, kind filter, k; missing `--query-vector`→ErrUsage; dim-mismatch→ErrUsage.
- `internal/cli/index.go` (new) — `atm index` command tree: `write-batch`, `status`, `drop`, `models`.
- `internal/cli/index_test.go` (new) — golden for each verb; model-mismatch/dim-mismatch rejection; `--actor` required on mutating verbs.
- `internal/cli/eval.go` (new) — `atm eval` command tree: `write`, `run`, `runs`, `show`.
- `internal/cli/eval_test.go` (new) — golden for each verb; missing index→ErrNotFound; skipped entries reported.
- `internal/cli/root.go` (modify) — register `newSearchCmd`, `newIndexCmd`, `newEvalCmd`.
- `internal/cli/manager.go` (modify) — add `Index bool` to `managerOpts`; add `--index` flag to agent + ollama subcommands; mutual-exclusion with `--onboard`; set `ATM_INDEX=1` in env; `tmuxLabelIndex` window label.
- `internal/cli/manager_test.go` (modify) — `--index` dry-run golden for opencode + ollama; `ATM_INDEX=1` in env; `--index --onboard`→ErrUsage; non-index dry-run unchanged.
- `internal/cli/testdata/golden/manager-index-dry-run-opencode.json` (new).
- `internal/cli/testdata/golden/manager-index-dry-run-ollama.json` (new).
- `internal/cli/tmux.go` (modify) — add `tmuxLabelIndex` constant.
- `internal/manager/context_v1.md` (modify) — add Indexing responsibility (ATM_INDEX-gated), Inquiry responsibility, Evaluation responsibility, Write-side representation (folds into Ledger hygiene), and the new commands to the cheat sheet.
- `internal/manager/context_test.go` (modify) — assert indexing section present/absent on ATM_INDEX; assert inquiry/eval/representation sections present in all renders; assert new commands in cheat sheet.
- `internal/developing/context_v1.md` (modify) — add a Retrieval section documenting direct `atm search` + `hint: question` dispatch.
- `internal/developing/context_test.go` (modify) — assert Retrieval section present.
- `internal/cli/conventions.go` (modify) — document `ATM:comment:superseded` label convention; add search/index/eval commands to first-contact sequence.
- `internal/cli/conventions_test.go` (modify) — update golden for the new convention + commands.

---

## Task 1: store vector read/write helpers

**Files:**
- Create: `internal/store/vectors.go`
- Create: `internal/store/vectors_test.go`
- Modify: `internal/store/store.go` (add path helpers)

**Interfaces:**
- Consumes: `s.projectDir(code)`, `s.WithLock(code, fn)`, `WriteFileAtomic`, `ReadJSON`, `Now`, `RFC3339UTC`, `ErrUsage`, `ErrNotFound`, `IsNotFound`, `os.MkdirAll`.
- Produces: `VectorEntry` struct, `VectorMeta` struct, `ReadVectors(code, slug)`, `WriteVectorBatch(code, slug, entries, lastLogSeq)`, `VectorMeta(code, slug)`, `DropVectors(code, slug)`, `ListVectorModels(code)`, path helpers `vectorsDir`/`vectorPath`/`vectorMetaPath`.

- [ ] **Step 1: Write the failing test for path helpers + round-trip**

Create `internal/store/vectors_test.go`:

```go
package store

import (
	"path/filepath"
	"testing"
)

func TestVectorPaths(t *testing.T) {
	s := newTestStore(t)
	got := s.vectorPath("ATM", "nomic-embed-text")
	want := filepath.Join(s.projectDir("ATM"), "vectors", "nomic-embed-text.jsonl")
	if got != want {
		t.Errorf("vectorPath = %q, want %q", got, want)
	}
	got = s.vectorMetaPath("ATM", "nomic-embed-text")
	want = filepath.Join(s.projectDir("ATM"), "vectors", "nomic-embed-text.meta.json")
	if got != want {
		t.Errorf("vectorMetaPath = %q, want %q", got, want)
	}
}

func TestWriteVectorBatchRoundTrip(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "ATM")
	entries := []VectorEntry{
		{ID: "ATM-0001", Kind: "task", Model: "nomic-embed-text", Dim: 4, Vector: []float64{0.1, 0.2, 0.3, 0.4}, TextHash: "h1", LogSeq: 5},
		{ID: "ATM-0001-c0001", Kind: "comment", Model: "nomic-embed-text", Dim: 4, Vector: []float64{0.5, 0.6, 0.7, 0.8}, TextHash: "h2", LogSeq: 6},
	}
	if err := s.WriteVectorBatch("ATM", "nomic-embed-text", entries, 6); err != nil {
		t.Fatalf("WriteVectorBatch: %v", err)
	}
	got, err := s.ReadVectors("ATM", "nomic-embed-text")
	if err != nil {
		t.Fatalf("ReadVectors: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got[0].ID != "ATM-0001" || got[1].ID != "ATM-0001-c0001" {
		t.Errorf("entry order: %+v", got)
	}
	meta, err := s.VectorMeta("ATM", "nomic-embed-text")
	if err != nil {
		t.Fatalf("VectorMeta: %v", err)
	}
	if meta.LastLogSeq != 6 || meta.Count != 2 || meta.Dim != 4 {
		t.Errorf("meta = %+v, want last_log_seq=6 count=2 dim=4", meta)
	}
}
```

Add a helper at the top of the file (reuse the existing pattern from other store tests; if `newTestStore`/`seedProject` already exist in another `_test.go`, do not redefine — check first):

```go
// If these already exist in another _test.go in package store, do NOT redefine.
// Use the existing ones. Only add if missing.
```

(Check `internal/store/vocabulary_test.go` or `comment_test.go` for the existing `newTestStore`/`seedProject` helpers and reuse them. If they don't exist, add minimal versions to `vectors_test.go`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestVectorPaths -v`
Expected: FAIL — `s.vectorPath undefined` / `s.WriteVectorBatch undefined`.

- [ ] **Step 3: Implement path helpers in store.go**

Add to `internal/store/store.go` (after `lockPath`):

```go
func (s *Store) vectorsDir(code string) string  { return filepath.Join(s.projectDir(code), "vectors") }
func (s *Store) vectorPath(code, slug string) string {
	return filepath.Join(s.vectorsDir(code), slug+".jsonl")
}
func (s *Store) vectorMetaPath(code, slug string) string {
	return filepath.Join(s.vectorsDir(code), slug+".meta.json")
}
func (s *Store) evalDir(code string) string { return filepath.Join(s.projectDir(code), "eval") }
func (s *Store) evalSetPath(code string) string {
	return filepath.Join(s.evalDir(code), "evalset.jsonl")
}
func (s *Store) evalRunPath(code, slug, ts string) string {
	return filepath.Join(s.evalDir(code), "runs", slug, ts+".json")
}
```

- [ ] **Step 4: Implement vectors.go**

Create `internal/store/vectors.go`:

```go
package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type VectorEntry struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	Model    string    `json:"model"`
	Dim      int       `json:"dim"`
	Vector   []float64 `json:"vector"`
	TextHash string    `json:"text_hash"`
	LogSeq   int       `json:"log_seq"`
}

type VectorMeta struct {
	Model           string    `json:"model"`
	Dim             int       `json:"dim"`
	LastLogSeq      int       `json:"last_log_seq"`
	LastReindexedAt string    `json:"last_reindexed_at"`
	Count           int       `json:"count"`
}

// ReadVectors reads vectors/<slug>.jsonl. Malformed lines are skipped with no
// error (best-effort, mirroring log-line tolerance). A missing file returns
// (nil, nil).
func (s *Store) ReadVectors(code, slug string) ([]VectorEntry, error) {
	f, err := os.Open(s.vectorPath(code, slug))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []VectorEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e VectorEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		return out, err
	}
	return out, nil
}

// WriteVectorBatch appends entries to vectors/<slug>.jsonl and updates the
// meta file atomically under the project lock. Every entry's Model must match
// slug and every entry's Dim must equal the first entry's Dim; otherwise the
// whole batch is rejected (ErrUsage) with no partial write.
func (s *Store) WriteVectorBatch(code, slug string, entries []VectorEntry, lastLogSeq int) error {
	if len(entries) == 0 {
		return fmt.Errorf("%w: empty vector batch", ErrUsage)
	}
	dim := entries[0].Dim
	for i, e := range entries {
		if e.Model != slug {
			return fmt.Errorf("%w: entry %d model %q != batch model %q", ErrUsage, i, e.Model, slug)
		}
		if e.Dim != dim {
			return fmt.Errorf("%w: entry %d dim %d != batch dim %d", ErrUsage, i, e.Dim, dim)
		}
	}
	return s.WithLock(code, func() error {
		dir := s.vectorsDir(code)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		path := s.vectorPath(code, slug)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		for _, e := range entries {
			b, err := json.Marshal(e)
			if err != nil {
				f.Close()
				return err
			}
			if _, err := f.Write(append(b, '\n')); err != nil {
				f.Close()
				return err
			}
		}
		if err := f.Close(); err != nil {
			return err
		}
		count, err := countVectorLines(path)
		if err != nil {
			return err
		}
		meta := &VectorMeta{
			Model:           slug,
			Dim:             dim,
			LastLogSeq:      lastLogSeq,
			LastReindexedAt: RFC3339UTC(Now()),
			Count:           count,
		}
		return WriteFileAtomic(s.vectorMetaPath(code, slug), meta)
	})
}

func countVectorLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	n := 0
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) != "" {
			n++
		}
	}
	return n, sc.Err()
}

// VectorMeta reads the meta file. Missing file -> (nil, nil).
func (s *Store) VectorMeta(code, slug string) (*VectorMeta, error) {
	var m VectorMeta
	if err := ReadJSON(s.vectorMetaPath(code, slug), &m); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &m, nil
}

// DropVectors removes one model's vector file + meta. Missing -> ErrNotFound.
func (s *Store) DropVectors(code, slug string) error {
	return s.WithLock(code, func() error {
		var hadErr error
		if err := os.Remove(s.vectorPath(code, slug)); err != nil && !os.IsNotExist(err) {
			hadErr = err
		} else if os.IsNotExist(err) {
			return fmt.Errorf("%w: no vector index for model %q", ErrNotFound, slug)
		}
		_ = os.Remove(s.vectorMetaPath(code, slug))
		return hadErr
	})
}

// ListVectorModels returns the model slugs that have a vector file for the
// project, sorted.
func (s *Store) ListVectorModels(code string) ([]string, error) {
	entries, err := os.ReadDir(s.vectorsDir(code))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		out = append(out, strings.TrimSuffix(name, ".jsonl"))
	}
	sort.Strings(out)
	return out, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestVectorPaths|TestWriteVectorBatchRoundTrip' -v`
Expected: PASS.

- [ ] **Step 6: Write failing tests for append, stale, drop, list, mismatch**

Append to `internal/store/vectors_test.go`:

```go
func TestWriteVectorBatchAppends(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "ATM")
	first := []VectorEntry{{ID: "ATM-0001", Kind: "task", Model: "m", Dim: 2, Vector: []float64{0.1, 0.2}, TextHash: "h1", LogSeq: 1}}
	if err := s.WriteVectorBatch("ATM", "m", first, 1); err != nil {
		t.Fatal(err)
	}
	second := []VectorEntry{{ID: "ATM-0002", Kind: "task", Model: "m", Dim: 2, Vector: []float64{0.3, 0.4}, TextHash: "h2", LogSeq: 2}}
	if err := s.WriteVectorBatch("ATM", "m", second, 2); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadVectors("ATM", "m")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2 (append)", len(got))
	}
	meta, _ := s.VectorMeta("ATM", "m")
	if meta.Count != 2 || meta.LastLogSeq != 2 {
		t.Errorf("meta after append = %+v, want count=2 last_log_seq=2", meta)
	}
}

func TestWriteVectorBatchModelMismatchRejected(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "ATM")
	entries := []VectorEntry{{ID: "ATM-0001", Kind: "task", Model: "other", Dim: 2, Vector: []float64{0.1, 0.2}}}
	err := s.WriteVectorBatch("ATM", "m", entries, 1)
	if !IsUsage(err) {
		t.Errorf("err = %v, want ErrUsage", err)
	}
}

func TestWriteVectorBatchDimMismatchRejected(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "ATM")
	entries := []VectorEntry{
		{ID: "ATM-0001", Kind: "task", Model: "m", Dim: 2, Vector: []float64{0.1, 0.2}},
		{ID: "ATM-0002", Kind: "task", Model: "m", Dim: 4, Vector: []float64{0.1, 0.2, 0.3, 0.4}},
	}
	err := s.WriteVectorBatch("ATM", "m", entries, 2)
	if !IsUsage(err) {
		t.Errorf("err = %v, want ErrUsage", err)
	}
}

func TestDropVectors(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "ATM")
	if err := s.WriteVectorBatch("ATM", "m", []VectorEntry{{ID: "ATM-0001", Kind: "task", Model: "m", Dim: 2, Vector: []float64{0.1, 0.2}}}, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.DropVectors("ATM", "m"); err != nil {
		t.Fatalf("DropVectors: %v", err)
	}
	if _, err := s.ReadVectors("ATM", "m"); err != nil {
		t.Errorf("ReadVectors after drop: %v, want nil", err)
	}
	err = s.DropVectors("ATM", "m")
	if !IsNotFound(err) {
		t.Errorf("DropVectors missing = %v, want ErrNotFound", err)
	}
}

func TestListVectorModels(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "ATM")
	for _, slug := range []string{"nomic-embed-text", "text-embedding-3-small"} {
		if err := s.WriteVectorBatch("ATM", slug, []VectorEntry{{ID: "ATM-0001", Kind: "task", Model: slug, Dim: 2, Vector: []float64{0.1, 0.2}}}, 1); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListVectorModels("ATM")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"nomic-embed-text", "text-embedding-3-small"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("ListVectorModels = %v, want %v", got, want)
	}
}

func TestReadVectorsMalformedLineSkipped(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "ATM")
	if err := s.WriteVectorBatch("ATM", "m", []VectorEntry{{ID: "ATM-0001", Kind: "task", Model: "m", Dim: 2, Vector: []float64{0.1, 0.2}}}, 1); err != nil {
		t.Fatal(err)
	}
	// append a malformed line directly
	path := s.vectorPath("ATM", "m")
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString("{not valid json\n")
	_ = f.Close()
	got, err := s.ReadVectors("ATM", "m")
	if err != nil {
		t.Fatalf("ReadVectors: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d valid entries, want 1 (malformed skipped)", len(got))
	}
}
```

(Add `"os"` to the test file imports.)

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestWriteVectorBatch|TestDropVectors|TestListVectorModels|TestReadVectorsMalformed' -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/store/vectors.go internal/store/vectors_test.go internal/store/store.go
git commit -m "store: add per-project per-model vector read/write helpers (ATM-0057)"
```

---

## Task 2: store eval read/write helpers

**Files:**
- Create: `internal/store/eval.go`
- Create: `internal/store/eval_test.go`

**Interfaces:**
- Consumes: `s.evalSetPath`, `s.evalRunPath`, `s.WithLock`, `WriteFileAtomic`, `ReadJSON`, `Now`, `RFC3339UTC`, `ErrNotFound`, `IsNotFound`, `os.MkdirAll`.
- Produces: `EvalEntry`, `EvalRun`, `EvalQueryResult` types, `ReadEvalSet(code)`, `AppendEvalEntries(code, entries)`, `RecordEvalRun(code, slug, run)`, `ListEvalRuns(code, slug)`, `GetEvalRun(code, id)`.

- [ ] **Step 1: Write the failing test for eval set append + read**

Create `internal/store/eval_test.go`:

```go
package store

import "testing"

func TestAppendEvalEntriesAndRead(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "ATM")
	entries := []EvalEntry{
		{Query: "label conflicts", RelevantIDs: []string{"ATM-0001", "ATM-0002"}, QueryVectors: map[string][]float64{"m": {0.1, 0.2}}},
		{Query: "audit log", RelevantIDs: []string{"ATM-0003"}, QueryVectors: map[string][]float64{"m": {0.3, 0.4}}},
	}
	if err := s.AppendEvalEntries("ATM", entries); err != nil {
		t.Fatalf("AppendEvalEntries: %v", err)
	}
	got, err := s.ReadEvalSet("ATM")
	if err != nil {
		t.Fatalf("ReadEvalSet: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got[0].Query != "label conflicts" || len(got[0].RelevantIDs) != 2 {
		t.Errorf("entry 0 = %+v", got[0])
	}
}

func TestReadEvalSetMissing(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "ATM")
	got, err := s.ReadEvalSet("ATM")
	if err != nil {
		t.Fatalf("ReadEvalSet missing: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil for missing eval set", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestAppendEvalEntriesAndRead -v`
Expected: FAIL — `EvalEntry undefined` / `s.AppendEvalEntries undefined`.

- [ ] **Step 3: Implement eval.go**

Create `internal/store/eval.go`:

```go
package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

type EvalEntry struct {
	Query        string                `json:"query"`
	RelevantIDs  []string              `json:"relevant_ids"`
	QueryVectors map[string][]float64  `json:"query_vectors"`
}

type EvalQueryResult struct {
	Query       string   `json:"query"`
	HitIDs      []string `json:"hit_ids"`
	RelevantIDs []string `json:"relevant_ids"`
	Recall      float64  `json:"recall"`
	Precision   float64  `json:"precision"`
}

type EvalRun struct {
	ID           string            `json:"id"`
	Model        string            `json:"model"`
	K            int               `json:"k"`
	RecallAtK    float64           `json:"recall_at_k"`
	PrecisionAtK float64           `json:"precision_at_k"`
	PerQuery     []EvalQueryResult `json:"per_query"`
	Skipped      int               `json:"skipped"`
	RunAt        time.Time         `json:"run_at"`
}

// ReadEvalSet reads eval/evalset.jsonl. Missing file -> (nil, nil). Malformed
// lines are skipped (best-effort).
func (s *Store) ReadEvalSet(code string) ([]EvalEntry, error) {
	f, err := os.Open(s.evalSetPath(code))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []EvalEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e EvalEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

// AppendEvalEntries appends entries to eval/evalset.jsonl under the project lock.
func (s *Store) AppendEvalEntries(code string, entries []EvalEntry) error {
	return s.WithLock(code, func() error {
		if err := os.MkdirAll(s.evalDir(code), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(s.evalSetPath(code), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		for _, e := range entries {
			b, err := json.Marshal(e)
			if err != nil {
				f.Close()
				return err
			}
			if _, err := f.Write(append(b, '\n')); err != nil {
				f.Close()
				return err
			}
		}
		return f.Close()
	})
}

// RecordEvalRun writes a run record to eval/runs/<slug>/<timestamp>.json.
func (s *Store) RecordEvalRun(code, slug string, run *EvalRun) error {
	return s.WithLock(code, func() error {
		dir := filepath.Join(s.evalDir(code), "runs", slug)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		ts := run.RunAt.UTC().Format("20060102-150405")
		run.ID = slug + "-" + ts
		return WriteFileAtomic(s.evalRunPath(code, slug, ts), run)
	})
}

// ListEvalRuns returns run records for a model, sorted by RunAt ascending.
func (s *Store) ListEvalRuns(code, slug string) ([]EvalRun, error) {
	dir := filepath.Join(s.evalDir(code), "runs", slug)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []EvalRun
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var r EvalRun
		if err := ReadJSON(filepath.Join(dir, e.Name()), &r); err != nil {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RunAt.Before(out[j].RunAt) })
	return out, nil
}

// GetEvalRun reads one run record by id (<slug>-<timestamp>). Missing -> ErrNotFound.
func (s *Store) GetEvalRun(code, id string) (*EvalRun, error) {
	// id = <slug>-<timestamp>; scan runs/<slug>/ for a file matching <timestamp>.json
	// Simpler: list all runs and find by id.
	all, err := s.allEvalRuns(code)
	if err != nil {
		return nil, err
	}
	for _, r := range all {
		if r.ID == id {
			return &r, nil
		}
	}
	return nil, fmt.Errorf("%w: eval run %q", ErrNotFound, id)
}

func (s *Store) allEvalRuns(code string) ([]EvalRun, error) {
	runsDir := filepath.Join(s.evalDir(code), "runs")
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []EvalRun
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rs, err := s.ListEvalRuns(code, e.Name())
		if err != nil {
			return out, err
		}
		out = append(out, rs...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RunAt.Before(out[j].RunAt) })
	return out, nil
}
```

(Add `"path/filepath"` to the imports.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestAppendEvalEntriesAndRead|TestReadEvalSetMissing' -v`
Expected: PASS.

- [ ] **Step 5: Write failing tests for run record write/list/get**

Append to `internal/store/eval_test.go`:

```go
func TestRecordEvalRunListGet(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "ATM")
	run := &EvalRun{
		Model: "m", K: 5,
		RecallAtK: 0.8, PrecisionAtK: 0.6,
		PerQuery: []EvalQueryResult{{Query: "q1", HitIDs: []string{"ATM-0001"}, RelevantIDs: []string{"ATM-0001"}, Recall: 1.0, Precision: 1.0}},
		RunAt: Now(),
	}
	if err := s.RecordEvalRun("ATM", "m", run); err != nil {
		t.Fatalf("RecordEvalRun: %v", err)
	}
	if run.ID == "" {
		t.Fatal("run.ID not stamped")
	}
	list, err := s.ListEvalRuns("ATM", "m")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != run.ID {
		t.Errorf("ListEvalRuns = %+v, want one with id %q", list, run.ID)
	}
	got, err := s.GetEvalRun("ATM", run.ID)
	if err != nil {
		t.Fatalf("GetEvalRun: %v", err)
	}
	if got.RecallAtK != 0.8 {
		t.Errorf("GetEvalRun recall = %v, want 0.8", got.RecallAtK)
	}
	_, err = s.GetEvalRun("ATM", "m-nonexistent")
	if !IsNotFound(err) {
		t.Errorf("GetEvalRun missing = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/store/ -run TestRecordEvalRunListGet -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/store/eval.go internal/store/eval_test.go
git commit -m "store: add eval set + eval run read/write helpers (ATM-0057)"
```

---

## Task 3: store search engine (cosine + text fallback)

**Files:**
- Create: `internal/store/search.go`
- Create: `internal/store/search_test.go`

**Interfaces:**
- Consumes: `s.ReadVectors(code, slug)`, `s.cacheDB()` + existing task/comment cache reads (for text fallback + snippet rendering), `s.ListTasks`, `s.ListComments`, `ErrUsage`.
- Produces: `Hit` struct, `SearchParams` struct, `Search(p) (hits []Hit, fallbackUsed bool, err error)`, `cosineSimilarity(a, b)`, `textSearch(...)`.
- Depends on: an existing way to load all tasks + comments for a project for the text-fallback path. Use `s.Replay(code)` to get `ReplayState{Tasks, Comments}` (already exists; see `rebuild.go` usage). Confirm the `ReplayState` field names by reading `internal/store/log.go` before writing the implementation.

- [ ] **Step 1: Read ReplayState shape**

Run: `rg "type ReplayState" internal/store/log.go -A 10`
Confirm the fields `Tasks []*Task` and `Comments []*Comment` exist. Note the exact types for use in `search.go`.

- [ ] **Step 2: Write the failing test for cosine ranking**

Create `internal/store/search_test.go`:

```go
package store

import "testing"

func TestCosineSimilarity(t *testing.T) {
	got := cosineSimilarity([]float64{1, 0}, []float64{1, 0})
	if got < 0.9999 {
		t.Errorf("cosine identical = %v, want ~1", got)
	}
	got = cosineSimilarity([]float64{1, 0}, []float64{0, 1})
	if got > 0.0001 {
		t.Errorf("cosine orthogonal = %v, want ~0", got)
	}
	got = cosineSimilarity([]float64{1, 0}, []float64{-1, 0})
	if got > -0.9999 {
		t.Errorf("cosine opposite = %v, want ~-1", got)
	}
}

func TestSearchSemanticRanking(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "ATM")
	// index two tasks; query closer to ATM-0001
	entries := []VectorEntry{
		{ID: "ATM-0001", Kind: "task", Model: "m", Dim: 2, Vector: []float64{1, 0}, TextHash: "h1", LogSeq: 1},
		{ID: "ATM-0002", Kind: "task", Model: "m", Dim: 2, Vector: []float64{0, 1}, TextHash: "h2", LogSeq: 2},
	}
	if err := s.WriteVectorBatch("ATM", "m", entries, 2); err != nil {
		t.Fatal(err)
	}
	// seed the tasks so snippet rendering works
	seedTaskWith(t, s, "ATM", "ATM-0001", "label resolver refactor", "")
	seedTaskWith(t, s, "ATM", "ATM-0002", "audit log redesign", "")
	hits, fallback, err := s.Search(SearchParams{
		Project: "ATM", Model: "m", QueryVector: []float64{0.95, 0.05}, QueryText: "label resolver",
		K: 5, Threshold: 0.3,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if fallback {
		t.Errorf("fallback=true, want false (strong semantic hit exists)")
	}
	if len(hits) == 0 || hits[0].ID != "ATM-0001" {
		t.Errorf("hits = %+v, want ATM-0001 first", hits)
	}
	if hits[0].Match != "semantic" {
		t.Errorf("hit.Match = %q, want semantic", hits[0].Match)
	}
}

func TestSearchTextFallbackWhenNoIndex(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "ATM")
	seedTaskWith(t, s, "ATM", "ATM-0001", "label resolver refactor", "hierarchical prefixes")
	seedTaskWith(t, s, "ATM", "ATM-0002", "audit log redesign", "")
	hits, fallback, err := s.Search(SearchParams{
		Project: "ATM", Model: "m", QueryVector: nil, QueryText: "label resolver",
		K: 5, Threshold: 0.3,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !fallback {
		t.Errorf("fallback=false, want true (no index)")
	}
	if len(hits) == 0 {
		t.Fatalf("expected text hits, got none")
	}
	if hits[0].Match != "text" {
		t.Errorf("hit.Match = %q, want text", hits[0].Match)
	}
}

func TestSearchTextFallbackWhenWeakScore(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "ATM")
	entries := []VectorEntry{
		{ID: "ATM-0001", Kind: "task", Model: "m", Dim: 2, Vector: []float64{0.1, 0.0}, TextHash: "h1", LogSeq: 1},
	}
	if err := s.WriteVectorBatch("ATM", "m", entries, 1); err != nil {
		t.Fatal(err)
	}
	seedTaskWith(t, s, "ATM", "ATM-0001", "label resolver", "hierarchical")
	// query orthogonal -> score ~0 -> below threshold -> fallback
	hits, fallback, err := s.Search(SearchParams{
		Project: "ATM", Model: "m", QueryVector: []float64{0, 1}, QueryText: "label resolver",
		K: 5, Threshold: 0.3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !fallback {
		t.Errorf("fallback=false, want true (weak score)")
	}
	if len(hits) == 0 || hits[0].Match != "text" {
		t.Errorf("hits = %+v, want text fallback", hits)
	}
}

func TestSearchDimMismatchRejected(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "ATM")
	if err := s.WriteVectorBatch("ATM", "m", []VectorEntry{{ID: "ATM-0001", Kind: "task", Model: "m", Dim: 2, Vector: []float64{0.1, 0.2}}}, 1); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.Search(SearchParams{
		Project: "ATM", Model: "m", QueryVector: []float64{0.1, 0.2, 0.3}, QueryText: "q",
		K: 5, Threshold: 0.3,
	})
	if !IsUsage(err) {
		t.Errorf("err = %v, want ErrUsage (dim mismatch)", err)
	}
}

func TestSearchKindFilter(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "ATM")
	entries := []VectorEntry{
		{ID: "ATM-0001", Kind: "task", Model: "m", Dim: 2, Vector: []float64{1, 0}, TextHash: "h1", LogSeq: 1},
		{ID: "ATM-0001-c0001", Kind: "comment", Model: "m", Dim: 2, Vector: []float64{1, 0}, TextHash: "h2", LogSeq: 2},
	}
	if err := s.WriteVectorBatch("ATM", "m", entries, 2); err != nil {
		t.Fatal(err)
	}
	seedTaskWith(t, s, "ATM", "ATM-0001", "t", "")
	hits, _, err := s.Search(SearchParams{
		Project: "ATM", Model: "m", QueryVector: []float64{1, 0}, QueryText: "t",
		Kind: "task", K: 5, Threshold: 0.3,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.Kind != "task" {
			t.Errorf("got kind %q, want only task", h.Kind)
		}
	}
}
```

Note: `seedTaskWith` may not exist. Check `vocabulary_test.go`/`comment_test.go` for the existing task-seed helper. If none exists that lets you set a specific ID/title, add a minimal one to `search_test.go` that creates a task via `s.CreateTask` then `s.SetTitle`/`s.SetDescription` as needed. The text-fallback path must find the task by token overlap, so the title/description must be set.

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'TestCosineSimilarity|TestSearch' -v`
Expected: FAIL — `cosineSimilarity`/`Search`/`SearchParams`/`Hit` undefined.

- [ ] **Step 4: Implement search.go**

Create `internal/store/search.go`:

```go
package store

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

type Hit struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Score   float64  `json:"score"`
	Title   string   `json:"title,omitempty"`
	Snippet string   `json:"snippet"`
	Labels  []string `json:"labels,omitempty"`
	Match   string   `json:"match"`
}

type SearchParams struct {
	Project     string
	Model       string
	QueryVector []float64
	QueryText   string
	Kind        string
	K           int
	Threshold   float64
}

// Search runs semantic cosine search against vectors/<slug>.jsonl and falls
// back to local full-text search when no index exists or the top semantic
// score is below threshold. Returns hits (ranked) and fallbackUsed.
func (s *Store) Search(p SearchParams) (hits []Hit, fallbackUsed bool, err error) {
	if p.K <= 0 {
		p.K = 5
	}
	if p.Threshold <= 0 {
		p.Threshold = 0.30
	}
	entries, err := s.ReadVectors(p.Project, p.Model)
	if err != nil {
		return nil, false, err
	}
	// semantic path
	if len(entries) > 0 && len(p.QueryVector) > 0 {
		idxDim := entries[0].Dim
		if len(p.QueryVector) != idxDim {
			return nil, false, fmt.Errorf("%w: query vector dim %d != index dim %d for model %q", ErrUsage, len(p.QueryVector), idxDim, p.Model)
		}
		stale := s.vectorTextHashes(p.Project, entries)
		scored := make([]Hit, 0, len(entries))
		for _, e := range entries {
			if p.Kind != "" && p.Kind != "all" && e.Kind != p.Kind {
				continue
			}
			if stale[e.ID] {
				continue
			}
			score := cosineSimilarity(p.QueryVector, e.Vector)
			if score < p.Threshold {
				continue
			}
			scored = append(scored, Hit{ID: e.ID, Kind: e.Kind, Score: score, Match: "semantic"})
		}
		sort.SliceStable(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })
		if len(scored) > 0 {
			top := scored[0].Score
			if top >= p.Threshold {
				s.enrichHits(p.Project, scored)
				if len(scored) > p.K {
					scored = scored[:p.K]
				}
				return scored, false, nil
			}
		}
	}
	// text fallback
	textHits := s.textSearch(p.Project, p.QueryText, p.Kind, p.K)
	return textHits, true, nil
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// vectorTextHashes returns a map of id->stale(true) for entries whose current
// task/comment text hash no longer matches the embedded text_hash. Tasks or
// comments that can't be loaded are treated as stale (skip).
func (s *Store) vectorTextHashes(code string, entries []VectorEntry) map[string]bool {
	stale := make(map[string]bool, len(entries))
	for _, e := range entries {
		var current string
		switch e.Kind {
		case "task":
			t, err := s.GetTask(e.ID)
			if err != nil {
				stale[e.ID] = true
				continue
			}
			current = taskDocumentText(t)
		case "comment":
			c, err := s.GetComment(e.ID)
			if err != nil {
				stale[e.ID] = true
				continue
			}
			current = commentDocumentText(c)
		default:
			stale[e.ID] = true
			continue
		}
		if hashText(current) != e.TextHash {
			stale[e.ID] = true
		}
	}
	return stale
}

// enrichHits fills Title/Snippet/Labels for task and comment hits.
func (s *Store) enrichHits(code string, hits []Hit) {
	for i := range hits {
		switch hits[i].Kind {
		case "task":
			t, err := s.GetTask(hits[i].ID)
			if err != nil {
				continue
			}
			hits[i].Title = t.Title
			hits[i].Snippet = snippet(t.Description, 80)
			hits[i].Labels = t.Labels
		case "comment":
			c, err := s.GetComment(hits[i].ID)
			if err != nil {
				continue
			}
			hits[i].Snippet = snippet(c.Body, 80)
			hits[i].Labels = c.Labels
		}
	}
}

// textSearch is the pure-Go fallback: token-overlap + recency over all
// tasks + comments for the project.
func (s *Store) textSearch(code, query, kind string, k int) []Hit {
	qtokens := tokenize(query)
	if len(qtokens) == 0 {
		return nil
	}
	var hits []Hit
	if kind == "" || kind == "all" || kind == "task" {
		st, err := s.Replay(code)
		if err == nil && st != nil {
			for _, t := range st.Tasks {
				doc := taskDocumentText(t)
				score := tokenOverlap(qtokens, tokenize(doc))
				if score <= 0 {
					continue
				}
				hits = append(hits, Hit{ID: t.ID, Kind: "task", Score: float64(score), Title: t.Title, Snippet: snippet(t.Description, 80), Labels: t.Labels, Match: "text"})
			}
		}
	}
	if kind == "" || kind == "all" || kind == "comment" {
		st, err := s.Replay(code)
		if err == nil && st != nil {
			for _, c := range st.Comments {
				doc := commentDocumentText(c)
				score := tokenOverlap(qtokens, tokenize(doc))
				if score <= 0 {
					continue
				}
				hits = append(hits, Hit{ID: c.ID, Kind: "comment", Score: float64(score), Snippet: snippet(c.Body, 80), Labels: c.Labels, Match: "text"})
			}
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > k {
		hits = hits[:k]
	}
	return hits
}

func tokenize(s string) []string {
	s = strings.ToLower(s)
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	return fields
}

func tokenOverlap(query, doc []string) int {
	dset := map[string]bool{}
	for _, w := range doc {
		dset[w] = true
	}
	n := 0
	for _, w := range query {
		if dset[w] {
			n++
		}
	}
	return n
}

func snippet(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
```

Add two helpers for document text + hashing. The document text composition must match what the manager embeds (task: title + description + labels; comment: body + labels). Put these in `search.go`:

```go
func taskDocumentText(t *Task) string {
	return strings.Join(append(append([]string{t.Title, t.Description}, t.Labels...), " "), " ")
}

func commentDocumentText(c *Comment) string {
	return strings.Join(append(append([]string{c.Body}, c.Labels...), " "), " ")
}

func hashText(s string) string {
	// use crypto/sha256; add "crypto/sha256" + "encoding/hex" imports
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])
}
```

(Add `"crypto/sha256"` and `"encoding/hex"` to the imports of `search.go`.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestCosineSimilarity|TestSearch' -v`
Expected: PASS. If `seedTaskWith` doesn't exist, add it first (see note in Step 2) and re-run.

- [ ] **Step 6: Commit**

```bash
git add internal/store/search.go internal/store/search_test.go
git commit -m "store: add semantic cosine search + text fallback engine (ATM-0057)"
```

---

## Task 4: rebuild/verify integration for vectors + eval

**Files:**
- Modify: `internal/store/rebuild.go`
- Modify: `internal/store/verify.go`
- Test: `internal/store/rebuild_test.go` (modify), `internal/store/verify_test.go` (modify)

**Interfaces:**
- Consumes: `s.projectCodesOnDisk`, `s.Replay`, `s.cacheDB`, existing rebuild/verify.
- Produces: `Rebuild` drops `vectors/`+`eval/` per project; `VerifyReport` gains info-level `VectorIndexes []VectorIndexInfo` + `EvalSetPresent bool`.

- [ ] **Step 1: Write the failing test**

Append to `internal/store/rebuild_test.go` (read the file first to match its existing style):

```go
func TestRebuildDropsVectorsAndEval(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "ATM")
	// write a vector index + an eval entry
	if err := s.WriteVectorBatch("ATM", "m", []VectorEntry{{ID: "ATM-0001", Kind: "task", Model: "m", Dim: 2, Vector: []float64{0.1, 0.2}}}, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvalEntries("ATM", []EvalEntry{{Query: "q", RelevantIDs: []string{"ATM-0001"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rebuild(); err != nil {
		t.Fatal(err)
	}
	if models, _ := s.ListVectorModels("ATM"); len(models) != 0 {
		t.Errorf("vectors not dropped: %v", models)
	}
	if got, _ := s.ReadEvalSet("ATM"); got != nil {
		t.Errorf("eval not dropped: %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestRebuildDropsVectorsAndEval -v`
Expected: FAIL — vectors/eval still present after Rebuild.

- [ ] **Step 3: Implement the drop in Rebuild**

In `internal/store/rebuild.go`, inside the `for _, code := range codes` loop (after the replay+upsert block), add:

```go
		// vectors/ and eval/ are derived; drop them. They regenerate on reindex.
		_ = os.RemoveAll(s.vectorsDir(code))
		_ = os.RemoveAll(s.evalDir(code))
```

(Add `"os"` to imports if not already present.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestRebuildDropsVectorsAndEval -v`
Expected: PASS.

- [ ] **Step 5: Add info-level verify reporting**

In `internal/store/verify.go`, add to `VerifyReport`:

```go
	VectorIndexes []VectorIndexInfo `json:"vector_indexes,omitempty"`
	EvalSetPresent bool              `json:"eval_set_present"`
```

Add the type:

```go
type VectorIndexInfo struct {
	Model  string `json:"model"`
	Count  int    `json:"count"`
	Stale  int    `json:"stale"`
	LastLogSeq int `json:"last_log_seq"`
}
```

In `VerifyProject`, before returning, add:

```go
	if models, err := s.ListVectorModels(code); err == nil {
		for _, slug := range models {
			meta, _ := s.VectorMeta(code, slug)
			info := VectorIndexInfo{Model: slug}
			if meta != nil {
				info.Count = meta.Count
				info.LastLogSeq = meta.LastLogSeq
			}
			report.VectorIndexes = append(report.VectorIndexes, info)
		}
	}
	if es, _ := s.ReadEvalSet(code); es != nil {
		report.EvalSetPresent = true
	}
```

Absence of `VectorIndexes` / `EvalSetPresent=false` is info, not an error — do NOT set `report.Diverged` for it.

- [ ] **Step 6: Run full store tests + verify build**

Run: `go test ./internal/store/ -v && go build ./...`
Expected: PASS, build OK.

- [ ] **Step 7: Commit**

```bash
git add internal/store/rebuild.go internal/store/rebuild_test.go internal/store/verify.go internal/store/verify_test.go
git commit -m "store: rebuild drops vectors/eval; verify reports them info-level (ATM-0057)"
```

---

## Task 5: cli `atm search` command

**Files:**
- Create: `internal/cli/search.go`
- Create: `internal/cli/search_test.go`
- Modify: `internal/cli/root.go` (register `newSearchCmd`)

**Interfaces:**
- Consumes: `st.openStore`, `st.emit`, `st.stdout`, `st.isJSON`, `store.Search`, `store.Hit`, `ErrUsage`.
- Produces: `newSearchCmd(st) *cobra.Command`.

- [ ] **Step 1: Write the failing golden test**

Create `internal/cli/search_test.go`. Use the existing golden harness pattern from `vocabulary_test.go` / `harness_test.go`. Read `harness_test.go` first to confirm helper names.

```go
package cli

import "testing"

func TestSearchTextFallbackGolden(t *testing.T) {
	h := newGoldenHarness(t)
	// seed a project + a task whose title contains "label resolver"
	// (use the harness's seed helpers; if none, use atm task create via h.run)
	h.run(t, "project create --code FOO --name Foo")
	h.run(t, "task create --project FOO --title \"label resolver refactor\" --actor tester")
	// no vector index -> text fallback
	got := h.run(t, "search --project FOO --model m --query-vector \"[]\" \"label resolver\" --output json")
	compareGolden(t, "search-text-fallback", got)
}
```

(Adjust the harness calls to match the real `newGoldenHarness` API. The key assertions: `match` is `text`, `fallback_used` is true, at least one hit. Commit the golden JSON after first run with `-update`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestSearchTextFallbackGolden -v`
Expected: FAIL — `unknown command "search"`.

- [ ] **Step 3: Implement search.go**

Create `internal/cli/search.go`:

```go
package cli

import (
	"encoding/json"
	"fmt"

	"atm/internal/store"

	"github.com/spf13/cobra"
)

func newSearchCmd(st *cliState) *cobra.Command {
	var (
		project     string
		model       string
		queryVector string
		kind        string
		k           int
		threshold   float64
	)
	cmd := &cobra.Command{
		Use:   "search \"query text\"",
		Short: "Semantic search over tasks + comments (with text fallback)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if model == "" {
				return fmt.Errorf("%w: --model is required", ErrUsage)
			}
			var qv []float64
			if queryVector != "" {
				if err := json.Unmarshal([]byte(queryVector), &qv); err != nil {
					return fmt.Errorf("%w: --query-vector must be a JSON array of numbers: %v", ErrUsage, err)
				}
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if project == "" {
				return fmt.Errorf("%w: --project is required", ErrUsage)
			}
			hits, fallback, err := s.Search(store.SearchParams{
				Project: project, Model: model, QueryVector: qv, QueryText: args[0],
				Kind: kind, K: k, Threshold: threshold,
			})
			if err != nil {
				return err
			}
			match := "semantic"
			if fallback {
				match = "text"
			}
			return st.emit(st.stdout(), map[string]any{
				"query":          args[0],
				"model":          model,
				"match":          match,
				"hits":           hitsToJSON(hits),
				"fallback_used":  fallback,
			}, func() {
				fmt.Fprintf(st.stdout(), "MODEL: %s  MATCH: %s  K: %d\n", model, match, k)
				for _, h := range hits {
					label := h.Title
					if label == "" {
						label = h.Snippet
					}
					fmt.Fprintf(st.stdout(), "%s\t%s\t%.4f\t%s\t%s\n", h.ID, h.Kind, h.Score, h.Match, label)
				}
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&model, "model", "", "embedding model slug (selects vectors/<slug>.jsonl)")
	cmd.Flags().StringVar(&queryVector, "query-vector", "", "JSON array of floats (the query embedded by the caller's host)")
	cmd.Flags().StringVar(&kind, "kind", "all", "task | comment | all")
	cmd.Flags().IntVar(&k, "k", 5, "top-K results")
	cmd.Flags().Float64Var(&threshold, "threshold", 0.30, "cosine threshold below which text fallback triggers")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("model")
	return cmd
}

type jsonHit struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Score   float64  `json:"score"`
	Title   string   `json:"title,omitempty"`
	Snippet string   `json:"snippet"`
	Labels  []string `json:"labels,omitempty"`
	Match   string   `json:"match"`
}

func hitsToJSON(hits []store.Hit) []jsonHit {
	out := make([]jsonHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, jsonHit(h))
	}
	return out
}
```

Register in `internal/cli/root.go` (after `newVocabularyCmd`):

```go
	root.AddCommand(newSearchCmd(st))
```

- [ ] **Step 4: Run test, regenerate golden**

Run: `go test ./internal/cli/ -run TestSearchTextFallbackGolden -v -update`
Then re-run without `-update`: `go test ./internal/cli/ -run TestSearchTextFallbackGolden -v`
Expected: PASS.

- [ ] **Step 5: Add a semantic-match golden + error-case test**

Append to `search_test.go`:

```go
func TestSearchMissingQueryVector(t *testing.T) {
	h := newGoldenHarness(t)
	h.run(t, "project create --code FOO --name Foo")
	_, err := h.runErr(t, "search --project FOO --model m \"q\"")
	if !isUsage(err) {
		t.Errorf("err = %v, want ErrUsage", err)
	}
}
```

(Use the harness's `runErr` if it exists; otherwise run the command and assert exit code. Match the existing pattern in `vocabulary_test.go` for error cases.)

- [ ] **Step 6: Run all search tests + build**

Run: `go test ./internal/cli/ -run TestSearch -v && go build ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/search.go internal/cli/search_test.go internal/cli/root.go internal/cli/testdata/golden/search-text-fallback.json
git commit -m "cli: add atm search (semantic + text fallback) (ATM-0057)"
```

---

## Task 6: cli `atm index` command tree

**Files:**
- Create: `internal/cli/index.go`
- Create: `internal/cli/index_test.go`
- Modify: `internal/cli/root.go` (register `newIndexCmd`)

**Interfaces:**
- Consumes: `st.openStore`, `st.emit`, `st.resolveActor`, `store.WriteVectorBatch`, `store.VectorMeta`, `store.DropVectors`, `store.ListVectorModels`, `store.LastLogSeq`, `store.VectorEntry`, `ErrUsage`, `ErrNotFound`.
- Produces: `newIndexCmd(st)` with subcommands `write-batch`, `status`, `drop`, `models`.

- [ ] **Step 1: Write failing golden tests**

Create `internal/cli/index_test.go`:

```go
package cli

import "testing"

func TestIndexModelsEmptyGolden(t *testing.T) {
	h := newGoldenHarness(t)
	h.run(t, "project create --code FOO --name Foo")
	got := h.run(t, "index models --project FOO --output json")
	compareGolden(t, "index-models-empty", got)
}

func TestIndexWriteBatchGolden(t *testing.T) {
	h := newGoldenHarness(t)
	h.run(t, "project create --code FOO --name Foo")
	// write a batch file the manager would produce
	// (in real use the manager passes --file <path>; for the test, write a temp file)
	tmp := writeTemp(t, "batch.jsonl", `{"id":"FOO-0001","kind":"task","model":"m","dim":2,"vector":[0.1,0.2],"text_hash":"sha256:abc","log_seq":1}`+"\n")
	got := h.run(t, "index write-batch --project FOO --model m --file "+tmp+" --actor tester --output json")
	compareGolden(t, "index-write-batch", got)
}

func TestIndexStatusGolden(t *testing.T) {
	h := newGoldenHarness(t)
	h.run(t, "project create --code FOO --name Foo")
	h.run(t, "task create --project FOO --title t --actor tester")
	tmp := writeTemp(t, "b.jsonl", `{"id":"FOO-0001","kind":"task","model":"m","dim":2,"vector":[0.1,0.2],"text_hash":"x","log_seq":1}`+"\n")
	h.run(t, "index write-batch --project FOO --model m --file "+tmp+" --actor tester")
	got := h.run(t, "index status --project FOO --output json")
	compareGolden(t, "index-status", got)
}
```

(Add a `writeTemp` helper to the test file if none exists: `os.CreateTemp` + write + return name.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestIndex' -v`
Expected: FAIL — `unknown command "index"`.

- [ ] **Step 3: Implement index.go**

Create `internal/cli/index.go`:

```go
package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"atm/internal/store"

	"github.com/spf13/cobra"
)

func newIndexCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{Use: "index", Short: "Vector index management (per-project, per-model)"}
	cmd.AddCommand(newIndexWriteBatchCmd(st))
	cmd.AddCommand(newIndexStatusCmd(st))
	cmd.AddCommand(newIndexDropCmd(st))
	cmd.AddCommand(newIndexModelsCmd(st))
	return cmd
}

func newIndexWriteBatchCmd(st *cliState) *cobra.Command {
	var project, model, file string
	cmd := &cobra.Command{
		Use:   "write-batch",
		Short: "Append a batch of vectors to vectors/<model>.jsonl (manager write target)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			_ = actor
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if _, err := s.GetProject(project); err != nil {
				return fmt.Errorf("%w: project %s not found", ErrNotFound, project)
			}
			entries, lastSeq, err := readVectorBatchFile(file)
			if err != nil {
				return err
			}
			if err := s.WriteVectorBatch(project, model, entries, lastSeq); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{
				"project": project, "model": model, "written": len(entries), "last_log_seq": lastSeq,
			}, func() {
				fmt.Fprintf(st.stdout(), "wrote %d vectors to %s/%s\n", len(entries), project, model)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&model, "model", "", "embedding model slug")
	cmd.Flags().StringVar(&file, "file", "", "path to a JSONL file of vector entries")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("model")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func readVectorBatchFile(path string) (entries []store.VectorEntry, lastSeq int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: open batch file: %v", ErrUsage, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e store.VectorEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, 0, fmt.Errorf("%w: malformed batch line: %v", ErrUsage, err)
		}
		entries = append(entries, e)
		if e.LogSeq > lastSeq {
			lastSeq = e.LogSeq
		}
	}
	return entries, lastSeq, sc.Err()
}

func newIndexStatusCmd(st *cliState) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Report per-model index staleness vs the project log",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if _, err := s.GetProject(project); err != nil {
				return fmt.Errorf("%w: project %s not found", ErrNotFound, project)
			}
			lastLogSeq, _ := s.LastLogSeq(project)
			models, err := s.ListVectorModels(project)
			if err != nil {
				return err
			}
			type statusRow struct {
				Model      string `json:"model"`
				Count      int    `json:"count"`
				LastLogSeq int    `json:"last_log_seq"`
				Behind     int    `json:"behind"`
				Stale      int    `json:"stale,omitempty"`
			}
			rows := make([]statusRow, 0, len(models))
			for _, slug := range models {
				meta, _ := s.VectorMeta(project, slug)
				r := statusRow{Model: slug}
				if meta != nil {
					r.Count = meta.Count
					r.LastLogSeq = meta.LastLogSeq
					r.Behind = lastLogSeq - meta.LastLogSeq
				}
				rows = append(rows, r)
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "log_last_seq": lastLogSeq, "indexes": rows}, func() {
				for _, r := range rows {
					fmt.Fprintf(st.stdout(), "%s\tcount=%d\tlast_log_seq=%d\tbehind=%d\n", r.Model, r.Count, r.LastLogSeq, r.Behind)
				}
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newIndexDropCmd(st *cliState) *cobra.Command {
	var project, model string
	cmd := &cobra.Command{
		Use:   "drop",
		Short: "Delete one model's vector index",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			_ = actor
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.DropVectors(project, model); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"dropped": model, "project": project}, func() {
				fmt.Fprintf(st.stdout(), "dropped vector index %s/%s\n", project, model)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&model, "model", "", "embedding model slug")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("model")
	return cmd
}

func newIndexModelsCmd(st *cliState) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "models",
		Short: "List which models have a vector index for the project",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if _, err := s.GetProject(project); err != nil {
				return fmt.Errorf("%w: project %s not found", ErrNotFound, project)
			}
			models, err := s.ListVectorModels(project)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "models": models}, func() {
				for _, m := range models {
					fmt.Fprintln(st.stdout(), m)
				}
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
```

Register in `internal/cli/root.go`:

```go
	root.AddCommand(newIndexCmd(st))
```

- [ ] **Step 4: Run tests, regenerate goldens**

Run: `go test ./internal/cli/ -run 'TestIndex' -v -update`
Then: `go test ./internal/cli/ -run 'TestIndex' -v`
Expected: PASS.

- [ ] **Step 5: Add model-mismatch + missing-actor tests**

Append to `index_test.go`:

```go
func TestIndexWriteBatchModelMismatch(t *testing.T) {
	h := newGoldenHarness(t)
	h.run(t, "project create --code FOO --name Foo")
	tmp := writeTemp(t, "b.jsonl", `{"id":"FOO-0001","kind":"task","model":"other","dim":2,"vector":[0.1,0.2],"text_hash":"x","log_seq":1}`+"\n")
	_, err := h.runErr(t, "index write-batch --project FOO --model m --file "+tmp+" --actor tester")
	if !isUsage(err) {
		t.Errorf("err = %v, want ErrUsage (model mismatch)", err)
	}
}

func TestIndexWriteBatchMissingActor(t *testing.T) {
	h := newGoldenHarness(t)
	h.run(t, "project create --code FOO --name Foo")
	tmp := writeTemp(t, "b.jsonl", `{"id":"FOO-0001","kind":"task","model":"m","dim":2,"vector":[0.1,0.2],"text_hash":"x","log_seq":1}`+"\n")
	_, err := h.runErr(t, "index write-batch --project FOO --model m --file "+tmp)
	if !isUsage(err) {
		t.Errorf("err = %v, want ErrUsage (missing actor)", err)
	}
}
```

(Match the harness's `runErr` + `isUsage` helpers; they exist in `harness_test.go`/`errors.go`.)

- [ ] **Step 6: Run all index tests + build**

Run: `go test ./internal/cli/ -run TestIndex -v && go build ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/index.go internal/cli/index_test.go internal/cli/root.go internal/cli/testdata/golden/index-*.json
git commit -m "cli: add atm index write-batch/status/drop/models (ATM-0057)"
```

---

## Task 7: cli `atm eval` command tree

**Files:**
- Create: `internal/cli/eval.go`
- Create: `internal/cli/eval_test.go`
- Modify: `internal/cli/root.go` (register `newEvalCmd`)

**Interfaces:**
- Consumes: `st.openStore`, `st.emit`, `st.resolveActor`, `store.ReadEvalSet`, `store.AppendEvalEntries`, `store.RecordEvalRun`, `store.ListEvalRuns`, `store.GetEvalRun`, `store.Search`, `store.EvalEntry`, `store.EvalRun`, `store.Hit`, `ErrNotFound`.
- Produces: `newEvalCmd(st)` with subcommands `write`, `run`, `runs`, `show`.

- [ ] **Step 1: Write failing golden tests**

Create `internal/cli/eval_test.go`:

```go
package cli

import "testing"

func TestEvalRunNoIndexGolden(t *testing.T) {
	h := newGoldenHarness(t)
	h.run(t, "project create --code FOO --name Foo")
	_, err := h.runErr(t, "eval run --project FOO --model m --output json")
	if !isNotFound(err) {
		t.Errorf("err = %v, want ErrNotFound (no index)", err)
	}
}

func TestEvalRunGolden(t *testing.T) {
	h := newGoldenHarness(t)
	h.run(t, "project create --code FOO --name Foo")
	h.run(t, "task create --project FOO --title \"label resolver\" --actor tester")
	// index one task
	vtmp := writeTemp(t, "v.jsonl", `{"id":"FOO-0001","kind":"task","model":"m","dim":2,"vector":[1,0],"text_hash":"x","log_seq":1}`+"\n")
	h.run(t, "index write-batch --project FOO --model m --file "+vtmp+" --actor tester")
	// eval set: one query whose relevant id is FOO-0001, query vector [1,0]
	etmp := writeTemp(t, "e.jsonl", `{"query":"label resolver","relevant_ids":["FOO-0001"],"query_vectors":{"m":[1,0]}}`+"\n")
	h.run(t, "eval write --project FOO --file "+etmp+" --actor tester")
	got := h.run(t, "eval run --project FOO --model m --k 5 --output json")
	compareGolden(t, "eval-run", got)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run TestEval -v`
Expected: FAIL — `unknown command "eval"`.

- [ ] **Step 3: Implement eval.go**

Create `internal/cli/eval.go`:

```go
package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"atm/internal/store"

	"github.com/spf13/cobra"
)

func newEvalCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{Use: "eval", Short: "Recall measurement (eval set + run records)"}
	cmd.AddCommand(newEvalWriteCmd(st))
	cmd.AddCommand(newEvalRunCmd(st))
	cmd.AddCommand(newEvalRunsCmd(st))
	cmd.AddCommand(newEvalShowCmd(st))
	return cmd
}

func newEvalWriteCmd(st *cliState) *cobra.Command {
	var project, file string
	cmd := &cobra.Command{
		Use:   "write",
		Short: "Append eval entries (queries + relevant IDs + per-model query vectors) to eval/evalset.jsonl",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			_ = actor
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if _, err := s.GetProject(project); err != nil {
				return fmt.Errorf("%w: project %s not found", ErrNotFound, project)
			}
			entries, err := readEvalFile(file)
			if err != nil {
				return err
			}
			if err := s.AppendEvalEntries(project, entries); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "written": len(entries)}, func() {
				fmt.Fprintf(st.stdout(), "wrote %d eval entries for %s\n", len(entries), project)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&file, "file", "", "path to a JSONL file of eval entries")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func readEvalFile(path string) ([]store.EvalEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%w: open eval file: %v", ErrUsage, err)
	}
	defer f.Close()
	var out []store.EvalEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e store.EvalEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("%w: malformed eval line: %v", ErrUsage, err)
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

func newEvalRunCmd(st *cliState) *cobra.Command {
	var project, model string
	k := 5
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Compute recall@k + precision@k for a model's index against the eval set",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if _, err := s.GetProject(project); err != nil {
				return fmt.Errorf("%w: project %s not found", ErrNotFound, project)
			}
			meta, err := s.VectorMeta(project, model)
			if err != nil {
				return err
			}
			if meta == nil {
				return fmt.Errorf("%w: no vector index for model %q; run 'atm manager <host> --index' first", ErrNotFound, model)
			}
			entries, err := s.ReadEvalSet(project)
			if err != nil {
				return err
			}
			run := &store.EvalRun{Model: model, K: k, RunAt: time.Now().UTC()}
			var recallSum, precSum float64
			evaluated := 0
			for _, e := range entries {
				qv := e.QueryVectors[model]
				if len(qv) == 0 {
					run.Skipped++
					continue
				}
				hits, _, serr := s.Search(store.SearchParams{
					Project: project, Model: model, QueryVector: qv, QueryText: e.Query,
					K: k, Threshold: 0.0,
				})
				if serr != nil {
					return serr
				}
				hitIDs := make([]string, 0, len(hits))
				for _, h := range hits {
					hitIDs = append(hitIDs, h.ID)
				}
				recall, precision := recallPrecision(hitIDs, e.RelevantIDs, k)
				run.PerQuery = append(run.PerQuery, store.EvalQueryResult{
					Query: e.Query, HitIDs: hitIDs, RelevantIDs: e.RelevantIDs,
					Recall: recall, Precision: precision,
				})
				recallSum += recall
				precSum += precision
				evaluated++
			}
			if evaluated > 0 {
				run.RecallAtK = recallSum / float64(evaluated)
				run.PrecisionAtK = precSum / float64(evaluated)
			}
			if err := s.RecordEvalRun(project, model, run); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{
				"run": run.ID, "model": model, "k": k,
				"recall_at_k": run.RecallAtK, "precision_at_k": run.PrecisionAtK,
				"evaluated": evaluated, "skipped": run.Skipped,
			}, func() {
				fmt.Fprintf(st.stdout(), "run %s  model=%s  recall@%d=%.3f  precision@%d=%.3f  evaluated=%d  skipped=%d\n",
					run.ID, model, k, run.RecallAtK, k, run.PrecisionAtK, evaluated, run.Skipped)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&model, "model", "", "embedding model slug")
	cmd.Flags().IntVar(&k, "k", 5, "top-K for recall/precision")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("model")
	return cmd
}

func recallPrecision(hitIDs, relevantIDs []string, k int) (recall, precision float64) {
	rel := map[string]bool{}
	for _, id := range relevantIDs {
		rel[id] = true
	}
	if len(rel) == 0 {
		return 0, 0
	}
	hits := 0
	for i, id := range hitIDs {
		if i >= k {
			break
		}
		if rel[id] {
			hits++
		}
	}
	recall = float64(hits) / float64(len(rel))
	returned := k
	if len(hitIDs) < k {
		returned = len(hitIDs)
	}
	if returned == 0 {
		return recall, 0
	}
	precision = float64(hits) / float64(returned)
	return recall, precision
}

func newEvalRunsCmd(st *cliState) *cobra.Command {
	var project, model string
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "List eval run records (time series) for a model",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			runs, err := s.ListEvalRuns(project, model)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "model": model, "runs": runs}, func() {
				for _, r := range runs {
					fmt.Fprintf(st.stdout(), "%s\trecall@%d=%.3f\tprecision@%d=%.3f\n", r.ID, r.K, r.RecallAtK, r.K, r.PrecisionAtK)
				}
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&model, "model", "", "embedding model slug")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("model")
	return cmd
}

func newEvalShowCmd(st *cliState) *cobra.Command {
	var project, id string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show one eval run's per-query breakdown",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			run, err := s.GetEvalRun(project, id)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"run": run}, func() {
				fmt.Fprintf(st.stdout(), "run %s  model=%s  recall@%d=%.3f  precision@%d=%.3f\n",
					run.ID, run.Model, run.K, run.RecallAtK, run.K, run.PrecisionAtK)
				for _, q := range run.PerQuery {
					fmt.Fprintf(st.stdout(), "  %s\trecall=%.3f\tprecision=%.3f\n", q.Query, q.Recall, q.Precision)
				}
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&id, "id", "", "eval run id (<slug>-<timestamp>)")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}
```

**Note on `GetEvalRun` + project:** `GetEvalRun(code, id)` takes a code. For `show`, require `--project` (simplest, deterministic) and pass it. The implementation above already does this. Do not implement the `allEvalRuns` scan-all fallback — it's unused; remove `allEvalRuns` from `eval.go` if it would otherwise be dead code, or simply don't add it (the `GetEvalRun` implementation in Task 2 only needs `ListEvalRuns` per slug, invoked for the given `code`).

Register in `internal/cli/root.go`:

```go
	root.AddCommand(newEvalCmd(st))
```

- [ ] **Step 4: Run tests, regenerate goldens**

Run: `go test ./internal/cli/ -run TestEval -v -update`
Then: `go test ./internal/cli/ -run TestEval -v`
Expected: PASS.

- [ ] **Step 5: Run all eval tests + build**

Run: `go test ./internal/cli/ -run TestEval -v && go build ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/eval.go internal/cli/eval_test.go internal/cli/root.go internal/cli/testdata/golden/eval-*.json
git commit -m "cli: add atm eval write/run/runs/show (recall measurement) (ATM-0057)"
```

---

## Task 8: cli `atm manager --index` flag

**Files:**
- Modify: `internal/cli/manager.go`
- Modify: `internal/cli/manager_test.go`
- Modify: `internal/cli/tmux.go`
- Create: `internal/cli/testdata/golden/manager-index-dry-run-opencode.json`
- Create: `internal/cli/testdata/golden/manager-index-dry-run-ollama.json`

**Interfaces:**
- Consumes: existing `managerOpts`, `runManager`, `managerEnvValues`, `BuildArgv`, `emitLaunchHeader`, `tmuxLabelOnboarding` pattern.
- Produces: `Index bool` on `managerOpts`; `--index` flag; `ATM_INDEX=1` env; `--index`/`--onboard` mutual exclusion; `tmuxLabelIndex`.

- [ ] **Step 1: Write the failing dry-run test**

Append to `internal/cli/manager_test.go` (match existing `--onboard` dry-run test style):

```go
func TestManagerIndexDryRunOpencodeGolden(t *testing.T) {
	h := newGoldenHarness(t)
	h.run(t, "project create --code FOO --name Foo")
	got := h.run(t, "manager opencode --project FOO --index --dry-run --output json")
	compareGolden(t, "manager-index-dry-run-opencode", got)
}

func TestManagerIndexOnboardMutualExclusion(t *testing.T) {
	h := newGoldenHarness(t)
	h.run(t, "project create --code FOO --name Foo")
	_, err := h.runErr(t, "manager opencode --project FOO --index --onboard --dry-run")
	if !isUsage(err) {
		t.Errorf("err = %v, want ErrUsage (index+onboard)", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestManagerIndex' -v`
Expected: FAIL — `unknown flag: --index`.

- [ ] **Step 3: Add the flag + env + mutual exclusion**

In `internal/cli/manager.go`:

1. Add `Index bool` to `managerOpts`:
```go
type managerOpts struct {
	Project     string
	Actor       string
	Integration string
	Onboard     bool
	Index       bool
	DryRun      bool
	ExtraArgs   []string
}
```

2. In `newManagerAgentCmd` and `newManagerOllamaCmd`, add the flag after `--onboard`:
```go
	cmd.Flags().BoolVar(&opts.Index, "index", false, "indexing run: tail the log, embed new/changed tasks+comments via the host, write vectors (activates ATM_INDEX)")
```

3. In `runManager`, enforce mutual exclusion and select argv + env. After the `s.GetProject` check (or near the onboarding guard), add:
```go
	if opts.Index && opts.Onboard {
		return fmt.Errorf("%w: --index and --onboard are mutually exclusive", ErrUsage)
	}
```
4. Replace the argv selection:
```go
	var base []string
	switch {
	case opts.Onboard:
		base = l.BuildArgvOnboard(contextPath)
	default:
		base = l.BuildArgv()
	}
```
(`--index` uses the interactive `BuildArgv`, same as default — no change needed beyond the switch.)
5. In `managerEnvValues`, add the index signal:
```go
func managerEnvValues(project, atmBin, actor, runID, contextPath string, onboard, index bool) map[string]string {
	m := map[string]string{
		"ATM_ROLE":         "manager",
		"ATM_PROJECT":      project,
		"ATM_BIN":          atmBin,
		"ATM_ACTOR":        actor,
		"ATM_RUN_ID":       runID,
		"ATM_CONTEXT_FILE": contextPath,
	}
	if onboard {
		m["ATM_ONBOARD"] = "1"
	}
	if index {
		m["ATM_INDEX"] = "1"
	}
	return m
}
```
Update the call site in `runManager`:
```go
	envValues := managerEnvValues(opts.Project, atmBin, opts.Actor, runID, contextPath, opts.Onboard, opts.Index)
```
6. Add the tmux label for index (mirror `tmuxLabelOnboarding`):
```go
	if opts.Index {
		setTmuxWindowLabel(os.Stdout, tmuxLabelIndex)
	}
```

In `internal/cli/tmux.go`, add:
```go
const tmuxLabelIndex = "atm-manager-index"
```

- [ ] **Step 4: Run tests, regenerate goldens**

Run: `go test ./internal/cli/ -run 'TestManagerIndex' -v -update`
Then: `go test ./internal/cli/ -run 'TestManagerIndex' -v`
Expected: PASS. Verify the golden JSON includes `"ATM_INDEX": "1"` in env and that argv is the interactive `BuildArgv` (not `--auto --prompt`).

- [ ] **Step 5: Run full manager tests (regression)**

Run: `go test ./internal/cli/ -run TestManager -v`
Expected: PASS (existing `--onboard` tests still green).

- [ ] **Step 6: Commit**

```bash
git add internal/cli/manager.go internal/cli/manager_test.go internal/cli/tmux.go internal/cli/testdata/golden/manager-index-dry-run-*.json
git commit -m "cli: add atm manager --index flag (ATM_INDEX=1, interactive argv, mutual-excludes --onboard) (ATM-0057)"
```

---

## Task 9: manager prompt — indexing, inquiry, evaluation, write-side representation

**Files:**
- Modify: `internal/manager/context_v1.md`
- Modify: `internal/manager/context_test.go`

**Interfaces:**
- Consumes: existing `RenderContext`, `ContextData`, placeholder substitution.
- Produces: four new prompt sections + command cheat-sheet additions; render tests assert their presence/absence.

- [ ] **Step 1: Write the failing render tests**

Append to `internal/manager/context_test.go` (match existing fragment-assertion style):

```go
func TestRenderContextIndexSectionGated(t *testing.T) {
	base := RenderContext(ContextData{Code: "ATM", Name: "Agent Tasks Management", ATMBin: "atm", Actor: "opencode-manager", RunID: "R1", Timestamp: "2026-07-08T00:00:00Z"})
	if strings.Contains(base, "Indexing responsibility") {
		t.Errorf("base render should NOT contain indexing section (ATM_INDEX not set in render)")
	}
	// The indexing section is env-conditional: it is present in the prompt text
	// gated by an `ATM_INDEX` marker the manager reads at runtime. The render
	// itself always contains the section text but wrapped in an env guard.
	// Confirm the section text exists and references ATM_INDEX.
	if !strings.Contains(base, "ATM_INDEX") {
		t.Errorf("prompt should reference ATM_INDEX for the indexing section gate")
	}
}

func TestRenderContextInquirySectionPresent(t *testing.T) {
	base := RenderContext(ContextData{Code: "ATM", Name: "Agent Tasks Management", ATMBin: "atm", Actor: "opencode-manager", RunID: "R1", Timestamp: "2026-07-08T00:00:00Z"})
	for _, frag := range []string{"Inquiry responsibility", "hint: question", "atm search"} {
		if !strings.Contains(base, frag) {
			t.Errorf("prompt missing %q", frag)
		}
	}
}

func TestRenderContextEvalSectionPresent(t *testing.T) {
	base := RenderContext(ContextData{Code: "ATM", Name: "Agent Tasks Management", ATMBin: "atm", Actor: "opencode-manager", RunID: "R1", Timestamp: "2026-07-08T00:00:00Z"})
	for _, frag := range []string{"Evaluation responsibility", "atm eval run", "recall"} {
		if !strings.Contains(base, frag) {
			t.Errorf("prompt missing %q", frag)
		}
	}
}

func TestRenderContextWriteSideRepresentationPresent(t *testing.T) {
	base := RenderContext(ContextData{Code: "ATM", Name: "Agent Tasks Management", ATMBin: "atm", Actor: "opencode-manager", RunID: "R1", Timestamp: "2026-07-08T00:00:00Z"})
	for _, frag := range []string{"ATM:comment:superseded", "retrieval preparation"} {
		if !strings.Contains(base, frag) {
			t.Errorf("prompt missing %q", frag)
		}
	}
}

func TestRenderContextNewCommandsInCheatSheet(t *testing.T) {
	base := RenderContext(ContextData{Code: "ATM", Name: "Agent Tasks Management", ATMBin: "atm", Actor: "opencode-manager", RunID: "R1", Timestamp: "2026-07-08T00:00:00Z"})
	for _, frag := range []string{"atm search", "atm index write-batch", "atm eval run"} {
		if !strings.Contains(base, frag) {
			t.Errorf("cheat sheet missing %q", frag)
		}
	}
}
```

(Add `"strings"` to imports if not present.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/manager/ -run 'TestRenderContext(Index|Inquiry|Eval|WriteSide|NewCommands)' -v`
Expected: FAIL — fragments not found.

- [ ] **Step 3: Edit context_v1.md**

In `internal/manager/context_v1.md`:

a) Add an **Indexing responsibility** section after the **Onboarding responsibility** section, env-gated:

```markdown
## Indexing responsibility

When `ATM_INDEX=1` is set in your environment, you are running as the project's
indexer. The host process you are running in is the persistent carrier; ATM is
invoked only for vector writes and eval runs. Work in a loop:

1. Resolve your host's embedding model to a slug: the model name, lowercased,
   with non-alphanumeric characters replaced by hyphens (e.g.
   `nomic-embed-text`, `text-embedding-3-small`). Use one slug per session; it
   must be deterministic so the same host+model always writes and reads the
   same `vectors/<slug>.jsonl` file.
2. Compute the delta: compare `<ATM_BIN> index status --project <CODE> --output
   json` (the `behind` field) to the project's current log. Identify new and
   changed tasks + comments since the index's `last_log_seq`. A task/comment
   is "changed" when its title/description/body/labels differ from what was
   embedded (detected via text_hash mismatch).
3. For each new/changed task + comment, compose the document text:
   - task: title + " " + description + " " + labels joined by spaces
   - comment: body + " " + labels joined by spaces
   Call your host's embedding capability to produce a vector for the document
   text. Emit a JSONL line:
   `{"id":"...","kind":"task|comment","model":"<slug>","dim":N,"vector":[...],"text_hash":"sha256:...","log_seq":M}`
   where `text_hash` is `sha256:` + the hex of the SHA-256 of the document
   text, and `log_seq` is the task/comment's current `log_seq`.
4. Write the batch with one CLI call:
   `<ATM_BIN> index write-batch --project <CODE> --model <slug> --file <temp.jsonl> --actor <ACTOR>`
5. Run recall measurement:
   `<ATM_BIN> eval run --project <CODE> --model <slug> --output json`
6. If recall@5 dropped below ~0.7 since the last run (compare to prior runs
   via `<ATM_BIN> eval runs --project <CODE> --model <slug>`), re-curate
   before reindexing: rewrite vague titles to name the concept, add missing
   labels, mark superseded decision comments `ATM:comment:superseded`. The
   curation changes document text, which changes text_hash, which makes the
   next batch re-embed the affected items.
7. Sleep until the next batch threshold or a quiet period, then loop from
   step 2.

Pacing is your judgment, not a CLI flag. Do not embed on every single track
call; batch. If the host's embedding capability is unavailable, surface the
error in your session output and retry on the next loop iteration; vectors
already written are kept.

## Inquiry responsibility

When a track request arrives with `hint: question` (or clearly asks "what do I
know about X / has this been done / what blocked last time"), answer from the
knowledge base:

1. Embed the question with your host's model (the same model you index under,
   if you have one).
2. Run `<ATM_BIN> search --model <slug> --query-vector <json> "question" --output json`.
3. Read the ranked hits. Drill into specific ones with `<ATM_BIN> task show
   --id <ID> --output json` or `<ATM_BIN> task comment list --task <ID>
   --output json` if you need more detail.
4. Synthesize a grounded answer that cites the hit IDs you used. If
   `fallback_used` is true or no hits came back, say so explicitly and answer
   from text results or from your general knowledge of the project.
5. Return the answer to the developing agent. Do not block on a reply.

This is the search/query capability the knowledge-base-owner role declared as
forward-compatible. It is now built. Use it.

## Evaluation responsibility

When the human asks in an interactive session, or on a track call with
`eval` hint, author or refresh the eval set:

1. Write representative queries paired with the task/comment IDs each should
   surface, plus a query vector per model you have access to. Emit a JSONL
   line per entry:
   `{"query":"...","relevant_ids":["ATM-0001",...],"query_vectors":{"<slug>":[...]}}`
2. Append via `<ATM_BIN> eval write --project <CODE> --file <temp.jsonl> --actor <ACTOR>`.
3. Grow the eval set from real inquiries: when you field a `hint: question`
   and cite hit IDs, append `{query, relevant_ids: the cited IDs,
   query_vectors: per available model}` so real questions become ground-truth
   test cases.
4. Run `<ATM_BIN> eval run --project <CODE> --model <slug> --k 5 --output json`
   before and after a reindex to detect recall drift. Compare to prior runs
   with `<ATM_BIN> eval runs --project <CODE> --model <slug>`.
5. Target curation at queries that recall poorly: the per-query breakdown
   (in the run record JSON / `atm eval show --id <RUN-ID>`) names which
   queries missed. Rewrite the titles/labels of the tasks those queries
   should have surfaced, then reindex and rerun eval.

The eval set is semi-dynamic: a stable core of hand-picked queries grows from
real usage over time. Do not regenerate it wholesale each reindex — ground
truth must be comparable across runs. Retire stale entries when the underlying
tasks are removed or merged.

Documented convention: recall@5 below ~0.7 is a signal to re-curate before
reindexing. Use judgment, not a hard cutoff.
```

b) Fold **write-side representation** into the existing **Ledger hygiene** section. After the existing bullet about concise/searchable titles, add:

```markdown
- Your curation is retrieval preparation. Good titles, descriptions, and
  labels are what make future `atm search` results relevant. When you touch a
  task, write it so future search finds it: name the concept, not the
  transient activity. Use consistent labels.
- When you supersede a prior decision, mark the old comment with the label
  `ATM:comment:superseded` so stale rulings do not surface at equal rank in
  semantic search alongside the new ruling.
- Prefer per-decision granularity: one decision per comment, or a numbered
  per-decision structure within a single comment, so retrieval can cite
  individual decisions rather than a multi-decision bundle.
```

c) Add the new commands to the **Commands** cheat sheet (after the existing `vocabulary` lines):

```markdown
- `<ATM_BIN> search --model <slug> --query-vector <json> "query" [--kind task|comment|all] [--k 5] [--output json]`
- `<ATM_BIN> index write-batch --project <CODE> --model <slug> --file <jsonl> [--actor <ACTOR>]`
- `<ATM_BIN> index status --project <CODE> [--model <slug>]`
- `<ATM_BIN> index drop --project <CODE> --model <slug> [--actor <ACTOR>]`
- `<ATM_BIN> index models --project <CODE>`
- `<ATM_BIN> eval write --project <CODE> --file <jsonl> [--actor <ACTOR>]`
- `<ATM_BIN> eval run --project <CODE> --model <slug> [--k 5] [--output json]`
- `<ATM_BIN> eval runs --project <CODE> --model <slug>`
- `<ATM_BIN> eval show --id <RUN-ID>`
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/manager/ -run 'TestRenderContext(Index|Inquiry|Eval|WriteSide|NewCommands)' -v`
Expected: PASS.

- [ ] **Step 5: Run full manager tests + build**

Run: `go test ./internal/manager/ -v && go build ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/manager/context_v1.md internal/manager/context_test.go
git commit -m "manager: add indexing, inquiry, evaluation, write-side-representation responsibilities (ATM-0057)"
```

---

## Task 10: developing context — Retrieval section

**Files:**
- Modify: `internal/developing/context_v1.md` (or `context_v*.md` — confirm the active file)
- Modify: `internal/developing/context_test.go`

**Interfaces:**
- Consumes: existing developing context render.
- Produces: a Retrieval section documenting direct `atm search` + `hint: question` dispatch.

- [ ] **Step 1: Confirm the active developing context file**

Run: `rg -l "ATM_ROLE=developing" internal/developing/`
and read the top of the matched file to confirm the section style.

- [ ] **Step 2: Write the failing render test**

Append to `internal/developing/context_test.go` (match its existing style):

```go
func TestRenderContextRetrievalSectionPresent(t *testing.T) {
	got := RenderContext(ContextData{Code: "ATM", Name: "Agent Tasks Management", ATMBin: "atm", Actor: "ollama-dev", RunID: "R1", Timestamp: "2026-07-08T00:00:00Z"})
	for _, frag := range []string{"Retrieval", "atm search", "hint: question"} {
		if !strings.Contains(got, frag) {
			t.Errorf("developing context missing %q", frag)
		}
	}
}
```

(Adjust `ContextData` field names to match the developing package's actual struct.)

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/developing/ -run TestRenderContextRetrievalSectionPresent -v`
Expected: FAIL.

- [ ] **Step 4: Add the Retrieval section**

In the developing context file, add a section (near the working routine):

```markdown
## Retrieval

You have two read surfaces into the project memory:

- **Direct search:** `atm search --model <your-model-slug> --query-vector <json> "query"`.
  Embed the query with your host's embedding model and pass the vector via
  `--query-vector`; ATM searches your model's index (`vectors/<slug>.jsonl`)
  and returns ranked hits. If no index exists for your model or results are
  weak, ATM falls back to local text search automatically. Discover available
  models with `atm index models --project <CODE>`. The slug is your host's
  embedding model name, lowercased with non-alphanumerics replaced by hyphens.
- **Synthesized answer:** dispatch the `atm-manager` subagent with
  `hint: question` followed by your question. The manager runs `atm search`
  inside its host session and returns a grounded answer citing the hit IDs.
  Use this when you want synthesis, not just hits.

Both are read-only; neither blocks your work.
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/developing/ -run TestRenderContextRetrievalSectionPresent -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/developing/context_v1.md internal/developing/context_test.go
git commit -m "developing: add Retrieval section (direct atm search + manager inquiry) (ATM-0057)"
```

---

## Task 11: conventions update

**Files:**
- Modify: `internal/cli/conventions.go`
- Modify: `internal/cli/conventions_test.go`

**Interfaces:**
- Consumes: existing `atm conventions` output + golden.
- Produces: `ATM:comment:superseded` convention row + search/index/eval commands in the first-contact sequence.

- [ ] **Step 1: Write the failing golden test**

Run the existing conventions test to see the current golden path:
`go test ./internal/cli/ -run TestConventions -v`
Then append a test that asserts the new fragments (or update the existing golden):

```go
func TestConventionsIncludesMemorySubstrate(t *testing.T) {
	got := newGoldenHarness(t).run(t, "conventions --output json")
	compareGolden(t, "conventions-memory", got)
}
```

(If `TestConventions` already covers the whole output via one golden, just plan to update that golden after editing.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestConventions -v`
Expected: FAIL (new fragments missing) OR golden mismatch after edit.

- [ ] **Step 3: Edit conventions.go**

In `internal/cli/conventions.go`, add a row to the suggested-seed-namespace table:

```markdown
| `comment:superseded` | `ATM:comment:superseded` | mark a decision comment that a newer comment supersedes, so stale rulings do not surface at equal rank in semantic search |
```

And add to the agent first-contact sequence (after the existing `task list --label <CODE>:status:open` line):

```markdown
5. `atm index models --project <CODE>` — see which embedding models have a vector index.
6. `atm search --model <your-slug> --query-vector <json> "query"` — semantic search over tasks + comments (text fallback if no index).
7. To get a synthesized answer, dispatch `atm-manager` with `hint: question`.
```

- [ ] **Step 4: Regenerate golden + run tests**

Run: `go test ./internal/cli/ -run TestConventions -v -update`
Then: `go test ./internal/cli/ -run TestConventions -v`
Expected: PASS.

- [ ] **Step 5: Run full verify**

Run: `make verify`
Expected: PASS (build + all tests).

- [ ] **Step 6: Commit**

```bash
git add internal/cli/conventions.go internal/cli/conventions_test.go internal/cli/testdata/golden/conventions*.json
git commit -m "conventions: document comment:superseded + memory-substrate first-contact commands (ATM-0057)"
```

---

## Task 12: final verify

- [ ] **Step 1: Full verify**

Run: `make verify`
Expected: PASS (build + all tests green).

- [ ] **Step 2: Manual smoke (optional, record results as a comment on ATM-0057)**

```sh
atm manager opencode --project ATM --index --dry-run   # confirm ATM_INDEX=1 + interactive argv
atm manager opencode --project ATM --onboard --index --dry-run   # confirm ErrUsage
atm search --project ATM --model nomic-embed-text --query-vector "[0.1,0.2]" "label"   # text fallback (no index)
atm index models --project ATM
```

- [ ] **Step 3: Record completion on ATM-0057**

Dispatch `atm-manager` with `hint: progress` noting all 12 tasks landed, `make verify` green, smoke results. Do not mark the task done until the human confirms.