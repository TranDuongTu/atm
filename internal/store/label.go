package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"atm/internal/seed"
)

type labelsFile struct {
	Labels []Label `json:"labels"`
}

type LabelRemoveResult struct {
	RetainedUsage int `json:"retained_usage"`
}

func (s *Store) LabelAdd(name, description, actor string) error {
	if err := ValidateLabelName(name); err != nil {
		return err
	}
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	if err := s.labelProjectExists(name); err != nil {
		return err
	}
	return s.WithLock(labelProject(name), func() error {
		lf, err := s.loadLabels()
		if err != nil {
			return err
		}
		for i, l := range lf.Labels {
			if l.Name == name {
				if description != "" && l.Description != description {
					lf.Labels[i].Description = description
				}
				return s.writeLabels(lf)
			}
		}
		lf.Labels = append(lf.Labels, Label{Name: name, Description: description})
		sort.SliceStable(lf.Labels, func(i, j int) bool { return lf.Labels[i].Name < lf.Labels[j].Name })
		return s.writeLabels(lf)
	})
}

// LabelSeed upserts a label but only sets the description when the label
// is newly created. Existing labels keep their descriptions — this
// preserves human edits when SeedLabels re-applies the default set. Used
// by SeedLabels (project create + on-demand seed). Contrast with
// LabelAdd, which overwrites the description when the new one is
// non-empty and differs.
func (s *Store) LabelSeed(name, description, actor string) error {
	if err := ValidateLabelName(name); err != nil {
		return err
	}
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	if err := s.labelProjectExists(name); err != nil {
		return err
	}
	return s.WithLock(labelProject(name), func() error {
		lf, err := s.loadLabels()
		if err != nil {
			return err
		}
		for _, l := range lf.Labels {
			if l.Name == name {
				// Exists: preserve the existing description (no-op).
				return nil
			}
		}
		lf.Labels = append(lf.Labels, Label{Name: name, Description: description})
		sort.SliceStable(lf.Labels, func(i, j int) bool { return lf.Labels[i].Name < lf.Labels[j].Name })
		return s.writeLabels(lf)
	})
}

// SeedLabels applies the default seed labels (internal/seed.Labels) to the
// project. Idempotent — preserves existing descriptions (via LabelSeed).
// Called by the CLI/TUI on-demand seed path. (CreateProject seeds via
// seedLabelsLocked inside its own lock instead.)
func (s *Store) SeedLabels(code, actor string) error {
	for _, l := range seed.Labels {
		full := code + ":" + l.Suffix
		if err := s.LabelSeed(full, l.Description, actor); err != nil {
			return err
		}
	}
	return nil
}

// seedLabelsLocked appends label.upserted events for each default label not
// already in the log. Caller MUST hold the project lock. CreateProject calls
// this from inside its WithLock callback so the log epoch + label seeds land
// atomically under one lock acquisition. Unlike LabelSeed, this does NOT
// preserve existing descriptions — at create time the log is empty.
func (s *Store) seedLabelsLocked(code, actor string, at time.Time) error {
	for _, l := range seed.Labels {
		full := code + ":" + l.Suffix
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      at,
			Actor:   actor,
			Action:  ActionLabelUpserted,
			Subject: Subject{Kind: "label", Name: full},
			Payload: mustMarshal(Label{Name: full, Description: l.Description}),
		})
		if err != nil {
			return err
		}
		// Update per-label cache stamp on the global labels.json — minimal
		// patch here; full labels.json derivation comes in Task 5. For now
		// the derived file is written by refreshDerivedLabelsLocked.
		_ = entry
	}
	return s.refreshDerivedLabelsLocked(code)
}

// refreshDerivedLabelsLocked regenerates labels.json from label.* events in
// this project's log. Merges with other projects' labels so a single project
// create does not wipe the global registry. Minimal implementation here —
// Task 5 will fold this into the full labels.json rewrite.
func (s *Store) refreshDerivedLabelsLocked(code string) error {
	st, err := s.Replay(code)
	if err != nil {
		return err
	}
	all := s.LabelList("", "")
	merged := map[string]Label{}
	for _, l := range all {
		merged[l.Name] = l
	}
	for _, l := range st.Labels {
		merged[l.Name] = l
	}
	var out []Label
	for _, l := range merged {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return WriteJSON(s.labelsPath(), labelsFile{Labels: out})
}

func (s *Store) LabelRemove(name, actor string) (*LabelRemoveResult, error) {
	if err := ValidateLabelName(name); err != nil {
		return nil, err
	}
	if actor == "" {
		return nil, fmt.Errorf("%w: actor is required", ErrUsage)
	}
	var result *LabelRemoveResult
	err := s.WithLock(labelProject(name), func() error {
		lf, err := s.loadLabels()
		if err != nil {
			return err
		}
		idx := -1
		for i, l := range lf.Labels {
			if l.Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("%w: label %q", ErrNotFound, name)
		}
		lf.Labels = append(lf.Labels[:idx], lf.Labels[idx+1:]...)
		if err := s.writeLabels(lf); err != nil {
			return err
		}
		count, err := s.countTasksWithLabelGlobally(name)
		if err != nil {
			return err
		}
		result = &LabelRemoveResult{RetainedUsage: count}
		return nil
	})
	return result, err
}

func (s *Store) LabelList(project, namespace string) []Label {
	lf, err := s.loadLabels()
	if err != nil {
		return nil
	}
	var out []Label
	for _, l := range lf.Labels {
		if project != "" && !strings.HasPrefix(l.Name, project+":") {
			continue
		}
		if namespace != "" && !strings.HasPrefix(l.Name, project+":"+namespace+":") {
			continue
		}
		out = append(out, l)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Store) LabelShow(name string) (Label, error) {
	lf, err := s.loadLabels()
	if err != nil {
		return Label{}, err
	}
	for _, l := range lf.Labels {
		if l.Name == name {
			return l, nil
		}
	}
	return Label{}, fmt.Errorf("%w: label %q", ErrNotFound, name)
}

func (s *Store) Namespaces(code string) []string {
	lf, err := s.loadLabels()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	prefix := code + ":"
	for _, l := range lf.Labels {
		if !strings.HasPrefix(l.Name, prefix) {
			continue
		}
		rest := strings.TrimPrefix(l.Name, prefix)
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) == 2 {
			ns := parts[0]
			if !seen[ns] {
				seen[ns] = true
				out = append(out, ns)
			}
		}
	}
	sort.Strings(out)
	return out
}

// autoRegisterLabels upserts each label into the registry (called by CreateTask/TaskLabelAdd).
func (s *Store) autoRegisterLabels(labels []string) error {
	if len(labels) == 0 {
		return nil
	}
	lf, err := s.loadLabels()
	if err != nil {
		return err
	}
	changed := false
	existing := map[string]bool{}
	for _, l := range lf.Labels {
		existing[l.Name] = true
	}
	for _, name := range labels {
		if existing[name] {
			continue
		}
		lf.Labels = append(lf.Labels, Label{Name: name})
		existing[name] = true
		changed = true
	}
	if !changed {
		return nil
	}
	sort.SliceStable(lf.Labels, func(i, j int) bool { return lf.Labels[i].Name < lf.Labels[j].Name })
	return s.writeLabels(lf)
}

func (s *Store) labelProjectExists(name string) error {
	code := labelProject(name)
	if _, err := s.GetProject(code); err != nil {
		return fmt.Errorf("%w: project %q for label %q does not exist", ErrUsage, code, name)
	}
	return nil
}

func labelProject(name string) string {
	return strings.SplitN(name, ":", 2)[0]
}

func (s *Store) loadLabels() (*labelsFile, error) {
	var lf labelsFile
	if _, err := os.Stat(s.labelsPath()); os.IsNotExist(err) {
		return &labelsFile{Labels: []Label{}}, nil
	}
	if err := ReadJSON(s.labelsPath(), &lf); err != nil {
		return nil, err
	}
	if lf.Labels == nil {
		lf.Labels = []Label{}
	}
	return &lf, nil
}

func (s *Store) writeLabels(lf *labelsFile) error {
	if lf.Labels == nil {
		lf.Labels = []Label{}
	}
	return WriteJSON(s.labelsPath(), lf)
}

func (s *Store) countTasksWithLabelGlobally(label string) (int, error) {
	count := 0
	for _, p := range s.ListProjects() {
		entries, err := os.ReadDir(s.tasksDir(p.Code))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, err
		}
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
				continue
			}
			var t Task
			if err := ReadJSON(filepath.Join(s.tasksDir(p.Code), e.Name()), &t); err != nil {
				continue
			}
			for _, l := range t.Labels {
				if l == label {
					count++
					break
				}
			}
		}
	}
	return count, nil
}

// LabelUsage counts tasks in the given project carrying the label. Exported
// for the TUI's project-detail reconciliation surface (Screen 4: "(N tasks)"
// suffix per label).
func (s *Store) LabelUsage(projectCode, label string) (int, error) {
	count := 0
	for _, id := range s.listTaskIDs(projectCode) {
		t, err := s.GetTask(id)
		if err != nil {
			continue
		}
		for _, l := range t.Labels {
			if l == label {
				count++
				break
			}
		}
	}
	return count, nil
}
