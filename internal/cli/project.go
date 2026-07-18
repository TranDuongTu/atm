package cli

import (
	"fmt"
	"os"
	"strings"

	"atm/internal/capability"
	"atm/internal/core"

	"github.com/spf13/cobra"
)

func newProjectCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Project commands",
	}
	bindActorFlag(cmd, st)
	cmd.AddCommand(newProjectCreateCmd(st))
	cmd.AddCommand(newProjectListCmd(st))
	cmd.AddCommand(newProjectShowCmd(st))
	cmd.AddCommand(newProjectSetNameCmd(st))
	cmd.AddCommand(newProjectRemoveCmd(st))
	cmd.AddCommand(newProjectSetEmbeddingCmd(st))
	cmd.AddCommand(newProjectCapabilityCmd(st))
	return cmd
}

func newProjectCreateCmd(st *cliState) *cobra.Command {
	var code, name string
	var capabilities []string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a project (minimal: code + name)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			chosen, err := resolveCapabilityChoice(st.registry, capabilities)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			p, err := s.CreateProject(code, name, actor)
			if err != nil {
				return err
			}
			for _, cname := range chosen {
				if err := s.EnableProjectCapability(p.Code, cname, actor); err != nil {
					return err
				}
			}
			proj, err := s.GetProject(p.Code)
			if err != nil {
				return err
			}
			if _, err := st.registry.For(proj).EnsureVocabulary(s, p.Code, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": projectToJSON(proj, nil)}, func() {
				fmt.Fprintf(os.Stdout, "created project %s\n", proj.Code)
			})
		},
	}
	cmd.Flags().StringVar(&code, "code", "", "project code (^[A-Z]{3,6}$)")
	cmd.Flags().StringVar(&name, "name", "", "project name")
	cmd.Flags().StringSliceVar(&capabilities, "capabilities", nil,
		"capabilities to enable for the project (default: all registered)")
	_ = cmd.MarkFlagRequired("code")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

// resolveCapabilityChoice validates requested capability names against the
// registry; nil/empty request means every registered capability. New
// projects always record an explicit choice — only pre-enablement projects
// read as nil/all.
func resolveCapabilityChoice(reg *capability.Registry, requested []string) ([]string, error) {
	known := reg.Names()
	if len(requested) == 0 {
		return known, nil
	}
	valid := make(map[string]bool, len(known))
	for _, n := range known {
		valid[n] = true
	}
	for _, r := range requested {
		if !valid[r] {
			return nil, fmt.Errorf("%w: unknown capability %q (registered: %s)", ErrUsage, r, strings.Join(known, ", "))
		}
	}
	return requested, nil
}

func newProjectCapabilityCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "capability",
		Short: "View or change the project's enabled capability set",
	}
	cmd.AddCommand(newProjectCapabilityListCmd(st))
	cmd.AddCommand(newProjectCapabilityAddCmd(st))
	cmd.AddCommand(newProjectCapabilityRemoveCmd(st))
	return cmd
}

func newProjectCapabilityListCmd(st *cliState) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the project's enabled capabilities",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			p, err := s.GetProject(project)
			if err != nil {
				return err
			}
			explicit := p.Capabilities != nil
			enabled := p.Capabilities
			if !explicit {
				enabled = st.registry.Names()
			}
			return st.emit(st.stdout(), map[string]any{
				"project": project, "capabilities": enabled, "explicit": explicit,
			}, func() {
				if !explicit {
					fmt.Fprintln(st.stdout(), "(all — no explicit choice recorded)")
				}
				for _, n := range enabled {
					fmt.Fprintln(st.stdout(), n)
				}
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newProjectCapabilityAddCmd(st *cliState) *cobra.Command {
	var project, name string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Enable a capability for the project (seeds its vocabulary)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.RequireMutatingActor()
			if err != nil {
				return err
			}
			// Validate against the FULL registry, not st.registry: the mount
			// narrowed st.registry to the project's CURRENTLY-enabled set, which
			// by definition excludes the capability being added.
			if _, err := resolveCapabilityChoice(st.fullRegistry, []string{name}); err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.EnableProjectCapability(project, name, actor); err != nil {
				return err
			}
			p, err := s.GetProject(project)
			if err != nil {
				return err
			}
			// Seed from the FULL registry narrowed to the project's NEW enabled
			// set (p was refetched after the enable). st.registry still reflects
			// the pre-add set and would filter the just-added capability out.
			if _, err := st.fullRegistry.For(p).EnsureVocabulary(s, project, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "enabled": name}, func() {
				fmt.Fprintf(st.stdout(), "%s: enabled %s\n", project, name)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&name, "name", "", "capability name")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newProjectCapabilityRemoveCmd(st *cliState) *cobra.Command {
	var project, name string
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Disable a capability for the project (vocabulary and labels stay)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.RequireMutatingActor()
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.DisableProjectCapability(project, name, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "disabled": name}, func() {
				fmt.Fprintf(st.stdout(), "%s: disabled %s\n", project, name)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&name, "name", "", "capability name")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newProjectListCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			ps := s.ListProjects()
			return st.emit(st.stdout(), map[string]any{"projects": projectsToJSON(ps)}, func() {
				fmt.Fprint(os.Stdout, renderProjectListText(projectsToJSON(ps)))
			})
		},
	}
	return cmd
}

func newProjectShowCmd(st *cliState) *cobra.Command {
	var code string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			p, err := s.GetProject(code)
			if err != nil {
				return err
			}
			hv, err := s.HistoryE(p.Code, core.Subject{Kind: "project", Code: p.Code})
			if err != nil {
				return err
			}
			pj := projectToJSON(p, hv)
			if cfg, _ := s.GetProjectConfig(code); cfg != nil {
				pj.Embedding = cfg.Embedding
			}
			return st.emit(st.stdout(), map[string]any{"project": pj}, func() {
				fmt.Fprintln(os.Stdout, renderProjectText(pj))
			})
		},
	}
	cmd.Flags().StringVar(&code, "code", "", "project code")
	_ = cmd.MarkFlagRequired("code")
	return cmd
}

func newProjectSetNameCmd(st *cliState) *cobra.Command {
	var code, name string
	cmd := &cobra.Command{
		Use:   "set-name",
		Short: "Rename a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.SetProjectName(code, name, actor); err != nil {
				return err
			}
			p, err := s.GetProject(code)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": projectToJSON(p, nil)}, func() {
				fmt.Fprintf(os.Stdout, "renamed project %s\n", p.Code)
			})
		},
	}
	cmd.Flags().StringVar(&code, "code", "", "project code")
	cmd.Flags().StringVar(&name, "name", "", "new project name")
	_ = cmd.MarkFlagRequired("code")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newProjectRemoveCmd(st *cliState) *cobra.Command {
	var code string
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a project (zero-task guard)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.RemoveProject(code, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"removed": code}, func() {
				fmt.Fprintf(os.Stdout, "removed project %s\n", code)
			})
		},
	}
	cmd.Flags().StringVar(&code, "code", "", "project code")
	_ = cmd.MarkFlagRequired("code")
	return cmd
}

func newProjectSetEmbeddingCmd(st *cliState) *cobra.Command {
	var project, model, endpoint, queryPrefix, docPrefix string
	var dim int
	var threshold float64
	cmd := &cobra.Command{
		Use:   "set-embedding",
		Short: "Declare the project's embedding model + endpoint (enables atm search / atm index)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if _, err := s.GetProject(project); err != nil {
				return fmt.Errorf("%w: project %s not found", ErrNotFound, project)
			}
			cfg := core.EmbeddingConfig{
				Model: model, Endpoint: endpoint, QueryPrefix: queryPrefix, DocPrefix: docPrefix,
				Dim: dim, Threshold: threshold,
			}
			if err := s.SetEmbeddingConfig(project, cfg, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{
				"project": project, "embedding": cfg, "actor": actor,
			}, func() {
				fmt.Fprintf(os.Stdout, "set embedding for %s: model=%s endpoint=%s dim=%d threshold=%.2f\n", project, model, endpoint, dim, threshold)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&model, "model", "", "embedding model slug (e.g. nomic-embed-text)")
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "OpenAI-compatible /v1/embeddings base URL")
	cmd.Flags().StringVar(&queryPrefix, "query-prefix", "", "prefix applied to query text (default none)")
	cmd.Flags().StringVar(&docPrefix, "doc-prefix", "", "prefix applied to document text (default none)")
	cmd.Flags().IntVar(&dim, "dim", 0, "vector dimension")
	cmd.Flags().Float64Var(&threshold, "threshold", 0, "cosine threshold below which text fallback triggers (0 = engine default)")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("model")
	_ = cmd.MarkFlagRequired("endpoint")
	return cmd
}
