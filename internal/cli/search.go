package cli

import (
	"encoding/json"
	"fmt"

	"atm/internal/core"
	"atm/internal/embed"

	"github.com/spf13/cobra"
)

func newSearchCmd(st *cliState) *cobra.Command {
	var project, model, queryVector, kind string
	var k int
	var threshold float64
	cmd := &cobra.Command{
		Use:   "search \"query text\"",
		Short: "Semantic search over tasks + comments (cosine + text fallback)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if _, err := s.GetProject(project); err != nil {
				return fmt.Errorf("%w: project %s not found", ErrNotFound, project)
			}
			var qv []float64
			resolvedModel := model
			if queryVector != "" {
				if err := json.Unmarshal([]byte(queryVector), &qv); err != nil {
					return fmt.Errorf("%w: --query-vector must be a JSON array of numbers: %v", ErrUsage, err)
				}
			} else {
				cfg, err := s.GetProjectConfig(project)
				if err != nil {
					return err
				}
				if cfg != nil && cfg.Embedding != nil {
					client := embed.New(*cfg.Embedding)
					vec, err := client.Embed(args[0], "query")
					if err != nil {
						return err
					}
					qv = vec
					resolvedModel = cfg.Embedding.Model
					if threshold == 0 {
						threshold = cfg.Embedding.Threshold
					}
				}
			}
			if resolvedModel == "" {
				resolvedModel = model
			}
			p := core.SearchParams{
				Project: project, Model: resolvedModel, QueryVector: qv, QueryText: args[0],
				Kind: kind, K: k, Threshold: threshold,
			}
			hits, fallback, err := s.Search(p)
			if err != nil {
				return err
			}
			match := "semantic"
			if fallback {
				match = "text"
			}
			return st.emit(st.stdout(), map[string]any{
				"query": args[0], "model": resolvedModel, "match": match,
				"hits": hitsToJSON(hits), "fallback_used": fallback,
			}, func() {
				fmt.Fprintf(st.stdout(), "MODEL: %s  MATCH: %s  K: %d\n", resolvedModel, match, k)
				for _, h := range hits {
					label := h.Title
					if label == "" {
						label = h.Snippet
					}
					fmt.Fprintf(st.stdout(), "%s\t%s\t%.4f\t%s\t%s\n", h.ID, h.Kind, h.Score, h.Match, label)
				}
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&model, "model", "", "embedding model slug; auto-resolved from project config if absent and no --query-vector")
	cmd.Flags().StringVar(&queryVector, "query-vector", "", "JSON array of floats (pre-embedded query; pure form)")
	cmd.Flags().StringVar(&kind, "kind", "all", "task | comment | all")
	cmd.Flags().IntVar(&k, "k", 5, "top-K results")
	cmd.Flags().Float64Var(&threshold, "threshold", 0, "cosine threshold below which text fallback triggers (0 = config/engine default)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

type jsonHit struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Score   float64  `json:"score"`
	Title   string   `json:"title,omitempty"`
	Snippet string   `json:"snippet"`
	Labels  []string `json:"labels,omitempty"`
	Match   string   `json:"match"`
}

func hitsToJSON(hits []core.Hit) []jsonHit {
	out := make([]jsonHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, jsonHit(h))
	}
	return out
}
