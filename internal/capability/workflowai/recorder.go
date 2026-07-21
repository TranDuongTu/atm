package workflowai

import (
	"fmt"
	"strings"
	"time"

	"atm/internal/core"
)

// Service is what the recorder needs from the store: task reads/writes,
// the capability metadata mutator (ATM-2e64a5), and comments for the
// demote audit trail. core.Service and *store.Store both satisfy it.
type Service interface {
	core.TaskService
	CreateComment(taskID, body string, labels []string, replyTo, actor string) (*core.Comment, error)
}

// Recorder is the mutating side of the workflow_ai capability. It maintains
// the exactly-one-stage invariant and the payload's plan/link/demotion
// state; the store itself enforces nothing (paved road, not a fence).
type Recorder struct {
	Store Service
	Actor string
	// Now overrides the timestamp source in tests; nil means time.Now.
	Now func() time.Time
}

func (r *Recorder) now() string {
	f := time.Now
	if r.Now != nil {
		f = r.Now
	}
	return f().UTC().Format(time.RFC3339)
}

// taskPayload reads the task and decodes this capability's payload. A
// malformed payload is an error — verbs never overwrite state they cannot
// read (hand-repair via the raw metadata surface instead).
func (r *Recorder) taskPayload(taskID string) (*core.Task, *Payload, error) {
	tk, err := r.Store.GetTask(taskID)
	if err != nil {
		return nil, nil, err
	}
	pl, err := DecodePayload(tk.Meta[CapabilityName])
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", taskID, err)
	}
	return tk, pl, nil
}

func (r *Recorder) writePayload(taskID string, pl *Payload) error {
	s, err := pl.Encode()
	if err != nil {
		return err
	}
	return r.Store.SetTaskCapabilityMeta(taskID, CapabilityName, s, r.Actor)
}

// stageState collects the task's stage labels: full names, values, whether
// target is among them, and the prior value for reporting (first non-target,
// lexicographic — the store returns labels sorted).
func stageState(tk *core.Task, code, target string) (existing []string, vals []string, hasTarget bool, prior string) {
	prefix := code + ":" + StageNamespace + ":"
	prior = StageNew
	for _, l := range tk.Labels {
		if !strings.HasPrefix(l, prefix) {
			continue
		}
		existing = append(existing, l)
		v := strings.TrimPrefix(l, prefix)
		vals = append(vals, v)
		if v == target {
			hasTarget = true
		} else if prior == StageNew {
			prior = v
		}
	}
	return
}

// setStage performs the guarded stage swap for verb: the transition to
// target is allowed only from the stages in from (StageNew means "no stage
// label"). Already exactly at target: idempotent no-op, zero store calls,
// prior == target. On a hand-edited multi-stage task whose set contains an
// allowed from-stage, the swap proceeds and self-heals: add target first,
// then remove every other stage label (no transactions; add-first bounds
// the worst case to a recoverable extra label, re-running converges).
func (r *Recorder) setStage(taskID, verb, target string, from ...string) (string, error) {
	tk, err := r.Store.GetTask(taskID)
	if err != nil {
		return "", err
	}
	code, _, ok := core.ParseTaskID(taskID)
	if !ok {
		return "", fmt.Errorf("invalid task id %q", taskID)
	}
	existing, vals, hasTarget, prior := stageState(tk, code, target)
	if len(existing) == 1 && hasTarget {
		return target, nil
	}
	allowed := false
	for _, f := range from {
		if f == StageNew {
			if len(existing) == 0 {
				allowed = true
			}
		} else if containsString(vals, f) {
			allowed = true
		}
	}
	if !allowed {
		current := "new"
		if len(vals) > 0 {
			current = strings.Join(vals, ", ")
		}
		return prior, fmt.Errorf("cannot %s %s: stage is %s (%s requires %s)", verb, taskID, current, verb, fromWords(from))
	}
	targetLabel := code + ":" + StageNamespace + ":" + target
	if !hasTarget {
		if err := r.Store.TaskLabelAdd(taskID, targetLabel, r.Actor); err != nil {
			return prior, fmt.Errorf("add %s: %w", targetLabel, err)
		}
	}
	for _, l := range existing {
		if l == targetLabel {
			continue
		}
		if err := r.Store.TaskLabelRemove(taskID, l, r.Actor); err != nil {
			return prior, fmt.Errorf("remove %s: %w", l, err)
		}
	}
	return prior, nil
}

func fromWords(from []string) string {
	out := make([]string, len(from))
	for i, f := range from {
		if f == StageNew {
			out[i] = "a new task"
		} else {
			out[i] = f
		}
	}
	return strings.Join(out, " or ")
}

// Brainstorm marks the idea explored: new → brainstormed.
func (r *Recorder) Brainstorm(taskID string) (string, error) {
	return r.setStage(taskID, "brainstorm", StageBrainstormed, StageNew)
}

// Clarify marks scope and success criteria settled: brainstormed → clarified.
func (r *Recorder) Clarify(taskID string) (string, error) {
	return r.setStage(taskID, "clarify", StageClarified, StageBrainstormed)
}

// Plan records the plan locator and, from clarified, advances to planned.
// From planned/implementable it UPDATES the locator in place (stage
// untouched) — a moved plan file or a re-planning pass. The payload is
// written before the label swap: a planned task must never lack a plan
// record; the recoverable direction is a leftover record on a still-
// clarified task. Returns the prior stage (== current stage on update).
func (r *Recorder) Plan(taskID, kind, ref string) (string, error) {
	switch kind {
	case PlanKindFile, PlanKindCommit, PlanKindEphemeral:
	default:
		return "", fmt.Errorf("invalid plan kind %q (want %s, %s or %s)", kind, PlanKindFile, PlanKindCommit, PlanKindEphemeral)
	}
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("plan requires a non-empty --ref")
	}
	tk, pl, err := r.taskPayload(taskID)
	if err != nil {
		return "", err
	}
	code, _, ok := core.ParseTaskID(taskID)
	if !ok {
		return "", fmt.Errorf("invalid task id %q", taskID)
	}
	_, vals, _, _ := stageState(tk, code, StagePlanned)
	update := containsString(vals, StagePlanned) || containsString(vals, StageImplementable)
	if !update && !containsString(vals, StageClarified) {
		current := "new"
		if len(vals) > 0 {
			current = strings.Join(vals, ", ")
		}
		return "", fmt.Errorf("cannot plan %s: stage is %s (plan requires clarified, or planned/implementable to update the locator)", taskID, current)
	}
	pl.SetPlan(PlanRecord{Kind: kind, Ref: ref, RecordedAt: r.now(), Actor: r.Actor})
	if err := r.writePayload(taskID, pl); err != nil {
		return "", err
	}
	if update {
		return firstStageValue(tk.Labels, code), nil
	}
	return r.setStage(taskID, "plan", StagePlanned, StageClarified)
}

// Ready clears the task for implementation: planned → implementable. Guard:
// a plan record must exist — "never implement an unplanned task" starts here.
func (r *Recorder) Ready(taskID string) (string, error) {
	_, pl, err := r.taskPayload(taskID)
	if err != nil {
		return "", err
	}
	if pl.Plan() == nil {
		return "", fmt.Errorf("cannot ready %s: no plan recorded (run `plan` first)", taskID)
	}
	return r.setStage(taskID, "ready", StageImplementable, StagePlanned)
}

// Done closes the cycle: implementable → done.
func (r *Recorder) Done(taskID string) (string, error) {
	return r.setStage(taskID, "done", StageDone, StageImplementable)
}

// Demote resets the task to new from any stage: clears the stage label(s)
// and the plan record, writes the demoted breadcrumb, and appends the
// reason as a task comment (audit trail). Links and the revision marker
// survive — topology is true regardless of stage. A task already new with
// no plan record is a pure no-op. Payload is written first, labels second,
// comment last: every partial-failure state converges on re-run.
func (r *Recorder) Demote(taskID, reason string) (string, error) {
	if strings.TrimSpace(reason) == "" {
		return "", fmt.Errorf("demote requires --reason")
	}
	tk, pl, err := r.taskPayload(taskID)
	if err != nil {
		return "", err
	}
	code, _, ok := core.ParseTaskID(taskID)
	if !ok {
		return "", fmt.Errorf("invalid task id %q", taskID)
	}
	existing, _, _, _ := stageState(tk, code, "\x00none")
	prior := firstStageValue(tk.Labels, code)
	if len(existing) == 0 && pl.Plan() == nil {
		return StageNew, nil
	}
	pl.ClearPlan()
	pl.SetDemoted(Demotion{At: r.now(), By: r.Actor, Reason: reason})
	if err := r.writePayload(taskID, pl); err != nil {
		return prior, err
	}
	for _, l := range existing {
		if err := r.Store.TaskLabelRemove(taskID, l, r.Actor); err != nil {
			return prior, fmt.Errorf("remove %s: %w", l, err)
		}
	}
	if _, err := r.Store.CreateComment(taskID, "workflow_ai: demoted to new — "+reason, nil, "", r.Actor); err != nil {
		return prior, fmt.Errorf("demoted, but recording the reason comment failed: %w", err)
	}
	return prior, nil
}
