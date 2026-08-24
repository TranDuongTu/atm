package cli

import (
	"fmt"
	"strings"

	"atm/internal/capability"
	"atm/internal/core"

	"github.com/spf13/cobra"
)

// Wiring — which finished work reaches a flow capability's inbox — is PROJECT
// DATA, not capability code and not a config field. It lives as the expression
// on the capability's seeded inbox board, so it syncs with the store, shows up
// in the label registry, and is auditable like any other label. These commands
// are just a safe editor for that one expression.
//
// Every inbox expression has the same shape:
//
//	(<eligibility>) AND <self-exclusion tail>
//
// The eligibility half is the project's choice — typically an upstream
// capability's finish socket, narrowed. The tail is invariant: an inbox never
// shows work its own capability has already claimed or evicted, so the writer
// re-appends it after whatever the project sets. Nobody has to remember it.

func newProjectWiringCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wiring",
		Short: "View or change which work reaches each flow capability's inbox",
		Long: "A flow capability's intake is the expression on its inbox lane " +
			"board — project data, stored in the label substrate, synced and " +
			"auditable. Downstream capabilities wire to upstream FINISH SOCKETS " +
			"(stored labels), never to another capability's metadata. The " +
			"self-exclusion tail is maintained for you.",
	}
	cmd.AddCommand(newProjectWiringShowCmd(st))
	cmd.AddCommand(newProjectWiringSetCmd(st))
	cmd.AddCommand(newProjectWiringDefaultCmd(st))
	return cmd
}

// selfExclusion is the invariant tail for one flow capability, derived from
// the claim atoms it declares — single-sourced from the Flow contract rather
// than restated per capability.
func selfExclusion(f capability.Flow) string {
	parts := make([]string, 0, len(f.ClaimExprs()))
	for _, e := range f.ClaimExprs() {
		parts = append(parts, "NOT "+e)
	}
	return strings.Join(parts, " AND ")
}

// otherFlowsPool is the unclaimed-work pool MINUS this capability's own atoms:
// the default eligibility for a first-stage flow. Composed with the tail it is
// exactly Registry.DefaultPoolExpr, without saying the same thing twice.
func otherFlowsPool(reg *capability.Registry, name string) string {
	var parts []string
	for _, f := range reg.Flows() {
		if f.Name() == name {
			continue
		}
		for _, e := range f.ClaimExprs() {
			parts = append(parts, "NOT "+e)
		}
	}
	return strings.Join(parts, " AND ")
}

// composeInboxExpr joins an eligibility with the tail. An empty eligibility
// leaves the tail standing alone — which is exactly what the capability seeds.
func composeInboxExpr(eligibility, tail string) string {
	eligibility = strings.TrimSpace(eligibility)
	if eligibility == "" {
		return tail
	}
	if tail == "" {
		return eligibility
	}
	return "(" + eligibility + ") AND " + tail
}

// splitInboxExpr recovers the eligibility half of a stored expression. It
// reports ok=false for an expression that does not carry the tail — a
// hand-edited board, which `show` reports as-is rather than misrepresenting.
func splitInboxExpr(expr, tail string) (eligibility string, ok bool) {
	expr = strings.TrimSpace(expr)
	if expr == tail {
		return "", true
	}
	suffix := " AND " + tail
	if !strings.HasSuffix(expr, suffix) {
		return "", false
	}
	head := strings.TrimSpace(strings.TrimSuffix(expr, suffix))
	if strings.HasPrefix(head, "(") && strings.HasSuffix(head, ")") {
		head = strings.TrimSpace(head[1 : len(head)-1])
	}
	return head, true
}

// resolveFlow narrows the registry to the project's enabled set and finds the
// named flow capability. A registry capability is refused by name: it has no
// inbox to wire, and saying so is more useful than a missing-lane error later.
func resolveFlow(st *cliState, p *core.Project, name string) (capability.Flow, error) {
	reg := st.fullRegistry.For(p)
	for _, f := range reg.Flows() {
		if f.Name() == name {
			return f, nil
		}
	}
	for _, n := range reg.Names() {
		if n == name {
			return nil, fmt.Errorf("%w: capability %q is not a flow capability (it has no inbox to wire)", core.ErrUsage, name)
		}
	}
	var flows []string
	for _, f := range reg.Flows() {
		flows = append(flows, f.Name())
	}
	return nil, fmt.Errorf("%w: no flow capability %q enabled for %s (flows: %s)", core.ErrUsage, name, p.Code, strings.Join(flows, ", "))
}

// wiringRow is one capability's intake, as reported.
type wiringRow struct {
	Capability  string `json:"capability"`
	Board       string `json:"board"`
	Expr        string `json:"expr"`
	Eligibility string `json:"eligibility"`
	Tail        string `json:"tail"`
	Separable   bool   `json:"separable"`
	Seeded      bool   `json:"seeded"`
}

func newProjectWiringShowCmd(st *cliState) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show each enabled flow capability's inbox expression",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			p, err := s.GetProject(project)
			if err != nil {
				return err
			}
			byName := map[string]core.Label{}
			for _, l := range s.LabelList(project, "") {
				byName[l.Name] = l
			}
			var rows []wiringRow
			for _, f := range st.fullRegistry.For(p).Flows() {
				board := f.Lanes(project).Inbox
				tail := selfExclusion(f)
				l, seeded := byName[board]
				row := wiringRow{Capability: f.Name(), Board: board, Expr: l.Expr, Tail: tail, Seeded: seeded}
				row.Eligibility, row.Separable = splitInboxExpr(l.Expr, tail)
				rows = append(rows, row)
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "wiring": rows}, func() {
				w := st.stdout()
				for _, r := range rows {
					switch {
					case !r.Seeded:
						fmt.Fprintf(w, "%s\t%s\tNOT SEEDED (run `atm capability %s seed --project %s`)\n", r.Capability, r.Board, r.Capability, project)
					case !r.Separable:
						fmt.Fprintf(w, "%s\t%s\thand-edited: %s\n", r.Capability, r.Board, r.Expr)
					case r.Eligibility == "":
						fmt.Fprintf(w, "%s\t%s\teligibility: (none — only unclaimed work)\n", r.Capability, r.Board)
					default:
						fmt.Fprintf(w, "%s\t%s\teligibility: %s\n", r.Capability, r.Board, r.Eligibility)
					}
				}
				fmt.Fprintf(w, "%d flow capabilities enabled for %s\n", len(rows), project)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

// writeWiring is the one write path both `set` and `default` go through, so
// the tail can never be lost by taking a different route.
func writeWiring(st *cliState, project, capName, eligibility string) (wiringRow, error) {
	actor, err := st.resolveActor(true)
	if err != nil {
		return wiringRow{}, err
	}
	s, err := st.openStore()
	if err != nil {
		return wiringRow{}, err
	}
	p, err := s.GetProject(project)
	if err != nil {
		return wiringRow{}, err
	}
	f, err := resolveFlow(st, p, capName)
	if err != nil {
		return wiringRow{}, err
	}
	board := f.Lanes(project).Inbox
	tail := selfExclusion(f)
	expr := composeInboxExpr(eligibility, tail)
	// LabelAdd parses the expression and walks the board reference graph, so
	// an unparseable or cyclic wiring is refused before it is stored.
	if err := s.LabelAdd(board, "", expr, actor); err != nil {
		return wiringRow{}, err
	}
	return wiringRow{
		Capability: f.Name(), Board: board, Expr: expr,
		Eligibility: strings.TrimSpace(eligibility), Tail: tail, Separable: true, Seeded: true,
	}, nil
}

func newProjectWiringSetCmd(st *cliState) *cobra.Command {
	var project, capName, expr string
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Set a flow capability's inbox eligibility",
		Long: "--expr is the ELIGIBILITY half only; the self-exclusion tail is " +
			"appended for you. Wire downstream capabilities to upstream finish " +
			"sockets, e.g. --capability qa --expr \"scrum-stage:done AND " +
			"(scrum:task OR scrum:bug OR scrum:story)\".",
		RunE: func(cmd *cobra.Command, args []string) error {
			row, err := writeWiring(st, project, capName, expr)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "wiring": row}, func() {
				fmt.Fprintf(st.stdout(), "%s: %s -> %s\n", row.Capability, row.Board, row.Expr)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&capName, "capability", "", "flow capability name")
	cmd.Flags().StringVar(&expr, "expr", "", "inbox eligibility expression (the tail is added for you)")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("capability")
	_ = cmd.MarkFlagRequired("expr")
	return cmd
}

func newProjectWiringDefaultCmd(st *cliState) *cobra.Command {
	var project, capName string
	cmd := &cobra.Command{
		Use:   "default",
		Short: "Reset a flow capability's inbox to the unclaimed work pool",
		Long: "The first-stage default: work no enabled flow capability has " +
			"claimed or evicted. Composed with the self-exclusion tail this is " +
			"exactly the registry's default pool.",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			p, err := s.GetProject(project)
			if err != nil {
				return err
			}
			row, err := writeWiring(st, project, capName, otherFlowsPool(st.fullRegistry.For(p), capName))
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "wiring": row}, func() {
				fmt.Fprintf(st.stdout(), "%s: %s -> %s\n", row.Capability, row.Board, row.Expr)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&capName, "capability", "", "flow capability name")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("capability")
	return cmd
}
