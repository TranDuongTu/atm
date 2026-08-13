# Channel Capability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement Channels (spec: `docs/superpowers/specs/2026-08-13-channel-capability-design.md`, task ATM-097849): a `channel` capability where each channel is a labelled task behind a first-class `atm channel` CLI noun and a read-only TUI overlay, with a three-tier config split (ledger record / local wiring / no secrets), full replacement of the repo dispatch machinery, and probe-plus-stamp status.

**Architecture:** Channel domain types and the payload codec live in `internal/core` (pure leaf). All channel logic that touches storage lives in `internal/store` behind new `core.ChannelService` methods, because BOTH adapters need it and the arch tests forbid adapters from importing capability packages. The `internal/capability/channel` package owns only the capability contract: vocabulary, guide, annotate, seed. The `atm channel` CLI group lives in `internal/cli` (precedent: `atm project repo`) and consumes `core.Service` only.

**Tech Stack:** Go, cobra, Bubble Tea, `git` CLI for probes (stdlib `os/exec`). No new dependencies. No cache schema change (channels reuse the existing task/label tables).

## Global Constraints

- Never run against the shared store: every test uses a `t.TempDir()` store. Never point a dev binary at `~/.config/atm` (a schema-changing build against the shared cache breaks the installed binary — project doctrine).
- Arch import rules are enforced by `tests/arch/imports_test.go`: `internal/core` imports nothing from the repo; capability packages import only `atm/internal/capability`, `atm/internal/core` (and `atm/skills`, per the workflowai precedent — the arch test checks only `atm/internal/...` imports); `internal/cli` and `internal/tui` never import `atm/internal/capability/<name>` or `atm/internal/store`.
- Secrets never touch ATM: no token, password, or credential field anywhere in this plan. Tier 2 wiring is paths, MCP server names, and verification stamps only.
- Actor stamping: every mutating call takes an `actor string`; tests use `"developer@test:unit"`.
- The full gate is `make verify` (build + test + lint + scripts); run it at the end of every task before committing.
- Markdown files: prose as single un-wrapped lines (project convention).
- The capability name string is exactly `"channel"`; the meta key is `core.ChannelMetaKey` (= `"channel"`); labels are `<CODE>:channel:<type>`; the board is `<CODE>:channels`.
- Commit messages: `<type>(ATM-097849): <subject>`, ending with the Claude co-author trailer used in this repo's recent history.

---

### Task 1: Core channel types + payload codec

**Files:**
- Create: `internal/core/channel.go`
- Create: `internal/core/channel_test.go`
- Modify: `internal/core/config.go` (add `ChannelWiring`, `VerificationStamp`, `ProjectConfig.Channels`)

**Interfaces:**
- Consumes: `core.Task`, `core.Label` (existing).
- Produces (later tasks rely on these exact names): `core.ChannelMetaKey`, `core.ChannelTypeRepo`, `core.ChannelTypeNotion`, `core.ChannelTypes`, `core.ChannelAddress{URL,Workspace,Database,Page string}`, `core.ChannelRecord{TaskID,Name,Type,Purpose string; Address ChannelAddress}`, `core.ChannelProbe{PathExists,IsGitRepo,Dirty,HasUpstream bool; Ahead,Behind int}`, `core.ChannelView{ChannelRecord; Wiring *ChannelWiring; Probe *ChannelProbe}`, `core.DecodeChannelPayload(s string) (map[string]any, error)`, `core.EncodeChannelPayload(m map[string]any) (string, error)`, `core.ChannelPayloadFrom(rec ChannelRecord) map[string]any`, `core.ChannelFromTask(code string, t Task) (*ChannelRecord, error)`, `core.ChannelLabel(code, typ string) string`, `core.VerificationStamp{At,By,Note string}`, `core.ChannelWiring{Path,MCPServer string; Stamps []VerificationStamp}`.

- [ ] **Step 1: Write the failing test**

```go
// internal/core/channel_test.go
package core

import "testing"

func TestChannelPayloadRoundTrip(t *testing.T) {
	rec := ChannelRecord{Name: "specs", Type: ChannelTypeNotion, Address: ChannelAddress{Workspace: "acme", Database: "abc123"}}
	s, err := EncodeChannelPayload(ChannelPayloadFrom(rec))
	if err != nil {
		t.Fatal(err)
	}
	task := Task{ID: "ATM-x1", Description: "specs live here", Labels: []string{"ATM:channel:notion"}, Meta: map[string]string{ChannelMetaKey: s}}
	got, err := ChannelFromTask("ATM", task)
	if err != nil {
		t.Fatal(err)
	}
	if got.TaskID != "ATM-x1" || got.Name != "specs" || got.Type != ChannelTypeNotion || got.Purpose != "specs live here" || got.Address.Database != "abc123" {
		t.Fatalf("got %+v", got)
	}
}

func TestChannelPayloadPreservesUnknownFields(t *testing.T) {
	m, err := DecodeChannelPayload(`{"v":1,"name":"x","future_field":"kept"}`)
	if err != nil {
		t.Fatal(err)
	}
	m["name"] = "y"
	s, err := EncodeChannelPayload(m)
	if err != nil {
		t.Fatal(err)
	}
	m2, _ := DecodeChannelPayload(s)
	if m2["future_field"] != "kept" || m2["name"] != "y" {
		t.Fatalf("unknown field lost: %s", s)
	}
}

func TestChannelPayloadErrors(t *testing.T) {
	if _, err := DecodeChannelPayload("not json"); err == nil {
		t.Fatal("want error on malformed payload")
	}
	if m, err := DecodeChannelPayload(""); err != nil || len(m) != 0 {
		t.Fatal("empty payload must decode to empty map")
	}
	// A task without a channel:<type> label is not a channel: nil, nil.
	if rec, err := ChannelFromTask("ATM", Task{ID: "t", Labels: []string{"ATM:status:open"}}); rec != nil || err != nil {
		t.Fatalf("non-channel task: got %v, %v", rec, err)
	}
	// A channel label with an unreadable payload is an error (verbs must not overwrite state they cannot read).
	if _, err := ChannelFromTask("ATM", Task{ID: "t", Labels: []string{"ATM:channel:repo"}, Meta: map[string]string{ChannelMetaKey: "garbage"}}); err == nil {
		t.Fatal("want error for unreadable payload")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/core/ -run TestChannelPayload -v`
Expected: FAIL (compile error: undefined `ChannelRecord`, etc.)

- [ ] **Step 3: Write the implementation**

```go
// internal/core/channel.go
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
// it survives title edits); Purpose is the task description.
type ChannelRecord struct {
	TaskID  string
	Name    string
	Type    string
	Purpose string
	Address ChannelAddress
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
func EncodeChannelPayload(m map[string]any) (string, error) {
	rest := 0
	for k := range m {
		if k != "v" {
			rest++
		}
	}
	if rest == 0 {
		return "", nil
	}
	m["v"] = 1
	b, err := json.Marshal(m)
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
```

And in `internal/core/config.go`, add above `ProjectConfig` and extend it:

```go
// VerificationStamp is one tier-2 verification record: an actor touched the
// channel and vouched for its wiring at a moment in time. No secrets.
type VerificationStamp struct {
	At   string `json:"at"`
	By   string `json:"by"`
	Note string `json:"note,omitempty"`
}

// ChannelWiring is how THIS machine reaches a channel — tier 2: config, not
// substrate state, no event-log entry, not synced, and never a secret. Path
// is the local clone for repo channels; MCPServer names the agent-side MCP
// server for notion channels. A fresh machine has no wiring until a
// concierge session records it.
type ChannelWiring struct {
	Path      string              `json:"path,omitempty"`
	MCPServer string              `json:"mcp_server,omitempty"`
	Stamps    []VerificationStamp `json:"stamps,omitempty"`
}
```

```go
// added field inside ProjectConfig, after Repos:
	Channels map[string]ChannelWiring `json:"channels,omitempty"`
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/core/ -v`
Expected: PASS (all core tests, not just the new ones — the config change must not break existing tests)

- [ ] **Step 5: Commit**

```bash
git add internal/core/channel.go internal/core/channel_test.go internal/core/config.go
git commit -m "feat(ATM-097849): core channel types, payload codec, wiring config"
```

---

### Task 2: Store — channel record CRUD over the task substrate

**Files:**
- Create: `internal/store/channel.go`
- Create: `internal/store/channel_test.go`

**Interfaces:**
- Consumes: Task 1's core types; existing `Store` methods `CreateTask`, `GetTask`, `ListTasksErr`, `SetDescription`, `SetTaskCapabilityMeta`, `RemoveTask`, and `core.QueryFilters{Project, Expr}` (`Expr: "channel:*"` selects the namespace; wildcards in `Labels` are facets and do NOT restrict — never use them for this).
- Produces: `(*Store) CreateChannel(code string, rec core.ChannelRecord, actor string) (*core.Task, error)`, `(*Store) ChannelRecords(code string) ([]core.ChannelRecord, error)`, `(*Store) EditChannel(code, name string, purpose *string, addr *core.ChannelAddress, actor string) error`, `(*Store) RemoveChannel(code, name, actor string) error`, unexported `(*Store) channelByName(code, name string) (*core.ChannelRecord, error)`.

- [ ] **Step 1: Write the failing test**

Test setup mirrors the existing store tests in this package (open a store in `t.TempDir()`, create a project). Grep `internal/store/config_test.go` for the canonical `newTestStore`/setup helper and reuse it verbatim.

```go
// internal/store/channel_test.go
package store

import (
	"errors"
	"testing"

	"atm/internal/core"
)

const chActor = "developer@test:unit"

func TestChannelCreateListEditRemove(t *testing.T) {
	s := newTestStore(t) // reuse this package's existing test-store helper
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	rec := core.ChannelRecord{Name: "specs", Type: core.ChannelTypeNotion, Purpose: "specs and plans live here", Address: core.ChannelAddress{Workspace: "acme", Database: "abc123"}}
	tk, err := s.CreateChannel("ATM", rec, chActor)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Title != "specs" {
		t.Fatalf("task title = %q, want handle", tk.Title)
	}
	got, err := s.ChannelRecords("ATM")
	if err != nil || len(got) != 1 {
		t.Fatalf("records: %v %v", got, err)
	}
	if got[0].Name != "specs" || got[0].Type != core.ChannelTypeNotion || got[0].Purpose != "specs and plans live here" {
		t.Fatalf("got %+v", got[0])
	}
	// duplicate handle rejected, across ALL types
	if _, err := s.CreateChannel("ATM", core.ChannelRecord{Name: "specs", Type: core.ChannelTypeRepo}, chActor); !errors.Is(err, core.ErrUsage) {
		t.Fatalf("dup: %v", err)
	}
	// unknown type rejected
	if _, err := s.CreateChannel("ATM", core.ChannelRecord{Name: "x", Type: "slack"}, chActor); !errors.Is(err, core.ErrUsage) {
		t.Fatalf("type: %v", err)
	}
	// edit purpose + address
	p := "moved purpose"
	if err := s.EditChannel("ATM", "specs", &p, &core.ChannelAddress{Workspace: "acme", Database: "zzz"}, chActor); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ChannelRecords("ATM")
	if got[0].Purpose != "moved purpose" || got[0].Address.Database != "zzz" {
		t.Fatalf("after edit: %+v", got[0])
	}
	// remove
	if err := s.RemoveChannel("ATM", "specs", chActor); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ChannelRecords("ATM"); len(got) != 0 {
		t.Fatalf("after remove: %v", got)
	}
	if err := s.RemoveChannel("ATM", "specs", chActor); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("remove missing: %v", err)
	}
}

func TestChannelEditPreservesUnknownPayloadFields(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	tk, err := s.CreateChannel("ATM", core.ChannelRecord{Name: "code", Type: core.ChannelTypeRepo, Address: core.ChannelAddress{URL: "git@x:y.git"}}, chActor)
	if err != nil {
		t.Fatal(err)
	}
	// simulate a newer binary's field
	m, _ := core.DecodeChannelPayload(mustTask(t, s, tk.ID).Meta[core.ChannelMetaKey])
	m["future_field"] = "kept"
	enc, _ := core.EncodeChannelPayload(m)
	if err := s.SetTaskCapabilityMeta(tk.ID, core.ChannelMetaKey, enc, chActor); err != nil {
		t.Fatal(err)
	}
	if err := s.EditChannel("ATM", "code", nil, &core.ChannelAddress{URL: "git@x:z.git"}, chActor); err != nil {
		t.Fatal(err)
	}
	m2, _ := core.DecodeChannelPayload(mustTask(t, s, tk.ID).Meta[core.ChannelMetaKey])
	if m2["future_field"] != "kept" {
		t.Fatal("edit dropped an unknown payload field")
	}
}

func mustTask(t *testing.T, s *Store, id string) *core.Task {
	t.Helper()
	tk, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	return tk
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run TestChannel -v`
Expected: FAIL (undefined `CreateChannel`, ...). If `newTestStore` does not exist under that name, adapt the test to this package's actual helper first — the failure must be the missing channel methods, not the harness.

- [ ] **Step 3: Write the implementation**

```go
// internal/store/channel.go
package store

import (
	"fmt"

	"atm/internal/core"
)

// channelTypeValid reports whether typ is a recognized channel type.
func channelTypeValid(typ string) bool {
	for _, t := range core.ChannelTypes {
		if t == typ {
			return true
		}
	}
	return false
}

// ChannelRecords lists the project's tier-1 channel records, decoding every
// task in the channel:* namespace. Tasks with unreadable payloads are skipped
// here (list degrades; Annotate and channelByName surface them).
func (s *Store) ChannelRecords(code string) ([]core.ChannelRecord, error) {
	tasks, err := s.ListTasksErr(core.QueryFilters{Project: code, Expr: "channel:*"})
	if err != nil {
		return nil, err
	}
	var out []core.ChannelRecord
	for _, t := range tasks {
		rec, err := core.ChannelFromTask(code, *t)
		if err != nil || rec == nil {
			continue
		}
		out = append(out, *rec)
	}
	return out, nil
}

// channelByName resolves a handle to its record; core.ErrNotFound when absent,
// and a decode error when the named channel's payload is unreadable.
func (s *Store) channelByName(code, name string) (*core.ChannelRecord, error) {
	tasks, err := s.ListTasksErr(core.QueryFilters{Project: code, Expr: "channel:*"})
	if err != nil {
		return nil, err
	}
	for _, t := range tasks {
		rec, err := core.ChannelFromTask(code, *t)
		if err != nil {
			return nil, err
		}
		if rec != nil && rec.Name == name {
			return rec, nil
		}
	}
	return nil, fmt.Errorf("%w: channel %q", core.ErrNotFound, name)
}

// CreateChannel authors the tier-1 record: a task titled by the handle,
// labelled channel:<type>, description = purpose, payload = name/type/address.
// The handle must be unique across the project's channels (all types).
func (s *Store) CreateChannel(code string, rec core.ChannelRecord, actor string) (*core.Task, error) {
	if rec.Name == "" || !channelTypeValid(rec.Type) {
		return nil, fmt.Errorf("%w: channel needs a name and a type in %v", core.ErrUsage, core.ChannelTypes)
	}
	existing, err := s.ChannelRecords(code)
	if err != nil {
		return nil, err
	}
	for _, e := range existing {
		if e.Name == rec.Name {
			return nil, fmt.Errorf("%w: channel %q already exists (task %s)", core.ErrUsage, rec.Name, e.TaskID)
		}
	}
	t, err := s.CreateTask(code, rec.Name, rec.Purpose, []string{core.ChannelLabel(code, rec.Type)}, actor)
	if err != nil {
		return nil, err
	}
	payload, err := core.EncodeChannelPayload(core.ChannelPayloadFrom(rec))
	if err != nil {
		return nil, err
	}
	if err := s.SetTaskCapabilityMeta(t.ID, core.ChannelMetaKey, payload, actor); err != nil {
		return nil, err
	}
	return s.GetTask(t.ID)
}

// EditChannel updates purpose and/or address. Address writes decode the
// existing payload and mutate only the address key, so unknown fields from
// newer binaries survive (degrade-never-reject applied to ourselves).
func (s *Store) EditChannel(code, name string, purpose *string, addr *core.ChannelAddress, actor string) error {
	rec, err := s.channelByName(code, name)
	if err != nil {
		return err
	}
	if purpose != nil {
		if err := s.SetDescription(rec.TaskID, *purpose, actor); err != nil {
			return err
		}
	}
	if addr != nil {
		t, err := s.GetTask(rec.TaskID)
		if err != nil {
			return err
		}
		m, err := core.DecodeChannelPayload(t.Meta[core.ChannelMetaKey])
		if err != nil {
			return err
		}
		next := *rec
		next.Address = *addr
		m["address"] = core.ChannelPayloadFrom(next)["address"]
		m["name"], m["type"] = rec.Name, rec.Type
		enc, err := core.EncodeChannelPayload(m)
		if err != nil {
			return err
		}
		if err := s.SetTaskCapabilityMeta(rec.TaskID, core.ChannelMetaKey, enc, actor); err != nil {
			return err
		}
	}
	return nil
}

// RemoveChannel removes the ledger record (task tombstone) and drops this
// machine's wiring entry for the handle, if any.
func (s *Store) RemoveChannel(code, name, actor string) error {
	rec, err := s.channelByName(code, name)
	if err != nil {
		return err
	}
	if err := s.RemoveTask(rec.TaskID, actor); err != nil {
		return err
	}
	return s.dropChannelWiring(code, name, actor)
}
```

`dropChannelWiring` lands in Task 3; for THIS task stub it as `func (s *Store) dropChannelWiring(code, name, actor string) error { return nil }` at the bottom of `channel.go` with a `// replaced in the wiring change` comment, so the task compiles and tests green on its own.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -run TestChannel -v` then `go test ./internal/store/`
Expected: PASS, and no existing store test broken.

- [ ] **Step 5: Commit**

```bash
git add internal/store/channel.go internal/store/channel_test.go
git commit -m "feat(ATM-097849): store channel record CRUD over the task substrate"
```

---

### Task 3: Store — tier-2 wiring (config.json) + stamps

**Files:**
- Modify: `internal/store/channel.go` (wiring methods, replace the `dropChannelWiring` stub)
- Modify: `internal/store/config.go` (extend `GetProjectConfig`'s emptiness check with `len(c.Channels) == 0`)
- Test: `internal/store/channel_test.go` (append)

**Interfaces:**
- Consumes: Task 1's `core.ChannelWiring`/`core.VerificationStamp`; Task 2's `channelByName`; existing `s.WithLock`, `s.GetProjectConfig`, `WriteFileAtomic(s.configPath(code), ...)`, `s.validateActor`, `core.RFC3339UTC(core.Now())` (exact pattern: `SetProjectRepo` at `internal/store/config.go:116`).
- Produces: `(*Store) SetChannelWiring(code, name, path, mcpServer, actor string) error` (merge semantics: non-empty args overwrite, empty args keep; path resolved absolute and must be an existing directory; stamps preserved), `(*Store) AddChannelStamp(code, name, note, actor string) error`, working `dropChannelWiring`.

- [ ] **Step 1: Write the failing test**

```go
func TestChannelWiringAndStamps(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateChannel("ATM", core.ChannelRecord{Name: "code", Type: core.ChannelTypeRepo, Address: core.ChannelAddress{URL: "git@x:y.git"}}, chActor); err != nil {
		t.Fatal(err)
	}
	// wiring an unknown channel fails: the ledger record must exist first
	if err := s.SetChannelWiring("ATM", "nope", t.TempDir(), "", chActor); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	// a path must exist
	if err := s.SetChannelWiring("ATM", "code", "/nonexistent/dir", "", chActor); !errors.Is(err, core.ErrUsage) {
		t.Fatalf("missing dir: %v", err)
	}
	dir := t.TempDir()
	if err := s.SetChannelWiring("ATM", "code", dir, "", chActor); err != nil {
		t.Fatal(err)
	}
	if err := s.AddChannelStamp("ATM", "code", "authorized as tu", chActor); err != nil {
		t.Fatal(err)
	}
	// merge: setting only mcp_server keeps path and stamps
	if err := s.SetChannelWiring("ATM", "code", "", "notion", chActor); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.GetProjectConfig("ATM")
	if err != nil || cfg == nil {
		t.Fatalf("config: %v %v", cfg, err)
	}
	w := cfg.Channels["code"]
	if w.Path != dir || w.MCPServer != "notion" || len(w.Stamps) != 1 || w.Stamps[0].By != chActor || w.Stamps[0].Note != "authorized as tu" {
		t.Fatalf("wiring: %+v", w)
	}
	// removing the channel drops its wiring
	if err := s.RemoveChannel("ATM", "code", chActor); err != nil {
		t.Fatal(err)
	}
	cfg, _ = s.GetProjectConfig("ATM")
	if cfg != nil {
		if _, ok := cfg.Channels["code"]; ok {
			t.Fatal("wiring survived channel removal")
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestChannelWiring -v`
Expected: FAIL (undefined `SetChannelWiring`)

- [ ] **Step 3: Write the implementation**

Append to `internal/store/channel.go` (imports gain `"os"`, `"path/filepath"`):

```go
// SetChannelWiring records how THIS machine reaches the channel — tier 2:
// config, not substrate, no event, no secrets. Merge semantics: a non-empty
// path or mcpServer overwrites that field, an empty one keeps the existing
// value, and stamps always survive. The channel must exist in the ledger.
func (s *Store) SetChannelWiring(code, name, path, mcpServer, actor string) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	if _, err := s.channelByName(code, name); err != nil {
		return err
	}
	abs := ""
	if path != "" {
		var err error
		abs, err = filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("resolve channel path: %w", err)
		}
		if info, err := os.Stat(abs); err != nil || !info.IsDir() {
			return fmt.Errorf("%w: channel path does not exist or is not a directory: %s", core.ErrUsage, abs)
		}
	}
	return s.WithLock(code, func() error {
		merged, err := s.lockedProjectConfig(code)
		if err != nil {
			return err
		}
		if merged.Channels == nil {
			merged.Channels = map[string]core.ChannelWiring{}
		}
		w := merged.Channels[name]
		if abs != "" {
			w.Path = abs
		}
		if mcpServer != "" {
			w.MCPServer = mcpServer
		}
		merged.Channels[name] = w
		merged.UpdatedAt = core.RFC3339UTC(core.Now())
		merged.UpdatedBy = actor
		return WriteFileAtomic(s.configPath(code), merged)
	})
}

// AddChannelStamp appends a verification stamp (actor + timestamp + note) to
// the channel's wiring: "someone actually touched this channel and vouches".
func (s *Store) AddChannelStamp(code, name, note, actor string) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	if _, err := s.channelByName(code, name); err != nil {
		return err
	}
	return s.WithLock(code, func() error {
		merged, err := s.lockedProjectConfig(code)
		if err != nil {
			return err
		}
		if merged.Channels == nil {
			merged.Channels = map[string]core.ChannelWiring{}
		}
		w := merged.Channels[name]
		w.Stamps = append(w.Stamps, core.VerificationStamp{At: core.RFC3339UTC(core.Now()), By: actor, Note: note})
		merged.Channels[name] = w
		merged.UpdatedAt = core.RFC3339UTC(core.Now())
		merged.UpdatedBy = actor
		return WriteFileAtomic(s.configPath(code), merged)
	})
}

// lockedProjectConfig is the read half of every config read-modify-write in
// this file: current config or a fresh zero value. Call under WithLock only.
func (s *Store) lockedProjectConfig(code string) (*ProjectConfig, error) {
	existing, err := s.GetProjectConfig(code)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}
	return &ProjectConfig{}, nil
}

// dropChannelWiring removes the handle's wiring entry, silently succeeding
// when there is none (RemoveChannel's local cleanup).
func (s *Store) dropChannelWiring(code, name, actor string) error {
	return s.WithLock(code, func() error {
		merged, err := s.lockedProjectConfig(code)
		if err != nil {
			return err
		}
		if _, ok := merged.Channels[name]; !ok {
			return nil
		}
		delete(merged.Channels, name)
		merged.UpdatedAt = core.RFC3339UTC(core.Now())
		merged.UpdatedBy = actor
		return WriteFileAtomic(s.configPath(code), merged)
	})
}
```

(Delete the Task 2 stub for `dropChannelWiring`.) In `internal/store/config.go`, extend `GetProjectConfig`'s "all-zero means nil" check (line ~18) with `&& len(c.Channels) == 0`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store/channel.go internal/store/config.go internal/store/channel_test.go
git commit -m "feat(ATM-097849): tier-2 channel wiring and verification stamps in project config"
```

---

### Task 4: Store — probes, joined views, repo migration; `core.ChannelService`

**Files:**
- Create: `internal/store/channel_probe.go`
- Modify: `internal/store/channel.go` (`ProjectChannels`, `GetChannelByName`, `MigrateReposToChannels`)
- Modify: `internal/core/service.go` (add `ChannelService`, include in `Service`)
- Test: `internal/store/channel_probe_test.go`, `internal/store/channel_test.go` (append)

**Interfaces:**
- Consumes: Tasks 1-3; `core.RepoConfig`; existing `s.ProjectRepos`.
- Produces: `(*Store) ProjectChannels(code string) ([]core.ChannelView, error)` (sorted by name; Wiring/Probe joined), `(*Store) GetChannelByName(code, name string) (*core.ChannelView, error)`, `(*Store) MigrateReposToChannels(code, actor string) (int, error)`, `probeRepoPath(path string) *core.ChannelProbe` (unexported), and the new interface consumed by cli/tui:

```go
// added to internal/core/service.go
type ChannelService interface {
	CreateChannel(code string, rec ChannelRecord, actor string) (*Task, error)
	EditChannel(code, name string, purpose *string, addr *ChannelAddress, actor string) error
	RemoveChannel(code, name, actor string) error
	ChannelRecords(code string) ([]ChannelRecord, error)
	ProjectChannels(code string) ([]ChannelView, error)
	GetChannelByName(code, name string) (*ChannelView, error)
	SetChannelWiring(code, name, path, mcpServer, actor string) error
	AddChannelStamp(code, name, note, actor string) error
	MigrateReposToChannels(code, actor string) (int, error)
}
```

and `ChannelService` added to the `Service` composite interface list.

- [ ] **Step 1: Write the failing probe test**

```go
// internal/store/channel_probe_test.go
package store

import (
	"os/exec"
	"testing"
)

// gitDir builds a git fixture; skips when git is unavailable.
func gitDir(t *testing.T, commands ...[]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(cmd.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("commit", "--allow-empty", "-q", "-m", "init")
	for _, c := range commands {
		run(c...)
	}
	return dir
}

func TestProbeRepoPath(t *testing.T) {
	if p := probeRepoPath("/nonexistent/dir"); p.PathExists {
		t.Fatal("missing dir must probe PathExists=false")
	}
	plain := t.TempDir()
	if p := probeRepoPath(plain); !p.PathExists || p.IsGitRepo {
		t.Fatalf("plain dir: %+v", p)
	}
	clean := gitDir(t)
	if p := probeRepoPath(clean); !p.IsGitRepo || p.Dirty || p.HasUpstream {
		t.Fatalf("clean repo: %+v", p)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestProbeRepoPath -v`
Expected: FAIL (undefined `probeRepoPath`)

- [ ] **Step 3: Implement the probe**

```go
// internal/store/channel_probe.go
package store

import (
	"os"
	"os/exec"
	"strconv"
	"strings"

	"atm/internal/core"
)

// probeRepoPath runs the cheap, strictly-local repo checks: existence, git
// repo, dirty tree, and ahead/behind against the ALREADY-FETCHED upstream
// tracking ref. It never fetches, never touches the network, and degrades a
// failed check to its zero value — a probe is a status light, not a gate.
func probeRepoPath(path string) *core.ChannelProbe {
	p := &core.ChannelProbe{}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return p
	}
	p.PathExists = true
	git := func(args ...string) (string, error) {
		out, err := exec.Command("git", append([]string{"-C", path}, args...)...).Output()
		return strings.TrimSpace(string(out)), err
	}
	if out, err := git("rev-parse", "--is-inside-work-tree"); err != nil || out != "true" {
		return p
	}
	p.IsGitRepo = true
	if out, err := git("status", "--porcelain"); err == nil && out != "" {
		p.Dirty = true
	}
	if out, err := git("rev-list", "--left-right", "--count", "@{upstream}...HEAD"); err == nil {
		if parts := strings.Fields(out); len(parts) == 2 {
			p.HasUpstream = true
			p.Behind, _ = strconv.Atoi(parts[0])
			p.Ahead, _ = strconv.Atoi(parts[1])
		}
	}
	return p
}
```

- [ ] **Step 4: Run probe tests**

Run: `go test ./internal/store/ -run TestProbeRepoPath -v`
Expected: PASS

- [ ] **Step 5: Write the failing view + migration tests**

Append to `internal/store/channel_test.go`:

```go
func TestProjectChannelsJoinsWiringAndProbe(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateChannel("ATM", core.ChannelRecord{Name: "code", Type: core.ChannelTypeRepo}, chActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateChannel("ATM", core.ChannelRecord{Name: "specs", Type: core.ChannelTypeNotion}, chActor); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := s.SetChannelWiring("ATM", "code", dir, "", chActor); err != nil {
		t.Fatal(err)
	}
	views, err := s.ProjectChannels("ATM")
	if err != nil || len(views) != 2 {
		t.Fatalf("views: %v %v", views, err)
	}
	// sorted by name: code, specs
	if views[0].Name != "code" || views[0].Wiring == nil || views[0].Probe == nil || !views[0].Probe.PathExists {
		t.Fatalf("repo view: %+v", views[0])
	}
	if views[1].Name != "specs" || views[1].Wiring != nil || views[1].Probe != nil {
		t.Fatalf("notion view without wiring: %+v", views[1])
	}
	v, err := s.GetChannelByName("ATM", "code")
	if err != nil || v.Probe == nil {
		t.Fatalf("by name: %v %v", v, err)
	}
}

func TestMigrateReposToChannels(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := s.SetProjectRepo("ATM", "atm", dir, "git@github.com:TranDuongTu/atm.git", chActor); err != nil {
		t.Fatal(err)
	}
	n, err := s.MigrateReposToChannels("ATM", chActor)
	if err != nil || n != 1 {
		t.Fatalf("migrated %d, %v", n, err)
	}
	v, err := s.GetChannelByName("ATM", "atm")
	if err != nil || v.Type != core.ChannelTypeRepo || v.Address.URL != "git@github.com:TranDuongTu/atm.git" || v.Wiring == nil || v.Wiring.Path != dir {
		t.Fatalf("migrated channel: %+v err %v", v, err)
	}
	if repos, _ := s.ProjectRepos("ATM"); len(repos) != 0 {
		t.Fatalf("legacy repos not cleared: %v", repos)
	}
	// idempotent: second run migrates nothing and does not error on the existing handle
	if n, err := s.MigrateReposToChannels("ATM", chActor); err != nil || n != 0 {
		t.Fatalf("re-run: %d %v", n, err)
	}
}
```

- [ ] **Step 6: Run to verify failure, then implement views + migration**

Run: `go test ./internal/store/ -run 'TestProjectChannels|TestMigrate' -v` → FAIL. Then append to `internal/store/channel.go` (imports gain `"sort"`):

```go
// ProjectChannels is the joined read every surface consumes: tier-1 records
// + this machine's tier-2 wiring + local probes, sorted by handle. Probes run
// only where there is something local to probe (a repo channel with a path).
func (s *Store) ProjectChannels(code string) ([]core.ChannelView, error) {
	recs, err := s.ChannelRecords(code)
	if err != nil {
		return nil, err
	}
	cfg, err := s.GetProjectConfig(code)
	if err != nil {
		return nil, err
	}
	var wirings map[string]core.ChannelWiring
	if cfg != nil {
		wirings = cfg.Channels
	}
	out := make([]core.ChannelView, 0, len(recs))
	for _, rec := range recs {
		v := core.ChannelView{ChannelRecord: rec}
		if w, ok := wirings[rec.Name]; ok {
			wc := w
			v.Wiring = &wc
		}
		if rec.Type == core.ChannelTypeRepo && v.Wiring != nil && v.Wiring.Path != "" {
			v.Probe = probeRepoPath(v.Wiring.Path)
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// GetChannelByName is ProjectChannels narrowed to one handle — the CLI's
// `atm channel show` read and the agent endpoint's shape.
func (s *Store) GetChannelByName(code, name string) (*core.ChannelView, error) {
	views, err := s.ProjectChannels(code)
	if err != nil {
		return nil, err
	}
	for i := range views {
		if views[i].Name == name {
			return &views[i], nil
		}
	}
	return nil, fmt.Errorf("%w: channel %q", core.ErrNotFound, name)
}

// MigrateReposToChannels lifts every legacy RepoConfig into a repo channel:
// tier-1 record (handle, URL; purpose left for the concierge to author) plus
// tier-2 wiring (path), then clears the legacy repos list. Handles that
// already exist as channels are skipped, so a re-run is safe. Returns the
// number migrated.
func (s *Store) MigrateReposToChannels(code, actor string) (int, error) {
	repos, err := s.ProjectRepos(code)
	if err != nil {
		return 0, err
	}
	existing, err := s.ChannelRecords(code)
	if err != nil {
		return 0, err
	}
	taken := make(map[string]bool, len(existing))
	for _, e := range existing {
		taken[e.Name] = true
	}
	n := 0
	for _, r := range repos {
		if taken[r.Name] {
			continue
		}
		if _, err := s.CreateChannel(code, core.ChannelRecord{Name: r.Name, Type: core.ChannelTypeRepo, Address: core.ChannelAddress{URL: r.URL}}, actor); err != nil {
			return n, err
		}
		if err := s.SetChannelWiring(code, r.Name, r.Path, "", actor); err != nil {
			return n, err
		}
		n++
	}
	if len(repos) > 0 {
		if err := s.WithLock(code, func() error {
			merged, err := s.lockedProjectConfig(code)
			if err != nil {
				return err
			}
			merged.Repos = nil
			merged.UpdatedAt = core.RFC3339UTC(core.Now())
			merged.UpdatedBy = actor
			return WriteFileAtomic(s.configPath(code), merged)
		}); err != nil {
			return n, err
		}
	}
	return n, nil
}
```

Then add `ChannelService` to `internal/core/service.go` exactly as in this task's Interfaces block, and add `ChannelService` to the `Service` composite.

- [ ] **Step 7: Run the full store + core suites**

Run: `go test ./internal/store/ ./internal/core/`
Expected: PASS (the `Service` addition compiles because `*Store` now implements every method structurally — if anything is missing the build tells you exactly what).

- [ ] **Step 8: Commit**

```bash
git add internal/store/channel.go internal/store/channel_probe.go internal/store/channel_probe_test.go internal/store/channel_test.go internal/core/service.go
git commit -m "feat(ATM-097849): channel probes, joined views, repo migration, ChannelService seam"
```

---

### Task 5: The `channel` capability package + guide + registration

**Files:**
- Create: `internal/capability/channel/vocabulary.go`, `internal/capability/channel/guide.go`, `internal/capability/channel/annotate.go`, `internal/capability/channel/command.go`
- Create: `internal/capability/channel/vocabulary_test.go`, `internal/capability/channel/annotate_test.go`, `internal/capability/channel/guide_skills_test.go`
- Create: `skills/capability/channel.md`
- Modify: `cmd/atm/main.go:20-22` (register `channel.New()`)
- Modify: `tests/arch/imports_test.go:99` and `:163` (add `"internal/capability/channel"` to both directory lists)

**Interfaces:**
- Consumes: `capability.Capability`/`capability.Env`/`capability.Cell` contract (`internal/capability/capability.go:68`), `core.ChannelFromTask`, `core.ChannelLabel`, `skills.MustCapability` (pattern: `internal/capability/workflowai/guide.go`).
- Produces: `channel.New() capability.Capability`, `channel.CapabilityName = "channel"`, `channel.Vocabulary(code)`, `channel.BoardChannels(code) string`. The cobra tree under `atm capability channel` carries only `seed` (the registry mounts `guide` itself); the working verbs are the top-level `atm channel` noun (Task 6).

- [ ] **Step 1: Write the failing tests**

```go
// internal/capability/channel/vocabulary_test.go
package channel

import "testing"

func TestVocabularyShape(t *testing.T) {
	v := Vocabulary("ATM")
	names := map[string]string{}
	for _, l := range v {
		names[l.Name] = l.Expr
	}
	if _, ok := names["ATM:channel:*"]; !ok {
		t.Fatal("missing namespace descriptor")
	}
	if _, ok := names["ATM:channel:repo"]; !ok {
		t.Fatal("missing repo type label")
	}
	if _, ok := names["ATM:channel:notion"]; !ok {
		t.Fatal("missing notion type label")
	}
	if expr := names["ATM:channels"]; expr != "channel:*" {
		t.Fatalf("channels board expr = %q", expr)
	}
	if len(Exposed("ATM")) != 1 || Exposed("ATM")[0].Name != "ATM:channels" {
		t.Fatalf("exposed: %v", Exposed("ATM"))
	}
}
```

```go
// internal/capability/channel/annotate_test.go
package channel

import (
	"testing"

	"atm/internal/core"
)

func TestAnnotate(t *testing.T) {
	c := Cap{}
	if cell := c.Annotate(core.Task{ID: "t", Labels: []string{"ATM:status:open"}}); cell != nil {
		t.Fatalf("non-channel task: %v", cell)
	}
	payload, _ := core.EncodeChannelPayload(core.ChannelPayloadFrom(core.ChannelRecord{Name: "specs", Type: core.ChannelTypeNotion}))
	cell := c.Annotate(core.Task{ID: "ATM-1", Labels: []string{"ATM:channel:notion"}, Meta: map[string]string{core.ChannelMetaKey: payload}})
	if cell == nil || cell.Text != "channel specs · notion" {
		t.Fatalf("cell: %+v", cell)
	}
	bad := c.Annotate(core.Task{ID: "ATM-2", Labels: []string{"ATM:channel:repo"}, Meta: map[string]string{core.ChannelMetaKey: "garbage"}})
	if bad == nil || bad.Tone != 2 { // capability.ToneAttention
		t.Fatalf("unreadable payload must degrade to an attention cell: %+v", bad)
	}
}
```

For `guide_skills_test.go`, copy `internal/capability/workflowai/guide_skills_test.go` verbatim and change the capability name to `channel` — it pins the skill file's frontmatter `labels:`/`boards:` against the Go vocabulary.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/capability/channel/ -v`
Expected: FAIL (package does not exist)

- [ ] **Step 3: Implement the package**

```go
// internal/capability/channel/vocabulary.go
package channel

import "atm/internal/core"

const CapabilityName = "channel"

// BoardChannels is the one board this capability owns: every channel record.
func BoardChannels(code string) string { return code + ":channels" }

// vocabulary is the single literal list every contract method derives from.
func vocabulary(code string) []core.Label {
	return []core.Label{
		{Name: code + ":channel:*", Description: "channel records: how personas communicate with each other and humans. Each member task is one channel — its title is the handle, its description the purpose, and its payload the type-shaped address. Managed by `atm channel`; do not hand-edit the payload."},
		{Name: core.ChannelLabel(code, core.ChannelTypeRepo), Description: "a git repository channel: the address is the remote URL; this machine's clone path lives in local wiring (config, not synced)"},
		{Name: core.ChannelLabel(code, core.ChannelTypeNotion), Description: "a Notion channel: the address names the workspace and database/page; agents reach it through their own MCP server, never through ATM, and ATM stores no credentials"},
		{Name: BoardChannels(code), Description: "every channel record in the project. Browse channels with `atm channel list` or the TUI channels overlay; this board exists so queries and other boards can see them.", Expr: "channel:*"},
	}
}

// Vocabulary returns every label this capability owns for code. Pure.
func Vocabulary(code string) []core.Label { return vocabulary(code) }

// Exposed returns the ring surface: exactly the channels board.
func Exposed(code string) []core.Label {
	for _, l := range vocabulary(code) {
		if l.Name == BoardChannels(code) {
			return []core.Label{l}
		}
	}
	return nil
}

// EnsureVocabulary seeds the labels and board in one LabelSeedBatch
// transaction and returns the board labels it owns.
func EnsureVocabulary(s core.LabelService, code, actor string) ([]core.Label, error) {
	vocab := vocabulary(code)
	if err := s.LabelSeedBatch(vocab, actor); err != nil {
		return nil, err
	}
	var boards []core.Label
	for _, l := range vocab {
		if l.Expr != "" {
			boards = append(boards, l)
		}
	}
	return boards, nil
}
```

```go
// internal/capability/channel/guide.go
package channel

import "atm/skills"

func (Cap) Summary() string { return skills.MustCapability(CapabilityName).Description }
func (Cap) Guide() string   { return skills.MustCapability(CapabilityName).Body }
```

```go
// internal/capability/channel/annotate.go
package channel

import (
	"atm/internal/capability"
	"atm/internal/core"
	"strings"
)

type Cap struct{}

// Annotate renders the channel cell for a channel task: handle + type, or an
// attention cell when the payload is unreadable (degrade, never panic).
func (Cap) Annotate(t core.Task) *capability.Cell {
	code, _, ok := strings.Cut(t.ID, "-")
	if !ok {
		return nil
	}
	rec, err := core.ChannelFromTask(code, t)
	if err != nil {
		return &capability.Cell{Text: "channel: unreadable payload", Tone: capability.ToneAttention}
	}
	if rec == nil {
		return nil
	}
	return &capability.Cell{Text: "channel " + rec.Name + " · " + rec.Type, Tone: capability.ToneNeutral}
}
```

```go
// internal/capability/channel/command.go
package channel

import (
	"fmt"

	"atm/internal/capability"
	"atm/internal/core"

	"github.com/spf13/cobra"
)

// New returns the capability the composition root registers.
func New() capability.Capability { return Cap{} }

func (Cap) Name() string                        { return CapabilityName }
func (Cap) Vocabulary(code string) []core.Label { return Vocabulary(code) }
func (Cap) Exposed(code string) []core.Label    { return Exposed(code) }

func (Cap) EnsureVocabulary(svc core.LabelService, code, actor string) ([]core.Label, error) {
	return EnsureVocabulary(svc, code, actor)
}

// Command mounts only seed here: the working verbs are the top-level
// `atm channel` noun (a deliberate facade — channels are managed as
// channels, not as capability plumbing). The registry adds `guide` itself.
func (Cap) Command(env capability.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   CapabilityName,
		Short: "Channels: how personas communicate — repositories, Notion; managed via the top-level `atm channel` noun",
	}
	var project string
	seed := &cobra.Command{
		Use:   "seed",
		Short: "Ensure the channel vocabulary and board exist for a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := env.RequireMutatingActor()
			if err != nil {
				return err
			}
			svc, err := env.OpenService()
			if err != nil {
				return err
			}
			if _, err := svc.GetProject(project); err != nil {
				return fmt.Errorf("project %q: %w", project, err)
			}
			if _, err := EnsureVocabulary(svc, project, actor); err != nil {
				return err
			}
			return env.Emit(map[string]any{"project": project}, func() {
				fmt.Fprintf(env.Stdout(), "ensured channel vocabulary for %s\n", project)
			})
		},
	}
	seed.Flags().StringVar(&project, "project", "", "project code")
	_ = seed.MarkFlagRequired("project")
	env.BindActorFlag(cmd)
	cmd.AddCommand(seed)
	return cmd
}
```

Create `skills/capability/channel.md` (single un-wrapped prose lines):

```markdown
---
name: channel
description: Channels — repositories, Notion, and future surfaces personas communicate through; ledger-recorded identity and address, machine-local wiring, agent-side I/O.
labels: [channel:*]
boards: [channels]
---
# channel capability — agent guide

A channel is how personas communicate with each other and with humans: a git repository, a Notion database, later Slack. ATM is the phone book, not the switchboard: it records what a channel is and how to address it, and agents do all channel I/O themselves through their own tools (git, MCP servers). The concierge is the point of contact for setting channels up and re-establishing them on new machines.

## Semantics

Three tiers. Tier 1, the ledger record (synced): a task labelled `channel:<type>` whose title is the unique handle, whose description is the purpose, and whose `channel` metadata payload carries the type-shaped address (repo: remote URL; notion: workspace + database/page). Addresses are not credentials. Tier 2, local wiring (`config.json`, this machine only, never synced): the repo clone path, the MCP server name agents here use, and verification stamps (actor + time + note) recorded when someone actually touches the channel. Tier 3 does not exist: ATM stores no tokens, passwords, or credentials, anywhere — authorization lives with the agent's own tooling (e.g. the Notion MCP server's OAuth store).

Channel records are tasks only as plumbing: manage them exclusively through `atm channel`, never through raw task verbs. The `channels` board exists so queries can see them; workflow boards filter on their own namespaces and never show channels.

## Actions

- `atm channel add --project <CODE> --name <handle> --type repo|notion --purpose "..." [--url <u>] [--workspace <w>] [--database <id>] [--page <id>]` — author the ledger record.
- `atm channel list --project <CODE>` / `atm channel show --project <CODE> --name <handle>` — the agent endpoint: with `--output json`, returns identity, purpose, address, this machine's wiring, and probe results. Resolve a channel once, then do I/O directly through your own tools.
- `atm channel edit --project <CODE> --name <handle> [--purpose "..."] [address flags]` / `atm channel remove --project <CODE> --name <handle>` — tier-1 updates.
- `atm channel wire --project <CODE> --name <handle> [--path <dir>] [--mcp-server <name>]` — record how THIS machine reaches the channel.
- `atm channel stamp --project <CODE> --name <handle> --note "..."` — vouch that you actually reached the channel just now; stamps age into the status display.
- `atm channel migrate-repos --project <CODE>` — one-shot lift of legacy repo dispatch targets into repo channels.
- `atm capability channel seed --project <CODE>` — idempotently ensure vocabulary and board.

## Converge

Every place personas exchange work is recorded as a channel with an honest purpose; addresses live in the ledger so a new machine can rehydrate. On this machine, every channel an agent needs is wired (path or MCP server) and carries a reasonably fresh stamp; stale stamps mean dispatch a concierge session, which reads the ledger records, re-wires, walks the human through agent-side MCP auth, and re-stamps. After using a channel successfully, refresh its stamp. Never write credentials into any ATM surface — a channel needing auth is set up in the agent's own tooling, and ATM only records that it happened.
```

Register in `cmd/atm/main.go`: add `"atm/internal/capability/channel"` to the imports and change line 22 to `capability.NewRegistry(workflow.New(), contextmap.New(), workflowai.New(), channel.New())`. Add `"internal/capability/channel"` to the two lists in `tests/arch/imports_test.go` (lines 99 and 163).

- [ ] **Step 4: Run tests**

Run: `go test ./internal/capability/... ./tests/arch/ ./skills/ && go build ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/capability/channel/ skills/capability/channel.md cmd/atm/main.go tests/arch/imports_test.go
git commit -m "feat(ATM-097849): channel capability package, guide, registration"
```

---

### Task 6: CLI — the `atm channel` noun (add/list/show/edit/remove)

**Files:**
- Create: `internal/cli/channel.go`
- Create: `internal/cli/channel_test.go`
- Modify: `internal/cli/root.go` (mount `newChannelCmd(st)` alongside the other top-level nouns — grep for `newProjectCmd(st)` and add at that mount site)

**Interfaces:**
- Consumes: Task 4's `core.ChannelService` via `st.openStore()` (returns the service; see `newProjectRepoCmd` in `internal/cli/project.go:606` for the exact cliState idioms: `st.resolveActor(true)`, `st.emit`, `st.isJSON()`, `bindActorFlag`).
- Produces: `newChannelCmd(st *cliState) *cobra.Command` mounting `add`, `list`, `show`, `edit`, `remove` (Task 7 appends `wire`, `stamp`, `migrate-repos`); helper `channelProject(cmd) (string, error)` (flag `--project` falling back to `ATM_PROJECT`); helper `requireChannelCapability(s core.Service, project string) error` (nil `Capabilities` = legacy = enabled; otherwise the list must contain `"channel"`, else a `core.ErrUsage` error naming the enable command).

- [ ] **Step 1: Write the failing test**

Mirror this package's existing CLI test harness (grep `internal/cli/project_test.go` or the nearest `_test.go` that builds a root command against a temp store and captures stdout; reuse its helpers instead of inventing new ones). Cases:

```go
// internal/cli/channel_test.go — shapes, adapt to the package's harness helpers
func TestChannelAddListShowJSON(t *testing.T) {
	// setup: temp store, project ATM created, capability set left nil (legacy = all enabled)
	// run: atm channel add --project ATM --name specs --type notion --purpose "specs here" --workspace acme --database abc123 --actor developer@test:unit
	// assert exit 0, stdout contains "created channel specs"
	// run: atm channel list --project ATM --output json
	// assert JSON array of one view: name specs, type notion, address.database abc123
	// run: atm channel show --project ATM --name specs --output json
	// assert single view JSON with purpose "specs here"
}

func TestChannelGateWhenCapabilityDisabled(t *testing.T) {
	// setup: project with Capabilities explicitly ["workflow"] (no channel)
	// run: atm channel list --project ATM
	// assert error mentioning `atm project capability add --project ATM --name channel`
}

func TestChannelAddValidation(t *testing.T) {
	// run: atm channel add --project ATM --name x --type slack ... → usage error listing repo|notion
	// run: atm channel add without --name → usage error
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/cli/ -run TestChannel -v`
Expected: FAIL (undefined `newChannelCmd` once the test names it, or command-not-found through the harness)

- [ ] **Step 3: Implement**

```go
// internal/cli/channel.go
package cli

import (
	"fmt"
	"os"

	"atm/internal/core"

	"github.com/spf13/cobra"
)

// newChannelCmd is the first-class channel noun. Channels are labelled tasks
// plus local wiring underneath (see the channel capability guide), but they
// are managed as channels: this group is the only sanctioned write path.
func newChannelCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "channel",
		Short: "Manage the project's channels (repositories, Notion) — ledger identity, local wiring, status",
		Long: "A channel is how personas communicate: a repository, a Notion database. " +
			"The ledger holds identity, purpose, and address (synced; never credentials); " +
			"local wiring in config.json holds this machine's path or MCP server name " +
			"(not synced); secrets live only in agent-side tooling. `--output json` on " +
			"list/show is the agent endpoint.",
	}
	bindActorFlag(cmd, st)
	cmd.AddCommand(newChannelAddCmd(st))
	cmd.AddCommand(newChannelListCmd(st))
	cmd.AddCommand(newChannelShowCmd(st))
	cmd.AddCommand(newChannelEditCmd(st))
	cmd.AddCommand(newChannelRemoveCmd(st))
	return cmd
}

// channelProject resolves the target project: --project flag, else ATM_PROJECT.
func channelProject(cmd *cobra.Command) (string, error) {
	p, _ := cmd.Flags().GetString("project")
	if p == "" {
		p = os.Getenv("ATM_PROJECT")
	}
	if p == "" {
		return "", fmt.Errorf("%w: --project is required (or ATM_PROJECT)", core.ErrUsage)
	}
	return p, nil
}

// requireChannelCapability gates the noun on the project's enabled set. A nil
// Capabilities list is a legacy project: all built-ins read as enabled.
func requireChannelCapability(s core.Service, project string) error {
	p, err := s.GetProject(project)
	if err != nil {
		return err
	}
	if p.Capabilities == nil {
		return nil
	}
	for _, n := range p.Capabilities {
		if n == "channel" {
			return nil
		}
	}
	return fmt.Errorf("%w: capability \"channel\" is not enabled for project %s (enable with: atm project capability add --project %s --name channel)", core.ErrUsage, project, project)
}
```

Each verb follows the `newProjectRepoCmd` idiom exactly. `add`:

```go
func newChannelAddCmd(st *cliState) *cobra.Command {
	var typ, name, purpose, url, workspace, database, page string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Author a channel's ledger record (identity + purpose + address; synced, never credentials)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := channelProject(cmd)
			if err != nil {
				return err
			}
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := requireChannelCapability(s, project); err != nil {
				return err
			}
			rec := core.ChannelRecord{Name: name, Type: typ, Purpose: purpose, Address: core.ChannelAddress{URL: url, Workspace: workspace, Database: database, Page: page}}
			tk, err := s.CreateChannel(project, rec, actor)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "name": name, "type": typ, "task": tk.ID}, func() {
				fmt.Fprintf(st.stdout(), "created channel %s (%s, task %s)\n", name, typ, tk.ID)
			})
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	cmd.Flags().StringVar(&name, "name", "", "unique channel handle")
	cmd.Flags().StringVar(&typ, "type", "", "channel type: repo|notion")
	cmd.Flags().StringVar(&purpose, "purpose", "", "what this channel is for (the searchable narrative)")
	cmd.Flags().StringVar(&url, "url", "", "repo: remote URL")
	cmd.Flags().StringVar(&workspace, "workspace", "", "notion: workspace")
	cmd.Flags().StringVar(&database, "database", "", "notion: database id")
	cmd.Flags().StringVar(&page, "page", "", "notion: parent page id")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("type")
	return cmd
}
```

`list` emits `s.ProjectChannels(project)` (`writeJSON` when `st.isJSON()`, else one line per channel: `name\ttype\tstatus-ish summary` — wired/unwired, stamp age); `show` emits `s.GetChannelByName(project, name)`; `edit` passes `--purpose` (as `*string`, only when the flag changed: use `cmd.Flags().Changed("purpose")`) and an address built from the same four flags (as `*core.ChannelAddress`, only when any address flag changed); `remove` calls `s.RemoveChannel`. All five call `requireChannelCapability` first; reads use `st.resolveActor(false)`. Mount `cmd.AddCommand(newChannelCmd(st))` in `root.go` next to the other top-level nouns.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/cli/ -run TestChannel -v && go test ./internal/cli/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/channel.go internal/cli/channel_test.go internal/cli/root.go
git commit -m "feat(ATM-097849): atm channel noun — add/list/show/edit/remove"
```

---

### Task 7: CLI — wire/stamp/migrate-repos; retire `atm project repo`

**Files:**
- Modify: `internal/cli/channel.go` (three more verbs)
- Modify: `internal/cli/project.go:606-709` (`newProjectRepoCmd` verbs become deprecation pointers)
- Test: `internal/cli/channel_test.go` (append), adjust any existing `atm project repo` tests to expect the pointer error

**Interfaces:**
- Consumes: Task 4's `SetChannelWiring`, `AddChannelStamp`, `MigrateReposToChannels`.
- Produces: `atm channel wire --project <p> --name <n> [--path <dir>] [--mcp-server <s>]`, `atm channel stamp --project <p> --name <n> --note <text>`, `atm channel migrate-repos --project <p>`; `atm project repo add|list|remove` now return an error: `repo dispatch targets moved to channels: use 'atm channel add/wire' (one-shot migration: atm channel migrate-repos --project <CODE>)`.

- [ ] **Step 1: Write the failing tests**

Append cases to `internal/cli/channel_test.go` (same harness):

```go
func TestChannelWireStampAndShowStatus(t *testing.T) {
	// setup: project + `atm channel add --name code --type repo --url git@x:y.git`
	// run: atm channel wire --project ATM --name code --path <t.TempDir()>
	// run: atm channel stamp --project ATM --name code --note "cloned and verified"
	// run: atm channel show --project ATM --name code --output json
	// assert JSON has wiring.path set, wiring.stamps[0].note == "cloned and verified", probe.path_exists == true
}

func TestChannelMigrateRepos(t *testing.T) {
	// setup: project; seed a legacy repo via the STORE (s.SetProjectRepo), since the CLI verb is retired
	// run: atm channel migrate-repos --project ATM
	// assert stdout reports "migrated 1"
	// run: atm channel list --project ATM --output json → one repo channel with wiring
}

func TestProjectRepoVerbsPointToChannels(t *testing.T) {
	// run: atm project repo list --project ATM
	// assert non-zero exit and the error mentions "atm channel" and "migrate-repos"
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/cli/ -run 'TestChannelWire|TestChannelMigrate|TestProjectRepoVerbs' -v`
Expected: FAIL

- [ ] **Step 3: Implement**

`wire` (require at least one of the two flags), `stamp` (`--note` required), `migrate-repos`:

```go
func newChannelMigrateCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate-repos",
		Short: "Lift legacy repo dispatch targets into repo channels (idempotent; concierge confirms purpose later)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := channelProject(cmd)
			if err != nil {
				return err
			}
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := requireChannelCapability(s, project); err != nil {
				return err
			}
			n, err := s.MigrateReposToChannels(project, actor)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "migrated": n}, func() {
				fmt.Fprintf(st.stdout(), "migrated %d repo(s) into channels; author each channel's purpose with `atm channel edit`\n", n)
			})
		},
	}
	cmd.Flags().String("project", "", "project code (or ATM_PROJECT)")
	return cmd
}
```

In `newProjectRepoCmd`, replace each verb's `RunE` body with the pointer error (keep the commands mounted so old muscle memory gets a direction, not a cobra unknown-command error):

```go
var errRepoVerbRetired = fmt.Errorf("%w: repo dispatch targets moved to channels: use 'atm channel add/wire' (one-shot migration: atm channel migrate-repos --project <CODE>)", core.ErrUsage)
```

and each `RunE: func(cmd *cobra.Command, args []string) error { return errRepoVerbRetired }`. Update the group's `Short`/`Long` to say retired. Delete or update any existing tests that exercised the old behavior (`grep -rn "project repo" internal/cli/*_test.go`) to expect the pointer.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/cli/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/channel.go internal/cli/channel_test.go internal/cli/project.go
git commit -m "feat(ATM-097849): channel wire/stamp/migrate-repos; retire atm project repo verbs"
```

---

### Task 8: TUI — dispatch dialog reads repo channels

**Files:**
- Modify: `internal/tui/dispatch.go:145-150` (the `open()` repo load)
- Test: `internal/tui/dispatch_test.go` (adjust/append)

**Interfaces:**
- Consumes: `core.ChannelService.ProjectChannels` via `d.m.store` (the TUI's `core.Service`); `core.ChannelTypeRepo`.
- Produces: unchanged dialog behavior over a new source — `d.repos []core.RepoConfig` stays, now built from repo channels' wiring; legacy `ProjectRepos` remains ONLY as a fallback when no repo channel is wired (the deprecation window for stores that never ran migrate-repos).

- [ ] **Step 1: Write the failing test**

Follow the existing tests in `internal/tui/dispatch_test.go` (they drive `dispatchModel.open` against a fake or temp store — reuse their store setup):

```go
func TestDispatchReadsRepoChannels(t *testing.T) {
	// setup: temp store, project ATM, channel add {code, repo}, wire path <dir>
	// open developer dispatch for ATM
	// assert d.repos == [{Name: "code", Path: <dir>}]
}

func TestDispatchLegacyRepoFallback(t *testing.T) {
	// setup: temp store with ONLY legacy s.SetProjectRepo entries (no channels)
	// open developer dispatch
	// assert d.repos falls back to the legacy list
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run TestDispatch -v`
Expected: the new tests FAIL (existing ones still pass)

- [ ] **Step 3: Implement**

Replace the repo load in `open()` (`internal/tui/dispatch.go:146-150`):

```go
	if kind == dispatchDeveloper && project != "" {
		if views, err := d.m.store.ProjectChannels(project); err == nil {
			for _, v := range views {
				if v.Type == core.ChannelTypeRepo && v.Wiring != nil && v.Wiring.Path != "" {
					d.repos = append(d.repos, core.RepoConfig{Name: v.Name, Path: v.Wiring.Path, URL: v.Address.URL})
				}
			}
		}
		if len(d.repos) == 0 { // legacy fallback until migrate-repos has run
			if repos, err := d.m.store.ProjectRepos(project); err == nil {
				d.repos = repos
			}
		}
	}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/dispatch.go internal/tui/dispatch_test.go
git commit -m "feat(ATM-097849): dispatch dialog reads repo channels, legacy fallback"
```

---

### Task 9: TUI — channels overlay (read-only + dispatch concierge)

**Files:**
- Create: `internal/tui/channels.go`
- Create: `internal/tui/channels_test.go`
- Modify: `internal/tui/app.go` — Model field + wiring (pattern anchors: `personasOv` at `app.go:104`, `:197`, `:400`, `:593`, `:678`, `:936`), and the KEEP-IN-SYNC comment's overlay count
- Modify: `internal/tui/keymap.go` (add row)

**Interfaces:**
- Consumes: `core.ChannelService.ProjectChannels`; `dispatchModel.open(dispatchConcierge, project, "", "")`; helpers `titledBoxHeight`, `fitLine`, `styles.DialogBody`, `styles.KeyMenuDim`, `styles.RowCursor` (all as used in `personas.go`).
- Produces: `channelsModel` with `open bool`, `openOverlay()`, `handleKey(tea.KeyMsg) tea.Cmd`, `renderOverlay() string`; global key `E` opens it; inside: `j/k` move, `Enter` detail, `c` dispatches concierge, `Esc` closes/backs. Status glyph rule (single-sourced in `channelStatusGlyph`): `●` ok, `◐` attention, `○` missing/stale.

- [ ] **Step 1: Write the failing test**

Follow `internal/tui/personas_test.go` for the harness (model construction + key injection):

```go
// internal/tui/channels_test.go — shapes, adapt to the package's harness
func TestChannelsOverlayListsAndCloses(t *testing.T) {
	// setup: model over a temp store with project ATM, two channels (repo wired to t.TempDir(), notion unwired)
	// press "E": overlay open, renderOverlay() contains both handles and the type words
	// press "j" then "enter": detail mode, render contains the purpose and address
	// press "esc": back to list; press "esc": closed
}

func TestChannelsOverlayStatusGlyphs(t *testing.T) {
	// repo channel wired to an existing dir with a fresh stamp → glyph ●
	// notion channel with wiring but stamp older than 45 days → glyph ○
	// channel with no wiring → glyph ○ and the word "unwired"
	// exercise channelStatusGlyph directly with constructed core.ChannelView values
}

func TestChannelsOverlayDispatchConcierge(t *testing.T) {
	// press "E" then "c": channels overlay closed, dispatch dialog open with kind dispatchConcierge
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run TestChannelsOverlay -v`
Expected: FAIL (undefined `channelsModel`)

- [ ] **Step 3: Implement**

`internal/tui/channels.go`, modeled line-for-line on `personas.go`:

```go
package tui

import (
	"fmt"
	"strings"
	"time"

	"atm/internal/core"

	tea "github.com/charmbracelet/bubbletea"
)

// channelsModel is the read-only channels overlay: list every channel with
// its status, enter for detail. The one action is dispatching a concierge
// session to fix what the status shows; all writes go through `atm channel`.
type channelsModel struct {
	m       *Model
	open    bool
	cursor  int
	project string
	entries []core.ChannelView
	loadErr string
	detail  bool
	offset  int
}

func (c *channelsModel) openOverlay(project string) {
	c.project = project
	c.entries, c.loadErr = nil, ""
	if views, err := c.m.store.ProjectChannels(project); err != nil {
		c.loadErr = err.Error()
	} else {
		c.entries = views
	}
	c.open, c.detail, c.offset = true, false, 0
	if c.cursor >= len(c.entries) {
		c.cursor = 0
	}
}

func (c *channelsModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc", "E":
		if c.detail {
			c.detail = false
			return nil
		}
		c.open = false
	case "j", "down":
		if c.detail {
			c.offset++
		} else if c.cursor < len(c.entries)-1 {
			c.cursor++
		}
	case "k", "up":
		if c.detail {
			if c.offset > 0 {
				c.offset--
			}
		} else if c.cursor > 0 {
			c.cursor--
		}
	case "g":
		c.offset, c.cursor = 0, 0
	case "enter":
		if !c.detail && len(c.entries) > 0 {
			c.detail, c.offset = true, 0
		}
	case "c":
		c.open = false
		c.m.dispatchDlg.open(dispatchConcierge, c.project, "", "")
	}
	return nil
}

// channelStatusGlyph is the single-sourced status rule: ● wired and verified
// fresh (or probe-green), ◐ wired but aging/dirty, ○ unwired, missing, or
// stale. now is injected for testability.
func channelStatusGlyph(v core.ChannelView, now time.Time) (string, string) {
	if v.Wiring == nil {
		return "○", "unwired"
	}
	if v.Probe != nil {
		if !v.Probe.PathExists {
			return "○", "path missing"
		}
		if !v.Probe.IsGitRepo {
			return "◐", "not a git repo"
		}
		note := "clean"
		if v.Probe.Dirty {
			note = "dirty"
		}
		if v.Probe.HasUpstream && (v.Probe.Ahead > 0 || v.Probe.Behind > 0) {
			return "◐", fmt.Sprintf("%s · %d ahead %d behind", note, v.Probe.Ahead, v.Probe.Behind)
		}
		if v.Probe.Dirty {
			return "◐", note
		}
		return "●", note
	}
	if len(v.Wiring.Stamps) == 0 {
		return "◐", "wired, never verified"
	}
	last := v.Wiring.Stamps[len(v.Wiring.Stamps)-1]
	at, err := time.Parse(time.RFC3339, last.At)
	if err != nil {
		return "◐", "unparseable stamp"
	}
	age := now.Sub(at)
	days := int(age.Hours() / 24)
	switch {
	case days <= 14:
		return "●", fmt.Sprintf("verified %dd ago", days)
	case days <= 45:
		return "◐", fmt.Sprintf("verified %dd ago", days)
	default:
		return "○", fmt.Sprintf("stale · verified %dd ago", days)
	}
}
```

`renderOverlay` mirrors `personasModel.renderOverlay`'s box math exactly (`bw := m.width*60/100`, min 64, max `width-4`; `titledBoxHeight(styles.DialogBody, bw, title, body, h)`): list rows `fmt.Sprintf("%s %-*s %-7s %s", glyph, nameW, v.Name, v.Type, note)` with `RowCursor` on the cursor row, footer `[↑/↓]move [Enter]detail [c]dispatch concierge [Esc]close`, and the `loadErr` rendered as the body when non-empty. Detail mode renders purpose, each non-empty address field (`URL/Workspace/Database/Page`), wiring path / MCP server, every stamp (`at · by · note`), and probe fields, scrolled by `offset` like the personas detail.

Wire into `app.go` mirroring `personasOv` at each anchor: field `channelsOv channelsModel` (`:104`), `m.channelsOv.m = m` (`:197`), `!m.channelsOv.open` in `workspaceIdle` (`:400`), routing `if m.channelsOv.open { return m.channelsOv.handleKey(k) }` next to the personas routing (`:593`), opening on `"E"` next to the `"V"` case (`:678`) — pass the same project value the `D` binding passes to `dispatchDlg.open` in that pane's handler (grep `dispatchDlg.open(` call sites and reuse the pane's project source) — and the render layer `if m.channelsOv.open { out = m.placeOverlay(out, m.channelsOv.renderOverlay()) }` in `View` (`:936`), updating the KEEP-IN-SYNC comment's count. Add the keymap row after `V`: `{"E", "channels overlay", "channels overlay", "-", "-"}`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui/`
Expected: PASS (including `view_purity_test.go` — the overlay does no I/O in `renderOverlay`; loading happens in `openOverlay`)

- [ ] **Step 5: Commit**

```bash
git add internal/tui/channels.go internal/tui/channels_test.go internal/tui/app.go internal/tui/keymap.go
git commit -m "feat(ATM-097849): read-only channels overlay with status glyphs and concierge dispatch"
```

---

### Task 10: Concierge persona, docs, full verify

**Files:**
- Modify: `skills/persona/concierge.md` (Step 2 line 38, Step 4 line 61)
- Modify: `README.md` (channels section near the repo-dispatch docs), `CHANGELOG.md` (entry)

**Interfaces:**
- Consumes: the CLI surface from Tasks 6-7 (verbatim command shapes).
- Produces: concierge prose that authors channels; user docs.

- [ ] **Step 1: Update the concierge persona**

Replace the repo sentence in Step 2 (line 38) so it reads:

```markdown
- Ask about their projects and where the work actually flows: which repositories they plan to bring in, and any other place their agents and people exchange work — a Notion workspace holding specs, external trackers, runbooks. These are the project's channels. For each repo, ask where it lives on this machine (the local folder) and its remote link; for a Notion channel, ask which workspace and which database or parent page — never ask for tokens or passwords, authorization happens later in the agent's own tooling.
```

Replace the repo bullet in Step 4 (line 61) with:

```markdown
- For each channel the user named in Step 2, record it in the ledger and wire it on this machine — in plain words, never exposing flag shapes. A repo: `atm channel add --project <CODE> --name <short-name> --type repo --purpose <what flows here> --url <remote-link>` then `atm channel wire --project <CODE> --name <short-name> --path <local-folder>`. A Notion channel: `atm channel add ... --type notion --workspace <w> --database <id>` then `atm channel wire ... --mcp-server <name>` after helping them install and authorize the agent-side Notion MCP server (the token stays in that server's own store; ATM never holds credentials). After verifying a channel actually works, `atm channel stamp --project <CODE> --name <short-name> --note <what you verified>`. On a machine that synced an existing ledger, read `atm channel list` first: records without wiring are exactly what needs re-establishing — re-wire, re-authorize, re-stamp. If `atm channel list` mentions legacy repo dispatch targets, run `atm channel migrate-repos --project <CODE>` and confirm each migrated channel's purpose with the user via `atm channel edit`.
```

- [ ] **Step 2: Update README and CHANGELOG**

README: in the section that documented `atm project repo` (grep for it), replace with a "Channels" subsection: the three-tier model in three sentences, the verb list from the capability guide, the `E` overlay key, and the migration one-liner. CHANGELOG: add an entry under the next release heading: `- Channels: repositories and Notion databases as first-class, ledger-recorded communication surfaces ('atm channel', channels TUI overlay, 'E'); 'atm project repo' verbs retired ('atm channel migrate-repos' lifts existing entries).`

- [ ] **Step 3: Full verification**

Run: `make verify`
Expected: build + all tests + lint + scripts green. Fix anything red before committing.

- [ ] **Step 4: Commit**

```bash
git add skills/persona/concierge.md README.md CHANGELOG.md
git commit -m "docs(ATM-097849): concierge channels flow, README and CHANGELOG"
```

- [ ] **Step 5: Ledger close-out**

Journal an implementation-complete comment on ATM-097849 (`atm task comment add --task ATM-097849 --actor <actor> --body "..."` summarizing what landed and any deviations), then hand off per the finishing workflow — do NOT stamp `stage:done` until the work is merged per the project's process.

---

## Plan Self-Review Notes

- Spec coverage: concept/data model → Tasks 1-2; three tiers → Tasks 1, 3 (and the no-secrets constraint is global); CLI surface incl. agent endpoint → Tasks 6-7; repo cutover (dispatch, retire, migrate) → Tasks 4, 7, 8; status model + overlay + concierge button → Tasks 4, 9; concierge → Task 10; vocabulary/board/guide → Task 5; testing strategy → embedded per task. Out-of-scope items (Slack, workflow_ai channel locator kind, TUI writes, deep MCP probing) have deliberately no tasks.
- Type consistency: `ChannelService` method names in Task 4's interface block are the exact names Tasks 6-9 call; `core.ChannelView` embeds `ChannelRecord` so `v.Name`/`v.Type`/`v.Address` field accesses in Tasks 8-9 resolve; `channelStatusGlyph` takes `(core.ChannelView, time.Time)` in both Task 9's test and implementation.
- Known judgment calls an implementer may adjust with a journal note: the `E` keybinding (any free global key is fine — check `keymap.go` at implementation time), the exact test-harness helper names in `internal/store` and `internal/cli` (reuse whatever those packages already define; the failing-test step says to adapt the harness, not the assertion).
