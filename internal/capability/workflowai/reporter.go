package workflowai

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"atm/internal/core"
)

// Reporter is the read-only side of the workflow_ai capability. It never
// mutates the store — the reporter reports, the decider demotes.
type Reporter struct {
	Store core.TaskService
}

// Stage returns the task's stage value or StageNew ("") when the task
// carries no stage:* label. On a hand-edited multi-stage task it reports
// the lexicographically first value (store labels sort); Recorder verbs
// converge such tasks.
func (r *Reporter) Stage(taskID string) (string, error) {
	tk, err := r.Store.GetTask(taskID)
	if err != nil {
		return "", err
	}
	code, _, ok := core.ParseTaskID(taskID)
	if !ok {
		return "", fmt.Errorf("invalid task id %q", taskID)
	}
	return firstStageValue(tk.Labels, code), nil
}

// Finding is one at-risk plan in a PlanCheck report.
type Finding struct {
	TaskID string `json:"task"`
	Stage  string `json:"stage"`
	Detail string `json:"detail"`
}

// PlanVerifier checks one locator against the outside world (filesystem,
// git). Split out so PlanCheck stays deterministic in tests; the CLI passes
// DefaultVerifier(cwd). Annotate never verifies — pure over the task.
type PlanVerifier func(kind, ref string) (ok bool, detail string)

// PlanCheck walks the project's planned and implementable tasks and reports
// every plan at risk: unparseable payloads, missing records, ephemeral
// plans (unverifiable by construction), and locators the verifier rejects.
// Healthy plans are counted, not listed. Read-only: the reporter reports,
// the DECIDER demotes — never this code.
func (r *Reporter) PlanCheck(code string, verify PlanVerifier) ([]Finding, int, error) {
	tasks, err := r.Store.ListTasksErr(core.QueryFilters{Project: code, Expr: "stage:planned OR stage:implementable"})
	if err != nil {
		return nil, 0, err
	}
	var findings []Finding
	healthy := 0
	for _, tk := range tasks {
		stage := firstStageValue(tk.Labels, code)
		pl, err := DecodePayload(tk.Meta[CapabilityName])
		if err != nil {
			findings = append(findings, Finding{tk.ID, stage, "payload unparseable (hand-repair needed)"})
			continue
		}
		p := pl.Plan()
		switch {
		case p == nil:
			findings = append(findings, Finding{tk.ID, stage, "no plan recorded"})
		case p.Kind == PlanKindEphemeral:
			findings = append(findings, Finding{tk.ID, stage, "ephemeral plan: " + p.Ref + " (unverifiable)"})
		default:
			if ok, detail := verify(p.Kind, p.Ref); !ok {
				findings = append(findings, Finding{tk.ID, stage, detail})
			} else {
				healthy++
			}
		}
	}
	return findings, healthy, nil
}

// DefaultVerifier verifies locators from dir: file paths by existence,
// commits by resolvability in dir's git repository. Running outside the
// right repository makes commit plans unresolvable — the message says so
// rather than calling the plan missing.
func DefaultVerifier(dir string) PlanVerifier {
	return func(kind, ref string) (bool, string) {
		switch kind {
		case PlanKindFile:
			if _, err := os.Stat(filepath.Join(dir, ref)); err != nil {
				return false, "plan file missing: " + ref
			}
			return true, ""
		case PlanKindCommit:
			if err := exec.Command("git", "-C", dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}").Run(); err != nil {
				return false, "plan commit unresolvable from " + dir + " (missing commit or not a git repository): " + ref
			}
			return true, ""
		}
		return false, "unknown plan kind: " + kind
	}
}

// TaskLinks is the links view for one task: outbound from its own payload,
// inbound computed by scanning the project's workflow_ai payloads (one
// writer per fact — inbound is always derived, never stored).
type TaskLinks struct {
	RevisionOf  string   `json:"revision_of,omitempty"`
	RelatesTo   []string `json:"relates_to,omitempty"`
	Revisions   []string `json:"revisions,omitempty"`
	RelatedFrom []string `json:"related_from,omitempty"`
}

// Links reports taskID's link topology. The task's own malformed payload is
// an error; malformed payloads on OTHER tasks are skipped in the scan
// (PlanCheck is the surface that reports those).
func (r *Reporter) Links(taskID string) (*TaskLinks, error) {
	tk, err := r.Store.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	code, _, ok := core.ParseTaskID(taskID)
	if !ok {
		return nil, fmt.Errorf("invalid task id %q", taskID)
	}
	pl, err := DecodePayload(tk.Meta[CapabilityName])
	if err != nil {
		return nil, fmt.Errorf("%s: %w", taskID, err)
	}
	out := &TaskLinks{RevisionOf: pl.RevisionOf(), RelatesTo: pl.RelatesTo()}
	tasks, err := r.Store.ListTasksErr(core.QueryFilters{Project: code})
	if err != nil {
		return nil, err
	}
	for _, other := range tasks {
		if other.ID == taskID {
			continue
		}
		opl, err := DecodePayload(other.Meta[CapabilityName])
		if err != nil {
			continue
		}
		if opl.RevisionOf() == taskID {
			out.Revisions = append(out.Revisions, other.ID)
		}
		if containsString(opl.RelatesTo(), taskID) {
			out.RelatedFrom = append(out.RelatedFrom, other.ID)
		}
	}
	return out, nil
}
