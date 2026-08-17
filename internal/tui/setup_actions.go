package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"atm/internal/agent"
	"atm/internal/core"
	"atm/internal/developing"
	"atm/internal/dispatch"
	atmsetup "atm/internal/setup"
	"atm/skills"

	tea "github.com/charmbracelet/bubbletea"
)

// The fix ladder: what the wizard can repair from the row it is reporting on.
// Every fix belongs to exactly one of two classes, and they must not blur:
//
//	DIRECT   — ATM performs it. It may shell out (a harness's own `mcp add`,
//	           codex's plugin registration), but it starts NO agent session.
//	SPAWNED  — handed to dispatch.Service, because it is interactive or long
//	           running: `mcp login`, a harness update, a concierge session.
//
// The line matters because of the bootstrap paradox: on an empty store no
// agent is ready, so every action that makes the FIRST agent ready must work
// with no agent at all. That is why `mcp add` is direct and `mcp login` is
// spawned, and why only the concierge class is ever gated.

// conciergeActions are the actions that run INSIDE an agent session. They are
// the only gated ones — see actionGated.
var conciergeActions = map[string]bool{"interview": true, "author": true}

// actionGated reports whether an action must wait for a ready agent. ONLY
// concierge dispatches are gated: every direct action has to work on an
// empty store, or making the first agent ready would require an agent.
func (s *setupModel) actionGated(action string) bool {
	if !conciergeActions[action] {
		return false
	}
	// Glyph() is the single readiness authority (see setupAnyReady); readiness
	// is never re-derived from the individual Fact fields here.
	return !setupAnyReady(s.model.Agents)
}

// action runs the fix bound to key. The ladder is section-scoped because the
// cursor is: in AGENTS a row names a harness, in CHANNELS a channel, in
// PERSONAS the project's checklist accounting. So the same key can mean
// different things in different sections (`s` stamps a channel and seeds
// starters), exactly as the workspace panes already work. An unbound key is a
// no-op, never an error toast — the wizard is a place to look around in.
func (s *setupModel) action(key string) tea.Cmd {
	// The concierge is reachable from every section: it is a session about the
	// whole setup, not about the row under the cursor.
	if key == "c" {
		s.concierge()
		return nil
	}
	switch s.section {
	case setupSectionChannels:
		return s.channelAction(key)
	case setupSectionPersonas:
		return s.personaAction(key)
	default:
		return s.agentAction(key)
	}
}

// agentAction runs the AGENTS ladder against the row under the cursor.
func (s *setupModel) agentAction(key string) tea.Cmd {
	name := s.currentAgent()
	if name == "" {
		return nil
	}
	switch key {
	case "i":
		s.installPlugin(name)
	case "d":
		s.makeDefault(name)
	case "m":
		s.openModelForm(name)
	case "a":
		// Only when something was actually added: what an agent's `mcp list`
		// reports is a TIER-2 fact, so the columns can only tell the truth
		// again once the probe has re-run — but re-probing after a refusal
		// would contradict the toast that just asked for exactly that.
		if s.mcpAdd(name) {
			return s.refresh()
		}
	case "l":
		s.mcpLogin(name)
	case "u":
		s.updateAgent(name)
	}
	return nil
}

// channelAction runs the CHANNELS ladder against the row under the cursor.
func (s *setupModel) channelAction(key string) tea.Cmd {
	ch, ok := s.currentChannel()
	if !ok {
		return nil
	}
	switch key {
	case "w":
		s.openWireForm(ch)
	case "s":
		s.stampChannel(ch.Name)
	}
	return nil
}

// personaAction runs the PERSONAS ladder. Both fixes are section-level rather
// than row-level: with the capability off the section has NO rows, and that is
// precisely the state [e] exists to leave.
func (s *setupModel) personaAction(key string) tea.Cmd {
	switch key {
	case "e":
		s.enableChecklists()
	case "s":
		s.seedStarters(s.m.projectScope)
	}
	return nil
}

// actionHints is the focused section's half of the footer. A ladder nobody
// can see is not a ladder: the keys differ per section (see action), so the
// hint has to as well, and the renderer draws this line above its own
// navigation one. The `c` hint is listed everywhere because the concierge is.
func (s *setupModel) actionHints() string {
	switch s.section {
	case setupSectionChannels:
		return "[w]wire  [s]stamp  [c]concierge"
	case setupSectionPersonas:
		return "[e]enable  [s]seed starters  [c]concierge"
	default:
		return "[i]plugin  [d]default  [m]model  [a]mcp add  [l]login  [u]update  [c]concierge"
	}
}

// currentAgent is the harness under the cursor, or "" when the AGENTS section
// does not have focus.
func (s *setupModel) currentAgent() string {
	if s.section != setupSectionAgents || s.cursor < 0 || s.cursor >= len(s.model.Agents) {
		return ""
	}
	return s.model.Agents[s.cursor].Agent
}

// currentChannel is the channel under the cursor. The bool is false when the
// section is empty — a project with no channels yet has nothing to stamp.
func (s *setupModel) currentChannel() (atmsetup.ChannelRow, bool) {
	if s.model.Project == nil || s.cursor < 0 || s.cursor >= len(s.model.Project.Channels) {
		return atmsetup.ChannelRow{}, false
	}
	return s.model.Project.Channels[s.cursor], true
}

// currentPersona is the persona row under the cursor. The bool is false when
// the section is empty — with the checklist capability off there is nothing
// to account for, so there is nothing to detail either.
func (s *setupModel) currentPersona() (atmsetup.PersonaRow, bool) {
	if s.model.Project == nil || s.cursor < 0 || s.cursor >= len(s.model.Project.Personas) {
		return atmsetup.PersonaRow{}, false
	}
	return s.model.Project.Personas[s.cursor], true
}

// rowFor returns the snapshot row for a harness.
func (s *setupModel) rowFor(agentName string) (atmsetup.AgentRow, bool) {
	for _, r := range s.model.Agents {
		if r.Agent == agentName {
			return r, true
		}
	}
	return atmsetup.AgentRow{}, false
}

// --- direct actions ---

// installPlugin installs ATM's harness plugin for one agent, through the same
// developing.InstallPlugin path `atm agents plugin install` uses. DIRECT: it
// may shell out (codex registers its plugin through its own CLI) but it starts
// no agent session and asks the user nothing — which is what makes it usable
// on an empty store, where it is the action that ends the bootstrap.
func (s *setupModel) installPlugin(agentName string) {
	home, err := os.UserHomeDir()
	if err != nil {
		s.m.showToast("resolve home dir: " + err.Error())
		return
	}
	res, err := developing.InstallPlugin(agentName, home, false)
	if err != nil {
		s.m.showToast("install " + agentName + " plugin: " + err.Error())
		return
	}
	// Re-read rather than patch the row: what the wizard shows next frame is
	// what is now on disk, including anything else that changed with it.
	s.reload()
	msg := fmt.Sprintf("%s plugin installed (%d files)", agentName, len(res.Files))
	// ATM can install its own plugin for a harness that is not installed at
	// all; it cannot install the harness. Saying so is the difference between
	// the ○ grade (the fix is outside ATM) and a row the user thinks is fixed.
	if row, ok := s.rowFor(agentName); ok && row.Binary != atmsetup.FactPresent {
		msg += " — the " + agentName + " binary itself is still missing"
	}
	s.m.showToast(msg)
}

// makeDefault records agentName as the selection ATM launches with. It re-reads
// agents.json first: a CLI in another terminal may have selected this SAME
// agent through ollama, and writing the native key back from the wizard's
// snapshot would silently demote it. When the stored selection names some
// other agent there is no launcher to preserve, so the launcher comes from the
// row's own facts — ollama only when the native binary is genuinely absent,
// since `ollama launch <agent> --` is then the only way to start it.
func (s *setupModel) makeDefault(agentName string) {
	cfg, err := s.m.store.GetAgentsConfig()
	if err != nil {
		s.m.showToast("read agents config: " + err.Error())
		return
	}
	key := agentName
	if sel, perr := agent.ParseSelectionKey(cfg.Selected); perr == nil && sel.Agent == agentName {
		key = sel.Key()
	} else if row, ok := s.rowFor(agentName); ok && row.NativeOK != atmsetup.FactPresent && row.OllamaOK == atmsetup.FactPresent {
		key = agent.Selection{Agent: agentName, Launcher: agent.LauncherOllama}.Key()
	}
	if err := s.m.store.SetSelectedAgent(key, s.m.actor); err != nil {
		s.m.showToast("select " + key + ": " + err.Error())
		return
	}
	s.reload()
	s.m.showToast("default agent: " + key)
}

// mcpAdd configures, through the harness's OWN `mcp add` verb, every server the
// scoped project's channels need that this agent does not already have. DIRECT:
// `mcp add` writes configuration and prompts for nothing — the credential
// prompt belongs to `mcp login`, which is why that one is spawned instead. It
// reports whether it wrote anything, so the caller knows whether the tier-2
// facts on screen are now stale.
func (s *setupModel) mcpAdd(agentName string) bool {
	adapter, row, ok := s.mcpTarget(agentName)
	if !ok {
		return false
	}
	missing := s.missingServers(row)
	if len(missing) == 0 {
		s.m.showToast(agentName + " already has every server this project's channels name")
		return false
	}
	var added []string
	for _, r := range missing {
		ctx, cancel := context.WithTimeout(context.Background(), setupProbeTimeout)
		_, err := s.run(ctx, agentName, adapter.AddArgv(r)...)
		cancel()
		if err != nil {
			// Whatever landed before the failure still landed; say what was
			// added rather than reporting the whole action as a no-op.
			s.m.showToast("add " + r.Server + " to " + agentName + ": " + err.Error())
			return len(added) > 0
		}
		added = append(added, r.Server)
	}
	s.m.showToast(agentName + ": added " + strings.Join(added, ", ") + " — press [l] to authorize")
	return true
}

// mcpTarget resolves the adapter and the snapshot row for an MCP action, and
// reports the reasons there is nothing to do. The unknown-state check is the
// cardinal rule applied to a WRITE: a probe that could not answer is unknown,
// not empty, and acting on a list ATM never managed to read would "repair" a
// configuration that may be perfectly fine.
func (s *setupModel) mcpTarget(agentName string) (atmsetup.MCPAdapter, atmsetup.AgentRow, bool) {
	adapter, ok := atmsetup.MCPAdapterFor(agentName)
	if !ok {
		s.m.showToast(agentName + " has no mcp adapter")
		return nil, atmsetup.AgentRow{}, false
	}
	row, ok := s.rowFor(agentName)
	if !ok {
		return nil, atmsetup.AgentRow{}, false
	}
	// The project comes first: with none selected there is no work to name, so
	// saying that is more useful than reporting the state of a probe whose
	// answer nothing would have used.
	if s.model.Project == nil {
		s.m.showToast("no project selected — a project's channels name the servers")
		return nil, atmsetup.AgentRow{}, false
	}
	if row.MCPState != atmsetup.FactPresent {
		s.m.showToast(agentName + ": mcp state is unknown — press [r] to probe again")
		return nil, atmsetup.AgentRow{}, false
	}
	return adapter, row, true
}

// missingServers is the recipes for servers this project's channels need and
// this agent has not configured at all. Configured-but-disconnected is NOT
// missing — that is what login fixes — so it is decided against the names the
// harness reported, not against the connected fact.
func (s *setupModel) missingServers(row atmsetup.AgentRow) []atmsetup.Recipe {
	have := map[string]bool{}
	for _, name := range row.MCPServers {
		have[name] = true
	}
	seen := map[string]bool{}
	var out []atmsetup.Recipe
	for _, ch := range s.model.Project.Channels {
		r, ok := atmsetup.RecipeFor(ch.Type)
		if !ok {
			continue // e.g. a repo channel: wired by path, not by an MCP server
		}
		// The channel's own resolved server name wins over the recipe's: a
		// channel wired against a differently-named server was configured that
		// way on purpose (see BuildProject).
		if ch.MCPServer != "" {
			r.Server = ch.MCPServer
		}
		if have[r.Server] || seen[r.Server] {
			continue
		}
		seen[r.Server] = true
		out = append(out, r)
	}
	return out
}

// stampChannel records "someone actually touched this and vouches" against the
// channel under the cursor. DIRECT: one store write, no agent anywhere. The
// store's AddChannelStamp is itself a locked read-modify-write, so a stamp
// added from another terminal in the meantime survives this one.
func (s *setupModel) stampChannel(name string) {
	code := s.m.projectScope
	if code == "" {
		s.m.showToast("select a project first")
		return
	}
	if err := s.m.store.AddChannelStamp(code, name, "verified from the setup wizard", s.m.actor); err != nil {
		s.m.showToast("stamp " + name + ": " + err.Error())
		return
	}
	s.reload()
	s.m.showToast("stamped " + name)
}

// enableChecklists turns the checklist capability on for the scoped project.
// DIRECT: it is the affordance the PERSONAS section already offers by name,
// and until it is on there is nothing to account for. The capability name is
// the same literal checklistEnabled reads, so the two cannot drift.
func (s *setupModel) enableChecklists() {
	code := s.m.projectScope
	if code == "" {
		s.m.showToast("select a project first")
		return
	}
	if err := s.m.store.EnableProjectCapability(code, "checklist", s.m.actor); err != nil {
		s.m.showToast("enable checklists: " + err.Error())
		return
	}
	s.reload()
	s.m.showToast("checklists enabled for " + code)
}

// seedStarters authors the shipped starter checklists this project does not
// have. It adds ONLY what is absent: a seeded starter is MEANT to be edited
// afterwards — setup.BuildPersonas calls an edited one Customised, which is
// informational and never actionable — so an existing record is left exactly
// as it is, whatever its steps now say. DIRECT: store writes only, which
// matters because the starters are how a concierge session knows what to do.
func (s *setupModel) seedStarters(code string) {
	if code == "" {
		s.m.showToast("select a project first")
		return
	}
	// Re-read the capability rather than trusting the snapshot: a CLI can have
	// disabled it since the wizard was opened, and `atm checklist` would then
	// refuse to work with what the wizard had just written.
	if !s.checklistEnabled(code) {
		s.m.showToast("checklists are off for " + code + " — press [e] first")
		return
	}
	created := 0
	for _, seed := range skills.ChecklistSeeds() {
		// A fresh read per starter, not the snapshot's MissingStarters: the
		// model is only as new as the last reload, and CreateChecklist refuses
		// a duplicate — so a starter authored elsewhere since would abort the
		// whole seed.
		_, err := s.m.store.GetChecklist(code, seed.Persona, seed.Name)
		if err == nil {
			continue
		}
		if !errors.Is(err, core.ErrNotFound) {
			s.m.showToast("read checklists: " + err.Error())
			return
		}
		steps := make([]string, len(seed.Steps))
		for i, step := range seed.Steps {
			steps[i] = setupSubCode(step, code)
		}
		rec := core.ChecklistRecord{
			Persona: seed.Persona,
			Name:    seed.Name,
			Purpose: setupSubCode(seed.Purpose, code),
			Steps:   steps,
		}
		if _, err := s.m.store.CreateChecklist(code, rec, s.m.actor); err != nil {
			s.m.showToast("seed " + seed.Persona + "/" + seed.Name + ": " + err.Error())
			return
		}
		created++
	}
	s.reload()
	s.m.showToast(fmt.Sprintf("seeded %d starter checklist(s) for %s", created, code))
}

// openModelForm asks which model this agent's selection should launch with.
// Still DIRECT despite the prompt: the value is typed into ATM's own form and
// the write is ATM's own — no agent session is started to collect it, so this
// works on an empty store like every other direct fix. The value is typed
// rather than picked because claude and codex ship no model-list verb at all
// (agent.ErrNoLister), and offering a list ATM cannot build for two of the
// three harnesses would be a picker that lies about what it knows.
func (s *setupModel) openModelForm(agentName string) {
	row, _ := s.rowFor(agentName)
	f := NewForm("Model · "+agentName, []formField{
		{Label: "model", Value: row.Model, Hint: "empty = the harness picks its own default"},
	})
	f.SetWidth(FormWidth(s.m.width))
	s.m.form = f
	s.m.formKind = formSetupAgentModel
	s.formTarget = agentName
}

// doSetModel lands the model form. setModelFor owns the write, including the
// re-read that resolves the SELECTION key (which depends on the launcher the
// store currently uses), so the form adds no store logic of its own.
func (s *setupModel) doSetModel(vals map[string]string) tea.Cmd {
	s.setModelFor(s.formTarget, vals["model"])
	return nil
}

// openWireForm asks where this machine reaches a repo channel. The path is
// deliberately NOT defaulted to the working directory: a wrong wiring reads as
// a healthy channel everywhere else in ATM, and the directory the TUI happened
// to start in is a guess. A notion channel has no path — it is reached through
// an MCP server — so it says so instead of collecting a value it cannot use.
func (s *setupModel) openWireForm(ch atmsetup.ChannelRow) {
	if ch.Type != core.ChannelTypeRepo {
		s.m.showToast(ch.Name + " is a " + ch.Type + " channel — it is wired by its MCP server, not a path")
		return
	}
	f := NewForm("Wire · "+ch.Name, []formField{
		{Label: "path", Required: true, Hint: "path to this channel's clone on THIS machine"},
	})
	f.SetWidth(FormWidth(s.m.width))
	s.m.form = f
	s.m.formKind = formSetupChannelWire
	s.formTarget = ch.Name
}

// doWire lands the wire form. DIRECT: one store write, no agent. The store
// resolves the path, refuses one that is not an existing directory, and MERGES
// rather than replaces — so an mcp_server or a stamp recorded elsewhere
// survives this write.
func (s *setupModel) doWire(vals map[string]string) tea.Cmd {
	code := s.m.projectScope
	if code == "" {
		s.m.showToast("select a project first")
		return nil
	}
	if err := s.m.store.SetChannelWiring(code, s.formTarget, vals["path"], "", s.m.actor); err != nil {
		s.m.showToast("wire " + s.formTarget + ": " + err.Error())
		return nil
	}
	s.reload()
	s.m.showToast("wired " + s.formTarget)
	return nil
}

// setupSubCode resolves the <CODE> placeholder the shipped starters carry,
// mirroring `atm checklist seed` — a step telling the user to run a command
// with a literal <CODE> in it is not a step anyone can follow.
func setupSubCode(s, code string) string { return strings.ReplaceAll(s, "<CODE>", code) }

// --- spawned actions ---

// mcpLogin starts the harness's own auth flow for a server this agent has
// configured but not authorized. SPAWNED: the harness prompts for the
// credential — ATM never sees or stores it — so it needs a terminal of its own.
func (s *setupModel) mcpLogin(agentName string) {
	adapter, _, ok := s.mcpTarget(agentName)
	if !ok {
		return
	}
	connected := map[string]atmsetup.Fact{}
	for _, sv := range s.servers[agentName] {
		connected[sv.Name] = sv.Connected
	}
	unconfigured := ""
	for _, r := range s.neededServers() {
		state, configured := connected[r.Server]
		if !configured {
			if unconfigured == "" {
				unconfigured = r.Server
			}
			continue
		}
		// FactUnknown counts as "worth logging in": codex's list reports
		// configuration but not health, and offering the fix is honest where
		// claiming it is already authorized would not be.
		if state != atmsetup.FactPresent {
			s.runSpawnAction(agentName, adapter.LoginArgv(r.Server))
			return
		}
	}
	if unconfigured != "" {
		s.m.showToast(agentName + " has not added " + unconfigured + " yet — press [a] first")
		return
	}
	s.m.showToast(agentName + " is authorized for every server this project's channels name")
}

// neededServers is the recipes for every MCP server the scoped project's
// channels name, deduplicated. Callers must have gone through mcpTarget, which
// is what guarantees a project is selected.
func (s *setupModel) neededServers() []atmsetup.Recipe {
	seen := map[string]bool{}
	var out []atmsetup.Recipe
	for _, ch := range s.model.Project.Channels {
		r, ok := atmsetup.RecipeFor(ch.Type)
		if !ok {
			continue
		}
		if ch.MCPServer != "" {
			r.Server = ch.MCPServer
		}
		if seen[r.Server] {
			continue
		}
		seen[r.Server] = true
		out = append(out, r)
	}
	return out
}

// updateAgent runs the harness's own update verb. SPAWNED: an update is long
// running and can prompt. Offering it is NOT a claim that a newer version
// exists — nothing the wizard can see knows that (see setup.ProbeVersion).
func (s *setupModel) updateAgent(agentName string) {
	argv, ok := atmsetup.UpdateArgv(agentName)
	if !ok {
		s.m.showToast(agentName + " has no update verb — it is installed out of band")
		return
	}
	s.runSpawnAction(agentName, argv)
}

// concierge hands off to the dispatch dialog for a guided session, scoped to
// whatever the user is looking at. This is the ONE class of action that waits
// for a ready agent, and the toast has to say so in terms of the fix: the
// bootstrap out of "nothing is ready" is [i], never this.
func (s *setupModel) concierge() {
	if s.actionGated(s.conciergeAction()) {
		s.m.showToast("no agent is ready yet — press [i] on an agent row to install its plugin")
		return
	}
	capability := ""
	if s.m.projectScope != "" {
		// A capability scope only means something with a project to scope it
		// to; --capability without --project would render a session context
		// naming a project that was never chosen.
		switch s.section {
		case setupSectionChannels:
			capability = "channel"
		case setupSectionPersonas:
			capability = "checklist"
		}
	}
	// concierge is project-optional, so this works with no project selected —
	// which is the empty-store case the wizard opens in.
	s.m.dispatchDlg.open("concierge", s.m.projectScope, "", "", capability)
}

// conciergeAction names which concierge session the current section wants.
// Both names appear in conciergeActions: the gate is about the session, not
// about which question it will ask.
func (s *setupModel) conciergeAction() string {
	if s.section == setupSectionPersonas {
		return "author"
	}
	return "interview"
}

// setupShellCommand renders argv as a command a stranded user can PASTE. Both
// obvious answers are wrong for that job: dispatch.ShellCommand quotes every
// argument ('claude' 'mcp' 'login' 'notion'), which is noise to read past on
// the overwhelmingly common case, while a plain join breaks outright on the
// names harnesses really return — `claude mcp list` reports servers such as
// "claude.ai Google Drive" (Task 3's parser splits on the first ": " precisely
// to preserve them), and joined bare that becomes three arguments, not one.
// So: quote only what a shell would otherwise mangle.
func setupShellCommand(argv []string) string {
	out := make([]string, len(argv))
	for i, a := range argv {
		out[i] = setupShellArg(a)
	}
	return strings.Join(out, " ")
}

// setupShellArgUnsafe are the characters that make an argument need quoting:
// whitespace, both quotes, and the POSIX shell metacharacters. The set is
// deliberately generous — quoting something that did not need it is cosmetic,
// while missing one produces a command that does something ELSE when pasted.
const setupShellArgUnsafe = " \t\n\r'\"\\$`&|;<>()*?[]{}~!#"

func setupShellArg(a string) string {
	if a != "" && !strings.ContainsAny(a, setupShellArgUnsafe) {
		return a
	}
	// Single quotes, so nothing inside is expanded; an embedded single quote
	// uses the '\'' idiom (the same one dispatch.ShellCommand uses).
	return "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
}

// runSpawnAction hands an interactive or long-running command to the same
// dispatcher the D dialog uses. When there is no dispatch target — no herdr,
// no tmux, no terminal_cmd, which is plausibly the first-run case — it shows
// the literal command rather than dead-ending.
func (s *setupModel) runSpawnAction(binary string, args []string) {
	argv := append([]string{binary}, args...)
	cmdline := setupShellCommand(argv)
	// os.Getwd, mirroring the dispatch dialog (dispatch.go). There is no repo
	// override here: these commands configure the HARNESS, not a checkout, so
	// the directory is incidental to them.
	dir, err := os.Getwd()
	if err != nil {
		s.m.showToast("error: " + err.Error())
		return
	}
	// A build without dispatch is the same dead end as no target, and must not
	// be a nil dereference.
	if s.m.dispatcher == nil {
		s.m.showToast("no dispatch target — run: " + cmdline)
		return
	}
	if err := s.m.dispatcher.Spawn(dispatch.Spec{Title: cmdline, Argv: argv, Dir: dir}); err != nil {
		s.m.showToast("no dispatch target — run: " + cmdline)
		return
	}
	s.m.showToast("dispatched: " + cmdline)
}
