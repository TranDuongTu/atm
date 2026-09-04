package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// applyCLI is a CLI over the production registry with a project that has
// only scrum enabled, so apply has capabilities to turn on.
func applyCLI(t *testing.T) *testCLI {
	t.Helper()
	t.Setenv("ATM_PROJECT", "")
	st := newRegistryTestCLI(t)
	runArgsOut(t, st, "project", "create", "--code", "DEMO", "--name", "demo", "--capabilities", "scrum", "--actor", "admin@cli:unset")
	return st
}

// writeApplyProfileDir is writeProfileDir plus a channel expectation and a
// checklist that requires it, so the setup report has something to say.
func writeApplyProfileDir(t *testing.T, version string) string {
	t.Helper()
	dir := writeProfileDir(t, version)
	write := func(rel, body string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("manifest.yaml", "name: demo\nversion: "+version+"\nformat: 1\ndescription: a demo profile\nrequires_capabilities: [scrum, channel]\n")
	write("channels/design.md", "---\nname: design\n---\nWhere <CODE> specs live.\n")
	write("checklists/work.md", "---\nname: work\npurpose: do the work\nsuits: [coder]\nrequires_capabilities: [scrum]\nrequires_channels: [design]\n---\n1. Do it.\n")
	return dir
}

func installDemo(t *testing.T, st *testCLI, version string) string {
	t.Helper()
	dir := writeApplyProfileDir(t, version)
	out := filepath.Join(t.TempDir(), "demo.atmprofile")
	runArgsOut(t, st, "profile", "build", "--dir", dir, "-o", out)
	runArgsOut(t, st, "profile", "install", out)
	return dir
}

func TestProfileApplyFromADirectoryAppliesAsDev(t *testing.T) {
	st := applyCLI(t)
	dir := writeApplyProfileDir(t, "1.0.0")
	out := runArgsOut(t, st, "profile", "apply", "--project", "DEMO", "--dir", dir, "--actor", "admin@cli:unset")
	mustContain(t, out, "applied demo@dev to DEMO")
	mustContain(t, out, "channel (enabled now)")
	mustContain(t, out, "scrum (already enabled)")
	for _, line := range []string{"persona\tcoder\tcreate", "checklist\twork\tcreate", "channel\tdesign\tcreate"} {
		mustContain(t, out, line)
	}
	show := runArgsOut(t, st, "checklist", "show", "--project", "DEMO", "--name", "work")
	mustContain(t, show, "origin: demo@dev")
	show = runArgsOut(t, st, "persona", "show", "--project", "DEMO", "coder")
	mustContain(t, show, "You write code.")
	// The capability is really on: its noun answers.
	list := runArgsOut(t, st, "channel", "list", "--project", "DEMO")
	mustContain(t, list, "design")
}

func TestProfileApplyByNameStampsTheInstalledVersionAndReappliesInSync(t *testing.T) {
	st := applyCLI(t)
	installDemo(t, st, "1.0.0")
	out := runArgsOut(t, st, "profile", "apply", "--project", "DEMO", "--name", "demo", "--actor", "admin@cli:unset")
	mustContain(t, out, "applied demo@1.0.0 to DEMO")
	show := runArgsOut(t, st, "checklist", "show", "--project", "DEMO", "--name", "work")
	mustContain(t, show, "origin: demo@1.0.0")

	out = runArgsOut(t, st, "profile", "apply", "--project", "DEMO", "--name", "demo", "--actor", "admin@cli:unset")
	mustContain(t, out, "checklist\twork\tin sync")
	mustContain(t, out, "3 in sync")
	mustNotContain(t, out, "create")
}

func TestProfileApplyReportsConflictsAndExitsNonZeroUntilForced(t *testing.T) {
	st := applyCLI(t)
	dir := installDemo(t, st, "1.0.0")
	runArgsOut(t, st, "profile", "apply", "--project", "DEMO", "--name", "demo", "--actor", "admin@cli:unset")
	// A local rewording of the checklist.
	doc := filepath.Join(t.TempDir(), "work.md")
	if err := os.WriteFile(doc, []byte("---\nname: work\npurpose: our own words\nsuits: [coder]\n---\n1. Do it our way.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runArgsOut(t, st, "checklist", "set", "--project", "DEMO", "--name", "work", "--file", doc, "--actor", "admin@cli:unset")

	stdout, stderr, code := runArgs(st, "profile", "apply", "--project", "DEMO", "--name", "demo", "--actor", "admin@cli:unset")
	if code == ExitSuccess {
		t.Fatalf("apply with a conflict exited 0\n%s", stdout)
	}
	mustContain(t, stdout, "checklist\twork\tconflict")
	mustContain(t, stdout, "modified locally")
	mustContain(t, stdout+stderr, "--force")
	show := runArgsOut(t, st, "checklist", "show", "--project", "DEMO", "--name", "work")
	mustContain(t, show, "our own words")

	out := runArgsOut(t, st, "profile", "apply", "--project", "DEMO", "--name", "demo", "--force", "--actor", "admin@cli:unset")
	mustContain(t, out, "checklist\twork\tconflict (overwritten)")
	show = runArgsOut(t, st, "checklist", "show", "--project", "DEMO", "--name", "work")
	mustContain(t, show, "do the work")
	mustContain(t, show, "origin: demo@1.0.0")
	_ = dir
}

func TestProfileApplyDryRunWritesNothing(t *testing.T) {
	st := applyCLI(t)
	dir := writeApplyProfileDir(t, "1.0.0")
	out := runArgsOut(t, st, "profile", "apply", "--project", "DEMO", "--dir", dir, "--dry-run", "--actor", "admin@cli:unset")
	mustContain(t, out, "would apply demo@dev to DEMO")
	mustContain(t, out, "channel (would enable)")
	mustContain(t, out, "checklist\twork\tcreate")
	if _, _, code := runArgs(st, "checklist", "show", "--project", "DEMO", "--name", "work"); code == ExitSuccess {
		t.Fatal("dry run created the checklist")
	}
	caps := runArgsOut(t, st, "project", "capability", "list", "--project", "DEMO")
	mustNotContain(t, caps, "channel")
}

func TestProfileApplyRefusesAnUnknownCapabilityBeforeWriting(t *testing.T) {
	st := applyCLI(t)
	dir := writeApplyProfileDir(t, "1.0.0")
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte("name: demo\nversion: 1.0.0\nformat: 1\nrequires_capabilities: [scrum, telepathy]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	msg, code := runChecklistErrText(t, st, "profile", "apply", "--project", "DEMO", "--dir", dir, "--actor", "admin@cli:unset")
	if code == ExitSuccess || !strings.Contains(msg, "telepathy") {
		t.Fatalf("code %d, msg %q", code, msg)
	}
	if _, _, code := runArgs(st, "checklist", "show", "--project", "DEMO", "--name", "work"); code == ExitSuccess {
		t.Fatal("a refused apply still wrote records")
	}
}

func TestProfileApplyReportsTheRemainingMechanicalSetup(t *testing.T) {
	st := applyCLI(t)
	dir := writeApplyProfileDir(t, "1.0.0")
	out := runArgsOut(t, st, "profile", "apply", "--project", "DEMO", "--dir", dir, "--actor", "admin@cli:unset")
	mustContain(t, out, "Remaining setup")
	mustContain(t, out, "channel design has no endpoint")
	mustContain(t, out, "atm channel endpoint add --project DEMO --name design")
	mustContain(t, out, "no agent launcher is selected")

	runArgsOut(t, st, "channel", "endpoint", "add", "--project", "DEMO", "--name", "design", "--type", "notion", "--page", "p1", "--actor", "admin@cli:unset")
	runArgsOut(t, st, "agents", "select", "claude")
	out = runArgsOut(t, st, "profile", "apply", "--project", "DEMO", "--dir", dir, "--actor", "admin@cli:unset")
	mustNotContain(t, out, "Remaining setup")
	mustContain(t, out, "nothing left to set up")
}

func TestProfileApplyJSONCarriesThePlanAndSetup(t *testing.T) {
	st := applyCLI(t)
	dir := writeApplyProfileDir(t, "1.0.0")
	st.output = outputJSON
	out := runArgsOut(t, st, "profile", "apply", "--project", "DEMO", "--dir", dir, "--actor", "admin@cli:unset")
	for _, key := range []string{`"ref": "demo@dev"`, `"state": "create"`, `"kind": "channel-endpoint"`, `"applied": true`} {
		mustContain(t, out, key)
	}
}

func TestProfileApplyCollidesLoudlyWithAnotherProfile(t *testing.T) {
	st := applyCLI(t)
	dir := writeApplyProfileDir(t, "1.0.0")
	runArgsOut(t, st, "profile", "apply", "--project", "DEMO", "--dir", dir, "--actor", "admin@cli:unset")
	other := writeApplyProfileDir(t, "1.0.0")
	if err := os.WriteFile(filepath.Join(other, "manifest.yaml"), []byte("name: other\nversion: 1.0.0\nformat: 1\nrequires_capabilities: [scrum, channel]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _, code := runArgs(st, "profile", "apply", "--project", "DEMO", "--dir", other, "--actor", "admin@cli:unset")
	if code == ExitSuccess {
		t.Fatal("a same-name record from another profile applied silently")
	}
	mustContain(t, stdout, "owned by profile demo@dev")
}

func TestChecklistResetRestoresTheOriginVersion(t *testing.T) {
	st := applyCLI(t)
	installDemo(t, st, "1.0.0")
	runArgsOut(t, st, "profile", "apply", "--project", "DEMO", "--name", "demo", "--actor", "admin@cli:unset")
	doc := filepath.Join(t.TempDir(), "work.md")
	if err := os.WriteFile(doc, []byte("---\nname: work\npurpose: our own words\nsuits: [coder]\n---\n1. Do it our way.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runArgsOut(t, st, "checklist", "set", "--project", "DEMO", "--name", "work", "--file", doc, "--actor", "admin@cli:unset")
	out := runArgsOut(t, st, "checklist", "reset", "--project", "DEMO", "--name", "work", "--actor", "admin@cli:unset")
	mustContain(t, out, "reset checklist work to demo@1.0.0")
	show := runArgsOut(t, st, "checklist", "show", "--project", "DEMO", "--name", "work")
	mustContain(t, show, "do the work")
	mustNotContain(t, show, "our own words")

	// User-authored: refused, with the reason.
	runArgsOut(t, st, "checklist", "add", "--project", "DEMO", "--name", "ours", "--step", "x", "--actor", "admin@cli:unset")
	msg, code := runChecklistErrText(t, st, "checklist", "reset", "--project", "DEMO", "--name", "ours", "--actor", "admin@cli:unset")
	if code == ExitSuccess || !strings.Contains(msg, "origin user") {
		t.Fatalf("code %d, msg %q", code, msg)
	}
}

func TestChecklistResetNamesAMissingVersion(t *testing.T) {
	st := applyCLI(t)
	dir := writeApplyProfileDir(t, "1.0.0")
	runArgsOut(t, st, "profile", "apply", "--project", "DEMO", "--dir", dir, "--actor", "admin@cli:unset")
	msg, code := runChecklistErrText(t, st, "checklist", "reset", "--project", "DEMO", "--name", "work", "--actor", "admin@cli:unset")
	if code == ExitSuccess || !strings.Contains(msg, "demo@dev") || !strings.Contains(msg, "not installed") {
		t.Fatalf("code %d, msg %q", code, msg)
	}
}

func TestChannelResetRestoresPurposeAndKeepsEndpoints(t *testing.T) {
	st := applyCLI(t)
	installDemo(t, st, "1.0.0")
	runArgsOut(t, st, "profile", "apply", "--project", "DEMO", "--name", "demo", "--actor", "admin@cli:unset")
	runArgsOut(t, st, "channel", "endpoint", "add", "--project", "DEMO", "--name", "design", "--type", "notion", "--page", "p1", "--actor", "admin@cli:unset")
	runArgsOut(t, st, "channel", "edit", "--project", "DEMO", "--name", "design", "--purpose", "edited", "--actor", "admin@cli:unset")
	out := runArgsOut(t, st, "channel", "reset", "--project", "DEMO", "--name", "design", "--actor", "admin@cli:unset")
	mustContain(t, out, "reset channel design to demo@1.0.0")
	show := runArgsOut(t, st, "channel", "show", "--project", "DEMO", "--name", "design")
	mustContain(t, show, "Where DEMO specs live.")
	mustContain(t, show, "p1")
}
