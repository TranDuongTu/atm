package cli

import (
	"fmt"
	"io"
	"strings"

	"atm/internal/core"
)

const (
	outputJSON = "json"
	outputText = "text"
)

// ---- v2 JSON shapes ----

type jsonHistory struct {
	Seq    int    `json:"seq"`
	Action string `json:"action"`
	Actor  string `json:"actor"`
	At     string `json:"at"`
}

type jsonTask struct {
	ID          string   `json:"id"`
	ProjectCode string   `json:"project_code"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Labels      []string `json:"labels"`
	// Ordinal is the entity's v2 creation ordinal from the projector fold.
	Ordinal   int           `json:"ordinal"`
	History   []jsonHistory `json:"history"`
	CreatedAt string        `json:"created_at"`
	CreatedBy string        `json:"created_by"`
	UpdatedAt string        `json:"updated_at"`
	UpdatedBy string        `json:"updated_by"`
}

type jsonProject struct {
	Code      string                `json:"code"`
	Name      string                `json:"name"`
	Ordinal   int                   `json:"ordinal"`
	History   []jsonHistory         `json:"history"`
	CreatedAt string                `json:"created_at"`
	CreatedBy string                `json:"created_by"`
	UpdatedAt string                `json:"updated_at"`
	UpdatedBy string                `json:"updated_by"`
	Embedding *core.EmbeddingConfig `json:"embedding,omitempty"`
}

type jsonLabel struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Expr        string `json:"expr,omitempty"`
}

// IsComputed reports whether this label's membership is derived (a board or
// namespace label) rather than asserted by tasks.
func (l jsonLabel) IsComputed() bool {
	return l.Expr != "" || core.IsNamespaceName(l.Name)
}

type jsonLabelGroup struct {
	Label string     `json:"label"`
	Tasks []jsonTask `json:"tasks"`
}

type jsonFacets struct {
	Groups []jsonLabelGroup `json:"groups"`
	Others []jsonTask       `json:"others"`
}

// ---- mappers ----

func historyToJSON(h []core.HistoryView) []jsonHistory {
	out := make([]jsonHistory, 0, len(h))
	for _, e := range h {
		out = append(out, jsonHistory{
			Seq:    e.Seq,
			Action: e.Action,
			Actor:  e.Actor,
			At:     core.RFC3339UTC(e.At),
		})
	}
	return out
}

func taskToJSON(t *core.Task, history []core.HistoryView) jsonTask {
	return jsonTask{
		ID:          t.ID,
		ProjectCode: t.ProjectCode,
		Title:       t.Title,
		Description: t.Description,
		Labels:      normalizeStrSlice(t.Labels),
		Ordinal:     t.Ordinal,
		History:     historyToJSON(history),
		CreatedAt:   core.RFC3339UTC(t.CreatedAt),
		CreatedBy:   t.CreatedBy,
		UpdatedAt:   core.RFC3339UTC(t.UpdatedAt),
		UpdatedBy:   t.UpdatedBy,
	}
}

func tasksToJSON(ts []*core.Task) []jsonTask {
	out := make([]jsonTask, 0, len(ts))
	for _, t := range ts {
		out = append(out, taskToJSON(t, nil))
	}
	return out
}

func projectToJSON(p *core.Project, history []core.HistoryView) jsonProject {
	return jsonProject{
		Code:      p.Code,
		Name:      p.Name,
		Ordinal:   p.Ordinal,
		History:   historyToJSON(history),
		CreatedAt: core.RFC3339UTC(p.CreatedAt),
		CreatedBy: p.CreatedBy,
		UpdatedAt: core.RFC3339UTC(p.UpdatedAt),
		UpdatedBy: p.UpdatedBy,
	}
}

func projectsToJSON(ps []*core.Project) []jsonProject {
	out := make([]jsonProject, 0, len(ps))
	for _, p := range ps {
		out = append(out, projectToJSON(p, nil))
	}
	return out
}

func labelToJSON(l core.Label) jsonLabel {
	return jsonLabel{Name: l.Name, Description: l.Description, Expr: l.Expr}
}

func labelsToJSON(ls []core.Label) []jsonLabel {
	out := make([]jsonLabel, 0, len(ls))
	for _, l := range ls {
		out = append(out, labelToJSON(l))
	}
	return out
}

func normalizeStrSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	out := make([]string, len(s))
	copy(out, s)
	return out
}

// ---- helpers ----

func writeJSON(out io.Writer, v any) error {
	data, err := core.MarshalSorted(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(data))
	return err
}

func renderTime(s string) string {
	return s
}

func formatLabels(labels []string) string {
	if len(labels) == 0 {
		return "-"
	}
	return strings.Join(labels, ",")
}

// ---- text renderers ----

func renderTaskText(t jsonTask) string {
	return fmt.Sprintf("%s\t%s\t%s", t.ID, t.Title, formatLabels(t.Labels))
}

func renderTaskListText(ts []jsonTask) string {
	var b strings.Builder
	for _, t := range ts {
		fmt.Fprintf(&b, "%s\t%s\t%s\n", t.ID, t.Title, formatLabels(t.Labels))
	}
	return b.String()
}

func renderProjectText(p jsonProject) string {
	return fmt.Sprintf("%s\t%s\t%s", p.Code, p.Name, renderTime(p.UpdatedAt))
}

func renderProjectListText(ps []jsonProject) string {
	var b strings.Builder
	for _, p := range ps {
		fmt.Fprintf(&b, "%s\t%s\t%s\n", p.Code, p.Name, renderTime(p.UpdatedAt))
	}
	return b.String()
}

func renderLabelListText(ls []jsonLabel) string {
	var b strings.Builder
	for _, l := range ls {
		marker := " "
		if l.IsComputed() {
			marker = "*"
		}
		if l.Description == "" {
			fmt.Fprintf(&b, "%s %s\n", marker, l.Name)
		} else {
			fmt.Fprintf(&b, "%s %s\t%s\n", marker, l.Name, l.Description)
		}
	}
	return b.String()
}

type jsonComment struct {
	ID      string   `json:"id"`
	TaskID  string   `json:"task_id"`
	ReplyTo string   `json:"reply_to,omitempty"`
	Body    string   `json:"body"`
	Labels  []string `json:"labels"`
	// Ordinal is the comment's v2 creation ordinal from the projector fold.
	Ordinal   int           `json:"ordinal"`
	History   []jsonHistory `json:"history"`
	CreatedAt string        `json:"created_at"`
	CreatedBy string        `json:"created_by"`
	UpdatedAt string        `json:"updated_at"`
	UpdatedBy string        `json:"updated_by"`
}

func commentToJSON(c *core.Comment, hv []core.HistoryView) jsonComment {
	return jsonComment{
		ID:        c.ID,
		TaskID:    c.TaskID,
		ReplyTo:   c.ReplyTo,
		Body:      c.Body,
		Labels:    normalizeStrSlice(c.Labels),
		Ordinal:   c.Ordinal,
		History:   historyToJSON(hv),
		CreatedAt: core.RFC3339UTC(c.CreatedAt),
		CreatedBy: c.CreatedBy,
		UpdatedAt: core.RFC3339UTC(c.UpdatedAt),
		UpdatedBy: c.UpdatedBy,
	}
}

func commentsToJSON(cs []*core.Comment) []jsonComment {
	out := make([]jsonComment, 0, len(cs))
	for _, c := range cs {
		out = append(out, commentToJSON(c, nil))
	}
	return out
}

func renderCommentListText(cs []jsonComment) string {
	var b strings.Builder
	for _, c := range cs {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\n", c.ID, c.CreatedAt, c.CreatedBy, formatLabels(c.Labels))
		for _, line := range strings.Split(c.Body, "\n") {
			fmt.Fprintf(&b, "    %s\n", line)
		}
	}
	return b.String()
}

func renderCommentText(c jsonComment) string {
	var b strings.Builder
	fmt.Fprintf(&b, "id      %s\n", c.ID)
	fmt.Fprintf(&b, "task    %s\n", c.TaskID)
	if c.ReplyTo != "" {
		fmt.Fprintf(&b, "reply-to %s\n", c.ReplyTo)
	}
	fmt.Fprintf(&b, "actor   %s\n", c.CreatedBy)
	fmt.Fprintf(&b, "created %s\n", c.CreatedAt)
	fmt.Fprintf(&b, "updated %s  by %s\n", c.UpdatedAt, c.UpdatedBy)
	fmt.Fprintf(&b, "labels  %s\n", formatLabels(c.Labels))
	b.WriteString("\n")
	b.WriteString(c.Body)
	b.WriteString("\n")
	return b.String()
}

func renderFacetsText(f jsonFacets) string {
	var b strings.Builder
	for _, g := range f.Groups {
		fmt.Fprintf(&b, "%s (%d)\n", g.Label, len(g.Tasks))
		for _, t := range g.Tasks {
			fmt.Fprintf(&b, "  %s\t%s\t%s\n", t.ID, t.Title, formatLabels(t.Labels))
		}
	}
	if len(f.Others) > 0 {
		fmt.Fprintf(&b, "(no matching labels) (%d)\n", len(f.Others))
		for _, t := range f.Others {
			fmt.Fprintf(&b, "  %s\t%s\t%s\n", t.ID, t.Title, formatLabels(t.Labels))
		}
	}
	return b.String()
}
