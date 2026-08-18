package store

import (
	"atm/internal/core"
	"atm/internal/store/eventlog"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"
)

func (s *Store) Search(p SearchParams) (hits []Hit, fallbackUsed bool, err error) {
	if p.K <= 0 {
		p.K = 5
	}
	if p.Threshold <= 0 {
		p.Threshold = 0.30
	}
	entries, err := s.ReadVectors(p.Project, p.Model)
	if err != nil {
		return nil, false, err
	}
	entries = dedupVectorsByID(entries)
	if len(entries) > 0 && len(p.QueryVector) > 0 {
		idxDim := entries[0].Dim
		if len(p.QueryVector) != idxDim {
			return nil, false, fmt.Errorf("%w: query vector dim %d != index dim %d for model %q", core.ErrUsage, len(p.QueryVector), idxDim, p.Model)
		}
		scored := make([]Hit, 0, len(entries))
		for _, e := range entries {
			if p.Kind != "" && p.Kind != "all" && e.Kind != p.Kind {
				continue
			}
			score := cosineSimilarity(p.QueryVector, e.Vector)
			if score < p.Threshold {
				continue
			}
			scored = append(scored, Hit{ID: e.ID, Kind: e.Kind, Score: score, Title: e.Title, Snippet: e.Snippet, Labels: e.Labels, Match: "semantic"})
		}
		sort.SliceStable(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })
		if len(scored) > 0 {
			if len(scored) > p.K {
				scored = scored[:p.K]
			}
			return scored, false, nil
		}
	}
	textHits, err := s.textSearch(p.Project, p.QueryText, p.Kind, p.K)
	if err != nil {
		return nil, true, err
	}
	return textHits, true, nil
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// textSearch is the keyword fallback behind Search. Every project is v2
// (born-v2 conversion is complete), so this folds the event file (through the
// freshness-gated cache rows) rather than reading any log directly.
//
// Two tiers, not one score: an entity the query NAMES (its ID contains the
// query) outranks one that merely mentions the query's words. The ID is not
// part of the document text — taskDocumentText is title+description+labels,
// because an ID is not worth embedding — so token overlap cannot see it, and a
// pasted ID would otherwise find nothing at all. A comment's ID carries its
// task's ID as a prefix, so pasting a task ID surfaces the task and its
// comments together, task first.
//
// Tier 0 is guarded against the project-code prefix every ID in the project
// shares (see namesEntity below): "names an entity" has to mean more than
// "starts the way they all do".
func (s *Store) textSearch(code, query, kind string, k int) ([]Hit, error) {
	qtokens := tokenize(query)
	if len(qtokens) == 0 {
		return nil, nil
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	// The format lookup is propagated, never swallowed: a swallowed error here
	// would silently report "no results" for a project whose entities plainly
	// exist.
	f, err := s.eng.ProjectFormat(code)
	if err != nil {
		return nil, err
	}
	if f != eventlog.StoreFormatV2 {
		return nil, nil
	}
	tasks, comments, err := s.v2CompatEntities(code)
	if err != nil {
		// An integrity failure must never render as "no results". Every other
		// error class returns no hits, no error.
		if core.IsIntegrity(err) {
			return nil, err
		}
		return nil, nil
	}
	// A query that matches nothing but the project-code prefix names no
	// particular entity: every ID in the project starts with it, so treating
	// that as "the user pasted an ID" ranks the whole project at the tier
	// reserved for a named entity. `atm search --project ATM a` returned every
	// task at score 1 before this guard.
	prefix := strings.ToLower(code) + "-"
	namesEntity := needle != "" && !strings.Contains(prefix, needle)
	// tier 0 = the query names this entity, tier 1 = a document match.
	type ranked struct {
		hit  Hit
		tier int
	}
	var scored []ranked
	add := func(id string, hit Hit, overlap int) {
		named := namesEntity && strings.Contains(strings.ToLower(id), needle)
		if overlap == 0 && !named {
			return
		}
		if overlap == 0 {
			// A named entity with no word overlap is still a hit; give it the
			// smallest positive score so Score keeps meaning "how much of the
			// query the document carries" for everything else.
			overlap = 1
		}
		hit.Score = float64(overlap)
		tier := 1
		if named {
			tier = 0
		}
		scored = append(scored, ranked{hit, tier})
	}
	if kind == "" || kind == "all" || kind == "task" {
		for _, t := range tasks {
			add(t.ID, Hit{ID: t.ID, Kind: "task", Title: t.Title, Snippet: snippet(t.Description, 80), Labels: t.Labels, Match: "text"},
				tokenPrefixOverlap(qtokens, tokenize(taskDocumentText(t))))
		}
	}
	if kind == "" || kind == "all" || kind == "comment" {
		for _, c := range comments {
			add(c.ID, Hit{ID: c.ID, Kind: "comment", Snippet: snippet(c.Body, 80), Labels: c.Labels, Match: "text"},
				tokenPrefixOverlap(qtokens, tokenize(commentDocumentText(c))))
		}
	}
	// Stable, so a tie inside a tier keeps entity order: tasks before comments,
	// and creation order within each — which is what makes a task lead its own
	// comments when an ID names all of them.
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].tier != scored[j].tier {
			return scored[i].tier < scored[j].tier
		}
		return scored[i].hit.Score > scored[j].hit.Score
	})
	hits := make([]Hit, 0, len(scored))
	for _, r := range scored {
		hits = append(hits, r.hit)
	}
	if len(hits) > k {
		hits = hits[:k]
	}
	return hits, nil
}

func tokenize(s string) []string {
	s = strings.ToLower(s)
	return strings.FieldsFunc(s, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
}

// tokenPrefixOverlap counts how many of the query's tokens the document
// carries, matching each query token as a PREFIX of a document token.
//
// Prefix, not exact: the spotlight queries this path on a debounce as the user
// types, so exact-token overlap left the results section empty until a whole
// word was finished — a user who had typed "ind" of "indexer" saw nothing,
// which is strictly worse than the strings.Contains matcher the search
// redesign replaced. An exact match still counts, being a prefix of itself.
//
// Prefix, not fuzzy and not infix: "ind" finds "indexer", "dex" does not.
// That keeps the rule explainable at the surface — what you have typed so far
// is the start of a word in the thing you are looking for.
func tokenPrefixOverlap(query, doc []string) int {
	n := 0
	for _, q := range query {
		for _, d := range doc {
			if strings.HasPrefix(d, q) {
				n++
				break
			}
		}
	}
	return n
}

// snippet trims s to at most max RUNES, never bytes: a byte slice can split a
// multi-byte rune, and encoding/json silently rewrites the resulting invalid
// UTF-8 to U+FFFD rather than rejecting it — so the corruption reaches an
// agent parsing citations[].snippet without anything having failed loudly
// (ATM-d4ceed).
func snippet(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	n := 0
	for i := range s {
		if n == max-1 {
			return s[:i] + "…"
		}
		n++
	}
	return s
}

func dedupVectorsByID(entries []VectorEntry) []VectorEntry {
	latest := map[string]VectorEntry{}
	for _, e := range entries {
		// >= not >: file order is append order, so on a tied LogSeq the later
		// entry is the newer embedding. v1 was indifferent (seqs strictly
		// increase); v2 re-embeddings reuse the entity's stable creation
		// ordinal, so first-wins would pin the STALE vector.
		if cur, ok := latest[e.ID]; !ok || e.LogSeq >= cur.LogSeq {
			latest[e.ID] = e
		}
	}
	out := make([]VectorEntry, 0, len(latest))
	for _, e := range latest {
		out = append(out, e)
	}
	return out
}
