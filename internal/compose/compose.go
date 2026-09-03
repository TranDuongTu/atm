// Package compose is the single choke point for session binding
// (DispatchV2 spec §5): it resolves a persona's dispatch defaults, computes
// the suits-based default checklist set narrowed by capability scope,
// applies overrides, validates requires (warn, never block), renders the
// session context, and builds the host argv/env. The CLI launcher and the
// TUI dispatch dialog both consume it, so they cannot diverge. Like
// internal/session it stays registry-free: adapters inject the enabled
// capability names and the pre-rendered capabilities block.
package compose

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"atm/internal/core"
	"atm/internal/session"
	"atm/skills"
)

// Service composes session plans over the domain service. The two funcs are
// adapter-injected registry views; nil behaves as empty.
type Service struct {
	Svc core.Service
	// EnabledCapabilities returns the project's enabled capability names
	// (registry-narrowed; a project with nil Capabilities means all built-ins).
	EnabledCapabilities func(code string) []string
	// CapabilitiesBlock returns the pre-rendered ## Capabilities block.
	CapabilitiesBlock func(code string) string
	// ExpectedChecklists returns the checklist names the project's enabled
	// capabilities ship as seeds (registry view; empty until unit 4 ships
	// seeds). nil behaves as empty.
	ExpectedChecklists func(code string) []string
}

// Request is one dispatch's binding inputs. Code/ProjName/Task arrive
// resolved and validated by the caller — launch-time project auto-create and
// task-ownership checks are CLI UX, not binding.
type Request struct {
	Persona    string
	Code       string // "" for project-optional personas
	ProjName   string
	Task       string
	Capability string
	Checklists []string // override; nil = computed default set
	Launch     string   // override; "" = persona default
	Actor      string   // override; "" computes persona@launcher:model (or stays empty for context-only renders)

	Launcher    session.Launcher // nil = context-only (no argv)
	Model       string
	AgentName   string // launcher name for the actor stamp when Launcher is nil
	DefaultArgs []string
	EnvArgs     []string
	ExtraArgs   []string
	RunID       string
	Timestamp   string
}

// Plan is the composed session binding.
type Plan struct {
	Mode        string            // prompt | hook | tui
	Argv        []string          // nil for tui and context-only requests
	EnvValues   map[string]string // merged over os.Environ by the caller
	ContextPath string
	ContextText string
	Checklists  []string // resolved names, selection order
	Warnings    []string // unmet requires — never blocking
	Actor       string
}

// ResolvePersona resolves a persona for a project: the project's own record
// when it has one, else the code-side built-in. Callers use it for
// pre-binding decisions before composing the full plan.
func (s *Service) ResolvePersona(code, name string) (core.Persona, error) {
	return resolvePersona(s.Svc, code, name)
}

// ChecklistOption is one row of the dispatch dialog's checklist multi-select.
type ChecklistOption struct {
	Name    string
	Purpose string
	Default bool // member of the compose-computed default set (suited ∩ scope)
	// Warnings are the unmet requires — the row greys with these reasons
	// but stays selectable (warn never block, spec decision 4).
	Warnings []string
}

// DispatchOptions is the dialog-facing view of one persona's dispatch
// defaults: launch mode, every selectable checklist row, and the
// expected-but-absent shipped checklists.
type DispatchOptions struct {
	Launch string // persona's default launch mode (concrete: prompt|hook|tui)
	// Rows lists ALL project checklists in store order — any persona can run
	// any checklist (spec decision 3); Default marks the pre-checked set.
	Rows []ChecklistOption
	// Missing are enabled capabilities' seed names with no project record:
	// the dialog's expected-but-absent warning rows.
	Missing []string
}

// DispatchOptions computes the dialog-facing dispatch defaults for one
// persona/project/scope. The dialog reads THIS and nothing else, so it
// cannot diverge from what Compose will do at launch.
func (s *Service) DispatchOptions(persona, code, capability string) (*DispatchOptions, error) {
	p, err := resolvePersona(s.Svc, code, persona)
	if err != nil {
		return nil, err
	}
	opts := &DispatchOptions{Launch: LaunchModeOf(p)}
	if code == "" {
		return opts, nil
	}
	recs, err := s.Svc.ChecklistRecords(code)
	if err != nil {
		return nil, err
	}
	suited, err := s.Svc.SuitedChecklists(code, persona)
	if err != nil {
		return nil, err
	}
	defaults := map[string]bool{}
	for _, r := range core.DefaultChecklistSet(suited, capability) {
		defaults[r.Name] = true
	}
	var enabled []string
	if s.EnabledCapabilities != nil {
		enabled = s.EnabledCapabilities(code)
	}
	channels := s.channelViewsUnprobed(code)
	have := map[string]bool{}
	for _, r := range recs {
		have[r.Name] = true
		opts.Rows = append(opts.Rows, ChecklistOption{
			Name:     r.Name,
			Purpose:  r.Purpose,
			Default:  defaults[r.Name],
			Warnings: core.ChecklistRequireWarnings(r, enabled, channels),
		})
	}
	if s.ExpectedChecklists != nil {
		for _, name := range s.ExpectedChecklists(code) {
			if !have[name] {
				opts.Missing = append(opts.Missing, name)
			}
		}
	}
	return opts, nil
}

// Compose computes the session binding for one dispatch.
func (s *Service) Compose(req Request) (*Plan, error) {
	persona, err := resolvePersona(s.Svc, req.Code, req.Persona)
	if err != nil {
		return nil, err
	}
	mode := LaunchModeOf(persona)
	if req.Launch != "" {
		switch req.Launch {
		case "prompt", "hook", "tui":
			mode = req.Launch
		default:
			return nil, fmt.Errorf("%w: launch must be prompt, hook, or tui, got %q", core.ErrUsage, req.Launch)
		}
	}
	if mode == "tui" && req.Launcher != nil {
		// A tui LAUNCH ignores project/agent/task; nothing else is composed.
		// A context-only request (nil Launcher) still renders — the caller
		// asked for the context, not a session.
		return &Plan{Mode: "tui"}, nil
	}

	recs, err := s.selectChecklists(req)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(recs))
	for i, r := range recs {
		names[i] = r.Name
	}
	warnings := s.requireWarnings(req.Code, recs)

	launcherName := req.AgentName
	if req.Launcher != nil {
		launcherName = req.Launcher.Name()
	}
	actor := req.Actor
	if actor == "" && launcherName != "" {
		actor = sessionActor(persona.Name, launcherName, req.Model)
	}

	capBlock := ""
	if req.Code != "" && s.CapabilitiesBlock != nil {
		capBlock = s.CapabilitiesBlock(req.Code)
	}
	contextPath := contextCachePath(s.Svc.StorePath(), req.Code, persona.Name, req.Task, req.Capability)
	sections := make([]session.ChecklistSection, len(recs))
	for i, r := range recs {
		sections[i] = session.ChecklistSection{
			Name:          r.Name,
			Purpose:       r.Purpose,
			StepsRendered: core.RenderChecklistSteps(r.Steps),
		}
	}
	contextText := session.RenderContext(session.ContextData{
		Code: req.Code, Name: req.ProjName, Actor: actor,
		TaskID:        req.Task,
		Capability:    req.Capability,
		Capabilities:  capBlock,
		PersonaPrompt: buildPersonaPrompt(persona, req.Code, req.ProjName, actor, req.Task),
		Checklists:    sections,
	})

	role := persona.Name
	if mode == "hook" {
		role = "developing"
	}
	env := sessionEnvValues(req.Code, actor, req.RunID, contextPath, launcherName, persona.Name, role, req.Capability, req.Task, req.Timestamp)
	if len(names) > 0 {
		env["ATM_CHECKLISTS"] = strings.Join(names, ",")
	}

	var argv []string
	if req.Launcher != nil {
		var base []string
		if mode == "hook" {
			base = req.Launcher.BuildArgv(req.Model)
		} else {
			// One generic kickoff for every persona. A per-persona
			// kickoff template was identity carrying dispatch plumbing —
			// no built-in ever set one, and increment 8 replaces the whole
			// idea with a Compose-built message driven by the checklist.
			base = req.Launcher.BuildArgvMessage(session.PromptMessage(contextPath), req.Model)
		}
		base = append(base, req.DefaultArgs...)
		argv = make([]string, 0, len(base)+len(req.EnvArgs)+len(req.ExtraArgs))
		argv = append(argv, base...)
		argv = append(argv, req.EnvArgs...)
		argv = append(argv, req.ExtraArgs...)
	}

	return &Plan{
		Mode:        mode,
		Argv:        argv,
		EnvValues:   env,
		ContextPath: contextPath,
		ContextText: contextText,
		Checklists:  names,
		Warnings:    warnings,
		Actor:       actor,
	}, nil
}

// channelViewsUnprobed joins channel records with this machine's wiring
// WITHOUT running repo probes — the requires evaluation only reads
// existence and Wiring, and DispatchOptions runs on a dialog keypress where
// a per-repo git probe is not acceptable (the launch path keeps the fully
// probed ProjectChannels read).
func (s *Service) channelViewsUnprobed(code string) []core.ChannelView {
	recs, err := s.Svc.ChannelRecords(code)
	if err != nil {
		return nil
	}
	var wirings map[string]core.ChannelWiring
	if cfg, err := s.Svc.GetProjectConfig(code); err == nil && cfg != nil {
		wirings = cfg.Channels
	}
	out := make([]core.ChannelView, 0, len(recs))
	for _, rec := range recs {
		v := core.ChannelView{ChannelRecord: rec}
		if w, ok := wirings[rec.Name]; ok {
			wc := w
			v.Wiring = &wc
		}
		out = append(out, v)
	}
	return out
}

// selectChecklists resolves the dispatch's checklist set: an explicit
// override resolves exactly those names; otherwise the default set is every
// suited checklist, narrowed by capability scope (core.DefaultChecklistSet
// — the rule itself is domain logic and lives in core).
func (s *Service) selectChecklists(req Request) ([]core.ChecklistRecord, error) {
	if req.Code == "" {
		return nil, nil
	}
	if req.Checklists != nil {
		out := make([]core.ChecklistRecord, 0, len(req.Checklists))
		for _, name := range req.Checklists {
			r, err := s.Svc.GetChecklist(req.Code, name)
			if err != nil {
				return nil, fmt.Errorf("checklist %q: %w", name, err)
			}
			out = append(out, *r)
		}
		return out, nil
	}
	recs, err := s.Svc.SuitedChecklists(req.Code, req.Persona)
	if err != nil {
		return nil, err
	}
	return core.DefaultChecklistSet(recs, req.Capability), nil
}

// requireWarnings gathers the inputs (enabled capabilities via the injected
// registry view, channels via the port) and delegates the evaluation to
// core.ChecklistRequireWarnings. Warnings never block a launch.
func (s *Service) requireWarnings(code string, recs []core.ChecklistRecord) []string {
	var enabled []string
	if s.EnabledCapabilities != nil {
		enabled = s.EnabledCapabilities(code)
	}
	var channels []core.ChannelView
	if v, err := s.Svc.ProjectChannels(code); err == nil {
		channels = v
	}
	var out []string
	for _, r := range recs {
		out = append(out, core.ChecklistRequireWarnings(r, enabled, channels)...)
	}
	return out
}

// resolvePersona resolves the persona a session runs as. The PROJECT'S OWN
// RECORD WINS: a persona is that project's operating identity, so two
// projects running the same-named persona from different profiles each get
// their own text. The code-side built-ins are the fallback, and the
// machine-global custom personas behind them are what the second half of
// ATM-207ab8 retires.
func resolvePersona(s core.Service, code, name string) (core.Persona, error) {
	if code != "" {
		if rec, err := s.GetPersonaRecord(code, name); err == nil {
			return *rec, nil
		} else if !errors.Is(err, core.ErrNotFound) {
			return core.Persona{}, err
		}
	}
	if spec, ok := skills.Persona(name); ok {
		return core.Persona{
			Name: spec.Name, Description: spec.Description, Prompt: spec.Body,
			// Launch and ProjectOptional are the last dispatch plumbing
			// still riding on persona content. They are carried, not
			// honoured as identity: a document that declares a vehicle
			// keeps deciding it, and project records — which declare none —
			// fall through to LaunchVehicle.
			Launch:          spec.Launch,
			ProjectOptional: spec.ProjectOptional,
			Origin:          "builtin",
		}, nil
	}
	doc, err := s.PersonaDoc(name)
	if err != nil {
		return core.Persona{}, err
	}
	spec, err := skills.ParsePersona(name, []byte(doc))
	if err != nil {
		return core.Persona{}, fmt.Errorf("%w: stored persona %q: %v", core.ErrUsage, name, err)
	}
	return core.Persona{
		Name: spec.Name, Description: spec.Description, Prompt: spec.Body,
		Launch: spec.Launch, ProjectOptional: spec.ProjectOptional, Origin: "user",
	}, nil
}

// LaunchModeOf is the vehicle a session for this persona actually starts
// with: what the persona document declares, when it declares one, else the
// code-side default for the name. Project records declare nothing, so they
// take the default — and a document that has always chosen its own vehicle
// keeps choosing it.
func LaunchModeOf(p core.Persona) string {
	if p.Launch != "" {
		return p.Launch
	}
	return LaunchVehicle(p.Name)
}

// LaunchVehicle is how a session for this persona is STARTED — prompt (an
// initial message points at the rendered context), hook (a session-start
// plugin loads it), or tui (the interactive surface).
//
// Vehicle is launcher plumbing, not identity, so it lives in code rather
// than in persona content: a project rewording its developer persona must
// not accidentally change how sessions boot. The table is deliberately
// small and deliberately temporary — the user-facing half of this
// (eager/interactive autonomy) becomes the checklist mode axis, and this
// function goes with it.
func LaunchVehicle(persona string) string {
	switch persona {
	case "admin":
		return "tui"
	case "developer":
		return "hook"
	default:
		return "prompt"
	}
}

// buildPersonaPrompt renders a persona's prompt text with context params
// substituted. The prompt's own leading "# Persona: <name>" heading is
// stripped — this function writes the one header the context carries.
func buildPersonaPrompt(p core.Persona, code, name, actor, taskID string) string {
	sub := func(s string) string {
		r := strings.NewReplacer(
			"<CODE>", code,
			"<PROJECT_NAME>", name,
			"<ACTOR>", actor,
			"<TASK_ID>", taskID,
		)
		return r.Replace(s)
	}

	body := p.Prompt
	if rest, ok := strings.CutPrefix(body, "# Persona: "+p.Name+"\n"); ok {
		body = strings.TrimLeft(rest, "\n")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Persona: %s\n\n%s\n\n", p.Name, p.Description)
	b.WriteString(sub(body))
	b.WriteString("\nYou are operating as this persona. Hold to its principles throughout the session, alongside repo instructions and the working routine below.\n")
	return b.String()
}

// sessionActor renders the actor the session stamps its ledger writes with.
// The middle segment is the LAUNCHER (ollama for ollama-launched harnesses),
// matching the pre-existing convention. An empty model yields :unset, which
// is honest: ATM does not know the harness's own default.
func sessionActor(persona, launcher, model string) string {
	if model == "" {
		model = "unset"
	}
	return persona + "@" + launcher + ":" + model
}

// sessionEnvValues builds the env map handed to the host agent. ATM_ROLE is
// "developing" for hook launches (back-compat with installed session-start
// hooks that gate on it) and the persona name otherwise. ATM_CAPABILITY and
// ATM_TASK are omitted when empty.
func sessionEnvValues(project, actor, runID, contextPath, agentName, persona, role, capability, task, timestamp string) map[string]string {
	m := map[string]string{
		"ATM_ROLE":         role,
		"ATM_PROJECT":      project,
		"ATM_ACTOR":        actor,
		"ATM_RUN_ID":       runID,
		"ATM_TIMESTAMP":    timestamp,
		"ATM_CONTEXT_FILE": contextPath,
		"ATM_AGENT":        agentName,
		"ATM_PERSONA":      persona,
	}
	if capability != "" {
		m["ATM_CAPABILITY"] = capability
	}
	if task != "" {
		m["ATM_TASK"] = task
	}
	return m
}

// contextCachePath returns the stable on-disk path for a rendered session
// prompt keyed on (persona, task, capability). Repeated launches of the same
// tuple reuse the same file. With no project (project-optional personas), the
// file lives in the store-level cache dir.
func contextCachePath(storePath, code, persona, task, capability string) string {
	key := cacheKey(persona, task, capability)
	if code == "" {
		return filepath.Join(storePath, "cache", key+".md")
	}
	return filepath.Join(storePath, "projects", code, "cache", key+".md")
}

// cacheKey builds the filename stem: session-<persona>[-<task>][-<capability>].
// Non-alphanumeric characters collapse to a single "-"; the result is
// lowercased and trimmed of leading/trailing "-".
func cacheKey(persona, task, capability string) string {
	parts := []string{"session", persona}
	if task != "" {
		parts = append(parts, task)
	}
	if capability != "" {
		parts = append(parts, capability)
	}
	for i, p := range parts {
		parts[i] = sanitizeCacheSegment(p)
	}
	return strings.Join(parts, "-")
}

// sanitizeCacheSegment lowercases and collapses non-alphanumeric runs to "-".
func sanitizeCacheSegment(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	prevDash := true // suppress leading "-"
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else {
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if out == "" {
		return "x"
	}
	return out
}
