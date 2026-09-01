# ATM session — <CODE>

- Project: `<CODE>` (`<PROJECT_NAME>`)
- Actor: `<ACTOR>`
- Task: `<TASK_ID>`

<PERSONA_PROMPT>

<CHECKLISTS_SECTIONS>

<CAPABILITY_SCOPE>

<CAPABILITIES>

## Orientation

ATM is the visible ledger for this work. Use it to record ideas, discussions, decisions, and progress as you go, and to find prior work and handoffs from earlier sessions. Start with the CLI landscape and the conventions; the Capabilities section above lists what this project has enabled — read a capability's guide before relying on one.

First, establish which task this session works on: create one or pick from the backlog, stamp its stage per the project's workflow, and record your intent as a task comment before any design or code work.

```
atm -h                                # general CLI landscape
atm conventions                       # what ATM is, the label substrate, the actor convention
atm capability list --project <CODE>  # which capabilities this project has enabled
atm capability <name> guide           # one capability's semantics, actions, and converged state
atm search --project <CODE> "..."     # find prior tasks, decisions, and handoffs before starting
```

Run `atm <cmd> --help` for exact flags. Stamp every ATM mutation with actor `<ACTOR>` — replace the `:unset` model segment with your actual model.

## Persona and checklists

The persona prompt above is HOW you work — judgment, tone, principles, and communication. Any `## Checklist:` sections above are the operating procedure for WHAT you do: follow them in order, and let them win over your persona's generic routine wherever they overlap. If no checklist section is present, fall back to your persona's own routine. When told a checklist changed mid-session, re-read it with `atm checklist show` before continuing.
