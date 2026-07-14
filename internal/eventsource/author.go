package eventsource

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// Draft is the caller-supplied part of a new event. Payload values must
// marshal to JSON; the keys the fold reads per action are defined by
// writesOf in fold.go.
type Draft struct {
	At      time.Time
	Actor   string
	Action  string
	Subject Subject
	Payload map[string]any
}

// NewEvent authors a v2 event: it ticks the clock, sorts and dedupes
// parents (array order must not fork identities, spec decision 12), and
// derives the identity by funnelling through Parse — one code path for
// every id in the system. replica must be a minted id; ReplicaV1 is
// reserved for the D6 upgrade. For creation events that carry a stored
// alias (task.created, comment.created), prefer NewTaskCreated /
// NewCommentCreated — they mint the alias from the pre-alias draft so the
// caller never has to discover the correct ordering (ATM-0125).
func NewEvent(clock *Clock, replica string, parents []string, d Draft) (*Event, error) {
	if replica == "" || replica == ReplicaV1 {
		return nil, fmt.Errorf("eventsource: invalid replica id %q", replica)
	}
	if d.Action == "" || d.Subject.Kind == "" {
		return nil, fmt.Errorf("eventsource: action and subject.kind are required")
	}
	ps := sortDedupeParents(parents)
	at := d.At
	if at.IsZero() {
		at = time.Now()
	}
	return assembleEvent(ps, clock.Tick(), replica, at, d.Actor, d.Action, d.Subject, d.Payload)
}

// TaskCreateDraft is the caller-supplied part of a task.created event.
type TaskCreateDraft struct {
	ProjectCode string
	At          time.Time
	Actor       string
	Title       string
	Description string
	Labels      []string
}

// NewTaskCreated authors a task.created event and mints its stored alias.
// The alias is a prefix of the PRE-ALIAS draft's SHA-256, not of the final
// event's identity (ATM-0125): the event is assembled without payload.alias,
// hashed, the alias is minted from that hash, then payload.alias is filled
// and the event is re-assembled and re-hashed to yield the final identity.
// The alias therefore never equals a prefix of the returned event's ID —
// nothing in the system may rely on that (dropped) invariant.
//
// taken reports the aliases the minting replica currently holds, so the
// prefix can be extended to disambiguate locally; a nil taken means no
// aliases are held (no extension). Returns the authored event and its
// minted alias.
func NewTaskCreated(clock *Clock, replica string, parents []string, d TaskCreateDraft, taken func(string) bool) (*Event, string, error) {
	if replica == "" || replica == ReplicaV1 {
		return nil, "", fmt.Errorf("eventsource: invalid replica id %q", replica)
	}
	if d.ProjectCode == "" {
		return nil, "", fmt.Errorf("eventsource: task create requires a project code")
	}
	if taken == nil {
		taken = func(string) bool { return false }
	}
	at := d.At
	if at.IsZero() {
		at = time.Now()
	}
	ps := sortDedupeParents(parents)
	hlc := clock.Tick()

	// Phase 1: assemble the pre-alias draft (payload without alias) and
	// hash it. The draft is a fully valid event minus the alias; Parse
	// yields its identity, which is the pre-image digest the alias is
	// derived from.
	payload := map[string]any{"title": d.Title}
	if d.Description != "" {
		payload["description"] = d.Description
	}
	if len(d.Labels) > 0 {
		payload["labels"] = d.Labels
	}
	draft, err := assembleEvent(ps, hlc, replica, at, d.Actor, ActionTaskCreated, Subject{Kind: "task"}, payload)
	if err != nil {
		return nil, "", err
	}

	// Phase 2: mint the alias from the pre-alias digest, write it into the
	// payload, and re-assemble to yield the final event + identity.
	alias := MintTaskAlias(d.ProjectCode, draft.ID, taken)
	payload["alias"] = alias
	ev, err := assembleEvent(ps, hlc, replica, at, d.Actor, ActionTaskCreated, Subject{Kind: "task"}, payload)
	if err != nil {
		return nil, "", err
	}
	return ev, alias, nil
}

// CommentCreateDraft is the caller-supplied part of a comment.created event.
// TaskAlias is the owning task's display alias (the comment alias prefix);
// TaskRef is the task's identity, written to payload.task_ref. ReplyToRef,
// if set, is the parent comment's identity, written to payload.reply_to_ref.
type CommentCreateDraft struct {
	TaskAlias  string
	TaskRef    string
	ReplyToRef string
	At         time.Time
	Actor      string
	Body       string
	Labels     []string
}

// NewCommentCreated authors a comment.created event and mints its stored
// alias. As with NewTaskCreated, the alias is minted from the pre-alias
// draft's SHA-256, not the final identity (ATM-0125). taken reports the
// aliases the minting replica currently holds within this task, for local
// disambiguation; nil means none. Returns the authored event and its
// minted alias.
func NewCommentCreated(clock *Clock, replica string, parents []string, d CommentCreateDraft, taken func(string) bool) (*Event, string, error) {
	if replica == "" || replica == ReplicaV1 {
		return nil, "", fmt.Errorf("eventsource: invalid replica id %q", replica)
	}
	if d.TaskAlias == "" || d.TaskRef == "" {
		return nil, "", fmt.Errorf("eventsource: comment create requires a task alias and task ref")
	}
	if taken == nil {
		taken = func(string) bool { return false }
	}
	at := d.At
	if at.IsZero() {
		at = time.Now()
	}
	ps := sortDedupeParents(parents)
	hlc := clock.Tick()

	payload := map[string]any{
		"task_ref": d.TaskRef,
		"body":     d.Body,
	}
	if d.ReplyToRef != "" {
		payload["reply_to_ref"] = d.ReplyToRef
	}
	if len(d.Labels) > 0 {
		payload["labels"] = d.Labels
	}
	draft, err := assembleEvent(ps, hlc, replica, at, d.Actor, ActionCommentCreated, Subject{Kind: "comment"}, payload)
	if err != nil {
		return nil, "", err
	}

	alias := MintCommentAlias(d.TaskAlias, draft.ID, taken)
	payload["alias"] = alias
	ev, err := assembleEvent(ps, hlc, replica, at, d.Actor, ActionCommentCreated, Subject{Kind: "comment"}, payload)
	if err != nil {
		return nil, "", err
	}
	return ev, alias, nil
}

// ProjectCreateDraft is the caller-supplied part of a project.created event.
// Code becomes the stored alias (a project's alias IS its code).
type ProjectCreateDraft struct {
	Code  string
	Name  string
	At    time.Time
	Actor string
}

// NewProjectCreated authors a project.created event. The alias is the
// project code (not hash-derived), so there is no fixed-point problem and
// no pre-alias draft; the helper exists so L3 has a uniform authoring
// surface. Returns the authored event and its alias (the code).
func NewProjectCreated(clock *Clock, replica string, parents []string, d ProjectCreateDraft) (*Event, string, error) {
	if replica == "" || replica == ReplicaV1 {
		return nil, "", fmt.Errorf("eventsource: invalid replica id %q", replica)
	}
	if d.Code == "" {
		return nil, "", fmt.Errorf("eventsource: project create requires a code")
	}
	at := d.At
	if at.IsZero() {
		at = time.Now()
	}
	ps := sortDedupeParents(parents)
	payload := map[string]any{"alias": d.Code, "name": d.Name}
	ev, err := assembleEvent(ps, clock.Tick(), replica, at, d.Actor, ActionProjectCreated, Subject{Kind: "project", Code: d.Code}, payload)
	if err != nil {
		return nil, "", err
	}
	return ev, d.Code, nil
}

// sortDedupeParents returns the sorted, deduplicated parent set, with a
// non-nil empty slice for a root event. Array order must not fork
// identities (spec decision 12).
func sortDedupeParents(parents []string) []string {
	ps := slices.Clone(parents)
	slices.Sort(ps)
	ps = slices.Compact(ps)
	if ps == nil {
		ps = []string{}
	}
	return ps
}

// assembleEvent builds the v2 envelope from its parts, marshals it,
// canonicalizes, and derives the identity via Parse — the single identity
// code path. The HLC is supplied (already ticked) so a two-phase authoring
// helper can hash the pre-alias draft and re-assemble the final event with
// the same stamp.
func assembleEvent(ps []string, hlc HLC, replica string, at time.Time, actor, action string, subject Subject, payload map[string]any) (*Event, error) {
	obj := map[string]any{
		"v":       2,
		"parents": ps,
		"hlc":     hlc,
		"replica": replica,
		"at":      at.UTC().Format(time.RFC3339Nano),
		"actor":   actor,
		"action":  action,
		"subject": subject,
	}
	if len(payload) > 0 {
		obj["payload"] = payload
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("eventsource: marshal draft: %w", err)
	}
	return Parse(raw)
}
