# ATM TUI Theme Refresh Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a runtime-only theme system and visual refresh for the ATM Bubble Tea TUI.

**Architecture:** Add a semantic `Theme`/`Styles` layer owned by the root TUI model, then migrate render paths from package-global styles to model-derived styles. Keep theme state in memory only and preserve all store, filter, label, and task behavior.

**Tech Stack:** Go 1.22+, Bubble Tea, Lip Gloss, existing `internal/tui` tests, `make verify`.

## Global Constraints

- Themes are runtime-only and write nothing to `$ATM_HOME`.
- No store changes.
- No label or task behavior changes.
- No new TUI tabs, panes, or data model concepts.
- No CLI flags or config commands for themes.
- No mouse-driven redesign.
- `T` cycles themes only in normal navigation contexts; form fields and filter input keep normal text entry.
- Final completion requires `make verify`.

---

## File Structure

- Create `internal/tui/theme.go`: theme names, semantic palette, built-in themes, style builder, theme cycle helpers.
- Modify `internal/tui/styles.go`: keep layout helpers; remove package-global style variables after migration.
- Modify `internal/tui/app.go`: add active theme state, initialize styles, handle theme cycling, render theme in status, use style fields.
- Modify `internal/tui/form.go`: render through a `Styles` value and move ad hoc error/hint styles into semantic styles.
- Modify `internal/tui/projects.go`, `internal/tui/tasks.go`, `internal/tui/labels.go`, `internal/tui/help.go`: replace global style references with `m.styles` roles and add small layout polish.
- Modify `internal/tui/keymap.go`: document `T`.
- Modify `internal/tui/app_test.go`: add state-preservation, theme cycling, overlay, and chip rendering tests while keeping existing substring behavior tests.
- Modify `internal/tui/labels_test.go`: update any style-sensitive assertions that fail after semantic style migration.

---

### Task 1: Theme Infrastructure

**Files:**
- Create: `internal/tui/theme.go`
- Modify: `internal/tui/app.go`
- Test: `internal/tui/app_test.go`

**Interfaces:**
- Produces: `type ThemeName string`
- Produces: `const themeATMDark ThemeName = "atm-dark"` plus `themeGraphite`, `themeLight`, `themeMono`
- Produces: `func defaultThemeName() ThemeName`
- Produces: `func nextThemeName(current ThemeName) ThemeName`
- Produces: `func buildStyles(themeName ThemeName) Styles`
- Produces: `type Styles struct`
- Consumes: existing `Model`, `NewModel`, `newTestModel`

- [ ] **Step 1: Write failing theme unit tests**

Add these tests to `internal/tui/app_test.go` near the tab/status tests:

```go
func TestDefaultTheme(t *testing.T) {
	m := newTestModel(t)
	if m.themeName != themeATMDark {
		t.Fatalf("themeName = %q want %q", m.themeName, themeATMDark)
	}
	if string(m.themeName) != "atm-dark" {
		t.Fatalf("themeName string = %q want atm-dark", m.themeName)
	}
}

func TestNextThemeNameWraps(t *testing.T) {
	order := []ThemeName{themeATMDark, themeGraphite, themeLight, themeMono, themeATMDark}
	for i := 0; i < len(order)-1; i++ {
		if got := nextThemeName(order[i]); got != order[i+1] {
			t.Fatalf("nextThemeName(%q) = %q want %q", order[i], got, order[i+1])
		}
	}
	if got := nextThemeName(ThemeName("unknown")); got != themeATMDark {
		t.Fatalf("nextThemeName(unknown) = %q want %q", got, themeATMDark)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/tui -run 'Test(DefaultTheme|NextThemeNameWraps)' -count=1`

Expected: FAIL because `themeName`, `ThemeName`, and `nextThemeName` are not defined.

- [ ] **Step 3: Implement theme types and style builder**

Create `internal/tui/theme.go`:

```go
package tui

import "github.com/charmbracelet/lipgloss"

type ThemeName string

const (
	themeATMDark  ThemeName = "atm-dark"
	themeGraphite ThemeName = "graphite"
	themeLight    ThemeName = "light"
	themeMono     ThemeName = "mono"
)

var themeOrder = []ThemeName{themeATMDark, themeGraphite, themeLight, themeMono}

type Theme struct {
	Text       lipgloss.Color
	Muted      lipgloss.Color
	Subtle     lipgloss.Color
	Surface    lipgloss.Color
	Border     lipgloss.Color
	Accent     lipgloss.Color
	AccentText lipgloss.Color
	Warning    lipgloss.Color
	Error      lipgloss.Color
	Success    lipgloss.Color
}

type Styles struct {
	ActiveTab       lipgloss.Style
	InactiveTab     lipgloss.Style
	KeyMenu         lipgloss.Style
	KeyMenuDim      lipgloss.Style
	Status          lipgloss.Style
	StatusLabel     lipgloss.Style
	Dialog          lipgloss.Style
	DialogTitle     lipgloss.Style
	DialogBody      lipgloss.Style
	FieldLabel      lipgloss.Style
	FieldValue      lipgloss.Style
	FieldHint       lipgloss.Style
	ButtonActive    lipgloss.Style
	ButtonInactive  lipgloss.Style
	RowCursor       lipgloss.Style
	GutterSelect    lipgloss.Style
	EmptyHead       lipgloss.Style
	EmptyText       lipgloss.Style
	EmptyKey        lipgloss.Style
	EmptyDim        lipgloss.Style
	HeaderLabel     lipgloss.Style
	HeaderLine      lipgloss.Style
	GroupHeader     lipgloss.Style
	NamespaceHeader lipgloss.Style
	LabelChip       lipgloss.Style
	Muted           lipgloss.Style
	Body            lipgloss.Style
	Warning         lipgloss.Style
	Error           lipgloss.Style
	Success         lipgloss.Style
	Toast           lipgloss.Style
	OverlayBackdrop lipgloss.Style
	HelpTable       lipgloss.Style
}

func defaultThemeName() ThemeName { return themeATMDark }

func nextThemeName(current ThemeName) ThemeName {
	for i, name := range themeOrder {
		if current == name {
			return themeOrder[(i+1)%len(themeOrder)]
		}
	}
	return defaultThemeName()
}

func themeByName(name ThemeName) Theme {
	switch name {
	case themeGraphite:
		return Theme{Text: "252", Muted: "244", Subtle: "238", Surface: "235", Border: "242", Accent: "214", AccentText: "0", Warning: "214", Error: "203", Success: "113"}
	case themeLight:
		return Theme{Text: "235", Muted: "244", Subtle: "250", Surface: "255", Border: "245", Accent: "25", AccentText: "255", Warning: "130", Error: "160", Success: "28"}
	case themeMono:
		return Theme{Text: "255", Muted: "250", Subtle: "240", Surface: "0", Border: "255", Accent: "255", AccentText: "0", Warning: "255", Error: "255", Success: "255"}
	default:
		return Theme{Text: "255", Muted: "245", Subtle: "240", Surface: "0", Border: "39", Accent: "39", AccentText: "0", Warning: "214", Error: "203", Success: "42"}
	}
}

func buildStyles(themeName ThemeName) Styles {
	t := themeByName(themeName)
	s := Styles{
		ActiveTab:       lipgloss.NewStyle().Foreground(t.AccentText).Background(t.Accent).Bold(true).Padding(0, 1),
		InactiveTab:     lipgloss.NewStyle().Foreground(t.Muted).Padding(0, 1),
		KeyMenu:         lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		KeyMenuDim:      lipgloss.NewStyle().Foreground(t.Muted),
		Status:          lipgloss.NewStyle().Foreground(t.Muted),
		StatusLabel:     lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		Dialog:          lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(t.Border).Padding(0, 1),
		DialogTitle:     lipgloss.NewStyle().Foreground(t.Text).Bold(true).Padding(0, 1),
		DialogBody:      lipgloss.NewStyle().Foreground(t.Text),
		FieldLabel:      lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		FieldValue:      lipgloss.NewStyle().Foreground(t.Text),
		FieldHint:       lipgloss.NewStyle().Foreground(t.Muted).Italic(true),
		ButtonActive:    lipgloss.NewStyle().Foreground(t.AccentText).Background(t.Accent).Bold(true).Padding(0, 2),
		ButtonInactive:  lipgloss.NewStyle().Foreground(t.Muted).Padding(0, 2),
		RowCursor:       lipgloss.NewStyle().Reverse(true),
		GutterSelect:    lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		EmptyHead:       lipgloss.NewStyle().Foreground(t.Text).Bold(true),
		EmptyText:       lipgloss.NewStyle().Foreground(t.Text),
		EmptyKey:        lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		EmptyDim:        lipgloss.NewStyle().Foreground(t.Muted),
		HeaderLabel:     lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		HeaderLine:      lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		GroupHeader:     lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		NamespaceHeader: lipgloss.NewStyle().Foreground(t.Text).Bold(true),
		LabelChip:       lipgloss.NewStyle().Foreground(t.Text).Background(t.Subtle).Padding(0, 1),
		Muted:           lipgloss.NewStyle().Foreground(t.Muted),
		Body:            lipgloss.NewStyle().Foreground(t.Text),
		Warning:         lipgloss.NewStyle().Foreground(t.Warning).Bold(true),
		Error:           lipgloss.NewStyle().Foreground(t.Error).Bold(true),
		Success:         lipgloss.NewStyle().Foreground(t.Success).Bold(true),
		Toast:           lipgloss.NewStyle().Foreground(t.AccentText).Background(t.Error).Bold(true).Padding(0, 1),
		OverlayBackdrop: lipgloss.NewStyle().Foreground(t.Subtle),
		HelpTable:       lipgloss.NewStyle().Foreground(t.Text),
	}
	if themeName == themeMono {
		s.ActiveTab = lipgloss.NewStyle().Reverse(true).Bold(true).Padding(0, 1)
		s.HeaderLabel = lipgloss.NewStyle().Bold(true).Underline(true)
		s.GroupHeader = lipgloss.NewStyle().Bold(true)
		s.LabelChip = lipgloss.NewStyle().Reverse(true).Padding(0, 1)
	}
	return s
}
```

- [ ] **Step 4: Add theme fields to the root model**

Modify `internal/tui/app.go`:

```go
type Model struct {
	store    *store.Store
	storeSet bool
	actor    string
	km       keymap

	themeName ThemeName
	styles    Styles

	width, height   int
	contentHeight   int
	focused         workspacePane
	projectScope    string
	quitting        bool
	keymapOverlayOn bool
	// existing fields continue unchanged
}
```

In `NewModel`, initialize theme fields:

```go
themeName := defaultThemeName()
m := &Model{
	store:     s,
	storeSet:  true,
	km:        defaultKeymap(),
	width:     100,
	height:    30,
	actor:     actor,
	themeName: themeName,
	styles:    buildStyles(themeName),
}
```

- [ ] **Step 5: Run theme infrastructure tests**

Run: `go test ./internal/tui -run 'Test(DefaultTheme|NextThemeNameWraps)' -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/theme.go internal/tui/app.go internal/tui/app_test.go
git commit -m "tui: add runtime theme infrastructure"
```

---

### Task 2: Theme Cycling, Status, and Help

**Files:**
- Modify: `internal/tui/app.go`
- Modify: `internal/tui/keymap.go`
- Modify: `internal/tui/help.go`
- Test: `internal/tui/app_test.go`

**Interfaces:**
- Consumes: `ThemeName`, `nextThemeName`, `buildStyles`
- Produces: `func (m *Model) cycleTheme()`
- Produces: status line segment `theme: <name>`

- [ ] **Step 1: Write failing tests for cycling and state preservation**

Add these tests to `internal/tui/app_test.go`:

```go
func TestThemeCycleKeyUpdatesThemeAndStatus(t *testing.T) {
	m := newTestModel(t)
	mustContain(t, m.renderStatusLine(), "theme: atm-dark")
	update(t, m, "T")
	if m.themeName != themeGraphite {
		t.Fatalf("after T: themeName = %q want %q", m.themeName, themeGraphite)
	}
	mustContain(t, m.renderStatusLine(), "theme: graphite")
}

func TestThemeCyclePreservesNavigationState(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "open task", "ATM:status:open")
	update(t, m, "s")
	update(t, m, "2")
	update(t, m, "/")
	for _, r := range "ATM:status:open" {
		update(t, m, string(r))
	}
	update(t, m, "enter")
	update(t, m, "enter")
	if m.tasks.view != tViewDetail {
		t.Fatalf("setup: expected task detail")
	}
	update(t, m, "T")
	if m.focused != paneTasks {
		t.Errorf("focused = %v want paneTasks", m.focused)
	}
	if m.projectScope != "ATM" {
		t.Errorf("projectScope = %q want ATM", m.projectScope)
	}
	if m.tasks.filter != "ATM:status:open" {
		t.Errorf("filter = %q want ATM:status:open", m.tasks.filter)
	}
	if m.tasks.view != tViewDetail {
		t.Errorf("tasks.view = %v want tViewDetail", m.tasks.view)
	}
}

func TestThemeKeyDoesNotHijackTextInput(t *testing.T) {
	m := newTestModel(t)
	update(t, m, "a")
	update(t, m, "T")
	if m.themeName != themeATMDark {
		t.Fatalf("themeName changed in form input: %q", m.themeName)
	}
	if got := m.form.Fields[0].Value; got != "T" {
		t.Fatalf("form field value = %q want T", got)
	}
}

func TestHelpTabKeymapIncludesTheme(t *testing.T) {
	m := newTestModel(t)
	update(t, m, "4")
	content := strings.Join(m.help.lines, "\n")
	mustContain(t, content, "T")
	mustContain(t, content, "cycle theme")
}

func TestThemeCyclesInsideKeymapOverlay(t *testing.T) {
	m := newTestModel(t)
	update(t, m, "?")
	if !m.keymapOverlayOn {
		t.Fatalf("setup: keymap overlay should be open")
	}
	update(t, m, "T")
	if m.themeName != themeGraphite {
		t.Fatalf("themeName = %q want %q", m.themeName, themeGraphite)
	}
	if !m.keymapOverlayOn {
		t.Fatalf("theme cycling should not close keymap overlay")
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/tui -run 'TestTheme|TestHelpTabKeymapIncludesTheme' -count=1`

Expected: FAIL because `T` is not handled, status lacks theme, and keymap lacks theme.

- [ ] **Step 3: Implement model theme cycling**

Add to `internal/tui/app.go`:

```go
func (m *Model) cycleTheme() {
	m.themeName = nextThemeName(m.themeName)
	m.styles = buildStyles(m.themeName)
}
```

In `handleKey`, make `T` work in keymap overlay, confirm overlay, and normal
navigation contexts while leaving form and filter text entry alone:

```go
if m.keymapOverlayOn {
	if k.String() == "T" {
		m.cycleTheme()
		return nil
	}
	if k.String() == "?" || k.String() == "esc" {
		m.keymapOverlayOn = false
	}
	return nil
}

if m.confirm != confirmNone {
	if k.String() == "T" {
		m.cycleTheme()
		return nil
	}
	return m.handleConfirmKey(k)
}

if m.form != nil && m.form.Active {
	return m.handleFormKey(k)
}

if m.focused == paneTasks && m.tasks.filterEditing {
	return m.tasks.handleKey(k)
}

if k.String() == "T" {
	m.cycleTheme()
	return nil
}
```

Keep the existing quit handling before this block so `ctrl+c` still quits
everywhere.

- [ ] **Step 4: Add theme to status line**

Modify `renderStatusLine` in `internal/tui/app.go`:

```go
parts = append(parts, statusLabelStyle.Render("STORE: ")+statusStyle.Render(shortenPath(m.store.StorePath(), 40)))
if m.projectScope != "" {
	parts = append(parts, statusLabelStyle.Render("SELECTED: ")+statusStyle.Render(m.projectScope))
}
parts = append(parts, statusLabelStyle.Render("theme: ")+statusStyle.Render(string(m.themeName)))
```

This step still uses old globals if Task 3 has not migrated render styles yet.

- [ ] **Step 5: Document theme in keymap**

Modify `internal/tui/keymap.go` by adding this row before the `?` row:

```go
{"T", "cycle theme", "cycle theme", "cycle theme", "cycle theme", "cycle theme"},
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/tui -run 'TestTheme|TestHelpTabKeymapIncludesTheme' -count=1`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/app.go internal/tui/keymap.go internal/tui/app_test.go
git commit -m "tui: wire runtime theme switching"
```

---

### Task 3: Migrate Renderers to Semantic Styles

**Files:**
- Modify: `internal/tui/styles.go`
- Modify: `internal/tui/app.go`
- Modify: `internal/tui/form.go`
- Modify: `internal/tui/projects.go`
- Modify: `internal/tui/tasks.go`
- Modify: `internal/tui/labels.go`
- Modify: `internal/tui/help.go`
- Test: `internal/tui/app_test.go`
- Test: `internal/tui/labels_test.go`

**Interfaces:**
- Consumes: `Model.styles Styles`
- Produces: `func (f *Form) View(styles Styles) string`
- Produces: `func renderLabelChips(styles Styles, labels []string, width int) string`

- [ ] **Step 1: Write failing tests for migrated style behavior**

Add these tests to `internal/tui/app_test.go`:

```go
func TestThemeChangesRenderedANSI(t *testing.T) {
	m := newTestModel(t)
	before := m.renderTabBar()
	update(t, m, "T")
	after := m.renderTabBar()
	if before == after {
		t.Fatalf("tab bar did not change after theme cycle")
	}
}

func TestTaskDetailLabelsRenderAsChips(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "chip task", "ATM:status:open", "ATM:type:bug")
	update(t, m, "s")
	update(t, m, "2")
	update(t, m, "enter")
	v := m.View()
	mustContain(t, v, " ATM:status:open ")
	mustContain(t, v, " ATM:type:bug ")
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/tui -run 'TestThemeChangesRenderedANSI|TestTaskDetailLabelsRenderAsChips' -count=1`

Expected: FAIL because renderers still use global styles and label detail strings have no chip padding.

- [ ] **Step 3: Change `Form.View` to accept styles**

Modify `internal/tui/form.go`:

```go
func (f *Form) View(styles Styles) string {
	var b strings.Builder
	innerW := f.width

	b.WriteString(styles.DialogTitle.Render(f.Title))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", innerW))
	b.WriteString("\n\n")

	for i, fld := range f.Fields {
		active := f.zone == focusFields && i == f.cursor
		label := styles.FieldLabel.Render(fld.Label + ":")
		val := styles.FieldValue.Render(fld.Value)
		if active {
			val += styles.FieldValue.Underline(true).Render(" ")
		}
		b.WriteString(fmt.Sprintf("%s %s\n", label, val))
		if fld.Hint != "" {
			b.WriteString(styles.FieldHint.Render("  " + fld.Hint))
			b.WriteString("\n")
		}
		if errMsg := f.fieldError(i); errMsg != "" {
			b.WriteString(styles.Error.Render("  x " + errMsg))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if f.Err != "" {
		b.WriteString(styles.Error.Render("x " + f.Err))
		b.WriteString("\n\n")
	}

	submitActive := f.zone == focusButtons && f.btnIdx == 0
	cancelActive := f.zone == focusButtons && f.btnIdx == 1
	submit := styles.ButtonInactive.Render("[ Submit ]")
	cancel := styles.ButtonInactive.Render("[ Cancel ]")
	if submitActive && f.valid() {
		submit = styles.ButtonActive.Render("[ Submit ]")
	}
	if cancelActive {
		cancel = styles.ButtonActive.Render("[ Cancel ]")
	}
	buttons := lipgloss.JoinHorizontal(lipgloss.Center, submit, "  ", cancel)
	hint := styles.KeyMenuDim.Render("Tab/arrows to navigate  Enter to confirm  Esc to cancel")
	b.WriteString(buttons)
	b.WriteString("\n")
	b.WriteString(hint)

	return styles.Dialog.Render(b.String())
}
```

Update the call site in `Model.View`:

```go
out = m.placeOverlay(out, m.form.View(m.styles))
```

- [ ] **Step 4: Migrate `app.go` styles**

In `internal/tui/app.go`, replace style globals with `m.styles` fields:

```go
parts = append(parts, m.styles.ActiveTab.Render(label))
parts = append(parts, m.styles.InactiveTab.Render(label))
parts = append(parts, m.styles.StatusLabel.Render("STORE: ")+m.styles.Status.Render(shortenPath(m.store.StorePath(), 40)))
parts = append(parts, m.styles.KeyMenu.Render(hint))
line := left + spaces(gap) + m.styles.Status.Render(actor)
out = m.placeToast(out, m.styles.Toast.Render(" "+m.toastMsg+" "))
b.WriteString(m.styles.DialogTitle.Render(m.confirmMsg))
b.WriteString(m.styles.Warning.Render(m.confirmArg))
b.WriteString(m.styles.KeyMenuDim.Render("[Enter] confirm   [Esc] cancel"))
return m.styles.Dialog.Render(b.String())
```

- [ ] **Step 5: Migrate pane renderers**

Replace existing global style references in pane files. Use these concrete
replacement patterns:

```go
// projects.go
p.m.styles.HeaderLabel.Render(fmt.Sprintf("%-6s %-30s %6s %7s %10s", "CODE", "NAME", "TASKS", "LABELS", "UPDATED"))
p.m.styles.GutterSelect.Render("▸")
p.m.styles.RowCursor.Render(line)
p.m.styles.EmptyHead.Render("no projects")
p.m.styles.EmptyText.Render(fmt.Sprintf("press %s to add a project, then seed", p.m.styles.EmptyKey.Render("[a]")))
p.m.styles.EmptyDim.Render("index tasks (start-here, repo:, doc:)")

// tasks.go
t.m.styles.HeaderLine.Render(t.headerLine())
t.m.styles.EmptyHead.Render("no project selected")
t.m.styles.EmptyText.Render(fmt.Sprintf("press %s in the Projects tab to scope this view", t.m.styles.EmptyKey.Render("[s]")))
t.m.styles.EmptyKey.Render("[/]")
t.m.styles.HeaderLabel.Render(fmt.Sprintf(" %-10s %-40s %-30s %10s", "ID", "TITLE", "LABELS", "UPDATED"))
t.m.styles.RowCursor.Render(line)
t.m.styles.GroupHeader.Render(fmt.Sprintf("%s%s %s (%d)", indent, marker, name, count))
t.m.styles.KeyMenuDim.Render("[Enter]apply [Esc]cancel")

// labels.go
l.m.styles.EmptyHead.Render("no project selected")
l.m.styles.EmptyText.Render(fmt.Sprintf("press %s in the Projects tab to scope this view", l.m.styles.EmptyKey.Render("[s]")))
l.m.styles.EmptyKey.Render("[s]")
l.m.styles.HeaderLabel.Render(fmt.Sprintf(" %-30s %8s  %s", "LABEL", "USAGE", "DESCRIPTION"))
l.m.styles.NamespaceHeader.Render(ns + ":")
l.m.styles.RowCursor.Render(strings.TrimPrefix(line, " "))
l.m.styles.KeyMenuDim.Render("[d]esc  [l]remove  [Esc]back")

// help.go
h.m.styles.HeaderLabel.Render("ATM Help")
h.m.styles.HelpTable.Render(parityTable)
```

- [ ] **Step 6: Add label chip renderer and use it in task detail**

Add to `internal/tui/styles.go`:

```go
func renderLabelChips(styles Styles, labels []string, width int) string {
	if len(labels) == 0 {
		return ""
	}
	parts := make([]string, 0, len(labels))
	for _, label := range labels {
		parts = append(parts, styles.LabelChip.Render(" "+label+" "))
	}
	line := strings.Join(parts, " ")
	if width > 0 && lipgloss.Width(line) > width {
		return strings.Join(labels, "   ")
	}
	return line
}
```

Modify `tasksModel.renderDetail`:

```go
if len(tk.Labels) == 0 {
	b.WriteString(" (no labels)\n")
} else {
	b.WriteString(" " + renderLabelChips(t.m.styles, tk.Labels, t.m.width-2) + "\n")
}
```

- [ ] **Step 7: Remove old global style variables**

In `internal/tui/styles.go`, delete the top-level grouped style variable block
that starts with `activeTabStyle = lipgloss.NewStyle()` and ends with
`emptyDimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))`.
Keep helper functions such as `spaces`, `fitLine`, `centerLinesBoth`,
`truncateRunes`, `sepLine`, and `relTime`.

- [ ] **Step 8: Run migration tests**

Run: `go test ./internal/tui -count=1`

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/tui
git commit -m "tui: migrate rendering to semantic styles"
```

---

### Task 4: Overlay Layering and Visual Polish

**Files:**
- Modify: `internal/tui/app.go`
- Modify: `internal/tui/styles.go`
- Test: `internal/tui/app_test.go`

**Interfaces:**
- Consumes: `Styles.OverlayBackdrop`, `Styles.Dialog`
- Produces: `func overlayLines(base, overlay string, width, height int) string`
- Produces: layered overlay output that preserves underlying screen text outside the overlay rectangle

- [ ] **Step 1: Write failing overlay layering tests**

Add this test to `internal/tui/app_test.go`:

```go
func TestOverlayPreservesUnderlyingScreen(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	base := m.View()
	mustContain(t, base, "Acme Task Manager")
	update(t, m, "a")
	withOverlay := m.View()
	mustContain(t, withOverlay, "New project")
	mustContain(t, withOverlay, "Acme Task Manager")
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/tui -run TestOverlayPreservesUnderlyingScreen -count=1`

Expected: FAIL because the current `placeOverlay` discards `base`.

- [ ] **Step 3: Implement line-aware overlay placement**

Add to `internal/tui/app.go`:

```go
func (m *Model) placeOverlay(base, overlay string) string {
	return overlayLines(base, overlay, m.width, m.height)
}

func overlayLines(base, overlay string, width, height int) string {
	baseLines := strings.Split(base, "\n")
	for len(baseLines) < height {
		baseLines = append(baseLines, spaces(width))
	}
	if len(baseLines) > height {
		baseLines = baseLines[:height]
	}
	overlayLines := strings.Split(overlay, "\n")
	overlayH := len(overlayLines)
	overlayW := 0
	for _, line := range overlayLines {
		if w := lipgloss.Width(line); w > overlayW {
			overlayW = w
		}
	}
	x := (width - overlayW) / 2
	if x < 0 {
		x = 0
	}
	y := (height - overlayH) / 2
	if y < 0 {
		y = 0
	}
	for i, overlayLine := range overlayLines {
		target := y + i
		if target < 0 || target >= len(baseLines) {
			continue
		}
		baseLines[target] = overlayLineAt(baseLines[target], overlayLine, x, width)
	}
	return strings.Join(baseLines, "\n")
}

func overlayLineAt(baseLine, overlayLine string, x, width int) string {
	plainPrefix := fitLine(baseLine, x)
	if lipgloss.Width(plainPrefix) < x {
		plainPrefix += spaces(x - lipgloss.Width(plainPrefix))
	}
	remaining := width - x - lipgloss.Width(overlayLine)
	if remaining < 0 {
		remaining = 0
	}
	suffixStart := x + lipgloss.Width(overlayLine)
	suffix := ""
	if lipgloss.Width(baseLine) > suffixStart {
		suffix = fitLineFrom(baseLine, suffixStart, remaining)
	}
	line := plainPrefix + overlayLine + suffix
	if lipgloss.Width(line) < width {
		line += spaces(width - lipgloss.Width(line))
	}
	return line
}
```

Add helper to `internal/tui/styles.go`:

```go
func fitLineFrom(s string, start, width int) string {
	if width <= 0 {
		return ""
	}
	var out strings.Builder
	used := 0
	pos := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if pos+rw <= start {
			pos += rw
			continue
		}
		if used+rw > width {
			break
		}
		out.WriteRune(r)
		used += rw
		pos += rw
	}
	return out.String()
}
```

- [ ] **Step 4: Run overlay test**

Run: `go test ./internal/tui -run TestOverlayPreservesUnderlyingScreen -count=1`

Expected: PASS.

- [ ] **Step 5: Run full TUI tests**

Run: `go test ./internal/tui -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/app.go internal/tui/styles.go internal/tui/app_test.go
git commit -m "tui: layer overlays over existing screen"
```

---

### Task 5: Final Verification and Polish Pass

**Files:**
- Modify: any `internal/tui/*.go` files needed for compile, gofmt, or small visual polish aligned with the spec
- Test: full repository

**Interfaces:**
- Consumes: all previous task outputs
- Produces: verified TUI theme refresh

- [ ] **Step 1: Search for direct color/style construction outside theme builder**

Run:

```bash
rg -n "lipgloss\\.Color|NewStyle\\(\\).*Foreground|NewStyle\\(\\).*Background|activeTabStyle|emptyHeadStyle|toastStyle|dialogStyle|rowCursorStyle" internal/tui
```

Expected: matches are either in `internal/tui/theme.go` or layout-only helper
styles such as `lipgloss.NewStyle().Width(w).Align(lipgloss.Center)`.
Move every remaining color-bearing style construction into `buildStyles`.

- [ ] **Step 2: Format code**

Run:

```bash
gofmt -w internal/tui
```

Expected: no command output.

- [ ] **Step 3: Run focused tests**

Run:

```bash
go test ./internal/tui ./internal/store -count=1
```

Expected: PASS.

- [ ] **Step 4: Run repository verification**

Run:

```bash
make verify
```

Expected: PASS.

- [ ] **Step 5: Review final diff**

Run:

```bash
git diff --stat
git diff -- internal/tui docs/superpowers/plans/2026-07-03-tui-theme-refresh.md
```

Expected: diff only contains the runtime TUI theme refresh and this plan.

- [ ] **Step 6: Commit final polish**

If Step 1 or verification changed files, commit them:

```bash
git add internal/tui docs/superpowers/plans/2026-07-03-tui-theme-refresh.md
git commit -m "tui: finish theme refresh verification"
```

If Step 1 and verification did not change files and all prior task commits
already contain the code, commit only this plan if it is still uncommitted:

```bash
git add docs/superpowers/plans/2026-07-03-tui-theme-refresh.md
git commit -m "docs: add TUI theme refresh plan"
```
