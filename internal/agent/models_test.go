package agent

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func fakeRun(out string, err error) RunFunc {
	return func(context.Context, string, ...string) ([]byte, error) { return []byte(out), err }
}

func TestListModelsOpencodeNative(t *testing.T) {
	var gotName string
	var gotArgs []string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		gotName, gotArgs = name, args
		return []byte("opencode/big-pickle\nopencode-go/glm-5.2\n"), nil
	}
	got, err := ListModels(context.Background(), Selection{Agent: "opencode", Launcher: LauncherNative}, run)
	if err != nil {
		t.Fatal(err)
	}
	if gotName != "opencode" || !reflect.DeepEqual(gotArgs, []string{"models"}) {
		t.Fatalf("ran %q %v", gotName, gotArgs)
	}
	if !reflect.DeepEqual(got, []string{"opencode/big-pickle", "opencode-go/glm-5.2"}) {
		t.Fatalf("models = %v", got)
	}
}

// The LAUNCHER decides who lists: ollama serves the model, so ollama lists it
// no matter which harness it is launching.
func TestListModelsOllamaListsRegardlessOfAgent(t *testing.T) {
	out := "NAME                       ID              SIZE      MODIFIED\n" +
		"nomic-embed-text:latest    0a109f422b47    274 MB    4 weeks ago\n" +
		"qwen3:8b                   1b209f422b48    5.2 GB    2 days ago\n"
	for _, ag := range []string{"claude", "codex", "opencode"} {
		var gotName string
		run := func(_ context.Context, name string, _ ...string) ([]byte, error) {
			gotName = name
			return []byte(out), nil
		}
		got, err := ListModels(context.Background(), Selection{Agent: ag, Launcher: LauncherOllama}, run)
		if err != nil {
			t.Fatalf("%s: %v", ag, err)
		}
		if gotName != "ollama" {
			t.Fatalf("%s ran %q, want ollama", ag, gotName)
		}
		if !reflect.DeepEqual(got, []string{"nomic-embed-text:latest", "qwen3:8b"}) {
			t.Fatalf("%s models = %v (header must be dropped)", ag, got)
		}
	}
}

func TestListModelsNoListerForClaudeAndCodex(t *testing.T) {
	for _, ag := range []string{"claude", "codex"} {
		_, err := ListModels(context.Background(), Selection{Agent: ag, Launcher: LauncherNative}, fakeRun("", nil))
		if !errors.Is(err, ErrNoLister) {
			t.Fatalf("%s: err = %v, want ErrNoLister", ag, err)
		}
	}
}

// A failing command must surface the error, never an empty list — an empty
// list reads as "this launcher has no models", which is a different claim.
func TestListModelsPropagatesRunError(t *testing.T) {
	boom := errors.New("exit status 1")
	_, err := ListModels(context.Background(), Selection{Agent: "opencode", Launcher: LauncherNative}, fakeRun("", boom))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListModelsIgnoresBlankLines(t *testing.T) {
	got, err := ListModels(context.Background(), Selection{Agent: "opencode", Launcher: LauncherNative},
		fakeRun("\nopencode/a\n\n  \nopencode/b\n", nil))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"opencode/a", "opencode/b"}) {
		t.Fatalf("models = %v", got)
	}
}
