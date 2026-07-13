# TUI Refresh Recency Indicator Design

**Task:** ATM-0053

## Purpose

The TUI already refreshes in the background to pick up store changes made by
CLI or other processes. Users need a compact always-visible signal showing how
recently the current TUI snapshot was refreshed.

## Design

Add a rightmost status-bar segment for refresh freshness:

- `✓` in the success style when the last refresh is at most 10 seconds old.
- `✓` in the normal status style during the 10-15 second grace window.
- `↻ 16s ago`, `↻ 2m ago`, or `↻ 1h ago` in the warning style when the
  last refresh is more than 15 seconds old.

The model records `lastRefreshAt` every time `refreshAll()` completes. This
keeps startup, in-TUI mutations, and periodic background refresh ticks on the
same path. The indicator is informational only; it does not detect whether a
background process changed data, only how long ago the TUI reloaded its view of
the store.

The periodic background refresh interval is 10 seconds. The segment is appended
after all plugin dock segments so it is the rightmost status-line item and
does not repeatedly show a changing age while the TUI is fresh. Existing
status-line width behavior remains unchanged.

## Error Handling

If no refresh timestamp exists, render `↻ --`. Normal construction calls
`refreshAll()`, so this is only a defensive fallback.

## Testing

Add TUI tests for:

- the refresh tick interval is 10 seconds
- the status line renders the compact refresh indicator at the rightmost edge
- `refreshAll()` updates `lastRefreshAt`
- the fresh indicator is check-only and uses the success style through 10
  seconds
- the stale indicator shows `↻ <age> ago` only after 15 seconds

## Verification

Run focused TUI tests first, then the repository verification gate:

```bash
go test ./internal/tui
make verify
```
