package tui

import (
	"fmt"
	"strings"
	"time"

	"atm/internal/store"
	"github.com/charmbracelet/bubbletea"
)

type tasksModel struct {
	app      *Model
	tasks    []*store.Task
	filters  store.QueryFilters
	cursor   int
	detail   *store.ShowWithContextResult
	detailID string
	mode     taskMode
	width    int
	height   int
	filter   string
}

type taskMode int

const (
	taskListMode taskMode = iota
	taskDetailMode
)

func newTasksModel(app *Model) *tasksModel {
	return &tasksModel{app: app}
}

func (t *tasksModel) setSize(w, h int) {
	t.width = w
	t.height = h
}

func (t *tasksModel) setFilter(v string) {
	t.filter = v
}

func (t *tasksModel) refresh() {
	if !t.app.storeSet {
		return
	}
	filters := t.filters
	filters.Project = t.app.projectScope
	t.tasks = t.app.store.ListTasks(filters)
	if t.cursor >= len(t.tasks) {
		t.cursor = max0(len(t.tasks) - 1)
	}
	if t.detailID != "" {
		res, err := t.app.store.ShowWithContext(t.detailID)
		if err == nil {
			t.detail = res
		} else {
			t.detail = nil
			t.detailID = ""
		}
	}
}

func (t *tasksModel) openTaskByID(id string) {
	res, err := t.app.store.ShowWithContext(id)
	if err != nil {
		t.app.showToast(fmt.Sprintf("3 not-found: %v", err))
		return
	}
	t.detail = res
	t.detailID = id
	t.mode = taskDetailMode
}

func (t *tasksModel) filtered() []*store.Task {
	if t.filter == "" {
		return t.tasks
	}
	lower := strings.ToLower(t.filter)
	var out []*store.Task
	for _, tk := range t.tasks {
		if strings.Contains(strings.ToLower(tk.ID), lower) ||
			strings.Contains(strings.ToLower(tk.Title), lower) ||
			strings.Contains(strings.ToLower(tk.Status), lower) {
			out = append(out, tk)
		}
	}
	return out
}

func (t *tasksModel) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key := msg.(tea.KeyMsg).String()
	if t.mode == taskListMode {
		return t.updateList(key)
	}
	return t.updateDetail(key)
}

func (t *tasksModel) updateList(key string) (tea.Model, tea.Cmd) {
	list := t.filtered()
	switch key {
	case "j", "down":
		if t.cursor < len(list)-1 {
			t.cursor++
		}
	case "k", "up":
		if t.cursor > 0 {
			t.cursor--
		}
	case "g":
		t.cursor = 0
	case "G":
		if len(list) > 0 {
			t.cursor = len(list) - 1
		}
	case "enter":
		if t.cursor < len(list) {
			t.openTaskByID(list[t.cursor].ID)
		}
	case t.app.km.add:
		if !t.app.requireActor() {
			t.app.showToast("set actor first")
			return t.app, nil
		}
		projects := t.app.store.ListProjects()
		defaultProject := ""
		if len(projects) > 0 {
			defaultProject = projects[0].Code
		}
		f := NewForm("New task", []formField{
			{Label: "project", Required: true, Value: defaultProject},
			{Label: "title", Required: true},
			{Label: "description"},
			{Label: "labels", Hint: "comma-separated"},
		})
		p_app_openTaskCreate(t.app, f)
	case t.app.km.taskN:
		projects := t.app.store.ListProjects()
		if len(projects) == 0 {
			t.app.showToast("no projects")
			return t.app, nil
		}
		code := projects[0].Code
		next, _, err := t.app.store.Next(code, false, "")
		if err != nil {
			t.app.showToast(fmt.Sprintf("error: %v", err))
			return t.app, nil
		}
		if next == nil {
			t.app.showToast("no claimable task in " + code)
		} else {
			t.app.showToast("next: " + next.ID + " " + next.Title)
			t.filters.Project = code
			t.refresh()
			for i, tk := range t.tasks {
				if tk.ID == next.ID {
					t.cursor = i
					break
				}
			}
		}
	case t.app.km.taskC:
		if !t.app.requireActor() {
			t.app.showToast("set actor first")
			return t.app, nil
		}
		if t.cursor < len(list) {
			id := list[t.cursor].ID
			_, err := t.app.store.Claim(id, t.app.actor)
			if err != nil {
				t.app.showToast(fmt.Sprintf("4 conflict: %v", err))
			} else {
				t.app.showToast("claimed " + id)
				t.refresh()
			}
		}
	case t.app.km.taskU:
		if !t.app.requireActor() {
			t.app.showToast("set actor first")
			return t.app, nil
		}
		if t.cursor < len(list) {
			id := list[t.cursor].ID
			_, err := t.app.store.Unclaim(id, t.app.actor)
			if err != nil {
				t.app.showToast(fmt.Sprintf("error: %v", err))
			} else {
				t.app.showToast("unclaimed " + id)
				t.refresh()
			}
		}
	}
	return t.app, nil
}

func (t *tasksModel) updateDetail(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "back":
		t.mode = taskListMode
		t.detail = nil
		t.detailID = ""
		t.refresh()
	case t.app.km.taskS:
		if !t.app.requireActor() {
			t.app.showToast("set actor first")
			return t.app, nil
		}
		if t.detail != nil && t.detail.Task != nil {
			t.showStatusOverlay(t.detail.Task.Status)
		}
	case t.app.km.taskE:
		if !t.app.requireActor() {
			t.app.showToast("set actor first")
			return t.app, nil
		}
		if t.detail != nil && t.detail.Task != nil {
			f := NewForm("Edit title", []formField{{Label: "title", Required: true, Value: t.detail.Task.Title}})
			id := t.detail.Task.ID
			t.app.openForm("set-title", f, func(fm *Form) tea.Cmd {
				vals := fm.Values()
				err := t.app.store.SetTitle(id, vals["title"], t.app.actor)
				return func() tea.Msg {
					if err != nil {
						return errMsg{err}
					}
					t.refresh()
					return refreshMsg{}
				}
			})
		}
	case t.app.km.taskB:
		if !t.app.requireActor() {
			t.app.showToast("set actor first")
			return t.app, nil
		}
		f := NewForm("Add task label", []formField{{Label: "label", Required: true}})
		id := t.detailID
		t.app.openForm("task-label-add", f, func(fm *Form) tea.Cmd {
			vals := fm.Values()
			err := t.app.store.TaskLabelAdd(id, vals["label"], t.app.actor)
			return func() tea.Msg {
				if err != nil {
					return errMsg{err}
				}
				t.refresh()
				return refreshMsg{}
			}
		})
	case t.app.km.taskBigL:
		if !t.app.requireActor() {
			t.app.showToast("set actor first")
			return t.app, nil
		}
		f := NewForm("Add link", []formField{
			{Label: "type", Required: true, Hint: "blocks|related-to|implements|documents"},
			{Label: "target", Required: true, Hint: "task id"},
		})
		id := t.detailID
		t.app.openForm("link-add", f, func(fm *Form) tea.Cmd {
			vals := fm.Values()
			err := t.app.store.LinkAdd(id, vals["type"], vals["target"], t.app.actor)
			return func() tea.Msg {
				if err != nil {
					return errMsg{err}
				}
				t.refresh()
				return refreshMsg{}
			}
		})
	case t.app.km.taskT:
		if !t.app.requireActor() {
			t.app.showToast("set actor first")
			return t.app, nil
		}
		f := NewForm("Add todo", []formField{{Label: "text", Required: true}})
		id := t.detailID
		t.app.openForm("todo-add", f, func(fm *Form) tea.Cmd {
			vals := fm.Values()
			_, err := t.app.store.TodoAdd(id, vals["text"], t.app.actor)
			return func() tea.Msg {
				if err != nil {
					return errMsg{err}
				}
				t.refresh()
				return refreshMsg{}
			}
		})
	case t.app.km.taskO:
		if !t.app.requireActor() {
			t.app.showToast("set actor first")
			return t.app, nil
		}
		f := NewForm("Add followup", []formField{
			{Label: "text", Required: true},
			{Label: "assignee", Hint: "optional"},
			{Label: "due", Hint: "RFC3339, optional"},
		})
		id := t.detailID
		t.app.openForm("followup-add", f, func(fm *Form) tea.Cmd {
			vals := fm.Values()
			var due *time.Time
			if vals["due"] != "" {
				if d, err := time.Parse(time.RFC3339, vals["due"]); err == nil {
					due = &d
				}
			}
			_, err := t.app.store.FollowupAdd(id, vals["text"], vals["assignee"], t.app.actor, due)
			return func() tea.Msg {
				if err != nil {
					return errMsg{err}
				}
				t.refresh()
				return refreshMsg{}
			}
		})
	case t.app.km.taskBigO:
		if !t.app.requireActor() {
			t.app.showToast("set actor first")
			return t.app, nil
		}
		f := NewForm("Resolve followup", []formField{{Label: "followup", Required: true, Hint: "e.g. f1"}})
		id := t.detailID
		t.app.openForm("followup-resolve", f, func(fm *Form) tea.Cmd {
			vals := fm.Values()
			_, err := t.app.store.FollowupResolve(id, vals["followup"], t.app.actor)
			return func() tea.Msg {
				if err != nil {
					return errMsg{err}
				}
				t.refresh()
				return refreshMsg{}
			}
		})
	case t.app.km.taskD:
		if !t.app.requireActor() {
			t.app.showToast("set actor first")
			return t.app, nil
		}
		f := NewForm("Add discussion", []formField{{Label: "text", Required: true}})
		id := t.detailID
		t.app.openForm("discussion-add", f, func(fm *Form) tea.Cmd {
			vals := fm.Values()
			_, err := t.app.store.DiscussionAdd(id, vals["text"], t.app.actor)
			return func() tea.Msg {
				if err != nil {
					return errMsg{err}
				}
				t.refresh()
				return refreshMsg{}
			}
		})
	case t.app.km.taskV:
		if !t.app.requireActor() {
			t.app.showToast("set actor first")
			return t.app, nil
		}
		id := t.detailID
		err := t.app.store.RequestReview(id, t.app.actor)
		if err != nil {
			t.app.showToast(fmt.Sprintf("4 conflict: %v", err))
		} else {
			t.app.showToast("review requested")
			t.refresh()
		}
	case " ":
		if !t.app.requireActor() {
			t.app.showToast("set actor first")
			return t.app, nil
		}
		if t.detail != nil && t.detail.Task != nil {
			for i, todo := range t.detail.Task.Todos {
				if !todo.Done {
					_, err := t.app.store.TodoToggle(t.detail.Task.ID, todo.ID, t.app.actor)
					if err == nil {
						t.app.showToast("toggled " + todo.ID)
						t.refresh()
					}
					_ = i
					break
				}
			}
		}
	}
	return t.app, nil
}

func (t *tasksModel) showStatusOverlay(current string) {
	allowed, ok := allowedTransitionsPub()[current]
	if !ok {
		t.app.showToast("no transitions from " + current)
		return
	}
	var lines []string
	lines = append(lines, fmt.Sprintf("current: %s", current))
	for _, s := range allStatuses() {
		if allowed[s] {
			lines = append(lines, fmt.Sprintf("  [%s] %s", statusKey(s), s))
		} else {
			lines = append(lines, fmt.Sprintf("  [ ] %s  (invalid transition)", s))
		}
	}
	lines = append(lines, "Press the key to set status; Esc to cancel.")
	t.app.overlay.title = "Set status"
	t.app.overlay.lines = lines
	t.app.overlay.visible = true
}

func statusKey(s string) string {
	switch s {
	case "open":
		return "o"
	case "in-progress":
		return "i"
	case "blocked":
		return "b"
	case "review":
		return "v"
	case "done":
		return "d"
	case "cancelled":
		return "c"
	}
	return s
}

func allStatuses() []string {
	return []string{"open", "in-progress", "blocked", "review", "done", "cancelled"}
}

func (t *tasksModel) view() string {
	if t.mode == taskDetailMode && t.detail != nil {
		return t.renderDetail()
	}
	return t.renderList()
}

func (t *tasksModel) rightView() string {
	if t.mode == taskDetailMode && t.detail != nil {
		return t.renderDetailWithActorContext(t.detail.Task)
	}
	list := t.filtered()
	var selected *store.Task
	if t.cursor >= 0 && t.cursor < len(list) {
		selected = list[t.cursor]
	}
	return t.renderTaskSummaryWithActorContext(selected)
}

func (t *tasksModel) renderDetailWithActorContext(tk *store.Task) string {
	var b strings.Builder
	b.WriteString(t.renderDetail())
	b.WriteString("\n\n")
	b.WriteString(t.actorClaimsContext(tk))
	return b.String()
}

func (t *tasksModel) renderTaskSummaryWithActorContext(tk *store.Task) string {
	var b strings.Builder
	b.WriteString("TASK DETAIL\n")
	if tk == nil {
		b.WriteString("No task selected.\n\n")
		b.WriteString(t.actorClaimsContext(nil))
		return b.String()
	}
	b.WriteString(fmt.Sprintf("%s  %s\n", tk.ID, tk.Title))
	b.WriteString(fmt.Sprintf("project: %s\nstatus: %s\nlabels: %s\n\n", tk.ProjectCode, tk.Status, strings.Join(tk.Labels, ", ")))
	b.WriteString("Actions\n  [c] claim  [u] unclaim  [s] status  [e] edit  [b] labels\n\n")
	b.WriteString("Dependencies and timeline\n  Press Enter to open full task context.\n\n")
	b.WriteString(t.actorClaimsContext(tk))
	return b.String()
}

func (t *tasksModel) actorClaimsContext(tk *store.Task) string {
	claimant := "none"
	if tk != nil && tk.Claim != nil {
		claimant = tk.Claim.Actor
	}
	return fmt.Sprintf("Actor / claims\ncurrent actor: %s\nclaimant: %s\nassignee: none", t.app.actorString(), claimant)
}

func (t *tasksModel) renderList() string {
	var b strings.Builder
	b.WriteString("TASKS")
	if t.filter != "" {
		b.WriteString("  filter: " + t.filter)
	}
	if t.filters.Project != "" {
		b.WriteString("  project: " + t.filters.Project)
	}
	if t.app.projectScope != "" {
		b.WriteString("  scope: " + t.app.projectScope)
	}
	if t.filters.Status != "" {
		b.WriteString("  status: " + t.filters.Status)
	}
	b.WriteString("\n")
	b.WriteString("  ID            TITLE                          STATUS     CLAIMANT          LABELS\n")
	list := t.filtered()
	for i, tk := range list {
		cursor := " "
		if i == t.cursor {
			cursor = ">"
		}
		claimant := "(none)"
		if tk.Claim != nil {
			claimant = tk.Claim.Actor
		}
		title := tk.Title
		if len(title) > 30 {
			title = title[:30]
		}
		b.WriteString(fmt.Sprintf("%s %-13s %-30s %-9s %-16s  %s\n",
			cursor, tk.ID, title, tk.Status, claimant, strings.Join(tk.Labels, ",")))
	}
	b.WriteString("\n  [a]dd  Enter open  [n]ext  [c]laim  [u]nclaim  [/]filter")
	return b.String()
}

func (t *tasksModel) renderDetail() string {
	res := t.detail
	tk := res.Task
	var b strings.Builder
	claim := "(none)"
	if tk.Claim != nil {
		claim = tk.Claim.Actor
	}
	b.WriteString(fmt.Sprintf("%s  %s\n", tk.ID, tk.Title))
	b.WriteString(fmt.Sprintf("status: %s   claim: %s\n", tk.Status, claim))
	b.WriteString("labels: " + strings.Join(tk.Labels, ", ") + "\n")
	b.WriteString(fmt.Sprintf("created: %s   updated: %s\n", tk.CreatedAt.Format("2006-01-02 15:04"), tk.UpdatedAt.Format("2006-01-02 15:04")))
	if tk.Description != "" {
		b.WriteString("description:\n")
		for _, line := range strings.Split(tk.Description, "\n") {
			b.WriteString("  " + line + "\n")
		}
	}
	if res.Context.Guide != nil {
		b.WriteString("\nPROJECT GUIDE (always-read)\n")
		for _, sec := range res.Context.Guide.Sections {
			b.WriteString("  " + sec.Name + ":\n")
			for _, r := range sec.Refs {
				state := guideRefState(t.app.store, tk.ProjectCode, r)
				b.WriteString(fmt.Sprintf("    [%s] %s  [%s]\n", r.Kind, r.Target, state))
			}
		}
	}
	if len(res.Context.LinksOut) > 0 || len(res.Context.LinksIn) > 0 {
		b.WriteString("\nLINKS\n")
		for _, e := range res.Context.LinksOut {
			marker := "OK"
			if isStaleTarget(t.app.store, e.Link.Target) {
				marker = "STALE"
			}
			b.WriteString(fmt.Sprintf("  out  %-12s %s  [%s]\n", e.Link.Type, e.Link.Target, marker))
		}
		for _, e := range res.Context.LinksIn {
			b.WriteString(fmt.Sprintf("  in   %-12s %s\n", e.Link.Type, e.Link.Target))
		}
	}
	if len(res.Context.Conventions) > 0 {
		b.WriteString("\nMATCHING CONVENTIONS\n")
		for _, c := range res.Context.Conventions {
			b.WriteString(fmt.Sprintf("  %s  %s   matched: %s\n", c.ID, c.Title, strings.Join(c.MatchedLabels, ",")))
		}
	}
	b.WriteString("\nTIMELINE\n")
	for _, e := range res.Context.Timeline {
		line := fmt.Sprintf("  %s  %-10s  %s", e.At.Format("2006-01-02 15:04"), e.Kind, e.ID)
		switch e.Kind {
		case "todo":
			if todo, ok := e.Data.(store.Todo); ok {
				mark := "[ ]"
				if todo.Done {
					mark = "[x]"
				}
				line += "  " + mark + " " + todo.Text + "  " + todo.Author
			}
		case "followup":
			if fu, ok := e.Data.(store.Followup); ok {
				line += "  " + fu.Text + "  " + fu.Status
			}
		case "discussion":
			if d, ok := e.Data.(store.DiscussionEntry); ok {
				line += "  " + d.Text + "  " + d.Author
			}
		case "history":
			if h, ok := e.Data.(store.HistoryEntry); ok {
				line += "  " + h.Action + " by " + h.Actor
			}
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n[t]odo Space:toggle [o]followup [O]resolve [d]isc [s]tatus [e]dit [b]label [L]link [v]review")
	return b.String()
}

func isStaleTarget(s *store.Store, id string) bool {
	_, err := s.GetTask(id)
	return err != nil
}

func guideRefState(s *store.Store, code string, r store.GuideRef) string {
	if r.Kind == "task" {
		_, err := s.GetTask(r.Target)
		if err != nil {
			return "MISS"
		}
		return "OK"
	}
	return "OK"
}

func p_app_openTaskCreate(app *Model, f *Form) {
	app.openForm("task-create", f, func(fm *Form) tea.Cmd {
		vals := fm.Values()
		labels := parseCSV(vals["labels"])
		_, err := app.store.CreateTask(vals["project"], vals["title"], vals["description"], labels, app.actor)
		return func() tea.Msg {
			if err != nil {
				return errMsg{err}
			}
			app.tasks.refresh()
			return refreshMsg{}
		}
	})
}

func allowedTransitionsPub() map[string]map[string]bool {
	return map[string]map[string]bool{
		"open":        {"in-progress": true, "blocked": true, "cancelled": true, "review": true},
		"in-progress": {"review": true, "done": true, "open": true},
		"blocked":     {"open": true, "in-progress": true, "cancelled": true},
		"review":      {"done": true, "in-progress": true, "open": true},
		"done":        {"open": true},
		"cancelled":   {"open": true},
	}
}
