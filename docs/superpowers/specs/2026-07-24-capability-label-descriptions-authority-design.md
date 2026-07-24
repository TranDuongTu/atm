# Capability Label Description Authority Design

## Context

`ATM-0116` was filed against the old global label seed path: `atm label seed`
and `internal/seed/seed.go`. That path has since been removed. Capability
vocabulary is now the only self-setup path for managed labels, through each
capability's `EnsureVocabulary`.

The older capability doctrine said `EnsureVocabulary` was additive and never
overwrote a human-curated label description. That is no longer the intended
ownership model. Labels are substrate records, and capability-owned labels
should describe the capability's current semantics. A label description edited
outside the capability vocabulary is not authoritative for a managed label.

## Decision

For labels emitted by a capability's `EnsureVocabulary`, the capability's
description is authoritative.

`Store.LabelSeed(name, description, expr, actor)` remains the store API used by
capabilities to converge vocabulary. Its contract changes:

- If the label is absent or tombstoned, create it with the supplied description
  and, when non-empty, the supplied expression.
- If the label exists and `description` differs from the supplied non-empty
  description, append a `label.upserted` event that updates the description.
- If the label exists and the supplied description is empty or already matches,
  do not append anything for the description.
- Do not overwrite an existing expression from `LabelSeed`. Expressions remain
  create-only through seed; deliberate expression changes still use explicit
  force-update paths such as `LabelAdd`.

This resolves the original empty-description bug as a subset of the new rule:
an empty managed-label description differs from the capability vocabulary, so
the next capability ensure fills it.

## Non-Goals

- No global `atm label seed` command is restored.
- No `internal/seed/seed.go` path is restored.
- No automatic deletion of unmanaged labels is added.
- No expression migration is added to `LabelSeed`.
- No UI editing behavior changes are required for this task.

## Data Flow

Capability code calls `EnsureVocabulary`, which iterates its literal
vocabulary and calls `LabelSeed`. `LabelSeed` delegates to the event-log
changeset. The changeset folds current project state, compares the live label's
description to the requested vocabulary description, and appends only when the
label is absent/tombstoned or the non-empty description differs.

The existing dirty-transaction contract remains intact: unchanged labels stay
clean and skip reprojection; changed descriptions are real mutations and must
mark the transaction dirty so cache projection sees the new description.

## Compatibility

Existing projects with manually edited descriptions on capability-owned labels
will converge back to capability vocabulary text on project create, project
capability add, TUI project select, TUI Boards re-ensure, or a capability seed
command. This is intentional.

Unmanaged labels, such as ad hoc `type:*` or `comment:*` labels not emitted by
any capability, are unaffected unless a caller explicitly invokes `LabelSeed`
for them.

## Testing

Store tests cover the contract directly:

- `LabelSeed` overwrites an existing non-empty description with the supplied
  vocabulary description.
- `LabelSeed` fills an existing empty description.
- `LabelSeed` does not overwrite an existing expression.
- An unchanged existing label is still a clean no-op.
- A changed description marks the changeset dirty and reprojects.

Capability tests cover the user-visible path:

- `workflow.EnsureVocabulary` overwrites existing descriptions for owned board
  labels.
- `contextmap.EnsureVocabulary` overwrites existing descriptions for owned
  stored labels.

The full repository verification gate remains `make verify`.
