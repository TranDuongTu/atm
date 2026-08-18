package tui

import (
	"strings"
	"testing"

	"atm/internal/activity"
	"atm/internal/core"
)

func TestPersonaActivityOverlayRendersSnapshotAndCloses(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	now := core.Now()
	entries := []core.LogEntry{
		{At: now, Actor: "developer@codex:gpt-5", Action: "task.created"},
		{At: now, Actor: "developer@codex:gpt-5", Action: "task.updated"},
		{At: now, Actor: "manager@claude:opus", Action: "task.created"},
	}

	m.personaAct.openFor("developer", chartRanges[0], entries)
	view := stripANSI(m.personaAct.renderOverlay())
	for _, want := range []string{
		personaIcon("developer") + " developer \u00b7 1w",
		"2 events",
		"models",
		"gpt-5",
		"agents",
		"codex",
		"actions",
		"task.created",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("overlay missing %q:\n%s", want, view)
		}
	}
	for _, section := range [][2]string{{"models", "agents"}, {"agents", "actions"}} {
		if strings.Index(view, section[0]) > strings.Index(view, section[1]) {
			t.Errorf("%s must precede %s:\n%s", section[0], section[1], view)
		}
	}

	update(t, m, "esc")
	if m.personaAct.open {
		t.Fatal("Esc must close the persona activity overlay")
	}
}

func TestPersonaActivityOverlayContainsNoDispatchAffordance(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.personaAct.openFor("", chartRanges[1], []core.LogEntry{
		{At: core.Now(), Actor: "developer@codex:gpt-5", Action: "task.created"},
	})

	if view := strings.ToLower(stripANSI(m.personaAct.renderOverlay())); strings.Contains(view, "dispatch") {
		t.Fatalf("persona activity overlay must be read-only, got:\n%s", view)
	}
}

func TestOpenPersonaActivityUsesAllAndCurrentRange(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.projects.chartRange = 1
	m.projects.chartPersona = ""
	m.projects.summaryEntries = []core.LogEntry{
		{At: core.Now(), Actor: "developer@codex:gpt-5", Action: "task.created"},
	}

	m.projects.openPersonaActivity()
	if !m.personaAct.open {
		t.Fatal("openPersonaActivity must open the persona activity overlay")
	}
	if m.personaAct.key != "" {
		t.Fatalf("overlay key = %q, want All", m.personaAct.key)
	}
	if got := m.personaAct.spec.key; got != "1m" {
		t.Fatalf("overlay range = %q, want 1m", got)
	}
}

func TestOpenPersonaActivityUsesCarouselFallbackForVanishedPersona(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.projects.chartPersona = "manager"
	m.projects.summaryEntries = []core.LogEntry{
		{At: core.Now(), Actor: "developer@codex:gpt-5", Action: "task.created"},
	}

	m.projects.openPersonaActivity()
	if got := m.personaAct.key; got != "" {
		t.Fatalf("overlay key = %q, want All after the chart falls back", got)
	}
}

func TestPersonaActivityOverlayClampsOverscrollAndKeepsFooter(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 16)
	m.personaAct = personaActivityModel{
		m:    m,
		open: true,
		key:  "developer",
		spec: chartRanges[0],
		group: activity.Group{
			Count:   3,
			Models:  map[string]int{"gpt-5": 1},
			Agents:  map[string]int{"codex": 1},
			Actions: map[string]int{"task.created": 1},
		},
	}

	for range 20 {
		m.personaAct.handleKey(keyMsg("j"))
	}
	bottom := stripANSI(m.personaAct.renderOverlay())
	if !strings.Contains(bottom, "[j/k]scroll  [g]top  [Esc]close") {
		t.Errorf("overlay footer is truncated:\n%s", bottom)
	}

	m.personaAct.handleKey(keyMsg("k"))
	if up := stripANSI(m.personaAct.renderOverlay()); up == bottom {
		t.Fatalf("up after overscroll must reveal an earlier window:\n%s", up)
	}
}

func TestPersonaActivityOverlayClampsStoredOffsetAfterResize(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 16)
	m.personaAct = personaActivityModel{
		m:    m,
		open: true,
		key:  "developer",
		spec: chartRanges[0],
		group: activity.Group{
			Count:   3,
			Models:  map[string]int{"gpt-5": 1},
			Agents:  map[string]int{"codex": 1},
			Actions: map[string]int{"task.created": 1},
		},
	}

	for range 20 {
		m.personaAct.handleKey(keyMsg("j"))
	}
	if m.personaAct.offset == 0 {
		t.Fatal("short overlay must scroll before the resize")
	}

	m.SetSize(120, 40)
	m.personaAct.handleKey(keyMsg("k"))
	if got := m.personaAct.offset; got != 0 {
		t.Fatalf("offset after resize and up = %d, want 0", got)
	}
}
