package contextmap

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"atm/internal/core"
)

// Recorder holds the four mutating verbs. Each ensures the capability's
// vocabulary before using it, so none requires a seeded project.
type Recorder struct {
	Store    Service
	Resolver *Resolver
	Actor    string
}

func projectOf(taskID string) string {
	code, _, _ := strings.Cut(taskID, "-")
	return code
}

// Add turns a task into a context pointer: it applies the kind label and writes
// the first provenance stamp.
func (rec *Recorder) Add(taskID, kind string, sources []Source) error {
	code := projectOf(taskID)
	if _, err := EnsureVocabulary(rec.Store, code, rec.Actor); err != nil {
		return err
	}
	if err := rec.Store.TaskLabelAdd(taskID, LabelContextKind(code, kind), rec.Actor); err != nil {
		return err
	}
	return rec.writeStamp(taskID, code, sources)
}

// Stamp re-witnesses the sources already recorded on the task: the subject is
// unchanged in meaning, so record fresh evidence for it.
func (rec *Recorder) Stamp(taskID string) error {
	code := projectOf(taskID)
	prev, ok, err := LatestStamp(rec.Store, taskID, code)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s has no provenance to re-stamp; use `atm context add` first", taskID)
	}
	sources := make([]Source, 0, len(prev.Witnesses))
	for _, w := range prev.Witnesses {
		sources = append(sources, w.Source)
	}
	return rec.writeStamp(taskID, code, sources)
}

// Retarget records new sources for a pointer whose subject survived but moved.
// The task ID is stable, so anything referencing it keeps working.
func (rec *Recorder) Retarget(taskID string, sources []Source) error {
	code := projectOf(taskID)
	if _, err := EnsureVocabulary(rec.Store, code, rec.Actor); err != nil {
		return err
	}
	return rec.writeStamp(taskID, code, sources)
}

// Supersede retires a pointer whose subject died or was replaced. The task keeps
// its kind, its narrative, and its provenance history -- it is simply no longer
// current, so it drops out of the context-current board.
func (rec *Recorder) Supersede(taskID, byID, reason string) error {
	code := projectOf(taskID)
	if _, err := EnsureVocabulary(rec.Store, code, rec.Actor); err != nil {
		return err
	}
	if _, err := rec.Store.GetTask(byID); err != nil {
		return fmt.Errorf("successor %s: %w", byID, err)
	}
	t, err := rec.Store.GetTask(taskID)
	if err != nil {
		return err
	}
	note := fmt.Sprintf("SUPERSEDED BY %s", byID)
	if reason != "" {
		note += ": " + reason
	}
	desc := t.Description
	if desc != "" {
		desc += "\n\n"
	}
	if err := rec.Store.SetDescription(taskID, desc+note, rec.Actor); err != nil {
		return err
	}
	return rec.Store.TaskLabelAdd(taskID, LabelSuperseded(code), rec.Actor)
}

// writeStamp witnesses each source now and appends a provenance comment. It
// appends rather than replaces, so the thread keeps the full freshness history.
func (rec *Recorder) writeStamp(taskID, code string, sources []Source) error {
	if len(sources) == 0 {
		return fmt.Errorf("%s: at least one --source is required", taskID)
	}
	head, err := rec.Resolver.Head()
	if err != nil {
		return err
	}
	stamp := Stamp{Version: StampVersion, At: time.Now().UTC(), Head: head}
	for _, src := range sources {
		value, err := rec.Resolver.Witness(src)
		if err != nil {
			if isGone(err) {
				return fmt.Errorf("%s: source %s is gone; use retarget or supersede", taskID, src)
			}
			return fmt.Errorf("witness %s: %w", src, err)
		}
		stamp.Witnesses = append(stamp.Witnesses, Witness{Source: src, Value: value})
	}
	body, err := MarshalStamp(stamp)
	if err != nil {
		return err
	}
	_, err = rec.Store.CreateComment(taskID, body, []string{LabelProvenance(code)}, "", rec.Actor)
	return err
}

func isGone(err error) bool { return errors.Is(err, errGone) }

// LatestStamp returns the newest provenance stamp on a task. ok is false when
// the pointer was never stamped -- check reports that as UNVERIFIED, never as an
// error: a human may have written the pointer by hand.
func LatestStamp(s core.CommentService, taskID, code string) (Stamp, bool, error) {
	comments, err := s.ListComments(taskID)
	if err != nil {
		return Stamp{}, false, err
	}
	want := LabelProvenance(code)
	var newest Stamp
	found := false
	for _, c := range comments {
		if !hasLabelIn(c.Labels, want) {
			continue
		}
		st, err := UnmarshalStamp(c.Body)
		if err != nil {
			continue // unreadable stamp: treat as absent, never fail
		}
		if !found || st.At.After(newest.At) {
			newest, found = st, true
		}
	}
	return newest, found, nil
}

func hasLabelIn(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}
