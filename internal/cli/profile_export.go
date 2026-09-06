package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"atm/internal/core"
	"atm/internal/profile"
)

// newProfileExportCmd is apply's inverse at the CLI: a project's live
// records back out as a profile directory. All file I/O lives here — the
// profile package stays a pure format — and the records are read through
// the service verbs every other noun uses.
func newProfileExportCmd(st *cliState) *cobra.Command {
	var dir, name, version string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Fork a project's operating content into a new profile directory",
		Long: "Export writes the project's live personas, checklists and channel " +
			"expectations as a NEW profile with the identity you give it — the " +
			"directory form build packs and apply reads. A record still in sync " +
			"with its origin version is written from that version's document, so " +
			"placeholders survive; a record the project changed or authored is " +
			"written as it is, and the report says which is which. Same-named " +
			"documents are overwritten, documents the project no longer has are " +
			"removed, and any other file in the directory is left alone: " +
			"`git diff` after an export is the review surface. Endpoints, " +
			"addresses, stamps and origin marks never leave the store.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			code, err := profileProject(cmd)
			if err != nil {
				return err
			}
			if dir == "" || name == "" {
				return fmt.Errorf("%w: --dir and --name are required", core.ErrUsage)
			}
			if !core.ValidProfileName(name) {
				return fmt.Errorf("%w: --name %q: lowercase letters, digits, - and _", core.ErrUsage, name)
			}
			if !core.ValidProfileVersion(version) {
				return fmt.Errorf("%w: --version %q must be semver (1.2.3)", core.ErrUsage, version)
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			proj, err := s.GetProject(code)
			if err != nil {
				return err
			}
			cur := profile.Current{}
			if cur.Personas, err = s.PersonaRecords(code); err != nil {
				return err
			}
			if cur.Checklists, err = s.ChecklistRecords(code); err != nil {
				return err
			}
			if cur.Channels, err = s.ChannelRecords(code); err != nil {
				return err
			}
			enabled := st.fullRegistry.For(proj).Names()
			origin := func(ref string) *core.Profile {
				o, err := core.ParseOrigin(ref)
				if err != nil {
					return nil
				}
				p, _, err := s.GetProfile(o.Profile, o.Version)
				if err != nil {
					return nil
				}
				return p
			}
			p, notes := profile.Export(code, cur, enabled, origin, core.ProfileManifest{Name: name, Version: version})
			files, err := profile.Write(p)
			if err != nil {
				return err
			}
			removed, err := syncProfileDir(dir, files)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{
				"project": code, "dir": dir, "ref": p.Manifest.Ref(), "documents": notes, "removed": removed,
			}, func() { renderExportReport(st.stdout(), code, dir, p.Manifest.Ref(), notes, removed) })
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().StringVar(&dir, "dir", "", "profile directory to write (created if missing)")
	cmd.Flags().StringVar(&name, "name", "", "name of the new profile")
	cmd.Flags().StringVar(&version, "version", "0.1.0", "version of the new profile")
	return cmd
}

// syncProfileDir makes dir mirror files: every document written, every
// owned document files does not carry removed, everything else untouched.
// It returns the removed paths (profile-relative, sorted).
func syncProfileDir(dir string, files map[string][]byte) ([]string, error) {
	removed := profile.StaleDocuments(os.DirFS(dir), files)
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, body, 0o644); err != nil {
			return nil, err
		}
	}
	for _, rel := range removed {
		if err := os.Remove(filepath.Join(dir, filepath.FromSlash(rel))); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	return removed, nil
}

func renderExportReport(w io.Writer, code, dir, ref string, notes []profile.ExportNote, removed []string) {
	fmt.Fprintf(w, "exported %s to %s as %s\n", code, dir, ref)
	counts := map[string]int{}
	for _, n := range notes {
		counts[n.Source]++
		var src string
		switch n.Source {
		case profile.ExportFromOrigin:
			src = "origin " + n.Origin
		case profile.ExportModified:
			src = "record (modified since " + n.Origin + ")"
		case profile.ExportUnverifiable:
			src = "record (origin " + n.Origin + " not available here)"
		default:
			src = "record (origin " + n.Origin + ")"
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\n", n.Kind, n.Name, src)
	}
	for _, rel := range removed {
		fmt.Fprintf(w, "  removed %s\n", rel)
	}
	labels := map[string]string{profile.ExportFromOrigin: "from origin", profile.ExportModified: "modified", profile.ExportUser: "user", profile.ExportUnverifiable: "unverifiable origin"}
	var parts []string
	for _, src := range []string{profile.ExportFromOrigin, profile.ExportModified, profile.ExportUser, profile.ExportUnverifiable} {
		if n := counts[src]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, labels[src]))
		}
	}
	summary := fmt.Sprintf("  %d documents", len(notes))
	if len(parts) > 0 {
		summary += ": " + strings.Join(parts, ", ")
	}
	if len(removed) > 0 {
		summary += fmt.Sprintf("; %d stale removed", len(removed))
	}
	fmt.Fprintln(w, summary)
	fmt.Fprintf(w, "next: fill description and authors in %s, then `atm profile build --dir %s`\n", filepath.Join(dir, "manifest.yaml"), dir)
}
