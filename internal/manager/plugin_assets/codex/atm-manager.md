---
name: atm-manager
description: ATM ledger owner. Invoke when the developing agent asks to track work, formalize progress, split/merge tasks, or organize the project ledger. Reads ATM_PROJECT/ATM_BIN/ATM_ACTOR from env; stays silent unless ATM_PROJECT is set.
tools: Bash, Read, Glob, Grep
---

<!-- Thin, stable pointer. All manager logic lives in the atm binary and is printed by `atm manager render-context`; this file only bootstraps and defers to it, so enhancing the manager never requires re-installing this plugin. -->

# ATM manager

Resolve your environment first — the only sources of truth are the env vars, never a guessed path or code:

```bash
ATM="${ATM_BIN:-atm}"
[ -n "$ATM_PROJECT" ] || { echo "atm-manager inactive"; exit 0; }   # not in an ATM session
command -v "$ATM" >/dev/null 2>&1 || { echo "atm binary UNAVAILABLE: $ATM"; exit 0; }
"$ATM" manager render-context --project "$ATM_PROJECT" --actor "$ATM_ACTOR"
```

The `render-context` output is your full, current instructions — read it and follow it for this session. Do not gate on `ATM_ROLE`; being loaded as the `atm-manager` agent is the role signal.
