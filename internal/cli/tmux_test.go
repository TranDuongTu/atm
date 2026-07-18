package cli

import (
	"bytes"
	"os"
	"testing"
)

// With $TMUX unset the helper must write nothing and must not exec tmux.
// A fake tmux early on PATH poisons via marker file if it is invoked.
func TestSetTmuxWindowLabel_NoTMUX(t *testing.T) {
	t.Setenv("TMUX", "")
	dir := t.TempDir()
	writeScript(t, dir, "tmux", "#!/usr/bin/env bash\necho called >> "+dir+"/called\n")
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	var buf bytes.Buffer
	setTmuxWindowLabel(&buf, tmuxLabelTUI)

	if buf.Len() != 0 {
		t.Fatalf("expected no OSC output outside tmux; got %q", buf.String())
	}
	if _, err := os.Stat(dir + "/called"); err == nil {
		t.Fatalf("tmux was invoked despite $TMUX being unset")
	}
}

// With $TMUX set the helper writes OSC 2 to the writer and execs
// `tmux rename-window <label>`. We assert the exact byte sequence on w
// and the exact argv recorded by a fake tmux.
func TestSetTmuxWindowLabel_TMUX(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,1234,0")
	dir := t.TempDir()
	writeScript(t, dir, "tmux", `#!/usr/bin/env bash
set -eu
printf '%s\n' "$@" >> "`+dir+`/argv.log"
`)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	var buf bytes.Buffer
	setTmuxWindowLabel(&buf, tmuxLabelTUI)

	wantOSC := "\x1b]2;\u2261 ATM TUI\x07"
	if buf.String() != wantOSC {
		t.Fatalf("OSC mismatch:\nwant %q\ngot  %q", wantOSC, buf.String())
	}
	data, err := os.ReadFile(dir + "/argv.log")
	if err != nil {
		t.Fatalf("fake tmux was not invoked: %v", err)
	}
	wantArgv := "rename-window\n\u2261 ATM TUI\n"
	if string(data) != wantArgv {
		t.Fatalf("argv mismatch:\nwant %q\ngot  %q", wantArgv, string(data))
	}
}

// With $TMUX set but no tmux binary on PATH the helper still emits OSC
// (-pane title works without tmux too) and silently skips rename-window.
func TestSetTmuxWindowLabel_NoTmuxBinary(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,1234,0")
	t.Setenv("PATH", "/nonexistent")

	var buf bytes.Buffer
	setTmuxWindowLabel(&buf, tmuxLabelTUI)

	wantOSC := "\x1b]2;\u2261 ATM TUI\x07"
	if buf.String() != wantOSC {
		t.Fatalf("OSC mismatch:\nwant %q\ngot  %q", wantOSC, buf.String())
	}
}

func writeScript(t *testing.T, dir, name, body string) {
	t.Helper()
	path := dir + "/" + name
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}
