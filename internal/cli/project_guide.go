package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newProjectGuideCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "guide",
		Short: "Project guide commands",
	}
	cmd.AddCommand(newProjectGuideShowCmd(st))
	cmd.AddCommand(newProjectGuideSectionCmd(st))
	cmd.AddCommand(newProjectGuideRefCmd(st))
	cmd.AddCommand(newProjectGuideSetFreshnessCmd(st))
	cmd.AddCommand(newProjectGuideStatusCmd(st))
	return cmd
}

func newProjectGuideShowCmd(st *cliState) *cobra.Command {
	var code string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show the project guide",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			g, err := s.GuideGet(code)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"guide": guideToJSON(g)}, func() {
				if g == nil {
					fmt.Fprintln(os.Stdout, "guide: (none)")
					return
				}
				fmt.Fprintf(os.Stdout, "guide: %d section(s), updated by %s\n", len(g.Sections), g.UpdatedBy)
			})
		},
	}
	cmd.Flags().StringVar(&code, "code", "", "project code")
	_ = cmd.MarkFlagRequired("code")
	return cmd
}

func newProjectGuideSectionCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "section",
		Short: "Guide section commands",
	}
	var code, name, newName, before string
	add := &cobra.Command{
		Use:   "add",
		Short: "Add a guide section",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.GuideSectionAdd(code, name, actor); err != nil {
				return err
			}
			g, _ := s.GuideGet(code)
			return st.emit(st.stdout(), map[string]any{"guide": guideToJSON(g)}, func() {
				fmt.Fprintf(os.Stdout, "section %s added\n", name)
			})
		},
	}
	rename := &cobra.Command{
		Use:   "rename",
		Short: "Rename a guide section",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.GuideSectionRename(code, name, newName, actor); err != nil {
				return err
			}
			g, _ := s.GuideGet(code)
			return st.emit(st.stdout(), map[string]any{"guide": guideToJSON(g)}, func() {
				fmt.Fprintf(os.Stdout, "section %s -> %s\n", name, newName)
			})
		},
	}
	remove := &cobra.Command{
		Use:   "remove",
		Short: "Remove a guide section",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.GuideSectionRemove(code, name, actor); err != nil {
				return err
			}
			g, _ := s.GuideGet(code)
			return st.emit(st.stdout(), map[string]any{"guide": guideToJSON(g)}, func() {
				fmt.Fprintf(os.Stdout, "section %s removed\n", name)
			})
		},
	}
	move := &cobra.Command{
		Use:   "move",
		Short: "Reorder a guide section (before=<other>|empty=end)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.GuideSectionMove(code, name, before, actor); err != nil {
				return err
			}
			g, _ := s.GuideGet(code)
			return st.emit(st.stdout(), map[string]any{"guide": guideToJSON(g)}, func() {
				fmt.Fprintf(os.Stdout, "section %s moved\n", name)
			})
		},
	}
	for _, c := range []*cobra.Command{add, rename, remove, move} {
		c.Flags().StringVar(&code, "code", "", "project code")
		_ = c.MarkFlagRequired("code")
	}
	add.Flags().StringVar(&name, "name", "", "section name")
	_ = add.MarkFlagRequired("name")
	rename.Flags().StringVar(&name, "name", "", "section name")
	rename.Flags().StringVar(&newName, "new-name", "", "new section name")
	_ = rename.MarkFlagRequired("name")
	_ = rename.MarkFlagRequired("new-name")
	remove.Flags().StringVar(&name, "name", "", "section name")
	_ = remove.MarkFlagRequired("name")
	move.Flags().StringVar(&name, "name", "", "section name")
	move.Flags().StringVar(&before, "before", "", "section to insert before (empty = end)")
	_ = move.MarkFlagRequired("name")
	cmd.AddCommand(add, rename, remove, move)
	return cmd
}

func newProjectGuideRefCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ref",
		Short: "Guide ref commands",
	}
	var code, section, kind, target, before string
	add := &cobra.Command{
		Use:   "add",
		Short: "Add a guide ref",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.GuideRefAdd(code, section, kind, target, actor); err != nil {
				return err
			}
			g, _ := s.GuideGet(code)
			return st.emit(st.stdout(), map[string]any{"guide": guideToJSON(g)}, func() {
				fmt.Fprintf(os.Stdout, "ref %s:%s added to %s\n", kind, target, section)
			})
		},
	}
	remove := &cobra.Command{
		Use:   "remove",
		Short: "Remove a guide ref",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.GuideRefRemove(code, section, kind, target, actor); err != nil {
				return err
			}
			g, _ := s.GuideGet(code)
			return st.emit(st.stdout(), map[string]any{"guide": guideToJSON(g)}, func() {
				fmt.Fprintf(os.Stdout, "ref %s:%s removed from %s\n", kind, target, section)
			})
		},
	}
	move := &cobra.Command{
		Use:   "move",
		Short: "Reorder a guide ref within a section (before=<other target>|empty=end)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.GuideRefMove(code, section, kind, target, before, actor); err != nil {
				return err
			}
			g, _ := s.GuideGet(code)
			return st.emit(st.stdout(), map[string]any{"guide": guideToJSON(g)}, func() {
				fmt.Fprintf(os.Stdout, "ref %s:%s moved\n", kind, target)
			})
		},
	}
	for _, c := range []*cobra.Command{add, remove, move} {
		c.Flags().StringVar(&code, "code", "", "project code")
		c.Flags().StringVar(&section, "section", "", "section name")
		c.Flags().StringVar(&kind, "kind", "", "ref kind: task|file")
		c.Flags().StringVar(&target, "target", "", "ref target")
		_ = c.MarkFlagRequired("code")
		_ = c.MarkFlagRequired("section")
		_ = c.MarkFlagRequired("kind")
		_ = c.MarkFlagRequired("target")
	}
	move.Flags().StringVar(&before, "before", "", "ref target to insert before (empty = end)")
	cmd.AddCommand(add, remove, move)
	return cmd
}

func newProjectGuideSetFreshnessCmd(st *cliState) *cobra.Command {
	var code, threshold string
	cmd := &cobra.Command{
		Use:   "set-freshness",
		Short: "Set or unset the guide freshness threshold (duration string or 'unset')",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.GuideSetFreshness(code, threshold, actor); err != nil {
				return err
			}
			p, err := s.GetProject(code)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{
				"code":                      p.Code,
				"guide_freshness_threshold": p.GuideFreshnessThreshold,
			}, func() {
				if p.GuideFreshnessThreshold == "" {
					fmt.Fprintf(os.Stdout, "%s freshness unset\n", p.Code)
				} else {
					fmt.Fprintf(os.Stdout, "%s freshness -> %s\n", p.Code, p.GuideFreshnessThreshold)
				}
			})
		},
	}
	cmd.Flags().StringVar(&code, "code", "", "project code")
	cmd.Flags().StringVar(&threshold, "threshold", "", "duration string (e.g. 720h) or 'unset'")
	_ = cmd.MarkFlagRequired("code")
	_ = cmd.MarkFlagRequired("threshold")
	return cmd
}

type jsonGuideCoverage struct {
	EmptySections []string `json:"empty_sections"`
	TotalSections int      `json:"total_sections"`
	TotalRefs     int      `json:"total_refs"`
}

type jsonGuideFreshness struct {
	Section   string `json:"section"`
	Kind      string `json:"kind"`
	Target    string `json:"target"`
	State     string `json:"state"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

func newProjectGuideStatusCmd(st *cliState) *cobra.Command {
	var code string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show guide coverage and freshness",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			res, err := s.GuideStatus(code)
			if err != nil {
				return err
			}
			empty := res.Coverage.EmptySections
			if empty == nil {
				empty = []string{}
			}
			fresh := make([]jsonGuideFreshness, 0, len(res.Freshness))
			for _, f := range res.Freshness {
				fresh = append(fresh, jsonGuideFreshness{
					Section:   f.Section,
					Kind:      f.Kind,
					Target:    f.Target,
					State:     f.State,
					UpdatedAt: f.UpdatedAt,
				})
			}
			return st.emit(st.stdout(), map[string]any{
				"coverage": jsonGuideCoverage{
					EmptySections: empty,
					TotalSections: res.Coverage.TotalSections,
					TotalRefs:     res.Coverage.TotalRefs,
				},
				"freshness": fresh,
			}, func() {
				fmt.Fprintf(os.Stdout, "coverage: %d section(s), %d ref(s), empty: %v\n",
					res.Coverage.TotalSections, res.Coverage.TotalRefs, res.Coverage.EmptySections)
				for _, f := range res.Freshness {
					fmt.Fprintf(os.Stdout, "  %s %s:%s [%s]\n", f.Section, f.Kind, f.Target, f.State)
				}
			})
		},
	}
	cmd.Flags().StringVar(&code, "code", "", "project code")
	_ = cmd.MarkFlagRequired("code")
	return cmd
}
