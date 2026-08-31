// internal/cli/checklist.go
package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"atm/internal/core"
	"atm/skills"

	"github.com/spf13/cobra"
)

// newChecklistCmd is the first-class checklist noun. A checklist is a
// free-standing, name-keyed record of a standing operating procedure: purpose,
// a recursive step tree, suits (default-bind persona hints), requires, and
// origin. This group is the only sanctioned write path.
func newChecklistCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "checklist",
		Short: "Manage checklists — name-keyed standing operating procedures with recursive steps",
		Long: "A checklist is a free-standing, name-keyed record of a standing " +
			"operating procedure: purpose, recursive steps, suits (default-bind " +
			"persona hints), requires, and origin. `--output json` on list/show " +
			"is the agent endpoint.",
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

func coreStepsOf(in []skills.SeedStep) []core.ChecklistStep {
	if len(in) == 0 {
		return nil
	}
	out := make([]core.ChecklistStep, len(in))
	for i, s := range in {
		out[i] = core.ChecklistStep{Text: s.Text, Children: coreStepsOf(s.Children)}
	}
	return out
}

// checklistSteps resolves the step tree from --step (repeatable, flat
// top-level nodes) and/or --steps-file (a markdown nested list — the seed
// body format; '-' reads stdin). The two are mutually exclusive.
func checklistSteps(cmd *cobra.Command, st *cliState) ([]core.ChecklistStep, error) {
	stepFlags, _ := cmd.Flags().GetStringArray("step")
	file, _ := cmd.Flags().GetString("steps-file")
	if file != "" && len(stepFlags) > 0 {
		return nil, fmt.Errorf("%w: --step and --steps-file are mutually exclusive", core.ErrUsage)
	}
	if file == "" {
		out := make([]core.ChecklistStep, len(stepFlags))
		for i, s := range stepFlags {
			out[i] = core.ChecklistStep{Text: s}
		}
		return out, nil
	}
	var data []byte
	var err error
	if file == "-" {
		data, err = io.ReadAll(st.stdin())
	} else {
		data, err = os.ReadFile(file)
	}
	if err != nil {
		return nil, err
	}
	seedSteps, err := skills.ParseSteps(string(data))
	if err != nil {
		return nil, err
	}
	return coreStepsOf(seedSteps), nil
}

// renderChecklistSteps writes the step tree with the shared nested numbering:
// top level "N.", nested "1.2" / "1.2.1", two-space indent per level.
func renderChecklistSteps(w io.Writer, steps []core.ChecklistStep, prefix, indent string) {
	for i, st := range steps {
		num := strconv.Itoa(i + 1)
		if prefix != "" {
			num = prefix + "." + num
			fmt.Fprintf(w, "%s%s %s\n", indent, num, st.Text)
		} else {
			fmt.Fprintf(w, "%s%s. %s\n", indent, num, st.Text)
		}
		renderChecklistSteps(w, st.Children, num, indent+"  ")
	}
}

func checklistSuitsCell(suits []string) string {
	if len(suits) == 0 {
		return "-"
	}
	return strings.Join(suits, ",")
}

func checklistRequiresCell(req core.ChecklistRequires) string {
	var parts []string
	if len(req.Capabilities) > 0 {
		parts = append(parts, "capabilities: "+strings.Join(req.Capabilities, ", "))
	}
	if len(req.Channels) > 0 {
		parts = append(parts, "channels: "+strings.Join(req.Channels, ", "))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, "; ")
}

// newChecklistListCmd is the agent endpoint's list: records suited to a
// persona (--persona / ATM_PERSONA), or every record with --all.
func newChecklistListCmd(st *cliState) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List checklists suited to a persona, or the whole project with --all",
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
			emit := func(recs []core.ChecklistRecord) error {
				if st.isJSON() {
					if recs == nil {
						recs = []core.ChecklistRecord{}
					}
					return writeJSON(st.stdout(), recs)
				}
				for _, r := range recs {
					fmt.Fprintf(st.stdout(), "%s\t%s\t%s\n", r.Name, checklistSuitsCell(r.Suits), r.Purpose)
				}
				return nil
			}
			if all {
				recs, err := s.ChecklistRecords(project)
				if err != nil {
					return err
				}
				return emit(recs)
			}
			persona, _ := cmd.Flags().GetString("persona")
			if persona == "" {
				persona = os.Getenv("ATM_PERSONA")
			}
			if persona == "" {
				return fmt.Errorf("%w: --persona is required (or ATM_PERSONA, or --all)", core.ErrUsage)
			}
			recs, err := s.SuitedChecklists(project, persona)
			if err != nil {
				return err
			}
			if len(recs) == 0 && !st.isJSON() {
				fmt.Fprintf(st.stdout(), "No checklists suited to %s @ %s. Author one with atm checklist add, or seed shipped ones: atm capability checklist seed.\n", persona, project)
				return nil
			}
			return emit(recs)
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().String("persona", "", "filter to records whose suits name this persona (or ATM_PERSONA)")
	cmd.Flags().BoolVar(&all, "all", false, "list every checklist in the project")
	return cmd
}

// newChecklistShowCmd is the agent endpoint's single-checklist read.
func newChecklistShowCmd(st *cliState) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show one checklist: purpose, suits, requires, origin, and the step tree",
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
			r, err := s.GetChecklist(project, name)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), r, func() {
				fmt.Fprintf(st.stdout(), "%s\t%s\n", r.Name, r.Purpose)
				fmt.Fprintf(st.stdout(), "origin: %s\tsuits: %s\trequires: %s\n", r.Origin, checklistSuitsCell(r.Suits), checklistRequiresCell(r.Requires))
				renderChecklistSteps(st.stdout(), r.Steps, "", "  ")
			})
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().StringVar(&name, "name", "", "checklist name")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

// newChecklistAddCmd authors a checklist's ledger record. Origin is never a
// flag: CLI-created records are always "user"; shipped origins come from the
// seed paths only.
func newChecklistAddCmd(st *cliState) *cobra.Command {
	var name, purpose string
	var stepFlags, suits, reqCaps, reqChans []string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Author a checklist (purpose, suits, requires, and a step tree)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := checklistProject(cmd)
			if err != nil {
				return err
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
			rec := core.ChecklistRecord{
				Name:    name,
				Purpose: purpose,
				Steps:   resolved,
				Suits:   suits,
				Requires: core.ChecklistRequires{
					Capabilities: reqCaps,
					Channels:     reqChans,
				},
			}
			tk, err := s.CreateChecklist(project, rec, actor)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "name": name, "task": tk.ID}, func() {
				fmt.Fprintf(st.stdout(), "created checklist %s (task %s)\n", name, tk.ID)
			})
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().StringVar(&name, "name", "", "checklist name")
	cmd.Flags().StringVar(&purpose, "purpose", "", "what the checklist is for (the one-line selection surface)")
	cmd.Flags().StringArrayVar(&stepFlags, "step", nil, "one top-level imperative step (repeatable)")
	cmd.Flags().String("steps-file", "", "read the step tree from a markdown nested list ('-' for stdin)")
	cmd.Flags().StringArrayVar(&suits, "suits", nil, "persona this checklist suits — a default-bind hint (repeatable)")
	cmd.Flags().StringArrayVar(&reqCaps, "requires-capability", nil, "capability this checklist needs (repeatable)")
	cmd.Flags().StringArrayVar(&reqChans, "requires-channel", nil, "channel handle this checklist needs (repeatable)")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

// newChecklistEditCmd applies a partial update: every field is optional and
// independent, gated by cmd.Flags().Changed. A lone empty --suits (or
// requires) value clears the field.
func newChecklistEditCmd(st *cliState) *cobra.Command {
	var name, purpose string
	var stepFlags, suits, reqCaps, reqChans []string
	cmd := &cobra.Command{
		Use:   "edit",
		Short: "Edit a checklist's purpose, steps, suits, and/or requires",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := checklistProject(cmd)
			if err != nil {
				return err
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
			var e core.ChecklistEdit
			if cmd.Flags().Changed("purpose") {
				e.Purpose = &purpose
			}
			if cmd.Flags().Changed("step") || cmd.Flags().Changed("steps-file") {
				resolved, err := checklistSteps(cmd, st)
				if err != nil {
					return err
				}
				if len(resolved) == 0 {
					return fmt.Errorf("%w: a checklist needs at least one step", core.ErrUsage)
				}
				e.Steps = resolved
			}
			if cmd.Flags().Changed("suits") {
				if len(suits) == 1 && suits[0] == "" {
					suits = []string{}
				}
				e.Suits = suits
			}
			if cmd.Flags().Changed("requires-capability") || cmd.Flags().Changed("requires-channel") {
				if len(reqCaps) == 1 && reqCaps[0] == "" {
					reqCaps = nil
				}
				if len(reqChans) == 1 && reqChans[0] == "" {
					reqChans = nil
				}
				e.Requires = &core.ChecklistRequires{Capabilities: reqCaps, Channels: reqChans}
			}
			if err := s.EditChecklist(project, name, e, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "name": name}, func() {
				fmt.Fprintf(st.stdout(), "updated checklist %s\n", name)
			})
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().StringVar(&name, "name", "", "checklist name")
	cmd.Flags().StringVar(&purpose, "purpose", "", "what the checklist is for (empty clears)")
	cmd.Flags().StringArrayVar(&stepFlags, "step", nil, "one top-level imperative step (repeatable; replaces the tree)")
	cmd.Flags().String("steps-file", "", "read the replacement step tree from a markdown nested list ('-' for stdin)")
	cmd.Flags().StringArrayVar(&suits, "suits", nil, "replacement suits list (repeatable; a lone empty value clears)")
	cmd.Flags().StringArrayVar(&reqCaps, "requires-capability", nil, "replacement required capabilities (repeatable; a lone empty value clears)")
	cmd.Flags().StringArrayVar(&reqChans, "requires-channel", nil, "replacement required channels (repeatable; a lone empty value clears)")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

// newChecklistRemoveCmd removes a checklist's ledger record. --task
// disambiguates a legacy v1 same-name collision.
func newChecklistRemoveCmd(st *cliState) *cobra.Command {
	var name, taskID string
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a checklist",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := checklistProject(cmd)
			if err != nil {
				return err
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
			if err := s.RemoveChecklist(project, name, taskID, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "name": name}, func() {
				fmt.Fprintf(st.stdout(), "removed checklist %s\n", name)
			})
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().StringVar(&name, "name", "", "checklist name")
	cmd.Flags().StringVar(&taskID, "task", "", "task ID, to disambiguate a legacy same-name collision")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}
