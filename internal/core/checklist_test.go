// internal/core/checklist_test.go
package core

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestChecklistLabelHelpers(t *testing.T) {
	if got := ChecklistLabel("ATM"); got != "ATM:checklist" {
		t.Fatalf("ChecklistLabel = %q", got)
	}
	if got := ChecklistPersonaLabelPrefix("ATM"); got != "ATM:checklist:" {
		t.Fatalf("ChecklistPersonaLabelPrefix = %q", got)
	}
}

func TestValidChecklistOrigin(t *testing.T) {
	for _, ok := range []string{"user", "shipped:atm", "shipped:scrum", "shipped:my-cap"} {
		if !ValidChecklistOrigin(ok) {
			t.Fatalf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "shipped:", "shipped:Bad Name", "vendor", "user:x"} {
		if ValidChecklistOrigin(bad) {
			t.Fatalf("%q should be invalid", bad)
		}
	}
}

func TestChecklistStepCountRecursive(t *testing.T) {
	steps := []ChecklistStep{
		{Text: "a", Children: []ChecklistStep{{Text: "a1"}, {Text: "a2", Children: []ChecklistStep{{Text: "a2i"}}}}},
		{Text: "b"},
	}
	if got := ChecklistStepCount(steps); got != 5 {
		t.Fatalf("count = %d, want 5", got)
	}
}

func TestEncodeStampsV2AndPreservesUnknownFields(t *testing.T) {
	m := map[string]any{"name": "x", "future": "field", "v": 1}
	enc, err := EncodeChecklistPayload(m)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal([]byte(enc), &back); err != nil {
		t.Fatal(err)
	}
	if back["v"] != float64(2) {
		t.Fatalf("v = %v, want 2", back["v"])
	}
	if back["future"] != "field" {
		t.Fatalf("unknown field dropped: %s", enc)
	}
	if m["v"] != 1 {
		t.Fatalf("input mutated: %v", m)
	}
}

func TestChecklistFromTaskV2(t *testing.T) {
	payload := `{"v":2,"name":"scrum-backlog","purpose":"p","steps":[{"text":"a","children":[{"text":"a1"}]}],"suits":["manager"],"requires":{"capabilities":["scrum"]},"origin":"shipped:scrum"}`
	task := Task{ID: "ATM-1", Title: "scrum-backlog",
		Labels: []string{"ATM:checklist"}, Meta: map[string]string{ChecklistMetaKey: payload}}
	rec, err := ChecklistFromTask("ATM", task)
	if err != nil {
		t.Fatal(err)
	}
	want := &ChecklistRecord{
		TaskID:  "ATM-1",
		Name:    "scrum-backlog",
		Purpose: "p",
		Steps:   []ChecklistStep{{Text: "a", Children: []ChecklistStep{{Text: "a1"}}}},
		Suits:   []string{"manager"},
		Requires: ChecklistRequires{
			Capabilities: []string{"scrum"},
		},
		// A record written before the dispatch facts existed reads back
		// with their defaults, never unset.
		Target: ChecklistTargetProject,
		Mode:   ChecklistModeEager,
		Origin: "shipped:scrum",
	}
	if !reflect.DeepEqual(rec, want) {
		t.Fatalf("got %+v, want %+v", rec, want)
	}
}

func TestChecklistFromTaskV1ReadCompat(t *testing.T) {
	payload := `{"v":1,"persona":"developer","name":"pr-convention","purpose":"p","steps":["one","two"]}`
	task := Task{ID: "ATM-2", Title: "developer/pr-convention",
		Labels: []string{"ATM:checklist:developer"}, Meta: map[string]string{ChecklistMetaKey: payload}}
	rec, err := ChecklistFromTask("ATM", task)
	if err != nil {
		t.Fatal(err)
	}
	want := &ChecklistRecord{
		TaskID:  "ATM-2",
		Name:    "pr-convention",
		Purpose: "p",
		Steps:   []ChecklistStep{{Text: "one"}, {Text: "two"}},
		Suits:   []string{"developer"},
		Target:  ChecklistTargetProject,
		Mode:    ChecklistModeEager,
		Origin:  "user",
	}
	if !reflect.DeepEqual(rec, want) {
		t.Fatalf("got %+v, want %+v", rec, want)
	}
}

func TestChecklistFromTaskV1NameFallsBackToTitle(t *testing.T) {
	payload := `{"v":1,"persona":"developer","steps":["one"]}`
	task := Task{ID: "ATM-3", Title: "developer/pr-convention",
		Labels: []string{"ATM:checklist:developer"}, Meta: map[string]string{ChecklistMetaKey: payload}}
	rec, err := ChecklistFromTask("ATM", task)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Name != "pr-convention" {
		t.Fatalf("Name = %q, want title-derived pr-convention", rec.Name)
	}
}

func TestChecklistFromTaskNonChecklistIsNil(t *testing.T) {
	got, err := ChecklistFromTask("ATM", Task{ID: "ATM-4", Labels: []string{"ATM:status:open"}})
	if err != nil || got != nil {
		t.Fatalf("want nil,nil; got %v,%v", got, err)
	}
}

func TestChecklistFromTaskMalformedPayloadErrors(t *testing.T) {
	for _, labels := range [][]string{
		{"ATM:checklist"},
		{"ATM:checklist:developer"},
	} {
		task := Task{ID: "ATM-5", Labels: labels, Meta: map[string]string{ChecklistMetaKey: "{not json"}}
		_, err := ChecklistFromTask("ATM", task)
		if err == nil || !strings.Contains(err.Error(), "ATM-5") {
			t.Fatalf("labels %v: want decode error naming the task, got %v", labels, err)
		}
	}
}

func TestChecklistPayloadUnknownFieldsSurvive(t *testing.T) {
	m, err := DecodeChecklistPayload(`{"v":1,"name":"main","future":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := EncodeChecklistPayload(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(enc, `"future":"x"`) {
		t.Fatalf("unknown field dropped: %s", enc)
	}
}

func TestChecklistPayloadFromRoundTrips(t *testing.T) {
	rec := ChecklistRecord{Name: "n", Purpose: "p",
		Steps: []ChecklistStep{{Text: "a", Children: []ChecklistStep{{Text: "a1"}}}},
		Suits: []string{"manager"}, Requires: ChecklistRequires{Capabilities: []string{"scrum"}},
		Target: ChecklistTargetProject, Mode: ChecklistModeEager, Origin: "user"}
	enc, err := EncodeChecklistPayload(ChecklistPayloadFrom(rec))
	if err != nil {
		t.Fatal(err)
	}
	task := Task{ID: "ATM-6", Title: "n", Labels: []string{"ATM:checklist"}, Meta: map[string]string{ChecklistMetaKey: enc}}
	got, err := ChecklistFromTask("ATM", task)
	if err != nil {
		t.Fatal(err)
	}
	rec.TaskID = "ATM-6"
	if !reflect.DeepEqual(got, &rec) {
		t.Fatalf("round trip: got %+v, want %+v", got, rec)
	}
}

func TestDefaultChecklistSetNarrowsByCapabilityScope(t *testing.T) {
	recs := []ChecklistRecord{
		{Name: "neutral"},
		{Name: "scrum-backlog", Requires: ChecklistRequires{Capabilities: []string{"scrum"}}},
		{Name: "qa-backlog", Requires: ChecklistRequires{Capabilities: []string{"qa"}}},
	}
	names := func(in []ChecklistRecord) []string {
		out := make([]string, len(in))
		for i, r := range in {
			out[i] = r.Name
		}
		return out
	}
	// A scoped session keeps capability-neutral checklists and those whose
	// required capabilities include the scope; the rest drop. Order holds.
	got := DefaultChecklistSet(recs, "scrum")
	if want := []string{"neutral", "scrum-backlog"}; !reflect.DeepEqual(names(got), want) {
		t.Fatalf("scoped set = %v, want %v", names(got), want)
	}
	// No scope selects everything.
	got = DefaultChecklistSet(recs, "")
	if want := []string{"neutral", "scrum-backlog", "qa-backlog"}; !reflect.DeepEqual(names(got), want) {
		t.Fatalf("unscoped set = %v, want %v", names(got), want)
	}
	// The input slice is never mutated or aliased.
	got[0].Name = "mutated"
	if recs[0].Name != "neutral" {
		t.Fatal("DefaultChecklistSet must copy, not alias, its input")
	}
	if DefaultChecklistSet(nil, "scrum") != nil {
		t.Fatal("nil in, nil out")
	}
}

func TestChecklistRequireWarnings(t *testing.T) {
	rec := ChecklistRecord{
		Name: "neutral",
		Requires: ChecklistRequires{
			Capabilities: []string{"scrum", "qa"},
			Channels:     []string{"journal", "prs"},
		},
	}
	channels := []ChannelView{
		{ChannelRecord: ChannelRecord{Name: "journal", Type: "slack"}}, // exists, unwired
	}
	got := ChecklistRequireWarnings(rec, []string{"scrum"}, channels)
	want := []string{
		"checklist neutral: requires capability qa (not enabled)",
		"checklist neutral: requires channel journal (unwired)",
		"checklist neutral: requires channel prs (none exists)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("warnings = %v, want %v", got, want)
	}
	// A wired channel and an enabled capability warn about nothing.
	channels[0].Wiring = &ChannelWiring{}
	if got := ChecklistRequireWarnings(ChecklistRecord{Name: "n", Requires: ChecklistRequires{Capabilities: []string{"scrum"}, Channels: []string{"journal"}}}, []string{"scrum"}, channels); got != nil {
		t.Fatalf("satisfied requires must yield nil, got %v", got)
	}
}

func TestRenderChecklistStepsNumbering(t *testing.T) {
	steps := []ChecklistStep{
		{Text: "Triage the inbox", Children: []ChecklistStep{
			{Text: "list it"},
			{Text: "decide one", Children: []ChecklistStep{{Text: "absorb"}}},
		}},
		{Text: "Advance"},
	}
	want := "1. Triage the inbox\n" +
		"   1.1 list it\n" +
		"   1.2 decide one\n" +
		"      1.2.1 absorb\n" +
		"2. Advance\n"
	if got := RenderChecklistSteps(steps); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if got := RenderChecklistSteps(nil); got != "" {
		t.Fatalf("nil steps render %q, want empty", got)
	}
}

// The dispatch facts survive a payload round trip, and a task-target record
// keeps its targets expression.
func TestChecklistPayloadCarriesDispatchFacts(t *testing.T) {
	rec := ChecklistRecord{
		Name:    "scrum-coding",
		Purpose: "implement one increment",
		Steps:   []ChecklistStep{{Text: "gate"}},
		Target:  ChecklistTargetTask,
		Targets: "ATM:scrum-stage:implementable",
		Mode:    ChecklistModeInteractive,
		Origin:  "scrumban@1.0.0",
	}
	enc, err := EncodeChecklistPayload(ChecklistPayloadFrom(rec))
	if err != nil {
		t.Fatal(err)
	}
	task := Task{ID: "ATM-7", Title: "scrum-coding", Labels: []string{"ATM:checklist"}, Meta: map[string]string{ChecklistMetaKey: enc}}
	got, err := ChecklistFromTask("ATM", task)
	if err != nil {
		t.Fatal(err)
	}
	rec.TaskID = "ATM-7"
	if !reflect.DeepEqual(got, &rec) {
		t.Fatalf("round trip: got %+v, want %+v", got, rec)
	}
}

// The defaults are not written into the payload: an unwritten key and the
// default mean the same thing, so every record that predates these fields
// stays byte-identical on its next edit.
func TestChecklistPayloadOmitsDispatchDefaults(t *testing.T) {
	m := ChecklistPayloadFrom(ChecklistRecord{
		Name: "standup", Steps: []ChecklistStep{{Text: "a"}},
		Target: ChecklistTargetProject, Mode: ChecklistModeEager, Origin: "user",
	})
	for _, k := range []string{"target", "mode", "targets"} {
		if _, ok := m[k]; ok {
			t.Fatalf("payload writes default %q: %v", k, m)
		}
	}
}

// targets only narrows tasks, so a record that is not task-targeted must not
// carry one back out of the ledger — a stale expression would silently
// filter nothing while looking like it filtered something.
func TestChecklistDecodeDropsTargetsOnProjectTarget(t *testing.T) {
	payload := `{"v":2,"name":"planning","steps":[{"text":"a"}],"targets":"ATM:scrum:task","origin":"user"}`
	task := Task{ID: "ATM-8", Title: "planning", Labels: []string{"ATM:checklist"}, Meta: map[string]string{ChecklistMetaKey: payload}}
	rec, err := ChecklistFromTask("ATM", task)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Targets != "" {
		t.Fatalf("targets = %q on a %s-target record", rec.Targets, ChecklistTargetProject)
	}
}
