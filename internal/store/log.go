package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

var ErrIntegrity = errors.New("integrity")

// Action enum — closed. Unknown action → ErrUsage.
const (
	ActionProjectCreated      = "project.created"
	ActionProjectNameChanged  = "project.name-changed"
	ActionProjectRemoved      = "project.removed"
	ActionTaskCreated         = "task.created"
	ActionTaskTitleChanged    = "task.title-changed"
	ActionTaskDescChanged     = "task.description-changed"
	ActionTaskLabelAdded      = "task.label-added"
	ActionTaskLabelRemoved    = "task.label-removed"
	ActionTaskRemoved         = "task.removed"
	ActionLabelUpserted       = "label.upserted"
	ActionLabelRemoved        = "label.removed"
	ActionTaskMetaChanged     = "task.meta-changed"
	ActionCommentCreated      = "comment.created"
	ActionCommentBodyChanged  = "comment.body-changed"
	ActionCommentLabelAdded   = "comment.label-added"
	ActionCommentLabelRemoved = "comment.label-removed"
	ActionCommentRemoved      = "comment.removed"
)

var validActions = map[string]bool{
	ActionProjectCreated:      true,
	ActionProjectNameChanged:  true,
	ActionProjectRemoved:      true,
	ActionTaskCreated:         true,
	ActionTaskTitleChanged:    true,
	ActionTaskDescChanged:     true,
	ActionTaskLabelAdded:      true,
	ActionTaskLabelRemoved:    true,
	ActionTaskRemoved:         true,
	ActionLabelUpserted:       true,
	ActionLabelRemoved:        true,
	ActionTaskMetaChanged:     true,
	ActionCommentCreated:      true,
	ActionCommentBodyChanged:  true,
	ActionCommentLabelAdded:   true,
	ActionCommentLabelRemoved: true,
	ActionCommentRemoved:      true,
}

type LogEntry struct {
	Seq     int             `json:"seq"`
	At      time.Time       `json:"at"`
	Actor   string          `json:"actor"`
	Action  string          `json:"action"`
	Subject Subject         `json:"subject"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Subject struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
	Code string `json:"code,omitempty"`
	Name string `json:"name,omitempty"`
}

type ReplayState struct {
	Project  *Project
	Tasks    []*Task
	Labels   []Label
	Comments []*Comment
}

type HistoryView struct {
	Seq    int       `json:"seq"`
	Action string    `json:"action"`
	Actor  string    `json:"actor"`
	At     time.Time `json:"at"`
}

func (s *Store) logPath(code string) string {
	return filepath.Join(s.projectDir(code), "log.jsonl")
}

func IsIntegrity(err error) bool { return errors.Is(err, ErrIntegrity) }

func (s *Store) AppendLog(code string, e LogEntry) (LogEntry, error) {
	if !validActions[e.Action] {
		return LogEntry{}, fmt.Errorf("%w: unknown action %q", ErrUsage, e.Action)
	}
	err := s.WithLock(code, func() error {
		var err2 error
		e, err2 = s.appendLogLocked(code, e)
		return err2
	})
	if err != nil {
		return LogEntry{}, err
	}
	return e, nil
}

// appendLogLocked assigns Seq, appends one line to projects/<CODE>/log.jsonl,
// and fsyncs. Caller MUST already hold the project lock — this is the
// re-entrancy-safe variant of AppendLog for write-first paths that already
// run inside WithLock.
func (s *Store) appendLogLocked(code string, e LogEntry) (LogEntry, error) {
	last, err := s.LastLogSeq(code)
	if err != nil {
		return e, err
	}
	e.Seq = last + 1
	if e.At.IsZero() {
		e.At = Now()
	}
	line, err := marshalLogLine(e)
	if err != nil {
		return e, err
	}
	path := s.logPath(code)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return e, err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return e, err
	}
	defer f.Close()
	if _, err := f.Write(line); err != nil {
		return e, err
	}
	if err := f.Sync(); err != nil {
		return e, err
	}
	return e, nil
}

func marshalLogLine(e LogEntry) ([]byte, error) {
	raw, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func (s *Store) ReadLog(code string) ([]LogEntry, error) {
	f, err := os.Open(s.logPath(code))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []LogEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	truncated := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		var e LogEntry
		if err := json.Unmarshal(line, &e); err != nil {
			truncated += len(line) + 1 // +1 for the newline Scanner stripped
			continue
		}
		out = append(out, e)
	}
	// If scanner stopped early due to a partial last line (no trailing newline),
	// bufio.Scanner skips it silently; check the file tail.
	if truncErr := s.detectPartialTail(code); truncErr != nil {
		truncated += truncErr.bytes
	}
	if truncated > 0 {
		err = fmt.Errorf("%w: %d bytes of malformed log tail truncated", ErrIntegrity, truncated)
	}
	return out, err
}

type partialTailError struct{ bytes int }

func (e *partialTailError) Error() string { return "partial tail" }

func (s *Store) detectPartialTail(code string) *partialTailError {
	// Re-scan the file raw for a final non-newline-terminated segment.
	data, err := os.ReadFile(s.logPath(code))
	if err != nil {
		return nil
	}
	if len(data) == 0 {
		return nil
	}
	if data[len(data)-1] == '\n' {
		return nil
	}
	// Find last newline
	i := len(data) - 1
	for i >= 0 && data[i] != '\n' {
		i--
	}
	tail := data[i+1:]
	return &partialTailError{bytes: len(tail)}
}

func (s *Store) LastLogSeq(code string) (int, error) {
	entries, err := s.ReadLog(code)
	if err != nil && !IsIntegrity(err) {
		return 0, err
	}
	if len(entries) == 0 {
		return 0, nil
	}
	return entries[len(entries)-1].Seq, nil
}

func (s *Store) Replay(code string) (*ReplayState, error) {
	entries, err := s.ReadLog(code)
	if err != nil && !IsIntegrity(err) {
		return nil, err
	}
	st := &ReplayState{}
	var proj *Project
	tasks := map[string]*Task{}
	labels := map[string]Label{}
	comments := map[string]*Comment{}
	maxTaskN := 0
	for _, e := range entries {
		switch e.Subject.Kind {
		case "project":
			switch e.Action {
			case ActionProjectCreated, ActionProjectNameChanged:
				var p Project
				if err := json.Unmarshal(e.Payload, &p); err == nil {
					p.LogSeq = e.Seq
					proj = &p
				}
			case ActionProjectRemoved:
				proj = nil
			}
		case "task":
			// Track the highest task-ID N seen across ALL task.* entries
			// (including task.removed tombstones) so NextTaskN can be
			// reconstructed below without relying on a project.* log event
			// that CreateTask never appends. A removed task's number must
			// never be reused.
			if _, n, ok := ParseTaskID(e.Subject.ID); ok && n > maxTaskN {
				maxTaskN = n
			}
			var tk Task
			_ = json.Unmarshal(e.Payload, &tk)
			tk.LogSeq = e.Seq
			switch e.Action {
			case ActionTaskCreated, ActionTaskTitleChanged, ActionTaskDescChanged, ActionTaskLabelAdded, ActionTaskLabelRemoved, ActionTaskMetaChanged:
				tasks[e.Subject.ID] = &tk
			case ActionTaskRemoved:
				delete(tasks, e.Subject.ID)
			}
		case "comment":
			var c Comment
			_ = json.Unmarshal(e.Payload, &c)
			c.LogSeq = e.Seq
			switch e.Action {
			case ActionCommentCreated, ActionCommentBodyChanged,
				ActionCommentLabelAdded, ActionCommentLabelRemoved:
				comments[e.Subject.ID] = &c
			case ActionCommentRemoved:
				delete(comments, e.Subject.ID)
			}
		case "label":
			var l Label
			_ = json.Unmarshal(e.Payload, &l)
			l.LogSeq = e.Seq
			switch e.Action {
			case ActionLabelUpserted:
				labels[e.Subject.Name] = l
			case ActionLabelRemoved:
				delete(labels, e.Subject.Name)
			}
		}
	}
	if proj != nil {
		proj.NextTaskN = max(proj.NextTaskN, maxTaskN+1)
	}
	st.Project = proj
	for _, tk := range tasks {
		st.Tasks = append(st.Tasks, tk)
	}
	sort.Slice(st.Tasks, func(i, j int) bool { return st.Tasks[i].ID < st.Tasks[j].ID })
	for _, l := range labels {
		st.Labels = append(st.Labels, l)
	}
	sort.Slice(st.Labels, func(i, j int) bool { return st.Labels[i].Name < st.Labels[j].Name })
	for _, c := range comments {
		st.Comments = append(st.Comments, c)
	}
	sort.Slice(st.Comments, func(i, j int) bool { return st.Comments[i].ID < st.Comments[j].ID })
	return st, nil
}

func (s *Store) History(code string, subject Subject) []HistoryView {
	entries, _ := s.ReadLog(code)
	var out []HistoryView
	for _, e := range entries {
		if !subjectMatch(e.Subject, subject) {
			continue
		}
		out = append(out, HistoryView{Seq: e.Seq, Action: e.Action, Actor: e.Actor, At: e.At})
	}
	return out
}

func subjectMatch(a, b Subject) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case "project":
		return a.Code == b.Code
	case "task":
		return a.ID == b.ID
	case "label":
		return a.Name == b.Name
	case "comment":
		return a.ID == b.ID
	}
	return false
}
