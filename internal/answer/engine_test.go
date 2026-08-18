package answer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"atm/internal/chat"
	"atm/internal/core"
)

type fakeSearcher struct {
	hits    []core.Hit
	err     error
	pending int
	params  core.SearchParams
	calls   int
}

func (f *fakeSearcher) Search(p core.SearchParams) ([]core.Hit, bool, error) {
	f.calls++
	f.params = p
	if f.err != nil {
		return nil, false, f.err
	}
	return f.hits, false, nil
}

func (f *fakeSearcher) PendingIndexCount(code, slug string) (int, error) { return f.pending, nil }

type fakeChat struct {
	deltas []string
	err    error
	msgs   []chat.Message
	before func() // runs before the last delta; the cancellation hook
}

func (f *fakeChat) Stream(ctx context.Context, msgs []chat.Message, onDelta func(string)) error {
	f.msgs = msgs
	for i, d := range f.deltas {
		if i == len(f.deltas)-1 && f.before != nil {
			f.before()
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		onDelta(d)
	}
	return f.err
}

// record collects the event stream as readable names, so a test can assert
// the ORDER the spec fixes rather than poke at one event.
func record(events *[]string) func(Event) {
	return func(e Event) {
		switch ev := e.(type) {
		case Retrieved:
			*events = append(*events, fmt.Sprintf("retrieved:%d:behind=%d", len(ev.Hits), ev.Behind))
		case Delta:
			*events = append(*events, "delta:"+ev.Text)
		case Done:
			*events = append(*events, fmt.Sprintf("done:cited=%d:degraded=%t", len(ev.Citations), ev.Degraded))
		case Failed:
			*events = append(*events, fmt.Sprintf("failed:canceled=%t", ev.Canceled))
		}
	}
}

func twoHits() []core.Hit {
	return []core.Hit{
		{ID: "ATM-aaa111", Kind: "task", Title: "wire the indexer", Snippet: "the watcher owns freshness", Score: 0.8, Match: "semantic"},
		{ID: "ATM-bbb222", Kind: "task", Title: "spotlight search", Snippet: "one retrieval path", Score: 0.7, Match: "semantic"},
	}
}

func TestAskEmitsRetrievedThenDeltasThenDone(t *testing.T) {
	s := &fakeSearcher{hits: twoHits(), pending: 3}
	e := New(Config{Project: "ATM", Searcher: s, Model: "m", Chat: &fakeChat{deltas: []string{"the watcher ", "owns it [1]"}}})
	var got []string
	if err := e.Ask(context.Background(), Query{Question: "who owns freshness?"}, record(&got)); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	want := []string{"retrieved:2:behind=3", "delta:the watcher ", "delta:owns it [1]", "done:cited=1:degraded=false"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("events =\n%v\nwant\n%v", got, want)
	}
}

// Done carries what the answer cited, not everything retrieved.
func TestAskDoneCarriesOnlyTheCitedSources(t *testing.T) {
	s := &fakeSearcher{hits: twoHits()}
	e := New(Config{Project: "ATM", Searcher: s, Model: "m", Chat: &fakeChat{deltas: []string{"only the second one [2] matters"}}})
	var done Done
	if err := e.Ask(context.Background(), Query{Question: "q"}, func(ev Event) {
		if d, ok := ev.(Done); ok {
			done = d
		}
	}); err != nil {
		t.Fatal(err)
	}
	if len(done.Citations) != 1 || done.Citations[0].ID != "ATM-bbb222" {
		t.Errorf("citations = %+v, want only ATM-bbb222", done.Citations)
	}
}

// The question is embedded for the semantic path, and the retrieval params
// are the ones the engine was configured with.
func TestAskEmbedsTheQuestionForSemanticRetrieval(t *testing.T) {
	s := &fakeSearcher{hits: twoHits()}
	embedded := ""
	e := New(Config{
		Project:  "ATM",
		Searcher: s,
		Model:    "nomic-embed-text",
		K:        4,
		Embed: func(ctx context.Context, text, role string) ([]float64, error) {
			embedded = text + "/" + role
			return []float64{0.1, 0.2}, nil
		},
		Chat: &fakeChat{deltas: []string{"ok"}},
	})
	if err := e.Ask(context.Background(), Query{Question: "who owns freshness?"}, func(Event) {}); err != nil {
		t.Fatal(err)
	}
	if embedded != "who owns freshness?/query" {
		t.Errorf("embedded = %q, want the question in the query role", embedded)
	}
	if len(s.params.QueryVector) != 2 || s.params.K != 4 || s.params.Project != "ATM" || s.params.Kind != "all" {
		t.Errorf("params = %+v", s.params)
	}
}

// A turn with history still retrieves — it is not skipped just because the
// question is a follow-up. (This does not pin re-running per turn across
// multiple Ask calls; it only proves history's presence doesn't suppress
// this one retrieval.)
func TestAskRetrievesEvenWhenHistoryIsPresent(t *testing.T) {
	s := &fakeSearcher{hits: twoHits()}
	c := &fakeChat{deltas: []string{"ok"}}
	e := New(Config{Project: "ATM", Searcher: s, Model: "m", Chat: c})
	q := Query{Question: "and who watches it?", History: []Turn{{Question: "who owns freshness?", Answer: "the watcher [1]"}}}
	if err := e.Ask(context.Background(), q, func(Event) {}); err != nil {
		t.Fatal(err)
	}
	if s.calls != 1 {
		t.Errorf("Search calls = %d, want exactly one for this turn", s.calls)
	}
	if len(c.msgs) != 4 {
		t.Errorf("messages = %d, want system + 2 history + current", len(c.msgs))
	}
}

// Behind is a hint about the index, not about this model's answer: with no
// embedding model there is no index to be behind.
func TestAskBehindIsZeroWithoutAnEmbeddingModel(t *testing.T) {
	s := &fakeSearcher{hits: twoHits(), pending: 9}
	e := New(Config{Project: "ATM", Searcher: s, Chat: &fakeChat{deltas: []string{"ok"}}})
	var got []string
	if err := e.Ask(context.Background(), Query{Question: "q"}, record(&got)); err != nil {
		t.Fatal(err)
	}
	if got[0] != "retrieved:2:behind=0" {
		t.Errorf("first event = %q, want behind=0", got[0])
	}
}

// The umbrella spec's "retrieval never breaks" rule (ATM-e4be94), applied to
// the embedding half: an Embed that errors must not fail the turn. The query
// vector is dropped and store.Search falls back to its text pass, so the
// engine still emits a normal Retrieved -> Delta -> Done sequence. Asserting
// QueryVector == nil on the params the fake Searcher received is what
// actually pins the fallback — without it, a broken Embed could silently
// leak a stale or zero vector into Search and this test would still pass.
func TestAskFallsBackToTextRetrievalWhenEmbedFails(t *testing.T) {
	s := &fakeSearcher{hits: twoHits()}
	e := New(Config{
		Project:  "ATM",
		Searcher: s,
		Model:    "nomic-embed-text",
		Embed: func(ctx context.Context, text, role string) ([]float64, error) {
			return []float64{9, 9}, fmt.Errorf("ollama unreachable")
		},
		Chat: &fakeChat{deltas: []string{"the watcher owns it [1]"}},
	})
	var got []string
	if err := e.Ask(context.Background(), Query{Question: "who owns freshness?"}, record(&got)); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	want := []string{"retrieved:2:behind=0", "delta:the watcher owns it [1]", "done:cited=1:degraded=false"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("events =\n%v\nwant\n%v", got, want)
	}
	if s.params.QueryVector != nil {
		t.Errorf("QueryVector = %v, want nil (a failed embed must not reach Search)", s.params.QueryVector)
	}
	if s.params.QueryText != "who owns freshness?" {
		t.Errorf("QueryText = %q, want the question, which is all the text path has left to match on", s.params.QueryText)
	}
}

// No chat model at all: hits, then a degraded Done naming the fix. Never a
// Delta, never a Failed (ATM-e4be94: retrieval survives generation).
func TestAskWithoutAChatModelDegradesToHits(t *testing.T) {
	s := &fakeSearcher{hits: twoHits(), pending: 1}
	e := New(Config{Project: "ATM", Searcher: s, Model: "m"})
	var got []string
	var done Done
	err := e.Ask(context.Background(), Query{Question: "q"}, func(ev Event) {
		record(&got)(ev)
		if d, ok := ev.(Done); ok {
			done = d
		}
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	want := []string{"retrieved:2:behind=1", "done:cited=0:degraded=true"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("events = %v, want %v", got, want)
	}
	if !strings.Contains(done.Reason, "set-chat") {
		t.Errorf("reason = %q, want it to name the fix", done.Reason)
	}
}

// An endpoint that never produced a delta never generated anything: that is
// the spec's "unreachable" degrade, not a failure.
func TestAskUnreachableChatDegradesRatherThanFails(t *testing.T) {
	s := &fakeSearcher{hits: twoHits()}
	e := New(Config{Project: "ATM", Searcher: s, Model: "m", Chat: &fakeChat{err: fmt.Errorf("dial tcp 127.0.0.1:11434: connection refused")}})
	var got []string
	var done Done
	if err := e.Ask(context.Background(), Query{Question: "q"}, func(ev Event) {
		record(&got)(ev)
		if d, ok := ev.(Done); ok {
			done = d
		}
	}); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if strings.Join(got, "|") != "retrieved:2:behind=0|done:cited=0:degraded=true" {
		t.Errorf("events = %v, want retrieved then a degraded done", got)
	}
	if !strings.Contains(done.Reason, "connection refused") {
		t.Errorf("reason = %q, want the endpoint's own words", done.Reason)
	}
}

// A stream that ends CLEANLY without ever producing a token is the same
// outcome as an unreachable endpoint, and must not report success: an
// endpoint that ignores "stream": true answers with a plain JSON body, so the
// SSE loop matches no data: lines and returns nil having delivered nothing.
// Without the zero-delta guard this renders as a non-degraded Done — `atm
// ask` printing nothing and exiting 0, with no reason for the consumer to
// show.
func TestAskCleanStreamWithNoDeltasDegrades(t *testing.T) {
	s := &fakeSearcher{hits: twoHits()}
	e := New(Config{Project: "ATM", Searcher: s, Model: "m", Chat: &fakeChat{}})
	var got []string
	var done Done
	if err := e.Ask(context.Background(), Query{Question: "q"}, func(ev Event) {
		record(&got)(ev)
		if d, ok := ev.(Done); ok {
			done = d
		}
	}); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if strings.Join(got, "|") != "retrieved:2:behind=0|done:cited=0:degraded=true" {
		t.Errorf("events = %v, want retrieved then a degraded done", got)
	}
	if done.Reason == "" {
		t.Error("reason = \"\", want a degraded Done to say why there is no answer")
	}
}

// A break AFTER deltas is a real failure: the partial answer is delivered and
// the consumer marks it interrupted.
func TestAskMidStreamFailureKeepsTheDeltasAndFails(t *testing.T) {
	s := &fakeSearcher{hits: twoHits()}
	e := New(Config{Project: "ATM", Searcher: s, Model: "m", Chat: &fakeChat{deltas: []string{"half an ans"}, err: fmt.Errorf("unexpected EOF")}})
	var got []string
	if err := e.Ask(context.Background(), Query{Question: "q"}, record(&got)); err != nil {
		t.Fatalf("Ask returned %v; a mid-stream break is exit 0 for the CLI", err)
	}
	want := []string{"retrieved:2:behind=0", "delta:half an ans", "failed:canceled=false"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("events = %v, want %v", got, want)
	}
}

// Cancellation is its own ending: a mid-stream cancel (after a delta has
// already arrived) reports Failed{Canceled: true}, not the mid-stream-failure
// default a non-canceled break would produce. This does NOT by itself prove
// cancellation outranks the zero-delta degrade row — answer.Len() is already
// non-zero here by the time the cancel fires, so that ordering is pinned
// separately by TestAskCanceledBeforeAnyDeltaIsStillACanceledFailure below.
func TestAskCanceledStreamIsACanceledFailure(t *testing.T) {
	s := &fakeSearcher{hits: twoHits()}
	ctx, cancel := context.WithCancel(context.Background())
	c := &fakeChat{deltas: []string{"partial", "never"}, before: cancel}
	e := New(Config{Project: "ATM", Searcher: s, Model: "m", Chat: c})
	var got []string
	if err := e.Ask(ctx, Query{Question: "q"}, record(&got)); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if got[len(got)-1] != "failed:canceled=true" {
		t.Errorf("events = %v, want a canceled failure last", got)
	}
}

// The only state that actually separates the cancel row from the zero-delta
// degrade row: canceling before any token ever arrives, so ctx.Err() != nil
// AND answer.Len() == 0 are both true when the switch runs. This is not an
// edge case — a user pressing Esc while waiting for generation to start is
// probably the MOST common cancel there is. If a refactor ever swapped the
// order of those two switch cases, this cancel would silently render as
// Done{Degraded: true} ("sources only, generation unavailable") instead of
// Failed{Canceled: true} ("you stopped it") — a user-visible lie that every
// other test in this file would stay green through.
func TestAskCanceledBeforeAnyDeltaIsStillACanceledFailure(t *testing.T) {
	s := &fakeSearcher{hits: twoHits()}
	ctx, cancel := context.WithCancel(context.Background())
	// A single-delta stream: fakeChat's before hook fires ahead of the LAST
	// delta, which here is also the FIRST — so cancellation lands before
	// onDelta is ever called and answer.Len() stays 0.
	c := &fakeChat{deltas: []string{"never"}, before: cancel}
	e := New(Config{Project: "ATM", Searcher: s, Model: "m", Chat: c})
	var got []string
	if err := e.Ask(ctx, Query{Question: "q"}, record(&got)); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	want := []string{"retrieved:2:behind=0", "failed:canceled=true"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("events = %v, want %v (canceled, never a degraded done)", got, want)
	}
}

// The one path that returns an error: a broken ledger is not a degraded
// answer, and `atm ask` must be able to exit non-zero for it.
func TestAskStoreErrorFailsAndReturnsTheError(t *testing.T) {
	s := &fakeSearcher{err: fmt.Errorf("integrity: event log truncated")}
	e := New(Config{Project: "ATM", Searcher: s, Model: "m", Chat: &fakeChat{deltas: []string{"x"}}})
	var got []string
	err := e.Ask(context.Background(), Query{Question: "q"}, record(&got))
	if err == nil || !strings.Contains(err.Error(), "integrity") {
		t.Fatalf("err = %v, want the store's error returned", err)
	}
	if strings.Join(got, "|") != "failed:canceled=false" {
		t.Errorf("events = %v, want a single Failed and no Retrieved", got)
	}
}

func TestAskEmptyQuestionIsAUsageErrorWithNoEvents(t *testing.T) {
	s := &fakeSearcher{hits: twoHits()}
	e := New(Config{Project: "ATM", Searcher: s, Model: "m", Chat: &fakeChat{deltas: []string{"x"}}})
	var got []string
	err := e.Ask(context.Background(), Query{Question: "   "}, record(&got))
	if !errors.Is(err, core.ErrUsage) {
		t.Fatalf("err = %v, want core.ErrUsage", err)
	}
	if len(got) != 0 {
		t.Errorf("events = %v, want none", got)
	}
}
