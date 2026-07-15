package store

import (
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
	// Every project is born v2, so comment creation is unconditionally v2. There
	// is no v1 NextCommentN minting or task.meta-changed companion append any
	// more — v2 aliases are content hashes with no per-task counter.
	return s.createCommentV2(code, taskID, body, labels, replyTo, actor)
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
		return s.WithLock(code, func() error {
			return s.rebuildEntityCacheLocked(code, func() error { return noV1RebuildErr(code) })
		})
	})
}

// getCommentLocked is identical to GetComment except that, on a cache
// miss/stale hit, it triggers the rebuild directly instead of wrapping it in
// s.WithLock. Callers MUST already hold the comment's project lock (i.e. be
// running inside their own s.WithLock(code, ...) closure) — calling
// GetComment in that situation would re-enter the (non-reentrant) mutex and
// deadlock.
func (s *Store) getCommentLocked(id string) (*Comment, error) {
	code, _, _, ok := ParseCommentID(id)
	if !ok {
		return nil, fmt.Errorf("%w: invalid comment id %q", ErrUsage, id)
	}
	return s.getCommentWithRebuild(id, code, func() error {
		return s.rebuildEntityCacheLocked(code, func() error { return noV1RebuildErr(code) })
	})
}

// getCommentWithRebuild contains the fast-path cache read + staleness check
// shared by GetComment and getCommentLocked. It is parameterized only by how
// the rebuild call itself gets invoked: wrapped in a fresh s.WithLock
// (GetComment, for callers that do not already hold the lock) or called
// directly (getCommentLocked, for callers that do).
//
// The non-v2 arm below is not a revival of v1 lazy-rebuild — see
// getProjectWithRebuild's doc comment for why a comment's project can still
// legitimately resolve to a non-v2 format (a fully removed project, or a
// cache row written directly ahead of format registration) and why the
// correct response is to serve the cache row as-is (or ErrNotFound if
// absent) without ever attempting a rebuild.
func (s *Store) getCommentWithRebuild(id, code string, rebuild func() error) (*Comment, error) {
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	format, err := s.projectFormat(code)
	if err != nil {
		return nil, err
	}
	if format != StoreFormatV2 {
		c, found, err := cacheGetComment(db, id)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("%w: comment %q", ErrNotFound, id)
		}
		return c, nil
	}
	if fresh, err := s.v2CacheFresh(code); err != nil {
		return nil, err
	} else if !fresh {
		if err := rebuild(); err != nil {
			return nil, err
		}
	}
	c, found, err := cacheGetComment(db, id)
	if err != nil {
		return nil, err
	}
	if !found {
		// Fresh count + missing row can still be a damaged cache: rebuild
		// once and re-read before declaring not-found.
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

func (s *Store) ListComments(taskID string) ([]*Comment, error) {
	code, _, ok := ParseTaskID(taskID)
	if !ok {
		return nil, fmt.Errorf("%w: invalid task id %q", ErrUsage, taskID)
	}
	// Same freshness gap ListTasksErr closes: under v2 an external append (a
	// second process, or a writer that died between the append commit point
	// and its reprojection) can leave the cache legitimately behind the event
	// file, and this read has no other freshness check on its path. A caller
	// holding this task's project lock would deadlock here (WithLock is not
	// reentrant) -- confirmed none of ListComments' callers do.
	//
	// The lookup error is PROPAGATED, not swallowed: a swallowed lookup leaves
	// f == "", skips the v2 branch (and its freshness gate), and serves stale
	// cache rows with a nil error. Same reasoning as ListTasksErr / textSearch.
	f, err := s.projectFormat(code)
	if err != nil {
		return nil, err
	}
	if f == StoreFormatV2 {
		if err := s.ensureV2CacheFresh(code); err != nil {
			return nil, err
		}
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
	if f == StoreFormatV2 {
		// cacheListComments returns id-asc. Under v1 that IS thread order (the
		// -cNNNN segment is a zero-padded per-task creation counter), but a v2
		// comment alias is a content hash, so id-asc renders the narrative in
		// hash order -- a reply can print above the comment it answers. The
		// projector stamps Comment.LogSeq with the per-task creation ordinal
		// from the fold (CommentsByCreation, i.e. HLC creation order), which is
		// the honest thread order; sort on it. Ordinals may have gaps (tombstoned
		// comments consume one) but stay monotone in creation order.
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].LogSeq != out[j].LogSeq {
				return out[i].LogSeq < out[j].LogSeq
			}
			return out[i].ID < out[j].ID
		})
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
	code, _, err := s.commentProjectFormat(id)
	if err != nil {
		return err
	}
	return s.mutateCommentV2(code, id, ActionCommentRemoved, actor, nil)
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
	code, _, err := s.commentProjectFormat(id)
	if err != nil {
		return err
	}
	return s.commentLabelAddV2(code, id, label, actor)
}

// mutateComment is the shared entry point for non-delete comment mutations;
// every project is v2-active (D-Task5b removed the v1 write arm), so it
// resolves the project code and delegates to mutateCommentV2 with the given
// action and payload. fn is unused now that there is no v1 struct to mutate
// in place; it survives only because SetCommentBody/CommentLabelRemove still
// pass it and trimming their call sites is out of scope for this prune.
func (s *Store) mutateComment(id, actor string, fn func(c *Comment, now time.Time), action string, v2Payload map[string]any) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	code, _, err := s.commentProjectFormat(id)
	if err != nil {
		return err
	}
	return s.mutateCommentV2(code, id, action, actor, v2Payload)
}
