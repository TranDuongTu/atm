// internal/store/checklist.go
package store

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"atm/internal/core"
)

// checklistQueryExpr matches both record generations: the bare v2 label by
// stored-label lookup, and v1's persona labels via the namespace predicate
// (which is a prefix test on "checklist:" and does NOT match the bare label —
// both terms are required).
const checklistQueryExpr = "checklist OR checklist:*"

const checklistLabelDesc = "checklist records: named standing operating procedures. Managed by `atm checklist`."

// checklistTasks lists candidate tasks once; every verb goes through it.
func (s *Store) checklistTasks(code string) ([]*Task, error) {
	return s.ListTasksErr(core.QueryFilters{Project: code, Expr: checklistQueryExpr})
}

// checklistTitleIs reports whether a task's title identifies it as the named
// record even when its payload is unreadable: v2 titles are the bare name,
// v1 titles are persona/name.
func checklistTitleIs(title, name string) bool {
	return title == name || strings.HasSuffix(title, "/"+name)
}

// findChecklist resolves name -> (task, record). A decode error is returned
// only when the NAMED record's own payload is unreadable; an ambiguous name
// (a v1 legacy: the same name under two personas) errors naming every
// colliding task so the caller can disambiguate.
func (s *Store) findChecklist(code, name string) (*Task, *core.ChecklistRecord, error) {
	tasks, err := s.checklistTasks(code)
	if err != nil {
		return nil, nil, err
	}
	var hits []*Task
	var recs []*core.ChecklistRecord
	var brokenErr error
	for _, t := range tasks {
		rec, err := core.ChecklistFromTask(code, *t)
		if err != nil {
			if checklistTitleIs(t.Title, name) && brokenErr == nil {
				brokenErr = err
			}
			continue
		}
		if rec != nil && rec.Name == name {
			hits = append(hits, t)
			recs = append(recs, rec)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], recs[0], nil
	case 0:
		if brokenErr != nil {
			return nil, nil, brokenErr
		}
		return nil, nil, fmt.Errorf("%w: checklist %s", core.ErrNotFound, name)
	default:
		ids := make([]string, len(hits))
		for i, t := range hits {
			ids[i] = t.ID
		}
		return nil, nil, fmt.Errorf("%w: checklist name %q is ambiguous: tasks %s — disambiguate with `atm checklist remove --name %s --task <ID>` or rename",
			core.ErrUsage, name, strings.Join(ids, ", "), name)
	}
}

// ChecklistRecords lists the project's checklist records of both generations;
// unreadable payloads are skipped here (list degrades) and surface via
// Annotate and by-name lookup.
func (s *Store) ChecklistRecords(code string) ([]core.ChecklistRecord, error) {
	tasks, err := s.checklistTasks(code)
	if err != nil {
		return nil, err
	}
	var out []core.ChecklistRecord
	for _, t := range tasks {
		rec, err := core.ChecklistFromTask(code, *t)
		if err != nil || rec == nil {
			continue
		}
		out = append(out, *rec)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].TaskID < out[j].TaskID
	})
	return out, nil
}

// SuitedChecklists narrows ChecklistRecords to those whose suits name the
// persona. Suitability is a default-bind hint, not ownership.
func (s *Store) SuitedChecklists(code, persona string) ([]core.ChecklistRecord, error) {
	all, err := s.ChecklistRecords(code)
	if err != nil {
		return nil, err
	}
	var out []core.ChecklistRecord
	for _, r := range all {
		for _, suit := range r.Suits {
			if suit == persona {
				out = append(out, r)
				break
			}
		}
	}
	return out, nil
}

// GetChecklist resolves a record by name; core.ErrNotFound when absent, a
// decode error only when the asked-for record's own payload is unreadable,
// an ambiguity error when two legacy records share the name.
func (s *Store) GetChecklist(code, name string) (*core.ChecklistRecord, error) {
	_, rec, err := s.findChecklist(code, name)
	return rec, err
}

// CreateChecklist authors a v2 record: the bare name-keyed label (seeded
// idempotently), a task titled with the name, a fixed one-line description
// (checklist content is not task history), and the v2 payload.
func (s *Store) CreateChecklist(code string, rec core.ChecklistRecord, actor string) (*core.Task, error) {
	if rec.Name == "" || strings.Contains(rec.Name, "/") {
		return nil, fmt.Errorf("%w: checklist needs a name not containing '/'", core.ErrUsage)
	}
	for _, suit := range rec.Suits {
		if suit == "" || strings.Contains(suit, "/") {
			return nil, fmt.Errorf("%w: suits entries must be persona names not containing '/'", core.ErrUsage)
		}
	}
	if len(rec.Steps) == 0 {
		return nil, fmt.Errorf("%w: checklist needs at least one step", core.ErrUsage)
	}
	if rec.Origin == "" {
		rec.Origin = "user"
	}
	if !core.ValidChecklistOrigin(rec.Origin) {
		return nil, fmt.Errorf("%w: origin %q must be user or <profile>@<version>", core.ErrUsage, rec.Origin)
	}
	if _, existing, err := s.findChecklist(code, rec.Name); err == nil {
		return nil, fmt.Errorf("%w: checklist %s already exists (task %s)", core.ErrUsage, rec.Name, existing.TaskID)
	} else if !errors.Is(err, core.ErrNotFound) {
		return nil, err
	}
	label := core.ChecklistLabel(code)
	if err := s.LabelSeed(label, checklistLabelDesc, "", actor); err != nil {
		return nil, err
	}
	desc := fmt.Sprintf("Checklist %q. Managed by `atm checklist`.", rec.Name)
	t, err := s.CreateTask(code, rec.Name, desc, []string{label}, actor)
	if err != nil {
		return nil, err
	}
	payload, err := core.EncodeChecklistPayload(core.ChecklistPayloadFrom(rec))
	if err != nil {
		return nil, err
	}
	if err := s.SetTaskCapabilityMeta(t.ID, core.ChecklistMetaKey, payload, actor); err != nil {
		return nil, err
	}
	return s.GetTask(t.ID)
}

// relabelChecklistV1 moves a v1 task from its persona label to the bare v2
// label; a task already carrying the bare label is left alone (any lingering
// persona label from an interrupted earlier move is still cleaned up).
func (s *Store) relabelChecklistV1(code string, t *Task, actor string) error {
	bare := core.ChecklistLabel(code)
	hasBare, persona := false, ""
	for _, l := range t.Labels {
		if l == bare {
			hasBare = true
		} else if strings.HasPrefix(l, core.ChecklistPersonaLabelPrefix(code)) {
			persona = l
		}
	}
	if !hasBare {
		if err := s.LabelSeed(bare, checklistLabelDesc, "", actor); err != nil {
			return err
		}
		if err := s.TaskLabelAdd(t.ID, bare, actor); err != nil {
			return err
		}
	}
	if persona != "" {
		return s.TaskLabelRemove(t.ID, persona, actor)
	}
	return nil
}

// SetChecklist replaces the named record's content wholesale. Checklists are
// authored outside ATM and imported — ATM is the source of record, not an
// editor — so there is no field merge: the given record IS the record, and a
// field it omits is gone. The task survives (same ID and history); Name and
// Origin are identity and provenance, taken from the EXISTING record and never
// from the caller. A v1 record is relabelled to the bare label here — and
// nowhere else: reads never write.
func (s *Store) SetChecklist(code, name string, rec core.ChecklistRecord, actor string) error {
	t, existing, err := s.findChecklist(code, name)
	if err != nil {
		return err
	}
	if len(rec.Steps) == 0 {
		return fmt.Errorf("%w: a checklist needs at least one step", core.ErrUsage)
	}
	for _, suit := range rec.Suits {
		if suit == "" || strings.Contains(suit, "/") {
			return fmt.Errorf("%w: suits entries must be persona names not containing '/'", core.ErrUsage)
		}
	}
	rec.Name = existing.Name
	rec.Origin = existing.Origin
	if err := s.relabelChecklistV1(code, t, actor); err != nil {
		return err
	}
	payload, err := core.EncodeChecklistPayload(core.ChecklistPayloadFrom(rec))
	if err != nil {
		return err
	}
	return s.SetTaskCapabilityMeta(t.ID, core.ChecklistMetaKey, payload, actor)
}

// RemoveChecklist removes the ledger record. Tolerates an unreadable payload —
// deleting a record does not require understanding it — and accepts a task ID
// to disambiguate legacy same-name collisions.
func (s *Store) RemoveChecklist(code, name, taskID, actor string) error {
	tasks, err := s.checklistTasks(code)
	if err != nil {
		return err
	}
	if taskID != "" {
		for _, t := range tasks {
			if t.ID != taskID {
				continue
			}
			rec, err := core.ChecklistFromTask(code, *t)
			if err == nil && rec != nil && rec.Name == name {
				return s.RemoveTask(t.ID, actor)
			}
			if err != nil && checklistTitleIs(t.Title, name) {
				return s.RemoveTask(t.ID, actor)
			}
			return fmt.Errorf("%w: task %s is not checklist %q", core.ErrUsage, taskID, name)
		}
		return fmt.Errorf("%w: checklist task %s", core.ErrNotFound, taskID)
	}
	// By-name path: healthy records win; a broken record (unreadable payload)
	// is the fallback identity via its title, mirroring findChecklist.
	var hits []string
	broken := ""
	for _, t := range tasks {
		rec, err := core.ChecklistFromTask(code, *t)
		if err != nil {
			if checklistTitleIs(t.Title, name) && broken == "" {
				broken = t.ID
			}
			continue
		}
		if rec != nil && rec.Name == name {
			hits = append(hits, t.ID)
		}
	}
	switch len(hits) {
	case 1:
		return s.RemoveTask(hits[0], actor)
	case 0:
		if broken != "" {
			return s.RemoveTask(broken, actor)
		}
		return fmt.Errorf("%w: checklist %s", core.ErrNotFound, name)
	default:
		return fmt.Errorf("%w: checklist name %q is ambiguous: tasks %s — pass --task <ID>",
			core.ErrUsage, name, strings.Join(hits, ", "))
	}
}
