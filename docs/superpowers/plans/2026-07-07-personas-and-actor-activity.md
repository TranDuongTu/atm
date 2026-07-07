# Personas & Actor Activity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add reusable Personas that enrich agent developing prompts, encode the actor as `persona@agent:model`, migrate legacy actors via a global alias table + seeded built-in personas, and surface actor activity through `atm activity` and a TUI Actors pane.

**Architecture:** A new leaf package `internal/actor` holds pure actor-string resolution (`persona@agent:model` + legacy patterns). `internal/store` gains a global `Persona` entity (plain atomic JSON files under `<root>/personas/`, no per-project log/cache) plus alias persistence (`<root>/actor-aliases.json`) and a migration. `internal/activity` builds and aggregates activity records from project logs. The CLI adds `atm persona`, `atm actor`, and `atm activity`; `atm developing` gains `--persona`. The TUI adds a maximized `[4] Actors` pane reusing the existing `meterBar` chart.

**Tech Stack:** Go, cobra CLI, Bubble Tea/lipgloss TUI, spf13/cobra, existing `internal/store` (SQLite cache + JSONL log, but personas/aliases bypass both and use `store.WriteFileAtomic`).

## Global Constraints

- Go module path is `atm` (imports are `atm/internal/...`).
- Persona name slug: `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$` (lowercase; never contains `@` or `:`).
- Actor-string convention: `<persona>@<agent>:<model>`; `<agent>` ∈ {`claude`, `codex`, `opencode`, `ollama`}.
- Unknown/empty persona resolves to the literal string `(none)` — define once as `actor.NonePersona`.
- Personas and aliases are **global** (machine-wide store root), never per-project. Persona mutations are **not** written to any `log.jsonl`; no new log action; `Replay` is untouched.
- All global JSON writes go through `store.WriteFileAtomic` (atomic rename, sorted keys). Reads through `store.ReadJSON`.
- Global writes are serialized with `s.WithLock("<sentinel>", fn)` using lowercase sentinel keys `"personas"` / `"actor-aliases"` (no collision with uppercase `^[A-Z]{3,6}$` project codes).
- Error sentinels: `store.ErrUsage`, `store.ErrNotFound`, `store.ErrConflict` (wrap with `%w`).
- JSON output is deterministic; text/JSON dual output goes through `st.emit(w, jsonMap, textFunc)`.
- Timestamps: `store.Now()` (UTC) for values, `store.RFC3339UTC(t)` for rendering.
- Run `make build && make test` (a.k.a. `make verify`) green before each commit.

---

## File Structure

- `internal/actor/actor.go` (new) — pure resolution: `Identity`, `AliasEntry`, `AliasMap`, `Resolve`, `LegacyAlias`, `NonePersona`, `Agents`.
- `internal/actor/actor_test.go` (new).
- `internal/store/persona.go` (new) — `Persona`, validation, CRUD, `SeedPersonas`.
- `internal/store/persona_test.go` (new).
- `internal/store/alias.go` (new) — alias load/save/set/remove, `MigrateActors`.
- `internal/store/alias_test.go` (new).
- `internal/seed/persona.go` (new) — `seed.Persona`, `seed.Personas` (data only).
- `internal/activity/activity.go` (new) — `Record`, `Group`, `Build`, `Aggregate`.
- `internal/activity/activity_test.go` (new).
- `internal/cli/persona.go` (new) — `atm persona` commands.
- `internal/cli/actor.go` (new) — `atm actor migrate|alias`.
- `internal/cli/activity.go` (new) — `atm activity`.
- `internal/cli/root.go` (modify) — register the three new command groups.
- `internal/cli/persona_test.go`, `internal/cli/actor_test.go`, `internal/cli/activity_test.go` (new).
- `internal/cli/developing.go` (modify) — `--persona`, actor base, env, context data.
- `internal/developing/context.go` (modify) — `ContextData.Persona`, `PersonaPrompt`, render.
- `internal/developing/context_v1.md` (modify) — persona block + actor convention.
- `internal/tui/actors.go` (new) — `actorsModel` (list + detail).
- `internal/tui/app.go` (modify) — `paneActors`, `numPanes=4`, tab `[4]`, render, statusHint.
- `internal/tui/actors_test.go` (new).

---

## Task 1: `internal/actor` resolution package

**Files:**
- Create: `internal/actor/actor.go`
- Test: `internal/actor/actor_test.go`

**Interfaces:**
- Consumes: nothing (leaf package, stdlib only).
- Produces:
  - `type Identity struct { Persona, Agent, Model string }`
  - `type AliasEntry struct { Persona string \`json:"persona"\`; Agent string \`json:"agent,omitempty"\`; Model string \`json:"model,omitempty"\` }`
  - `type AliasMap = map[string]AliasEntry`
  - `const NonePersona = "(none)"`
  - `var Agents = []string{"claude", "codex", "opencode", "ollama"}`
  - `func Resolve(raw string, aliases AliasMap) Identity`
  - `func LegacyAlias(raw string) (AliasEntry, bool)`

- [ ] **Step 1: Write the failing test**

```go
package actor

import "testing"

func id(p, a, m string) Identity { return Identity{Persona: p, Agent: a, Model: m} }

func TestResolve_Convention(t *testing.T) {
	cases := map[string]Identity{
		"staff-engineer@claude:opus-4.8": id("staff-engineer", "claude", "opus-4.8"),
		"staff@codex":                    id("staff", "codex", ""),
		"solo":                           id("solo", "", ""),
		"@claude:gpt-5":                  id(NonePersona, "claude", "gpt-5"),
		"p@a:b:c":                        id("p", "a", "b:c"), // model keeps extra colons
		"":                              id(NonePersona, "", ""),
	}
	for raw, want := range cases {
		if got := Resolve(raw, nil); got != want {
			t.Errorf("Resolve(%q) = %+v, want %+v", raw, got, want)
		}
	}
}

func TestResolve_AliasWins(t *testing.T) {
	aliases := AliasMap{
		"opencode-dev": {Persona: "developer", Agent: "opencode"},
		// alias overrides even a convention-formatted string:
		"x@y:z": {Persona: "manager", Agent: "ollama"},
	}
	if got := Resolve("opencode-dev", aliases); got != id("developer", "opencode", "") {
		t.Errorf("legacy alias: got %+v", got)
	}
	if got := Resolve("x@y:z", aliases); got != id("manager", "ollama", "") {
		t.Errorf("alias precedence: got %+v", got)
	}
}

func TestLegacyAlias(t *testing.T) {
	cases := map[string]AliasEntry{
		"opencode-dev":     {Persona: "developer", Agent: "opencode"},
		"ollama-onboard":   {Persona: "manager", Agent: "ollama"},
		"opencode-manager": {Persona: "manager", Agent: "opencode"},
		"codex":            {Persona: "developer", Agent: "codex"},
		"atm-manager":      {Persona: "manager"},
		"default":          {Persona: "developer"},
		"weird-name":       {Persona: "developer"},
	}
	for raw, want := range cases {
		got, ok := LegacyAlias(raw)
		if !ok || got != want {
			t.Errorf("LegacyAlias(%q) = %+v,%v want %+v", raw, got, ok, want)
		}
	}
	if _, ok := LegacyAlias("has@at"); ok {
		t.Errorf("convention-formatted string should not be legacy-aliased")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/actor/`
Expected: FAIL (package/functions undefined).

- [ ] **Step 3: Write minimal implementation**

```go
// Package actor resolves free-form actor strings into a (persona, agent,
// model) identity. It is a pure leaf package (no store dependency) so both
// the store's migration and the read-side activity aggregation can share it.
package actor

import "strings"

type Identity struct {
	Persona string
	Agent   string
	Model   string
}

type AliasEntry struct {
	Persona string `json:"persona"`
	Agent   string `json:"agent,omitempty"`
	Model   string `json:"model,omitempty"`
}

type AliasMap = map[string]AliasEntry

const NonePersona = "(none)"

var Agents = []string{"claude", "codex", "opencode", "ollama"}

// Resolve maps a raw actor string to an Identity. Resolution order:
// alias table (exact match, wins over everything) -> convention parse -> (none).
func Resolve(raw string, aliases AliasMap) Identity {
	if e, ok := aliases[raw]; ok {
		return normalize(Identity{Persona: e.Persona, Agent: e.Agent, Model: e.Model})
	}
	persona, rest, hasAt := strings.Cut(raw, "@")
	if !hasAt {
		return normalize(Identity{Persona: raw})
	}
	agent, model, _ := strings.Cut(rest, ":")
	return normalize(Identity{Persona: persona, Agent: agent, Model: model})
}

func normalize(i Identity) Identity {
	if i.Persona == "" {
		i.Persona = NonePersona
	}
	return i
}

// LegacyAlias derives an alias entry for a pre-convention actor string using
// ATM's own generated-default patterns. Returns ok=false for strings that
// already use the convention (contain '@').
func LegacyAlias(raw string) (AliasEntry, bool) {
	if strings.Contains(raw, "@") {
		return AliasEntry{}, false
	}
	if raw == "default" {
		return AliasEntry{Persona: "developer"}, true
	}
	if raw == "atm-manager" {
		return AliasEntry{Persona: "manager"}, true
	}
	for _, a := range Agents {
		switch raw {
		case a:
			return AliasEntry{Persona: "developer", Agent: a}, true
		case a + "-dev":
			return AliasEntry{Persona: "developer", Agent: a}, true
		case a + "-manager", a + "-onboard":
			return AliasEntry{Persona: "manager", Agent: a}, true
		}
	}
	// Anything else without '@': default working persona, unknown agent.
	return AliasEntry{Persona: "developer"}, true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/actor/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/actor/
git commit -m "Add actor resolution package (persona@agent:model + legacy patterns) (ATM-0052)"
```

---

## Task 2: `seed.Personas` built-in data

**Files:**
- Create: `internal/seed/persona.go`
- Test: covered via store `SeedPersonas` in Task 3 (no standalone test — this is pure data, mirroring how `seed.Labels` has no own test).

**Interfaces:**
- Produces:
  - `type Persona struct { Name, Prompt, Description string }`
  - `var Personas []seed.Persona` (exactly two: `developer`, `manager`)

- [ ] **Step 1: Write the data file**

```go
package seed

// Persona is one built-in persona seeded on demand by the store's
// SeedPersonas / `atm actor migrate`. Data only — no store import (the
// store applies it), mirroring Labels above.
type Persona struct {
	Name        string
	Prompt      string
	Description string
}

// Personas is the built-in persona set. These two names are also the targets
// the legacy actor migration maps onto (internal/actor.LegacyAlias).
var Personas = []Persona{
	{
		Name:        "developer",
		Description: "Default working persona: implements features, fixes, and chores.",
		Prompt: "You are a developer working in an ATM developing session. Implement " +
			"features, fixes, and chores to a high standard: small, well-bounded changes; " +
			"tests before implementation; frequent commits; and clear task-comment records " +
			"of intent, decisions, and results.",
	},
	{
		Name:        "manager",
		Description: "Curates the ledger and oversees work.",
		Prompt: "You are a manager persona. Keep the ATM ledger accurate and legible: " +
			"organize tasks and labels, summarize progress, surface blockers, and hold a " +
			"high bar on scope and clarity rather than writing feature code yourself.",
	},
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/seed/`
Expected: builds clean (no test yet; exercised in Task 3).

- [ ] **Step 3: Commit**

```bash
git add internal/seed/persona.go
git commit -m "Add built-in developer/manager persona seed data (ATM-0052)"
```

---

## Task 3: Persona store entity + CRUD + SeedPersonas

**Files:**
- Create: `internal/store/persona.go`
- Test: `internal/store/persona_test.go`

**Interfaces:**
- Consumes: `store.WriteFileAtomic`, `store.ReadJSON`, `store.WithLock`, `store.Now`, `store.ErrUsage/ErrNotFound/ErrConflict`, `seed.Personas`.
- Produces:
  - `type Persona struct { Name, Prompt, Description string; CreatedAt, UpdatedAt time.Time; CreatedBy, UpdatedBy string }` (json snake_case)
  - `func ValidatePersonaName(name string) error`
  - `func (s *Store) CreatePersona(name, prompt, description, actor string) (*Persona, error)`
  - `func (s *Store) GetPersona(name string) (*Persona, error)`
  - `func (s *Store) ListPersonas() []*Persona`
  - `func (s *Store) EditPersona(name string, prompt, description *string, actor string) (*Persona, error)`
  - `func (s *Store) RemovePersona(name string) error`
  - `func (s *Store) SeedPersonas(actor string) ([]string, error)` (returns names newly created)

- [ ] **Step 1: Write the failing test**

```go
package store

import (
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(""); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPersonaCRUD(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreatePersona("Staff", "p", "", "tester"); !IsUsage(err) {
		t.Fatalf("uppercase name should be ErrUsage, got %v", err)
	}
	if _, err := s.CreatePersona("staff-engineer", "high bar", "reviewer", "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePersona("staff-engineer", "dup", "", "tester"); !IsConflict(err) {
		t.Fatalf("duplicate should be ErrConflict, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, "personas", "staff-engineer.json")); err != nil {
		t.Fatalf("persona file missing: %v", err)
	}

	got, err := s.GetPersona("staff-engineer")
	if err != nil || got.Prompt != "high bar" || got.Description != "reviewer" {
		t.Fatalf("get = %+v, %v", got, err)
	}

	newPrompt := "even higher bar"
	if _, err := s.EditPersona("staff-engineer", &newPrompt, nil, "tester"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetPersona("staff-engineer")
	if got.Prompt != "even higher bar" || got.Description != "reviewer" {
		t.Fatalf("edit left wrong state: %+v", got)
	}
	if _, err := s.EditPersona("ghost", &newPrompt, nil, "tester"); !IsNotFound(err) {
		t.Fatalf("edit missing should be ErrNotFound, got %v", err)
	}

	if err := s.RemovePersona("staff-engineer"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPersona("staff-engineer"); !IsNotFound(err) {
		t.Fatalf("get after remove should be ErrNotFound, got %v", err)
	}
}

func TestSeedPersonasIdempotent(t *testing.T) {
	s := newTestStore(t)
	added, err := s.SeedPersonas("seed")
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 2 {
		t.Fatalf("first seed added %v, want 2", added)
	}
	// User edits a built-in.
	edited := "custom"
	if _, err := s.EditPersona("developer", &edited, nil, "u"); err != nil {
		t.Fatal(err)
	}
	added2, err := s.SeedPersonas("seed")
	if err != nil {
		t.Fatal(err)
	}
	if len(added2) != 0 {
		t.Fatalf("second seed added %v, want none", added2)
	}
	got, _ := s.GetPersona("developer")
	if got.Prompt != "custom" {
		t.Fatalf("seed clobbered user edit: %q", got.Prompt)
	}
	if len(s.ListPersonas()) != 2 {
		t.Fatalf("list = %d, want 2", len(s.ListPersonas()))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestPersona`
Expected: FAIL (undefined symbols; also add `"os"` import to the test file).

- [ ] **Step 3: Write minimal implementation**

```go
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"atm/internal/seed"
)

type Persona struct {
	Name        string    `json:"name"`
	Prompt      string    `json:"prompt"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	CreatedBy   string    `json:"created_by"`
	UpdatedBy   string    `json:"updated_by"`
}

var personaNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

func ValidatePersonaName(name string) error {
	if !personaNameRe.MatchString(name) {
		return fmt.Errorf("%w: invalid persona name %q (want ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$)", ErrUsage, name)
	}
	return nil
}

func (s *Store) personasDir() string { return filepath.Join(s.Root, "personas") }
func (s *Store) personaPath(name string) string {
	return filepath.Join(s.personasDir(), name+".json")
}

func (s *Store) CreatePersona(name, prompt, description, actor string) (*Persona, error) {
	if err := ValidatePersonaName(name); err != nil {
		return nil, err
	}
	if actor == "" {
		return nil, fmt.Errorf("%w: actor is required", ErrUsage)
	}
	var created *Persona
	err := s.WithLock("personas", func() error {
		if _, err := os.Stat(s.personaPath(name)); err == nil {
			return fmt.Errorf("%w: persona %q already exists", ErrConflict, name)
		} else if !os.IsNotExist(err) {
			return err
		}
		now := Now()
		p := &Persona{
			Name: name, Prompt: prompt, Description: description,
			CreatedAt: now, UpdatedAt: now, CreatedBy: actor, UpdatedBy: actor,
		}
		if err := WriteFileAtomic(s.personaPath(name), p); err != nil {
			return err
		}
		created = p
		return nil
	})
	return created, err
}

func (s *Store) GetPersona(name string) (*Persona, error) {
	var p Persona
	if err := ReadJSON(s.personaPath(name), &p); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: persona %q", ErrNotFound, name)
		}
		return nil, err
	}
	return &p, nil
}

func (s *Store) ListPersonas() []*Persona {
	entries, err := os.ReadDir(s.personasDir())
	if err != nil {
		return nil
	}
	var out []*Persona
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		name := e.Name()[:len(e.Name())-len(".json")]
		if p, err := s.GetPersona(name); err == nil {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Store) EditPersona(name string, prompt, description *string, actor string) (*Persona, error) {
	if actor == "" {
		return nil, fmt.Errorf("%w: actor is required", ErrUsage)
	}
	var updated *Persona
	err := s.WithLock("personas", func() error {
		p, err := s.GetPersona(name)
		if err != nil {
			return err
		}
		if prompt != nil {
			p.Prompt = *prompt
		}
		if description != nil {
			p.Description = *description
		}
		p.UpdatedAt = Now()
		p.UpdatedBy = actor
		if err := WriteFileAtomic(s.personaPath(name), p); err != nil {
			return err
		}
		updated = p
		return nil
	})
	return updated, err
}

func (s *Store) RemovePersona(name string) error {
	return s.WithLock("personas", func() error {
		if _, err := s.GetPersona(name); err != nil {
			return err
		}
		return os.Remove(s.personaPath(name))
	})
}

// SeedPersonas creates any built-in persona (seed.Personas) that does not yet
// exist. Idempotent: never overwrites an existing (possibly user-edited) file.
// Returns the names newly created.
func (s *Store) SeedPersonas(actor string) ([]string, error) {
	var added []string
	for _, sp := range seed.Personas {
		_, err := s.CreatePersona(sp.Name, sp.Prompt, sp.Description, actor)
		if err == nil {
			added = append(added, sp.Name)
			continue
		}
		if IsConflict(err) {
			continue // already exists — leave it untouched
		}
		return added, err
	}
	sort.Strings(added)
	return added, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/store/ -run 'TestPersona|TestSeedPersonas'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/persona.go internal/store/persona_test.go
git commit -m "Add global Persona entity: CRUD + idempotent SeedPersonas (ATM-0052)"
```

---

## Task 4: Alias persistence + MigrateActors

**Files:**
- Create: `internal/store/alias.go`
- Test: `internal/store/alias_test.go`

**Interfaces:**
- Consumes: `actor.AliasMap`, `actor.AliasEntry`, `actor.LegacyAlias`, `store.WriteFileAtomic`, `store.ReadJSON`, `store.WithLock`, `s.projectCodesOnDisk`, `s.ReadLog`, `s.SeedPersonas`.
- Produces:
  - `func (s *Store) aliasPath() string`
  - `func (s *Store) LoadAliases() (actor.AliasMap, error)` (empty, non-nil map if file absent)
  - `func (s *Store) SetAlias(raw string, e actor.AliasEntry) error`
  - `func (s *Store) RemoveAlias(raw string) error`
  - `type MigrationResult struct { Seeded []string; Added map[string]actor.AliasEntry }`
  - `func (s *Store) MigrateActors(dryRun bool) (*MigrationResult, error)`

- [ ] **Step 1: Write the failing test**

```go
package store

import (
	"testing"

	"atm/internal/actor"
)

func TestAliasSetLoadRemove(t *testing.T) {
	s := newTestStore(t)
	if m, err := s.LoadAliases(); err != nil || len(m) != 0 {
		t.Fatalf("empty load = %v, %v", m, err)
	}
	if err := s.SetAlias("opencode-dev", actor.AliasEntry{Persona: "developer", Agent: "opencode"}); err != nil {
		t.Fatal(err)
	}
	m, err := s.LoadAliases()
	if err != nil || m["opencode-dev"].Persona != "developer" || m["opencode-dev"].Agent != "opencode" {
		t.Fatalf("load = %+v, %v", m, err)
	}
	if err := s.RemoveAlias("opencode-dev"); err != nil {
		t.Fatal(err)
	}
	if m, _ := s.LoadAliases(); len(m) != 0 {
		t.Fatalf("after remove = %+v", m)
	}
}

func TestMigrateActors(t *testing.T) {
	s := newTestStore(t)
	// Two projects with legacy actors on their logs.
	if _, err := s.CreateProject("AAA", "A", "opencode-dev"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject("BBB", "B", "ollama-manager"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("AAA", "t", nil, "default"); err != nil {
		t.Fatal(err)
	}

	res, err := s.MigrateActors(true) // dry-run
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Seeded) != 2 {
		t.Fatalf("dry-run seeded %v, want developer+manager", res.Seeded)
	}
	if got := res.Added["opencode-dev"]; got.Persona != "developer" || got.Agent != "opencode" {
		t.Fatalf("opencode-dev -> %+v", got)
	}
	if got := res.Added["ollama-manager"]; got.Persona != "manager" || got.Agent != "ollama" {
		t.Fatalf("ollama-manager -> %+v", got)
	}
	if got := res.Added["default"]; got.Persona != "developer" {
		t.Fatalf("default -> %+v", got)
	}
	if m, _ := s.LoadAliases(); len(m) != 0 {
		t.Fatalf("dry-run must not write aliases, got %+v", m)
	}

	// Real run writes; second run is a no-op and preserves a user override.
	if _, err := s.MigrateActors(false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAlias("default", actor.AliasEntry{Persona: "manager"}); err != nil {
		t.Fatal(err)
	}
	res2, err := s.MigrateActors(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Added) != 0 {
		t.Fatalf("second migrate added %+v, want none", res2.Added)
	}
	m, _ := s.LoadAliases()
	if m["default"].Persona != "manager" {
		t.Fatalf("migrate clobbered user override: %+v", m["default"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'TestAlias|TestMigrate'`
Expected: FAIL (undefined symbols).

- [ ] **Step 3: Write minimal implementation**

```go
package store

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"atm/internal/actor"
)

func (s *Store) aliasPath() string { return filepath.Join(s.Root, "actor-aliases.json") }

func (s *Store) LoadAliases() (actor.AliasMap, error) {
	m := actor.AliasMap{}
	err := ReadJSON(s.aliasPath(), &m)
	if err != nil {
		if os.IsNotExist(err) {
			return actor.AliasMap{}, nil
		}
		return nil, err
	}
	return m, nil
}

func (s *Store) SetAlias(raw string, e actor.AliasEntry) error {
	return s.WithLock("actor-aliases", func() error {
		m, err := s.LoadAliases()
		if err != nil {
			return err
		}
		m[raw] = e
		return WriteFileAtomic(s.aliasPath(), m)
	})
}

func (s *Store) RemoveAlias(raw string) error {
	return s.WithLock("actor-aliases", func() error {
		m, err := s.LoadAliases()
		if err != nil {
			return err
		}
		delete(m, raw)
		return WriteFileAtomic(s.aliasPath(), m)
	})
}

type MigrationResult struct {
	Seeded []string
	Added  map[string]actor.AliasEntry
}

// MigrateActors seeds the built-in personas and generates alias entries for
// distinct legacy actor strings found across all project logs. Idempotent:
// existing alias entries (including user overrides) are never overwritten.
// dryRun computes the result without writing personas or aliases.
func (s *Store) MigrateActors(dryRun bool) (*MigrationResult, error) {
	res := &MigrationResult{Added: map[string]actor.AliasEntry{}}

	// 1. Personas.
	if dryRun {
		for _, sp := range seedPersonaNamesMissing(s) {
			res.Seeded = append(res.Seeded, sp)
		}
	} else {
		added, err := s.SeedPersonas("migrate")
		if err != nil {
			return nil, err
		}
		res.Seeded = added
	}
	sort.Strings(res.Seeded)

	// 2. Distinct legacy actors across all project logs.
	existing, err := s.LoadAliases()
	if err != nil {
		return nil, err
	}
	codes, err := s.projectCodesOnDisk()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, code := range codes {
		entries, err := s.ReadLog(code)
		if err != nil && !IsIntegrity(err) {
			return nil, err
		}
		for _, e := range entries {
			raw := e.Actor
			if raw == "" || seen[raw] || strings.Contains(raw, "@") {
				continue
			}
			seen[raw] = true
			if _, ok := existing[raw]; ok {
				continue // never overwrite
			}
			if ent, ok := actor.LegacyAlias(raw); ok {
				res.Added[raw] = ent
			}
		}
	}

	// 3. Persist (unless dry-run).
	if !dryRun && len(res.Added) > 0 {
		err = s.WithLock("actor-aliases", func() error {
			m, err := s.LoadAliases()
			if err != nil {
				return err
			}
			for raw, ent := range res.Added {
				if _, ok := m[raw]; !ok {
					m[raw] = ent
				}
			}
			return WriteFileAtomic(s.aliasPath(), m)
		})
		if err != nil {
			return nil, err
		}
	}
	return res, nil
}

// seedPersonaNamesMissing returns built-in persona names not yet on disk
// (used to preview seeding under dry-run without creating files).
func seedPersonaNamesMissing(s *Store) []string {
	var missing []string
	for _, sp := range seedPersonas() {
		if _, err := s.GetPersona(sp); IsNotFound(err) {
			missing = append(missing, sp)
		}
	}
	return missing
}
```

- [ ] **Step 4: Add the `seedPersonas()` name helper**

The dry-run preview needs the built-in names without importing anything new. Add to `internal/store/persona.go`:

```go
// seedPersonas returns the built-in persona names (order-independent).
func seedPersonas() []string {
	names := make([]string, 0, len(seed.Personas))
	for _, sp := range seed.Personas {
		names = append(names, sp.Name)
	}
	return names
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/store/ -run 'TestAlias|TestMigrate'`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/store/alias.go internal/store/alias_test.go internal/store/persona.go
git commit -m "Add actor alias persistence + MigrateActors (ATM-0052)"
```

---

## Task 5: `atm persona` CLI

**Files:**
- Create: `internal/cli/persona.go`
- Modify: `internal/cli/root.go` (register `newPersonaCmd(st)`)
- Test: `internal/cli/persona_test.go`

**Interfaces:**
- Consumes: `st.resolveActor`, `st.openStore`, `st.emit`, `st.stdout`, store persona CRUD.
- Produces: `func newPersonaCmd(st *cliState) *cobra.Command`; JSON shape `{"persona": {name, prompt, description, created_at, updated_at, created_by, updated_by}}` and `{"personas": [...]}`.

- [ ] **Step 1: Write the failing test**

Study an existing CLI test (e.g. `internal/cli/label_test.go`) for the harness helper that runs a command and captures output. Assume a helper `runCLI(t, st, args...) (stdout string, err error)` exists in `internal/cli` tests (reuse the same one the label/task tests use; do not invent a new harness).

```go
package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPersonaCreateListShowEditRemove(t *testing.T) {
	st := newTestCLIState(t) // same constructor label_test.go uses

	if _, err := runCLI(t, st, "persona", "create", "--name", "staff", "--prompt", "high bar", "--actor", "t"); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, st, "persona", "list", "--output", "json")
	if err != nil {
		t.Fatal(err)
	}
	var listed struct{ Personas []struct{ Name string } }
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Personas) != 1 || listed.Personas[0].Name != "staff" {
		t.Fatalf("list = %s", out)
	}

	out, _ = runCLI(t, st, "persona", "show", "--name", "staff", "--output", "json")
	if !strings.Contains(out, "high bar") {
		t.Fatalf("show = %s", out)
	}

	if _, err := runCLI(t, st, "persona", "edit", "--name", "staff", "--description", "reviewer", "--actor", "t"); err != nil {
		t.Fatal(err)
	}
	out, _ = runCLI(t, st, "persona", "show", "--name", "staff", "--output", "json")
	if !strings.Contains(out, "reviewer") || !strings.Contains(out, "high bar") {
		t.Fatalf("edit lost data: %s", out)
	}

	if _, err := runCLI(t, st, "persona", "remove", "--name", "staff", "--actor", "t"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, st, "persona", "show", "--name", "staff"); err == nil {
		t.Fatal("show after remove should error")
	}
}

func TestPersonaPromptMutualExclusion(t *testing.T) {
	st := newTestCLIState(t)
	_, err := runCLI(t, st, "persona", "create", "--name", "x", "--prompt", "a", "--prompt-file", "/tmp/x", "--actor", "t")
	if err == nil || !strings.Contains(err.Error(), "prompt") {
		t.Fatalf("want mutual-exclusion error, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestPersona`
Expected: FAIL (`persona` command unknown).

- [ ] **Step 3: Write minimal implementation**

```go
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newPersonaCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{Use: "persona", Short: "Persona registry commands"}
	cmd.AddCommand(newPersonaCreateCmd(st))
	cmd.AddCommand(newPersonaListCmd(st))
	cmd.AddCommand(newPersonaShowCmd(st))
	cmd.AddCommand(newPersonaEditCmd(st))
	cmd.AddCommand(newPersonaRemoveCmd(st))
	return cmd
}

// resolvePrompt returns the prompt from --prompt or --prompt-file (mutually
// exclusive). ok reports whether a prompt was supplied at all.
func resolvePrompt(prompt, promptFile string) (val string, ok bool, err error) {
	if prompt != "" && promptFile != "" {
		return "", false, fmt.Errorf("%w: --prompt and --prompt-file are mutually exclusive", ErrUsage)
	}
	if promptFile != "" {
		b, e := os.ReadFile(promptFile)
		if e != nil {
			return "", false, fmt.Errorf("read --prompt-file: %w", e)
		}
		return string(b), true, nil
	}
	if prompt != "" {
		return prompt, true, nil
	}
	return "", false, nil
}

func personaToJSON(p any) any { return p } // store.Persona already has json tags

func newPersonaCreateCmd(st *cliState) *cobra.Command {
	var name, prompt, promptFile, description string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a persona",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			pr, _, err := resolvePrompt(prompt, promptFile)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			p, err := s.CreatePersona(name, pr, description, actor)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"persona": p}, func() {
				fmt.Fprintf(st.stdout(), "created persona %s\n", p.Name)
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "persona name (lowercase slug)")
	cmd.Flags().StringVar(&prompt, "prompt", "", "persona prompt text")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "read persona prompt from file")
	cmd.Flags().StringVar(&description, "description", "", "one-line description")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newPersonaListCmd(st *cliState) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List personas",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			ps := s.ListPersonas()
			return st.emit(st.stdout(), map[string]any{"personas": ps}, func() {
				for _, p := range ps {
					if p.Description == "" {
						fmt.Fprintf(st.stdout(), "%s\n", p.Name)
					} else {
						fmt.Fprintf(st.stdout(), "%s\t%s\n", p.Name, p.Description)
					}
				}
			})
		},
	}
}

func newPersonaShowCmd(st *cliState) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show a persona",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			p, err := s.GetPersona(name)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"persona": p}, func() {
				fmt.Fprintf(st.stdout(), "%s\t%s\n\n%s\n", p.Name, p.Description, p.Prompt)
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "persona name")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newPersonaEditCmd(st *cliState) *cobra.Command {
	var name, prompt, promptFile, description string
	cmd := &cobra.Command{
		Use:   "edit",
		Short: "Edit a persona (only supplied fields change)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			pr, prOK, err := resolvePrompt(prompt, promptFile)
			if err != nil {
				return err
			}
			var pPtr, dPtr *string
			if prOK {
				pPtr = &pr
			}
			if cmd.Flags().Changed("description") {
				dPtr = &description
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			p, err := s.EditPersona(name, pPtr, dPtr, actor)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"persona": p}, func() {
				fmt.Fprintf(st.stdout(), "updated persona %s\n", p.Name)
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "persona name")
	cmd.Flags().StringVar(&prompt, "prompt", "", "new prompt text")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "read new prompt from file")
	cmd.Flags().StringVar(&description, "description", "", "new description")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newPersonaRemoveCmd(st *cliState) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a persona",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.RemovePersona(name); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"removed": name}, func() {
				fmt.Fprintf(st.stdout(), "removed persona %s\n", name)
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "persona name")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}
```

Delete the unused `personaToJSON` stub if `go vet` flags it — `store.Persona` serializes directly via its json tags, so `map[string]any{"persona": p}` is correct.

- [ ] **Step 4: Register the command in `root.go`**

In `newRootCmdWithState`, next to `root.AddCommand(newLabelCmd(st))`, add:

```go
	root.AddCommand(newPersonaCmd(st))
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/cli/ -run TestPersona`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/persona.go internal/cli/persona_test.go internal/cli/root.go
git commit -m "Add atm persona CLI (create/list/show/edit/remove) (ATM-0052)"
```

---

## Task 6: `atm actor` CLI (migrate + alias)

**Files:**
- Create: `internal/cli/actor.go`
- Modify: `internal/cli/root.go` (register `newActorCmd(st)`)
- Test: `internal/cli/actor_test.go`

**Interfaces:**
- Consumes: `st.openStore`, `st.emit`, `actor.AliasEntry`, store `MigrateActors/LoadAliases/SetAlias/RemoveAlias`.
- Produces: `func newActorCmd(st *cliState) *cobra.Command`. `atm actor migrate [--dry-run]`; `atm actor alias set <raw> [--persona] [--agent] [--model]`; `atm actor alias list`; `atm actor alias remove <raw>`.

- [ ] **Step 1: Write the failing test**

```go
package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestActorMigrateAndAlias(t *testing.T) {
	st := newTestCLIState(t)
	// Seed a project whose creator actor is a legacy string.
	if _, err := runCLI(t, st, "project", "create", "--code", "AAA", "--name", "A", "--actor", "opencode-dev"); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, st, "actor", "migrate", "--dry-run", "--output", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "opencode-dev") || !strings.Contains(out, "developer") {
		t.Fatalf("dry-run = %s", out)
	}

	if _, err := runCLI(t, st, "actor", "migrate"); err != nil {
		t.Fatal(err)
	}
	out, _ = runCLI(t, st, "actor", "alias", "list", "--output", "json")
	var listed struct {
		Aliases map[string]struct{ Persona, Agent string }
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Aliases["opencode-dev"].Persona != "developer" {
		t.Fatalf("alias list = %s", out)
	}

	if _, err := runCLI(t, st, "actor", "alias", "set", "codex", "--persona", "staff-engineer", "--agent", "codex"); err != nil {
		t.Fatal(err)
	}
	out, _ = runCLI(t, st, "actor", "alias", "list", "--output", "json")
	if !strings.Contains(out, "staff-engineer") {
		t.Fatalf("after set = %s", out)
	}
	if _, err := runCLI(t, st, "actor", "alias", "remove", "codex"); err != nil {
		t.Fatal(err)
	}
	out, _ = runCLI(t, st, "actor", "alias", "list", "--output", "json")
	if strings.Contains(out, "staff-engineer") {
		t.Fatalf("after remove = %s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestActorMigrate`
Expected: FAIL (`actor` command unknown).

- [ ] **Step 3: Write minimal implementation**

```go
package cli

import (
	"fmt"

	"atm/internal/actor"

	"github.com/spf13/cobra"
)

func newActorCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{Use: "actor", Short: "Actor migration and alias commands"}
	cmd.AddCommand(newActorMigrateCmd(st))
	cmd.AddCommand(newActorAliasCmd(st))
	return cmd
}

func newActorMigrateCmd(st *cliState) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Seed built-in personas and alias legacy actor strings",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			res, err := s.MigrateActors(dryRun)
			if err != nil {
				return err
			}
			// actor.AliasEntry has json tags, so res.Added serializes directly.
			return st.emit(st.stdout(), map[string]any{
				"dry_run": dryRun,
				"seeded":  res.Seeded,
				"aliases": res.Added,
			}, func() {
				fmt.Fprintf(st.stdout(), "seeded personas: %v\n", res.Seeded)
				for raw, e := range res.Added {
					fmt.Fprintf(st.stdout(), "%s -> persona=%s agent=%s\n", raw, e.Persona, e.Agent)
				}
				if dryRun {
					fmt.Fprintln(st.stdout(), "(dry-run: nothing written)")
				}
			})
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "compute the migration without writing")
	return cmd
}

func newActorAliasCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{Use: "alias", Short: "Manage actor aliases"}
	cmd.AddCommand(newActorAliasSetCmd(st))
	cmd.AddCommand(newActorAliasListCmd(st))
	cmd.AddCommand(newActorAliasRemoveCmd(st))
	return cmd
}

func newActorAliasSetCmd(st *cliState) *cobra.Command {
	var persona, agent, model string
	cmd := &cobra.Command{
		Use:   "set <raw-actor>",
		Short: "Set (or override) the alias for a raw actor string",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if persona == "" {
				return fmt.Errorf("%w: --persona is required", ErrUsage)
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			e := actor.AliasEntry{Persona: persona, Agent: agent, Model: model}
			if err := s.SetAlias(args[0], e); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"alias": map[string]any{args[0]: e}}, func() {
				fmt.Fprintf(st.stdout(), "set alias %s -> persona=%s agent=%s model=%s\n", args[0], persona, agent, model)
			})
		},
	}
	cmd.Flags().StringVar(&persona, "persona", "", "persona name (required)")
	cmd.Flags().StringVar(&agent, "agent", "", "agent")
	cmd.Flags().StringVar(&model, "model", "", "model")
	return cmd
}

func newActorAliasListCmd(st *cliState) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List actor aliases",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			m, err := s.LoadAliases()
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"aliases": m}, func() {
				for raw, e := range m {
					fmt.Fprintf(st.stdout(), "%s\t%s\t%s\t%s\n", raw, e.Persona, e.Agent, e.Model)
				}
			})
		},
	}
}

func newActorAliasRemoveCmd(st *cliState) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <raw-actor>",
		Short: "Remove an actor alias",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.RemoveAlias(args[0]); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"removed": args[0]}, func() {
				fmt.Fprintf(st.stdout(), "removed alias %s\n", args[0])
			})
		},
	}
}
```

- [ ] **Step 4: Register in `root.go`**

```go
	root.AddCommand(newActorCmd(st))
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/cli/ -run TestActor`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/actor.go internal/cli/actor_test.go internal/cli/root.go
git commit -m "Add atm actor CLI: migrate + alias set/list/remove (ATM-0052)"
```

---

## Task 7: `internal/activity` build + aggregate

**Files:**
- Create: `internal/activity/activity.go`
- Test: `internal/activity/activity_test.go`

**Interfaces:**
- Consumes: `store.LogEntry`, `actor.AliasMap`, `actor.Resolve`.
- Produces:
  - `type Record struct { Persona, Agent, Model, Action string }`
  - `func Build(entries []store.LogEntry, aliases actor.AliasMap) []Record`
  - `type Group struct { Key string; Count int; Agents, Models, Actions map[string]int }`
  - `func Aggregate(recs []Record, groupBy string) []Group` — groupBy ∈ {`persona`,`agent`,`model`}; ordering count desc then key asc; empty agent/model counted under key `"(unknown)"` in the breakdown maps but never as a group key when the group value itself is empty (skip empty group keys for agent/model grouping).

- [ ] **Step 1: Write the failing test**

```go
package activity

import (
	"testing"

	"atm/internal/actor"
	"atm/internal/store"
)

func entry(a string) store.LogEntry { return store.LogEntry{Actor: a, Action: "task.created"} }

func TestBuildAndAggregateByPersona(t *testing.T) {
	aliases := actor.AliasMap{"opencode-dev": {Persona: "developer", Agent: "opencode"}}
	entries := []store.LogEntry{
		{Actor: "staff@claude:opus-4.8", Action: "task.created"},
		{Actor: "staff@claude:opus-4.8", Action: "comment.created"},
		{Actor: "staff@codex:gpt-5", Action: "task.created"},
		entry("opencode-dev"),
		entry("mystery"), // no alias, no '@' -> (none)
	}
	recs := Build(entries, aliases)
	groups := Aggregate(recs, "persona")

	byKey := map[string]Group{}
	for _, g := range groups {
		byKey[g.Key] = g
	}
	if byKey["staff"].Count != 3 {
		t.Fatalf("staff count = %d, want 3", byKey["staff"].Count)
	}
	if byKey["staff"].Agents["claude"] != 2 || byKey["staff"].Models["gpt-5"] != 1 {
		t.Fatalf("staff breakdown = %+v", byKey["staff"])
	}
	if byKey["developer"].Count != 1 || byKey["developer"].Agents["opencode"] != 1 {
		t.Fatalf("developer = %+v", byKey["developer"])
	}
	if byKey[actor.NonePersona].Count != 1 {
		t.Fatalf("(none) count = %d", byKey[actor.NonePersona].Count)
	}
	// Ordering: staff (3) first.
	if groups[0].Key != "staff" {
		t.Fatalf("ordering: first = %s", groups[0].Key)
	}
}

func TestAggregateByAgentSkipsEmpty(t *testing.T) {
	recs := []Record{
		{Persona: "p", Agent: "claude", Action: "a"},
		{Persona: "p", Agent: "", Action: "a"}, // unknown agent — not its own group
	}
	groups := Aggregate(recs, "agent")
	if len(groups) != 1 || groups[0].Key != "claude" {
		t.Fatalf("agent groups = %+v", groups)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/activity/`
Expected: FAIL (undefined).

- [ ] **Step 3: Write minimal implementation**

```go
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

func Build(entries []store.LogEntry, aliases actor.AliasMap) []Record {
	out := make([]Record, 0, len(entries))
	for _, e := range entries {
		id := actor.Resolve(e.Actor, aliases)
		out = append(out, Record{Persona: id.Persona, Agent: id.Agent, Model: id.Model, Action: e.Action})
	}
	return out
}

type Group struct {
	Key     string
	Count   int
	Agents  map[string]int
	Models  map[string]int
	Actions map[string]int
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
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/activity/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/activity/
git commit -m "Add activity aggregation (build records + group counts) (ATM-0052)"
```

---

## Task 8: `atm activity` CLI

**Files:**
- Create: `internal/cli/activity.go`
- Modify: `internal/cli/root.go` (register `newActivityCmd(st)`)
- Test: `internal/cli/activity_test.go`

**Interfaces:**
- Consumes: `st.openStore`, `st.emit`, `s.ReadLog`, `s.LoadAliases`, `activity.Build`, `activity.Aggregate`.
- Produces: `func newActivityCmd(st *cliState) *cobra.Command`. `atm activity --project CODE [--group-by persona|agent|model] [--output json]`. JSON: `{"groups": [{key, count, agents, models, actions}]}`.

- [ ] **Step 1: Write the failing test**

```go
package cli

import (
	"strings"
	"testing"
)

func TestActivityGroupsByPersona(t *testing.T) {
	st := newTestCLIState(t)
	if _, err := runCLI(t, st, "project", "create", "--code", "AAA", "--name", "A", "--actor", "staff@claude:opus-4.8"); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, st, "task", "create", "--project", "AAA", "--title", "t", "--actor", "staff@claude:opus-4.8"); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, st, "activity", "--project", "AAA", "--output", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"key": "staff"`) || !strings.Contains(out, `"claude"`) {
		t.Fatalf("activity = %s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestActivity`
Expected: FAIL (`activity` unknown).

- [ ] **Step 3: Write minimal implementation**

```go
package cli

import (
	"fmt"

	"atm/internal/activity"

	"github.com/spf13/cobra"
)

func newActivityCmd(st *cliState) *cobra.Command {
	var project, groupBy string
	cmd := &cobra.Command{
		Use:   "activity",
		Short: "Aggregate actor activity for a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			switch groupBy {
			case "persona", "agent", "model":
			default:
				return fmt.Errorf("%w: --group-by must be persona|agent|model", ErrUsage)
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if _, err := s.GetProject(project); err != nil {
				return err
			}
			entries, err := s.ReadLog(project)
			if err != nil && !isIntegrityErr(err) {
				return err
			}
			aliases, err := s.LoadAliases()
			if err != nil {
				return err
			}
			groups := activity.Aggregate(activity.Build(entries, aliases), groupBy)
			return st.emit(st.stdout(), map[string]any{"groups": groups}, func() {
				for _, g := range groups {
					fmt.Fprintf(st.stdout(), "%s\t%d\n", g.Key, g.Count)
				}
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&groupBy, "group-by", "persona", "persona|agent|model")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
```

Add a tiny helper near the top of `activity.go` (or reuse an existing one if the CLI already wraps integrity errors — check `internal/cli` first):

```go
func isIntegrityErr(err error) bool { return store.IsIntegrity(err) }
```

(Import `atm/internal/store` for it. If `store.IsIntegrity` is already imported elsewhere in the CLI package, call it directly and drop this helper.)

- [ ] **Step 4: Register in `root.go`**

```go
	root.AddCommand(newActivityCmd(st))
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/cli/ -run TestActivity`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/activity.go internal/cli/activity_test.go internal/cli/root.go
git commit -m "Add atm activity CLI (group actor activity by persona/agent/model) (ATM-0052)"
```

---

## Task 9: Launch integration — `atm developing --persona`

**Files:**
- Modify: `internal/developing/context.go` (add `Persona`, `PersonaPrompt`; render block)
- Modify: `internal/developing/context_v1.md` (persona block placeholder + actor convention line)
- Modify: `internal/cli/developing.go` (`--persona` flag, actor base, env, context data)
- Test: `internal/developing/context_test.go` (extend), `internal/cli/developing_test.go` (extend)

**Interfaces:**
- Consumes: `store.GetPersona`, `developing.RenderContext`.
- Produces: `ContextData` gains `Persona string`, `PersonaPrompt string`. Env gains `ATM_PERSONA`, `ATM_AGENT`. Default actor becomes `<persona>@<agent>` when `--persona` set.

- [ ] **Step 1: Write the failing test (context render)**

Add to `internal/developing/context_test.go`:

```go
func TestRenderContext_Persona(t *testing.T) {
	out := RenderContext(ContextData{
		Code: "ATM", Name: "ATM", ATMBin: "/atm", Actor: "staff@claude",
		RunID: "R", Timestamp: "T", Persona: "staff", PersonaPrompt: "hold a high bar",
	})
	if !strings.Contains(out, "Persona: staff") || !strings.Contains(out, "hold a high bar") {
		t.Fatalf("persona block missing:\n%s", out)
	}
	if !strings.Contains(out, "staff@claude:") {
		t.Fatalf("actor convention guidance missing:\n%s", out)
	}

	out2 := RenderContext(ContextData{Code: "ATM", Name: "ATM", ATMBin: "/atm", Actor: "claude-dev", RunID: "R", Timestamp: "T"})
	if strings.Contains(out2, "## Persona") {
		t.Fatalf("no-persona render should omit persona block:\n%s", out2)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/developing/ -run TestRenderContext_Persona`
Expected: FAIL (field/behavior missing).

- [ ] **Step 3: Update the template**

In `internal/developing/context_v1.md`, add a placeholder line right after the `## Role` heading's intro paragraph (choose a spot after the title block), on its own line:

```
<PERSONA_BLOCK>
```

And in the `## Commands` guidance, update the actor stamp guidance by adding this line after the existing `Actor: <ACTOR>` header line near the top:

```
Stamp ATM commands with --actor <ACTOR>:<your-model> (e.g. staff@claude:opus-4.8), filling in the model you are actually running as. If unsure, use --actor <ACTOR>.
```

(These two literal strings are replaced/kept by `RenderContext`; `<ACTOR>` is already a placeholder.)

- [ ] **Step 4: Update `context.go` render**

```go
func RenderContext(data ContextData) string {
	personaBlock := ""
	if data.Persona != "" {
		personaBlock = fmt.Sprintf("## Persona: %s\n\n%s\n\nYou are operating as this persona. Hold to its principles throughout the session, alongside repo instructions and the working routine below.\n",
			data.Persona, data.PersonaPrompt)
	}
	replacer := strings.NewReplacer(
		"<CODE>", data.Code,
		"<PROJECT_NAME>", data.Name,
		"<ATM_BIN>", data.ATMBin,
		"<ACTOR>", data.Actor,
		"<RUN_ID>", data.RunID,
		"<TIMESTAMP>", data.Timestamp,
		"<PERSONA_BLOCK>", personaBlock,
	)
	return replacer.Replace(contextV1)
}
```

Add `Persona` and `PersonaPrompt` to `ContextData`, and import `fmt`.

```go
type ContextData struct {
	Code, Name, ATMBin, Actor, RunID, Timestamp string
	Persona, PersonaPrompt                       string
}
```

- [ ] **Step 5: Run to verify context test passes**

Run: `go test ./internal/developing/ -run TestRenderContext_Persona`
Expected: PASS. Then run the whole package: `go test ./internal/developing/` (fix any golden/context test that now sees an empty `<PERSONA_BLOCK>` line — trim the placeholder line to empty cleanly; if an existing golden asserts exact output, update it to expect the collapsed blank).

- [ ] **Step 6: Write the failing test (CLI launch env)**

In `internal/cli/developing_test.go`, add a dry-run assertion (follow the existing dry-run test pattern in that file — it already exercises `--dry-run` and inspects emitted env/argv):

```go
func TestDeveloping_PersonaEnvAndActor(t *testing.T) {
	st := newTestCLIState(t)
	mustRun(t, st, "project", "create", "--code", "ATM", "--name", "ATM", "--actor", "t")
	mustRun(t, st, "persona", "create", "--name", "staff", "--prompt", "high bar", "--actor", "t")
	out := mustRun(t, st, "developing", "claude", "--project", "ATM", "--persona", "staff", "--dry-run", "--output", "json")
	if !strings.Contains(out, `"ATM_PERSONA": "staff"`) ||
		!strings.Contains(out, `"ATM_AGENT": "claude"`) ||
		!strings.Contains(out, `"ATM_ACTOR": "staff@claude"`) {
		t.Fatalf("persona launch env wrong:\n%s", out)
	}
}
```

(Use whatever run/capture helper `developing_test.go` already uses instead of `mustRun` if named differently.)

- [ ] **Step 7: Run to verify it fails**

Run: `go test ./internal/cli/ -run TestDeveloping_Persona`
Expected: FAIL (`--persona` unknown / env absent).

- [ ] **Step 8: Wire `--persona` in `developing.go`**

Add `Persona string` to `developingOpts`. In `newDevelopingAgentCmd` and `newDevelopingOllamaCmd`, add:

```go
	cmd.Flags().StringVar(&opts.Persona, "persona", "", "persona name; injects its prompt and defaults actor to <persona>@<agent>")
```

In each `RunE`, after resolving the launcher/agent and before `runDeveloping`, when a persona is set compute the actor base and stash the prompt. Change `defaultDevelopingActor` handling: if `opts.Persona != ""` and no explicit `--actor`, the default actor becomes `<persona>@<agent>`. Implement inside `runDeveloping` for both call sites by passing the agent name (already available as the `agent` param):

In `runDeveloping`, after `s, err := st.openStore()`:

```go
	var personaPrompt string
	if opts.Persona != "" {
		p, err := s.GetPersona(opts.Persona)
		if err != nil {
			return err
		}
		personaPrompt = p.Prompt
		// Default actor to persona@agent unless the user set --actor explicitly.
		if opts.Actor == "" || opts.Actor == defaultDevelopingActor(l.Name(), st, "") {
			opts.Actor = opts.Persona + "@" + l.Name()
		}
	}
```

Note: `defaultDevelopingActor` is called in the command RunE before `runDeveloping`, so `opts.Actor` already holds `<agent>-dev` when the user did not pass `--actor`. The guard above replaces that default (but respects an explicit `--actor`). To make the "explicit" check reliable, move the `defaultDevelopingActor` call: pass the raw explicit value through and compute the default inside `runDeveloping`. Concretely:

- In both commands, keep `opts.Actor` as the raw `--actor` value (delete the `opts.Actor = defaultDevelopingActor(...)` line from RunE).
- At the top of `runDeveloping`, after opening the store and resolving persona:

```go
	if opts.Actor == "" {
		if opts.Persona != "" {
			opts.Actor = opts.Persona + "@" + l.Name()
		} else {
			opts.Actor = defaultDevelopingActor(l.Name(), st, "")
		}
	}
```

Then extend the context render and env:

```go
	rendered := developing.RenderContext(developing.ContextData{
		Code: p.Code, Name: p.Name, ATMBin: atmBin, Actor: opts.Actor,
		RunID: runID, Timestamp: store.RFC3339UTC(time.Now().UTC()),
		Persona: opts.Persona, PersonaPrompt: personaPrompt,
	})
```

And in `developingEnvValues`, add the two vars. Change its signature to accept agent + persona:

```go
func developingEnvValues(project, atmBin, actor, runID, contextPath, agent, persona string) map[string]string {
	m := map[string]string{
		"ATM_ROLE":         "developing",
		"ATM_PROJECT":      project,
		"ATM_BIN":          atmBin,
		"ATM_ACTOR":        actor,
		"ATM_RUN_ID":       runID,
		"ATM_CONTEXT_FILE": contextPath,
		"ATM_AGENT":        agent,
	}
	if persona != "" {
		m["ATM_PERSONA"] = persona
	}
	return m
}
```

Update the call site: `envValues := developingEnvValues(opts.Project, atmBin, opts.Actor, runID, contextPath, l.Name(), opts.Persona)`.

- [ ] **Step 9: Run tests**

Run: `go test ./internal/cli/ -run TestDeveloping ./internal/developing/`
Expected: PASS. Also run `go test ./internal/cli/` to catch any other developing test asserting the old env map (update those to include `ATM_AGENT`).

- [ ] **Step 10: Commit**

```bash
git add internal/developing/ internal/cli/developing.go internal/cli/developing_test.go
git commit -m "atm developing --persona: inject persona + set ATM_PERSONA/ATM_AGENT, actor=persona@agent (ATM-0052)"
```

---

## Task 10: TUI `[4] Actors` pane

**Files:**
- Create: `internal/tui/actors.go`
- Modify: `internal/tui/app.go` (`paneActors`, `numPanes`, tab `[4]`, render, statusHint)
- Test: `internal/tui/actors_test.go`

**Interfaces:**
- Consumes: `activity.Build`, `activity.Aggregate`, `s.ReadLog`, `s.LoadAliases`, `meterBar`, `dashboardLine`, `padToHeight`, `m.projectScope`, `m.styles`.
- Produces: `actorsModel` with `SetSize(w, h)`, `View() string`, `statusHint() string`, `Update`-style key handling for list/detail; a `refresh()` that reloads from the store.

**Design note (deviation from spec wording):** the spec said "fourth workspace pane"; the persistent 3-pane grid (Projects left; Tasks/Labels stacked right) has no clean slot for a fourth box without cramping. Per the spec's explicit deferral of "the exact split … to the plan," implement `[4] Actors` as a **maximized pane**: when focused, it replaces the whole workspace area (like a full-screen view); `[1]/[2]/[3]` return to the grid. This ships the activity chart cleanly and can graduate to a split later.

- [ ] **Step 1: Write the failing test**

```go
package tui

import (
	"strings"
	"testing"
)

func TestActorsPaneRendersChart(t *testing.T) {
	// mkTestModel is the existing TUI test constructor (see app_test.go /
	// tasks_test.go). It seeds a store; extend or reuse it to create a project
	// with a couple of actor-stamped events.
	m := mkActorsTestModel(t) // helper: project ATM with staff@claude:opus-4.8 activity
	m.SetSize(80, 24)
	m.focused = paneActors
	m.actors.refresh()
	view := m.actors.View()
	if !strings.Contains(view, "staff") {
		t.Fatalf("actors view missing persona row:\n%s", view)
	}
}

func TestTabReachesActorsPane(t *testing.T) {
	m := mkActorsTestModel(t)
	m.SetSize(80, 24)
	m.handleKey(key("4")) // use the same key-injection helper other app tests use
	if m.focused != paneActors {
		t.Fatalf("focused = %v, want paneActors", m.focused)
	}
}
```

(Match the exact constructor/key helpers used by `internal/tui/app_test.go`; do not invent new harness names — wire `mkActorsTestModel` on top of the existing one.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestActorsPane|TestTabReaches'`
Expected: FAIL (paneActors/actorsModel undefined).

- [ ] **Step 3: Implement `actorsModel`**

```go
package tui

import (
	"fmt"
	"strings"

	"atm/internal/activity"
)

type actorsModel struct {
	m            *Model
	width        int
	contentHeight int
	groups       []activity.Group
	cursor       int
	detail       bool // false = list, true = per-persona detail
}

func (a *actorsModel) SetSize(w, h int) { a.width = w; a.contentHeight = h }

func (a *actorsModel) statusHint() string {
	if a.detail {
		return "[Esc]back"
	}
	return "[Enter]detail [↑/↓]move"
}

// refresh reloads activity for the current project scope.
func (a *actorsModel) refresh() {
	a.groups = nil
	a.cursor = 0
	a.detail = false
	code := a.m.projectScope
	if code == "" {
		return
	}
	entries, err := a.m.store.ReadLog(code)
	if err != nil {
		return
	}
	aliases, _ := a.m.store.LoadAliases()
	a.groups = activity.Aggregate(activity.Build(entries, aliases), "persona")
}

func (a *actorsModel) handleKey(s string) {
	switch s {
	case "up", "k":
		if a.cursor > 0 {
			a.cursor--
		}
	case "down", "j":
		if a.cursor < len(a.groups)-1 {
			a.cursor++
		}
	case "enter":
		if len(a.groups) > 0 {
			a.detail = true
		}
	case "esc":
		a.detail = false
	}
}

func (a *actorsModel) View() string {
	if a.m.projectScope == "" {
		return padToHeight(dashboardLine(a.width, a.m.styles.Muted.Render("select a project (pane 1) to see actor activity")), a.contentHeight)
	}
	if a.detail && a.cursor < len(a.groups) {
		return a.renderDetail(a.groups[a.cursor])
	}
	return a.renderList()
}

func (a *actorsModel) renderList() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", dashboardLine(a.width, fmt.Sprintf("actor activity: %s", a.m.projectScope)))
	b.WriteString("\n")
	if len(a.groups) == 0 {
		b.WriteString(dashboardLine(a.width, a.m.styles.Muted.Render("no activity")))
		return padToHeight(b.String(), a.contentHeight)
	}
	total := 0
	nameW := 0
	for _, g := range a.groups {
		total += g.Count
		if len(g.Key) > nameW {
			nameW = len(g.Key)
		}
	}
	meterW := a.width - nameW - 16
	if meterW < 10 {
		meterW = 10
	}
	for i, g := range a.groups {
		percent := 0
		if total > 0 {
			percent = (g.Count*100 + total/2) / total
		}
		cursor := " "
		if i == a.cursor {
			cursor = ">"
		}
		line := fmt.Sprintf("%s%-*s %s %3d%% %4d", cursor, nameW, g.Key, meterBar(percent, meterW), percent, g.Count)
		b.WriteString(dashboardLine(a.width, line))
		b.WriteString("\n")
	}
	return padToHeight(b.String(), a.contentHeight)
}

func (a *actorsModel) renderDetail(g activity.Group) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", dashboardLine(a.width, fmt.Sprintf("persona: %s   (%d events)", g.Key, g.Count)))
	b.WriteString("\n")
	writeBreakdown(&b, a, "agents", g.Agents)
	writeBreakdown(&b, a, "models", g.Models)
	writeBreakdown(&b, a, "actions", g.Actions)
	b.WriteString("\n")
	b.WriteString(dashboardLine(a.width, a.m.styles.Muted.Render("[Esc] back")))
	return padToHeight(b.String(), a.contentHeight)
}

func writeBreakdown(b *strings.Builder, a *actorsModel, title string, counts map[string]int) {
	b.WriteString(dashboardLine(a.width, a.m.styles.Muted.Render(title)))
	b.WriteString("\n")
	if len(counts) == 0 {
		b.WriteString(dashboardLine(a.width, "  (none)"))
		b.WriteString("\n")
		return
	}
	// Deterministic order: count desc, key asc.
	type kv struct {
		k string
		v int
	}
	var rows []kv
	total := 0
	for k, v := range counts {
		rows = append(rows, kv{k, v})
		total += v
	}
	sortKV(rows)
	meterW := a.width - 24
	if meterW < 8 {
		meterW = 8
	}
	for _, r := range rows {
		percent := 0
		if total > 0 {
			percent = (r.v*100 + total/2) / total
		}
		line := fmt.Sprintf("  %-14s %s %4d", r.k, meterBar(percent, meterW), r.v)
		b.WriteString(dashboardLine(a.width, line))
		b.WriteString("\n")
	}
}

func sortKV(rows []struct {
	k string
	v int
}) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0; j-- {
			a, b := rows[j-1], rows[j]
			if b.v > a.v || (b.v == a.v && b.k < a.k) {
				rows[j-1], rows[j] = rows[j], rows[j-1]
			} else {
				break
			}
		}
	}
}
```

(If `go vet`/staticcheck objects to the anonymous-struct parameter on `sortKV`, hoist a named `type kvRow struct{ k string; v int }` to package scope and use it in both `writeBreakdown` and `sortKV`.)

- [ ] **Step 4: Wire the pane into `app.go`**

1. Enum + count:

```go
const (
	paneProjects workspacePane = iota
	paneTasks
	paneLabels
	paneActors
)

const numPanes = 4
```

2. Add `actors actorsModel` to `Model`; initialize it in the constructor (`NewModel`) with `m.actors = actorsModel{m: &m}` (mirror how labels/tasks sub-models get a back-pointer — match the existing pattern exactly).

3. In `SetSize`, size the actors pane to the full workspace area:

```go
	m.actors.SetSize(innerPaneWidth(m.width), innerPaneHeight(m.contentHeight))
```

4. Tab switch — add to the `switch k.String()` block:

```go
	case "4":
		m.focused = paneActors
		m.actors.refresh()
		return nil
```

5. Route keys to the actors pane when focused (in the per-pane key dispatch `switch m.focused { ... }`):

```go
	case paneActors:
		a := m.actors
		m.actors.handleKey(k.String())
		_ = a
		return nil
```

(Match the real dispatch signature in `app.go`; the key point is `m.actors.handleKey(k.String())` runs when `m.focused == paneActors`.)

6. Maximized render — at the top of `renderWorkspace`:

```go
	if m.focused == paneActors {
		return m.renderPane(paneActors, m.width, m.contentHeight, "[4] Actors", m.actors.View())
	}
```

7. statusHint — add a case:

```go
	case paneActors:
		return m.actors.statusHint()
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/tui/ -run 'TestActorsPane|TestTabReaches'`
Expected: PASS. Then `go test ./internal/tui/` (update any layout golden/snapshot that enumerates panes or asserts `numPanes == 3`).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/actors.go internal/tui/app.go internal/tui/actors_test.go
git commit -m "Add [4] Actors TUI pane: activity chart + per-persona detail (ATM-0052)"
```

---

## Task 11: Docs, conventions, and full verification

**Files:**
- Modify: `README.md` (personas + actor convention + `atm activity`/`atm actor migrate`)
- Modify: whatever `atm conventions` renders from (grep `conventions`) to document the `persona@agent:model` actor convention.
- Modify: `CHANGELOG.md`

**Interfaces:** none (docs only).

- [ ] **Step 1: Locate the conventions source**

Run: `grep -rn "conventions" internal/cli/ | grep -iv _test`
Read the command's text source and add a short paragraph:

> Actors follow the convention `persona@agent:model` (e.g. `staff-engineer@claude:opus-4.8`). Personas are global (`atm persona`); the agent stamps its own model. Legacy actors are mapped by `atm actor migrate`.

- [ ] **Step 2: Update README**

Add a "Personas & actor activity" section documenting: `atm persona` CRUD, `atm developing <agent> --persona <name>`, the `persona@agent:model` convention, `atm activity --project`, and `atm actor migrate`. Keep it consistent with the existing README voice.

- [ ] **Step 3: Update CHANGELOG.md**

Add an entry under the current unreleased/next version summarizing the feature.

- [ ] **Step 4: Full verification**

Run: `make verify`
Expected: build + all tests PASS.

- [ ] **Step 5: Manual smoke (real binary)**

```bash
make build
./bin/atm persona create --name staff-engineer --prompt "Holds a high bar in review." --actor me
./bin/atm persona list
./bin/atm developing claude --project ATM --persona staff-engineer --dry-run
./bin/atm actor migrate --dry-run
./bin/atm activity --project ATM --output json
```

Expected: persona created and listed; dry-run launch shows `ATM_PERSONA`, `ATM_AGENT`, `ATM_ACTOR=staff-engineer@claude` and a `## Persona: staff-engineer` block in the rendered context path; migrate dry-run lists legacy actor mappings; activity prints grouped counts.

- [ ] **Step 6: Commit**

```bash
git add README.md CHANGELOG.md internal/cli/
git commit -m "Document personas, actor convention, and activity (ATM-0052)"
```

---

## Self-Review

**Spec coverage:**
- Persona entity (global, `{name, prompt, description}`) → Task 3. ✓
- Built-in `developer`/`manager` seed → Task 2 + `SeedPersonas` Task 3. ✓
- Actor convention `persona@agent:model` + resolution order (alias → parse → `(none)`) → Task 1. ✓
- Alias table + `atm actor migrate`/`alias` → Tasks 4, 6. ✓
- `atm persona` CLI → Task 5. ✓
- Launch `--persona`, ATM_PERSONA/ATM_AGENT, persona-injected context, actor=persona@agent → Task 9. ✓
- `atm activity` API → Tasks 7, 8. ✓
- `[4] Actors` TUI pane → Task 10 (maximized-pane variant, deviation documented). ✓
- Docs/conventions → Task 11. ✓
- No new log action / no Replay change → honored (personas & aliases are plain global files). ✓

**Placeholder scan:** No TBD/TODO; each code step carries full implementations. TUI/CLI test steps intentionally defer to the repo's existing test harness helpers (constructor + run/capture + key-injection) rather than inventing new ones — the implementer must reuse the real names from `app_test.go` / `label_test.go` / `developing_test.go`. This is a deliberate instruction, not a placeholder.

**Type consistency:** `actor.Identity`/`AliasEntry`/`AliasMap` used consistently across Tasks 1, 4, 7. `store.Persona` json tags reused by CLI (Task 5) and never re-marshaled. `activity.Group` fields (`Key/Count/Agents/Models/Actions`) match between Tasks 7, 8, 10. `developingEnvValues` signature change is applied at its single call site (Task 9). `EditPersona(name, *string, *string, actor)` pointer semantics consistent between store (Task 3) and CLI (Task 5).

**Open implementer note:** Tasks 5/6/8/9/10 depend on existing test harness helper names in `internal/cli` and `internal/tui`. Before writing those tests, read the sibling `_test.go` files and reuse their constructors/among (`newTestCLIState`/`runCLI`/`mkTestModel` or whatever they are actually called) — names in this plan are indicative.
