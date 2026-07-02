package cli

import (
	"bytes"
	"path/filepath"
	"testing"

	"atm/internal/store"
)

func TestLabelListNamespaceRequiresProject(t *testing.T) {
	dir := t.TempDir()
	sp := filepath.Join(dir, "store")
	if _, err := store.Open(sp); err != nil {
		t.Fatal(err)
	}

	st := &cliState{out: &bytes.Buffer{}, err: &bytes.Buffer{}}
	root := newRootCmdWithState(st)
	root.SetOut(st.out)
	root.SetErr(st.err)
	root.SetArgs([]string{"label", "list", "--store", sp, "--namespace", "status"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error for --namespace without --project, got nil")
	}
	code := ExitCodeForError(err)
	if code != ExitUsage {
		t.Fatalf("expected exit %d (usage) for --namespace without --project, got %d (err=%v)", ExitUsage, code, err)
	}
}
