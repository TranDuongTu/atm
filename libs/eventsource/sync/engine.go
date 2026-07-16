package eventsync

import (
	"context"
	"fmt"
	"slices"

	"atm/libs/eventsource"
)

// PlanResult is the outcome of diffing a local event set against a
// remote snapshot of the same project: the events each side is missing,
// in the shape its consumer needs (decoded events to append locally,
// raw wire lines to publish remotely).
type PlanResult struct {
	ToIngest     []*eventsource.Event // topological order, ready to append
	ToPublish    []RawEvent
	RemoteAbsent bool
	LocalAbsent  bool
}

// Plan diffs local against remote and reports what each side is
// missing. Remote event ids are never trusted from the wire: every
// RawEvent is re-parsed via eventsource.Parse, which recomputes its id
// from Raw and rejects malformed lines. The union of both sides is
// validated as one DAG (every parent present, acyclic) before any
// diffing happens, so a remote event whose parent is absent from both
// sides is refused up front, as is a project.created root mismatch
// between the two sides (or within either one).
func Plan(local []*eventsource.Event, remote *RemoteSnapshot) (*PlanResult, error) {
	res := &PlanResult{RemoteAbsent: remote.Absent, LocalAbsent: len(local) == 0}

	localByID := make(map[string]*eventsource.Event, len(local))
	for _, e := range local {
		localByID[e.ID] = e
	}

	remoteByID := make(map[string]*eventsource.Event, len(remote.Events))
	union := slices.Clone(local)
	for _, re := range remote.Events {
		ev, err := eventsource.Parse(re.Raw) // recomputes id; rejects bad lines
		if err != nil {
			return nil, fmt.Errorf("eventsync: remote event: %w", err)
		}
		if remoteByID[ev.ID] != nil {
			continue // dedupe repeats on the wire
		}
		remoteByID[ev.ID] = ev
		if localByID[ev.ID] == nil {
			union = append(union, ev)
		}
	}

	dag, err := eventsource.BuildDAG(union) // validates parents + acyclicity over the union
	if err != nil {
		return nil, fmt.Errorf("eventsync: staged validation: %w", err)
	}

	localRoot, err := rootOf(local)
	if err != nil {
		return nil, err
	}
	remoteRoot, err := rootOf(mapValues(remoteByID))
	if err != nil {
		return nil, err
	}
	if localRoot != "" && remoteRoot != "" && localRoot != remoteRoot {
		return nil, fmt.Errorf("%w: local %s, remote %s", ErrRootMismatch, localRoot, remoteRoot)
	}

	for _, e := range dag.Events() { // topo order, deterministic
		if localByID[e.ID] == nil {
			res.ToIngest = append(res.ToIngest, e)
		}
	}
	for _, e := range local {
		// Publish everything when the remote is absent; otherwise publish
		// only the local events the remote doesn't already have.
		if remote.Absent || remoteByID[e.ID] == nil {
			res.ToPublish = append(res.ToPublish, RawEvent{ID: e.ID, Raw: e.Raw})
		}
	}
	return res, nil
}

// LocalStore is the store surface the orchestrator drives: read the full
// event set for a project, append events another replica has that we lack,
// and lay down a project we've never seen. It is deliberately narrow so
// the orchestrator stays decoupled from the concrete store; *store.Store
// satisfies it structurally.
type LocalStore interface {
	// SyncSnapshot returns the project's full local event set and whether
	// the project is absent locally (true = not present; the event slice
	// is then empty).
	SyncSnapshot(project string) ([]*eventsource.Event, bool, error)
	// SyncIngest appends events into an existing local project, reporting
	// how many were newly written and how many became newly contested.
	SyncIngest(project string, incoming []*eventsource.Event) (ingested, newlyContested int, err error)
	// SyncBootstrap lays down a project that does not exist locally yet.
	SyncBootstrap(project string, incoming []*eventsource.Event) error
}

// Options selects sync direction and whether to mutate anything. Pull
// ingests what the remote has and we lack; Push publishes what we have and
// it lacks. Neither set means both (the bidirectional default). DryRun
// computes and reports counts without touching either side.
type Options struct {
	Pull, Push, DryRun bool
}

// Report is the outcome of one Sync. Pulled/Pushed count events actually
// moved (or, under DryRun, that would move). PushErr carries a push
// failure that followed a committed pull: pulling succeeded and is
// reported, but the publish leg failed — a legal, non-fatal state under
// the L4 failure model, distinct from the error Sync returns.
type Report struct {
	Project        string
	Pulled, Pushed int
	Bootstrapped   bool
	NewlyContested int
	RemoteAbsent   bool
	DryRun         bool
	PushErr        error // pull committed, push failed — reported, not returned
}

// Sync reconciles one project between the local store and a remote target.
// It reads both sides, runs the pure Plan diff, then applies the plan
// according to opt: ingesting (or bootstrapping) what Pull found missing
// locally and publishing what Push found missing remotely.
//
// A Narrowing target lets a fully bidirectional sync short-circuit: if the
// remote's frontier digest already equals ours, both replicas hold the
// same events and Sync returns a zero-change Report without fetching the
// log at all.
//
// A push that fails after a pull committed is reported in Report.PushErr,
// not returned — the pulled events are real and worth reporting. Sync
// returns an error only when it accomplished nothing: a bare push that
// failed, or a project absent on both sides.
func Sync(ctx context.Context, local LocalStore, target SyncTarget, project string, opt Options) (*Report, error) {
	if !opt.Pull && !opt.Push {
		opt.Pull, opt.Push = true, true
	}
	report := &Report{Project: project, DryRun: opt.DryRun}

	localEvents, _, err := local.SyncSnapshot(project)
	if err != nil {
		return nil, fmt.Errorf("eventsync: local snapshot: %w", err)
	}

	// Digest short-circuit: a Narrowing target reports its full-set digest
	// cheaply. When we're syncing both ways and it already matches ours,
	// there is nothing to move — skip the Fetch entirely.
	if n, ok := target.(Narrowing); ok && opt.Pull && opt.Push {
		digest, _, err := n.Frontier(ctx, project)
		if err != nil {
			return nil, fmt.Errorf("eventsync: frontier: %w", err)
		}
		if digest != "" && digest == SetDigest(eventIDs(localEvents)) {
			return report, nil
		}
	}

	snap, err := target.Fetch(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("eventsync: fetch: %w", err)
	}

	plan, err := Plan(localEvents, snap)
	if err != nil {
		return nil, err
	}
	report.RemoteAbsent = plan.RemoteAbsent

	if plan.LocalAbsent && plan.RemoteAbsent {
		return nil, fmt.Errorf("eventsync: nothing to sync: project %q absent locally and remotely", project)
	}

	if opt.DryRun {
		if opt.Pull {
			report.Pulled = len(plan.ToIngest)
			report.Bootstrapped = plan.LocalAbsent && len(plan.ToIngest) > 0
		}
		if opt.Push {
			report.Pushed = len(plan.ToPublish)
		}
		return report, nil
	}

	if opt.Pull && len(plan.ToIngest) > 0 {
		if plan.LocalAbsent {
			if err := local.SyncBootstrap(project, plan.ToIngest); err != nil {
				return nil, fmt.Errorf("eventsync: bootstrap: %w", err)
			}
			report.Bootstrapped = true
			report.Pulled = len(plan.ToIngest)
		} else {
			ingested, newlyContested, err := local.SyncIngest(project, plan.ToIngest)
			if err != nil {
				return nil, fmt.Errorf("eventsync: ingest: %w", err)
			}
			report.Pulled = ingested
			report.NewlyContested = newlyContested
		}
	}

	if opt.Push && len(plan.ToPublish) > 0 {
		if err := target.Publish(ctx, project, plan.ToPublish, snap); err != nil {
			// A push failure is fatal only in push-only mode: with no pull
			// leg, the failed publish is the whole sync, so it's a real
			// error. When pull was enabled the sync is bidirectional and
			// "pulled OK, push failed" is a legal reported state (L4-10) —
			// even if pull happened to move nothing, that's a successful
			// no-op pull, so we report PushErr and return a nil error.
			if !opt.Pull {
				return nil, fmt.Errorf("eventsync: publish: %w", err)
			}
			report.PushErr = err
		} else {
			report.Pushed = len(plan.ToPublish)
		}
	}

	return report, nil
}

// eventIDs collects event ids, for feeding a set into SetDigest.
func eventIDs(events []*eventsource.Event) []string {
	ids := make([]string, len(events))
	for i, e := range events {
		ids[i] = e.ID
	}
	return ids
}

// rootOf returns the id of events' single project.created root, or ""
// if none is present. Two distinct roots within the same set is itself
// ErrRootMismatch — one side can't hold two roots for one project.
func rootOf(events []*eventsource.Event) (string, error) {
	var root string
	for _, e := range events {
		if e.Action != eventsource.ActionProjectCreated {
			continue
		}
		if root != "" && root != e.ID {
			return "", fmt.Errorf("%w: %s, %s", ErrRootMismatch, root, e.ID)
		}
		root = e.ID
	}
	return root, nil
}

// mapValues collects a map's values. Order is unspecified; callers that
// need determinism must not rely on it.
func mapValues(m map[string]*eventsource.Event) []*eventsource.Event {
	out := make([]*eventsource.Event, 0, len(m))
	for _, e := range m {
		out = append(out, e)
	}
	return out
}
