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

func TestLabelsCountIs22(t *testing.T) {
	if len(Labels) != 22 {
		t.Fatalf("seed.Labels has %d entries, want 22", len(Labels))
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

func TestNewDefaultsPresent(t *testing.T) {
	want := map[string]string{
		"status:planned":  "workflow state: planned; task is scoped and intended but not yet queued for work",
		"status:archived": "workflow state: archived; task is no longer active and kept for historical reference; excluded from active-work filters",
		"type:design":     "task categorization: design; producing a spec or design document",
		"context:stale":   "the task's description references a context resource that has drifted from reality; needs reconciliation or replacement",
	}
	for _, l := range Labels {
		delete(want, l.Suffix)
	}
	for suffix := range want {
		t.Errorf("new default label %q missing from seed.Labels", suffix)
	}
}
