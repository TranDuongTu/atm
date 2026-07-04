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

func TestLabelsCountIs18(t *testing.T) {
	if len(Labels) != 18 {
		t.Fatalf("seed.Labels has %d entries, want 18", len(Labels))
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
