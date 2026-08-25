package tui

import (
	"strings"
	"testing"

	"atm/internal/capability"
	"atm/internal/capability/scrum"
	"atm/internal/core"
	"atm/internal/store"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"
)

// newTestStore opens a fresh temp-dir store for direct store-API tests that
// do not need a full TUI Model. Auto-initialized.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return s
}

// newTestModelWithCaps builds a full Model whose registry is assembled from
// the given capabilities, for tests that drive the key harness (forms, etc.)
// but need a synthetic capability (e.g. an undescribed namespace) registered.
func newTestModelWithCaps(t *testing.T, caps ...capability.Capability) *Model {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	m, err := NewModel(NewModelOpts{Service: s, Actor: testActor, Registry: capability.NewRegistry(caps...)})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	return m
}

// undescribedCap is a synthetic capability that exposes a single namespace
// descriptor ("flagme:*") with no description. It exists only to drive the
// NeedsDescription code path in buildBoardRows ("⚠ flag fires") and the
// flag-clears-after-edit path, restoring the coverage the Task 4 review
// flagged. Vocabulary and Exposed return the same single literal so the
// namespace is owned (not unmanaged) and surfaces at L0.
type undescribedCap struct {
	code string
}

func (u *undescribedCap) Name() string    { return "undescribed-test" }
func (u *undescribedCap) Summary() string { return "synthetic test capability" }
func (u *undescribedCap) Brief() string   { return "" }
func (u *undescribedCap) Guide() string   { return "" }
func (u *undescribedCap) Command(_ capability.Env) *cobra.Command {
	return &cobra.Command{Use: u.Name()}
}

func (u *undescribedCap) Annotate(core.Task) *capability.Cell { return nil }

func (u *undescribedCap) Vocabulary(code string) []core.Label {
	if code != u.code {
		return nil
	}
	return []core.Label{{Name: code + ":flagme:*"}}
}

func (u *undescribedCap) Exposed(code string) []core.Label {
	if code != u.code {
		return nil
	}
	return []core.Label{{Name: code + ":flagme:*"}}
}

func (u *undescribedCap) EnsureVocabulary(svc core.LabelService, code, actor string) ([]core.Label, error) {
	if code != u.code {
		return nil, nil
	}
	if err := svc.LabelSeed(code+":flagme:*", "", "", actor); err != nil {
		return nil, err
	}
	return nil, nil
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// TestBoardCountSumsMatchingTasks guards the boardCount fix: a board's
// FullName is never a wildcard, so GroupTasksErr's no-wildcard branch returns
// the matching tasks as the second return value. The count must equal the
// number of tasks matching its expression, not 0.
func TestBoardCountSumsMatchingTasks(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := scrum.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "claimed1", "ATM:scrum:task")
	seedTask(t, m, "ATM", "claimed2", "ATM:scrum:bug")
	seedTask(t, m, "ATM", "evicted1", "ATM:scrum-out:duplicate")

	// scrum-pipeline has Expr "scrum:* AND NOT scrum-out:*" -> 2 matches.
	count, broken := m.boardCount("ATM:scrum-pipeline")
	if count != 2 {
		t.Errorf("scrum-pipeline count = %d want 2 (matching tasks)", count)
	}
	if broken {
		t.Errorf("scrum-pipeline marked broken; its expression is valid")
	}
}

func TestFitLineResetsANSIWhenTruncatingSelectedRows(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	m := newTestModel(t)
	line := m.styles.RowCursor.Render(strings.Repeat("x", 80))

	got := fitLine(line, 20)

	if !strings.HasSuffix(got, "\x1b[0m") {
		t.Fatalf("truncated selected row does not reset ANSI styling: %q", got)
	}
}

// --- UX refinement follow-up tests (pin cap, description wrapping, hint removal) ---

// --- Task 4: capability-scoped ring tests ---

// --- Task 5: unmanaged mode (full-width label drill-down, unset-cursor) ---
