package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeOllama serves the OpenAI-compatible SSE shape internal/chat parses.
// delay is applied before the first chunk, so a test can outrun a deadline.
func fakeOllama(t *testing.T, delay time.Duration, chunks ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		time.Sleep(delay)
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", c)
			if flusher != nil {
				flusher.Flush()
			}
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

// fakeOllamaSlowTail emits the leading chunks, flushes them, and only then
// stalls past the caller's deadline. This is the shape that matters for
// --timeout: the answer is already streaming when the deadline lands, so the
// partial has to survive. A server that stalls BEFORE its first chunk exercises
// the zero-delta path instead, which is a different case.
func fakeOllamaSlowTail(t *testing.T, stall time.Duration, chunks ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", c)
			if flusher != nil {
				flusher.Flush()
			}
		}
		time.Sleep(stall)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

// The streamed text ("arrived incrementally") is deliberately distinct from
// the task's title ("label resolver") and description ("the resolver walks
// the hierarchy") that seed the SOURCES footer — so this assertion can only
// pass if the delta path actually printed something, not because the phrase
// happened to already be in a source snippet.
func TestAskStreamsAndCitesInTextMode(t *testing.T) {
	srv := fakeOllama(t, 0, "streaming confirms the answer ", "arrived incrementally [1]")
	defer srv.Close()
	h := newGoldenHarness(t)
	h.output = outputText
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	h.run("task", "create", "--store", sp, "--project", "FOO", "--title", "label resolver", "--description", "the resolver walks the hierarchy", "--actor", "admin@cli:unset")
	h.run("project", "set-chat", "--store", sp, "--project", "FOO", "--model", "fake", "--endpoint", srv.URL, "--actor", "admin@cli:unset")
	out, _, code := h.run("ask", "how does the label resolver work?", "--store", sp, "--project", "FOO")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, "arrived incrementally") {
		t.Errorf("output = %q, want the streamed answer", out)
	}
	if !strings.Contains(out, "SOURCES") {
		t.Errorf("output = %q, want the cited-sources footer", out)
	}
}

// A deadline the caller set is an interruption: the partial is kept, the
// document says truncated, and the exit code stays 0.
func TestAskTimeoutIsTruncatedNotFailed(t *testing.T) {
	srv := fakeOllama(t, 300*time.Millisecond, "too late")
	defer srv.Close()
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	h.run("task", "create", "--store", sp, "--project", "FOO", "--title", "t", "--description", "d", "--actor", "admin@cli:unset")
	h.run("project", "set-chat", "--store", sp, "--project", "FOO", "--model", "fake", "--endpoint", srv.URL, "--actor", "admin@cli:unset")
	out, _, code := h.run("ask", "anything", "--store", sp, "--project", "FOO", "--timeout", "50ms", "--output", "json")
	if code != 0 {
		t.Fatalf("exit=%d, want 0 — a timeout is an interruption, not a failure", code)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["truncated"] != true {
		t.Errorf("truncated = %v, want true: %s", doc["truncated"], out)
	}
	if doc["degraded"] == true {
		t.Errorf("degraded = true, but a timeout is not a missing chat model: %s", out)
	}
}

// The real --timeout case: the answer is already streaming when the deadline
// lands. The partial must survive in the document, marked truncated, exit 0.
// Distinct from the zero-delta case its sibling covers.
func TestAskTimeoutMidStreamKeepsThePartial(t *testing.T) {
	srv := fakeOllamaSlowTail(t, 2*time.Second, "the partial answer")
	defer srv.Close()
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	h.run("task", "create", "--store", sp, "--project", "FOO", "--title", "t", "--description", "d", "--actor", "admin@cli:unset")
	h.run("project", "set-chat", "--store", sp, "--project", "FOO", "--model", "fake", "--endpoint", srv.URL, "--actor", "admin@cli:unset")
	out, _, code := h.run("ask", "anything", "--store", sp, "--project", "FOO", "--timeout", "200ms", "--output", "json")
	if code != 0 {
		t.Fatalf("exit=%d, want 0 — a timeout is an interruption, not a failure", code)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["truncated"] != true {
		t.Errorf("truncated = %v, want true: %s", doc["truncated"], out)
	}
	if doc["degraded"] == true {
		t.Errorf("degraded = true, but a timeout is not a missing chat model: %s", out)
	}
	if ans, _ := doc["answer"].(string); !strings.Contains(ans, "the partial answer") {
		t.Errorf("answer = %q, want the partial that streamed before the deadline — a truncated answer must keep what arrived", ans)
	}
}

// With no chat model configured there is nothing to generate with, but the
// hits must still arrive and the exit code must still be 0: a script parses
// one shape whether or not ollama is up.
func TestGoldenAskDegradedWithoutChatModel(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	h.run("task", "create", "--store", sp, "--project", "FOO", "--title", "label resolver", "--description", "the resolver walks the hierarchy", "--actor", "admin@cli:unset")
	out, _, code := h.run("ask", "how does the label resolver work?", "--store", sp, "--project", "FOO", "--output", "json")
	if code != 0 {
		t.Fatalf("exit=%d, want 0 — a missing chat model degrades, it does not fail; stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "ask-degraded", out)
}

// Every key present, always: an agent must be able to index the document
// without existence checks.
func TestAskJSONAlwaysCarriesEveryKey(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	out, _, _ := h.run("ask", "anything", "--store", sp, "--project", "FOO", "--output", "json")
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	for _, k := range []string{"answer", "citations", "hits", "chat_model", "embed_model", "session", "behind", "degraded", "truncated", "error"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("key %q missing from %s", k, out)
		}
	}
	if !strings.Contains(out, `"citations": []`) && !strings.Contains(out, `"citations":[]`) {
		t.Errorf("citations must marshal as [] and never null: %s", out)
	}
}

// An empty question is a malformed call, not a broken answer.
func TestAskWithEmptyQuestionIsUsage(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	_, _, code := h.run("ask", "   ", "--store", sp, "--project", "FOO", "--output", "json")
	if code != ExitUsage {
		t.Errorf("exit=%d, want %d", code, ExitUsage)
	}
}
