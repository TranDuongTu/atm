package store

import (
	"context"
	"fmt"
	"time"
)

type IndexDoc struct {
	ID       string
	Kind     string
	Text     string
	TextHash string
	LogSeq   int
	Title    string
	Snippet  string
	Labels   []string
}

type IndexResult struct {
	Indexed int
	Model   string
	LogSeq  int
}

type EmbedFunc func(text, role string) ([]float64, error)

type ProgressFunc func(msg string)

func (s *Store) PendingIndex(code, slug string) ([]IndexDoc, error) {
	meta, err := s.VectorMeta(code, slug)
	if err != nil {
		return nil, err
	}
	lastIndexed := 0
	if meta != nil {
		lastIndexed = meta.LastLogSeq
	}
	st, err := s.Replay(code)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, nil
	}
	existing := map[string]string{}
	if existingEntries, _ := s.ReadVectors(code, slug); existingEntries != nil {
		for _, e := range existingEntries {
			existing[e.ID] = e.TextHash
		}
	}
	var pending []IndexDoc
	for _, t := range st.Tasks {
		if t.LogSeq <= lastIndexed {
			if h, ok := existing[t.ID]; ok && h == hashText(taskDocumentText(t)) {
				continue
			}
		}
		pending = append(pending, IndexDoc{
			ID: t.ID, Kind: "task", Text: taskDocumentText(t), TextHash: hashText(taskDocumentText(t)),
			LogSeq: t.LogSeq, Title: t.Title, Snippet: snippet(t.Description, 80), Labels: t.Labels,
		})
	}
	for _, c := range st.Comments {
		if c.LogSeq <= lastIndexed {
			if h, ok := existing[c.ID]; ok && h == hashText(commentDocumentText(c)) {
				continue
			}
		}
		pending = append(pending, IndexDoc{
			ID: c.ID, Kind: "comment", Text: commentDocumentText(c), TextHash: hashText(commentDocumentText(c)),
			LogSeq: c.LogSeq, Snippet: snippet(c.Body, 80), Labels: c.Labels,
		})
	}
	return pending, nil
}

func (s *Store) ReindexOnce(code string, embed EmbedFunc, log ProgressFunc) (IndexResult, error) {
	cfg, err := s.GetProjectConfig(code)
	if err != nil {
		return IndexResult{}, err
	}
	if cfg == nil || cfg.Embedding == nil {
		return IndexResult{}, fmt.Errorf("%w: no embedding configured for project %q; run 'atm project set-embedding' first", ErrUsage, code)
	}
	slug := cfg.Embedding.Model
	pending, err := s.PendingIndex(code, slug)
	if err != nil {
		return IndexResult{}, err
	}
	res := IndexResult{Model: slug}
	if len(pending) == 0 {
		if last, _ := s.LastLogSeq(code); last >= 0 {
			res.LogSeq = last
		}
		if log != nil {
			log(fmt.Sprintf("index fresh: nothing to do (model=%s, log_seq=%d)", slug, res.LogSeq))
		}
		return res, nil
	}
	if log != nil {
		log(fmt.Sprintf("indexing %d %s+comment(s) for project %q (model=%s)...", len(pending), kindLabel(pending), code, slug))
	}
	entries := make([]VectorEntry, 0, len(pending))
	maxSeq := 0
	for i, doc := range pending {
		if log != nil {
			log(fmt.Sprintf("embedding %d/%d %s (%s)", i+1, len(pending), doc.ID, doc.Kind))
		}
		vec, err := embed(doc.Text, "document")
		if err != nil {
			return res, fmt.Errorf("embed %s: %w", doc.ID, err)
		}
		entries = append(entries, VectorEntry{
			ID: doc.ID, Kind: doc.Kind, Model: slug, Dim: len(vec), Vector: vec,
			TextHash: doc.TextHash, LogSeq: doc.LogSeq, Title: doc.Title, Snippet: doc.Snippet, Labels: doc.Labels,
		})
		if doc.LogSeq > maxSeq {
			maxSeq = doc.LogSeq
		}
	}
	if err := s.WriteVectorBatch(code, slug, entries, maxSeq); err != nil {
		return res, err
	}
	res.Indexed = len(entries)
	res.LogSeq = maxSeq
	if log != nil {
		log(fmt.Sprintf("wrote %d vector(s) to %s/%s at log_seq %d", res.Indexed, code, slug, res.LogSeq))
	}
	return res, nil
}

func kindLabel(docs []IndexDoc) string {
	if len(docs) == 0 {
		return ""
	}
	first := docs[0].Kind
	for _, d := range docs[1:] {
		if d.Kind != first {
			return "task/comment"
		}
	}
	return first
}

func (s *Store) Watch(ctx context.Context, code string, embed EmbedFunc, log ProgressFunc) error {
	res, err := s.ReindexOnce(code, embed, log)
	if err != nil {
		return err
	}
	const basePoll = 1 * time.Second
	const maxPoll = 30 * time.Second
	poll := basePoll
	lastSeq := res.LogSeq
	for {
		ticker := time.NewTicker(poll)
		defer ticker.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			cur, _ := s.LastLogSeq(code)
			if cur <= lastSeq {
				continue
			}
			res, err := s.ReindexOnce(code, embed, log)
			if err != nil {
				if log != nil {
					log(fmt.Sprintf("index error: %v", err))
				}
				poll *= 2
				if poll > maxPoll {
					poll = maxPoll
				}
				continue
			}
			lastSeq = res.LogSeq
			poll = basePoll
			if log != nil && res.Indexed > 0 {
				log(fmt.Sprintf("indexed %d (model=%s); index at log_seq %d", res.Indexed, res.Model, res.LogSeq))
			}
		}
	}
}
