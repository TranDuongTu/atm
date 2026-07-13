package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// chdirRepo creates a git repo with one committed file and chdirs into it for
// the duration of the test. `atm context` resolves its repo from the working
// directory, which is the repo the manager is running in.
func chdirRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg", "a.go"), []byte("package pkg\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "init")
	t.Chdir(dir) // Go 1.24+; restores the old cwd at test end
	return dir
}

func commitInRepo(t *testing.T, dir, rel, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-qm", "change"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// storeDigest hashes every logical-store file under the store root, so a
// byte-level change anywhere in the ledger changes the digest. The cache.db
// SQLite file (and its -shm/-wal sidecars) is documented as disposable and
// rebuildable from log.jsonl, and it always churns on every open in WAL mode;
// it is not the logical store, so it is excluded.
func storeDigest(t *testing.T, root string) string {
	t.Helper()
	var paths []string
	if err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			base := filepath.Base(p)
			if base == "cache.db" || base == "cache.db-shm" || base == "cache.db-wal" {
				return nil
			}
			paths = append(paths, p)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		h.Write([]byte(p))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// setup makes a harness with project TST, inside a git repo, with an actor set.
func setup(t *testing.T) (*goldenHarness, string) {
	t.Helper()
	repo := chdirRepo(t)
	h := newGoldenHarness(t)
	h.output = outputText
	const actor = "manager@claude:opus-4.8"
	if _, _, code := h.run("project", "create", "--code", "TST", "--name", "Test", "--actor", actor); code != ExitSuccess {
		t.Fatalf("project create: exit %d", code)
	}
	return h, repo
}

func TestContextAddThenCheckReportsOK(t *testing.T) {
	h, _ := setup(t)
	const actor = "manager@claude:opus-4.8"
	h.run("task", "create", "--project", "TST", "--title", "Pointer: pkg", "--actor", actor)
	if _, errOut, code := h.run("context", "add", "--task", "TST-0001",
		"--kind", "documentation", "--source", "git:pkg", "--actor", actor); code != ExitSuccess {
		t.Fatalf("context add: exit %d: %s", code, errOut)
	}

	out, _, code := h.run("context", "check", "--project", "TST")
	if code != ExitSuccess {
		t.Fatalf("context check: exit %d", code)
	}
	if !strings.Contains(out, "OK (1)") {
		t.Errorf("want OK (1) in report:\n%s", out)
	}
	if strings.Contains(out, "DRIFT") {
		t.Errorf("nothing changed; check must not report drift:\n%s", out)
	}
}

func TestContextCheckIsReadOnly(t *testing.T) {
	// The design invariant: check reports, it never decides. Prove the store is
	// byte-identical after a check run that finds drift, age, and unverified
	// pointers all at once.
	h, repo := setup(t)
	const actor = "manager@claude:opus-4.8"
	h.run("task", "create", "--project", "TST", "--title", "Pointer: pkg", "--actor", actor)
	h.run("task", "create", "--project", "TST", "--title", "Pointer: handwritten", "--actor", actor)
	h.run("task", "label", "add", "--task", "TST-0002", "--label", "TST:context:documentation", "--actor", actor)
	h.run("context", "add", "--task", "TST-0001", "--kind", "documentation",
		"--source", "git:pkg", "--source", "external:jira/TST-9", "--actor", actor)
	commitInRepo(t, repo, "pkg/a.go", "package pkg\n\nfunc New() {}\n") // force DRIFT

	root := h.store.StorePath()
	before := storeDigest(t, root)
	out, _, code := h.run("context", "check", "--project", "TST")
	if code != ExitSuccess {
		t.Fatalf("context check: exit %d", code)
	}
	after := storeDigest(t, root)

	if before != after {
		t.Fatalf("check mutated the store\nreport was:\n%s", out)
	}
	// And it really did have all three things to report.
	for _, want := range []string{"DRIFT", "AGE", "UNVERIFIED"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %s section:\n%s", want, out)
		}
	}
}

func TestContextAddRequiresActor(t *testing.T) {
	h, _ := setup(t)
	h.run("task", "create", "--project", "TST", "--title", "x", "--actor", "manager@claude:opus-4.8")
	if _, _, code := h.run("context", "add", "--task", "TST-0001",
		"--kind", "documentation", "--source", "git:pkg"); code == ExitSuccess {
		t.Error("mutating command must require --actor or ATM_ACTOR")
	}
}

func TestContextRejectsUnkindedSource(t *testing.T) {
	h, _ := setup(t)
	const actor = "manager@claude:opus-4.8"
	h.run("task", "create", "--project", "TST", "--title", "x", "--actor", actor)
	_, _, code := h.run("context", "add", "--task", "TST-0001",
		"--kind", "documentation", "--source", "pkg", "--actor", actor)
	if code != ExitUsage {
		t.Errorf("bare path without a kind prefix must be a usage error, got exit %d", code)
	}
}

func TestContextWorksWithoutSeededLabels(t *testing.T) {
	// The capability bootstraps its own vocabulary. project create seeds the
	// default labels, but context-current and knowledge:superseded are NOT among
	// them -- so this proves the capability ensured them itself.
	h, _ := setup(t)
	const actor = "manager@claude:opus-4.8"
	h.run("task", "create", "--project", "TST", "--title", "Pointer: pkg", "--actor", actor)
	h.run("context", "add", "--task", "TST-0001", "--kind", "documentation",
		"--source", "git:pkg", "--actor", actor)

	out, _, _ := h.run("label", "list", "--project", "TST")
	for _, want := range []string{"TST:context-current", "TST:comment:provenance"} {
		if !strings.Contains(out, want) {
			t.Errorf("%s was not ensured by the capability:\n%s", want, out)
		}
	}
}
