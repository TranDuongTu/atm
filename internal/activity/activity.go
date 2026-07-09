// Package activity turns a project's log entries into resolved actor records
// and aggregates them for the `atm activity` command and the TUI Actors pane.
package activity

import (
	"sort"

	"atm/internal/actor"
	"atm/internal/store"
)

type Record struct {
	Persona string
	Agent   string
	Model   string
	Action  string
}

func Build(entries []store.LogEntry) []Record {
	out := make([]Record, 0, len(entries))
	for _, e := range entries {
		id := actor.Resolve(e.Actor)
		out = append(out, Record{Persona: id.Persona, Agent: id.Agent, Model: id.Model, Action: e.Action})
	}
	return out
}

type Group struct {
	Key     string         `json:"key"`
	Count   int            `json:"count"`
	Agents  map[string]int `json:"agents"`
	Models  map[string]int `json:"models"`
	Actions map[string]int `json:"actions"`
}

func groupKey(r Record, groupBy string) (string, bool) {
	switch groupBy {
	case "agent":
		return r.Agent, r.Agent != ""
	case "model":
		return r.Model, r.Model != ""
	default: // persona
		return r.Persona, r.Persona != ""
	}
}

func Aggregate(recs []Record, groupBy string) []Group {
	idx := map[string]*Group{}
	var order []string
	for _, r := range recs {
		key, ok := groupKey(r, groupBy)
		if !ok {
			continue
		}
		g, exists := idx[key]
		if !exists {
			g = &Group{Key: key, Agents: map[string]int{}, Models: map[string]int{}, Actions: map[string]int{}}
			idx[key] = g
			order = append(order, key)
		}
		g.Count++
		if r.Agent != "" {
			g.Agents[r.Agent]++
		}
		if r.Model != "" {
			g.Models[r.Model]++
		}
		if r.Action != "" {
			g.Actions[r.Action]++
		}
	}
	out := make([]Group, 0, len(order))
	for _, k := range order {
		out = append(out, *idx[k])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	return out
}
