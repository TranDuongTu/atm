package store

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"atm/internal/core"
	"atm/internal/profile"
)

const managerDoc = `---
name: manager
description: Runs the flow.
---
# Persona: manager

You are the manager of <CODE>.

## Principles

1. **Multiply, don't produce.**
`

// personaProject opens a store with one project ready for persona records.
func personaProject(t *testing.T) (*Store, string) {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject("ATM", "ATM", testActor); err != nil {
		t.Fatal(err)
	}
	return s, "ATM"
}

func personaDoc(t *testing.T, src string) core.Persona {
	t.Helper()
	p, err := profile.ParsePersonaDocument("manager", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSetPersonaRecordCreatesAProjectRecord(t *testing.T) {
	s, code := personaProject(t)
	if _, err := s.SetPersonaRecord(code, personaDoc(t, managerDoc), testActor); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPersonaRecord(code, "manager")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "manager" || got.Description != "Runs the flow." {
		t.Fatalf("record = %+v", got)
	}
	// The prompt is the task's description, so `atm search` finds a persona
	// by what it says and the TUI shows it without a special case.
	if !strings.Contains(got.Prompt, "## Principles") {
		t.Fatalf("prompt not carried whole: %q", got.Prompt)
	}
	tk, err := s.GetTask(got.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tk.Description, "## Principles") {
		t.Fatalf("prompt is not the task description: %q", tk.Description)
	}
	// A record the project authored itself has nothing to reset to.
	if got.Origin != "user" {
		t.Fatalf("origin = %q, want user", got.Origin)
	}
}

// Records are per PROJECT: two projects may run the same-named persona from
// different profiles, and neither may see the other's.
func TestPersonaRecordsAreScopedToTheirProject(t *testing.T) {
	s, code := personaProject(t)
	if _, err := s.CreateProject("OPS", "Ops", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetPersonaRecord(code, personaDoc(t, managerDoc), testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetPersonaRecord("OPS", personaDoc(t, strings.Replace(managerDoc, "Runs the flow.", "Runs Ops.", 1)), testActor); err != nil {
		t.Fatal(err)
	}
	here, err := s.GetPersonaRecord(code, "manager")
	if err != nil {
		t.Fatal(err)
	}
	there, err := s.GetPersonaRecord("OPS", "manager")
	if err != nil {
		t.Fatal(err)
	}
	if here.Description == there.Description {
		t.Fatalf("both projects see %q — records are not project-scoped", here.Description)
	}
	recs, err := s.PersonaRecords(code)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("PersonaRecords(%s) = %d records, want 1", code, len(recs))
	}
}

// set replaces wholesale — ATM is the source of record, not an editor — and
// the task survives so its history is not orphaned.
func TestSetPersonaRecordReplacesWholesale(t *testing.T) {
	s, code := personaProject(t)
	first, err := s.SetPersonaRecord(code, personaDoc(t, managerDoc), testActor)
	if err != nil {
		t.Fatal(err)
	}
	revised := strings.Replace(managerDoc, "Runs the flow.", "Runs everything.", 1)
	second, err := s.SetPersonaRecord(code, personaDoc(t, revised), testActor)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("set created a new task %s, replacing %s — history orphaned", second.ID, first.ID)
	}
	got, _ := s.GetPersonaRecord(code, "manager")
	if got.Description != "Runs everything." {
		t.Fatalf("description = %q", got.Description)
	}
}

// Editing a profile-applied persona keeps its provenance, which is exactly
// what makes reset able to restore it.
func TestSetPersonaRecordPreservesProfileOrigin(t *testing.T) {
	s, code := installedProfileStore(t)
	if err := applyPersonaFromProfile(t, s, code, "scrumban", "1.0.0", "manager"); err != nil {
		t.Fatal(err)
	}
	local := personaDoc(t, strings.Replace(managerDoc, "Runs the flow.", "Locally reworded.", 1))
	if _, err := s.SetPersonaRecord(code, local, testActor); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetPersonaRecord(code, "manager")
	if got.Origin != "scrumban@1.0.0" {
		t.Fatalf("origin = %q, want the profile it came from", got.Origin)
	}
	if got.Description != "Locally reworded." {
		t.Fatalf("description = %q", got.Description)
	}
}

func TestResetPersonaRecordRestoresTheProfileCopy(t *testing.T) {
	s, code := installedProfileStore(t)
	if err := applyPersonaFromProfile(t, s, code, "scrumban", "1.0.0", "manager"); err != nil {
		t.Fatal(err)
	}
	local := personaDoc(t, strings.Replace(managerDoc, "Runs the flow.", "Locally reworded.", 1))
	if _, err := s.SetPersonaRecord(code, local, testActor); err != nil {
		t.Fatal(err)
	}
	restored, err := s.ResetPersonaRecord(code, "manager", testActor)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Description != "Runs the flow." {
		t.Fatalf("reset gave %q, want the profile's own text", restored.Description)
	}
	if restored.Origin != "scrumban@1.0.0" {
		t.Fatalf("reset changed the origin to %q", restored.Origin)
	}
}

// A record the project authored has no source to restore from; saying so is
// better than silently doing nothing.
func TestResetPersonaRecordRefusesUserOrigin(t *testing.T) {
	s, code := personaProject(t)
	if _, err := s.SetPersonaRecord(code, personaDoc(t, managerDoc), testActor); err != nil {
		t.Fatal(err)
	}
	_, err := s.ResetPersonaRecord(code, "manager", testActor)
	if err == nil {
		t.Fatal("reset accepted a user-origin record")
	}
	if !strings.Contains(err.Error(), "user") {
		t.Fatalf("err = %v, want it to name the reason", err)
	}
}

// The version a record came from can be gone — uninstalled, or never
// installed on this machine. The refusal has to name what to do about it.
func TestResetPersonaRecordNamesAMissingVersion(t *testing.T) {
	s, code := installedProfileStore(t)
	if err := applyPersonaFromProfile(t, s, code, "scrumban", "1.0.0", "manager"); err != nil {
		t.Fatal(err)
	}
	// Restamp the record as having come from a version nobody installed.
	rec, _ := s.GetPersonaRecord(code, "manager")
	if err := s.setPersonaOrigin(rec.TaskID, "scrumban@9.9.9", testActor); err != nil {
		t.Fatal(err)
	}
	_, err := s.ResetPersonaRecord(code, "manager", testActor)
	if err == nil {
		t.Fatal("reset succeeded against a version that is not installed")
	}
	if !strings.Contains(err.Error(), "9.9.9") {
		t.Fatalf("err = %v, want it to name the missing version", err)
	}
}

func TestRemovePersonaRecord(t *testing.T) {
	s, code := personaProject(t)
	if _, err := s.SetPersonaRecord(code, personaDoc(t, managerDoc), testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.RemovePersonaRecord(code, "manager", "", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPersonaRecord(code, "manager"); err == nil {
		t.Fatal("record survived removal")
	}
	recs, _ := s.PersonaRecords(code)
	if len(recs) != 0 {
		t.Fatalf("PersonaRecords = %+v after removal", recs)
	}
}

func TestGetPersonaRecordNotFound(t *testing.T) {
	s, code := personaProject(t)
	if _, err := s.GetPersonaRecord(code, "nobody"); err == nil {
		t.Fatal("resolved a persona nobody set")
	}
}

// installedProfileStore gives a store with a one-persona profile installed,
// so origin and reset can be tested against a real installed version.
func installedProfileStore(t *testing.T) (*Store, string) {
	t.Helper()
	s, code := personaProject(t)
	src := fstest.MapFS{
		"manifest.yaml":          &fstest.MapFile{Data: []byte("name: scrumban\nversion: 1.0.0\nformat: 1\nrequires_capabilities: [scrum]\n")},
		"personas/manager.md":    &fstest.MapFile{Data: []byte(managerDoc)},
		"checklists/planning.md": &fstest.MapFile{Data: []byte("---\nname: planning\npurpose: the weekly pass\nsuits: [manager]\nrequires_capabilities: [scrum]\n---\n1. Orient.\n")},
	}
	var buf bytes.Buffer
	if _, err := profile.Build(src, &buf); err != nil {
		t.Fatal(err)
	}
	// No embedded profile: the installed one is the only candidate.
	s.embeddedProfilesFn = func() map[string]fs.FS { return nil }
	if _, err := s.installProfile(bytes.NewReader(buf.Bytes()), ""); err != nil {
		t.Fatal(err)
	}
	return s, code
}

// applyPersonaFromProfile is the slice of `profile apply` this increment
// needs: import one profile persona as a project record with its origin.
// The full verb lands in increment 7.
func applyPersonaFromProfile(t *testing.T, s *Store, code, name, version, persona string) error {
	t.Helper()
	p, e, err := s.GetProfile(name, version)
	if err != nil {
		return err
	}
	doc, ok := p.ForProject(code).ProfilePersona(persona)
	if !ok {
		t.Fatalf("profile %s has no persona %q", e.Ref(), persona)
	}
	doc.Origin = e.Ref()
	_, err = s.SetPersonaRecord(code, doc, testActor)
	return err
}
