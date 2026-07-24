package embed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"atm/internal/store"
)

// A canceled context must abort an in-flight embedding request promptly —
// the indexer's stop path depends on it (ATM-4c476c): before ctx-aware
// requests, a stop mid-embed waited out the whole HTTP call.
func TestEmbedHonorsContextCancellation(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release // hold the request open until the test ends
	}))
	defer srv.Close()
	defer close(release)

	c := New(store.EmbeddingConfig{Model: "m", Endpoint: srv.URL})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := c.Embed(ctx, "some text", "document")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("Embed should fail when its context is canceled")
	}
	if elapsed > time.Second {
		t.Fatalf("Embed took %v to notice cancellation; want well under 1s", elapsed)
	}
}

// The HTTP client must carry a backstop timeout so a hung endpoint can never
// wedge an embed call forever, even with a background context.
func TestNewClientHasBackstopTimeout(t *testing.T) {
	c := New(store.EmbeddingConfig{Model: "m", Endpoint: "http://localhost:1"})
	if c.client.Timeout <= 0 {
		t.Fatal("New must set a non-zero http.Client Timeout")
	}
}
