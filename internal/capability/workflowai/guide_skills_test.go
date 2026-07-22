package workflowai

import (
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
	guide := (Cap{}).Guide()
	for _, sec := range []string{"## Semantics", "## Actions", "## Converge"} {
		if !strings.Contains(guide, sec) {
			t.Fatalf("guide missing %s", sec)
		}
	}
}
