package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"atm/internal/agent"
	"atm/internal/core"
	"atm/internal/developing"
	"atm/internal/manager"

	"github.com/spf13/cobra"
)

func newInitCmd(st *cliState) *cobra.Command {
	var agents []string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the store and install ATM agent plugins",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.Init(""); err != nil {
				return err
			}
			cfg, err := s.GetAgentsConfig()
			if err != nil {
				return err
			}
			setup := initSetupPromptResult{PluginAgents: agents}
			interactive := len(agents) == 0 && st.flags.output == outputText && st.isStdinTerminal()
			if interactive {
				setup, err = promptInitSetup(st, cfg)
				if err != nil {
					return err
				}
			}
			installed, err := installInitPlugins(setup.PluginAgents, dryRun)
			if err != nil {
				return err
			}
			if setup.SelectedAgent != "" {
				if err := warnInitSelectedAgent(st, setup.SelectedAgent); err != nil {
					return err
				}
			}
			if !interactive && len(installed) > 0 && cfg.Selected == "" {
				for _, res := range installed {
					if _, ok := agent.Lookup(res.Agent); ok {
						setup.SelectedAgent = res.Agent
						setup.SelectedAgentProvided = true
						break
					}
				}
			}
			if err := persistInitSetup(s, setup, dryRun); err != nil {
				return err
			}
			if st.isJSON() {
				out := map[string]any{"store": s.StorePath()}
				if len(installed) > 0 {
					out["installed"] = installed
				}
				if setup.SelectedAgent != "" {
					out["selected"] = setup.SelectedAgent
				}
				if setup.ArgsProvided {
					out["args"] = setup.Args
				}
				return writeJSON(st.stdout(), out)
			}
			fmt.Fprintln(st.stdout(), "initialized store at", s.StorePath())
			for _, res := range installed {
				mode := "installed"
				if res.DryRun {
					mode = "would install"
				}
				fmt.Fprintf(st.stdout(), "%s\t%s\t%s\t%s\n", res.Role, res.Agent, mode, res.Path)
			}
			if setup.SelectedAgent != "" {
				label := "selected"
				if dryRun {
					label = "would select"
				}
				fmt.Fprintf(st.stdout(), "%s\t%s\n", label, setup.SelectedAgent)
			}
			if setup.ArgsProvided && setup.SelectedAgent != "" {
				label := "args"
				if dryRun {
					label = "would set args"
				}
				fmt.Fprintf(st.stdout(), "%s\t%s\t%s\n", label, setup.SelectedAgent, strings.Join(setup.Args, " "))
			}
			if interactive {
				fmt.Fprintln(st.stdout(), "Next: atm manage --project <CODE> --action brief")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&st.flags.actor, "actor", "", "actor id (free-form; env ATM_ACTOR)")
	cmd.Flags().StringArrayVar(&agents, "agent", nil, "agent plugin to install (repeatable: opencode, codex, claude, all)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print plugin files that would be written without modifying user config")
	return cmd
}

func promptInitAgents(st *cliState) ([]string, error) {
	res, err := promptInitSetup(st, core.AgentsConfig{})
	return res.PluginAgents, err
}

func promptInitSetup(st *cliState, cfg core.AgentsConfig) (initSetupPromptResult, error) {
	var res initSetupPromptResult
	fmt.Fprintln(st.stdout())
	fmt.Fprintln(st.stdout(), "ATM setup")
	fmt.Fprintln(st.stdout(), "Choose agent integrations to install (multiple allowed):")
	fmt.Fprintln(st.stdout(), "  1) opencode")
	fmt.Fprintln(st.stdout(), "  2) codex")
	fmt.Fprintln(st.stdout(), "  3) claude")
	fmt.Fprint(st.stdout(), "Agents [comma-separated numbers/names, all, or Enter to skip]: ")

	scanner := bufio.NewScanner(st.stdin())
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return res, fmt.Errorf("read init selection: %w", err)
		}
		return res, nil
	}
	plugins, err := parseInitAgentSelection(scanner.Text())
	if err != nil {
		return res, err
	}
	res.PluginAgents = plugins
	previewInstalled, err := previewInitInstallResults(plugins)
	if err != nil {
		return res, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return res, fmt.Errorf("resolve home dir: %w", err)
	}
	entries := viableInitDefaultAgents(previewInstalled, cfg, home)
	if len(entries) == 0 {
		fmt.Fprintln(st.stdout(), "No default agent candidates yet; install an agent plugin or run `atm agents select <name>` later.")
		return res, nil
	}
	fmt.Fprintln(st.stdout())
	fmt.Fprintln(st.stdout(), "Default agent:")
	for i, e := range entries {
		marker := ""
		if cfg.Selected == e.Name {
			marker = " (current)"
		}
		fmt.Fprintf(st.stdout(), "  %d) %s%s\n", i+1, e.Name, marker)
	}
	fmt.Fprint(st.stdout(), "Default agent [number/name, or Enter to keep current]: ")
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return res, fmt.Errorf("read init default agent: %w", err)
		}
		return res, nil
	}
	selected, ok, err := parseInitDefaultAgentSelection(scanner.Text(), entries)
	if err != nil {
		return res, err
	}
	if !ok {
		selected = cfg.Selected
	} else {
		res.SelectedAgentProvided = true
	}
	res.SelectedAgent = selected
	if selected == "" {
		return res, nil
	}
	fmt.Fprintf(st.stdout(), "Agent args for %s [optional, shell-like quoting; Enter to keep current]: ", selected)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return res, fmt.Errorf("read init agent args: %w", err)
		}
		return res, nil
	}
	args, argsOK, err := parseInitArgsLine(scanner.Text())
	if err != nil {
		return res, err
	}
	res.Args = args
	res.ArgsProvided = argsOK
	return res, nil
}

type initSetupPromptResult struct {
	PluginAgents          []string
	SelectedAgent         string
	SelectedAgentProvided bool
	Args                  []string
	ArgsProvided          bool
}

func parseInitDefaultAgentSelection(input string, entries []agent.Entry) (string, bool, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", false, nil
	}
	for i, e := range entries {
		if input == fmt.Sprintf("%d", i+1) || input == e.Name {
			return e.Name, true, nil
		}
	}
	return "", false, fmt.Errorf("%w: unknown init default agent selection %q", ErrUsage, input)
}

func parseInitArgsLine(input string) ([]string, bool, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, false, nil
	}
	var out []string
	var cur strings.Builder
	var quote rune
	escaped := false
	tokenStarted := false
	emit := func() {
		if tokenStarted {
			out = append(out, cur.String())
			cur.Reset()
			tokenStarted = false
		}
	}
	for _, r := range input {
		if escaped {
			cur.WriteRune(r)
			tokenStarted = true
			escaped = false
			continue
		}
		if r == '\\' {
			tokenStarted = true
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			cur.WriteRune(r)
			tokenStarted = true
			continue
		}
		switch r {
		case '\'', '"':
			tokenStarted = true
			quote = r
		case ' ', '\t', '\n':
			emit()
		default:
			cur.WriteRune(r)
			tokenStarted = true
		}
	}
	if escaped {
		cur.WriteRune('\\')
	}
	if quote != 0 {
		return nil, false, fmt.Errorf("%w: unterminated quote in init args", ErrUsage)
	}
	emit()
	return out, true, nil
}

func parseInitAgentSelection(input string) ([]string, error) {
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return nil, nil
	}
	replacer := strings.NewReplacer(",", " ", ";", " ")
	fields := strings.Fields(replacer.Replace(input))
	selected := make([]string, 0, len(fields))
	for _, field := range fields {
		switch field {
		case "1", "opencode":
			selected = append(selected, "opencode")
		case "2", "codex":
			selected = append(selected, "codex")
		case "3", "claude":
			selected = append(selected, "claude")
		case "all":
			selected = append(selected, "all")
		default:
			return nil, fmt.Errorf("%w: unknown init agent selection %q", ErrUsage, field)
		}
	}
	return selected, nil
}

type initInstallResult struct {
	Role   string   `json:"role"`
	Agent  string   `json:"agent"`
	Path   string   `json:"path"`
	Files  []string `json:"files"`
	DryRun bool     `json:"dry_run"`
}

func installInitPlugins(selected []string, dryRun bool) ([]initInstallResult, error) {
	agents, err := initAgents(selected)
	if err != nil {
		return nil, err
	}
	if len(agents) == 0 {
		return nil, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}
	var out []initInstallResult
	for _, agent := range agents {
		dev, err := developing.InstallPlugin(agent, home, dryRun)
		if err != nil {
			return out, err
		}
		out = append(out, initInstallResult{
			Role:   "developing",
			Agent:  dev.Agent,
			Path:   dev.Path,
			Files:  dev.Files,
			DryRun: dev.DryRun,
		})
		mgr, err := manager.InstallPlugin(agent, home, dryRun)
		if err != nil {
			return out, err
		}
		out = append(out, initInstallResult{
			Role:   "manager",
			Agent:  mgr.Agent,
			Path:   mgr.Path,
			Files:  mgr.Files,
			DryRun: mgr.DryRun,
		})
	}
	return out, nil
}

func previewInitInstallResults(selected []string) ([]initInstallResult, error) {
	agents, err := initAgents(selected)
	if err != nil {
		return nil, err
	}
	out := make([]initInstallResult, 0, len(agents))
	for _, name := range agents {
		out = append(out, initInstallResult{Agent: name})
	}
	return out, nil
}

func viableInitDefaultAgents(installed []initInstallResult, cfg core.AgentsConfig, home string) []agent.Entry {
	pluginAgents := map[string]bool{}
	for _, res := range installed {
		pluginAgents[res.Agent] = true
	}
	for _, name := range []string{"opencode", "codex", "claude"} {
		if developing.PluginStatus(name, home).State == "installed" {
			pluginAgents[name] = true
		}
	}
	if cfg.Selected != "" {
		if e, ok := agent.Lookup(cfg.Selected); ok {
			pluginAgents[e.PluginAgent()] = true
		}
	}
	var out []agent.Entry
	seen := map[string]bool{}
	for _, e := range agent.Catalog() {
		if !pluginAgents[e.PluginAgent()] || seen[e.Name] {
			continue
		}
		out = append(out, e)
		seen[e.Name] = true
	}
	return out
}

func warnInitSelectedAgent(st *cliState, selected string) error {
	e, ok := agent.Lookup(selected)
	if !ok {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}
	status := agent.Status(e, home, exec.LookPath)
	if !status.Ready() {
		fmt.Fprintf(st.stderr(), "warning: %s is not ready (%s)\n", selected, status.String())
	}
	return nil
}

func persistInitSetup(s core.Service, setup initSetupPromptResult, dryRun bool) error {
	if dryRun {
		return nil
	}
	if setup.SelectedAgentProvided && setup.SelectedAgent != "" {
		if err := s.SetSelectedAgent(setup.SelectedAgent, "admin@cli:unset"); err != nil {
			return err
		}
	}
	if setup.ArgsProvided && setup.SelectedAgent != "" {
		if err := s.SetAgentArgs(setup.SelectedAgent, setup.Args, "admin@cli:unset"); err != nil {
			return err
		}
	}
	return nil
}

func initAgents(selected []string) ([]string, error) {
	if len(selected) == 0 {
		return nil, nil
	}
	allowed := map[string]bool{"opencode": true, "codex": true, "claude": true}
	seen := map[string]bool{}
	for _, raw := range selected {
		if raw == "all" {
			for _, agent := range []string{"opencode", "codex", "claude"} {
				seen[agent] = true
			}
			continue
		}
		if !allowed[raw] {
			return nil, fmt.Errorf("%w: unknown init agent %q", ErrUsage, raw)
		}
		seen[raw] = true
	}
	var out []string
	for _, agent := range []string{"opencode", "codex", "claude"} {
		if seen[agent] {
			out = append(out, agent)
		}
	}
	return out, nil
}
