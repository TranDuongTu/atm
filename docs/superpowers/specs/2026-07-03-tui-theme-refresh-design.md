# ATM TUI Theme Refresh — Design Spec

**Status:** Approved for design; pending implementation plan.
**Scope:** Runtime-only TUI visual refresh and theme switching for the v2
Bubble Tea application.

## Driver

The v2 TUI already has the right product shape: Projects, Tasks, Labels, and
Help are first-class management surfaces over the label substrate. The current
implementation is useful and dense, but it reads as a raw terminal utility:
most emphasis uses the same cyan accent, style definitions are global package
variables, labels are visually plain despite being ATM's organizing substrate,
and overlays currently render detached from the underlying screen.

This refresh makes the TUI feel more professional without changing ATM's data
model, store API, filter semantics, label behavior, command surface, or
workflow philosophy. Themes are runtime-only: switching themes changes the
current TUI session and writes nothing to `$ATM_HOME`.

## Goals

- Add runtime theme switching with a small set of built-in themes.
- Replace direct color use with semantic style roles.
- Improve hierarchy in chrome, list views, grouped task views, labels, detail
  pages, overlays, and Help.
- Preserve the current tab model, keyboard-driven interaction, and dense
  terminal-table ergonomics.
- Keep render output stable enough that existing behavior tests remain useful.

## Non-goals

- No persisted theme setting.
- No store changes.
- No label or task behavior changes.
- No new TUI tabs, panes, or data model concepts.
- No CLI flags or config commands for themes.
- No mouse-driven redesign.

## Theme Model

The root TUI model owns the active theme for the current process:

- `ThemeName` identifies a built-in theme.
- `Theme` defines semantic palette values.
- `Styles` contains the Lip Gloss styles derived from a `Theme`.
- `Model` stores the active `ThemeName` and `Styles`.

Theme switching is a pure presentation update. It must not change:

- focused pane
- selected project
- cursors and offsets
- task filter text
- sort mode
- detail/list view state
- active form values
- active confirmation state
- toast message

## Built-in Themes

ATM ships four themes:

1. `atm-dark`
   - Default theme.
   - Dark background assumption.
   - Restrained blue/cyan accent.
   - Intended to feel close to the current UI, but quieter and more structured.

2. `graphite`
   - Neutral dark theme with gray surfaces and an amber accent.
   - Intended for a mature operational-tool feel.
   - Avoids the app reading as a one-note blue interface.

3. `light`
   - Light-background friendly theme.
   - Uses darker text, muted gray metadata, and a clear accent.
   - Must remain readable in terminals configured with light palettes.

4. `mono`
   - High-contrast, low-color theme.
   - Uses bold, reverse video, and border contrast more than hue.
   - Useful for limited terminals and accessibility.

All themes use semantic roles rather than direct call-site colors.

## Semantic Style Roles

The style system exposes roles for the UI's jobs, not for colors:

- app chrome
- active tab
- inactive tab
- status text
- status label
- key hint
- dim key hint
- section heading
- section divider
- table header
- table row
- selected row
- selected project marker
- task group header
- label chip
- label namespace heading
- muted text
- body text
- warning
- error
- success
- dialog border
- dialog title
- dialog body
- form label
- form value
- form hint
- active button
- inactive button
- toast
- overlay backdrop

Implementation may keep the existing role names where they already map cleanly,
but direct `lipgloss.Color(...)` construction should move into the theme/style
builder rather than staying scattered through render code.

## Runtime Switching

`T` cycles to the next built-in theme in normal TUI navigation contexts:
list views, detail views, confirms, keymap overlay, and Help. Text-entry
contexts keep normal typing behavior, so `T` entered while a form field or
filter input is focused inserts the character rather than changing the theme.

The status line includes the active theme:

```text
STORE: ~/.config/atm  SELECTED: ATM  theme: atm-dark  [/]filter [s]sort ...  actor: ttran
```

The global keymap overlay and Help keymap document `[T]theme`.

## Layout Refresh

The refresh keeps the current shared chrome:

- tab bar
- separator
- body
- separator
- status line

The tab bar stays numbered and keyboard-first. Active and inactive tab styling
should be obvious but quiet. Inactive tabs should not compete with the active
pane.

The status line remains dense and always visible. It should group operational
context first (`STORE`, selected project, theme), then contextual keys, then
actor on the right. When terminal width is tight, existing truncation behavior
can be retained; theme display may be omitted before actor if necessary.

## Lists and Grouped Tasks

Projects, Tasks, and Labels keep their table layout. The refresh improves
scanability:

- headers use the table-header role
- selected rows use a consistent selected-row role
- metadata uses muted text where practical
- selected project marker remains distinct from cursor highlight
- grouped task headers use a group-header role distinct from table headers
- nested grouped tasks keep indentation stable

No columns are added or removed as part of this feature.

## Labels

Labels are ATM's central organizing substrate, so they should have a consistent
visual treatment.

In task detail, labels render as compact chips when width allows. A chip is
still plain terminal text; it must not introduce wrapping surprises or make
copy/paste impossible. In dense tables, labels may remain inline text for
alignment, but they should use a label-specific style.

Labels tab namespace headings use a namespace-heading role. Usage counts remain
visible and easy to scan.

## Detail Views

Project, Task, and Label detail pages keep their current data. The refresh
organizes that data with stronger section hierarchy:

- heading
- facts block
- labels block where applicable
- history block where applicable
- local action hints

Metadata such as timestamps and actors should be quieter than primary fields.
Action hints should use the shared key-hint roles.

## Overlays and Toasts

Forms, confirms, keymap overlay, and toast rendering should layer over the
existing screen instead of replacing it with blank whitespace. The current
screen remains visually present behind the overlay.

Dialogs use theme roles for border, title, body, field labels, hints,
validation errors, and buttons. Validation error styles move out of ad hoc
call-site construction and into the shared style system.

Toast messages use semantic success/error/warning styling where the calling
code makes that classification clear. Existing generic messages may continue to
use a default toast style.

## Help

Help remains text-first. The refresh improves section hierarchy but does not
rewrite the conventions content. The keymap table includes theme switching.

## Error Handling

Theme cycling itself should not fail. If an unknown theme name is requested
internally, the TUI falls back to `atm-dark`.

Theme rendering must not panic on narrow terminal widths. Existing truncation,
padding, and fit helpers should continue to guard render paths.

## Testing

Implementation must add or update tests for:

- default theme is `atm-dark`
- `T` cycles through all built-in themes and wraps
- status line shows the active theme when width allows
- Help/keymap overlay documents theme switching
- theme switching preserves pane, selection, cursor, task filter, sort mode,
  and detail/list state
- typing `T` while a form field or filter input is focused preserves normal
  text input behavior
- overlays include underlying screen content after placement
- existing list, detail, label, and form behavior tests continue to pass

Visual assertions should focus on stable text, state preservation, and presence
of style escape sequences only where necessary. Tests should not become brittle
snapshots of complete ANSI output.

## Verification

Before declaring implementation complete, run:

```sh
make verify
```

If the implementation changes only TUI rendering and `make verify` is too broad
for an intermediate checkpoint, `go test ./internal/tui ./internal/store` is an
acceptable local loop, but final completion still requires `make verify`.
