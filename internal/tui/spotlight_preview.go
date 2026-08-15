package tui

import "fmt"

// previewFunc renders an entry's live preview at the region's dimensions.
type previewFunc func(m *Model, w, h int) string

// previewKeyFor identifies an entry for the registry the same way the parity
// test identifies it: key plus primary scope, because the same key means
// different things in different scopes.
func previewKeyFor(e menuEntry) string {
	if e.section == sectionViews {
		return e.key + "|views"
	}
	if len(e.scopes) > 0 {
		return fmt.Sprintf("%s|%d", e.key, e.scopes[0])
	}
	return ""
}

// previewRegistry maps an entry to a live renderer. An entry with no entry
// here previews its summary line instead — that is the designed fallback, not
// a gap to be filled for every action.
//
// Each renderer loads (never opens) the state its overlay's previewBody
// needs. personasModel and dispatchModel are only populated once their
// overlay has actually been opened, so their entries call the model's
// load-only half here — capabilityModel is kept fresh by refreshAll on every
// launch/mutation, so its entry previews what it already holds.
func previewRegistry() map[string]previewFunc {
	return map[string]previewFunc{
		"E|views": func(m *Model, w, h int) string {
			m.channelsOv.loadFor(m.overlayProject()) // populates entries; does not open
			return m.channelsOv.previewBody(w)
		},
		"V|views": func(m *Model, w, h int) string {
			m.personasOv.loadFor() // populates entries; does not open
			return m.personasOv.previewBody(w)
		},
		"C|views": func(m *Model, w, h int) string { return m.capability.previewBody(w) },
		"D|views": func(m *Model, w, h int) string {
			persona, project, taskID, taskTitle := m.dispatchDefaults()
			m.dispatchDlg.loadFor(persona, project, taskID, taskTitle, "") // populates fields; does not activate
			return m.dispatchDlg.previewBody(w)
		},
		fmt.Sprintf("a|%d", scopeProjectsList): func(m *Model, w, h int) string {
			return newProjectCreateForm(w).View(m.styles)
		},
		fmt.Sprintf("a|%d", scopeTasksList): func(m *Model, w, h int) string {
			return m.tasks.newTaskCreateForm(w).View(m.styles)
		},
	}
}
