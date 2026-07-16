package store

import (
	"encoding/json"
	"os"
	"path/filepath"

	"atm/libs/eventsource"
)

// localInstanceMarker is written to the store root (NOT store.json, and NOT
// governed by the store-meta lock's "only mutateStoreMeta writes store.json"
// rule below -- it is a separate file) after every call to
// ensureReplicaForWriteLocked. It records the identity this Store instance
// believed it owned the last time it authored an event, plus the
// filesystem path it was writing from.
//
// Detection rule: a directory copy (cp -r, rsync, tar extract-to-new-path,
// a cloned VM disk, ...) carries the marker file along with it. If the
// marker's StoreInstanceID still matches store.json's StoreInstanceID but
// the marker's StorePath no longer matches the store root actually being
// opened, this Store instance is a copy of the one that wrote the marker --
// re-mint the replica id before authoring anything, so the two copies never
// stamp events under the same replica id.
//
// KNOWN LIMITATION (carried forward from the ATM-0107 Task 10 plan): a copy
// restored at the IDENTICAL path -- e.g. an in-place backup/restore, or a
// second machine deliberately mirroring the original's directory layout --
// defeats this check. The marker's StorePath equals the copy's s.Root, so
// the mismatch never fires and the copy silently reuses the original's
// replica id. Closing this gap needs an identity signal that does NOT
// travel with a copied directory: machine identity (e.g. /etc/machine-id),
// a filesystem UUID, or an inode/device fingerprint of a file the copy
// necessarily recreates. All of those were considered and rejected for this
// task:
//   - machine-id / filesystem UUID: not portable (Linux-specific or
//     absent in containers/CI), and legitimately migrating a store to new
//     hardware would then ALWAYS look like a copy -- a new failure mode
//     (spurious remints) traded for closing a narrower one.
//   - inode/device fingerprint of the marker file itself: cheap in theory,
//     but the fingerprint has to be captured AFTER the file is written
//     (you cannot know your own inode before creation), which reopens a
//     second read/stat race window inside the very lock this function
//     takes to avoid one, and still does not survive filesystems that
//     legitimately reuse or virtualize inodes (network/overlay FS, some
//     container runtimes) -- it would swap a documented, narrow gap for an
//     undocumented, platform-dependent one.
//
// Per the task brief, this known limitation is accepted and documented
// rather than "fixed": it is real, but narrow (same-path restores/clones
// on the SAME hostname or an intentionally-identical path), and every
// alternative considered trades it for a new, less predictable failure
// mode. See the ATM-0107 Task 10 report for the full analysis.
type localInstanceMarker struct {
	StoreInstanceID string `json:"store_instance_id"`
	ReplicaID       string `json:"replica_id"`
	StorePath       string `json:"store_path"`
}

// localInstanceMarkerPath is store-local, per-directory state, not
// store-wide state: it never participates in the byte-identical v1 log or
// the eventsource frontier, and it is fully derived at write time -- its
// ABSENCE (a fresh store, or a marker deleted out from under this store)
// never blocks authoring; it just means detection is skipped for that
// write, which is the same as never having had a marker at all.
func (s *Store) localInstanceMarkerPath() string {
	return filepath.Join(s.Root, ".atm-local-instance.json")
}

// ensureReplicaForWriteLocked returns the replica id this Store instance
// should stamp on the NEXT authored event, re-minting it first if
// localInstanceMarker's detection rule (see its doc comment, including the
// documented identical-path limitation) says this store root looks like a
// copy of another instance that already authored under the current
// store.json's replica/instance ids.
//
// Caller must already hold this store's PROJECT lock -- v2 authoring's lock
// order is project -> store-meta (eventsource_meta.go:100-101). This
// function itself takes the store-meta lock via mutateStoreMeta, which is a
// DIFFERENT lock name and therefore safe to nest: WithLock is only
// non-reentrant on the SAME name (lock.go:22-39). The whole
// detect-and-possibly-remint-and-persist sequence -- reading store.json,
// reading the marker, deciding, writing store.json, writing the marker --
// runs inside that single store-meta critical section, so two projects on
// the same store instance calling this concurrently cannot interleave and
// observe/write a torn marker or a stale store.json.
//
// Re-minting only changes what FUTURE events are stamped with; it never
// rewrites or removes anything already appended to events.v2.jsonl (this
// function does not touch project event files at all). Consequences of a
// misfire, either direction:
//   - False positive (detection fires on a store that was never actually
//     copied -- cannot happen via the rule above without an actual path
//     change, but consider it anyway): harmless. The store simply starts a
//     new replica lineage for its own future writes, indistinguishable
//     from a brand-new store picking its first replica id. No data is
//     lost; convergence with itself is trivially preserved since nothing
//     else is authoring under the old id.
//   - False negative (the documented identical-path copy): also does not
//     corrupt anything by itself, but the two copies WILL author future
//     events under the same replica id, which is the condition this
//     function exists to prevent -- see the KNOWN LIMITATION above.
func (s *Store) ensureReplicaForWriteLocked() (string, error) {
	var replicaID string
	err := s.mutateStoreMeta(func(m *StoreMeta) error {
		if m.StoreInstanceID == "" {
			minted, err := eventsource.MintReplicaID(s.replicaEntropy)
			if err != nil {
				return err
			}
			m.StoreInstanceID = minted
		}
		if m.ReplicaID == "" {
			minted, err := eventsource.MintReplicaID(s.replicaEntropy)
			if err != nil {
				return err
			}
			m.ReplicaID = minted
		}

		var marker localInstanceMarker
		if raw, readErr := os.ReadFile(s.localInstanceMarkerPath()); readErr == nil {
			// Best-effort: a missing or malformed marker cannot prove a
			// copy happened, so it never blocks authoring -- it just
			// means the zero-value marker below never matches m below,
			// and detection is silently skipped for this write.
			_ = json.Unmarshal(raw, &marker)
		}
		if marker.StoreInstanceID != "" && marker.StoreInstanceID == m.StoreInstanceID && marker.StorePath != s.Root {
			minted, err := eventsource.MintReplicaID(s.replicaEntropy)
			if err != nil {
				return err
			}
			m.ReplicaID = minted
		}

		next := localInstanceMarker{
			StoreInstanceID: m.StoreInstanceID,
			ReplicaID:       m.ReplicaID,
			StorePath:       s.Root,
		}
		out, err := json.MarshalIndent(next, "", "  ")
		if err != nil {
			return err
		}
		out = append(out, '\n')
		if err := os.WriteFile(s.localInstanceMarkerPath(), out, 0o644); err != nil {
			return err
		}

		replicaID = m.ReplicaID
		return nil
	})
	if err != nil {
		return "", err
	}
	return replicaID, nil
}
