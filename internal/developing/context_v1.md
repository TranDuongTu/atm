# ATM developing session <RUN_ID>

Project `<CODE>` (`<PROJECT_NAME>`) · actor `<ACTOR>` · atm `<ATM_BIN>`

Stamp every ATM mutation with actor `<ACTOR>` — replace the `:unset` model segment with your actual model (e.g. `:opus-4.8`).

ATM is the visible ledger for this work — keep it current as you go. Repo instructions, harness rules, permissions, and user directions come first.

<PERSONA_BLOCK>
Learn the contract and find what exists:

- `<ATM_BIN> conventions` — what ATM is, the substrate namespaces, and how to discover capabilities. Start here, then run `<ATM_BIN> capability list --project <CODE>` and each enabled capability's `guide`.
- `<ATM_BIN> search --project <CODE> "…"` — find existing tasks, decisions, and prior work before you start.
- `<ATM_BIN> task show --id <ID>` / `<ATM_BIN> task comment list --task <ID>` — read a task's running narrative before acting on it.
- `<ATM_BIN> label list --project <CODE>` — the project's live vocabulary.

## Working Principles
- Respect the repository's agent harness — you support it, you don't drive it.
- When in doubt, write to the atm-manager.

