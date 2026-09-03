// Package capability defines the registry seam between the composition root
// and the capability commands (docs/architecture/logical-components.md;
// https://app.notion.com/3bc70f5f1db581a0950bfaac4e30c822).
// A capability owns a slice of the label substrate, exposes intent verbs, and
// registers its cobra command tree; the adapters (cli, tui) consume only this
// package, never a specific capability. Enable/disable is editing the slice
// the composition root passes to NewRegistry.
package capability

import (
	"fmt"
	"io"
	"strings"

	"atm/internal/core"

	"github.com/spf13/cobra"
)

// Env is the surface a capability's cobra layer builds on. internal/cli's
// cliState implements it; every method is a thin delegation to an existing
// cli helper, so a command behaves identically whether its cobra layer lives
// in cli or in a capability package.
type Env interface {
	// OpenService opens the store as the core service composite.
	OpenService() (core.Service, error)
	Stdout() io.Writer
	Stderr() io.Writer
	// Emit writes v as JSON in --output json mode, else runs textFn.
	Emit(v any, textFn func()) error
	// RequireMutatingActor errors unless --actor/ATM_ACTOR was given.
	RequireMutatingActor() (string, error)
	// ResolveActor defaults a missing actor for read-only verbs.
	ResolveActor(required bool) (string, error)
	// BindActorFlag registers the persistent --actor flag on cmd.
	BindActorFlag(cmd *cobra.Command)
	// BindTaskIDFlags registers --task and the hidden deprecated --id alias.
	BindTaskIDFlags(cmd *cobra.Command, id, legacy *string)
	// ResolveTaskID folds a deprecated --id value into --task, warning on
	// stderr; errors when neither was given.
	ResolveTaskID(id, legacy string) (string, error)
	// TaskJSON renders a task in the CLI's canonical JSON envelope shape.
	TaskJSON(t *core.Task) any
}

// Tone is a Cell's semantic emphasis. Capabilities say what a value MEANS;
// the TUI maps tones to theme colors. The contract is plain data — no ANSI,
// no styles — so it survives a future process boundary (third-party
// capability packaging, ATM-e39512).
type Tone int

const (
	ToneNeutral Tone = iota
	ToneOK
	ToneAttention
	ToneStale
)

// Cell is one interpreted annotation for the TUI's contextual column: short
// text plus emphasis, never raw payload bytes.
type Cell struct {
	Text string
	Tone Tone
	// Rank is the capability's ordinal for how soon a reader should look at
	// this state: lower sorts first. 0 means unranked — after all ranked
	// cells, before nil cells. Computed at Annotate time, never persisted,
	// and never compared across capabilities (sort is per-capability).
	Rank int
}

// Capability is one registered capability command: it owns its label slice,
// seeds its own vocabulary, and mounts its cobra verb tree.
type Capability interface {
	// Name is the stable identifier ("scrum", "checklist").
	Name() string
	// Summary is a one-line description for enumeration surfaces
	// (conventions, manager prompt). No trailing newline.
	Summary() string
	// Definition is the capability's structured self-description: lanes,
	// axes and the meaning of each value, sockets, state, invariants, and
	// what converged looks like. The `guide` subcommand RENDERS it — there
	// is no authored guide text, so prose cannot drift from the code, and
	// the verb list is walked off this capability's own command tree.
	Definition() Definition
	// Vocabulary declares every label this capability owns for the project:
	// stored labels, namespace descriptors, and boards — exactly the set
	// EnsureVocabulary seeds. Pure read, no store side effect. The registry
	// batches it across capabilities to converge a project in one write.
	Vocabulary(code string) []core.Label
	// Annotate renders this capability's interpreted cell for a task — its
	// reading of the task's labels and of its own Meta key. Pure read over
	// the task value: no store access, nil when the capability has nothing
	// to say. A capability whose own payload is unreadable reports that as a
	// cell (degrade, never panic, never leak raw bytes).
	Annotate(task core.Task) *Cell
	// EnsureVocabulary seeds ALL the capability's labels (stored, namespace,
	// boards) for a project and returns the BOARD labels (Expr != "") the
	// capability owns. Seeded descriptions are authoritative; expressions are
	// create-only through seed. One call leaves the project converged for this
	// capability. The Registry batches vocabularies across capabilities itself
	// and does not call this method; it remains the standalone
	// single-capability converge entry.
	EnsureVocabulary(svc core.LabelService, code, actor string) ([]core.Label, error)
	// Command returns the capability's cobra verb tree, built over env.
	Command(env Env) *cobra.Command
}

// Parenter is the optional "who is this task's parent" hook. A capability
// that records unit topology implements it; the answer is a task id or "".
// Same purity rule as Annotate: pure over the task value (labels + own
// payload), no store access. Optional (type-asserted) so the five
// capabilities with no parent notion implement nothing.
type Parenter interface {
	ParentOf(task core.Task) string
}

// LaneSet names a flow capability's three lane boards for a project. The
// boards themselves are seeded through EnsureVocabulary like any others;
// this struct only carries their FullNames for adapters (TUI pane [2], the
// wiring writer) to select by name, never by expression.
type LaneSet struct {
	Inbox    string
	Pipeline string
	Out      string
}

// Flow is the flow-capability contract: work moving toward a finish through
// three lanes (Inbox -> Pipeline -> Out). A registry capability simply does
// not implement it — the interface check IS the kind distinction.
type Flow interface {
	Capability
	// ClaimExprs are the expression atoms that mean "claimed by me"
	// (namespace descriptors, unprefixed: "scrum:*"). The registry's
	// DefaultPoolExpr negates the union across enabled flows.
	ClaimExprs() []string
	// FinishLabel is the declared finish socket: the stored label this
	// capability stamps on work it certifies finished. Downstream wiring
	// selects on it. Declaration over convention: the name is
	// capability-chosen, the meaning machine-readable here.
	FinishLabel(code string) core.Label
	// EvictLabel is the declared evict socket (a namespace descriptor:
	// presence of any member means evicted-by-me).
	EvictLabel(code string) core.Label
	// Lanes names the capability's three seeded lane boards.
	Lanes(code string) LaneSet
}

// Registry is an ordered collection of capabilities. All methods are
// nil-receiver safe: a nil *Registry behaves as an empty one, so adapters
// and tests constructed without capabilities keep working.
type Registry struct {
	caps []Capability
}

// NewRegistry builds a registry; order is significant (mount order,
// EnsureVocabulary order). It enforces the duty contract at construction —
// a flow capability's guide must carry a well-formed `## Duty: <persona>`
// section and a registry capability's guide must not — and panics on a
// violation: a mis-shaped capability is a composition-root programmer
// error, same as skills.MustCapability on a missing file.
// NewRegistry builds a registry from the capabilities the composition root
// enabled.
//
// It no longer polices guide prose. The old check panicked unless a flow
// capability's guide carried a `## Duty` section naming the persona that
// runs it — enforcement that only made sense while operating procedure
// lived inside capability text. Procedure now lives in the profile's
// checklists, and the contract that a shipped flow comes with an action to
// operate it is a test over the PROFILE, where both halves are visible at
// once.
func NewRegistry(caps ...Capability) *Registry {
	return &Registry{caps: caps}
}

// Flows returns the registered capabilities that implement Flow, in
// registration order. Callers narrow to the enabled set first:
// reg.For(project).Flows().
func (r *Registry) Flows() []Flow {
	if r == nil {
		return nil
	}
	var out []Flow
	for _, c := range r.caps {
		if f, ok := c.(Flow); ok {
			out = append(out, f)
		}
	}
	return out
}

// DefaultFlow is the flow capability a new project starts on. One flow is
// the whole point of the default: a project that enables every registered
// flow at birth gets downstream lanes (qa, codereview) fed by nothing, and
// a [C] switcher listing stages nobody has reached yet.
const DefaultFlow = "scrum"

// DefaultNames is the capability set a project enables when it does not
// choose: every registry capability (ambient vocabularies with no lanes —
// channel, checklist, release) plus DefaultFlow. Registration order is
// preserved. A registry missing DefaultFlow simply contributes no flow.
func (r *Registry) DefaultNames() []string {
	if r == nil {
		return nil
	}
	var out []string
	for _, c := range r.caps {
		if _, isFlow := c.(Flow); !isFlow || c.Name() == DefaultFlow {
			out = append(out, c.Name())
		}
	}
	return out
}

// DefaultPoolExpr is the unclaimed work pool over the registry's flow set:
// tasks claimed by no flow capability. "*" (every task) when there are no
// flows. This is the DEFAULT inbox eligibility for first-stage flow
// capabilities; downstream capabilities get explicit wiring instead.
func (r *Registry) DefaultPoolExpr() string {
	flows := r.Flows()
	if len(flows) == 0 {
		return "*"
	}
	var parts []string
	for _, f := range flows {
		for _, e := range f.ClaimExprs() {
			parts = append(parts, "NOT "+e)
		}
	}
	return strings.Join(parts, " AND ")
}

// Description is one capability's enumeration entry. The capability's Name
// IS its mounted command under `atm capability` — there is no separate
// command identity (Clarification 1 of the v2 spec).
type Description struct {
	Name    string
	Summary string
	Brief   string
}

// Describe enumerates the registered capabilities in registration order.
func (r *Registry) Describe() []Description {
	if r == nil {
		return nil
	}
	out := make([]Description, 0, len(r.caps))
	for _, c := range r.caps {
		// Brief was a second one-liner a capability could author for the
		// session context. One summary is enough: two descriptions of the
		// same thing is one too many to keep true.
		out = append(out, Description{Name: c.Name(), Summary: c.Summary(), Brief: c.Summary()})
	}
	return out
}

// Commands returns each capability's command tree in registration order.
// The registry, not the capability, mounts the uniform `guide` subcommand,
// so its shape is identical everywhere and cannot be forgotten.
// Mount-by-name is a structural invariant: whatever Use the capability chose,
// the mounted command answers to Name() (Clarification 1 of the v2 spec).
func (r *Registry) Commands(env Env) []*cobra.Command {
	if r == nil {
		return nil
	}
	out := make([]*cobra.Command, 0, len(r.caps))
	for _, c := range r.caps {
		cmd := c.Command(env)
		cmd.Use = c.Name()
		cmd.AddCommand(newGuideCmd(c, env, cmd))
		out = append(out, cmd)
	}
	return out
}

// newGuideCmd is the uniform read-only guide printer. It opens no store.
// verbs is the capability's own command tree: the Actions section is walked
// off it, so the documented verbs are the ones that exist.
func newGuideCmd(c Capability, env Env, verbs *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "guide",
		Short: "Print this capability's definition: lanes, axes, sockets, verbs, converged state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			guide := RenderDefinition(c, verbs)
			return env.Emit(map[string]any{
				"capability": c.Name(),
				"summary":    c.Summary(),
				"guide":      guide,
			}, func() {
				fmt.Fprint(env.Stdout(), guide)
			})
		},
	}
}

// EnsureVocabulary converges every registered capability's vocabulary for
// the project in ONE LabelSeedBatch transaction (one event-log fold per
// select — ATM-40faff), and returns the union of the board labels
// (Expr != "") in registration order, vocabulary order within a
// capability. It relies on the Capability contract that Vocabulary
// declares exactly the set EnsureVocabulary seeds; per-capability
// EnsureVocabulary remains the standalone single-capability converge.
func (r *Registry) EnsureVocabulary(svc core.LabelService, code, actor string) ([]core.Label, error) {
	if r == nil {
		return nil, nil
	}
	var all []core.Label
	for _, c := range r.caps {
		all = append(all, c.Vocabulary(code)...)
	}
	if len(all) == 0 {
		return nil, nil
	}
	if err := svc.LabelSeedBatch(all, actor); err != nil {
		return nil, err
	}
	var boards []core.Label
	for _, l := range all {
		if l.Expr != "" {
			boards = append(boards, l)
		}
	}
	return boards, nil
}

// Names lists the registered capability names in registration order.
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.caps))
	for _, c := range r.caps {
		out = append(out, c.Name())
	}
	return out
}

// For narrows the registry to the project's enabled set. A nil project or a
// project with no recorded capability choice (Capabilities == nil — every
// project born before enablement existed) keeps the full registry: legacy
// projects read as "all built-ins enabled", with no migration event. The
// fence is on the tooling surface only; the store keeps accepting anything.
func (r *Registry) For(p *core.Project) *Registry {
	if r == nil || p == nil || p.Capabilities == nil {
		return r
	}
	enabled := make(map[string]bool, len(p.Capabilities))
	for _, n := range p.Capabilities {
		enabled[n] = true
	}
	kept := make([]Capability, 0, len(r.caps))
	for _, c := range r.caps {
		if enabled[c.Name()] {
			kept = append(kept, c)
		}
	}
	return &Registry{caps: kept}
}

// Annotate resolves the named capability and renders its cell for the task.
// Nil for unknown names, and when the capability has nothing to say.
func (r *Registry) Annotate(capName string, t core.Task) *Cell {
	if r == nil {
		return nil
	}
	for _, c := range r.caps {
		if c.Name() == capName {
			return c.Annotate(t)
		}
	}
	return nil
}

// ParentOf resolves the named capability and asks its Parenter hook, if it
// has one. "" for unknown names, non-Parenter capabilities, and no-parent.
func (r *Registry) ParentOf(capName string, t core.Task) string {
	if r == nil {
		return ""
	}
	for _, c := range r.caps {
		if c.Name() == capName {
			if p, ok := c.(Parenter); ok {
				return p.ParentOf(t)
			}
			return ""
		}
	}
	return ""
}
