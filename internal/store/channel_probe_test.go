// internal/store/channel_probe_test.go
package store

import (
	"os/exec"
	"testing"
)

// gitDir builds a git fixture; skips when git is unavailable.
func gitDir(t *testing.T, commands ...[]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(cmd.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("commit", "--allow-empty", "-q", "-m", "init")
	for _, c := range commands {
		run(c...)
	}
	return dir
}

func TestProbeRepoPath(t *testing.T) {
	if p := probeRepoPath("/nonexistent/dir"); p.PathExists {
		t.Fatal("missing dir must probe PathExists=false")
	}
	plain := t.TempDir()
	if p := probeRepoPath(plain); !p.PathExists || p.IsGitRepo {
		t.Fatalf("plain dir: %+v", p)
	}
	clean := gitDir(t)
	if p := probeRepoPath(clean); !p.IsGitRepo || p.Dirty || p.HasUpstream {
		t.Fatalf("clean repo: %+v", p)
	}
}
