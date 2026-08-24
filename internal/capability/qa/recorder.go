package qa

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

// Recorder is the mutating side of the qa capability. It maintains the single
// claim axis, the exclusivity of claim and evict, and — the one guarantee
// downstream depends on — that qa:done never lands on a test scaffold.
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

// StateOf reports the task's qa state, "" when qa has not claimed it.
func StateOf(tk *core.Task, code string) string {
	_, vals := labelValues(tk, code, ClaimNamespace)
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

// EvictedAs reports the task's evict reason, "" when qa has not evicted it.
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

// Absorb claims an inbox task as an ORIGINAL under verification. Scaffolds are
// born claimed, so absorbing one is refused: it would blur the one distinction
// the finish guarantee rests on.
func (r *Recorder) Absorb(taskID string) error {
	tk, pl, code, err := r.entry(taskID)
	if err != nil {
		return err
	}
	if reason := EvictedAs(tk, code); reason != "" {
		return fmt.Errorf("%s was evicted from qa (%s); release it first", taskID, reason)
	}
	if pl.PartOf() != "" {
		return fmt.Errorf("%s is a test scaffold of %s — scaffolds are born claimed, not absorbed", taskID, pl.PartOf())
	}
	return r.swapNamespaceLabels(taskID, code, ClaimNamespace, StateTesting)
}

// Scaffold creates a test scaffold born into qa's pipeline beneath an
// original, and records the edge at both ends: part_of on the scaffold,
// the roster on the original. Scaffolds do not nest.
func (r *Recorder) Scaffold(originalID, title string) (*core.Task, error) {
	if strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("scaffold requires --title")
	}
	orig, pl, code, err := r.entry(originalID)
	if err != nil {
		return nil, err
	}
	if pl.PartOf() != "" {
		return nil, fmt.Errorf("%s is itself a scaffold of %s — scaffolds do not nest", originalID, pl.PartOf())
	}
	if StateOf(orig, code) == "" {
		return nil, fmt.Errorf("%s is not claimed by qa (absorb it first)", originalID)
	}
	sc, err := r.Store.CreateTask(code, title, "", []string{code + ":" + ClaimNamespace + ":" + StateTesting}, r.Actor)
	if err != nil {
		return nil, err
	}
	scPl, err := DecodePayload("")
	if err != nil {
		return sc, err
	}
	scPl.SetPartOf(originalID)
	if err := r.writePayload(sc.ID, scPl); err != nil {
		return sc, fmt.Errorf("created %s, but recording its original failed: %w", sc.ID, err)
	}
	if pl.AddScaffold(sc.ID) {
		if err := r.writePayload(originalID, pl); err != nil {
			return sc, fmt.Errorf("created %s, but adding it to %s's roster failed: %w", sc.ID, originalID, err)
		}
	}
	return sc, nil
}

// Pass records successful verification. What that means depends on which side
// of the scaffold edge the task sits:
//
//   - A SCAFFOLD passing simply gives up its claim; it keeps part_of so the
//     history stays readable. It is never stamped done.
//   - An ORIGINAL passing is the finish socket, and it is refused while any of
//     its scaffolds is still claimed. The refusal names them.
//
// qa:done therefore only ever lands on absorbed originals — the guarantee
// downstream capabilities and release selection both rely on.
func (r *Recorder) Pass(taskID string) error {
	tk, pl, code, err := r.entry(taskID)
	if err != nil {
		return err
	}
	if reason := EvictedAs(tk, code); reason != "" {
		return fmt.Errorf("%s was evicted from qa (%s); release it first", taskID, reason)
	}
	if StateOf(tk, code) == "" {
		return fmt.Errorf("%s is not claimed by qa (absorb it first)", taskID)
	}
	if pl.PartOf() != "" {
		if err := r.clearNamespace(taskID, code, ClaimNamespace); err != nil {
			return err
		}
		if _, err := r.Store.CreateComment(taskID, CapabilityName+": scaffold passed", nil, "", r.Actor); err != nil {
			return fmt.Errorf("passed, but recording the comment failed: %w", err)
		}
		return nil
	}
	live, err := r.liveScaffolds(code, pl)
	if err != nil {
		return err
	}
	if len(live) > 0 {
		return fmt.Errorf("%s still has scaffolds under test: %s", taskID, strings.Join(live, ", "))
	}
	return r.swapNamespaceLabels(taskID, code, ClaimNamespace, StateDone)
}

// liveScaffolds lists the original's scaffolds that have not given up their
// claim. A scaffold that no longer exists is not a reason to block the
// original — the reporter surfaces it as a finding instead.
func (r *Recorder) liveScaffolds(code string, pl *Payload) ([]string, error) {
	var live []string
	for _, id := range pl.Scaffolds() {
		sc, err := r.Store.GetTask(id)
		if err != nil {
			continue
		}
		if EvictedAs(sc, code) != "" {
			continue
		}
		if StateOf(sc, code) != "" {
			live = append(live, id)
		}
	}
	return live, nil
}

// Evict settles a task out of qa with a reason. A `failed` eviction is a
// verdict, not a shrug: it is the backward-flow signal the manager routes,
// usually into the upstream reopen plus this capability's release.
func (r *Recorder) Evict(taskID, reason, coveredBy string) error {
	if reason == "" {
		reason = OutNotRelevant
	}
	if !validReason(reason) {
		return fmt.Errorf("invalid evict reason %q (want one of %s)", reason, strings.Join(OutReasons(), ", "))
	}
	if reason == OutCoveredBy && coveredBy == "" {
		return fmt.Errorf("evict reason %s requires --covered-by", OutCoveredBy)
	}
	_, pl, code, err := r.entry(taskID)
	if err != nil {
		return err
	}
	if coveredBy != "" {
		if err := r.setCoveredBy(taskID, coveredBy); err != nil {
			return err
		}
	} else if pl.CoveredBy() != "" {
		pl.ClearCoveredBy()
		if err := r.writePayload(taskID, pl); err != nil {
			return err
		}
	}
	if err := r.swapNamespaceLabels(taskID, code, OutNamespace, reason); err != nil {
		return err
	}
	return r.clearNamespace(taskID, code, ClaimNamespace)
}

func (r *Recorder) setCoveredBy(taskID, otherID string) error {
	aCode, _, ok := core.ParseTaskID(taskID)
	if !ok {
		return fmt.Errorf("invalid task id %q", taskID)
	}
	bCode, _, ok := core.ParseTaskID(otherID)
	if !ok {
		return fmt.Errorf("invalid task id %q", otherID)
	}
	if aCode != bCode {
		return fmt.Errorf("cannot link across projects (%s vs %s)", taskID, otherID)
	}
	if taskID == otherID {
		return fmt.Errorf("cannot link %s to itself", taskID)
	}
	if _, err := r.Store.GetTask(otherID); err != nil {
		return err
	}
	_, pl, err := r.taskPayload(taskID)
	if err != nil {
		return err
	}
	if pl.CoveredBy() == otherID {
		return nil
	}
	pl.SetCoveredBy(otherID)
	return r.writePayload(taskID, pl)
}

// Release withdraws qa's perspective entirely: every label it owns goes, its
// whole payload goes, and the reason is recorded as a comment. It is the
// DOWNSTREAM half of the manager's re-review pair.
func (r *Recorder) Release(taskID, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("release requires --reason")
	}
	tk, pl, code, err := r.entry(taskID)
	if err != nil {
		return err
	}
	if StateOf(tk, code) == "" && EvictedAs(tk, code) == "" {
		return fmt.Errorf("qa has not decided about %s — nothing to release", taskID)
	}
	pl.ClearPartOf()
	pl.ClearCoveredBy()
	for _, id := range pl.Scaffolds() {
		pl.RemoveScaffold(id)
	}
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
