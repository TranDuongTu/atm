// internal/cli/checklist.go
package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"atm/internal/core"

	"github.com/spf13/cobra"
)

// newChecklistCmd is the first-class checklist noun. A checklist is a
// persona-scoped, ordered list of imperative steps a concierge authors so a
// persona can self-steer; the ledger record is a labelled task plus a payload.
// This group is the only sanctioned write path.
func newChecklistCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "checklist",
		Short: "Manage persona checklists — ordered imperative steps for a persona's routine",
		Long: "A checklist is a persona-scoped, ordered list of imperative steps " +
			"a concierge sets up so a persona can self-steer. `--output json` on " +
			"list/show is the agent endpoint.",
	}
	bindActorFlag(cmd, st)
	cmd.AddCommand(newChecklistAddCmd(st))
	cmd.AddCommand(newChecklistListCmd(st))
	cmd.AddCommand(newChecklistShowCmd(st))
	cmd.AddCommand(newChecklistEditCmd(st))
	cmd.AddCommand(newChecklistRemoveCmd(st))
	return cmd
}

// checklistProject resolves the target project: --project flag, else ATM_PROJECT.
func checklistProject(cmd *cobra.Command) (string, error) {
	p, _ := cmd.Flags().GetString("project")
	if p == "" {
		p = os.Getenv("ATM_PROJECT")
	}
	if p == "" {
		return "", fmt.Errorf("%w: --project is required (or ATM_PROJECT)", core.ErrUsage)
	}
	return p, nil
}

// checklistPersona resolves the target persona: --persona flag, else ATM_PERSONA.
func checklistPersona(cmd *cobra.Command) string {
	p, _ := cmd.Flags().GetString("persona")
	if p == "" {
		p = os.Getenv("ATM_PERSONA")
	}
	return p
}

// requireChecklistCapability gates the noun on the project's enabled set;
// bare string literal because internal/cli may not import capability packages.
func requireChecklistCapability(s core.Service, project string) error {
	p, err := s.GetProject(project)
	if err != nil {
		return err
	}
	if p.Capabilities == nil {
		return nil
	}
	for _, n := range p.Capabilities {
		if n == "checklist" {
			return nil
		}
	}
	return fmt.Errorf("%w: capability \"checklist\" is not enabled for project %s (enable with: atm project capability add --project %s --name checklist)", core.ErrUsage, project, project)
}

// checklistSteps resolves the step list from --step (repeatable) and/or
// --steps-file. The two are mutually exclusive; a file is read one step per
// non-empty line ('-' reads stdin).
func checklistSteps(cmd *cobra.Command, st *cliState) ([]string, error) {
	steps, _ := cmd.Flags().GetStringArray("step")
	file, _ := cmd.Flags().GetString("steps-file")
	if file != "" && len(steps) > 0 {
		return nil, fmt.Errorf("%w: --step and --steps-file are mutually exclusive", core.ErrUsage)
	}
	if file == "" {
		return steps, nil
	}
	var r io.Reader
	if file == "-" {
		r = st.stdin()
	} else {
		f, err := os.Open(file)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}
	sc := bufio.NewScanner(r)
	var out []string
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			out = append(out, line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// newChecklistListCmd is the agent endpoint's list: a whole persona's
// checklists (purpose + steps resolved from each record), or every persona in
// the project with --all.
func newChecklistListCmd(st *cliState) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List a persona's checklists, or the whole project with --all",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := checklistProject(cmd)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := requireChecklistCapability(s, project); err != nil {
				return err
			}
			if all {
				recs, err := s.ChecklistRecords(project)
				if err != nil {
					return err
				}
				if st.isJSON() {
					if recs == nil {
						recs = []core.ChecklistRecord{}
					}
					return writeJSON(st.stdout(), recs)
				}
				for _, r := range recs {
					fmt.Fprintf(st.stdout(), "%s/%s\t%s\n", r.Persona, r.Name, r.Purpose)
				}
				return nil
			}
			persona := checklistPersona(cmd)
			if persona == "" {
				return fmt.Errorf("%w: --persona is required (or ATM_PERSONA, or --all)", core.ErrUsage)
			}
			recs, err := s.PersonaChecklists(project, persona)
			if err != nil {
				return err
			}
			if st.isJSON() {
				if recs == nil {
					recs = []core.ChecklistRecord{}
				}
				return writeJSON(st.stdout(), recs)
			}
			if len(recs) == 0 {
				fmt.Fprintf(st.stdout(), "No checklists for %s @ %s. (Concierge can set these up — atm capability checklist guide.)\n", persona, project)
				return nil
			}
			for _, r := range recs {
				fmt.Fprintf(st.stdout(), "%s/%s\t%s\n", r.Persona, r.Name, r.Purpose)
			}
			return nil
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().String("persona", "", "persona (or ATM_PERSONA)")
	cmd.Flags().BoolVar(&all, "all", false, "list every persona's checklists in the project")
	return cmd
}

// newChecklistShowCmd is the agent endpoint's single-checklist read.
func newChecklistShowCmd(st *cliState) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show one checklist: persona, purpose, and ordered steps",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := checklistProject(cmd)
			if err != nil {
				return err
			}
			persona := checklistPersona(cmd)
			if persona == "" {
				return fmt.Errorf("%w: --persona is required (or ATM_PERSONA)", core.ErrUsage)
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := requireChecklistCapability(s, project); err != nil {
				return err
			}
			r, err := s.GetChecklist(project, persona, name)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), r, func() {
				fmt.Fprintf(st.stdout(), "%s/%s\t%s\n", r.Persona, r.Name, r.Purpose)
				for i, step := range r.Steps {
					fmt.Fprintf(st.stdout(), "  %d. %s\n", i+1, step)
				}
			})
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().String("persona", "", "persona (or ATM_PERSONA)")
	cmd.Flags().StringVar(&name, "name", "", "checklist name")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

// newChecklistAddCmd authors a checklist's ledger record: persona, name,
// purpose, and the ordered imperative steps.
func newChecklistAddCmd(st *cliState) *cobra.Command {
	var persona, name, purpose string
	var stepFlags []string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Author a persona's checklist (purpose + ordered imperative steps)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := checklistProject(cmd)
			if err != nil {
				return err
			}
			persona = checklistPersona(cmd)
			if persona == "" {
				return fmt.Errorf("%w: --persona is required (or ATM_PERSONA)", core.ErrUsage)
			}
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := requireChecklistCapability(s, project); err != nil {
				return err
			}
			resolved, err := checklistSteps(cmd, st)
			if err != nil {
				return err
			}
			rec := core.ChecklistRecord{Persona: persona, Name: name, Purpose: purpose, Steps: resolved}
			tk, err := s.CreateChecklist(project, rec, actor)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "persona": persona, "name": name, "task": tk.ID}, func() {
				fmt.Fprintf(st.stdout(), "created checklist %s/%s (task %s)\n", persona, name, tk.ID)
			})
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().StringVar(&persona, "persona", "", "persona the checklist belongs to (or ATM_PERSONA)")
	cmd.Flags().StringVar(&name, "name", "", "checklist name")
	cmd.Flags().StringVar(&purpose, "purpose", "", "what the checklist is for (the one-line selection surface)")
	cmd.Flags().StringArrayVar(&stepFlags, "step", nil, "one imperative step (repeatable)")
	cmd.Flags().String("steps-file", "", "read steps from a file, one per non-empty line ('-' for stdin)")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

// newChecklistEditCmd updates purpose and/or steps. Both are optional and
// independent: cmd.Flags().Changed gates the purpose pointer, and the steps
// slice is passed only when a step flag was named (nil = unchanged).
func newChecklistEditCmd(st *cliState) *cobra.Command {
	var persona, name, purpose string
	var stepFlags []string
	cmd := &cobra.Command{
		Use:   "edit",
		Short: "Edit a checklist's purpose and/or steps",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := checklistProject(cmd)
			if err != nil {
				return err
			}
			persona = checklistPersona(cmd)
			if persona == "" {
				return fmt.Errorf("%w: --persona is required (or ATM_PERSONA)", core.ErrUsage)
			}
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := requireChecklistCapability(s, project); err != nil {
				return err
			}
			var purposePtr *string
			if cmd.Flags().Changed("purpose") {
				purposePtr = &purpose
			}
			var steps []string
			if cmd.Flags().Changed("step") || cmd.Flags().Changed("steps-file") {
				resolved, err := checklistSteps(cmd, st)
				if err != nil {
					return err
				}
				if len(resolved) == 0 {
					return fmt.Errorf("%w: a checklist needs at least one step", core.ErrUsage)
				}
				steps = resolved
			}
			if err := s.EditChecklist(project, persona, name, purposePtr, steps, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "persona": persona, "name": name}, func() {
				fmt.Fprintf(st.stdout(), "updated checklist %s/%s\n", persona, name)
			})
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().StringVar(&persona, "persona", "", "persona the checklist belongs to (or ATM_PERSONA)")
	cmd.Flags().StringVar(&name, "name", "", "checklist name")
	cmd.Flags().StringVar(&purpose, "purpose", "", "what the checklist is for (the one-line selection surface)")
	cmd.Flags().StringArrayVar(&stepFlags, "step", nil, "one imperative step (repeatable)")
	cmd.Flags().String("steps-file", "", "read steps from a file, one per non-empty line ('-' for stdin)")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

// newChecklistRemoveCmd removes a checklist's ledger record.
func newChecklistRemoveCmd(st *cliState) *cobra.Command {
	var persona, name string
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a persona's checklist",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := checklistProject(cmd)
			if err != nil {
				return err
			}
			persona = checklistPersona(cmd)
			if persona == "" {
				return fmt.Errorf("%w: --persona is required (or ATM_PERSONA)", core.ErrUsage)
			}
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := requireChecklistCapability(s, project); err != nil {
				return err
			}
			if err := s.RemoveChecklist(project, persona, name, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "persona": persona, "name": name}, func() {
				fmt.Fprintf(st.stdout(), "removed checklist %s/%s\n", persona, name)
			})
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().StringVar(&persona, "persona", "", "persona the checklist belongs to (or ATM_PERSONA)")
	cmd.Flags().StringVar(&name, "name", "", "checklist name")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}
