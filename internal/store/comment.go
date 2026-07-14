package store

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"atm/internal/eventsource"
)

func (s *Store) CreateComment(taskID, body string, labels []string, replyTo, actor string) (*Comment, error) {
	if body == "" {
		return nil, fmt.Errorf("%w: body is required", ErrUsage)
	}
	if err := s.validateActor(actor); err != nil {
		return nil, err
	}
	code, _, ok := ParseTaskID(taskID)
	if !ok {
		return nil, fmt.Errorf("%w: invalid task id %q", ErrUsage, taskID)
	}
	if replyTo != "" {
		rtask, ok := commentTaskAlias(replyTo)
		if !ok {
			return nil, fmt.Errorf("%w: invalid reply-to %q", ErrUsage, replyTo)
		}
		if rtask != taskID {
			return nil, fmt.Errorf("%w: reply-to %q must belong to task %q", ErrUsage, replyTo, taskID)
		}
	}
	for _, l := range labels {
		if err := ValidateLabelName(l); err != nil {
			return nil, err
		}
		if err := s.labelProjectExists(l); err != nil {
			return nil, err
		}
	}
	if f, err := s.dispatchFormat(code); err != nil {
		return nil, err
	} else if f == StoreFormatV2 {
		return s.createCommentV2(code, taskID, body, labels, replyTo, actor)
	}
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	var created *Comment
	err = s.withProjectFormatLock(code, StoreFormatV1, func() error {
		t, err := s.getTaskLocked(taskID)
		if err != nil {
			return err
		}
		n := t.NextCommentN
		idN := n + 1
		id := RenderCommentID(taskID, idN)
		ts := Now()
		labelsSorted := append([]string(nil), labels...)
		sort.Strings(labelsSorted)
		c := &Comment{
			ID:        id,
			TaskID:    taskID,
			ReplyTo:   replyTo,
			Body:      body,
			Labels:    labelsSorted,
			CreatedAt: ts,
			CreatedBy: actor,
			UpdatedAt: ts,
			UpdatedBy: actor,
		}
		if _, err := s.appendLabelUpsertsLocked(code, labels, actor, ts); err != nil {
			return err
		}
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      ts,
			Actor:   actor,
			Action:  ActionCommentCreated,
			Subject: Subject{Kind: "comment", ID: id},
			Payload: mustMarshal(c),
		})
		if err != nil {
			return err
		}
		c.LogSeq = entry.Seq
		t.NextCommentN = idN
		t.UpdatedAt = ts
		t.UpdatedBy = actor
		metaEntry, err := s.appendLogLocked(code, LogEntry{
			At:      ts,
			Actor:   actor,
			Action:  ActionTaskMetaChanged,
			Subject: Subject{Kind: "task", ID: taskID},
			Payload: mustMarshal(t),
		})
		if err != nil {
			return err
		}
		t.LogSeq = metaEntry.Seq
		if err := cacheUpsertComment(db, c); err != nil {
			return err
		}
		if err := cacheUpsertTask(db, t); err != nil {
			return err
		}
		created = c
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// commentProjectFormat parses a comment alias for its project code and
// resolves the project's EFFECTIVE format. v2 comment aliases carry hex
// segments, so this keys on the full alias string (Task 2b).
func (s *Store) commentProjectFormat(id string) (string, StoreFormat, error) {
	code, _, _, ok := ParseCommentID(id)
	if !ok {
		return "", "", fmt.Errorf("%w: invalid comment id %q", ErrUsage, id)
	}
	f, err := s.dispatchFormat(code)
	if err != nil {
		return "", "", err
	}
	return code, f, nil
}

// createCommentV2 writes comment.created (plus any label.upserted
// auto-registration) to events.v2.jsonl. Both the alias and the task/reply-to
// identity references come from the eventsource helper, which resolves them
// from the fold — L3 never mints an alias or resolves an identity itself
// (ATM-0125). There is no v1 task.meta-changed counterpart: NextCommentN is v1
// bookkeeping and has no meaning under v2's hash aliases.
func (s *Store) createCommentV2(code, taskID, body string, labels []string, replyTo, actor string) (*Comment, error) {
	var created *Comment
	err := s.withProjectFormatLock(code, StoreFormatV2, func() error {
		// Resolve the task (and reply-to) reference BEFORE appending anything.
		// appendV2CommentCreatedLocked resolves them too — that is where the
		// values actually used come from — but an event append is DURABLE, so
		// discovering there that the task does not exist would already have
		// committed the label.upserted events below, and the error path skips
		// reprojectV2Locked, leaving cache.db and the v2 freshness count behind
		// the file. v1's CreateComment (getTaskLocked first) and createTaskV2
		// (resolveProjectRef + label validation first) both validate first;
		// this is the same rule.
		ctx, err := s.beginV2AuthorLocked(code)
		if err != nil {
			return err
		}
		if _, err := ctx.resolveTaskRef(taskID); err != nil {
			return err
		}
		if replyTo != "" {
			if _, err := ctx.resolveCommentRef(replyTo); err != nil {
				return err
			}
		}
		if err := s.appendV2LabelUpsertsLocked(code, labels, actor); err != nil {
			return err
		}
		_, alias, err := s.appendV2CommentCreatedLocked(code, taskID, body, labels, replyTo, actor)
		if err != nil {
			return err
		}
		if err := s.reprojectV2Locked(code); err != nil {
			return err
		}
		db, err := s.cacheDB()
		if err != nil {
			return err
		}
		c, ok, err := cacheGetComment(db, alias)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: comment %q", ErrNotFound, alias)
		}
		created = c
		return nil
	})
	return created, err
}

// mutateCommentV2 appends one v2 comment event against the comment's IDENTITY
// and reprojects.
func (s *Store) mutateCommentV2(code, id, action, actor string, payload map[string]any) error {
	return s.withProjectFormatLock(code, StoreFormatV2, func() error {
		ctx, err := s.beginV2AuthorLocked(code)
		if err != nil {
			return err
		}
		ref, err := ctx.resolveCommentRef(id)
		if err != nil {
			return err
		}
		if _, err := s.appendV2Locked(code, V2Draft{
			Actor:   actor,
			Action:  action,
			Subject: eventsource.Subject{Kind: "comment", ID: ref},
			Payload: payload,
		}); err != nil {
			return err
		}
		return s.reprojectV2Locked(code)
	})
}

// commentLabelAddV2 auto-registers the label (v1 parity) and asserts membership.
func (s *Store) commentLabelAddV2(code, id, label, actor string) error {
	return s.withProjectFormatLock(code, StoreFormatV2, func() error {
		ctx, err := s.beginV2AuthorLocked(code)
		if err != nil {
			return err
		}
		ref, err := ctx.resolveCommentRef(id)
		if err != nil {
			return err
		}
		if c, ok := ctx.state.Comments[ref]; ok {
			for _, l := range c.Labels {
				if l == label {
					return nil
				}
			}
		}
		if err := s.appendV2LabelUpsertsLocked(code, []string{label}, actor); err != nil {
			return err
		}
		if _, err := s.appendV2Locked(code, V2Draft{
			Actor:   actor,
			Action:  ActionCommentLabelAdded,
			Subject: eventsource.Subject{Kind: "comment", ID: ref},
			Payload: map[string]any{"label": label},
		}); err != nil {
			return err
		}
		return s.reprojectV2Locked(code)
	})
}

func (s *Store) GetComment(id string) (*Comment, error) {
	code, _, _, ok := ParseCommentID(id)
	if !ok {
		return nil, fmt.Errorf("%w: invalid comment id %q", ErrUsage, id)
	}
	return s.getCommentWithRebuild(id, code, func() error {
		return s.WithLock(code, func() error { return s.rebuildCommentFromLog(id, code) })
	})
}

// getCommentLocked is identical to GetComment except that, on a cache
// miss/stale hit, it calls rebuildCommentFromLog directly instead of
// wrapping it in s.WithLock. Callers MUST already hold the comment's
// project lock (i.e. be running inside their own s.WithLock(code, ...)
// closure) — calling GetComment in that situation would re-enter the
// (non-reentrant) mutex and deadlock.
func (s *Store) getCommentLocked(id string) (*Comment, error) {
	code, _, _, ok := ParseCommentID(id)
	if !ok {
		return nil, fmt.Errorf("%w: invalid comment id %q", ErrUsage, id)
	}
	return s.getCommentWithRebuild(id, code, func() error { return s.rebuildCommentFromLog(id, code) })
}

// getCommentWithRebuild contains the fast-path cache read + staleness check
// shared by GetComment and getCommentLocked. It is parameterized only by
// how the rebuild-from-log call itself gets invoked: wrapped in a fresh
// s.WithLock (GetComment, for callers that do not already hold the lock) or
// called directly (getCommentLocked, for callers that do).
func (s *Store) getCommentWithRebuild(id, code string, rebuild func() error) (*Comment, error) {
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	c, found, err := cacheGetComment(db, id)
	if err != nil {
		return nil, err
	}
	if !found {
		if err := rebuild(); err != nil {
			return nil, err
		}
		c, found, err = cacheGetComment(db, id)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("%w: comment %q", ErrNotFound, id)
		}
		return c, nil
	}
	last, lastErr := s.LastLogSeq(code)
	if lastErr != nil {
		return nil, lastErr
	}
	if c.LogSeq > last {
		return nil, fmt.Errorf("%w: comment %q cache LogSeq=%d > log LastSeq=%d", ErrIntegrity, id, c.LogSeq, last)
	}
	commentLast, err := s.lastCommentEventSeq(code, id)
	if err != nil {
		return nil, err
	}
	if c.LogSeq < commentLast {
		if err := rebuild(); err != nil {
			return nil, err
		}
		c, found, err = cacheGetComment(db, id)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("%w: comment %q", ErrNotFound, id)
		}
	}
	return c, nil
}

func (s *Store) lastCommentEventSeq(code, id string) (int, error) {
	entries, err := s.ReadLog(code)
	if err != nil {
		return 0, err
	}
	last := 0
	for _, e := range entries {
		if e.Subject.Kind == "comment" && e.Subject.ID == id {
			last = e.Seq
		}
	}
	return last, nil
}

func (s *Store) rebuildCommentFromLog(id, code string) error {
	entries, err := s.ReadLog(code)
	if err != nil && !IsIntegrity(err) {
		return err
	}
	var c *Comment
	lastSeq := 0
	for _, e := range entries {
		if e.Subject.Kind != "comment" || e.Subject.ID != id {
			continue
		}
		lastSeq = e.Seq
		if e.Action == ActionCommentRemoved {
			c = nil
			continue
		}
		var cc Comment
		if err := json.Unmarshal(e.Payload, &cc); err == nil {
			c = &cc
		}
	}
	if c == nil {
		return fmt.Errorf("%w: comment %q", ErrNotFound, id)
	}
	c.LogSeq = lastSeq
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return cacheUpsertComment(db, c)
}

func (s *Store) ListComments(taskID string) ([]*Comment, error) {
	if _, _, ok := ParseTaskID(taskID); !ok {
		return nil, fmt.Errorf("%w: invalid task id %q", ErrUsage, taskID)
	}
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	out, err := cacheListComments(db, taskID)
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []*Comment{}
	}
	return out, nil
}

func (s *Store) SetCommentBody(id, body, actor string) error {
	if body == "" {
		return fmt.Errorf("%w: body is required", ErrUsage)
	}
	if err := s.validateActor(actor); err != nil {
		return err
	}
	return s.mutateComment(id, actor, func(c *Comment, now time.Time) {
		c.Body = body
	}, ActionCommentBodyChanged, map[string]any{"body": body})
}

func (s *Store) CommentLabelRemove(id, label, actor string) error {
	return s.mutateComment(id, actor, func(c *Comment, now time.Time) {
		out := c.Labels[:0]
		for _, l := range c.Labels {
			if l != label {
				out = append(out, l)
			}
		}
		c.Labels = out
	}, ActionCommentLabelRemoved, map[string]any{"label": label})
}

func (s *Store) RemoveComment(id, actor string) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	code, f, err := s.commentProjectFormat(id)
	if err != nil {
		return err
	}
	if f == StoreFormatV2 {
		return s.mutateCommentV2(code, id, ActionCommentRemoved, actor, nil)
	}
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return s.withProjectFormatLock(code, StoreFormatV1, func() error {
		c, err := s.getCommentLocked(id)
		if err != nil {
			return err
		}
		now := Now()
		c.UpdatedAt = now
		c.UpdatedBy = actor
		if _, err := s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionCommentRemoved,
			Subject: Subject{Kind: "comment", ID: id},
			Payload: mustMarshal(c),
		}); err != nil {
			return err
		}
		return cacheDeleteComment(db, id)
	})
}

func (s *Store) CommentLabelAdd(id, label, actor string) error {
	if err := ValidateLabelName(label); err != nil {
		return err
	}
	if err := s.labelProjectExists(label); err != nil {
		return err
	}
	if err := s.validateActor(actor); err != nil {
		return err
	}
	code, f, err := s.commentProjectFormat(id)
	if err != nil {
		return err
	}
	if f == StoreFormatV2 {
		return s.commentLabelAddV2(code, id, label, actor)
	}
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return s.withProjectFormatLock(code, StoreFormatV1, func() error {
		c, err := s.getCommentLocked(id)
		if err != nil {
			return err
		}
		for _, l := range c.Labels {
			if l == label {
				return nil
			}
		}
		c.Labels = append(c.Labels, label)
		sort.Strings(c.Labels)
		if _, err := s.appendLabelUpsertsLocked(code, []string{label}, actor, Now()); err != nil {
			return err
		}
		now := Now()
		c.UpdatedAt = now
		c.UpdatedBy = actor
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionCommentLabelAdded,
			Subject: Subject{Kind: "comment", ID: id},
			Payload: mustMarshal(c),
		})
		if err != nil {
			return err
		}
		c.LogSeq = entry.Seq
		return cacheUpsertComment(db, c)
	})
}

// mutateComment is the log-first write-through helper for non-delete comment
// mutations. v2Payload is the equivalent v2 event payload (the writesOf key
// set for action); on a v2-active project the v1 body is never reached.
func (s *Store) mutateComment(id, actor string, fn func(c *Comment, now time.Time), action string, v2Payload map[string]any) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	code, f, err := s.commentProjectFormat(id)
	if err != nil {
		return err
	}
	if f == StoreFormatV2 {
		return s.mutateCommentV2(code, id, action, actor, v2Payload)
	}
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return s.withProjectFormatLock(code, StoreFormatV1, func() error {
		c, err := s.getCommentLocked(id)
		if err != nil {
			return err
		}
		now := Now()
		fn(c, now)
		c.UpdatedAt = now
		c.UpdatedBy = actor
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  action,
			Subject: Subject{Kind: "comment", ID: id},
			Payload: mustMarshal(c),
		})
		if err != nil {
			return err
		}
		c.LogSeq = entry.Seq
		return cacheUpsertComment(db, c)
	})
}
