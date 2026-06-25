# AGENTS.md

Guidance for any AI agent working in this repository. These rules are **agent-agnostic**: they apply regardless of which coding assistant (Claude Code, Codex, Cursor, Gemini, Copilot, opencode, …) is driving.

## 1. Superpowers-driven development is mandatory

This repo uses **Superpowers** for its software-development lifecycle (SDLC). Specs/design docs are the source of truth; code follows the approved design and implementation plan.

- Initialize once: `./setup.sh` (validates the local repo workflow; Superpowers itself is provided by the agent environment).
- Day-to-day design artifacts live under `docs/superpowers/specs/`.
- Before writing or changing code, check for an existing Superpowers design/spec in `docs/superpowers/specs/`. If none exists, use the Superpowers brainstorming and planning workflow before implementing.
- Keep design docs honest: update them only when requirements genuinely change; never rewrite requirements just to match code.

## 2. Agent configuration lives in `.agents/`

Anything agent-specific — skills, commands, subagents, prompts, permission rules — is stored under `.agents/`. Keep it that way so the repo stays agent-agnostic:

```
.agents/
  skills/      # reusable, agent-agnostic skill definitions
  commands/    # project-scoped commands / slash-commands
  subagents/   # subagent role definitions
```

- Do **not** scatter agent config into `.cursor/`, `.claude/`, `.opencode/`, `.github/copilot/`, etc. If a specific agent needs a thin shim, that shim should only point back into `.agents/`.
- Skills/commands/subagents must be written generically (no vendor-proprietary features) so they can be reused across agents.

## 3. Workflow

1. Pick a task from the task system (this repo dogfoods itself).
2. Find or author the corresponding Superpowers design/spec.
3. Write or follow the implementation plan.
4. Verify: run lint/typecheck/tests before declaring done.
5. Update the spec only if requirements genuinely changed; never to match code.

## 4. Tooling prerequisites

- Go 1.22+.
- `make verify` for the repository verification gate.
- `./setup.sh` — optional local sanity check for required commands.

## 5. Conventions

- Keep the API surface stable and versioned; the TUI consumes it.
- No emojis in code or commits.
- Follow existing style in neighboring files.

<!-- SUPERPOWERS START -->
For additional context about technologies to be used, project structure,
shell commands, and other important information, read the current plan
at docs/superpowers/specs/001-tasks-management/plan.md (Tasks Management System). Key facts:
- Language: Go 1.22+; single binary `atm` (CLI + Bubble Tea TUI).
- Layers: internal/store (stable in-process API), internal/cli (stable out-of-process API via cobra), internal/tui (thin client over store).
- Storage: machine-global text files under `$ATM_HOME` (default ~/.config/atm; one file per task JSON; per-project file locking; no DB; detachable by directory copy). A project is NOT 1:1 with a repo.
- Guide: each project has an optional Guide (the always-read agent-context harness); `next`/`show --with-context` return it alongside per-task label-matched convention docs.
- TUI: `atm tui` is a first-class management surface that mirrors every CLI op (FR-002); see docs/superpowers/specs/001-tasks-management/tui-mockups.md + contracts/tui.md.
- Spec/design artifacts: docs/superpowers/specs/001-tasks-management/{spec,plan,research,data-model,quickstart,tui-mockups}.md + contracts/{cli,tui}.md.
- Verify: `make verify` (or `make build && make test`) before declaring done.
<!-- SUPERPOWERS END -->
