package answer

import (
	"strings"
	"unicode/utf8"

	"atm/internal/core"
)

// defaultSourceBudget is the total characters of source text handed to the
// model across ALL hits in one turn (~3000 tokens). Large enough that a
// comment's body survives, small enough for the local models ATM targets —
// ATM-66a6d2's live verification ran qwen3:0.6b.
const defaultSourceBudget = 12000

// truncMarker signals to the model that it is holding a fragment. Its length
// is RESERVED from max rather than added after, so clip's result honours the
// allowance allot handed it — the budget is only a budget if the thing that
// enforces it does not overshoot.
const truncMarker = "…[truncated]"

// source is one numbered block of the prompt: the hit it came from, and the
// text actually shown for it after hydration and budgeting.
type source struct {
	hit  core.Hit
	text string
}

// buildSources pairs each hit with the best text available for it — the
// hydrated document when there is one, the hit's own Snippet otherwise — and
// fits the set inside budget.
//
// docs may be nil or partial. Hydration failing is not an answer failing
// (ATM-e4be94's rule that retrieval never breaks because generation cannot
// run), so a missing entry silently degrades to the snippet.
func buildSources(hits []core.Hit, docs map[string]string, budget int) []source {
	if budget <= 0 {
		budget = defaultSourceBudget
	}
	texts := make([]string, len(hits))
	lengths := make([]int, len(hits))
	for i, h := range hits {
		text := docs[h.ID]
		if strings.TrimSpace(text) == "" {
			text = h.Snippet
		}
		texts[i] = text
		lengths[i] = utf8.RuneCountInString(text)
	}
	allowance := allot(lengths, budget)
	out := make([]source, 0, len(hits))
	for i, h := range hits {
		out = append(out, source{hit: h, text: clip(texts[i], allowance[i])})
	}
	return out
}

// allot spreads budget across lengths: a fair share each, then the remainder
// that short entries did not use redistributed to the ones still over.
//
// A flat budget/N cap would clip a single long, highly-relevant comment while
// seven short ones left most of their share unspent — which is precisely the
// case worth spending context on.
func allot(lengths []int, budget int) []int {
	n := len(lengths)
	out := make([]int, n)
	if n == 0 {
		return out
	}
	settled := make([]bool, n)
	remaining, unsettled := budget, n
	for unsettled > 0 {
		share := remaining / unsettled
		progress := false
		for i, l := range lengths {
			if settled[i] || l > share {
				continue
			}
			out[i], settled[i] = l, true
			remaining -= l
			unsettled--
			progress = true
		}
		if !progress {
			// Everyone left is over the share; they each take exactly it.
			for i := range lengths {
				if !settled[i] {
					out[i] = share
				}
			}
			break
		}
	}
	return out
}

// clip cuts s to at most max runes, on a rune boundary, and says so when it
// cuts. The result is never longer than max.
func clip(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	markerRunes := utf8.RuneCountInString(truncMarker)
	if max <= markerRunes {
		// No room for content and a marker both. A bare ellipsis is 1 rune, so
		// it always fits, and still signals that text was dropped.
		return "…"
	}
	keep := max - markerRunes
	n := 0
	for i := range s {
		if n == keep {
			return s[:i] + truncMarker
		}
		n++
	}
	return s
}
