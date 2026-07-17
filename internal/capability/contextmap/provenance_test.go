package contextmap

import (
	"testing"
	"time"
)

func TestStampRoundTrip(t *testing.T) {
	want := Stamp{
		Version: StampVersion,
		At:      time.Date(2026, 7, 13, 9, 30, 0, 0, time.UTC),
		Head:    "d1f8cc4",
		Witnesses: []Witness{
			{Source: Source{Kind: KindGit, Locator: "internal/store"}, Value: "a3f9b1"},
			{Source: Source{Kind: KindExternal, Locator: "jira/ATM-441"}, Value: ""},
		},
	}
	body, err := MarshalStamp(want)
	if err != nil {
		t.Fatalf("MarshalStamp: %v", err)
	}
	got, err := UnmarshalStamp(body)
	if err != nil {
		t.Fatalf("UnmarshalStamp: %v", err)
	}
	if !got.At.Equal(want.At) || got.Head != want.Head || got.Version != want.Version {
		t.Errorf("scalar mismatch: got %+v, want %+v", got, want)
	}
	if len(got.Witnesses) != len(want.Witnesses) {
		t.Fatalf("witnesses: got %d, want %d", len(got.Witnesses), len(want.Witnesses))
	}
	for i := range want.Witnesses {
		if got.Witnesses[i] != want.Witnesses[i] {
			t.Errorf("witness %d: got %+v, want %+v", i, got.Witnesses[i], want.Witnesses[i])
		}
	}
}

func TestUnmarshalStampRejectsGarbage(t *testing.T) {
	// A human hand-wrote prose into a provenance comment. Report it as
	// unreadable; never panic, never "repair" it.
	for _, body := range []string{"", "not json", `{"v":99,"sources":[]}`} {
		if _, err := UnmarshalStamp(body); err == nil {
			t.Errorf("UnmarshalStamp(%q): want error, got nil", body)
		}
	}
}
