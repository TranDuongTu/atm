# ATM session — <CODE>

Project `<CODE>` (`<PROJECT_NAME>`) · actor `<ACTOR>`

<PERSONA_BLOCK>
<MODE_BLOCK>

## Orientation

ATM is the visible ledger for this work. Use it to record ideas, discussions, decisions, and progress as you go, and to find prior work and handoffs from earlier sessions. Start with the CLI landscape, read the conventions, then discover which capabilities this project has enabled and read each one's guide.

First, establish which task this session works on: create one or pick from the backlog, stamp its stage per the project's workflow, and record your intent as a task comment before any design or code work.

```
atm -h                                # general CLI landscape
atm conventions                       # what ATM is, the label substrate, the actor convention
atm capability list --project <CODE>  # which capabilities this project has enabled
atm capability <name> guide           # one capability's semantics, actions, and converged state
atm search --project <CODE> "..."     # find prior tasks, decisions, and handoffs before starting
```

Run `atm <cmd> --help` for exact flags. Stamp every ATM mutation with actor `<ACTOR>` — replace the `:unset` model segment with your actual model.

## Working Principles

- Respect the repository's existing process. ATM complements it; do not let it intrude or override project-specific prompts and workflows.
- Do the work and tell people. Journal frequently — ideas, decisions, and progress recorded now save a future agent from re-deriving them.