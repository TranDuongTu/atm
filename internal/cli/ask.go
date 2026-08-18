package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"atm/internal/answer"
	"atm/internal/chat"
	"atm/internal/core"
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

// askHistoryTurns bounds how much of a session is replayed. A long-lived
// session id would otherwise grow the prompt without limit, competing with the
// source budget for the same context window.
const askHistoryTurns = 10

func newAskCmd(st *cliState) *cobra.Command {
	var project string
	var k int
	var timeout time.Duration
	var session string
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
			// SIGINT follows atm index watch's precedent — this verb streams, so
			// ctrl-C must cancel the generation rather than kill the process
			// mid-write. It is also what makes Failed{Canceled:true} reachable
			// from the CLI at all.
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			if timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
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
			streamText := !st.isJSON()
			var b strings.Builder
			// Set in the answer.Failed arm below. A cancel is the user
			// REJECTING the answer, not a deadline running out on one they
			// still wanted — see the recording condition after Ask returns.
			var canceled bool
			q := answer.Query{Question: args[0]}
			if session != "" {
				res.Session = session
				turns, err := s.ReadAskTurns(project, session)
				if err != nil {
					// A rejected session id is a usage error and must surface: an
					// agent that mistyped an id should learn it rather than get a
					// silently different conversation.
					return err
				}
				if len(turns) > askHistoryTurns {
					turns = turns[len(turns)-askHistoryTurns:]
				}
				for _, t := range turns {
					q.History = append(q.History, answer.Turn{Question: t.Question, Answer: t.Answer})
				}
			}
			err = eng.Ask(ctx, q, func(e answer.Event) {
				switch ev := e.(type) {
				case answer.Retrieved:
					res.Hits = hitsToJSON(ev.Hits)
					res.Behind = ev.Behind
				case answer.Delta:
					b.WriteString(ev.Text)
					if streamText {
						// Text mode only. JSON mode buffers and emits one document:
						// a stream of partial JSON is not parseable, and the document
						// shape is this verb's whole contract for scripts.
						fmt.Fprint(st.stdout(), ev.Text)
					}
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
						canceled = true
					}
				}
			})
			res.Answer = b.String()
			// Recorded only when there is an answer to record. A degraded ask
			// generated nothing, and an empty assistant turn replayed as history
			// poisons every later turn in the session. A truncated partial IS
			// recorded — that text is genuinely what the conversation contained.
			// A CANCELED partial is not: a cancel is the user rejecting the
			// answer, not the clock running out on one they still wanted, so it
			// must not be replayed as the assistant's real prior reply on the
			// next turn (ATM-d4ceed).
			if session != "" && err == nil && !canceled && strings.TrimSpace(res.Answer) != "" {
				if aerr := s.AppendAskTurn(project, session, core.AskTurn{Question: args[0], Answer: res.Answer}); aerr != nil {
					fmt.Fprintf(st.stderr(), "warning: could not record the turn: %v\n", aerr)
				}
			}
			if err != nil {
				// The only non-nil returns are a broken ledger and a malformed
				// call. Everything else is an exit-0 outcome carried by the
				// document, so this is the whole exit-code rule.
				return err
			}
			logInquiry(st, s, cfg, project, args[0], idsOf(res.Hits), idsOf(res.Citations))
			return st.emit(st.stdout(), res, func() {
				printAskText(st, res)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().IntVar(&k, "k", 0, "how many sources to retrieve (default: the engine's 8)")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "give up after this long (default: none; internal/chat still bounds an idle stream)")
	cmd.Flags().StringVar(&session, "session", "", "conversation id; consecutive calls with the same id share history")
	return cmd
}

// logInquiry appends one query to inquiry-log.jsonl, the ground-truth stream
// the eval subsystem replays (ATM-028a8d). It NEVER fails the command: by the
// time it runs the answer has already streamed to the user, and a side-effect
// write must not turn a delivered answer into an error. The warning goes to
// stderr so JSON mode's stdout stays parseable.
func logInquiry(st *cliState, s core.Service, cfg *core.ProjectConfig, project, query string, returned, cited []string) {
	// nil means enabled: a plain bool would read every project predating the
	// field as opted out.
	if cfg != nil && cfg.InquiryLog != nil && !*cfg.InquiryLog {
		return
	}
	if err := s.AppendInquiry(project, query, returned, cited); err != nil {
		fmt.Fprintf(st.stderr(), "warning: could not record the inquiry: %v\n", err)
	}
}

// idsOf pulls the IDs out of a hit list for the inquiry log.
func idsOf(hits []jsonHit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.ID)
	}
	return out
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
	// The model numbers its citations over the FULL retrieval set, and
	// citedHits returns them in first-mention order — so renumbering from 1
	// here would print keys that do not match the [n] markers in the answer
	// above. Look each cited hit up by its position in the hit list instead.
	// On the degraded path, where `cited` falls back to res.Hits, this yields
	// i+1 anyway, so both paths share one rule.
	pos := make(map[string]int, len(res.Hits))
	for i, h := range res.Hits {
		pos[h.ID] = i + 1
	}
	for i, h := range cited {
		label := h.Title
		if label == "" {
			label = h.Snippet
		}
		n, ok := pos[h.ID]
		if !ok {
			n = i + 1
		}
		fmt.Fprintf(out, "[%d] %s (%s) %s\n", n, h.ID, h.Kind, label)
	}
	if res.Behind > 0 {
		fmt.Fprintf(out, "\nsources may lag · %d items still indexing\n", res.Behind)
	}
}
