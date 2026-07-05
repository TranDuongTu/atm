package cli

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDevelopingCodexDryRunJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "FOO", "--name", "Foo", "--actor", "ttran")
	h.reset()
	_, _, code := h.run("developing", "codex", "--project", "FOO", "--dry-run")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	got := normalizeDevelopingOutput(h.stdout.String(), h.store.StorePath())
	compareGolden(t, "developing-dry-run-codex", got)
}

func TestDevelopingMissingProject(t *testing.T) {
	h := newGoldenHarness(t)
	_, stderrStr, code := h.run("developing", "codex", "--project", "NOPE", "--dry-run")
	if code != ExitNotFound {
		t.Fatalf("exit = %d, want %d", code, ExitNotFound)
	}
	got := normalizeDevelopingOutput(stderrStr, h.store.StorePath())
	compareGolden(t, "developing-missing-project", got)
}

func TestDevelopingTailSummaryJSON(t *testing.T) {
	st := &cliState{flags: globalFlags{output: outputJSON}}
	buf := &bytes.Buffer{}
	st.out = buf
	if err := emitDevelopingTail(st, "codex", "FOO", "FOO-RUNID", "/STORE/developing/FOO-RUNID.md", 0); err != nil {
		t.Fatalf("emitDevelopingTail: %v", err)
	}
	got := normalizeDevelopingOutput(buf.String(), "")
	compareGolden(t, "developing-tail-summary", got)
}

func normalizeDevelopingOutput(s, storePath string) string {
	s = normalizeOutput(s)
	if storePath != "" {
		contextPathRe := regexp.MustCompile(strings.ReplaceAll(filepath.ToSlash(storePath), `/`, `\/`) + `/developing/FOO-\d{14}-[0-9a-f]{6}\.md`)
		s = contextPathRe.ReplaceAllString(s, "/STORE/developing/FOO-RUNID.md")
	}
	runIDRe := regexp.MustCompile(`FOO-\d{14}-[0-9a-f]{6}`)
	s = runIDRe.ReplaceAllString(s, "FOO-RUNID")
	atmBinRe := regexp.MustCompile(`"ATM_BIN": "[^"]+"`)
	s = atmBinRe.ReplaceAllString(s, `"ATM_BIN": "/ATM_BIN"`)
	return s
}
