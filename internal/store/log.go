package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
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
	ActionCommentCreated      = "comment.created"
	ActionCommentBodyChanged  = "comment.body-changed"
	ActionCommentLabelAdded   = "comment.label-added"
	ActionCommentLabelRemoved = "comment.label-removed"
	ActionCommentRemoved      = "comment.removed"
)

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

// LastLogSeq is THE staleness probe every poller in the codebase uses (Watch,
// tui/indexer.go, cli/index.go's Behind count, ReadLogCached's cross-process
// freshness check). It therefore has to answer in whatever sequence space the
// project's format actually advances in.
//
// The v2 sequence surface is the EVENT COUNT (spec L3-11): monotonic under
// local appends (the only writer before L4 sync) and the same value the
// cache freshness row records.
func (s *Store) LastLogSeq(code string) (int, error) {
	if _, err := s.projectFormat(code); err != nil {
		return 0, err
	}
	return s.v2EventCount(code)
}

// readLogForViews returns the project's history as compatibility []LogEntry
// — the read behind every log-derived view (History, ReadLogCached and,
// through it, activity.Build).
//
// The v2 read returns the recoverable prefix alongside an ErrIntegrity, and
// that error is propagated rather than dropped: swallowing it would render a
// corrupt event file as a silently truncated view. Callers that can tolerate
// damage (the TUI project summary) keep consuming the prefix; callers that
// cannot (the CLI) surface the error.
func (s *Store) readLogForViews(code string) ([]LogEntry, error) {
	if _, err := s.projectFormat(code); err != nil {
		return nil, err
	}
	return s.readV2LogEntries(code)
}

// ReadLogCached returns the project's log entries, memoizing the parsed
// result in memory for the Store's lifetime. The snapshot is invalidated
// whenever the cached snapshot's builtSeq falls behind LastLogSeq (now O(1)
// from cache.db): a local append bumps the v2 event count immediately, and a
// remote process's append bumps it the same way, so one freshness check
// covers both cases without a separate local-invalidation call.
// UpgradeProjectToV2 is the one write path that still calls
// invalidateLogSnapshot directly, since an upgrade replaces the project's
// format/media rather than advancing its event count.
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
	if err != nil && !IsIntegrity(err) {
		return nil, err
	}
	if err != nil {
		// Integrity failure: hand back the recoverable prefix ALONGSIDE the error
		// (v1's posture, now v2's too) and never memoize it — a damaged view must
		// not freeze into the snapshot and must not be mistaken for a fresh one.
		return entries, err
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

// invalidateLogSnapshot drops the in-memory log snapshot for a project.
// Called by UpgradeProjectToV2 after cutting a project over, so the next
// ReadLogCached re-scans the (now v2) history instead of serving a stale v1
// snapshot.
func (s *Store) invalidateLogSnapshot(code string) {
	s.logSnapMu.Lock()
	delete(s.logSnapshots, code)
	s.logSnapMu.Unlock()
}

// History renders one subject's event trail. The compatibility entries carry
// the entity's ALIAS in Subject.ID for both formats, so the v1-shaped callers
// (tui/comments.go, tui/projects.go) and subjectMatch itself are unchanged.
//
// This is the error-free wrapper the TUI keeps calling: it renders whatever the
// read produced (for a v2 integrity failure, the recoverable prefix). Callers
// that CAN report an error — every CLI caller does — must use HistoryE instead,
// so a corrupt event file surfaces as a real error rather than a short history.
func (s *Store) History(code string, subject Subject) []HistoryView {
	out, _ := s.HistoryE(code, subject)
	return out
}

// HistoryE is History with an error channel. The rendered rows and the error are
// BOTH returned: a v2 integrity failure yields the recoverable prefix alongside
// ErrIntegrity, mirroring v1's long-standing partial-view posture, so a caller
// that tolerates integrity errors still gets everything that parsed and a caller
// that does not gets the failure.
func (s *Store) HistoryE(code string, subject Subject) ([]HistoryView, error) {
	entries, err := s.readLogForViews(code)
	var out []HistoryView
	for _, e := range entries {
		if !subjectMatch(e.Subject, subject) {
			continue
		}
		out = append(out, HistoryView{Seq: e.Seq, Action: e.Action, Actor: e.Actor, At: e.At})
	}
	return out, err
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
