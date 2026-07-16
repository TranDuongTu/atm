package eventsource

import (
	"errors"
	"testing"
)

const digest = "sha256:7f3a2bc4d5e6f708192a3b4c5d6e7f808192a3b4c5d6e7f808192a3b4c5d6e7f"

func TestMintTaskAlias(t *testing.T) {
	none := func(string) bool { return false }
	if got := MintTaskAlias("ATM", digest, none); got != "ATM-7f3a2b" {
		t.Errorf("alias = %q", got)
	}
	// A collision with a held alias extends the prefix to disambiguate.
	taken := func(a string) bool { return a == "ATM-7f3a2b" }
	if got := MintTaskAlias("ATM", digest, taken); got != "ATM-7f3a2bc" {
		t.Errorf("extended alias = %q", got)
	}
}

func TestMintCommentAlias(t *testing.T) {
	none := func(string) bool { return false }
	if got := MintCommentAlias("ATM-0042", digest, none); got != "ATM-0042-c7f3a" {
		t.Errorf("alias = %q", got)
	}
	taken := func(a string) bool { return a == "ATM-0042-c7f3a" }
	if got := MintCommentAlias("ATM-0042", digest, taken); got != "ATM-0042-c7f3a2" {
		t.Errorf("extended alias = %q", got)
	}
}

func resolveState(t *testing.T) (*State, *Event, *Event) {
	t.Helper()
	ca, cb := testClock(1000), testClock(2000)
	// Two tasks holding the SAME alias — legitimately possible after a
	// cross-project merge (L1: aliases need not be unique).
	t1 := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "ATM-0142", "title": "fix cache"})
	t2 := testEvent(t, cb, replicaB, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "ATM-0142", "title": "add export"})
	st, err := FoldEvents([]*Event{t1, t2})
	if err != nil {
		t.Fatal(err)
	}
	return st, t1, t2
}

func TestResolveExactAliasUnique(t *testing.T) {
	ca := testClock(1000)
	e := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "ATM-0001", "title": "t"})
	st, err := FoldEvents([]*Event{e})
	if err != nil {
		t.Fatal(err)
	}
	m, err := st.Resolve("ATM-0001")
	if err != nil || m.ID != e.ID || m.Kind != "task" {
		t.Errorf("Resolve = %+v, %v", m, err)
	}
}

func TestResolveAmbiguousAliasNeverPicksOne(t *testing.T) {
	st, _, _ := resolveState(t)
	_, err := st.Resolve("ATM-0142")
	var amb *AmbiguousError
	if !errors.As(err, &amb) {
		t.Fatalf("err = %v, want *AmbiguousError", err)
	}
	if len(amb.Matches) != 2 {
		t.Errorf("candidates = %+v", amb.Matches)
	}
}

func TestResolveIdentityPrefix(t *testing.T) {
	st, t1, _ := resolveState(t)
	// A unique prefix of the identity hex resolves, git-style, with or
	// without the sha256: prefix.
	hex := t1.ID[len("sha256:"):]
	for _, in := range []string{hex[:12], "sha256:" + hex[:12], t1.ID} {
		m, err := st.Resolve(in)
		if err != nil || m.ID != t1.ID {
			t.Errorf("Resolve(%q) = %+v, %v", in, m, err)
		}
	}
}

func TestResolveNoMatch(t *testing.T) {
	st, _, _ := resolveState(t)
	if _, err := st.Resolve("ATM-9999"); !errors.Is(err, ErrNoMatch) {
		t.Errorf("err = %v, want ErrNoMatch", err)
	}
}

func TestResolveFindsComments(t *testing.T) {
	ca := testClock(1000)
	task := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "ATM-0001", "title": "t"})
	cm := testEvent(t, ca, replicaA, []string{task.ID}, ActionCommentCreated, Subject{Kind: "comment"},
		map[string]any{"alias": "ATM-0001-c0001", "task_ref": task.ID, "body": "b"})
	st, err := FoldEvents([]*Event{task, cm})
	if err != nil {
		t.Fatal(err)
	}
	m, err := st.Resolve("ATM-0001-c0001")
	if err != nil || m.Kind != "comment" || m.ID != cm.ID {
		t.Errorf("Resolve comment = %+v, %v", m, err)
	}
}
