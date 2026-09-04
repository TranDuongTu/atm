package profile

import (
	"slices"
	"strings"
	"testing"

	"atm/internal/core"
)

func planFixture(t *testing.T) *core.Profile {
	t.Helper()
	p, err := Load(goodFiles())
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	return p.ForProject("DEMO")
}

func itemOf(t *testing.T, plan *core.ApplyPlan, kind, name string) core.ApplyItem {
	t.Helper()
	for _, it := range plan.Items {
		if it.Kind == kind && it.Name == name {
			return it
		}
	}
	t.Fatalf("plan has no %s %s: %+v", kind, name, plan.Items)
	return core.ApplyItem{}
}

// An empty project creates every document and enables every capability the
// manifest names that the project lacks.
func TestPlanApplyOnAnEmptyProjectCreatesEverything(t *testing.T) {
	p := planFixture(t)
	plan := PlanApply(p, Current{Enabled: []string{"scrum"}}, nil)
	if plan.Ref != "scrumban@1.0.0" {
		t.Fatalf("ref = %q", plan.Ref)
	}
	want := len(p.Personas) + len(p.Checklists) + len(p.Channels)
	if len(plan.Items) != want || plan.Count(core.ApplyCreate) != want {
		t.Fatalf("want %d creates, got %+v", want, plan.Items)
	}
	// Order: personas, then checklists, then channels — as the report reads.
	if plan.Items[0].Kind != core.ApplyKindPersona || plan.Items[len(plan.Items)-1].Kind != core.ApplyKindChannel {
		t.Fatalf("items are not kind-ordered: %+v", plan.Items)
	}
	caps := map[string]bool{}
	for _, c := range plan.Capabilities {
		caps[c.Name] = c.Enabled
	}
	if !caps["scrum"] || caps["channel"] {
		t.Fatalf("capabilities = %+v; scrum was enabled, channel was not", plan.Capabilities)
	}
	// The fixture manifest never names checklist, yet it ships checklists:
	// the substrate is implied, and listed after the manifest's own.
	if _, listed := caps["checklist"]; !listed || plan.Capabilities[len(plan.Capabilities)-1].Name != "checklist" {
		t.Fatalf("capabilities = %+v; want the implied checklist substrate last", plan.Capabilities)
	}
}

func TestRequiredCapabilitiesImpliesTheSubstrateOfShippedKinds(t *testing.T) {
	p := planFixture(t)
	if got := RequiredCapabilities(p); !slices.Equal(got, []string{"scrum", "channel", "checklist"}) {
		t.Fatalf("got %v", got)
	}
	p.Checklists, p.Channels = nil, nil
	if got := RequiredCapabilities(p); !slices.Equal(got, []string{"scrum", "channel"}) {
		t.Fatalf("without records: got %v", got)
	}
}

// A legacy project (nil capabilities) has everything enabled.
func TestPlanApplyTreatsNilCapabilitiesAsAllEnabled(t *testing.T) {
	plan := PlanApply(planFixture(t), Current{}, nil)
	for _, c := range plan.Capabilities {
		if !c.Enabled {
			t.Fatalf("capability %s reads as disabled on a legacy project", c.Name)
		}
	}
}

// Re-applying the same version is a no-op: every record is in sync and
// nothing is restamped.
func TestPlanApplyIsInSyncAfterItself(t *testing.T) {
	p := planFixture(t)
	cur := currentFrom(p, p.Origin().String())
	plan := PlanApply(p, cur, nil)
	if n := plan.Count(core.ApplyInSync); n != len(plan.Items) {
		t.Fatalf("%d of %d in sync: %+v", n, len(plan.Items), plan.Items)
	}
	for _, it := range plan.Items {
		if it.Restamp {
			t.Fatalf("%s %s restamped on a same-version re-apply", it.Kind, it.Name)
		}
	}
}

// Identical content at an older version of the SAME profile is in sync but
// restamped: the words did not change, the provenance moves forward.
func TestPlanApplyRestampsUnchangedRecordsFromAnOlderVersion(t *testing.T) {
	p := planFixture(t)
	cur := currentFrom(p, "scrumban@0.9.0")
	plan := PlanApply(p, cur, nil)
	it := itemOf(t, plan, core.ApplyKindChecklist, "planning")
	if it.State != core.ApplyInSync || !it.Restamp || it.Origin != "scrumban@0.9.0" {
		t.Fatalf("item = %+v", it)
	}
}

// A record the project edited is a conflict, whatever version it came from.
func TestPlanApplyFlagsLocallyModifiedRecordsAsConflicts(t *testing.T) {
	p := planFixture(t)
	cur := currentFrom(p, p.Origin().String())
	cur.Checklists[0].Purpose = "reworded locally"
	cur.Personas[0].Prompt += "\n\nLocal addendum."
	cur.Channels[0].Purpose = "something else"
	plan := PlanApply(p, cur, nil)
	for _, kind := range []string{core.ApplyKindChecklist, core.ApplyKindPersona, core.ApplyKindChannel} {
		var name string
		switch kind {
		case core.ApplyKindChecklist:
			name = cur.Checklists[0].Name
		case core.ApplyKindPersona:
			name = cur.Personas[0].Name
		default:
			name = cur.Channels[0].Name
		}
		it := itemOf(t, plan, kind, name)
		if it.State != core.ApplyConflict || !strings.Contains(it.Reason, "modified locally") {
			t.Fatalf("%s %s = %+v; want a modified-locally conflict", kind, name, it)
		}
	}
}

// When the record's own origin version is available and the record still
// matches it, the difference is the profile's, not the project's: update.
func TestPlanApplyUpgradesUnmodifiedRecordsWhenTheOldVersionProvesIt(t *testing.T) {
	older, err := Load(withFile("manifest.yaml", strings.Replace(string(goodFiles()["manifest.yaml"].Data), "1.0.0", "0.9.0", 1)))
	if err != nil {
		t.Fatal(err)
	}
	older = older.ForProject("DEMO")
	newer := planFixture(t)
	// The newer version rewords a checklist's purpose.
	for i := range newer.Checklists {
		if newer.Checklists[i].Name == "planning" {
			newer.Checklists[i].Purpose = "reworded by the profile"
		}
	}
	cur := currentFrom(older, "scrumban@0.9.0")
	previous := func(ref string) *core.Profile {
		if ref == "scrumban@0.9.0" {
			return older
		}
		return nil
	}
	plan := PlanApply(newer, cur, previous)
	it := itemOf(t, plan, core.ApplyKindChecklist, "planning")
	if it.State != core.ApplyUpdate || !strings.Contains(it.Reason, "purpose") {
		t.Fatalf("item = %+v; want an update naming the purpose", it)
	}
	// Without the old version to compare against, the same difference is a
	// conflict: nothing proves who changed it.
	plan = PlanApply(newer, cur, nil)
	if it := itemOf(t, plan, core.ApplyKindChecklist, "planning"); it.State != core.ApplyConflict {
		t.Fatalf("item = %+v; want a conflict when the origin version is unavailable", it)
	}
}

// Same-name records the project authored, or that predate profiles, or
// that another profile owns, are collisions: loud conflicts, never merges.
func TestPlanApplyRefusesToSilentlyReownForeignRecords(t *testing.T) {
	p := planFixture(t)
	cases := map[string]string{
		"user":          "owned by the project",
		"shipped:scrum": "pre-profile origin",
		"other@2.0.0":   "owned by profile other@2.0.0",
	}
	for origin, want := range cases {
		cur := currentFrom(p, origin)
		cur.Checklists[0].Purpose = "differs"
		plan := PlanApply(p, cur, nil)
		it := itemOf(t, plan, core.ApplyKindChecklist, cur.Checklists[0].Name)
		if it.State != core.ApplyConflict || !strings.Contains(it.Reason, want) {
			t.Errorf("origin %s: item = %+v; want reason containing %q", origin, it, want)
		}
	}
	// Identical content under a user or legacy origin is adopted: nothing
	// to lose, and the record gains a version to reset to. Another
	// profile's identical record is still a collision.
	for _, origin := range []string{"user", "shipped:scrum"} {
		plan := PlanApply(p, currentFrom(p, origin), nil)
		if it := itemOf(t, plan, core.ApplyKindPersona, p.Personas[0].Name); it.State != core.ApplyInSync || !it.Restamp {
			t.Errorf("origin %s identical: item = %+v; want in-sync + restamp", origin, it)
		}
	}
	plan := PlanApply(p, currentFrom(p, "other@2.0.0"), nil)
	if it := itemOf(t, plan, core.ApplyKindPersona, p.Personas[0].Name); it.State != core.ApplyConflict {
		t.Errorf("other profile identical: item = %+v; want a conflict", it)
	}
}

// Channels compare on purpose and role hint only: endpoints are project
// facts a profile never carries, so an addressed channel is still in sync.
func TestPlanApplyIgnoresChannelEndpoints(t *testing.T) {
	p := planFixture(t)
	cur := currentFrom(p, p.Origin().String())
	cur.Channels[0].Endpoints = []core.ChannelEndpoint{{Type: "notion", Role: "home", Address: core.ChannelAddress{Page: "abc"}}}
	cur.Channels[0].Type = "notion"
	plan := PlanApply(p, cur, nil)
	if it := itemOf(t, plan, core.ApplyKindChannel, cur.Channels[0].Name); it.State != core.ApplyInSync {
		t.Fatalf("item = %+v", it)
	}
}

// The setup report names what only the project or this machine can answer,
// each with the command that answers it.
func TestSetupReportNamesMissingEndpointsChannelsAndLauncher(t *testing.T) {
	p := planFixture(t)
	cur := currentFrom(p, p.Origin().String())
	// One channel addressed, the rest not; one checklist requires a channel
	// no record answers to.
	cur.Channels[0].Endpoints = []core.ChannelEndpoint{{Type: "slack", Role: "broadcast"}}
	cur.Checklists = append(cur.Checklists, core.ChecklistRecord{Name: "extra", Requires: core.ChecklistRequires{Channels: []string{"ghost"}}})
	steps := SetupReport("DEMO", cur, false)
	var kinds []string
	for _, s := range steps {
		kinds = append(kinds, s.Kind+":"+s.Subject)
	}
	joined := strings.Join(kinds, " ")
	if strings.Contains(joined, core.SetupChannelEndpoint+":"+cur.Channels[0].Name) {
		t.Fatalf("addressed channel reported as missing an endpoint: %v", kinds)
	}
	for _, ch := range cur.Channels[1:] {
		if !strings.Contains(joined, core.SetupChannelEndpoint+":"+ch.Name) {
			t.Fatalf("channel %s without endpoints not reported: %v", ch.Name, kinds)
		}
	}
	if !strings.Contains(joined, core.SetupChannelMissing+":ghost") {
		t.Fatalf("unmet requires_channels not reported: %v", kinds)
	}
	if !strings.Contains(joined, core.SetupLauncher+":") {
		t.Fatalf("unconfigured launcher not reported: %v", kinds)
	}
	for _, s := range steps {
		if s.Kind == core.SetupChannelEndpoint && !strings.Contains(s.Command, "atm channel endpoint add --project DEMO --name "+s.Subject) {
			t.Fatalf("endpoint step carries no exact command: %+v", s)
		}
	}
	if steps := SetupReport("DEMO", currentFrom(p, ""), true); len(steps) == 0 {
		t.Fatal("channels without endpoints must still be reported when the launcher is configured")
	}
}

// currentFrom pretends the profile was applied verbatim with the given
// origin: every document is a record.
func currentFrom(p *core.Profile, origin string) Current {
	cur := Current{Enabled: p.Manifest.RequiresCapabilities}
	for _, x := range p.Personas {
		x.Origin = origin
		x.TaskID = "T-" + x.Name
		cur.Personas = append(cur.Personas, x)
	}
	for _, x := range p.Checklists {
		x.Origin = origin
		x.TaskID = "T-" + x.Name
		cur.Checklists = append(cur.Checklists, x)
	}
	for _, x := range p.Channels {
		x.Origin = origin
		x.TaskID = "T-" + x.Name
		cur.Channels = append(cur.Channels, x)
	}
	return cur
}
