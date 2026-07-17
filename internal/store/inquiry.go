package store

import (
	"atm/internal/core"
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

type InquiryEntry struct {
	Query    string   `json:"query"`
	CitedIDs []string `json:"cited_ids"`
	At       string   `json:"at"`
}

func (s *Store) AppendInquiry(code, query string, citedIDs []string) error {
	return s.WithLock(code, func() error {
		if err := os.MkdirAll(s.projectDir(code), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(s.inquiryLogPath(code), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		e := InquiryEntry{Query: query, CitedIDs: citedIDs, At: core.RFC3339UTC(core.Now())}
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
