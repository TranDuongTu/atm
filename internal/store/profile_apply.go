package store

import (
	"errors"
	"fmt"

	"atm/internal/core"
	"atm/internal/profile"
)

// currentOf gathers what the project already holds, the input apply and
// the readiness computation compare a profile against.
func (s *Store) currentOf(code string) (profile.Current, error) {
	p, err := s.GetProject(code)
	if err != nil {
		return profile.Current{}, err
	}
	cur := profile.Current{Enabled: p.Capabilities}
	if cur.Personas, err = s.PersonaRecords(code); err != nil {
		return cur, err
	}
	if cur.Checklists, err = s.ChecklistRecords(code); err != nil {
		return cur, err
	}
	if cur.Channels, err = s.ChannelRecords(code); err != nil {
		return cur, err
	}
	return cur, nil
}

// previousProfileFor resolves a record's origin ref to that version of the
// profile, substituted for the project — nil when it is not available
// here. It is how apply tells a local edit from an upstream change.
func (s *Store) previousProfileFor(code string) func(ref string) *core.Profile {
	return func(ref string) *core.Profile {
		o, err := core.ParseOrigin(ref)
		if err != nil || o.Kind != core.OriginProfile {
			return nil
		}
		p, _, err := s.GetProfile(o.Profile, o.Version)
		if err != nil {
			return nil
		}
		return p.ForProject(code)
	}
}

// PlanProfile reports what ApplyProfile would do, writing nothing.
func (s *Store) PlanProfile(code string, p *core.Profile) (*core.ApplyPlan, error) {
	cur, err := s.currentOf(code)
	if err != nil {
		return nil, err
	}
	return profile.PlanApply(p.ForProject(code), cur, s.previousProfileFor(code)), nil
}

// ApplyProfile turns a profile's documents into the project's records.
// Every write is one ordinary record verb, so a partial failure leaves
// ordinary records behind, never a half-state a later apply cannot read.
func (s *Store) ApplyProfile(code string, p *core.Profile, force bool, actor string) (*core.ApplyPlan, error) {
	if err := s.validateActor(actor); err != nil {
		return nil, err
	}
	cur, err := s.currentOf(code)
	if err != nil {
		return nil, err
	}
	sub := p.ForProject(code)
	plan := profile.PlanApply(sub, cur, s.previousProfileFor(code))
	origin := sub.Origin().String()
	for i := range plan.Items {
		it := &plan.Items[i]
		write := false
		switch it.State {
		case core.ApplyCreate, core.ApplyUpdate:
			write = true
		case core.ApplyConflict:
			if !force {
				continue
			}
			it.Forced = true
			write = true
		case core.ApplyInSync:
			if !it.Restamp {
				continue
			}
		}
		var err error
		switch it.Kind {
		case core.ApplyKindPersona:
			doc, _ := sub.ProfilePersona(it.Name)
			err = s.applyPersona(code, doc, origin, write, actor)
		case core.ApplyKindChecklist:
			doc, _ := sub.ProfileChecklist(it.Name)
			err = s.applyChecklist(code, doc, origin, write, actor)
		case core.ApplyKindChannel:
			doc, _ := sub.ProfileChannel(it.Name)
			err = s.applyChannel(code, doc, origin, write, actor)
		}
		if err != nil {
			return plan, fmt.Errorf("apply %s %s: %w", it.Kind, it.Name, err)
		}
	}
	return plan, nil
}

func (s *Store) applyPersona(code string, doc core.Persona, origin string, write bool, actor string) error {
	t, existing, err := s.findPersonaRecord(code, doc.Name)
	switch {
	case errors.Is(err, core.ErrNotFound):
		doc.Origin = origin
		_, err := s.SetPersonaRecord(code, doc, actor)
		return err
	case err != nil:
		return err
	}
	if write {
		if _, err := s.SetPersonaRecord(code, doc, actor); err != nil {
			return err
		}
	}
	if existing.Origin != origin {
		return s.setPersonaOrigin(t.ID, origin, actor)
	}
	return nil
}

func (s *Store) applyChecklist(code string, doc core.ChecklistRecord, origin string, write bool, actor string) error {
	t, existing, err := s.findChecklist(code, doc.Name)
	switch {
	case errors.Is(err, core.ErrNotFound):
		doc.Origin = origin
		_, err := s.CreateChecklist(code, doc, actor)
		return err
	case err != nil:
		return err
	}
	if write {
		if err := s.SetChecklist(code, doc.Name, doc, actor); err != nil {
			return err
		}
	}
	if existing.Origin != origin {
		return s.setChecklistOrigin(t.ID, origin, actor)
	}
	return nil
}

func (s *Store) applyChannel(code string, doc core.ChannelRecord, origin string, write bool, actor string) error {
	rec, err := s.channelByName(code, doc.Name)
	switch {
	case errors.Is(err, core.ErrNotFound):
		doc.Origin = origin
		_, err := s.CreateChannel(code, doc, actor)
		return err
	case err != nil:
		return err
	}
	if write {
		if err := s.SetDescription(rec.TaskID, doc.Purpose, actor); err != nil {
			return err
		}
	}
	if write || rec.Origin != origin {
		return s.setChannelExpectation(rec, doc.RoleHint, origin, actor)
	}
	return nil
}

// setChecklistOrigin restamps provenance. Only apply and reset have any
// business calling it, which is why it is unexported.
func (s *Store) setChecklistOrigin(taskID, origin, actor string) error {
	t, err := s.GetTask(taskID)
	if err != nil {
		return err
	}
	m, err := core.DecodeChecklistPayload(t.Meta[core.ChecklistMetaKey])
	if err != nil {
		return err
	}
	m["origin"] = origin
	payload, err := core.EncodeChecklistPayload(m)
	if err != nil {
		return err
	}
	return s.SetTaskCapabilityMeta(taskID, core.ChecklistMetaKey, payload, actor)
}

// setChannelExpectation writes the profile-owned half of a channel record —
// role hint and origin — leaving the endpoints exactly as they are.
func (s *Store) setChannelExpectation(rec *core.ChannelRecord, roleHint, origin, actor string) error {
	t, err := s.GetTask(rec.TaskID)
	if err != nil {
		return err
	}
	m, err := core.DecodeChannelPayload(t.Meta[core.ChannelMetaKey])
	if err != nil {
		return err
	}
	if roleHint == "" || roleHint == core.ChannelRoleHome {
		delete(m, "role_hint")
	} else {
		m["role_hint"] = roleHint
	}
	if origin == "" || origin == "user" {
		delete(m, "origin")
	} else {
		m["origin"] = origin
	}
	payload, err := core.EncodeChannelPayload(m)
	if err != nil {
		return err
	}
	return s.SetTaskCapabilityMeta(rec.TaskID, core.ChannelMetaKey, payload, actor)
}

// resetSource resolves what a record restores from: its OWN origin version,
// never the newest installed one. Reset means "back to what I was given";
// quietly upgrading someone during a restore would be a different
// operation wearing the same name. The three refusals each name their
// reason.
func (s *Store) resetSource(code, kind, name, origin string) (*core.Profile, error) {
	o, err := core.ParseOrigin(origin)
	if err != nil {
		return nil, fmt.Errorf("%w: %s %s: %v", core.ErrUsage, kind, name, err)
	}
	switch o.Kind {
	case core.OriginUser:
		return nil, fmt.Errorf("%w: %s %s has origin user — the project authored it, so there is nothing to restore from", core.ErrUsage, kind, name)
	case core.OriginLegacy:
		return nil, fmt.Errorf("%w: %s %s has the pre-profile origin %q, which names no profile version to restore from", core.ErrUsage, kind, name, origin)
	}
	p, _, err := s.GetProfile(o.Profile, o.Version)
	if err != nil {
		return nil, fmt.Errorf("%w: %s %s came from %s, which is not installed here (install that version, or re-apply the profile): %v", core.ErrUsage, kind, name, o.Ref(), err)
	}
	return p.ForProject(code), nil
}

// ResetChecklistRecord restores a checklist from the profile version it
// came from, discarding local edits. The task survives with its history.
func (s *Store) ResetChecklistRecord(code, name, actor string) (*core.ChecklistRecord, error) {
	_, rec, err := s.findChecklist(code, name)
	if err != nil {
		return nil, err
	}
	p, err := s.resetSource(code, "checklist", name, rec.Origin)
	if err != nil {
		return nil, err
	}
	doc, ok := p.ProfileChecklist(name)
	if !ok {
		return nil, fmt.Errorf("%w: profile %s no longer ships a checklist named %s", core.ErrUsage, p.Manifest.Ref(), name)
	}
	if err := s.SetChecklist(code, name, doc, actor); err != nil {
		return nil, err
	}
	return s.GetChecklist(code, name)
}

// ResetChannelRecord restores a channel's purpose and role hint from the
// profile version it came from. Endpoints and this machine's wiring are
// the project's own facts — no profile ever carried them — and survive.
func (s *Store) ResetChannelRecord(code, name, actor string) (*core.ChannelRecord, error) {
	rec, err := s.channelByName(code, name)
	if err != nil {
		return nil, err
	}
	p, err := s.resetSource(code, "channel", name, rec.Origin)
	if err != nil {
		return nil, err
	}
	doc, ok := p.ProfileChannel(name)
	if !ok {
		return nil, fmt.Errorf("%w: profile %s no longer ships a channel named %s", core.ErrUsage, p.Manifest.Ref(), name)
	}
	if err := s.SetDescription(rec.TaskID, doc.Purpose, actor); err != nil {
		return nil, err
	}
	if err := s.setChannelExpectation(rec, doc.RoleHint, rec.Origin, actor); err != nil {
		return nil, err
	}
	return s.channelByName(code, name)
}
