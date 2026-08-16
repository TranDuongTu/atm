package tui

import (
	"fmt"
	"strings"

	"atm/internal/core"
	"github.com/muesli/reflow/wordwrap"
)

// taskPreviewDescLines caps how much of a task's description the preview
// shows. The preview is an identification aid — enough to be sure this is the
// task you meant — and the history under it is the part the deleted overlay
// existed for, so a long description must not push it off the pane.
const taskPreviewDescLines = 5

// taskPreviewLines is a hovered task row's preview: the ID and title, the
// task's label chips, the first lines of its description, then the task's
// audit history. The history is rendered by taskHistoryLines — the same
// renderer the deleted task-detail history overlay used, which is what makes
// this pane that overlay's replacement rather than a lookalike.
//
// The header and the history are unconditional: a task with neither
// description nor labels still previews as itself, never as the "(no
// preview)" empty pane.
func taskPreviewLines(m *Model, tk *core.Task, w int) []string {
	if tk == nil {
		return nil
	}
	// One line, cut with an ellipsis: the header is identity, and an identity
	// line that wraps into the labels below it reads as two facts, not one.
	out := []string{fitLineTail(tk.ID+"  "+tk.Title, w)}
	if chips := renderLabelChips(m.styles, tk.Labels, w); chips != "" {
		out = append(out, chips)
	}
	if desc := strings.TrimSpace(tk.Description); desc != "" {
		lines := strings.Split(wordwrap.String(desc, w), "\n")
		if len(lines) > taskPreviewDescLines {
			lines = append(lines[:taskPreviewDescLines:taskPreviewDescLines], "…")
		}
		out = append(out, "")
		out = append(out, lines...)
	}
	out = append(out, "")
	return append(out, taskHistoryLines(m, tk.ProjectCode, tk.ID, w)...)
}

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
//
// Package-level rather than built fresh per call: refreshPreview runs on
// every cursor move and (via SetSize) every resize while the spotlight is
// open, and this map's closures never vary across calls — rebuilding it each
// time was pure allocation for the same six entries.
var previewRegistry = map[string]previewFunc{
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
