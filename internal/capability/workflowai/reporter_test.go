package workflowai

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanCheckFindings(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)

	mkPlanned := func(title, kind, ref string) string {
		tk, _ := s.CreateTask("ATM", title, "", []string{"ATM:stage:clarified"}, testActor)
		if _, err := r.Plan(tk.ID, kind, ref); err != nil {
			t.Fatalf("plan %s: %v", title, err)
		}
		return tk.ID
	}
	good := mkPlanned("good", PlanKindFile, "docs/good.md")
	missing := mkPlanned("missing", PlanKindFile, "docs/gone.md")
	eph := mkPlanned("eph", PlanKindEphemeral, "session 2026-07-14")
	// Hand-edited to planned with NO record at all.
	bare, _ := s.CreateTask("ATM", "bare", "", []string{"ATM:stage:planned"}, testActor)
	// Malformed payload on a planned task.
	bad, _ := s.CreateTask("ATM", "bad", "", []string{"ATM:stage:planned"}, testActor)
	_ = s.SetTaskCapabilityMeta(bad.ID, CapabilityName, "not json", testActor)
	// A new task with no stage is out of scope entirely.
	_, _ = s.CreateTask("ATM", "outofscope", "", nil, testActor)

	verify := func(kind, ref string) (bool, string) {
		if ref == "docs/good.md" {
			return true, ""
		}
		return false, "plan file missing: " + ref
	}
	findings, healthy, err := (&Reporter{Store: s}).PlanCheck("ATM", verify)
	if err != nil {
		t.Fatalf("PlanCheck: %v", err)
	}
	if healthy != 1 {
		t.Errorf("healthy = %d, want 1 (%s)", healthy, good)
	}
	byTask := map[string]string{}
	for _, f := range findings {
		byTask[f.TaskID] = f.Detail
	}
	if len(findings) != 4 {
		t.Errorf("findings = %d (%v), want 4", len(findings), byTask)
	}
	if d := byTask[missing]; !strings.Contains(d, "plan file missing") {
		t.Errorf("missing: %q", d)
	}
	if d := byTask[eph]; !strings.Contains(d, "unverifiable") {
		t.Errorf("ephemeral: %q", d)
	}
	if d := byTask[bare.ID]; !strings.Contains(d, "no plan recorded") {
		t.Errorf("bare: %q", d)
	}
	if d := byTask[bad.ID]; !strings.Contains(d, "unparseable") {
		t.Errorf("bad payload: %q", d)
	}
}

func TestDefaultVerifierFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "p.md"), []byte("# plan"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := DefaultVerifier(dir)
	if ok, _ := v(PlanKindFile, "docs/p.md"); !ok {
		t.Error("existing file reported missing")
	}
	if ok, detail := v(PlanKindFile, "docs/nope.md"); ok || !strings.Contains(detail, "missing") {
		t.Errorf("missing file: ok=%v detail=%q", ok, detail)
	}
}

func TestDefaultVerifierCommit(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f")
	run("commit", "-q", "-m", "c1")
	v := DefaultVerifier(dir)
	if ok, _ := v(PlanKindCommit, "HEAD"); !ok {
		t.Error("HEAD reported unresolvable")
	}
	if ok, detail := v(PlanKindCommit, "deadbeef"); ok || !strings.Contains(detail, "unresolvable") {
		t.Errorf("bogus commit: ok=%v detail=%q", ok, detail)
	}
}

func TestLinksOutboundAndInbound(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	parent, _ := s.CreateTask("ATM", "parent", "", nil, testActor)
	c1, _ := s.CreateTask("ATM", "c1", "", nil, testActor)
	c2, _ := s.CreateTask("ATM", "c2", "", nil, testActor)
	other, _ := s.CreateTask("ATM", "other", "", nil, testActor)
	_ = r.LinkRevisionOf(c1.ID, parent.ID)
	_ = r.LinkRevisionOf(c2.ID, parent.ID)
	_ = r.LinkRelatesTo(other.ID, parent.ID)
	_ = r.LinkRelatesTo(c1.ID, other.ID)

	got, err := (&Reporter{Store: s}).Links(parent.ID)
	if err != nil {
		t.Fatalf("Links: %v", err)
	}
	if got.RevisionOf != "" {
		t.Errorf("RevisionOf = %q", got.RevisionOf)
	}
	if len(got.Revisions) != 2 || !containsString(got.Revisions, c1.ID) || !containsString(got.Revisions, c2.ID) {
		t.Errorf("Revisions = %v", got.Revisions)
	}
	if len(got.RelatedFrom) != 1 || got.RelatedFrom[0] != other.ID {
		t.Errorf("RelatedFrom = %v", got.RelatedFrom)
	}
	gotC1, _ := (&Reporter{Store: s}).Links(c1.ID)
	if gotC1.RevisionOf != parent.ID || len(gotC1.RelatesTo) != 1 || gotC1.RelatesTo[0] != other.ID {
		t.Errorf("c1 links = %+v", gotC1)
	}
}
