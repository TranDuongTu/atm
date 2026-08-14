package tui

import (
	"strings"
	"testing"
)

// Every keyed entry's replay string must round-trip through bubbletea: the
// KeyMsg we synthesize for it must .String() back to the same string the
// real key produces in handleKey. This is the no-phantom-bindings guard
// (the old status bar advertised [Ctrl+Shift+→]dispatch, which was never a
// real binding).
func TestMenuEntriesReplayRoundTrip(t *testing.T) {
	for _, e := range menuEntries {
		if e.key == "" || e.hidden { // hidden entries are display-only rows in the reference table
			continue
		}
		if got := keyMsgFromString(e.key).String(); got != e.key {
			t.Errorf("entry %q: keyMsgFromString(%q).String() = %q", e.label, e.key, got)
		}
	}
}

// Hidden entries never carry a section other than Actions-invisible
// documentation, and reference entries never carry a key.
func TestMenuEntriesShape(t *testing.T) {
	for _, e := range menuEntries {
		if e.ref != refNone && e.key != "" {
			t.Errorf("reference entry %q must not carry a key", e.label)
		}
		if e.section == sectionViews && len(e.scopes) != 0 {
			t.Errorf("views entry %q must not be scope-filtered (use needsProject)", e.label)
		}
	}
}

func TestKeymapReferenceTextCoversAllEntries(t *testing.T) {
	ref := keymapReferenceText()
	for _, e := range menuEntries {
		if e.key == "" {
			continue
		}
		if !strings.Contains(ref, e.key) {
			t.Errorf("keymap reference missing key %q (%s)", e.key, e.label)
		}
	}
}
