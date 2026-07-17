package eventlog

import (
	"context"
	"path/filepath"

	"atm/internal/core"
	eventsync "atm/libs/eventsource/sync"
)

// SyncProject reconciles one project against the remote at url: transport
// selection and the set-union sync engine live HERE, so nothing above store
// ever names an event. The caller resolves remote names to URLs (that is
// project-config knowledge) and persists bootstrap origins from the report.
func (e *Engine) SyncProject(ctx context.Context, code, url string, opts core.SyncOptions) (*core.SyncReport, error) {
	target, err := eventsync.SelectTarget(filepath.Join(e.root, "remotes"), url)
	if err != nil {
		return nil, err
	}
	rep, err := eventsync.Sync(ctx, e, target, code, eventsync.Options{Pull: opts.Pull, Push: opts.Push, DryRun: opts.DryRun})
	if err != nil {
		return nil, err
	}
	out := &core.SyncReport{
		Project:        rep.Project,
		Pulled:         rep.Pulled,
		Pushed:         rep.Pushed,
		Bootstrapped:   rep.Bootstrapped,
		NewlyContested: rep.NewlyContested,
		RemoteAbsent:   rep.RemoteAbsent,
		DryRun:         rep.DryRun,
	}
	if rep.PushErr != nil {
		out.PushErr = rep.PushErr.Error()
	}
	return out, nil
}
