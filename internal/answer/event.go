package answer

import "atm/internal/core"

// Event is one step of an answer, delivered to Ask's emit callback in order.
// A sealed interface rather than a struct with a kind field: consumers read
// it with a type switch (the TUI's ask level is a bubbletea update loop), and
// the compiler then checks that every arm handles a real event.
//
// The order is fixed for a question that passes validation: Retrieved, then
// zero or more Delta, then exactly one terminal event — Done or Failed. An
// empty question never enters this sequence at all — Ask returns
// core.ErrUsage before the stream begins and emits nothing, because a
// malformed call is not an answer that broke — so a consumer driven purely
// by events would never see that case; it must also check Ask's returned
// error.
type Event interface{ event() }

// Retrieved carries the sources, always, before any generation is attempted:
// hits reach the consumer even when no model ever answers (ATM-e4be94's rule
// that retrieval never breaks because generation cannot run). Behind is how
// many entities are still waiting to be embedded, so a consumer can say
// "sources may lag".
type Retrieved struct {
	Hits   []core.Hit
	Behind int
}

// Delta is one chunk of the answer as it streams.
type Delta struct{ Text string }

// Done ends a successful ask. Citations are the sources the answer actually
// named, in the order it first named them.
//
// Degraded means no answer was generated at all — no chat model configured,
// or an endpoint that never produced a single delta — and Reason says which.
// It rides on Done rather than Failed because a hits-only answer is a
// successful outcome: `atm ask` exits 0 for it and the spotlight shows sources
// with a hint, neither of which is an error.
type Done struct {
	Citations []core.Hit
	Degraded  bool
	Reason    string
}

// Failed ends an ask that broke. It arrives AFTER any deltas already
// delivered — the partial answer is the consumer's to keep and to mark.
// Canceled distinguishes the caller's own cancellation (Esc, ctrl-C) from an
// endpoint that dropped mid-answer.
type Failed struct {
	Reason   string
	Canceled bool
}

func (Retrieved) event() {}
func (Delta) event()     {}
func (Done) event()      {}
func (Failed) event()    {}
