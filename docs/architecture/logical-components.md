# Logical components and code structure

This document is the physical counterpart to [the label substrate and capability commands](label-substrate-and-capabilities.md). That document says what ATM *means*; this one says where the code *lives*, which package may import which, and how the current tree migrates to the target shape. It is the reference every refactoring task under ATM-9eb7dc reads first.

The architecture is hexagonal: a dependency-free domain core in the middle, adapters around it, and one composition root that wires them together. The CLI and the TUI are **sibling adapters over the same core interface** — the TUI never imports the CLI and never shells out to it; both consume the core and are handed a concrete store at wire-up.

## Target component map

```
cmd/atm ─────────────── composition root: constructs the store, injects it everywhere
  ├── internal/cli ──────── adapter: cobra ⇄ core services + capability registry
  ├── internal/tui ──────── adapter: bubbletea ⇄ core service interface
  ├── internal/capability/* ── capability commands (contextmap, …), each owning a label slice
  │        │
  │        ▼
  │   internal/core ─────── domain leaf: types, label algebra, faceting, service + repository interfaces
  │        ▲
  └── internal/store ────── adapter: implements core's repositories via eventsource + sqlite
           │
           ▼
      libs/eventsource ──── nested Go module: pure event-log library (root + sync/), future separate repo
```

## Components and responsibilities

| Component | Responsibility | Must never |
|---|---|---|
| `cmd/atm` | `main` and the composition root. Constructs the concrete store, the capability registry, and hands them to the adapters. | Contain domain or presentation logic. |
| `internal/core` | The domain leaf. Task/Label/Comment/Project types, the label algebra (wildcards, faceting, grouping, board expressions), vocabulary rules, and the narrow service + repository interfaces the adapters consume. | Import any other internal package or any I/O library. Know that persistence is event-sourced. |
| `internal/store` | The persistence adapter. Implements `core`'s repository interfaces using the event log (author → project → sqlite cache). The **only** package that knows events exist. | Export event-sourcing concepts upward; grow UI- or CLI-shaped helpers. |
| `internal/cli` | The terminal adapter. Cobra command tree: parse flags → call core services → emit text/JSON. Mounts the capability registry's commands; the registry itself is assembled by `cmd/atm`. | Contain business logic; be imported by anything except `cmd/atm`. |
| `internal/tui` | The interactive adapter. Bubble Tea panes over the core service interface, with live features (watch, reindex) via that interface. | Import `cli` or the concrete store type; reimplement core algebra (faceting, wildcard matching). |
| `internal/capability/*` | One package per capability command (first: `contextmap`). Owns its label slice, exposes intent verbs, registers its cobra command with the registry. | Reach past core into store internals. |
| `libs/eventsource` | Nested Go module (own `go.mod`, stitched via `go.work`). Root package: event canon, hashing, HLC, DAG, fold, replay. `sync/` subpackage: sync engine, `LocalStore`/`SyncTarget` interfaces, dir and git transports. | Import anything from this repo. Depend on more than the standard library (plus `jcs`). |

Satellites keep their current roles: `internal/actor` and `internal/seed` stay small leaves consumed by core or store; `internal/embed`, `internal/activity`, `internal/agent`, `internal/developing`, `internal/manager`, `internal/version` remain thin, with `version` restored to a pure leaf.

## Import rules

These rules are the enforceable heart of this document. A change that violates one is wrong even if it compiles and passes tests.

| Package | May import (internal) |
|---|---|
| `cmd/atm` | anything — it is the composition root |
| `internal/cli` | `core`, `capability` (registry only), satellites; `store` only until step 6 moves the remaining admin surface behind interfaces |
| `internal/tui` | `core`, `capability` (registry only), `tui/components` — plus the acknowledged satellites until they are purged |
| `internal/capability/*` | `capability`, `core` |
| `internal/capability` | nothing internal but `core` |
| `internal/core` | nothing internal (pure leaf) |
| `internal/store` | `core`, `libs/eventsource`, `seed` |
| `libs/eventsource` | nothing from this repo |

Direction of knowledge: `core` defines interfaces in domain terms; `store` implements them (the `eventsync.LocalStore` pattern, generalized). Nobody above `store` may name an event, a replica, an HLC, or a projector.

## Why eventsource and eventsync merge into one library

`eventsource` already satisfies the library constraint — standard library plus `jcs`, no internal imports, no file I/O outside tests. `eventsync` imports only `eventsource`, defines its own consumer interfaces, and its two transports (dir, git-via-subprocess) are generic. Set-union sync of an event log is protocol, not ATM domain, so it travels with the log format it synchronizes: one module, one version line, one future repository. The module keeps its import hygiene by construction — a nested `go.mod` makes importing ATM internals a compile error, today.

## Migration plan

The order is deliberate: mechanical splits first (make everything after reviewable), then pure logic into `core` (kill the duplication), then the interface seam (decouple the adapters), then the risky carve (store internals) last, behind a now-stable boundary. The library promotion is independent and lands first. Every step keeps the build green and behavior identical; the faceting step is gated on parity tests written before the move.

| Step | Ledger task | Scope | Risk |
|---|---|---|---|
| 1 | ATM-8dbf94 | Promote `eventsource` + `eventsync` to `libs/eventsource` (root + `sync/`) as a nested module with `go.work`. | Low |
| 2 | ATM-f125d9 | God-file splits: `cli/root.go` init wizard → `cli/init.go`; `cli/store.go` → integrity / migrate / sync files; `tui/tasks.go` → list / detail / grouping / mutations; `store/cache.go` DDL → `cache_schema.go`. No signature changes. | Low |
| 3 | ATM-cca7b0 | Create `internal/core`; write faceting/wildcard parity tests against both implementations, then move the algebra into `core` and delete both copies (`store/query.go` and `tui/tasks.go` variants). | Medium |
| 4 | ATM-b9d83a | Move domain types into `core`; define the core service/repository interfaces; TUI depends only on the interface; composition root moves to `cmd/atm`; fix backwards edges (`version → store`, `cli → tui` via the runner seam). | Medium |
| 5 | ATM-08db6e | Capability registry in `cli`; move `contextmap` to `internal/capability/contextmap`; `cli` stops importing capability internals. | Medium |
| 6 | ATM-3b873c | Carve the event-log write-engine inside `store` behind `core`'s repository interfaces; collapse the twelve `eventsource_*.go` wrappers into a coherent adapter. | High |

Each task is written to be executed in a fresh session: it carries its own context, file pointers, and acceptance criteria, and it names this document as the specification. ATM-9eb7dc is the umbrella; progress lives there.

## What this document is not

It does not redefine the substrate — labels, boards, and capabilities are specified in the companion document. It does not promise an SDK: out-of-repo capability commands (git-style `atm-<name>` discovery against a published core module) are anticipated by the registry seam but deliberately unspecified until the first external capability exists. And it does not pre-modularize anything beyond `libs/eventsource` — `core`, `store`, and the adapters stay plain internal packages until a concrete external consumer says otherwise.
