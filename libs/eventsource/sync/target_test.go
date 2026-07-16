package eventsync

import (
	"path/filepath"
	"testing"
)

// TestSelectTarget drives SelectTarget's transport-selection order against
// the table in task-4-brief.md: a "git::" prefix always forces git; a
// "git@"/"ssh://" prefix, a ".git" suffix, or a ".git//" infix is
// recognized as git without forcing; an existing directory is a
// DirTarget; anything else is an error.
func TestSelectTarget(t *testing.T) {
	dir := t.TempDir() // exists → DirTarget

	cases := []struct {
		in, kind, url, sub string
		err                bool
	}{
		{in: dir, kind: "dir"},
		{in: "git@github.com:u/r.git", kind: "git", url: "git@github.com:u/r.git", sub: ".atm"},
		{in: "git@github.com:u/r.git//store", kind: "git", url: "git@github.com:u/r.git", sub: "store"},
		{in: "https://host/u/r.git", kind: "git", url: "https://host/u/r.git", sub: ".atm"},
		{in: "ssh://host/r", kind: "git", url: "ssh://host/r", sub: ".atm"},
		{in: "git::/tmp/bare.git//x", kind: "git", url: "/tmp/bare.git", sub: "x"},
		{in: filepath.Join(dir, "missing"), err: true}, // neither an existing dir nor a git URL
	}

	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			target, err := SelectTarget("/remotes-cache", c.in)
			if c.err {
				if err == nil {
					t.Fatalf("SelectTarget(%q): want error, got nil", c.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("SelectTarget(%q): unexpected error: %v", c.in, err)
			}

			switch c.kind {
			case "dir":
				dt, ok := target.(*DirTarget)
				if !ok {
					t.Fatalf("SelectTarget(%q): got %T, want *DirTarget", c.in, target)
				}
				if dt.root != c.in {
					t.Errorf("SelectTarget(%q): root = %q, want %q", c.in, dt.root, c.in)
				}
			case "git":
				gt, ok := target.(*GitTarget)
				if !ok {
					t.Fatalf("SelectTarget(%q): got %T, want *GitTarget", c.in, target)
				}
				if gt.url != c.url {
					t.Errorf("SelectTarget(%q): url = %q, want %q", c.in, gt.url, c.url)
				}
				if gt.subpath != c.sub {
					t.Errorf("SelectTarget(%q): subpath = %q, want %q", c.in, gt.subpath, c.sub)
				}
			default:
				t.Fatalf("bad test case kind %q", c.kind)
			}
		})
	}
}
