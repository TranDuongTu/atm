// internal/cli/profile_status.go
package cli

import (
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"

	"atm/internal/agent"
	"atm/internal/core"
	"atm/internal/profile"

	"github.com/spf13/cobra"
)

// configuredAgentKeys lists the agent selections this machine knows about
// (agents.json): the selected one and every one with stored args or a
// model — never every conceivable harness. Sorted, deduplicated.
func configuredAgentKeys(cfg core.AgentsConfig) []string {
	seen := map[string]bool{}
	var out []string
	add := func(k string) {
		if k == "" || seen[k] {
			return
		}
		if _, err := agent.ParseSelectionKey(k); err != nil {
			return
		}
		seen[k] = true
		out = append(out, k)
	}
	add(cfg.Selected)
	for k := range cfg.Args {
		add(k)
	}
	for k := range cfg.Models {
		add(k)
	}
	sort.Strings(out)
	return out
}

// attestingSegment maps a selection key to the actor segment its sessions
// record stamps under. sessionActor composes persona@LAUNCHER:model, so a
// native claude session attests as "claude" while an ollama-launched one —
// whatever harness it hosts — attests as "ollama". The matrix must key on
// what stamps actually say, not on the harness the launcher hosts.
func attestingSegment(key string) string {
	if sel, err := agent.ParseSelectionKey(key); err == nil {
		if sel.Launcher == agent.LauncherOllama {
			return "ollama"
		}
		return sel.Agent
	}
	// A bare "ollama" (ATM_AGENT inside an ollama-launched session) is a
	// legitimate segment — it is what those sessions' stamps record.
	return key
}

// readinessFor gathers the readiness inputs from the store and runs the
// ONE computation. agents are harness names.
func readinessFor(s core.Service, code string, agents []string) (*profile.Readiness, error) {
	proj, err := s.GetProject(code)
	if err != nil {
		return nil, err
	}
	in := profile.ReadinessInput{Code: code, Agents: agents, Now: core.Now()}
	in.Current.Enabled = proj.Capabilities
	if in.Current.Personas, err = s.PersonaRecords(code); err != nil {
		return nil, err
	}
	if in.Current.Checklists, err = s.ChecklistRecords(code); err != nil {
		return nil, err
	}
	if in.Current.Channels, err = s.ChannelRecords(code); err != nil {
		return nil, err
	}
	if in.Channels, err = s.ProjectChannels(code); err != nil {
		return nil, err
	}
	if in.Available, err = s.ListProfiles(); err != nil {
		return nil, err
	}
	in.Profile = func(ref string) *core.Profile {
		o, err := core.ParseOrigin(ref)
		if err != nil || o.Kind != core.OriginProfile {
			return nil
		}
		p, _, err := s.GetProfile(o.Profile, o.Version)
		if err != nil {
			return nil
		}
		return p.ForProject(code)
	}
	return profile.ComputeReadiness(in), nil
}

func newProfileStatusCmd(st *cliState) *cobra.Command {
	var agentFlag, strict string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Readiness of the project's applied profiles on this machine, per agent",
		Long: "Readiness is a property of (profile × project × machine × agent), read " +
			"as a ladder: valid → applied → addressed (every channel an action needs " +
			"has an endpoint with an address) → wired (this machine reaches it) → " +
			"attested (this agent actually touched it recently). Status shows each " +
			"applied profile's sync state, the endpoint × agent attestation matrix " +
			"over the agents configured here, and every action's rung with the " +
			"exact command that lifts it. Semantics are advisory — warn, never block " +
			"— and --strict <rung> exits non-zero below that rung, for CI.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			code, err := profileProject(cmd)
			if err != nil {
				return err
			}
			if strict != "" && profile.RungIndex(strict) < 0 {
				return fmt.Errorf("%w: --strict must be one of %s", core.ErrUsage, strings.Join(profile.Rungs, ", "))
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			cfg, err := s.GetAgentsConfig()
			if err != nil {
				return err
			}
			keys := configuredAgentKeys(cfg)
			var agents []string
			for _, k := range keys {
				if h := attestingSegment(k); !slices.Contains(agents, h) {
					agents = append(agents, h)
				}
			}
			// The readiness table is agent-relative: --agent (or ATM_AGENT)
			// narrows it to one; otherwise every configured agent shows.
			selected := agents
			if a := agentFlag; a != "" || os.Getenv("ATM_AGENT") != "" {
				if a == "" {
					a = os.Getenv("ATM_AGENT")
				}
				h := attestingSegment(a)
				if !slices.Contains(agents, h) {
					agents = append(agents, h)
				}
				selected = []string{h}
			}
			r, err := readinessFor(s, code, agents)
			if err != nil {
				return err
			}
			if err := st.emit(st.stdout(), map[string]any{"readiness": r, "selected_agents": selected}, func() {
				renderReadiness(st.stdout(), r, selected)
			}); err != nil {
				return err
			}
			if strict != "" {
				var below []string
				for _, a := range r.Actions {
					for _, ag := range selected {
						if profile.RungIndex(a.Rung[ag]) < profile.RungIndex(strict) {
							below = append(below, a.Name+" ("+ag+": "+a.Rung[ag]+")")
						}
					}
					if len(selected) == 0 && profile.RungIndex(a.Rung[""]) < profile.RungIndex(strict) {
						below = append(below, a.Name+" ("+a.Rung[""]+")")
					}
				}
				if len(below) > 0 {
					return fmt.Errorf("%d action(s) below %s: %s", len(below), strict, strings.Join(below, ", "))
				}
			}
			return nil
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().StringVar(&agentFlag, "agent", "", "show readiness for one agent (default: every configured agent, or ATM_AGENT)")
	cmd.Flags().StringVar(&strict, "strict", "", "exit non-zero when any action is below this rung: "+strings.Join(profile.Rungs, "|"))
	return cmd
}

func renderReadiness(w io.Writer, r *profile.Readiness, selected []string) {
	fmt.Fprintf(w, "profile status — %s\n\n", r.Code)
	fmt.Fprintln(w, "profiles")
	if len(r.Profiles) == 0 {
		fmt.Fprintf(w, "  none applied — atm profile apply --project %s --name <profile>\n", r.Code)
	}
	for _, p := range r.Profiles {
		o, _ := core.ParseOrigin(p.Ref)
		switch {
		case !p.Available:
			fmt.Fprintf(w, "  %s\tnot installed here\t%d record(s) unverifiable\n", p.Ref, len(p.Records))
		default:
			line := fmt.Sprintf("  %s\t%d in sync, %d modified, %d missing", p.Ref, p.InSync, p.Modified, p.Missing)
			if p.Latest != "" {
				line += fmt.Sprintf("\tnewer available: %s — atm profile apply --project %s --name %s", p.Latest, r.Code, o.Profile)
			}
			fmt.Fprintln(w, line)
		}
		for _, rec := range p.Records {
			if rec.State == "modified" || rec.State == "missing" {
				fmt.Fprintf(w, "    %s %s: %s\n", rec.Kind, rec.Name, rec.State)
			}
		}
	}

	fmt.Fprintf(w, "\nendpoints × agents")
	if len(r.Agents) == 0 {
		fmt.Fprintln(w, " (no agent configured — atm agents select <name>)")
	} else {
		fmt.Fprintf(w, " (configured: %s)\n", strings.Join(r.Agents, ", "))
	}
	if len(r.Matrix) == 0 {
		fmt.Fprintln(w, "  no endpoint yet — atm channel endpoint add")
	} else {
		head := []string{"  channel", "type", "role", "addressed", "wired"}
		head = append(head, r.Agents...)
		fmt.Fprintln(w, strings.Join(head, "\t"))
		for _, row := range r.Matrix {
			cells := []string{"  " + row.Channel, row.Type, row.Role, yesNo(row.Addressed), row.Wiring}
			if !row.Wired {
				cells[4] = "no"
			}
			for _, a := range r.Agents {
				cells = append(cells, attestationCell(row.Agents[a]))
			}
			fmt.Fprintln(w, strings.Join(cells, "\t"))
		}
	}

	cols := selected
	if len(cols) == 0 {
		cols = []string{""}
	}
	for _, ag := range cols {
		if ag == "" {
			fmt.Fprintln(w, "\nreadiness (no agent to attest)")
		} else {
			fmt.Fprintf(w, "\nreadiness for %s\n", ag)
		}
		if len(r.Actions) == 0 {
			fmt.Fprintln(w, "  no checklists — nothing to dispatch")
			continue
		}
		fmt.Fprintln(w, "  action\tpersona\trung\tnext")
		below := 0
		for _, a := range r.Actions {
			rung := a.Rung[ag]
			next := ""
			if ws := a.Warnings[ag]; len(ws) > 0 {
				next = ws[0].Text
				if ws[0].Command != "" {
					next += " → " + ws[0].Command
				}
				if len(ws) > 1 {
					next += fmt.Sprintf(" (+%d more)", len(ws)-1)
				}
			}
			if rung != profile.RungAttested {
				below++
			}
			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n", a.Name, a.Persona, rung, next)
		}
		switch {
		case ag == "":
			fmt.Fprintf(w, "  ready: no — nothing can attest until an agent is configured\n")
		case below == 0:
			fmt.Fprintf(w, "  ready: yes — every action attested for %s\n", ag)
		default:
			fmt.Fprintf(w, "  ready: no — %d of %d action(s) below attested for %s\n", below, len(r.Actions), ag)
		}
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func attestationCell(a profile.Attestation) string {
	switch a.State {
	case profile.AttestFresh:
		return fmt.Sprintf("%s %dd", a.Kind, a.Days)
	case profile.AttestStale:
		return fmt.Sprintf("%s %dd (stale)", a.Kind, a.Days)
	default:
		if a.At != "" {
			return fmt.Sprintf("%dd (expired)", a.Days)
		}
		return "—"
	}
}

// newProfileVerifyCmd dispatches the attest checklist on each configured
// agent: verification is agentic by nature, because reaching notion or
// slack takes the agent's own tools and auth, which ATM never holds.
func newProfileVerifyCmd(st *cliState) *cobra.Command {
	var agentFlag string
	var all, dryRun bool
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Dispatch the attest checklist to refresh this machine's attestation stamps",
		Long: "Runs the profile's attest action as the manager on one agent (--agent, " +
			"else the selected one) or on every configured agent (--all-agents), " +
			"one session after another. The session reaches each endpoint read-only, " +
			"refreshes its stamps, and reports what it could not reach and why. " +
			"Then `atm profile status` shows only the real gaps.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			code, err := profileProject(cmd)
			if err != nil {
				return err
			}
			if all && agentFlag != "" {
				return fmt.Errorf("%w: pass --agent or --all-agents, not both", core.ErrUsage)
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if _, err := s.GetChecklist(code, "attest"); err != nil {
				return fmt.Errorf("%w: project %s has no attest checklist — apply a profile that ships one (scrumban does): atm profile apply --project %s --name scrumban", core.ErrUsage, code, code)
			}
			cfg, err := s.GetAgentsConfig()
			if err != nil {
				return err
			}
			var keys []string
			if all {
				keys = configuredAgentKeys(cfg)
				if len(keys) == 0 {
					return fmt.Errorf("%w: no agent is configured on this machine; run `atm agents select <name>`", core.ErrUsage)
				}
			} else {
				name, err := resolveSelectionKey(agentFlag, cfg)
				if err != nil {
					return err
				}
				keys = []string{name}
			}
			for _, key := range keys {
				if dryRun {
					fmt.Fprintf(st.stdout(), "would dispatch attest as manager on %s for %s\n", key, code)
					continue
				}
				fmt.Fprintf(st.stdout(), "verifying %s on %s\n", code, key)
				if err := st.launchSession(sessionOpts{
					Project:   code,
					Agent:     key,
					Checklist: "attest",
					Launch:    "prompt",
				}); err != nil {
					return fmt.Errorf("attest on %s: %w", key, err)
				}
			}
			return nil
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().StringVar(&agentFlag, "agent", "", "agent to attest on (default: the selected agent)")
	cmd.Flags().BoolVar(&all, "all-agents", false, "attest on every configured agent, one after another")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would be dispatched")
	return cmd
}
