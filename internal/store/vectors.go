package store

import (
	"atm/internal/core"
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type VectorEntry struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	Model    string    `json:"model"`
	Dim      int       `json:"dim"`
	Vector   []float64 `json:"vector"`
	TextHash string    `json:"text_hash"`
	LogSeq   int       `json:"log_seq"`
	Title    string    `json:"title,omitempty"`
	Snippet  string    `json:"snippet"`
	Labels   []string  `json:"labels,omitempty"`
}

func (s *Store) ReadVectors(code, slug string) ([]VectorEntry, error) {
	f, err := os.Open(s.vectorPath(code, slug))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []VectorEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e VectorEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

func (s *Store) WriteVectorBatch(code, slug string, entries []VectorEntry, lastLogSeq int) error {
	if len(entries) == 0 {
		return fmt.Errorf("%w: empty vector batch", core.ErrUsage)
	}
	dim := entries[0].Dim
	for i, e := range entries {
		if e.Model != slug {
			return fmt.Errorf("%w: entry %d model %q != batch model %q", core.ErrUsage, i, e.Model, slug)
		}
		if e.Dim != dim {
			return fmt.Errorf("%w: entry %d dim %d != batch dim %d", core.ErrUsage, i, e.Dim, dim)
		}
	}
	return s.WithLock(code, func() error {
		dir := s.vectorsDir(code)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		path := s.vectorPath(code, slug)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		for _, e := range entries {
			b, err := json.Marshal(e)
			if err != nil {
				f.Close()
				return err
			}
			if _, err := f.Write(append(b, '\n')); err != nil {
				f.Close()
				return err
			}
		}
		if err := f.Close(); err != nil {
			return err
		}
		count, err := countVectorLines(path)
		if err != nil {
			return err
		}
		meta := &VectorMeta{
			Model:           slug,
			Dim:             dim,
			LastLogSeq:      lastLogSeq,
			LastReindexedAt: core.RFC3339UTC(core.Now()),
			Count:           count,
		}
		return WriteFileAtomic(s.vectorMetaPath(code, slug), meta)
	})
}

func countVectorLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	n := 0
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) != "" {
			n++
		}
	}
	return n, sc.Err()
}

func (s *Store) VectorMeta(code, slug string) (*VectorMeta, error) {
	var m VectorMeta
	if err := ReadJSON(s.vectorMetaPath(code, slug), &m); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &m, nil
}

func (s *Store) DropVectors(code, slug string) error {
	return s.WithLock(code, func() error {
		if err := os.Remove(s.vectorPath(code, slug)); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("%w: no vector index for model %q", core.ErrNotFound, slug)
			}
			return err
		}
		_ = os.Remove(s.vectorMetaPath(code, slug))
		return nil
	})
}

func (s *Store) ListVectorModels(code string) ([]string, error) {
	entries, err := os.ReadDir(s.vectorsDir(code))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		out = append(out, strings.TrimSuffix(name, ".jsonl"))
	}
	sort.Strings(out)
	return out, nil
}

func taskDocumentText(t *Task) string {
	return strings.Join(append([]string{t.Title, t.Description}, t.Labels...), " ")
}

func commentDocumentText(c *Comment) string {
	return strings.Join(append([]string{c.Body}, c.Labels...), " ")
}

func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])
}
