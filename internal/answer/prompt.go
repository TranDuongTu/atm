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
//
// One number per bracket is spelled out because citeRe matches only single-
// number brackets: a model writing [1, 2] would have BOTH citations dropped.
// The fix belongs here rather than in the regex — a pattern loose enough to
// read [1, 2] also reads a year range quoted out of a task description, and
// fabricating a citation is worse than missing one.
const systemPrompt = "You are ATM's answer engine. ATM is a task ledger, and the numbered sources below are its own tasks and comments.\n" +
	"Answer the question using ONLY those sources. Cite every claim with its source's number in square brackets, like [2]. Use one number per bracket: write [1][2], not [1, 2].\n" +
	"If the sources do not carry the answer, say so plainly instead of guessing. Be concise."

// buildMessages renders one turn's request: the standing instruction, the
// prior turns verbatim, then this turn's numbered sources and question.
//
// Only the newest turn carries source blocks. Retrieval re-runs every turn, so
// re-inlining earlier hits would spend the model's context on sources it has
// already answered from.
//
// Sources arrive already hydrated and budgeted (see sources.go). Until
// ATM-d4ceed they were built from core.Hit.Snippet, an 80-rune truncation — so
// the model was told to answer from sources while holding roughly one line of
// each, and for a comment, whose body IS its content, that was close to
// nothing. buildSources now supplies the full document text where the store
// has it, clipped only to fit a total budget and visibly marked when clipped.
func buildMessages(q Query, srcs []source) []chat.Message {
	msgs := []chat.Message{{Role: "system", Content: systemPrompt}}
	for _, t := range q.History {
		msgs = append(msgs, chat.Message{Role: "user", Content: t.Question})
		msgs = append(msgs, chat.Message{Role: "assistant", Content: t.Answer})
	}
	var b strings.Builder
	b.WriteString("SOURCES\n")
	if len(srcs) == 0 {
		// Stated, not omitted: a model handed a bare question answers from its
		// own weights, which is the one thing this engine must not do.
		b.WriteString("(none - the ledger returned no matching tasks or comments)\n")
	}
	for i, s := range srcs {
		label := s.hit.Title
		if label == "" {
			label = s.hit.ID
		}
		fmt.Fprintf(&b, "[%d] %s (%s) %s\n", i+1, s.hit.ID, s.hit.Kind, strings.TrimSpace(label))
		if text := strings.TrimSpace(s.text); text != "" {
			fmt.Fprintf(&b, "    %s\n", text)
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
