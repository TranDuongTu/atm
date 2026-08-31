package checklist

import (
	"slices"
	"strings"
	"testing"

	"atm/skills"
)

// The skills file's frontmatter labels must agree with the vocabulary the
// package actually manages, so documentation and code cannot drift.
func TestSkillsFileMatchesVocabulary(t *testing.T) {
	spec := skills.MustCapability(Cap{}.Name())
	if spec.Description != (Cap{}).Summary() {
		t.Fatalf("Summary() %q != frontmatter description %q", (Cap{}).Summary(), spec.Description)
	}
	if spec.Brief == "" || (Cap{}).Brief() != spec.Brief {
		t.Fatalf("checklist must ship a frontmatter brief; Brief() = %q", (Cap{}).Brief())
	}
	guide := (Cap{}).Guide()
	for _, sec := range []string{"## Semantics", "## Actions", "## Converge"} {
		if !strings.Contains(guide, sec) {
			t.Fatalf("guide missing %s", sec)
		}
	}
	if got, want := spec.Labels, []string{"checklist", "checklist:*"}; !slices.Equal(got, want) {
		t.Fatalf("frontmatter labels %v, want %v", got, want)
	}
	if got, want := spec.Boards, []string{"checklists"}; !slices.Equal(got, want) {
		t.Fatalf("frontmatter boards %v, want %v", got, want)
	}
}
