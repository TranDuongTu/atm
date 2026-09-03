package scrum

import (
	"slices"
	"strings"
	"testing"
)

// implementable sits between planned and implementing: design approval is
// what moves a unit across it, and scrum-coding's dispatch gate reads it.
func TestStagesIncludeImplementableInOrder(t *testing.T) {
	want := []string{StageBrainstormed, StagePlanned, StageImplementable, StageImplementing, StageReview, StageDone}
	if got := Stages(); !slices.Equal(got, want) {
		t.Fatalf("Stages() = %v, want %v", got, want)
	}
	if StageImplementable != "implementable" {
		t.Fatalf("StageImplementable = %q", StageImplementable)
	}
}

// The stage is a real label with a description, or a project seeded before
// this change would carry an undescribed one.
func TestVocabularyDescribesImplementable(t *testing.T) {
	var desc string
	for _, l := range Vocabulary("ATM") {
		if l.Name == "ATM:scrum-stage:"+StageImplementable {
			desc = l.Description
		}
	}
	if desc == "" {
		t.Fatal("no ATM:scrum-stage:implementable label in the vocabulary")
	}
	if !strings.Contains(desc, "approved") {
		t.Fatalf("description = %q; it must say what makes a unit implementable", desc)
	}
}
