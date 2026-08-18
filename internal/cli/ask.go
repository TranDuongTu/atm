package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"atm/internal/answer"
	"atm/internal/chat"
	"atm/internal/embed"

	"github.com/spf13/cobra"
)

// askResult is the whole contract of `atm ask --output json`: ONE document,
// with every key present on every outcome. No omitempty anywhere, and the
// slices are initialized so they marshal as [] rather than null — an agent
// indexes this without existence checks, and a key that vanishes on the
// degraded path is a key it cannot rely on.
type askResult struct {
	Answer     string    `json:"answer"`
	Citations  []jsonHit `json:"citations"`
	Hits       []jsonHit `json:"hits"`
	ChatModel  string    `json:"chat_model"`
	EmbedModel string    `json:"embed_model"`
	Session    string    `json:"session"`
	Behind     int       `json:"behind"`
	Degraded   bool      `json:"degraded"`
	Truncated  bool      `json:"truncated"`
	Error      string    `json:"error"`
}

func newAskCmd(st *cliState) *cobra.Command {
	var project string
	var k int
	cmd := &cobra.Command{
		Use:   "ask \"question\"",
		Short: "Ask a question answered from this project's own tasks and comments",
		Long: "Retrieves the project's most relevant tasks and comments and has the configured local " +
			"chat model answer from them, citing each claim by source number. Retrieval never breaks " +
			"because generation cannot: with no chat model configured, or an endpoint that cannot be " +
			"reached, the sources are still returned with degraded set and an exit code of 0.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				project = os.Getenv("ATM_PROJECT")
			}
			if project == "" {
				return fmt.Errorf("%w: --project is required (or set ATM_PROJECT)", ErrUsage)
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if _, err := s.GetProject(project); err != nil {
				return fmt.Errorf("%w: project %s not found", ErrNotFound, project)
			}
			cfg, err := s.GetProjectConfig(project)
			if err != nil {
				return err
			}
			acfg := answer.Config{Project: project, Searcher: s, K: k}
			res := askResult{Citations: []jsonHit{}, Hits: []jsonHit{}}
			if cfg != nil && cfg.Embedding != nil {
				client := embed.New(*cfg.Embedding)
				acfg.Embed = func(ctx context.Context, text, role string) ([]float64, error) {
					return client.Embed(ctx, text, role)
				}
				acfg.Model = cfg.Embedding.Model
				acfg.Threshold = cfg.Embedding.Threshold
				res.EmbedModel = cfg.Embedding.Model
			}
			// Assigned ONLY inside the branch. Declaring a *chat.Client above and
			// assigning it unconditionally would put a typed nil in the interface,
			// and answer.Config.Chat != nil would be true for a project with no chat
			// model — turning a clean degrade into a nil-pointer panic.
			if cfg != nil && cfg.Chat != nil {
				acfg.Chat = chat.New(*cfg.Chat)
				res.ChatModel = cfg.Chat.Model
			}
			eng := answer.New(acfg)
			var b strings.Builder
			err = eng.Ask(cmd.Context(), answer.Query{Question: args[0]}, func(e answer.Event) {
				switch ev := e.(type) {
				case answer.Retrieved:
					res.Hits = hitsToJSON(ev.Hits)
					res.Behind = ev.Behind
				case answer.Delta:
					b.WriteString(ev.Text)
				case answer.Done:
					res.Citations = hitsToJSON(ev.Citations)
					res.Degraded = ev.Degraded
					if ev.Degraded {
						res.Error = ev.Reason
					}
				case answer.Failed:
					// Both a disconnect and an expired deadline land here. The
					// partial is kept and the outcome stays exit 0 — an answer that
					// was cut short is still an answer, and a script gets the same
					// document shape either way.
					res.Truncated = true
					res.Error = ev.Reason
					if ev.Canceled {
						res.Error = "canceled"
					}
				}
			})
			res.Answer = b.String()
			if err != nil {
				// The only non-nil returns are a broken ledger and a malformed
				// call. Everything else is an exit-0 outcome carried by the
				// document, so this is the whole exit-code rule.
				return err
			}
			return st.emit(st.stdout(), res, func() {
				printAskText(st, res)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().IntVar(&k, "k", 0, "how many sources to retrieve (default: the engine's 8)")
	return cmd
}

// printAskText renders the non-streaming remainder of text mode. Task 11 adds
// live delta printing above it; this prints what a reader needs after the
// answer: which sources it leaned on, or why there was no answer.
func printAskText(st *cliState, res askResult) {
	out := st.stdout()
	if res.Degraded {
		fmt.Fprintf(out, "no answer generated: %s\n\n", res.Error)
	}
	if res.Truncated {
		fmt.Fprintf(out, "\n\n⚠ answer interrupted: %s\n", res.Error)
	}
	cited := res.Citations
	if len(cited) == 0 {
		cited = res.Hits
	}
	if len(cited) == 0 {
		return
	}
	fmt.Fprintf(out, "\nSOURCES\n")
	for i, h := range cited {
		label := h.Title
		if label == "" {
			label = h.Snippet
		}
		fmt.Fprintf(out, "[%d] %s (%s) %s\n", i+1, h.ID, h.Kind, label)
	}
	if res.Behind > 0 {
		fmt.Fprintf(out, "\nsources may lag · %d items still indexing\n", res.Behind)
	}
}
