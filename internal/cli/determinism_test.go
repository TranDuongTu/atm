package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"atm/internal/store"
)

func seedDeterminismStore(t *testing.T, h *goldenHarness) {
	t.Helper()
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "human:alice")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "Agent Tasks Management",
		"--label", "type:epic", "--label", "type:user-story", "--label", "type:impl",
		"--label", "type:bug", "--label", "area:cli", "--label", "area:tui",
		"--label", "kind:convention", "--type-axis", "type", "--actor", "human:alice")
	h.run("project", "create", "--store", sp, "--code", "DEMO", "--name", "Demo",
		"--label", "type:impl", "--label", "area:cli", "--actor", "human:alice")
	h.run("project", "label", "add", "--store", sp, "--code", "DEMO",
		"--label", "type:bug", "--description", "Bug fix", "--actor", "human:alice")
	h.run("project", "set-type-axis", "--store", sp, "--code", "DEMO",
		"--namespace", "type", "--actor", "human:alice")
	h.run("project", "set-name", "--store", sp, "--code", "DEMO",
		"--name", "Demo Project", "--actor", "human:alice")
	h.run("project", "repo", "add", "--store", sp, "--code", "ATM",
		"--path", "/tmp/repo-atm", "--actor", "human:alice")

	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "PR conventions for bug fixes",
		"--label", "kind:convention", "--label", "type:bug", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Fix claim race",
		"--label", "type:bug", "--label", "area:cli", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Blocked subtask",
		"--label", "type:impl", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Epic: agent workflow",
		"--label", "type:epic", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Impl: claim command",
		"--label", "type:impl", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Testing conventions",
		"--label", "kind:convention", "--label", "area:cli", "--actor", "human:alice")
	h.run("task", "create", "--store", sp, "--project", "DEMO", "--title", "Old task",
		"--label", "area:cli", "--actor", "human:alice")

	h.run("task", "link", "add", "--store", sp, "--id", "ATM-0002", "--type", "blocks",
		"--target", "ATM-0003", "--actor", "human:alice")
	h.run("task", "link", "add", "--store", sp, "--id", "ATM-0005", "--type", "implements",
		"--target", "ATM-0004", "--actor", "human:alice")

	h.run("task", "set-title", "--store", sp, "--id", "ATM-0002",
		"--title", "Fix claim race (revised)", "--actor", "human:alice")
	h.run("task", "set-description", "--store", sp, "--id", "ATM-0002",
		"--description", "Investigate and fix the claim race.", "--actor", "human:alice")
	h.run("task", "label", "add", "--store", sp, "--id", "ATM-0002",
		"--label", "area:tui", "--actor", "human:alice")
	h.run("task", "label", "remove", "--store", sp, "--id", "ATM-0002",
		"--label", "area:tui", "--actor", "human:alice")

	h.run("task", "todo", "add", "--store", sp, "--id", "ATM-0002", "--text", "Write tests for claim",
		"--actor", "agent:claude-1")
	h.run("task", "todo", "toggle", "--store", sp, "--id", "ATM-0002", "--todo", "t1",
		"--actor", "agent:claude-1")
	h.run("task", "followup", "add", "--store", sp, "--id", "ATM-0002", "--text", "Decide storage format",
		"--assignee", "human:alice", "--actor", "human:alice")
	h.run("task", "discussion", "add", "--store", sp, "--id", "ATM-0002", "--text", "Use file-level locking.",
		"--actor", "human:alice")

	h.run("task", "claim", "--store", sp, "--id", "ATM-0005", "--actor", "agent:claude-1")
	h.run("task", "set-status", "--store", sp, "--id", "ATM-0005", "--status", "in-progress",
		"--actor", "agent:claude-1")
	h.run("review", "request", "--store", sp, "--id", "ATM-0005", "--actor", "agent:claude-1")

	h.run("project", "guide", "section", "add", "--store", sp, "--code", "ATM",
		"--name", "conventions", "--actor", "human:alice")
	h.run("project", "guide", "section", "add", "--store", sp, "--code", "ATM",
		"--name", "testing", "--actor", "human:alice")
	h.run("project", "guide", "ref", "add", "--store", sp, "--code", "ATM",
		"--section", "conventions", "--kind", "task", "--target", "ATM-0001", "--actor", "human:alice")
	h.run("project", "guide", "ref", "add", "--store", sp, "--code", "ATM",
		"--section", "testing", "--kind", "task", "--target", "ATM-0006", "--actor", "human:alice")
	h.run("project", "guide", "set-freshness", "--store", sp, "--code", "ATM",
		"--threshold", "720h", "--actor", "human:alice")

	h.run("project", "label", "remove", "--store", sp, "--code", "DEMO",
		"--label", "area:cli", "--actor", "human:alice")

	h.reset()
}

func determinismReadCommands() [][]string {
	return [][]string{
		{"store", "path"},
		{"project", "list"},
		{"project", "show", "--code", "ATM"},
		{"project", "show", "--code", "DEMO"},
		{"project", "label", "list", "--code", "ATM"},
		{"project", "label", "list", "--code", "DEMO"},
		{"project", "guide", "show", "--code", "ATM"},
		{"project", "guide", "status", "--code", "ATM"},
		{"task", "list"},
		{"task", "list", "--project", "ATM"},
		{"task", "list", "--project", "DEMO"},
		{"task", "list", "--project", "ATM", "--label", "type:bug"},
		{"task", "list", "--project", "ATM", "--status", "open"},
		{"task", "list", "--project", "ATM", "--claimant", "agent:claude-1"},
		{"task", "show", "--id", "ATM-0001"},
		{"task", "show", "--id", "ATM-0002"},
		{"task", "show", "--id", "ATM-0002", "--with-context"},
		{"task", "show", "--id", "ATM-0005"},
		{"task", "next", "--project", "ATM"},
		{"task", "link", "list", "--id", "ATM-0001"},
		{"task", "link", "list", "--id", "ATM-0002"},
		{"task", "link", "list", "--id", "ATM-0004"},
		{"task", "timeline", "--id", "ATM-0002"},
		{"review", "queue"},
		{"review", "queue", "--project", "ATM"},
		{"review", "followups"},
		{"review", "followups", "--project", "ATM"},
		{"review", "dashboard", "--project", "ATM"},
		{"actor", "list"},
		{"actor", "show", "--id", "agent:claude-1"},
		{"actor", "show", "--id", "human:alice"},
	}
}

func TestDeterminismByteIdentical(t *testing.T) {
	h := newGoldenHarness(t)
	seedDeterminismStore(t, h)

	for _, args := range determinismReadCommands() {
		out1, _, code1 := h.run(args...)
		if code1 != 0 {
			t.Fatalf("%v: first run exit = %d stderr=%s", args, code1, h.stderr.String())
		}
		out2, _, code2 := h.run(args...)
		if code2 != 0 {
			t.Fatalf("%v: second run exit = %d stderr=%s", args, code2, h.stderr.String())
		}
		n1, n2 := normalizeOutput(out1), normalizeOutput(out2)
		if n1 != n2 {
			t.Fatalf("determinism violated for %v\n--- run1 ---\n%s\n--- run2 ---\n%s", args, n1, n2)
		}
		if isStorePathCommand(args) {
			continue
		}
		name := "determinism-" + sanitizedPath(args)
		compareGolden(t, name, out1)
	}
}

func isStorePathCommand(args []string) bool {
	return len(args) == 2 && args[0] == "store" && args[1] == "path"
}

func TestDetachabilityByCopy(t *testing.T) {
	h := newGoldenHarness(t)
	seedDeterminismStore(t, h)
	src := h.store.StorePath()

	dst, err := os.MkdirTemp("", "atm-detach-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dst)
	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copy store: %v", err)
	}

	readCmds := [][]string{
		{"task", "list", "--project", "ATM"},
		{"task", "show", "--id", "ATM-0002", "--with-context"},
		{"review", "dashboard", "--project", "ATM"},
		{"project", "guide", "status", "--code", "ATM"},
		{"task", "timeline", "--id", "ATM-0002"},
		{"actor", "show", "--id", "agent:claude-1"},
	}

	for _, args := range readCmds {
		outSrc, _, codeSrc := h.run(args...)
		if codeSrc != 0 {
			t.Fatalf("src %v: exit = %d stderr=%s", args, codeSrc, h.stderr.String())
		}
		hCopy := newGoldenHarnessAt(t, dst)
		outDst, _, codeDst := hCopy.run(args...)
		if codeDst != 0 {
			t.Fatalf("dst %v: exit = %d stderr=%s", args, codeDst, hCopy.stderr.String())
		}
		nSrc, nDst := normalizeOutput(outSrc), normalizeOutput(outDst)
		if nSrc != nDst {
			t.Fatalf("detachability violated for %v\n--- src ---\n%s\n--- dst ---\n%s", args, nSrc, nDst)
		}
	}
}

func newGoldenHarnessAt(t *testing.T, storePath string) *goldenHarness {
	t.Helper()
	st := &cliState{flags: globalFlags{output: outputJSON}}
	buf := &bytes.Buffer{}
	ebuf := &bytes.Buffer{}
	st.out = buf
	st.err = ebuf
	s, err := store.Open(storePath)
	if err != nil {
		t.Fatal(err)
	}
	st.flags.store = s.StorePath()
	return &goldenHarness{t: t, st: st, store: s, stdout: buf, stderr: ebuf}
}

func sanitizedPath(args []string) string {
	out := ""
	for _, a := range args {
		if a == "" {
			continue
		}
		a = strings.TrimLeft(a, "-")
		if a == "" {
			continue
		}
		if out != "" {
			out += "-"
		}
		out += a
	}
	if out == "" {
		out = "root"
	}
	return out
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}
