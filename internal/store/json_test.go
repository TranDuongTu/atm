package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMarshalSortedDeterministic(t *testing.T) {
	in := map[string]any{
		"z": "last",
		"a": "first",
		"nested": map[string]any{
			"b": 2,
			"a": 1,
		},
		"arr": []any{3, 1, 2},
	}
	out1, err := MarshalSorted(in)
	if err != nil {
		t.Fatal(err)
	}
	out2, err := MarshalSorted(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(out1) != string(out2) {
		t.Fatalf("non-deterministic output\nfirst: %s\nsecond: %s", out1, out2)
	}
	want := `{
  "a": "first",
  "arr": [
    3,
    1,
    2
  ],
  "nested": {
    "a": 1,
    "b": 2
  },
  "z": "last"
}`
	if string(out1) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out1, want)
	}
}

func TestMarshalSortedKeysSorted(t *testing.T) {
	in := map[string]any{
		"zebra":  1,
		"apple":  2,
		"mango":  3,
		"banana": 4,
	}
	out, err := MarshalSorted(in)
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "apple": 2,
  "banana": 4,
  "mango": 3,
  "zebra": 1
}`
	if string(out) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestMarshalSortedUseNumber(t *testing.T) {
	in := map[string]any{"big": uint64(12345678901234567890)}
	out, err := MarshalSorted(in)
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "big": 12345678901234567890
}`
	if string(out) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "data.json")
	in := map[string]any{"k": "v"}
	if err := WriteFileAtomic(path, in); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := ReadJSON(path, &got); err != nil {
		t.Fatal(err)
	}
	if got["k"] != "v" {
		t.Fatalf("got %v want v", got["k"])
	}
}

func TestWriteFileAtomicReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	_ = os.WriteFile(path, []byte(`{"old": true}`), 0o644)
	if err := WriteFileAtomic(path, map[string]any{"new": true}); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := ReadJSON(path, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["old"]; ok {
		t.Fatal("old key still present")
	}
	if got["new"] != true {
		t.Fatal("new key missing")
	}
}

func TestMarshalSortedRFC3339(t *testing.T) {
	ts := time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC)
	in := map[string]any{"at": RFC3339UTC(ts)}
	out, err := MarshalSorted(in)
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "at": "2026-06-23T10:00:00Z"
}`
	if string(out) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}
