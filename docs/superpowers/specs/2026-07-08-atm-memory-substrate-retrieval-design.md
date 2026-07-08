# ATM as Memory Substrate: Retrieval Surface + Indexing Model + Manager Cognition — Design Spec

**Status:** Draft (awaiting user review)
**Date:** 2026-07-08
**Tracking:** ATM-0057.
**Depends on:** `2026-07-02-tasks-management-v2-design.md` (label substrate, no intrinsic
workflow knowledge), `2026-07-04-audit-log-redesign-design.md` (log.jsonl source of truth,
derived caches), `2026-07-05-task-comments-v1-design.md` (comments as narrative entity),
`2026-07-06-atm-manager-subagent-design.md` (manager subagent + interactive launcher),
`2026-07-08-manager-knowledge-base-onboarding-unification-design.md` (manager as
knowledge-base owner: ledger + ubiquitous language + context map; declared inquiry/search
as forward-compatible remit).
**Does NOT depend on:** `2026-07-06-atm-storage-sync-design.md` (`cache.db`). This spec
deliberately stores vectors and eval artifacts as per-project files, decoupled from the
storage-sync spec's `cache.db` consolidation. The two specs are independent.

## Driver

ATM is already a memory substrate on the **contribution** side: agents journal everything
into tasks, comments, vocabulary, and labels, and the manager sorts it out. What is missing
is **retrieval** and **representation awareness**. Today the only read surface is
`task list` / `task show` / `label list` / `vocabulary show` — flat listings, no semantic
search, no cross-task synthesis, no "retrieve the memory relevant to what I am about to do."
The manager-as-knowledge-base-owner reframe declared inquiry/search as the manager's remit
but deferred it as "a future capability." This spec closes that gap.

The consolidation does **not** add new memory kinds. Tasks + comments + vocabulary + labels
remain the substrate. Relationships between entities stay implicit in the prose (v2
deliberately removed structural `Links`; this spec does not revive them). Instead, this spec
**powers up the manager** into a synthesizing inquiry engine over the existing ledger, gives
the developing agent a direct semantic search surface, and makes the manager's write-side
curation retrieval-aware. ATM stays a "dump join" — agents journal everything; the manager
sorts, organizes, and makes it retrieval-ready.

### The principle that shapes every decision

ATM the binary is a **thin coordinator**: it stores vectors, runs cosine search, computes
recall — all pure Go, offline, deterministic. It **never calls a model.** Embeddings come
from the **host agent's** embedding capability (ollama's `nomic-embed-text`, OpenAI's
`text-embedding-3-small`, etc.), accessed by the manager during manager sessions and by the
developing agent's host at search time. This mirrors how `vocabulary.json` already works
(the manager computes vocabulary using its language understanding and writes via CLI);
vectors are the same pattern one layer down (the manager uses the host's *embedding*
capability and writes via CLI).

## Decisions (locked during brainstorming)

| # | Decision |
|---|----------|
| D1 | **No new memory kinds.** Tasks + comments + vocabulary + labels stay the substrate. No free-floating note entity. No structural `Links` revival. Relationships stay implicit in prose; the manager's cognition digests them at retrieval time. |
| D2 | **Both developing agent and manager are first-class retrievers.** The developing agent has a direct CLI search; the manager has an inquiry-synthesis capability via track dispatch. Both read the same vector indexes. |
| D3 | **Retrieval is two surfaces.** (a) Direct CLI search returning ranked semantic hits (offline, deterministic). (b) Manager-dispatch synthesis on `hint: question`: the manager runs `atm search` itself, reads hits, synthesizes a grounded answer citing hit IDs. Search returns ranked hits only — no synthesis from the CLI path (preserves the offline/deterministic CLI invariant). |
| D4 | **Search semantics: semantic/embedding search** (not keyword-only). Per-project, per-model vector indexes. Cosine similarity. |
| D5 | **Embeddings come from the host agent's embedding capability.** ATM the binary never calls a model. The manager (during an index session) and the developing agent's host (at search time) embed text using the host's model and hand vectors to ATM via CLI. |
| D6 | **Vector storage = per-project, per-model JSONL files** at `$ATM_HOME/projects/<CODE>/vectors/<model-slug>.jsonl`. NOT inside `cache.db`. Decouples from the storage-sync spec. Multiple models coexist as parallel index files. Each indexing process owns its model's file. |
| D7 | **Multi-host is first-class.** Each host indexes under its own model; a developing agent searches only its own model's index (space-mismatch can't happen — it never searches a different model's space). No "one designated indexing host" restriction. |
| D8 | **Query embedding: the caller's host embeds the query and passes the vector via `--query-vector`.** `atm search --model <slug> --query-vector <json> "query text"`. The text string is also passed for the text-fallback path. ATM never embeds. |
| D9 | **Text-search fallback.** When (a) no vector index exists for the current host's model, OR (b) the top cosine score is below a relevance threshold (weak/no semantic matches), `atm search` falls back to local full-text matching (token overlap + recency ranking) over tasks + comments. Text hits are marked `match:"text"` / `kind:text-fallback`. Pure Go, offline, always available even with zero indexes. Gracefully degradable instead of binary works/broken. |
| D10 | **Indexing happens during manager sessions via a new `--index` runtime mode.** `atm manager <host> --project <CODE> --index` sets `ATM_INDEX=1`, activating an indexing responsibility section in the one manager prompt (parallel to `--onboard` / `ATM_ONBOARD=1`). The host process is the persistent carrier (it tails the log, batches, embeds, writes via CLI, sleeps); ATM stays daemon-free. `--index` and `--onboard` are mutually exclusive. |
| D11 | **Index scope: tasks + comments** (two vector kinds). Vocabulary is not embedded (12 terms, already a flat lookup). |
| D12 | **Vector write path: CLI, batched.** The manager emits a temp JSONL of `{id, kind, model, dim, vector, text_hash, log_seq}` and hands it to `atm index write-batch --model <slug> --file <jsonl>`, which does one transactional append into `vectors/<slug>.jsonl` + meta update. The manager stays a pure CLI actor; ATM is the only writer to vector files. Direct DB/file access by the manager was rejected (it would reintroduce multi-writer consistency hazards and couple the manager to file internals). |
| D13 | **Manager inquiry path: the manager runs `atm search` itself** via bash on a `hint: question` dispatch (mirroring how it already runs `atm task list` / `atm task comment list`), reads the ranked hits, and synthesizes a grounded answer citing hit IDs. One search surface (the CLI), used by both developing agent and manager. The manager stays a pure CLI actor. |
| D14 | **Write-side representation: teach the manager in the prompt, no new write commands.** An "Indexing responsibility" section + folds into "Ledger hygiene": good titles/descriptions/labels *are* retrieval preparation; rewrite vague titles to name the concept; use consistent labels; when superseding a prior decision, mark the old comment `ATM:comment:superseded` (the ATM-0062 convention) so stale rulings don't surface at equal rank in semantic search. |
| D15 | **Recall measurement is included in v1.** An eval set (queries + known-relevant task/comment IDs + per-model query vectors), `atm eval run` computing recall@k + precision@k (pure Go, offline), and a manager evaluation responsibility. The eval set grows from real `hint: question` inquiries (query + cited-hit-IDs appended). Eval is per-model; the eval set is shared ground truth so models are compared directly. |
| D16 | **The improvement loop.** Measure → detect gaps → curate → reindex → measure again. Automatic *within one model's space* (the manager re-curates vague titles when recall drops, detected by `atm eval run` at the start of an index pass). Deliberate *across models* (switching a host's embedding model = that host reindexes its own file under the new model, eval-compared against the stable eval set; other hosts' indexes untouched). The embedding model is never auto-switched inside a reindex cycle — cross-model comparison needs a clean full-reindex + stable eval set. |
| D17 | **Eval set is semi-dynamic, not wholesale-regenerated.** Initial set is hand-picked by the human/manager (representative queries + known-relevant IDs). It grows from real inquiries over time. The manager can retire stale entries when underlying tasks are removed/merged. Ground truth must be stable enough to compare runs against; if it churned every cycle the recall number would be meaningless. |
| D18 | **Embedding model = the host's text→vector converter.** Model choice is per-host, recorded as a slug in each vector file and eval run record. Different models produce vectors in different spaces; cosine similarity is only meaningful within one model's space. The design prevents silent space-mismatch: a host only ever searches its own model's index file. |
| D19 | **Self-improvement gene ATM-0060 adopted as a write-side convention.** Per-decision track-call/comment granularity aids retrieval (a 13-decision bundle yields one retrieval hit; one decision per comment yields per-decision citation). The manager prompt encourages numbered, per-decision structure. |

## Section 1: Architecture & data flow

### File layout (additive to the existing store)

```
$ATM_HOME/
  projects/
    <CODE>/
      log.jsonl                          # source of truth (unchanged)
      vocabulary.json                    # ubiquitous language (unchanged)
      vectors/                           # NEW — per-model semantic indexes
        <model-slug>.jsonl               # one JSON object per line per embedded task/comment
        <model-slug>.meta.json           # model_id, dim, last_reindexed_at, last_log_seq, count
      eval/                              # NEW — recall measurement
        evalset.jsonl                    # queries + relevant IDs + per-model query vectors
        runs/<model-slug>/<timestamp>.json  # eval run records (recall@k, precision@k, per-query breakdown)
```

No `cache.db` dependency. Everything new is per-project, derived, disposable, and
**sync-excluded** (same rule as `vocabulary.json` — derived artifacts don't sync; the log is
the source of truth, and a freshly-pulled machine rebuilds its own vectors/eval by
reindexing).

### Vector file shape (`vectors/<model-slug>.jsonl`, one JSON object per line)

```json
{"id":"ATM-0042","kind":"task","model":"nomic-embed-text","dim":768,"vector":[0.012,...],"text_hash":"sha256:...","log_seq":1287}
{"id":"ATM-0042-c0003","kind":"comment","model":"nomic-embed-text","dim":768,"vector":[...],"text_hash":"...","log_seq":1290}
```

- `id`: task ID (`<CODE>-<NNNN>`) or comment ID (`<CODE>-<NNNN>-c<NNNN>`).
- `kind`: `task` or `comment`.
- `model`: the embedding model slug (matches the filename).
- `dim`: vector dimensionality (stored per-entry for validation; must be consistent within a file).
- `vector`: the embedding (float array).
- `text_hash`: SHA-256 of the embedded document text (task: title + description + labels;
  comment: body + labels). Detects staleness: if the task/comment body changed since
  embedding, the hash mismatches and the vector is skipped at search time, flagged at reindex.
- `log_seq`: the last log seq the embedding reflects; the indexer compares it to
  `LastLogSeq(code)` to find what's new/changed.

### Vector meta shape (`vectors/<model-slug>.meta.json`)

```json
{"model":"nomic-embed-text","dim":768,"last_log_seq":1287,"last_reindexed_at":"2026-07-08T12:00:00Z","count":42}
```

### Indexing data flow (`atm manager <host> --project <CODE> --index`, `ATM_INDEX=1` active)

```
host process (manager prompt, indexing responsibility active)
  -> reads ATM_PROJECT, ATM_BIN, ATM_ACTOR, ATM_INDEX from env
  -> resolves host's embedding model -> model-slug (e.g. "nomic-embed-text")
  -> loop:
       1. LastLogSeq(code) vs vectors/<slug>.meta.json last_log_seq -> delta set
          (new tasks/comments + changed ones detected by text_hash mismatch)
       2. for each new/changed task+comment in the delta:
            - compose document text (task: title+desc+labels; comment: body+labels)
            - call host embedding capability -> vector
            - append {id, kind, model, dim, vector, text_hash, log_seq} to a temp jsonl
       3. atm index write-batch --model <slug> --file <temp.jsonl>
          (one CLI call; atomic append into vectors/<slug>.jsonl + meta update)
       4. atm eval run --model <slug>  (cheap, offline; records a run)
       5. if recall@5 dropped below threshold since last run:
            re-curate (rewrite vague titles, fix labels, mark superseded comments)
            then loop again (the curation changes text_hash -> triggers re-embed)
       6. sleep until next batch threshold or quiet period
```

The host process is the persistent carrier. ATM the binary is invoked only for steps 3 and 4
(short-lived CLI calls). ATM stays daemon-free.

### Search data flow (`atm search`, called by developing agent's host OR by manager in inquiry)

```
caller (host with model M)
  -> embeds query string with M -> query vector
  -> atm search --model <slug> --query-vector <json> "query text" [--k 5] [--kind task|comment|all]
  -> ATM:
       1. read vectors/<slug>.jsonl; skip stale (text_hash mismatch vs current task/comment text)
          and malformed lines
       2. cosine similarity query vector vs each valid entry -> ranked, top-K
       3. if no index for <slug> OR top score < threshold (default 0.30):
            TEXT FALLBACK
            - local full-text match over tasks+comments (token overlap + recency ranking)
            - hits marked match:"text" / kind:text-fallback
       4. return ranked hits: [{id, kind, score, title/body-snippet, labels, match}]
```

### Manager inquiry data flow (developing agent dispatches `atm-manager` with `hint: question`)

```
manager (in its host, model M available)
  -> reads the question
  -> embeds it with M -> query vector
  -> atm search --model <slug> --query-vector <json> "question" --output json
  -> reads ranked hits (drill into specific ones via atm task show / atm task comment list if needed)
  -> synthesizes grounded answer citing hit IDs
  -> returns answer to developing agent (does not block on a reply)
```

If no index exists for the manager's host model or results are weak, the manager falls back
to text results and says so in its answer.

### Eval data flow

```
authoring (manager, interactive mode or `eval` hint):
  -> for each representative query: write {query, relevant_ids[], query_vectors: {<slug>: [<floats>]}}
  -> atm eval write --file <jsonl>  (appends to eval/evalset.jsonl)
  -> also appends real inquiries: when the manager fields a hint:question and cites hit IDs,
     it appends {query: the question, relevant_ids: the cited IDs, query_vectors: per available model}

running (manager at start of index pass, OR human directly):
  -> atm eval run --model <slug> [--k 5]
  -> ATM: for each eval entry, cosine-search vectors/<slug>.jsonl with query_vectors[<slug>],
     compute recall@k + precision@k vs relevant_ids
  -> write run record to eval/runs/<slug>/<timestamp>.json
  -> print summary (text) or per-query breakdown (json)

reviewing:
  -> atm eval runs --project <CODE> --model <slug>   (time series of recall for that model)
  -> atm eval show --id <RUN-ID>                      (per-query breakdown of one run)
```

### Key invariants preserved

- ATM the binary never calls a model. Hosts embed; ATM stores and searches.
- The log is the source of truth. Vectors and eval are derived/disposable — `atm store
  rebuild` drops them; reindexing regenerates.
- Per-project isolation. No cross-project search.
- Space-mismatch can't happen: a host only ever searches its own model's index file.
- Offline/deterministic CLI: `atm search`, `atm eval run` are pure Go reading local files.
- The closed action enum and replay semantics (audit-log v1) are untouched — vectors/eval
  are not log events; they are derived artifacts (like `vocabulary.json`).

## Section 2: CLI surface

### `atm search`

```
atm search --model <slug> --query-vector <json> "query text"
           [--project <CODE>] [--kind task|comment|all] [--k 5]
           [--threshold <float>] [--output json|text]
```

- `--model <slug>`: which model's index to search (selects `vectors/<slug>.jsonl`). Required.
- `--query-vector <json>`: JSON array of floats, the query embedded by the caller's host
  with `--model`. Required for the semantic path.
- `"query text"` (positional): the raw query string. Used for the text-fallback path and for
  snippet/match highlight in results.
- `--kind`: restrict to `task` or `comment` hits (default `all`).
- `--k`: top-K (default 5).
- `--threshold`: cosine threshold below which semantic hits are considered weak and text
  fallback triggers (default 0.30, configurable).
- `--project` resolved from `ATM_PROJECT` if absent (consistent with other commands).

**Result shape (JSON):**

```json
{
  "query": "label conflicts",
  "model": "nomic-embed-text",
  "match": "semantic",
  "hits": [
    {"id":"ATM-0042","kind":"task","score":0.82,"title":"...","snippet":"...","labels":["ATM:type:feature"]}
  ],
  "fallback_used": false
}
```

When fallback fires: `"match":"text"`, `"fallback_used":true`, scores are token-overlap-based.

**Text output:** one row per hit: `<ID>\t<kind>\t<score>\t<match>\t<title/snippet>`, with a
header line `MODEL: <slug>  MATCH: semantic|text|fallback  K: 5`.

### `atm index`

```
atm index write-batch --project <CODE> --model <slug> --file <jsonl> [--actor <id>]
atm index status       --project <CODE> [--model <slug>] [--output json|text]
atm index drop         --project <CODE> --model <slug> [--actor <id>]
atm index models       --project <CODE> [--output json]
```

- `write-batch`: one transactional append into `vectors/<slug>.jsonl` + meta update
  (`last_log_seq`, `last_reindexed_at`, `count`, `dim`). Validates each entry's `model`
  matches `--model` and `dim` is consistent. The manager's write target.
- `status`: reports per-model `last_log_seq` vs current `LastLogSeq(code)`, stale count
  (text_hash mismatches), so the manager or human sees how stale each index is. Drives the
  "needs reindex" decision.
- `drop`: deletes one model's vector file + meta (used when switching a host's model or
  pruning a stale model).
- `models`: lists which models have indexes for the project (so a developing agent/host can
  discover "is there an index for my model?").

### `atm eval`

```
atm eval write --project <CODE> --file <jsonl> [--actor <id>]
atm eval run   --project <CODE> --model <slug> [--k 5] [--output json|text]
atm eval runs  --project <CODE> --model <slug> [--output json]
atm eval show  --id <RUN-ID> [--output json]
```

- `write`: appends eval entries to `eval/evalset.jsonl`. Each entry:
  `{query, relevant_ids[], query_vectors: {<model-slug>: [<floats>]}}` — the manager writes
  query vectors per model it has access to.
- `run`: for each eval entry, cosine-search `vectors/<slug>.jsonl` with the stored
  `query_vectors[<slug>]`, compute recall@k + precision@k vs `relevant_ids`, write a run
  record to `eval/runs/<slug>/<timestamp>.json`, print summary. Entries lacking
  `query_vectors[<slug>]` are skipped and reported in the run record.
- `runs`: time series of run records for a model (timestamp, recall@5, precision@5).
- `show`: full per-query breakdown of one run.

### `atm manager ... --index`

```
atm manager <host> --project <CODE> --index [--actor <id>] [--dry-run]
```

- Parallel to `--onboard`. Sets `ATM_INDEX=1` in the child env. The manager prompt's
  indexing responsibility section activates. The host process runs the indexing loop.
- `--dry-run` prints argv+env (including `ATM_INDEX=1`) and exits without launching.
- `--index` and `--onboard` are mutually exclusive (a session is either onboarding, indexing,
  or interactive — not two at once).
- The interactive argv (`BuildArgv`) is used, not `BuildArgvOnboard` — the indexer is an
  interactive long-running host session, just with a different responsibility active.

## Section 3: Manager prompt changes (`internal/manager/context_v1.md`)

Four additions, all consistent with the existing env-conditional section pattern:

### 3.1 Indexing responsibility (active when `ATM_INDEX=1`)

Documents the tail-log → batch → embed-via-host → `atm index write-batch` → `atm eval run`
→ curate-if-recall-dropped → sleep loop. Names the host's embedding capability as the
engine. Names the model-slug convention: the slug is the host's embedding model name,
lowercased with non-alphanumeric characters replaced by hyphens (e.g. `nomic-embed-text`,
`text-embedding-3-small`). The manager derives and uses one slug per session; it must be
deterministic so the same host+model always writes and reads the same file. Documents the
batch threshold / quiet-period pacing (left to the manager's judgment, not a CLI flag).
Documents that the host process is the persistent carrier and ATM is invoked only for the
`write-batch` and `eval run` CLI calls.

### 3.2 Inquiry responsibility (active in all modes — subagent, interactive, index)

On a `hint: question` track call, the manager runs `atm search` (embedding the query with
the host's model, passing `--query-vector` and `--model`), reads the ranked hits, synthesizes
a grounded answer citing hit IDs. If no index exists for the host's model or results are
weak, falls back to text results and says so. This is the "search/query capability" the
knowledge-base-owner reframe declared as forward-compatible — now built.

### 3.3 Evaluation responsibility (active in interactive mode and on `eval` hint)

Author/refresh the eval set (representative queries + relevant IDs + query vectors per
available model). Run `atm eval run` before/after reindex to detect recall drift. Target
curation at queries that recall poorly. Documented convention: recall@5 below ~0.7 →
re-curate before reindexing. Append real `hint: question` inquiries (query + cited hit IDs)
to grow the eval set from actual usage.

### 3.4 Write-side representation (folds into the existing Ledger hygiene section, all modes)

Teach the manager that its curation *is* retrieval preparation:
- Good titles/descriptions/labels make future search work; rewrite vague titles to name the
  concept, not the transient activity.
- Use consistent labels; the reindex step makes curation searchable.
- When superseding a prior decision, mark the old comment `ATM:comment:superseded` (the
  ATM-0062 convention) so stale rulings don't surface at equal rank in semantic search.
- Per-decision granularity (ATM-0060): prefer one decision per comment / numbered per-decision
  structure so retrieval can cite individual decisions.

No new write commands — the manager already creates tasks/comments/labels.

### Command cheat-sheet additions

```
<ATM_BIN> search --model <slug> --query-vector <json> "query" [--kind task|comment|all] [--k 5] [--output json]
<ATM_BIN> index write-batch --project <CODE> --model <slug> --file <jsonl> [--actor <ACTOR>]
<ATM_BIN> index status --project <CODE> [--model <slug>]
<ATM_BIN> index drop --project <CODE> --model <slug> [--actor <ACTOR>]
<ATM_BIN> index models --project <CODE>
<ATM_BIN> eval write --project <CODE> --file <jsonl> [--actor <ACTOR>]
<ATM_BIN> eval run --project <CODE> --model <slug> [--k 5]
<ATM_BIN> eval runs --project <CODE> --model <slug>
<ATM_BIN> eval show --id <RUN-ID>
```

## Section 4: Developing context changes (`internal/developing/context_v*.md`)

Add a short **Retrieval** section so the developing agent knows its read surface:

- **Direct:** `atm search --model <your-model-slug> --query-vector <json> "query"` — you
  embed the query with your host's model; ATM searches your model's index; text fallback if
  no index or weak hits. Discover available models with `atm index models`.
- **Synthesized:** dispatch `atm-manager` with `hint: question` for a grounded, synthesized
  answer citing hit IDs.
- Both are read-only; neither blocks your work.

The developing agent is responsible for knowing its own model slug (it can ask its host or
the user). The developing context documents the convention but does not hardcode a model.

## Section 5: Store layer (`internal/store/`)

### `internal/store/vectors.go` (new)

```go
type VectorEntry struct {
    ID       string    `json:"id"`        // task ID or comment ID
    Kind     string    `json:"kind"`      // "task" | "comment"
    Model    string    `json:"model"`     // model slug (matches filename)
    Dim      int       `json:"dim"`
    Vector   []float64 `json:"vector"`
    TextHash string    `json:"text_hash"` // sha256 of embedded document text
    LogSeq   int       `json:"log_seq"`
}

type VectorMeta struct {
    Model          string    `json:"model"`
    Dim            int       `json:"dim"`
    LastLogSeq     int       `json:"last_log_seq"`
    LastReindexedAt time.Time `json:"last_reindexed_at"`
    Count          int       `json:"count"`
}

func (s *Store) ReadVectors(code, slug string) ([]VectorEntry, error)   // best-effort; skips malformed lines
func (s *Store) WriteVectorBatch(code, slug string, entries []VectorEntry, lastLogSeq int) error // lock-guarded, atomic append + meta update
func (s *Store) VectorMeta(code, slug string) (*VectorMeta, error)      // nil, nil if absent
func (s *Store) DropVectors(code, slug string) error                    // lock-guarded
func (s *Store) ListVectorModels(code string) ([]string, error)         // slugs present in vectors/
```

Path helpers `vectorsDir(code)` and `vectorPath(code, slug)` in `store.go`, mirroring
`tasksDir` / `taskPath`. Writes are lock-guarded (per-project lock) and fsynced like other
store writes. Reads are lock-free (best-effort).

### `internal/store/eval.go` (new)

```go
type EvalEntry struct {
    Query        string             `json:"query"`
    RelevantIDs  []string           `json:"relevant_ids"`
    QueryVectors map[string][]float64 `json:"query_vectors"` // keyed by model slug
}

type EvalRun struct {
    ID         string          `json:"id"`          // <slug>-<timestamp>
    Model      string          `json:"model"`
    K          int             `json:"k"`
    RecallAtK  float64         `json:"recall_at_k"`
    PrecisionAtK float64       `json:"precision_at_k"`
    PerQuery   []EvalQueryResult `json:"per_query"`
    Skipped    int             `json:"skipped"`     // entries lacking query_vectors[<slug>]
    RunAt      time.Time       `json:"run_at"`
}

type EvalQueryResult struct {
    Query      string  `json:"query"`
    HitIDs     []string `json:"hit_ids"`      // top-k returned
    RelevantIDs []string `json:"relevant_ids"`
    Recall     float64 `json:"recall"`
    Precision  float64 `json:"precision"`
}

func (s *Store) ReadEvalSet(code string) ([]EvalEntry, error)
func (s *Store) AppendEvalEntries(code string, entries []EvalEntry) error  // lock-guarded
func (s *Store) RecordEvalRun(code, slug string, run *EvalRun) error
func (s *Store) ListEvalRuns(code, slug string) ([]EvalRun, error)        // sorted by RunAt
func (s *Store) GetEvalRun(code, id string) (*EvalRun, error)
```

### `internal/store/search.go` (new — the pure-Go engine)

```go
type Hit struct {
    ID       string  `json:"id"`
    Kind     string  `json:"kind"`        // "task" | "comment" | "text-fallback"
    Score    float64 `json:"score"`
    Title    string  `json:"title,omitempty"`       // for tasks
    Snippet  string  `json:"snippet"`               // body/comment snippet with match highlight
    Labels   []string `json:"labels,omitempty"`
    Match    string  `json:"match"`      // "semantic" | "text"
}

type SearchParams struct {
    Project      string
    Model        string
    QueryVector  []float64
    QueryText    string
    Kind         string  // "task" | "comment" | "all"
    K            int
    Threshold    float64
}

func (s *Store) Search(p SearchParams) (hits []Hit, fallbackUsed bool, err error)
```

`Search` reads vectors via `ReadVectors`, skips stale entries (text_hash mismatch vs current
task/comment text), computes cosine similarity, ranks, and if no index or top score <
threshold runs the text-fallback path (token overlap + recency over tasks + comments loaded
via existing store reads). Returns `fallbackUsed` so the CLI can mark the result.

### `rebuild.go` / `verify.go`

- `Rebuild()` drops `vectors/` and `eval/` (derived; regenerated by reindex). The existing
  task/comment cache rebuild is unchanged.
- `Verify()` reports `vectors/` and `eval/` absence as info-level, not errors (they're
  optional derived artifacts, unlike task/comment caches which are required). If present,
  reports stale-entry counts per model.

### Sync exclusion

`vectors/` and `eval/` are derived and never synced — consistent with `vocabulary.json`'s
treatment in the storage-sync spec. A freshly-pulled machine has no vectors until its own
host reindexes. The log remains the source of truth.

## Section 6: Error handling

- `atm search` with no `vectors/<slug>.jsonl` → semantic path skipped, text fallback fires,
  `match:"text"`, `fallback_used:true`. Not an error (exit 0).
- `atm search` with `--model <slug>` but no `--query-vector` → `ErrUsage` (2). Text-only
  search is not a separate command — fallback is automatic.
- `atm search` with a `--query-vector` whose dimension ≠ the index's `dim` → `ErrUsage` (2)
  with a clear message (space-mismatch prevention at the CLI boundary, in addition to the
  per-model-file isolation).
- `atm index write-batch` with an entry whose `model` ≠ `--model` → `ErrUsage`, whole batch
  rejected (atomic; no partial write).
- `atm index write-batch` with entries of inconsistent `dim` → `ErrUsage`.
- `atm index drop` on a missing model file → `ErrNotFound` (3).
- `atm eval run --model <slug>` with no vector index for that model → `ErrNotFound` (3) ("no
  index for model `<slug>`; run `atm manager <host> --index` first").
- `atm eval run` with an eval entry whose `query_vectors[<slug>]` is absent → that entry is
  skipped, reported in the run record as `skipped: no query vector for model`, not fatal.
- Stale vectors (`text_hash` mismatch) → skipped at search time, counted in `atm index
  status` as `stale: N`. Not an error.
- `atm manager <host> --index --onboard` (both set) → `ErrUsage` (2).
- Host embedding capability unavailable during an index session → the manager surfaces the
  error in its session output and retries on the next loop iteration; partial vectors
  already written are kept (the batch is atomic per `write-batch` call, not per session).
- Malformed `vectors/<slug>.jsonl` line → skipped with a warning to stderr; search continues
  over valid lines (best-effort, mirroring how the audit log tolerates malformed lines).

## Section 7: Testing, verification & rollout

### Testing approach

Same layered structure as v2 / comments / audit-log v1: unit tests per store file, CLI tests
via the `testdata/golden/` pattern, prompt-render tests.

| Area | Invariants |
|---|---|
| `store/vectors_test.go` | `WriteVectorBatch` appends atomically + updates meta; `ReadVectors` returns entries skipping malformed lines; `text_hash` staleness detection; `DropVectors` removes file + meta; `ListVectorModels` discovers slugs; lock-guarded writes; per-project isolation; model-mismatch and dim-mismatch rejection. |
| `store/eval_test.go` | `AppendEvalEntries` appends; `RecordEvalRun` writes a run record; `ListEvalRuns` returns time series sorted by RunAt; `GetEvalRun` by ID; skipped-entries reported. |
| `store/search_test.go` | Cosine ranking over a fixture vector file returns correct top-K; `--kind` filter works; stale entries skipped; threshold triggers text fallback; text fallback ranks by token overlap + recency; empty/missing index → text fallback; `Hit` shape correct; dim-mismatch query rejected. |
| `cli/search_test.go` | Golden text + JSON for semantic-match, text-fallback, empty, `--kind` filter, `--k`; `--query-vector` required with `--model`; missing model file → fallback, not error; dim-mismatch → `ErrUsage`. |
| `cli/index_test.go` | `write-batch` atomic append + meta update; `status` reports staleness + stale count; `drop` removes; `models` lists; model-mismatch and dim-mismatch in batch rejected; `--actor` required on mutating verbs. |
| `cli/eval_test.go` | `write` appends; `run` computes recall@k/precision@k from a fixture, writes run record, prints summary; `runs` time series; `show` per-query breakdown; missing index → `ErrNotFound`; skipped entries reported. |
| `cli/manager_test.go` | `--index` sets `ATM_INDEX=1` + selects `BuildArgv` (interactive, not `BuildArgvOnboard`); `--index --onboard` → `ErrUsage`; `--index --dry-run` prints env incl. `ATM_INDEX=1`; regression: `--onboard` path unbroken. |
| Manager prompt render | With `ATM_INDEX=1` the indexing responsibility section is present; without it, inert. Inquiry + evaluation + write-side-representation sections present in all renders (mode-agnostic). Command cheat-sheet includes the new verbs. |
| Developing context render | Retrieval section present; documents both direct `atm search` and `hint: question` dispatch paths. |

**Verification gate:** `make verify` (`make build && make test`) unchanged. No new make
targets. `.golangci.yml` unchanged.

### Rollout (layered commits, each green; strictly additive — no existing types/actions removed)

1. `store/vectors.go` + tests (path helpers, read/write-batch, meta, drop, list-models).
2. `store/eval.go` + tests (eval set append, run record write/list/get).
3. `store/search.go` + tests (cosine engine + text fallback + `Hit` shape).
4. `cli/search.go` + output mappers + golden tests.
5. `cli/index.go` + golden tests (`write-batch`, `status`, `drop`, `models`).
6. `cli/eval.go` + golden tests (`write`, `run`, `runs`, `show`).
7. `cli/manager.go` `--index` flag + `ATM_INDEX=1` env + tests.
8. `internal/manager/context_v1.md` rewrite: add indexing, inquiry, evaluation,
   write-side-representation sections + command cheat-sheet additions + render tests.
9. `internal/developing/context_v*.md` Retrieval section + render test.
10. `rebuild.go`/`verify.go`: drop `vectors/`+`eval/` on rebuild; info-level in verify.
11. Conventions update: document the `ATM:comment:superseded` label convention (ATM-0062) in
    `atm conventions` output; add the search/index/eval commands to the agent first-contact
    sequence.

**Compatibility:** fully additive. No log action changes, no entity schema changes, no
existing command changes. Existing stores work unchanged; `vectors/` and `eval/` simply
don't exist until a manager indexes. No migration tooling.

## Out of scope (v1)

- **New memory entities** (notes, decisions, Q&A pairs, learned patterns as distinct kinds).
  Tasks + comments + vocabulary + labels remain the substrate.
- **Structural relationship fields** (no `Links` revival). Relationships stay implicit in
  prose; the manager digests them at retrieval time.
- **ATM the binary calling any model.** Embeddings come from the host during manager
  sessions and from the developing agent's host at search time; ATM only stores and searches.
- **Cross-project search.** Per-project, matching the existing no-cross-project invariant.
- **A real persistent daemon owned by ATM.** The host process is the persistent carrier; ATM
  stays daemon-free.
- **TUI search UI.** CLI-only in v1. The TUI is a reader of vectors only for an eventual
  chart, not in this spec.
- **Auto-switching the embedding model inside a reindex cycle.** Cross-model comparison needs
  a clean full-reindex + stable eval set; the switch is a deliberate, eval-compared action.
- **A separate text-only search command.** Text fallback is automatic within `atm search`,
  not a separate verb.
- **Cross-model result merging.** Each host searches only its own model's index; results are
  never merged across models.
- **Embedding a fixed/precomputed query index.** Ad-hoc queries are supported via the
  caller-embeds-and-passes-vector path; pre-embedding queries is rejected (useless for real
  retrieval).
- **Whole-store (multi-project) search or eval.** Single project per invocation, consistent
  with the existing no-cross-project-atomicity invariant.