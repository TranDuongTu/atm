package release

import (
	"fmt"
	"strings"

	"atm/internal/core"
)

// Service is what the recorder needs from the store.
type Service interface {
	core.TaskService
	CreateComment(taskID, body string, labels []string, replyTo, actor string) (*core.Comment, error)
}

// Recorder is the mutating side of the release capability. Its verbs are
// deliberately MECHANICAL: they check that a task exists, is in the same
// project, and is not already a member. What SHOULD go into a release —
// certified originals — is judgment, and judgment lives in the guide and with
// the decider, never in a hidden check here.
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

// Cut creates the release container: an ordinary task whose comment thread is
// the release log, carrying its own version label so search finds it the same
// way it finds everything else.
func (r *Recorder) Cut(code, version string) (*core.Task, error) {
	value, err := SanitizeVersion(version)
	if err != nil {
		return nil, err
	}
	tk, err := r.Store.CreateTask(code, "Release "+strings.TrimSpace(version), "", []string{VersionLabel(code, value)}, r.Actor)
	if err != nil {
		return nil, err
	}
	pl, err := DecodePayload("")
	if err != nil {
		return tk, err
	}
	pl.SetVersion(value)
	if err := r.writePayload(tk.ID, pl); err != nil {
		return tk, fmt.Errorf("created %s, but recording its version failed: %w", tk.ID, err)
	}
	return tk, nil
}

// Include adds a task to a release: the version label on the member, the
// member on the container's roster, the container on the member's payload.
//
// It checks only that the pieces fit together — same project, both tasks
// exist, the container really is one, the member is not already in it. It does
// NOT check that the member is certified. Which work belongs in a release is
// the decider's call, made against this capability's guide; a verb that
// second-guessed it would just be a rule nobody could see.
func (r *Recorder) Include(taskID, containerID string) error {
	code, err := sameProject(taskID, containerID)
	if err != nil {
		return err
	}
	_, cpl, err := r.taskPayload(containerID)
	if err != nil {
		return err
	}
	version := cpl.Version()
	if version == "" {
		return fmt.Errorf("%s is not a release container (cut one with `release cut`)", containerID)
	}
	_, mpl, err := r.taskPayload(taskID)
	if err != nil {
		return err
	}
	if cur := mpl.ReleaseOf(); cur != "" && cur != containerID {
		return fmt.Errorf("%s is already part of release %s (exclude it first)", taskID, cur)
	}
	if containsString(cpl.Members(), taskID) && mpl.ReleaseOf() == containerID {
		return nil
	}
	if err := r.Store.TaskLabelAdd(taskID, VersionLabel(code, version), r.Actor); err != nil {
		return err
	}
	mpl.SetReleaseOf(containerID)
	if err := r.writePayload(taskID, mpl); err != nil {
		return err
	}
	if cpl.AddMember(taskID) {
		return r.writePayload(containerID, cpl)
	}
	return nil
}

// Exclude reverses Include. A shipped release keeps its roster: excluding from
// one would rewrite history rather than correct it.
func (r *Recorder) Exclude(taskID, containerID string) error {
	code, err := sameProject(taskID, containerID)
	if err != nil {
		return err
	}
	container, cpl, err := r.taskPayload(containerID)
	if err != nil {
		return err
	}
	version := cpl.Version()
	if version == "" {
		return fmt.Errorf("%s is not a release container", containerID)
	}
	if containsString(container.Labels, ShippedLabel(code)) {
		return fmt.Errorf("%s has shipped; its roster is history now", containerID)
	}
	_, mpl, err := r.taskPayload(taskID)
	if err != nil {
		return err
	}
	if mpl.ReleaseOf() != containerID && !containsString(cpl.Members(), taskID) {
		return fmt.Errorf("%s is not part of release %s", taskID, containerID)
	}
	if err := r.Store.TaskLabelRemove(taskID, VersionLabel(code, version), r.Actor); err != nil {
		return err
	}
	mpl.ClearReleaseOf()
	if err := r.writePayload(taskID, mpl); err != nil {
		return err
	}
	if cpl.RemoveMember(taskID) {
		return r.writePayload(containerID, cpl)
	}
	return nil
}

// Ship stamps the shipped label on the container and every member, and records
// the ship on the container's comment thread — the release log.
func (r *Recorder) Ship(containerID string) ([]string, error) {
	code, _, ok := core.ParseTaskID(containerID)
	if !ok {
		return nil, fmt.Errorf("invalid task id %q", containerID)
	}
	_, cpl, err := r.taskPayload(containerID)
	if err != nil {
		return nil, err
	}
	version := cpl.Version()
	if version == "" {
		return nil, fmt.Errorf("%s is not a release container", containerID)
	}
	shipped := ShippedLabel(code)
	stamped := make([]string, 0, len(cpl.Members())+1)
	for _, id := range append([]string{containerID}, cpl.Members()...) {
		if _, err := r.Store.GetTask(id); err != nil {
			// A member that no longer exists must not block the ship; the
			// reporter carries it as a dangling roster entry.
			continue
		}
		if err := r.Store.TaskLabelAdd(id, shipped, r.Actor); err != nil {
			return stamped, fmt.Errorf("stamp %s: %w", id, err)
		}
		stamped = append(stamped, id)
	}
	body := fmt.Sprintf("%s: shipped %s with %d task(s)", CapabilityName, version, len(cpl.Members()))
	if _, err := r.Store.CreateComment(containerID, body, nil, "", r.Actor); err != nil {
		return stamped, fmt.Errorf("shipped, but recording the release log entry failed: %w", err)
	}
	return stamped, nil
}

// sameProject validates both IDs and requires one project.
func sameProject(aID, bID string) (code string, err error) {
	aCode, _, ok := core.ParseTaskID(aID)
	if !ok {
		return "", fmt.Errorf("invalid task id %q", aID)
	}
	bCode, _, ok := core.ParseTaskID(bID)
	if !ok {
		return "", fmt.Errorf("invalid task id %q", bID)
	}
	if aCode != bCode {
		return "", fmt.Errorf("cannot include across projects (%s vs %s)", aID, bID)
	}
	if aID == bID {
		return "", fmt.Errorf("a release cannot contain itself")
	}
	return aCode, nil
}
