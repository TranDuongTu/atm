package store

// Transitional bridge to internal/store/eventlog while the carve is in
// flight (refactor step 6, ATM-3b873c). Tasks 4-6 shrink it; Task 9 deletes
// it. Nothing new may start depending on these names.

import "atm/internal/store/eventlog"

type StoreFormat = eventlog.StoreFormat
type StoreMeta = eventlog.StoreMeta
type ProjectEventsourceMeta = eventlog.ProjectEventsourceMeta
type V2FileSnapshot = eventlog.V2FileSnapshot

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
func (s *Store) removeProjectFormat(code string) error { return s.eng.RemoveProjectFormat(code) }
func (s *Store) SetActiveFormat(f eventlog.StoreFormat) error { return s.eng.SetActiveFormat(f) }
func (s *Store) ProjectFormatForCLI(code string) (eventlog.StoreFormat, error) {
	return s.eng.ProjectFormat(code)
}
func (s *Store) eventsV2Path(code string) string { return s.eng.EventsV2Path(code) }
func (s *Store) readV2File(code string, repairTail bool) (*eventlog.V2FileSnapshot, error) {
	return s.eng.ReadV2File(code, repairTail)
}
func (s *Store) readV2FileAt(path string, repairTail bool) (*eventlog.V2FileSnapshot, error) {
	return s.eng.ReadFileAt(path, repairTail)
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
