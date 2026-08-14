# Channels-Scoped Concierge Dispatch and the [?] Main Menu — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Dispatching the concierge from the channels overlay launches a session scoped to channel setup, and `[?]` becomes the TUI's single context-aware main menu, freeing the status bar of all key hints.

**Architecture:** Capability scoping flows through the existing `--capability` launcher flag: a new `Session scope` section renders into the session context, the channel capability guide gains the scoped flow the concierge follows, and the dispatch dialog passes `--capability channel` as a context default. The menu is a new `menuModel` overlay driven by one declarative entry table (replacing `keymapRows`); activating a keyed entry replays that key through the normal `handleKey` path so menu and keyboard share one behavior path.

**Tech Stack:** Go, bubbletea + lipgloss (TUI), go:embed markdown (personas/capability guides/session template).

**Spec:** `docs/superpowers/specs/2026-08-14-channels-scoped-dispatch-and-menu-design.md`

## Global Constraints

- ATM ledger task: ATM-3714db. Stamp every ATM mutation with actor `developer@<agent>:<model>`.
- Prose in `.md` files is single un-wrapped lines (no hard column wrapping).
- The universal dispatch dialog never branches on how it was opened; context supplies defaults only (ATM-29f8b0 decision of record).
- The concierge persona file `skills/persona/concierge.md` is NOT modified.
- No new probing/discovery Go code; discovery is prompt guidance in the capability guide.
- Run `go build ./... && go test ./...` before every commit; `gofmt` clean.
- All work on one branch in one worktree; implementers are serialized (subagents share a cwd — never run two implementers with overlapping files at once).

---

### Task 1: Capability segment in the context cache key

**Files:**
- Modify: `internal/cli/launcher_shared.go:150-170` (`contextCachePath`, `cacheKey`)
- Modify: `internal/cli/session.go:167` (the one `contextCachePath` call site — verify with grep, there may be others)
- Test: `internal/cli/launcher_shared_test.go` (or wherever `cacheKey` is currently tested — `grep -rn "cacheKey" internal/cli/*_test.go`)

**Interfaces:**
- Produces: `cacheKey(persona, task, capability string) string` → `session-<persona>[-<task>][-<capability>]`; `contextCachePath(storePath, code, persona, task, capability string) string`. Empty capability produces the exact same key as today (goldens must not change).

- [ ] **Step 1: Write the failing test**

Add to the file that tests `cacheKey` (create `internal/cli/launcher_shared_test.go` if none exists):

```go
func TestCacheKeyCapabilitySegment(t *testing.T) {
	if got := cacheKey("concierge", "", "channel"); got != "session-concierge-channel" {
		t.Errorf("cacheKey with capability = %q, want session-concierge-channel", got)
	}
	if got := cacheKey("concierge", "ATM-3714db", "channel"); got != "session-concierge-atm-3714db-channel" {
		t.Errorf("cacheKey with task+capability = %q", got)
	}
	if got := cacheKey("concierge", "", ""); got != "session-concierge" {
		t.Errorf("cacheKey without capability changed: %q", got)
	}
}
```

- [ ] **Step 2: Run it to make sure it fails**

Run: `go test ./internal/cli/ -run TestCacheKeyCapabilitySegment`
Expected: compile error (`too many arguments to cacheKey`).

- [ ] **Step 3: Implement**

In `internal/cli/launcher_shared.go`:

```go
func contextCachePath(storePath, code, persona, task, capability string) string {
	key := cacheKey(persona, task, capability)
	if code == "" {
		return filepath.Join(storePath, "cache", key+".md")
	}
	return filepath.Join(storePath, "projects", code, "cache", key+".md")
}

// cacheKey builds the filename stem: session-<persona>[-<task>][-<capability>].
// Non-alphanumeric characters collapse to a single "-"; the result is
// lowercased and trimmed of leading/trailing "-".
func cacheKey(persona, task, capability string) string {
	parts := []string{"session", persona}
	if task != "" {
		parts = append(parts, task)
	}
	if capability != "" {
		parts = append(parts, capability)
	}
	for i, p := range parts {
		parts[i] = sanitizeCacheSegment(p)
	}
	return strings.Join(parts, "-")
}
```

Update every call site: `grep -rn "contextCachePath(\|cacheKey(" internal/cli/` — `internal/cli/session.go:167` becomes `contextCachePath(s.StorePath(), code, spec.Name, opts.Task, opts.Capability)`; fix any test callers with `""`.

- [ ] **Step 4: Run the package tests**

Run: `go test ./internal/cli/`
Expected: PASS (existing goldens unchanged because empty capability yields the old key).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/launcher_shared.go internal/cli/launcher_shared_test.go internal/cli/session.go
git commit -m "feat(ATM-3714db): capability segment in session context cache key"
```

---

### Task 2: Session scope section in the rendered context

**Files:**
- Modify: `internal/session/context.go` (ContextData + RenderContext)
- Modify: `internal/session/context_v1.md` (add `<CAPABILITY_SCOPE>` placeholder)
- Modify: `internal/cli/session.go:169-173` (launchSession ContextData) and `:300-304` (renderSessionContext ContextData)
- Test: `internal/session/context_test.go`

**Interfaces:**
- Consumes: nothing from Task 1 (independent; both thread `opts.Capability`).
- Produces: `session.ContextData.Capability string`. When set, the rendered context contains a `## Session scope` section between the persona prompt and `## Orientation`; when empty, no `<CAPABILITY_SCOPE>` residue remains.

- [ ] **Step 1: Write the failing test**

Add to `internal/session/context_test.go`:

```go
func TestRenderContextCapabilityScope(t *testing.T) {
	d := ContextData{Code: "ATM", Name: "Acme", Actor: "concierge@claude:test", Capability: "channel", PersonaPrompt: "PROMPT"}
	out := RenderContext(d)
	for _, want := range []string{
		"## Session scope",
		"scoped to the `channel` capability",
		"atm capability channel guide",
		"for project ATM",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("scoped context missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "<CAPABILITY_SCOPE>") {
		t.Error("placeholder leaked into scoped render")
	}
	if idx := strings.Index(out, "## Session scope"); idx > strings.Index(out, "## Orientation") {
		t.Error("scope section must precede Orientation")
	}
}

func TestRenderContextNoCapabilityNoResidue(t *testing.T) {
	out := RenderContext(ContextData{Code: "ATM", Actor: "a@b:c", PersonaPrompt: "PROMPT"})
	if strings.Contains(out, "CAPABILITY_SCOPE") || strings.Contains(out, "Session scope") {
		t.Errorf("unscoped context must carry no scope residue:\n%s", out)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/session/ -run TestRenderContext`
Expected: FAIL (`unknown field Capability`).

- [ ] **Step 3: Implement**

`internal/session/context_v1.md` — insert the placeholder between the persona prompt and Orientation, exactly:

```
<PERSONA_PROMPT>

<CAPABILITY_SCOPE>

## Orientation
```

`internal/session/context.go`:

```go
type ContextData struct {
	Code          string
	Name          string
	Actor         string
	TaskID        string
	Capability    string
	PersonaPrompt string
}

// capabilityScopeSection is the Session scope block rendered when a session
// launches with --capability. It is capability-agnostic; capability-specific
// flow lives in that capability's guide. <CODE> is substituted (or left as
// the literal placeholder, like the rest of the template) by RenderContext's
// main replacer.
func capabilityScopeSection(capability string) string {
	return "## Session scope\n\nThis session is scoped to the `" + capability + "` capability. Skip any general onboarding or survey flow your persona defines — orient only as far as this capability needs. Read `atm capability " + capability + " guide` first, then work solely on this capability's setup and health for project <CODE>, and hand off when it is healthy."
}
```

In `RenderContext`, before building `pairs`, resolve the placeholder (the template stays literal-placeholder-friendly for every other field):

```go
tmpl := contextV1
if d.Capability == "" {
	tmpl = strings.Replace(tmpl, "<CAPABILITY_SCOPE>\n\n", "", 1)
} else {
	tmpl = strings.Replace(tmpl, "<CAPABILITY_SCOPE>", capabilityScopeSection(d.Capability), 1)
}
```

…and run the existing replacer over `tmpl` instead of `contextV1` (so `<CODE>` inside the section is filled by the same rules).

Thread the field in `internal/cli/session.go`: launchSession's `session.ContextData{...}` gains `Capability: opts.Capability`; `renderSessionContext`'s `data := session.ContextData{...}` gains `Capability: capability`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/session/ ./internal/cli/`
Expected: PASS. If a cli golden captures a rendered context, regenerate only if its launch used `--capability` (none do today).

- [ ] **Step 5: Commit**

```bash
git add internal/session/context.go internal/session/context_v1.md internal/session/context_test.go internal/cli/session.go
git commit -m "feat(ATM-3714db): render Session scope section for --capability launches"
```

---

### Task 3: Scoped-session flow in the channel capability guide

**Files:**
- Modify: `skills/capability/channel.md`

**Interfaces:**
- Consumes: the Session scope section's instruction "Read `atm capability <name> guide` first" (Task 2).
- Produces: a `## Scoped session` markdown section the concierge follows.

- [ ] **Step 1: Add the section**

Append to `skills/capability/channel.md` after the `## Converge` section (single un-wrapped lines per repo doc style):

```markdown
## Scoped session

A session dispatched with `--capability channel` (for example from the TUI channels overlay) exists to make this project's channels healthy and nothing else. Run `atm channel list --project <CODE> --output json` first and branch on what it shows:

- **No records.** Interview the user in their own words about where work flows — repositories, Notion workspaces, other surfaces — one question at a time. In parallel, discover candidates yourself: scan the working directory, its parent, and sibling checkouts/worktrees for git repositories and note each one's remote URL. Propose every candidate (remote URL as the address, folder as the wiring) and confirm before acting: `atm channel add`, then `atm channel wire`, verify you can actually reach it, then `atm channel stamp`.
- **Records without wiring (fresh machine).** The ledger already knows this project's channels; only this machine's wiring is missing. For each unwired record, search local checkouts for a repository whose remote matches the record's address and propose `atm channel wire --path` for it; offer to clone the recorded URL when nothing matches. For a notion record, help the user install and authorize the agent-side MCP server, then `atm channel wire --mcp-server`. Re-stamp each channel after verifying it.
- **Partial or stale.** For every `◐`/`○` entry with wiring, read its status note and repair it: re-verify and re-stamp fresh ones, re-wire moved paths, surface dirty or diverged repos to the user rather than fixing them silently.

Never expose flag shapes to the user — speak in plain words and run the commands yourself. Never ask for tokens or passwords; authorization lives in the agent's own tooling. Hand off with a one-paragraph summary of what is now wired, stamped, and still outstanding.
```

- [ ] **Step 2: Verify the guide renders and nothing else broke**

Run: `go build ./... && go test ./...`
Expected: PASS. If a golden captures `atm capability channel guide` output, update it (`grep -rln "Scoped session\|channel capability" internal/cli/testdata/golden/` to find candidates; regenerate per the repo's golden-update convention found in the failing test's message).

- [ ] **Step 3: Commit**

```bash
git add skills/capability/channel.md
git commit -m "docs(ATM-3714db): channel guide — scoped session flow with discovery and re-wiring"
```

---

### Task 4: Dispatch dialog carries the capability scope (and always passes a known project)

**Files:**
- Modify: `internal/tui/dispatch.go` (struct, `open`, `submit`, `renderOverlay`)
- Modify: `internal/tui/app.go:754` (`openDispatch` call site)
- Modify: `internal/tui/projects.go:276-278` and `:336-339` (persona-drill `d`/`D` call sites)
- Modify: `internal/tui/channels.go:83-86` (`c` dispatch)
- Test: `internal/tui/dispatch_test.go`, `internal/tui/channels_test.go`

**Interfaces:**
- Consumes: `--capability` launcher flag (exists), Task 2's scoped rendering.
- Produces: `dispatchModel.open(defaultPersona, project, taskID, taskTitle, capability string)`; argv gains `--capability <name>` when set, and `--project <code>` for any non-admin persona whenever `project != ""`.

- [ ] **Step 1: Write the failing tests**

In `internal/tui/channels_test.go` (model helpers `newTestModel`, `seedChannels`, `fakeDispatcher` already exist — see `dispatch_test.go:18-33` for the fake):

```go
func TestChannelsDispatchIsCapabilityScoped(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedChannels(t, m)
	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	m.channelsOv.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if !m.dispatchDlg.active {
		t.Fatal("c must open the dispatch dialog")
	}
	view := m.dispatchDlg.renderOverlay()
	if !strings.Contains(view, "Scope:") || !strings.Contains(view, "channel") {
		t.Errorf("dialog must show the capability scope line:\n%s", view)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatalf("spawned %d, want 1", len(fd.spawned))
	}
	argv := strings.Join(fd.spawned[0].Argv, " ")
	for _, want := range []string{"--persona concierge", "--project ATM", "--capability channel"} {
		if !strings.Contains(argv, want) {
			t.Errorf("argv missing %q: %s", want, argv)
		}
	}
}
```

In `internal/tui/dispatch_test.go`, add coverage that an unscoped open renders no Scope line and appends no `--capability`:

```go
func TestDispatchNoCapabilityByDefault(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.openDispatch() // empty workspace → concierge, no project, no capability
	if v := m.dispatchDlg.renderOverlay(); strings.Contains(v, "Scope:") {
		t.Errorf("unscoped dialog must not render a Scope line:\n%s", v)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if argv := strings.Join(fd.spawned[0].Argv, " "); strings.Contains(argv, "--capability") {
		t.Errorf("unscoped argv must omit --capability: %s", argv)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run 'TestChannelsDispatchIsCapabilityScoped|TestDispatchNoCapabilityByDefault'`
Expected: compile error (`open` arity) after updating the test; first make it fail on arity by writing tests against the new 5-arg signature.

- [ ] **Step 3: Implement**

`internal/tui/dispatch.go`:

1. Add field `capability string` to `dispatchModel` (after `taskTitle`).
2. `open` signature: `func (d *dispatchModel) open(defaultPersona, project, taskID, taskTitle, capability string)`; first line becomes `d.project, d.taskID, d.taskTitle, d.capability = project, taskID, taskTitle, capability`.
3. In `submit()`, replace the argv project/task block:

```go
argv := []string{"atm", "--persona", p.Name}
// A known project rides along for every non-admin persona — a
// project-optional persona still accepts --project, and a scoped session
// (e.g. concierge from the channels overlay) needs it to render a
// project-bound context. admin routes to a fresh TUI that ignores it.
if d.project != "" && p.Name != "admin" {
	argv = append(argv, "--project", d.project)
}
if p.Name != "admin" {
	argv = append(argv, "--agent", a.name)
}
// --task rides only with --project: the CLI launcher rejects
// "--task requires --project".
if d.taskID != "" && d.project != "" && p.Name != "admin" {
	argv = append(argv, "--task", d.taskID)
}
if d.capability != "" && p.Name != "admin" {
	argv = append(argv, "--capability", d.capability)
}
```

4. In `renderOverlay()`, after the Task block (`if d.taskID != "" {...}`):

```go
if d.capability != "" {
	b.WriteString("Scope:  " + d.capability + " capability\n\n")
}
```

Call sites: `app.go:754` → `m.dispatchDlg.open(persona, project, taskID, taskTitle, "")`; both `projects.go` sites append `""`; `channels.go:85` → `c.m.dispatchDlg.open("concierge", c.project, "", "", "channel")`.

- [ ] **Step 4: Run the full TUI package**

Run: `go test ./internal/tui/`
Expected: the new tests PASS. Existing tests that asserted a project-optional persona's argv omits `--project` (`dispatch_test.go` around lines 676-681) still pass if they open with no project; any test that opened concierge WITH a project and asserted no `--project` now legitimately changes — update its expectation to include `--project`, citing the spec's bug-fix paragraph.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/dispatch.go internal/tui/app.go internal/tui/projects.go internal/tui/channels.go internal/tui/dispatch_test.go internal/tui/channels_test.go
git commit -m "feat(ATM-3714db): dispatch dialog carries capability scope; known project always rides argv"
```

---

### Task 5: The declarative menu entry table

**Files:**
- Rewrite: `internal/tui/keymap.go` (delete `keymap`, `defaultKeymap`, `keyEntry`, `keymapRows`; add the entry table)
- Modify: `internal/tui/help.go` (replace `keymapTable()` with a flat renderer over the new table)
- Test: `internal/tui/keymap_test.go` (new)

**Interfaces:**
- Produces (consumed by Tasks 6-7): 

```go
type menuScope int
const (
	scopeGlobal menuScope = iota
	scopeProjectsList
	scopeProjectsDetail
	scopeProjectsDrill
	scopeTasksList
	scopeTasksDetail
	scopeBoards
)

type menuSection int
const (
	sectionActions menuSection = iota
	sectionViews
	sectionReference
)

type refKind int
const (
	refNone refKind = iota
	refKeymap
	refParity
	refConventions
)

type menuEntry struct {
	key          string      // display + replay string; "" for reference entries
	label        string
	scopes       []menuScope // which contexts show it under Actions; nil for Views/Reference
	section      menuSection
	ref          refKind
	hidden       bool // keymap-reference-only (navigation pairs); never a menu row
	needsProject bool // shown only when a project scope exists (capabilities switcher)
}

var menuEntries = []menuEntry{ ... }
func keyMsgFromString(s string) tea.KeyMsg
func keymapReferenceText() string // flat Key | Where | Action table
```

- [ ] **Step 1: Write the failing tests**

`internal/tui/keymap_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run TestMenuEntries`
Expected: compile error (types undefined).

- [ ] **Step 3: Implement the table**

Replace `internal/tui/keymap.go`'s contents with the types above plus the table. Transcribe entries from the old `keymapRows` (`keymap.go:24-63`) and the three deleted `statusHint` implementations (`projects.go:1332-1346`, `tasks_list.go:655-673`, `labels.go:1698-1713`). The table, entry by entry:

```go
var menuEntries = []menuEntry{
	// Views (global overlays and panes; needsProject gates the capabilities switcher)
	{key: "D", label: "Dispatch a session", section: sectionViews},
	{key: "E", label: "Channels", section: sectionViews},
	{key: "V", label: "Personas", section: sectionViews},
	{key: "C", label: "Capabilities", section: sectionViews, needsProject: true},
	{key: "T", label: "Cycle theme", section: sectionViews},
	{key: "1", label: "Projects pane", section: sectionViews},
	{key: "2", label: "Tasks pane", section: sectionViews},

	// Actions — projects list
	{key: "a", label: "Add project", scopes: []menuScope{scopeProjectsList}, section: sectionActions},
	{key: "s", label: "Select project", scopes: []menuScope{scopeProjectsList}, section: sectionActions},
	{key: "x", label: "Remove project", scopes: []menuScope{scopeProjectsList}, section: sectionActions},
	{key: "ctrl+right", label: "Drill into persona activity", scopes: []menuScope{scopeProjectsList}, section: sectionActions},

	// Actions — projects detail
	{key: "N", label: "Set project name", scopes: []menuScope{scopeProjectsDetail}, section: sectionActions},
	{key: "H", label: "Toggle history", scopes: []menuScope{scopeProjectsDetail}, section: sectionActions},
	{key: "c", label: "Toggle capability (switcher)", scopes: []menuScope{scopeProjectsDetail}, section: sectionActions},
	{key: "x", label: "Remove project", scopes: []menuScope{scopeProjectsDetail}, section: sectionActions},

	// Actions — projects persona drill
	{key: "d", label: "Dispatch this persona", scopes: []menuScope{scopeProjectsDrill}, section: sectionActions},
	{key: "ctrl+left", label: "Back from persona detail", scopes: []menuScope{scopeProjectsDrill}, section: sectionActions},

	// Actions — tasks list
	{key: "a", label: "Add task", scopes: []menuScope{scopeTasksList}, section: sectionActions},
	{key: "s", label: "Cycle sort", scopes: []menuScope{scopeTasksList}, section: sectionActions},
	{key: "S", label: "Re-ensure capability vocabulary", scopes: []menuScope{scopeTasksList}, section: sectionActions},
	{key: "p", label: "Pin/unpin board", scopes: []menuScope{scopeTasksList}, section: sectionActions},

	// Actions — task detail
	{key: "e", label: "Edit title", scopes: []menuScope{scopeTasksDetail}, section: sectionActions},
	{key: "d", label: "Edit description", scopes: []menuScope{scopeTasksDetail}, section: sectionActions},
	{key: "b", label: "Add label", scopes: []menuScope{scopeTasksDetail}, section: sectionActions},
	{key: "B", label: "Remove label", scopes: []menuScope{scopeTasksDetail}, section: sectionActions},
	{key: "M", label: "Add comment", scopes: []menuScope{scopeTasksDetail}, section: sectionActions},
	{key: "H", label: "History overlay", scopes: []menuScope{scopeTasksDetail}, section: sectionActions},
	{key: "x", label: "Remove task", scopes: []menuScope{scopeTasksDetail}, section: sectionActions},

	// Actions — boards
	{key: "n", label: "New board", scopes: []menuScope{scopeBoards}, section: sectionActions},
	{key: "e", label: "Edit board", scopes: []menuScope{scopeBoards}, section: sectionActions},
	{key: "d", label: "Describe label", scopes: []menuScope{scopeBoards}, section: sectionActions},
	{key: "l", label: "Remove label", scopes: []menuScope{scopeBoards}, section: sectionActions},
	{key: "S", label: "Seed vocabulary", scopes: []menuScope{scopeBoards}, section: sectionActions},

	// Reference (no keys; open menu detail views)
	{label: "Keymap reference", section: sectionReference, ref: refKeymap},
	{label: "CLI ↔ TUI parity", section: sectionReference, ref: refParity},
	{label: "Conventions", section: sectionReference, ref: refConventions},

	// Hidden: documented in the keymap reference, never menu rows.
	{key: "j/k", label: "Move cursor / scroll", hidden: true},
	{key: "g", label: "Top of list · plugin leader prefix", hidden: true},
	{key: "enter", label: "Open detail / confirm", hidden: true},
	{key: "esc", label: "Back / close overlay", hidden: true},
	{key: "[ / ]", label: "Prev/next board or page", hidden: true},
	{key: "shift+up/down", label: "Feed scroll / thumbnail cursor", hidden: true},
	{key: "shift+right/left", label: "Feed page / thumbnail drill", hidden: true},
	{key: "pgup/pgdown", label: "Page list / scroll detail", hidden: true},
	{key: "ctrl+up/down", label: "Scroll persona chart", hidden: true},
	{key: "A", label: "Toggle project art", hidden: true},
	{key: "space", label: "Toggle capability / scroll", hidden: true},
	{key: "!1..!9 / !0", label: "Jump to pinned / center board", hidden: true},
	{key: "q / ctrl+c", label: "Quit", hidden: true},
}
```

Adjust hidden-entry key strings freely — they are display-only, and the round-trip test skips `hidden` entries.

`keyMsgFromString`:

```go
// keyMsgFromString synthesizes the tea.KeyMsg whose String() equals s, for
// menu replay. Only keys that appear in menuEntries need handling; the
// round-trip test enforces the mapping against the vendored bubbletea.
func keyMsgFromString(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+right":
		return tea.KeyMsg{Type: tea.KeyCtrlRight}
	case "ctrl+left":
		return tea.KeyMsg{Type: tea.KeyCtrlLeft}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}
```

(Verify `tea.KeyCtrlRight`/`tea.KeyCtrlLeft` exist in the vendored bubbletea — `grep -rn "KeyCtrlRight" $(go env GOMODCACHE)/github.com/charmbracelet/bubbletea*/key.go` or check `go doc`. If absent, drop the two ctrl entries from Actions into `hidden` and note it in the commit message; the drill remains reachable by key.)

`keymapReferenceText()` in `help.go` replaces `keymapTable()` — flat table, grouped: global/views entries first, then per-scope groups, then hidden entries, columns `Key | Where | Action` via `fmt.Fprintf("%-18s %-18s %s\n", ...)`, where `Where` is a scope display name (`"global"`, `"projects"`, `"projects detail"`, `"persona drill"`, `"tasks"`, `"task detail"`, `"boards"`, `"—"` for hidden). Delete `keymapTable` and its `keymapRows` dependency.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui/ -run 'TestMenuEntries|TestKeymapReference'`
Expected: PASS. Then `go build ./...` — expect ONE remaining compile error at `help.go:52` (`keymapTable` deleted); point that call at `keymapReferenceText()` so the old help overlay keeps compiling until Task 7 removes it.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/keymap.go internal/tui/keymap_test.go internal/tui/help.go
git commit -m "feat(ATM-3714db): declarative menu entry table replaces keymapRows"
```

---

### Task 6: menuModel — the [?] overlay

**Files:**
- Create: `internal/tui/menu.go`
- Test: `internal/tui/menu_test.go`

**Interfaces:**
- Consumes: `menuEntries`, `keyMsgFromString`, `keymapReferenceText` (Task 5); `parityTable`, `renderConventionsText`, `conventionsTextTUI` (existing, `help.go`); `titledBoxHeight`, `styles.DialogBody`, `fitLine`, `padToHeight` (existing box helpers).
- Produces: `menuModel` with `openMenu()`, `handleKey(tea.KeyMsg) tea.Cmd`, `renderOverlay() string`, field `open bool`. Model integration happens in Task 7; tests in this task drive `menuModel` directly with a constructed `*Model`.

- [ ] **Step 1: Write the failing tests**

`internal/tui/menu_test.go`:

```go
func TestMenuListsScopedActions(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.focused = paneTasks

	m.menu.openMenu()
	if !m.menu.open {
		t.Fatal("openMenu must open")
	}
	view := m.menu.renderOverlay()
	for _, want := range []string{"Add task", "Channels", "Capabilities", "Keymap reference", "Conventions", "[D]", "[E]"} {
		if !strings.Contains(view, want) {
			t.Errorf("menu missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Add project") {
		t.Errorf("projects-scope action leaked into tasks-focused menu:\n%s", view)
	}
	if strings.Contains(view, "Quit") {
		t.Errorf("hidden entries must not render as menu rows:\n%s", view)
	}
}

func TestMenuHidesProjectGatedViewsWithoutProject(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.menu.openMenu()
	if view := m.menu.renderOverlay(); strings.Contains(view, "Capabilities") {
		t.Errorf("needsProject entry shown without a project scope:\n%s", view)
	}
}

func TestMenuActivationReplaysKey(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedChannels(t, m)
	m.menu.openMenu()
	// Walk the cursor to the Channels entry, then activate with Enter.
	for i := 0; i < 50 && m.menu.selectedLabel() != "Channels"; i++ {
		m.menu.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	}
	if m.menu.selectedLabel() != "Channels" {
		t.Fatal("could not reach the Channels entry")
	}
	m.menu.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.menu.open {
		t.Error("activation must close the menu")
	}
	if !m.channelsOv.open {
		t.Error("activating Channels must open the channels overlay (replay through handleKey)")
	}
}

func TestMenuReferenceDrillAndBack(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.menu.openMenu()
	for i := 0; i < 50 && m.menu.selectedLabel() != "Conventions"; i++ {
		m.menu.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	}
	m.menu.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.menu.open || m.menu.view != refConventions {
		t.Fatal("reference entry must open its detail view in-menu")
	}
	if view := m.menu.renderOverlay(); !strings.Contains(view, "What ATM is") {
		t.Errorf("conventions detail missing content:\n%s", view)
	}
	m.menu.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.menu.open || m.menu.view != refNone {
		t.Error("esc from detail must return to the menu list, not close")
	}
	m.menu.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.menu.open {
		t.Error("esc from list must close the menu")
	}
}
```

(`m.menu` lands on Model in this task — see step 3; the app-level `?` binding is Task 7.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run TestMenu`
Expected: compile error (`m.menu` undefined).

- [ ] **Step 3: Implement**

`internal/tui/menu.go` (pattern: `channelsModel` — sub-model with `handleKey`/`renderOverlay`, box math copied from `channels.go:95-100`):

```go
// menuModel is the [?] main menu overlay: the single discovery surface for
// every TUI action. Rows are built at open from menuEntries filtered by the
// model's current scope; activating a keyed entry closes the menu and
// replays that key through Model.handleKey so the menu can never drift from
// the real bindings. Reference entries drill into scrollable detail views.
type menuModel struct {
	m      *Model
	open   bool
	cursor int
	rows   []menuRow
	view   refKind // refNone = the list; otherwise the open detail view
	offset int
	lines  []string // detail content, one string per line
}

// menuRow is one rendered line: a section header (entry == nil) or an entry.
type menuRow struct {
	header string
	entry  *menuEntry
}
```

`currentScopes` on Model (in `menu.go`; it reads pane state the way `overlayProject` does):

```go
// currentScopes resolves the focused pane and its drill state into the menu
// scopes whose Actions apply right now.
func (m *Model) currentScopes() []menuScope {
	switch m.focused {
	case paneProjects:
		if m.projects.personaDrilled {
			return []menuScope{scopeProjectsDrill}
		}
		if m.projects.view == pViewDetail {
			return []menuScope{scopeProjectsDetail}
		}
		return []menuScope{scopeProjectsList}
	case paneTasks:
		if m.tasks.view == tViewDetail {
			return []menuScope{scopeTasksDetail}
		}
		return []menuScope{scopeTasksList}
	}
	return nil
}
```

(Boards: `scopeBoards` applies when the tasks pane is showing the boards level — locate the boards-active predicate by finding what routes keys to `boardsModel` (`grep -n "boards\." internal/tui/tasks_list.go | head`) and include `scopeBoards` from that state. If boards routing proves not cleanly detectable from Model state, ship without live `scopeBoards` filtering — the entries stay reachable in the keymap reference — and note it in the commit message.)

`openMenu` builds rows: `sectionActions` entries whose `scopes` intersect `currentScopes()` under an `"Actions"` header (header omitted when empty), then `sectionViews` entries (skip `needsProject` ones when `m.projectScope == ""` and, for the Projects-pane resolution, `overlayProject() == ""`), then `sectionReference` under `"Reference"`. Skip `hidden` entries everywhere. Cursor starts on the first entry row.

`handleKey`: `j/down`/`k/up` move the cursor skipping header rows (in detail view they scroll `offset` like `channelsModel`); `enter`/`right` on a keyed entry → `mm.open = false; return mm.m.handleKey(keyMsgFromString(e.key))`; on a reference entry → set `view`, build `lines` (refKeymap → `keymapReferenceText()`; refParity → `parityTable`; refConventions → `renderConventionsText(mm.m.styles, bw-4, conventionsTextTUI)`), `offset = 0`; `esc`/`left` → detail-to-list, list-to-close; `g` → top of current view.

`selectedLabel() string` returns the cursor row's entry label ("" for headers) — used by tests.

`renderOverlay`: same box math as `channelsModel.renderOverlay` (60% width, min 64). List rows: headers via `styles.FieldHint`/section-divider style, entry rows `cursorGlyph + label` padded so `[key]` right-aligns to the inner width (`fitLine` on long labels). Detail: `offset`-scrolled `lines` window, `padToHeight`. Footer line list: `[↑/↓]move  [Enter/→]open  [Esc]close`; detail: `[j/k]scroll  [Esc]back`. Title: `"Menu"`; detail titles `"Menu — Keymap"`, `"Menu — CLI ↔ TUI"`, `"Menu — Conventions"`.

Add the field to Model (`app.go`, next to `channelsOv`): `menu menuModel`, initialized where the other sub-models are wired (`grep -n "channelsOv = \|channelsOv:" internal/tui/app.go` and mirror it: `m.menu = menuModel{m: m}`).

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui/ -run TestMenu`
Expected: PASS (`TestMenuActivationReplaysKey` exercises replay through the real `handleKey` — the `E` binding is still live from the global switch).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/menu.go internal/tui/menu_test.go internal/tui/app.go
git commit -m "feat(ATM-3714db): [?] main menu overlay with key replay and reference views"
```

---

### Task 7: Wire the menu in, delete the help overlay and every status-bar hint

**Files:**
- Modify: `internal/tui/app.go` (handleKey `:502-716`, View `:930-978`, `workspaceIdle` `~:397`, `renderStatusLine` `:1020-1060`; delete `helpOverlayKind` `:27-33`, `helpOverlay` field, `openHelp`/`closeHelp`/`helpBoxSize`/`renderHelpOverlay`, `statusHint` `:995-1004`)
- Modify: `internal/tui/help.go` (delete `helpModel` and its methods; keep `parityTable`, `renderConventionsText`, `isNumberedItem`, `conventionsTextTUI`, `keymapReferenceText`)
- Modify: `internal/tui/projects.go:1332-1346`, `internal/tui/tasks_list.go:655-673`, `internal/tui/labels.go:1698-1713` (delete the three `statusHint` methods)
- Test: `internal/tui/app_test.go` (or a new `internal/tui/statusline_test.go`), existing tests that reference help/statusHint

**Interfaces:**
- Consumes: `menuModel` (Task 6).
- Produces: `?` opens the menu everywhere (global, and from within the plugin overlay); `C` opens only the capabilities switcher (project-scoped tasks context) and is otherwise a no-op; the status bar right cluster is `[?]menu` + version + refresh.

- [ ] **Step 1: Write the failing tests**

```go
func TestQuestionMarkOpensMenuAndStatusBarIsClean(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if !m.menu.open {
		t.Fatal("? must open the menu")
	}
	m.menu.handleKey(tea.KeyMsg{Type: tea.KeyEsc})

	for _, focused := range []workspacePane{paneProjects, paneTasks} {
		m.focused = focused
		line := m.renderStatusLine()
		if !strings.Contains(line, "[?]menu") {
			t.Errorf("status line must advertise [?]menu: %s", line)
		}
		for _, stale := range []string{"[C]conv", "[T]theme", "[?]help", "[a]dd", "[e]title", "Ctrl+Shift"} {
			if strings.Contains(line, stale) {
				t.Errorf("status line still advertises %q: %s", stale, line)
			}
		}
	}
}

func TestConventionsKeyRemoved(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	// No project scope, projects pane: C used to open conventions help.
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("C")})
	if m.menu.open {
		t.Error("C must not open any overlay outside a project-scoped tasks context")
	}
	if m.capability.open {
		t.Error("C without project scope must not open the capabilities switcher")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run 'TestQuestionMarkOpensMenu|TestConventionsKeyRemoved'`
Expected: FAIL (`?` opens the old help overlay; status line carries `[C]conv`).

- [ ] **Step 3: Implement**

In `app.go` `handleKey`:

1. Replace the help-overlay block (`:517-545`) with the menu block, in the same position (menu consumes keys first):

```go
	// Menu overlay consumes keys until closed. T still cycles the theme.
	if m.menu.open {
		if k.String() == "T" {
			m.cycleTheme()
			return nil
		}
		return m.menu.handleKey(k)
	}
```

2. Plugin-overlay branch (`:571-588`): replace the `case "?"` body with `m.menu.openMenu(); return nil`; delete the `case "C"` branch.
3. Global switch (`:642-680`): `case "?"` → `m.menu.openMenu(); return nil`. `case "C"` → keep only the capabilities branch and make the fallback a no-op:

```go
	case "C":
		if m.focused == paneTasks && m.projectScope != "" {
			m.capability.openOverlay()
		}
		return nil
```

4. Delete `helpOverlayKind`/`helpNone`/`helpKeys`/`helpConventions`, the `helpOverlay` and `help` Model fields, `openHelp`, `closeHelp`, `helpBoxSize`, `renderHelpOverlay`, and `newHelpModel` call sites. In `View()` replace the `m.helpOverlay != helpNone` gate with `if m.menu.open { out = m.placeOverlay(out, m.menu.renderOverlay()) }` and update `workspaceIdle()`'s condition (the comment at `:944-949` demands the two stay in sync) — `m.helpOverlay == helpNone` becomes `!m.menu.open`.
5. `renderStatusLine`: delete the `statusHint` block (`:1032-1034`) and change the right cluster (`:1041`) to `m.styles.KeyMenu.Render("[?]menu")`. Delete `Model.statusHint` (`:995-1004`).
6. Delete the three pane `statusHint` methods. Then `go build ./...` and chase every remaining reference (`grep -rn "statusHint\|openHelp\|helpOverlay\|helpKeys\|helpConventions\|newHelpModel\|helpModel" internal/tui/` must come back empty apart from menu.go's refKind names).
7. In `help.go`, delete `helpModel`, `newHelpModel`, `SetSize`, `refresh`, `clampOffset`, `handleKey`, `View`; keep the pure content helpers Task 5/6 consume. If little remains, rename the file `reference.go` in the same commit (`git mv`).

- [ ] **Step 4: Fix fallout, then run the whole repo**

Existing tests that press `?`/`C` expecting help, or assert status-bar hints (`grep -rln "helpOverlay\|\[C\]conv\|statusHint\|Help - Keys" internal/tui/*_test.go`), get updated to the menu behavior or deleted where they tested deleted machinery (e.g. a `grouped_footer_test.go` hint assertion). `channels.go:50`'s `esc, E` close behavior and its footer hints inside overlays are unchanged.

Run: `go build ./... && go test ./...`
Expected: PASS, gofmt clean.

- [ ] **Step 5: Commit**

```bash
git add -A internal/tui/
git commit -m "feat(ATM-3714db): [?] menu replaces help overlay; status bar freed of key hints"
```

---

### Task 8: Docs, changelog, and the final sweep

**Files:**
- Modify: `README.md` (TUI keys section if one exists — `grep -n "\[?\]\|keymap\|status bar" README.md`), `CHANGELOG.md`
- Verify: whole repo

**Interfaces:** none — documentation and verification.

- [ ] **Step 1: Update docs**

CHANGELOG entry (match the existing entry style at the top of `CHANGELOG.md`) covering: `[?]` main menu replacing the help overlay and all status-bar key hints; conventions binding removed (menu-only); channels-overlay concierge dispatch now scoped via `--capability channel`; `--capability` renders a Session scope section; dispatch argv now carries `--project` for any non-admin persona when a project is known. Update any README key tables to match.

- [ ] **Step 2: Full verification**

Run: `go build ./... && go test ./... && gofmt -l .`
Expected: build ok, all tests pass, no unformatted files. Manually smoke the TUI against a COPY of the store (never `~/.config/atm` directly — a schema-changing dev build against the shared cache breaks the installed binary): `cp -r ~/.config/atm /tmp/atm-store-copy && ATM_HOME=/tmp/atm-store-copy go run . tui` — press `?`, walk the menu, activate Channels, press `c`, confirm the dialog shows `Scope: channel capability`.

- [ ] **Step 3: Commit and journal**

```bash
git add README.md CHANGELOG.md
git commit -m "docs(ATM-3714db): README and CHANGELOG for scoped dispatch and [?] menu"
```

Journal completion on ATM-3714db (`atm task comment add --task ATM-3714db --actor developer@<agent>:<model> --label ATM:comment:progress --body "..."`) and follow the repo's finishing-a-development-branch flow.
