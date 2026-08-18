package answer

import (
	"strings"
	"testing"

	"atm/internal/core"
)

func hit(id, kind, title, snippet string) core.Hit {
	return core.Hit{ID: id, Kind: kind, Title: title, Snippet: snippet, Score: 0.9, Match: "semantic"}
}

func TestBuildMessagesNumbersSourcesAndAsksLast(t *testing.T) {
	msgs := buildMessages(Query{Question: "who owns the indexer?"}, []core.Hit{
		hit("ATM-aaa111", "task", "wire the indexer", "the watcher owns freshness"),
		hit("ATM-aaa111-c42", "comment", "", "g1 is its console"),
	})
	if len(msgs) != 2 || msgs[0].Role != "system" || msgs[1].Role != "user" {
		t.Fatalf("messages = %+v, want system then user", msgs)
	}
	if !strings.Contains(msgs[0].Content, "[2]") {
		t.Error("the system message must show the cite-by-number form it demands")
	}
	body := msgs[1].Content
	for _, want := range []string{"[1] ATM-aaa111 (task)", "[2] ATM-aaa111-c42 (comment)", "who owns the indexer?"} {
		if !strings.Contains(body, want) {
			t.Errorf("user message missing %q:\n%s", want, body)
		}
	}
	if strings.Index(body, "SOURCES") > strings.Index(body, "QUESTION") {
		t.Error("sources must precede the question")
	}
}

// Retrieval re-runs per turn, so only the newest turn carries source blocks:
// replaying stale ones spends context on hits already answered from.
func TestBuildMessagesReplaysHistoryWithoutStaleSources(t *testing.T) {
	msgs := buildMessages(Query{
		Question: "and who watches it?",
		History:  []Turn{{Question: "who owns the indexer?", Answer: "the watcher [1]"}},
	}, []core.Hit{hit("ATM-bbb222", "task", "watcher", "runs per project")})
	if len(msgs) != 4 {
		t.Fatalf("messages = %d, want system + 2 history + 1 current", len(msgs))
	}
	if msgs[1].Role != "user" || msgs[1].Content != "who owns the indexer?" {
		t.Errorf("history question = %+v", msgs[1])
	}
	if msgs[2].Role != "assistant" || msgs[2].Content != "the watcher [1]" {
		t.Errorf("history answer = %+v", msgs[2])
	}
	var withSources int
	for _, m := range msgs {
		if strings.Contains(m.Content, "SOURCES") {
			withSources++
		}
	}
	if withSources != 1 {
		t.Errorf("%d messages carry sources, want only the newest turn", withSources)
	}
}

// An empty retrieval is stated, not hidden: a model handed a bare question
// answers from its own weights, which is exactly what ATM must not do.
func TestBuildMessagesSaysSoWhenNothingWasRetrieved(t *testing.T) {
	msgs := buildMessages(Query{Question: "anything?"}, nil)
	body := msgs[len(msgs)-1].Content
	if !strings.Contains(body, "SOURCES") || !strings.Contains(body, "none") {
		t.Errorf("user message = %q, want an explicit empty-sources line", body)
	}
}

func TestCitedHitsKeepsFirstAppearanceOrderAndDedupes(t *testing.T) {
	hits := []core.Hit{hit("A", "task", "a", ""), hit("B", "task", "b", ""), hit("C", "task", "c", "")}
	got := citedHits("first [3], then [1], again [3].", hits)
	if len(got) != 2 || got[0].ID != "C" || got[1].ID != "A" {
		t.Errorf("citations = %+v, want [C A]", got)
	}
}

func TestCitedHitsIgnoresNumbersNamingNoSource(t *testing.T) {
	hits := []core.Hit{hit("A", "task", "a", "")}
	if got := citedHits("see [4] and [0] and [1]", hits); len(got) != 1 || got[0].ID != "A" {
		t.Errorf("citations = %+v, want just [A]", got)
	}
}

// Citations are what the answer leaned on, not the retrieval. The hits
// already travel on Retrieved; padding this would claim support the answer
// never used.
func TestCitedHitsEmptyWhenTheAnswerCitedNothing(t *testing.T) {
	hits := []core.Hit{hit("A", "task", "a", "")}
	if got := citedHits("I could not find that in the ledger.", hits); len(got) != 0 {
		t.Errorf("citations = %+v, want none", got)
	}
}
