package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func (s *Store) CreateComment(taskID, body string, labels []string, replyTo, actor string) (*Comment, error) {
	if body == "" {
		return nil, fmt.Errorf("%w: body is required", ErrUsage)
	}
	if actor == "" {
		return nil, fmt.Errorf("%w: actor is required", ErrUsage)
	}
	code, _, ok := ParseTaskID(taskID)
	if !ok {
		return nil, fmt.Errorf("%w: invalid task id %q", ErrUsage, taskID)
	}
	if replyTo != "" {
		rcode, _, _, ok := ParseCommentID(replyTo)
		if !ok {
			return nil, fmt.Errorf("%w: invalid reply-to %q", ErrUsage, replyTo)
		}
		if rcode != code {
			return nil, fmt.Errorf("%w: reply-to %q must belong to the same project as task %q", ErrUsage, replyTo, taskID)
		}
		_, rtaskN, _, _ := ParseCommentID(replyTo)
		_, ttaskN, _ := ParseTaskID(taskID)
		if rtaskN != ttaskN {
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
	var created *Comment
	err := s.WithLock(code, func() error {
		t, err := s.GetTask(taskID)
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
		labelEntries, err := s.appendLabelUpsertsLocked(code, labels, actor, ts)
		if err != nil {
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
		if err := os.MkdirAll(s.commentsDir(code), 0o755); err != nil {
			return err
		}
		if err := WriteJSON(s.commentPath(id), c); err != nil {
			return err
		}
		if err := WriteJSON(s.taskPath(taskID), t); err != nil {
			return err
		}
		if len(labelEntries) > 0 {
			if err := s.refreshDerivedLabelsLocked(code); err != nil {
				return err
			}
		}
		created = c
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (s *Store) GetComment(id string) (*Comment, error) {
	code, _, _, ok := ParseCommentID(id)
	if !ok {
		return nil, fmt.Errorf("%w: invalid comment id %q", ErrUsage, id)
	}
	var c Comment
	cachePath := s.commentPath(id)
	if err := ReadJSON(cachePath, &c); err != nil {
		if !os.IsNotExist(err) {
			if err := s.WithLock(code, func() error {
				return s.rebuildCommentFromLog(id, code)
			}); err != nil {
				return nil, err
			}
			if err := ReadJSON(cachePath, &c); err != nil {
				return nil, err
			}
			return &c, nil
		}
		if err := s.WithLock(code, func() error {
			return s.rebuildCommentFromLog(id, code)
		}); err != nil {
			return nil, err
		}
		if err := ReadJSON(cachePath, &c); err != nil {
			return nil, err
		}
		return &c, nil
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
		if err := s.WithLock(code, func() error {
			return s.rebuildCommentFromLog(id, code)
		}); err != nil {
			return nil, err
		}
		if err := ReadJSON(cachePath, &c); err != nil {
			return nil, err
		}
	}
	return &c, nil
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
	if err := os.MkdirAll(s.commentsDir(code), 0o755); err != nil {
		return err
	}
	return WriteJSON(s.commentPath(id), c)
}

func (s *Store) ListComments(taskID string) ([]*Comment, error) {
	code, _, ok := ParseTaskID(taskID)
	if !ok {
		return nil, fmt.Errorf("%w: invalid task id %q", ErrUsage, taskID)
	}
	entries, err := os.ReadDir(s.commentsDir(code))
	if err != nil {
		if os.IsNotExist(err) {
			return []*Comment{}, nil
		}
		return nil, err
	}
	prefix := taskID + "-c"
	dir := s.commentsDir(code)
	var out []*Comment
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) < len(prefix) || name[:len(prefix)] != prefix {
			continue
		}
		var c Comment
		if err := ReadJSON(filepath.Join(dir, name), &c); err != nil {
			continue
		}
		out = append(out, &c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) SetCommentBody(id, body, actor string) error {
	if body == "" {
		return fmt.Errorf("%w: body is required", ErrUsage)
	}
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	return s.mutateComment(id, actor, func(c *Comment, now time.Time) {
		c.Body = body
	}, ActionCommentBodyChanged)
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
	}, ActionCommentLabelRemoved)
}

func (s *Store) RemoveComment(id, actor string) error {
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	code, _, _, ok := ParseCommentID(id)
	if !ok {
		return fmt.Errorf("%w: invalid comment id %q", ErrUsage, id)
	}
	return s.WithLock(code, func() error {
		c, err := s.GetComment(id)
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
		return os.Remove(s.commentPath(id))
	})
}

func (s *Store) CommentLabelAdd(id, label, actor string) error {
	if err := ValidateLabelName(label); err != nil {
		return err
	}
	if err := s.labelProjectExists(label); err != nil {
		return err
	}
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	code, _, _, ok := ParseCommentID(id)
	if !ok {
		return fmt.Errorf("%w: invalid comment id %q", ErrUsage, id)
	}
	return s.WithLock(code, func() error {
		c, err := s.GetComment(id)
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
		labelEntries, err := s.appendLabelUpsertsLocked(code, []string{label}, actor, Now())
		if err != nil {
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
		if err := WriteJSON(s.commentPath(id), c); err != nil {
			return err
		}
		if len(labelEntries) > 0 {
			return s.refreshDerivedLabelsLocked(code)
		}
		return nil
	})
}

func (s *Store) mutateComment(id, actor string, fn func(c *Comment, now time.Time), action string) error {
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	code, _, _, ok := ParseCommentID(id)
	if !ok {
		return fmt.Errorf("%w: invalid comment id %q", ErrUsage, id)
	}
	return s.WithLock(code, func() error {
		c, err := s.GetComment(id)
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
		return WriteJSON(s.commentPath(id), c)
	})
}
