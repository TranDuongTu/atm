package cli

import (
	"errors"
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
