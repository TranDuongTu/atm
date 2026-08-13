package core

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ChannelMetaKey is the Task.Meta key the channel capability owns.
const ChannelMetaKey = "channel"

// Channel types recognized today; the type is the channel:<type> label suffix.
const (
	ChannelTypeRepo   = "repo"
	ChannelTypeNotion = "notion"
)

var ChannelTypes = []string{ChannelTypeRepo, ChannelTypeNotion}

// ChannelLabel is the stored label a channel task of the given type carries.
func ChannelLabel(code, typ string) string { return code + ":channel:" + typ }

// ChannelAddress is the machine-independent address of a channel — tier 1,
// synced. An address is not a credential. Type-shaped: repo uses URL; notion
// uses Workspace plus Database or Page.
type ChannelAddress struct {
	URL       string `json:"url,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Database  string `json:"database,omitempty"`
	Page      string `json:"page,omitempty"`
}

// ChannelRecord is the tier-1 ledger record decoded from a channel task.
// Name is the unique handle within the project (canonical in the payload, so
// it survives title edits); Purpose is the task description. The json tags
// are load-bearing: ChannelView embeds this struct and `--output json` on
// list/show is the agent endpoint's contract — snake_case keys, not Go field
// names.
type ChannelRecord struct {
	TaskID  string         `json:"task_id"`
	Name    string         `json:"name"`
	Type    string         `json:"type"`
	Purpose string         `json:"purpose,omitempty"`
	Address ChannelAddress `json:"address,omitzero"`
}

// ChannelProbe is the cheap local-probe result for a channel's wiring. All
// checks are local; probing never fetches or speaks a third-party API.
type ChannelProbe struct {
	PathExists  bool `json:"path_exists"`
	IsGitRepo   bool `json:"is_git_repo"`
	Dirty       bool `json:"dirty"`
	HasUpstream bool `json:"has_upstream"`
	Ahead       int  `json:"ahead"`
	Behind      int  `json:"behind"`
}

// ChannelView is the joined read: ledger record + this machine's wiring +
// probe. Wiring is nil when this machine has none; Probe is nil when there
// is nothing to probe locally (e.g. a notion channel).
type ChannelView struct {
	ChannelRecord
	Wiring *ChannelWiring `json:"wiring,omitempty"`
	Probe  *ChannelProbe  `json:"probe,omitempty"`
}

// DecodeChannelPayload parses a payload string; "" is a valid empty payload.
// A malformed payload is an ERROR — verbs fail rather than overwrite state
// they cannot read (same doctrine as workflowai's payload).
func DecodeChannelPayload(s string) (map[string]any, error) {
	if s == "" {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, fmt.Errorf("%s payload is not a JSON object (hand-repair needed): %w", ChannelMetaKey, err)
	}
	return m, nil
}

// EncodeChannelPayload serializes, stamping v:1. Unknown fields survive
// because the map is the source of truth. Empty (besides v) encodes to "".
// The argument is copied, never mutated: callers decode-mutate-encode and
// must not have their map silently stamped by a failed encode.
func EncodeChannelPayload(m map[string]any) (string, error) {
	out := make(map[string]any, len(m)+1)
	for k, v := range m {
		if k != "v" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return "", nil
	}
	out["v"] = 1
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ChannelPayloadFrom builds the payload map for a record (name, type, address).
func ChannelPayloadFrom(rec ChannelRecord) map[string]any {
	addr := map[string]any{}
	if rec.Address.URL != "" {
		addr["url"] = rec.Address.URL
	}
	if rec.Address.Workspace != "" {
		addr["workspace"] = rec.Address.Workspace
	}
	if rec.Address.Database != "" {
		addr["database"] = rec.Address.Database
	}
	if rec.Address.Page != "" {
		addr["page"] = rec.Address.Page
	}
	m := map[string]any{"name": rec.Name, "type": rec.Type}
	if len(addr) > 0 {
		m["address"] = addr
	}
	return m
}

func channelStr(v any) string { s, _ := v.(string); return s }

// ChannelFromTask decodes a channel task. (nil, nil) when the task carries no
// channel:<type> label; an error when it does but the payload is unreadable.
func ChannelFromTask(code string, t Task) (*ChannelRecord, error) {
	prefix := code + ":channel:"
	typ := ""
	for _, l := range t.Labels {
		if strings.HasPrefix(l, prefix) {
			typ = strings.TrimPrefix(l, prefix)
			break
		}
	}
	if typ == "" {
		return nil, nil
	}
	m, err := DecodeChannelPayload(t.Meta[ChannelMetaKey])
	if err != nil {
		return nil, fmt.Errorf("task %s: %w", t.ID, err)
	}
	rec := &ChannelRecord{TaskID: t.ID, Name: channelStr(m["name"]), Type: typ, Purpose: t.Description}
	if rec.Name == "" {
		rec.Name = t.Title
	}
	if am, ok := m["address"].(map[string]any); ok {
		rec.Address = ChannelAddress{URL: channelStr(am["url"]), Workspace: channelStr(am["workspace"]), Database: channelStr(am["database"]), Page: channelStr(am["page"])}
	}
	return rec, nil
}
