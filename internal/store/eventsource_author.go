package store

import (
	"errors"
	"fmt"

	"atm/internal/eventsource"
)

type V2Draft struct {
	Actor   string
	Action  string
	Subject eventsource.Subject
	Payload map[string]any
}

// v2AuthorCtx is everything a locked writer needs: the current snapshot and
// fold (frontier, alias→identity resolution, taken-alias sets), a clock that
// has observed the persisted local HLC and every event in the file, and the
// writing replica id. It must only be built while holding the project lock.
type v2AuthorCtx struct {
	snap    *V2FileSnapshot
	state   *eventsource.State
	clock   *eventsource.Clock
	replica string
}

func (s *Store) beginV2AuthorLocked(code string) (*v2AuthorCtx, error) {
	snap, err := s.readV2File(code, true)
	if err != nil {
		return nil, err
	}
	state, err := eventsource.FoldEvents(snap.Events)
	if err != nil {
		return nil, err
	}
	replica, err := s.currentReplicaIDLocked()
	if err != nil {
		return nil, err
	}
	clock := eventsource.NewClock(nil)
	m, err := s.readStoreMeta()
	if err != nil {
		return nil, err
	}
	if m.LastHLC != nil {
		clock.Observe(*m.LastHLC) // spec authoring step 5: the persisted local HLC
	}
	for _, ev := range snap.Events {
		clock.Observe(ev.HLC)
	}
	return &v2AuthorCtx{snap: snap, state: state, clock: clock, replica: replica}, nil
}

// commitV2AuthorLocked appends the event line — the commit point — and then
// persists the local HLC. The metadata write is rebuildable state; the event
// line is the truth.
func (s *Store) commitV2AuthorLocked(code string, ev *eventsource.Event) error {
	if err := s.appendV2EventLineLocked(code, ev.Raw); err != nil {
		return err
	}
	// store.json is store-wide but this writer only holds the project lock,
	// so the read-modify-write must go through the store-scoped lock or a
	// concurrent writer in another project would drop our (or their)
	// ProjectFormats entry. Nesting a DIFFERENT lock name inside the project
	// lock is safe; WithLock is not reentrant on the same name.
	return s.mutateStoreMeta(func(m *StoreMeta) error {
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
// Both go through resolveV2Ref, which translates the eventsource-layer errors
// into the store's sentinels. That translation is the whole point: EVERY v2
// mutator resolves here, and internal/store is the compatibility API the CLI
// keys its exit codes off (cli.CodeForError -> errors.Is(err, ErrNotFound)).
// Returning eventsource.ErrNoMatch raw made `atm task set-title --task <typo>`
// exit 1 (generic) instead of 3 (not-found) and leaked an "eventsource:" prefix
// into user-facing output. The read side was pinned by
// TestV2ActiveMissingEntityReadsReturnErrNotFound; this is its write-side twin.
func (c *v2AuthorCtx) resolveTaskRef(alias string) (string, error) {
	return c.resolveV2Ref(alias, "task")
}

func (c *v2AuthorCtx) resolveCommentRef(alias string) (string, error) {
	return c.resolveV2Ref(alias, "comment")
}

func (c *v2AuthorCtx) resolveV2Ref(alias, kind string) (string, error) {
	m, err := c.state.Resolve(alias)
	if err != nil {
		if errors.Is(err, eventsource.ErrNoMatch) {
			return "", fmt.Errorf("%w: %s %q", ErrNotFound, kind, alias)
		}
		var amb *eventsource.AmbiguousError
		if errors.As(err, &amb) {
			// No v1 sentinel exists for an ambiguous id (v1 ids are exact), so
			// this is a caller-input problem: ErrUsage (CLI exit 2). The
			// candidate list rides along in the message — never pick one (L1-4).
			return "", fmt.Errorf("%w: %s", ErrUsage, amb.Error())
		}
		return "", err
	}
	if m.Kind != kind {
		return "", fmt.Errorf("%w: %q is a %s, not a %s", ErrUsage, alias, m.Kind, kind)
	}
	return m.ID, nil
}

// resolveProjectRef maps a project code to the project entity's identity.
// A project's alias IS its code, but the event model still keys project slot
// writes off subject.id (the identity of its project.created event), so every
// project mutator must go through the fold rather than sending the code.
func (c *v2AuthorCtx) resolveProjectRef(code string) (string, error) {
	p, ok := v2LiveProject(c.state, code)
	if !ok {
		return "", fmt.Errorf("%w: project %q", ErrNotFound, code)
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

// appendV2LabelUpsertsLocked is the v2 mirror of appendLabelUpsertsLocked: it
// auto-registers any label name a task/comment mutation asserts but the fold
// does not already hold live. The payload carries NO fields — label.upserted
// writes the existence slot unconditionally (writesOf), so an empty payload
// registers the label without clobbering a description/expr some other
// replica may have set. Caller MUST hold the project lock.
func (s *Store) appendV2LabelUpsertsLocked(code string, labels []string, actor string) error {
	if len(labels) == 0 {
		return nil
	}
	ctx, err := s.beginV2AuthorLocked(code)
	if err != nil {
		return err
	}
	for _, name := range labels {
		if l, ok := ctx.state.Labels[name]; ok && !l.Tombstoned {
			continue
		}
		if _, err := s.appendV2Locked(code, V2Draft{
			Actor:   actor,
			Action:  ActionLabelUpserted,
			Subject: eventsource.Subject{Kind: "label", Name: name},
			Payload: map[string]any{},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) appendV2Locked(code string, draft V2Draft) (*eventsource.Event, error) {
	ctx, err := s.beginV2AuthorLocked(code)
	if err != nil {
		return nil, err
	}
	ev, err := eventsource.NewEvent(ctx.clock, ctx.replica, ctx.snap.Frontier, eventsource.Draft{
		At:      Now(),
		Actor:   draft.Actor,
		Action:  draft.Action,
		Subject: draft.Subject,
		Payload: draft.Payload,
	})
	if err != nil {
		return nil, err
	}
	return ev, s.commitV2AuthorLocked(code, ev)
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

func (s *Store) appendV2TaskCreatedLocked(code, title, description string, labels []string, actor string) (*eventsource.Event, string, error) {
	ctx, err := s.beginV2AuthorLocked(code)
	if err != nil {
		return nil, "", err
	}
	ev, alias, err := eventsource.NewTaskCreated(ctx.clock, ctx.replica, ctx.snap.Frontier, eventsource.TaskCreateDraft{
		ProjectCode: code,
		At:          Now(),
		Actor:       actor,
		Title:       title,
		Description: description,
		Labels:      labels,
	}, takenTaskAliases(ctx.state))
	if err != nil {
		return nil, "", err
	}
	return ev, alias, s.commitV2AuthorLocked(code, ev)
}

func (s *Store) appendV2CommentCreatedLocked(code, taskAlias, body string, labels []string, replyToAlias, actor string) (*eventsource.Event, string, error) {
	ctx, err := s.beginV2AuthorLocked(code)
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
		At:         Now(),
		Actor:      actor,
		Body:       body,
		Labels:     labels,
	}, takenCommentAliases(ctx.state, taskRef))
	if err != nil {
		return nil, "", err
	}
	return ev, alias, s.commitV2AuthorLocked(code, ev)
}

// currentReplicaIDLocked returns the replica id to stamp on the next
// authored event. It defers entirely to ensureReplicaForWriteLocked
// (eventsource_replica.go), which mints on first use AND re-mints on every
// call if the store root looks like a copy of another instance -- see that
// function's doc comment for the detection rule and its known limitation.
func (s *Store) currentReplicaIDLocked() (string, error) {
	return s.ensureReplicaForWriteLocked()
}
