package tui

import (
	"fmt"
	"regexp"

	"atm/internal/core"

	tea "github.com/charmbracelet/bubbletea"
)

func pluralTasks(n int) string {
	if n == 1 {
		return "task"
	}
	return "tasks"
}

// labelSuffixRe validates the suffix the user types in the label add/remove
// forms. The fixed "<CODE>:" prefix is prepended by the form submit handler,
// so the suffix is "<namespace>:<value>" or "<tag>" with NO leading colon.
var labelSuffixRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*(:[a-z0-9][a-z0-9-]*)?$`)

// openLabelAddForm opens the add-label form bound to the given project code.
// Used by the Labels pane.
func (m *Model) openLabelAddForm(code string) {
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
		{Label: "name", Required: true, Hint: "<namespace>:<value> or <tag>, e.g. status:open", Validator: validator},
	}
	f := NewForm(fmt.Sprintf("Add label  %s:", code), fields)
	f.Title = fmt.Sprintf("Add label  %s:", code)
	f.SetWidth(FormWidth(m.width))
	m.form = f
	m.formKind = formLabelAdd
	m.formPayload = code
}

// openLabelRemoveForm opens the remove-label form bound to the given project code.
func (m *Model) openLabelRemoveForm(code string) {
	m.openLabelRemoveFormFor(code, "")
}

// openLabelRemoveFormFor opens the remove-label form with a known suffix.
// Used when the Labels pane has a real label under the cursor.
func (m *Model) openLabelRemoveFormFor(code, suffix string) {
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
		{Label: "name", Required: true, Value: suffix, Hint: "<namespace>:<value> or <tag>", Validator: validator},
	}
	f := NewForm(fmt.Sprintf("Remove label  %s:", code), fields)
	f.Title = fmt.Sprintf("Remove label  %s:", code)
	f.SetWidth(FormWidth(m.width))
	m.form = f
	m.formKind = formLabelRemove
	m.formPayload = code
}

// doLabelAdd handles submit of the add-label form.
func (m *Model) doLabelAdd(vals map[string]string) tea.Cmd {
	code := m.formPayload
	suffix := vals["name"]
	full := code + ":" + suffix
	if err := m.store.LabelAdd(full, "", "", m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	return nil
}

// doLabelRemove handles submit of the remove-label form.
func (m *Model) doLabelRemove(vals map[string]string) tea.Cmd {
	code := m.formPayload
	suffix := vals["name"]
	full := code + ":" + suffix
	res, err := m.store.LabelRemove(full, m.actor)
	if err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.showToast(fmt.Sprintf("removed label %s (retained usage: %d)", full, res.RetainedUsage))
	m.refreshAll()
	return nil
}

// boardCount returns the task count for a board (label with an Expr) and
// whether the expression is broken (invalid or cyclic). Uses GroupTasksErr
// via a single-label query because ListTasks swallows expression errors
// and would conflate a broken board with an empty one. It lives on the Model
// rather than on a pane because both the board ring and the lane strip count
// boards the same way.
func (m *Model) boardCount(full string) (int, bool) {
	_, others, err := m.store.GroupTasksErr(core.QueryFilters{
		Project: m.projectScope,
		Labels:  []string{full},
	})
	if err != nil {
		return 0, true
	}
	return len(others), false
}

// seedDefaults ensures every enabled capability's vocabulary exists for the
// scoped project. It converges labels, not boards — which is why it outlives
// the boards pane it used to hang off.
func (m *Model) seedDefaults() tea.Cmd {
	boards, err := m.regFor(m.projectScope).EnsureVocabulary(m.store, m.projectScope, m.actor)
	if err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.showToast(fmt.Sprintf("ensured capability vocabulary in %s (%d labels)", m.projectScope, len(boards)))
	m.refreshAll()
	return nil
}

// --- describe form (used by [d] in list and detail) ---

// openLabelDescribeForm opens a form with name + description fields. The
// user types the label suffix and a new description; submit calls LabelAdd
// (the upsert that overwrites the description).
func (m *Model) openLabelDescribeForm() {
	f := m.newLabelDescribeForm("", "")
	m.form = f
	m.formKind = formLabelDescribe
	m.formPayload = m.projectScope
}

// openLabelDescribeFormFor opens the describe form pre-filled with a known
// suffix and its current description (used from the label detail view).
func (m *Model) openLabelDescribeFormFor(suffix, currentDesc string) {
	f := m.newLabelDescribeForm(suffix, currentDesc)
	m.form = f
	m.formKind = formLabelDescribe
	m.formPayload = m.projectScope
}

func (m *Model) newLabelDescribeForm(suffix, desc string) *Form {
	nameValidator := func(field, value string) error {
		if value == "" {
			return nil
		}
		if !labelSuffixRe.MatchString(value) {
			return fmt.Errorf("use <namespace>:<value> or <tag>")
		}
		return nil
	}
	fields := []formField{
		{Label: "name", Required: true, Value: suffix, Hint: "<namespace>:<value> or <tag>", Validator: nameValidator},
		{Label: "description", Required: false, Value: desc, Hint: "new description (overwrites)"},
	}
	f := NewForm(fmt.Sprintf("Describe label  %s:", m.projectScope), fields)
	f.Title = fmt.Sprintf("Describe label  %s:", m.projectScope)
	f.SetWidth(FormWidth(m.width))
	return f
}

// doLabelDescribe handles submit of the describe-label form.
func (m *Model) doLabelDescribe(vals map[string]string) tea.Cmd {
	code := m.formPayload
	suffix := vals["name"]
	full := code + ":" + suffix
	desc := vals["description"]
	if err := m.store.LabelAdd(full, desc, "", m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	return nil
}

// --- namespace descriptor form (used by [e] on a namespace row) ---

// openNamespaceDescribeForm opens a description-only form for a namespace
// descriptor label (<code>:<ns>:*). The namespace name is fixed (shown as a
// read-only hint); only the description is editable. Submit upserts the
// descriptor via LabelAdd, which creates it if absent or overwrites the
// description if present. This is the curation path for the ⚠-flagged
// undescribed namespaces conventions rule 6 asks a human to reconcile.
func (m *Model) openNamespaceDescribeForm(code, ns, currentDesc string) {
	fields := []formField{
		{Label: "namespace", Required: true, Value: ns, Hint: "fixed — edit description below"},
		{Label: "description", Required: false, Value: currentDesc, Hint: "what this namespace means (overwrites)"},
	}
	f := NewForm(fmt.Sprintf("Describe namespace  %s:%s:*", code, ns), fields)
	f.Title = fmt.Sprintf("Describe namespace  %s:%s:*", code, ns)
	f.SetWidth(FormWidth(m.width))
	m.form = f
	m.formKind = formNamespaceDescribe
	// Stash the code + ns so the submit handler can rebuild the descriptor
	// name; the form's own "namespace" field is read-only display.
	m.formPayload = code + ":" + ns
}

// doNamespaceDescribe handles submit of the namespace-describe form. It
// upserts the <code>:<ns>:* descriptor with the typed description.
func (m *Model) doNamespaceDescribe(vals map[string]string) tea.Cmd {
	payload := m.formPayload
	desc := vals["description"]
	full := payload + ":*"
	if err := m.store.LabelAdd(full, desc, "", m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.showToast(fmt.Sprintf("saved descriptor %s", full))
	m.refreshAll()
	return nil
}
