// Package eventsync implements set-reconciliation sync between replicas
// of the same v2 ATM project: given a local event set and what a remote
// reports having, it computes what each side is missing. This file holds
// the wire types and transport interfaces; engine.go holds the pure Plan
// diff/validate logic.
package eventsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
)

// RawEvent is one canonical event line on the wire. ID is a
// convenience/debugging value only, filled in by whoever constructs the
// RawEvent — no code path may trust it without re-parsing Raw via
// eventsource.Parse, which recomputes the id from the bytes.
type RawEvent struct {
	ID  string // recomputed from Raw via eventsource.Parse
	Raw []byte // canonical line bytes, verbatim
}

// RemoteSnapshot is what a SyncTarget reports about a project's state on
// the other side. Absent means the project doesn't exist there yet (so
// everything local must be published); otherwise Events holds its full
// event set and Digest is its SetDigest, letting callers detect drift
// without recomputing it. State is transport-private data (e.g. a git
// commit sha) round-tripped back into Publish so a transport can perform
// a compare-and-swap against the snapshot it just fetched.
type RemoteSnapshot struct {
	Absent bool
	Events []RawEvent
	Digest string
	State  any // transport-private (e.g. git head)
}

// SyncTarget is a whole-project sync transport: it reports a project's
// full remote state and accepts the events a Plan found missing there.
type SyncTarget interface {
	Fetch(ctx context.Context, project string) (*RemoteSnapshot, error)
	Publish(ctx context.Context, project string, missing []RawEvent, base *RemoteSnapshot) error
}

// Narrowing is a transport that can serve just the events a caller is
// missing without transferring the whole log: Frontier reports the
// remote's current heads and a digest of its full set; FetchSince
// returns only the events beyond the caller's own haves.
type Narrowing interface {
	Frontier(ctx context.Context, project string) (digest string, heads []string, err error)
	FetchSince(ctx context.Context, project string, haves []string) ([]RawEvent, error)
}

// SetDigest is an order-independent fingerprint of an event id set:
// "sha256:" + hex(sha256(sorted ids joined by "\n")). Two replicas
// holding the same events, authored or synced in any order, produce the
// same digest.
func SetDigest(ids []string) string {
	sorted := slices.Clone(ids)
	slices.Sort(sorted)
	sum := sha256.Sum256([]byte(strings.Join(sorted, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ErrRootMismatch reports that two event sets belong to different
// projects: their project.created roots differ, or one side itself
// holds two distinct roots (a store can't hold two roots for one
// project). Cross-project merge is a deliberately separate operation.
var ErrRootMismatch = errors.New("eventsync: different projects (root project.created mismatch); cross-project merge is a separate operation")
