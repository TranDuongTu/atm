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
	// OpenedIDs is what a HUMAN opened from these results -- the spotlight's
	// click-through (ATM-f71b81). Kept apart from CitedIDs because sub-task 6
	// weighs them differently: a citation is a model saying a source looked
	// relevant, an open is a person acting on it.
	//
	// No omitempty, for the reason ReturnedIDs already records: a missing key
	// is how a line written before this field existed looks, and "opened
	// nothing" is a different fact from "we do not know".
	OpenedIDs []string `json:"opened_ids"`
	At        string   `json:"at"`
}

func (s *Store) AppendInquiry(code, query string, returnedIDs, citedIDs, openedIDs []string) error {
	return s.WithLock(code, func() error {
		if err := os.MkdirAll(s.projectDir(code), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(s.inquiryLogPath(code), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		e := InquiryEntry{
			Query:       query,
			ReturnedIDs: nonNilIDs(returnedIDs),
			CitedIDs:    nonNilIDs(citedIDs),
			OpenedIDs:   nonNilIDs(openedIDs),
			At:          core.RFC3339UTC(core.Now()),
		}
		b, err := json.Marshal(e)
		if err != nil {
			return err
		}
		_, err = f.Write(append(b, '\n'))
		return err
	})
}

// nonNilIDs turns a nil slice into a non-nil empty one so it marshals as []
// rather than null. A caller passing nil means "recorded, and there was
// nothing" -- it must not collapse onto the encoding a pre-field line
// produces on read (an absent key unmarshals to a nil slice too). Relying on
// each caller to already hand back a non-nil empty slice is exactly the
// accident that let this drift before; this is the one choke point that can
// guarantee it regardless of what any caller passes.
func nonNilIDs(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
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
