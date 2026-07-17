# Capability registry; contextmap and workflow become registered capabilities

- **Date:** 2026-07-17
- **Ledger task:** ATM-08db6e (refactor step 5 of 6; umbrella ATM-9eb7dc)
- **Specification context:** docs/architecture/logical-components.md (physical structure), docs/architecture/label-substrate-and-capabilities.md (what a capability is)
- **Depends on:** ATM-b9d83a (step 4: core interfaces + composition root) — landed on main through 0c352b0

## Goal

Introduce the capability registry seam the architecture documents promise: capabilities are self-contained packages under `internal/capability/*` that own their label slice, their verbs, and their cobra command, and are assembled into a registry by the composition root. Neither `internal/cli` nor `internal/tui` names a specific capability after this step — they consume only the registry. This is the seam that later lets capabilities be enabled and disabled dynamically (plugins); this step builds the seam, not the plugin loader.

## Current state (survey 2026-07-17, main @ 596e78b)

- `internal/contextmap` holds `*store.Store` concretely in `Recorder`, `Check`, and `EnsureVocabulary`. Its actual store surface is seven methods, all already present on core role interfaces: `ListTasksErr`, `GetTask`, `TaskLabelAdd`, `SetDescription` (TaskService); `CreateComment`, `ListComments` (CommentService); `LabelSeed` (LabelService). The witness/resolver side (git subprocess + file hashing) has no store dependency.
- `internal/workflow` (landed mid-step-4 as ATM-e23fe5) already consumes `core.TaskService`/`core.LabelService`, but lives outside `internal/capability/`. `tests/arch/imports_test.go` explicitly assigns its relocation to this step.
- `internal/cli` mounts both via `newContextCmd`/`newWorkflowCmd` in root.go's hardcoded `AddCommand` list; the cobra layers live in `cli/context.go` (285 LOC) and `cli/workflow.go` (209 LOC) on `cliState` helpers (`openStore`, `emit`, `requireMutatingActor`, `resolveActor`, `bindActorFlag`, `bindTaskIDFlags`/`resolveTaskID`, `taskToJSON`).
- `internal/tui` imports workflow in three places: `EnsureVocabulary` on project select (projects.go) and on scoped construction (app.go), and `BoardOpenTasks` for the default board selection (labels.go `selectDefault`).
- `cli/project.go` (project create) and `cli/label.go` (label seed) hardcode `workflow.EnsureVocabulary`.

## Design

### Package layout

```
internal/capability/            registry package: Capability, Registry, Env
internal/capability/contextmap/ from internal/contextmap + its cobra layer (from cli/context.go)
internal/capability/workflow/   from internal/workflow  + its cobra layer (from cli/workflow.go)
```

Chosen approach (over "domain moves, cobra stays in cli" and "init() self-registration"): an explicit interface + registry assembled in `cmd/atm`. Explicit construction gives the plugin seam without global mutable state, and moving the cobra layers into the capability packages is what actually removes the adapters' knowledge of individual capabilities.

### The Capability interface and Registry (internal/capability)

```go
type Capability interface {
    // Name is the stable identifier ("contextmap", "workflow").
    Name() string
    // EnsureVocabulary seeds the capability's labels and boards for a
    // project. Idempotent; never overwrites curated descriptions.
    EnsureVocabulary(svc core.LabelService, code, actor string) error
    // Command returns the capability's cobra verb tree, built over env.
    Command(env Env) *cobra.Command
    // DefaultBoard nominates the board a UI should select by default for
    // the project, or "" if this capability nominates none.
    DefaultBoard(code string) string
}
```

`Registry` is an ordered collection (`NewRegistry(caps ...Capability)`) with three methods: `Commands(env Env) []*cobra.Command` (mount order = registration order), `EnsureVocabulary(svc core.LabelService, code, actor string) error` (loops all capabilities, first error wins), and `DefaultBoard(code string) string` (first non-empty nomination). `cmd/atm` registers workflow before contextmap so `open-tasks` remains the default board.

`internal/capability` imports `internal/core` and cobra, nothing else internal. Each capability package imports `internal/capability` (for `Env`), `internal/core`, and cobra.

### The Env seam

`Env` is the interface a capability's cobra layer builds on, defined in `internal/capability`, implemented by `cliState`. Every method is a one-line delegation to an existing `cliState` helper — no new logic, so JSON envelopes, deprecation warnings, actor defaults, and exit codes stay byte-identical:

```go
type Env interface {
    OpenService() (core.Service, error)
    Stdout() io.Writer
    Stderr() io.Writer
    Emit(v any, textFn func()) error
    RequireMutatingActor() (string, error)
    ResolveActor(required bool) (string, error)
    BindActorFlag(cmd *cobra.Command)
    BindTaskIDFlags(cmd *cobra.Command, id, legacy *string)
    ResolveTaskID(id, legacy string) (string, error)
    TaskJSON(t *core.Task) any
}
```

`OpenService` is `openStore` with the declared type changed — `*store.Store` satisfies `core.Service` structurally (step 4). `TaskJSON` exposes the CLI's canonical task envelope (today's `taskToJSON`) so the workflow status verbs keep emitting the identical shape. Usage errors inside capabilities wrap `core.ErrUsage`, which `cli.CodeForError` already maps (via the store alias chain). `os.Getwd` — the contextmap resolver root — stays a direct call inside the capability; it is process state, not a cli service.

### Domain flips off the concrete store

`contextmap.Recorder.Store`, `contextmap.Check`, and `contextmap.EnsureVocabulary` change from `*store.Store` to core role interfaces, exactly as step 4 typed workflow's `Recorder`/`Reporter`: `EnsureVocabulary` takes `core.LabelService`; the recorder and check paths take a small local composite of `core.TaskService` + `core.CommentService` + `core.LabelService` (the seven methods listed above), declared in the contextmap package. `internal/capability/contextmap` ends with zero store imports in production files.

### Adapters consume only the registry

- `cli.Deps` gains `Registry *capability.Registry`; root.go replaces `newContextCmd`/`newWorkflowCmd` with mounting `Registry.Commands(st)`, and `cli/project.go` + `cli/label.go` replace the hardcoded `workflow.EnsureVocabulary` with the registry loop. `internal/cli` stops importing both capability packages.
- `tui.Run`/`NewModelOpts` gain the registry. The three workflow call sites become `reg.EnsureVocabulary(...)` (app.go, projects.go) and `reg.DefaultBoard(...)` (labels.go `selectDefault` — an empty result never matches a board row, so the existing first-board fallback covers a registry with no nomination). `internal/tui` stops importing workflow.
- `cmd/atm` (composition root) constructs `capability.NewRegistry(workflow.New(), contextmap.New())` and injects it into both adapters.

### Import-rules amendment (docs/architecture/logical-components.md)

| Package | May import (internal) |
|---|---|
| `internal/cli` | `core`, `capability` (registry only), satellites; `store` until steps 5/6 finish the migration |
| `internal/tui` | `core`, `capability` (registry only), `tui/components`, acknowledged satellites |
| `internal/capability` | `core` |
| `internal/capability/*` | `capability`, `core` |

The doc's `capability/*` row and the tui row are updated accordingly; the arch tests are the enforcement.

## Decisions

1. **Both capabilities move in this step.** The registry with two clients proves the seam; the arch test already assigns workflow's relocation here.
2. **Eager vocabulary seeding.** Project create (cli and tui project-select) ensures every registered capability's vocabulary, not just workflow's. Accepted visible change: contextmap's labels and the `context-current` board exist immediately after project create instead of appearing on first `atm context add`. Seeding is idempotent and `LabelSeed` never overwrites curated descriptions; capabilities remain self-bootstrapping on their own verbs for pre-existing projects.
3. **`DefaultBoard` is interface metadata, not a TUI import.** The TUI asks the registry, and the registry answers with the first non-empty nomination. A disabled workflow capability degrades to the first-board fallback that already exists.
4. **Plugin future is anticipated, not built.** Enable/disable today is editing the slice in `cmd/atm`. Out-of-repo capabilities (git-style `atm-<name>` discovery) remain deliberately unspecified per the architecture doc, until the first external capability exists.

## Compatibility guarantees

- `atm context ...` and `atm workflow ...` trees, flags, help text, JSON envelopes, text output, deprecation warnings, and exit codes are unchanged; the cobra code moves, it does not get rewritten.
- The only behavior change is decision 2 (eager seeding), which is additive and idempotent.
- Registry `EnsureVocabulary` returns the first error; call sites keep their current single-error handling (toast in tui, propagate in cli).

## Testing

- **Arch tests** (`tests/arch/imports_test.go`): capability packages import only `capability` + `core` internally; `internal/capability` imports only `core`; cli and tui import neither capability package; the step-4 comment deferring workflow's relocation is resolved.
- **Package tests move with their packages**; `cli/context_test.go` and `cli/workflow_test.go` relocate to the capability packages or stay as cli-level integration tests according to what each test actually exercises (the implementation plan decides per file).
- **Registry unit tests**: mount order, EnsureVocabulary loop and first-error propagation, DefaultBoard first-non-empty.
- **Behavior parity**: `atm context --help` and `atm workflow --help` (full trees) captured before and after the move must be byte-identical, the step-2 technique. `make verify` green (both modules + script tests).

## Out of scope

- Step 6 (ATM-3b873c): the store-internal write-engine carve; `cli`'s remaining direct `store` imports (integrity/migrate/sync surface) stay until then.
- Purging the acknowledged satellites (`activity`, `seed`, `embed`) from adapter imports.
- Any plugin loading mechanism, config-driven enable/disable, or external capability discovery.
