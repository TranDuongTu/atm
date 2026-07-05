package store

import (
	"os"
	"path/filepath"
	"sort"
)

type RebuildReport struct {
	Projects int
	Tasks    int
	Labels   int
}

// Rebuild regenerates every cache file from the logs. Walks all project logs,
// replays each, writes the project + every task cache, then regenerates
// labels.json from all label.* events across projects.
func (s *Store) Rebuild() (*RebuildReport, error) {
	rep := &RebuildReport{}
	mergedLabels := map[string]Label{}
	for _, p := range s.ListProjects() {
		st, err := s.Replay(p.Code)
		if err != nil && !IsIntegrity(err) {
			return rep, err
		}
		// Rebuild project cache.
		if st.Project != nil {
			if err := WriteJSON(s.projectPath(p.Code), st.Project); err != nil {
				return rep, err
			}
			rep.Projects++
		}
		// Rebuild task caches. Delete caches for tombstoned tasks.
		live := map[string]bool{}
		for _, t := range st.Tasks {
			if err := WriteJSON(s.taskPath(t.ID), t); err != nil {
				return rep, err
			}
			live[t.ID] = true
			rep.Tasks++
		}
		// Remove orphan task cache files (caches for tasks no longer in the log).
		entries, _ := os.ReadDir(s.tasksDir(p.Code))
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
				continue
			}
			id := e.Name()[:len(e.Name())-len(".json")]
			if !live[id] {
				_ = os.Remove(filepath.Join(s.tasksDir(p.Code), e.Name()))
			}
		}
		// Rebuild comment caches. Delete caches for tombstoned comments.
		liveComments := map[string]bool{}
		for _, c := range st.Comments {
			if err := WriteJSON(s.commentPath(c.ID), c); err != nil {
				return rep, err
			}
			liveComments[c.ID] = true
		}
		commentEntries, _ := os.ReadDir(s.commentsDir(p.Code))
		for _, e := range commentEntries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
				continue
			}
			cid := e.Name()[:len(e.Name())-len(".json")]
			if !liveComments[cid] {
				_ = os.Remove(filepath.Join(s.commentsDir(p.Code), e.Name()))
			}
		}
		// Merge labels.
		for _, l := range st.Labels {
			mergedLabels[l.Name] = l
		}
	}
	// Write the derived labels.json.
	var labels []Label
	for _, l := range mergedLabels {
		labels = append(labels, l)
	}
	sort.Slice(labels, func(i, j int) bool { return labels[i].Name < labels[j].Name })
	if err := WriteJSON(s.labelsPath(), labelsFile{Labels: labels}); err != nil {
		return rep, err
	}
	rep.Labels = len(labels)
	return rep, nil
}
