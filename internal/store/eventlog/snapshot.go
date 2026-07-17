package eventlog

import (
	"fmt"
	"os"
	"sort"

	"atm/internal/core"
	"atm/libs/eventsource"
)

// Snapshot is the strict lock-free read: verify the file, fold, convert.
// Integrity failures wrap core.ErrIntegrity (VerifyFile's posture).
func (e *Engine) Snapshot(code string) (*core.ProjectSnapshot, error) {
	snap, err := e.VerifyFile(code)
	if err != nil {
		return nil, err
	}
	state, err := eventsource.FoldEvents(snap.Events)
	if err != nil {
		return nil, err
	}
	return convertState(code, state, snap.EventCount), nil
}

// convertState renders a fold as the domain snapshot the facade projects:
// the same iteration order, ordinal assignment, alias resolution and
// tombstone handling cacheProjectFromV2StateDB used, so the projected rows
// are byte-identical to the pre-carve projection.
func convertState(code string, st *eventsource.State, eventCount int) *core.ProjectSnapshot {
	out := &core.ProjectSnapshot{ChangeCount: eventCount, TotalTasks: len(st.Tasks)}
	for _, p := range st.Projects {
		if p.Code != code || p.Tombstoned {
			continue
		}
		out.Project = projectFromV2(p)
	}
	commentAlias := func(id string) string {
		if c, ok := st.Comments[id]; ok && !c.Tombstoned {
			return c.Alias
		}
		return ""
	}
	ordinal := 0
	for _, t := range st.TasksByCreation() {
		ordinal++
		if t.Tombstoned {
			continue
		}
		out.Tasks = append(out.Tasks, taskFromV2(code, t, ordinal))
		for i, c := range st.CommentsByCreation(t.ID) {
			if c.Tombstoned {
				continue
			}
			out.Comments = append(out.Comments, commentFromV2(c, t.Alias, commentAlias(c.ReplyToRef), i+1))
		}
	}
	names := make([]string, 0, len(st.Labels))
	for name, l := range st.Labels {
		if l.Tombstoned {
			out.RemovedLabels = append(out.RemovedLabels, name)
			continue
		}
		names = append(names, name)
	}
	sort.Strings(out.RemovedLabels)
	sort.Strings(names)
	for i, name := range names {
		out.Labels = append(out.Labels, labelFromV2(st.Labels[name], i+1))
	}
	return out
}

func projectFromV2(p *eventsource.ProjectState) *core.Project {
	// A v2 project has no per-project ordinal (only tasks/comments/labels do),
	// so Ordinal is left 0 here.
	return &core.Project{
		Code:      p.Code,
		Name:      p.Name,
		CreatedAt: p.CreatedAt,
		CreatedBy: p.CreatedBy,
		UpdatedAt: p.UpdatedAt,
		UpdatedBy: p.UpdatedBy,
	}
}

func taskFromV2(code string, t *eventsource.TaskState, ordinal int) *core.Task {
	labels := append([]string(nil), t.Labels...)
	sort.Strings(labels)
	return &core.Task{
		ID:          t.Alias,
		ProjectCode: code,
		Title:       t.Title,
		Description: t.Description,
		Labels:      labels,
		Ordinal:     ordinal,
		CreatedAt:   t.CreatedAt,
		CreatedBy:   t.CreatedBy,
		UpdatedAt:   t.UpdatedAt,
		UpdatedBy:   t.UpdatedBy,
	}
}

func commentFromV2(c *eventsource.CommentState, taskAlias, replyToAlias string, ordinal int) *core.Comment {
	labels := append([]string(nil), c.Labels...)
	sort.Strings(labels)
	return &core.Comment{
		ID:        c.Alias,
		TaskID:    taskAlias,
		ReplyTo:   replyToAlias,
		Body:      c.Body,
		Labels:    labels,
		Ordinal:   ordinal,
		CreatedAt: c.CreatedAt,
		CreatedBy: c.CreatedBy,
		UpdatedAt: c.UpdatedAt,
		UpdatedBy: c.UpdatedBy,
	}
}

func labelFromV2(l *eventsource.LabelState, ordinal int) core.Label {
	return core.Label{Name: l.Name, Description: l.Description, Expr: l.Expr, Ordinal: ordinal}
}

// MediaExists reports ErrConflict when EITHER format's media is on disk for
// code. cache.db is documented as disposable and rebuildable, so a cache row
// alone is not proof of life; log.jsonl alone stopped being proof of ABSENCE
// the moment projects can be born on v2 (a v2-born project has no log.jsonl at
// all, so the old os.Stat(logPath) check would have let CreateProject append a
// second project.created over a live project). RemoveProject deletes the whole
// project directory, so both paths truly absent means the project was actually
// removed and recreation is allowed.
func (e *Engine) MediaExists(code string) error {
	for _, path := range []string{e.LogPath(code), e.EventsV2Path(code)} {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%w: project %q already exists", core.ErrConflict, code)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
