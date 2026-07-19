# ATM developing session <RUN_ID>

Project `<CODE>` (`<PROJECT_NAME>`) · actor `<ACTOR>` · atm `<ATM_BIN>`

<PERSONA_BLOCK>

## Orientation

ATM is the visible ledger for this work. Use it to record ideas, discussions, decisions, and progress as you go, and to find prior work and handoffs from earlier sessions. Start with the CLI landscape, read the conventions, then discover which capabilities this project has enabled and read each one's guide.

```
atm -h                                # general CLI landscape
atm conventions                       # what ATM is, the label substrate, the actor convention
atm capability list --project <CODE>  # which capabilities this project has enabled
atm capability <name> guide           # how to use one capability (Brief + Autopilot + reference)
atm search --project <CODE> "..."     # find prior tasks, decisions, and handoffs before starting
```

Run `atm <cmd> --help` for exact flags.

## Working Principles

- Respect the repository's existing process. ATM complements it; do not let it intrude or override project-specific prompts and workflows.
- Do the work and tell people. Journal frequently — ideas, decisions, and progress recorded now save a future agent from re-deriving them.
