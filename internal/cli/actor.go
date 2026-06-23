package cli

import (
	"fmt"

	"atm/internal/store"

	"github.com/spf13/cobra"
)

func newActorCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "actor",
		Short: "Actor registry commands",
	}
	cmd.AddCommand(newActorListCmd(st))
	cmd.AddCommand(newActorShowCmd(st))
	return cmd
}

func newActorListCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all registered actors",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			actors := s.List()
			return st.emit(st.stdout(), map[string]any{"actors": actorsToJSON(actors)}, func() {
				for _, a := range actors {
					fmt.Fprintf(st.stdout(), "%s %s\n", a.ID, a.Name)
				}
			})
		},
	}
	return cmd
}

func newActorShowCmd(st *cliState) *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show an actor with a summary of claimed tasks and open followups",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			a, err := s.Get(id)
			if err != nil {
				return err
			}
			claimedTasks := s.ListTasks(store.QueryFilters{Claimant: id})
			claimedIDs := make([]string, 0, len(claimedTasks))
			for _, t := range claimedTasks {
				claimedIDs = append(claimedIDs, t.ID)
			}
			openFollowups := 0
			for _, t := range claimedTasks {
				for _, f := range t.Followups {
					if f.Status == "open" {
						openFollowups++
					}
				}
			}
			return st.emit(st.stdout(), map[string]any{
				"actor":          actorToJSON(a),
				"claimed_tasks":  claimedIDs,
				"open_followups": openFollowups,
			}, func() {
				fmt.Fprintf(st.stdout(), "%s %s first_seen=%s\n", a.ID, a.Name, renderTime(a.FirstSeen))
				fmt.Fprintf(st.stdout(), "claimed tasks: %d\n", len(claimedIDs))
				fmt.Fprintf(st.stdout(), "open followups: %d\n", openFollowups)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "actor id")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

type jsonActor struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	FirstSeen string `json:"first_seen"`
}

func actorToJSON(a store.Actor) jsonActor {
	return jsonActor{
		ID:        a.ID,
		Kind:      a.Kind,
		Name:      a.Name,
		FirstSeen: renderTime(a.FirstSeen),
	}
}

func actorsToJSON(actors []store.Actor) []jsonActor {
	out := make([]jsonActor, 0, len(actors))
	for _, a := range actors {
		out = append(out, actorToJSON(a))
	}
	return out
}
