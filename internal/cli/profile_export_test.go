package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"atm/internal/profile"
)

// exportCLI: DEMO with demo@1.0.0 installed and applied BY NAME, so the
// records carry a resolvable origin (persona coder, checklist work,
// channel design).
func exportCLI(t *testing.T) *testCLI {
	t.Helper()
	st := applyCLI(t)
	installDemo(t, st, "1.0.0")
	runArgsOut(t, st, "profile", "apply", "--project", "DEMO", "--name", "demo", "--actor", "admin@cli:unset")
	return st
}

func TestProfileExportWritesALoadableFork(t *testing.T) {
	st := exportCLI(t)
	dir := filepath.Join(t.TempDir(), "fork")
	out := runArgsOut(t, st, "profile", "export", "--project", "DEMO", "--dir", dir, "--name", "myteam")
	mustContain(t, out, "exported DEMO to "+dir+" as myteam@0.1.0")
	for _, line := range []string{"persona\tcoder\torigin demo@1.0.0", "checklist\twork\torigin demo@1.0.0", "channel\tdesign\torigin demo@1.0.0"} {
		mustContain(t, out, line)
	}
	mustContain(t, out, "atm profile build --dir "+dir)

	p, err := profile.Load(os.DirFS(dir))
	if err != nil {
		t.Fatalf("exported profile does not load: %v", err)
	}
	if p.Manifest.Name != "myteam" || p.Manifest.Version != "0.1.0" {
		t.Fatalf("manifest = %+v", p.Manifest)
	}
	for _, want := range []string{"scrum", "channel", "checklist"} {
		if !hasString(p.Manifest.RequiresCapabilities, want) {
			t.Fatalf("requires_capabilities %v lacks %s", p.Manifest.RequiresCapabilities, want)
		}
	}
	// In sync with the origin -> the origin's text, placeholder intact.
	design, _ := os.ReadFile(filepath.Join(dir, "channels", "design.md"))
	mustContain(t, string(design), "Where <CODE> specs live.")
	if strings.Contains(string(design), "DEMO") {
		t.Fatalf("in-sync document was substituted:\n%s", design)
	}
}

func TestProfileExportWritesModifiedRecordsLiterally(t *testing.T) {
	st := exportCLI(t)
	doc := filepath.Join(t.TempDir(), "work.md")
	if err := os.WriteFile(doc, []byte("---\nname: work\npurpose: do the work, my way\nsuits: [coder]\nrequires_capabilities: [scrum]\nrequires_channels: [design]\n---\n1. Do it.\n2. Then check.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runArgsOut(t, st, "checklist", "set", "--project", "DEMO", "--name", "work", "--file", doc, "--actor", "admin@cli:unset")
	dir := filepath.Join(t.TempDir(), "fork")
	out := runArgsOut(t, st, "profile", "export", "--project", "DEMO", "--dir", dir, "--name", "myteam", "--version", "1.2.3")
	mustContain(t, out, "checklist\twork\trecord (modified since demo@1.0.0)")
	mustContain(t, out, "as myteam@1.2.3")
	work, _ := os.ReadFile(filepath.Join(dir, "checklists", "work.md"))
	mustContain(t, string(work), "do the work, my way")
	mustContain(t, string(work), "2. Then check.")
}

func TestProfileExportSyncsTheDirectory(t *testing.T) {
	st := exportCLI(t)
	dir := filepath.Join(t.TempDir(), "fork")
	for rel, body := range map[string]string{"checklists/old.md": "stale", "README.md": "mine", "personas/coder.md": "will be overwritten"} {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := runArgsOut(t, st, "profile", "export", "--project", "DEMO", "--dir", dir, "--name", "myteam")
	mustContain(t, out, "removed checklists/old.md")
	if _, err := os.Stat(filepath.Join(dir, "checklists", "old.md")); !os.IsNotExist(err) {
		t.Fatal("stale document survived")
	}
	readme, _ := os.ReadFile(filepath.Join(dir, "README.md"))
	if string(readme) != "mine" {
		t.Fatal("foreign file was touched")
	}
	coder, _ := os.ReadFile(filepath.Join(dir, "personas", "coder.md"))
	mustContain(t, string(coder), "You write code.")

	// A second export of an unchanged project is a no-op on disk.
	before := snapshotDir(t, dir)
	runArgsOut(t, st, "profile", "export", "--project", "DEMO", "--dir", dir, "--name", "myteam")
	if after := snapshotDir(t, dir); after != before {
		t.Fatalf("re-export changed the directory:\n%s\n---\n%s", before, after)
	}
}

// snapshotDir concatenates every file under dir with its path.
func snapshotDir(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, _ := os.ReadFile(p)
		b.WriteString(p + "\n" + string(body) + "\n")
		return nil
	})
	return b.String()
}

func TestProfileExportFeedsApplyOnAFreshProject(t *testing.T) {
	st := exportCLI(t)
	dir := filepath.Join(t.TempDir(), "fork")
	runArgsOut(t, st, "profile", "export", "--project", "DEMO", "--dir", dir, "--name", "myteam")
	runArgsOut(t, st, "project", "create", "--code", "OTHER", "--name", "other", "--capabilities", "scrum", "--actor", "admin@cli:unset")
	out := runArgsOut(t, st, "profile", "apply", "--project", "OTHER", "--dir", dir, "--actor", "admin@cli:unset")
	mustContain(t, out, "applied myteam@dev to OTHER")
	for _, line := range []string{"persona\tcoder\tcreate", "checklist\twork\tcreate", "channel\tdesign\tcreate"} {
		mustContain(t, out, line)
	}
	out = runArgsOut(t, st, "profile", "apply", "--project", "OTHER", "--dir", dir, "--dry-run")
	mustContain(t, out, "3 in sync")

	// Applying the fork back onto its source conflicts by design: the
	// records there belong to demo@1.0.0, and a flat namespace never
	// merges two owners silently. --force adopts them.
	msg, code := runChecklistErrText(t, st, "profile", "apply", "--project", "DEMO", "--dir", dir, "--actor", "admin@cli:unset")
	if code == ExitSuccess {
		t.Fatal("applying the fork onto its source should conflict")
	}
	mustContain(t, msg, "--force")
}

func TestProfileExportJSONIsTheAgentEndpoint(t *testing.T) {
	st := exportCLI(t)
	dir := filepath.Join(t.TempDir(), "fork")
	out := runArgsOut(t, st, "profile", "export", "--project", "DEMO", "--dir", dir, "--name", "myteam", "--output", "json")
	var got struct {
		Project   string               `json:"project"`
		Dir       string               `json:"dir"`
		Ref       string               `json:"ref"`
		Documents []profile.ExportNote `json:"documents"`
		Removed   []string             `json:"removed"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out)
	}
	if got.Ref != "myteam@0.1.0" || got.Project != "DEMO" || len(got.Documents) != 3 {
		t.Fatalf("got %+v", got)
	}
}

func TestProfileExportRefusesBadInputs(t *testing.T) {
	st := exportCLI(t)
	dir := t.TempDir()
	cases := [][]string{
		{"profile", "export", "--project", "DEMO", "--dir", dir},                                    // no --name
		{"profile", "export", "--project", "DEMO", "--name", "myteam"},                              // no --dir
		{"profile", "export", "--project", "DEMO", "--dir", dir, "--name", "My Team"},               // bad name
		{"profile", "export", "--project", "DEMO", "--dir", dir, "--name", "ok", "--version", "v1"}, // bad version
		{"profile", "export", "--project", "NOPE", "--dir", dir, "--name", "ok"},                    // unknown project
	}
	for _, args := range cases {
		if _, code := runChecklistErrText(t, st, args...); code == ExitSuccess {
			t.Errorf("%v succeeded", args)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a refused export wrote files: %v", entries)
	}
}

func hasString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
