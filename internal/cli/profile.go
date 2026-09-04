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

// newProfileApplyCmd turns a profile into a project's operating model. The
// order is the plan's (§3.2): load and validate, so nothing is written for
// a broken profile; enable the capabilities it presupposes (an unknown one
// is a hard error before any write); import the three record kinds; then
// report the mechanical setup only the project or this machine can finish
// — the report that replaced the starter checklists.
func newProfileApplyCmd(st *cliState) *cobra.Command {
	var name, version, dir string
	var dryRun, force bool
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply a profile to a project: enable its capabilities and import its records",
		Long: "Apply is additive over a flat per-kind namespace. Each document becomes " +
			"a project record stamped with the profile's origin: created when absent, " +
			"in sync when identical, updated when the project never touched it and " +
			"the profile moved, and a CONFLICT when the difference is the project's " +
			"own — a local edit, a record it authored, or one another profile owns. " +
			"Conflicts are left untouched and the command exits non-zero naming " +
			"--force, which overwrites them and takes ownership. Re-apply is the " +
			"upgrade path. --dir applies an unbuilt directory as <name>@dev; " +
			"--dry-run reports the plan and writes nothing.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			code, err := profileProject(cmd)
			if err != nil {
				return err
			}
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			if (name == "") == (dir == "") {
				return fmt.Errorf("%w: pass exactly one of --name or --dir", core.ErrUsage)
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			p, err := loadProfileToApply(s, name, version, dir)
			if err != nil {
				return err
			}
			// Unknown capability: hard error, before any write.
			if err := profile.ValidateCapabilities(profile.RequiredCapabilities(p), st.fullRegistry.Names()); err != nil {
				return fmt.Errorf("%w: %v", core.ErrUsage, err)
			}
			proj, err := s.GetProject(code)
			if err != nil {
				return err
			}
			plan, err := s.PlanProfile(code, p)
			if err != nil {
				return err
			}
			if !dryRun {
				for _, c := range plan.Capabilities {
					if c.Enabled {
						continue
					}
					if err := s.EnableProjectCapability(code, c.Name, actor); err != nil {
						return err
					}
				}
				proj, err = s.GetProject(code)
				if err != nil {
					return err
				}
				if _, err := st.fullRegistry.For(proj).EnsureVocabulary(s, code, actor); err != nil {
					return err
				}
				applied, err := s.ApplyProfile(code, p, force, actor)
				if applied != nil {
					// Keep the pre-apply capability view: it says what THIS
					// run turned on, which the post-apply plan cannot.
					applied.Capabilities = plan.Capabilities
					plan = applied
				}
				if err != nil {
					return err
				}
			}
			setup, err := profileSetupReport(s, code)
			if err != nil {
				return err
			}
			conflicts := plan.Conflicts()
			if err := st.emit(st.stdout(), map[string]any{
				"project": code, "plan": plan, "applied": !dryRun, "setup": setup,
			}, func() {
				renderApplyPlan(st.stdout(), code, plan, dryRun)
				renderSetupReport(st.stdout(), setup)
			}); err != nil {
				return err
			}
			if len(conflicts) > 0 && !dryRun {
				return fmt.Errorf("%w: %d record(s) left untouched because the project's copy differs; overwrite them with --force", core.ErrUsage, len(conflicts))
			}
			return nil
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().StringVar(&name, "name", "", "installed or embedded profile to apply")
	cmd.Flags().StringVar(&version, "version", "", "profile version (default: the newest available)")
	cmd.Flags().StringVar(&dir, "dir", "", "apply an unbuilt profile directory as <name>@dev")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what apply would do and write nothing")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite conflicting records and take ownership of them")
	return cmd
}

// profileProject resolves the target project: --project flag, else ATM_PROJECT.
func profileProject(cmd *cobra.Command) (string, error) {
	p, _ := cmd.Flags().GetString("project")
	if p == "" {
		p = os.Getenv("ATM_PROJECT")
	}
	if p == "" {
		return "", fmt.Errorf("%w: --project is required (or ATM_PROJECT)", core.ErrUsage)
	}
	return p, nil
}

// loadProfileToApply resolves --name/--version through the profile store,
// or loads --dir through the same parser and validation and stamps it as
// the dev version — a directory is not a release, and records it creates
// say so in their origin.
func loadProfileToApply(s core.Service, name, version, dir string) (*core.Profile, error) {
	if dir != "" {
		p, err := profile.Load(os.DirFS(dir))
		if err != nil {
			return nil, err
		}
		p.Manifest.Version = core.DevVersion
		return p, nil
	}
	p, _, err := s.GetProfile(name, version)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// profileSetupReport gathers what the setup report reads: the project's
// records after apply and whether this machine has a launcher selected.
func profileSetupReport(s core.Service, code string) ([]core.SetupStep, error) {
	cur := profile.Current{}
	var err error
	if cur.Checklists, err = s.ChecklistRecords(code); err != nil {
		return nil, err
	}
	if cur.Channels, err = s.ChannelRecords(code); err != nil {
		return nil, err
	}
	cfg, err := s.GetAgentsConfig()
	if err != nil {
		return nil, err
	}
	return profile.SetupReport(code, cur, cfg.Selected != ""), nil
}

func renderApplyPlan(w io.Writer, code string, plan *core.ApplyPlan, dryRun bool) {
	verb := "applied"
	if dryRun {
		verb = "would apply"
	}
	fmt.Fprintf(w, "%s %s to %s\n", verb, plan.Ref, code)
	if len(plan.Capabilities) > 0 {
		var caps []string
		for _, c := range plan.Capabilities {
			switch {
			case c.Enabled:
				caps = append(caps, c.Name+" (already enabled)")
			case dryRun:
				caps = append(caps, c.Name+" (would enable)")
			default:
				caps = append(caps, c.Name+" (enabled now)")
			}
		}
		fmt.Fprintf(w, "  capabilities: %s\n", strings.Join(caps, ", "))
	}
	counts := map[core.ApplyState]int{}
	for _, it := range plan.Items {
		state := strings.ReplaceAll(string(it.State), "-", " ")
		note := ""
		switch {
		case it.State == core.ApplyConflict && it.Forced:
			state = "conflict (overwritten)"
			note = it.Reason
		case it.State == core.ApplyConflict:
			note = it.Reason + " — overwrite with --force"
		case it.State == core.ApplyInSync && it.Restamp && it.Origin != "":
			note = "origin " + it.Origin + " -> " + plan.Ref
		case it.State == core.ApplyUpdate:
			note = it.Reason
		}
		counts[it.State]++
		if note != "" {
			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n", it.Kind, it.Name, state, note)
		} else {
			fmt.Fprintf(w, "  %s\t%s\t%s\n", it.Kind, it.Name, state)
		}
	}
	var summary []string
	for _, st := range []core.ApplyState{core.ApplyCreate, core.ApplyInSync, core.ApplyUpdate, core.ApplyConflict} {
		if n := counts[st]; n > 0 {
			summary = append(summary, fmt.Sprintf("%d %s", n, strings.ReplaceAll(string(st), "-", " ")))
		}
	}
	if len(summary) > 0 {
		fmt.Fprintf(w, "  %s\n", strings.Join(summary, ", "))
	}
}

func renderSetupReport(w io.Writer, setup []core.SetupStep) {
	if len(setup) == 0 {
		fmt.Fprintln(w, "\nnothing left to set up")
		return
	}
	fmt.Fprintln(w, "\nRemaining setup (what only the project or this machine can answer):")
	for _, s := range setup {
		fmt.Fprintf(w, "  - %s\n", s.Detail)
		if s.Command != "" {
			fmt.Fprintf(w, "      %s\n", s.Command)
		}
	}
}

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
	bindActorFlag(cmd, st)
	cmd.AddCommand(newProfileBuildCmd(st))
	cmd.AddCommand(newProfileInstallCmd(st))
	cmd.AddCommand(newProfileListCmd(st))
	cmd.AddCommand(newProfileApplyCmd(st))
	cmd.AddCommand(newProfileStatusCmd(st))
	cmd.AddCommand(newProfileVerifyCmd(st))
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
