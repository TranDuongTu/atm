package tui

import (
	"strings"
	"testing"

	"atm/internal/store"
)

func TestRenderPersonaActivityChart(t *testing.T) {
	m := newTestModelWithActor(t, "staff@claude:opus-4.8")
	seedStaffPersona(t, m)
	if _, err := m.store.CreateProject("ATM", "Acme Task Manager", "staff@claude:opus-4.8"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.store.CreateTask("ATM", "task one", "", nil, "staff@claude:opus-4.8"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.store.CreateTask("ATM", "task two", "", nil, "staff@claude:opus-4.8"); err != nil {
		t.Fatal(err)
	}

	m.SetSize(80, 24)
	m.projectScope = "ATM"
	m.refreshAll()

	entries, err := m.store.ReadLog("ATM")
	if err != nil && !store.IsIntegrity(err) {
		t.Fatalf("ReadLog: %v", err)
	}
	lines := m.projects.renderPersonaActivityChart(entries, 8)
	view := strings.Join(lines, "\n")
	if !strings.Contains(view, "activity by persona") {
		t.Fatalf("chart title wrong:\n%s", view)
	}
	if !strings.Contains(view, "staff") {
		t.Fatalf("missing persona row 'staff':\n%s", view)
	}
}

func TestRenderPersonaActivityChartEmpty(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.SetSize(80, 24)
	m.projectScope = "ATM"
	m.refreshAll()
	entries, _ := m.store.ReadLog("ATM")
	lines := m.projects.renderPersonaActivityChart(entries, 1)
	view := strings.Join(lines, "\n")
	if !strings.Contains(view, "activity by persona") {
		t.Fatalf("degenerate title wrong:\n%s", view)
	}
}

func TestRenderUbiquitousLanguageChartEmptyState(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "bug one")
	m.SetSize(80, 24)
	m.projectScope = "ATM"
	m.refreshAll()
	got := m.projects.renderSummary(12)
	mustContain(t, got, "Ubiquitous Language")
	mustContain(t, got, "no vocabulary yet")
	mustNotContain(t, got, "events")
	mustNotContain(t, got, "(agents)")
}

func TestRenderUbiquitousLanguageChartShowsTerms(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "bug one")
	if err := m.store.WriteVocabulary("ATM", &store.Vocabulary{
		Actor: testActor,
		Terms: []store.VocabularyTerm{
			{Term: "labels", Weight: 9},
			{Term: "audit log", Weight: 7},
			{Term: "persona", Weight: 5},
		},
	}); err != nil {
		t.Fatal(err)
	}
	m.SetSize(80, 24)
	m.projectScope = "ATM"
	m.refreshAll()
	got := m.projects.renderSummary(14)
	mustContain(t, got, "Ubiquitous Language")
	mustContain(t, got, "labels")
	mustContain(t, got, "audit log")
	mustContain(t, got, "persona")
	mustNotContain(t, got, "no vocabulary yet")
}

func TestRenderUbiquitousLanguageChartSortsByWeightDescending(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "bug one")
	if err := m.store.WriteVocabulary("ATM", &store.Vocabulary{
		Actor: testActor,
		Terms: []store.VocabularyTerm{
			{Term: "alpha", Weight: 5},
			{Term: "beta", Weight: 9},
			{Term: "gamma", Weight: 7},
		},
	}); err != nil {
		t.Fatal(err)
	}
	m.SetSize(120, 24)
	m.projectScope = "ATM"
	m.refreshAll()
	got := m.projects.renderSummary(20)
	mustContain(t, got, "beta")
	mustContain(t, got, "alpha")
	if strings.Index(got, "beta") >= strings.Index(got, "alpha") {
		t.Fatalf("beta (weight 9) should appear before alpha (weight 5):\n%s", got)
	}
}
