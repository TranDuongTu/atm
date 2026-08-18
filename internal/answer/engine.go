// Package answer is ATM's local RAG engine: it retrieves from the project's
// own ledger and streams a cited answer from a local chat model.
//
// The governing rule (ATM-e4be94): retrieval never breaks because generation
// cannot run. Every path delivers its hits first, and a missing or unreachable
// chat model ends in a degraded Done, never a failure.
package answer

import (
	"context"
	"fmt"
	"strings"

	"atm/internal/chat"
	"atm/internal/core"
)

// Turn is one completed exchange, replayed to the model as history.
type Turn struct {
	Question string
	Answer   string
}

// Query is one ask: the new question plus the conversation it continues.
type Query struct {
	Question string
	History  []Turn
}

// defaultK is the retrieval width when a caller states none. Eight matches
// what the spotlight asks for (ATM-f58753) and fits a local model's context
// alongside the instruction.
const defaultK = 8

// Searcher is the slice of the store the engine reads. An interface rather
// than *store.Store, so the engine's tests are pure fakes (internal/setup's
// injected-probe design) and any holder of a core.Service satisfies it.
type Searcher interface {
	Search(p core.SearchParams) ([]core.Hit, bool, error)
	PendingIndexCount(code, slug string) (int, error)
}

// Streamer is internal/chat's client, narrowed to the one method generation
// needs.
type Streamer interface {
	Stream(ctx context.Context, msgs []chat.Message, onDelta func(string)) error
}

type Config struct {
	Project  string
	Searcher Searcher
	// Embed embeds the question for the semantic path. Nil, or a call that
	// fails, is survivable: the query vector is dropped and store.Search falls
	// back to its text pass over live entities.
	Embed core.EmbedFunc
	// Model is the embedding model slug the vector index is keyed by, and the
	// key Behind counts against. Empty means text-only retrieval.
	Model string
	// Chat generates the answer. Nil means answers degrade to hits.
	Chat      Streamer
	K         int
	Threshold float64
}

type Engine struct{ cfg Config }

func New(cfg Config) *Engine {
	if cfg.K <= 0 {
		cfg.K = defaultK
	}
	return &Engine{cfg: cfg}
}

// Ask answers one question, emitting events in order: Retrieved, then zero or
// more Delta, then exactly one terminal Done or Failed. emit is called on the
// calling goroutine; cancellation runs through ctx.
//
// That sequence is the guarantee for a question that passes validation. The
// empty-question guard below is caller validation, not an answer that broke:
// it returns core.ErrUsage before the stream begins and emits NOTHING, because
// a malformed call is not a failed answer — Failed is reserved for a real
// break (a mid-stream disconnect or a cancellation), the same line Done's
// Degraded field draws for "no answer, but not broken" (see event.go). A
// consumer driven purely by events would otherwise never see this case, so it
// must also check the returned error.
//
// The returned error is narrow by design: it is non-nil only when retrieval
// itself failed (a broken ledger, or a question with nothing in it). A
// degraded answer and a mid-stream break both return nil, because both are
// exit-0 outcomes for `atm ask` (ATM-d4ceed) — the event says what happened.
func (e *Engine) Ask(ctx context.Context, q Query, emit func(Event)) error {
	if strings.TrimSpace(q.Question) == "" {
		// Caller validation, not a broken answer: returns an error and emits
		// nothing. Contrast the store-error path just below, which is the one
		// path that both emits a terminal event AND returns an error — the two
		// are easy to conflate, so this comment and that one each say which
		// they are.
		return fmt.Errorf("%w: empty question", core.ErrUsage)
	}
	hits, err := e.retrieve(ctx, q.Question)
	if err != nil {
		// The ledger itself failed. This is the one path that both emits a
		// terminal event and returns an error: a broken store is not a
		// degraded answer, and a CLI must be able to exit non-zero for it.
		emit(Failed{Reason: err.Error()})
		return err
	}
	emit(Retrieved{Hits: hits, Behind: e.behind()})
	if e.cfg.Chat == nil {
		emit(Done{Degraded: true, Reason: "no chat model configured; run 'atm project set-chat'"})
		return nil
	}
	var answer strings.Builder
	streamErr := e.cfg.Chat.Stream(ctx, buildMessages(q, hits), func(text string) {
		answer.WriteString(text)
		emit(Delta{Text: text})
	})
	if streamErr != nil {
		switch {
		case ctx.Err() != nil:
			// The caller stopped it. The chat client cancels a DERIVED context
			// for its own watchdogs, so this only trips on a real cancel.
			emit(Failed{Reason: streamErr.Error(), Canceled: true})
		case answer.Len() == 0:
			// Nothing ever arrived: generation never happened, so this is the
			// unreachable-endpoint degrade, not an interrupted answer.
			emit(Done{Degraded: true, Reason: streamErr.Error()})
		default:
			emit(Failed{Reason: streamErr.Error()})
		}
		return nil
	}
	if answer.Len() == 0 {
		// The endpoint ended the stream cleanly without ever producing a token:
		// generation did not happen, which is the same outcome as an unreachable
		// endpoint and must not read as a finished answer. Reachable in practice
		// — an endpoint that ignores "stream": true returns a plain JSON body
		// with no data: lines, and the SSE loop reads it to EOF and returns nil.
		emit(Done{Degraded: true, Reason: "the chat endpoint returned no answer"})
		return nil
	}
	emit(Done{Citations: citedHits(answer.String(), hits)})
	return nil
}

// retrieve runs one turn's retrieval. An embedding failure is NOT a retrieval
// failure: the query vector is dropped and store.Search falls back to its text
// path, which reads live entities — so a down ollama still answers with hits,
// and Hit.Match ("text") is how a consumer can tell which path ran.
func (e *Engine) retrieve(ctx context.Context, question string) ([]core.Hit, error) {
	var qv []float64
	if e.cfg.Embed != nil && e.cfg.Model != "" {
		if vec, err := e.cfg.Embed(ctx, question, "query"); err == nil {
			qv = vec
		}
	}
	hits, _, err := e.cfg.Searcher.Search(core.SearchParams{
		Project: e.cfg.Project, Model: e.cfg.Model, QueryVector: qv, QueryText: question,
		Kind: "all", K: e.cfg.K, Threshold: e.cfg.Threshold,
	})
	if err != nil {
		return nil, err
	}
	return hits, nil
}

// behind is the staleness hint, never a failure: a count ATM cannot take is
// reported as zero rather than sinking an answer over a display detail. With
// no embedding model there is no index to be behind.
func (e *Engine) behind() int {
	if e.cfg.Model == "" {
		return 0
	}
	n, err := e.cfg.Searcher.PendingIndexCount(e.cfg.Project, e.cfg.Model)
	if err != nil {
		return 0
	}
	return n
}
