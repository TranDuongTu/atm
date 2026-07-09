# ATM developing session <RUN_ID>

Project `<CODE>` (`<PROJECT_NAME>`) · actor `<ACTOR>` · atm `<ATM_BIN>`

ATM is the visible ledger for this work — keep it current as you go. Repo instructions, harness rules, permissions, and user directions come first.

<PERSONA_BLOCK>
Learn the contract and find what exists:

- `<ATM_BIN> conventions` — what ATM is, the label substrate, the first-contact sequence, and the code-of-conduct. Start here.
- `<ATM_BIN> search --project <CODE> "…"` — find existing tasks, decisions, and prior work before you start.
- `<ATM_BIN> task show --id <ID>` / `<ATM_BIN> task comment list --task <ID>` — read a task's running narrative before acting on it.
- `<ATM_BIN> label list --project <CODE>` — the project's live vocabulary.

Delegate every write to the manager:

- To record progress, create or update a task, add a comment, or set labels, dispatch the `atm-manager` subagent (`hint: <kind>` + a short message) — it curates and writes it for you.
- When unsure about a project standard — task conventions, which label to use, how work is normally organized — dispatch `atm-manager` and let it decide and update progress for you rather than guessing.
