// internal/cli/checklist.go
package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"atm/internal/core"
	"atm/internal/profile"

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
	cmd.AddCommand(newChecklistSetCmd(st))
	cmd.AddCommand(newChecklistResetCmd(st))
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

// checklistSteps resolves the step tree from --step (repeatable, flat
// top-level nodes) and/or --steps-file (a markdown nested list — the
// checklist body format; '-' reads stdin). The two are mutually exclusive.
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
	rec, err := profile.ParseChecklistDocument("", data)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", core.ErrUsage, err)
	}
	return rec.Steps, nil
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

// newChecklistSetCmd replaces a record wholesale from a checklist document —
// the seed format: frontmatter name/purpose/suits/requires, a nested-list
// step body. Checklists are authored elsewhere and imported; ATM is the
// source of record, not an editor, so there is no per-field edit. The
// document's name must match --name (a guard against overwriting the wrong
// record); its origin, if present, is ignored — provenance stays with the
// record.
func newChecklistSetCmd(st *cliState) *cobra.Command {
	var name, file string
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Replace a checklist wholesale from a checklist document",
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
			var data []byte
			if file == "-" {
				data, err = io.ReadAll(st.stdin())
			} else {
				data, err = os.ReadFile(file)
			}
			if err != nil {
				return err
			}
			doc, err := profile.ParseChecklistDocument(name, data)
			if err != nil {
				return fmt.Errorf("%w: %v", core.ErrUsage, err)
			}
			rec := core.ChecklistRecord{
				Purpose: doc.Purpose,
				Steps:   doc.Steps,
				Suits:   doc.Suits,
				Requires: core.ChecklistRequires{
					Capabilities: doc.Requires.Capabilities,
					Channels:     doc.Requires.Channels,
				},
			}
			if err := s.SetChecklist(project, name, rec, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "name": name}, func() {
				fmt.Fprintf(st.stdout(), "set checklist %s\n", name)
			})
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().StringVar(&name, "name", "", "checklist name (the document's frontmatter name must match)")
	cmd.Flags().StringVar(&file, "file", "", "checklist document to import ('-' reads stdin)")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

// newChecklistResetCmd restores a record from the profile version it came
// from — its OWN origin version, never the newest installed one.
func newChecklistResetCmd(st *cliState) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Restore a checklist from the profile version it came from",
		Long: "Reset discards local edits and restores the record from its OWN origin " +
			"version — not the newest installed one. Reset means back to what you " +
			"were given; upgrading is `atm profile apply`. A record the project " +
			"authored itself has no source to restore from and is refused; one " +
			"whose version is not installed here names it.",
		Args: cobra.NoArgs,
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
			rec, err := s.ResetChecklistRecord(project, name, actor)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "checklist": rec}, func() {
				fmt.Fprintf(st.stdout(), "reset checklist %s to %s\n", rec.Name, rec.Origin)
			})
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().StringVar(&name, "name", "", "checklist name")
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
