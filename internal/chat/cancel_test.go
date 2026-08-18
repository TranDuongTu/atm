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
func TestStreamSurvivesLongerThanTheIdleWindow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 6; i++ {
			_, _ = w.Write([]byte(delta("x")))
			w.(http.Flusher).Flush()
			time.Sleep(20 * time.Millisecond)
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		w.(http.Flusher).Flush()
	}))
	defer srv.Close()
	c := New(core.ChatConfig{Model: "m", Endpoint: srv.URL})
	c.idle = 50 * time.Millisecond // shorter than the whole stream, longer than each gap
	var got []string
	if err := c.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(s string) { got = append(got, s) }); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(got) != 6 {
		t.Errorf("deltas = %d, want all 6 (a resetting watchdog must not truncate a live stream)", len(got))
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
