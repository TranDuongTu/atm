package profile

import (
	"sort"

	"atm/internal/core"
)

// Where a document's text came from (ExportNote.Source).
const (
	ExportFromOrigin   = "origin"       // in sync with an available origin version: that version's document, placeholders intact
	ExportModified     = "modified"     // differs from its origin version: the record, literally
	ExportUser         = "user"         // user or legacy shipped:* origin: the record
	ExportUnverifiable = "unverifiable" // origin version not available here (or lacks the document): the record
)

// ExportNote says, for one exported document, which text was written and
// why — the report a fork's author reads before publishing.
type ExportNote struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Source string `json:"source"`
	Origin string `json:"origin,omitempty"`
}

// Export turns a project's records into a new profile — the inverse of
// PlanApply. Every record becomes one document. A record still in sync
// with an AVAILABLE origin version (equal to that version's document
// once <CODE> is substituted for code) contributes the origin's
// unsubstituted document, so placeholders survive; any other record —
// modified, user-authored, legacy, or stamped with a version this machine
// cannot resolve — contributes its own text.
//
// origin resolves a name@version ref to that version's profile, or nil;
// it may be nil. enabled is the project's capability set and becomes the
// manifest's requires_capabilities (sorted). Ledger identity, origin
// stamps, endpoints and addresses never make it into the result.
func Export(code string, cur Current, enabled []string, origin func(ref string) *core.Profile, manifest core.ProfileManifest) (*core.Profile, []ExportNote) {
	p := &core.Profile{Manifest: manifest}
	p.Manifest.Format = Format
	p.Manifest.RequiresCapabilities = append([]string(nil), enabled...)
	sort.Strings(p.Manifest.RequiresCapabilities)
	if len(p.Manifest.RequiresCapabilities) == 0 {
		p.Manifest.RequiresCapabilities = nil
	}
	e := exporter{code: code, origin: origin, cache: map[string]*core.Profile{}}
	var notes []ExportNote

	for _, rec := range cur.Personas {
		doc := core.Persona{Name: rec.Name, Description: rec.Description, Prompt: rec.Prompt, Launch: rec.Launch, ProjectOptional: rec.ProjectOptional}
		note, from := e.classify(core.ApplyKindPersona, rec.Name, rec.Origin, func(sub *core.Profile) ([]string, bool) {
			o, ok := sub.ProfilePersona(rec.Name)
			return personaDiff(rec, o), ok
		})
		if from != nil {
			doc, _ = from.ProfilePersona(rec.Name)
		}
		p.Personas = append(p.Personas, doc)
		notes = append(notes, note)
	}
	for _, rec := range cur.Checklists {
		doc := core.ChecklistRecord{Name: rec.Name, Purpose: rec.Purpose, Steps: rec.Steps, Suits: rec.Suits, Requires: rec.Requires, Target: rec.Target, Targets: rec.Targets, Mode: rec.Mode}
		note, from := e.classify(core.ApplyKindChecklist, rec.Name, rec.Origin, func(sub *core.Profile) ([]string, bool) {
			o, ok := sub.ProfileChecklist(rec.Name)
			return checklistDiff(rec, o), ok
		})
		if from != nil {
			doc, _ = from.ProfileChecklist(rec.Name)
		}
		p.Checklists = append(p.Checklists, doc)
		notes = append(notes, note)
	}
	for _, rec := range cur.Channels {
		doc := core.ChannelRecord{Name: rec.Name, RoleHint: rec.RoleHint, Purpose: rec.Purpose}
		note, from := e.classify(core.ApplyKindChannel, rec.Name, rec.Origin, func(sub *core.Profile) ([]string, bool) {
			o, ok := sub.ProfileChannel(rec.Name)
			return channelDiff(rec, o), ok
		})
		if from != nil {
			doc, _ = from.ProfileChannel(rec.Name)
		}
		p.Channels = append(p.Channels, doc)
		notes = append(notes, note)
	}

	sort.Slice(p.Personas, func(i, j int) bool { return p.Personas[i].Name < p.Personas[j].Name })
	sort.Slice(p.Checklists, func(i, j int) bool { return p.Checklists[i].Name < p.Checklists[j].Name })
	sort.Slice(p.Channels, func(i, j int) bool { return p.Channels[i].Name < p.Channels[j].Name })
	sort.Slice(notes, func(i, j int) bool {
		if notes[i].Kind != notes[j].Kind {
			return notes[i].Kind < notes[j].Kind
		}
		return notes[i].Name < notes[j].Name
	})
	return p, notes
}

// exporter resolves each origin version at most once per export and
// applies the source rule to one record at a time.
type exporter struct {
	code   string
	origin func(ref string) *core.Profile
	cache  map[string]*core.Profile
}

// classify returns the note for one record and, when the record is in
// sync with an available origin, that origin's UNSUBSTITUTED profile —
// the caller takes the document from it. diffAgainst receives the origin
// substituted for the project and reports the field diff plus whether
// the origin carries the document at all.
func (e *exporter) classify(kind, name, originStr string, diffAgainst func(sub *core.Profile) ([]string, bool)) (ExportNote, *core.Profile) {
	note := ExportNote{Kind: kind, Name: name, Origin: originStr}
	o, err := core.ParseOrigin(originStr)
	if err != nil || o.Kind != core.OriginProfile {
		note.Source = ExportUser
		return note, nil
	}
	ref := o.Ref()
	prof, seen := e.cache[ref]
	if !seen {
		if e.origin != nil {
			prof = e.origin(ref)
		}
		e.cache[ref] = prof
	}
	if prof == nil {
		note.Source = ExportUnverifiable
		return note, nil
	}
	diff, ok := diffAgainst(prof.ForProject(e.code))
	switch {
	case !ok:
		note.Source = ExportUnverifiable
		return note, nil
	case len(diff) > 0:
		note.Source = ExportModified
		return note, nil
	}
	note.Source = ExportFromOrigin
	return note, prof
}
