package tui

import (
	"fmt"
	"strings"

	"atm/internal/store"
	"github.com/charmbracelet/bubbletea"
)

type projectsModel struct {
	app        *Model
	projects   []*store.Project
	cursor     int
	detail     *store.Project
	mode       projMode
	width      int
	height     int
	filter     string
	paneCursor int
}

type projMode int

const (
	projList projMode = iota
	projDetail
)

func newProjectsModel(app *Model) *projectsModel {
	return &projectsModel{app: app}
}

func (p *projectsModel) setSize(w, h int) {
	p.width = w
	p.height = h
}

func (p *projectsModel) setFilter(v string) {
	p.filter = v
}

func (p *projectsModel) refresh() {
	if !p.app.storeSet {
		return
	}
	p.projects = p.app.store.ListProjects()
	if p.cursor >= len(p.projects) {
		p.cursor = max0(len(p.projects) - 1)
	}
	if p.detail != nil {
		d, err := p.app.store.GetProject(p.detail.Code)
		if err == nil {
			p.detail = d
		}
	}
}

func max0(i int) int {
	if i < 0 {
		return 0
	}
	return i
}

func (p *projectsModel) filtered() []*store.Project {
	if p.filter == "" {
		return p.projects
	}
	lower := strings.ToLower(p.filter)
	var out []*store.Project
	for _, pr := range p.projects {
		if strings.Contains(strings.ToLower(pr.Code), lower) ||
			strings.Contains(strings.ToLower(pr.Name), lower) {
			out = append(out, pr)
		}
	}
	return out
}

func (p *projectsModel) selectedCode() (string, bool) {
	list := p.filtered()
	if p.cursor < 0 || p.cursor >= len(list) {
		return "", false
	}
	return list[p.cursor].Code, true
}

func (p *projectsModel) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key := msg.(tea.KeyMsg).String()
	if p.mode == projList {
		return p.updateList(key)
	}
	return p.updateDetail(key)
}

func (p *projectsModel) updateList(key string) (tea.Model, tea.Cmd) {
	list := p.filtered()
	switch key {
	case "j", "down":
		if p.cursor < len(list)-1 {
			p.cursor++
		}
	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
		}
	case "g":
		p.cursor = 0
	case "G":
		if len(list) > 0 {
			p.cursor = len(list) - 1
		}
	case "enter", p.app.km.edit, "e":
		if p.cursor < len(list) {
			d, err := p.app.store.GetProject(list[p.cursor].Code)
			if err != nil {
				p.app.showToast(fmt.Sprintf("error: %v", err))
				return p.app, nil
			}
			p.detail = d
			p.mode = projDetail
		}
	case p.app.km.add:
		if !p.app.requireActor() {
			p.app.showToast("set actor first")
			return p.app, nil
		}
		f := NewForm("New project", []formField{
			{Label: "code", Required: true, Hint: "^[A-Z][A-Z0-9-]{1,15}$; unique"},
			{Label: "name", Required: true},
			{Label: "type-axis"},
			{Label: "labels", Hint: "comma-separated"},
			{Label: "repos", Hint: "comma-separated paths"},
		})
		p.app.openForm("project-create", f, func(fm *Form) tea.Cmd {
			vals := fm.Values()
			labels := parseCSVLabels(vals["labels"])
			repos := parseCSV(vals["repos"])
			_, err := p.app.store.CreateProject(vals["code"], vals["name"], vals["type-axis"], labels, repos, p.app.actor)
			return func() tea.Msg {
				if err != nil {
					return errMsg{err}
				}
				p.refresh()
				return refreshMsg{}
			}
		})
	case p.app.km.remove:
		if p.cursor < len(list) {
			code := list[p.cursor].Code
			tasks := p.app.store.ListTasks(store.QueryFilters{Project: code})
			if len(tasks) > 0 {
				p.app.showToast(fmt.Sprintf("4 conflict: project has %d tasks", len(tasks)))
				return p.app, nil
			}
			p.app.showOverlay("Remove project "+code, []string{
				"Project removal is not yet supported by the store.",
				"TODO: store.Project.Remove not implemented.",
				"Press Esc to close.",
			})
		}
	}
	return p.app, nil
}

func (p *projectsModel) updateDetail(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "back":
		p.mode = projList
		p.detail = nil
	case p.app.km.projectN:
		if !p.app.requireActor() {
			p.app.showToast("set actor first")
			return p.app, nil
		}
		f := NewForm("Set name", []formField{{Label: "name", Required: true, Value: p.detail.Name}})
		code := p.detail.Code
		p.app.openForm("set-name", f, func(fm *Form) tea.Cmd {
			vals := fm.Values()
			err := p.app.store.SetProjectName(code, vals["name"], p.app.actor)
			return func() tea.Msg {
				if err != nil {
					return errMsg{err}
				}
				p.refresh()
				return refreshMsg{}
			}
		})
	case p.app.km.projectT:
		if !p.app.requireActor() {
			p.app.showToast("set actor first")
			return p.app, nil
		}
		f := NewForm("Set type-axis", []formField{{Label: "namespace", Required: true, Value: p.detail.TypeAxis, Hint: "namespace must have >=1 label"}})
		code := p.detail.Code
		p.app.openForm("set-type-axis", f, func(fm *Form) tea.Cmd {
			vals := fm.Values()
			err := p.app.store.SetTypeAxis(code, vals["namespace"], p.app.actor)
			return func() tea.Msg {
				if err != nil {
					return errMsg{err}
				}
				p.refresh()
				return refreshMsg{}
			}
		})
	case p.app.km.projectBigL:
		if !p.app.requireActor() {
			p.app.showToast("set actor first")
			return p.app, nil
		}
		f := NewForm("Add label", []formField{
			{Label: "name", Required: true, Hint: "namespace:value"},
			{Label: "description"},
		})
		code := p.detail.Code
		p.app.openForm("label-add", f, func(fm *Form) tea.Cmd {
			vals := fm.Values()
			err := p.app.store.LabelAdd(code, vals["name"], vals["description"], p.app.actor)
			return func() tea.Msg {
				if err != nil {
					return errMsg{err}
				}
				p.refresh()
				return refreshMsg{}
			}
		})
	case p.app.km.projectL:
		if !p.app.requireActor() {
			p.app.showToast("set actor first")
			return p.app, nil
		}
		f := NewForm("Remove label", []formField{{Label: "name", Required: true, Hint: "existing label"}})
		code := p.detail.Code
		p.app.openForm("label-remove", f, func(fm *Form) tea.Cmd {
			vals := fm.Values()
			res, err := p.app.store.LabelRemove(code, vals["name"], p.app.actor)
			return func() tea.Msg {
				if err != nil {
					return errMsg{err}
				}
				if res != nil && res.RetainedUsage > 0 {
					p.app.showToast(fmt.Sprintf("soft-removed; retained_usage: %d", res.RetainedUsage))
				} else {
					p.app.showToast("label removed")
				}
				p.refresh()
				return refreshMsg{}
			}
		})
	case p.app.km.projectBigR:
		if !p.app.requireActor() {
			p.app.showToast("set actor first")
			return p.app, nil
		}
		f := NewForm("Add repo path", []formField{{Label: "path", Required: true}})
		code := p.detail.Code
		p.app.openForm("repo-add", f, func(fm *Form) tea.Cmd {
			vals := fm.Values()
			err := p.app.store.RepoAdd(code, vals["path"], p.app.actor)
			return func() tea.Msg {
				if err != nil {
					return errMsg{err}
				}
				p.refresh()
				return refreshMsg{}
			}
		})
	case p.app.km.projectR:
		if !p.app.requireActor() {
			p.app.showToast("set actor first")
			return p.app, nil
		}
		f := NewForm("Remove repo path", []formField{{Label: "path", Required: true}})
		code := p.detail.Code
		p.app.openForm("repo-remove", f, func(fm *Form) tea.Cmd {
			vals := fm.Values()
			err := p.app.store.RepoRemove(code, vals["path"], p.app.actor)
			return func() tea.Msg {
				if err != nil {
					return errMsg{err}
				}
				p.refresh()
				return refreshMsg{}
			}
		})
	case p.app.km.guideBigS:
		if !p.app.requireActor() {
			p.app.showToast("set actor first")
			return p.app, nil
		}
		f := NewForm("Add guide section", []formField{{Label: "name", Required: true}})
		code := p.detail.Code
		p.app.openForm("section-add", f, func(fm *Form) tea.Cmd {
			vals := fm.Values()
			err := p.app.store.GuideSectionAdd(code, vals["name"], p.app.actor)
			return func() tea.Msg {
				if err != nil {
					return errMsg{err}
				}
				p.refresh()
				return refreshMsg{}
			}
		})
	case p.app.km.guideS:
		if !p.app.requireActor() {
			p.app.showToast("set actor first")
			return p.app, nil
		}
		f := NewForm("Rename section", []formField{
			{Label: "name", Required: true},
			{Label: "new-name", Required: true},
		})
		code := p.detail.Code
		p.app.openForm("section-rename", f, func(fm *Form) tea.Cmd {
			vals := fm.Values()
			err := p.app.store.GuideSectionRename(code, vals["name"], vals["new-name"], p.app.actor)
			return func() tea.Msg {
				if err != nil {
					return errMsg{err}
				}
				p.refresh()
				return refreshMsg{}
			}
		})
	case p.app.km.guideBigX:
		if !p.app.requireActor() {
			p.app.showToast("set actor first")
			return p.app, nil
		}
		f := NewForm("Remove section", []formField{{Label: "name", Required: true}})
		code := p.detail.Code
		p.app.openForm("section-remove", f, func(fm *Form) tea.Cmd {
			vals := fm.Values()
			err := p.app.store.GuideSectionRemove(code, vals["name"], p.app.actor)
			return func() tea.Msg {
				if err != nil {
					return errMsg{err}
				}
				p.refresh()
				return refreshMsg{}
			}
		})
	case p.app.km.guideBigM:
		if !p.app.requireActor() {
			p.app.showToast("set actor first")
			return p.app, nil
		}
		f := NewForm("Move section", []formField{
			{Label: "name", Required: true},
			{Label: "before", Hint: "section to move before, or blank for end"},
		})
		code := p.detail.Code
		p.app.openForm("section-move", f, func(fm *Form) tea.Cmd {
			vals := fm.Values()
			err := p.app.store.GuideSectionMove(code, vals["name"], vals["before"], p.app.actor)
			return func() tea.Msg {
				if err != nil {
					return errMsg{err}
				}
				p.refresh()
				return refreshMsg{}
			}
		})
	case p.app.km.guideG:
		if !p.app.requireActor() {
			p.app.showToast("set actor first")
			return p.app, nil
		}
		f := NewForm("Add guide ref", []formField{
			{Label: "section", Required: true},
			{Label: "kind", Required: true, Hint: "task|file"},
			{Label: "target", Required: true, Hint: "task id or absolute file path"},
		})
		code := p.detail.Code
		p.app.openForm("ref-add", f, func(fm *Form) tea.Cmd {
			vals := fm.Values()
			err := p.app.store.GuideRefAdd(code, vals["section"], vals["kind"], vals["target"], p.app.actor)
			return func() tea.Msg {
				if err != nil {
					return errMsg{err}
				}
				p.refresh()
				return refreshMsg{}
			}
		})
	case p.app.km.guideM:
		if !p.app.requireActor() {
			p.app.showToast("set actor first")
			return p.app, nil
		}
		f := NewForm("Move guide ref", []formField{
			{Label: "section", Required: true},
			{Label: "kind", Required: true},
			{Label: "target", Required: true},
			{Label: "before", Hint: "target to move before, or blank for end"},
		})
		code := p.detail.Code
		p.app.openForm("ref-move", f, func(fm *Form) tea.Cmd {
			vals := fm.Values()
			err := p.app.store.GuideRefMove(code, vals["section"], vals["kind"], vals["target"], vals["before"], p.app.actor)
			return func() tea.Msg {
				if err != nil {
					return errMsg{err}
				}
				p.refresh()
				return refreshMsg{}
			}
		})
	case p.app.km.guideD:
		if !p.app.requireActor() {
			p.app.showToast("set actor first")
			return p.app, nil
		}
		f := NewForm("Remove guide ref", []formField{
			{Label: "section", Required: true},
			{Label: "kind", Required: true},
			{Label: "target", Required: true},
		})
		code := p.detail.Code
		p.app.openForm("ref-remove", f, func(fm *Form) tea.Cmd {
			vals := fm.Values()
			err := p.app.store.GuideRefRemove(code, vals["section"], vals["kind"], vals["target"], p.app.actor)
			return func() tea.Msg {
				if err != nil {
					return errMsg{err}
				}
				p.refresh()
				return refreshMsg{}
			}
		})
	case p.app.km.guideBigF:
		if !p.app.requireActor() {
			p.app.showToast("set actor first")
			return p.app, nil
		}
		current := ""
		if p.detail != nil {
			current = p.detail.GuideFreshnessThreshold
		}
		f := NewForm("Set freshness", []formField{
			{Label: "threshold", Required: true, Value: current, Hint: "duration (720h) or 'unset'"},
		})
		code := p.detail.Code
		p.app.openForm("set-freshness", f, func(fm *Form) tea.Cmd {
			vals := fm.Values()
			err := p.app.store.GuideSetFreshness(code, vals["threshold"], p.app.actor)
			return func() tea.Msg {
				if err != nil {
					return errMsg{err}
				}
				p.refresh()
				return refreshMsg{}
			}
		})
	case p.app.km.guideBigD:
		if p.detail != nil {
			d_app_setDashProject(p.app, p.detail.Code)
		}
	}
	return p.app, nil
}

func (p *projectsModel) view() string {
	if p.mode == projDetail && p.detail != nil {
		return p.renderDetail()
	}
	return p.renderList()
}

func (p *projectsModel) renderList() string {
	var b strings.Builder
	b.WriteString("PROJECTS")
	if p.filter != "" {
		b.WriteString("  filter: " + p.filter)
	}
	b.WriteString("\n")
	b.WriteString("  CODE    NAME                            TASKS   LABELS   GUIDE   UPDATED\n")
	list := p.filtered()
	for i, pr := range list {
		cursor := " "
		if i == p.cursor {
			cursor = ">"
		}
		guide := "none"
		if pr.Guide != nil && len(pr.Guide.Sections) > 0 {
			guide = fmt.Sprintf("%d", len(pr.Guide.Sections))
		}
		tasks := len(p.app.store.ListTasks(store.QueryFilters{Project: pr.Code}))
		name := pr.Name
		if len(name) > 31 {
			name = name[:31]
		}
		b.WriteString(fmt.Sprintf("%s %-6s  %-31s  %-5d   %-7d  %-6s  %s\n",
			cursor, pr.Code, name, tasks, len(pr.Labels), guide, pr.UpdatedAt.Format("2006-01-02 15:04")))
	}
	b.WriteString("\n  [a]dd  [e]/Enter detail  [x] remove (zero-task guard)")
	return b.String()
}

func (p *projectsModel) renderDetail() string {
	pr := p.detail
	var b strings.Builder
	b.WriteString(fmt.Sprintf("PROJECT %s  %s\n\n", pr.Code, pr.Name))
	b.WriteString("FACTS & LABELS\n")
	b.WriteString(fmt.Sprintf("  code:    %s\n", pr.Code))
	b.WriteString(fmt.Sprintf("  name:    %s\n", pr.Name))
	b.WriteString(fmt.Sprintf("  type-axis: %s\n", pr.TypeAxis))
	b.WriteString(fmt.Sprintf("  next_task_n: %d\n", pr.NextTaskN))
	b.WriteString(fmt.Sprintf("  created: %s\n", pr.CreatedAt.Format("2006-01-02")))
	b.WriteString(fmt.Sprintf("  updated: %s\n", pr.UpdatedAt.Format("2006-01-02")))
	if len(pr.RepoPaths) > 0 {
		b.WriteString("  repo paths:\n")
		for _, r := range pr.RepoPaths {
			b.WriteString("    " + r + "\n")
		}
	}
	b.WriteString("\nLABELS\n")
	if len(pr.Labels) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, l := range pr.Labels {
			line := "  " + l.Name
			if l.Description != "" {
				line += "  " + l.Description
			}
			b.WriteString(line + "\n")
		}
	}
	b.WriteString("\nGUIDE\n")
	if pr.Guide == nil || len(pr.Guide.Sections) == 0 {
		b.WriteString("  (no guide)\n")
	} else {
		for _, sec := range pr.Guide.Sections {
			marker := " "
			if len(sec.Refs) == 0 {
				marker = " [EMPTY]"
			}
			b.WriteString(fmt.Sprintf("  %s  %d refs%s\n", sec.Name, len(sec.Refs), marker))
			for _, r := range sec.Refs {
				state := refState(p.app.store, pr.Code, r)
				b.WriteString(fmt.Sprintf("    [%s] %s  [%s]\n", r.Kind, r.Target, state))
			}
		}
		if pr.GuideFreshnessThreshold != "" {
			b.WriteString(fmt.Sprintf("  freshness threshold: %s\n", pr.GuideFreshnessThreshold))
		}
		if pr.Guide != nil {
			b.WriteString(fmt.Sprintf("  guide updated: %s by %s\n", pr.Guide.UpdatedAt.Format("2006-01-02"), pr.Guide.UpdatedBy))
		}
	}
	b.WriteString("\n[L]add label [l]remove [T]type-axis [N]name [R]/[r]repo [S]/[s]/[X]/[M]section [g]/[m]/[d]ref [F]freshness [D]dashboard")
	return b.String()
}

func refState(s *store.Store, code string, r store.GuideRef) string {
	if r.Kind == "task" {
		t, err := s.GetTask(r.Target)
		if err != nil || t == nil {
			return "MISS"
		}
		return "OK"
	}
	return "OK"
}

func parseCSVLabels(s string) []store.Label {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var out []store.Label
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, store.Label{Name: p})
		}
	}
	return out
}

func parseCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
