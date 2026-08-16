package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestRegistryGroupInvariants pins the structure the spotlight tree derives
// from: menuGroups is a small, fully-populated table, and every non-hidden
// menuEntries row files under groupNone (sectionViews, the inline root) or a
// registered group — never an orphan or unregistered id.
func TestRegistryGroupInvariants(t *testing.T) {
	seenGroupIDs := map[menuGroupID]bool{}
	for _, g := range menuGroups {
		if lipgloss.Width(g.icon) != 1 {
			t.Errorf("group %q icon %q has width %d, want 1", g.label, g.icon, lipgloss.Width(g.icon))
		}
		if seenGroupIDs[g.id] {
			t.Errorf("group id %v appears more than once in menuGroups", g.id)
		}
		seenGroupIDs[g.id] = true
	}

	memberCount := map[menuGroupID]int{}
	for _, e := range menuEntries {
		if e.hidden {
			continue
		}
		if e.section == sectionViews {
			if e.group != groupNone {
				t.Errorf("entry %q is sectionViews but group=%v, want groupNone", e.label, e.group)
			}
		} else if e.group == groupNone {
			t.Errorf("entry %q (section %v) has no group", e.label, e.section)
		} else if groupByID(e.group) == nil {
			t.Errorf("entry %q has unregistered group id %v", e.label, e.group)
		} else {
			memberCount[e.group]++
		}

		if e.icon == "" {
			continue // reference entries and Views entries may still be mid-assignment; icon presence is checked below per-entry where required
		}
		if lipgloss.Width(e.icon) != 1 {
			t.Errorf("entry %q icon %q has width %d, want 1", e.label, e.icon, lipgloss.Width(e.icon))
		}
	}

	// Every non-hidden entry (Views included) must carry an icon.
	for _, e := range menuEntries {
		if e.hidden {
			continue
		}
		if e.icon == "" {
			t.Errorf("entry %q has no icon", e.label)
		}
	}

	for _, g := range menuGroups {
		if memberCount[g.id] == 0 {
			t.Errorf("group %q (id %v) has no entries", g.label, g.id)
		}
	}
}
