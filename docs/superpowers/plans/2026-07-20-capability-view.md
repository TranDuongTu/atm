# Capability View Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make capability the first-class navigation context of the TUI Tasks pane: one current capability at a time, a [C] switcher overlay, per-capability board rings, and `unmanaged` as a selectable capability with a labels-only drill-down.

**Architecture:** A new `capabilityModel` (`internal/tui/capabilities.go`) owns current-capability state, resolution, persistence (`config.json` → `boards.capability`), and the [C] modal. `boardsModel` scopes its ring to the current capability's `Exposed` labels and repurposes the umbrella sub-table as unmanaged mode's only surface. Ownership-based task counts come from a new pure `Registry.OwnedLabels` + shared `LabelSet` matcher in `internal/capability`.

**Tech Stack:** Go, Bubble Tea (`github.com/charmbracelet/bubbletea`), Lipgloss. Tests are plain `go test` with the existing helpers in `internal/tui/*_test.go` (`newTestModel`, `seedProject`, `seedTask`).

**Spec:** `docs/superpowers/specs/2026-07-20-capability-view-design.md` (approved). Tracking task: ATM-90171b.

## Global Constraints

- The pseudo-capability name is exactly `unmanaged`; header shows `CAPABILITY: <name>    TOTAL: <cap>/<project> tasks    SORT: <mode>`.
- Under `unmanaged`, NO unfiltered task query may ever run: tasks render only after a label row is selected (cursor starts unset at `-1`).
- Ring widths: 1 board → full pane width; 2 boards → 70% selected / 30% next (no prev cell); 3+ → existing 25/50/25.
- `C` opens the capabilities switcher only when pane [2] is focused AND a project is scoped; everywhere else it keeps opening the conventions overlay.
- Persistence writes happen ONLY on explicit switch (never write-back on read/resolution); persist failure keeps the in-memory switch and shows a toast.
- Pins are global across capabilities (max 3 = `core.MaxBoardPins`); a pin jump switches capability first; the pinned-tabs box renders at the very bottom of pane [2].
- Tasks table drops the LABELS column everywhere in pane [2] list views: `ID  TITLE  UPDATED`.
- No new store events, no CLI changes, no changes to `Exposed`/`Vocabulary` contracts.
- Commit after every task; run `gofmt -l internal/` before each commit (must print nothing).
- Commit messages end with:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`
  `Claude-Session: https://claude.ai/code/session_016VYHnumt4sx773LuSHYoeq`

## File Structure

| File | Change |
|---|---|
| `internal/core/config.go` | `BoardsConfig` gains `Capability string` |
| `internal/store/config_test.go` (or nearest store test file) | round-trip test |
| `internal/capability/capability.go` | new `LabelSet`, `NewLabelSet`, `Registry.OwnedLabels`; `Unmanaged` refactored onto `LabelSet` |
| `internal/capability/capability_test.go` | tests for the above |
| `internal/tui/capabilities.go` | NEW — `capabilityModel`: state, resolution, switch, [C] overlay |
| `internal/tui/capabilities_test.go` | NEW — tests |
| `internal/tui/app.go` | `Model.capability` field + wiring, `C` routing, overlay render, `countTasksCarrying`/`capabilityTaskCount` |
| `internal/tui/labels.go` | ring scoping, umbrella-row removal, unmanaged mode, cursor-unset selection, pins changes |
| `internal/tui/thumbnails.go` | `splitStripWidths(paneW, ringN)`, strip cases, unmanaged full-width surface |
| `internal/tui/tasks.go` | count fields, refresh order note |
| `internal/tui/tasks_list.go` | header, LABELS column removal, hint variants, pane stacking |
| `internal/tui/projects.go` | project-select handler resets capability state |
| `internal/tui/keymap.go` | `C` row split |
| existing `internal/tui/*_test.go` | updates per task |

Task order matters: 1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9 → 10. Each task leaves `go build ./...` and `go test ./...` green.

---

### Task 1: `BoardsConfig.Capability` field

**Files:**
- Modify: `internal/core/config.go:21-25`
- Test: `internal/store/config_capability_test.go` (new file in package `store`)

**Interfaces:**
- Produces: `core.BoardsConfig.Capability string` (JSON `capability,omitempty`), persisted/read through the existing `SetProjectBoards`/`GetBoardsConfig` (no signature changes).

- [ ] **Step 1: Write the failing test**

Create `internal/store/config_capability_test.go`:

```go
package store

import (
	"testing"

	"atm/internal/core"
)

// TestBoardsConfigCapabilityRoundTrip pins the capability-view persistence
// contract: boards.capability survives a SetProjectBoards/GetBoardsConfig
// round trip, and a boards config carrying ONLY Capability still reads back
// non-nil (the GetProjectConfig emptiness check already treats Boards != nil
// as present).
func TestBoardsConfigCapabilityRoundTrip(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.CreateProject("ATM", "Acme", "tester@t:1"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := s.SetProjectBoards("ATM", &core.BoardsConfig{Capability: "workflow"}, "tester@t:1"); err != nil {
		t.Fatalf("SetProjectBoards: %v", err)
	}
	got, err := s.GetBoardsConfig("ATM")
	if err != nil {
		t.Fatalf("GetBoardsConfig: %v", err)
	}
	if got == nil || got.Capability != "workflow" {
		t.Fatalf("Capability = %+v, want workflow", got)
	}
	cfg, err := s.GetProjectConfig("ATM")
	if err != nil || cfg == nil || cfg.Boards == nil {
		t.Fatalf("GetProjectConfig = %+v, %v; want non-nil with Boards", cfg, err)
	}
}
```

Note: if `store.Open` / `CreateProject` signatures differ (check `internal/store` — `newTestStore` in `internal/tui/labels_test.go:625` shows the canonical open-and-seed sequence), mirror what that helper does.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestBoardsConfigCapabilityRoundTrip -v`
Expected: FAIL — `got.Capability undefined (type *core.BoardsConfig has no field or method Capability)` (compile error).

- [ ] **Step 3: Add the field**

In `internal/core/config.go`, replace the `BoardsConfig` struct:

```go
type BoardsConfig struct {
	Order  []string `json:"order,omitempty"`  // ring order override (partial, FullName list)
	Hidden []string `json:"hidden,omitempty"` // hidden FullNames
	Pins   []string `json:"pins,omitempty"`   // pin-slot FullNames (max MaxBoardPins)
	// Capability is the current capability-view selection ("workflow",
	// "unmanaged", ...). Written only on an explicit switch in the TUI;
	// readers fall back silently when it names nothing enabled.
	Capability string `json:"capability,omitempty"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ ./internal/core/ -v -run TestBoardsConfig`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/core/config.go internal/store/config_capability_test.go
git commit -m "feat(ATM-90171b): BoardsConfig gains persisted capability-view selection"
```

---

### Task 2: `capability.LabelSet` + `Registry.OwnedLabels`

**Files:**
- Modify: `internal/capability/capability.go` (append after `OrderFullNames`, refactor `Unmanaged` at lines 233-264)
- Test: `internal/capability/capability_test.go` (append)

**Interfaces:**
- Produces: `capability.LabelSet` with `NewLabelSet(labels []core.Label) LabelSet` and `(LabelSet).Contains(fullName string) bool`; `(*Registry).OwnedLabels(code, capName string) []core.Label`.
- `Unmanaged` behavior is UNCHANGED — only its internals move onto `LabelSet`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/capability/capability_test.go` (match the file's existing fake-capability helpers; if it builds fakes differently, adapt the construction, not the assertions):

```go
func TestOwnedLabelsReturnsNamedCapabilityVocabulary(t *testing.T) {
	reg := NewRegistry(workflow.New(), contextmap.New())
	got := reg.OwnedLabels("ATM", "workflow")
	if len(got) != 13 {
		t.Fatalf("workflow OwnedLabels len = %d, want 13", len(got))
	}
	if reg.OwnedLabels("ATM", "nope") != nil {
		t.Fatalf("unknown capability should return nil")
	}
	var nilReg *Registry
	if nilReg.OwnedLabels("ATM", "workflow") != nil {
		t.Fatalf("nil registry should return nil")
	}
}

func TestLabelSetContains(t *testing.T) {
	s := NewLabelSet([]core.Label{
		{Name: "ATM:status:*"},
		{Name: "ATM:all-tasks", Expr: "*"},
		{Name: "ATM:needs-triage"},
	})
	for name, want := range map[string]bool{
		"ATM:all-tasks":      true,  // exact board
		"ATM:needs-triage":   true,  // exact tag
		"ATM:status:*":       true,  // exact descriptor
		"ATM:status:wip":     true,  // member of owned namespace
		"ATM:priority:high":  false, // unowned namespace member
		"ATM:other-tag":      false, // unowned tag
	} {
		if got := s.Contains(name); got != want {
			t.Errorf("Contains(%q) = %v, want %v", name, got, want)
		}
	}
}

// fakeLabelList satisfies the one LabelService method Unmanaged reads; the
// rest panic (Unmanaged must stay a pure LabelList subtraction). If this
// file already has a LabelService fake, reuse it instead.
type fakeLabelList struct {
	core.LabelService
	labels []core.Label
}

func (f fakeLabelList) LabelList(project, namespace string) []core.Label { return f.labels }

// TestUnmanagedMatchesOwnedLabelsSubtraction pins the single-sourcing
// property: LabelList minus the union of every capability's OwnedLabels
// (via LabelSet) equals Unmanaged, label for label.
func TestUnmanagedMatchesOwnedLabelsSubtraction(t *testing.T) {
	reg := NewRegistry(workflow.New(), contextmap.New())
	svc := fakeLabelList{labels: append(
		append(workflow.Vocabulary("ATM"), contextmap.Vocabulary("ATM")...),
		core.Label{Name: "ATM:type:bug"},      // unmanaged namespace member
		core.Label{Name: "ATM:needs-triage"},  // unmanaged loose tag
		core.Label{Name: "ATM:status:wip"},    // ad-hoc member of an OWNED namespace -> managed
	)}
	un, err := reg.Unmanaged(svc, "ATM")
	if err != nil {
		t.Fatalf("Unmanaged: %v", err)
	}
	inUn := map[string]bool{}
	for _, l := range un {
		inUn[l.Name] = true
	}
	var vocab []core.Label
	for _, name := range reg.Names() {
		vocab = append(vocab, reg.OwnedLabels("ATM", name)...)
	}
	owned := NewLabelSet(vocab)
	for _, l := range svc.LabelList("ATM", "") {
		if owned.Contains(l.Name) == inUn[l.Name] {
			t.Errorf("%s: owned=%v but in-unmanaged=%v — the two surfaces disagree", l.Name, owned.Contains(l.Name), inUn[l.Name])
		}
	}
	if !inUn["ATM:type:bug"] || !inUn["ATM:needs-triage"] || inUn["ATM:status:wip"] {
		t.Errorf("spot checks failed: unmanaged = %v", inUn)
	}
}
```

Imports for `workflow`/`contextmap` may be disallowed in this package (check for an import cycle — `capability_test.go` may be package `capability_test`). If the existing test file is external (`package capability_test`), keep these tests there and use the exported API as shown; if internal and the imports cycle, move the two registry-level tests to `internal/capability/registry_ext_test.go` with `package capability_test`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/capability/... -run 'TestOwnedLabels|TestLabelSet|TestUnmanagedMatches' -v`
Expected: FAIL — `undefined: NewLabelSet`, `reg.OwnedLabels undefined`.

- [ ] **Step 3: Implement**

Append to `internal/capability/capability.go`:

```go
// LabelSet is an ownership matcher over a label list: exact FullNames plus
// member prefixes derived from namespace descriptors (<code>:<ns>:* owns
// every <code>:<ns>:<value>). Registry.Unmanaged and the TUI's capability
// task counts share it, so the ownership rule stays single-sourced.
type LabelSet struct {
	exact    map[string]bool
	prefixes []string
}

// NewLabelSet indexes labels for Contains lookups.
func NewLabelSet(labels []core.Label) LabelSet {
	s := LabelSet{exact: make(map[string]bool, len(labels))}
	for _, l := range labels {
		s.exact[l.Name] = true
		if core.IsNamespaceName(l.Name) {
			// "<code>:<ns>:*" -> member prefix "<code>:<ns>:"
			s.prefixes = append(s.prefixes, strings.TrimSuffix(l.Name, "*"))
		}
	}
	return s
}

// Contains reports whether fullName is owned by the set: an exact member, or
// a member of an owned namespace descriptor.
func (s LabelSet) Contains(fullName string) bool {
	if s.exact[fullName] {
		return true
	}
	for _, p := range s.prefixes {
		if strings.HasPrefix(fullName, p) {
			return true
		}
	}
	return false
}

// OwnedLabels returns the named registered capability's vocabulary for code,
// or nil when the name is not registered. Pure read, no store side effect.
// The TUI's capability-view header counts tasks against NewLabelSet of this.
func (r *Registry) OwnedLabels(code, capName string) []core.Label {
	if r == nil {
		return nil
	}
	for _, c := range r.caps {
		if c.Name() == capName {
			return c.Vocabulary(code)
		}
	}
	return nil
}
```

Replace the body of `Unmanaged` (keep its doc comment, drop the now-stale "TUI renders these under the synthetic umbrella row" sentence — the umbrella row is gone after Task 4; reword to "the TUI renders these in the unmanaged capability view"):

```go
func (r *Registry) Unmanaged(svc core.LabelService, code string) ([]core.Label, error) {
	var vocab []core.Label
	if r != nil {
		for _, c := range r.caps {
			vocab = append(vocab, c.Vocabulary(code)...)
		}
	}
	owned := NewLabelSet(vocab)
	var out []core.Label
	for _, l := range svc.LabelList(code, "") {
		if !owned.Contains(l.Name) {
			out = append(out, l)
		}
	}
	return out, nil
}
```

- [ ] **Step 4: Run the package tests**

Run: `go test ./internal/capability/... -v`
Expected: PASS (including all pre-existing `Unmanaged` tests — the refactor must not change behavior).

- [ ] **Step 5: Commit**

```bash
git add internal/capability/
git commit -m "feat(ATM-90171b): Registry.OwnedLabels + shared LabelSet ownership matcher"
```

---

### Task 3: `capabilityModel` — state, resolution, switch, persistence

**Files:**
- Create: `internal/tui/capabilities.go`
- Modify: `internal/tui/app.go` (Model field at :66-133, `NewModel` at :159-177, `refreshAll` at :329-335, append count helpers)
- Modify: `internal/tui/projects.go:242-244` (project-select reset)
- Test: `internal/tui/capabilities_test.go` (new)

**Interfaces:**
- Produces (consumed by Tasks 4-9):
  - `Model.capability capabilityModel` field
  - `const unmanagedCapability = "unmanaged"`
  - `(*capabilityModel).refresh()` — rebuilds entries + resolves `current`; MUST run before `boards.refresh()` in `refreshAll`
  - `(*capabilityModel).current string` — resolved current capability name, `""` when no project scoped
  - `(*capabilityModel).unmanagedCurrent() bool`
  - `(*capabilityModel).switchTo(name string)` — full switch flow
  - `(*Model).countTasksCarrying(scope string, set capability.LabelSet) int`
  - `(*Model).capabilityTaskCount(capName string) int`
- Overlay fields (`open`, `cursor`, `entries`) exist from this task but the overlay UI itself is Task 8.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/capabilities_test.go`:

```go
package tui

import (
	"testing"

	"atm/internal/core"
)

// setupCapProject seeds a project with workflow+contextmap vocabularies and
// one unmanaged label, and points the model at it. Mirrors the seeding
// helpers in labels_test.go.
func setupCapProject(t *testing.T, m *Model) {
	t.Helper()
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := m.regFor("ATM").EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	seedTask(t, m, "ATM", "stray", "ATM:needs-triage")
	m.refreshAll()
}

func TestCapabilityResolutionDefaultsToFirstEnabled(t *testing.T) {
	m := newTestModel(t)
	setupCapProject(t, m)
	// newTestModel registers workflow then contextmap (check newTestModel in
	// app_test.go:29 — if the order differs, assert that first name instead).
	if m.capability.current != "workflow" {
		t.Fatalf("current = %q, want workflow (first enabled)", m.capability.current)
	}
}

func TestCapabilityResolutionUsesPersistedValue(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if err := m.store.SetProjectBoards("ATM", &core.BoardsConfig{Capability: "contextmap"}, m.actor); err != nil {
		t.Fatalf("SetProjectBoards: %v", err)
	}
	m.projectScope = "ATM"
	m.refreshAll()
	if m.capability.current != "contextmap" {
		t.Fatalf("current = %q, want persisted contextmap", m.capability.current)
	}
}

func TestCapabilityResolutionFallsBackWhenPersistedInvalid(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if err := m.store.SetProjectBoards("ATM", &core.BoardsConfig{Capability: "ghost"}, m.actor); err != nil {
		t.Fatalf("SetProjectBoards: %v", err)
	}
	m.projectScope = "ATM"
	m.refreshAll()
	if m.capability.current != "workflow" {
		t.Fatalf("current = %q, want workflow fallback", m.capability.current)
	}
	// Resolution must NOT write back: persisted value stays "ghost".
	cfg, _ := m.store.GetBoardsConfig("ATM")
	if cfg.Capability != "ghost" {
		t.Fatalf("persisted = %q; resolution must not write back", cfg.Capability)
	}
}

func TestCapabilityResolutionZeroEnabledIsUnmanaged(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	for _, name := range m.reg.Names() {
		if err := m.store.DisableProjectCapability("ATM", name, m.actor); err != nil {
			t.Fatalf("disable %s: %v", name, err)
		}
	}
	m.projectScope = "ATM"
	m.refreshAll()
	if m.capability.current != unmanagedCapability {
		t.Fatalf("current = %q, want unmanaged", m.capability.current)
	}
}

func TestSwitchToPersistsAndKeepsInMemoryCurrent(t *testing.T) {
	m := newTestModel(t)
	setupCapProject(t, m)
	m.capability.switchTo("contextmap")
	if m.capability.current != "contextmap" {
		t.Fatalf("current = %q, want contextmap", m.capability.current)
	}
	cfg, err := m.store.GetBoardsConfig("ATM")
	if err != nil || cfg.Capability != "contextmap" {
		t.Fatalf("persisted = %+v (%v), want capability=contextmap", cfg, err)
	}
	// A later refresh keeps the in-session current even though other values
	// are also valid.
	m.refreshAll()
	if m.capability.current != "contextmap" {
		t.Fatalf("current after refresh = %q, want contextmap", m.capability.current)
	}
}

func TestCapabilityTaskCountOwnershipBased(t *testing.T) {
	m := newTestModel(t)
	setupCapProject(t, m)
	// "open one" carries ATM:status:open (workflow-owned); "stray" carries
	// only ATM:needs-triage (unmanaged).
	if got := m.capabilityTaskCount("workflow"); got != 1 {
		t.Errorf("workflow count = %d, want 1", got)
	}
	if got := m.capabilityTaskCount(unmanagedCapability); got != 1 {
		t.Errorf("unmanaged count = %d, want 1", got)
	}
	if got := m.capabilityTaskCount("contextmap"); got != 0 {
		t.Errorf("contextmap count = %d, want 0", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestCapability|TestSwitchTo' -v`
Expected: FAIL — `m.capability undefined`, `undefined: unmanagedCapability`.

- [ ] **Step 3: Create `internal/tui/capabilities.go`**

```go
package tui

import (
	"fmt"

	"atm/internal/capability"
	"atm/internal/core"
	"github.com/charmbracelet/bubbletea"
)

// unmanagedCapability is the pseudo-capability of the capability view: the
// labels no enabled capability owns, browsed via the label drill-down. It is
// always selectable and never appears in Project.Capabilities.
const unmanagedCapability = "unmanaged"

// capEntry is one row of the [C] switcher overlay.
type capEntry struct {
	name      string
	summary   string
	enabled   bool
	unmanaged bool
	count     string // "6 boards" / "5 labels · 12 tasks"
}

// capabilityModel owns the capability concern of pane [2]: which capabilities
// are listed and enabled, which is current, the [C] switcher overlay, and the
// persistence of the selection (config.json -> boards.capability). The ring
// (boardsModel) and the header (tasksModel) read from it; it never renders
// their surfaces.
type capabilityModel struct {
	m       *Model
	current string
	entries []capEntry
	open    bool
	cursor  int
}

func newCapabilityModel(m *Model) capabilityModel { return capabilityModel{m: m} }

func (c *capabilityModel) unmanagedCurrent() bool { return c.current == unmanagedCapability }

// refresh rebuilds the switcher entries and re-resolves current. It MUST run
// before boardsModel.refresh in refreshAll — the ring is scoped to current.
func (c *capabilityModel) refresh() {
	c.entries = nil
	scope := c.m.projectScope
	if scope == "" {
		c.current = ""
		return
	}
	enabled := map[string]bool{}
	names := c.m.regFor(scope).Names()
	for _, n := range names {
		enabled[n] = true
	}
	boardsPer := map[string]int{}
	for _, e := range c.m.reg.Exposed(scope) {
		boardsPer[e.Owner]++
	}
	for _, d := range c.m.reg.Describe() {
		n := boardsPer[d.Name]
		c.entries = append(c.entries, capEntry{
			name:    d.Name,
			summary: d.Summary,
			enabled: enabled[d.Name],
			count:   fmt.Sprintf("%d %s", n, pluralBoards(n)),
		})
	}
	un, _ := c.m.regFor(scope).Unmanaged(c.m.store, scope)
	c.entries = append(c.entries, capEntry{
		name:      unmanagedCapability,
		summary:   "labels no enabled capability owns",
		unmanaged: true,
		count: fmt.Sprintf("%d %s · %d %s",
			len(un), pluralLabels(len(un)),
			c.m.countTasksCarrying(scope, capability.NewLabelSet(un)), pluralTasks(c.m.countTasksCarrying(scope, capability.NewLabelSet(un)))),
	})
	c.current = c.resolveCurrent(names)
	if c.cursor >= len(c.entries) {
		c.cursor = len(c.entries) - 1
	}
	if c.cursor < 0 {
		c.cursor = 0
	}
}

func pluralBoards(n int) string {
	if n == 1 {
		return "board"
	}
	return "boards"
}

func pluralLabels(n int) string {
	if n == 1 {
		return "label"
	}
	return "labels"
}

// resolveCurrent applies the resolution rule: the in-session current if still
// valid, else the persisted boards.capability if valid, else the first
// enabled capability, else unmanaged. Never writes back — only switchTo
// persists.
func (c *capabilityModel) resolveCurrent(enabledNames []string) string {
	valid := func(v string) bool {
		if v == unmanagedCapability {
			return true
		}
		for _, n := range enabledNames {
			if n == v {
				return true
			}
		}
		return false
	}
	if c.current != "" && valid(c.current) {
		return c.current
	}
	if cfg, err := c.m.store.GetBoardsConfig(c.m.projectScope); err == nil && cfg != nil && cfg.Capability != "" && valid(cfg.Capability) {
		return cfg.Capability
	}
	if len(enabledNames) > 0 {
		return enabledNames[0]
	}
	return unmanagedCapability
}

// switchTo makes name the current capability: persist (best-effort — the
// in-memory switch survives a failed write), rebuild the ring for the new
// scope, and reset the task focus through the boards channel.
func (c *capabilityModel) switchTo(name string) {
	scope := c.m.projectScope
	if scope == "" {
		return
	}
	c.open = false
	if name == c.current {
		return
	}
	c.current = name
	cfg, err := c.m.store.GetBoardsConfig(scope)
	if err != nil || cfg == nil {
		cfg = &core.BoardsConfig{}
	}
	cfg.Capability = name
	if err := c.m.store.SetProjectBoards(scope, cfg, c.m.actor); err != nil {
		c.m.showToast("save capability: " + err.Error())
	}
	c.m.boards.resetDrill()
	c.m.boards.selected = ""
	c.m.boards.refresh()
	c.m.boards.selectDefault()
	c.m.boards.loadPins()
}
```

(`tea` import is unused until Task 8's `handleKey`; omit it now and let Task 8 add it — do NOT leave an unused import.)

- [ ] **Step 4: Wire into `Model` (app.go)**

1. Add the field after `boards   boardsModel` (app.go:93): `capability capabilityModel`.
2. In `NewModel` after `m.boards = newBoardsModel(m)` (app.go:172): `m.capability = newCapabilityModel(m)`.
3. In `refreshAll` (app.go:329), insert the capability refresh FIRST:

```go
func (m *Model) refreshAll() {
	m.capability.refresh()
	m.projects.refresh()
	m.tasks.refresh()
	m.boards.refresh()
	m.help.refresh()
	m.lastRefreshAt = core.Now()
}
```

4. Append the count helpers to app.go:

```go
// countTasksCarrying counts distinct project tasks carrying at least one
// label the set owns. Runs on refresh, never per frame.
func (m *Model) countTasksCarrying(scope string, set capability.LabelSet) int {
	count := 0
	for _, tk := range m.store.ListTasks(core.QueryFilters{Project: scope}) {
		for _, full := range tk.Labels {
			if set.Contains(full) {
				count++
				break
			}
		}
	}
	return count
}

// capabilityTaskCount is the header's capability-owned total: tasks carrying
// at least one label the named capability owns (ownership rule shared with
// Registry.Unmanaged via LabelSet). For unmanaged it counts tasks carrying
// any unmanaged label. Deliberate: workflow's count reflects the paved road
// (status:/priority:-labeled tasks), not its all-tasks board match.
func (m *Model) capabilityTaskCount(capName string) int {
	scope := m.projectScope
	if scope == "" || capName == "" {
		return 0
	}
	if capName == unmanagedCapability {
		un, _ := m.regFor(scope).Unmanaged(m.store, scope)
		return m.countTasksCarrying(scope, capability.NewLabelSet(un))
	}
	return m.countTasksCarrying(scope, capability.NewLabelSet(m.reg.OwnedLabels(scope, capName)))
}
```

5. In `projects.go` project-select handler, after `p.m.boards.reset()` (projects.go:242) add:

```go
			p.m.capability.current = "" // re-resolve for the new project
```

(The subsequent `refreshAll`-equivalent calls at :262-265 run `tasks.refresh`/`boards.refresh` directly — add `p.m.capability.refresh()` immediately before `p.m.tasks.refresh()` at :262.)

- [ ] **Step 5: Run tests**

Run: `go test ./internal/tui/ -run 'TestCapability|TestSwitchTo' -v`
Expected: PASS. Then `go test ./internal/tui/` — pre-existing tests must still pass (the ring is not yet scoped; nothing consumes `current` besides the new code).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/capabilities.go internal/tui/capabilities_test.go internal/tui/app.go internal/tui/projects.go
git commit -m "feat(ATM-90171b): capabilityModel — current-capability state, resolution, persistence"
```

---

### Task 4: Ring scoped to the current capability; umbrella row and owner column removed

**Files:**
- Modify: `internal/tui/labels.go` (`buildBoardRows` :655-740, `selectDefault` :290-315, `togglePin` :357-359, `focusCenter` :438-443, `applyFocus` :630-639, `renderTable` :1428-1449, `boardTableLine` :1466-1490, `boardRow` :120-131, `drillIn` :513-517, `handleTableKey` :1266-1269)
- Test: `internal/tui/labels_test.go` (update), new assertions below

**Interfaces:**
- Consumes: `m.capability.current`, `m.capability.unmanagedCurrent()` (Task 3).
- Produces: ring rows are exactly the current capability's `Exposed` labels (hidden/order still applied). `boardRow.Umbrella` field and `Owner` field REMOVED. `b.unmanaged` is populated only in unmanaged mode (Task 5 consumes it); in managed mode it is nil.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/labels_test.go`:

```go
func TestRingScopedToCurrentCapability(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := m.regFor("ATM").EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	m.refreshAll()
	if m.capability.current != "workflow" {
		t.Fatalf("precondition: current = %q", m.capability.current)
	}
	// workflow exposes 6 labels; contextmap's context-current must NOT appear.
	if len(m.boards.rows) != 6 {
		t.Fatalf("ring len = %d, want 6; rows = %v", len(m.boards.rows), m.boards.rowNames())
	}
	for _, r := range m.boards.rows {
		if r.FullName == "ATM:context-current" {
			t.Fatalf("contextmap board leaked into workflow ring")
		}
	}
	m.capability.switchTo("contextmap")
	if len(m.boards.rows) != 1 || m.boards.rows[0].FullName != "ATM:context-current" {
		t.Fatalf("contextmap ring = %v, want [ATM:context-current]", m.boards.rowNames())
	}
}

func TestRingHasNoUmbrellaRow(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := m.regFor("ATM").EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "stray", "ATM:needs-triage") // creates an unmanaged label
	m.refreshAll()
	for _, r := range m.boards.rows {
		if r.FullName == "ATM:unmanaged" {
			t.Fatalf("umbrella row must not be in the capability-scoped ring")
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestRingScoped|TestRingHasNoUmbrella' -v`
Expected: FAIL — ring contains 7 rows (6 workflow + context-current) plus the umbrella when unmanaged labels exist.

- [ ] **Step 3: Rewrite `buildBoardRows`**

Replace `buildBoardRows` (labels.go:655-740) — the umbrella append block (lines 698-715) is deleted, the loop filters by owner, and `Owner` stops being stored on rows:

```go
// buildBoardRows constructs the ring for the CURRENT capability: its Exposed
// labels in the capability's preferred order, hidden rows dropped, then the
// boards-config order override applied as a partial reorder. Other enabled
// capabilities' boards are not in this ring — the [C] switcher changes scope.
// In unmanaged mode the ring is empty (the drill-down is the surface).
func (b *boardsModel) buildBoardRows(ls []core.Label) []boardRow {
	scope := b.m.projectScope
	current := b.m.capability.current
	if current == "" || current == unmanagedCapability {
		return nil
	}
	stored := map[string]core.Label{}
	for _, l := range ls {
		stored[l.Name] = l
	}
	reg := b.m.regFor(scope)
	var out []boardRow
	seen := map[string]bool{}
	for _, e := range reg.Exposed(scope) {
		if e.Owner != current {
			continue
		}
		l := e.Label
		if seen[l.Name] {
			continue
		}
		seen[l.Name] = true
		if s, ok := stored[l.Name]; ok && s.Description != "" {
			l.Description = s.Description
		}
		suffix := strings.TrimPrefix(l.Name, scope+":")
		if core.IsNamespaceName(l.Name) {
			ns := strings.TrimSuffix(suffix, ":*")
			out = append(out, boardRow{
				Name:             ns,
				FullName:         l.Name,
				Description:      l.Description,
				Count:            b.namespaceTaskCount(ns),
				Expandable:       true,
				NeedsDescription: l.Description == "",
			})
			continue
		}
		count, broken := b.boardCount(l.Name)
		out = append(out, boardRow{
			Name:        suffix,
			FullName:    l.Name,
			Description: l.Description,
			Expr:        l.Expr,
			Count:       count,
			Broken:      broken,
		})
	}
	// Hidden filter, then partial order override (unchanged).
	hidden := map[string]bool{}
	for _, n := range b.boardsCfg.Hidden {
		hidden[n] = true
	}
	kept := out[:0:0]
	for _, r := range out {
		if !hidden[r.FullName] {
			kept = append(kept, r)
		}
	}
	out = kept
	if len(b.boardsCfg.Order) > 0 {
		effective := make([]string, len(out))
		for i, r := range out {
			effective[i] = r.FullName
		}
		pos := map[string]int{}
		for i, n := range capability.OrderFullNames(effective, b.boardsCfg.Order) {
			pos[n] = i
		}
		sort.SliceStable(out, func(i, j int) bool { return pos[out[i].FullName] < pos[out[j].FullName] })
	}
	return out
}
```

- [ ] **Step 4: Remove umbrella/owner traces from managed-mode paths**

All in `labels.go`:

1. `boardRow` struct (:120-131): delete the `Owner string` and `Umbrella bool` fields and their comments.
2. `selectDefault` (:290-315): the inner loop that skips `r.Umbrella` simplifies to selecting `b.rows[0]`:

```go
	if len(b.rows) > 0 {
		b.selected = b.rows[0].FullName
		b.applyFocus()
		return
	}
```

3. `togglePin` (:357-359): delete the three-line umbrella guard (`if idx := b.ringIndex(); ...`).
4. `focusCenter` (:438-443): replace the `if r := b.rows[idx]; r.Umbrella { b.enterUmbrella() } else if r.Expandable {...}` block with:

```go
	if r := b.rows[idx]; r.Expandable {
		b.enterChart(r.Name)
	}
```

5. `applyFocus` (:630-639): delete the `if r.Umbrella {...}` block entirely.
6. `drillIn` case `lLevelTable` (:513-517): delete the `if r.Umbrella { b.enterUmbrella(); return }` block.
7. `handleTableKey` case `"enter"` (:1266-1269): delete the `if r.Umbrella { b.enterUmbrella(); return nil }` block.
8. `renderTable` (:1428-1449): drop the OWNER column — header becomes `boardTableLine(b.width, "BOARD", "DESCRIPTION", "COUNT")` and the row line `boardTableLine(b.width, name, r.Description, count)` (delete the `owner :=` lines).
9. `boardTableLine` (:1466-1490): drop the `owner` parameter:

```go
// boardTableLine renders one row of the flat Boards list: fixed-width name,
// flexible description, and an 8-wide count. Padding is by display width
// (lipgloss.Width), not byte length.
func boardTableLine(width int, name, description, count string) string {
	nameW := 16
	countW := 8
	// leading space (1) + 2 inter-column separators (2) = 3
	descW := width - nameW - countW - 3
	if descW < 8 {
		descW = 8
	}
	namePad := nameW - lipgloss.Width(name)
	if namePad < 0 {
		namePad = 0
	}
	desc := truncateRunes(description, descW)
	descPad := descW - lipgloss.Width(desc)
	if descPad < 0 {
		descPad = 0
	}
	return fitLine(fmt.Sprintf(" %s%s %s%s %8s", name, spaces(namePad), desc, spaces(descPad), count), width)
}
```

10. In `refresh()` (:237-282): `b.unmanaged` is no longer populated by `buildBoardRows`; add `b.unmanaged = nil` right before `b.rows = b.buildBoardRows(ls)` (Task 5 populates it in unmanaged mode).

Do NOT delete `enterUmbrella`, `buildUmbrellaRows`, `renderUmbrella`, `handleUmbrellaKey`, `lLevelUmbrella`, `umbrellaCaption`, or `unmanagedTaskCount` — Task 5 repurposes them. Delete `umbrellaDescription` (:208) only if the compiler now flags it unused (it was only used by the deleted ring row); same for `capability.UmbrellaFullName` references in labels.go — the `capability` import stays (still used by `OrderFullNames`).

- [ ] **Step 5: Update existing tests**

Run: `go test ./internal/tui/ 2>&1 | head -80` and fix systematically:

- Tests asserting the umbrella row is IN the ring (grep `Umbrella` and `ATM:unmanaged` in `labels_test.go`): sentinel-shadowing tests and umbrella-ring-presence tests are DELETED (the concept is gone). Umbrella drill/sub-table behavior tests are kept only if they exercise `buildUmbrellaRows`/`handleUmbrellaKey` directly — those survive until Task 5 re-anchors them; if they drive the ring to reach the umbrella, delete them (Task 5 adds unmanaged-mode equivalents).
- Tests asserting mixed-capability rings (e.g. context-current alongside workflow boards): update to switch capability first (`m.capability.switchTo("contextmap")`) or assert per-capability rings.
- Tests calling `boardTableLine` with 4 content args or asserting the OWNER header: update to the 3-arg shape.
- `newTestBoardsModel`/`newTestBoardsModelWithCaps` (labels_test.go:639-665) construct `boardsModel` without a full Model — if they bypass `Model.capability`, set `m.capability.current = "workflow"` (or the test's capability) explicitly after construction; add that to the helpers so every existing call site resolves.
- `TestSelectDefaultPicksAllTasksWithoutRegistryConsultation` (labels_test.go:73): registers contextmap before workflow to prove name-based pick within a mixed ring. The mixed ring is gone; rewrite the test to seed only workflow enabled and assert all-tasks wins over backlog within the workflow ring (keep the name; update its comment to say the ring is capability-scoped now).

Expected after fixes: `go test ./internal/tui/` PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/labels.go internal/tui/labels_test.go
git commit -m "feat(ATM-90171b): ring scoped to current capability; umbrella row and owner column removed"
```

---

### Task 5: Unmanaged mode — full-width label drill-down, unset-cursor selection

**Files:**
- Modify: `internal/tui/labels.go` (`refresh` :237-282, `chartCursorMove` :596-616, `drillOut` :584-589, `handleUmbrellaKey` :964-968, `renderUmbrella` :978-996, `renderMeterRows` :1556, `statusHint` :1634-1651, `selectDefault`)
- Modify: `internal/tui/thumbnails.go` (`renderStrip` :56-84)
- Modify: `internal/tui/tasks_list.go` (`renderFlatList` idle copy :340-347)
- Test: `internal/tui/labels_test.go` (append)

**Interfaces:**
- Consumes: `unmanagedCapability`, `m.capability.unmanagedCurrent()` (Task 3); surviving umbrella machinery (Task 4).
- Produces: `(*boardsModel).inUnmanagedMode() bool`; `(*boardsModel).enterUnmanagedBase()`; `(*boardsModel).applyUmbrellaSelection()`. In unmanaged mode: `b.rows == nil`, `b.level` starts at `lLevelUmbrella` with `b.cursor == -1`, tasks focus starts `focusUmbrellaIdle`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/labels_test.go`:

```go
func setupUnmanagedMode(t *testing.T, m *Model) {
	t.Helper()
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := m.regFor("ATM").EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "stray one", "ATM:needs-triage")
	seedTask(t, m, "ATM", "typed", "ATM:type:bug")
	m.refreshAll()
	m.capability.switchTo(unmanagedCapability)
}

func TestUnmanagedModeStartsIdleWithUnsetCursor(t *testing.T) {
	m := newTestModel(t)
	setupUnmanagedMode(t, m)
	if m.boards.level != lLevelUmbrella {
		t.Fatalf("level = %v, want lLevelUmbrella", m.boards.level)
	}
	if m.boards.cursor != -1 {
		t.Fatalf("cursor = %d, want -1 (unset)", m.boards.cursor)
	}
	if m.tasks.focus.mode != focusUmbrellaIdle {
		t.Fatalf("focus = %v, want focusUmbrellaIdle", m.tasks.focus.mode)
	}
	if len(m.tasks.rows) != 0 {
		t.Fatalf("tasks rows = %d, want 0 before any label selection", len(m.tasks.rows))
	}
	if len(m.boards.rows) != 0 {
		t.Fatalf("ring rows = %d, want 0 in unmanaged mode", len(m.boards.rows))
	}
}

func TestUnmanagedFirstCursorMoveSelectsAndFilters(t *testing.T) {
	m := newTestModel(t)
	setupUnmanagedMode(t, m)
	m.boards.chartCursorMove(1) // first Shift-↓: -1 -> 0 and applies the filter
	if m.boards.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.boards.cursor)
	}
	if m.tasks.focus.mode == focusUmbrellaIdle {
		t.Fatalf("focus still idle after first cursor move")
	}
	if len(m.tasks.rows) == 0 && len(m.tasks.groups) == 0 {
		t.Fatalf("no tasks listed after selecting the first unmanaged label")
	}
}

func TestUnmanagedEscDoesNotClimbOut(t *testing.T) {
	m := newTestModel(t)
	setupUnmanagedMode(t, m)
	m.boards.drillOut() // Shift-← / Esc at the base level: nowhere to go
	if m.boards.level != lLevelUmbrella {
		t.Fatalf("level = %v after drillOut, want lLevelUmbrella (no ring above)", m.boards.level)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run TestUnmanaged -v`
Expected: FAIL — after `switchTo(unmanagedCapability)` the boards model sits at `lLevelTable` with an empty ring and no umbrella surface.

- [ ] **Step 3: Implement unmanaged mode in `labels.go`**

1. Mode predicate + base entry (place next to `enterUmbrella`):

```go
// inUnmanagedMode reports whether pane [2] is in the unmanaged capability
// view: no ring, the label drill-down is the whole surface.
func (b *boardsModel) inUnmanagedMode() bool {
	return b.m.capability.unmanagedCurrent()
}

// enterUnmanagedBase (re)enters unmanaged mode's base level: the full-width
// label drill-down with an UNSET cursor (-1) and the idle task focus. The
// first Shift-↑/↓ sets the cursor and applies that label's filter — until
// then no task query runs (the performance guarantee of the capability-view
// spec).
func (b *boardsModel) enterUnmanagedBase() {
	b.level = lLevelUmbrella
	b.umbrellaRows = b.buildUmbrellaRows()
	b.cursor = -1
	b.offset = 0
	b.tableCursor = 0
	b.fromUmbrella = false
	b.m.tasks.setFocus(taskFocus{mode: focusUmbrellaIdle}, "")
}

// applyUmbrellaSelection pushes the cursor row's label as the tasks filter:
// namespace rows facet (focusPresent), leaf rows filter exactly. An unset or
// out-of-range cursor restores the idle state.
func (b *boardsModel) applyUmbrellaSelection() {
	if b.cursor < 0 || b.cursor >= len(b.umbrellaRows) {
		b.m.tasks.setFocus(taskFocus{mode: focusUmbrellaIdle}, "")
		return
	}
	r := b.umbrellaRows[b.cursor]
	if r.Expandable {
		b.m.tasks.setFocus(taskFocus{mode: focusPresent, ns: r.Name}, core.FacetToken(b.m.projectScope, r.Name))
	} else {
		b.m.tasks.setFocus(taskFocus{mode: focusOff}, r.FullName)
	}
}
```

2. `refresh()` (:237-282) — add the unmanaged branch right after `b.boardsCfg = cfg` is set and the `memberRows` loop (keep `memberRows` populated — charts drilled from the sub-table need it):

```go
	if b.inUnmanagedMode() {
		un, _ := b.m.regFor(scope).Unmanaged(b.m.store, scope)
		b.unmanaged = un
		b.rows = nil
		b.selected = ""
		switch b.level {
		case lLevelUmbrella:
			// Background tick while browsing: rebuild rows, keep the cursor
			// meaningful (clamped; an unset cursor stays unset).
			b.umbrellaRows = b.buildUmbrellaRows()
			if b.cursor >= len(b.umbrellaRows) {
				b.cursor = len(b.umbrellaRows) - 1
			}
		case lLevelChart, lLevelDetail:
			// Drilled below the sub-table: leave the drill state alone.
		default:
			// Just switched in (resetDrill left us at lLevelTable).
			b.enterUnmanagedBase()
		}
		b.loadPins()
		return
	}
```

3. `selectDefault` (:290): first line, before `b.resetDrill()`:

```go
	if b.inUnmanagedMode() {
		b.enterUnmanagedBase()
		return
	}
```

4. `chartCursorMove` (:596-616) — the umbrella case gains the unset-start and selection application:

```go
func (b *boardsModel) chartCursorMove(dir int) {
	switch b.level {
	case lLevelChart:
		n := len(b.chartRows())
		if n == 0 {
			return
		}
		b.cursor += dir
		if b.cursor < 0 {
			b.cursor = 0
		}
		if b.cursor >= n {
			b.cursor = n - 1
		}
	case lLevelUmbrella:
		n := len(b.umbrellaRows)
		if n == 0 {
			return
		}
		if b.cursor < 0 {
			// First move from the unset cursor lands on the top row
			// regardless of direction.
			b.cursor = 0
		} else {
			b.cursor += dir
			if b.cursor < 0 {
				b.cursor = 0
			}
			if b.cursor >= n {
				b.cursor = n - 1
			}
		}
		b.applyUmbrellaSelection()
	}
}
```

5. `drillOut` case `lLevelUmbrella` (:584-589) — nowhere to climb in unmanaged mode:

```go
	case lLevelUmbrella:
		// Unmanaged mode's base level: there is no ring above.
		return
```

(The old climb-to-table body is dead — `lLevelUmbrella` is only reachable in unmanaged mode now.) Same for `handleUmbrellaKey` case `"esc"` (:964-968): replace the body with `return nil`. Also in `handleUmbrellaKey` case `"enter"` — after the existing drill code, no change; but with `b.cursor < 0` the existing bounds check already returns nil.

6. Chart/detail climbs re-apply the selection, not the ring focus: in `drillOut` cases `lLevelDetail`/`lLevelChart` and `handleChartKey`/`handleDetailKey` `"esc"` cases, the `fromUmbrella` branches call `b.reenterUmbrella(); b.applyFocus()`. Change every one of those four `b.applyFocus()` calls (labels.go:566/577, :1327-1328, :1372-1373) to `b.applyUmbrellaSelection()`, and in `reenterUmbrella` (:913-919) keep `b.cursor = 0` — the user had a row selected to drill from; then `applyUmbrellaSelection` re-applies row 0. Better: preserve the cursor — add a `b.umbrellaCursor int` save in `handleUmbrellaKey`'s `"enter"` case (`b.umbrellaCursor = b.cursor`) and restore it in `reenterUmbrella` (`b.cursor = b.umbrellaCursor`), clamped. Add the `umbrellaCursor int` field to `boardsModel` next to `tableCursor`.

7. `renderUmbrella` (:978-996): header text becomes label-count based and the empty copy updates:

```go
	if len(b.umbrellaRows) == 0 {
		return padToHeight("nothing unmanaged — every label is capability-owned", b.contentHeight)
	}
```

and the header line: `fmt.Sprintf("unmanaged  ·  %d %s", len(b.unmanaged), pluralLabels(len(b.unmanaged)))` (keep the caption line).

8. `renderMeterRows` (:1522-1562): windowing must tolerate the unset cursor — replace `windowLines(len(lines), b.cursor, b.pageSize)` with:

```go
	cur := b.cursor
	if cur < 0 {
		cur = 0
	}
	start, end := windowLines(len(lines), cur, b.pageSize)
```

(the `i == b.cursor` highlight comparison stays — with `-1` no row highlights, which is the unset look).

9. `statusHint` (:1634-1651) `lLevelUmbrella` case: `return "[Shift-↑/↓]select label  [Shift-→]drill"`.

- [ ] **Step 4: Full-width surface in `renderStrip` (thumbnails.go:56)**

The existing guard at thumbnails.go:57 is a COMBINED condition (`b.m.projectScope == "" || len(b.rows) == 0`); split it — no-project first, then the unmanaged branch, then the empty-ring branch:

```go
	if b.m.projectScope == "" {
		return titledBoxHeight(b.m.styles.PaneInactive, paneW, "Boards", "no project selected", stripH)
	}
	if b.inUnmanagedMode() {
		title := fmt.Sprintf("unmanaged · %d %s", len(b.unmanaged), pluralLabels(len(b.unmanaged)))
		savedW, savedH := b.width, b.contentHeight
		b.SetSize(paneW-2, stripH-2)
		var inner string
		switch b.level {
		case lLevelChart:
			inner = b.renderChart()
		case lLevelDetail:
			inner = b.renderDetail()
		default:
			inner = b.renderUmbrella()
		}
		b.SetSize(savedW, savedH)
		return titledBoxHeight(b.m.styles.PaneActive, paneW, title, inner, stripH)
	}
```

Then the empty-ring branch (a scoped project whose current capability exposes zero boards — spec §10) follows the unmanaged block:

```go
	if len(b.rows) == 0 {
		return titledBoxHeight(b.m.styles.PaneInactive, paneW, "Boards",
			fmt.Sprintf("%s exposes no boards", b.m.capability.current), stripH)
	}
```

- [ ] **Step 5: Idle copy in `renderFlatList` (tasks_list.go:340-347)**

```go
	if t.focus.mode == focusUmbrellaIdle {
		t.renderEmptyState(b, []string{
			t.m.styles.EmptyHead.Render("unmanaged labels"),
			"",
			t.m.styles.EmptyText.Render("select a label below (Shift-↑/↓) to see its tasks"),
		})
		return
	}
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/tui/ -run TestUnmanaged -v` → PASS, then `go test ./internal/tui/` and fix fallout: any surviving umbrella tests from Task 4 that drove `enterUmbrella` via the ring now go through `switchTo(unmanagedCapability)`; re-anchor them on `setupUnmanagedMode`. The old "press Enter to drill in" idle-copy assertion updates to the new copy.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/labels.go internal/tui/thumbnails.go internal/tui/tasks_list.go internal/tui/labels_test.go
git commit -m "feat(ATM-90171b): unmanaged mode — full-width label drill-down with unset-cursor selection"
```

---

### Task 6: Strip width variants + pinned box to the pane bottom

**Files:**
- Modify: `internal/tui/thumbnails.go` (`splitStripWidths` :12-50, `renderStrip` :56-84)
- Modify: `internal/tui/tasks_list.go` (`renderListWithStrip` :223-246)
- Test: `internal/tui/thumbnails_test.go` (update + append)

**Interfaces:**
- Produces: `splitStripWidths(paneW, ringN int) (prev, sel, next int)` — NOTE the new second parameter; a `0` width means "cell absent".

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/thumbnails_test.go`:

```go
func TestSplitStripWidthsOneBoardFullWidth(t *testing.T) {
	prev, sel, next := splitStripWidths(100, 1)
	if prev != 0 || next != 0 || sel != 100 {
		t.Fatalf("got %d/%d/%d, want 0/100/0", prev, sel, next)
	}
}

func TestSplitStripWidthsTwoBoards70_30(t *testing.T) {
	prev, sel, next := splitStripWidths(100, 2)
	if prev != 0 {
		t.Fatalf("prev = %d, want 0 (no prev cell with 2 boards)", prev)
	}
	if sel != 70 || next != 30 {
		t.Fatalf("sel/next = %d/%d, want 70/30", sel, next)
	}
}

func TestSplitStripWidthsThreePlusKeeps25_50_25(t *testing.T) {
	prev, sel, next := splitStripWidths(100, 3)
	if prev != 25 || sel != 50 || next != 25 {
		t.Fatalf("got %d/%d/%d, want 25/50/25", prev, sel, next)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run TestSplitStripWidths -v`
Expected: FAIL — compile error (`splitStripWidths` takes 1 arg).

- [ ] **Step 3: Implement**

Replace `splitStripWidths` (thumbnails.go:12-50):

```go
// splitStripWidths divides pane [2] inner width among the strip cells by
// ring size: 1 board fills the pane; 2 boards split 70% SELECTED / 30% next
// (no prev cell); 3+ keep prev 25% / SELECTED 50% / next 25%. A returned 0
// means that cell is absent. Minimum clamps keep names readable on narrow
// terminals.
func splitStripWidths(paneW, ringN int) (prev, sel, next int) {
	const minSide = 6
	const minSel = 8
	switch {
	case ringN <= 1:
		return 0, paneW, 0
	case ringN == 2:
		sel = paneW * 70 / 100
		if sel < minSel {
			sel = minSel
		}
		next = paneW - sel
		if next < minSide {
			next = minSide
		}
		for sel+next > paneW && next > minSide {
			next--
		}
		for sel+next > paneW && sel > minSel {
			sel--
		}
		if sel+next > paneW { // degenerate width: selected takes everything
			return 0, paneW, 0
		}
		return 0, sel, next
	}
	prev = paneW * 25 / 100
	sel = paneW * 50 / 100
	next = paneW - prev - sel
	if prev < minSide {
		prev = minSide
	}
	if next < minSide {
		next = minSide
	}
	if sel < minSel {
		sel = minSel
	}
	for prev+sel+next > paneW && prev > minSide {
		prev--
	}
	for prev+sel+next > paneW && next > minSide {
		next--
	}
	for prev+sel+next > paneW && sel > minSel {
		sel--
	}
	if prev+sel+next > paneW {
		next = paneW - prev - sel
		if next < 0 {
			next = 0
			sel = paneW - prev
			if sel < 0 {
				sel = 0
				prev = paneW
			}
		}
	}
	return
}
```

In `renderStrip` (:56-84), pass the ring size and skip absent cells:

```go
	prevW, selW, nextW := splitStripWidths(paneW, len(b.rows))
	idx := b.ringIndex()
	if idx < 0 {
		idx = 0
	}
	selRow := b.rows[idx]

	var prevCell, nextCell string
	if prevW > 0 {
		prevCell = titledBoxHeight(b.m.styles.PaneInactive, prevW, "", "", stripH)
		if len(b.rows) >= 3 {
			prevCell = b.renderSideCell(prevW, stripH, b.rows[(idx-1+len(b.rows))%len(b.rows)], "◂")
		}
	}
	if nextW > 0 {
		nextCell = titledBoxHeight(b.m.styles.PaneInactive, nextW, "", "", stripH)
		if len(b.rows) >= 2 {
			nextCell = b.renderSideCell(nextW, stripH, b.rows[(idx+1)%len(b.rows)], "▸")
		}
	}
	selCell := b.renderSelectedCell(selW, stripH, selRow)

	cells := make([]string, 0, 3)
	if prevCell != "" {
		cells = append(cells, prevCell)
	}
	cells = append(cells, selCell)
	if nextCell != "" {
		cells = append(cells, nextCell)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cells...)
```

In `renderListWithStrip` (tasks_list.go:223-246), swap the stacking order (list → strip → pins) and update the doc comment:

```go
	var b strings.Builder
	b.WriteString(listOut)
	b.WriteString("\n")
	b.WriteString(strip)
	if pinned != "" {
		b.WriteString("\n")
		b.WriteString(pinned)
	}
	return padToHeight(b.String(), t.contentHeight)
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui/ -run 'TestSplitStripWidths' -v` → PASS, then `go test ./internal/tui/` — fix strip-layout assertions in `thumbnails_test.go` (existing `splitStripWidths` call sites in tests gain the ring-size arg; any golden asserting pins-above-strip order flips).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/thumbnails.go internal/tui/tasks_list.go internal/tui/thumbnails_test.go
git commit -m "feat(ATM-90171b): ring width variants (100/70-30/25-50-25); pinned box at pane bottom"
```

---

### Task 7: Header (`CAPABILITY / TOTAL / SORT`) + LABELS column removal

**Files:**
- Modify: `internal/tui/tasks.go` (`tasksModel` fields :8-35, `refresh` :121-194)
- Modify: `internal/tui/tasks_list.go` (`headerLine` :248-254, `focusCaption` :258-278 deleted, `taskColumnWidths` :322-337, `renderFlatList` :356-378, `renderGroupedList` :419-438, `renderGroup` :483-503)
- Test: `internal/tui/tasks_test.go` / `labels_test.go` (update + append)

**Interfaces:**
- Consumes: `m.capability.current`, `m.capabilityTaskCount` (Task 3).
- Produces: `tasksModel.capCount, totalCount int` fields (set in `refresh`); header string `CAPABILITY: <name>    TOTAL: <cap>/<total> tasks    SORT: <mode>`; task table columns `ID  TITLE  UPDATED`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/tasks_test.go`:

```go
func TestHeaderLineShowsCapabilityAndCounts(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := m.regFor("ATM").EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	seedTask(t, m, "ATM", "stray", "ATM:needs-triage")
	m.refreshAll()
	got := m.tasks.headerLine()
	want := "CAPABILITY: workflow    TOTAL: 1/2 tasks    SORT: updated-desc"
	if got != want {
		t.Fatalf("headerLine = %q, want %q", got, want)
	}
}

func TestFlatListDropsLabelsColumn(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := m.regFor("ATM").EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.refreshAll()
	m.boards.selectDefault()
	out := m.tasks.renderList()
	if strings.Contains(out, "LABELS") {
		t.Fatalf("flat list still shows LABELS column:\n%s", out)
	}
	if !strings.Contains(out, "TITLE") || !strings.Contains(out, "UPDATED") {
		t.Fatalf("flat list lost TITLE/UPDATED columns:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestHeaderLine|TestFlatListDrops' -v`
Expected: FAIL — header still `PROJECT: ... FOCUS: ...`; LABELS present.

- [ ] **Step 3: Implement**

1. `tasksModel` (tasks.go:14-35) gains two fields after `focus taskFocus`:

```go
	// header counts, computed on refresh (never per frame): capCount is the
	// current capability's owned-task total, totalCount the project total.
	capCount   int
	totalCount int
```

2. In `refresh()` (tasks.go:121), after `scope := t.m.projectScope`:

```go
	t.totalCount = len(t.m.store.ListTasks(core.QueryFilters{Project: scope}))
	t.capCount = t.m.capabilityTaskCount(t.m.capability.current)
```

(and zero both fields in the early-return `projectScope == ""` branch).

3. `headerLine` (tasks_list.go:248-254):

```go
func (t *tasksModel) headerLine() string {
	capName := t.m.capability.current
	if capName == "" {
		capName = "(none)"
	}
	return fmt.Sprintf("CAPABILITY: %s    TOTAL: %d/%d tasks    SORT: %s", capName, t.capCount, t.totalCount, t.sortMode)
}
```

4. Delete `focusCaption` (tasks_list.go:258-278). One test references it (`labels_test.go:1685`) — rewrite that assertion against `m.tasks.filter` containing `all-tasks` instead (it verifies the board filter reached the tasks pane; `t.filter` is the same signal).

5. `taskColumnWidths` (tasks_list.go:322-337) drops labels:

```go
// taskColumnWidths returns fixed widths for ID/UPDATED and a flexible TITLE
// width that absorbs the remaining pane width. The format string used by both
// the header and data rows is " %-*s %-*s %*s" (leading space + 2
// inter-column spaces = 3 extra columns of padding). idW sizing note as
// before (IDs are "<CODE>-<hash>").
func (t *tasksModel) taskColumnWidths() (idW, updatedW, titleW int) {
	idW, updatedW = 9, 8
	for _, r := range t.rows {
		if w := len(r.id); w > idW {
			idW = w
		}
	}
	if idW > 14 {
		idW = 14
	}
	titleW = t.width - idW - updatedW - 3
	if titleW < 16 {
		titleW = 16
	}
	return
}
```

6. `renderFlatList` (tasks_list.go:356-378) — header and rows lose the labels cell:

```go
	idW, updatedW, titleW := t.taskColumnWidths()
	header := fmt.Sprintf(" %-*s %-*s %*s", idW, "ID", titleW, "TITLE", updatedW, "UPDATED")
	...
	for i := start; i < end; i++ {
		r := t.rows[i]
		line := fmt.Sprintf(" %-*s %-*s %*s", idW, truncateRunes(r.id, idW), titleW, truncateRunes(r.title, titleW), updatedW, r.updated)
		...
	}
```

(delete the `labels := "-"` block).

7. Grouped rows: in `renderGroupedList` (:431) and `renderGroup` (:495) delete the `labels := "(no labels)"` blocks and drop the `labels %s` segment:

```go
		line := fmt.Sprintf("  %s   id %s   updated %s", truncateRunes(r.title, titleW), r.id, r.updated)
```

and in `renderGroup`:

```go
			line := fmt.Sprintf("%s%s   id %s   updated %s", rowIndent, truncateRunes(r.title, titleW), r.id, r.updated)
```

(`taskRow.labels` stays — grouping logic and detail view still read it.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui/` — fix fallout: any test asserting the old header format (grep `PROJECT:` / `FOCUS:` in `internal/tui/*_test.go`), the old 4-column task line, or `focusCaption`.
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/tasks.go internal/tui/tasks_list.go internal/tui/tasks_test.go internal/tui/labels_test.go
git commit -m "feat(ATM-90171b): capability header with owned/total counts; LABELS column removed"
```

---

### Task 8: [C] switcher overlay

**Files:**
- Modify: `internal/tui/capabilities.go` (overlay open/keys/render)
- Modify: `internal/tui/app.go` (`handleKey` :592-597 C routing + overlay key block after :547; `View` :786-804)
- Modify: `internal/tui/tasks_list.go` (`statusHint` :575-589)
- Modify: `internal/tui/keymap.go` (:52)
- Test: `internal/tui/capabilities_test.go` (append)

**Interfaces:**
- Consumes: everything from Task 3.
- Produces: `(*capabilityModel).openOverlay()`, `(*capabilityModel).handleKey(k tea.KeyMsg) tea.Cmd`, `(*capabilityModel).renderOverlay() string`. While `m.capability.open`, ALL keys route to it (T excepted for theme).

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/capabilities_test.go` (add `tea "github.com/charmbracelet/bubbletea"` and `"strings"` to imports):

```go
func key(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestCKeyOpensSwitcherOnlyInTasksPane(t *testing.T) {
	m := newTestModel(t)
	setupCapProject(t, m)
	m.focused = paneProjects
	m.handleKey(key("C"))
	if m.capability.open {
		t.Fatalf("switcher opened from Projects pane; C must keep conventions there")
	}
	if m.helpOverlay != helpConventions {
		t.Fatalf("helpOverlay = %v, want conventions", m.helpOverlay)
	}
	m.closeHelp()
	m.focused = paneTasks
	m.handleKey(key("C"))
	if !m.capability.open {
		t.Fatalf("switcher did not open from Tasks pane")
	}
	if m.helpOverlay != helpNone {
		t.Fatalf("conventions overlay opened alongside the switcher")
	}
}

func TestOverlayCursorOpensOnCurrent(t *testing.T) {
	m := newTestModel(t)
	setupCapProject(t, m)
	m.capability.switchTo("contextmap")
	m.capability.openOverlay()
	e := m.capability.entries[m.capability.cursor]
	if e.name != "contextmap" {
		t.Fatalf("cursor on %q, want contextmap (the current)", e.name)
	}
}

func TestOverlayEnterSwitches(t *testing.T) {
	m := newTestModel(t)
	setupCapProject(t, m)
	m.capability.openOverlay()
	// Move to the unmanaged entry (always last) and select it.
	m.capability.cursor = len(m.capability.entries) - 1
	m.capability.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.capability.open {
		t.Fatalf("overlay still open after Enter")
	}
	if !m.capability.unmanagedCurrent() {
		t.Fatalf("current = %q, want unmanaged", m.capability.current)
	}
}

func TestOverlayEnterOnDisabledEnablesAndSwitches(t *testing.T) {
	m := newTestModel(t)
	setupCapProject(t, m)
	if err := m.store.DisableProjectCapability("ATM", "contextmap", m.actor); err != nil {
		t.Fatalf("disable: %v", err)
	}
	m.refreshAll()
	m.capability.openOverlay()
	for i, e := range m.capability.entries {
		if e.name == "contextmap" {
			m.capability.cursor = i
		}
	}
	m.capability.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.capability.current != "contextmap" {
		t.Fatalf("current = %q, want contextmap", m.capability.current)
	}
	p, err := m.store.GetProject("ATM")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	enabled := false
	for _, n := range p.Capabilities {
		if n == "contextmap" {
			enabled = true
		}
	}
	if !enabled {
		t.Fatalf("contextmap not enabled after Enter; capabilities = %v", p.Capabilities)
	}
}

func TestOverlaySpaceDisablesCurrentAndFallsBack(t *testing.T) {
	m := newTestModel(t)
	setupCapProject(t, m)
	m.capability.openOverlay()
	for i, e := range m.capability.entries {
		if e.name == "workflow" {
			m.capability.cursor = i
		}
	}
	m.capability.handleKey(key(" "))
	if !m.capability.open {
		t.Fatalf("space must not close the overlay")
	}
	if m.capability.current == "workflow" {
		t.Fatalf("current still workflow after disabling it; want fallback")
	}
}

func TestStatusHintLeadsWithCapabilities(t *testing.T) {
	m := newTestModel(t)
	setupCapProject(t, m)
	hint := m.tasks.statusHint()
	if !strings.HasPrefix(hint, "[C]apabilities") {
		t.Fatalf("hint = %q, want [C]apabilities first", hint)
	}
}
```

Note on `key("C")`: `tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("C")}.String()` yields `"C"` — check how existing tests in `app_test.go` synthesize key presses (grep `tea.KeyMsg` there) and use the same helper if one exists.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestCKey|TestOverlay|TestStatusHintLeads' -v`
Expected: FAIL — `openOverlay`/`handleKey` undefined; C opens conventions everywhere; hint lacks `[C]`.

- [ ] **Step 3: Implement the overlay in `capabilities.go`**

Add `tea "github.com/charmbracelet/bubbletea"`, `"strings"`, `"github.com/charmbracelet/lipgloss"` to imports as needed, then append:

```go
// openOverlay opens the [C] switcher with the cursor on the current
// capability's row (the last one the user selected), not the top.
func (c *capabilityModel) openOverlay() {
	c.refresh()
	c.open = true
	c.cursor = 0
	for i, e := range c.entries {
		if e.name == c.current {
			c.cursor = i
			break
		}
	}
}

// handleKey consumes every key while the overlay is open. Enter switches
// (enabling first when the row is disabled — one-stroke happy path); space
// toggles enable/disable without switching; Esc/C closes.
func (c *capabilityModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc", "C":
		c.open = false
	case "j", "down":
		if c.cursor < len(c.entries)-1 {
			c.cursor++
		}
	case "k", "up":
		if c.cursor > 0 {
			c.cursor--
		}
	case "T":
		c.m.cycleTheme()
	case "enter":
		if c.cursor < 0 || c.cursor >= len(c.entries) {
			return nil
		}
		e := c.entries[c.cursor]
		if !e.enabled && !e.unmanaged {
			if err := c.m.store.EnableProjectCapability(c.m.projectScope, e.name, c.m.actor); err != nil {
				c.m.showToast("enable " + e.name + ": " + err.Error())
				return nil
			}
			c.m.refreshAll()
		}
		c.switchTo(e.name)
	case " ":
		if c.cursor < 0 || c.cursor >= len(c.entries) {
			return nil
		}
		e := c.entries[c.cursor]
		if e.unmanaged {
			return nil // unmanaged is not enable/disable-able
		}
		if e.enabled {
			if err := c.m.store.DisableProjectCapability(c.m.projectScope, e.name, c.m.actor); err != nil {
				c.m.showToast("disable " + e.name + ": " + err.Error())
				return nil
			}
			if c.current == e.name {
				c.current = "" // resolution re-picks on refresh
			}
		} else {
			if err := c.m.store.EnableProjectCapability(c.m.projectScope, e.name, c.m.actor); err != nil {
				c.m.showToast("enable " + e.name + ": " + err.Error())
				return nil
			}
		}
		cursor := c.cursor
		c.m.refreshAll()
		c.cursor = cursor
		if c.cursor >= len(c.entries) {
			c.cursor = len(c.entries) - 1
		}
	}
	return nil
}

// renderOverlay draws the centered switcher modal. Row shape:
//   ▶ ● workflow     status verbs and boards · 6 boards
func (c *capabilityModel) renderOverlay() string {
	styles := c.m.styles
	nameW := 12
	for _, e := range c.entries {
		if len(e.name) > nameW {
			nameW = len(e.name)
		}
	}
	var body strings.Builder
	for i, e := range c.entries {
		marker := "  "
		if e.name == c.current {
			marker = "▶ "
		}
		state := "● "
		st := styles.Body
		switch {
		case e.unmanaged:
			state = "— "
		case !e.enabled:
			state = "○ "
			st = styles.Muted
		}
		name := fmt.Sprintf("%-*s", nameW, e.name)
		detail := e.summary
		if e.count != "" {
			detail += "  ·  " + e.count
		}
		line := marker + state + name + "  " + detail
		if i == c.cursor {
			line = styles.RowCursor.Render(line)
		} else {
			line = st.Render(line)
		}
		body.WriteString(line)
		body.WriteString("\n")
	}
	body.WriteString("\n")
	body.WriteString(styles.KeyMenuDim.Render("[↑/↓]move  [Enter]switch  [space]enable/disable  [Esc]close"))

	bw := c.m.width * 60 / 100
	if bw < 64 {
		bw = 64
	}
	if bw > c.m.width-4 {
		bw = c.m.width - 4
	}
	bh := len(c.entries) + 5
	_ = lipgloss.Width // keep lipgloss imported only if actually used; drop this line and the import if not
	return titledBoxHeight(styles.DialogBody, bw, "Capabilities", body.String(), bh)
}
```

(If `lipgloss` ends up unused in the final file, remove both the import and the `_ = lipgloss.Width` line.)

- [ ] **Step 4: Route in `app.go`**

1. Overlay key block — insert AFTER the plugin-overlay block (app.go:531-548) and BEFORE the `q` handler (:552):

```go
	// Capabilities switcher consumes keys until closed (Esc/C). T still cycles
	// the theme, mirroring the other overlays.
	if m.capability.open {
		return m.capability.handleKey(k)
	}
```

2. Global `C` case (app.go:596-597) becomes:

```go
		case "C":
			if m.focused == paneTasks && m.projectScope != "" {
				m.capability.openOverlay()
				return nil
			}
			m.openHelp(helpConventions)
			return nil
```

(The `C` handlers inside the help/actors/plugin overlay blocks at :475-481, :518-520, :542-544 stay as conventions — those overlays sit above the workspace, where the conventions toggle remains the correct meaning.)

3. `View()` (app.go:786-804): add after the confirm overlay placement:

```go
	if m.capability.open {
		out = m.placeOverlay(out, m.capability.renderOverlay())
	}
```

- [ ] **Step 5: Hints + keymap**

1. `statusHint` (tasks_list.go:575-589) list-mode return values:

```go
	if t.m.capability.unmanagedCurrent() {
		return "[C]apabilities  [↑/↓]tasks  [Shift-↑/↓]labels  [Shift-→]drill  [s]ort  [Enter]detail  [?]keys"
	}
	return "[C]apabilities  [↑/↓]tasks  [ [ / ] ]board  [s]ort  [a]dd  [p]pin/unpin  [Enter]detail  [?]keys"
```

2. `keymap.go:52` row becomes:

```go
	{"C", "open conventions", "capabilities switcher", "-", "close help overlay"},
```

and add after the Shift-0 row (:45):

```go
	{"Space", "-", "enable/disable capability (in switcher)", "-", "scroll down"},
```

(replacing the existing trailing `{"Space", ...}` row at :56 — merge, don't duplicate the key).

- [ ] **Step 6: Run tests**

Run: `go test ./internal/tui/ -run 'TestCKey|TestOverlay|TestStatusHint' -v` → PASS, then the full `go test ./internal/tui/` (fix any hint-string assertions).

- [ ] **Step 7: Commit**

```bash
git add internal/tui/capabilities.go internal/tui/capabilities_test.go internal/tui/app.go internal/tui/tasks_list.go internal/tui/keymap.go
git commit -m "feat(ATM-90171b): [C] capabilities switcher overlay with enable/disable"
```

---

### Task 9: Cross-capability pins

**Files:**
- Modify: `internal/tui/labels.go` (`loadPins` :320-338, `jumpPin` :403-415)
- Test: `internal/tui/labels_test.go` (append)

**Interfaces:**
- Consumes: `m.capability.switchTo` (Task 3), scoped ring (Task 4).
- Produces: pins survive across capabilities; `jumpPin` switches capability when the pin's owner differs; new `(*boardsModel).ownerOf(full string) string`.

- [ ] **Step 1: Write the failing test**

```go
func TestPinJumpSwitchesCapability(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if _, err := m.regFor("ATM").EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	m.refreshAll()
	m.boards.selectDefault()
	// Pin workflow's all-tasks, then switch to contextmap.
	m.boards.togglePin()
	if len(m.boards.pins) != 1 {
		t.Fatalf("pins = %v, want the selected board pinned", m.boards.pins)
	}
	m.capability.switchTo("contextmap")
	if len(m.boards.pins) != 1 {
		t.Fatalf("pin vanished after capability switch: %v (loadPins must prune against ALL enabled exposed, not the current ring)", m.boards.pins)
	}
	if !m.boards.jumpPin(1) {
		t.Fatalf("jumpPin(1) returned false")
	}
	if m.capability.current != "workflow" {
		t.Fatalf("current = %q after pin jump, want workflow", m.capability.current)
	}
	if m.boards.selected != "ATM:all-tasks" {
		t.Fatalf("selected = %q, want ATM:all-tasks", m.boards.selected)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestPinJumpSwitches -v`
Expected: FAIL — the pin is pruned when the ring no longer contains it.

- [ ] **Step 3: Implement**

1. `loadPins` (labels.go:320-338): prune against every ENABLED capability's exposed labels (minus hidden), not the current ring:

```go
// loadPins reads the project's pins from the boards config and prunes any
// whose board no enabled capability exposes (or that is hidden). Pins are
// GLOBAL across capabilities: a pin may name a board outside the current
// ring — jumpPin switches capability to follow it. Clamped to maxPins.
func (b *boardsModel) loadPins() {
	b.pins = nil
	if b.m.projectScope == "" || b.boardsCfg == nil {
		return
	}
	live := map[string]bool{}
	for _, e := range b.m.regFor(b.m.projectScope).Exposed(b.m.projectScope) {
		live[e.Label.Name] = true
	}
	hidden := map[string]bool{}
	for _, n := range b.boardsCfg.Hidden {
		hidden[n] = true
	}
	for _, full := range b.boardsCfg.Pins {
		if live[full] && !hidden[full] {
			b.pins = append(b.pins, full)
		}
		if len(b.pins) >= maxPins {
			break
		}
	}
	b.syncPinFocus()
}
```

2. `jumpPin` (labels.go:403-415):

```go
// jumpPin moves the selection to the nth pinned board (1-based), switching
// the current capability first when the pin belongs to another one. Returns
// false if n is out of range.
func (b *boardsModel) jumpPin(n int) bool {
	if n < 1 || n > len(b.pins) {
		return false
	}
	full := b.pins[n-1]
	if owner := b.ownerOf(full); owner != "" && owner != b.m.capability.current {
		b.m.capability.switchTo(owner)
	}
	b.selected = full
	b.resetDrill()
	b.pinFocus = n - 1
	b.applyFocus()
	return true
}

// ownerOf returns the enabled capability exposing full, or "".
func (b *boardsModel) ownerOf(full string) string {
	for _, e := range b.m.regFor(b.m.projectScope).Exposed(b.m.projectScope) {
		if e.Label.Name == full {
			return e.Owner
		}
	}
	return ""
}
```

Note `switchTo` internally runs `selectDefault` + `loadPins`; `jumpPin` then overrides `selected`/`pinFocus` and re-applies focus — the double focus application is harmless (same channel, last write wins).

3. `renderPinnedTabs` (thumbnails.go:241-242): the body names `b.selected` which is always in the current ring — unchanged. But `boardDescription` (thumbnails.go:267-274) looks up `b.rows`; that still resolves for the selected board. No change needed.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui/ -run 'TestPin' -v` → PASS (existing pin tests included), then full package.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/labels.go internal/tui/labels_test.go
git commit -m "feat(ATM-90171b): global pins across capabilities; pin jump switches capability"
```

---

### Task 10: Integration sweep

**Files:**
- Modify: whatever the failures name (tests only, plus `internal/tui/help.go` parity table if it references removed strings)

- [ ] **Step 1: Full suite**

Run: `go test ./...`
Expected: everything green except possibly stragglers in `internal/tui` / `internal/cli` goldens that mention the old header, OWNER column, umbrella ring row, or pins-above-strip order. Fix each per the rules established in Tasks 4-9. Do not weaken assertions — update them to pin the NEW behavior.

- [ ] **Step 2: Help/parity surfaces**

Run: `grep -rn "unmanaged" internal/tui/help.go internal/cli/ --include="*.go" | grep -v _test`
If the help parity table or conventions text describes the umbrella ring row ("drill into the umbrella"), update the copy to the capability view ("[C] switches capabilities; unmanaged is a capability"). CLI verbs (`atm capability unmanaged`, `atm project boards ...`) are UNCHANGED.

- [ ] **Step 3: Manual smoke**

Run: `go build ./... && go run ./cmd/atm dev --help >/dev/null 2>&1 || true` then launch the TUI against a scratch store if the environment allows (`ATM_STORE=$(mktemp -d) go run ./cmd/atm tui` — check the actual TUI launch verb with `go run ./cmd/atm --help`). Verify visually: header shows `CAPABILITY:`, C opens the switcher in pane [2], switching to unmanaged shows the drill-down, pins render at the bottom.

- [ ] **Step 4: gofmt + final commit**

```bash
gofmt -l internal/   # must print nothing
go test ./...        # must pass
git add -u internal/ docs/
git commit -m "feat(ATM-90171b): capability view integration sweep — help copy and remaining goldens"
```

(`git add -u` limited to `internal/ docs/` — NEVER `git add -A`; other sessions may be editing this worktree.)

- [ ] **Step 5: Journal to ATM**

Ask the atm-manager agent (or run directly) to comment on ATM-90171b: implementation complete on this branch, listing the task commits.

---

## Self-Review Notes (already applied)

1. **Refresh order** — `capabilityModel.refresh()` runs first in `refreshAll` so ring and header read a resolved `current` (Task 3 Step 4).
2. **`chartCursorMove` rewrite** (Task 5) folds the old shared clamp into per-case bodies because the umbrella case now differs (unset start + selection application); the chart case is behavior-identical.
3. **`lLevelUmbrella` is unreachable outside unmanaged mode** after Task 4 (no ring row enters it), so Task 5's Esc guards can be unconditional.
4. **Type consistency check**: `splitStripWidths(paneW, ringN)` (Task 6) matches its only production caller `renderStrip` (rewritten same task); `boardTableLine` 3-content-arg shape (Task 4) matches both callers in `renderTable`; `taskColumnWidths` 3-return shape (Task 7) matches its only caller `renderFlatList`.
5. **Spec coverage**: §1→T1/T3, §2→T3/T8, §3→T4/T6, §4→T5, §5→T2/T3/T7, §6→T7, §7→T6/T9, §8→T8, §9→T4/T7, §10 edge cases→T3 (resolution tests), T5 (empty unmanaged), T6 (zero-board placeholder in renderStrip — Task 5 Step 4).
