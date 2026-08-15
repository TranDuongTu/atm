// internal/store/checklist.go
package store

import (
	"fmt"
	"sort"
	"strings"

	"atm/internal/core"
)

// ChecklistRecords lists the project's checklist records; unreadable payloads
// are skipped here (list degrades) and surface via Annotate and by-key lookup.
func (s *Store) ChecklistRecords(code string) ([]core.ChecklistRecord, error) {
	tasks, err := s.ListTasksErr(core.QueryFilters{Project: code, Expr: "checklist:*"})
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
		if out[i].Persona != out[j].Persona {
			return out[i].Persona < out[j].Persona
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// PersonaChecklists narrows ChecklistRecords to one persona.
func (s *Store) PersonaChecklists(code, persona string) ([]core.ChecklistRecord, error) {
	all, err := s.ChecklistRecords(code)
	if err != nil {
		return nil, err
	}
	var out []core.ChecklistRecord
	for _, r := range all {
		if r.Persona == persona {
			out = append(out, r)
		}
	}
	return out, nil
}

// GetChecklist resolves (persona, name); core.ErrNotFound when absent, a
// decode error only when the asked-for record's own payload is unreadable
// (identified by its title, written from persona/name at creation).
func (s *Store) GetChecklist(code, persona, name string) (*core.ChecklistRecord, error) {
	tasks, err := s.ListTasksErr(core.QueryFilters{Project: code, Expr: "checklist:*"})
	if err != nil {
		return nil, err
	}
	title := persona + "/" + name
	for _, t := range tasks {
		rec, err := core.ChecklistFromTask(code, *t)
		if err != nil {
			if t.Title == title {
				return nil, err
			}
			continue
		}
		if rec != nil && rec.Persona == persona && rec.Name == name {
			return rec, nil
		}
	}
	return nil, fmt.Errorf("%w: checklist %s/%s", core.ErrNotFound, persona, name)
}

// CreateChecklist authors the record: persona value label (seeded lazily —
// personas are user-creatable, so the vocabulary cannot enumerate them), a
// task titled persona/name, a fixed one-line description (checklist content
// is not task history), and the payload.
func (s *Store) CreateChecklist(code string, rec core.ChecklistRecord, actor string) (*core.Task, error) {
	if rec.Persona == "" || rec.Name == "" || strings.Contains(rec.Persona, "/") || strings.Contains(rec.Name, "/") {
		return nil, fmt.Errorf("%w: checklist needs a persona and a name, neither containing '/'", core.ErrUsage)
	}
	if len(rec.Steps) == 0 {
		return nil, fmt.Errorf("%w: checklist needs at least one step", core.ErrUsage)
	}
	existing, err := s.ChecklistRecords(code)
	if err != nil {
		return nil, err
	}
	for _, e := range existing {
		if e.Persona == rec.Persona && e.Name == rec.Name {
			return nil, fmt.Errorf("%w: checklist %s/%s already exists (task %s)", core.ErrUsage, rec.Persona, rec.Name, e.TaskID)
		}
	}
	label := core.ChecklistLabel(code, rec.Persona)
	if err := s.LabelSeed(label, "checklists for persona "+rec.Persona, "", actor); err != nil {
		return nil, err
	}
	desc := fmt.Sprintf("Checklist %q for persona %q. Managed by `atm checklist`.", rec.Name, rec.Persona)
	t, err := s.CreateTask(code, rec.Persona+"/"+rec.Name, desc, []string{label}, actor)
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

// EditChecklist updates purpose and/or steps via decode-mutate-encode so
// unknown fields from newer binaries survive. nil purpose / nil steps = keep.
func (s *Store) EditChecklist(code, persona, name string, purpose *string, steps []string, actor string) error {
	rec, err := s.GetChecklist(code, persona, name)
	if err != nil {
		return err
	}
	if purpose == nil && steps == nil {
		return nil
	}
	t, err := s.GetTask(rec.TaskID)
	if err != nil {
		return err
	}
	m, err := core.DecodeChecklistPayload(t.Meta[core.ChecklistMetaKey])
	if err != nil {
		return err
	}
	if purpose != nil {
		if *purpose == "" {
			delete(m, "purpose")
		} else {
			m["purpose"] = *purpose
		}
	}
	if steps != nil {
		if len(steps) == 0 {
			return fmt.Errorf("%w: a checklist needs at least one step", core.ErrUsage)
		}
		arr := make([]any, len(steps))
		for i, st := range steps {
			arr[i] = st
		}
		m["steps"] = arr
	}
	m["persona"], m["name"] = rec.Persona, rec.Name
	enc, err := core.EncodeChecklistPayload(m)
	if err != nil {
		return err
	}
	return s.SetTaskCapabilityMeta(rec.TaskID, core.ChecklistMetaKey, enc, actor)
}

// checklistTaskIDByKey is removal's weaker lookup: a record whose payload is
// unreadable has no knowable key, so the task title (persona/name, written at
// creation) is the fallback identity; healthy records win first.
func (s *Store) checklistTaskIDByKey(code, persona, name string) (string, error) {
	tasks, err := s.ListTasksErr(core.QueryFilters{Project: code, Expr: "checklist:*"})
	if err != nil {
		return "", err
	}
	title := persona + "/" + name
	broken := ""
	for _, t := range tasks {
		rec, err := core.ChecklistFromTask(code, *t)
		if err != nil {
			if t.Title == title && broken == "" {
				broken = t.ID
			}
			continue
		}
		if rec != nil && rec.Persona == persona && rec.Name == name {
			return t.ID, nil
		}
	}
	if broken != "" {
		return broken, nil
	}
	return "", fmt.Errorf("%w: checklist %s/%s", core.ErrNotFound, persona, name)
}

// RemoveChecklist removes the ledger record. Tolerates an unreadable payload —
// deleting a record does not require understanding it.
func (s *Store) RemoveChecklist(code, persona, name, actor string) error {
	id, err := s.checklistTaskIDByKey(code, persona, name)
	if err != nil {
		return err
	}
	return s.RemoveTask(id, actor)
}
