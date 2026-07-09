# Actor Convention Enforcement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `persona@agent:model` the only actor form written to the log, enforced at the store against a registered persona, with client-owned resolution and pure read-time inference for legacy strings.

**Architecture:** The store gains a `validateActor` gate called by every mutation (valid shape + registered persona). The client (host agent via injected prompt, or the ATM binary for its own human CLI/TUI sessions) builds the full triple; the binary supplies floor defaults (`persona@agent:unset`). The alias/migration subsystem is deleted and replaced by a pure `actor.Resolve` that infers a persona from legacy strings at read time. A new protected `admin` built-in persona represents human operators.

**Tech Stack:** Go, Cobra CLI, Bubble Tea TUI, file-based JSON store with per-name flock. Tests use Go's `testing` package.

## Global Constraints

- Actor canonical form is `persona@agent:model` — all three segments non-empty. Copied verbatim into the log.
- Human non-prompt sessions stamp model segment `unset` (real values arrive with ATM-0083). CLI surface = `cli`, TUI surface = `tui`.
- Bootstrap system actor for seeding built-ins: `admin@atm:seed`.
- Built-in personas: `developer`, `manager`, `admin` — seeded on demand, never removable.
- No log rewrite / no data migration. Existing `actor-aliases.json` files are left inert on disk.
- Unknown/unparseable legacy actor string resolves to persona `developer` (preserves current activity aggregation); the empty-persona convention case resolves to `(none)` (`actor.NonePersona`).
- Follow existing store patterns: actor validation happens at the top of each mutation, before `WithLock`.

---

### Task 1: Add `admin` built-in persona + protect built-ins from removal

**Files:**
- Modify: `internal/seed/persona.go` (append `admin` to `Personas`)
- Modify: `internal/store/persona.go:129` (`RemovePersona` guard)
- Test: `internal/store/persona_test.go`

**Interfaces:**
- Produces: `seed.Personas` now contains `developer`, `manager`, `admin`. `seedPersonas()` (existing, `internal/store/persona.go:162`) returns all three names. `RemovePersona(name)` returns `ErrUsage` for any built-in.

- [ ] **Step 1: Write the failing test**

In `internal/store/persona_test.go`:

```go
func TestRemovePersonaRejectsBuiltins(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.SeedPersonas("admin@atm:seed"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for _, name := range []string{"developer", "manager", "admin"} {
		if err := s.RemovePersona(name); !errors.Is(err, ErrUsage) {
			t.Errorf("RemovePersona(%q) = %v, want ErrUsage", name, err)
		}
		if _, err := s.GetPersona(name); err != nil {
			t.Errorf("built-in %q was removed: %v", name, err)
		}
	}
}

func TestSeedPersonasIncludesAdmin(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.SeedPersonas("admin@atm:seed"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := s.GetPersona("admin"); err != nil {
		t.Errorf("admin not seeded: %v", err)
	}
}
```

Ensure `errors` is imported in the test file.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'TestRemovePersonaRejectsBuiltins|TestSeedPersonasIncludesAdmin' -v`
Expected: FAIL — `admin` not seeded / built-in removed successfully.

- [ ] **Step 3: Add the `admin` persona to the seed set**

In `internal/seed/persona.go`, append to the `Personas` slice (after `manager`):

```go
	{
		Name:        "admin",
		Description: "Human operator persona: a person driving ATM directly via the CLI or TUI, not an autonomous agent.",
		Prompt: "You are the human operator of ATM, acting directly through the CLI or " +
			"TUI rather than as an autonomous agent. Keep the ledger honest and legible: " +
			"record intent and outcomes plainly, and prefer small, reversible changes.",
	},
```

- [ ] **Step 4: Guard `RemovePersona` against built-ins**

In `internal/store/persona.go`, at the top of `RemovePersona` (after `ValidatePersonaName`):

```go
	for _, b := range seedPersonas() {
		if b == name {
			return fmt.Errorf("%w: cannot remove built-in persona %q", ErrUsage, name)
		}
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestRemovePersona|TestSeedPersonas' -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/seed/persona.go internal/store/persona.go internal/store/persona_test.go
git commit -m "ATM-0072: add admin built-in persona; protect built-ins from removal

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: Pure `actor.Resolve` + delete the alias/migration subsystem

This task is a single compilation-consistent change: `Resolve` loses its `aliases` argument and folds the legacy inference inline, and every consumer plus the alias/migration code is removed together. Read behavior is preserved; write enforcement comes in Task 3.

**Files:**
- Modify: `internal/actor/actor.go` (rewrite `Resolve`; remove `AliasMap`, `AliasEntry`, `LegacyAlias`)
- Modify: `internal/actor/actor_test.go`
- Modify: `internal/activity/activity.go:19` (`Build` drops `aliases`)
- Modify: `internal/activity/activity_test.go`
- Modify: `internal/cli/activity.go:34-38`
- Modify: `internal/tui/actors.go:57-58`
- Modify: `internal/tui/projects.go:499-500`
- Modify: `internal/cli/root.go` (remove `newActorCmd` registration)
- Delete: `internal/store/alias.go`, `internal/store/alias_test.go`
- Delete: `internal/cli/actor.go`, `internal/cli/actor_test.go`

**Interfaces:**
- Produces: `actor.Resolve(raw string) actor.Identity` (no aliases). `activity.Build(entries []store.LogEntry) []activity.Record`. The `atm actor` command no longer exists. `store.LoadAliases/SetAlias/RemoveAlias/MigrateActors` no longer exist.

- [ ] **Step 1: Write the failing test for the pure resolver**

Replace the alias-based cases in `internal/actor/actor_test.go` with:

```go
func TestResolveConvention(t *testing.T) {
	got := Resolve("developer@claude:opus-4.8")
	want := Identity{Persona: "developer", Agent: "claude", Model: "opus-4.8"}
	if got != want {
		t.Errorf("Resolve = %+v, want %+v", got, want)
	}
}

func TestResolveLegacyInference(t *testing.T) {
	cases := map[string]Identity{
		"default":        {Persona: "developer"},
		"claude":         {Persona: "developer", Agent: "claude"},
		"ollama-dev":     {Persona: "developer", Agent: "ollama"},
		"atm-manager":    {Persona: "manager"},
		"codex-manager":  {Persona: "manager", Agent: "codex"},
		"ollama-onboard": {Persona: "manager", Agent: "ollama"},
		"somethingelse":  {Persona: "developer"},
		"":               {Persona: NonePersona},
	}
	for raw, want := range cases {
		if got := Resolve(raw); got != want {
			t.Errorf("Resolve(%q) = %+v, want %+v", raw, got, want)
		}
	}
}
```

Remove any test referencing `AliasMap`, `AliasEntry`, or `LegacyAlias`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/actor/ -run TestResolve -v`
Expected: FAIL — `Resolve` still takes two args / signature mismatch (compile error).

- [ ] **Step 3: Rewrite `internal/actor/actor.go`**

Replace the file body below the package doc with:

```go
package actor

import "strings"

type Identity struct {
	Persona string
	Agent   string
	Model   string
}

const NonePersona = "(none)"

var Agents = []string{"claude", "codex", "opencode", "ollama"}

// Resolve maps a raw actor string to an Identity. Convention strings
// (persona@agent:model) are parsed; legacy strings are inferred to a persona
// at read time. Pure function — no alias table, no store dependency.
func Resolve(raw string) Identity {
	if strings.Contains(raw, "@") {
		persona, rest, _ := strings.Cut(raw, "@")
		agent, model, _ := strings.Cut(rest, ":")
		return normalize(Identity{Persona: persona, Agent: agent, Model: model})
	}
	return inferLegacy(raw)
}

func normalize(i Identity) Identity {
	if i.Persona == "" {
		i.Persona = NonePersona
	}
	return i
}

// inferLegacy maps a pre-convention actor string (no '@') to an Identity using
// ATM's own generated-default patterns.
func inferLegacy(raw string) Identity {
	switch raw {
	case "default":
		return Identity{Persona: "developer"}
	case "atm-manager":
		return Identity{Persona: "manager"}
	}
	for _, a := range Agents {
		switch raw {
		case a, a + "-dev":
			return Identity{Persona: "developer", Agent: a}
		case a + "-manager", a + "-onboard":
			return Identity{Persona: "manager", Agent: a}
		}
	}
	// Anything else without '@': default working persona, unknown agent.
	return Identity{Persona: "developer"}
}
```

- [ ] **Step 4: Update `activity.Build` and its consumers**

In `internal/activity/activity.go`, change:

```go
func Build(entries []store.LogEntry) []Record {
	out := make([]Record, 0, len(entries))
	for _, e := range entries {
		id := actor.Resolve(e.Actor)
		out = append(out, Record{Persona: id.Persona, Agent: id.Agent, Model: id.Model, Action: e.Action})
	}
	return out
}
```

In `internal/cli/activity.go` (lines ~34-38) remove the `LoadAliases` call:

```go
			groups := activity.Aggregate(activity.Build(entries), groupBy)
```

In `internal/tui/actors.go` (lines 57-58):

```go
	a.groups = activity.Aggregate(activity.Build(entries), "persona")
```

In `internal/tui/projects.go` (lines 499-500):

```go
	groups := activity.Aggregate(activity.Build(entries), "persona")
```

Update `internal/activity/activity_test.go` to call `Build(entries)` (drop the aliases arg); replace any alias-map fixtures with raw legacy strings that infer via `Resolve`.

- [ ] **Step 5: Delete the alias/migration code and command**

```bash
git rm internal/store/alias.go internal/store/alias_test.go internal/cli/actor.go internal/cli/actor_test.go
```

In `internal/cli/root.go`, remove the line registering the actor command (search for `newActorCmd`):

```bash
grep -n "newActorCmd" internal/cli/root.go
```

Delete that `root.AddCommand(newActorCmd(st))` line.

- [ ] **Step 6: Verify the whole tree compiles and package tests pass**

Run: `go build ./... && go test ./internal/actor/ ./internal/activity/ -v`
Expected: build succeeds; actor + activity tests PASS. (Store/cli tests may still reference deleted symbols — fix any remaining compile errors from removed alias references now, e.g. in `internal/store` migration tests.)

Run: `go vet ./...`
Expected: no references to `LoadAliases`, `MigrateActors`, `LegacyAlias`, `AliasMap`.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "ATM-0072: pure read-time actor.Resolve; delete alias/migration subsystem

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: Store `validateActor` gate + bootstrap seeding + wire into every mutation

**Files:**
- Modify: `internal/store/store.go` (add `builtinsOnce sync.Once` field + `ensureBuiltinPersonas`)
- Create: `internal/store/actor_validate.go` (`validateActor`)
- Modify: `internal/store/persona.go` (`createPersona(..., validate bool)` split; `SeedPersonas` skips validation)
- Modify: `internal/store/task.go`, `internal/store/comment.go`, `internal/store/label.go`, `internal/store/project.go`, `internal/store/config.go` (replace `if actor == ""` with `validateActor`)
- Modify: store test helper `internal/store/project_test.go:53` (`newTestStore`) + sweep test actor literals
- Test: `internal/store/actor_validate_test.go`

**Interfaces:**
- Consumes: `seed.Personas`, `seedPersonas()`, `actor.Resolve` (Task 2).
- Produces: `(s *Store) validateActor(raw string) error` — `ErrUsage` on bad shape or unregistered persona. `(s *Store) ensureBuiltinPersonas() error`. `SeedPersonas(actor)` writes built-ins without validation. Test constant `testActor = "admin@cli:test"`.

- [ ] **Step 1: Write the failing test**

Create `internal/store/actor_validate_test.go`:

```go
package store

import (
	"errors"
	"testing"
)

func TestValidateActor(t *testing.T) {
	s := newTestStore(t)
	good := []string{"developer@claude:opus-4.8", "admin@cli:unset", "manager@codex:unset"}
	for _, a := range good {
		if err := s.validateActor(a); err != nil {
			t.Errorf("validateActor(%q) = %v, want nil", a, err)
		}
	}
	bad := []string{"", "developer", "developer@claude", "@claude:x", "developer@:x", "developer@claude:"}
	for _, a := range bad {
		if err := s.validateActor(a); !errors.Is(err, ErrUsage) {
			t.Errorf("validateActor(%q) = %v, want ErrUsage", a, err)
		}
	}
}

func TestValidateActorUnregisteredPersona(t *testing.T) {
	s := newTestStore(t)
	if err := s.validateActor("ghost@cli:unset"); !errors.Is(err, ErrUsage) {
		t.Errorf("validateActor(ghost) = %v, want ErrUsage", err)
	}
}

func TestCreateTaskRejectsUnregisteredPersona(t *testing.T) {
	s := newTestStore(t)
	mustProject(t, s)
	if _, err := s.CreateTask("ATM", "t", "", nil, "ghost@cli:unset"); !errors.Is(err, ErrUsage) {
		t.Errorf("CreateTask with unregistered persona = %v, want ErrUsage", err)
	}
}
```

If a `mustProject` helper does not already exist, use the existing project-creation helper in the store test suite (grep `CreateProject` in `internal/store/*_test.go`) and inline it.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'TestValidateActor|TestCreateTaskRejectsUnregistered' -v`
Expected: FAIL — `validateActor` undefined.

- [ ] **Step 3: Add bootstrap seeding to the store**

In `internal/store/store.go`, add to the `Store` struct:

```go
	builtinsOnce sync.Once
	builtinsErr  error
```

Add `"sync"` to imports. Add the method (new file or append):

```go
// ensureBuiltinPersonas seeds developer/manager/admin once per Store, skipping
// actor validation (the personas being created cannot yet satisfy it).
func (s *Store) ensureBuiltinPersonas() error {
	s.builtinsOnce.Do(func() {
		_, s.builtinsErr = s.SeedPersonas("admin@atm:seed")
	})
	return s.builtinsErr
}
```

- [ ] **Step 4: Split persona creation into validated / unvalidated paths**

In `internal/store/persona.go`, refactor `CreatePersona` so its locked body is shared:

```go
func (s *Store) CreatePersona(name, prompt, description, actor string) (*Persona, error) {
	if err := ValidatePersonaName(name); err != nil {
		return nil, err
	}
	if err := s.validateActor(actor); err != nil {
		return nil, err
	}
	return s.createPersonaLocked(name, prompt, description, actor)
}

// createPersonaLocked writes the persona file. It performs NO actor validation
// and is the path used by SeedPersonas during bootstrap.
func (s *Store) createPersonaLocked(name, prompt, description, actor string) (*Persona, error) {
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
```

Change `SeedPersonas` to call `createPersonaLocked` (skips validation) instead of `CreatePersona`:

```go
		_, err := s.createPersonaLocked(sp.Name, sp.Prompt, sp.Description, actor)
```

Add `s.validateActor(actor)` to `EditPersona` (replacing its `actor == ""` check).

- [ ] **Step 5: Implement `validateActor`**

Create `internal/store/actor_validate.go`:

```go
package store

import (
	"fmt"
	"strings"
)

// validateActor enforces the canonical actor form persona@agent:model with a
// registered persona. Called at the top of every mutation, before WithLock.
func (s *Store) validateActor(raw string) error {
	if err := s.ensureBuiltinPersonas(); err != nil {
		return err
	}
	persona, rest, ok := strings.Cut(raw, "@")
	if !ok {
		return fmt.Errorf("%w: actor must be persona@agent:model (got %q)", ErrUsage, raw)
	}
	agent, model, ok := strings.Cut(rest, ":")
	if !ok || persona == "" || agent == "" || model == "" {
		return fmt.Errorf("%w: actor must be persona@agent:model (got %q)", ErrUsage, raw)
	}
	if _, err := s.GetPersona(persona); err != nil {
		if IsNotFound(err) {
			return fmt.Errorf("%w: unknown persona %q; create it first with 'atm persona create'", ErrUsage, persona)
		}
		return err
	}
	return nil
}
```

- [ ] **Step 6: Wire `validateActor` into every mutation**

Replace each `if actor == "" { return ...ErrUsage...actor is required... }` block with:

```go
	if err := s.validateActor(actor); err != nil {
		return err   // or `return nil, err` for methods returning (T, error)
	}
```

Sites to update (verify with the grep below):
- `internal/store/task.go`: `CreateTask`, `TaskLabelAdd`, `RemoveTask`, `mutateTask`
- `internal/store/comment.go`: `CreateComment`, `SetCommentBody`, `RemoveComment`, and comment-label add/remove
- `internal/store/label.go`: `LabelAdd`, `LabelSeed`, `LabelRemove`
- `internal/store/config.go`: `SetEmbeddingConfig`
- `internal/store/project.go`: project create/edit mutations
- `internal/store/persona.go`: `CreatePersona`, `EditPersona` (done in Step 4)

Find every remaining site:

```bash
grep -rn 'actor is required' internal/store/*.go
```

Every hit must become a `validateActor` call. Leave `RemovePersona` (no actor param) alone.

- [ ] **Step 7: Migrate the store test suite to conforming actors**

Add a shared constant near `newTestStore` in `internal/store/project_test.go`:

```go
const testActor = "admin@cli:test"
```

`newTestStore` needs the built-ins available; they are seeded lazily on the first `validateActor`, so no change is required there unless a test writes before any mutation — in that case call `s.ensureBuiltinPersonas()` in the helper.

Sweep the dominant non-conforming literals:

```bash
grep -rl '"tester"\|"testing"' internal/store/*_test.go | while read f; do
  sed -i 's/"tester"/"admin@cli:test"/g; s/"testing"/"admin@cli:test"/g' "$f"
done
```

For tests that intentionally write legacy actors to exercise activity aggregation (e.g. literals `"opencode-dev"`, `"ollama-manager"`), convert them: either (a) write with a conforming actor and assert `actor.Resolve` separately, or (b) move the assertion into `internal/actor` unit tests (Task 2 already covers inference). Grep them:

```bash
grep -rn '"opencode-dev"\|"ollama-manager"\|"opencode-manager"\|"tester2"' internal/store/*_test.go
```

- [ ] **Step 8: Run the store suite**

Run: `go test ./internal/store/ -v`
Expected: PASS. Fix any remaining `ErrUsage: actor must be persona@agent:model` failures by converting the offending test's actor to a conforming, registered value.

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "ATM-0072: store validateActor gate + bootstrap persona seeding

Every mutation now requires actor=persona@agent:model with a registered
persona. Built-ins seeded lazily via a validation-skipping path.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: Client-side default actors (CLI, TUI, developing, manager)

**Files:**
- Modify: `internal/cli/root.go:106-114` (`resolveActor`)
- Modify: `internal/tui/app.go:143-146` (TUI fallback + bare-persona expansion)
- Modify: `internal/cli/developing.go:179-214` (`defaultDevelopingActor`, effective persona)
- Modify: `internal/cli/manager.go:182-190` and `:146,167` (`defaultManagerActor`)
- Test: `internal/cli/root_test.go` (or the existing actor-resolution test file), `internal/cli/developing_test.go`, `internal/tui/app_test.go`

**Interfaces:**
- Consumes: store `validateActor` (Task 3) as the authoritative backstop.
- Produces: `resolveActor` returns `admin@cli:unset` (empty) / `<name>@cli:unset` (bare) / raw (has `@`). `defaultDevelopingActor` → `<persona>@<agent>:unset`. `defaultManagerActor` → `manager@<agent>:unset`. TUI fallback `admin@tui:unset`.

- [ ] **Step 1: Write the failing tests**

In the CLI test suite (add `internal/cli/actor_resolve_test.go`):

```go
func TestResolveActorDefaults(t *testing.T) {
	cases := map[string]string{
		"":                       "admin@cli:unset",
		"reviewer":               "reviewer@cli:unset",
		"developer@claude:opus":  "developer@claude:opus",
	}
	for in, want := range cases {
		st := &cliState{flags: cliFlags{actor: in}}
		got, err := st.resolveActor(true)
		if err != nil || got != want {
			t.Errorf("resolveActor(%q) = %q,%v; want %q", in, got, err, want)
		}
	}
}
```

(Match `cliState`/`cliFlags` construction to the existing test helpers in `internal/cli`.)

In `internal/tui/app_test.go`:

```go
func TestNewModelDefaultActor(t *testing.T) {
	m := newTestModel(t) // existing helper; launched without an actor
	if m.actor != "admin@tui:unset" {
		t.Errorf("default TUI actor = %q, want admin@tui:unset", m.actor)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run TestResolveActorDefaults -v && go test ./internal/tui/ -run TestNewModelDefaultActor -v`
Expected: FAIL — returns `anonymous` / `default`.

- [ ] **Step 3: Rewrite `resolveActor`**

In `internal/cli/root.go`:

```go
func (s *cliState) resolveActor(required bool) (string, error) {
	raw := s.flags.actor
	if raw == "" {
		return "admin@cli:unset", nil
	}
	if !strings.Contains(raw, "@") {
		return raw + "@cli:unset", nil
	}
	return raw, nil
}
```

Add `"strings"` to imports if absent. (The `required` parameter is now always satisfiable; keep the signature to avoid touching all callers. The store re-validates that the persona is registered, producing the friendly error.)

- [ ] **Step 4: Update the TUI fallback + bare-persona expansion**

In `internal/tui/app.go` (lines 143-146):

```go
	actor := opts.Actor
	switch {
	case actor == "":
		actor = "admin@tui:unset"
	case !strings.Contains(actor, "@"):
		actor = actor + "@tui:unset"
	}
```

Add `"strings"` to the file's imports if not present. Update the stale comments at `app.go:325-338` to say the actor defaults to `admin@tui:unset`.

- [ ] **Step 5: Update developing defaults + effective persona**

In `internal/cli/developing.go`, replace the block at lines ~200-214 with:

```go
	effectivePersona := opts.Persona
	if effectivePersona == "" {
		effectivePersona = "developer"
	}
	pp, err := s.GetPersona(effectivePersona)
	if err != nil {
		return err // unregistered --persona fails fast
	}
	personaPrompt := pp.Prompt
	personaDescription := pp.Description
	if opts.Actor == "" {
		opts.Actor = effectivePersona + "@" + l.Name() + ":unset"
	}
```

Pass `Persona: effectivePersona`, `PersonaPrompt: personaPrompt`, `PersonaDescription: personaDescription` into the `developing.RenderContext` call (Task 5 adds the field). Delete the now-unused `defaultDevelopingActor` helper (lines 179-187) and its call site; if kept elsewhere, adjust to return `effectivePersona + "@" + agent + ":unset"`.

- [ ] **Step 6: Update manager defaults + effective persona**

In `internal/cli/manager.go`, change `defaultManagerActor` (lines 182-190):

```go
func defaultManagerActor(agent string, st *cliState, explicit string) string {
	if explicit != "" {
		return explicit
	}
	if st.flags.actor != "" {
		return st.flags.actor
	}
	return "manager@" + agent + ":unset"
}
```

In `runManager` (before `RenderContext`, ~line 254), look up the manager persona and pass it into the context:

```go
	mp, err := s.GetPersona("manager")
	if err != nil {
		return err
	}
```

Add `Persona: "manager"`, `PersonaPrompt: mp.Prompt`, `PersonaDescription: mp.Description` to the `manager.RenderContext` call (Task 5 adds these fields).

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/cli/ ./internal/tui/ -run 'ResolveActor|DefaultActor|Developing|Manager' -v`
Expected: PASS. Update any existing test asserting old defaults (`anonymous`, `<agent>-dev`, `<agent>-manager`, `default`) to the new floor values.

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "ATM-0072: client floor defaults persona@agent:unset (cli/tui/developing/manager)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: Inject persona description + model-stamp instruction into prompts

**Files:**
- Modify: `internal/developing/context.go` (add `PersonaDescription`, always render block)
- Modify: `internal/developing/context_v1.md` (model-stamp instruction line)
- Modify: `internal/manager/context.go` (add persona fields + block)
- Modify: `internal/manager/context_v1.md` (`<PERSONA_BLOCK>` + model instruction)
- Test: `internal/developing/context_test.go`, `internal/manager/context_test.go`

**Interfaces:**
- Consumes: `PersonaDescription`, `PersonaPrompt`, `Persona` passed by Task 4 launchers.
- Produces: `developing.ContextData` and `manager.ContextData` both carry `Persona`, `PersonaPrompt`, `PersonaDescription`; rendered output includes the persona description and a "stamp your real model" instruction.

- [ ] **Step 1: Write failing tests**

In `internal/developing/context_test.go`:

```go
func TestRenderContextIncludesPersonaDescription(t *testing.T) {
	out := RenderContext(ContextData{
		Persona:            "developer",
		PersonaPrompt:      "do good work",
		PersonaDescription: "Default working persona.",
		Actor:              "developer@claude:unset",
	})
	for _, want := range []string{"developer", "Default working persona.", "do good work"} {
		if !strings.Contains(out, want) {
			t.Errorf("context missing %q", want)
		}
	}
}
```

In `internal/manager/context_test.go`:

```go
func TestManagerContextInjectsPersona(t *testing.T) {
	out := RenderContext(ContextData{
		Persona:            "manager",
		PersonaPrompt:      "curate the ledger",
		PersonaDescription: "Curates the ledger and oversees work.",
	})
	if !strings.Contains(out, "curate the ledger") {
		t.Error("manager context missing persona prompt")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/developing/ ./internal/manager/ -run 'PersonaDescription|InjectsPersona' -v`
Expected: FAIL — `PersonaDescription` field missing / manager has no persona block.

- [ ] **Step 3: Extend developing context**

In `internal/developing/context.go`, add `PersonaDescription string` to `ContextData` and render the description in the persona block:

```go
	personaBlock := ""
	if data.Persona != "" {
		personaBlock = fmt.Sprintf("## Persona: %s\n\n%s\n\n%s\n\nYou are operating as this persona. Hold to its principles throughout the session, alongside repo instructions and the working routine below.\n",
			data.Persona, data.PersonaDescription, data.PersonaPrompt)
	}
```

In `internal/developing/context_v1.md`, add a line near the actor line (line 3) instructing the agent to stamp its model:

```markdown
Stamp every ATM mutation with actor `<ACTOR>` — replace the `:unset` model segment with your actual model (e.g. `:opus-4.8`).
```

- [ ] **Step 4: Extend manager context**

In `internal/manager/context.go`, add `Persona`, `PersonaPrompt`, `PersonaDescription` to `ContextData`, and render a persona block. Add to the `pairs` slice a `"<PERSONA_BLOCK>"` entry built like the developing version (empty when `Persona == ""` so `render-context` with no project still works):

```go
	personaBlock := ""
	if data.Persona != "" {
		personaBlock = fmt.Sprintf("## Persona: %s\n\n%s\n\n%s\n", data.Persona, data.PersonaDescription, data.PersonaPrompt)
	}
	pairs := []string{
		"<CODE>", data.Code,
		"<PROJECT_NAME>", data.Name,
		"<ATM_BIN>", data.ATMBin,
		"<ACTOR>", data.Actor,
		"<RUN_ID>", data.RunID,
		"<TIMESTAMP>", data.Timestamp,
		"<PERSONA_BLOCK>", personaBlock,
	}
```

Note: `<PERSONA_BLOCK>` must be exempt from the "empty leaves the placeholder" rule — when `personaBlock` is empty, substitute it with empty string (append `key, ""`), not the placeholder. Adjust the empty-value loop accordingly, e.g. special-case `<PERSONA_BLOCK>`.

Add `<PERSONA_BLOCK>` and a model-stamp instruction to `internal/manager/context_v1.md` (near the top, after the intro line).

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/developing/ ./internal/manager/ -v`
Expected: PASS. Update existing golden/context tests that assert the exact rendered block.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "ATM-0072: inject persona description + model-stamp instruction into prompts

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 6: Update the `conventions` documentation

**Files:**
- Modify: `internal/cli/conventions.go:39,105` (actor-identity text)
- Test: `internal/cli/conventions_test.go` (if present) — otherwise a new assertion

**Interfaces:**
- Produces: `atm conventions` output describes `persona@agent:model` as mandatory with a registered persona; no `atm actor migrate`/`alias` references.

- [ ] **Step 1: Write the failing test**

In `internal/cli/conventions_test.go` (create if absent, matching the CLI test harness):

```go
func TestConventionsActorText(t *testing.T) {
	out := conventionsText() // or the exported map value used by the command
	if strings.Contains(out, "actor migrate") || strings.Contains(out, "actor alias") {
		t.Error("conventions still references the removed alias subsystem")
	}
	if !strings.Contains(out, "persona@agent:model") || !strings.Contains(out, "registered persona") {
		t.Error("conventions does not describe the enforced actor convention")
	}
}
```

Match the accessor to how `conventions.go` exposes its text (a function or a map literal).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestConventionsActorText -v`
Expected: FAIL — text still mentions `atm actor migrate`.

- [ ] **Step 3: Rewrite the actor-identity text**

In `internal/cli/conventions.go`, replace the actor-identity passages (lines 39 and 105) so they state:

- Every mutation stamps an actor of the form `persona@agent:model`; the store rejects anything else.
- The `persona` segment must be a **registered** persona (`atm persona create` first); the `agent`/`model` segments are supplied by the host agent (human CLI/TUI sessions use `<surface>:unset` until model config lands).
- Legacy actor strings in older logs are inferred to a persona at read time; there is no migration step.
- Remove all mentions of `atm actor migrate` and `atm actor alias`.

Keep the `atm activity` / Projects-pane guidance intact.

- [ ] **Step 4: Run the test**

Run: `go test ./internal/cli/ -run TestConventionsActorText -v`
Expected: PASS

- [ ] **Step 5: Full suite + build**

Run: `go build ./... && go test ./...`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "ATM-0072: rewrite conventions actor-identity docs for enforced convention

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Final verification

- [ ] `go build ./... && go test ./...` — all green.
- [ ] `go vet ./...` — no dangling references to removed symbols.
- [ ] Manual smoke (fresh temp store):
  ```bash
  export ATM_HOME=$(mktemp -d)
  atm init
  atm persona list --output json | grep -q admin && echo "admin seeded OK"
  atm project create --code DEMO --name Demo
  atm task create --project DEMO --title "hi"            # no --actor -> admin@cli:unset
  atm task create --project DEMO --title "x" --actor ghost@cli:unset  # expect ErrUsage
  atm activity --project DEMO --group-by persona --output json        # admin bucket present
  ```
  Expected: task 1 succeeds and its log actor is `admin@cli:unset`; the `ghost` task errors with "unknown persona"; activity shows the `admin` persona.
```

## Notes for the implementer

- **Task ordering matters for compilation.** Task 2 must land as one commit (signature change + deletions + consumer updates) or the tree won't build. Task 3's test sweep must leave `go test ./internal/store/` green before committing.
- **Blast radius beyond the store.** After Task 3, packages outside `internal/store` that drive mutations in their own tests (`internal/cli`, `internal/tui`) may also need conforming actors. Run `go test ./...` after Task 3 and fix stragglers in the package where they fail — prefer the `admin@cli:test` / `developer@claude:test` conforming literals, or seed the needed persona.
- **`WithLock` is not nested by `ensureBuiltinPersonas`.** `validateActor` runs before each mutation's `WithLock(code)`, and `ensureBuiltinPersonas` acquires/releases `WithLock("personas")` sequentially — no re-entrant locking.
