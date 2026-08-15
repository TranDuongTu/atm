---
persona: concierge
name: setup-agent-launcher
purpose: get opencode/codex/claude launching ATM sessions on this machine
---
1. Detect installed host agents: `atm agents list`, and check `claude`, `codex`, `opencode` on PATH.
2. Verify the selected agent's launcher and args (`atm agents list --output json`); fix the selection with `atm agents` verbs if needed.
3. Dry-run a render: `atm session-context --persona developer --project <CODE>` and confirm project, actor, and the Capabilities block appear.
4. If the user agrees, launch a short real session and confirm the env (ATM_PROJECT, ATM_PERSONA, ATM_CONTEXT_FILE) reaches the agent.
5. Brief the user on launching each persona, then hand off.
