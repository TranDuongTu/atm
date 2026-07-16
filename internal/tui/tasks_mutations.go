package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
)

func (t *tasksModel) openCreateForm() {
	labelsValidator := func(field, value string) error {
		if value == "" {
			return nil
		}
		for _, tok := range strings.Fields(value) {
			if !labelSuffixRe.MatchString(tok) {
				return fmt.Errorf("bad label %q: use <namespace>:<value> or <tag>", tok)
			}
		}
		return nil
	}
	fields := []formField{
		{Label: "title", Required: true, Hint: "task title"},
		{Label: "description", Required: false, Hint: "optional; multi-line later"},
		{Label: "labels", Required: false, Hint: "space-separated suffixes, e.g. 'status:open type:bug' (prefix auto-added)", Validator: labelsValidator},
	}
	f := NewForm("New task  "+t.m.projectScope+":", fields)
	f.Title = "New task  " + t.m.projectScope + ":"
	t.m.form = f
	t.m.formKind = formTaskCreate
}

func (t *tasksModel) openTitleForm() {
	tk := t.detail.task
	if tk == nil {
		return
	}
	fields := []formField{
		{Label: "title", Required: true, Value: tk.Title, Hint: "new task title"},
	}
	f := NewForm("Edit title", fields)
	t.m.form = f
	t.m.formKind = formTaskSetTitle
}

func (t *tasksModel) openDescriptionForm() {
	tk := t.detail.task
	if tk == nil {
		return
	}
	fields := []formField{
		{Label: "description", Required: false, Value: tk.Description, Hint: "new description (empty clears)"},
	}
	f := NewForm("Edit description", fields)
	t.m.form = f
	t.m.formKind = formTaskSetDescription
}

func (t *tasksModel) openLabelAddForm() {
	tk := t.detail.task
	if tk == nil {
		return
	}
	validator := func(field, value string) error {
		if value == "" {
			return nil
		}
		if !labelSuffixRe.MatchString(value) {
			return fmt.Errorf("use <namespace>:<value> or <tag>, e.g. status:open")
		}
		return nil
	}
	fields := []formField{
		{Label: "name", Required: true, Hint: "<namespace>:<value> or <tag>", Validator: validator},
	}
	f := NewForm("Add label  "+t.m.projectScope+":", fields)
	f.Title = "Add label  " + t.m.projectScope + ":"
	t.m.form = f
	t.m.formKind = formTaskLabelAdd
}

func (t *tasksModel) openLabelRemoveForm() {
	tk := t.detail.task
	if tk == nil {
		return
	}
	validator := func(field, value string) error {
		if value == "" {
			return nil
		}
		if !labelSuffixRe.MatchString(value) {
			return fmt.Errorf("use <namespace>:<value> or <tag>")
		}
		return nil
	}
	fields := []formField{
		{Label: "name", Required: true, Hint: "<namespace>:<value> or <tag>", Validator: validator},
	}
	f := NewForm("Remove label  "+t.m.projectScope+":", fields)
	f.Title = "Remove label  " + t.m.projectScope + ":"
	t.m.form = f
	t.m.formKind = formTaskLabelRemove
}

func (t *tasksModel) requestRemoveTask() tea.Cmd {
	t.m.confirm = confirmRemoveTask
	t.m.confirmMsg = fmt.Sprintf("Remove task %s?", t.detail.id)
	t.m.confirmArg = "History is lost. Registry labels are unaffected."
	return nil
}

func (t *tasksModel) openCommentAddForm() {
	tk := t.detail.task
	if tk == nil {
		return
	}
	labelsValidator := func(field, value string) error {
		if value == "" {
			return nil
		}
		for _, tok := range strings.Fields(value) {
			if !labelSuffixRe.MatchString(tok) {
				return fmt.Errorf("bad label %q: use <namespace>:<value> or <tag>", tok)
			}
		}
		return nil
	}
	fields := []formField{
		{Label: "body", Required: true, Hint: "comment body (free-form prose)"},
		{Label: "labels", Required: false, Hint: "space-separated suffixes, e.g. 'comment:open-question' (prefix auto-added)", Validator: labelsValidator},
		{Label: "reply-to", Required: false, Hint: "optional comment id this replies to (same task)"},
	}
	f := NewForm("New comment  "+tk.ID+":", fields)
	f.Title = "New comment  " + tk.ID + ":"
	t.m.form = f
	t.m.formKind = formCommentAdd
}

func (m *Model) doTaskCreate(vals map[string]string) tea.Cmd {
	title := vals["title"]
	desc := vals["description"]
	var labels []string
	for _, tok := range strings.Fields(vals["labels"]) {
		labels = append(labels, m.projectScope+":"+tok)
	}
	tk, err := m.store.CreateTask(m.projectScope, title, desc, labels, m.actor)
	if err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	if tk != nil {
		m.tasks.openDetail(tk.ID)
	}
	return nil
}

func (m *Model) doTaskSetTitle(vals map[string]string) tea.Cmd {
	id := m.tasks.detail.id
	title := vals["title"]
	if err := m.store.SetTitle(id, title, m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	m.tasks.openDetail(id)
	return nil
}

func (m *Model) doTaskSetDescription(vals map[string]string) tea.Cmd {
	id := m.tasks.detail.id
	desc := vals["description"]
	if err := m.store.SetDescription(id, desc, m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	m.tasks.openDetail(id)
	return nil
}

func (m *Model) doTaskLabelAdd(vals map[string]string) tea.Cmd {
	id := m.tasks.detail.id
	suffix := vals["name"]
	full := m.projectScope + ":" + suffix
	if err := m.store.TaskLabelAdd(id, full, m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	m.tasks.openDetail(id)
	return nil
}

func (m *Model) doTaskLabelRemove(vals map[string]string) tea.Cmd {
	id := m.tasks.detail.id
	suffix := vals["name"]
	full := m.projectScope + ":" + suffix
	if err := m.store.TaskLabelRemove(id, full, m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	m.tasks.openDetail(id)
	return nil
}
