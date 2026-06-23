package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestExitCodeForError(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{nil, ExitSuccess},
		{ErrUsage, ExitUsage},
		{ErrNotFound, ExitNotFound},
		{ErrConflict, ExitConflict},
		{errors.New("boom"), ExitGeneric},
		{fmt.Errorf("%w: wrapped", ErrUsage), ExitUsage},
		{fmt.Errorf("%w: wrapped", ErrNotFound), ExitNotFound},
		{fmt.Errorf("%w: wrapped", ErrConflict), ExitConflict},
	}
	for i, c := range cases {
		got := ExitCodeForError(c.err)
		if got != c.want {
			t.Fatalf("case %d: got %d want %d", i, got, c.want)
		}
	}
}

func TestCodeForError(t *testing.T) {
	cases := []struct {
		err  error
		want ErrorCode
	}{
		{nil, CodeSuccess},
		{ErrUsage, CodeUsage},
		{ErrNotFound, CodeNotFound},
		{ErrConflict, CodeConflict},
		{errors.New("x"), CodeGeneric},
	}
	for _, c := range cases {
		if got := CodeForError(c.err); got != c.want {
			t.Errorf("got %q want %q", got, c.want)
		}
	}
}

func TestErrorEnvelopeJSON(t *testing.T) {
	env := NewErrorEnvelope(CodeConflict, "already claimed")
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"error":{"code":"conflict","message":"already claimed"}}`
	if string(data) != want {
		t.Fatalf("got %s want %s", data, want)
	}
}

func TestErrorEnvelopeFromError(t *testing.T) {
	err := fmt.Errorf("%w: task ATM-0001 not found", ErrNotFound)
	env := NewErrorEnvelopeFromError(err)
	if env.Error.Code != string(CodeNotFound) {
		t.Fatalf("code = %q want %q", env.Error.Code, CodeNotFound)
	}
	if !strings.Contains(env.Error.Message, "ATM-0001") {
		t.Fatalf("message = %q", env.Error.Message)
	}
}

func TestErrorEnvelopeWrappedErrors(t *testing.T) {
	wrapped := fmt.Errorf("outer: %w", ErrConflict)
	code := CodeForError(wrapped)
	if code != CodeConflict {
		t.Fatalf("got %q want %q", code, CodeConflict)
	}
}

func TestExitCodesConstants(t *testing.T) {
	if ExitSuccess != 0 || ExitGeneric != 1 || ExitUsage != 2 || ExitNotFound != 3 || ExitConflict != 4 {
		t.Fatal("exit code constants do not match spec")
	}
}
