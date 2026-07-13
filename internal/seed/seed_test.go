package seed

import (
	"regexp"
	"testing"
)

var suffixRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*(:[a-z0-9][a-z0-9-]*|:\*)?$`)

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

func TestLabelsCountIs16(t *testing.T) {
	if len(Labels) != 16 {
		t.Fatalf("seed.Labels has %d entries, want 16", len(Labels))
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

func TestNamespaceDescriptorsSeeded(t *testing.T) {
	want := map[string]string{
		"status:*":   "lifecycle state of a task; exactly one status label should be present",
		"priority:*": "optional urgency ranking; absent means default priority",
		"context:*":  "index tasks whose description is the payload: agent directions, repos, docs, questions",
		"comment:*":  "the kinds of narrative an agent writes on a task",
	}
	have := map[string]Label{}
	for _, l := range Labels {
		have[l.Suffix] = l
	}
	for suffix, desc := range want {
		l, ok := have[suffix]
		if !ok {
			t.Errorf("namespace descriptor %q missing from seed.Labels", suffix)
			continue
		}
		if l.Description != desc {
			t.Errorf("namespace descriptor %q description = %q want %q", suffix, l.Description, desc)
		}
		if l.Expr != "" {
			t.Errorf("namespace descriptor %q must not carry an Expr (implicit), got %q", suffix, l.Expr)
		}
	}
}

func TestTypeNamespaceNotSeeded(t *testing.T) {
	for _, l := range Labels {
		if l.Suffix == "type:*" {
			t.Errorf("type:* must NOT be seeded — type is invented on demand, found %q", l.Suffix)
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
