// internal/cli/profile.go
package cli

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"atm/internal/core"
	"atm/internal/profile"

	"github.com/spf13/cobra"
)

// newProfileCmd is the profile noun: the lifecycle of ATM's operating
// content as a distributable package. A profile is a named, versioned
// bundle of personas, checklists, and channel expectations — built from a
// directory, published as a file, installed onto a machine, and (from unit
// 4's later increments) applied to a project.
func newProfileCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Build, install, and inspect operating-model profiles",
		Long: "A profile is a named, versioned bundle of operating content: personas, " +
			"checklists, and channel expectations, plus the capabilities they " +
			"presuppose. `build` validates a profile directory and packs a digest-" +
			"identified artifact; `install` puts an artifact into this machine's " +
			"profile store; `list` shows what is available, installed or embedded in " +
			"the binary. `--output json` on list is the agent endpoint.",
	}
	cmd.AddCommand(newProfileBuildCmd(st))
	cmd.AddCommand(newProfileInstallCmd(st))
	cmd.AddCommand(newProfileListCmd(st))
	return cmd
}

func newProfileBuildCmd(st *cliState) *cobra.Command {
	var dir, out string
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Validate a profile directory and pack a distributable artifact",
		Long: "Validation runs first and nothing is written if it fails: a built " +
			"artifact is a validated one, so a broken profile fails on its author's " +
			"machine rather than on whoever installs it. The artifact is canonical — " +
			"the same source always packs to the same bytes, so its sha256 digest " +
			"identifies content and can be recomputed from the published file.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if dir == "" {
				return fmt.Errorf("%w: --dir is required", core.ErrUsage)
			}
			// Pack into memory first so a validation failure leaves no
			// half-written artifact on disk.
			var buf bytes.Buffer
			art, err := profile.Build(os.DirFS(dir), &buf)
			if err != nil {
				return err
			}
			path := out
			if path == "" {
				path = art.Filename()
			}
			if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
				return err
			}
			if st.isJSON() {
				return writeJSON(st.stdout(), map[string]any{"artifact": art, "path": path})
			}
			fmt.Fprintf(st.stdout(), "built %s\n", art.Ref())
			fmt.Fprintf(st.stdout(), "  file   %s (%d bytes)\n", path, art.Size)
			fmt.Fprintf(st.stdout(), "  digest %s\n", art.Digest)
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "profile directory to build")
	cmd.Flags().StringVarP(&out, "out", "o", "", "artifact path (default <name>-<version>.atmprofile)")
	return cmd
}

func newProfileInstallCmd(st *cliState) *cobra.Command {
	var verify string
	cmd := &cobra.Command{
		Use:   "install <file|url>",
		Short: "Install a profile artifact into this machine's profile store",
		Long: "Installing has no effect on any project: it only makes a profile " +
			"available to apply. The artifact is verified and validated before " +
			"anything is written, and a rejected install leaves nothing behind. " +
			"Pass --verify with the publisher's digest to pin exactly which bytes " +
			"you accept.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			path, cleanup, err := localArtifactPath(args[0])
			if err != nil {
				return err
			}
			defer cleanup()
			e, err := s.InstallProfile(path, verify)
			if err != nil {
				return err
			}
			if st.isJSON() {
				return writeJSON(st.stdout(), map[string]any{"installed": e})
			}
			fmt.Fprintf(st.stdout(), "installed %s\n", e.Ref())
			fmt.Fprintf(st.stdout(), "  path   %s\n", e.Path)
			fmt.Fprintf(st.stdout(), "  digest %s\n", e.Digest)
			if e.Description != "" {
				fmt.Fprintf(st.stdout(), "  %s\n", e.Description)
			}
			fmt.Fprintf(st.stdout(), "\nNothing changed in any project. Apply it with: atm profile apply --project <CODE> --name %s\n", e.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&verify, "verify", "", "require this sha256:... digest")
	return cmd
}

// localArtifactPath resolves the install argument to a local file,
// downloading first when it is an http(s) URL. Fetching lives HERE, in the
// adapter, not in the store: reaching the network is a user-facing act the
// terminal command owns, and nothing deeper in ATM ever does it — apply in
// particular never touches the network.
func localArtifactPath(src string) (string, func(), error) {
	noop := func() {}
	if !strings.HasPrefix(src, "http://") && !strings.HasPrefix(src, "https://") {
		return src, noop, nil
	}
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(src)
	if err != nil {
		return "", noop, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", noop, fmt.Errorf("fetch %s: %s", src, resp.Status)
	}
	f, err := os.CreateTemp("", "atm-profile-*"+profile.ArtifactExt)
	if err != nil {
		return "", noop, err
	}
	cleanup := func() { os.Remove(f.Name()) }
	_, err = io.Copy(f, io.LimitReader(resp.Body, profile.MaxArtifactBytes+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		cleanup()
		return "", noop, err
	}
	return f.Name(), cleanup, nil
}

func newProfileListCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the profiles available on this machine",
		Long: "Installed profiles and the ones embedded in the binary, newest " +
			"version first within each name — so the first row for a name is what " +
			"a bare name resolves to. Embedded profiles are served from the binary " +
			"and never written to disk.",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			list, err := s.ListProfiles()
			if err != nil {
				return err
			}
			if st.isJSON() {
				return writeJSON(st.stdout(), map[string]any{"profiles": list})
			}
			if len(list) == 0 {
				fmt.Fprintln(st.stdout(), "no profiles available")
				return nil
			}
			for _, e := range list {
				origin := "installed"
				if e.Embedded {
					origin = "embedded"
				}
				fmt.Fprintf(st.stdout(), "%s\t%s\t%s\n", e.Ref(), origin, e.Description)
			}
			return nil
		},
	}
	return cmd
}
