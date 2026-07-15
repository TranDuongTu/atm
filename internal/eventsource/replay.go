package eventsource

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
)

// ErrIntegrity marks a semantic divergence discovered while self-verifying an
// upgrade: the v2 fold of the upgraded events disagrees with a pure replay of
// the v1 log bytes. It is the eventsource-local mirror of store.ErrIntegrity,
// declared here so the upgrade tool can verify its own output without
// importing internal/store.
var ErrIntegrity = errors.New("eventsource: integrity")

// ReplayResult is a pure replay of a v1 log into live entity snapshots, keyed
// by v1 alias. It carries only the fields CompareReplayToFold needs; the
// retired counters, timestamps, and log-seq bookkeeping that store.ReplayState
// tracks are out of scope for the v1↔v2 semantic comparison.
type ReplayResult struct {
	Project  *ReplayProject
	Tasks    []*ReplayTask
	Comments []*ReplayComment
	Labels   []*ReplayLabel
}

// ReplayProject is the live project snapshot (its v1 code and name).
type ReplayProject struct {
	Code string
	Name string
}

// ReplayTask is a live task snapshot keyed by its v1 alias (ID).
type ReplayTask struct {
	ID          string
	Title       string
	Description string
	Labels      []string
}

// ReplayComment is a live comment snapshot keyed by its v1 alias (ID). TaskID
// and ReplyTo are v1 aliases, mirroring the v1 payload verbatim.
type ReplayComment struct {
	ID      string
	TaskID  string
	ReplyTo string
	Body    string
	Labels  []string
}

// ReplayLabel is a live label snapshot keyed by name.
type ReplayLabel struct {
	Name        string
	Description string
	Expr        string
}

// IsComputed mirrors LabelState.IsComputed for a replayed v1 label: boards
// (Expr set) and namespace labels carry derived membership (L2-6).
func (l *ReplayLabel) IsComputed() bool {
	return l.Expr != "" || isNamespaceName(l.Name)
}

// v1 payload shapes for the replay. Each v1 log line carries a whole-entity
// SNAPSHOT, so replay keeps only the latest snapshot per live entity.
type (
	v1ProjectPayload struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	v1TaskPayload struct {
		ID          string   `json:"id"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Labels      []string `json:"labels"`
	}
	v1CommentPayload struct {
		ID      string   `json:"id"`
		TaskID  string   `json:"task_id"`
		ReplyTo string   `json:"reply_to"`
		Body    string   `json:"body"`
		Labels  []string `json:"labels"`
	}
	v1LabelPayload struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Expr        string `json:"expr"`
	}
)

// ReplayV1 is a pure replay of v1 log bytes into live entity snapshots. It
// mirrors store.Replay's live-set semantics exactly: each *.created/*-changed
// event replaces the entity's snapshot, each *.removed tombstone deletes it,
// and label upsert/remove tracks the live label set. It operates purely on the
// log bytes and eventsource-native types — no dependency on internal/store.
func ReplayV1(logData []byte) (*ReplayResult, error) {
	var proj *ReplayProject
	tasks := map[string]*ReplayTask{}
	comments := map[string]*ReplayComment{}
	labels := map[string]*ReplayLabel{}

	sc := bufio.NewScanner(bytes.NewReader(logData))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var e v1Entry
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("eventsource: replay: line %d: %w", lineNo, err)
		}
		switch e.Subject.Kind {
		case "project":
			switch e.Action {
			case ActionProjectCreated, ActionProjectNameChanged:
				var p v1ProjectPayload
				if err := json.Unmarshal(e.Payload, &p); err == nil {
					proj = &ReplayProject{Code: p.Code, Name: p.Name}
				}
			case ActionProjectRemoved:
				proj = nil
			}
		case "task":
			var tk v1TaskPayload
			_ = json.Unmarshal(e.Payload, &tk)
			switch e.Action {
			// task.meta-changed is retired in v2 but a v1 log may still carry
			// it; replay treats it as any other whole-entity snapshot, exactly
			// as store.Replay does, so the live set stays faithful.
			case ActionTaskCreated, ActionTaskTitleChanged, ActionTaskDescChanged,
				ActionTaskLabelAdded, ActionTaskLabelRemoved, "task.meta-changed":
				tasks[e.Subject.ID] = &ReplayTask{
					ID:          e.Subject.ID,
					Title:       tk.Title,
					Description: tk.Description,
					Labels:      tk.Labels,
				}
			case ActionTaskRemoved:
				delete(tasks, e.Subject.ID)
			}
		case "comment":
			var c v1CommentPayload
			_ = json.Unmarshal(e.Payload, &c)
			switch e.Action {
			case ActionCommentCreated, ActionCommentBodyChanged,
				ActionCommentLabelAdded, ActionCommentLabelRemoved:
				comments[e.Subject.ID] = &ReplayComment{
					ID:      e.Subject.ID,
					TaskID:  c.TaskID,
					ReplyTo: c.ReplyTo,
					Body:    c.Body,
					Labels:  c.Labels,
				}
			case ActionCommentRemoved:
				delete(comments, e.Subject.ID)
			}
		case "label":
			var l v1LabelPayload
			_ = json.Unmarshal(e.Payload, &l)
			switch e.Action {
			case ActionLabelUpserted:
				labels[e.Subject.Name] = &ReplayLabel{
					Name:        e.Subject.Name,
					Description: l.Description,
					Expr:        l.Expr,
				}
			case ActionLabelRemoved:
				delete(labels, e.Subject.Name)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("eventsource: replay: %w", err)
	}

	res := &ReplayResult{Project: proj}
	for _, tk := range tasks {
		res.Tasks = append(res.Tasks, tk)
	}
	sort.Slice(res.Tasks, func(i, j int) bool { return res.Tasks[i].ID < res.Tasks[j].ID })
	for _, l := range labels {
		res.Labels = append(res.Labels, l)
	}
	sort.Slice(res.Labels, func(i, j int) bool { return res.Labels[i].Name < res.Labels[j].Name })
	for _, c := range comments {
		res.Comments = append(res.Comments, c)
	}
	sort.Slice(res.Comments, func(i, j int) bool { return res.Comments[i].ID < res.Comments[j].ID })
	return res, nil
}

// CompareReplayToFold is the computed-label-aware semantic comparison between a
// pure v1 replay and the v2 fold of the upgraded events. It is a faithful port
// of store.compareV2FoldToV1Replay: every carried field must agree, and
// membership of COMPUTED labels (boards + namespace labels) is excluded on
// BOTH sides — that is the one intentional, documented v1↔v2 divergence (L2-6:
// such membership is derived, never asserted, so the fold drops an asserted
// board label that v1's replay still lists; the raw assertion survives
// verbatim in the upgraded event, so nothing is lost). Any other difference
// returns a wrapped ErrIntegrity.
func CompareReplayToFold(rep *ReplayResult, st *State) error {
	// The v1 log is single-project. Derive its code from the fold, whose
	// ProjectState.Code is always the true code (it equals the creation
	// alias) — the v1 replay cannot supply it reliably, because a
	// project.name-changed snapshot omits the code, leaving rep.Project.Code
	// empty after any rename.
	code := ""
	for _, p := range st.Projects {
		code = p.Code
		break
	}

	// --- Project.
	var v2proj *ProjectState
	for _, p := range st.Projects {
		if p.Code == code && !p.Tombstoned {
			v2proj = p
			break
		}
	}
	if (rep.Project == nil) != (v2proj == nil) {
		return fmt.Errorf("%w: upgrade of %q: project existence differs (v1 present=%v, v2 present=%v)",
			ErrIntegrity, code, rep.Project != nil, v2proj != nil)
	}
	if rep.Project != nil && rep.Project.Name != v2proj.Name {
		return fmt.Errorf("%w: upgrade of %q: project name: v2 %q, v1 %q", ErrIntegrity, code, v2proj.Name, rep.Project.Name)
	}

	// --- Labels (name → description, expr), live set on both sides.
	v1Labels := map[string]*ReplayLabel{}
	for _, l := range rep.Labels {
		v1Labels[l.Name] = l
	}
	v2Labels := map[string]*LabelState{}
	for name, l := range st.Labels {
		if !l.Tombstoned {
			v2Labels[name] = l
		}
	}
	for name, want := range v1Labels {
		got, ok := v2Labels[name]
		if !ok {
			return fmt.Errorf("%w: upgrade of %q: label %q is live in v1 but missing from the v2 fold", ErrIntegrity, code, name)
		}
		if got.Description != want.Description {
			return fmt.Errorf("%w: upgrade of %q: label %q description: v2 %q, v1 %q", ErrIntegrity, code, name, got.Description, want.Description)
		}
		if got.Expr != want.Expr {
			return fmt.Errorf("%w: upgrade of %q: label %q expr: v2 %q, v1 %q", ErrIntegrity, code, name, got.Expr, want.Expr)
		}
	}
	for name := range v2Labels {
		if _, ok := v1Labels[name]; !ok {
			return fmt.Errorf("%w: upgrade of %q: label %q is live in the v2 fold but absent from v1", ErrIntegrity, code, name)
		}
	}

	// computed reports whether membership in name is derived (L2-6) and so is
	// not comparable between the two sides. It MIRRORS Fold's own closure
	// (fold.go): ask the fold's label state — ALL of them, tombstoned included,
	// because Fold decides inertness from st.Labels[name] regardless of
	// Tombstoned — and fall back to the name for a label that was never
	// upserted. A label that became a board and was then REMOVED is the case
	// that makes this load-bearing: LabelRemove drops the record but leaves the
	// name on the task, so v1 still lists it, the fold still drops it, and a
	// live-only view of the label maps would refuse the upgrade forever.
	// v1Labels is consulted only as a belt-and-braces fallback for a label the
	// fold never saw at all.
	computed := func(name string) bool {
		if l := st.Labels[name]; l != nil {
			return l.IsComputed()
		}
		if l, ok := v1Labels[name]; ok {
			return l.IsComputed()
		}
		return isNamespaceName(name)
	}
	assertedLabels := func(in []string) []string {
		out := make([]string, 0, len(in))
		for _, name := range in {
			if !computed(name) {
				out = append(out, name)
			}
		}
		sort.Strings(out)
		return out
	}

	// --- Tasks, keyed by alias, live only.
	v2Tasks := map[string]*TaskState{}
	for _, tk := range st.Tasks {
		if !tk.Tombstoned {
			v2Tasks[tk.Alias] = tk
		}
	}
	if len(v2Tasks) != len(rep.Tasks) {
		return fmt.Errorf("%w: upgrade of %q: live tasks: v2 %d, v1 %d", ErrIntegrity, code, len(v2Tasks), len(rep.Tasks))
	}
	for _, want := range rep.Tasks {
		got, ok := v2Tasks[want.ID]
		if !ok {
			return fmt.Errorf("%w: upgrade of %q: task %s is live in v1 but missing from the v2 fold", ErrIntegrity, code, want.ID)
		}
		if got.Title != want.Title {
			return fmt.Errorf("%w: upgrade of %q: task %s title: v2 %q, v1 %q", ErrIntegrity, code, want.ID, got.Title, want.Title)
		}
		if got.Description != want.Description {
			return fmt.Errorf("%w: upgrade of %q: task %s description: v2 %q, v1 %q", ErrIntegrity, code, want.ID, got.Description, want.Description)
		}
		gotLabels, wantLabels := assertedLabels(got.Labels), assertedLabels(want.Labels)
		if !slices.Equal(gotLabels, wantLabels) {
			return fmt.Errorf("%w: upgrade of %q: task %s labels: v2 %v, v1 %v", ErrIntegrity, code, want.ID, gotLabels, wantLabels)
		}
	}

	// --- Comments, keyed by alias, live only. Cross-entity references are
	// identities in the fold; map them back to aliases as the projector does.
	v2Comments := map[string]*CommentState{}
	for _, cm := range st.Comments {
		if !cm.Tombstoned {
			v2Comments[cm.Alias] = cm
		}
	}
	taskAliasOf := func(id string) string {
		if tk, ok := st.Tasks[id]; ok {
			return tk.Alias
		}
		return ""
	}
	commentAliasOf := func(id string) string {
		if cm, ok := st.Comments[id]; ok {
			return cm.Alias
		}
		return ""
	}
	if len(v2Comments) != len(rep.Comments) {
		return fmt.Errorf("%w: upgrade of %q: live comments: v2 %d, v1 %d", ErrIntegrity, code, len(v2Comments), len(rep.Comments))
	}
	for _, want := range rep.Comments {
		got, ok := v2Comments[want.ID]
		if !ok {
			return fmt.Errorf("%w: upgrade of %q: comment %s is live in v1 but missing from the v2 fold", ErrIntegrity, code, want.ID)
		}
		if got.Body != want.Body {
			return fmt.Errorf("%w: upgrade of %q: comment %s body: v2 %q, v1 %q", ErrIntegrity, code, want.ID, got.Body, want.Body)
		}
		if a := taskAliasOf(got.TaskRef); a != want.TaskID {
			return fmt.Errorf("%w: upgrade of %q: comment %s task: v2 %q, v1 %q", ErrIntegrity, code, want.ID, a, want.TaskID)
		}
		gotReply := ""
		if got.ReplyToRef != "" {
			gotReply = commentAliasOf(got.ReplyToRef)
		}
		if gotReply != want.ReplyTo {
			return fmt.Errorf("%w: upgrade of %q: comment %s reply-to: v2 %q, v1 %q", ErrIntegrity, code, want.ID, gotReply, want.ReplyTo)
		}
		gotLabels, wantLabels := assertedLabels(got.Labels), assertedLabels(want.Labels)
		if !slices.Equal(gotLabels, wantLabels) {
			return fmt.Errorf("%w: upgrade of %q: comment %s labels: v2 %v, v1 %v", ErrIntegrity, code, want.ID, gotLabels, wantLabels)
		}
	}
	return nil
}
