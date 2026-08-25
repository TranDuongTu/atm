package scrum

import (
	"io"
	"strings"
	"testing"

	"atm/internal/capability"
	"atm/internal/core"

	"github.com/spf13/cobra"
)

// stubEnv is enough capability.Env to BUILD the command tree — the guide test
// only enumerates it, never runs a verb.
type stubEnv struct{}

func (stubEnv) OpenService() (core.Service, error)              { return nil, nil }
func (stubEnv) Stdout() io.Writer                               { return io.Discard }
func (stubEnv) Stderr() io.Writer                               { return io.Discard }
func (stubEnv) Emit(any, func()) error                          { return nil }
func (stubEnv) RequireMutatingActor() (string, error)           { return testActor, nil }
func (stubEnv) ResolveActor(bool) (string, error)               { return testActor, nil }
func (stubEnv) BindActorFlag(*cobra.Command)                    {}
func (stubEnv) ResolveTaskID(id, legacy string) (string, error) { return id, nil }
func (stubEnv) TaskJSON(t *core.Task) any                       { return t }
func (stubEnv) BindTaskIDFlags(cmd *cobra.Command, id, legacy *string) {
	cmd.Flags().StringVar(id, "task", "", "task id")
}

func TestGuideHasTheRequiredSections(t *testing.T) {
	guide := Cap{}.Guide()
	for _, want := range []string{"## Semantics", "## Actions", "## Converge"} {
		if !strings.Contains(guide, want) {
			t.Errorf("guide missing %q", want)
		}
	}
}

// Every mounted verb must be documented: a verb nobody can discover is a verb
// nobody calls, and the guide is the only discovery surface an agent has.
func TestGuideDocumentsEveryMountedVerb(t *testing.T) {
	guide := Cap{}.Guide()
	for _, sub := range (Cap{}).Command(stubEnv{}).Commands() {
		if !strings.Contains(guide, "atm capability scrum "+sub.Name()) {
			t.Errorf("guide does not document the %q verb", sub.Name())
		}
	}
}

func TestGuideNamesTheSocketsAndLanes(t *testing.T) {
	guide := Cap{}.Guide()
	c := New()
	for _, want := range []string{
		c.FinishLabel("<CODE>").Name, c.EvictLabel("<CODE>").Name,
		"scrum-inbox", "scrum-pipeline", "scrum-out-board",
		`Meta["scrum"]`, "part_of", "depends_on", "covered_by", "spec", "plan",
	} {
		if !strings.Contains(guide, want) {
			t.Errorf("guide missing %q", want)
		}
	}
}

func TestGuideFrontmatterMatchesTheCode(t *testing.T) {
	c := New()
	lanes := c.Lanes("ATM")
	guide := c.Guide()
	for _, n := range []string{lanes.Inbox, lanes.Pipeline, lanes.Out} {
		if !strings.Contains(guide, strings.TrimPrefix(n, "ATM:")) {
			t.Fatalf("guide does not name the %q lane board", n)
		}
	}
	var _ capability.Flow = c
}
