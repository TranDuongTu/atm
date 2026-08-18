package chat

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"atm/internal/core"
)

// A canceled context must abort an in-flight stream promptly: the TUI's Esc
// during generation is exactly this path (ATM-4c476c precedent, ATM-f71b81
// depends on it).
func TestStreamHonorsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(delta("partial")))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	c := New(core.ChatConfig{Model: "m", Endpoint: srv.URL})
	ctx, cancel := context.WithCancel(context.Background())
	var got []string
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	err := c.Stream(ctx, []Message{{Role: "user", Content: "hi"}}, func(s string) { got = append(got, s) })
	elapsed := time.Since(start)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if elapsed > time.Second {
		t.Fatalf("took %v to notice cancellation; want well under 1s", elapsed)
	}
	if len(got) != 1 {
		t.Errorf("deltas = %q, want the one delivered before the cancel to survive", got)
	}
}

// Silence, not duration, is what the idle watchdog acts on: it fires only
// because nothing arrived, and the delta that did arrive is kept.
func TestStreamIdleTimeoutAbortsAQuietStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(delta("a")))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	c := New(core.ChatConfig{Model: "m", Endpoint: srv.URL})
	c.idle = 60 * time.Millisecond
	var got []string
	err := c.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(s string) { got = append(got, s) })
	if !errors.Is(err, ErrIdleTimeout) {
		t.Fatalf("err = %v, want ErrIdleTimeout", err)
	}
	if len(got) != 1 || got[0] != "a" {
		t.Errorf("deltas = %q, want the delivered one kept", got)
	}
}

// A stream that keeps producing must NOT be killed by the idle watchdog:
// this is the assertion that a long answer survives, which a whole-request
// timeout would fail.
//
// The two properties that make it discriminating: the window is far longer
// than any single gap (so a healthy stream never trips it even on a loaded
// box) and shorter than the whole stream (so a watchdog that never reset
// would kill this stream mid-way and the count below would come up short).
func TestStreamSurvivesLongerThanTheIdleWindow(t *testing.T) {
	const deltas, gap = 30, 20 * time.Millisecond
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < deltas; i++ {
			_, _ = w.Write([]byte(delta("x")))
			w.(http.Flusher).Flush()
			time.Sleep(gap)
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		w.(http.Flusher).Flush()
	}))
	defer srv.Close()
	c := New(core.ChatConfig{Model: "m", Endpoint: srv.URL})
	c.idle = 250 * time.Millisecond // ~12x each gap, well under the stream's 600ms
	var got []string
	if err := c.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(s string) { got = append(got, s) }); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(got) != deltas {
		t.Errorf("deltas = %d, want all %d (a resetting watchdog must not truncate a live stream)", len(got), deltas)
	}
}

// The watchdog measures silence between CHUNKS, not between tokens: a server
// that opens with role-only chunks, or streams content in a field this client
// does not read, is still sending. Every chunk here carries an empty content
// delta, spread across more than the idle window in total, and the real token
// arrives only at the end — if the reset were gated on non-empty content, the
// stream would be killed with ErrIdleTimeout before it ever landed.
func TestStreamEmptyContentChunksKeepTheWatchdogAlive(t *testing.T) {
	const empties, gap = 30, 20 * time.Millisecond
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < empties; i++ {
			_, _ = w.Write([]byte(delta("")))
			w.(http.Flusher).Flush()
			time.Sleep(gap)
		}
		_, _ = w.Write([]byte(delta("the answer")))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		w.(http.Flusher).Flush()
	}))
	defer srv.Close()
	c := New(core.ChatConfig{Model: "m", Endpoint: srv.URL})
	c.idle = 250 * time.Millisecond // ~12x each gap, well under the stream's 600ms
	var got []string
	if err := c.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(s string) { got = append(got, s) }); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(got) != 1 || got[0] != "the answer" {
		t.Errorf("deltas = %q, want the one real token (empty chunks are skipped, not counted as silence)", got)
	}
}

// The ceiling is the backstop for a stream that never goes quiet and never
// ends — the case a headless script has no way to interrupt.
func TestStreamTotalCeilingAborts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(10 * time.Millisecond):
				_, _ = w.Write([]byte(delta("x")))
				w.(http.Flusher).Flush()
			}
		}
	}))
	defer srv.Close()
	c := New(core.ChatConfig{Model: "m", Endpoint: srv.URL})
	c.idle = time.Second
	c.total = 120 * time.Millisecond
	err := c.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(string) {})
	if !errors.Is(err, ErrCeiling) {
		t.Fatalf("err = %v, want ErrCeiling", err)
	}
}

// The regression guard for the truncation trap: a whole-request timeout on
// the streaming path would cut off long answers, so there must not be one.
func TestNewClientHasNoWholeRequestTimeout(t *testing.T) {
	c := New(core.ChatConfig{Model: "m", Endpoint: "http://localhost:1"})
	if c.client.Timeout != 0 {
		t.Fatalf("http.Client.Timeout = %v, want 0 (the idle watchdog is the bound)", c.client.Timeout)
	}
	if c.idle <= 0 || c.total <= 0 {
		t.Fatalf("idle=%v total=%v, want both armed by New", c.idle, c.total)
	}
}

// classify's ordering is what keeps a watchdog abort from masquerading as
// caller cancellation: the caller's context is checked first, ahead of both
// watchdogs (client.go:153-155). None of the integration tests above can
// reach the branch that actually proves that order, because none of them
// both shorten the idle window AND cancel the caller's context — idled and
// callerCtx.Err() are never both true there. This table test calls classify
// directly, so the full ladder is pinned deterministically with no server
// and no timing window to race.
func TestClassifyPrecedence(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	expired, stop := context.WithTimeout(context.Background(), 0)
	defer stop()
	<-expired.Done() // make sure the deadline has actually elapsed

	live := context.Background()
	underlying := errors.New("boom")

	tests := []struct {
		name      string
		callerCtx context.Context
		streamCtx context.Context
		idled     bool
		want      error
	}{
		{
			// The discriminating case: a canceled ask is not a broken
			// endpoint, so it must report as context.Canceled even when the
			// idle watchdog also fired. If the two branches were swapped, or
			// the caller check dropped, this is the only case in the
			// package that would notice.
			name:      "caller cancellation outranks an idled watchdog",
			callerCtx: canceled,
			streamCtx: live,
			idled:     true,
			want:      context.Canceled,
		},
		{
			name:      "idled watchdog fires when the caller is still live",
			callerCtx: live,
			streamCtx: live,
			idled:     true,
			want:      ErrIdleTimeout,
		},
		{
			name:      "ceiling fires when the caller is live and not idled",
			callerCtx: live,
			streamCtx: expired,
			idled:     false,
			want:      ErrCeiling,
		},
		{
			name:      "neither watchdog nor caller: the underlying error passes through",
			callerCtx: live,
			streamCtx: live,
			idled:     false,
			want:      underlying,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classify(tt.callerCtx, tt.streamCtx, tt.idled, underlying); !errors.Is(got, tt.want) {
				t.Errorf("classify() = %v, want %v", got, tt.want)
			}
		})
	}
}
