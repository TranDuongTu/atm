// Package capability defines the registry seam between the composition root
// and the capability commands (docs/architecture/logical-components.md;
// docs/superpowers/specs/2026-07-17-capability-registry-contextmap-design.md).
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

// Capability is one registered capability command: it owns its label slice,
// seeds its own vocabulary, and mounts its cobra verb tree.
type Capability interface {
	// Name is the stable identifier ("contextmap", "workflow").
	Name() string
	// Summary is a one-line description for enumeration surfaces
	// (conventions, manager prompt). No trailing newline.
	Summary() string
	// Guide is the capability's full agent-facing semantics: vocabulary
	// meaning, verb usage, operating procedure, and `## Brief` / `## Autopilot`
	// sections (spec §7). Served verbatim by the uniform `guide` subcommand.
	Guide() string
	// Vocabulary declares every label this capability owns for the project:
	// stored labels, namespace descriptors, and boards — exactly the set
	// EnsureVocabulary seeds. Pure read, no store side effect. This is the
	// OWNERSHIP surface: Registry.Unmanaged subtracts it.
	Vocabulary(code string) []core.Label
	// Exposed declares the computed labels (boards + namespace descriptors)
	// this capability surfaces in the TUI ring for the project. Pure read,
	// no store side effect. Order within the slice is the capability's
	// preferred ring order; the registry preserves registration order across
	// capabilities. Invariant: Exposed ⊆ Vocabulary.
	Exposed(code string) []core.Label
	// EnsureVocabulary seeds ALL the capability's labels (stored, namespace,
	// boards) for a project, idempotently, and returns the BOARD labels
	// (Expr != "") the capability owns. One call leaves the project fully
	// seeded for this capability.
	EnsureVocabulary(svc core.LabelService, code, actor string) ([]core.Label, error)
	// Command returns the capability's cobra verb tree, built over env.
	Command(env Env) *cobra.Command
}

// Registry is an ordered collection of capabilities. All methods are
// nil-receiver safe: a nil *Registry behaves as an empty one, so adapters
// and tests constructed without capabilities keep working.
type Registry struct {
	caps []Capability
}

// NewRegistry builds a registry; order is significant (mount order,
// EnsureVocabulary order).
func NewRegistry(caps ...Capability) *Registry { return &Registry{caps: caps} }

// Description is one capability's enumeration entry. The capability's Name
// IS its mounted command under `atm capability` — there is no separate
// command identity (Clarification 1 of the v2 spec).
type Description struct {
	Name    string
	Summary string
}

// Describe enumerates the registered capabilities in registration order.
func (r *Registry) Describe() []Description {
	if r == nil {
		return nil
	}
	out := make([]Description, 0, len(r.caps))
	for _, c := range r.caps {
		out = append(out, Description{Name: c.Name(), Summary: c.Summary()})
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
		cmd.AddCommand(newGuideCmd(c, env))
		out = append(out, cmd)
	}
	return out
}

// newGuideCmd is the uniform read-only guide printer. It opens no store.
func newGuideCmd(c Capability, env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "guide",
		Short: "Print this capability's agent guide (semantics and operating mode)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return env.Emit(map[string]any{
				"capability": c.Name(),
				"summary":    c.Summary(),
				"guide":      c.Guide(),
			}, func() {
				fmt.Fprint(env.Stdout(), c.Guide())
			})
		},
	}
}

// EnsureVocabulary seeds every capability's vocabulary for the project,
// stopping at the first error, and returns the union of the boards the
// capabilities own, in registration order.
func (r *Registry) EnsureVocabulary(svc core.LabelService, code, actor string) ([]core.Label, error) {
	if r == nil {
		return nil, nil
	}
	var boards []core.Label
	for _, c := range r.caps {
		bs, err := c.EnsureVocabulary(svc, code, actor)
		if err != nil {
			return nil, err
		}
		boards = append(boards, bs...)
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

// UmbrellaFullName is the synthetic "unmanaged" umbrella row's identifier for
// a project: a TUI/CLI sentinel, never a real label. It is used only as a ring
// row FullName and as an order/hidden key in the project's boards config.
func UmbrellaFullName(code string) string { return code + ":unmanaged" }

// ExposedLabel is one ring entry: a capability-surfaced label tagged with the
// owning capability's name (rendered as the muted owner column in the TUI).
type ExposedLabel struct {
	Label core.Label
	Owner string
}

// Exposed enumerates every registered capability's exposed labels in
// registration order (each capability's own preferred order preserved
// within its block), tagged with the owner name.
func (r *Registry) Exposed(code string) []ExposedLabel {
	if r == nil {
		return nil
	}
	var out []ExposedLabel
	for _, c := range r.caps {
		for _, l := range c.Exposed(code) {
			out = append(out, ExposedLabel{Label: l, Owner: c.Name()})
		}
	}
	return out
}

// Unmanaged returns labels in the project's LabelList that no registered
// capability owns via Vocabulary. A label is owned when its FullName is in
// the vocabulary union, or when it sits under an owned namespace descriptor
// (<code>:<ns>:<value> with <code>:<ns>:* owned). Derived, not stored. The
// TUI renders these in the unmanaged capability view; `atm capability
// unmanaged` exposes the same set to the manager agent for triage. Callers
// narrow to the enabled set first: reg.For(project).Unmanaged(...).
func (r *Registry) Unmanaged(svc core.LabelService, code string) ([]core.Label, error) {
	var vocab []core.Label
	if r != nil {
		for _, c := range r.caps {
			vocab = append(vocab, c.Vocabulary(code)...)
		}
	}
	owned := NewLabelSet(vocab)
	var out []core.Label
	for _, l := range svc.LabelList(code, "") {
		if !owned.Contains(l.Name) {
			out = append(out, l)
		}
	}
	return out, nil
}

// LabelSet is an ownership matcher over a label list: exact FullNames plus
// member prefixes derived from namespace descriptors (<code>:<ns>:* owns
// every <code>:<ns>:<value>). Registry.Unmanaged and the TUI's capability
// task counts share it, so the ownership rule stays single-sourced.
type LabelSet struct {
	exact    map[string]bool
	prefixes []string
}

// NewLabelSet indexes labels for Contains lookups.
func NewLabelSet(labels []core.Label) LabelSet {
	s := LabelSet{exact: make(map[string]bool, len(labels))}
	for _, l := range labels {
		s.exact[l.Name] = true
		if core.IsNamespaceName(l.Name) {
			// "<code>:<ns>:*" -> member prefix "<code>:<ns>:"
			s.prefixes = append(s.prefixes, strings.TrimSuffix(l.Name, "*"))
		}
	}
	return s
}

// Contains reports whether fullName is owned by the set: an exact member, or
// a member of an owned namespace descriptor.
func (s LabelSet) Contains(fullName string) bool {
	if s.exact[fullName] {
		return true
	}
	for _, p := range s.prefixes {
		if strings.HasPrefix(fullName, p) {
			return true
		}
	}
	return false
}

// OwnedLabels returns the named registered capability's vocabulary for code,
// or nil when the name is not registered. Pure read, no store side effect.
// The TUI's capability-view header counts tasks against NewLabelSet of this.
func (r *Registry) OwnedLabels(code, capName string) []core.Label {
	if r == nil {
		return nil
	}
	for _, c := range r.caps {
		if c.Name() == capName {
			return c.Vocabulary(code)
		}
	}
	return nil
}

// OrderFullNames applies a partial order override to an effective ring order:
// override names present in effective come first (override order, duplicates
// dropped), then every remaining effective name in its original order.
// Override entries naming nothing in effective are silently ignored —
// defensive against typos and stale entries after a capability is disabled.
func OrderFullNames(effective, override []string) []string {
	present := make(map[string]bool, len(effective))
	for _, n := range effective {
		present[n] = true
	}
	out := make([]string, 0, len(effective))
	taken := make(map[string]bool, len(effective))
	for _, n := range override {
		if present[n] && !taken[n] {
			out = append(out, n)
			taken[n] = true
		}
	}
	for _, n := range effective {
		if !taken[n] {
			out = append(out, n)
		}
	}
	return out
}
