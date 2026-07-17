package store

// Transitional bridge to internal/store/eventlog while the carve is in
// flight (refactor step 6, ATM-3b873c). Tasks 4-6 shrink it; Task 9 deletes
// it. Nothing new may start depending on these names.

import (
	"atm/internal/core"
	"atm/internal/store/eventlog"
)

type StoreFormat = eventlog.StoreFormat
type StoreMeta = eventlog.StoreMeta
type ProjectEventsourceMeta = eventlog.ProjectEventsourceMeta
type V2FileSnapshot = eventlog.V2FileSnapshot

// UpgradeReport/PruneReport now live in the engine (sync/upgrade/prune moved
// there in Task 6); these aliases keep the cli and store-package callers that
// still name store.X compiling until Task 7 relocates the types to core.
type UpgradeReport = eventlog.UpgradeReport
type PruneReport = eventlog.PruneReport

// ErrSyncNeedsV2 moved into the engine with sync.go; the alias preserves the
// store-package sentinel name for tests that match against it.
var ErrSyncNeedsV2 = eventlog.ErrSyncNeedsV2

// V2LogView aliases the storage-neutral read model the CLI still names; Task 8
// retargets the CLI at core.LogView directly and drops this.
type V2LogView = core.LogView

// ReadV2LogForDisplay delegates to the engine's DisplayLog; the CLI keeps
// calling this facade name until Task 8.
func (s *Store) ReadV2LogForDisplay(code string) ([]core.LogView, error) {
	return s.eng.DisplayLog(code)
}

const (
	StoreFormatV1 = eventlog.StoreFormatV1
	StoreFormatV2 = eventlog.StoreFormatV2
)

func (s *Store) readStoreMeta() (*eventlog.StoreMeta, error) { return s.eng.ReadStoreMeta() }
func (s *Store) mutateStoreMeta(fn func(m *eventlog.StoreMeta) error) error {
	return s.eng.MutateStoreMeta(fn)
}
func (s *Store) projectFormat(code string) (eventlog.StoreFormat, error) {
	return s.eng.ProjectFormat(code)
}
func (s *Store) dispatchFormat(code string) (eventlog.StoreFormat, error) {
	return s.eng.DispatchFormat(code)
}
func (s *Store) withProjectFormatLock(code string, want eventlog.StoreFormat, fn func() error) error {
	return s.eng.WithProjectFormatLock(code, want, fn)
}
func (s *Store) setProjectFormat(code string, f eventlog.StoreFormat) error {
	return s.eng.SetProjectFormat(code, f)
}
func (s *Store) removeProjectFormat(code string) error        { return s.eng.RemoveProjectFormat(code) }
func (s *Store) SetActiveFormat(f eventlog.StoreFormat) error { return s.eng.SetActiveFormat(f) }
func (s *Store) ProjectFormatForCLI(code string) (eventlog.StoreFormat, error) {
	return s.eng.ProjectFormat(code)
}
func (s *Store) eventsV2Path(code string) string { return s.eng.EventsV2Path(code) }
func (s *Store) readV2File(code string, repairTail bool) (*eventlog.V2FileSnapshot, error) {
	return s.eng.ReadV2File(code, repairTail)
}
func (s *Store) verifyV2File(code string) (*eventlog.V2FileSnapshot, error) {
	return s.eng.VerifyFile(code)
}
func (s *Store) appendV2EventLineLocked(code string, raw []byte) error {
	return s.eng.AppendEventLineLocked(code, raw)
}
func (s *Store) ensureReplicaForWriteLocked() (string, error) {
	return s.eng.EnsureReplicaForWriteLocked()
}
