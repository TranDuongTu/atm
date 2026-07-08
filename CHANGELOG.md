## Unreleased
<!-- manual entry; release.sh prepends a dated section at cut — fold these bullets into the next release section and delete this block -->

### tui
- Replace Projects pane "activity by actor" chart with persona-grouped "activity by persona" (alias-resolved); fix bar-width alignment (ATM-0054)
- Remove the `[4] Actors` maximized pane; `numPanes` back to 3 (ATM-0054)
- Add `P` key in Projects pane to expand the persona activity chart into an overlay with per-persona agents/models/actions drilldown (ATM-0054)
- Add `p` key in Projects pane to open a New persona form (name + description; prompt set via CLI) (ATM-0054)

### docs
- Document personas, actor convention (`persona@agent:model`), `atm actor migrate`/`alias`, `atm developing --persona`, and `atm activity` (ATM-0052)
- Update conventions/README to reference the `P` overlay + `p` add-persona instead of the removed `[4] Actors` pane (ATM-0054)


## v1.2.2 - 2026-07-07

### docs
- Toggle exact-label filter on row Enter; move label detail to i key (ATM-0041)
- Add implementation plan for ATM-0041 labels namespace filtering
- Add design spec for ATM-0041 labels namespace filtering


## v1.2.1 - 2026-07-07


## v1.2.0 - 2026-07-07


## v1.1.4 - 2026-07-06

### docs
- Add implementation plan: cache.db consolidation (ATM-0027 phase 1)
- Add design spec: consolidate caches into SQLite + cross-machine sync
- Add implementation plan: launcher extra agent args + ollama host
- Refine launcher-args spec: construct OllamaLauncher directly (mirror onboarding)
- Add design spec: launcher extra agent args + ollama host for developing/manager


## v1.1.3 - 2026-07-06

### docs
- Add implementation plan: cache.db consolidation (ATM-0027 phase 1)
- Add design spec: consolidate caches into SQLite + cross-machine sync
- Add implementation plan: launcher extra agent args + ollama host
- Refine launcher-args spec: construct OllamaLauncher directly (mirror onboarding)
- Add design spec: launcher extra agent args + ollama host for developing/manager
- fix manager logics


## v1.1.2 - 2026-07-06

### docs
- fix manager logics


## v1.1.1 - 2026-07-06

### docs
- Add ATM manager subagent implementation plan (ATM-0024)
- Revise manager spec: add prompt appendix, dispatch contract, fast/thorough pacing
- Add ATM manager subagent design spec (ATM-0024)


## v1.1.0 - 2026-07-06

### docs
- Add ATM manager subagent implementation plan (ATM-0024)
- Revise manager spec: add prompt appendix, dispatch contract, fast/thorough pacing
- Add ATM manager subagent design spec (ATM-0024)


## v1.0.2 - 2026-07-06


## v0.1.1 - 2026-07-06

### docs
- docs: semver build & release pipeline implementation plan
- docs: semver build & release pipeline design spec
- remove existing tasks interpolation in developing prompt
- docs: TUI visual polish implementation plan
- docs: TUI visual polish design spec
- docs: plan atm developing launcher
- docs: design atm developing launcher
- docs: task comments v1 implementation plan
- docs: task comments v1 design spec
- feat(tui): add project summary activity charts
- docs: tighten plan task-brief cross-refs and seq-gap assertion
- docs: audit log redesign implementation plan
- docs: audit log redesign spec (event-sourced per-project WAL)
- docs: plan project summary charts
- docs: design project summary charts
- docs: onboarding v1 spec and implementation plan
- update Help and Convention layout
- docs: add three-pane tui implementation plan
- docs: specify three-pane tui workspace
- fix layout and themes
- docs: add TUI theme refresh plan
- docs: add TUI theme refresh design
- chore: gofmt canonicalization (trailing newlines + alignment) + add label-management plan doc
- spec: label management refinement — dedicated Labels tab, multi-label task create, default seeding, agent code-of-conduct
- update plan
- v2: amend spec facets JSON shape + fix label form titles + keymap b row
- add spec
- spec: add Onboarding & conventions (Section 7) + atm conventions cmd + tui auto-init
- update spec
- spec: rewrite tasks-management v2 as pure label-substrate
- remove legacy plan
- spec: rewrite tasks-management v2 as single superpowers design file
- spec: rewrite 001-tasks-management to v2.0.0
- feat: introduce tui workspace focus
- Add lazygit-style TUI rewrite design
- replace speckit with superpowers


## v0.1.0 - 2026-07-06

### docs
- docs: semver build & release pipeline implementation plan
- docs: semver build & release pipeline design spec
- remove existing tasks interpolation in developing prompt
- docs: TUI visual polish implementation plan
- docs: TUI visual polish design spec
- docs: plan atm developing launcher
- docs: design atm developing launcher
- docs: task comments v1 implementation plan
- docs: task comments v1 design spec
- feat(tui): add project summary activity charts
- docs: tighten plan task-brief cross-refs and seq-gap assertion
- docs: audit log redesign implementation plan
- docs: audit log redesign spec (event-sourced per-project WAL)
- docs: plan project summary charts
- docs: design project summary charts
- docs: onboarding v1 spec and implementation plan
- update Help and Convention layout
- docs: add three-pane tui implementation plan
- docs: specify three-pane tui workspace
- fix layout and themes
- docs: add TUI theme refresh plan
- docs: add TUI theme refresh design
- chore: gofmt canonicalization (trailing newlines + alignment) + add label-management plan doc
- spec: label management refinement — dedicated Labels tab, multi-label task create, default seeding, agent code-of-conduct
- update plan
- v2: amend spec facets JSON shape + fix label form titles + keymap b row
- add spec
- spec: add Onboarding & conventions (Section 7) + atm conventions cmd + tui auto-init
- update spec
- spec: rewrite tasks-management v2 as pure label-substrate
- remove legacy plan
- spec: rewrite tasks-management v2 as single superpowers design file
- spec: rewrite 001-tasks-management to v2.0.0
- feat: introduce tui workspace focus
- Add lazygit-style TUI rewrite design
- replace speckit with superpowers


# Changelog

All notable changes to atm are documented here. The first release section
will be prepended by `scripts/release.sh` phase 3.
