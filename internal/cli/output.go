package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"atm/internal/store"
)

type outputFormat string

const (
	outputJSON = "json"
	outputText = "text"
)

func renderTime(t time.Time) string {
	return store.RFC3339UTC(t)
}

type jsonClaim struct {
	Actor string `json:"actor"`
	At    string `json:"at"`
}

type jsonLink struct {
	Type   string `json:"type"`
	Target string `json:"target"`
}

type jsonLinkEdge struct {
	Link      jsonLink `json:"link"`
	Direction string   `json:"direction"`
}

type jsonTodo struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Done   bool   `json:"done"`
	Author string `json:"author"`
	At     string `json:"at"`
}

type jsonFollowup struct {
	ID         string  `json:"id"`
	Text       string  `json:"text"`
	Assignee   string  `json:"assignee"`
	Status     string  `json:"status"`
	Due        *string `json:"due,omitempty"`
	Author     string  `json:"author"`
	At         string  `json:"at"`
	ResolvedAt *string `json:"resolved_at,omitempty"`
	ResolvedBy *string `json:"resolved_by,omitempty"`
}

type jsonDiscussion struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Author string `json:"author"`
	At     string `json:"at"`
}

type jsonHistory struct {
	ID     string         `json:"id"`
	Action string         `json:"action"`
	Actor  string         `json:"actor"`
	At     string         `json:"at"`
	Meta   map[string]any `json:"meta"`
}

type jsonTask struct {
	ID          string           `json:"id"`
	ProjectCode string           `json:"project_code"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Status      string           `json:"status"`
	Labels      []string         `json:"labels"`
	Links       []jsonLink       `json:"links"`
	Claim       *jsonClaim       `json:"claim"`
	Todos       []jsonTodo       `json:"todos"`
	Followups   []jsonFollowup   `json:"followups"`
	Discussions []jsonDiscussion `json:"discussions"`
	History     []jsonHistory    `json:"history"`
	CreatedAt   string           `json:"created_at"`
	UpdatedAt   string           `json:"updated_at"`
}

func taskToJSON(t *store.Task) jsonTask {
	if t == nil {
		return jsonTask{}
	}
	out := jsonTask{
		ID:          t.ID,
		ProjectCode: t.ProjectCode,
		Title:       t.Title,
		Description: t.Description,
		Status:      t.Status,
		Labels:      normalizeStrSlice(t.Labels),
		Links:       linksToJSON(t.Links),
		Claim:       claimToJSON(t.Claim),
		Todos:       todosToJSON(t.Todos),
		Followups:   followupsToJSON(t.Followups),
		Discussions: discussionsToJSON(t.Discussions),
		History:     historyToJSON(t.History),
		CreatedAt:   renderTime(t.CreatedAt),
		UpdatedAt:   renderTime(t.UpdatedAt),
	}
	return out
}

func normalizeStrSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func linksToJSON(links []store.Link) []jsonLink {
	out := make([]jsonLink, 0, len(links))
	for _, l := range links {
		out = append(out, jsonLink{Type: l.Type, Target: l.Target})
	}
	return out
}

func claimToJSON(c *store.Claim) *jsonClaim {
	if c == nil {
		return nil
	}
	return &jsonClaim{Actor: c.Actor, At: renderTime(c.At)}
}

func todosToJSON(todos []store.Todo) []jsonTodo {
	out := make([]jsonTodo, 0, len(todos))
	for _, t := range todos {
		out = append(out, jsonTodo{
			ID: t.ID, Text: t.Text, Done: t.Done, Author: t.Author, At: renderTime(t.At),
		})
	}
	return out
}

func followupsToJSON(fus []store.Followup) []jsonFollowup {
	out := make([]jsonFollowup, 0, len(fus))
	for _, f := range fus {
		jf := jsonFollowup{
			ID: f.ID, Text: f.Text, Assignee: f.Assignee, Status: f.Status,
			Author: f.Author, At: renderTime(f.At),
		}
		if f.Due != nil {
			s := renderTime(*f.Due)
			jf.Due = &s
		}
		if f.ResolvedAt != nil {
			s := renderTime(*f.ResolvedAt)
			jf.ResolvedAt = &s
		}
		if f.ResolvedBy != "" {
			rb := f.ResolvedBy
			jf.ResolvedBy = &rb
		}
		out = append(out, jf)
	}
	return out
}

func discussionsToJSON(ds []store.DiscussionEntry) []jsonDiscussion {
	out := make([]jsonDiscussion, 0, len(ds))
	for _, d := range ds {
		out = append(out, jsonDiscussion{ID: d.ID, Text: d.Text, Author: d.Author, At: renderTime(d.At)})
	}
	return out
}

func historyToJSON(hs []store.HistoryEntry) []jsonHistory {
	out := make([]jsonHistory, 0, len(hs))
	for _, h := range hs {
		meta := h.Meta
		if meta == nil {
			meta = map[string]any{}
		}
		out = append(out, jsonHistory{ID: h.ID, Action: h.Action, Actor: h.Actor, At: renderTime(h.At), Meta: meta})
	}
	return out
}

func edgesToJSON(edges []store.LinkEdge) []jsonLinkEdge {
	out := make([]jsonLinkEdge, 0, len(edges))
	for _, e := range edges {
		out = append(out, jsonLinkEdge{Link: jsonLink{Type: e.Link.Type, Target: e.Link.Target}, Direction: e.Direction})
	}
	return out
}

type jsonConvention struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	MatchedLabels []string `json:"matched_labels"`
}

type jsonGuideRef struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
}

type jsonGuideSection struct {
	Name string         `json:"name"`
	Refs []jsonGuideRef `json:"refs"`
}

type jsonGuide struct {
	Sections  []jsonGuideSection `json:"sections"`
	UpdatedAt string             `json:"updated_at"`
	UpdatedBy string             `json:"updated_by"`
}

func guideToJSON(g *store.Guide) *jsonGuide {
	if g == nil {
		return nil
	}
	out := &jsonGuide{
		UpdatedAt: renderTime(g.UpdatedAt),
		UpdatedBy: g.UpdatedBy,
	}
	sections := make([]jsonGuideSection, 0, len(g.Sections))
	for _, s := range g.Sections {
		refs := make([]jsonGuideRef, 0, len(s.Refs))
		for _, r := range s.Refs {
			refs = append(refs, jsonGuideRef{Kind: r.Kind, Target: r.Target})
		}
		sections = append(sections, jsonGuideSection{Name: s.Name, Refs: refs})
	}
	out.Sections = sections
	return out
}

type jsonTimelineEntry struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	At   string `json:"at"`
	Data any    `json:"data"`
}

func timelineToJSON(entries []store.TimelineEntry) []jsonTimelineEntry {
	out := make([]jsonTimelineEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, jsonTimelineEntry{Kind: e.Kind, ID: e.ID, At: renderTime(e.At), Data: e.Data})
	}
	return out
}

type jsonLabel struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func labelsToJSON(ls []store.Label) []jsonLabel {
	out := make([]jsonLabel, 0, len(ls))
	for _, l := range ls {
		out = append(out, jsonLabel{Name: l.Name, Description: l.Description})
	}
	return out
}

type jsonProject struct {
	Code                    string        `json:"code"`
	Name                    string        `json:"name"`
	TypeAxis                string        `json:"type_axis"`
	Labels                  []jsonLabel   `json:"labels"`
	NextTaskN               int           `json:"next_task_n"`
	Guide                   *jsonGuide    `json:"guide"`
	GuideFreshnessThreshold string        `json:"guide_freshness_threshold"`
	RepoPaths               []string      `json:"repo_paths"`
	History                 []jsonHistory `json:"history"`
	CreatedAt               string        `json:"created_at"`
	CreatedBy               string        `json:"created_by"`
	UpdatedAt               string        `json:"updated_at"`
}

func projectToJSON(p *store.Project) jsonProject {
	repos := p.RepoPaths
	if repos == nil {
		repos = []string{}
	}
	hist := historyToJSON(p.History)
	if hist == nil {
		hist = []jsonHistory{}
	}
	return jsonProject{
		Code:                    p.Code,
		Name:                    p.Name,
		TypeAxis:                p.TypeAxis,
		Labels:                  labelsToJSON(p.Labels),
		NextTaskN:               p.NextTaskN,
		Guide:                   guideToJSON(p.Guide),
		GuideFreshnessThreshold: p.GuideFreshnessThreshold,
		RepoPaths:               repos,
		History:                 hist,
		CreatedAt:               renderTime(p.CreatedAt),
		CreatedBy:               p.CreatedBy,
		UpdatedAt:               renderTime(p.UpdatedAt),
	}
}

func writeJSON(w io.Writer, v any) error {
	data, err := store.MarshalSorted(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

func renderTaskText(w io.Writer, t *store.Task) {
	if t == nil {
		fmt.Fprintln(w, "no task")
		return
	}
	fmt.Fprintf(w, "%s  %s  [%s]\n", t.ID, t.Title, t.Status)
	if t.Description != "" {
		fmt.Fprintf(w, "  description: %s\n", t.Description)
	}
	if len(t.Labels) > 0 {
		fmt.Fprintf(w, "  labels: %s\n", strings.Join(t.Labels, ", "))
	}
	if t.Claim != nil {
		fmt.Fprintf(w, "  claimed by %s at %s\n", t.Claim.Actor, renderTime(t.Claim.At))
	}
}

func renderTaskListText(w io.Writer, tasks []*store.Task) {
	for _, t := range tasks {
		fmt.Fprintf(w, "%s  %s  [%s]\n", t.ID, t.Title, t.Status)
	}
}

func renderNextText(w io.Writer, t *store.Task, guide *store.Guide) {
	if t == nil {
		fmt.Fprintln(w, "no claimable task")
	} else {
		fmt.Fprintf(w, "%s  %s  [%s]\n", t.ID, t.Title, t.Status)
		if t.Claim != nil {
			fmt.Fprintf(w, "  claimed by %s at %s\n", t.Claim.Actor, renderTime(t.Claim.At))
		}
	}
	if guide != nil {
		fmt.Fprintf(w, "guide: %d section(s), updated by %s\n", len(guide.Sections), guide.UpdatedBy)
	} else {
		fmt.Fprintln(w, "guide: (none)")
	}
}
