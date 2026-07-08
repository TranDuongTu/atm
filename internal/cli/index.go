package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"atm/internal/embed"

	"github.com/spf13/cobra"
)

func newIndexCmd(st *cliState) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Run-once foreground watcher: does the initial delta then watches the log (no --actor; Ctrl-C stops)",
		RunE: func(cmd *cobra.Command, args []string) error {
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
			if cfg == nil || cfg.Embedding == nil {
				return fmt.Errorf("%w: no embedding configured for project %q; run 'atm project set-embedding' first", ErrUsage, project)
			}
			client := embed.New(*cfg.Embedding)
			embedFn := func(text, role string) ([]float64, error) { return client.Embed(text, role) }
			progress := func(msg string) { fmt.Fprintln(os.Stderr, msg) }
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			return s.Watch(ctx, project, embedFn, progress)
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	_ = cmd.MarkFlagRequired("project")
	cmd.AddCommand(newIndexReindexCmd(st))
	cmd.AddCommand(newIndexStatusCmd(st))
	cmd.AddCommand(newIndexDropCmd(st))
	cmd.AddCommand(newIndexModelsCmd(st))
	return cmd
}

func newIndexReindexCmd(st *cliState) *cobra.Command {
	var project string
	var once bool
	cmd := &cobra.Command{
		Use:   "reindex",
		Short: "One-shot batch reindex (CI/git-hooks); exits after the delta pass",
		RunE: func(cmd *cobra.Command, args []string) error {
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
			if cfg == nil || cfg.Embedding == nil {
				return fmt.Errorf("%w: no embedding configured for project %q", ErrUsage, project)
			}
			client := embed.New(*cfg.Embedding)
			embedFn := func(text, role string) ([]float64, error) { return client.Embed(text, role) }
			res, err := s.ReindexOnce(project, embedFn)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{
				"project": project, "indexed": res.Indexed, "model": res.Model, "log_seq": res.LogSeq,
			}, func() {
				fmt.Fprintf(st.stdout(), "indexed %d (model=%s); index at log_seq %d\n", res.Indexed, res.Model, res.LogSeq)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().BoolVar(&once, "once", true, "single batch pass then exit (batch primitive; default true)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newIndexStatusCmd(st *cliState) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Report per-model index staleness vs the project log",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if _, err := s.GetProject(project); err != nil {
				return fmt.Errorf("%w: project %s not found", ErrNotFound, project)
			}
			lastLogSeq, _ := s.LastLogSeq(project)
			models, err := s.ListVectorModels(project)
			if err != nil {
				return err
			}
			type statusRow struct {
				Model      string `json:"model"`
				Count      int    `json:"count"`
				LastLogSeq int    `json:"last_log_seq"`
				Behind     int    `json:"behind"`
			}
			rows := make([]statusRow, 0, len(models))
			for _, slug := range models {
				meta, _ := s.VectorMeta(project, slug)
				r := statusRow{Model: slug}
				if meta != nil {
					r.Count = meta.Count
					r.LastLogSeq = meta.LastLogSeq
					r.Behind = lastLogSeq - meta.LastLogSeq
				}
				rows = append(rows, r)
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "log_last_seq": lastLogSeq, "indexes": rows}, func() {
				for _, r := range rows {
					fmt.Fprintf(st.stdout(), "%s\tcount=%d\tlast_log_seq=%d\tbehind=%d\n", r.Model, r.Count, r.LastLogSeq, r.Behind)
				}
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newIndexDropCmd(st *cliState) *cobra.Command {
	var project, model string
	cmd := &cobra.Command{
		Use:   "drop",
		Short: "Delete one model's vector index (migration)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if _, err := s.GetProject(project); err != nil {
				return fmt.Errorf("%w: project %s not found", ErrNotFound, project)
			}
			if err := s.DropVectors(project, model); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"dropped": model, "project": project}, func() {
				fmt.Fprintf(st.stdout(), "dropped vector index %s/%s\n", project, model)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&model, "model", "", "embedding model slug")
	_ = cmd.MarkFlagRequired("project")
	_ = cmd.MarkFlagRequired("model")
	return cmd
}

func newIndexModelsCmd(st *cliState) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "models",
		Short: "List which models have a vector index for the project",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if _, err := s.GetProject(project); err != nil {
				return fmt.Errorf("%w: project %s not found", ErrNotFound, project)
			}
			models, err := s.ListVectorModels(project)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "models": models}, func() {
				for _, m := range models {
					fmt.Fprintln(st.stdout(), m)
				}
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
