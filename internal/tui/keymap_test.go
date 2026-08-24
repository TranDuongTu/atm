package tui

import (
	"fmt"
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

// The named (non-rune) keys keyMsgFromString special-cases must round-trip
// too. They are not all reachable from menuEntries — "tab" is the spotlight's
// own focus swap rather than any entry's replay key — so the table above
// cannot cover them, and a missing case would silently synthesize the literal
// runes "t","a","b" instead.
func TestKeyMsgFromStringNamedKeys(t *testing.T) {
	for _, k := range []string{"enter", "esc", "tab", "ctrl+right", "ctrl+left"} {
		if got := keyMsgFromString(k).String(); got != k {
			t.Errorf("keyMsgFromString(%q).String() = %q", k, got)
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
	}
}

// A Views entry may carry scopes even though the spotlight list never
// filters Views by them (openSpotlight's Views loop only reads e.section and
// needsProject) — a scope on a Views entry feeds preludeFor instead, so
// activation focuses the right pane before replaying the key (e.g. "C" needs
// the Tasks pane focused; see the Capabilities entry). Views availability is
// still gated by needsProject alone.
func TestViewsEntryScopesFeedPreludeNotFiltering(t *testing.T) {
	for _, e := range menuEntries {
		if e.section != sectionViews || len(e.scopes) == 0 {
			continue
		}
		if got := previewKeyFor(e); got != e.key+"|views" {
			t.Errorf("views entry %q with scopes must still key the preview registry as %q, got %q", e.label, e.key+"|views", got)
		}
	}
}

// Every non-hidden keyed entry must carry a summary (the preview fallback)
// and a kind (which decides what -> does). A missing summary would render an
// empty preview region; a missing kind would default kindAction and fire a
// dialog entry without recording the return.
func TestMenuEntriesCarrySummaryAndKind(t *testing.T) {
	for _, e := range menuEntries {
		if e.hidden || e.key == "" {
			continue
		}
		if strings.TrimSpace(e.summary) == "" {
			t.Errorf("entry %q (key %q) has no summary", e.label, e.key)
		}
		if e.kind != kindAction && e.kind != kindDialog {
			t.Errorf("entry %q (key %q) has kind %v, want kindAction or kindDialog", e.label, e.key, e.kind)
		}
	}
	for _, e := range menuEntries {
		if e.section == sectionReference && e.kind != kindReference {
			t.Errorf("reference entry %q must be kindReference", e.label)
		}
	}
}

// Every prelude segment must round-trip through bubbletea exactly like the
// entry keys do, and every action scope must have a non-empty prelude — an
// empty one would replay the entry's key into whatever pane happens to be
// focused, which is the contextual-menu bug this redesign removes.
func TestPreludesRoundTripAndCoverEveryScope(t *testing.T) {
	scopes := []menuScope{scopeProjectsList, scopeProjectsDetail, scopeTasksList, scopeTasksDetail}
	for _, s := range scopes {
		chain := preludeFor(s)
		if len(chain) == 0 {
			t.Errorf("scope %d has an empty prelude", s)
		}
		for _, seg := range chain {
			if got := keyMsgFromString(seg).String(); got != seg {
				t.Errorf("prelude segment %q round-trips to %q", seg, got)
			}
		}
	}
	if len(preludeFor(scopeGlobal)) != 0 {
		t.Error("global entries must have an empty prelude")
	}
}

// The pane-focus and activity-chart keys are advertised on the pane and
// chart borders already; they stay in the keymap reference but must never
// be spotlight rows.
func TestBorderHintedKeysAreHidden(t *testing.T) {
	for _, e := range menuEntries {
		switch e.key {
		case "1", "2", "ctrl+left/right", "ctrl+enter / ctrl+j / enter focused chart", "ctrl+up/down":
			if !e.hidden {
				t.Errorf("border-hinted key %q (%s) must be hidden", e.key, e.label)
			}
		}
	}
}

func TestActivityChartKeymapLabels(t *testing.T) {
	content := keymapReferenceText()
	for _, want := range []string{
		"ctrl+left/right",
		"Activity chart: prev/next persona",
		"ctrl+enter",
		"ctrl+j",
		"enter focused chart",
		"Activity chart: inline persona breakdown",
		"ctrl+up/down",
		"Activity chart: time range; scroll inline drill",
		"Open detail / confirm",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("keymap reference missing %q", want)
		}
	}
	for _, stale := range []string{"Drill into persona activity", "Back from persona detail", "Scroll persona chart", "persona activity overlay", "Dispatch this persona", "persona drill"} {
		if strings.Contains(content, stale) {
			t.Errorf("keymap reference contains stale text %q", stale)
		}
	}
}

// A global list renders every entry exactly once; the old table carried
// "Remove project" twice (once per projects scope).
func TestNoDuplicateVisibleEntries(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range menuEntries {
		if e.hidden || e.key == "" {
			continue
		}
		id := e.key + "|" + e.label
		if seen[id] {
			t.Errorf("duplicate visible entry %s", id)
		}
		seen[id] = true
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

// parityProbe sets up a Model in a menu entry's scope so that replaying the
// entry's key through Model.handleKey produces an observable effect, then
// asserts that effect. setup must reach a state where the entry applies; a
// probe whose setup cannot reach such a state (or whose key leaves the model
// unchanged) is a test failure, not a skip — that is how a phantom binding
// (a key the table advertises but no handler consumes) is caught.
type parityProbe struct {
	model func(t *testing.T) *Model // optional factory; defaults to newTestModel
	setup func(t *testing.T, m *Model)
	check func(t *testing.T, m *Model)
}

// probeID names the parity probe for an action entry by its key and primary
// scope. The same key means different things in different scopes (a/x/s/S in
// the projects and tasks panes, e/d in task detail vs. the boards ring), so
// the scope disambiguates.
func probeID(key string, scope menuScope) string {
	return fmt.Sprintf("%s|%d", key, scope)
}

// parityTasksScope selects the seeded project
// and focuses the Tasks pane, so the entry's key routes through
// tasksModel.handleListKey (the boards keys route to boardsModel from there).

// parityTasksSeed selects the seeded project without focusing the Tasks
// pane: "s" (not "2") is what runs EnsureVocabulary and boards.selectDefault,
// so the tasks/boards models are ready even though the pane isn't focused
// yet. This is the setup for probes whose own scope's prelude ends in "2" —
// the prelude does the focusing that the setup used to.
func parityTasksSeed(t *testing.T, m *Model) {
	t.Helper()
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")
}

// parityTasksScope seeds a project, selects it, and focuses the Tasks pane.
// It stays in use (rather than parityTasksSeed) wherever a probe's own setup
// goes on to replay pane-dependent keys of its own — a Views entry with no
// prelude at all ("C|views") — since that replay needs the Tasks pane focused
// before setup returns, not after the driver's prelude runs.
func parityTasksScope(t *testing.T, m *Model) {
	t.Helper()
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s") // select the project under the Projects cursor
	update(t, m, "2") // focus the Tasks pane
}

// parityTaskDetail seeds a task for the scopeTasksDetail probes (e/d/b/B/M/
// H/x): the {"2","enter"} prelude focuses the Tasks pane and opens it, so
// setup's job is done once the task exists to open.
func parityTaskDetail(t *testing.T, m *Model) {
	t.Helper()
	parityTasksSeed(t, m)
	seedTask(t, m, "ATM", "parity task")
}

// TestMenuEntriesConsumedByHandlers is the spec's parity test (decision 9):
// every keyed, menu-visible entry's replay sequence is consumed by the
// corresponding handler in its scope and produces the entry's advertised
// effect. It supplements TestMenuEntriesReplayRoundTrip, which only proves the
// key string round-trips through bubbletea — a phantom binding (like the
// historical [Ctrl+Shift+→]dispatch, which was advertised but never handled)
// round-trips fine but leaves the model unchanged, which this test rejects.
func TestMenuEntriesConsumedByHandlers(t *testing.T) {
	probes := map[string]parityProbe{
		// Views — global overlays and panes (no scope filtering).
		"D|views": {
			setup: func(t *testing.T, m *Model) {
				m.dispatcher = &fakeDispatcher{preview: "tmux · new window"}
				m.agentOptionsFn = testAgents
			},
			check: func(t *testing.T, m *Model) {
				if !m.dispatchDlg.active {
					t.Error("D must open the dispatch dialog")
				}
			},
		},
		"E|views": {
			check: func(t *testing.T, m *Model) {
				if !m.channelsOv.open {
					t.Error("E must open the channels overlay")
				}
			},
		},
		"V|views": {
			check: func(t *testing.T, m *Model) {
				if !m.personasOv.open {
					t.Error("V must open the personas overlay")
				}
			},
		},
		"C|views": {
			setup: func(t *testing.T, m *Model) {
				parityTasksScope(t, m)
			},
			check: func(t *testing.T, m *Model) {
				if !m.capability.open {
					t.Error("C in a project-scoped tasks pane must open the capabilities switcher")
				}
			},
		},
		"T|views": {
			setup: func(t *testing.T, m *Model) {
				m.themeName = themeGraphite
			},
			check: func(t *testing.T, m *Model) {
				if m.themeName == themeGraphite {
					t.Error("T must cycle the theme")
				}
			},
		},
		"W|views": {
			check: func(t *testing.T, m *Model) {
				if !m.setup.active {
					t.Error("W must open the setup & readiness wizard")
				}
			},
		},

		// Actions — projects list.
		probeID("a", scopeProjectsList): {
			check: func(t *testing.T, m *Model) {
				if m.form == nil || m.formKind != formProjectCreate {
					t.Errorf("a on the projects list must open the project-create form, formKind=%v", m.formKind)
				}
			},
		},
		probeID("s", scopeProjectsList): {
			setup: func(t *testing.T, m *Model) {
				seedProject(t, m, "ATM", "Acme")
			},
			check: func(t *testing.T, m *Model) {
				if m.projectScope != "ATM" {
					t.Errorf("s must select the highlighted project, projectScope=%q", m.projectScope)
				}
			},
		},
		probeID("x", scopeProjectsList): {
			setup: func(t *testing.T, m *Model) {
				seedProject(t, m, "ATM", "Acme")
			},
			check: func(t *testing.T, m *Model) {
				if m.confirm != confirmRemoveProject {
					t.Errorf("x on the projects list must open the remove-project confirm, confirm=%v", m.confirm)
				}
			},
		},

		// Actions — projects detail.
		probeID("n", scopeProjectsDetail): {
			setup: func(t *testing.T, m *Model) {
				seedProject(t, m, "ATM", "Acme")
			},
			check: func(t *testing.T, m *Model) {
				if m.form == nil || m.formKind != formProjectSetName {
					t.Errorf("n in project detail must open the set-name form, formKind=%v", m.formKind)
				}
			},
		},
		probeID("x", scopeProjectsDetail): {
			setup: func(t *testing.T, m *Model) {
				seedProject(t, m, "ATM", "Acme")
			},
			check: func(t *testing.T, m *Model) {
				if m.confirm != confirmRemoveProject {
					t.Errorf("x in project detail must open the remove-project confirm, confirm=%v", m.confirm)
				}
			},
		},

		// Actions — tasks list.
		probeID("a", scopeTasksList): {
			setup: parityTasksSeed,
			check: func(t *testing.T, m *Model) {
				if m.form == nil || m.formKind != formTaskCreate {
					t.Errorf("a on the tasks list must open the task-create form, formKind=%v", m.formKind)
				}
			},
		},

		// Actions — task detail.
		probeID("e", scopeTasksDetail): {
			setup: parityTaskDetail,
			check: func(t *testing.T, m *Model) {
				if m.form == nil || m.formKind != formTaskSetTitle {
					t.Errorf("e in task detail must open the title form, formKind=%v", m.formKind)
				}
			},
		},
		probeID("d", scopeTasksDetail): {
			setup: parityTaskDetail,
			check: func(t *testing.T, m *Model) {
				if m.form == nil || m.formKind != formTaskSetDescription {
					t.Errorf("d in task detail must open the description form, formKind=%v", m.formKind)
				}
			},
		},
		probeID("b", scopeTasksDetail): {
			setup: parityTaskDetail,
			check: func(t *testing.T, m *Model) {
				if m.form == nil || m.formKind != formTaskLabelAdd {
					t.Errorf("b in task detail must open the add-label form, formKind=%v", m.formKind)
				}
			},
		},
		probeID("B", scopeTasksDetail): {
			setup: parityTaskDetail,
			check: func(t *testing.T, m *Model) {
				if m.form == nil || m.formKind != formTaskLabelRemove {
					t.Errorf("B in task detail must open the remove-label form, formKind=%v", m.formKind)
				}
			},
		},
		probeID("M", scopeTasksDetail): {
			setup: parityTaskDetail,
			check: func(t *testing.T, m *Model) {
				if m.form == nil || m.formKind != formCommentAdd {
					t.Errorf("M in task detail must open the add-comment form, formKind=%v", m.formKind)
				}
			},
		},
		probeID("x", scopeTasksDetail): {
			setup: parityTaskDetail,
			check: func(t *testing.T, m *Model) {
				if m.confirm != confirmRemoveTask {
					t.Errorf("x in task detail must open the remove-task confirm, confirm=%v", m.confirm)
				}
			},
		},

		// Actions — lanes (routed from the tasks pane).
		probeID("s", scopeTasksList): {
			setup: parityTasksSeed,
			check: func(t *testing.T, m *Model) {
				if m.tasks.sortMode == sortUpdatedDesc {
					t.Errorf("s on the tasks list must advance the sort, sortMode=%v", m.tasks.sortMode)
				}
			},
		},
		probeID("S", scopeTasksList): {
			setup: parityTasksSeed,
			check: func(t *testing.T, m *Model) {
				if !strings.Contains(m.toastMsg, "ensured capability vocabulary in ATM") {
					t.Errorf("S on the tasks list must seed the capability vocabulary (toast), toast=%q", m.toastMsg)
				}
			},
		},
	}

	for _, e := range menuEntries {
		if e.key == "" || e.hidden { // reference rows and keymap-only navigation pairs are not menu entries
			continue
		}
		var id string
		if e.section == sectionViews {
			id = e.key + "|views"
		} else if len(e.scopes) > 0 {
			id = probeID(e.key, e.scopes[0])
		} else {
			t.Errorf("entry %q carries a key but no scope", e.label)
			continue
		}
		p, ok := probes[id]
		if !ok {
			// A keyed entry with no handler reachable in any scope is exactly
			// the phantom-binding bug this test exists to catch.
			t.Errorf("no parity probe for menu entry %q (key %q, id %s) — is it bound anywhere?", e.label, e.key, id)
			continue
		}
		e := e
		t.Run(id, func(t *testing.T) {
			m := newTestModel(t)
			if p.model != nil {
				m = p.model(t)
			}
			m.SetSize(120, 40)
			if p.setup != nil {
				p.setup(t, m)
			}
			// Replay exactly what the spotlight replays: the scope's prelude,
			// then the entry's key. A probe's setup now only seeds data — the
			// navigation half is the prelude's job, and a prelude that fails to
			// reach the scope surfaces here as the entry's key doing nothing.
			if len(e.scopes) > 0 {
				for _, seg := range preludeFor(e.scopes[0]) {
					m.handleKey(keyMsgFromString(seg))
				}
			}
			m.handleKey(keyMsgFromString(e.key))
			p.check(t, m)
		})
	}
}
