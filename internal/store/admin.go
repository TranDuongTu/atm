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
// method names (which the cli and tests still call) while the engine does the
// work. Task 7 relocates the report types to core and folds these behind
// core.StorageAdmin.

// SyncProject reconciles code against the remote at url and returns the
// per-project outcome. Direction and dry-run come from opts.
func (s *Store) SyncProject(ctx context.Context, code, url string, opts core.SyncOptions) (*core.SyncReport, error) {
	return s.eng.SyncProject(ctx, code, url, opts)
}

// UpgradeProjectToV2 converts one v1-active project's frozen log.jsonl into v2
// media and cuts it over.
func (s *Store) UpgradeProjectToV2(code string) (*eventlog.UpgradeReport, error) {
	return s.eng.UpgradeProject(code)
}

// UpgradeAllToV2 upgrades every v1-active project on disk, then flips the store
// default so new projects are born v2.
func (s *Store) UpgradeAllToV2() ([]eventlog.UpgradeReport, error) { return s.eng.UpgradeAll() }

// PruneProjectV1 retires an upgraded project's frozen log.jsonl (archive by
// default; del=true removes it outright). The clean-cutover gate stays here on
// the facade: VerifyProject mixes the engine's strict fold with a read-cache
// consistency check, and that cache check is a facade concern the engine has
// no handle on. The engine primitive runs the gate under the project lock at
// the exact point the pre-carve prune did, so a file-clean-but-cache-stale
// project is still refused, byte-for-byte as before.
func (s *Store) PruneProjectV1(code string, del bool) (*eventlog.PruneReport, error) {
	return s.eng.PruneLegacy(code, del, func() error {
		vr, err := s.VerifyProject(code)
		if err != nil {
			return err
		}
		if vr.Diverged || !vr.LogOK {
			return fmt.Errorf("%w: project %q does not verify clean; refusing to prune", ErrIntegrity, code)
		}
		return nil
	})
}

// ProjectCodes enumerates every project code on disk under projects/, sorted.
// It is the exported enumeration surface the CLI's `--all` verbs (prune-v1,
// sync) drive over; internally it delegates to projectCodesOnDisk, which every
// other store method (Verify, UpgradeAll, Rebuild) already uses.
func (s *Store) ProjectCodes() ([]string, error) {
	return s.projectCodesOnDisk()
}
