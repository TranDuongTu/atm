package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"atm/internal/update"
)

func TestUpdatePassesVersionFlag(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	var got update.Options
	h.st.runUpdate = func(_ context.Context, opts update.Options) (update.Result, error) {
		got = opts
		return update.Result{OldVersion: "v1.2.2", NewVersion: "v1.2.3", Updated: true}, nil
	}
	out, errOut, code := h.run("update", "--version", "v1.2.3")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, errOut)
	}
	if got.Version != "v1.2.3" {
		t.Fatalf("Version = %q", got.Version)
	}
	if strings.TrimSpace(out) != "updated atm v1.2.2 -> v1.2.3" {
		t.Fatalf("out = %q", out)
	}
}

func TestUpdateAlreadyCurrent(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	h.st.runUpdate = func(context.Context, update.Options) (update.Result, error) {
		return update.Result{OldVersion: "v1.2.3", NewVersion: "v1.2.3", Updated: false}, nil
	}
	out, errOut, code := h.run("update")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, errOut)
	}
	if strings.TrimSpace(out) != "atm is already up to date: v1.2.3" {
		t.Fatalf("out = %q", out)
	}
}

func TestUpdateJSON(t *testing.T) {
	h := newGoldenHarness(t)
	h.st.runUpdate = func(context.Context, update.Options) (update.Result, error) {
		return update.Result{OldVersion: "v1.2.2", NewVersion: "v1.2.3", TargetPath: "/tmp/atm", Updated: true}, nil
	}
	out, errOut, code := h.run("update")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, errOut)
	}
	compareGolden(t, "update-json", out)
}

func TestUpdateRunnerError(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	h.st.runUpdate = func(context.Context, update.Options) (update.Result, error) {
		return update.Result{}, errors.New("network down")
	}
	_, errOut, code := h.run("update")
	if code == 0 {
		t.Fatal("expected non-zero exit")
	}
	if errOut != "" {
		t.Fatalf("text-mode harness should not render production errors directly, got %q", errOut)
	}
}

func TestUpdateHelp(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	out, errOut, code := h.run("update", "--help")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, errOut)
	}
	if !strings.Contains(out, "--version") {
		t.Fatalf("help missing --version:\n%s", out)
	}
}
