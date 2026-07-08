package embed

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"atm/internal/store"
)

func TestClientEmbedAppliesRolePrefix(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": []float64{0.1, 0.2, 0.3}}},
		})
	}))
	defer srv.Close()
	c := New(store.EmbeddingConfig{Model: "m", Endpoint: srv.URL, QueryPrefix: "search_query: ", DocPrefix: "search_document: ", Dim: 3})
	got, err := c.Embed("hello", "query")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != 3 || got[0] != 0.1 {
		t.Errorf("vector = %v, want [0.1 0.2 0.3]", got)
	}
	if !strings.Contains(gotBody, "search_query: hello") {
		t.Errorf("body = %q, want query prefix applied", gotBody)
	}
}

func TestClientEmbedDocumentRole(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"embedding": []float64{1}}}})
	}))
	defer srv.Close()
	c := New(store.EmbeddingConfig{Model: "m", Endpoint: srv.URL, DocPrefix: "search_document: "})
	if _, err := c.Embed("doc", "document"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, "search_document: doc") {
		t.Errorf("body = %q, want doc prefix applied", gotBody)
	}
}

func TestClientEmbedBatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"embedding": []float64{1.0, 0.0}},
				{"embedding": []float64{0.0, 1.0}},
			},
		})
	}))
	defer srv.Close()
	c := New(store.EmbeddingConfig{Model: "m", Endpoint: srv.URL, Dim: 2})
	got, err := c.EmbedBatch([]EmbedItem{{Text: "a"}, {Text: "b"}})
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(got) != 2 || len(got[0]) != 2 || len(got[1]) != 2 {
		t.Errorf("batch result = %v, want 2 vectors of dim 2", got)
	}
}

func TestClientEmbedEndpointError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()
	c := New(store.EmbeddingConfig{Model: "m", Endpoint: srv.URL})
	if _, err := c.Embed("x", "query"); err == nil {
		t.Fatal("want error on 500, got nil")
	}
}
