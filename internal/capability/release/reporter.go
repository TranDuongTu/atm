package release

import (
	"strings"

	"atm/internal/core"
)

// Reporter is the read-only side of the capability. It NEVER mutates a task.
type Reporter struct {
	Store core.TaskService
}

// Finding is one observation the manager should resolve.
type Finding struct {
	TaskID string `json:"task"`
	Detail string `json:"detail"`
}

// MemberSummary is one member of a release, with a SNAPSHOT of the public
// labels it carries outside this capability's own namespace.
//
// The snapshot is deliberately unfiltered. Which labels mean "certified" is
// another capability's business, and naming them here would wire release to
// capabilities it must not know about; the guide tells the decider what to
// look for, and this just shows what is there.
type MemberSummary struct {
	TaskID  string   `json:"task"`
	Missing bool     `json:"missing,omitempty"`
	Shipped bool     `json:"shipped,omitempty"`
	Labels  []string `json:"labels,omitempty"`
}

// ContainerSummary is one release record.
type ContainerSummary struct {
	TaskID  string          `json:"task"`
	Version string          `json:"version"`
	Shipped bool            `json:"shipped,omitempty"`
	Members []MemberSummary `json:"members,omitempty"`
}

// ProjectReport is the manager's read of one project's releases.
type ProjectReport struct {
	Project    string             `json:"project"`
	Containers []ContainerSummary `json:"containers,omitempty"`
	Findings   []Finding          `json:"findings,omitempty"`
}

// Report reads every release record in the project. Read-only.
func (r *Reporter) Report(code string) (*ProjectReport, error) {
	tasks, err := r.Store.ListTasksErr(core.QueryFilters{Project: code})
	if err != nil {
		return nil, err
	}
	rep := &ProjectReport{Project: code}
	shippedLabel := ShippedLabel(code)

	byID := map[string]*core.Task{}
	payloads := map[string]*Payload{}
	for _, tk := range tasks {
		byID[tk.ID] = tk
		pl, err := DecodePayload(tk.Meta[CapabilityName])
		if err != nil {
			rep.Findings = append(rep.Findings, Finding{TaskID: tk.ID, Detail: "payload unparseable (hand-repair needed)"})
			continue
		}
		payloads[tk.ID] = pl
	}

	for _, tk := range tasks {
		pl := payloads[tk.ID]
		if pl == nil {
			continue
		}
		if v := pl.Version(); v != "" {
			sum := ContainerSummary{TaskID: tk.ID, Version: v, Shipped: containsString(tk.Labels, shippedLabel)}
			for _, id := range pl.Members() {
				m, ok := byID[id]
				if !ok {
					sum.Members = append(sum.Members, MemberSummary{TaskID: id, Missing: true})
					rep.Findings = append(rep.Findings, Finding{TaskID: tk.ID, Detail: "roster names a missing task " + id})
					continue
				}
				ms := MemberSummary{TaskID: id, Shipped: containsString(m.Labels, shippedLabel), Labels: foreignLabels(m, code)}
				if sum.Shipped && !ms.Shipped {
					rep.Findings = append(rep.Findings, Finding{TaskID: id, Detail: "release " + v + " has shipped but this member is not stamped shipped"})
				}
				sum.Members = append(sum.Members, ms)
			}
			rep.Containers = append(rep.Containers, sum)
			continue
		}
		if parent := pl.ReleaseOf(); parent != "" {
			c, ok := byID[parent]
			if !ok {
				rep.Findings = append(rep.Findings, Finding{TaskID: tk.ID, Detail: "release_of names a missing container " + parent})
				continue
			}
			if cpl := payloads[parent]; cpl != nil && !containsString(cpl.Members(), tk.ID) {
				rep.Findings = append(rep.Findings, Finding{TaskID: tk.ID, Detail: "release_of points at " + parent + " but that container's roster does not list it"})
			}
			_ = c
		}
	}
	return rep, nil
}

// foreignLabels is the member's labels outside this capability's namespace —
// the evidence a decider reads certification off, without release having an
// opinion about what any of them mean.
func foreignLabels(tk *core.Task, code string) []string {
	prefix := code + ":" + Namespace + ":"
	var out []string
	for _, l := range tk.Labels {
		if strings.HasPrefix(l, prefix) {
			continue
		}
		out = append(out, l)
	}
	return out
}
