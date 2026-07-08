package store

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

type Hit struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Score   float64  `json:"score"`
	Title   string   `json:"title,omitempty"`
	Snippet string   `json:"snippet"`
	Labels  []string `json:"labels,omitempty"`
	Match   string   `json:"match"`
}

type SearchParams struct {
	Project     string
	Model       string
	QueryVector []float64
	QueryText   string
	Kind        string
	K           int
	Threshold   float64
}

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
			return nil, false, fmt.Errorf("%w: query vector dim %d != index dim %d for model %q", ErrUsage, len(p.QueryVector), idxDim, p.Model)
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
	textHits := s.textSearch(p.Project, p.QueryText, p.Kind, p.K)
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

func (s *Store) textSearch(code, query, kind string, k int) []Hit {
	qtokens := tokenize(query)
	if len(qtokens) == 0 {
		return nil
	}
	var hits []Hit
	if kind == "" || kind == "all" || kind == "task" {
		if st, err := s.Replay(code); err == nil && st != nil {
			for _, t := range st.Tasks {
				doc := taskDocumentText(t)
				if score := tokenOverlap(qtokens, tokenize(doc)); score > 0 {
					hits = append(hits, Hit{ID: t.ID, Kind: "task", Score: float64(score), Title: t.Title, Snippet: snippet(t.Description, 80), Labels: t.Labels, Match: "text"})
				}
			}
		}
	}
	if kind == "" || kind == "all" || kind == "comment" {
		if st, err := s.Replay(code); err == nil && st != nil {
			for _, c := range st.Comments {
				doc := commentDocumentText(c)
				if score := tokenOverlap(qtokens, tokenize(doc)); score > 0 {
					hits = append(hits, Hit{ID: c.ID, Kind: "comment", Score: float64(score), Snippet: snippet(c.Body, 80), Labels: c.Labels, Match: "text"})
				}
			}
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > k {
		hits = hits[:k]
	}
	return hits
}

func tokenize(s string) []string {
	s = strings.ToLower(s)
	return strings.FieldsFunc(s, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
}

func tokenOverlap(query, doc []string) int {
	dset := map[string]bool{}
	for _, w := range doc {
		dset[w] = true
	}
	n := 0
	for _, w := range query {
		if dset[w] {
			n++
		}
	}
	return n
}

func snippet(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func dedupVectorsByID(entries []VectorEntry) []VectorEntry {
	latest := map[string]VectorEntry{}
	for _, e := range entries {
		if cur, ok := latest[e.ID]; !ok || e.LogSeq > cur.LogSeq {
			latest[e.ID] = e
		}
	}
	out := make([]VectorEntry, 0, len(latest))
	for _, e := range latest {
		out = append(out, e)
	}
	return out
}
