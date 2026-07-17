package contextmap

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"atm/internal/core"
)

// Finding is one pointer's verdict on one source.
type Finding struct {
	TaskID  string
	Title   string
	Source  Source
	Verdict Verdict
	Detail  string
	AgeDays int // set for AGE findings
}

// Report is the worklist a manager session works from.
type Report struct {
	Drift      []Finding
	Age        []Finding
	Unverified []Finding
	OK         []Finding
	Skipped    []Finding
	New        []string // repo paths changed in git that no pointer claims
	Since      string   // the revision NEW was computed from
}

// Check compares every current context pointer against reality and reports what
// it finds. It is STRICTLY READ-ONLY: it mutates nothing, ever. Deciding what a
// drift means -- and acting on it -- belongs to the manager.
//
// since bounds the NEW-territory scan; when empty it defaults to the HEAD
// recorded on the most recent stamp in the project, so no watermark needs
// storing anywhere.
func Check(s Service, r *Resolver, code, since string) (Report, error) {
	tasks, err := s.ListTasksErr(core.QueryFilters{
		Project: code,
		Labels:  []string{BoardCurrent(code)},
	})
	if err != nil {
		return Report{}, err
	}

	var rep Report
	covered := map[string]bool{}
	newestStampHead := ""
	var newestAt time.Time

	for _, t := range tasks {
		stamp, ok, err := LatestStamp(s, t.ID, code)
		if err != nil {
			return Report{}, err
		}
		if !ok {
			rep.Unverified = append(rep.Unverified, Finding{
				TaskID: t.ID, Title: t.Title, Verdict: "UNVERIFIED",
				Detail: "never stamped",
			})
			continue
		}
		if stamp.At.After(newestAt) {
			newestAt, newestStampHead = stamp.At, stamp.Head
		}
		for _, w := range stamp.Witnesses {
			if w.Source.Kind == KindGit {
				covered[w.Source.Locator] = true
			}
			f := Finding{TaskID: t.ID, Title: t.Title, Source: w.Source}
			verdict, err := r.Compare(w.Source, w.Value)
			if err != nil {
				return Report{}, err
			}
			f.Verdict = verdict
			switch verdict {
			case VerdictOK:
				rep.OK = append(rep.OK, f)
			case VerdictDrift:
				f.Detail = "content changed since verified"
				rep.Drift = append(rep.Drift, f)
			case VerdictGone:
				// Moved or deleted subjects are still the manager's drift
				// worklist -- they need a retarget or a supersede.
				f.Verdict = VerdictDrift
				f.Detail = "path moved or deleted"
				rep.Drift = append(rep.Drift, f)
			case VerdictSkipped:
				f.Detail = "could not witness (offline?)"
				rep.Skipped = append(rep.Skipped, f)
			case VerdictUnwitnessable:
				f.AgeDays = int(time.Since(stamp.At).Hours() / 24)
				f.Detail = fmt.Sprintf("%dd since verified; re-verify by hand", f.AgeDays)
				rep.Age = append(rep.Age, f)
			}
		}
	}

	rep.Since = since
	if rep.Since == "" {
		rep.Since = newestStampHead
	}
	changed, err := r.ChangedSince(rep.Since)
	if err != nil {
		return Report{}, err
	}
	for _, p := range changed {
		if !isCovered(p, covered) {
			rep.New = append(rep.New, p)
		}
	}
	sort.Strings(rep.New)
	return rep, nil
}

// isCovered reports whether a changed path falls under any pointer's git
// source. A pointer at "internal/store" covers "internal/store/task.go". A
// whole-repo pointer at "." (or "") covers everything.
func isCovered(p string, covered map[string]bool) bool {
	if covered["."] || covered[""] {
		return true
	}
	for c := range covered {
		if p == c || strings.HasPrefix(p, c+"/") {
			return true
		}
	}
	return false
}
