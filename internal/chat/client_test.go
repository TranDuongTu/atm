package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"atm/internal/core"
)

// delta renders one ollama-shaped SSE chunk. ollama's OpenAI-compatible
// endpoint sends "data: {json}\n\n" per token and a final "data: [DONE]".
func delta(text string) string {
	b, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{{"delta": map[string]string{"content": text}}},
	})
	return "data: " + string(b) + "\n\n"
}

func sseServer(t *testing.T, chunks ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range chunks {
			_, _ = w.Write([]byte(c))
			w.(http.Flusher).Flush()
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func collect(t *testing.T, srv *httptest.Server) ([]string, error) {
	t.Helper()
	c := New(core.ChatConfig{Model: "m", Endpoint: srv.URL})
	var got []string
	err := c.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(s string) {
		got = append(got, s)
	})
	return got, err
}

func TestStreamDeliversDeltasInOrder(t *testing.T) {
	srv := sseServer(t, delta("Hel"), delta("lo"), delta(" world"), "data: [DONE]\n\n")
	got, err := collect(t, srv)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if strings.Join(got, "") != "Hello world" || len(got) != 3 {
		t.Errorf("deltas = %q, want three chunks spelling \"Hello world\"", got)
	}
}

// [DONE] ends the stream. Anything after it is not part of the answer.
func TestStreamStopsAtDone(t *testing.T) {
	srv := sseServer(t, delta("a"), "data: [DONE]\n\n", delta("b"))
	got, err := collect(t, srv)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(got) != 1 || got[0] != "a" {
		t.Errorf("deltas = %q, want just [a]", got)
	}
}

// A stream that ends without [DONE] still delivered what it delivered; the
// engine decides what a short answer means, not the client.
func TestStreamEndsWithoutDoneKeepsDeltas(t *testing.T) {
	srv := sseServer(t, delta("a"), delta("b"))
	got, err := collect(t, srv)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("deltas = %q, want both kept", got)
	}
}

// Blank keepalive lines and non-data lines carry nothing and must not break
// the parse.
func TestStreamIgnoresKeepalivesAndComments(t *testing.T) {
	srv := sseServer(t, ": ping\n\n", "\n", delta("a"), "event: message\n", delta("b"), "data: [DONE]\n\n")
	got, err := collect(t, srv)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if strings.Join(got, "") != "ab" {
		t.Errorf("deltas = %q, want ab", got)
	}
}

func TestStreamSurfacesErrorPayload(t *testing.T) {
	srv := sseServer(t, `data: {"error":{"message":"model \"ghost\" not found"}}`+"\n\n")
	_, err := collect(t, srv)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want the endpoint's own message", err)
	}
}

func TestStreamSurfacesHTTPStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"model not found"}`))
	}))
	defer srv.Close()
	c := New(core.ChatConfig{Model: "ghost", Endpoint: srv.URL})
	err := c.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("err = %v, want the status in it", err)
	}
}

func TestStreamRequestShape(t *testing.T) {
	var body map[string]any
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()
	c := New(core.ChatConfig{Model: "qwen3:8b", Endpoint: srv.URL + "/v1"})
	msgs := []Message{{Role: "system", Content: "rules"}, {Role: "user", Content: "hi"}}
	if err := c.Stream(context.Background(), msgs, func(string) {}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if path != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", path)
	}
	if body["model"] != "qwen3:8b" || body["stream"] != true {
		t.Errorf("body = %v, want the model and stream:true", body)
	}
	sent, _ := body["messages"].([]any)
	if len(sent) != 2 {
		t.Fatalf("messages = %v, want both in order", sent)
	}
	if first, _ := sent[0].(map[string]any); first["role"] != "system" {
		t.Errorf("first message = %v, want the system message first", sent[0])
	}
}
