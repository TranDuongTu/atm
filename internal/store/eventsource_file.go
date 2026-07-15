package store

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"atm/internal/eventsource"
)

type V2FileSnapshot struct {
	Events         []*eventsource.Event
	EventCount     int
	FileSize       int64
	TruncatedBytes int
	Frontier       []string
}

// readV2FileAt reads a v2 event file. The commit point is a complete,
// newline-terminated line (L3-7): every byte after the last '\n' is an
// uncommitted partial tail — even if it happens to parse as JSON. A
// bufio.Scanner would hide that distinction (it yields an unterminated
// tail as a normal line), so the split is done on the raw bytes.
func (s *Store) readV2FileAt(path string, repairTail bool) (*V2FileSnapshot, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &V2FileSnapshot{}, nil
	}
	if err != nil {
		return nil, err
	}

	body, tail := raw, 0
	if n := len(raw); n > 0 && raw[n-1] != '\n' {
		cut := bytes.LastIndexByte(raw, '\n') + 1
		body, tail = raw[:cut], n-cut
	}
	if tail > 0 {
		if !repairTail {
			return nil, fmt.Errorf("%w: %s has %d bytes of uncommitted partial tail", ErrIntegrity, path, tail)
		}
		if err := os.Truncate(path, int64(len(body))); err != nil {
			return nil, err
		}
	}

	var events []*eventsource.Event
	lines := bytes.Split(body, []byte("\n"))
	for i, line := range lines {
		if i == len(lines)-1 && len(line) == 0 {
			break // split artifact after the final newline
		}
		ev, err := eventsource.Parse(line)
		if err != nil {
			// A complete line that fails to parse is an integrity error,
			// never a repair target (spec crash-recovery rules).
			return nil, fmt.Errorf("%w: %s:%d: %v", ErrIntegrity, path, i+1, err)
		}
		events = append(events, ev)
	}

	dag, err := eventsource.BuildDAG(events)
	if err != nil {
		return nil, fmt.Errorf("%w: %s DAG: %v", ErrIntegrity, path, err)
	}
	return &V2FileSnapshot{
		Events:         events,
		EventCount:     len(events),
		FileSize:       int64(len(body)),
		TruncatedBytes: tail,
		Frontier:       dag.Frontier(),
	}, nil
}

func (s *Store) readV2File(code string, repairTail bool) (*V2FileSnapshot, error) {
	return s.readV2FileAt(s.eventsV2Path(code), repairTail)
}

// verifyV2File is the strict read: parse, recompute ids, validate parents,
// build the DAG — and never repair.
func (s *Store) verifyV2File(code string) (*V2FileSnapshot, error) {
	return s.readV2File(code, false)
}

func (s *Store) appendV2EventLineLocked(code string, raw []byte) error {
	path := s.eventsV2Path(code)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(raw); err != nil {
		return err
	}
	if _, err := f.Write([]byte("\n")); err != nil {
		return err
	}
	return f.Sync()
}
