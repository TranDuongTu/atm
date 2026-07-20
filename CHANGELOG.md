## Unreleased

### feat
- ATM-793b19: Recent Events feed in the TUI Projects pane — a git-log-style digest of the selected project's event stream (commit-graph gutter, event ids when space permits, per-action wording), with `L` subfocus scrolling.
- ATM-0083: agent as config, not flags. The host agent is now a stored default — choose it once with `atm agents select <name>` (`atm agents list` shows readiness; `atm agents args <name> -- <flags>` sets per-agent defaults), then launch with `atm dev --project <CODE>` and `atm manage --project <CODE> --<action>`. Override a single launch with `--agent <name>` or `ATM_AGENT`.
- ATM-0115: boards (computed labels). A label may carry an expression over other labels; its membership is computed, not asserted. New `--expr` flag on `atm label add` and `atm task list` (`--label <CODE>:<board>` resolves a board's expression). Label-name grammar widened so `:*` namespace wildcards are legal labels; `atm label seed` back-fills namespace descriptors into existing projects.

### changed
- ATM-0115: the Labels pane is now the Boards pane — a flat list of computed labels (boards + namespaces) with a live-validated expression editor.
- Removed per-agent launch subcommands (`atm claude|codex|opencode|ollama` and `atm manage <agent>`). Ollama variants are now catalog entries (`ollama:<integration>`), so the `--integration` flag is gone. `atm init` additionally records the first installed agent as the default.


## v1.2.11 - 2026-07-11

### docs
- docs: plan CLI user surface simplification
- docs: specify CLI user surface simplification
- ATM-0072: implementation plan for actor convention enforcement
- ATM-0072: spec for actor convention enforcement


## v1.2.10 - 2026-07-11

### docs
- ATM-0072: implementation plan for actor convention enforcement
- ATM-0072: spec for actor convention enforcement


## v1.2.9 - 2026-07-09


## v1.2.8 - 2026-07-09

### docs
- Merge ATM-0071: agent prompt minimization — thin developing nudges + principle-driven manager render-context pointer
- docs: reconcile D15 — no-config does not auto-open overlay (dock hint surfaces it)
- spec+plan: dock always visible (D14) + auto-start on project select (D15) + error auto-opens overlay (D16)
- spec+plan: dock keybind hint (D12) + bottom-anchored log pane (D13)
- docs: ATM-0071 agent prompt minimization implementation plan
- docs: ATM-0071 agent prompt minimization design spec
- plan: TUI indexer integration — 9-task TDD rollout (ATM-0071)
- spec: TUI indexer integration — plugin dock + indexer overlay (ATM-0071)


## v1.2.7 - 2026-07-09

### docs
- Merge ATM-0071: agent prompt minimization — thin developing nudges + principle-driven manager render-context pointer
- docs: reconcile D15 — no-config does not auto-open overlay (dock hint surfaces it)
- spec+plan: dock always visible (D14) + auto-start on project select (D15) + error auto-opens overlay (D16)
- spec+plan: dock keybind hint (D12) + bottom-anchored log pane (D13)
- docs: ATM-0071 agent prompt minimization implementation plan
- docs: ATM-0071 agent prompt minimization design spec
- plan: TUI indexer integration — 9-task TDD rollout (ATM-0071)
- spec: TUI indexer integration — plugin dock + indexer overlay (ATM-0071)
- Refine ATM-0057 design: verified embedding backend, project-declared model, run-once watcher, eval deferred
- Implementation plan: ATM memory substrate — retrieval + indexing + recall measurement (ATM-0057)
- Design spec: ATM as memory substrate — retrieval surface + indexing model + manager cognition (ATM-0057)


## v1.2.6 - 2026-07-09

### docs
- Refine ATM-0057 design: verified embedding backend, project-declared model, run-once watcher, eval deferred
- Implementation plan: ATM memory substrate — retrieval + indexing + recall measurement (ATM-0057)
- Design spec: ATM as memory substrate — retrieval surface + indexing model + manager cognition (ATM-0057)
- Fix ATM-0047 + ATM-0061: manager plugin staleness detection + developing gene boundary


## v1.2.5 - 2026-07-08

### docs
- Fix ATM-0047 + ATM-0061: manager plugin staleness detection + developing gene boundary
- Reconcile TUI chart with spec: sort by weight, empty-state text, layout mockup (ATM-0028)
- Update docs/smoke for manager onboarding + Ubiquitous Language (ATM-0028)
- Implementation plan: manager knowledge-base owner + onboarding unification + Ubiquitous Language (ATM-0028)
- Design spec: manager as knowledge-base owner (onboarding unification + Ubiquitous Language) (ATM-0028)


## v1.2.4 - 2026-07-08

### docs
- Reconcile TUI chart with spec: sort by weight, empty-state text, layout mockup (ATM-0028)
- Update docs/smoke for manager onboarding + Ubiquitous Language (ATM-0028)
- Implementation plan: manager knowledge-base owner + onboarding unification + Ubiquitous Language (ATM-0028)
- Design spec: manager as knowledge-base owner (onboarding unification + Ubiquitous Language) (ATM-0028)
- Implementation plan: persona activity in Projects pane + P overlay + p form (ATM-0054)
- Design spec: persona activity in Projects pane + P overlay + p add-persona (ATM-0054)
- Implementation plan: personas & actor activity (ATM-0052)
- Design spec: personas & actor activity (ATM-0052)


## v1.2.3 - 2026-07-08

### docs
- Implementation plan: persona activity in Projects pane + P overlay + p form (ATM-0054)
- Design spec: persona activity in Projects pane + P overlay + p add-persona (ATM-0054)
- Implementation plan: personas & actor activity (ATM-0052)
- Design spec: personas & actor activity (ATM-0052)
- Toggle exact-label filter on row Enter; move label detail to i key (ATM-0041)
- Add implementation plan for ATM-0041 labels namespace filtering
- Add design spec for ATM-0041 labels namespace filtering


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
