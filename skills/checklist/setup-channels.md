---
persona: concierge
name: setup-channels
purpose: wire and verify this project's channels
---
1. Read the channel guide (`atm capability channel guide`); in a channel-scoped session, follow its scoped-session flow.
2. Run `atm channel list --project <CODE> --output json` and branch: no records — interview and author; records unwired — re-wire this machine; stale — re-verify.
3. For each channel touched: wire (`atm channel wire`), verify by actually reaching it with your own tools, then stamp (`atm channel stamp --note "..."`).
4. Offer to update the persona checklists that should reference the new channels by handle — a channel no checklist names will be forgotten.
5. Hand off with what is wired, stamped, and still outstanding.
