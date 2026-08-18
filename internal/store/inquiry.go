package store

import (
	"atm/internal/core"
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

type InquiryEntry struct {
	Query string `json:"query"`
	// ReturnedIDs is what the search actually returned. Recall@k has no
	// denominator without it (ATM-028a8d), which is why it exists separately
	// from CitedIDs — a cited ID is a strong relevance signal, a returned ID is
	// the candidate set that signal is measured against.
	//
	// Deliberately NOT omitempty, unlike a first draft of this field: an empty
	// returned set must serialise as [] and not vanish, because a missing key
	// is how a line written BEFORE this field existed looks. "Searched and
	// found nothing" and "we do not know what this search returned" are
	// different facts, and eval has to be able to tell them apart. CitedIDs
	// has never carried omitempty either.
	ReturnedIDs []string `json:"returned_ids"`
	CitedIDs    []string `json:"cited_ids"`
	At          string   `json:"at"`
}

func (s *Store) AppendInquiry(code, query string, returnedIDs, citedIDs []string) error {
	return s.WithLock(code, func() error {
		if err := os.MkdirAll(s.projectDir(code), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(s.inquiryLogPath(code), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		e := InquiryEntry{Query: query, ReturnedIDs: returnedIDs, CitedIDs: citedIDs, At: core.RFC3339UTC(core.Now())}
		b, err := json.Marshal(e)
		if err != nil {
			return err
		}
		_, err = f.Write(append(b, '\n'))
		return err
	})
}

func (s *Store) ReadInquiries(code string) ([]InquiryEntry, error) {
	f, err := os.Open(s.inquiryLogPath(code))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []InquiryEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e InquiryEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		out = append(out, e)
	}
	return out, sc.Err()
}
