package cli

import (
	"fmt"
	"io"

	"atm/internal/capability"
	"atm/internal/core"

	"github.com/spf13/cobra"
)

// cliState implements capability.Env. Every method is a one-line delegation
// to an existing helper, so a command behaves identically whether its cobra
// layer lives in this package or in a capability package.
var _ capability.Env = (*cliState)(nil)

func (s *cliState) OpenService() (core.Service, error) {
	st, err := s.openStore()
	if err != nil {
		return nil, err
	}
	return st, nil
}

func (s *cliState) Stdout() io.Writer { return s.stdout() }

func (s *cliState) Stderr() io.Writer { return s.stderr() }

func (s *cliState) Emit(v any, textFn func()) error { return s.emit(s.stdout(), v, textFn) }

// RequireMutatingActor enforces that a mutating verb has an explicit actor.
// The shared resolveActor helper deliberately defaults a missing actor to
// admin@cli:unset (so read-only commands and the TUI still work), so mutating
// verbs that must attribute their work check the flag directly.
func (s *cliState) RequireMutatingActor() (string, error) {
	if s.flags.actor == "" {
		return "", fmt.Errorf("%w: mutating command requires --actor or ATM_ACTOR", ErrUsage)
	}
	return s.resolveActor(true)
}

func (s *cliState) ResolveActor(required bool) (string, error) { return s.resolveActor(required) }

func (s *cliState) BindActorFlag(cmd *cobra.Command) { bindActorFlag(cmd, s) }

func (s *cliState) BindTaskIDFlags(cmd *cobra.Command, id, legacy *string) {
	bindTaskIDFlags(cmd, id, legacy)
}

func (s *cliState) ResolveTaskID(id, legacy string) (string, error) {
	return resolveTaskID(s, id, legacy)
}

func (s *cliState) TaskJSON(t *core.Task) any { return taskToJSON(t, nil) }
