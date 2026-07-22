// Package workflowai is the AI-native workflow capability: a
// brainstorm→clarify→plan→ready cycle over the stage:* namespace, task
// links and plan tracking in the capability's metadata key, and boards
// over the stage labels. It coexists with the workflow capability as an
// independent view: disjoint namespaces, no interplay. The store enforces
// nothing; every invariant here is a paved road maintained by the verbs
// (docs/superpowers/specs/2026-07-21-workflow-ai-capability-design.md).
package workflowai

import "strings"

// CapabilityName is the stable identifier: the registry name, the command
// mount, and the task metadata key are all this one string.
const CapabilityName = "workflow_ai"

// StageNamespace is the label namespace the stage ladder lives in.
const StageNamespace = "stage"

// MarkerNamespace holds the capability's marker labels.
const MarkerNamespace = "wfai"

// MarkerRevision is the marker value stamped on revision follow-ups so the
// revisions board can select them; the link itself lives in the payload.
const MarkerRevision = "revision"

// MarkerFramework is the label that carries the project's framework
// conventions in its description (written during a Semantics pass, read at
// Converge step 0). It is a stored label, not a marker stamped on tasks: the
// description is the note, not membership on any task.
const MarkerFramework = "framework"

// Stage values: the ladder new → brainstormed → clarified → planned →
// implementable → done. "New" is the ABSENCE of any stage:* label, not a
// stored label; StageNew is the sentinel guards and reporters use for it.
const (
	StageNew           = ""
	StageBrainstormed  = "brainstormed"
	StageClarified     = "clarified"
	StagePlanned       = "planned"
	StageImplementable = "implementable"
	StageDone          = "done"
)

// Plan locator kinds. Ephemeral is honest: a plan that lives in a
// conversation, unverifiable by construction and always at-risk.
const (
	PlanKindFile      = "file"
	PlanKindCommit    = "commit"
	PlanKindEphemeral = "ephemeral"
)

// firstStageValue returns the task's stage value or StageNew. On a
// hand-edited multi-stage task it reports the lexicographically first
// (the store returns labels sorted); Recorder verbs converge such tasks.
func firstStageValue(labels []string, code string) string {
	prefix := code + ":" + StageNamespace + ":"
	for _, l := range labels {
		if strings.HasPrefix(l, prefix) {
			return strings.TrimPrefix(l, prefix)
		}
	}
	return StageNew
}

func containsString(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
