package eventlog

import (
	"fmt"

	"atm/internal/core"
	"atm/libs/eventsource"
)

type changeSet struct {
	e             *Engine
	code          string
	rootCommitted bool
}

var _ core.ChangeSet = (*changeSet)(nil)

// WithProjectWrite is the format-gated write transaction every live mutator
// runs in: project lock + under-lock v2 re-check (the old
// withProjectFormatLock), with fn free to interleave guards, intent appends
// and the facade's own reads/projection.
func (e *Engine) WithProjectWrite(code string, fn func(core.ChangeSet) error) error {
	return e.WithProjectFormatLock(code, StoreFormatV2, func() error {
		return fn(&changeSet{e: e, code: code})
	})
}

// WithProjectBirth establishes a brand-new v2 project: plain project lock
// (never the format gate — the format is being ESTABLISHED), entry written
// before the first append, best-effort entry rollback if fn fails before the
// root event committed. Exactly createProjectV2's crash-window contract.
//
// preflight runs FIRST, under the lock, BEFORE any storage registration is
// established — it is the caller's existence guard (a duplicate create fails
// here), and a preflight error returns with store metadata COMPLETELY
// untouched. This restores the pre-carve order, where media/cache were checked
// before setProjectFormat, so a failed duplicate create never wrote store.json
// (and, critically, never flipped an explicit "v1" entry to "v2" — which would
// hide a live v1 project's log as an empty v2 project).
//
// Only once preflight passes does birth write ProjectFormats[code]=v2 (before
// the first append). If fn then fails before the root event commits, the entry
// is restored to exactly what birth found — its prior value, or removed if
// there was none — so a failed birth is atomic on store.json regardless of any
// pre-existing (e.g. orphaned) entry. Once the root event is durable
// (cs.rootCommitted), the entry stands: the project now has v2 media.
func (e *Engine) WithProjectBirth(code string, preflight func() error, fn func(core.ChangeSet) error) error {
	return e.WithLock(code, func() error {
		if err := preflight(); err != nil {
			return err
		}
		m, err := e.ReadStoreMeta()
		if err != nil {
			return err
		}
		prior, hadEntry := m.ProjectFormats[code]
		if err := e.SetProjectFormat(code, StoreFormatV2); err != nil {
			return err
		}
		cs := &changeSet{e: e, code: code}
		if err := fn(cs); err != nil {
			if !cs.rootCommitted {
				if hadEntry {
					_ = e.SetProjectFormat(code, prior)
				} else {
					_ = e.RemoveProjectFormat(code)
				}
			}
			return err
		}
		return nil
	})
}

// ---- ProjectWriter ----

// CreateProject carries createProjectV2's root-event block: the fresh file has
// an empty frontier, so project.created carries parents [] — beginAuthorLocked
// derives exactly that from the (absent) events.v2.jsonl. rootCommitted flips
// once the event is durable, which is what tells WithProjectBirth to stop
// rolling the format entry back.
func (cs *changeSet) CreateProject(name, actor string) error {
	ctx, err := cs.e.beginAuthorLocked(cs.code)
	if err != nil {
		return err
	}
	ev, _, err := eventsource.NewProjectCreated(ctx.clock, ctx.replica, ctx.snap.Frontier, eventsource.ProjectCreateDraft{
		Code: cs.code, Name: name, At: cs.e.now(), Actor: actor,
	})
	if err != nil {
		return err
	}
	if err := cs.e.commitAuthorLocked(cs.code, ev); err != nil {
		return err
	}
	cs.rootCommitted = true
	return nil
}

// SetProjectName emits project.name-changed against the project's identity
// (never its code: the fold keys slot writes off subject.id).
func (cs *changeSet) SetProjectName(name, actor string) error {
	ctx, err := cs.e.beginAuthorLocked(cs.code)
	if err != nil {
		return err
	}
	ref, err := ctx.resolveProjectRef(cs.code)
	if err != nil {
		return err
	}
	_, err = cs.e.appendLocked(cs.code, draft{
		Actor:   actor,
		Action:  actionProjectNameChanged,
		Subject: eventsource.Subject{Kind: "project", ID: ref, Code: cs.code},
		Payload: map[string]any{"name": name},
	})
	return err
}

// ForgetProject drops the storage registration; the facade removes the media
// and cache rows. No project.removed tombstone is written locally (that DAG
// event exists for remote observers; local removal is a filesystem op).
func (cs *changeSet) ForgetProject() error {
	return cs.e.RemoveProjectFormat(cs.code)
}

// ---- TaskWriter ----

func (cs *changeSet) CreateTask(d core.TaskDraft, actor string) (string, error) {
	_, alias, err := cs.e.appendTaskCreatedLocked(cs.code, d.Title, d.Description, d.Labels, actor)
	return alias, err
}

func (cs *changeSet) SetTaskTitle(id, title, actor string) error {
	return cs.mutateTask(id, actionTaskTitleChanged, actor, map[string]any{"title": title})
}

func (cs *changeSet) SetTaskDescription(id, description, actor string) error {
	return cs.mutateTask(id, actionTaskDescChanged, actor, map[string]any{"description": description})
}

// AddTaskLabel emits task.label-added; the label-existence guard and the
// auto-registration EnsureLabels stay facade-side (taskLabelAddV2's order).
func (cs *changeSet) AddTaskLabel(id, label, actor string) error {
	return cs.mutateTask(id, actionTaskLabelAdded, actor, map[string]any{"label": label})
}

func (cs *changeSet) RemoveTaskLabel(id, label, actor string) error {
	return cs.mutateTask(id, actionTaskLabelRemoved, actor, map[string]any{"label": label})
}

func (cs *changeSet) RemoveTask(id, actor string) error {
	return cs.mutateTask(id, actionTaskRemoved, actor, nil)
}

// mutateTask appends one v2 task event against the task's IDENTITY (the fold
// keys every slot write off subject.id, never the alias). It begins its own
// authorCtx to resolve the alias, then appendLocked begins another — exactly
// the two-begin shape mutateTaskV2 had per call.
func (cs *changeSet) mutateTask(id, action, actor string, payload map[string]any) error {
	ctx, err := cs.e.beginAuthorLocked(cs.code)
	if err != nil {
		return err
	}
	ref, err := ctx.resolveTaskRef(id)
	if err != nil {
		return err
	}
	_, err = cs.e.appendLocked(cs.code, draft{
		Actor:   actor,
		Action:  action,
		Subject: eventsource.Subject{Kind: "task", ID: ref},
		Payload: payload,
	})
	return err
}

// ---- CommentWriter ----

func (cs *changeSet) CreateComment(d core.CommentDraft, actor string) (string, error) {
	_, alias, err := cs.e.appendCommentCreatedLocked(cs.code, d.TaskID, d.Body, d.Labels, d.ReplyTo, actor)
	return alias, err
}

func (cs *changeSet) SetCommentBody(id, body, actor string) error {
	return cs.mutateComment(id, actionCommentBodyChanged, actor, map[string]any{"body": body})
}

func (cs *changeSet) AddCommentLabel(id, label, actor string) error {
	return cs.mutateComment(id, actionCommentLabelAdded, actor, map[string]any{"label": label})
}

func (cs *changeSet) RemoveCommentLabel(id, label, actor string) error {
	return cs.mutateComment(id, actionCommentLabelRemoved, actor, map[string]any{"label": label})
}

func (cs *changeSet) RemoveComment(id, actor string) error {
	return cs.mutateComment(id, actionCommentRemoved, actor, nil)
}

// mutateComment appends one v2 comment event against the comment's IDENTITY
// and reprojects facade-side (mutateCommentV2's inner body).
func (cs *changeSet) mutateComment(id, action, actor string, payload map[string]any) error {
	ctx, err := cs.e.beginAuthorLocked(cs.code)
	if err != nil {
		return err
	}
	ref, err := ctx.resolveCommentRef(id)
	if err != nil {
		return err
	}
	_, err = cs.e.appendLocked(cs.code, draft{
		Actor:   actor,
		Action:  action,
		Subject: eventsource.Subject{Kind: "comment", ID: ref},
		Payload: payload,
	})
	return err
}

// ---- LabelWriter ----

// UpsertLabel emits label.upserted. A label's NAME is its identity in the
// fold, so the subject carries the name and there is nothing to resolve. Only
// the fields being SET go into the payload (the writesOf action table): an
// omitted key writes no slot, so a concurrent writer's value survives.
func (cs *changeSet) UpsertLabel(name string, f core.LabelFields, actor string) error {
	payload := map[string]any{}
	if f.Description != nil {
		payload["description"] = *f.Description
	}
	if f.Expr != nil {
		payload["expr"] = *f.Expr
	}
	_, err := cs.e.appendLocked(cs.code, draft{
		Actor:   actor,
		Action:  actionLabelUpserted,
		Subject: eventsource.Subject{Kind: "label", Name: name},
		Payload: payload,
	})
	return err
}

// SeedLabel is a no-op when the label is already live in the fold (the fold,
// not cache.db, is the authority for a v2 project); otherwise it upserts
// description-always and expr-if-nonempty.
func (cs *changeSet) SeedLabel(name, description, expr, actor string) error {
	ctx, err := cs.e.beginAuthorLocked(cs.code)
	if err != nil {
		return err
	}
	if l, ok := ctx.state.Labels[name]; ok && !l.Tombstoned {
		return nil
	}
	payload := map[string]any{"description": description}
	if expr != "" {
		payload["expr"] = expr
	}
	_, err = cs.e.appendLocked(cs.code, draft{
		Actor:   actor,
		Action:  actionLabelUpserted,
		Subject: eventsource.Subject{Kind: "label", Name: name},
		Payload: payload,
	})
	return err
}

func (cs *changeSet) EnsureLabels(names []string, actor string) error {
	return cs.e.appendLabelUpsertsLocked(cs.code, names, actor)
}

// RemoveLabel emits label.removed; ErrNotFound when the fold does not hold the
// name live. The facade counts retained usage after the txn.
func (cs *changeSet) RemoveLabel(name, actor string) error {
	ctx, err := cs.e.beginAuthorLocked(cs.code)
	if err != nil {
		return err
	}
	if l, ok := ctx.state.Labels[name]; !ok || l.Tombstoned {
		return fmt.Errorf("%w: label %q", core.ErrNotFound, name)
	}
	_, err = cs.e.appendLocked(cs.code, draft{
		Actor:   actor,
		Action:  actionLabelRemoved,
		Subject: eventsource.Subject{Kind: "label", Name: name},
	})
	return err
}

// ---- guards / resolves ----

func (cs *changeSet) RequireProject() error {
	ctx, err := cs.e.beginAuthorLocked(cs.code)
	if err != nil {
		return err
	}
	_, err = ctx.resolveProjectRef(cs.code)
	return err
}

func (cs *changeSet) ResolveTask(id string) error {
	ctx, err := cs.e.beginAuthorLocked(cs.code)
	if err != nil {
		return err
	}
	_, err = ctx.resolveTaskRef(id)
	return err
}

func (cs *changeSet) ResolveComment(id string) error {
	ctx, err := cs.e.beginAuthorLocked(cs.code)
	if err != nil {
		return err
	}
	_, err = ctx.resolveCommentRef(id)
	return err
}

func (cs *changeSet) TaskHasLabel(id, label string) (bool, error) {
	ctx, err := cs.e.beginAuthorLocked(cs.code)
	if err != nil {
		return false, err
	}
	ref, err := ctx.resolveTaskRef(id)
	if err != nil {
		return false, err
	}
	if t, ok := ctx.state.Tasks[ref]; ok {
		for _, l := range t.Labels {
			if l == label {
				return true, nil
			}
		}
	}
	return false, nil
}

func (cs *changeSet) CommentHasLabel(id, label string) (bool, error) {
	ctx, err := cs.e.beginAuthorLocked(cs.code)
	if err != nil {
		return false, err
	}
	ref, err := ctx.resolveCommentRef(id)
	if err != nil {
		return false, err
	}
	if c, ok := ctx.state.Comments[ref]; ok {
		for _, l := range c.Labels {
			if l == label {
				return true, nil
			}
		}
	}
	return false, nil
}

// HasLiveTasks answers RemoveProject's "is this project empty?" guard from the
// FOLD, not from cache rows (v2HasTasksGuardLocked's scan).
func (cs *changeSet) HasLiveTasks() (bool, error) {
	ctx, err := cs.e.beginAuthorLocked(cs.code)
	if err != nil {
		return false, err
	}
	for _, t := range ctx.state.Tasks {
		if !t.Tombstoned {
			return true, nil
		}
	}
	return false, nil
}

// Snapshot is the strict re-read the facade projects at the end of a
// transaction — exactly reprojectV2Locked's read posture, now including this
// transaction's own committed writes.
func (cs *changeSet) Snapshot() (*core.ProjectSnapshot, error) {
	return cs.e.Snapshot(cs.code)
}
