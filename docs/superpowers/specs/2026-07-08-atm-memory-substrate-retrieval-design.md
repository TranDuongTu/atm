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
deliberately stores vectors as per-project files, decoupled from the storage-sync spec's
`cache.db` consolidation. The two specs are independent.
**Spawns:** a separate recall-measurement (eval) design task — see "Eval is stubbed, not
built here" (R7). This spec keeps only a lightweight driving hook; the measurement subsystem
gets its own spec + plan.

## Verification (spike, 2026-07-08)

Before locking the design, the load-bearing assumption — "the host can embed" — was verified
empirically on the dev machine (no GPU):

- `ollama pull nomic-embed-text` (376 MB) → embeds via `POST /api/embeddings` (and the
  OpenAI-compatible `/v1/embeddings`) in **59 ms on 100% CPU**. No GPU required.
- Output is **byte-for-byte deterministic** for identical input.
- Embedding is decoupled from the agent: the ollama server computes vectors locally
  regardless of which coding agent (claude/opencode/codex) runs, and regardless of whether
  that agent reasons via a local or a cloud model. **Agent-backend ≠ embedding-backend.**
- Cosine distribution for `nomic-embed-text`: synonym ≈ **0.84**, unrelated-but-technical ≈
  **0.41**, wholly-unrelated ≈ **0.31**. The floor is ~0.31, so a 0.30 fallback threshold is
  below the noise floor and would never fire — the threshold must be per-model (~0.55 for
  nomic) and nomic wants `search_query:` / `search_document:` prefixes to decorrelate.

These findings drive R1–R7 below.

## Driver

ATM is already a memory substrate on the **contribution** side: agents journal everything
into tasks, comments, vocabulary, and labels, and the manager sorts it out. What is missing
is **retrieval**. Today the only read surface is `task list` / `task show` / `label list` /
`vocabulary show` — flat listings, no semantic search, no "retrieve the memory relevant to
what I am about to do." The manager-as-knowledge-base-owner reframe declared inquiry/search
as the manager's remit but deferred it. This spec closes that gap.

The consolidation does **not** add new memory kinds. Tasks + comments + vocabulary + labels
remain the substrate. This spec adds a semantic search surface over the existing ledger, a
non-LLM indexing process that keeps a per-project vector cache fresh from the log, and a
manager inquiry/curation cognition. ATM stays a "dump join" — agents journal everything; the
process indexes; the manager synthesizes and curates.

### The principle that shapes every decision

ATM's **storage / search / index engine is pure Go, offline, deterministic — it never calls
a model.** Embeddings come from a project-declared, OpenAI-compatible embedding endpoint
(default: a local ollama serving `nomic-embed-text`). A single thin client boundary
(`atm embed`) is the *only* place ATM talks to that endpoint; it holds no model itself, and
the search/index engine is fully testable without any endpoint. This mirrors how
`vocabulary.json` is a derived artifact — the vector index is the same, one layer down.

## Decisions (locked during brainstorming + refined post-verification)

The refinements R1–R7 below supersede the earlier per-host / eval-in-v1 / LLM-sleep-loop
framing. The stable core decisions (no new memory kinds; per-project derived files; pure
cosine + text fallback; slug-keyed vector files; additive/sync-excluded artifacts) are
retained and restated where relevant.

| # | Decision |
|---|----------|
| D1 | **No new memory kinds.** Tasks + comments + vocabulary + labels stay the substrate. No free-floating note entity, no structural `Links` revival. Relationships stay implicit in prose; the manager digests them at retrieval time. |
| D2 | **Both developing agent and manager are first-class retrievers.** The developing agent has a direct CLI search; the manager has an inquiry-synthesis capability via track dispatch. Both read the same per-project vector index. |
| D3 | **Retrieval is two surfaces.** (a) Direct CLI search returning ranked semantic hits. (b) Manager-dispatch synthesis on `hint: question`: the manager runs `atm search` itself, reads hits, synthesizes a grounded answer citing hit IDs. The search *engine* returns ranked hits only. |
| D4 | **Search semantics: semantic/embedding search** with a local text-search fallback. Per-project vector index. Cosine similarity. |
| **R1** | **Embedding backend = a declared environment dependency.** A reachable OpenAI-compatible `/v1/embeddings` endpoint; default local ollama + `nomic-embed-text` (CPU-fine, no GPU, deterministic — verified). Users install ollama or point at an API. "No embedder available → text fallback" is a *setup/degraded* state, not a steady state. Supersedes the earlier "each host embeds with its own capability." |
| **R2** | **One embedding model per project, shared by all agents.** The project declares `embedding: {model, endpoint, query_prefix, doc_prefix, dim, threshold}`. Every agent (claude/opencode/codex/ollama) resolves the *same* config → the *same* shared index → cross-agent memory reuse. Vector files stay **slug-keyed only to enable deliberate migration** (index under a new slug → compare → repoint → drop old); steady state is one active model, not N coexisting. Supersedes D7/D18 "model choice is per-host; each agent searches its own model's index" (which fragmented shared memory). |
| **R3** | **Pure engine + one thin embed client.** The invariant refines from "the binary never calls a model" to "**the storage/search/index engine never calls a model.**" A single subcommand, `atm embed`, is the sole boundary that calls the project-configured endpoint (holds no model, config-driven). Chosen as a **subcommand, not a shell script**: single binary, no bash/curl dependency, identical on every host — best serves the agent-agnostic vision. |
| **R4** | **Indexing is a single run-once, non-LLM process; delta strictly from the log.** `atm index --project <CODE>` is one command the user starts once (foreground). On launch it does the initial full delta, then **watches `log.jsonl`** and re-embeds each new delta incrementally — no cron, no reruns, no scheduling logic on the user's side. Under it, `atm index reindex --once` is the batch primitive (CI/tests/git-hooks) — the same pipeline without the watch loop. The delta is always computed from the log (`LastLogSeq` + per-entry `text_hash`) vs. the vector meta; the log is the single source of truth, vectors never drive themselves. `atm index` requires **no `--actor`** (derived-cache rebuild, like `atm store rebuild`). It **logs progress on every delta processed.** The LLM manager is never in the mechanical loop. Supersedes D10's `--index` LLM-sleep-loop mode. |
| **R5** | **Threshold + prefixes are per-model config, not hardcoded.** Empirically nomic floors ≈0.31 (unrelated) and peaks ≈0.84 (synonym), so the old 0.30 default never fires. Threshold lives in the project `embedding` config (nomic default ≈0.55). `search_query:` / `search_document:` prefixes are carried in config and applied by `atm embed`. Supersedes D9's hardcoded 0.30. |
| **R6** | **Denormalized vector entries.** `VectorEntry` carries `title`, `snippet`, `labels` captured at index time. Search is pure vector math with **no per-query corpus reload** (drops the O(N) `GetTask`/`GetComment` loop the earlier design implied). Freshness is the indexer's job; search-time staleness is a soft flag from meta, not a blocking reload. Internal optimization — no CLI/data-flow change. |
| **R7** | **Recall measurement (eval) is stubbed as a driver, not built here.** This spec keeps one lightweight hook: append real inquiries (`query` + cited hit IDs) to an inquiry log as future ground truth. The full measurement subsystem (`atm eval` verbs, run records, recall@k/precision@k, the improvement loop) gets its **own ATM task + own spec + own plan**. Removes the eval store layer, `atm eval` CLI, and the manager evaluation responsibility from this v1. |
| D5 | **Curation is the manager's write-side job (retrieval preparation).** Good titles/descriptions/labels *are* retrieval preparation; rewrite vague titles to name the concept; use consistent labels; when superseding a prior decision, mark the old comment `ATM:comment:superseded` (the ATM-0062 convention) so stale rulings don't surface at equal rank. Per-decision granularity (ATM-0060) so retrieval can cite individual decisions. No new write commands — the manager already creates tasks/comments/labels. Curation changes text → next index cycle re-embeds automatically. |
| D6 | **Vector storage = per-project, per-model JSONL** at `$ATM_HOME/projects/<CODE>/vectors/<model-slug>.jsonl` (+ `.meta.json`). NOT inside `cache.db`. Slug-keyed for migration (R2). Derived, disposable, sync-excluded. |

## Section 1: Architecture & data flow

### File layout (additive to the existing store)

```
$ATM_HOME/
  projects/
    <CODE>/
      log.jsonl                          # source of truth (unchanged)
      vocabulary.json                    # ubiquitous language (unchanged)
      config.json                        # project config; gains an `embedding` block (NEW field)
      vectors/                           # NEW — the per-model semantic index (derived cache)
        <model-slug>.jsonl               # one JSON object per line per embedded task/comment
        <model-slug>.meta.json           # model, dim, last_log_seq, last_reindexed_at, count
      inquiry-log.jsonl                  # NEW — driving hook for the future eval task (R7):
                                         #        {query, cited_ids[], at} appended per inquiry
```

`vectors/` and `inquiry-log.jsonl` are derived/append-only and **sync-excluded** (same rule
as `vocabulary.json`): the log is the source of truth, and a freshly-pulled machine rebuilds
its own vectors by running `atm index`. The `embedding` config block *is* project state and
*does* sync (it declares which model the project uses).

### Project embedding config (`config.json`, new `embedding` block)

```json
{
  "embedding": {
    "model":        "nomic-embed-text",
    "endpoint":     "http://localhost:11434/v1",
    "query_prefix": "search_query: ",
    "doc_prefix":   "search_document: ",
    "dim":          768,
    "threshold":    0.55
  }
}
```

Set via `atm project set-embedding`. Absent → the project has no declared embedder; `atm
search` runs text-fallback only and `atm index` reports "no embedding configured."

### Vector file shape (`vectors/<model-slug>.jsonl`, one JSON object per line)

```json
{"id":"ATM-0042","kind":"task","model":"nomic-embed-text","dim":768,"vector":[...],"text_hash":"sha256:...","log_seq":1287,"title":"label resolver refactor","snippet":"...","labels":["ATM:type:feature"]}
{"id":"ATM-0042-c0003","kind":"comment","model":"nomic-embed-text","dim":768,"vector":[...],"text_hash":"...","log_seq":1290,"title":"","snippet":"...","labels":[]}
```

- `id`/`kind`/`model`/`dim`/`vector`/`text_hash`/`log_seq` as before.
- `title`/`snippet`/`labels` (R6): denormalized at index time so search never reloads tasks.
- `text_hash`: SHA-256 of the embedded document text (task: title + description + labels;
  comment: body + labels). Detects staleness.
- `log_seq`: last log seq the embedding reflects; compared to `LastLogSeq(code)` for delta.

### Vector meta shape (`vectors/<model-slug>.meta.json`)

```json
{"model":"nomic-embed-text","dim":768,"last_log_seq":1287,"last_reindexed_at":"2026-07-08T12:00:00Z","count":42}
```

### Indexing data flow (`atm index --project <CODE>`, run-once watcher, no LLM)

```
atm index --project <CODE>            # user starts once, foreground, no --actor
  -> resolve embedding config (model, endpoint, prefixes, dim); if absent -> error+exit
  -> INITIAL PASS (== `atm index reindex --once`):
       1. pending: LastLogSeq(code) vs vectors/<slug>.meta.json last_log_seq + text_hash
                   mismatch -> delta set (new/changed tasks + comments)
       2. atm embed (batch): compose doc text (with doc_prefix) -> POST endpoint -> vectors
       3. write-batch: atomic append into vectors/<slug>.jsonl + meta update
       4. log progress: "indexed N (M tasks, K comments); index at log_seq <seq>"
  -> WATCH: inotify log.jsonl; on append -> recompute delta from the log -> steps 2-4
  -> continues until the user stops it (Ctrl-C)
```

The engine invokes the endpoint only through `atm embed` (step 2). The core append/search is
pure Go. No cron; the single command keeps the index fresh from the log.

### Search data flow (`atm search`, developing agent OR manager inquiry)

```
atm search --project <CODE> "query text" [--k 5] [--kind task|comment|all] [--output json]
  -> resolve embedding config
  -> if config present: atm embed the query (query_prefix) -> query vector
       -> read vectors/<slug>.jsonl; cosine vs each entry; rank top-K
       -> if no index OR top score < config threshold: TEXT FALLBACK
  -> if no config OR no index: TEXT FALLBACK (token overlap + recency over tasks+comments)
  -> return ranked hits from denormalized fields (id, kind, score, title, snippet, labels, match)
```

The pure engine (`store.Search`) takes a query vector and returns hits — it never embeds.
`atm search` is the convenience layer that resolves config + calls `atm embed` + calls the
pure engine. A pure `atm search --query-vector <json>` form is also available (tests, callers
that pre-embed).

### Manager inquiry data flow (developing agent dispatches `atm-manager` with `hint: question`)

```
manager -> reads the question -> runs `atm search --project <CODE> "question" --output json`
        -> reads ranked hits (drills in via atm task show / comment list if needed)
        -> synthesizes a grounded answer citing hit IDs; returns it (does not block on reply)
        -> appends {query, cited_ids, at} to inquiry-log.jsonl  (R7 driving hook)
```

### Key invariants preserved

- The storage/search/index engine never calls a model; only `atm embed` talks to the endpoint.
- The log is the source of truth. Vectors are derived/disposable; `atm store rebuild` drops
  them; `atm index` regenerates. Delta is always computed from the log.
- Per-project isolation. No cross-project search.
- One declared model per project → one shared index → no space-mismatch (a vector whose
  dim/model doesn't match the config is a config error).
- Offline/deterministic engine: `store.Search` over a vector + local files is pure Go.
- The closed action enum and replay semantics (audit-log v1) are untouched — vectors and the
  inquiry log are derived artifacts (like `vocabulary.json`), not log events.

## Section 2: CLI surface

### `atm project set-embedding`

```
atm project set-embedding --project <CODE> --model <slug> --endpoint <url>
    [--query-prefix <s>] [--doc-prefix <s>] [--dim <n>] [--threshold <f>] [--actor <id>]
```

Writes the `embedding` block into `config.json` (mutating project state → requires `--actor`).
`atm project show` reports the current embedding config.

### `atm embed` (the one model-touching boundary)

```
atm embed --project <CODE> [--role query|document] [--file <jsonl>|"text"] [--output json]
```

- Resolves the project embedding config, applies the role prefix (`query_prefix` /
  `doc_prefix`), POSTs to the endpoint, returns vector(s). Single text (positional) or a batch
  (`--file` of `{id,text}` lines). The **only** command that calls the endpoint. Holds no
  model. If no config → `ErrUsage`.

### `atm index` (run-once watcher) + `reindex` primitive

```
atm index          --project <CODE> [--output json]      # run-once foreground watcher, NO --actor
atm index reindex  --project <CODE> [--once] [--output json]   # batch primitive (CI/hooks)
atm index status   --project <CODE> [--output json]      # last_log_seq vs current, stale count
atm index drop     --project <CODE> --model <slug>       # remove a model's index (migration)
```

- `atm index`: initial delta pass, then watch `log.jsonl`; re-embed each delta via `atm
  embed`; append via the store batch writer; **log progress per delta**. No `--actor`.
- `reindex --once`: the same pipeline, single pass, exits. Used by watch internally, and
  standalone for CI/git-hooks.
- `status`: how stale each model index is (drives migration/reindex decisions).
- `drop`: deletes one model's vector file + meta (model migration).

### `atm inquiry add` (R7 driving hook — the manager's write into the eval-ground-truth log)

```
atm inquiry add --project <CODE> --query "<question>" --cited <id[,id...]> [--actor <id>]
```

Appends `{query, cited_ids, at}` to `inquiry-log.jsonl`. The manager calls it after
synthesizing an inquiry answer, so real questions + the IDs it cited accrue as future eval
ground truth. This is the whole eval footprint in this spec; the separate eval task consumes
the log.

### `atm search`

```
atm search --project <CODE> "query text" [--kind task|comment|all] [--k 5] [--output json]
atm search --project <CODE> --query-vector <json> "query text" [...]   # pure form (pre-embedded)
```

- Convenience form: resolves config, embeds the query via `atm embed`, runs the pure engine,
  falls back to text search if no config / no index / weak top score.
- Pure form: caller supplies the vector; the engine never embeds. Used by tests and
  pre-embedding callers. `--query-vector` dim must match the index `dim` (`ErrUsage` otherwise).

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

### Command cheat-sheet additions (manager + developing contexts)

```
<ATM_BIN> project set-embedding --project <CODE> --model <slug> --endpoint <url> [--actor <ACTOR>]
<ATM_BIN> index --project <CODE>                 # run once; watches the log, keeps vectors fresh
<ATM_BIN> index reindex --project <CODE> --once  # one-shot batch (CI/hooks)
<ATM_BIN> index status --project <CODE>
<ATM_BIN> search --project <CODE> "query" [--kind task|comment|all] [--k 5] [--output json]
```

## Section 3: Manager prompt changes (`internal/manager/context_v1.md`)

Two additions (indexing is no longer an LLM responsibility — R4 makes it a mechanical
process). Both are mode-agnostic (present in all renders), consistent with the existing
section pattern.

### 3.1 Inquiry responsibility

On a `hint: question` track call, the manager runs `atm search --project <CODE> "question"
--output json`, reads the ranked hits, synthesizes a grounded answer citing hit IDs, and
appends `{query, cited_ids, at}` to `inquiry-log.jsonl` (the R7 driving hook). If no index
exists or results are weak, it falls back to text results and says so.

### 3.2 Write-side representation (folds into the existing Ledger hygiene section)

Teach the manager that its curation *is* retrieval preparation (D5): rewrite vague titles to
name the concept; use consistent labels; mark superseded decisions
`ATM:comment:superseded` (ATM-0062); prefer per-decision granularity (ATM-0060) so retrieval
can cite individual decisions. No new write commands. Note that curation changes text →
`atm index` re-embeds the affected items on its next delta.

## Section 4: Developing context changes (`internal/developing/context_v1.md`)

Add a short **Retrieval** section:

- **Direct:** `atm search --project <CODE> "query"` — the project's declared embedding model
  is used automatically (you don't pick a model); text fallback if no index / weak hits.
- **Synthesized:** dispatch `atm-manager` with `hint: question` for a grounded answer citing
  hit IDs.
- Both are read-only and don't block your work. To keep the index fresh, someone runs
  `atm index --project <CODE>` once (it watches the log).

## Section 5: Store layer (`internal/store/`)

### `internal/store/config.go` (modify)

Add the `Embedding` struct + field to the project config type; `GetEmbeddingConfig(code)` /
`SetEmbeddingConfig(code, cfg, actor)`. Config is synced project state (not a derived cache).

### `internal/store/vectors.go` (new)

```go
type VectorEntry struct {
    ID       string    `json:"id"`
    Kind     string    `json:"kind"`        // "task" | "comment"
    Model    string    `json:"model"`
    Dim      int       `json:"dim"`
    Vector   []float64 `json:"vector"`
    TextHash string    `json:"text_hash"`
    LogSeq   int       `json:"log_seq"`
    Title    string    `json:"title,omitempty"`  // denormalized (R6)
    Snippet  string    `json:"snippet"`          // denormalized (R6)
    Labels   []string  `json:"labels,omitempty"` // denormalized (R6)
}

type VectorMeta struct {
    Model           string `json:"model"`
    Dim             int    `json:"dim"`
    LastLogSeq      int    `json:"last_log_seq"`
    LastReindexedAt string `json:"last_reindexed_at"`
    Count           int    `json:"count"`
}

func (s *Store) ReadVectors(code, slug string) ([]VectorEntry, error)          // best-effort; skips malformed
func (s *Store) WriteVectorBatch(code, slug string, entries []VectorEntry, lastLogSeq int) error // lock-guarded atomic append + meta
func (s *Store) VectorMeta(code, slug string) (*VectorMeta, error)             // nil,nil if absent
func (s *Store) DropVectors(code, slug string) error
func (s *Store) ListVectorModels(code string) ([]string, error)
```

Path helpers `vectorsDir`/`vectorPath`/`vectorMetaPath` in `store.go`. Writes lock-guarded +
fsynced; reads lock-free (best-effort). `WriteVectorBatch` rejects a batch whose entry model
≠ slug or whose dim is inconsistent (`ErrUsage`, no partial write).

### `internal/store/search.go` (new — the pure engine)

```go
type Hit struct {
    ID string; Kind string; Score float64
    Title string; Snippet string; Labels []string; Match string // "semantic" | "text"
}
type SearchParams struct {
    Project string; Model string; QueryVector []float64; QueryText string
    Kind string; K int; Threshold float64
}
func (s *Store) Search(p SearchParams) (hits []Hit, fallbackUsed bool, err error)
```

`Search` reads vectors, computes cosine, ranks top-K **from the denormalized entry fields**
(no task/comment reload — R6). If no index or top score < threshold, runs the text-fallback
path (token overlap + recency over tasks+comments via existing reads). Dim-mismatch query →
`ErrUsage`.

### `internal/store/indexer.go` (new — mechanical delta + watch)

```go
func (s *Store) PendingIndex(code, slug string) ([]IndexDoc, error)  // delta from log vs meta
func (s *Store) ReindexOnce(code string, embed EmbedFunc) (IndexResult, error) // pending->embed->write-batch
func (s *Store) Watch(ctx, code string, embed EmbedFunc, log ProgressFunc) error // ReindexOnce + inotify loop
```

`EmbedFunc` is injected by the CLI (`atm embed` client) so the store engine stays model-free
and testable with a fake embedder. `Watch` logs progress per delta via `ProgressFunc`.

### `internal/embed/` (new — the client boundary, R3)

An OpenAI-compatible `/v1/embeddings` HTTP client. Reads the project embedding config, applies
role prefixes, returns vectors. The single place a model is contacted. Isolated from `store`
so the engine has no HTTP dependency.

### `internal/store/inquiry.go` (new — R7 driving hook)

`AppendInquiry(code, query string, citedIDs []string)` appends to `inquiry-log.jsonl`. That
is the entire eval footprint in this spec; consumption is the separate eval task.

### `rebuild.go` / `verify.go`

- `Rebuild()` drops `vectors/` (derived; regenerated by `atm index`). Task/comment cache
  rebuild unchanged. `inquiry-log.jsonl` is *not* dropped (it is captured ground truth, not a
  derived cache).
- `Verify()` reports `vectors/` absence as info-level (optional derived artifact). If present,
  reports stale-entry counts per model.

### Sync exclusion

`vectors/` and `inquiry-log.jsonl` are sync-excluded (derived / append-only local capture).
The `embedding` config block in `config.json` *does* sync (project state). A freshly-pulled
machine has no vectors until it runs `atm index`.

## Section 6: Error handling

- `atm search` with no embedding config OR no vector file → text fallback, `match:"text"`,
  `fallback_used:true`, exit 0. Not an error.
- `atm search --query-vector` whose dim ≠ index `dim` → `ErrUsage` (2).
- `atm embed` with no embedding config → `ErrUsage` (2, "no embedding configured; run atm
  project set-embedding").
- `atm embed` when the endpoint is unreachable → non-zero exit with the endpoint error; the
  caller (`atm index`) logs it and retries on the next delta; already-written vectors are kept
  (batch is atomic per write-batch).
- `atm index` with no embedding config → `ErrUsage` (2), exits (does not spin).
- `atm index write` path rejecting model-mismatch / dim-mismatch batch → `ErrUsage`, whole
  batch rejected (atomic).
- `atm index drop` on a missing model file → `ErrNotFound` (3).
- Stale vectors (`text_hash` mismatch) → re-embedded on the next delta; counted in `atm index
  status` as `stale: N`. Not an error.
- Malformed `vectors/<slug>.jsonl` line → skipped with a stderr warning; search continues over
  valid lines (best-effort, mirroring audit-log tolerance).

## Section 7: Testing, verification & rollout

### Testing approach

Same layered structure as v2 / comments / audit-log v1: unit tests per store file, an
injected fake `EmbedFunc` (engine tests never hit a real endpoint), CLI golden tests, prompt
render tests.

| Area | Invariants |
|---|---|
| `store/config_test.go` | `SetEmbeddingConfig`/`GetEmbeddingConfig` round-trip; config syncs; absent → nil. |
| `store/vectors_test.go` | `WriteVectorBatch` atomic append + meta; denormalized fields round-trip; `ReadVectors` skips malformed; staleness by `text_hash`; drop; list; model/dim-mismatch rejection; per-project isolation. |
| `store/search_test.go` | Cosine ranking from denormalized fields (no reload); `--kind` filter; threshold→fallback; text fallback ranking; empty/missing index→fallback; dim-mismatch rejected. |
| `store/indexer_test.go` | `PendingIndex` delta from log vs meta (new + changed by text_hash); `ReindexOnce` with a fake embedder writes correct batch + meta + progress; watch loop triggers on new log append (fake watcher). |
| `embed/client_test.go` | Role-prefix application; request/response mapping against a stub `/v1/embeddings` server; no-config → error. |
| `cli/search_test.go` | Golden semantic + text-fallback + empty + kind + k; pure `--query-vector` form; dim-mismatch → `ErrUsage`; no-config → fallback. |
| `cli/index_test.go` | `reindex --once` with a stub embedder; `status` staleness; `drop`; **`atm index` requires no `--actor`**; progress logged. |
| `cli/embed_test.go` | Resolves config + role prefix; stub endpoint; no-config → `ErrUsage`. |
| `cli/project_test.go` | `set-embedding` writes config block; requires `--actor`; `project show` displays it. |
| Manager prompt render | Inquiry + write-side-representation sections present in all renders; no indexing-loop section (removed); cheat-sheet includes new verbs. |
| Developing context render | Retrieval section present (direct search + `hint: question`). |

**Verification gate:** `make verify` (`make build && make test`) unchanged.

### Rollout (layered commits, each green; strictly additive)

1. `store/config.go` `Embedding` block + get/set + tests.
2. `store/vectors.go` (denormalized entries) + tests.
3. `store/search.go` pure engine (cosine + text fallback, no reload) + tests.
4. `internal/embed/` client boundary + tests.
5. `store/indexer.go` (`PendingIndex`, `ReindexOnce`, `Watch`) with injected `EmbedFunc` + tests.
6. `store/inquiry.go` append hook + tests.
7. `cli/project.go` `set-embedding` + golden tests.
8. `cli/embed.go` + golden tests.
9. `cli/index.go` (`atm index` watcher, `reindex --once`, `status`, `drop`) + golden tests.
10. `cli/search.go` (convenience + pure form) + `cli/inquiry.go` (`inquiry add`) + golden tests.
11. `internal/manager/context_v1.md`: inquiry + write-side-representation sections + cheat sheet + render tests.
12. `internal/developing/context_v1.md`: Retrieval section + render test.
13. `rebuild.go`/`verify.go`: drop `vectors/` on rebuild (keep `inquiry-log.jsonl`); info-level in verify.
14. Conventions update: `ATM:comment:superseded` (ATM-0062) in `atm conventions`; add
    search/index/embed/set-embedding to the agent first-contact sequence.

**Compatibility:** fully additive. No log action changes, no entity schema changes. The
`embedding` config block is optional; `vectors/` and `inquiry-log.jsonl` simply don't exist
until configured/indexed. No migration tooling.

## Out of scope (v1)

- **Recall measurement / eval** (`atm eval`, run records, recall@k, the improvement loop) —
  deferred to its own ATM task + spec + plan (R7). This spec ships only the inquiry-log hook.
- **New memory entities** (notes, decisions, Q&A pairs as distinct kinds). Tasks + comments +
  vocabulary + labels remain the substrate.
- **Structural relationship fields** (no `Links` revival).
- **Per-agent model divergence.** One declared model per project (R2); agents share the index.
- **N models coexisting in steady state.** Slug-keyed files exist only for deliberate migration.
- **The engine calling a model.** Only `atm embed` contacts the endpoint (R3).
- **Cross-project search / cross-model result merging.** Per-project, single model per query.
- **A real ATM-owned background daemon.** `atm index` is a foreground process the user starts;
  `reindex --once` is the batch primitive. No auto-scheduling inside ATM.
- **TUI search UI.** CLI-only in v1.
- **Reindex-on-write.** Rejected: it would couple every ledger write to the endpoint being up
  and break the offline/deterministic core.
