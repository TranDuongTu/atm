package compose

import (
	"reflect"
	"strings"
	"testing"

	"atm/internal/core"
	"atm/internal/session"
)

// fakeSvc implements only the core.Service methods Compose touches; the
// embedded interface panics on anything else, keeping the seam honest.
type fakeSvc struct {
	core.Service
	suited     []core.ChecklistRecord
	channels   []core.ChannelView
	personaDoc map[string]string
}

func (f *fakeSvc) StorePath() string { return "/store" }

func (f *fakeSvc) GetPersonality(name string) (string, error) { return "", nil }

func (f *fakeSvc) PersonaDoc(name string) (string, error) {
	if d, ok := f.personaDoc[name]; ok {
		return d, nil
	}
	return "", core.ErrNotFound
}

func (f *fakeSvc) SuitedChecklists(code, persona string) ([]core.ChecklistRecord, error) {
	return f.suited, nil
}

func (f *fakeSvc) GetChecklist(code, name string) (*core.ChecklistRecord, error) {
	for i := range f.suited {
		if f.suited[i].Name == name {
			return &f.suited[i], nil
		}
	}
	return nil, core.ErrNotFound
}

func (f *fakeSvc) ProjectChannels(code string) ([]core.ChannelView, error) {
	return f.channels, nil
}

func testService(f *fakeSvc) *Service {
	return &Service{
		Svc:                 f,
		EnabledCapabilities: func(code string) []string { return []string{"scrum"} },
		CapabilitiesBlock:   func(code string) string { return "## Capabilities\n\n- **scrum** — brief" },
	}
}

func testChecklists() []core.ChecklistRecord {
	return []core.ChecklistRecord{
		{Name: "neutral", Purpose: "always on", Steps: []core.ChecklistStep{{Text: "do"}}, Suits: []string{"developer"}, Origin: "user"},
		{Name: "scrum-backlog", Purpose: "sweep scrum", Steps: []core.ChecklistStep{{Text: "triage", Children: []core.ChecklistStep{{Text: "list"}}}}, Suits: []string{"developer"}, Requires: core.ChecklistRequires{Capabilities: []string{"scrum"}}, Origin: "shipped:scrum"},
		{Name: "qa-backlog", Purpose: "sweep qa", Steps: []core.ChecklistStep{{Text: "verify"}}, Suits: []string{"developer"}, Requires: core.ChecklistRequires{Capabilities: []string{"qa"}}, Origin: "shipped:qa"},
	}
}

func devRequest() Request {
	l, _ := session.LauncherFor("claude")
	return Request{
		Persona: "developer", Code: "ATM", ProjName: "Agent Tasks Management",
		Launcher: l, AgentName: "claude",
		RunID: "ATM-RUNID", Timestamp: "2026-09-01T00:00:00Z",
	}
}

func TestComposeDefaultSetNarrowedByCapabilityScope(t *testing.T) {
	s := testService(&fakeSvc{suited: testChecklists()})
	req := devRequest()
	req.Capability = "scrum"
	plan, err := s.Compose(req)
	if err != nil {
		t.Fatal(err)
	}
	// User decision 1: keep requires-empty and requires-contains-scope;
	// drop the rest. Order preserved from SuitedChecklists.
	if want := []string{"neutral", "scrum-backlog"}; !reflect.DeepEqual(plan.Checklists, want) {
		t.Fatalf("checklists = %v, want %v", plan.Checklists, want)
	}
}

func TestComposeNoScopeSelectsAllSuited(t *testing.T) {
	s := testService(&fakeSvc{suited: testChecklists()})
	plan, err := s.Compose(devRequest())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"neutral", "scrum-backlog", "qa-backlog"}; !reflect.DeepEqual(plan.Checklists, want) {
		t.Fatalf("checklists = %v, want %v", plan.Checklists, want)
	}
}

func TestComposeOverrideReplacesDefault(t *testing.T) {
	s := testService(&fakeSvc{suited: testChecklists()})
	req := devRequest()
	req.Capability = "scrum"
	req.Checklists = []string{"qa-backlog"}
	plan, err := s.Compose(req)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"qa-backlog"}; !reflect.DeepEqual(plan.Checklists, want) {
		t.Fatalf("checklists = %v, want %v", plan.Checklists, want)
	}
	req.Checklists = []string{"ghost"}
	if _, err := s.Compose(req); err == nil {
		t.Fatal("unknown checklist override must error")
	}
}

func TestComposeWarningsNeverBlock(t *testing.T) {
	recs := testChecklists()
	recs[0].Requires = core.ChecklistRequires{Channels: []string{"journal", "prs"}}
	f := &fakeSvc{suited: recs, channels: []core.ChannelView{
		{ChannelRecord: core.ChannelRecord{Name: "journal", Type: "slack"}}, // exists, unwired
	}}
	plan, err := testService(f).Compose(devRequest())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"checklist neutral: requires channel journal (unwired)",
		"checklist neutral: requires channel prs (none exists)",
		"checklist qa-backlog: requires capability qa (not enabled)",
	}
	if !reflect.DeepEqual(plan.Warnings, want) {
		t.Fatalf("warnings = %v, want %v", plan.Warnings, want)
	}
	if plan.Argv == nil || plan.ContextText == "" {
		t.Fatal("warnings must not block composition")
	}
}

func TestComposeModes(t *testing.T) {
	s := testService(&fakeSvc{suited: testChecklists()})

	// hook (developer): bare argv, ATM_ROLE back-compat.
	plan, err := s.Compose(devRequest())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Mode != "hook" || !reflect.DeepEqual(plan.Argv, []string{"claude"}) {
		t.Fatalf("hook plan = %+v", plan)
	}
	if plan.EnvValues["ATM_ROLE"] != "developing" {
		t.Fatalf("ATM_ROLE = %q, want developing", plan.EnvValues["ATM_ROLE"])
	}

	// prompt (manager): argv ends with the generic prompt message.
	req := devRequest()
	req.Persona = "manager"
	plan, err = s.Compose(req)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Mode != "prompt" || plan.Argv[len(plan.Argv)-1] != session.PromptMessage(plan.ContextPath) {
		t.Fatalf("prompt plan argv = %v", plan.Argv)
	}
	if plan.EnvValues["ATM_ROLE"] != "manager" {
		t.Fatalf("ATM_ROLE = %q", plan.EnvValues["ATM_ROLE"])
	}

	// tui (admin): no argv, no context.
	req = devRequest()
	req.Persona = "admin"
	plan, err = s.Compose(req)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Mode != "tui" || plan.Argv != nil {
		t.Fatalf("tui plan = %+v", plan)
	}

	// launch override: hook persona forced to prompt.
	req = devRequest()
	req.Launch = "prompt"
	plan, err = s.Compose(req)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Mode != "prompt" || plan.Argv[len(plan.Argv)-1] != session.PromptMessage(plan.ContextPath) {
		t.Fatalf("override plan argv = %v", plan.Argv)
	}
	req.Launch = "warp"
	if _, err := s.Compose(req); err == nil {
		t.Fatal("invalid launch override must error")
	}
}

func TestComposeKickoffTemplate(t *testing.T) {
	f := &fakeSvc{personaDoc: map[string]string{
		"kicked": "---\nname: kicked\ndescription: d\nkickoff: Go read <CONTEXT_FILE> for <TASK_ID> in <CODE>.\n---\nbody",
	}}
	req := devRequest()
	req.Persona = "kicked"
	req.Task = "ATM-99"
	plan, err := testService(f).Compose(req)
	if err != nil {
		t.Fatal(err)
	}
	got := plan.Argv[len(plan.Argv)-1]
	want := "Go read " + plan.ContextPath + " for ATM-99 in ATM."
	if got != want {
		t.Fatalf("kickoff message = %q, want %q", got, want)
	}
}

func TestComposeEnvChecklists(t *testing.T) {
	s := testService(&fakeSvc{suited: testChecklists()})
	plan, err := s.Compose(devRequest())
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.EnvValues["ATM_CHECKLISTS"]; got != "neutral,scrum-backlog,qa-backlog" {
		t.Fatalf("ATM_CHECKLISTS = %q", got)
	}
	for k, want := range map[string]string{
		"ATM_PROJECT":      "ATM",
		"ATM_PERSONA":      "developer",
		"ATM_AGENT":        "claude",
		"ATM_ACTOR":        "developer@claude:unset",
		"ATM_RUN_ID":       "ATM-RUNID",
		"ATM_TIMESTAMP":    "2026-09-01T00:00:00Z",
		"ATM_CONTEXT_FILE": plan.ContextPath,
	} {
		if plan.EnvValues[k] != want {
			t.Errorf("%s = %q, want %q", k, plan.EnvValues[k], want)
		}
	}
	// No checklists → no ATM_CHECKLISTS key.
	plan, err = testService(&fakeSvc{}).Compose(devRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plan.EnvValues["ATM_CHECKLISTS"]; ok {
		t.Fatal("ATM_CHECKLISTS must be absent when nothing is selected")
	}
}

func TestComposeContextDedupAndSections(t *testing.T) {
	s := testService(&fakeSvc{suited: testChecklists()})
	plan, err := s.Compose(devRequest())
	if err != nil {
		t.Fatal(err)
	}
	// The developer persona body starts with its own "# Persona: developer"
	// heading; the composed prompt must carry it exactly once.
	if n := strings.Count(plan.ContextText, "# Persona: developer"); n != 1 {
		t.Fatalf("persona header count = %d, want 1:\n%s", n, plan.ContextText)
	}
	if !strings.Contains(plan.ContextText, "## Checklist: scrum-backlog") {
		t.Fatalf("checklist section missing:\n%s", plan.ContextText)
	}
	if !strings.Contains(plan.ContextText, "   1.1 list") {
		t.Fatalf("nested numbering missing:\n%s", plan.ContextText)
	}
	if plan.ContextPath != "/store/projects/ATM/cache/session-developer.md" {
		t.Fatalf("context path = %q", plan.ContextPath)
	}
	if plan.Actor != "developer@claude:unset" {
		t.Fatalf("actor = %q", plan.Actor)
	}
}
