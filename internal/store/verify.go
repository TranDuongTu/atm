package store

import (
	"sort"
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
	if format != StoreFormatV2 {
		return report, nil
	}
	defer s.populateAuxReports(code, report)
	// Snapshot folds the file strictly; the two old failure branches (file read
	// vs fold) collapse into one — both set LogOK=false / Diverged=true.
	snap, err := s.eng.Snapshot(code)
	if err != nil {
		if !IsIntegrity(err) {
			return nil, err
		}
		report.LogOK = false
		report.Diverged = true
		return report, nil
	}
	report.V2FileOK = true
	report.V2Events = snap.ChangeCount
	report.LogEntries = snap.ChangeCount
	report.Caches = append(report.Caches, s.checkV2Cache(code, snap.ChangeCount)...)
	for _, c := range report.Caches {
		if c.Status != "ok" {
			report.Diverged = true
		}
	}
	return report, nil
}

// populateAuxReports fills the format-independent report tail: vector index
// info and inquiry counts.
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
// count. There is a single freshness key for the whole project:
// cacheProjectFromV2State always projects the entire live set from one fold,
// so there is no per-task/per-comment staleness to distinguish.
func (s *Store) checkV2Cache(code string, eventCount int) []CacheCheck {
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
