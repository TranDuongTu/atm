package tui

import (
	"context"
	"testing"
	"time"

	"atm/internal/store"
)

// stopIndexer runs on the Bubble Tea Update thread, so it must NOT wait for
// the watcher goroutine to finish (ATM-4c476c): a project switch or quit
// mid-embed used to block the whole UI on <-im.done until the in-flight
// embed call returned. The fake embed here ignores cancellation for 800ms —
// the worst case of an endpoint that does not honor the context — and the
// stop must still return immediately.
func TestStopIndexerDoesNotBlockOnInFlightEmbed(t *testing.T) {
	m := newIndexerTestModel(t)
	seedTask(t, m, "ATM", "first task")
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)

	inFlight := make(chan struct{})
	im.embedFnBuilder = func(*store.EmbeddingConfig) store.EmbedFunc {
		return func(ctx context.Context, text, role string) ([]float64, error) {
			select {
			case inFlight <- struct{}{}:
			default:
			}
			time.Sleep(800 * time.Millisecond) // deliberately ignores ctx
			return []float64{0.1, 0.2}, nil
		}
	}

	if cmd := startIndexer(m, "ATM"); cmd == nil {
		t.Fatal("startIndexer should return a tick cmd")
	}
	select {
	case <-inFlight:
	case <-time.After(3 * time.Second):
		t.Fatal("embed call never started")
	}

	start := time.Now()
	resetIndexer(m)
	elapsed := time.Since(start)
	if elapsed > 300*time.Millisecond {
		t.Fatalf("resetIndexer blocked %v on an in-flight embed; want an immediate return", elapsed)
	}
	if im.cancel != nil || im.done != nil {
		t.Fatal("reset should clear cancel + done immediately")
	}
	if im.state != idxStopped {
		t.Fatalf("after reset: state %v, want idxStopped", im.state)
	}
}

// A watcher abandoned by a non-blocking stop must not leak its progress
// messages into the NEXT watcher run's channel: each start gets a fresh
// msgCh, so late sends from the dying goroutine land in an orphaned channel.
func TestRestartAfterStopIsolatesOldWatcherMessages(t *testing.T) {
	m := newIndexerTestModel(t)
	seedTask(t, m, "ATM", "first task")
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.embedFnBuilder = fakeEmbedFnBuilder([]float64{0.1, 0.2})

	if cmd := startIndexer(m, "ATM"); cmd == nil {
		t.Fatal("first start should return a tick cmd")
	}
	oldCh := im.msgCh
	resetIndexer(m)
	if cmd := startIndexer(m, "ATM"); cmd == nil {
		t.Fatal("second start should return a tick cmd")
	}
	if im.msgCh == oldCh {
		t.Fatal("restart must allocate a fresh msgCh; sharing one lets the dying watcher pollute the new run's log")
	}
	resetIndexer(m)
}
