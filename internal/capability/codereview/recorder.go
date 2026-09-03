package codereview

import (
	"fmt"
	"strings"

	"atm/internal/core"
)

// Service is what the recorder needs from the store: task reads/writes, the
// capability metadata mutator, and comments for the audit trail.
type Service interface {
	core.TaskService
	CreateComment(taskID, body string, labels []string, replyTo, actor string) (*core.Comment, error)
}

// Recorder is the mutating side of the codereview capability. It maintains the
// single claim axis, the exclusivity of claim and evict, and the PR gate on
// absorb.
type Recorder struct {
	Store Service
	Actor string
}

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

func (r *Recorder) entry(taskID string) (*core.Task, *Payload, string, error) {
	tk, pl, err := r.taskPayload(taskID)
	if err != nil {
		return nil, nil, "", err
	}
	code, _, ok := core.ParseTaskID(taskID)
	if !ok {
		return nil, nil, "", fmt.Errorf("invalid task id %q", taskID)
	}
	return tk, pl, code, nil
}

// labelValues collects the task's labels in one of this capability's
// namespaces: full names and bare values, in store order.
func labelValues(tk *core.Task, code, ns string) (existing []string, vals []string) {
	prefix := code + ":" + ns + ":"
	for _, l := range tk.Labels {
		if !strings.HasPrefix(l, prefix) {
			continue
		}
		existing = append(existing, l)
		vals = append(vals, strings.TrimPrefix(l, prefix))
	}
	return
}

// StateOf reports the task's review state, "" when codereview has not claimed
// it.
func StateOf(tk *core.Task, code string) string {
	_, vals := labelValues(tk, code, ClaimNamespace)
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

// EvictedAs reports the task's evict reason, "" when not evicted.
func EvictedAs(tk *core.Task, code string) string {
	_, vals := labelValues(tk, code, OutNamespace)
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

func (r *Recorder) swapNamespaceLabels(taskID, code, ns, target string) error {
	tk, err := r.Store.GetTask(taskID)
	if err != nil {
		return err
	}
	existing, vals := labelValues(tk, code, ns)
	targetLabel := code + ":" + ns + ":" + target
	if len(existing) == 1 && vals[0] == target {
		return nil
	}
	if !containsString(vals, target) {
		if err := r.Store.TaskLabelAdd(taskID, targetLabel, r.Actor); err != nil {
			return fmt.Errorf("add %s: %w", targetLabel, err)
		}
	}
	for _, l := range existing {
		if l == targetLabel {
			continue
		}
		if err := r.Store.TaskLabelRemove(taskID, l, r.Actor); err != nil {
			return fmt.Errorf("remove %s: %w", l, err)
		}
	}
	return nil
}

func (r *Recorder) clearNamespace(taskID, code, ns string) error {
	tk, err := r.Store.GetTask(taskID)
	if err != nil {
		return err
	}
	existing, _ := labelValues(tk, code, ns)
	for _, l := range existing {
		if err := r.Store.TaskLabelRemove(taskID, l, r.Actor); err != nil {
			return fmt.Errorf("remove %s: %w", l, err)
		}
	}
	return nil
}

func validReason(s string) bool { return containsString(OutReasons(), s) }

// Absorb schedules a review, and REQUIRES the pull request up front. That is
// the whole gate: a task whose PR nobody could find is left in the inbox,
// where the swelling count is the warning. There is no other warning
// mechanism, and absorb is never called without a PR to make one.
func (r *Recorder) Absorb(taskID, pr string) error {
	if strings.TrimSpace(pr) == "" {
		return fmt.Errorf("absorb requires --pr: a task with no discoverable pull request stays in the inbox, which is the warning surface")
	}
	tk, pl, code, err := r.entry(taskID)
	if err != nil {
		return err
	}
	if reason := EvictedAs(tk, code); reason != "" {
		return fmt.Errorf("%s was evicted from codereview (%s); release it first", taskID, reason)
	}
	pl.SetPR(pr)
	if err := r.writePayload(taskID, pl); err != nil {
		return err
	}
	return r.swapNamespaceLabels(taskID, code, ClaimNamespace, StateScheduled)
}

// Begin moves a scheduled review to under way. The review conversation itself
// lives on the pull request; this only says someone picked it up.
func (r *Recorder) Begin(taskID string) error {
	tk, _, code, err := r.entry(taskID)
	if err != nil {
		return err
	}
	switch StateOf(tk, code) {
	case "":
		return fmt.Errorf("%s is not claimed by codereview (absorb it first)", taskID)
	case StateReviewing:
		return nil
	case StateDone:
		return fmt.Errorf("%s is already reviewed; release it to review again", taskID)
	}
	return r.swapNamespaceLabels(taskID, code, ClaimNamespace, StateReviewing)
}

// Finish stamps the finish socket and, optionally, records where the report
// lives. The verdict is the label; the substance is on the PR.
func (r *Recorder) Finish(taskID, report string) error {
	tk, pl, code, err := r.entry(taskID)
	if err != nil {
		return err
	}
	if StateOf(tk, code) == "" {
		return fmt.Errorf("%s is not claimed by codereview (absorb it first)", taskID)
	}
	if report != "" {
		pl.SetReport(report)
		if err := r.writePayload(taskID, pl); err != nil {
			return err
		}
	}
	return r.swapNamespaceLabels(taskID, code, ClaimNamespace, StateDone)
}

// Evict settles a task out of codereview with a reason.
// FollowUp creates a tracked item a review left behind, born into the
// pipeline beneath the review, and records the edge at both ends: part_of on
// the item, the roster on the review.
//
// Unlike a qa scaffold, an open follow-up does NOT block its parent from
// finishing. A verification is incomplete while a scaffold is unrun, but a
// review is the opposite case: the whole point of a tracked item is that a
// finding worth fixing need not hold the review open, or the review becomes
// the endless cycle the item exists to break.
func (r *Recorder) FollowUp(reviewID, title string) (*core.Task, error) {
	if strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("follow-up requires --title")
	}
	review, pl, code, err := r.entry(reviewID)
	if err != nil {
		return nil, err
	}
	if pl.PartOf() != "" {
		return nil, fmt.Errorf("%s is itself a follow-up of %s — follow-ups do not nest", reviewID, pl.PartOf())
	}
	if StateOf(review, code) == "" {
		return nil, fmt.Errorf("%s is not claimed by codereview (absorb it first)", reviewID)
	}
	item, err := r.Store.CreateTask(code, title, "", []string{code + ":" + ClaimNamespace + ":" + StateScheduled}, r.Actor)
	if err != nil {
		return nil, err
	}
	itemPl, err := DecodePayload("")
	if err != nil {
		return item, err
	}
	itemPl.SetPartOf(reviewID)
	if err := r.writePayload(item.ID, itemPl); err != nil {
		return item, fmt.Errorf("created %s, but recording its review failed: %w", item.ID, err)
	}
	if pl.AddFollowUp(item.ID) {
		if err := r.writePayload(reviewID, pl); err != nil {
			return item, fmt.Errorf("created %s, but adding it to %s's roster failed: %w", item.ID, reviewID, err)
		}
	}
	return item, nil
}

func (r *Recorder) Evict(taskID, reason string) error {
	if reason == "" {
		reason = OutNotWarranted
	}
	if !validReason(reason) {
		return fmt.Errorf("invalid evict reason %q (want one of %s)", reason, strings.Join(OutReasons(), ", "))
	}
	_, _, code, err := r.entry(taskID)
	if err != nil {
		return err
	}
	if err := r.swapNamespaceLabels(taskID, code, OutNamespace, reason); err != nil {
		return err
	}
	return r.clearNamespace(taskID, code, ClaimNamespace)
}

// Release withdraws codereview's perspective entirely: every label it owns
// goes, its whole payload goes, and the reason is recorded as a comment. It is
// the DOWNSTREAM half of the manager's re-review pair — when upstream reopens
// the work, this is what lets it come back for a fresh triage.
func (r *Recorder) Release(taskID, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("release requires --reason")
	}
	tk, pl, code, err := r.entry(taskID)
	if err != nil {
		return err
	}
	if StateOf(tk, code) == "" && EvictedAs(tk, code) == "" {
		return fmt.Errorf("codereview has not decided about %s — nothing to release", taskID)
	}
	pl.ClearPR()
	pl.ClearReport()
	if err := r.writePayload(taskID, pl); err != nil {
		return err
	}
	for _, ns := range []string{ClaimNamespace, OutNamespace} {
		if err := r.clearNamespace(taskID, code, ns); err != nil {
			return err
		}
	}
	if _, err := r.Store.CreateComment(taskID, CapabilityName+": released to the pool — "+reason, nil, "", r.Actor); err != nil {
		return fmt.Errorf("released, but recording the reason comment failed: %w", err)
	}
	return nil
}
