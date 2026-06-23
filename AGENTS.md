# AGENTS.md

Guidance for any AI agent working in this repository. These rules are **agent-agnostic**: they apply regardless of which coding assistant (Claude Code, Codex, Cursor, Gemini, Copilot, opencode, …) is driving.

## 1. Spec-Driven Development is mandatory

This repo uses **[Spec Kit](https://github.com/github/spec-kit)** for its software-development lifecycle (SDLC). Specs are the source of truth; code follows specs.

- Initialize once: `./setup.sh` (installs the `specify` CLI).
- Day-to-day workflow lives under the `.specify/` directory that `specify` creates: `memory/` (constitution, plans, specs), `templates/`, `scripts/`, `workflows/`, etc. Slash commands live in `.agents/commands/`.
- Before writing or changing code, check for an existing spec in `specs/`. If none exists, draft one via `specify` before implementing.
- Never edit or delete generated artifacts outside the documented Spec Kit workflow.

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
2. Find or author the corresponding spec via `specify`.
3. Implement against the spec.
4. Verify: run lint/typecheck/tests before declaring done.
5. Update the spec only if requirements genuinely changed; never to match code.

## 4. Tooling prerequisites

- `uv` — required by Spec Kit (`specify` is installed as a uv tool).
- `./setup.sh` — run once to install `specify` at the pinned version.

## 5. Conventions

- Keep the API surface stable and versioned; the TUI consumes it.
- No emojis in code or commits.
- Follow existing style in neighboring files.

<!-- SPECKIT START -->
For additional context about technologies to be used, project structure,
shell commands, and other important information, read the current plan
<!-- SPECKIT END -->
