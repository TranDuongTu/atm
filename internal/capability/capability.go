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

// ActionSpec is one manager action a capability contributes: a session mode
// the manager can be launched in. The procedure behind it lives in the
// capability's guide (Manager duty section), never in a prompt.
type ActionSpec struct {
	Name    string // the --action value, e.g. "mapping"
	Summary string // one line for the flag help and the prompt role list
}

// ManagerAction is an aggregated action entry: the ActionSpec plus which
// capability contributed it and the command an agent consults for it.
type ManagerAction struct {
	Capability string // capability name, e.g. "contextmap"
	Command    string // mounted command name, e.g. "context" (for the consult pointer)
	Name       string
	Summary    string
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
	// meaning, verb usage, operating procedure, and a "Manager duty"
	// section. Served verbatim by the uniform `guide` subcommand.
	Guide() string
	// EnsureVocabulary seeds ALL the capability's labels (stored, namespace,
	// boards) for a project, idempotently, and returns the BOARD labels
	// (Expr != "") the capability owns. One call leaves the project fully
	// seeded for this capability.
	EnsureVocabulary(svc core.LabelService, code, actor string) ([]core.Label, error)
	// Command returns the capability's cobra verb tree, built over env.
	Command(env Env) *cobra.Command
	// ManagerActions lists the manager session modes this capability
	// contributes (nil for none). The manager core (curate, recall) is not
	// a capability action — it exists for every project.
	ManagerActions() []ActionSpec
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

// Description is one capability's enumeration entry: its stable name, the
// cobra command it mounts as (what an agent types), and its one-line summary.
type Description struct {
	Name    string
	Command string
	Summary string
}

// Describe enumerates the registered capabilities in registration order.
// Command is taken from the built command tree so the consult instruction
// can never drift from what is actually mounted.
func (r *Registry) Describe(env Env) []Description {
	if r == nil {
		return nil
	}
	out := make([]Description, 0, len(r.caps))
	for _, c := range r.caps {
		out = append(out, Description{Name: c.Name(), Command: c.Command(env).Name(), Summary: c.Summary()})
	}
	return out
}

// Commands returns each capability's command tree in registration order.
// The registry, not the capability, mounts the uniform `guide` subcommand,
// so its shape is identical everywhere and cannot be forgotten.
func (r *Registry) Commands(env Env) []*cobra.Command {
	if r == nil {
		return nil
	}
	out := make([]*cobra.Command, 0, len(r.caps))
	for _, c := range r.caps {
		cmd := c.Command(env)
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

// ManagerActions aggregates the enabled capabilities' contributed manager
// actions in registration order. Command comes from the built tree, like
// Describe, so consult pointers cannot drift.
func (r *Registry) ManagerActions(env Env) []ManagerAction {
	if r == nil {
		return nil
	}
	var out []ManagerAction
	for _, c := range r.caps {
		specs := c.ManagerActions()
		if len(specs) == 0 {
			continue
		}
		cmdName := c.Command(env).Name()
		for _, s := range specs {
			out = append(out, ManagerAction{Capability: c.Name(), Command: cmdName, Name: s.Name, Summary: s.Summary})
		}
	}
	return out
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
