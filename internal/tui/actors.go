package tui

import (
	"fmt"
	"strings"

	"atm/internal/store"
	"github.com/charmbracelet/bubbletea"
)

type actorsModel struct {
	app    *Model
	actors []store.Actor
	cursor int
	detail *actorDetail
	mode   actorMode
	width  int
	height int
	filter string
}

type actorDetail struct {
	actor         store.Actor
	claimedTasks  []store.Task
	openFollowups []store.Followup
}

type actorMode int

const (
	actorListMode actorMode = iota
	actorDetailMode
)

func newActorsModel(app *Model) *actorsModel {
	return &actorsModel{app: app}
}

func (a *actorsModel) setSize(w, h int) {
	a.width = w
	a.height = h
}

func (a *actorsModel) setFilter(v string) {
	a.filter = v
}

func (a *actorsModel) refresh() {
	if !a.app.storeSet {
		return
	}
	a.actors = a.app.store.List()
	if a.cursor >= len(a.actors) {
		a.cursor = max0(len(a.actors) - 1)
	}
	if a.detail != nil {
		a.loadDetail(a.detail.actor.ID)
	}
}

func (a *actorsModel) filtered() []store.Actor {
	if a.filter == "" {
		return a.actors
	}
	lower := strings.ToLower(a.filter)
	var out []store.Actor
	for _, ac := range a.actors {
		if strings.Contains(strings.ToLower(ac.ID), lower) ||
			strings.Contains(strings.ToLower(ac.Name), lower) {
			out = append(out, ac)
		}
	}
	return out
}

func (a *actorsModel) loadDetail(id string) {
	actor, err := a.app.store.Get(id)
	if err != nil {
		a.detail = nil
		return
	}
	allTasks := a.app.store.ListTasks(store.QueryFilters{Claimant: id})
	var claimed []store.Task
	for _, t := range allTasks {
		if t.Status != "done" && t.Status != "cancelled" {
			claimed = append(claimed, *t)
		}
	}
	allFollowups := a.app.store.ListTasks(store.QueryFilters{})
	var open []store.Followup
	for _, t := range allFollowups {
		for _, f := range t.Followups {
			if f.Status == "open" && f.Assignee == id {
				open = append(open, f)
			}
		}
	}
	a.detail = &actorDetail{actor: actor, claimedTasks: claimed, openFollowups: open}
}

func (a *actorsModel) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key := msg.(tea.KeyMsg).String()
	if a.mode == actorListMode {
		return a.updateList(key)
	}
	return a.updateDetail(key)
}

func (a *actorsModel) updateList(key string) (tea.Model, tea.Cmd) {
	list := a.filtered()
	switch key {
	case "j", "down":
		if a.cursor < len(list)-1 {
			a.cursor++
		}
	case "k", "up":
		if a.cursor > 0 {
			a.cursor--
		}
	case "g":
		a.cursor = 0
	case "G":
		if len(list) > 0 {
			a.cursor = len(list) - 1
		}
	case "enter":
		if a.cursor < len(list) {
			a.loadDetail(list[a.cursor].ID)
			a.mode = actorDetailMode
		}
	}
	return a.app, nil
}

func (a *actorsModel) updateDetail(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "back":
		a.mode = actorListMode
		a.detail = nil
	}
	return a.app, nil
}

func (a *actorsModel) view() string {
	if a.mode == actorDetailMode && a.detail != nil {
		return a.renderDetail()
	}
	return a.renderList()
}

func (a *actorsModel) renderList() string {
	var b strings.Builder
	b.WriteString("ACTORS")
	if a.filter != "" {
		b.WriteString("  filter: " + a.filter)
	}
	b.WriteString("\n")
	b.WriteString("  ID                 KIND    NAME         FIRST SEEN   CLAIMED   OPEN FOLLOWUPS\n")
	list := a.filtered()
	for i, ac := range list {
		cursor := " "
		if i == a.cursor {
			cursor = ">"
		}
		claimed := len(a.app.store.ListTasks(store.QueryFilters{Claimant: ac.ID}))
		b.WriteString(fmt.Sprintf("%s %-18s %-7s %-12s %-11s %-9d\n",
			cursor, ac.ID, ac.Kind, ac.Name, ac.FirstSeen.Format("2006-01-02"), claimed))
	}
	b.WriteString("\n  [Enter] show detail")
	return b.String()
}

func (a *actorsModel) renderDetail() string {
	d := a.detail
	var b strings.Builder
	b.WriteString(fmt.Sprintf("ACTOR  %s\n\n", d.actor.ID))
	b.WriteString(fmt.Sprintf("kind: %s   name: %s   first seen: %s\n", d.actor.Kind, d.actor.Name, d.actor.FirstSeen.Format("2006-01-02")))
	b.WriteString(fmt.Sprintf("\nCLAIMED TASKS (%d)\n", len(d.claimedTasks)))
	for _, t := range d.claimedTasks {
		b.WriteString(fmt.Sprintf("  %s  %s  %s\n", t.ID, t.Status, t.Title))
	}
	b.WriteString(fmt.Sprintf("\nOPEN FOLLOWUPS (%d)\n", len(d.openFollowups)))
	for _, f := range d.openFollowups {
		b.WriteString(fmt.Sprintf("  %s  %s\n", f.ID, f.Text))
	}
	b.WriteString("\n[Esc] back")
	return b.String()
}
