// internal/store/persona_record.go
package store

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"atm/internal/core"
)

const personaLabelDesc = "persona records: the project's operating identities. Managed by `atm persona`."

// personaTasks lists candidate tasks once; every verb goes through it.
func (s *Store) personaTasks(code string) ([]*Task, error) {
	return s.ListTasksErr(core.QueryFilters{Project: code, Expr: "persona"})
}

// findPersonaRecord resolves name -> (task, record). A decode error is
// returned only when the NAMED record's own payload is unreadable.
func (s *Store) findPersonaRecord(code, name string) (*Task, *core.Persona, error) {
	tasks, err := s.personaTasks(code)
	if err != nil {
		return nil, nil, err
	}
	var hits []*Task
	var recs []*core.Persona
	var brokenErr error
	for _, t := range tasks {
		rec, err := core.PersonaFromTask(code, *t)
		if err != nil {
			if t.Title == name && brokenErr == nil {
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
		return nil, nil, fmt.Errorf("%w: persona %s", core.ErrNotFound, name)
	default:
		ids := make([]string, len(hits))
		for i, t := range hits {
			ids[i] = t.ID
		}
		return nil, nil, fmt.Errorf("%w: persona name %q is ambiguous: tasks %s — disambiguate with `atm persona remove --name %s --task <ID>`",
			core.ErrUsage, name, strings.Join(ids, ", "), name)
	}
}

// PersonaRecords lists the project's persona records, name-sorted.
// Unreadable payloads are skipped: a list degrades rather than failing whole.
func (s *Store) PersonaRecords(code string) ([]core.Persona, error) {
	tasks, err := s.personaTasks(code)
	if err != nil {
		return nil, err
	}
	var out []core.Persona
	for _, t := range tasks {
		rec, err := core.PersonaFromTask(code, *t)
		if err != nil || rec == nil {
			continue
		}
		out = append(out, *rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// GetPersonaRecord returns one project persona record.
func (s *Store) GetPersonaRecord(code, name string) (*core.Persona, error) {
	_, rec, err := s.findPersonaRecord(code, name)
	return rec, err
}

// SetPersonaRecord imports a persona document as the project's record,
// creating it or replacing it wholesale.
//
// There is no field merge and no editor: the document IS the record, so a
// field it omits is gone (spec decision 11). On replace the task survives —
// same ID, same history — and NAME and ORIGIN are taken from the existing
// record, never from the caller: reworded text does not change what a
// persona is, and it must not erase where it came from, or reset would have
// nothing to restore.
func (s *Store) SetPersonaRecord(code string, p core.Persona, actor string) (*core.Task, error) {
	if p.Name == "" || strings.Contains(p.Name, "/") {
		return nil, fmt.Errorf("%w: persona needs a name not containing '/'", core.ErrUsage)
	}
	if strings.TrimSpace(p.Prompt) == "" {
		return nil, fmt.Errorf("%w: persona %s needs a prompt — an identity with no text says nothing", core.ErrUsage, p.Name)
	}
	if p.Origin == "" {
		p.Origin = "user"
	}
	if _, err := core.ParseOrigin(p.Origin); err != nil {
		return nil, fmt.Errorf("%w: %v", core.ErrUsage, err)
	}

	t, existing, err := s.findPersonaRecord(code, p.Name)
	switch {
	case err == nil:
		p.Name, p.Origin = existing.Name, existing.Origin
		if err := s.SetDescription(t.ID, strings.TrimSpace(p.Prompt), actor); err != nil {
			return nil, err
		}
		if err := s.writePersonaPayload(t.ID, p, actor); err != nil {
			return nil, err
		}
		return s.GetTask(t.ID)
	case errors.Is(err, core.ErrNotFound):
	default:
		return nil, err
	}

	label := core.PersonaLabel(code)
	if err := s.LabelSeed(label, personaLabelDesc, "", actor); err != nil {
		return nil, err
	}
	created, err := s.CreateTask(code, p.Name, strings.TrimSpace(p.Prompt), []string{label}, actor)
	if err != nil {
		return nil, err
	}
	if err := s.writePersonaPayload(created.ID, p, actor); err != nil {
		return nil, err
	}
	return s.GetTask(created.ID)
}

func (s *Store) writePersonaPayload(taskID string, p core.Persona, actor string) error {
	payload, err := core.EncodePersonaPayload(core.PersonaPayloadFrom(p))
	if err != nil {
		return err
	}
	return s.SetTaskCapabilityMeta(taskID, core.PersonaMetaKey, payload, actor)
}

// setPersonaOrigin restamps provenance. Only apply and reset have any
// business calling it, which is why it is unexported.
func (s *Store) setPersonaOrigin(taskID, origin, actor string) error {
	t, err := s.GetTask(taskID)
	if err != nil {
		return err
	}
	m, err := core.DecodePersonaPayload(t.Meta[core.PersonaMetaKey])
	if err != nil {
		return err
	}
	m["origin"] = origin
	payload, err := core.EncodePersonaPayload(m)
	if err != nil {
		return err
	}
	return s.SetTaskCapabilityMeta(taskID, core.PersonaMetaKey, payload, actor)
}

// ResetPersonaRecord restores a record from the profile version it came
// from, discarding local edits. It resolves against the record's OWN
// origin version, not the newest installed one: reset means "back to what I
// was given", and quietly upgrading someone during a restore would be a
// different operation wearing the same name.
func (s *Store) ResetPersonaRecord(code, name, actor string) (*core.Persona, error) {
	_, rec, err := s.findPersonaRecord(code, name)
	if err != nil {
		return nil, err
	}
	o, err := core.ParseOrigin(rec.Origin)
	if err != nil {
		return nil, fmt.Errorf("%w: persona %s: %v", core.ErrUsage, name, err)
	}
	switch o.Kind {
	case core.OriginUser:
		return nil, fmt.Errorf("%w: persona %s has origin user — the project authored it, so there is nothing to restore from", core.ErrUsage, name)
	case core.OriginLegacy:
		return nil, fmt.Errorf("%w: persona %s has the pre-profile origin %q, which names no profile version to restore from — set it from a document instead", core.ErrUsage, name, rec.Origin)
	}
	p, _, err := s.GetProfile(o.Profile, o.Version)
	if err != nil {
		return nil, fmt.Errorf("%w: persona %s came from %s, which is not installed here (install it, or set the persona from a document): %v", core.ErrUsage, name, o.Ref(), err)
	}
	doc, ok := p.ForProject(code).ProfilePersona(name)
	if !ok {
		return nil, fmt.Errorf("%w: profile %s no longer ships a persona named %s", core.ErrUsage, o.Ref(), name)
	}
	if _, err := s.SetPersonaRecord(code, doc, actor); err != nil {
		return nil, err
	}
	return s.GetPersonaRecord(code, name)
}

// RemovePersonaRecord removes the ledger record. It tolerates an unreadable
// payload — deleting a record does not require understanding it — and takes
// a task ID to disambiguate a same-name collision.
func (s *Store) RemovePersonaRecord(code, name, taskID, actor string) error {
	if taskID != "" {
		return s.RemoveTask(taskID, actor)
	}
	t, _, err := s.findPersonaRecord(code, name)
	if err != nil {
		return err
	}
	return s.RemoveTask(t.ID, actor)
}

// personaDocumentOf renders a record back to the document form `persona set`
// accepts, so a round trip through the CLI is lossless.
func personaDocumentOf(p core.Persona) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", p.Name)
	fmt.Fprintf(&b, "description: %s\n", strings.TrimSpace(strings.ReplaceAll(p.Description, "\n", " ")))
	b.WriteString("---\n")
	b.WriteString(strings.TrimSpace(p.Prompt))
	b.WriteString("\n")
	return b.String()
}
