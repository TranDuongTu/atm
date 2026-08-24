package scrum

import (
	"fmt"
	"strings"

	"atm/internal/core"
)

// Service is what the recorder needs from the store: task reads/writes, the
// capability metadata mutator, and comments for the release/reopen audit
// trail. core.Service and *store.Store both satisfy it.
type Service interface {
	core.TaskService
	CreateComment(taskID, body string, labels []string, replyTo, actor string) (*core.Comment, error)
}

// Recorder is the mutating side of the scrum capability. It maintains the
// exactly-one-type invariant, the single stage axis, the exclusivity of claim
// and evict, and the private topology payload; the store itself enforces
// nothing (paved road, not a fence).
type Recorder struct {
	Store Service
	Actor string
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

// entry is the common prologue of every verb: the task exists, its payload is
// readable, and the project code is known.
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
// namespaces: full names and bare values, in store order (sorted).
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

// TypeOf reports the task's scrum type, "" when scrum has not claimed it.
func TypeOf(tk *core.Task, code string) string {
	_, vals := labelValues(tk, code, TypeNamespace)
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

// StageOf reports the task's scrum stage, "" when none is stamped.
func StageOf(tk *core.Task, code string) string {
	_, vals := labelValues(tk, code, StageNamespace)
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

// EvictedAs reports the task's evict reason, "" when scrum has not evicted it.
func EvictedAs(tk *core.Task, code string) string {
	_, vals := labelValues(tk, code, OutNamespace)
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

// swapNamespaceLabels makes target the only label in ns. Add-first, then
// remove the rest: no transactions here, so the worst case is a recoverable
// extra label and re-running converges.
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

// clearNamespace removes every label this capability owns in ns.
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

func validType(t string) bool   { return containsString(Types(), t) }
func validStage(s string) bool  { return containsString(Stages(), s) }
func validReason(s string) bool { return containsString(OutReasons(), s) }

// Absorb claims a task out of the inbox: it stamps the type and, optionally,
// the stage it is already at. Absorbing straight to done is deliberate — it is
// how existing, already-finished work is read into scrum without pretending it
// still has to be built.
//
// An evicted task is refused: scrum settled it once, and un-settling is the
// release verb, which leaves an audit comment.
func (r *Recorder) Absorb(taskID, typ, stage string) error {
	if !validType(typ) {
		return fmt.Errorf("invalid type %q (want one of %s)", typ, strings.Join(Types(), ", "))
	}
	if stage != "" && !validStage(stage) {
		return fmt.Errorf("invalid stage %q (want one of %s)", stage, strings.Join(Stages(), ", "))
	}
	tk, _, code, err := r.entry(taskID)
	if err != nil {
		return err
	}
	if reason := EvictedAs(tk, code); reason != "" {
		return fmt.Errorf("%s was evicted from scrum (%s); release it first", taskID, reason)
	}
	if err := r.swapNamespaceLabels(taskID, code, TypeNamespace, typ); err != nil {
		return err
	}
	if stage == "" {
		return nil
	}
	return r.swapNamespaceLabels(taskID, code, StageNamespace, stage)
}

// Add creates a child born into scrum's pipeline — the decomposition verb.
// The parent, when given, must already be claimed by scrum: a born-claimed
// child hanging off unclaimed work would be topology nobody owns.
func (r *Recorder) Add(code, title, typ, partOf, stage string) (*core.Task, error) {
	if !validType(typ) {
		return nil, fmt.Errorf("invalid type %q (want one of %s)", typ, strings.Join(Types(), ", "))
	}
	if stage != "" && !validStage(stage) {
		return nil, fmt.Errorf("invalid stage %q (want one of %s)", stage, strings.Join(Stages(), ", "))
	}
	if strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("add requires --title")
	}
	if partOf != "" {
		parentCode, _, ok := core.ParseTaskID(partOf)
		if !ok {
			return nil, fmt.Errorf("invalid task id %q", partOf)
		}
		if parentCode != code {
			return nil, fmt.Errorf("cannot parent %s under %s (different projects)", code, partOf)
		}
		parent, err := r.Store.GetTask(partOf)
		if err != nil {
			return nil, fmt.Errorf("parent %s: %w", partOf, err)
		}
		if TypeOf(parent, code) == "" {
			return nil, fmt.Errorf("parent %s is not claimed by scrum (absorb it first)", partOf)
		}
	}
	labels := []string{code + ":" + TypeNamespace + ":" + typ}
	if stage != "" {
		labels = append(labels, code+":"+StageNamespace+":"+stage)
	}
	tk, err := r.Store.CreateTask(code, title, "", labels, r.Actor)
	if err != nil {
		return nil, err
	}
	if partOf == "" {
		return tk, nil
	}
	if err := r.SetPartOf(tk.ID, partOf); err != nil {
		return tk, fmt.Errorf("created %s, but recording its parent failed: %w", tk.ID, err)
	}
	return tk, nil
}

// Stage moves a claimed unit along the working axis. Stamping done is the
// finish socket, so it is the one transition with a convergence rule: a story
// or an epic is done only when every live child is. The check is verb-side,
// never store-side — a hand-assigned label can still say anything.
func (r *Recorder) Stage(taskID, stage string) error {
	if !validStage(stage) {
		return fmt.Errorf("invalid stage %q (want one of %s)", stage, strings.Join(Stages(), ", "))
	}
	tk, _, code, err := r.entry(taskID)
	if err != nil {
		return err
	}
	typ := TypeOf(tk, code)
	if typ == "" {
		return fmt.Errorf("%s is not claimed by scrum (absorb it first)", taskID)
	}
	if stage == StageDone && (typ == TypeStory || typ == TypeEpic) {
		undone, err := r.undoneChildren(code, taskID)
		if err != nil {
			return err
		}
		if len(undone) > 0 {
			return fmt.Errorf("%s is a %s whose children are not all done: %s", taskID, typ, strings.Join(undone, ", "))
		}
	}
	return r.swapNamespaceLabels(taskID, code, StageNamespace, stage)
}

// undoneChildren lists the live children of parentID that are not stamped
// done. Children are DERIVED: only the child stores the edge (part_of), so the
// roster is a project scan. Evicted children do not count — scrum settled them
// and they are no longer part of what the parent has to deliver.
func (r *Recorder) undoneChildren(code, parentID string) ([]string, error) {
	tasks, err := r.Store.ListTasksErr(core.QueryFilters{Project: code})
	if err != nil {
		return nil, err
	}
	var undone []string
	for _, t := range tasks {
		if t.ID == parentID {
			continue
		}
		pl, err := DecodePayload(t.Meta[CapabilityName])
		if err != nil {
			// An unreadable child is not evidence of convergence.
			undone = append(undone, t.ID+" (unreadable scrum payload)")
			continue
		}
		if pl.PartOf() != parentID {
			continue
		}
		if EvictedAs(t, code) != "" {
			continue
		}
		if StageOf(t, code) != StageDone {
			undone = append(undone, t.ID)
		}
	}
	return undone, nil
}

// Evict settles a task out of scrum with a reason: the claim axis and the
// stage axis go, the evict axis arrives. The two are kept mutually exclusive
// here so a task never stands in two lanes at once.
func (r *Recorder) Evict(taskID, reason, coveredBy string) error {
	if reason == "" {
		reason = OutNotWorthIt
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
		if err := r.SetCoveredBy(taskID, coveredBy); err != nil {
			return err
		}
	} else if pl.CoveredBy() != "" {
		if err := r.ClearCoveredBy(taskID, ""); err != nil {
			return err
		}
	}
	if err := r.swapNamespaceLabels(taskID, code, OutNamespace, reason); err != nil {
		return err
	}
	for _, ns := range []string{TypeNamespace, StageNamespace} {
		if err := r.clearNamespace(taskID, code, ns); err != nil {
			return err
		}
	}
	return nil
}

// Release withdraws scrum's perspective entirely: every label it owns goes,
// its whole payload goes, and the reason is recorded as a comment. The task
// returns to the pool and reappears in scrum's inbox whenever it is eligible
// again. Labels and metadata owned by other capabilities are untouched.
func (r *Recorder) Release(taskID, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("release requires --reason")
	}
	tk, pl, code, err := r.entry(taskID)
	if err != nil {
		return err
	}
	if TypeOf(tk, code) == "" && EvictedAs(tk, code) == "" {
		return fmt.Errorf("scrum has not decided about %s — nothing to release", taskID)
	}
	pl.ClearPartOf()
	pl.ClearCoveredBy()
	for _, id := range pl.DependsOn() {
		pl.RemoveDependsOn(id)
	}
	pl.ClearSpec()
	pl.ClearPlan()
	if err := r.writePayload(taskID, pl); err != nil {
		return err
	}
	for _, ns := range []string{TypeNamespace, StageNamespace, OutNamespace} {
		if err := r.clearNamespace(taskID, code, ns); err != nil {
			return err
		}
	}
	if _, err := r.Store.CreateComment(taskID, CapabilityName+": released to the pool — "+reason, nil, "", r.Actor); err != nil {
		return fmt.Errorf("released, but recording the reason comment failed: %w", err)
	}
	return nil
}

// Reopen un-finishes a unit: done becomes implementing, with the reason on the
// record. It is the UPSTREAM half of the manager's backward-flow pair — the
// downstream capability's own release verb is the other half, and the two are
// never composed into one cross-capability verb.
func (r *Recorder) Reopen(taskID, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("reopen requires --reason")
	}
	tk, _, code, err := r.entry(taskID)
	if err != nil {
		return err
	}
	if StageOf(tk, code) != StageDone {
		return fmt.Errorf("%s is not at stage %s — nothing to reopen", taskID, StageDone)
	}
	if err := r.swapNamespaceLabels(taskID, code, StageNamespace, StageImplementing); err != nil {
		return err
	}
	if _, err := r.Store.CreateComment(taskID, CapabilityName+": reopened — "+reason, nil, "", r.Actor); err != nil {
		return fmt.Errorf("reopened, but recording the reason comment failed: %w", err)
	}
	return nil
}

// SetSpec records the repo-relative spec locator for a unit.
func (r *Recorder) SetSpec(taskID, path string) error { return r.setLocator(taskID, "spec", path) }

// SetPlan records the repo-relative implementation-plan locator for a unit.
func (r *Recorder) SetPlan(taskID, path string) error { return r.setLocator(taskID, "plan", path) }

func (r *Recorder) setLocator(taskID, key, path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("%s requires --path", key)
	}
	_, pl, _, err := r.entry(taskID)
	if err != nil {
		return err
	}
	switch key {
	case "spec":
		pl.SetSpec(path)
	case "plan":
		pl.SetPlan(path)
	}
	return r.writePayload(taskID, pl)
}
