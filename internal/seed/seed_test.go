package seed

import (
	"regexp"
	"testing"
)

var suffixRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*(:[a-z0-9][a-z0-9-]*)?$`)

func TestLabelsNonEmpty(t *testing.T) {
	if len(Labels) == 0 {
		t.Fatal("seed.Labels is empty")
	}
}

func TestLabelsAllValidSuffixes(t *testing.T) {
	for _, l := range Labels {
		if !suffixRe.MatchString(l.Suffix) {
			t.Errorf("suffix %q does not match the suffix regex", l.Suffix)
		}
	}
}

func TestLabelsNoDuplicates(t *testing.T) {
	seen := map[string]bool{}
	for _, l := range Labels {
		if seen[l.Suffix] {
			t.Errorf("duplicate suffix %q", l.Suffix)
		}
		seen[l.Suffix] = true
	}
}

func TestLabelsDescriptionsNonEmpty(t *testing.T) {
	for _, l := range Labels {
		if l.Description == "" {
			t.Errorf("label %q has empty description (code-of-conduct requires non-empty)", l.Suffix)
		}
	}
}

func TestLabelsCountIs12(t *testing.T) {
	if len(Labels) != 12 {
		t.Fatalf("seed.Labels has %d entries, want 12", len(Labels))
	}
}

func TestContextQuestionLabelPresent(t *testing.T) {
	for _, l := range Labels {
		if l.Suffix == "context:question" {
			if l.Description == "" {
				t.Errorf("context:question has empty description")
			}
			return
		}
	}
	t.Errorf("context:question label not found in seed.Labels")
}

func TestCommentLabelsPresent(t *testing.T) {
	want := []string{"comment:progress", "comment:decision", "comment:open-question"}
	have := map[string]bool{}
	for _, l := range Labels {
		have[l.Suffix] = true
	}
	for _, suffix := range want {
		if !have[suffix] {
			t.Errorf("%q missing from seed.Labels", suffix)
		}
	}
}

func TestDroppedNamespacesAbsent(t *testing.T) {
	// type:*, status:planned/todo/review/archived, context:fixit/stale,
	// priority:medium/low were intentionally removed from the seed.
	dropped := []string{
		"type:bug", "type:feature", "type:task", "type:chore", "type:design",
		"status:planned", "status:todo", "status:review", "status:archived",
		"context:fixit", "context:stale",
		"priority:medium", "priority:low",
	}
	have := map[string]bool{}
	for _, l := range Labels {
		have[l.Suffix] = true
	}
	for _, suffix := range dropped {
		if have[suffix] {
			t.Errorf("dropped label %q still present in seed.Labels", suffix)
		}
	}
}
