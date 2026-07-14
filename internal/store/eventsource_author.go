package store

import (
	"crypto/rand"
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
		m.LastHLC = &h
		return nil
	})
}

// resolveTaskRef and resolveCommentRef map a user-facing alias to the
// identity the event model needs (subject.id, task_ref, reply_to_ref).
// An alias collision surfaces as *eventsource.AmbiguousError — the caller
// reports the candidates; never silently pick one (L1-4).
func (c *v2AuthorCtx) resolveTaskRef(alias string) (string, error) {
	m, err := c.state.Resolve(alias)
	if err != nil {
		return "", err
	}
	if m.Kind != "task" {
		return "", fmt.Errorf("%w: %q is a %s, not a task", ErrUsage, alias, m.Kind)
	}
	return m.ID, nil
}

func (c *v2AuthorCtx) resolveCommentRef(alias string) (string, error) {
	m, err := c.state.Resolve(alias)
	if err != nil {
		return "", err
	}
	if m.Kind != "comment" {
		return "", fmt.Errorf("%w: %q is a %s, not a comment", ErrUsage, alias, m.Kind)
	}
	return m.ID, nil
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

func (s *Store) currentReplicaIDLocked() (string, error) {
	m, err := s.readStoreMeta()
	if err != nil {
		return "", err
	}
	if m.ReplicaID != "" {
		return m.ReplicaID, nil
	}
	// First use: mint under the store-scoped lock, re-reading inside it so a
	// replica id minted by a racing writer wins instead of being clobbered.
	var id string
	err = s.mutateStoreMeta(func(m *StoreMeta) error {
		if m.ReplicaID == "" {
			minted, err := eventsource.MintReplicaID(rand.Reader)
			if err != nil {
				return err
			}
			m.ReplicaID = minted
		}
		id = m.ReplicaID
		return nil
	})
	if err != nil {
		return "", err
	}
	return id, nil
}
