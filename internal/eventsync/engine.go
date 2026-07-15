package eventsync

import (
	"fmt"
	"slices"

	"atm/internal/eventsource"
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
