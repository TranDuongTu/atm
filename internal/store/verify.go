package store

import (
	"database/sql"
	"fmt"
	"sort"

	"atm/internal/eventsource"
)

type VerifyReport struct {
	Project       string
	LogEntries    int
	LogOK         bool
	Truncated     int
	SeqGaps       []int
	Caches        []CacheCheck
	Diverged      bool
	VectorIndexes []VectorIndexInfo `json:"vector_indexes,omitempty"`
	InquiryCount  int               `json:"inquiry_count"`
	Format        StoreFormat       `json:"format"`
	V2Events      int               `json:"v2_events,omitempty"`
	V2FileOK      bool              `json:"v2_file_ok,omitempty"`
}

type CacheCheck struct {
	Kind         string // "project" | "task" | "comment"
	ID           string // project code | task id | comment id
	Status       string // "ok" | "stale" | "missing" | "corrupt"
	CacheLogSeq  int
	LastEventSeq int
}

type VectorIndexInfo struct {
	Model      string `json:"model"`
	Count      int    `json:"count"`
	LastLogSeq int    `json:"last_log_seq"`
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
	format, err := s.projectFormat(code)
	if err != nil {
		return nil, err
	}
	report := &VerifyReport{Project: code, LogOK: true, Format: format}
	if format == StoreFormatV2 {
		defer s.populateAuxReports(code, report)
		snap, err := s.verifyV2File(code)
		if err != nil {
			if !IsIntegrity(err) {
				return nil, err
			}
			report.LogOK = false
			report.Diverged = true
			return report, nil
		}
		report.V2FileOK = true
		report.V2Events = snap.EventCount
		report.LogEntries = snap.EventCount
		state, err := eventsource.FoldEvents(snap.Events)
		if err != nil {
			if !IsIntegrity(err) {
				return nil, err
			}
			report.LogOK = false
			report.Diverged = true
			return report, nil
		}
		report.Caches = append(report.Caches, s.checkV2Cache(code, state, snap.EventCount)...)
		for _, c := range report.Caches {
			if c.Status != "ok" {
				report.Diverged = true
			}
		}
		return report, nil
	}
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
	s.populateAuxReports(code, report)
	return report, nil
}

// populateAuxReports fills the format-independent report tail: vector index
// info and inquiry counts. Shared by the v1 and v2 verify paths so the v2
// branch cannot silently drop them.
func (s *Store) populateAuxReports(code string, report *VerifyReport) {
	if models, err := s.ListVectorModels(code); err == nil {
		for _, slug := range models {
			info := VectorIndexInfo{Model: slug}
			if meta, _ := s.VectorMeta(code, slug); meta != nil {
				info.Count = meta.Count
				info.LastLogSeq = meta.LastLogSeq
			}
			report.VectorIndexes = append(report.VectorIndexes, info)
		}
	}
	if inq, _ := s.ReadInquiries(code); inq != nil {
		report.InquiryCount = len(inq)
	}
}

// checkV2Cache compares the v2 freshness meta row against the fold's event
// count. Unlike the v1 per-entity checks, there is a single freshness key
// for the whole project: cacheProjectFromV2State always projects the entire
// live set from one fold, so there is no per-task/per-comment staleness to
// distinguish.
func (s *Store) checkV2Cache(code string, st *eventsource.State, eventCount int) []CacheCheck {
	db, err := s.cacheDB()
	if err != nil {
		return []CacheCheck{{Kind: "project", ID: code, Status: "corrupt"}}
	}
	if got, ok, err := cacheGetV2Freshness(db, code); err != nil {
		return []CacheCheck{{Kind: "project", ID: code, Status: "corrupt"}}
	} else if !ok {
		return []CacheCheck{{Kind: "project", ID: code, Status: "missing", LastEventSeq: eventCount}}
	} else if got != eventCount {
		return []CacheCheck{{Kind: "project", ID: code, Status: "stale", CacheLogSeq: got, LastEventSeq: eventCount}}
	}
	return []CacheCheck{{Kind: "project", ID: code, Status: "ok", CacheLogSeq: eventCount, LastEventSeq: eventCount}}
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
