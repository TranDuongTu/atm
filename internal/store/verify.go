package store

import (
	"database/sql"
	"fmt"
	"sort"
)

type VerifyReport struct {
	Project    string
	LogEntries int
	LogOK      bool
	Truncated  int
	SeqGaps    []int
	Caches     []CacheCheck
	Diverged   bool
}

type CacheCheck struct {
	Kind         string // "project" | "task" | "comment"
	ID           string // project code | task id | comment id
	Status       string // "ok" | "stale" | "missing" | "corrupt"
	CacheLogSeq  int
	LastEventSeq int
}

func (s *Store) Verify() ([]VerifyReport, error) {
	codes, err := s.projectCodesOnDisk()
	if err != nil {
		return nil, err
	}
	var out []VerifyReport
	for _, code := range codes {
		r, err := s.VerifyProject(code)
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
	last := 0
	for _, e := range entries {
		if e.Seq != last+1 {
			report.SeqGaps = append(report.SeqGaps, last+1)
			report.LogOK = false
		}
		last = e.Seq
	}
	st, _ := s.Replay(code)
	db, err := s.cacheDB()
	if err != nil {
		return nil, err
	}
	report.Caches = append(report.Caches, s.checkProjectCache(db, code, st))
	for _, t := range st.Tasks {
		report.Caches = append(report.Caches, s.checkTaskCache(db, code, t.ID))
	}
	for _, c := range st.Comments {
		report.Caches = append(report.Caches, s.checkCommentCache(db, code, c.ID))
	}
	cachedIDs, err := cacheListCommentIDsForProject(db, code)
	if err != nil {
		return nil, err
	}
	liveComments := map[string]bool{}
	for _, c := range st.Comments {
		liveComments[c.ID] = true
	}
	for _, id := range cachedIDs {
		if !liveComments[id] {
			report.Caches = append(report.Caches, CacheCheck{Kind: "comment", ID: id, Status: "corrupt"})
			report.Diverged = true
		}
	}
	for _, c := range report.Caches {
		if c.Status != "ok" {
			report.Diverged = true
		}
	}
	return report, nil
}

func (s *Store) checkProjectCache(db *sql.DB, code string, st *ReplayState) CacheCheck {
	p, ok, err := cacheGetProject(db, code)
	if err != nil {
		return CacheCheck{Kind: "project", ID: code, Status: "corrupt"}
	}
	if !ok {
		return CacheCheck{Kind: "project", ID: code, Status: "missing"}
	}
	last, _ := s.lastProjectEventSeq(code)
	if p.LogSeq > last {
		return CacheCheck{Kind: "project", ID: code, Status: "corrupt", CacheLogSeq: p.LogSeq, LastEventSeq: last}
	}
	if p.LogSeq < last {
		return CacheCheck{Kind: "project", ID: code, Status: "stale", CacheLogSeq: p.LogSeq, LastEventSeq: last}
	}
	return CacheCheck{Kind: "project", ID: code, Status: "ok", CacheLogSeq: p.LogSeq, LastEventSeq: last}
}

func (s *Store) checkTaskCache(db *sql.DB, code, id string) CacheCheck {
	t, ok, err := cacheGetTask(db, id)
	if err != nil {
		return CacheCheck{Kind: "task", ID: id, Status: "corrupt"}
	}
	if !ok {
		return CacheCheck{Kind: "task", ID: id, Status: "missing"}
	}
	last, _ := s.lastTaskEventSeq(code, id)
	if t.LogSeq > last {
		return CacheCheck{Kind: "task", ID: id, Status: "corrupt", CacheLogSeq: t.LogSeq, LastEventSeq: last}
	}
	if t.LogSeq < last {
		return CacheCheck{Kind: "task", ID: id, Status: "stale", CacheLogSeq: t.LogSeq, LastEventSeq: last}
	}
	return CacheCheck{Kind: "task", ID: id, Status: "ok", CacheLogSeq: t.LogSeq, LastEventSeq: last}
}

func (s *Store) checkCommentCache(db *sql.DB, code, id string) CacheCheck {
	c, ok, err := cacheGetComment(db, id)
	if err != nil {
		return CacheCheck{Kind: "comment", ID: id, Status: "corrupt"}
	}
	if !ok {
		return CacheCheck{Kind: "comment", ID: id, Status: "missing"}
	}
	last, _ := s.lastCommentEventSeq(code, id)
	if c.LogSeq > last {
		return CacheCheck{Kind: "comment", ID: id, Status: "corrupt", CacheLogSeq: c.LogSeq, LastEventSeq: last}
	}
	if c.LogSeq < last {
		return CacheCheck{Kind: "comment", ID: id, Status: "stale", CacheLogSeq: c.LogSeq, LastEventSeq: last}
	}
	return CacheCheck{Kind: "comment", ID: id, Status: "ok", CacheLogSeq: c.LogSeq, LastEventSeq: last}
}

func extractTruncatedBytes(err error) int {
	var n int
	var prefix string
	_, _ = fmt.Sscanf(err.Error(), "%s %d bytes", &prefix, &n)
	return n
}
