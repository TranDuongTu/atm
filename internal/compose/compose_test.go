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
	all        []core.ChecklistRecord // every project checklist (ChecklistRecords)
	suited     []core.ChecklistRecord
	channels   []core.ChannelView
	personaDoc map[string]string
}

func (f *fakeSvc) ChecklistRecords(code string) ([]core.ChecklistRecord, error) {
	return f.all, nil
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

// optionsFake builds the DispatchOptions fixture: four project checklists,
// three suited to developer, scrum enabled.
func optionsFake() *fakeSvc {
	all := []core.ChecklistRecord{
		{Name: "dev-cycle", Purpose: "one task's flow", Suits: []string{"developer"}, Origin: "user"},
		{Name: "scrum-sweep", Suits: []string{"developer"}, Requires: core.ChecklistRequires{Capabilities: []string{"scrum"}}, Origin: "shipped:scrum"},
		{Name: "qa-sweep", Suits: []string{"developer"}, Requires: core.ChecklistRequires{Capabilities: []string{"qa"}}, Origin: "shipped:qa"},
		{Name: "pr-shape", Suits: []string{"manager"}, Origin: "user"},
	}
	return &fakeSvc{all: all, suited: all[:3]}
}

func TestDispatchOptionsRowsDefaultsWarnings(t *testing.T) {
	opts, err := testService(optionsFake()).DispatchOptions("developer", "ATM", "scrum")
	if err != nil {
		t.Fatal(err)
	}
	if opts.Launch != "hook" {
		t.Fatalf("launch = %q, want hook", opts.Launch)
	}
	var names []string
	defaults := map[string]bool{}
	for _, r := range opts.Rows {
		names = append(names, r.Name)
		defaults[r.Name] = r.Default
	}
	if want := []string{"dev-cycle", "scrum-sweep", "qa-sweep", "pr-shape"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("rows = %v, want %v", names, want)
	}
	if !defaults["dev-cycle"] || !defaults["scrum-sweep"] || defaults["qa-sweep"] || defaults["pr-shape"] {
		t.Fatalf("defaults = %v, want dev-cycle+scrum-sweep only", defaults)
	}
	if want := []string{"checklist qa-sweep: requires capability qa (not enabled)"}; !reflect.DeepEqual(opts.Rows[2].Warnings, want) {
		t.Fatalf("qa-sweep warnings = %v, want %v", opts.Rows[2].Warnings, want)
	}
	for _, i := range []int{0, 1, 3} {
		if opts.Rows[i].Warnings != nil {
			t.Fatalf("row %s warnings = %v, want none", opts.Rows[i].Name, opts.Rows[i].Warnings)
		}
	}
	if opts.Rows[0].Purpose != "one task's flow" {
		t.Fatalf("purpose = %q", opts.Rows[0].Purpose)
	}
}

func TestDispatchOptionsUnscopedKeepsAllSuited(t *testing.T) {
	opts, err := testService(optionsFake()).DispatchOptions("developer", "ATM", "")
	if err != nil {
		t.Fatal(err)
	}
	// Unscoped: every suited checklist is default — the unmet-requires
	// qa-sweep included (requires WARN, they never gate selection).
	for i, want := range []bool{true, true, true, false} {
		if opts.Rows[i].Default != want {
			t.Fatalf("row %s default = %v, want %v", opts.Rows[i].Name, opts.Rows[i].Default, want)
		}
	}
}

func TestDispatchOptionsMissing(t *testing.T) {
	s := testService(optionsFake())
	s.ExpectedChecklists = func(code string) []string { return []string{"qa-backlog", "dev-cycle"} }
	opts, err := s.DispatchOptions("developer", "ATM", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"qa-backlog"}; !reflect.DeepEqual(opts.Missing, want) {
		t.Fatalf("missing = %v, want %v", opts.Missing, want)
	}
	s.ExpectedChecklists = nil
	opts, err = s.DispatchOptions("developer", "ATM", "")
	if err != nil {
		t.Fatal(err)
	}
	if opts.Missing != nil {
		t.Fatalf("missing = %v, want nil without an injected view", opts.Missing)
	}
}

func TestDispatchOptionsNoProject(t *testing.T) {
	opts, err := testService(optionsFake()).DispatchOptions("developer", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if opts.Rows != nil || opts.Missing != nil || opts.Launch != "hook" {
		t.Fatalf("no-project options = %+v", opts)
	}
}

func TestDispatchOptionsTUIPersona(t *testing.T) {
	// Rows are still computed for a tui persona: the dialog may override
	// the launch back to a session mode and needs the checklist set.
	opts, err := testService(optionsFake()).DispatchOptions("admin", "ATM", "")
	if err != nil {
		t.Fatal(err)
	}
	if opts.Launch != "tui" || len(opts.Rows) != 4 {
		t.Fatalf("admin options = %+v", opts)
	}
}

func TestDispatchOptionsUnknownPersona(t *testing.T) {
	if _, err := testService(optionsFake()).DispatchOptions("ghost", "ATM", ""); err == nil {
		t.Fatal("unknown persona must error")
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
