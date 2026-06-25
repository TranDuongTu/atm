package tui

import (
	"fmt"
	"strings"

	"atm/internal/store"
	"github.com/charmbracelet/bubbletea"
)

type dashboardModel struct {
	app     *Model
	dash    *store.DashboardResult
	list    *dashList
	cursor  int
	width   int
	height  int
	section dashSection
	fresh   bool
	filter  string
}

type dashList struct {
	entries []dashEntry
	cursor  int
}

type dashEntry struct {
	kind     string
	taskID   string
	title    string
	claimant string
	followup string
	section  string
	state    string
}

type dashSection int

const (
	dashReview dashSection = iota
	dashFollowups
	dashGuide
)

func newDashboardModel(app *Model) *dashboardModel {
	d := &dashboardModel{app: app, list: &dashList{}, section: dashReview}
	return d
}

func (d *dashboardModel) setSize(w, h int) {
	d.width = w
	d.height = h
}

func (d *dashboardModel) setFilter(v string) {
	d.filter = v
}

func (d *dashboardModel) refresh() {
	if !d.app.storeSet {
		return
	}
	code := d.app.projectScope
	if code == "" {
		d.dash = nil
		d.rebuildList()
		d.fresh = true
		return
	}
	res, err := d.app.store.Dashboard(code)
	if err != nil {
		d.dash = nil
		return
	}
	d.dash = res
	d.rebuildList()
	d.fresh = true
}

func (d *dashboardModel) rebuildList() {
	d.list.entries = nil
	if d.dash == nil {
		return
	}
	for _, g := range d.dash.ReviewQueue.Groups {
		for _, t := range g.Tasks {
			d.list.entries = append(d.list.entries, dashEntry{
				kind:     "review",
				taskID:   t.ID,
				title:    t.Title,
				claimant: g.Claimant,
			})
		}
	}
	for _, f := range d.dash.OpenFollowups {
		d.list.entries = append(d.list.entries, dashEntry{
			kind:     "followup",
			taskID:   f.ID,
			followup: f.Followup,
			title:    f.Text,
			claimant: f.Assignee,
		})
	}
	if d.dash.GuideStatus != nil {
		for _, f := range d.dash.GuideStatus.Freshness {
			d.list.entries = append(d.list.entries, dashEntry{
				kind:    "guide",
				section: f.Section,
				taskID:  f.Target,
				title:   f.Kind + " " + f.Target,
				state:   f.State,
			})
		}
	}
}

func (d *dashboardModel) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key := msg.(tea.KeyMsg).String()
	switch key {
	case "j", "down":
		if d.list.cursor < len(d.list.entries)-1 {
			d.list.cursor++
		}
	case "k", "up":
		if d.list.cursor > 0 {
			d.list.cursor--
		}
	case "g":
		d.list.cursor = 0
	case "G":
		if len(d.list.entries) > 0 {
			d.list.cursor = len(d.list.entries) - 1
		}
	case "enter":
		e, ok := d.selected()
		if ok && e.taskID != "" {
			d.app.tasks.openTaskByID(e.taskID)
			d.app.focused = paneTasks
		}
	case d.app.km.dashboardA:
		e, ok := d.selected()
		if ok && e.kind == "review" {
			if !d.app.requireActor() {
				d.app.showToast("set actor first")
				return d.app, nil
			}
			f := NewForm("Approve review", []formField{
				{Label: "comment", Required: false, Hint: "optional review comment"},
			})
			taskID := e.taskID
			d.app.openForm("approve", f, func(fm *Form) tea.Cmd {
				vals := fm.Values()
				_, err := d.app.store.ApproveReview(taskID, d.app.actor, vals["comment"])
				return func() tea.Msg {
					if err != nil {
						return errMsg{err}
					}
					d.refresh()
					return refreshMsg{}
				}
			})
		}
	case d.app.km.dashboardR:
		e, ok := d.selected()
		if ok && e.kind == "review" {
			if !d.app.requireActor() {
				d.app.showToast("set actor first")
				return d.app, nil
			}
			f := NewForm("Reject review", []formField{
				{Label: "comment", Required: true, Hint: "comment required"},
			})
			taskID := e.taskID
			d.app.openForm("reject", f, func(fm *Form) tea.Cmd {
				vals := fm.Values()
				_, err := d.app.store.RejectReview(taskID, d.app.actor, vals["comment"])
				return func() tea.Msg {
					if err != nil {
						return errMsg{err}
					}
					d.refresh()
					return refreshMsg{}
				}
			})
		}
	case d.app.km.dashboardBigR:
		e, ok := d.selected()
		if ok && e.kind == "followup" {
			if !d.app.requireActor() {
				d.app.showToast("set actor first")
				return d.app, nil
			}
			_, err := d.app.store.FollowupResolve(e.taskID, e.followup, d.app.actor)
			if err != nil {
				d.app.showToast(fmt.Sprintf("error: %v", err))
			} else {
				d.app.showToast("followup resolved")
				d.refresh()
			}
		}
	case d.app.km.dashboardE:
		d.app.focused = paneProjects
		d.app.showToast("jump to Projects -> Guide")
	}
	return d.app, nil
}

func (d *dashboardModel) selected() (dashEntry, bool) {
	if d.list.cursor < 0 || d.list.cursor >= len(d.list.entries) {
		return dashEntry{}, false
	}
	return d.list.entries[d.list.cursor], true
}

func (d *dashboardModel) view() string {
	if !d.app.storeSet {
		return "  No store. Press [I] to init."
	}
	if len(d.app.store.ListProjects()) == 0 {
		return "  No projects. Create one in Projects (press 1)."
	}
	if d.app.projectScope == "" {
		return d.renderAggregate()
	}
	if d.dash == nil {
		return "  Summary empty for " + d.app.projectScope + ". See Projects (1) and Tasks (2)."
	}
	var b strings.Builder
	code := d.dash.Project
	tasks := d.app.store.ListTasks(store.QueryFilters{Project: code})
	counts := map[string]int{}
	for _, t := range tasks {
		counts[t.Status]++
	}
	b.WriteString(fmt.Sprintf("PROJECT %s   %d task(s)   open:%d  in-progress:%d  review:%d  done:%d  blocked:%d  cancelled:%d\n",
		code, len(tasks),
		counts["open"], counts["in-progress"], counts["review"], counts["done"], counts["blocked"], counts["cancelled"]))
	b.WriteString("\n")
	b.WriteString("REVIEW QUEUE")
	b.WriteString(fmt.Sprintf("  %d task(s) awaiting\n", countReview(d.dash)))
	for _, g := range d.dash.ReviewQueue.Groups {
		b.WriteString(fmt.Sprintf("  %s\n", g.Claimant))
		for _, t := range g.Tasks {
			marker := " "
			for _, e := range d.list.entries {
				if e.kind == "review" && e.taskID == t.ID {
					if d.list.entries[d.list.cursor].taskID == t.ID && d.list.entries[d.list.cursor].kind == "review" {
						marker = ">"
					}
					break
				}
			}
			b.WriteString(fmt.Sprintf("%s    %s  %s  review  claimed by %s\n", marker, t.ID, t.Title, g.Claimant))
		}
	}
	b.WriteString("\n")
	b.WriteString("OPEN FOLLOWUPS")
	b.WriteString(fmt.Sprintf("  %d open\n", len(d.dash.OpenFollowups)))
	for _, f := range d.dash.OpenFollowups {
		marker := " "
		for i, e := range d.list.entries {
			if e.kind == "followup" && e.taskID == f.ID && e.followup == f.Followup {
				if i == d.list.cursor {
					marker = ">"
				}
				break
			}
		}
		assignee := f.Assignee
		if assignee == "" {
			assignee = "(none)"
		}
		b.WriteString(fmt.Sprintf("%s    %s %s  %s  -> %s\n", marker, f.ID, f.Followup, f.Text, assignee))
	}
	b.WriteString("\n")
	b.WriteString("GUIDE STATUS")
	if d.dash.GuideStatus != nil {
		b.WriteString(fmt.Sprintf("  %d sections, %d refs\n", d.dash.GuideStatus.Coverage.TotalSections, d.dash.GuideStatus.Coverage.TotalRefs))
		for _, f := range d.dash.GuideStatus.Freshness {
			marker := stateMarker(f.State)
			b.WriteString(fmt.Sprintf("    %s  %s %s  [%s]\n", f.Section, f.Kind, f.Target, marker))
		}
		for _, e := range d.dash.GuideStatus.Coverage.EmptySections {
			b.WriteString(fmt.Sprintf("    %s  [EMPTY]\n", e))
		}
	} else {
		b.WriteString("  (no guide)\n")
	}
	return b.String()
}

func (d *dashboardModel) renderAggregate() string {
	tasks := d.app.store.ListTasks(store.QueryFilters{})
	counts := countStatuses(tasks)
	openFollowups := 0
	for _, tk := range tasks {
		for _, f := range tk.Followups {
			if f.Status == "open" {
				openFollowups++
			}
		}
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("SUMMARY scope: all   %d task(s)\n", len(tasks)))
	b.WriteString(fmt.Sprintf("open:%d  in-progress:%d  review:%d  done:%d  blocked:%d  cancelled:%d\n",
		counts["open"], counts["in-progress"], counts["review"], counts["done"], counts["blocked"], counts["cancelled"]))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("REVIEW QUEUE  %d task(s) awaiting\n", counts["review"]))
	b.WriteString(fmt.Sprintf("OPEN FOLLOWUPS  %d open\n", openFollowups))
	b.WriteString("GUIDE HEALTH  select a project for detailed guide freshness\n")
	return b.String()
}

func countStatuses(tasks []*store.Task) map[string]int {
	counts := map[string]int{}
	for _, t := range tasks {
		counts[t.Status]++
	}
	return counts
}

func countReview(d *store.DashboardResult) int {
	n := 0
	for _, g := range d.ReviewQueue.Groups {
		n += len(g.Tasks)
	}
	return n
}

func stateMarker(state string) string {
	switch state {
	case "fresh", "present":
		return "OK"
	case "stale":
		return "STALE"
	case "missing":
		return "MISS"
	default:
		return strings.ToUpper(state)
	}
}
