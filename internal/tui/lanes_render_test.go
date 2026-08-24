package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// laneCards splits a rendered strip into its three cards by column range, so
// a test can assert about one card without matching the whole row.
func laneCards(t *testing.T, strip string, w int) [3][]string {
	t.Helper()
	lines := strings.Split(strip, "\n")
	inboxW, pipelineW, _ := laneWidths(w)
	var out [3][]string
	for _, ln := range lines {
		plain := []rune(stripANSI(ln))
		cut := func(a, b int) string {
			if a > len(plain) {
				return ""
			}
			if b > len(plain) {
				b = len(plain)
			}
			return string(plain[a:b])
		}
		out[laneInbox] = append(out[laneInbox], cut(0, inboxW))
		out[lanePipeline] = append(out[lanePipeline], cut(inboxW, inboxW+pipelineW))
		out[laneOut] = append(out[laneOut], cut(inboxW+pipelineW, len(plain)))
	}
	return out
}

func TestLaneStripIsExactlyFiveLinesAtEveryWidth(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)
	m.lanes.selectDefault()

	for _, w := range []int{80, 120} {
		strip := m.lanes.render(w)
		if got := len(strings.Split(strip, "\n")); got != laneStripHeight {
			t.Fatalf("width %d: strip has %d lines, want %d", w, got, laneStripHeight)
		}
		for i, ln := range strings.Split(strip, "\n") {
			if got := lipgloss.Width(ln); got != w {
				t.Fatalf("width %d: line %d is %d columns wide, want %d", w, i, got, w)
			}
		}
	}
}

func TestLaneStripNamesEveryLaneInFixedOrder(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)
	m.lanes.selectDefault()

	cards := laneCards(t, m.lanes.render(80), 80)
	for i, want := range []string{"Inbox", "Pipeline", "Out"} {
		if !strings.Contains(strings.Join(cards[i], "\n"), want) {
			t.Fatalf("card %d does not name %q:\n%s", i, want, strings.Join(cards[i], "\n"))
		}
	}
}

func TestLaneStripEmphasizesTheSelectedCard(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)
	m.lanes.selectDefault()

	cards := laneCards(t, m.lanes.render(80), 80)
	if !strings.Contains(cards[lanePipeline][0], "╔") {
		t.Fatalf("selected Pipeline card lacks the emphasized border:\n%s", cards[lanePipeline][0])
	}
	if strings.Contains(cards[laneInbox][0], "╔") || strings.Contains(cards[laneOut][0], "╔") {
		t.Fatalf("an unselected card carries the emphasized border:\n%s", strings.Join(cards[laneInbox], "\n"))
	}

	m.lanes.move(-1)
	cards = laneCards(t, m.lanes.render(80), 80)
	if !strings.Contains(cards[laneInbox][0], "╔") {
		t.Fatalf("after move(-1) the Inbox card is not emphasized:\n%s", cards[laneInbox][0])
	}
	if strings.Contains(cards[lanePipeline][0], "╔") {
		t.Fatalf("Pipeline stayed emphasized after the selection moved away")
	}
}

func TestLaneStripInboxSignalOnlyWhenInboxHasWork(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)
	m.lanes.selectDefault()

	cards := laneCards(t, m.lanes.render(80), 80)
	if strings.Contains(strings.Join(cards[laneInbox], "\n"), "●") {
		t.Fatalf("empty Inbox shows the attention signal:\n%s", strings.Join(cards[laneInbox], "\n"))
	}

	seedTask(t, m, "ATM", "undecided work")
	m.refreshAll()
	cards = laneCards(t, m.lanes.render(80), 80)
	inbox := strings.Join(cards[laneInbox], "\n")
	if !strings.Contains(inbox, "●") {
		t.Fatalf("Inbox with 1 task lacks the attention signal:\n%s", inbox)
	}
	if !strings.Contains(inbox, "1") {
		t.Fatalf("Inbox does not show its count:\n%s", inbox)
	}
	// The signal is the Inbox's alone — it means "something is waiting on you".
	if strings.Contains(strings.Join(cards[lanePipeline], "\n"), "●") {
		t.Fatalf("Pipeline shows the Inbox attention signal")
	}
}

func TestLaneStripBrokenLaneShowsSeedHint(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, false) // lane boards never seeded
	m.lanes.selectDefault()

	cards := laneCards(t, m.lanes.render(80), 80)
	body := strings.Join(cards[lanePipeline], "\n")
	if !strings.Contains(body, "?") {
		t.Fatalf("broken lane does not render its count as ?:\n%s", body)
	}
	if !strings.Contains(body, "seed") {
		t.Fatalf("broken lane does not hint at seeding:\n%s", body)
	}
}

func TestLaneStripWithoutAProjectStillHoldsItsHeight(t *testing.T) {
	m := newLanesTestModel(t)
	m.projectScope = ""
	m.refreshAll()

	strip := m.lanes.render(80)
	if got := len(strings.Split(strip, "\n")); got != laneStripHeight {
		t.Fatalf("unscoped strip has %d lines, want %d", got, laneStripHeight)
	}
}
