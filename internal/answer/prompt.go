package answer

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"atm/internal/chat"
	"atm/internal/core"
)

// systemPrompt is the whole standing instruction. Cite-by-number is not
// cosmetic: it is how Done's citations are recovered from the finished text
// (see citedHits), so the numbering in buildMessages and the bracket form
// demanded here have to stay in step.
const systemPrompt = "You are ATM's answer engine. ATM is a task ledger, and the numbered sources below are its own tasks and comments.\n" +
	"Answer the question using ONLY those sources. Cite every claim with its source's number in square brackets, like [2].\n" +
	"If the sources do not carry the answer, say so plainly instead of guessing. Be concise."

// buildMessages renders one turn's request: the standing instruction, the
// prior turns verbatim, then this turn's numbered sources and question.
//
// Only the newest turn carries source blocks. Retrieval re-runs every turn,
// so re-inlining earlier hits would spend the model's context on sources it
// has already answered from.
func buildMessages(q Query, hits []core.Hit) []chat.Message {
	msgs := []chat.Message{{Role: "system", Content: systemPrompt}}
	for _, t := range q.History {
		msgs = append(msgs, chat.Message{Role: "user", Content: t.Question})
		msgs = append(msgs, chat.Message{Role: "assistant", Content: t.Answer})
	}
	var b strings.Builder
	b.WriteString("SOURCES\n")
	if len(hits) == 0 {
		// Stated, not omitted: a model handed a bare question answers from its
		// own weights, which is the one thing this engine must not do.
		b.WriteString("(none - the ledger returned no matching tasks or comments)\n")
	}
	for i, h := range hits {
		label := h.Title
		if label == "" {
			label = h.Snippet
		}
		fmt.Fprintf(&b, "[%d] %s (%s) %s\n", i+1, h.ID, h.Kind, strings.TrimSpace(label))
		if h.Title != "" && h.Snippet != "" {
			fmt.Fprintf(&b, "    %s\n", strings.TrimSpace(h.Snippet))
		}
	}
	fmt.Fprintf(&b, "\nQUESTION\n%s", q.Question)
	return append(msgs, chat.Message{Role: "user", Content: b.String()})
}

var citeRe = regexp.MustCompile(`\[(\d+)\]`)

// citedHits recovers the sources the answer leaned on, in the order it first
// named them, deduped. A number naming no source is ignored rather than
// guessed at, and an answer that cited nothing yields nothing: every hit
// already reached the consumer on Retrieved, so padding this list would claim
// support the answer never used.
func citedHits(text string, hits []core.Hit) []core.Hit {
	var out []core.Hit
	seen := map[int]bool{}
	for _, m := range citeRe.FindAllStringSubmatch(text, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 || n > len(hits) || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, hits[n-1])
	}
	return out
}
