package eventlog

import (
	"errors"
	"fmt"

	"atm/internal/core"
	"atm/libs/eventsource"
)

// Action enum — closed. These are the write-side action strings the engine
// stamps on authored events. The engine keeps its own unexported copies here;
// store's exported store.Action* constants (internal/store/log.go) carry the
// identical literals and serve the history views and store tests that render
// from the same strings. The two sets are deliberately independent across the
// carve seam — the engine no longer reaches into the store package for them.
const (
	actionProjectCreated            = "project.created"
	actionProjectNameChanged        = "project.name-changed"
	actionProjectRemoved            = "project.removed"
	actionProjectCapabilityEnabled  = "project.capability-enabled"
	actionProjectCapabilityDisabled = "project.capability-disabled"
	actionTaskCreated               = "task.created"
	actionTaskTitleChanged          = "task.title-changed"
	actionTaskDescChanged           = "task.description-changed"
	actionTaskLabelAdded            = "task.label-added"
	actionTaskLabelRemoved          = "task.label-removed"
	actionTaskRemoved               = "task.removed"
	actionTaskCapabilityMetaSet     = "task.capability-meta-set"
	actionLabelUpserted             = "label.upserted"
	actionLabelRemoved              = "label.removed"
	actionCommentCreated            = "comment.created"
	actionCommentBodyChanged        = "comment.body-changed"
	actionCommentLabelAdded         = "comment.label-added"
	actionCommentLabelRemoved       = "comment.label-removed"
	actionCommentRemoved            = "comment.removed"
)

type draft struct {
	Actor   string
	Action  string
	Subject eventsource.Subject
	Payload map[string]any
}

// authorCtx is everything a locked writer needs: the current snapshot and
// fold (frontier, alias→identity resolution, taken-alias sets), a clock that
// has observed the persisted local HLC and every event in the file, and the
// writing replica id. It must only be built while holding the project lock.
type authorCtx struct {
	snap    *V2FileSnapshot
	state   *eventsource.State
	clock   *eventsource.Clock
	replica string
}

func (e *Engine) beginAuthorLocked(code string) (*authorCtx, error) {
	snap, err := e.ReadV2File(code, true)
	if err != nil {
		return nil, err
	}
	state, err := eventsource.FoldEvents(snap.Events)
	if err != nil {
		return nil, err
	}
	replica, err := e.EnsureReplicaForWriteLocked()
	if err != nil {
		return nil, err
	}
	clock := eventsource.NewClock(e.opts.ClockNow)
	m, err := e.ReadStoreMeta()
	if err != nil {
		return nil, err
	}
	if m.LastHLC != nil {
		clock.Observe(*m.LastHLC) // spec authoring step 5: the persisted local HLC
	}
	for _, ev := range snap.Events {
		clock.Observe(ev.HLC)
	}
	return &authorCtx{snap: snap, state: state, clock: clock, replica: replica}, nil
}

// commitAuthorLocked appends the event line — the commit point — and then
// persists the local HLC. The metadata write is rebuildable state; the event
// line is the truth.
func (e *Engine) commitAuthorLocked(code string, ev *eventsource.Event) error {
	if err := e.AppendEventLineLocked(code, ev.Raw); err != nil {
		return err
	}
	// store.json is store-wide but this writer only holds the project lock,
	// so the read-modify-write must go through the store-scoped lock or a
	// concurrent writer in another project would drop our (or their)
	// ProjectFormats entry. Nesting a DIFFERENT lock name inside the project
	// lock is safe; WithLock is not reentrant on the same name.
	return e.MutateStoreMeta(func(m *StoreMeta) error {
		h := ev.HLC
		if m.LastHLC == nil || h.Compare(*m.LastHLC) > 0 {
			m.LastHLC = &h
		}
		return nil
	})
}

// resolveTaskRef and resolveCommentRef map a user-facing alias to the
// identity the event model needs (subject.id, task_ref, reply_to_ref).
// An alias collision surfaces as *eventsource.AmbiguousError — the caller
// reports the candidates; never silently pick one (L1-4).
//
// Both go through resolveRef, which translates the eventsource-layer errors
// into the store's sentinels. That translation is the whole point: EVERY v2
// mutator resolves here, and internal/store is the compatibility API the CLI
// keys its exit codes off (cli.CodeForError -> errors.Is(err, ErrNotFound)).
// Returning eventsource.ErrNoMatch raw made `atm task set-title --task <typo>`
// exit 1 (generic) instead of 3 (not-found) and leaked an "eventsource:" prefix
// into user-facing output. The read side was pinned by
// TestV2ActiveMissingEntityReadsReturnErrNotFound; this is its write-side twin.
func (c *authorCtx) resolveTaskRef(alias string) (string, error) {
	return c.resolveRef(alias, "task")
}

func (c *authorCtx) resolveCommentRef(alias string) (string, error) {
	return c.resolveRef(alias, "comment")
}

func (c *authorCtx) resolveRef(alias, kind string) (string, error) {
	m, err := c.state.Resolve(alias)
	if err != nil {
		if errors.Is(err, eventsource.ErrNoMatch) {
			return "", fmt.Errorf("%w: %s %q", core.ErrNotFound, kind, alias)
		}
		var amb *eventsource.AmbiguousError
		if errors.As(err, &amb) {
			// No v1 sentinel exists for an ambiguous id (v1 ids are exact), so
			// this is a caller-input problem: ErrUsage (CLI exit 2). The
			// candidate list rides along in the message — never pick one (L1-4).
			return "", fmt.Errorf("%w: %s", core.ErrUsage, amb.Error())
		}
		return "", err
	}
	if m.Kind != kind {
		return "", fmt.Errorf("%w: %q is a %s, not a %s", core.ErrUsage, alias, m.Kind, kind)
	}
	return m.ID, nil
}

// resolveProjectRef maps a project code to the project entity's identity.
// A project's alias IS its code, but the event model still keys project slot
// writes off subject.id (the identity of its project.created event), so every
// project mutator must go through the fold rather than sending the code.
func (c *authorCtx) resolveProjectRef(code string) (string, error) {
	p, ok := v2LiveProject(c.state, code)
	if !ok {
		return "", fmt.Errorf("%w: project %q", core.ErrNotFound, code)
	}
	return p.ID, nil
}

// v2LiveProject finds the live project entity for code in a fold state.
func v2LiveProject(st *eventsource.State, code string) (*eventsource.ProjectState, bool) {
	for _, p := range st.Projects {
		if p.Code == code && !p.Tombstoned {
			return p, true
		}
	}
	return nil, false
}

// appendLabelUpsertsLocked auto-registers any label name a task/comment
// mutation asserts but the fold does not already hold live, returning how
// many label.upserted events it appended (0 when every name was live). It was
// the v2 mirror of the v1 appendLabelUpsertsLocked, deleted in D-Task5b along
// with the v1 write branches that were its only callers. The payload carries
// NO fields — label.upserted writes the existence slot unconditionally
// (writesOf), so an empty payload registers the label without clobbering a
// description/expr some other replica may have set. Caller MUST hold the
// project lock.
func (e *Engine) appendLabelUpsertsLocked(code string, labels []string, actor string) (int, error) {
	if len(labels) == 0 {
		return 0, nil
	}
	ctx, err := e.beginAuthorLocked(code)
	if err != nil {
		return 0, err
	}
	appended := 0
	for _, name := range labels {
		if l, ok := ctx.state.Labels[name]; ok && !l.Tombstoned {
			continue
		}
		if _, err := e.appendLocked(code, draft{
			Actor:   actor,
			Action:  actionLabelUpserted,
			Subject: eventsource.Subject{Kind: "label", Name: name},
			Payload: map[string]any{},
		}); err != nil {
			return appended, err
		}
		appended++
	}
	return appended, nil
}

func (e *Engine) appendLocked(code string, d draft) (*eventsource.Event, error) {
	ctx, err := e.beginAuthorLocked(code)
	if err != nil {
		return nil, err
	}
	ev, err := eventsource.NewEvent(ctx.clock, ctx.replica, ctx.snap.Frontier, eventsource.Draft{
		At:      e.now(),
		Actor:   d.Actor,
		Action:  d.Action,
		Subject: d.Subject,
		Payload: d.Payload,
	})
	if err != nil {
		return nil, err
	}
	return ev, e.commitAuthorLocked(code, ev)
}

// takenTaskAliases and takenCommentAliases build the collision sets handed to
// the alias minter: every alias the fold currently holds (tombstoned entities
// included — their aliases are still resolvable, so reuse would be ambiguous).
// A hit extends the minted prefix (eventsource.mintAlias), which is the only
// mechanism keeping local lookups unambiguous.
func takenTaskAliases(st *eventsource.State) func(string) bool {
	taken := map[string]bool{}
	for _, t := range st.Tasks {
		taken[t.Alias] = true
	}
	return func(a string) bool { return taken[a] }
}

// Comment aliases need only disambiguate within their own task, so the set is
// scoped to comments whose task_ref is taskRef.
func takenCommentAliases(st *eventsource.State, taskRef string) func(string) bool {
	taken := map[string]bool{}
	for _, c := range st.Comments {
		if c.TaskRef == taskRef {
			taken[c.Alias] = true
		}
	}
	return func(a string) bool { return taken[a] }
}

func (e *Engine) appendTaskCreatedLocked(code, title, description string, labels []string, actor string) (*eventsource.Event, string, error) {
	ctx, err := e.beginAuthorLocked(code)
	if err != nil {
		return nil, "", err
	}
	ev, alias, err := eventsource.NewTaskCreated(ctx.clock, ctx.replica, ctx.snap.Frontier, eventsource.TaskCreateDraft{
		ProjectCode: code,
		At:          e.now(),
		Actor:       actor,
		Title:       title,
		Description: description,
		Labels:      labels,
	}, takenTaskAliases(ctx.state))
	if err != nil {
		return nil, "", err
	}
	return ev, alias, e.commitAuthorLocked(code, ev)
}

func (e *Engine) appendCommentCreatedLocked(code, taskAlias, body string, labels []string, replyToAlias, actor string) (*eventsource.Event, string, error) {
	ctx, err := e.beginAuthorLocked(code)
	if err != nil {
		return nil, "", err
	}
	taskRef, err := ctx.resolveTaskRef(taskAlias)
	if err != nil {
		return nil, "", err
	}
	replyToRef := ""
	if replyToAlias != "" {
		if replyToRef, err = ctx.resolveCommentRef(replyToAlias); err != nil {
			return nil, "", err
		}
	}
	ev, alias, err := eventsource.NewCommentCreated(ctx.clock, ctx.replica, ctx.snap.Frontier, eventsource.CommentCreateDraft{
		TaskAlias:  taskAlias,
		TaskRef:    taskRef,
		ReplyToRef: replyToRef,
		At:         e.now(),
		Actor:      actor,
		Body:       body,
		Labels:     labels,
	}, takenCommentAliases(ctx.state, taskRef))
	if err != nil {
		return nil, "", err
	}
	return ev, alias, e.commitAuthorLocked(code, ev)
}
