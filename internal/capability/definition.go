package capability

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// A capability's guide is GENERATED from this description, never authored.
//
// The litmus line: an invariant the system enforces, or a meaning a term
// carries, is capability DEFINITION; a decision an agent makes is a
// checklist. Prose that told an agent what to do lived here once and now
// lives in the profile's checklists, where it can be edited per project.
//
// Generating rather than authoring buys one thing above all: the verb list
// is walked off the capability's own cobra tree, so a flag renamed in code
// cannot leave a guide describing the old one. Everything else here is a
// short annotation — the vocabulary a term carries — which code cannot
// derive and a human must write.

// Definition is one capability's structured self-description.
type Definition struct {
	// Identity says what this capability IS, in a paragraph or two. No
	// procedure, no advice.
	Identity string
	// Lanes are the three boards a flow capability declares; empty for a
	// registry capability, and the emptiness IS the kind distinction.
	Lanes []LaneDoc
	// Axes are the label namespaces this capability owns, with the meaning
	// of each value.
	Axes []Axis
	// Sockets are the declared attachment points other capabilities wire to.
	Sockets []Socket
	// State describes the capability's own Meta payload.
	State StateDoc
	// Converge describes what a healthy, settled project looks like for
	// this capability — the target an agent steers toward.
	Converge []string
	// Invariants are the rules the verbs enforce, stated so an agent knows
	// what will refuse it before it tries.
	Invariants []string
}

// LaneDoc is one lane board: the label name (with its <CODE> placeholder),
// the expression behind it, and what standing in it means.
type LaneDoc struct {
	Label   string
	Expr    string
	Meaning string
}

// Axis is one label namespace and the values it takes.
type Axis struct {
	// Namespace is unprefixed ("scrum-stage"); the renderer adds <CODE>.
	Namespace string
	Meaning   string
	Values    []AxisValue
	// Ordered marks an axis whose values form a sequence, so the renderer
	// says so rather than leaving a reader to guess.
	Ordered bool
}

// AxisValue is one value of an axis and what it means.
type AxisValue struct {
	Value   string
	Meaning string
}

// Socket is a declared attachment point: the label another capability wires
// its intake to, and what stamping it certifies.
type Socket struct {
	// Role is FINISH or EVICT.
	Role    string
	Label   string
	Meaning string
}

// StateDoc describes the capability's Meta payload.
type StateDoc struct {
	Key    string
	Intro  string
	Fields []StateField
}

// StateField is one payload field and what it records.
type StateField struct {
	Name    string
	Meaning string
}

// Socket roles.
const (
	SocketFinish = "FINISH"
	SocketEvict  = "EVICT"
)

// RenderDefinition renders the agent-facing guide for a capability. verbs is
// the capability's own cobra tree, walked for the Actions section so the
// documented verbs and flags are the ones that actually exist.
func RenderDefinition(c Capability, verbs *cobra.Command) string {
	d := c.Definition()
	var b strings.Builder

	kind := "registry"
	if _, isFlow := c.(Flow); isFlow {
		kind = "flow"
	}
	fmt.Fprintf(&b, "# %s capability — definition\n\n", c.Name())
	fmt.Fprintf(&b, "%s\n\n", strings.TrimSpace(d.Identity))
	fmt.Fprintf(&b, "This is a %s capability. ", kind)
	if kind == "flow" {
		b.WriteString("It sees the project as three lanes and declares where work enters, finishes, and leaves.\n\n")
	} else {
		b.WriteString("It owns a vocabulary and verbs, but no lanes: nothing moves through it toward a finish.\n\n")
	}
	b.WriteString("This text is GENERATED from the capability's own declarations. It describes what the words mean and what the verbs refuse — never what you should decide. Decisions live in your checklists.\n")

	if len(d.Lanes) > 0 {
		b.WriteString("\n## Lanes\n\n")
		for _, l := range d.Lanes {
			fmt.Fprintf(&b, "- `%s` — %s\n", l.Label, l.Meaning)
			if l.Expr != "" {
				fmt.Fprintf(&b, "  expression: `%s`\n", l.Expr)
			}
		}
	}

	if len(d.Axes) > 0 {
		b.WriteString("\n## Axes\n")
		for _, a := range d.Axes {
			fmt.Fprintf(&b, "\n### `<CODE>:%s:*`\n\n%s\n", a.Namespace, a.Meaning)
			if a.Ordered {
				b.WriteString("\nThe values are ORDERED; a unit moves along them.\n")
			}
			if len(a.Values) > 0 {
				b.WriteString("\n| value | means |\n|---|---|\n")
				for _, v := range a.Values {
					fmt.Fprintf(&b, "| `%s` | %s |\n", v.Value, v.Meaning)
				}
			}
		}
	}

	if len(d.Sockets) > 0 {
		b.WriteString("\n## Sockets\n\nThe attachment points other capabilities wire their intake to. Both are STORED LABELS, never metadata: intake expressions are evaluated by the store's resolver, which never reads a payload.\n\n")
		for _, s := range d.Sockets {
			fmt.Fprintf(&b, "- %s: `%s` — %s\n", s.Role, s.Label, s.Meaning)
		}
	}

	if len(d.State.Fields) > 0 {
		b.WriteString("\n## State\n\n")
		if d.State.Intro != "" {
			fmt.Fprintf(&b, "%s\n\n", d.State.Intro)
		}
		fmt.Fprintf(&b, "`Meta[%q]`:\n\n", d.State.Key)
		for _, f := range d.State.Fields {
			fmt.Fprintf(&b, "- `%s` — %s\n", f.Name, f.Meaning)
		}
	}

	if len(d.Invariants) > 0 {
		b.WriteString("\n## Invariants\n\nWhat the verbs enforce. The store enforces none of it — this is a paved road, not a fence — but these verbs refuse rather than corrupt.\n\n")
		for _, inv := range d.Invariants {
			fmt.Fprintf(&b, "- %s\n", inv)
		}
	}

	if verbs != nil {
		b.WriteString(renderVerbs(c.Name(), verbs))
	}

	if len(d.Converge) > 0 {
		b.WriteString("\n## Converged\n\nA converged project reads like this:\n\n")
		for _, line := range d.Converge {
			fmt.Fprintf(&b, "- %s\n", line)
		}
	}
	return b.String()
}

// renderVerbs walks the capability's cobra tree. Reading the verbs off the
// command definitions is the point: a renamed flag cannot leave stale prose
// behind, because there is no prose to go stale.
func renderVerbs(name string, root *cobra.Command) string {
	subs := append([]*cobra.Command(nil), root.Commands()...)
	sort.Slice(subs, func(i, j int) bool { return subs[i].Name() < subs[j].Name() })

	var b strings.Builder
	b.WriteString("\n## Actions\n\nRead off the command tree itself, so these are the verbs and flags that exist.\n\n")
	for _, sub := range subs {
		// `guide` is the uniform meta-verb the registry mounts on every
		// capability; a reader of the guide has already run it.
		if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" || sub.Name() == "guide" {
			continue
		}
		fmt.Fprintf(&b, "- `atm capability %s %s%s` — %s\n", name, sub.Name(), flagUsage(sub), strings.TrimSpace(sub.Short))
		if long := strings.TrimSpace(sub.Long); long != "" && long != strings.TrimSpace(sub.Short) {
			for _, line := range strings.Split(long, "\n") {
				fmt.Fprintf(&b, "  %s\n", strings.TrimSpace(line))
			}
		}
	}
	return b.String()
}

// flagUsage renders a verb's own flags in the order cobra holds them,
// required ones first and unbracketed so the shape of a legal call is
// visible at a glance.
func flagUsage(cmd *cobra.Command) string {
	var required, optional []string
	inherited := cmd.InheritedFlags()
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		// Global flags (--actor, --store, --output) belong to `atm`, not to
		// this verb; listing them on every line would bury the ones that
		// actually shape the call.
		if f.Hidden || inherited.Lookup(f.Name) != nil {
			return
		}
		text := "--" + f.Name
		if f.Value.Type() != "bool" {
			text += " <" + f.Name + ">"
		}
		if isRequired(cmd, f.Name) {
			required = append(required, text)
			return
		}
		optional = append(optional, "["+text+"]")
	})
	all := append(required, optional...)
	if len(all) == 0 {
		return ""
	}
	return " " + strings.Join(all, " ")
}

func isRequired(cmd *cobra.Command, name string) bool {
	f := cmd.Flags().Lookup(name)
	if f == nil || f.Annotations == nil {
		return false
	}
	for _, v := range f.Annotations[cobra.BashCompOneRequiredFlag] {
		if v == "true" {
			return true
		}
	}
	return false
}
