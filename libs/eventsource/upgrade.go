package eventsource

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// UpgradeResult is the output of the one-time v1→v2 upgrade.
// IdentityByAlias maps every v1 alias ("ATM-0001", "ATM-0001-c0001",
// project codes) to the identity of its upgraded creation event.
type UpgradeResult struct {
	Events          []*Event
	IdentityByAlias map[string]string
}

// v1Entry is the v1 LogEntry shape (internal/store/log.go), decoded
// loosely: payload stays raw so unknown keys survive verbatim. The v1 type
// is redeclared rather than imported — eventsource must not depend on
// internal/store.
type v1Entry struct {
	Seq     int             `json:"seq"`
	At      time.Time       `json:"at"`
	Actor   string          `json:"actor"`
	Action  string          `json:"action"`
	Subject Subject         `json:"subject"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// UpgradeV1 converts a complete v1 log.jsonl into the equivalent v2 event
// chain. It is a PURE function of the log bytes (D6): no clock, no local
// replica id, no randomness — every machine upgrading the same log
// produces byte-identical events, so copies of a store converge trivially.
// It is also strict: any malformed line, dangling reference, duplicate
// creation, or non-monotonic seq aborts, because the upgrade must be
// lossless or it must not happen (spec decision 13).
func UpgradeV1(logData []byte) (*UpgradeResult, error) {
	res := &UpgradeResult{IdentityByAlias: map[string]string{}}
	prevID := ""
	prevSeq := 0
	// labelsByAlias tracks each entity's label list through the linear v1
	// history, so membership deltas can be synthesized (spec decision 4).
	// This is a pure function of the log bytes, so D6's purity rule holds.
	labelsByAlias := map[string][]string{}

	sc := bufio.NewScanner(bytes.NewReader(logData))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var v1 v1Entry
		if err := json.Unmarshal(line, &v1); err != nil {
			return nil, fmt.Errorf("eventsource: upgrade: line %d: %w", lineNo, err)
		}
		if v1.Seq <= prevSeq {
			return nil, fmt.Errorf("eventsource: upgrade: line %d: seq %d not increasing", lineNo, v1.Seq)
		}
		prevSeq = v1.Seq

		payload := map[string]any{}
		if len(v1.Payload) > 0 {
			dec := json.NewDecoder(bytes.NewReader(v1.Payload))
			dec.UseNumber() // preserve number formatting through the round trip
			if err := dec.Decode(&payload); err != nil {
				return nil, fmt.Errorf("eventsource: upgrade: line %d payload: %w", lineNo, err)
			}
		}

		subject := v1.Subject
		v1Alias := subject.ID // task/comment alias; empty for project/label

		switch v1.Action {
		case ActionProjectCreated:
			// A creation event cannot contain its own hash (spec decision 1):
			// the v1 alias moves into the payload. Projects are aliased by code.
			if subject.Code == "" {
				return nil, fmt.Errorf("eventsource: upgrade: line %d: project.created without subject.code", lineNo)
			}
			if _, dup := res.IdentityByAlias[subject.Code]; dup {
				return nil, fmt.Errorf("eventsource: upgrade: line %d: duplicate creation of %q", lineNo, subject.Code)
			}
			payload["alias"] = subject.Code
		case ActionTaskCreated, ActionCommentCreated:
			if v1Alias == "" {
				return nil, fmt.Errorf("eventsource: upgrade: line %d: creation without subject.id", lineNo)
			}
			if _, dup := res.IdentityByAlias[v1Alias]; dup {
				return nil, fmt.Errorf("eventsource: upgrade: line %d: duplicate creation of %q", lineNo, v1Alias)
			}
			payload["alias"] = v1Alias
			subject.ID = ""
			if v1.Action == ActionCommentCreated {
				// Synthesize identity references beside the v1 alias keys,
				// which stay verbatim (spec decision 2).
				taskID, _ := payload["task_id"].(string)
				ref, ok := res.IdentityByAlias[taskID]
				if !ok {
					return nil, fmt.Errorf("eventsource: upgrade: line %d: comment references unknown task %q", lineNo, taskID)
				}
				payload["task_ref"] = ref
				if rt, _ := payload["reply_to"].(string); rt != "" {
					rref, ok := res.IdentityByAlias[rt]
					if !ok {
						return nil, fmt.Errorf("eventsource: upgrade: line %d: reply to unknown comment %q", lineNo, rt)
					}
					payload["reply_to_ref"] = rref
				}
			}
		case ActionLabelUpserted:
			// v1 upsert semantics replace the whole record: an absent key
			// means "". Materialize both scalar keys so the v2 per-key slot
			// rule reproduces v1 exactly (spec decision 5).
			if _, ok := payload["description"]; !ok {
				payload["description"] = ""
			}
			if _, ok := payload["expr"]; !ok {
				payload["expr"] = ""
			}
		default:
			// Non-creation event: subject.id becomes the entity's identity.
			// This arm also carries the retired task.meta-changed through
			// unharmed — it writes no slot but stays a causal DAG node (D5).
			switch subject.Kind {
			case "task", "comment":
				id, ok := res.IdentityByAlias[v1Alias]
				if !ok {
					return nil, fmt.Errorf("eventsource: upgrade: line %d: %s event for unknown %q", lineNo, v1.Action, v1Alias)
				}
				subject.ID = id
			case "project":
				// Same rule as task/comment: a dangling reference aborts the
				// upgrade (spec decision 13 — lossless or it does not happen).
				// Tolerating the miss would emit an event with an empty
				// subject.id, which collectWrites silently drops.
				id, ok := res.IdentityByAlias[subject.Code]
				if !ok {
					return nil, fmt.Errorf("eventsource: upgrade: line %d: %s event for unknown project %q", lineNo, v1.Action, subject.Code)
				}
				subject.ID = id
			}
		}

		// Membership delta synthesis (spec decision 4). A v1 label-add/remove
		// payload is a whole-entity SNAPSHOT that never names the changed
		// label; the v2 fold needs a per-slot DELTA. Compute it by tracking
		// each entity's label list through the linear v1 history. Note that a
		// -removed snapshot lists the REMAINING labels, so the removal delta
		// is old−new — never the snapshot itself.
		switch v1.Action {
		case ActionTaskLabelAdded, ActionCommentLabelAdded, ActionTaskLabelRemoved, ActionCommentLabelRemoved:
			newLabels := stringList(payload["labels"])
			oldLabels := labelsByAlias[v1Alias]
			var delta []string
			if v1.Action == ActionTaskLabelAdded || v1.Action == ActionCommentLabelAdded {
				delta = diffStrings(newLabels, oldLabels)
			} else {
				delta = diffStrings(oldLabels, newLabels)
			}
			if len(delta) == 1 {
				payload["label"] = delta[0]
			} else if len(delta) > 1 {
				payload["label"] = delta
			}
			labelsByAlias[v1Alias] = newLabels
		case ActionTaskCreated, ActionCommentCreated:
			labelsByAlias[v1Alias] = stringList(payload["labels"])
		}

		parents := []string{}
		if prevID != "" {
			parents = []string{prevID}
		}
		// The v1 seq is demoted to the HLC's logical counter; it never
		// appears in the envelope and is never hashed as itself.
		obj := map[string]any{
			"v":       2,
			"parents": parents,
			"hlc":     HLC{P: v1.At.UnixMilli(), L: int64(v1.Seq)},
			"replica": ReplicaV1,
			"at":      v1.At.UTC().Format(time.RFC3339Nano),
			"actor":   v1.Actor,
			"action":  v1.Action,
			"subject": subject,
		}
		if len(payload) > 0 {
			obj["payload"] = payload
		}
		raw, err := json.Marshal(obj)
		if err != nil {
			return nil, fmt.Errorf("eventsource: upgrade: line %d: %w", lineNo, err)
		}
		e, err := Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("eventsource: upgrade: line %d: %w", lineNo, err)
		}
		res.Events = append(res.Events, e)
		prevID = e.ID
		switch v1.Action {
		case ActionProjectCreated:
			res.IdentityByAlias[subject.Code] = e.ID
		case ActionTaskCreated, ActionCommentCreated:
			res.IdentityByAlias[v1Alias] = e.ID
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("eventsource: upgrade: %w", err)
	}

	// Self-verify: the upgrade must be lossless or it must not happen (spec
	// decision 13). Fold the events just synthesized and compare them against a
	// pure replay of the input bytes; any semantic divergence aborts before
	// anything on disk moves. This makes UpgradeV1 prove its own output without
	// depending on internal/store.
	st, err := FoldEvents(res.Events)
	if err != nil {
		return nil, fmt.Errorf("eventsource: upgrade: self-fold: %w", err)
	}
	rep, err := ReplayV1(logData)
	if err != nil {
		return nil, err
	}
	if err := CompareReplayToFold(rep, st); err != nil {
		return nil, err
	}
	return res, nil
}

// stringList reads a decoded JSON array of strings.
func stringList(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, x := range list {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// diffStrings returns the elements of a not present in b, sorted — sorted so
// the synthesized delta is a deterministic function of the log (D6).
func diffStrings(a, b []string) []string {
	in := make(map[string]bool, len(b))
	for _, x := range b {
		in[x] = true
	}
	var out []string
	for _, x := range a {
		if !in[x] {
			out = append(out, x)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
