// Package capability defines the registry seam between the composition root
// and the capability commands (docs/architecture/logical-components.md;
// docs/superpowers/specs/2026-07-17-capability-registry-contextmap-design.md).
// A capability owns a slice of the label substrate, exposes intent verbs, and
// registers its cobra command tree; the adapters (cli, tui) consume only this
// package, never a specific capability. Enable/disable is editing the slice
// the composition root passes to NewRegistry.
package capability

import (
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

// Capability is one registered capability command: it owns its label slice,
// seeds its own vocabulary, and mounts its cobra verb tree.
type Capability interface {
	// Name is the stable identifier ("contextmap", "workflow").
	Name() string
	// EnsureVocabulary seeds the capability's labels and boards for a
	// project. Idempotent; never overwrites curated descriptions.
	EnsureVocabulary(svc core.LabelService, code, actor string) error
	// Command returns the capability's cobra verb tree, built over env.
	Command(env Env) *cobra.Command
	// DefaultBoard nominates the board a UI should select by default for
	// the project, or "" when this capability nominates none.
	DefaultBoard(code string) string
}

// Registry is an ordered collection of capabilities. All methods are
// nil-receiver safe: a nil *Registry behaves as an empty one, so adapters
// and tests constructed without capabilities keep working.
type Registry struct {
	caps []Capability
}

// NewRegistry builds a registry; order is significant (mount order,
// EnsureVocabulary order, DefaultBoard precedence).
func NewRegistry(caps ...Capability) *Registry { return &Registry{caps: caps} }

// Commands returns each capability's command tree in registration order.
func (r *Registry) Commands(env Env) []*cobra.Command {
	if r == nil {
		return nil
	}
	out := make([]*cobra.Command, 0, len(r.caps))
	for _, c := range r.caps {
		out = append(out, c.Command(env))
	}
	return out
}

// EnsureVocabulary seeds every capability's vocabulary for the project,
// stopping at the first error.
func (r *Registry) EnsureVocabulary(svc core.LabelService, code, actor string) error {
	if r == nil {
		return nil
	}
	for _, c := range r.caps {
		if err := c.EnsureVocabulary(svc, code, actor); err != nil {
			return err
		}
	}
	return nil
}

// DefaultBoard returns the first non-empty default-board nomination in
// registration order, or "" when no capability nominates one.
func (r *Registry) DefaultBoard(code string) string {
	if r == nil {
		return ""
	}
	for _, c := range r.caps {
		if b := c.DefaultBoard(code); b != "" {
			return b
		}
	}
	return ""
}
