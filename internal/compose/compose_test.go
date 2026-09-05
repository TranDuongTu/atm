package compose

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"atm/internal/core"
	"atm/internal/profile"
	"atm/internal/session"
)

// fakeSvc implements only the core.Service methods Compose touches; the
// embedded interface panics on anything else, keeping the seam honest.
type fakeSvc struct {
	core.Service
	all      []core.ChecklistRecord // every project checklist (ChecklistRecords)
	suited   []core.ChecklistRecord
	channels []core.ChannelView
	// eligible maps a targets expression to the task IDs the resolver would
	// return for it — the store's own evaluation, stubbed.
	eligible map[string][]string
	// tasks, when set, is returned verbatim by ListTasksErr — for fixtures
	// that need real labels rather than bare IDs.
	tasks []*core.Task
	// personaRecords keys project persona records by "<CODE>/<name>", the
	// source resolution prefers.
	personaRecords map[string]*core.Persona
}

// ListTasksErr answers the targets expression the way the store's resolver
// does: whatever the fixture declares eligible for that expression.
func (f *fakeSvc) ListTasksErr(filters core.QueryFilters) ([]*core.Task, error) {
	if f.tasks != nil {
		return append([]*core.Task(nil), f.tasks...), nil
	}
	var out []*core.Task
	for _, id := range f.eligible[filters.Expr] {
		out = append(out, &core.Task{ID: id, ProjectCode: filters.Project})
	}
	return out, nil
}

func (f *fakeSvc) ChecklistRecords(code string) ([]core.ChecklistRecord, error) {
	return f.all, nil
}

// ChannelRecords/GetProjectConfig back the probe-free channel view
// DispatchOptions reads; derived from the same channels fixture.
func (f *fakeSvc) ChannelRecords(code string) ([]core.ChannelRecord, error) {
	out := make([]core.ChannelRecord, 0, len(f.channels))
	for _, v := range f.channels {
		out = append(out, v.ChannelRecord)
	}
	return out, nil
}

func (f *fakeSvc) GetProjectConfig(code string) (*core.ProjectConfig, error) {
	cfg := &core.ProjectConfig{Channels: map[string]core.ChannelWiring{}}
	for _, v := range f.channels {
		if v.Wiring != nil {
			cfg.Channels[v.Name] = *v.Wiring
		}
	}
	return cfg, nil
}

func (f *fakeSvc) StorePath() string { return "/store" }

func (f *fakeSvc) GetPersonaRecord(code, name string) (*core.Persona, error) {
	if rec, ok := f.personaRecords[code+"/"+name]; ok {
		return rec, nil
	}
	return nil, core.ErrNotFound
}

func (f *fakeSvc) SuitedChecklists(code, persona string) ([]core.ChecklistRecord, error) {
	return f.suited, nil
}

func (f *fakeSvc) GetChecklist(code, name string) (*core.ChecklistRecord, error) {
	for _, set := range [][]core.ChecklistRecord{f.all, f.suited} {
		for i := range set {
			if set[i].Name == name {
				return &set[i], nil
			}
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
	}
}

// testChecklists is the ACTION fixture: five checklists covering both
// targets, both launchable modes, a suits-less one, and an unmet capability
// requirement.
func testChecklists() []core.ChecklistRecord {
	return []core.ChecklistRecord{
		{
			Name: "planning", Purpose: "the weekly planning pass",
			Steps:  []core.ChecklistStep{{Text: "sweep the boards"}},
			Suits:  []string{"manager"},
			Target: core.ChecklistTargetProject, Mode: core.ChecklistModeEager,
			Origin: "user",
		},
		{
			Name: "scrum-coding", Purpose: "implement one increment",
			Steps:    []core.ChecklistStep{{Text: "gate", Children: []core.ChecklistStep{{Text: "read the plan"}}}},
			Suits:    []string{"developer"},
			Requires: core.ChecklistRequires{Capabilities: []string{"scrum"}},
			Target:   core.ChecklistTargetTask, Targets: "scrum:task", Mode: core.ChecklistModeEager,
			Origin: "scrumban@1.0.0",
		},
		{
			Name: "scrum-design", Purpose: "take a task to implementable",
			Steps:  []core.ChecklistStep{{Text: "brainstorm with the user"}},
			Suits:  []string{"developer"},
			Target: core.ChecklistTargetTask, Mode: core.ChecklistModeInteractive,
			Origin: "scrumban@1.0.0",
		},
		{
			Name: "qa", Purpose: "verify finished work",
			Steps:    []core.ChecklistStep{{Text: "sit in the customer's seat"}},
			Suits:    []string{"manager"},
			Requires: core.ChecklistRequires{Capabilities: []string{"qa"}},
			Target:   core.ChecklistTargetTask, Mode: core.ChecklistModeEager,
			Origin: "scrumban@1.0.0",
		},
		{
			Name: "orphan", Purpose: "suits nobody",
			Steps:  []core.ChecklistStep{{Text: "x"}},
			Target: core.ChecklistTargetProject, Mode: core.ChecklistModeEager,
			Origin: "user",
		},
	}
}

// actionRequest is a dispatch of one action — the v3 entry point. Nothing
// names a persona: it derives from the action's suits.
func actionRequest(checklist string) Request {
	l, _ := session.LauncherFor("claude")
	return Request{
		Checklist: checklist, Code: "ATM", ProjName: "Agent Tasks Management",
		Launcher: l, AgentName: "claude",
		RunID: "ATM-RUNID", Timestamp: "2026-09-01T00:00:00Z",
	}
}

func actionService() *Service {
	recs := testChecklists()
	return testService(&fakeSvc{all: recs, suited: recs})
}

// THE inversion: a dispatch names an action, and the persona follows from
// it. Nothing in the request says "developer" or "manager"; the checklist's
// own suits do.
func TestComposeDerivesThePersonaFromTheAction(t *testing.T) {
	s := actionService()
	for _, tc := range []struct{ action, task, persona string }{
		{"planning", "", "manager"},
		{"scrum-coding", "ATM-1", "developer"},
	} {
		req := actionRequest(tc.action)
		req.Task = tc.task
		plan, err := s.Compose(req)
		if err != nil {
			t.Fatalf("%s: %v", tc.action, err)
		}
		if plan.Persona != tc.persona {
			t.Errorf("%s persona = %q, want %q", tc.action, plan.Persona, tc.persona)
		}
		if plan.EnvValues["ATM_PERSONA"] != tc.persona {
			t.Errorf("%s ATM_PERSONA = %q, want %q", tc.action, plan.EnvValues["ATM_PERSONA"], tc.persona)
		}
		if !strings.Contains(plan.ContextText, "## Persona: "+tc.persona) {
			t.Errorf("%s context did not render the derived persona:\n%s", tc.action, plan.ContextText)
		}
	}
}

// The override survives, because a human occasionally means it — but it is
// the exception, not the way a dispatch is expressed.
func TestComposePersonaOverrideBeatsSuits(t *testing.T) {
	req := actionRequest("planning")
	req.Persona = "developer"
	plan, err := actionService().Compose(req)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Persona != "developer" {
		t.Fatalf("persona = %q, want the override developer", plan.Persona)
	}
}

// An action nobody is suited for cannot pick an identity for itself, and
// guessing one would put an arbitrary persona's judgment behind the work.
func TestComposeRefusesAnActionThatSuitsNoPersona(t *testing.T) {
	_, err := actionService().Compose(actionRequest("orphan"))
	if err == nil {
		t.Fatal("an action with no suits and no override must be refused")
	}
	if !errors.Is(err, core.ErrUsage) || !strings.Contains(err.Error(), "orphan") {
		t.Fatalf("error = %v, want a usage error naming the action", err)
	}
	// ...unless the dispatch says who runs it.
	req := actionRequest("orphan")
	req.Persona = "developer"
	if _, err := actionService().Compose(req); err != nil {
		t.Fatalf("an explicit persona must rescue it: %v", err)
	}
}

func TestComposeRefusesAnUnknownAction(t *testing.T) {
	if _, err := actionService().Compose(actionRequest("ghost")); err == nil {
		t.Fatal("unknown checklist must error")
	}
}

// A dispatch with neither an action nor a persona has no identity and no
// procedure — there is nothing to compose.
func TestComposeRefusesADispatchWithNeitherActionNorPersona(t *testing.T) {
	req := actionRequest("")
	_, err := actionService().Compose(req)
	if err == nil || !errors.Is(err, core.ErrUsage) {
		t.Fatalf("error = %v, want a usage error", err)
	}
}

// TARGET SHAPE is an error, not a warning: a task-target action with no task
// has nothing to work on, and a project-target action handed a task would
// silently ignore it. Neither is a launch worth starting.
func TestComposeRefusesAMismatchedTargetShape(t *testing.T) {
	s := actionService()
	if _, err := s.Compose(actionRequest("scrum-coding")); err == nil {
		t.Error("a task-target action with no --task must be refused")
	} else if !errors.Is(err, core.ErrUsage) {
		t.Errorf("error = %v, want a usage error", err)
	}
	req := actionRequest("planning")
	req.Task = "ATM-1"
	if _, err := s.Compose(req); err == nil {
		t.Error("a project-target action given a --task must be refused")
	} else if !errors.Is(err, core.ErrUsage) {
		t.Errorf("error = %v, want a usage error", err)
	}
}

// The targets EXPRESSION is the warning half of the same question (plan
// §3.7): the dialog offers only eligible tasks, so reaching this path means
// a human asked for it explicitly. Warn, and let the checklist's own gate
// step be the defense behind it.
func TestComposeTargetsMismatchWarnsAndLaunchesAnyway(t *testing.T) {
	f := &fakeSvc{all: testChecklists(), suited: testChecklists(),
		eligible: map[string][]string{"scrum:task": {"ATM-eligible"}}}
	s := testService(f)

	req := actionRequest("scrum-coding")
	req.Task = "ATM-eligible"
	plan, err := s.Compose(req)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range plan.Warnings {
		if strings.Contains(w, "outside its targets") {
			t.Fatalf("an eligible task must not warn: %q", w)
		}
	}

	req.Task = "ATM-elsewhere"
	plan, err = s.Compose(req)
	if err != nil {
		t.Fatalf("a targets mismatch must WARN, not block: %v", err)
	}
	var found bool
	for _, w := range plan.Warnings {
		if strings.Contains(w, "ATM-elsewhere") && strings.Contains(w, "scrum:task") {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %v, want one naming the task and the expression", plan.Warnings)
	}
	if plan.Argv == nil || plan.ContextText == "" {
		t.Fatal("the launch must still be composed")
	}
}

// An action with no targets expression accepts any task of the project.
func TestComposeNoTargetsExpressionAcceptsAnyTask(t *testing.T) {
	req := actionRequest("scrum-design")
	req.Task = "ATM-anything"
	plan, err := actionService().Compose(req)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range plan.Warnings {
		if strings.Contains(w, "targets") {
			t.Fatalf("no targets expression must mean no targets warning: %q", w)
		}
	}
}

// MODE is the session's autonomy and rides on the action, because how much
// rope the work needs is a property of the work.
func TestComposeModeComesFromTheAction(t *testing.T) {
	s := actionService()
	for _, tc := range []struct{ action, mode string }{
		{"scrum-coding", core.ChecklistModeEager},
		{"scrum-design", core.ChecklistModeInteractive},
	} {
		req := actionRequest(tc.action)
		req.Task = "ATM-1"
		plan, err := s.Compose(req)
		if err != nil {
			t.Fatalf("%s: %v", tc.action, err)
		}
		if plan.Mode != tc.mode {
			t.Errorf("%s mode = %q, want %q", tc.action, plan.Mode, tc.mode)
		}
		if plan.EnvValues["ATM_MODE"] != tc.mode {
			t.Errorf("%s ATM_MODE = %q, want %q", tc.action, plan.EnvValues["ATM_MODE"], tc.mode)
		}
	}
}

func TestComposeModeOverrideWins(t *testing.T) {
	req := actionRequest("scrum-coding")
	req.Task, req.Mode = "ATM-1", core.ChecklistModeInteractive
	plan, err := actionService().Compose(req)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Mode != core.ChecklistModeInteractive {
		t.Fatalf("mode = %q, want the override", plan.Mode)
	}
}

// resident is in the vocabulary so the surfaces can show it as coming.
// Refusing it HERE means no surface has to remember to.
func TestComposeRefusesResidentAndUnknownModes(t *testing.T) {
	for _, mode := range []string{core.ChecklistModeResident, "sleepy"} {
		req := actionRequest("scrum-coding")
		req.Task, req.Mode = "ATM-1", mode
		_, err := actionService().Compose(req)
		if err == nil || !errors.Is(err, core.ErrUsage) {
			t.Errorf("mode %q: error = %v, want a usage error", mode, err)
		}
	}
}

// MODE decides whether the host is handed an opening instruction; the
// kickoff is built here from the dispatch's own facts, because the persona
// does not know which action it was dispatched for.
func TestComposeEagerCarriesTheKickoffInteractiveDoesNot(t *testing.T) {
	s := actionService()
	req := actionRequest("scrum-coding")
	req.Task = "ATM-1"
	req.Persona = "manager" // a prompt-vehicle persona, so argv carries a message
	plan, err := s.Compose(req)
	if err != nil {
		t.Fatal(err)
	}
	got := plan.Argv[len(plan.Argv)-1]
	want := session.KickoffMessage(plan.ContextPath, "scrum-coding", "ATM-1")
	if got != want {
		t.Fatalf("eager kickoff = %q, want %q", got, want)
	}
	if !strings.Contains(got, "scrum-coding") || !strings.Contains(got, "ATM-1") {
		t.Fatalf("the kickoff must name the action and the task: %q", got)
	}

	// interactive: the context is rendered, and the human opens the session.
	req.Mode = core.ChecklistModeInteractive
	plan, err = s.Compose(req)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(plan.Argv, []string{"claude"}) == false {
		t.Fatalf("interactive argv = %v, want a bare launch", plan.Argv)
	}
}

// The hook VEHICLE has no message channel — its plugin loads the context at
// session start — so an eager hook session starts bare and reads its
// instruction from the file. ATM_ROLE=developing back-compat rides on the
// vehicle, untouched by the mode axis.
func TestComposeHookVehicleCarriesNoMessageAndKeepsATMRole(t *testing.T) {
	req := actionRequest("scrum-coding")
	req.Task = "ATM-1" // suits developer, whose vehicle is hook
	plan, err := actionService().Compose(req)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Vehicle != "hook" {
		t.Fatalf("vehicle = %q, want hook", plan.Vehicle)
	}
	if !reflect.DeepEqual(plan.Argv, []string{"claude"}) {
		t.Fatalf("hook argv = %v, want a bare launch", plan.Argv)
	}
	if plan.Mode != core.ChecklistModeEager {
		t.Fatalf("mode = %q; the vehicle must not rewrite the autonomy axis", plan.Mode)
	}
	if plan.EnvValues["ATM_ROLE"] != "developing" {
		t.Fatalf("ATM_ROLE = %q, want developing", plan.EnvValues["ATM_ROLE"])
	}
}

func TestComposeVehicleOverrideAndTUIRoute(t *testing.T) {
	s := actionService()

	// A tui persona composes nothing else.
	req := actionRequest("")
	req.Persona = "admin"
	plan, err := s.Compose(req)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Vehicle != "tui" || plan.Argv != nil {
		t.Fatalf("tui plan = %+v", plan)
	}

	// The vehicle override forces a hook persona onto the prompt vehicle,
	// where an eager dispatch does carry its kickoff.
	req = actionRequest("scrum-coding")
	req.Task, req.Launch = "ATM-1", "prompt"
	plan, err = s.Compose(req)
	if err != nil {
		t.Fatal(err)
	}
	want := session.KickoffMessage(plan.ContextPath, "scrum-coding", "ATM-1")
	if plan.Vehicle != "prompt" || plan.Argv[len(plan.Argv)-1] != want {
		t.Fatalf("override plan = %+v", plan)
	}
	req.Launch = "warp"
	if _, err := s.Compose(req); err == nil {
		t.Fatal("invalid vehicle override must error")
	}
}

// The ad-hoc dispatch survives: a bare persona, no action, and the context
// says so rather than rendering an empty procedure section.
func TestComposeAdHocDispatchRendersTheFallback(t *testing.T) {
	req := actionRequest("")
	req.Persona = "manager"
	plan, err := actionService().Compose(req)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Checklist != "" {
		t.Fatalf("checklist = %q, want none", plan.Checklist)
	}
	if !strings.Contains(plan.ContextText, "No checklists were selected for this session.") {
		t.Fatalf("ad-hoc context must render the fallback:\n%s", plan.ContextText)
	}
	if _, ok := plan.EnvValues["ATM_CHECKLIST"]; ok {
		t.Fatal("ATM_CHECKLIST must be absent when no action was dispatched")
	}
	if plan.Mode != core.ChecklistModeEager {
		t.Fatalf("mode = %q, want eager by default", plan.Mode)
	}
	// With no action to name, the kickoff is the plain read-your-context.
	if got, want := plan.Argv[len(plan.Argv)-1], session.PromptMessage(plan.ContextPath); got != want {
		t.Fatalf("ad-hoc kickoff = %q, want %q", got, want)
	}
}

// One action per dispatch: the context carries the checklist that was
// dispatched and no other, which is what "the operating procedure selected
// at dispatch for exactly this work" means.
func TestComposeContextCarriesOnlyTheDispatchedAction(t *testing.T) {
	req := actionRequest("scrum-coding")
	req.Task = "ATM-1"
	plan, err := actionService().Compose(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.ContextText, "## Checklist: scrum-coding") {
		t.Fatalf("dispatched action missing:\n%s", plan.ContextText)
	}
	if n := strings.Count(plan.ContextText, "## Checklist: "); n != 1 {
		t.Fatalf("checklist sections = %d, want exactly the dispatched one:\n%s", n, plan.ContextText)
	}
	if !strings.Contains(plan.ContextText, "      1.1 read the plan") && !strings.Contains(plan.ContextText, "1.1 read the plan") {
		t.Fatalf("nested step numbering missing:\n%s", plan.ContextText)
	}
}

func TestComposeEnvNamesTheActionAndTheMode(t *testing.T) {
	req := actionRequest("scrum-coding")
	req.Task = "ATM-1"
	plan, err := actionService().Compose(req)
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"ATM_CHECKLIST":    "scrum-coding",
		"ATM_MODE":         core.ChecklistModeEager,
		"ATM_PROJECT":      "ATM",
		"ATM_PERSONA":      "developer",
		"ATM_TASK":         "ATM-1",
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
	// The plural is gone with the multi-select it served.
	if _, ok := plan.EnvValues["ATM_CHECKLISTS"]; ok {
		t.Error("ATM_CHECKLISTS must not survive the checklist-first inversion")
	}
}

// Dispatch warnings and `atm profile status` answer the same question, so
// they run THE readiness computation. Injected, they also carry the
// agent-relative attestation rungs the fallback cannot see.
func TestComposeWarningsComeFromTheReadinessComputation(t *testing.T) {
	recs := testChecklists()
	s := testService(&fakeSvc{all: recs, suited: recs,
		eligible: map[string][]string{"scrum:task": {"ATM-eligible"}}})
	var gotCode string
	var gotAgents []string
	s.Readiness = func(code string, agents []string) *profile.Readiness {
		gotCode, gotAgents = code, agents
		return &profile.Readiness{Actions: []profile.ActionReadiness{{
			Name: "scrum-coding",
			Warnings: map[string][]profile.Warning{"claude": {
				{Rung: profile.RungAttested, Text: "#prs stamp is 21 days old on this agent"},
			}},
		}}}
	}
	req := actionRequest("scrum-coding")
	req.Task = "ATM-eligible"
	plan, err := s.Compose(req)
	if err != nil {
		t.Fatal(err)
	}
	if gotCode != "ATM" || !reflect.DeepEqual(gotAgents, []string{"claude"}) {
		t.Fatalf("readiness asked for (%q, %v), want (ATM, [claude]) — warnings are agent-relative", gotCode, gotAgents)
	}
	want := []string{"checklist scrum-coding: #prs stamp is 21 days old on this agent"}
	if !reflect.DeepEqual(plan.Warnings, want) {
		t.Fatalf("warnings = %v, want %v", plan.Warnings, want)
	}
	if plan.Argv == nil {
		t.Fatal("warnings must never block a launch")
	}
}

// Without the injection the answer is the same question minus the machine-
// and agent-level rungs — a Service built bare warns rather than going
// silent.
func TestComposeWarningsFallBackToTheCapabilityAndChannelEvaluation(t *testing.T) {
	recs := testChecklists()
	f := &fakeSvc{all: recs, suited: recs, channels: []core.ChannelView{
		{ChannelRecord: core.ChannelRecord{Name: "journal", Type: "slack"}}, // exists, unwired
	}}
	s := testService(f)
	s.Readiness = nil
	req := actionRequest("qa") // requires capability qa, which is not enabled
	req.Task = "ATM-1"
	plan, err := s.Compose(req)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"checklist qa: requires capability qa (not enabled)"}
	if !reflect.DeepEqual(plan.Warnings, want) {
		t.Fatalf("warnings = %v, want %v", plan.Warnings, want)
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
	req := actionRequest("scrum-coding")
	req.Task = "ATM-1"
	plan, err := actionService().Compose(req)
	if err != nil {
		t.Fatal(err)
	}
	// The developer persona body starts with its own "# Persona: developer"
	// heading; the composed prompt must carry it exactly once, DEMOTED to
	// "##" so it sits inside the context's "# Who you are" frame rather than
	// beside it.
	if n := strings.Count(plan.ContextText, "## Persona: developer"); n != 1 {
		t.Fatalf("persona header count = %d, want 1:\n%s", n, plan.ContextText)
	}
	if strings.Contains(plan.ContextText, "\n# Persona: developer") {
		t.Fatalf("the persona heading must not sit at the framing level:\n%s", plan.ContextText)
	}
	// v2 appended a sentence pointing at "the working routine below" — the
	// Orientation tail v3 removed. The template's own framing prose says it
	// now, in one place.
	if strings.Contains(plan.ContextText, "You are operating as this persona") {
		t.Fatalf("the v2 persona coda survived:\n%s", plan.ContextText)
	}
	if !strings.Contains(plan.ContextText, "## Checklist: scrum-coding") {
		t.Fatalf("checklist section missing:\n%s", plan.ContextText)
	}
	if !strings.Contains(plan.ContextText, "   1.1 read the plan") {
		t.Fatalf("nested numbering missing:\n%s", plan.ContextText)
	}
	if plan.ContextPath != "/store/projects/ATM/cache/session-developer-atm-1.md" {
		t.Fatalf("context path = %q", plan.ContextPath)
	}
	if plan.Actor != "developer@claude:unset" {
		t.Fatalf("actor = %q", plan.Actor)
	}
}

// TestComposeRendersEnabledCapabilityNames pins the injection contract PR 1
// changes: Compose feeds the context the enabled capability NAMES, taken
// from the one registry view it already had. The pre-rendered briefs block
// is gone — the context names the capabilities and points at the guide, so
// there is no second copy of a capability's own words to go stale.
func TestComposeRendersEnabledCapabilityNames(t *testing.T) {
	s := actionService()
	s.EnabledCapabilities = func(code string) []string { return []string{"scrum", "qa", "channel"} }
	req := actionRequest("scrum-coding")
	req.Task = "ATM-1"
	plan, err := s.Compose(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.ContextText, "Enabled capabilities: scrum, qa, channel") {
		t.Fatalf("enabled names not rendered:\n%s", plan.ContextText)
	}
	if strings.Contains(plan.ContextText, "## Capabilities") {
		t.Fatalf("the briefs block must not survive:\n%s", plan.ContextText)
	}
}

// TestComposeProjectlessLeavesCapabilityNamesPlaceholder: with no project
// there is no enabled set to name, and the render is a generic template —
// the placeholder stays literal, exactly as <CODE> and <ACTOR> do.
func TestComposeProjectlessLeavesCapabilityNamesPlaceholder(t *testing.T) {
	// A projectless dispatch is necessarily ad-hoc: checklists are project
	// records, so there is no action to name either.
	req := actionRequest("")
	req.Persona = "manager"
	req.Code, req.ProjName = "", ""
	plan, err := actionService().Compose(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.ContextText, "<CAPABILITY_NAMES>") {
		t.Fatalf("projectless render must leave the placeholder literal:\n%s", plan.ContextText)
	}
}
