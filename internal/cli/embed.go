package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"atm/internal/embed"

	"github.com/spf13/cobra"
)

func newEmbedCmd(st *cliState) *cobra.Command {
	var project, role, file string
	cmd := &cobra.Command{
		Use:   "embed [--file <jsonl> | \"text\"]",
		Short: "Embed text via the project's configured endpoint (the one model-touching boundary)",
		Args:  cobra.MaximumNArgs(1),
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
			if role == "" {
				role = "query"
			}
			if file != "" {
				items, err := readEmbedFile(file)
				if err != nil {
					return err
				}
				vecs, err := client.EmbedBatch(items)
				if err != nil {
					return err
				}
				return st.emit(st.stdout(), map[string]any{"project": project, "model": cfg.Embedding.Model, "vectors": vecs}, func() {
					for i, v := range vecs {
						fmt.Fprintf(st.stdout(), "%d\t%d\n", i, len(v))
					}
				})
			}
			if len(args) == 0 {
				return fmt.Errorf("%w: provide text or --file", ErrUsage)
			}
			vec, err := client.Embed(args[0], role)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "model": cfg.Embedding.Model, "vector": vec}, func() {
				fmt.Fprintf(st.stdout(), "model=%s dim=%d\n", cfg.Embedding.Model, len(vec))
				fmt.Fprintln(st.stdout(), vec)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&role, "role", "query", "query | document")
	cmd.Flags().StringVar(&file, "file", "", "path to a JSONL file of {\"text\",\"role\"} items (batch)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func readEmbedFile(path string) ([]embed.EmbedItem, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%w: open embed file: %v", ErrUsage, err)
	}
	defer f.Close()
	var out []embed.EmbedItem
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var it struct {
			Text string `json:"text"`
			Role string `json:"role,omitempty"`
		}
		if err := json.Unmarshal([]byte(line), &it); err != nil {
			return nil, fmt.Errorf("%w: malformed embed line: %v", ErrUsage, err)
		}
		out = append(out, embed.EmbedItem{Text: it.Text, Role: it.Role})
	}
	return out, sc.Err()
}
