package workflowrpi

import (
	"fmt"

	"atm/internal/core"
)

// Reporter is the read-only side of the capability: it derives inbound link
// views by scanning the project (only outbound facts are stored) and reports
// what the manager should look at. It NEVER mutates a task — the manager is
// the decider; the reporter only shows evidence and gaps.
type Reporter struct {
	Store core.TaskService
}

// TaskLinks is one task's link topology, outbound (stored in its own
// payload) and inbound (derived by scanning the project).
type TaskLinks struct {
	ProductOf string   `json:"product_of,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"`
	RelatesTo []string `json:"relates_to,omitempty"`
	CoveredBy string   `json:"covered_by,omitempty"`

	PipelineChildren []string `json:"pipeline_children,omitempty"`
	Dependents       []string `json:"dependents,omitempty"`
	RelatedFrom      []string `json:"related_from,omitempty"`
	Covered          []string `json:"covered,omitempty"`
}

// Finding is one at-risk observation. The reporter never repairs it.
type Finding struct {
	TaskID string `json:"task"`
	Detail string `json:"detail"`
}

// TaskSummary is one task's RPI-visible state in a project report.
type TaskSummary struct {
	TaskID    string   `json:"task"`
	Lane      string   `json:"lane,omitempty"`
	Status    string   `json:"status,omitempty"`
	ProductOf string   `json:"product_of,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"`
	BlockedBy []string `json:"blocked_by,omitempty"`
	Children  int      `json:"children,omitempty"`
}

// ProjectReport is the manager's read of one project through the RPI lens.
type ProjectReport struct {
	Project  string        `json:"project"`
	Backlog  int           `json:"backlog"`
	Product  []TaskSummary `json:"product,omitempty"`
	Pipeline []TaskSummary `json:"pipeline,omitempty"`
	Reject   []TaskSummary `json:"reject,omitempty"`
	Findings []Finding     `json:"findings,omitempty"`
}

// laneOf reports the task's RPI lane, "" meaning backlog (the unset set).
func laneOf(tk *core.Task, code string) string {
	_, vals := labelValues(tk, code, RPINamespace)
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

// statusOf reports the lane-local status value for the task's lane, "" when
// the lane carries none.
func statusOf(tk *core.Task, code, lane string) string {
	ns := ""
	switch lane {
	case LaneProduct:
		ns = ProductNamespace
	case LanePipeline:
		ns = DevNamespace
	case LaneReject:
		ns = RejectNamespace
	default:
		return ""
	}
	_, vals := labelValues(tk, code, ns)
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

// projectTasks lists every task in the project — the whole point of the RPI
// perspective is that nothing is invisible to it.
func (r *Reporter) projectTasks(code string) ([]*core.Task, error) {
	return r.Store.ListTasksErr(core.QueryFilters{Project: code})
}

// Links returns the task's outbound links plus the inbound views derived
// from its siblings. A sibling whose own payload is unparseable is skipped
// for derivation (Report lists it as at-risk) — one bad payload never blinds
// the whole topology.
func (r *Reporter) Links(taskID string) (*TaskLinks, error) {
	code, _, ok := core.ParseTaskID(taskID)
	if !ok {
		return nil, fmt.Errorf("invalid task id %q", taskID)
	}
	tk, err := r.Store.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	pl, err := DecodePayload(tk.Meta[CapabilityName])
	if err != nil {
		return nil, fmt.Errorf("%s: %w", taskID, err)
	}
	out := &TaskLinks{
		ProductOf: pl.ProductOf(),
		DependsOn: pl.DependsOn(),
		RelatesTo: pl.RelatesTo(),
		CoveredBy: pl.CoveredBy(),
	}
	tasks, err := r.projectTasks(code)
	if err != nil {
		return nil, err
	}
	for _, other := range tasks {
		if other.ID == taskID {
			continue
		}
		opl, err := DecodePayload(other.Meta[CapabilityName])
		if err != nil {
			continue // at-risk, reported by Report; never fatal here
		}
		if opl.ProductOf() == taskID {
			out.PipelineChildren = append(out.PipelineChildren, other.ID)
		}
		if containsString(opl.DependsOn(), taskID) {
			out.Dependents = append(out.Dependents, other.ID)
		}
		if containsString(opl.RelatesTo(), taskID) {
			out.RelatedFrom = append(out.RelatedFrom, other.ID)
		}
		if opl.CoveredBy() == taskID {
			out.Covered = append(out.Covered, other.ID)
		}
	}
	return out, nil
}

// Report reads the whole project through the RPI lens: lane rosters, the
// backlog count, and the at-risk findings a manager should triage. Read-only.
func (r *Reporter) Report(code string) (*ProjectReport, error) {
	tasks, err := r.projectTasks(code)
	if err != nil {
		return nil, err
	}
	rep := &ProjectReport{Project: code}

	// Pass 1: decode every payload once. A task whose own payload is
	// unparseable is a finding and contributes nothing to derivation.
	payloads := map[string]*Payload{}
	byID := map[string]*core.Task{}
	children := map[string]int{}
	for _, tk := range tasks {
		byID[tk.ID] = tk
		pl, err := DecodePayload(tk.Meta[CapabilityName])
		if err != nil {
			rep.Findings = append(rep.Findings, Finding{TaskID: tk.ID, Detail: "payload unparseable (hand-repair needed)"})
			continue
		}
		payloads[tk.ID] = pl
	}
	for _, pl := range payloads {
		if p := pl.ProductOf(); p != "" {
			children[p]++
		}
	}

	for _, tk := range tasks {
		lane := laneOf(tk, code)
		if lane == "" {
			rep.Backlog++
		}
		if drift := unknownRPILabels(tk, code); len(drift) > 0 {
			rep.Findings = append(rep.Findings, Finding{
				TaskID: tk.ID,
				Detail: fmt.Sprintf("unmanaged RPI vocabulary drift: %v", drift),
			})
		}
		if _, lanes := labelValues(tk, code, RPINamespace); len(lanes) > 1 {
			rep.Findings = append(rep.Findings, Finding{
				TaskID: tk.ID,
				Detail: fmt.Sprintf("more than one lane label %v (re-run a lane verb to converge)", lanes),
			})
		}
		if lane == "" {
			continue
		}
		sum := TaskSummary{TaskID: tk.ID, Lane: lane, Status: statusOf(tk, code, lane), Children: children[tk.ID]}
		pl := payloads[tk.ID]
		if pl == nil {
			// Unparseable payload: already reported, and no link view exists.
			rep.appendByLane(lane, sum)
			continue
		}
		sum.ProductOf = pl.ProductOf()
		sum.DependsOn = pl.DependsOn()

		rep.Findings = append(rep.Findings, r.laneFindings(tk, code, lane, sum.Status, pl, byID)...)
		for _, dep := range pl.DependsOn() {
			other, ok := byID[dep]
			if !ok {
				sum.BlockedBy = append(sum.BlockedBy, dep)
				rep.Findings = append(rep.Findings, Finding{TaskID: tk.ID, Detail: "missing linked target " + dep + " (depends_on)"})
				continue
			}
			if statusOf(other, code, LanePipeline) != DevDone {
				sum.BlockedBy = append(sum.BlockedBy, dep)
			}
			if opl := payloads[dep]; opl != nil && containsString(opl.DependsOn(), tk.ID) {
				rep.Findings = append(rep.Findings, Finding{TaskID: tk.ID, Detail: "dependency cycle with " + dep})
			}
		}
		rep.appendByLane(lane, sum)
	}
	return rep, nil
}

// laneFindings collects the lane-specific gaps: the invariants the recorder
// maintains but raw label edits can break.
func (r *Reporter) laneFindings(tk *core.Task, code, lane, status string, pl *Payload, byID map[string]*core.Task) []Finding {
	var out []Finding
	if status == "" {
		out = append(out, Finding{TaskID: tk.ID, Detail: "lane " + lane + " without a status label"})
	}
	switch lane {
	case LanePipeline:
		parent := pl.ProductOf()
		switch {
		case parent == "":
			out = append(out, Finding{TaskID: tk.ID, Detail: "pipeline task missing product_of"})
		default:
			p, ok := byID[parent]
			if !ok {
				out = append(out, Finding{TaskID: tk.ID, Detail: "missing linked target " + parent + " (product_of)"})
			} else if laneOf(p, code) != LaneProduct {
				out = append(out, Finding{TaskID: tk.ID, Detail: "product_of target " + parent + " is not in the product lane"})
			}
		}
	case LaneReject:
		if status == RejectCoveredBy || status == RejectDuplicate {
			cov := pl.CoveredBy()
			if cov == "" {
				out = append(out, Finding{TaskID: tk.ID, Detail: "reject reason " + status + " without covered_by"})
			} else if _, ok := byID[cov]; !ok {
				out = append(out, Finding{TaskID: tk.ID, Detail: "missing linked target " + cov + " (covered_by)"})
			}
		}
	}
	for _, id := range pl.RelatesTo() {
		if _, ok := byID[id]; !ok {
			out = append(out, Finding{TaskID: tk.ID, Detail: "missing linked target " + id + " (relates_to)"})
		}
	}
	// Lane-local axes that belong to another lane are stale label state.
	for _, ns := range []string{ProductNamespace, DevNamespace, RejectNamespace} {
		if ns == laneNamespace(lane) {
			continue
		}
		if _, vals := labelValues(tk, code, ns); len(vals) > 0 {
			out = append(out, Finding{TaskID: tk.ID, Detail: fmt.Sprintf("stale %s labels %v outside their lane", ns, vals)})
		}
	}
	return out
}

func laneNamespace(lane string) string {
	switch lane {
	case LaneProduct:
		return ProductNamespace
	case LanePipeline:
		return DevNamespace
	case LaneReject:
		return RejectNamespace
	}
	return ""
}

// unknownRPILabels reports labels in this capability's namespaces whose
// values this version does not know — vocabulary drift a manager should
// resolve. Recorders converge only their own known axes.
func unknownRPILabels(tk *core.Task, code string) []string {
	var out []string
	check := func(ns string, valid func(string) bool) {
		existing, vals := labelValues(tk, code, ns)
		for i, v := range vals {
			if !valid(v) {
				out = append(out, existing[i])
			}
		}
	}
	check(RPINamespace, func(v string) bool {
		return containsString([]string{LaneProduct, LanePipeline, LaneReject}, v)
	})
	check(ProductNamespace, validProductStatus)
	check(DevNamespace, validDevStatus)
	check(RejectNamespace, validRejectReason)
	return out
}

func (rep *ProjectReport) appendByLane(lane string, sum TaskSummary) {
	switch lane {
	case LaneProduct:
		rep.Product = append(rep.Product, sum)
	case LanePipeline:
		rep.Pipeline = append(rep.Pipeline, sum)
	case LaneReject:
		rep.Reject = append(rep.Reject, sum)
	}
}
