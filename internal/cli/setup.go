// internal/cli/setup.go
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"atm/internal/core"
	atmsetup "atm/internal/setup"
	"atm/internal/version"
	"atm/skills"

	"github.com/spf13/cobra"
)

// probeTimeout bounds each individual probe subprocess (one `--version` or
// one `mcp list` call). The CLI runs every tier synchronously — it is a
// one-shot command, so a slow harness costs seconds, not a stuck UI — but an
// unresponsive binary must not hang the command forever.
const probeTimeout = 10 * time.Second

// newSetupCmd is the read-only diagnostic noun: it proves internal/setup's
// model end to end with no rendering in the way, and gives a concierge
// checklist step one endpoint to call instead of describing five separate
// probes by hand.
func newSetupCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Inspect ATM's own readiness: agents, plugins, MCP servers, and (with --project) channels/checklists",
	}
	cmd.AddCommand(newSetupStatusCmd(st))
	return cmd
}

// newSetupStatusCmd probes every tier synchronously and reports it. It never
// writes to the store — setup status is diagnostic only.
func newSetupStatusCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Probe agent readiness and (with --project) channel/checklist coverage",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, _ := cmd.Flags().GetString("project")
			if project == "" {
				project = os.Getenv("ATM_PROJECT")
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			// An unknown project code is a usage error, not "not found": the
			// caller typed something wrong, the same way an unresolved
			// --project reads everywhere else in this command.
			var proj *core.Project
			if project != "" {
				proj, err = s.GetProject(project)
				if err != nil {
					return fmt.Errorf("%w: unknown project %q", ErrUsage, project)
				}
			}
			cfg, err := s.GetAgentsConfig()
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home dir: %w", err)
			}
			probes := atmsetup.Probes{LookPath: st.lookPath, Run: execRun, Home: home, Now: core.Now}
			m := atmsetup.Instant(cfg, probes)
			m.ATMVersion = version.Version

			// Version and MCP servers cost a subprocess each (1.6-3s), which is
			// exactly why Instant leaves them out — but a one-shot CLI command
			// can afford to pay for every tier before it prints anything.
			servers := make(map[string][]atmsetup.MCPServer, len(m.Agents))
			states := make(map[string]atmsetup.Fact, len(m.Agents))
			for i := range m.Agents {
				row := &m.Agents[i]
				vctx, vcancel := context.WithTimeout(cmd.Context(), probeTimeout)
				row.Version = atmsetup.ProbeVersion(vctx, row.Agent, execRun)
				vcancel()
				mctx, mcancel := context.WithTimeout(cmd.Context(), probeTimeout)
				srv, state := atmsetup.ProbeMCP(mctx, row.Agent, execRun)
				mcancel()
				servers[row.Agent] = srv
				states[row.Agent] = state
				row.MCPState = state
				for _, sv := range srv {
					row.MCPServers = append(row.MCPServers, sv.Name)
				}
			}

			var ps *atmsetup.ProjectSetup
			if proj != nil {
				views, err := s.ProjectChannels(project)
				if err != nil {
					return err
				}
				ps = atmsetup.BuildProject(project, views, servers, states, m.ProbedAt)
				records, err := s.ChecklistRecords(project)
				if err != nil {
					return err
				}
				ps.Personas = atmsetup.BuildPersonas(personaNames(s.ListPersonas()), records, skills.ChecklistSeeds())
				ps.ChecklistCapEnabled = projectCapabilityEnabled(proj, "checklist")
			}
			atmsetup.Fill(&m, ps)
			m.Project = ps

			return st.emit(st.stdout(), setupModelToJSON(m), func() {
				writeSetupText(st.stdout(), m)
			})
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	return cmd
}

// personaNames flattens the persona catalog to the names BuildPersonas wants.
func personaNames(ps []*core.Persona) []string {
	names := make([]string, len(ps))
	for i, p := range ps {
		names[i] = p.Name
	}
	return names
}

// projectCapabilityEnabled mirrors requireChannelCapability/
// requireChecklistCapability's rule (a nil Capabilities list is a legacy
// project: every built-in reads as enabled) but returns a bool instead of an
// error — setup status reports the fact, it does not gate on it.
func projectCapabilityEnabled(p *core.Project, name string) bool {
	if p.Capabilities == nil {
		return true
	}
	for _, n := range p.Capabilities {
		if n == name {
			return true
		}
	}
	return false
}

// ---- JSON shape ----

type jsonSetupAgent struct {
	Agent       string   `json:"agent"`
	Glyph       string   `json:"glyph"`
	Version     string   `json:"version,omitempty"`
	Binary      string   `json:"binary"`
	Plugin      string   `json:"plugin"`
	NativeOK    string   `json:"native_ok"`
	OllamaOK    string   `json:"ollama_ok"`
	Model       string   `json:"model,omitempty"`
	IsDefault   bool     `json:"is_default"`
	DefaultVia  string   `json:"default_via,omitempty"`
	MCPServers  []string `json:"mcp_servers"`
	MCPState    string   `json:"mcp_state"`
	ChannelsOK  int      `json:"channels_ok"`
	ChannelsAll int      `json:"channels_all"`
	// UpdateArgv is the harness's own update verb, offered so a caller can run
	// it — never a claim that a newer version exists, which nothing available
	// here can know.
	UpdateArgv []string `json:"update_argv,omitempty"`
}

func setupAgentToJSON(r atmsetup.AgentRow) jsonSetupAgent {
	out := jsonSetupAgent{
		Agent:       r.Agent,
		Glyph:       r.Glyph(),
		Version:     r.Version,
		Binary:      r.Binary.String(),
		Plugin:      r.Plugin.String(),
		NativeOK:    r.NativeOK.String(),
		OllamaOK:    r.OllamaOK.String(),
		Model:       r.Model,
		IsDefault:   r.IsDefault,
		DefaultVia:  r.DefaultVia,
		MCPServers:  normalizeStrSlice(r.MCPServers),
		MCPState:    r.MCPState.String(),
		ChannelsOK:  r.ChannelsOK,
		ChannelsAll: r.ChannelsAll,
	}
	if argv, ok := atmsetup.UpdateArgv(r.Agent); ok {
		out.UpdateArgv = argv
	}
	return out
}

type jsonSetupChannel struct {
	Name      string            `json:"name"`
	Type      string            `json:"type"`
	Glyph     string            `json:"glyph"`
	Note      string            `json:"note"`
	MCPServer string            `json:"mcp_server,omitempty"`
	PerAgent  map[string]string `json:"per_agent"`
}

func setupChannelToJSON(c atmsetup.ChannelRow) jsonSetupChannel {
	perAgent := make(map[string]string, len(c.PerAgent))
	for k, v := range c.PerAgent {
		perAgent[k] = v.String()
	}
	return jsonSetupChannel{
		Name:      c.Name,
		Type:      c.Type,
		Glyph:     c.Glyph,
		Note:      c.Note,
		MCPServer: c.MCPServer,
		PerAgent:  perAgent,
	}
}

type jsonSetupPersona struct {
	Persona         string   `json:"persona"`
	Checklists      int      `json:"checklists"`
	Steps           int      `json:"steps"`
	StartersSeeded  int      `json:"starters_seeded"`
	StartersTotal   int      `json:"starters_total"`
	MissingStarters []string `json:"missing_starters"`
	Customised      []string `json:"customised"`
}

func setupPersonaToJSON(p atmsetup.PersonaRow) jsonSetupPersona {
	return jsonSetupPersona{
		Persona:         p.Persona,
		Checklists:      p.Checklists,
		Steps:           p.Steps,
		StartersSeeded:  p.StartersSeeded,
		StartersTotal:   p.StartersTotal,
		MissingStarters: normalizeStrSlice(p.MissingStarters),
		Customised:      normalizeStrSlice(p.Customised),
	}
}

type jsonSetupProject struct {
	Code                string             `json:"code"`
	Channels            []jsonSetupChannel `json:"channels"`
	Personas            []jsonSetupPersona `json:"personas"`
	ChecklistCapEnabled bool               `json:"checklist_cap_enabled"`
}

// setupModelToJSON builds the emit payload as a plain map so a nil Project
// simply omits the "project" key — never null, never an empty object. A
// struct field can only ever be present-with-a-value or present-as-null;
// only a map can leave the key out entirely.
func setupModelToJSON(m atmsetup.Model) map[string]any {
	agents := make([]jsonSetupAgent, 0, len(m.Agents))
	for _, r := range m.Agents {
		agents = append(agents, setupAgentToJSON(r))
	}
	payload := map[string]any{
		"atm_version": m.ATMVersion,
		"ollama":      m.Ollama.String(),
		"agents":      agents,
		"probed_at":   core.RFC3339UTC(m.ProbedAt),
	}
	if m.Project != nil {
		channels := make([]jsonSetupChannel, 0, len(m.Project.Channels))
		for _, c := range m.Project.Channels {
			channels = append(channels, setupChannelToJSON(c))
		}
		personas := make([]jsonSetupPersona, 0, len(m.Project.Personas))
		for _, p := range m.Project.Personas {
			personas = append(personas, setupPersonaToJSON(p))
		}
		payload["project"] = jsonSetupProject{
			Code:                m.Project.Code,
			Channels:            channels,
			Personas:            personas,
			ChecklistCapEnabled: m.Project.ChecklistCapEnabled,
		}
	}
	return payload
}

// ---- text rendering ----

// writeSetupText renders the same picture as JSON, by eye: one row per
// agent, then (with a project) one row per channel and one per persona. It
// never prints "update available" — only the installed version and the
// harness's own update verb, which is all a probe can honestly know.
func writeSetupText(w io.Writer, m atmsetup.Model) {
	fmt.Fprintf(w, "atm %s\n", m.ATMVersion)
	fmt.Fprintf(w, "ollama binary: %s\n\n", m.Ollama)

	const agentFormat = "%-2s%-9s %-6s %-10s %-8s %-8s %-8s %-14s %-8s %s"
	line := func(cells ...any) {
		fmt.Fprintln(w, strings.TrimRight(fmt.Sprintf(agentFormat, cells...), " "))
	}
	line("", "AGENT", "GLYPH", "VERSION", "BINARY", "PLUGIN", "MCP", "MODEL", "CHANNELS", "UPDATE")
	for _, r := range m.Agents {
		star := " "
		if r.IsDefault {
			star = "*"
		}
		version := r.Version
		if version == "" {
			version = "-"
		}
		model := r.Model
		if model == "" {
			model = "-"
		}
		channels := "-"
		if r.ChannelsAll > 0 {
			channels = fmt.Sprintf("%d/%d", r.ChannelsOK, r.ChannelsAll)
		}
		update := "-"
		if argv, ok := atmsetup.UpdateArgv(r.Agent); ok {
			update = strings.Join(argv, " ")
		}
		line(star, r.Agent, r.Glyph(), version, r.Binary.String(), r.Plugin.String(), r.MCPState.String(), model, channels, update)
	}
	fmt.Fprintln(w, "* = default agent;  glyph: ● ready ◐ fixable here ○ fix is outside ATM")

	ps := m.Project
	if ps == nil {
		return
	}
	fmt.Fprintf(w, "\nproject %s (checklist capability: %s)\n", ps.Code, capEnabledNote(ps.ChecklistCapEnabled))
	fmt.Fprintln(w, "CHANNEL\tTYPE\tSTATUS\tNOTE\tMCP SERVER")
	for _, c := range ps.Channels {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", c.Name, c.Type, c.Glyph, c.Note, c.MCPServer)
	}
	fmt.Fprintln(w, "\nPERSONA\tCHECKLISTS\tSTEPS\tSTARTERS")
	for _, p := range ps.Personas {
		fmt.Fprintf(w, "%s\t%d\t%d\t%d/%d\n", p.Persona, p.Checklists, p.Steps, p.StartersSeeded, p.StartersTotal)
		if len(p.MissingStarters) > 0 {
			fmt.Fprintf(w, "  missing starters: %s\n", strings.Join(p.MissingStarters, ", "))
		}
	}
}

func capEnabledNote(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}
