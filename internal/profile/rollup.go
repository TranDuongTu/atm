package profile

import "sort"

// ChannelRollup folds one channel's endpoint rows into the single line a
// list view can show: how many endpoints it has, whether any lack an
// address, and the WORST attestation each configured agent has across them.
//
// Worst, not best: a channel is only as reachable as its least-reachable
// endpoint, and a rollup that reported the best one would tell an agent it
// can reach a channel it cannot.
type ChannelRollup struct {
	Channel string
	// Endpoints is how many endpoint rows folded into this line; zero means
	// the channel exists but has nowhere to reach.
	Endpoints int
	// Unaddressed and Unwired count endpoints stuck below attestation, so a
	// list can say WHY an agent column is empty.
	Unaddressed int
	Unwired     int
	// Agents maps each configured agent to its worst attestation across the
	// channel's endpoints.
	Agents map[string]Attestation
}

// RollupByChannel groups an attestation matrix by channel, in channel-name
// order. Channels with no endpoint rows are absent — the caller knows which
// channels exist and renders the missing ones itself, since "this channel
// has nowhere to reach" is a fact about the record, not about the matrix.
func RollupByChannel(matrix []EndpointRow, agents []string) []ChannelRollup {
	byName := map[string]*ChannelRollup{}
	var order []string
	for _, row := range matrix {
		r, ok := byName[row.Channel]
		if !ok {
			r = &ChannelRollup{Channel: row.Channel, Agents: map[string]Attestation{}}
			byName[row.Channel] = r
			order = append(order, row.Channel)
		}
		r.Endpoints++
		if !row.Addressed {
			r.Unaddressed++
		} else if !row.Wired {
			r.Unwired++
		}
		for _, a := range agents {
			cur, seen := r.Agents[a]
			next := row.Agents[a]
			if !seen || attestWorse(next, cur) {
				r.Agents[a] = next
			}
		}
	}
	sort.Strings(order)
	out := make([]ChannelRollup, 0, len(order))
	for _, name := range order {
		out = append(out, *byName[name])
	}
	return out
}

// attestRank orders the states worst-first so a rollup can compare them.
func attestRank(a Attestation) int {
	switch a.State {
	case AttestNone:
		return 0
	case AttestStale:
		return 1
	case AttestFresh:
		return 2
	}
	return 0
}

// attestWorse reports whether x is a worse answer than y — by state, and
// within stale by age, so the oldest stamp is the one a rollup surfaces.
func attestWorse(x, y Attestation) bool {
	if rx, ry := attestRank(x), attestRank(y); rx != ry {
		return rx < ry
	}
	return x.Days > y.Days
}

// MatrixSummary is the aggregate line a list view closes with: what is
// missing across the whole project, so a reader who is not going to walk
// every row still learns whether anything is wrong.
type MatrixSummary struct {
	Endpoints   int
	Unaddressed int
	Unwired     int
	// NeverAttested counts, per agent, the addressed-and-wired endpoints it
	// has no stamp for. Endpoints below wiring are excluded: an agent cannot
	// attest what this machine cannot reach, so counting them would blame
	// the agent for a wiring gap.
	NeverAttested map[string]int
	Stale         map[string]int
}

// SummarizeMatrix aggregates the whole endpoint × agent matrix.
func SummarizeMatrix(matrix []EndpointRow, agents []string) MatrixSummary {
	s := MatrixSummary{NeverAttested: map[string]int{}, Stale: map[string]int{}}
	for _, row := range matrix {
		s.Endpoints++
		switch {
		case !row.Addressed:
			s.Unaddressed++
			continue
		case !row.Wired:
			s.Unwired++
			continue
		}
		for _, a := range agents {
			switch row.Agents[a].State {
			case AttestNone:
				s.NeverAttested[a]++
			case AttestStale:
				s.Stale[a]++
			}
		}
	}
	return s
}
