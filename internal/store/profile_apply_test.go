package store

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"atm/internal/core"
	"atm/internal/profile"
)

// applyFiles is profileFiles plus a channel expectation, so every record
// kind takes part.
func applyFiles(version string) fstest.MapFS {
	fsys := versioned(version)
	fsys["channels/design.md"] = &fstest.MapFile{Data: []byte("---\nname: design\nrole_hint: home\n---\nWhere <CODE> specs live.\n")}
	return fsys
}

func applyStore(t *testing.T, embedded map[string]fs.FS) *Store {
	t.Helper()
	s := newProfileTestStore(t, embedded)
	if _, err := s.CreateProject("DEMO", "demo", testActor); err != nil {
		t.Fatal(err)
	}
	return s
}

func loadFS(t *testing.T, fsys fs.FS) *core.Profile {
	t.Helper()
	p, err := profile.Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestApplyProfileCreatesEveryRecordKindWithTheProfileOrigin(t *testing.T) {
	s := applyStore(t, nil)
	plan, err := s.ApplyProfile("DEMO", loadFS(t, applyFiles("1.0.0")), false, testActor)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Count(core.ApplyCreate) != 3 {
		t.Fatalf("plan = %+v", plan.Items)
	}
	p, err := s.GetPersonaRecord("DEMO", "manager")
	if err != nil || p.Origin != "scrumban@1.0.0" {
		t.Fatalf("persona = %+v, %v", p, err)
	}
	c, err := s.GetChecklist("DEMO", "planning")
	if err != nil || c.Origin != "scrumban@1.0.0" {
		t.Fatalf("checklist = %+v, %v", c, err)
	}
	ch, err := s.GetChannelByName("DEMO", "design")
	if err != nil || ch.Origin != "scrumban@1.0.0" || len(ch.Endpoints) != 0 || ch.Purpose != "Where DEMO specs live." {
		t.Fatalf("channel = %+v, %v", ch, err)
	}
	// Re-apply: everything in sync, nothing restamped, nothing written.
	before := s.mustEventCount(t, "DEMO")
	plan, err = s.ApplyProfile("DEMO", loadFS(t, applyFiles("1.0.0")), false, testActor)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Count(core.ApplyInSync) != 3 {
		t.Fatalf("re-apply plan = %+v", plan.Items)
	}
	if after := s.mustEventCount(t, "DEMO"); after != before {
		t.Fatalf("an in-sync re-apply wrote %d events", after-before)
	}
}

func (s *Store) mustEventCount(t *testing.T, code string) int {
	t.Helper()
	st, err := s.StoreStats(code)
	if err != nil {
		t.Fatal(err)
	}
	return st.EventCount
}

func TestApplyProfileLeavesConflictsAloneUnlessForced(t *testing.T) {
	s := applyStore(t, nil)
	if _, err := s.ApplyProfile("DEMO", loadFS(t, applyFiles("1.0.0")), false, testActor); err != nil {
		t.Fatal(err)
	}
	// A local edit to the checklist and to the channel purpose.
	c, _ := s.GetChecklist("DEMO", "planning")
	c.Purpose = "reworded here"
	if err := s.SetChecklist("DEMO", "planning", *c, testActor); err != nil {
		t.Fatal(err)
	}
	purpose := "our own words"
	if err := s.EditChannel("DEMO", "design", &purpose, nil, testActor); err != nil {
		t.Fatal(err)
	}

	plan, err := s.ApplyProfile("DEMO", loadFS(t, applyFiles("1.0.0")), false, testActor)
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Conflicts(); len(got) != 2 {
		t.Fatalf("conflicts = %+v", got)
	}
	c, _ = s.GetChecklist("DEMO", "planning")
	if c.Purpose != "reworded here" {
		t.Fatalf("unforced apply overwrote the local edit: %q", c.Purpose)
	}

	plan, err = s.ApplyProfile("DEMO", loadFS(t, applyFiles("1.0.0")), true, testActor)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts()) != 0 {
		t.Fatalf("forced apply left conflicts: %+v", plan.Items)
	}
	forced := 0
	for _, it := range plan.Items {
		if it.Forced {
			forced++
		}
	}
	if forced != 2 {
		t.Fatalf("want 2 forced items, got %+v", plan.Items)
	}
	c, _ = s.GetChecklist("DEMO", "planning")
	if c.Purpose != "the weekly pass" || c.Origin != "scrumban@1.0.0" {
		t.Fatalf("forced apply did not restore the record: %+v", c)
	}
	ch, _ := s.GetChannelByName("DEMO", "design")
	if ch.Purpose != "Where DEMO specs live." {
		t.Fatalf("forced apply did not restore the channel purpose: %q", ch.Purpose)
	}
}

// Upgrading: a record untouched since 1.0.0 is updated to 1.1.0's words,
// and an in-sync record is restamped, because the installed 1.0.0 proves
// the project never edited them.
func TestApplyProfileUpgradesUnmodifiedRecordsAndRestampsTheRest(t *testing.T) {
	v1 := applyFiles("1.0.0")
	v2 := applyFiles("1.1.0")
	v2["checklists/planning.md"] = &fstest.MapFile{Data: []byte("---\nname: planning\npurpose: the weekly pass, revised\nsuits: [manager]\nrequires_capabilities: [scrum]\n---\n1. Orient.\n2. Decide.\n")}
	s := applyStore(t, map[string]fs.FS{"scrumban": v1})
	if _, err := s.ApplyProfile("DEMO", loadFS(t, v1), false, testActor); err != nil {
		t.Fatal(err)
	}
	plan, err := s.ApplyProfile("DEMO", loadFS(t, v2), false, testActor)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]core.ApplyState{}
	restamped := 0
	for _, it := range plan.Items {
		states[it.Kind+"/"+it.Name] = it.State
		if it.Restamp {
			restamped++
		}
	}
	if states["checklist/planning"] != core.ApplyUpdate || states["persona/manager"] != core.ApplyInSync || restamped != 2 {
		t.Fatalf("plan = %+v", plan.Items)
	}
	c, _ := s.GetChecklist("DEMO", "planning")
	if c.Origin != "scrumban@1.1.0" || !strings.Contains(c.Purpose, "revised") || len(c.Steps) != 2 {
		t.Fatalf("checklist after upgrade = %+v", c)
	}
	p, _ := s.GetPersonaRecord("DEMO", "manager")
	if p.Origin != "scrumban@1.1.0" {
		t.Fatalf("persona origin after upgrade = %q", p.Origin)
	}
	ch, _ := s.GetChannelByName("DEMO", "design")
	if ch.Origin != "scrumban@1.1.0" {
		t.Fatalf("channel origin after upgrade = %q", ch.Origin)
	}
}

// Apply touches purpose and role hint; endpoints and wiring are the
// project's and this machine's, and survive a forced overwrite.
func TestApplyProfileNeverTouchesChannelEndpoints(t *testing.T) {
	s := applyStore(t, nil)
	if _, err := s.ApplyProfile("DEMO", loadFS(t, applyFiles("1.0.0")), false, testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.AddChannelEndpoint("DEMO", "design", core.ChannelEndpoint{Type: "notion", Address: core.ChannelAddress{Page: "p1"}}, testActor); err != nil {
		t.Fatal(err)
	}
	purpose := "edited"
	if err := s.EditChannel("DEMO", "design", &purpose, nil, testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyProfile("DEMO", loadFS(t, applyFiles("1.0.0")), true, testActor); err != nil {
		t.Fatal(err)
	}
	ch, _ := s.GetChannelByName("DEMO", "design")
	if len(ch.Endpoints) != 1 || ch.Endpoints[0].Address.Page != "p1" || ch.Purpose != "Where DEMO specs live." {
		t.Fatalf("channel = %+v", ch.ChannelRecord)
	}
}

func TestPlanProfileWritesNothing(t *testing.T) {
	s := applyStore(t, nil)
	before := s.mustEventCount(t, "DEMO")
	plan, err := s.PlanProfile("DEMO", loadFS(t, applyFiles("1.0.0")))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Count(core.ApplyCreate) != 3 {
		t.Fatalf("plan = %+v", plan.Items)
	}
	if after := s.mustEventCount(t, "DEMO"); after != before {
		t.Fatal("planning wrote events")
	}
	if _, err := s.GetChecklist("DEMO", "planning"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("planning created a record: %v", err)
	}
}

func TestResetChecklistRestoresFromItsOwnOriginVersion(t *testing.T) {
	v1 := applyFiles("1.0.0")
	s := applyStore(t, map[string]fs.FS{"scrumban": v1})
	if _, err := s.ApplyProfile("DEMO", loadFS(t, v1), false, testActor); err != nil {
		t.Fatal(err)
	}
	c, _ := s.GetChecklist("DEMO", "planning")
	c.Purpose = "reworded"
	c.Steps = append(c.Steps, core.ChecklistStep{Text: "Extra."})
	if err := s.SetChecklist("DEMO", "planning", *c, testActor); err != nil {
		t.Fatal(err)
	}
	got, err := s.ResetChecklistRecord("DEMO", "planning", testActor)
	if err != nil {
		t.Fatal(err)
	}
	if got.Purpose != "the weekly pass" || len(got.Steps) != 1 || got.Origin != "scrumban@1.0.0" || got.TaskID != c.TaskID {
		t.Fatalf("reset = %+v", got)
	}
}

func TestResetChecklistRefusesWhatItCannotRestore(t *testing.T) {
	s := applyStore(t, nil)
	user := core.ChecklistRecord{Name: "ours", Steps: []core.ChecklistStep{{Text: "x"}}}
	if _, err := s.CreateChecklist("DEMO", user, testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResetChecklistRecord("DEMO", "ours", testActor); err == nil || !strings.Contains(err.Error(), "origin user") {
		t.Fatalf("user origin: %v", err)
	}
	legacy := core.ChecklistRecord{Name: "old", Origin: "shipped:scrum", Steps: []core.ChecklistStep{{Text: "x"}}}
	if _, err := s.CreateChecklist("DEMO", legacy, testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResetChecklistRecord("DEMO", "old", testActor); err == nil || !strings.Contains(err.Error(), "shipped:scrum") {
		t.Fatalf("legacy origin: %v", err)
	}
	// Applied from a directory (dev) or from a version not installed here:
	// the refusal names the version.
	if _, err := s.ApplyProfile("DEMO", loadFS(t, applyFiles("2.0.0")), false, testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResetChecklistRecord("DEMO", "planning", testActor); err == nil || !strings.Contains(err.Error(), "scrumban@2.0.0") || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("missing version: %v", err)
	}
}

func TestResetChannelRestoresPurposeAndKeepsEndpoints(t *testing.T) {
	v1 := applyFiles("1.0.0")
	s := applyStore(t, map[string]fs.FS{"scrumban": v1})
	if _, err := s.ApplyProfile("DEMO", loadFS(t, v1), false, testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.AddChannelEndpoint("DEMO", "design", core.ChannelEndpoint{Type: "slack", Address: core.ChannelAddress{ChannelID: "C1"}}, testActor); err != nil {
		t.Fatal(err)
	}
	purpose := "edited"
	if err := s.EditChannel("DEMO", "design", &purpose, nil, testActor); err != nil {
		t.Fatal(err)
	}
	got, err := s.ResetChannelRecord("DEMO", "design", testActor)
	if err != nil {
		t.Fatal(err)
	}
	if got.Purpose != "Where DEMO specs live." || len(got.Endpoints) != 1 || got.Endpoints[0].Address.ChannelID != "C1" {
		t.Fatalf("reset = %+v", got)
	}
	if _, err := s.CreateChannel("DEMO", core.ChannelRecord{Name: "ours", Type: "repo", Address: core.ChannelAddress{URL: "u"}}, testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResetChannelRecord("DEMO", "ours", testActor); err == nil || !strings.Contains(err.Error(), "origin user") {
		t.Fatalf("user origin: %v", err)
	}
}

// A profile channel is an expectation: a handle with no endpoint yet.
func TestCreateChannelAcceptsAnExpectationWithoutEndpoints(t *testing.T) {
	s := applyStore(t, nil)
	if _, err := s.CreateChannel("DEMO", core.ChannelRecord{Name: "design", Purpose: "p", Origin: "scrumban@1.0.0"}, testActor); err != nil {
		t.Fatal(err)
	}
	ch, err := s.GetChannelByName("DEMO", "design")
	if err != nil || len(ch.Endpoints) != 0 || ch.Type != "" || ch.Origin != "scrumban@1.0.0" {
		t.Fatalf("channel = %+v, %v", ch, err)
	}
	if _, err := s.CreateChannel("DEMO", core.ChannelRecord{Name: "bare"}, testActor); err == nil {
		t.Fatal("a user-authored channel still needs an endpoint: nothing else says what it is")
	}
}
