package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"atm/internal/core"
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

// The model numbers its [n] markers over the FULL retrieval set (buildMessages
// numbers every hit 1..N), but citedHits returns only the ones actually named,
// in first-mention order. Renumbering the footer from 1 over THAT list prints
// keys that do not match the markers in the streamed answer above it whenever
// the model cites anything but the leading sources in order. A single-hit
// fixture cannot show this — it needs enough hits that the numbers can
// disagree, and an answer that cites a later one, not the first.
func TestAskTextFooterKeepsRetrievalPositionNotCitationOrder(t *testing.T) {
	srv := fakeOllama(t, 0, "citing only the second source ", "here [2]")
	defer srv.Close()
	h := newGoldenHarness(t)
	h.output = outputText
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	// All three carry "component" exactly once, and none is named by the query,
	// so textSearch scores them identically and the stable sort keeps them in
	// creation order — hits[1] is deterministically the second one created.
	h.run("task", "create", "--store", sp, "--project", "FOO", "--title", "first widget", "--description", "a component of the system", "--actor", "admin@cli:unset")
	h.run("task", "create", "--store", sp, "--project", "FOO", "--title", "second widget", "--description", "a component of the system", "--actor", "admin@cli:unset")
	h.run("task", "create", "--store", sp, "--project", "FOO", "--title", "third widget", "--description", "a component of the system", "--actor", "admin@cli:unset")
	h.run("project", "set-chat", "--store", sp, "--project", "FOO", "--model", "fake", "--endpoint", srv.URL, "--actor", "admin@cli:unset")
	out, _, code := h.run("ask", "component", "--store", sp, "--project", "FOO", "--k", "3")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, "[2] ") {
		t.Errorf("output = %q, want the footer to key the cited source as [2] — the number the answer actually used", out)
	}
	if strings.Contains(out, "[1] ") {
		t.Errorf("output = %q, want no [1] entry — the answer never cited source 1, so the footer must not invent one", out)
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

// Two separate invocations, one conversation: the second must carry the
// first's turn, because atm ask is a process that exits and a follow-up
// question otherwise has no antecedent.
func TestAskSessionCarriesHistoryAcrossInvocations(t *testing.T) {
	srv := fakeOllama(t, 0, "first answer [1]")
	defer srv.Close()
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	h.run("task", "create", "--store", sp, "--project", "FOO", "--title", "t", "--description", "d", "--actor", "admin@cli:unset")
	h.run("project", "set-chat", "--store", sp, "--project", "FOO", "--model", "fake", "--endpoint", srv.URL, "--actor", "admin@cli:unset")
	h.run("ask", "first question", "--store", sp, "--project", "FOO", "--session", "s1", "--output", "json")
	turns, err := h.store.ReadAskTurns("FOO", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || turns[0].Question != "first question" {
		t.Fatalf("turns = %+v, want the first exchange recorded", turns)
	}
	out, _, code := h.run("ask", "second question", "--store", sp, "--project", "FOO", "--session", "s1", "--output", "json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, `"session": "s1"`) && !strings.Contains(out, `"session":"s1"`) {
		t.Errorf("output = %s, want the session echoed", out)
	}
}

// A degraded ask produced no answer. Recording an empty assistant turn would
// poison every later turn in the session.
func TestAskDegradedDoesNotRecordATurn(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	h.run("ask", "anything", "--store", sp, "--project", "FOO", "--session", "s2", "--output", "json")
	turns, err := h.store.ReadAskTurns("FOO", "s2")
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 0 {
		t.Errorf("turns = %+v, want none — there was no answer to record", turns)
	}
}

// A ctrl-C mid-stream is the user REJECTING the answer, not a deadline
// running out on one they still wanted — the branch draws that distinction
// everywhere else (Failed.Canceled, event.go), and ask.go's recording
// condition was the one consumer that ignored it, writing the half-sentence
// into the session and replaying it as the assistant's real prior reply on
// the next turn. Cancellation cannot be driven through a real SIGINT
// in-process (there is no way to land the signal inside the exact window
// ask's own signal.NotifyContext registration is open without racing every
// other signal listener in the test binary), so this drives the identical
// code path directly: it cancels the context handed to root.ExecuteContext
// while the stream is still in flight, which is exactly what a delivered
// SIGINT would do to the ctx signal.NotifyContext derives from.
func TestAskCanceledMidStreamIsNotRecorded(t *testing.T) {
	srv := fakeOllamaSlowTail(t, 2*time.Second, "half of an answer")
	defer srv.Close()
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	h.run("task", "create", "--store", sp, "--project", "FOO", "--title", "t", "--description", "d", "--actor", "admin@cli:unset")
	h.run("project", "set-chat", "--store", sp, "--project", "FOO", "--model", "fake", "--endpoint", srv.URL, "--actor", "admin@cli:unset")

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	out, _, code := h.runCtx(ctx, "ask", "anything", "--store", sp, "--project", "FOO", "--session", "s-cancel", "--output", "json")
	if code != 0 {
		t.Fatalf("exit=%d, want 0 — a cancellation is not a failure; out=%s", code, out)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if doc["error"] != "canceled" {
		t.Fatalf("error = %v, want %q — this test must actually exercise Failed{Canceled:true}: %s", doc["error"], "canceled", out)
	}
	turns, err := h.store.ReadAskTurns("FOO", "s-cancel")
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 0 {
		t.Errorf("turns = %+v, want none recorded — the user rejected this answer, it must not be replayed as history", turns)
	}
}

func TestAskRejectsTraversalSessionID(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	_, _, code := h.run("ask", "q", "--store", sp, "--project", "FOO", "--session", "../../../etc/passwd", "--output", "json")
	if code != ExitUsage {
		t.Errorf("exit=%d, want %d — a bad session id is rejected, never sanitized", code, ExitUsage)
	}
}

func TestAskAppendsToInquiryLogWithCitations(t *testing.T) {
	srv := fakeOllama(t, 0, "the answer [1]")
	defer srv.Close()
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	h.run("task", "create", "--store", sp, "--project", "FOO", "--title", "label resolver", "--description", "walks the hierarchy", "--actor", "admin@cli:unset")
	h.run("project", "set-chat", "--store", sp, "--project", "FOO", "--model", "fake", "--endpoint", srv.URL, "--actor", "admin@cli:unset")
	h.run("ask", "label resolver", "--store", sp, "--project", "FOO", "--output", "json")
	inq, err := h.store.ReadInquiries("FOO")
	if err != nil {
		t.Fatal(err)
	}
	if len(inq) != 1 {
		t.Fatalf("got %d inquiries, want 1", len(inq))
	}
	if len(inq[0].ReturnedIDs) == 0 {
		t.Error("want the returned IDs recorded — they are recall@k's denominator")
	}
	if len(inq[0].CitedIDs) == 0 {
		t.Error("want the cited IDs recorded — they are the strong relevance signal")
	}
}

// askHistoryTurns has no other test in this package: a broken cap fails
// silently, growing every request on exactly the long-lived sessions the
// feature exists for. This pins the exact count buildMessages sends: one
// system message, then two per replayed turn (user + assistant), then one
// final user message carrying sources and the question. With the cap
// holding and 12 turns on disk, only the newest 10 are replayed, so the
// request must carry 1 + (10 × 2) + 1 = 22 messages — not the 26 an
// unbounded replay of all 12 turns would send.
func TestAskHistoryCapsReplayedTurnsAt10(t *testing.T) {
	var captured struct {
		Messages []map[string]any `json:"messages"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	h.run("task", "create", "--store", sp, "--project", "FOO", "--title", "t", "--description", "d", "--actor", "admin@cli:unset")
	h.run("project", "set-chat", "--store", sp, "--project", "FOO", "--model", "fake", "--endpoint", srv.URL, "--actor", "admin@cli:unset")

	// Write turns directly to the store rather than running 12 asks — this
	// pins the cap in buildMessages, not the recording path finding 4 covers.
	for i := 1; i <= 12; i++ {
		if err := h.store.AppendAskTurn("FOO", "cap-test", core.AskTurn{
			Question: fmt.Sprintf("question %d", i),
			Answer:   fmt.Sprintf("answer %d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	out, _, code := h.run("ask", "the newest question", "--store", sp, "--project", "FOO", "--session", "cap-test", "--output", "json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s out=%s", code, h.stderr.String(), out)
	}
	if len(captured.Messages) != 22 {
		t.Errorf("messages sent to the model = %d, want 22 — the history cap did not hold", len(captured.Messages))
	}
}

func TestAskDoesNotLogWhenDisabled(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	h.run("task", "create", "--store", sp, "--project", "FOO", "--title", "t", "--description", "d", "--actor", "admin@cli:unset")
	h.run("project", "set-inquiry-log", "--store", sp, "--project", "FOO", "--enabled=false", "--actor", "admin@cli:unset")
	h.run("ask", "anything", "--store", sp, "--project", "FOO", "--output", "json")
	inq, err := h.store.ReadInquiries("FOO")
	if err != nil {
		t.Fatal(err)
	}
	if len(inq) != 0 {
		t.Errorf("got %d inquiries, want none when the log is disabled", len(inq))
	}
}
