// internal/cli/persona_record.go
package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"atm/internal/core"
	"atm/internal/profile"

	"github.com/spf13/cobra"
)

// personaProject resolves the target project: --project flag, else
// ATM_PROJECT. Persona records are project state, so there is no default.
func personaProject(cmd *cobra.Command) (string, error) {
	p, _ := cmd.Flags().GetString("project")
	if p == "" {
		p = os.Getenv("ATM_PROJECT")
	}
	if p == "" {
		return "", fmt.Errorf("%w: --project is required (or ATM_PROJECT)", core.ErrUsage)
	}
	return p, nil
}

// readPersonaDocument reads a persona document from a file, or from stdin
// when the path is "-". The name comes from the document's own frontmatter,
// which must match --name when both are given: a document that disagrees
// with the flag is a mistake worth stopping on, not a thing to resolve.
func readPersonaDocument(st *cliState, name, path string) (core.Persona, error) {
	var src []byte
	var err error
	if path == "-" {
		src, err = io.ReadAll(st.stdin())
	} else {
		src, err = os.ReadFile(path)
	}
	if err != nil {
		return core.Persona{}, err
	}
	stem := name
	if stem == "" && path != "-" {
		stem = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	p, err := profile.ParsePersonaDocument(stem, src)
	if err != nil {
		return core.Persona{}, fmt.Errorf("%w: %v", core.ErrUsage, err)
	}
	return p, nil
}

func newPersonaSetCmd(st *cliState) *cobra.Command {
	var name, file, project string
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Import a persona document as this project's record",
		Long: "A persona is a project record: the identity sessions run under, owned " +
			"by this project so two projects can run the same-named persona from " +
			"different profiles. The document IS the record — set replaces it " +
			"wholesale, with no per-field edit. Replacing a record keeps its name " +
			"and the profile it came from, so `atm persona reset` can still restore it.",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			code, err := personaProject(cmd)
			if err != nil {
				return err
			}
			if file == "" {
				return fmt.Errorf("%w: --file is required (use - for stdin)", core.ErrUsage)
			}
			doc, err := readPersonaDocument(st, name, file)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			existed := false
			if _, err := s.GetPersonaRecord(code, doc.Name); err == nil {
				existed = true
			}
			t, err := s.SetPersonaRecord(code, doc, actor)
			if err != nil {
				return err
			}
			verb := "created"
			if existed {
				verb = "updated"
			}
			return st.emit(st.stdout(), map[string]any{"persona": doc.Name, "task_id": t.ID, "created": !existed}, func() {
				fmt.Fprintf(st.stdout(), "%s persona %s (%s)\n", verb, doc.Name, t.ID)
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "persona name (default: the document's own)")
	cmd.Flags().StringVar(&file, "file", "", "persona document path, or - for stdin")
	cmd.Flags().StringVar(&project, "project", "", "project code (or ATM_PROJECT)")
	return cmd
}

func newPersonaResetCmd(st *cliState) *cobra.Command {
	var name, project string
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Restore a persona from the profile version it came from",
		Long: "Reset discards local edits and restores the record from its OWN origin " +
			"version — not the newest installed one. Reset means back to what you " +
			"were given; quietly upgrading you during a restore would be a different " +
			"operation wearing the same name. A record the project authored itself " +
			"has no source to restore from and is refused.",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			code, err := personaProject(cmd)
			if err != nil {
				return err
			}
			if name == "" {
				return fmt.Errorf("%w: --name is required", core.ErrUsage)
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			p, err := s.ResetPersonaRecord(code, name, actor)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"persona": p}, func() {
				fmt.Fprintf(st.stdout(), "reset persona %s to %s\n", p.Name, p.Origin)
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "persona name")
	cmd.Flags().StringVar(&project, "project", "", "project code (or ATM_PROJECT)")
	return cmd
}

// personaRecordList renders the project's persona records. It is the branch
// `persona list` takes once a project is in scope; the machine-global
// listing beside it is what the second half of ATM-207ab8 retires.
func personaRecordList(st *cliState, s core.Service, code string) error {
	recs, err := s.PersonaRecords(code)
	if err != nil {
		return err
	}
	return st.emit(st.stdout(), map[string]any{"personas": recs}, func() {
		for _, p := range recs {
			fmt.Fprintf(st.stdout(), "%s\t%s\t%s\n", p.Name, p.Origin, p.Description)
		}
	})
}
