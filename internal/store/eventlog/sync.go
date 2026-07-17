package eventlog

import (
	"errors"
	"os"

	"atm/internal/core"
	"atm/libs/eventsource"
)

// ErrSyncNeedsV2 is returned when a sync operation targets a project whose
// media is still v1: L4 sync is defined over the event DAG (events.v2.jsonl),
// so a v1-active project must be upgraded before it can participate.
var ErrSyncNeedsV2 = errors.New(`project is v1-active and cannot sync; run "atm store upgrade" first`)

// reprojectLocked folds what is now on disk for code and hands it to the
// facade's read-model hook — the engine-internal projection at the exact
// points the pre-carve code called reprojectV2Locked (sync ingest/bootstrap,
// upgrade). A nil hook (an engine wired without a read model) is a no-op.
// Caller MUST hold the project lock.
func (e *Engine) reprojectLocked(code string) error {
	if e.opts.OnProject == nil {
		return nil
	}
	snap, err := e.Snapshot(code)
	if err != nil {
		return err
	}
	return e.opts.OnProject(code, snap)
}

// SyncSnapshot returns the full ordered event set the peer needs to reconcile
// against this store's copy of code. An absent project (no media in any format)
// reports absent=true with no error -- the caller treats it as "nothing here
// yet" and may bootstrap. A project that exists but is not v2 is refused with
// ErrSyncNeedsV2: there is no event DAG to send.
//
// This is a lock-free read: the returned events are a point-in-time view, and
// the transport layer (Task 7) is responsible for any snapshot-consistency
// guarantees. ReadV2File(repairTail=false) is deliberately strict -- a sync
// read never silently truncates an uncommitted tail.
func (e *Engine) SyncSnapshot(code string) (events []*eventsource.Event, absent bool, err error) {
	switch err := e.MediaExists(code); {
	case err == nil:
		// MediaExists returns nil ONLY when neither log.jsonl nor
		// events.v2.jsonl is on disk, i.e. the project is genuinely absent.
		return nil, true, nil
	case errors.Is(err, core.ErrConflict):
		// Present in some medium -- the ErrConflict "already exists" is the
		// signal we want here; fall through to the format check.
	default:
		return nil, false, err
	}
	f, err := e.DispatchFormat(code)
	if err != nil {
		return nil, false, err
	}
	if f != StoreFormatV2 {
		return nil, false, ErrSyncNeedsV2
	}
	snap, err := e.ReadV2File(code, false)
	if err != nil {
		return nil, false, err
	}
	return snap.Events, false, nil
}

// SyncIngest appends the incoming events this store does not already hold,
// reprojects the cache, and reports how many events were written and how many
// slots became contested as a result. incoming arrives topologically ordered
// (the transport plans it), but the have-set is recomputed under the project
// lock so a concurrent local writer cannot cause a double-append.
//
// The HLC high-water mark is advanced exactly the way local authoring advances
// it (the engine's commitAuthorLocked): the observed maximum ingested stamp is persisted
// verbatim -- no artificial logical bump -- so a subsequent local author sorts
// after everything just received and convergence stays deterministic.
func (e *Engine) SyncIngest(code string, incoming []*eventsource.Event) (ingested, newlyContested int, err error) {
	err = e.WithLock(code, func() error {
		snap, err := e.ReadV2File(code, false)
		if err != nil {
			return err
		}
		before, err := eventsource.FoldEvents(snap.Events)
		if err != nil {
			return err
		}
		have := make(map[string]bool, len(snap.Events))
		for _, ev := range snap.Events {
			have[ev.ID] = true
		}
		all := snap.Events
		var maxHLC eventsource.HLC
		for _, ev := range incoming {
			if have[ev.ID] {
				continue
			}
			if err := e.AppendEventLineLocked(code, ev.Raw); err != nil {
				return err
			}
			have[ev.ID] = true
			all = append(all, ev)
			ingested++
			if ev.HLC.Compare(maxHLC) > 0 {
				maxHLC = ev.HLC
			}
		}
		if ingested == 0 {
			return nil
		}
		// Persist the observed high-water mark under the store-scoped lock, the
		// same read-modify-write the engine's commitAuthorLocked performs after a local
		// append. Copy maxHLC before taking its address so &h never aliases a
		// value that changes underneath the stored pointer.
		if err := e.MutateStoreMeta(func(m *StoreMeta) error {
			if m.LastHLC == nil || maxHLC.Compare(*m.LastHLC) > 0 {
				h := maxHLC
				m.LastHLC = &h
			}
			return nil
		}); err != nil {
			return err
		}
		after, err := eventsource.FoldEvents(all)
		if err != nil {
			return err
		}
		newlyContested = contestedDelta(before.Contested, after.Contested)
		return e.reprojectLocked(code)
	})
	return ingested, newlyContested, err
}

// SyncBootstrap materializes a brand-new project from a peer's full event set.
// It refuses to run against any project that already has media on disk --
// bootstrap is a create, never an overwrite, and clobbering a live file would
// lose local history. The format entry is written BEFORE the first append so
// no read path can ever observe v2 media without its explicit v2 entry (the
// same crash-window ordering createProjectV2 uses). Events are appended in the
// given (topological) order, the HLC high-water mark is seeded from them so a
// later local author is monotonic, and the cache is reprojected.
func (e *Engine) SyncBootstrap(code string, incoming []*eventsource.Event) error {
	if err := e.MediaExists(code); err != nil {
		// Non-nil means the project already exists (ErrConflict) or a real
		// stat error; either way bootstrap must not proceed.
		return err
	}
	if err := os.MkdirAll(e.projectDir(code), 0o755); err != nil {
		return err
	}
	return e.WithLock(code, func() error {
		// Establish the format before any append: a crash leaving the entry
		// with an empty/absent file reads as an empty v2 project (benign),
		// whereas v2 media with no entry would read as v1 and block recreation.
		if err := e.SetProjectFormat(code, StoreFormatV2); err != nil {
			return err
		}
		var maxHLC eventsource.HLC
		for _, ev := range incoming {
			if err := e.AppendEventLineLocked(code, ev.Raw); err != nil {
				return err
			}
			if ev.HLC.Compare(maxHLC) > 0 {
				maxHLC = ev.HLC
			}
		}
		if len(incoming) > 0 {
			if err := e.MutateStoreMeta(func(m *StoreMeta) error {
				if m.LastHLC == nil || maxHLC.Compare(*m.LastHLC) > 0 {
					h := maxHLC
					m.LastHLC = &h
				}
				return nil
			}); err != nil {
				return err
			}
		}
		return e.reprojectLocked(code)
	})
}

// contestedDelta counts the slots contested in after but not in before, keyed
// by the (entity, kind, field) triple a ContestedSlot names. It is how
// SyncIngest reports whether a freshly-ingested event introduced a divergent
// write to a slot that was previously settled.
func contestedDelta(before, after []eventsource.ContestedSlot) int {
	key := func(c eventsource.ContestedSlot) string {
		return c.Entity + "\x00" + c.Kind + "\x00" + c.Field
	}
	had := make(map[string]bool, len(before))
	for _, c := range before {
		had[key(c)] = true
	}
	n := 0
	for _, c := range after {
		if !had[key(c)] {
			n++
		}
	}
	return n
}
