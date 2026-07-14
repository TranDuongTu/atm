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
	last, err := s.lastLogSeqLocked(code)
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
	// Write-through the per-project last-log-seq cache row in the same
	// locked critical section as the file append. LastLogSeq reads this
	// row (O(1)) instead of re-scanning log.jsonl on every staleness
	// check.
	if err := s.setLastLogSeqLocked(code, e.Seq); err != nil {
		return e, err
	}
	// Invalidate any in-memory log snapshot for this project so the next
	// ReadLogCached re-scans and picks up this append.
	s.invalidateLogSnapshot(code)
	return e, nil
}

func marshalLogLine(e LogEntry) ([]byte, error) {
	raw, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

// ReadLog reads projects/<CODE>/log.jsonl. It is v1-only BY DESIGN and must
// NEVER grow a format branch: Replay, lastProjectEventSeq,
// compareV2FoldToV1Replay and RollbackProjectToV1 all depend on it reading the
// v1 bytes even for a v2-active project (the frozen log is the rollback
// artifact). Views that must follow the project's effective format go through
// readLogForViews instead.
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

// LastLogSeq is THE staleness probe every poller in the codebase uses (Watch,
// tui/indexer.go, cli/index.go's Behind count, ReadLogCached's cross-process
// freshness check). It therefore has to answer in whatever sequence space the
// project's format actually advances in.
//
// For a v2-active project the v1 log is frozen at the cutover seq, so the v1
// answer never changes again and every poller sleeps forever. The v2 sequence
// surface is the EVENT COUNT (spec L3-11): monotonic under local appends (the
// only writer before L4 sync) and the same value the cache freshness row
// records.
func (s *Store) LastLogSeq(code string) (int, error) {
	if f, err := s.projectFormat(code); err != nil {
		return 0, err
	} else if f == StoreFormatV2 {
		return s.v2EventCount(code)
	}
	// No WithLock here: callers (getProjectWithRebuild, getTaskWithRebuild)
	// already hold the project lock, and WithLock is non-reentrant. The
	// cache.db read is process-serialized by MaxOpenConns(1) + WAL; the
	// file-scan fallback only runs on a cache miss (fresh cache.db), where
	// contention with concurrent appenders is not a concern (the cache row
	// gets populated and subsequent calls are O(1)).
	return s.lastLogSeqLocked(code)
}

// readLogForViews returns the project's history as compatibility []LogEntry
// from whichever format the project is ACTIVE on — the read behind every
// log-derived view (History, ReadLogCached and, through it, activity.Build).
//
// The error postures of the two formats differ because the underlying reads do:
// v1's ReadLog returns every entry that parsed ALONGSIDE an ErrIntegrity for a
// malformed tail, so the long-standing lenient posture (keep the partial view;
// verify/doctor report the damage) is preserved by dropping it here. The v2 read
// is strict and yields NOTHING on an integrity error, so that error is returned
// bare: swallowing it would render a corrupt event file as an empty view.
func (s *Store) readLogForViews(code string) ([]LogEntry, error) {
	f, err := s.projectFormat(code)
	if err != nil {
		return nil, err
	}
	if f == StoreFormatV2 {
		return s.readV2LogEntries(code)
	}
	entries, err := s.ReadLog(code)
	if err != nil && !IsIntegrity(err) {
		return nil, err
	}
	return entries, nil
}

// lastLogSeqLocked returns the project's last log seq. Caller MUST hold the
// project lock when invoked from a write path (appendLogLocked), but the
// public LastLogSeq calls this directly without a lock because cache.db
// access is process-serialized. Reads the O(1) cache.db meta row first; on
// cache miss it scans log.jsonl, returns the right number, and write-throughs
// the meta row so subsequent calls stay O(1).
func (s *Store) lastLogSeqLocked(code string) (int, error) {
	db, err := s.cacheDB()
	if err != nil {
		return 0, err
	}
	if v, ok, err := cacheGetLastLogSeq(db, code); err != nil {
		return 0, err
	} else if ok {
		return v, nil
	}
	// Cache miss: scan the file, then populate the cache.
	entries, err := s.ReadLog(code)
	if err != nil && !IsIntegrity(err) {
		return 0, err
	}
	last := 0
	if len(entries) > 0 {
		last = entries[len(entries)-1].Seq
	}
	if err := cacheSetLastLogSeq(db, code, last); err != nil {
		return 0, err
	}
	return last, nil
}

// setLastLogSeqLocked write-throughs the per-project last-log-seq cache row.
// Caller MUST hold the project lock.
func (s *Store) setLastLogSeqLocked(code string, seq int) error {
	db, err := s.cacheDB()
	if err != nil {
		return err
	}
	return cacheSetLastLogSeq(db, code, seq)
}

// ReadLogCached returns the project's log entries, memoizing the parsed
// result in memory for the Store's lifetime. The snapshot is invalidated in
// two ways:
//   - locally: appendLogLocked calls invalidateLogSnapshot after every append,
//     so the next ReadLogCached re-scans;
//   - across processes: before serving, the cached snapshot's builtSeq is
//     compared against LastLogSeq (now O(1) from cache.db). If the cached
//     last_seq advanced (another process appended + bumped the meta row), the
//     snapshot is dropped and rebuilt.
//
// This keeps the TUI's per-frame renderSummary path from re-parsing
// log.jsonl on every keystroke while staying fresh under external mutation.
func (s *Store) ReadLogCached(code string) ([]LogEntry, error) {
	s.logSnapMu.Lock()
	if s.logSnapshots == nil {
		s.logSnapshots = map[string]logSnapshot{}
	}
	snap, ok := s.logSnapshots[code]
	s.logSnapMu.Unlock()

	// Cross-process freshness: if the cached last_seq advanced, drop the
	// snapshot. LastLogSeq is O(1) when the meta row is present.
	if ok {
		cur, err := s.LastLogSeq(code)
		if err != nil && !IsIntegrity(err) {
			return nil, err
		}
		if cur <= snap.builtSeq {
			return snap.entries, nil
		}
		s.logSnapMu.Lock()
		delete(s.logSnapshots, code)
		s.logSnapMu.Unlock()
	}

	// Format-dispatched re-scan. The memoization and invalidation logic above is
	// format-agnostic: the v2 entries' last Seq equals the event count, which is
	// exactly what the branched LastLogSeq returns for the staleness comparison.
	entries, err := s.readLogForViews(code)
	if err != nil {
		return nil, err
	}
	builtSeq := 0
	if len(entries) > 0 {
		builtSeq = entries[len(entries)-1].Seq
	}
	s.logSnapMu.Lock()
	s.logSnapshots[code] = logSnapshot{entries: entries, builtSeq: builtSeq}
	s.logSnapMu.Unlock()
	return entries, nil
}

// invalidateLogSnapshot drops the in-memory log snapshot for a project. Called
// by appendLogLocked after a local append so the next ReadLogCached re-scans.
func (s *Store) invalidateLogSnapshot(code string) {
	s.logSnapMu.Lock()
	delete(s.logSnapshots, code)
	s.logSnapMu.Unlock()
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
	// Replay is the cache.db-wiped recovery path: populate the per-project
	// last-log-seq cache row so subsequent LastLogSeq calls stay O(1).
	if len(entries) > 0 {
		if db, err := s.cacheDB(); err == nil {
			_ = cacheSetLastLogSeq(db, code, entries[len(entries)-1].Seq)
		}
	}
	return st, nil
}

// History renders one subject's event trail. The compatibility entries carry
// the entity's ALIAS in Subject.ID for both formats, so the v1-shaped callers
// (cli/task.go, cli/comment.go, cli/project.go, tui/comments.go, tui/projects.go)
// and subjectMatch itself are unchanged.
//
// It has no error channel and has always swallowed read errors; an integrity
// failure surfaces through verify/doctor and through every read path that CAN
// return one (ReadLogCached, the list reads, the point reads).
func (s *Store) History(code string, subject Subject) []HistoryView {
	entries, _ := s.readLogForViews(code)
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
