package store

import (
	"fmt"
	"os"
	"sort"
)

type VerifyReport struct {
	Project    string
	LogEntries int
	LogOK      bool
	Truncated  int
	SeqGaps    []int // seqs that are missing from an otherwise monotone sequence
	Caches     []CacheCheck
	Diverged   bool
}

type CacheCheck struct {
	Path         string
	Status       string // "ok" | "stale" | "missing" | "corrupt"
	CacheLogSeq  int
	LastEventSeq int
}

func (s *Store) Verify() ([]VerifyReport, error) {
	var out []VerifyReport
	for _, p := range s.ListProjects() {
		r, err := s.VerifyProject(p.Code)
		if err != nil {
			return out, err
		}
		out = append(out, *r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Project < out[j].Project })
	return out, nil
}

func (s *Store) VerifyProject(code string) (*VerifyReport, error) {
	report := &VerifyReport{Project: code, LogOK: true}
	entries, err := s.ReadLog(code)
	if err != nil {
		if IsIntegrity(err) {
			report.LogOK = false
			report.Truncated = extractTruncatedBytes(err)
		} else {
			return nil, err
		}
	}
	report.LogEntries = len(entries)
	// Detect seq gaps.
	last := 0
	for _, e := range entries {
		if e.Seq != last+1 {
			report.SeqGaps = append(report.SeqGaps, last+1)
			report.LogOK = false
		}
		last = e.Seq
	}
	// Replay to get the canonical live set.
	st, _ := s.Replay(code)
	// Verify project cache.
	report.Caches = append(report.Caches, s.checkProjectCache(code, st))
	// Verify each task cache.
	for _, t := range st.Tasks {
		report.Caches = append(report.Caches, s.checkTaskCache(code, t.ID, t.LogSeq))
	}
	for _, c := range report.Caches {
		if c.Status != "ok" {
			report.Diverged = true
		}
	}
	return report, nil
}

func (s *Store) checkProjectCache(code string, st *ReplayState) CacheCheck {
	path := s.projectPath(code)
	var p Project
	if err := ReadJSON(path, &p); err != nil {
		if os.IsNotExist(err) {
			return CacheCheck{Path: path, Status: "missing"}
		}
		return CacheCheck{Path: path, Status: "corrupt"}
	}
	last, _ := s.lastProjectEventSeq(code)
	if p.LogSeq > last {
		return CacheCheck{Path: path, Status: "corrupt", CacheLogSeq: p.LogSeq, LastEventSeq: last}
	}
	if p.LogSeq < last {
		return CacheCheck{Path: path, Status: "stale", CacheLogSeq: p.LogSeq, LastEventSeq: last}
	}
	return CacheCheck{Path: path, Status: "ok", CacheLogSeq: p.LogSeq, LastEventSeq: last}
}

func (s *Store) checkTaskCache(code, id string, expectedLogSeq int) CacheCheck {
	path := s.taskPath(id)
	var t Task
	if err := ReadJSON(path, &t); err != nil {
		if os.IsNotExist(err) {
			return CacheCheck{Path: path, Status: "missing"}
		}
		return CacheCheck{Path: path, Status: "corrupt"}
	}
	last, _ := s.lastTaskEventSeq(code, id)
	if t.LogSeq > last {
		return CacheCheck{Path: path, Status: "corrupt", CacheLogSeq: t.LogSeq, LastEventSeq: last}
	}
	if t.LogSeq < last {
		return CacheCheck{Path: path, Status: "stale", CacheLogSeq: t.LogSeq, LastEventSeq: last}
	}
	return CacheCheck{Path: path, Status: "ok", CacheLogSeq: t.LogSeq, LastEventSeq: last}
}

func extractTruncatedBytes(err error) int {
	var n int
	var prefix string
	_, _ = fmt.Sscanf(err.Error(), "%s %d bytes", &prefix, &n)
	return n
}
