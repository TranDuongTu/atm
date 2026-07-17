package store

import (
	"context"
	"fmt"

	"atm/internal/core"
	"atm/internal/store/eventlog"
)

// Storage-maintenance facade: sync, upgrade, and prune-v1 all delegate to the
// event-log engine, which owns transport selection, the v1→v2 migration, and
// the legacy-log retirement. These thin adapters keep the store's public
// method names (which the tests still call) while the engine does the work.
// The report types live in core and these methods satisfy core.StorageAdmin,
// the interface cmd/atm injects into the cli.

// SyncProject reconciles code against the remote at url and returns the
// per-project outcome. Direction and dry-run come from opts.
func (s *Store) SyncProject(ctx context.Context, code, url string, opts core.SyncOptions) (*core.SyncReport, error) {
	return s.eng.SyncProject(ctx, code, url, opts)
}

// UpgradeProjectToV2 converts one v1-active project's frozen log.jsonl into v2
// media and cuts it over.
func (s *Store) UpgradeProjectToV2(code string) (*core.UpgradeReport, error) {
	return s.eng.UpgradeProject(code)
}

// UpgradeAllToV2 upgrades every v1-active project on disk, then flips the store
// default so new projects are born v2.
func (s *Store) UpgradeAllToV2() ([]core.UpgradeReport, error) { return s.eng.UpgradeAll() }

// PruneProjectV1 retires an upgraded project's frozen log.jsonl (archive by
// default; del=true removes it outright). The clean-cutover gate stays here on
// the facade: VerifyProject mixes the engine's strict fold with a read-cache
// consistency check, and that cache check is a facade concern the engine has
// no handle on. The engine primitive runs the gate under the project lock at
// the exact point the pre-carve prune did, so a file-clean-but-cache-stale
// project is still refused, byte-for-byte as before.
func (s *Store) PruneProjectV1(code string, del bool) (*core.PruneReport, error) {
	return s.eng.PruneLegacy(code, del, func() error {
		vr, err := s.VerifyProject(code)
		if err != nil {
			return err
		}
		if vr.Diverged || !vr.LogOK {
			return fmt.Errorf("%w: project %q does not verify clean; refusing to prune", core.ErrIntegrity, code)
		}
		return nil
	})
}

// ProjectCodes enumerates every project code on disk under projects/, sorted.
// It is the exported enumeration surface the CLI's `--all` verbs (prune-v1,
// sync) drive over; internally it delegates to projectCodesOnDisk, which the
// store's other on-disk enumeration paths (Verify, Rebuild) already use.
func (s *Store) ProjectCodes() ([]string, error) {
	return s.projectCodesOnDisk()
}

// core.StorageAdmin conformance: thin delegations to the exported methods
// above (which stay under their existing names — store tests call them
// directly) using the exact method names Task 8's cli flip consumes.
var _ core.StorageAdmin = (*Store)(nil)

func (s *Store) VerifyStorage() ([]core.VerifyReport, error) { return s.Verify() }
func (s *Store) VerifyStorageProject(code string) (*core.VerifyReport, error) {
	return s.VerifyProject(code)
}
func (s *Store) RebuildDerived() (*core.RebuildReport, error) { return s.Rebuild() }
func (s *Store) UpgradeStorage(code string) (*core.UpgradeReport, error) {
	return s.UpgradeProjectToV2(code)
}
func (s *Store) UpgradeAllStorage() ([]core.UpgradeReport, error) { return s.UpgradeAllToV2() }
func (s *Store) PruneLegacy(code string, del bool) (*core.PruneReport, error) {
	return s.PruneProjectV1(code, del)
}

// SetStorageFormat sets the store-wide default new projects are born into.
// SetActiveFormat's unknown-format error wraps core.ErrUsage as exactly
// `unknown store format %q`.
func (s *Store) SetStorageFormat(format string) error {
	return s.eng.SetActiveFormat(eventlog.StoreFormat(format))
}

// StorageFormat reports the effective format a single project is stored in.
func (s *Store) StorageFormat(code string) (string, error) {
	f, err := s.eng.ProjectFormat(code)
	return string(f), err
}

// SetActiveFormat sets the store-wide default new projects are born into,
// typed as eventlog.StoreFormat. Store tests drive format transitions through
// it directly (SetStorageFormat is the string-typed CLI-facing surface).
func (s *Store) SetActiveFormat(f eventlog.StoreFormat) error { return s.eng.SetActiveFormat(f) }

// ProjectFormatForCLI reports one project's effective typed format. Store
// tests assert against eventlog.StoreFormatV1/V2 through it.
func (s *Store) ProjectFormatForCLI(code string) (eventlog.StoreFormat, error) {
	return s.eng.ProjectFormat(code)
}

// ReadChangeLog renders a project's change history for display.
func (s *Store) ReadChangeLog(code string) ([]core.LogView, error) {
	return s.eng.DisplayLog(code)
}
