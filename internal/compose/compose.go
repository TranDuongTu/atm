// Package compose is the single choke point for session binding
// (DispatchV2 spec §5, plan §3.7): a dispatch names an ACTION — one
// checklist — and everything else derives. The persona comes from the
// action's suits, the mode from the action's own frontmatter, the eligible
// task from its targets expression; requires are validated into warnings
// that never block; the session context is rendered and the host argv/env
// built. The CLI launcher and the TUI dispatch dialog both consume it, so
// they cannot diverge.
//
// The two axes this package keeps apart:
//
//   - MODE is the session's autonomy and is user-facing: eager (spawned with
//     a kickoff, executes immediately), interactive (context rendered, waits
//     for the human), resident (future — lives on its channels; refused at
//     launch). It rides on the checklist, because autonomy is a property of
//     the work, and is dialog-overridable.
//   - VEHICLE is how the host process is started — prompt, hook, or tui. It
//     is launcher plumbing, invisible to the user, and stays with the
//     persona/agent.
//
// Like internal/session it stays registry- and store-free: adapters inject
// the enabled capability names and the readiness computation.
package compose

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"atm/internal/core"
	"atm/internal/profile"
	"atm/internal/session"
	"atm/skills"
)

// Service composes session plans over the domain service. The funcs are
// adapter-injected views; nil behaves as empty.
type Service struct {
	Svc core.Service
	// EnabledCapabilities returns the project's enabled capability names
	// (registry-narrowed; a project with nil Capabilities means all built-ins).
	// The context file names them and points at `atm capability <name> guide`
	// rather than pasting each brief: the guide is the definition, and a
	// second copy in every rendered context is a copy that can go stale.
	EnabledCapabilities func(code string) []string
	// Readiness runs THE readiness computation (profile.ComputeReadiness) for
	// this project over the given agents. It is injected rather than called
	// directly because assembling its input is a pile of store reads, which
	// belong to the adapters.
	//
	// Dispatch warnings come from here so that a launch and `atm profile
	// status` cannot disagree about whether an action is ready: one
	// computation, several surfaces. nil falls back to the capability- and
	// channel-only evaluation in core, which is the same answer minus the
	// agent-relative attestation rungs.
	Readiness func(code string, agents []string) *profile.Readiness
}

// Request is one dispatch's binding inputs. Code/ProjName/Task arrive
// resolved and validated by the caller — launch-time project auto-create and
// task-ownership checks are CLI UX, not binding.
type Request struct {
	// Checklist is the ACTION being dispatched — the one fact a dispatch
	// starts from. Empty is the ad-hoc bare-persona dispatch, which renders
	// the no-checklist fallback and requires an explicit Persona.
	Checklist string
	Code      string // "" for project-optional personas
	ProjName  string
	Task      string
	// Persona overrides the action's suits[0]. Rare: the action names the
	// persona that runs it, and disagreeing with that is a deliberate act.
	Persona string
	// Mode overrides the action's own mode (eager|interactive). "resident"
	// is refused at launch — it is visible in the vocabulary, not runnable.
	Mode       string
	Capability string
	Launch     string // VEHICLE override; "" = the persona's default
	Actor      string // override; "" computes persona@launcher:model (or stays empty for context-only renders)

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
	Vehicle     string            // prompt | hook | tui — how the host starts
	Mode        string            // eager | interactive — the session's autonomy
	Argv        []string          // nil for tui and context-only requests
	EnvValues   map[string]string // merged over os.Environ by the caller
	ContextPath string
	ContextText string
	Checklist   string   // the resolved action; "" for an ad-hoc dispatch
	Persona     string   // the resolved persona, derived or overridden
	Warnings    []string // unmet requires and target mismatches — never blocking
	Actor       string
}

// DispatchPersona resolves the persona a dispatch will run as, from the same
// inputs and by the same rule Compose uses: the action's suits[0] unless
// overridden. The launcher needs it BEFORE composing — the vehicle decides
// whether this dispatch is a host process or the TUI — and deriving it a
// second time in the CLI is how the two would drift apart.
func (s *Service) DispatchPersona(req Request) (core.Persona, error) {
	action, err := s.resolveAction(req)
	if err != nil {
		return core.Persona{}, err
	}
	return s.resolveDispatchPersona(req, action)
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
	// Missing was the dialog's expected-but-absent rows, fed by a
	// per-capability seed interface no capability ever implemented. What a
	// project is MISSING is a question about its applied profile, not about
	// its capabilities, and `atm profile status` is where it gets answered.
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
	return opts, nil
}

// Compose computes the session binding for one dispatch: resolve the action,
// derive the persona and mode from it, validate the target, render the
// context, build argv/env.
func (s *Service) Compose(req Request) (*Plan, error) {
	action, err := s.resolveAction(req)
	if err != nil {
		return nil, err
	}
	persona, err := s.resolveDispatchPersona(req, action)
	if err != nil {
		return nil, err
	}
	vehicle := LaunchModeOf(persona)
	if req.Launch != "" {
		switch req.Launch {
		case "prompt", "hook", "tui":
			vehicle = req.Launch
		default:
			return nil, fmt.Errorf("%w: launch must be prompt, hook, or tui, got %q", core.ErrUsage, req.Launch)
		}
	}
	if vehicle == "tui" && req.Launcher != nil {
		// A tui LAUNCH ignores project/agent/task; nothing else is composed.
		// A context-only request (nil Launcher) still renders — the caller
		// asked for the context, not a session.
		return &Plan{Vehicle: "tui"}, nil
	}

	mode, err := resolveMode(req, action)
	if err != nil {
		return nil, err
	}
	if err := validateTargetShape(req, action); err != nil {
		return nil, err
	}

	var recs []core.ChecklistRecord
	name := ""
	if action != nil {
		recs, name = []core.ChecklistRecord{*action}, action.Name
	}
	warnings := s.dispatchWarnings(req, action)

	launcherName := req.AgentName
	if req.Launcher != nil {
		launcherName = req.Launcher.Name()
	}
	actor := req.Actor
	if actor == "" && launcherName != "" {
		actor = sessionActor(persona.Name, launcherName, req.Model)
	}

	capNames := ""
	if req.Code != "" && s.EnabledCapabilities != nil {
		capNames = strings.Join(s.EnabledCapabilities(req.Code), ", ")
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
		TaskID:          req.Task,
		Capability:      req.Capability,
		CapabilityNames: capNames,
		PersonaPrompt:   buildPersonaPrompt(persona, req.Code, req.ProjName, actor, req.Task),
		Checklists:      sections,
	})

	role := persona.Name
	if vehicle == "hook" {
		role = "developing"
	}
	env := sessionEnvValues(req.Code, actor, req.RunID, contextPath, launcherName, persona.Name, role, req.Capability, req.Task, req.Timestamp)
	env["ATM_MODE"] = mode
	if name != "" {
		env["ATM_CHECKLIST"] = name
	}

	var argv []string
	if req.Launcher != nil {
		var base []string
		// MODE decides whether the host is handed an opening instruction;
		// VEHICLE decides how. A hook vehicle has no message channel at all
		// — its plugin loads the context at session start — so an eager hook
		// session starts bare and reads its instruction from the file.
		if vehicle == "hook" || mode == core.ChecklistModeInteractive {
			base = req.Launcher.BuildArgv(req.Model)
		} else {
			base = req.Launcher.BuildArgvMessage(session.KickoffMessage(contextPath, name, req.Task), req.Model)
		}
		base = append(base, req.DefaultArgs...)
		argv = make([]string, 0, len(base)+len(req.EnvArgs)+len(req.ExtraArgs))
		argv = append(argv, base...)
		argv = append(argv, req.EnvArgs...)
		argv = append(argv, req.ExtraArgs...)
	}

	return &Plan{
		Vehicle:     vehicle,
		Mode:        mode,
		Argv:        argv,
		EnvValues:   env,
		ContextPath: contextPath,
		ContextText: contextText,
		Checklist:   name,
		Persona:     persona.Name,
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

// resolveAction resolves the dispatched checklist. A nil action is the
// ad-hoc bare-persona dispatch — legitimate, and rendered as the
// no-checklist fallback.
func (s *Service) resolveAction(req Request) (*core.ChecklistRecord, error) {
	if req.Checklist == "" {
		return nil, nil
	}
	if req.Code == "" {
		return nil, fmt.Errorf("%w: checklist %q needs a project — checklists are project records", core.ErrUsage, req.Checklist)
	}
	rec, err := s.Svc.GetChecklist(req.Code, req.Checklist)
	if err != nil {
		return nil, fmt.Errorf("checklist %q: %w", req.Checklist, err)
	}
	return rec, nil
}

// resolveDispatchPersona derives the persona from the action's suits, which
// is the inversion this increment is about: the action names who runs it, so
// a dispatch does not have to. An explicit override still wins — it is the
// rare deliberate case, not the entry point.
//
// An action with no suits and no override cannot be dispatched: something has
// to be the identity, and guessing one would put an arbitrary persona's
// judgment behind the work.
func (s *Service) resolveDispatchPersona(req Request, action *core.ChecklistRecord) (core.Persona, error) {
	name := req.Persona
	if name == "" {
		if action == nil {
			return core.Persona{}, fmt.Errorf("%w: a dispatch needs an action (--checklist) or a persona (--persona)", core.ErrUsage)
		}
		if len(action.Suits) == 0 {
			return core.Persona{}, fmt.Errorf("%w: checklist %q suits no persona; dispatch it with an explicit --persona", core.ErrUsage, action.Name)
		}
		name = action.Suits[0]
	}
	return resolvePersona(s.Svc, req.Code, name)
}

// resolveMode picks the session's autonomy: the override when given, else the
// action's own mode, else eager. resident is refused — it is in the
// vocabulary so the surfaces can show it as coming, and refusing it at the
// binding layer means no surface has to remember to.
func resolveMode(req Request, action *core.ChecklistRecord) (string, error) {
	mode := req.Mode
	if mode == "" && action != nil {
		mode = action.Mode
	}
	if mode == "" {
		mode = core.ChecklistModeEager
	}
	if mode == core.ChecklistModeResident {
		return "", fmt.Errorf("%w: mode resident is not launchable yet", core.ErrUsage)
	}
	if !core.ValidChecklistMode(mode) {
		return "", fmt.Errorf("%w: mode must be eager or interactive, got %q", core.ErrUsage, mode)
	}
	return mode, nil
}

// validateTargetShape refuses a dispatch whose target does not match the
// action's declared shape. This is an ERROR, not a warning: a task-target
// action with no task has nothing to work on, and a project-target action
// handed a task would silently ignore it. The `targets` EXPRESSION is the
// warning half — see targetsWarning.
func validateTargetShape(req Request, action *core.ChecklistRecord) error {
	if action == nil {
		return nil
	}
	switch action.Target {
	case core.ChecklistTargetTask:
		if req.Task == "" {
			return fmt.Errorf("%w: checklist %q targets a task; dispatch it with --task", core.ErrUsage, action.Name)
		}
	default:
		if req.Task != "" {
			return fmt.Errorf("%w: checklist %q targets the project, so it takes no --task", core.ErrUsage, action.Name)
		}
	}
	return nil
}

// dispatchWarnings collects everything wrong with this dispatch that is not
// worth refusing it over: the action's unmet requires, and a task that falls
// outside the action's targets expression.
func (s *Service) dispatchWarnings(req Request, action *core.ChecklistRecord) []string {
	if action == nil {
		return nil
	}
	out := s.requireWarnings(req, *action)
	if w := s.targetsWarning(req, *action); w != "" {
		out = append(out, w)
	}
	return out
}

// requireWarnings answers "is this action ready here?" from THE readiness
// computation when the adapter injected it, so a launch and `atm profile
// status` cannot disagree. Readiness also knows the agent-relative
// attestation rungs, which the fallback cannot see.
//
// The fallback is core's capability/channel evaluation: the same question,
// answered without the machine- and agent-level rungs. It exists so a
// Service built without the injection still warns rather than going silent.
func (s *Service) requireWarnings(req Request, rec core.ChecklistRecord) []string {
	if s.Readiness != nil {
		agent := req.AgentName
		if req.Launcher != nil {
			agent = req.Launcher.Name()
		}
		if agent != "" {
			if r := s.Readiness(req.Code, []string{agent}); r != nil {
				for _, a := range r.Actions {
					if a.Name != rec.Name {
						continue
					}
					var out []string
					for _, w := range a.Warnings[agent] {
						out = append(out, fmt.Sprintf("checklist %s: %s", rec.Name, w.Text))
					}
					return out
				}
			}
		}
	}
	var enabled []string
	if s.EnabledCapabilities != nil {
		enabled = s.EnabledCapabilities(req.Code)
	}
	var channels []core.ChannelView
	if v, err := s.Svc.ProjectChannels(req.Code); err == nil {
		channels = v
	}
	return core.ChecklistRequireWarnings(rec, enabled, channels)
}

// targetsWarning evaluates the action's targets expression against the task
// actually dispatched. A mismatch WARNS rather than refuses (plan §3.7): the
// dialog offers only eligible tasks, so reaching this path means a human
// asked for it explicitly, and the checklist's own gate step is the
// defense-in-depth behind it.
//
// The expression is evaluated by the store's resolver through the ordinary
// task query — the same evaluation the dialog's eligible-task list uses, so
// the two cannot disagree about what "eligible" means.
func (s *Service) targetsWarning(req Request, rec core.ChecklistRecord) string {
	if rec.Targets == "" || req.Task == "" {
		return ""
	}
	eligible, err := s.Svc.ListTasksErr(core.QueryFilters{Project: req.Code, Expr: rec.Targets})
	if err != nil {
		return fmt.Sprintf("checklist %s: could not evaluate targets %q: %v", rec.Name, rec.Targets, err)
	}
	for _, t := range eligible {
		if t.ID == req.Task {
			return ""
		}
	}
	return fmt.Sprintf("checklist %s: task %s is outside its targets (%s)", rec.Name, req.Task, rec.Targets)
}

// resolvePersona resolves the persona a session runs as. The PROJECT'S OWN
// RECORD WINS: a persona is that project's operating identity, so two
// projects running the same-named persona from different profiles each get
// their own text. The code-side built-ins are the fallback. The machine-
// global custom-persona store is pruned (plan §7): a persona outside a
// project is imported into it with `atm persona set --file`.
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
	return core.Persona{}, fmt.Errorf("%w: persona %q: not a project record (import one with `atm persona set --file`) and not a built-in", core.ErrNotFound, name)
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
//
// That header is "##", one level BELOW the context's three framing headers:
// the persona is the material "# Who you are" frames, not a fourth peer of
// it. A document that arrives with the "#" form is demoted here rather than
// rejected, so persona sources stay readable on their own.
//
// Nothing is appended after the body. v2 closed with a sentence pointing at
// "the working routine below" — the Orientation tail v3 replaced — and the
// template's own framing prose now says what that sentence said. One
// statement of a rule cannot drift out of step with another.
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

	// No trailing newline: the template already separates <PERSONA_PROMPT>
	// from the next framing header with a blank line, and a prompt that ends
	// in one adds a second.
	var b strings.Builder
	fmt.Fprintf(&b, "## Persona: %s\n\n%s\n\n", p.Name, p.Description)
	b.WriteString(strings.TrimRight(sub(body), "\n"))
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
