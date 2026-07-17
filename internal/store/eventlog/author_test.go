package eventlog

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"atm/libs/eventsource"
)

// testEngine builds an Engine over a fresh temp root with the production
// determinism defaults (wall clock, crypto/rand entropy) — the same defaults
// store.Open fills in when no options are passed.
func testEngine(t *testing.T) *Engine {
	t.Helper()
	return New(t.TempDir(), Options{
		ReplicaEntropy: rand.Reader,
		Now:            func() time.Time { return time.Now().UTC() },
	})
}

// authorTask/authorComment run the minting helpers under the project lock,
// exactly as the facade's mutators do.
func authorTask(t *testing.T, e *Engine, code, title string) (*eventsource.Event, string) {
	t.Helper()
	var ev *eventsource.Event
	var alias string
	if err := e.WithLock(code, func() error {
		var err error
		ev, alias, err = e.appendTaskCreatedLocked(code, title, "", nil, "admin@cli:unset")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return ev, alias
}

func authorComment(t *testing.T, e *Engine, code, taskAlias, body, replyTo string) (*eventsource.Event, string) {
	t.Helper()
	var ev *eventsource.Event
	var alias string
	if err := e.WithLock(code, func() error {
		var err error
		ev, alias, err = e.appendCommentCreatedLocked(code, taskAlias, body, nil, replyTo, "admin@cli:unset")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return ev, alias
}

func foldProject(t *testing.T, e *Engine, code string) *eventsource.State {
	t.Helper()
	snap, err := e.ReadV2File(code, false)
	if err != nil {
		t.Fatal(err)
	}
	st, err := eventsource.FoldEvents(snap.Events)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func payloadString(t *testing.T, ev *eventsource.Event, key string) string {
	t.Helper()
	var p map[string]json.RawMessage
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		t.Fatal(err)
	}
	raw, ok := p[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("payload[%q] is not a string: %s", key, raw)
	}
	return s
}

func TestAppendV2LockedParentsSecondLocalWriteOnFirst(t *testing.T) {
	e := testEngine(t)
	if err := e.SetProjectFormat("ATM", StoreFormatV2); err != nil {
		t.Fatal(err)
	}
	var firstID string
	if err := e.WithLock("ATM", func() error {
		ev, err := e.appendLocked("ATM", draft{
			Actor:   "admin@cli:unset",
			Action:  "project.created",
			Subject: eventsource.Subject{Kind: "project", Code: "ATM"},
			Payload: map[string]any{"alias": "ATM", "name": "x"},
		})
		if err != nil {
			return err
		}
		firstID = ev.ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.WithLock("ATM", func() error {
		ev, err := e.appendLocked("ATM", draft{
			Actor:   "admin@cli:unset",
			Action:  "project.name-changed",
			Subject: eventsource.Subject{Kind: "project", Code: "ATM"},
			Payload: map[string]any{"name": "y"},
		})
		if err != nil {
			return err
		}
		if len(ev.Parents) != 1 || ev.Parents[0] != firstID {
			t.Fatalf("parents = %#v, want [%s]", ev.Parents, firstID)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// --- ATM-0125: the alias is minted from the PRE-alias draft, so it is never
// a prefix of the event it lives on. Nothing may rely on the (dropped) fixed
// point: this pins the property, not a literal digest.

func TestAppendV2TaskCreatedAliasIsNotDerivedFromEventID(t *testing.T) {
	e := testEngine(t)
	ev, alias := authorTask(t, e, "ATM", "first")

	if !strings.HasPrefix(alias, "ATM-") {
		t.Fatalf("alias = %q, want ATM- prefix", alias)
	}
	suffix := strings.TrimPrefix(alias, "ATM-")
	idHex := strings.TrimPrefix(ev.ID, "sha256:")
	if strings.HasPrefix(idHex, suffix) {
		t.Fatalf("alias %q is a prefix of its own event id %s — the ATM-0125 fixed point is back", alias, ev.ID)
	}
	// The alias is the one stored on the event and the one the fold resolves.
	if got := payloadString(t, ev, "alias"); got != alias {
		t.Fatalf("payload.alias = %q, want %q", got, alias)
	}
	st := foldProject(t, e, "ATM")
	m, err := st.Resolve(alias)
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != "task" || m.ID != ev.ID {
		t.Fatalf("resolve(%q) = %+v, want task %s", alias, m, ev.ID)
	}
}

func TestAppendV2CommentCreatedAliasIsNotDerivedFromEventID(t *testing.T) {
	e := testEngine(t)
	_, taskAlias := authorTask(t, e, "ATM", "t")
	ev, alias := authorComment(t, e, "ATM", taskAlias, "hello", "")

	if !strings.HasPrefix(alias, taskAlias+"-c") {
		t.Fatalf("alias = %q, want %q-c prefix", alias, taskAlias)
	}
	suffix := strings.TrimPrefix(alias, taskAlias+"-c")
	idHex := strings.TrimPrefix(ev.ID, "sha256:")
	if strings.HasPrefix(idHex, suffix) {
		t.Fatalf("alias %q is a prefix of its own event id %s — the ATM-0125 fixed point is back", alias, ev.ID)
	}
	if got := payloadString(t, ev, "alias"); got != alias {
		t.Fatalf("payload.alias = %q, want %q", got, alias)
	}
}

// --- the `taken` collision set is honored.

func TestAppendV2TaskCreatedAliasesAreDistinctAndTaken(t *testing.T) {
	e := testEngine(t)
	_, a1 := authorTask(t, e, "ATM", "one")
	_, a2 := authorTask(t, e, "ATM", "two")
	if a1 == a2 {
		t.Fatalf("both tasks minted alias %q", a1)
	}
	// Both aliases are in the collision set the NEXT mint will be handed.
	taken := takenTaskAliases(foldProject(t, e, "ATM"))
	if !taken(a1) || !taken(a2) {
		t.Fatalf("taken set does not hold both minted aliases (%q, %q)", a1, a2)
	}
}

// TestTakenTaskAliasesForcesCollisionExtension drives eventsource.mintAlias's
// collision-extension branch with the engine's OWN predicate: a synthetic digest
// whose first 6 hex chars are exactly an existing task's alias suffix must not
// mint that alias again, but extend it.
func TestTakenTaskAliasesForcesCollisionExtension(t *testing.T) {
	e := testEngine(t)
	_, existing := authorTask(t, e, "ATM", "one")
	taken := takenTaskAliases(foldProject(t, e, "ATM"))

	// A digest that WOULD mint the existing alias (6 hex chars) verbatim.
	colliding := "sha256:" + strings.TrimPrefix(existing, "ATM-") + "0123456789abcdef"
	got := eventsource.MintTaskAlias("ATM", colliding, taken)
	if got == existing {
		t.Fatalf("mint reused taken alias %q — the collision set was ignored", existing)
	}
	if !strings.HasPrefix(got, existing) || len(got) <= len(existing) {
		t.Fatalf("mint = %q, want an extension of %q", got, existing)
	}
	if taken(got) {
		t.Fatalf("extended alias %q is itself taken", got)
	}
}

// TestTakenCommentAliasesAreScopedToTheirTask pins that a comment's collision
// set is its OWN task's comments — and that it forces extension there.
func TestTakenCommentAliasesAreScopedToTheirTask(t *testing.T) {
	e := testEngine(t)
	t1, alias1 := authorTask(t, e, "ATM", "one")
	t2, alias2 := authorTask(t, e, "ATM", "two")
	_, c1 := authorComment(t, e, "ATM", alias1, "on task one", "")
	_, c2 := authorComment(t, e, "ATM", alias2, "on task two", "")

	st := foldProject(t, e, "ATM")
	taken1 := takenCommentAliases(st, t1.ID)
	if !taken1(c1) {
		t.Fatalf("task one's collision set misses its own comment %q", c1)
	}
	if taken1(c2) {
		t.Fatalf("task one's collision set contains task two's comment %q", c2)
	}
	if taken2 := takenCommentAliases(st, t2.ID); !taken2(c2) || taken2(c1) {
		t.Fatalf("task two's collision set is wrong (c1=%v c2=%v)", taken2(c1), taken2(c2))
	}

	// Collision extension, driven through the engine's predicate.
	colliding := "sha256:" + strings.TrimPrefix(c1, alias1+"-c") + "0123456789abcdef"
	got := eventsource.MintCommentAlias(alias1, colliding, taken1)
	if got == c1 || !strings.HasPrefix(got, c1) || len(got) <= len(c1) {
		t.Fatalf("mint = %q, want an extension of taken alias %q", got, c1)
	}
}

// --- refs are fold-resolved identities, never aliases.

func TestAppendV2CommentCreatedRefsAreIdentities(t *testing.T) {
	e := testEngine(t)
	taskEv, taskAlias := authorTask(t, e, "ATM", "t")
	c1ev, c1 := authorComment(t, e, "ATM", taskAlias, "parent", "")
	c2ev, _ := authorComment(t, e, "ATM", taskAlias, "reply", c1) // reply_to given as an ALIAS

	taskRef := payloadString(t, c2ev, "task_ref")
	replyRef := payloadString(t, c2ev, "reply_to_ref")
	if taskRef != taskEv.ID {
		t.Fatalf("task_ref = %q, want the task's identity %q", taskRef, taskEv.ID)
	}
	if replyRef != c1ev.ID {
		t.Fatalf("reply_to_ref = %q, want the parent comment's identity %q", replyRef, c1ev.ID)
	}
	if !strings.HasPrefix(taskRef, "sha256:") || !strings.HasPrefix(replyRef, "sha256:") {
		t.Fatalf("refs are not identities: task_ref=%q reply_to_ref=%q", taskRef, replyRef)
	}
	if taskRef == taskAlias || replyRef == c1 {
		t.Fatalf("refs stored the alias instead of the identity")
	}
	if got := payloadString(t, c1ev, "reply_to_ref"); got != "" {
		t.Fatalf("top-level comment carries reply_to_ref %q", got)
	}
	// The fold agrees: the comment hangs off the task by identity.
	st := foldProject(t, e, "ATM")
	if c := st.Comments[c2ev.ID]; c == nil || c.TaskRef != taskEv.ID || c.ReplyToRef != c1ev.ID {
		t.Fatalf("fold: comment = %+v, want task_ref=%s reply_to_ref=%s", c, taskEv.ID, c1ev.ID)
	}
}

// --- HLC.

func TestCommitV2AuthorPersistsLastHLC(t *testing.T) {
	e := testEngine(t)
	ev, _ := authorTask(t, e, "ATM", "t")
	m, err := e.ReadStoreMeta()
	if err != nil {
		t.Fatal(err)
	}
	if m.LastHLC == nil {
		t.Fatal("store.json has no last_hlc after an append")
	}
	if m.LastHLC.Compare(ev.HLC) != 0 {
		t.Fatalf("last_hlc = %+v, want the authored event's stamp %+v", *m.LastHLC, ev.HLC)
	}
}

// TestBeginV2AuthorReobservesFileHLCs proves the clock is rebuilt from the
// FILE on every begin: with a stale (or absent) last_hlc in store.json, an
// event already in the log that carries a far-future stamp must still be
// sorted BEFORE the next locally-authored event.
func TestBeginV2AuthorReobservesFileHLCs(t *testing.T) {
	e := testEngine(t)
	_, _ = authorTask(t, e, "ATM", "t")

	// Splice in a legitimate v2 event stamped an hour into the future.
	future := time.Now().Add(time.Hour).UnixMilli()
	var futureHLC eventsource.HLC
	if err := e.WithLock("ATM", func() error {
		snap, err := e.ReadV2File("ATM", true)
		if err != nil {
			return err
		}
		replica, err := e.EnsureReplicaForWriteLocked()
		if err != nil {
			return err
		}
		clock := eventsource.NewClock(func() int64 { return future })
		ev, err := eventsource.NewEvent(clock, replica, snap.Frontier, eventsource.Draft{
			At:      e.now(),
			Actor:   "admin@cli:unset",
			Action:  "project.name-changed",
			Subject: eventsource.Subject{Kind: "project", Code: "ATM"},
			Payload: map[string]any{"name": "from the future"},
		})
		if err != nil {
			return err
		}
		futureHLC = ev.HLC
		return e.AppendEventLineLocked("ATM", ev.Raw)
	}); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"absent last_hlc", "stale last_hlc"} {
		t.Run(name, func(t *testing.T) {
			if err := e.MutateStoreMeta(func(m *StoreMeta) error {
				if name == "absent last_hlc" {
					m.LastHLC = nil
				} else {
					m.LastHLC = &eventsource.HLC{P: 1, L: 0}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			ev, alias := authorTask(t, e, "ATM", "after "+name)
			if ev.HLC.Compare(futureHLC) <= 0 {
				t.Fatalf("new event HLC %+v does not follow the file's future stamp %+v (%s)", ev.HLC, futureHLC, alias)
			}
			m, err := e.ReadStoreMeta()
			if err != nil {
				t.Fatal(err)
			}
			if m.LastHLC == nil || m.LastHLC.Compare(ev.HLC) != 0 {
				t.Fatalf("last_hlc = %+v, want %+v", m.LastHLC, ev.HLC)
			}
		})
	}
}

// TestBeginV2AuthorObservesPersistedLastHLC pins spec authoring step 5: a
// persisted local HLC ahead of everything in the file is still observed.
func TestBeginV2AuthorObservesPersistedLastHLC(t *testing.T) {
	e := testEngine(t)
	_, _ = authorTask(t, e, "ATM", "t")
	ahead := eventsource.HLC{P: time.Now().Add(time.Hour).UnixMilli(), L: 7}
	if err := e.MutateStoreMeta(func(m *StoreMeta) error {
		m.LastHLC = &ahead
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ev, _ := authorTask(t, e, "ATM", "next")
	if ev.HLC.Compare(ahead) <= 0 {
		t.Fatalf("new event HLC %+v does not follow the persisted last_hlc %+v", ev.HLC, ahead)
	}
}

// TestCommitV2AuthorKeepsLastHLCMonotone pins store.json's LastHLC as a
// STORE-WIDE watermark: a commit in one project must never move it backwards
// past a higher stamp a concurrent commit in another project just wrote.
// Per-project causality is unaffected either way — each project reobserves
// every event in its own file on every beginAuthorLocked — but the field
// is store-wide, so its name promises a store-wide max.
//
// This simulates the race directly: begin a v2 author context (observing
// today's last_hlc), then have a "concurrent" writer in another project push
// last_hlc ahead before this writer commits its (now stale, lower) event.
func TestCommitV2AuthorKeepsLastHLCMonotone(t *testing.T) {
	e := testEngine(t)

	var lowEv *eventsource.Event
	if err := e.WithLock("ATM", func() error {
		ctx, err := e.beginAuthorLocked("ATM")
		if err != nil {
			return err
		}
		lowEv, _, err = eventsource.NewTaskCreated(ctx.clock, ctx.replica, ctx.snap.Frontier, eventsource.TaskCreateDraft{
			ProjectCode: "ATM",
			At:          e.now(),
			Actor:       "admin@cli:unset",
			Title:       "low",
		}, takenTaskAliases(ctx.state))
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// A concurrent commit in another project moves last_hlc ahead of lowEv.
	ahead := eventsource.HLC{P: lowEv.HLC.P + 1_000_000, L: 0}
	if err := e.MutateStoreMeta(func(m *StoreMeta) error {
		m.LastHLC = &ahead
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// This project now commits its stale, lower-stamped event.
	if err := e.WithLock("ATM", func() error {
		return e.commitAuthorLocked("ATM", lowEv)
	}); err != nil {
		t.Fatal(err)
	}

	m, err := e.ReadStoreMeta()
	if err != nil {
		t.Fatal(err)
	}
	if m.LastHLC == nil || m.LastHLC.Compare(ahead) != 0 {
		t.Fatalf("last_hlc = %+v, want it to stay at the higher concurrent stamp %+v (must not regress to %+v)", m.LastHLC, ahead, lowEv.HLC)
	}
}

// --- store.json is store-WIDE state written under a PER-PROJECT lock.
// Without the store-scoped lock, a v2 append in one project read-modify-writes
// a stale store.json and silently drops another project's ProjectFormats entry
// — which downgrades a v2-media project to v1 and sends its next write to
// log.jsonl. Remove the WithLock in MutateStoreMeta and this test fails.
func TestStoreMetaWritesDoNotLoseProjectFormats(t *testing.T) {
	e := testEngine(t)
	if err := e.SetProjectFormat("BBB", StoreFormatV2); err != nil {
		t.Fatal(err)
	}

	codes := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		codes = append(codes, "PRJ"+string(rune('A'+i))) // PRJA..PRJT
	}

	var wg sync.WaitGroup
	wg.Add(2)
	errs := make(chan error, 2)
	go func() { // P1: upgrades projects, writing their format entries
		defer wg.Done()
		for _, code := range codes {
			if err := e.SetProjectFormat(code, StoreFormatV2); err != nil {
				errs <- err
				return
			}
		}
	}()
	go func() { // P2: appends v2 events in a DIFFERENT project (LastHLC RMW)
		defer wg.Done()
		for i := 0; i < 20; i++ {
			if err := e.WithLock("BBB", func() error {
				_, _, err := e.appendTaskCreatedLocked("BBB", fmt.Sprintf("t%d", i), "", nil, "admin@cli:unset")
				return err
			}); err != nil {
				errs <- err
				return
			}
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	m, err := e.ReadStoreMeta()
	if err != nil {
		t.Fatal(err)
	}
	var lost []string
	for _, code := range append(codes, "BBB") {
		if m.ProjectFormats[code] != StoreFormatV2 {
			lost = append(lost, code)
		}
	}
	if len(lost) > 0 {
		t.Fatalf("lost update: store.json dropped %d ProjectFormats entries: %v", len(lost), lost)
	}
	if m.LastHLC == nil {
		t.Fatal("lost update: store.json dropped last_hlc")
	}
}
