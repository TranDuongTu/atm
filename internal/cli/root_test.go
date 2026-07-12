package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootNoArgsLaunchesTUI(t *testing.T) {
	h := newGoldenHarness(t)
	var gotStore, gotActor string
	h.st.runTUI = func(storePath, actor string) error {
		gotStore = storePath
		gotActor = actor
		return nil
	}

	_, _, code := h.run()
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	if gotStore != h.store.StorePath() {
		t.Fatalf("tui store = %q, want %q", gotStore, h.store.StorePath())
	}
	if gotActor != "admin@tui:unset" {
		t.Fatalf("tui actor = %q, want admin@tui:unset", gotActor)
	}
}

func TestRootNoArgsLaunchesTUIWithEnvActor(t *testing.T) {
	h := newGoldenHarness(t)
	t.Setenv("ATM_ACTOR", "staff@tui:unset")
	var gotActor string
	h.st.runTUI = func(storePath, actor string) error {
		gotActor = actor
		return nil
	}

	_, _, code := h.run()
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0", code)
	}
	if gotActor != "staff@tui:unset" {
		t.Fatalf("tui actor = %q, want staff@tui:unset", gotActor)
	}
}

func TestRootNoArgsTUIErrorPropagates(t *testing.T) {
	h := newGoldenHarness(t)
	h.st.runTUI = func(storePath, actor string) error {
		return errors.New("boom")
	}

	_, stderr, code := h.run()
	if code != ExitGeneric {
		t.Fatalf("exit = %d, want %d", code, ExitGeneric)
	}
	if stderr == "" {
		t.Fatalf("stderr empty, want error envelope")
	}
}

func TestTUICommandRemoved(t *testing.T) {
	h := newGoldenHarness(t)
	_, _, code := h.run("tui")
	if code == ExitSuccess {
		t.Fatalf("atm tui should be removed")
	}
}

func TestRootActorFlagNotGlobal(t *testing.T) {
	h := newGoldenHarness(t)
	_, _, code := h.run("--actor", "admin@cli:unset", "version")
	if code == ExitSuccess {
		t.Fatalf("root --actor should not be accepted as a global flag")
	}
}

func TestInitInstallsSelectedAgentPlugins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	installFakeCodexForCLI(t, home)
	h := newGoldenHarness(t)
	h.output = outputText

	out, _, code := h.run("init", "--agent", "codex", "--agent", "claude")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, h.stderr.String())
	}
	for _, path := range []string{
		filepath.Join(home, ".codex", "plugins", "atm-developing", ".codex-plugin", "plugin.json"),
		filepath.Join(home, ".codex", "agents", "atm-manager.md"),
		filepath.Join(home, ".claude", "skills", "atm-developing", ".claude-plugin", "plugin.json"),
		filepath.Join(home, ".claude", "agents", "atm-manager.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected installed file %s: %v\noutput:\n%s", path, err, out)
		}
	}
	for _, want := range []string{
		"developing\tcodex\tinstalled",
		"manager\tcodex\tinstalled",
		"developing\tclaude\tinstalled",
		"manager\tclaude\tinstalled",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("init output missing %q:\n%s", want, out)
		}
	}
}

func TestInitDryRunAllReportsPluginsWithoutWriting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	h := newGoldenHarness(t)
	h.output = outputText

	out, _, code := h.run("init", "--dry-run", "--agent", "all")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, h.stderr.String())
	}
	for _, path := range []string{
		filepath.Join(home, ".config", "opencode", "plugins", "atm-developing.js"),
		filepath.Join(home, ".codex", "plugins", "atm-developing"),
		filepath.Join(home, ".claude", "skills", "atm-developing"),
		filepath.Join(home, ".config", "opencode", "agents", "atm-manager.md"),
		filepath.Join(home, ".codex", "agents", "atm-manager.md"),
		filepath.Join(home, ".claude", "agents", "atm-manager.md"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("dry-run wrote %s: %v\noutput:\n%s", path, err, out)
		}
	}
	for _, want := range []string{
		"developing\topencode\twould install",
		"manager\topencode\twould install",
		"developing\tcodex\twould install",
		"manager\tcodex\twould install",
		"developing\tclaude\twould install",
		"manager\tclaude\twould install",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("dry-run output missing %q:\n%s", want, out)
		}
	}
}

func installFakeCodexForCLI(t *testing.T, home string) {
	t.Helper()
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
	bin := filepath.Join(binDir, "codex")
	script := `#!/bin/sh
mkdir -p "$HOME/.codex/plugins/cache/atm-local/atm-developing/0.1.0/.codex-plugin"
printf '{"name":"atm-developing"}\n' > "$HOME/.codex/plugins/cache/atm-local/atm-developing/0.1.0/.codex-plugin/plugin.json"
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
