package eventlog

import (
	"bytes"
	"os"

	"atm/internal/core"
)

// StoreStats sums event-log size and line count for one project, or for
// every project on disk when project is "". Version is derived across ALL
// projects either way: the storage format is a property of the store, not
// of the slice being counted, so it stays put as the user switches scope.
//
// This is a read-only, advisory display path: no locks are taken (a torn
// read is corrected on the next refresh), and a missing log file
// contributes zero. Committed events are newline-terminated lines, so
// counting '\n' bytes never counts an uncommitted partial tail.
func (e *Engine) StoreStats(project string) (core.StoreStats, error) {
	var st core.StoreStats
	codes, err := e.ProjectCodesOnDisk()
	if err != nil {
		return st, err
	}
	formats := map[StoreFormat]bool{}
	for _, code := range codes {
		f, err := e.ProjectFormat(code)
		if err != nil {
			return st, err
		}
		formats[f] = true
		if project != "" && code != project {
			continue
		}
		path := e.LogPath(code)
		if f == StoreFormatV2 {
			path = e.EventsV2Path(code)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return st, err
		}
		st.SizeBytes += int64(len(raw))
		st.EventCount += bytes.Count(raw, []byte{'\n'})
	}
	switch len(formats) {
	case 0:
		m, err := e.ReadStoreMeta()
		if err != nil {
			return st, err
		}
		st.Version = string(m.ActiveFormat)
	case 1:
		for f := range formats {
			st.Version = string(f)
		}
	default:
		st.Version = "mixed"
	}
	return st, nil
}
