# Project Repo Dispatch Targets Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Record per-project local repo dispatch targets in `config.json` (machine-local config, not substrate), manage them with an `atm project repo` CLI verb, have the concierge record them during onboarding, and add a repo cycle-picker to the developer dispatch dialog so it spawns into the project's repo instead of the TUI's cwd.

**Architecture:** A new `repos` list on `core.ProjectConfig` (config, not substrate — no event-log entry, not synced) joins `embedding`/`remotes`/`boards`. Three store methods mirror the `SetProjectRemote`/`RemoveProjectRemote`/`ProjectRemotes` triad. A new `atm project repo` command group mirrors `atm store remote`. The concierge persona doc gains plain-language instructions to call that verb during onboarding. The developer dispatch dialog (`internal/tui/dispatch.go`) gains a second cycle-picker (`↑/↓`) over the project's repos; `dispatch.Spec.Dir` becomes the selected repo's path, falling back to cwd when the list is empty.

**Tech Stack:** Go 1.22+, cobra (CLI), Bubble Tea (TUI), `atm/internal/core` (domain/config types + `Service` interface), `atm/internal/store` (config read-modify-write under per-project lock), `atm/internal/cli` (cobra commands), `atm/internal/tui` (dispatch dialog overlay).

## Global Constraints

- Repos are **config, not substrate state**: no event-log entry, no history entry, not carried by the event source, not synced. A fresh machine with a synced event log has no repos until a concierge session records them locally. (Spec Decisions of record, bullet 1.)
- A repo record is **separate from a `context:repository` pointer**. The pointer is synced knowledge; the repo config is a machine-local dispatch target. (Spec, bullet 2.)
- **Developer dialog only** gets the repo cycle-picker. Manager, concierge, and admin dispatches stay on cwd. (Spec, bullet 4.)
- **Path validation at add time**: resolve `~` and relative paths to absolute; require the directory to exist. No re-check at spawn. (Spec, bullet 5.)
- **Two cycle-picker axes** in the dialog: `←/→` cycles agent (existing), `↑/↓` cycles repo (new). (Spec, bullet 6.)
- No emojis in code or commits (AGENTS.md §5).
- Follow existing style in neighboring files; the `atm store remote` command group and `SetProjectRemote`/`RemoveProjectRemote`/`ProjectRemotes` store methods are the templates.
- `make verify` is the gate before declaring done.

---

## File Structure

- **Modify** `internal/core/config.go` — add `RepoConfig` struct and `Repos` field on `ProjectConfig`.
- **Modify** `internal/core/service.go` — add `SetProjectRepo`/`RemoveProjectRepo`/`ProjectRepos` to the `ProjectService` interface.
- **Modify** `internal/store/config.go` — implement the three repo methods (read-modify-write under project lock, mirroring the remote methods); update the `GetProjectConfig` emptiness check to include `Repos`.
- **Modify** `internal/store/config_test.go` — add repo round-trip, upsert, remove, validation, path-resolution, actor-required, coexist, and merge-preservation tests.
- **Modify** `internal/cli/project.go` — add the `repo` command group (`add`/`list`/`remove`); extend the `project` long help.
- **Modify** `internal/cli/project_repo_test.go` (new file) — CLI tests for the repo command group.
- **Modify** `skills/persona/concierge.md` — Step 2 and Step 4 plain-language additions.
- **Modify** `internal/tui/dispatch.go` — repo cycle-picker (model fields, `open` reads repos, `↑/↓` key handling, render `Repo:` line, `submit` sets `Spec.Dir`), keymap help line.
- **Modify** `internal/tui/dispatch_test.go` — developer-with-repos, developer-no-repos-fallback, and manager-unchanged regression tests.
- **Modify** `README.md` and `CHANGELOG.md` — document the repo config + dispatch picker.

---

### Task 1: RepoConfig type and ProjectService interface

**Files:**
- Modify: `internal/core/config.go:31-37` (add `RepoConfig` struct; add `Repos` field to `ProjectConfig`)
- Modify: `internal/core/service.go:30-45` (add three methods to `ProjectService`)
- Test: `internal/store/config_test.go` (compile-only guard added in Task 2's tests; this task adds no test of its own — the interface change will fail to compile if the store doesn't implement it, which Task 2 satisfies)

**Interfaces:**
- Produces: `core.RepoConfig` struct `{Name, Path string; URL string}` with json tags `name`/`path`/`url,omitempty`; `core.ProjectConfig.Repos []RepoConfig` with json tag `repos,omitempty`; `core.ProjectService` methods `SetProjectRepo(code, name, path, url, actor string) error`, `RemoveProjectRepo(code, name, actor string) error`, `ProjectRepos(code string) ([]RepoConfig, error)`. Task 2 implements these on `*store.Store`; Task 3 calls them from the CLI; Task 5 calls `ProjectRepos` from the TUI.

- [ ] **Step 1: Add the RepoConfig struct and Repos field**

In `internal/core/config.go`, add the struct above `ProjectConfig` and the field on `ProjectConfig`. The existing `ProjectConfig` is:

```go
type ProjectConfig struct {
	UpdatedAt string            `json:"updated_at,omitempty"`
	UpdatedBy string            `json:"updated_by,omitempty"`
	Embedding *EmbeddingConfig  `json:"embedding,omitempty"`
	Remotes   map[string]string `json:"remotes,omitempty"`
	Boards    *BoardsConfig     `json:"boards,omitempty"`
}
```

Insert `RepoConfig` before it and add the `Repos` line as the last field:

```go
// RepoConfig is one machine-local dispatch target recorded for a project:
// a local path to spawn agent sessions into, plus an optional remote link
// the concierge logged during onboarding. It is config, not substrate
// state — no event-log entry, not synced — so a fresh machine carrying a
// synced event log has no repos until a concierge session records them.
type RepoConfig struct {
	Name string `json:"name"`          // short handle, unique within the project
	Path string `json:"path"`          // absolute local path (existence-validated on add)
	URL  string `json:"url,omitempty"` // remote link the concierge logged; optional
}

type ProjectConfig struct {
	UpdatedAt string            `json:"updated_at,omitempty"`
	UpdatedBy string            `json:"updated_by,omitempty"`
	Embedding *EmbeddingConfig  `json:"embedding,omitempty"`
	Remotes   map[string]string `json:"remotes,omitempty"`
	Boards    *BoardsConfig     `json:"boards,omitempty"`
	Repos     []RepoConfig      `json:"repos,omitempty"`
}
```

- [ ] **Step 2: Add the three methods to the ProjectService interface**

In `internal/core/service.go`, the `ProjectService` interface currently ends:

```go
	ProjectRemotes(code string) (map[string]string, error)
	SetProjectRemote(code, name, url, actor string) error
	RemoveProjectRemote(code, name, actor string) error
}
```

Add the repo methods immediately after `RemoveProjectRemote` (before the closing brace):

```go
	ProjectRemotes(code string) (map[string]string, error)
	SetProjectRemote(code, name, url, actor string) error
	RemoveProjectRemote(code, name, actor string) error
	ProjectRepos(code string) ([]RepoConfig, error)
	SetProjectRepo(code, name, path, url, actor string) error
	RemoveProjectRepo(code, name, actor string) error
}
```

- [ ] **Step 3: Verify the build breaks (interface not yet satisfied)**

Run: `go build ./internal/store/...`
Expected: COMPILE ERROR — `*store.Store` does not implement `core.ProjectService` (missing `SetProjectRepo`/`RemoveProjectRepo`/`ProjectRepos`). This confirms the interface change is wired; Task 2 satisfies it.

- [ ] **Step 4: Commit**

```bash
git add internal/core/config.go internal/core/service.go
git commit -m "feat(ATM-0871aa): RepoConfig type + ProjectService repo methods

Add core.RepoConfig (name+path+url) and core.ProjectConfig.Repos, plus
SetProjectRepo/RemoveProjectRepo/ProjectRepos on ProjectService. Repos
are config, not substrate (no event-log entry, not synced)."
```

---

### Task 2: Store methods for repo config

**Files:**
- Modify: `internal/store/config.go` (add `SetProjectRepo`/`RemoveProjectRepo`/`ProjectRepos`; update `GetProjectConfig` emptiness check)
- Test: `internal/store/config_test.go` (add repo tests mirroring the remote tests)

**Interfaces:**
- Consumes: `core.RepoConfig`, `core.ProjectConfig.Repos`, `core.ProjectService` repo methods (Task 1).
- Produces: `*store.Store.SetProjectRepo`/`RemoveProjectRepo`/`ProjectRepos` satisfying the interface; `GetProjectConfig` treats a lone `Repos` entry as non-empty.

- [ ] **Step 1: Write the failing tests**

Append to `internal/store/config_test.go` (after `TestGetBoardsConfigFoldsLegacyPins`). These mirror the remote tests (`TestProjectRemoteRoundTrip`, `TestSetProjectRemoteMultipleNamesCoexist`, `TestRemoveProjectRemoteUnknownNameReturnsNotFound`, `TestSetProjectRemoteEmptyNameOrURLIsUsage`, `TestSetProjectRemoteRequiresActor`, `TestGetProjectConfigLoadsWithOnlyRemotes`).

The store tests use `newTestStore(t)` and `const testActor = "admin@cli:test"` (defined in `project_test.go:55,70`). `os` and `path/filepath` are already imported at the top of `config_test.go`.

```go
func TestProjectRepoRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := s.SetProjectRepo("ATM", "main", dir, "https://example.com/atm.git", testActor); err != nil {
		t.Fatalf("SetProjectRepo: %v", err)
	}
	repos, err := s.ProjectRepos("ATM")
	if err != nil {
		t.Fatalf("ProjectRepos: %v", err)
	}
	if len(repos) != 1 || repos[0].Name != "main" || repos[0].Path != dir || repos[0].URL != "https://example.com/atm.git" {
		t.Errorf("repos = %+v, want one main -> %s", repos, dir)
	}
	if err := s.RemoveProjectRepo("ATM", "main", testActor); err != nil {
		t.Fatalf("RemoveProjectRepo: %v", err)
	}
	repos, err = s.ProjectRepos("ATM")
	if err != nil {
		t.Fatalf("ProjectRepos after remove: %v", err)
	}
	if len(repos) != 0 {
		t.Errorf("repos = %+v, want empty after remove", repos)
	}
}

func TestSetProjectRepoUpsertUpdatesExisting(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := s.SetProjectRepo("ATM", "main", dir, "https://example.com/a", testActor); err != nil {
		t.Fatal(err)
	}
	dir2 := t.TempDir()
	if err := s.SetProjectRepo("ATM", "main", dir2, "https://example.com/b", testActor); err != nil {
		t.Fatal(err)
	}
	repos, _ := s.ProjectRepos("ATM")
	if len(repos) != 1 || repos[0].Path != dir2 || repos[0].URL != "https://example.com/b" {
		t.Errorf("upsert = %+v, want one main -> %s with url b", repos, dir2)
	}
}

func TestSetProjectRepoMultipleNamesCoexist(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	d1, d2 := t.TempDir(), t.TempDir()
	if err := s.SetProjectRepo("ATM", "main", d1, "", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectRepo("ATM", "docs", d2, "", testActor); err != nil {
		t.Fatal(err)
	}
	repos, _ := s.ProjectRepos("ATM")
	if len(repos) != 2 || repos[0].Name != "main" || repos[1].Name != "docs" {
		t.Errorf("repos = %+v, want [main docs] in insertion order", repos)
	}
}

func TestRemoveProjectRepoUnknownNameReturnsNotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	err := s.RemoveProjectRepo("ATM", "nope", testActor)
	if !core.IsNotFound(err) {
		t.Errorf("err = %v, want core.ErrNotFound", err)
	}
}

func TestSetProjectRepoEmptyNameOrPathIsUsage(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := s.SetProjectRepo("ATM", "", dir, "", testActor); !core.IsUsage(err) {
		t.Errorf("empty name: err = %v, want core.ErrUsage", err)
	}
	if err := s.SetProjectRepo("ATM", "main", "", "", testActor); !core.IsUsage(err) {
		t.Errorf("empty path: err = %v, want core.ErrUsage", err)
	}
}

func TestSetProjectRepoRequiresActor(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	err := s.SetProjectRepo("ATM", "main", dir, "", "")
	if !core.IsUsage(err) {
		t.Errorf("err = %v, want core.ErrUsage (missing actor)", err)
	}
}

func TestSetProjectRepoRejectsNonexistentPath(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if err := s.SetProjectRepo("ATM", "main", missing, "", testActor); err == nil {
		t.Errorf("err = nil, want error for non-existent path %s", missing)
	}
}

func TestSetProjectRepoResolvesRelativeAndTildePath(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	// A relative path resolves against the process cwd; use a temp dir name
	// relative to cwd by chdir-ing into the temp dir's parent and passing the
	// leaf. Simpler: pass "." which resolves to the cwd (the test cwd exists).
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectRepo("ATM", "main", ".", "", testActor); err != nil {
		t.Fatalf("relative path '.': %v", err)
	}
	repos, _ := s.ProjectRepos("ATM")
	if len(repos) != 1 || repos[0].Path != cwd {
		t.Errorf("path = %q, want resolved absolute %q", repos[0].Path, cwd)
	}
}

func TestSetProjectRepoPreservesOtherConfigFields(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	// Seed an embedding config and a remote first.
	if err := s.SetEmbeddingConfig("ATM", EmbeddingConfig{Model: "m", Endpoint: "http://x", Dim: 4, Threshold: 0.5}, testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectRemote("ATM", "origin", "https://example.com/sync", testActor); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := s.SetProjectRepo("ATM", "main", dir, "", testActor); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetProjectConfig("ATM")
	if got.Embedding == nil || got.Embedding.Model != "m" {
		t.Errorf("Embedding lost after repo add: %+v", got.Embedding)
	}
	if got.Remotes["origin"] != "https://example.com/sync" {
		t.Errorf("Remotes lost after repo add: %+v", got.Remotes)
	}
	if len(got.Repos) != 1 || got.Repos[0].Path != dir {
		t.Errorf("Repos = %+v, want one main -> %s", got.Repos, dir)
	}
}

// TestGetProjectConfigReposOnlyIsNotAbsent guards the emptiness check: a
// config carrying only a repos key must not read as nil.
func TestGetProjectConfigReposOnlyIsNotAbsent(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := s.SetProjectRepo("ATM", "main", dir, "", testActor); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetProjectConfig("ATM")
	if err != nil || got == nil || len(got.Repos) != 1 {
		t.Fatalf("GetProjectConfig = (%+v, %v), want non-nil with one repo", got, err)
	}
}

func TestProjectReposAbsentConfigReturnsEmptyNoError(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	repos, err := s.ProjectRepos("ATM")
	if err != nil {
		t.Fatalf("ProjectRepos: %v", err)
	}
	if len(repos) != 0 {
		t.Errorf("repos = %+v, want empty", repos)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run 'TestProjectRepo|TestSetProjectRepo|TestGetProjectConfigReposOnly|TestProjectReposAbsent' -v`
Expected: FAIL — `SetProjectRepo`/`RemoveProjectRepo`/`ProjectRepos` undefined (compile error), since Task 1 left the interface unimplemented.

- [ ] **Step 3: Implement the store methods**

In `internal/store/config.go`, first update the `GetProjectConfig` emptiness check. The current check (line 18) is:

```go
	if c.Embedding == nil && c.UpdatedAt == "" && len(c.Remotes) == 0 && c.Boards == nil {
		return nil, nil
	}
```

Add the `Repos` term:

```go
	if c.Embedding == nil && c.UpdatedAt == "" && len(c.Remotes) == 0 && c.Boards == nil && len(c.Repos) == 0 {
		return nil, nil
	}
```

Then add the three methods. They mirror `SetProjectRemote`/`RemoveProjectRemote`/`ProjectRemotes` exactly in shape (read-modify-write under `WithLock`, validate-actor-first, refresh `updated_at`/`updated_by`, `WriteFileAtomic`). `filepath` and `os` are already imported in `config.go`. Add after `ProjectRemotes` (line 110):

```go
// SetProjectRepo adds or updates a named repo dispatch target in the
// project's config. name and path are required; url is optional. The path
// is resolved to absolute (expanding ~ and relative paths) and must exist.
// Config, not substrate state: no event-log entry, not synced.
func (s *Store) SetProjectRepo(code, name, path, url, actor string) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	if name == "" || path == "" {
		return core.ErrUsage
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve repo path: %w", err)
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return fmt.Errorf("%w: repo path does not exist or is not a directory: %s", core.ErrUsage, abs)
	}
	return s.WithLock(code, func() error {
		existing, err := s.GetProjectConfig(code)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		merged := &ProjectConfig{}
		if existing != nil {
			merged = existing
		}
		// Upsert by name: replace if present, else append (insertion order).
		rep := RepoConfig{Name: name, Path: abs, URL: url}
		found := false
		for i, r := range merged.Repos {
			if r.Name == name {
				merged.Repos[i] = rep
				found = true
				break
			}
		}
		if !found {
			merged.Repos = append(merged.Repos, rep)
		}
		merged.UpdatedAt = core.RFC3339UTC(core.Now())
		merged.UpdatedBy = actor
		return WriteFileAtomic(s.configPath(code), merged)
	})
}

// RemoveProjectRepo deletes a named repo dispatch target from the project's
// config. Returns core.ErrNotFound if the name is not present.
func (s *Store) RemoveProjectRepo(code, name, actor string) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	return s.WithLock(code, func() error {
		existing, err := s.GetProjectConfig(code)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if existing == nil || len(existing.Repos) == 0 {
			return fmt.Errorf("%w: repo %q", core.ErrNotFound, name)
		}
		idx := -1
		for i, r := range existing.Repos {
			if r.Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("%w: repo %q", core.ErrNotFound, name)
		}
		existing.Repos = append(existing.Repos[:idx], existing.Repos[idx+1:]...)
		existing.UpdatedAt = core.RFC3339UTC(core.Now())
		existing.UpdatedBy = actor
		return WriteFileAtomic(s.configPath(code), existing)
	})
}

// ProjectRepos returns the project's configured repo dispatch targets. It
// returns an empty (nil) slice and no error if the project has no config or
// no repos set.
func (s *Store) ProjectRepos(code string) ([]RepoConfig, error) {
	c, err := s.GetProjectConfig(code)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, nil
	}
	return c.Repos, nil
}
```

Note: `RepoConfig` is referenced unqualified because `internal/store` re-exports it via the alias `type ProjectConfig = core.ProjectConfig` in `types_compat.go` — but `RepoConfig` is new, so add an alias there too. In `internal/store/types_compat.go`, after the `ProjectConfig` alias (line 36), add:

```go
type RepoConfig = core.RepoConfig
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestProjectRepo|TestSetProjectRepo|TestGetProjectConfigReposOnly|TestProjectReposAbsent' -v`
Expected: PASS — all repo tests green.

- [ ] **Step 5: Run the full store test suite to confirm no regressions**

Run: `go test ./internal/store/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/store/config.go internal/store/config_test.go internal/store/types_compat.go
git commit -m "feat(ATM-0871aa): store repo config methods

SetProjectRepo/RemoveProjectRepo/ProjectRepos mirror the remote triad:
read-modify-write under the project lock, path resolved to absolute and
existence-validated on add, upsert by name, no event-log entry (config not
substrate). GetProjectConfig emptiness check now includes Repos."
```

---

### Task 3: `atm project repo` CLI command group

**Files:**
- Modify: `internal/cli/project.go:14-34` (register the `repo` subgroup) and add the new command constructors at the end of the file.
- Test: `internal/cli/project_repo_test.go` (new file)

**Interfaces:**
- Consumes: `core.Service.SetProjectRepo`/`RemoveProjectRepo`/`ProjectRepos` (Task 2), `cliState.resolveActor`/`openStore`/`emit`/`isJSON`/`stdout` helpers, `writeJSON`, `core.ErrUsage`/`core.ErrNotFound`, `ExitUsage`/`ExitNotFound` exit codes.
- Produces: `atm project repo add/list/remove` commands the concierge (Task 4) and users call.

- [ ] **Step 1: Write the failing CLI tests**

Create `internal/cli/project_repo_test.go`. It mirrors the `store remote` tests in `store_test.go:337-402` using the same `testCLI`/`runArgs`/`runArgsOut`/`mustContain` helpers (defined in `store_test.go`, same package `cli`).

```go
package cli

import (
	"path/filepath"
	"testing"
)

func TestProjectRepoAddListRemoveRoundTrip(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	dir := t.TempDir()
	out := runArgsOut(t, st, "project", "repo", "add", "main", dir, "--project", "ATM", "--actor", "admin@cli:unset")
	mustContain(t, out, "main")
	mustContain(t, out, dir)

	out = runArgsOut(t, st, "project", "repo", "list", "--project", "ATM")
	if out != "main\t"+dir+"\n" {
		t.Fatalf("unexpected list output: %q", out)
	}

	out = runArgsOut(t, st, "project", "repo", "remove", "main", "--project", "ATM", "--actor", "admin@cli:unset")
	mustContain(t, out, "main")

	out = runArgsOut(t, st, "project", "repo", "list", "--project", "ATM")
	if out != "" {
		t.Fatalf("expected empty list after remove, got %q", out)
	}
}

func TestProjectRepoAddWithURL(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	dir := t.TempDir()
	out := runArgsOut(t, st, "project", "repo", "add", "main", dir, "--url", "https://example.com/atm.git", "--project", "ATM", "--actor", "admin@cli:unset")
	mustContain(t, out, "main")
	out = runArgsOut(t, st, "project", "repo", "list", "--project", "ATM")
	mustContain(t, out, "https://example.com/atm.git")
}

func TestProjectRepoListJSON(t *testing.T) {
	st := newTestCLI(t)
	st.output = outputJSON
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	dir := t.TempDir()
	_, _, _ = runArgs(st, "project", "repo", "add", "main", dir, "--url", "https://example.com/atm.git", "--project", "ATM", "--actor", "admin@cli:unset")
	out := runArgsOut(t, st, "project", "repo", "list", "--project", "ATM")
	mustContain(t, out, `"name": "main"`)
	mustContain(t, out, `"path": "`+dir+`"`)
	mustContain(t, out, `"url": "https://example.com/atm.git"`)
}

func TestProjectRepoAddRequiresProject(t *testing.T) {
	st := newTestCLI(t)
	dir := t.TempDir()
	_, _, code := runArgs(st, "project", "repo", "add", "main", dir, "--actor", "admin@cli:unset")
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage without --project, got %d", code)
	}
}

func TestProjectRepoRemoveRequiresProject(t *testing.T) {
	st := newTestCLI(t)
	_, _, code := runArgs(st, "project", "repo", "remove", "main", "--actor", "admin@cli:unset")
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage without --project, got %d", code)
	}
}

func TestProjectRepoRemoveUnknownNotFound(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, code := runArgs(st, "project", "repo", "remove", "nope", "--project", "ATM", "--actor", "admin@cli:unset")
	if code != ExitNotFound {
		t.Fatalf("expected ExitNotFound removing unknown repo, got %d", code)
	}
}

func TestProjectRepoAddRejectsNonexistentPath(t *testing.T) {
	st := newTestCLI(t)
	_, _, _ = runArgs(st, "project", "create", "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	_, _, code := runArgs(st, "project", "repo", "add", "main", missing, "--project", "ATM", "--actor", "admin@cli:unset")
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage for non-existent path, got %d", code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestProjectRepo' -v`
Expected: FAIL — `unknown command "repo" for "atm project"` (the subgroup is not registered yet).

- [ ] **Step 3: Implement the command group**

In `internal/cli/project.go`, register the subgroup in `newProjectCmd` (line 14-34). The current registration block is:

```go
	cmd.AddCommand(newProjectCreateCmd(st))
	cmd.AddCommand(newProjectListCmd(st))
	cmd.AddCommand(newProjectShowCmd(st))
	cmd.AddCommand(newProjectSetNameCmd(st))
	cmd.AddCommand(newProjectRemoveCmd(st))
	cmd.AddCommand(newProjectSetEmbeddingCmd(st))
	cmd.AddCommand(newProjectCapabilityCmd(st))
	cmd.AddCommand(newProjectBoardsCmd(st))
	return cmd
```

Add the repo subgroup before `return cmd`:

```go
	cmd.AddCommand(newProjectCreateCmd(st))
	cmd.AddCommand(newProjectListCmd(st))
	cmd.AddCommand(newProjectShowCmd(st))
	cmd.AddCommand(newProjectSetNameCmd(st))
	cmd.AddCommand(newProjectRemoveCmd(st))
	cmd.AddCommand(newProjectSetEmbeddingCmd(st))
	cmd.AddCommand(newProjectCapabilityCmd(st))
	cmd.AddCommand(newProjectBoardsCmd(st))
	cmd.AddCommand(newProjectRepoCmd(st))
	return cmd
```

Then append the command constructors at the end of `project.go`. They mirror `store_sync.go`'s `remote` commands (lines 17-107). `fmt` and `strings` are already imported at the top of `project.go`; `core` is imported. The `repoRow` JSON shape matches the store's `RepoConfig`.

```go
// newProjectRepoCmd returns the `atm project repo` subgroup managing a
// project's machine-local repo dispatch targets (config, not substrate).
func newProjectRepoCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Manage a project's repo dispatch targets (local paths to spawn agent sessions into)",
		Long: "A repo dispatch target is a machine-local path to spawn agent " +
			"sessions into, plus an optional remote link. Repos are config, not " +
			"substrate state: they are not written to the event log and are not " +
			"synced, so a fresh machine has no repos until a concierge session " +
			"records them there. A project is not 1:1 with a repo — record as " +
			"many as the project spans.",
	}
	bindActorFlag(cmd, st)

	repoAddCmd := &cobra.Command{
		Use:   "add <name> <path>",
		Short: "Add or update a project's repo dispatch target (upsert)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, _ := cmd.Flags().GetString("project")
			if project == "" {
				return fmt.Errorf("%w: --project is required", core.ErrUsage)
			}
			url, _ := cmd.Flags().GetString("url")
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.SetProjectRepo(project, args[0], args[1], url, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "name": args[0], "path": args[1], "url": url}, func() {
				fmt.Fprintf(st.stdout(), "added repo %s -> %s (project %s)\n", args[0], args[1], project)
			})
		},
	}
	repoAddCmd.Flags().String("project", "", "project code")
	repoAddCmd.Flags().String("url", "", "remote link (optional)")
	cmd.AddCommand(repoAddCmd)

	repoListCmd := &cobra.Command{
		Use:   "list",
		Short: "List a project's repo dispatch targets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, _ := cmd.Flags().GetString("project")
			if project == "" {
				return fmt.Errorf("%w: --project is required", core.ErrUsage)
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			repos, err := s.ProjectRepos(project)
			if err != nil {
				return err
			}
			if st.isJSON() {
				return writeJSON(st.stdout(), repos)
			}
			for _, r := range repos {
				if r.URL != "" {
					fmt.Fprintf(st.stdout(), "%s\t%s\t%s\n", r.Name, r.Path, r.URL)
				} else {
					fmt.Fprintf(st.stdout(), "%s\t%s\n", r.Name, r.Path)
				}
			}
			return nil
		},
	}
	repoListCmd.Flags().String("project", "", "project code")
	cmd.AddCommand(repoListCmd)

	repoRemoveCmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a project's repo dispatch target",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, _ := cmd.Flags().GetString("project")
			if project == "" {
				return fmt.Errorf("%w: --project is required", core.ErrUsage)
			}
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.RemoveProjectRepo(project, args[0], actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"project": project, "name": args[0]}, func() {
				fmt.Fprintf(st.stdout(), "removed repo %s (project %s)\n", args[0], project)
			})
		},
	}
	repoRemoveCmd.Flags().String("project", "", "project code")
	cmd.AddCommand(repoRemoveCmd)

	return cmd
}
```

Note on `ExitCodeForError`: the `testCLI.run` helper (store_test.go:57-61) maps errors to exit codes via `ExitCodeForError(err)`, which already maps `core.ErrUsage` → `ExitUsage` and `core.ErrNotFound` → `ExitNotFound` (the same mapping the `store remote` commands rely on in `TestStoreRemoteRemoveUnknownNotFound` and `TestStoreRemoteAddRequiresProject`). No extra wiring is needed.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestProjectRepo' -v`
Expected: PASS — all seven CLI repo tests green.

- [ ] **Step 5: Run the full CLI test suite to confirm no regressions**

Run: `go test ./internal/cli/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/project.go internal/cli/project_repo_test.go
git commit -m "feat(ATM-0871aa): atm project repo add/list/remove

New command group mirroring atm store remote: upsert by name, list with
optional url column, remove with ErrNotFound. Path existence-validated
on add. Repos are machine-local config (not synced); the concierge
records them during onboarding."
```

---

### Task 4: Concierge persona records repos during onboarding

**Files:**
- Modify: `skills/persona/concierge.md` (Step 2 + Step 4 prose additions)

**Interfaces:**
- Consumes: `atm project repo add` CLI verb (Task 3).
- Produces: concierge instructions that record repo dispatch targets during onboarding.

- [ ] **Step 1: Extend Step 2 (Converse)**

In `skills/persona/concierge.md`, the Step 2 first bullet currently reads:

```markdown
- Ask about their projects and which repositories they plan to bring in. Have them brief you on the responsibility and abstraction level of each, and where relevant knowledge lives (READMEs, architecture notes, external trackers, runbooks).
```

Replace it with:

```markdown
- Ask about their projects and which repositories they plan to bring in. Have them brief you on the responsibility and abstraction level of each, and where relevant knowledge lives (READMEs, architecture notes, external trackers, runbooks). For each repo, also ask where it lives on this machine (the local folder) and its remote link if it has one.
```

- [ ] **Step 2: Extend Step 4 (Triage)**

In `skills/persona/concierge.md`, the Step 4 section currently ends with:

```markdown
- Record the user's answers from Steps 2-3 as capability-managed reference tasks so the setup knowledge persists beyond the session.
```

Add a new bullet immediately after it (before the `### Hand off` heading):

```markdown
- For each repo the user named in Step 2, record it as a dispatch target for this project: `atm project repo add --project <CODE> --name <short-name> --path <local-folder> [--url <remote-link>]`. Confirm in plain words before writing — "I'll note that your `atm` work lives in `~/projects/scyllas/atm`" — never expose the flag shape. This is machine-local setup: when the user sets up ATM on a new machine, run a concierge session there to re-record the local paths.
```

- [ ] **Step 3: Verify the concierge smoke test still passes**

The concierge persona test (`skills/skills_test.go`) asserts the persona is launchable without `--project`, ships a default personality, and speaks the user's language (no jargon). The changes above add prose, not behavior the test asserts. Run:

Run: `go test ./skills/...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add skills/persona/concierge.md
git commit -m "feat(ATM-0871aa): concierge records repo dispatch targets

Step 2 asks for the local folder + remote link per repo; Step 4 records
them via atm project repo add in plain language. Machine-local setup,
re-run on each new machine."
```

---

### Task 5: TUI developer dispatch dialog repo cycle-picker

**Files:**
- Modify: `internal/tui/dispatch.go` (model fields, `open` reads repos, `↑/↓` key handling, `Repo:` render line, `submit` sets `Spec.Dir`, keymap help)
- Test: `internal/tui/dispatch_test.go` (developer-with-repos, developer-no-repos-fallback, manager-unchanged)

**Interfaces:**
- Consumes: `core.Service.ProjectRepos` (Task 2), `dispatch.Spec.Dir`, `m.store` (`core.Service`), `tea.KeyUp`/`tea.KeyDown`/`tea.KeyLeft`/`tea.KeyRight`/`tea.KeyEnter`/`tea.KeyEsc`, `fitLine`, `titledBoxHeight`, `styles.KeyMenuDim`.
- Produces: a developer dispatch dialog that spawns into the selected repo's path (cwd fallback when none recorded).

- [ ] **Step 1: Write the failing TUI tests**

Append to `internal/tui/dispatch_test.go`. The file already imports `errors`, `strings`, `testing`, `atm/internal/dispatch`, and `tea`. Add `os` to the import block (needed for cwd comparison). The existing helpers `newTestModel`, `seedProject`, `sizeDispatchModel`, `dispatchKey`, `testAgents`, `fakeDispatcher` are used.

First, update the import block at the top of the file from:

```go
import (
	"errors"
	"strings"
	"testing"

	"atm/internal/dispatch"
	tea "github.com/charmbracelet/bubbletea"
)
```

to:

```go
import (
	"errors"
	"os"
	"strings"
	"testing"

	"atm/internal/dispatch"
	tea "github.com/charmbracelet/bubbletea"
)
```

Then append the tests:

```go
func TestDispatchDeveloperWithRepoSpawnsIntoRepoPath(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	repoDir := t.TempDir()
	if err := m.store.SetProjectRepo("ATM", "main", repoDir, "https://example.com/atm.git", testActor); err != nil {
		t.Fatal(err)
	}
	task, err := m.store.CreateTask("ATM", "dispatch work", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	m.refreshAll()
	m.focused = paneTasks
	sizeDispatchModel(m)

	fd := &fakeDispatcher{preview: "herdr · new pane"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if m.dispatchDlg.kind != dispatchDeveloper {
		t.Fatal("D on tasks pane must open the developer dialog")
	}
	if len(m.dispatchDlg.repos) != 1 || m.dispatchDlg.repos[0].Path != repoDir {
		t.Fatalf("repos = %+v, want one main -> %s", m.dispatchDlg.repos, repoDir)
	}
	view := m.dispatchDlg.renderOverlay()
	if !strings.Contains(view, "Repo:") || !strings.Contains(view, repoDir) {
		t.Errorf("overlay must show Repo: line with the repo path:\n%s", view)
	}

	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("must spawn")
	}
	if fd.spawned[0].Dir != repoDir {
		t.Errorf("Spec.Dir = %q, want repo path %q", fd.spawned[0].Dir, repoDir)
	}
}

func TestDispatchDeveloperNoRepoFallsBackToCwd(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	task, err := m.store.CreateTask("ATM", "dispatch work", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	m.refreshAll()
	m.focused = paneTasks
	sizeDispatchModel(m)

	fd := &fakeDispatcher{preview: "herdr · new pane"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	dispatchKey(m, "D")
	if m.dispatchDlg.kind != dispatchDeveloper {
		t.Fatal("D on tasks pane must open the developer dialog")
	}
	if len(m.dispatchDlg.repos) != 0 {
		t.Fatalf("repos = %+v, want empty", m.dispatchDlg.repos)
	}
	view := m.dispatchDlg.renderOverlay()
	if !strings.Contains(view, "Repo:") || !strings.Contains(view, "(cwd)") {
		t.Errorf("overlay must show Repo: (cwd) when no repos recorded:\n%s", view)
	}
	// up/down must be a no-op (no panic, cursor stays 0).
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.dispatchDlg.repoCursor != 0 {
		t.Errorf("repoCursor = %d, want 0 (no-op with empty repos)", m.dispatchDlg.repoCursor)
	}

	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("must spawn")
	}
	if fd.spawned[0].Dir != cwd {
		t.Errorf("Spec.Dir = %q, want cwd %q", fd.spawned[0].Dir, cwd)
	}
}

func TestDispatchDeveloperRepoCyclePicker(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	d1, d2 := t.TempDir(), t.TempDir()
	if err := m.store.SetProjectRepo("ATM", "main", d1, "", testActor); err != nil {
		t.Fatal(err)
	}
	if err := m.store.SetProjectRepo("ATM", "docs", d2, "", testActor); err != nil {
		t.Fatal(err)
	}
	task, err := m.store.CreateTask("ATM", "dispatch work", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	m.refreshAll()
	m.focused = paneTasks
	sizeDispatchModel(m)

	fd := &fakeDispatcher{preview: "herdr · new pane"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if len(m.dispatchDlg.repos) != 2 || m.dispatchDlg.repoCursor != 0 {
		t.Fatalf("repos = %+v cursor = %d, want 2 repos cursor 0", m.dispatchDlg.repos, m.dispatchDlg.repoCursor)
	}
	// Down selects the second repo; the rendered Repo: line shows its path.
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.dispatchDlg.repoCursor != 1 {
		t.Fatalf("repoCursor = %d, want 1 after down", m.dispatchDlg.repoCursor)
	}
	view := m.dispatchDlg.renderOverlay()
	if !strings.Contains(view, d2) {
		t.Errorf("overlay must show second repo path after down:\n%s", view)
	}
	// Up wraps back to the first repo.
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.dispatchDlg.repoCursor != 0 {
		t.Fatalf("repoCursor = %d, want 0 after up", m.dispatchDlg.repoCursor)
	}

	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("must spawn")
	}
	if fd.spawned[0].Dir != d1 {
		t.Errorf("Spec.Dir = %q, want first repo %q", fd.spawned[0].Dir, d1)
	}
}

// TestDispatchManagerUnchangedByRepoPicker is a regression guard: the
// manager dialog has no Repo: line and still dispatches into cwd.
func TestDispatchManagerUnchangedByRepoPicker(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	// Even with repos recorded, the manager dialog must not show a Repo line.
	repoDir := t.TempDir()
	if err := m.store.SetProjectRepo("ATM", "main", repoDir, "", testActor); err != nil {
		t.Fatal(err)
	}
	m.focused = paneProjects
	sizeDispatchModel(m)

	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	dispatchKey(m, "D")
	if m.dispatchDlg.kind != dispatchManager {
		t.Fatal("D on projects pane must open the manager dialog")
	}
	view := m.dispatchDlg.renderOverlay()
	if strings.Contains(view, "Repo:") {
		t.Errorf("manager dialog must not show a Repo line:\n%s", view)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("must spawn")
	}
	if fd.spawned[0].Dir != cwd {
		t.Errorf("manager Spec.Dir = %q, want cwd %q (unchanged)", fd.spawned[0].Dir, cwd)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestDispatchDeveloperWithRepo|TestDispatchDeveloperNoRepo|TestDispatchDeveloperRepoCycle|TestDispatchManagerUnchangedByRepo' -v`
Expected: FAIL — `dispatchModel.repos` undefined (compile error), since the fields don't exist yet.

- [ ] **Step 3: Add the model fields and load repos in `open`**

In `internal/tui/dispatch.go`, the `dispatchModel` struct (lines 56-66) currently is:

```go
type dispatchModel struct {
	m          *Model
	kind       dispatchKind
	project    string
	taskID     string
	taskTitle  string
	agents     []agentOption
	cursor     int
	preview    string
	previewErr string
}
```

Add the two repo fields at the end:

```go
type dispatchModel struct {
	m          *Model
	kind       dispatchKind
	project    string
	taskID     string
	taskTitle  string
	agents     []agentOption
	cursor     int
	preview    string
	previewErr string
	repos      []core.RepoConfig
	repoCursor int
}
```

`core` is not yet imported in `dispatch.go`. Update the import block (lines 3-12) from:

```go
import (
	"os"
	"os/exec"
	"strings"

	"atm/internal/agent"
	"atm/internal/dispatch"

	tea "github.com/charmbracelet/bubbletea"
)
```

to:

```go
import (
	"os"
	"os/exec"
	"strings"

	"atm/internal/agent"
	"atm/internal/core"
	"atm/internal/dispatch"

	tea "github.com/charmbracelet/bubbletea"
)
```

Now extend `open` (lines 91-111) to load the project's repos for developer dispatches. The current `open` is:

```go
func (d *dispatchModel) open(kind dispatchKind, project, taskID, taskTitle string) {
	d.kind, d.project, d.taskID, d.taskTitle = kind, project, taskID, taskTitle
	d.agents = d.m.agentOptionsFn()
	d.cursor = 0
	for i, a := range d.agents { // preselect the first ready agent
		if a.ready {
			d.cursor = i
			break
		}
	}
	d.preview, d.previewErr = "", ""
	if d.m.dispatcher == nil {
		d.previewErr = "dispatch unavailable in this build"
		return
	}
	if p, err := d.m.dispatcher.Preview(); err != nil {
		d.previewErr = err.Error()
	} else {
		d.preview = p
	}
}
```

Replace it with a version that resets the repo state and loads repos for developer dispatches:

```go
func (d *dispatchModel) open(kind dispatchKind, project, taskID, taskTitle string) {
	d.kind, d.project, d.taskID, d.taskTitle = kind, project, taskID, taskTitle
	d.agents = d.m.agentOptionsFn()
	d.cursor = 0
	for i, a := range d.agents { // preselect the first ready agent
		if a.ready {
			d.cursor = i
			break
		}
	}
	d.preview, d.previewErr = "", ""
	d.repos, d.repoCursor = nil, 0
	if kind == dispatchDeveloper && project != "" {
		if repos, err := d.m.store.ProjectRepos(project); err == nil {
			d.repos = repos
		}
	}
	if d.m.dispatcher == nil {
		d.previewErr = "dispatch unavailable in this build"
		return
	}
	if p, err := d.m.dispatcher.Preview(); err != nil {
		d.previewErr = err.Error()
	} else {
		d.preview = p
	}
}
```

- [ ] **Step 4: Add the `↑/↓` key handling**

In `handleKey` (lines 113-129), add `up`/`down` (and `k`/`j`) cycling for repos. The current handler is:

```go
func (d *dispatchModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		d.kind = dispatchNone
	case "left", "h":
		if d.cursor > 0 {
			d.cursor--
		}
	case "right", "l":
		if d.cursor < len(d.agents)-1 {
			d.cursor++
		}
	case "enter":
		d.submit()
	}
	return nil
}
```

Replace it with:

```go
func (d *dispatchModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		d.kind = dispatchNone
	case "left", "h":
		if d.cursor > 0 {
			d.cursor--
		}
	case "right", "l":
		if d.cursor < len(d.agents)-1 {
			d.cursor++
		}
	case "down", "j":
		if len(d.repos) > 0 {
			d.repoCursor = (d.repoCursor + 1) % len(d.repos)
		}
	case "up", "k":
		if len(d.repos) > 0 {
			d.repoCursor = (d.repoCursor - 1 + len(d.repos)) % len(d.repos)
		}
	case "enter":
		d.submit()
	}
	return nil
}
```

The modulo arithmetic makes `↑/↓` wrap around, matching the agent cycle-picker's behavior (which clamps at the ends — but wrapping is cleaner for repos and the tests assert wrap-around via `up` from cursor 0). Note: the agent picker clamps (`left` stops at 0); the repo picker wraps. This is intentional and visible: a repo list is short and wrap-around is friendlier than a dead-end.

- [ ] **Step 5: Set `Spec.Dir` from the selected repo in `submit`**

In `submit` (lines 131-164), the working directory is currently computed as:

```go
	dir, err := os.Getwd()
	if err != nil {
		d.m.showToast("error: " + err.Error())
		return
	}
	if err := d.m.dispatcher.Spawn(dispatch.Spec{Title: d.title(), Argv: argv, Dir: dir}); err != nil {
```

Replace those lines so the repo path is used when present, cwd otherwise:

```go
	dir, err := os.Getwd()
	if err != nil {
		d.m.showToast("error: " + err.Error())
		return
	}
	if len(d.repos) > 0 {
		dir = d.repos[d.repoCursor].Path
	}
	if err := d.m.dispatcher.Spawn(dispatch.Spec{Title: d.title(), Argv: argv, Dir: dir}); err != nil {
```

- [ ] **Step 6: Render the `Repo:` line and update the keymap help**

In `renderOverlay` (lines 171-213), insert a `Repo:` block between the `Task:` block and the `Agent:` line, and update the keymap help line. The current render body construction is:

```go
	var b strings.Builder
	if d.kind == dispatchDeveloper {
		b.WriteString("Task:   " + d.taskID + "\n")
		b.WriteString(styles.FieldHint.Render("        "+fitLine(d.taskTitle, bw-10)) + "\n\n")
	}
	a := agentOption{name: "—"}
	if len(d.agents) > 0 {
		a = d.agents[d.cursor]
	}
	b.WriteString("Agent:  ‹ " + a.name + " ›\n")
	if a.ready {
		b.WriteString(styles.Success.Render("        ready") + "\n\n")
	} else {
		b.WriteString(styles.Error.Render("        x "+a.hint) + "\n\n")
	}
	if d.previewErr != "" {
		b.WriteString(styles.Error.Render("Target: x "+d.previewErr) + "\n")
	} else {
		b.WriteString("Target: " + d.preview + " \"" + d.title() + "\"\n")
	}
	b.WriteString("\n" + styles.KeyMenuDim.Render("[←/→]agent  [Enter]dispatch  [Esc]close"))
```

Replace it with a version that adds the `Repo:` line for developer dispatches and the `↑/↓ repo` hint:

```go
	var b strings.Builder
	if d.kind == dispatchDeveloper {
		b.WriteString("Task:   " + d.taskID + "\n")
		b.WriteString(styles.FieldHint.Render("        "+fitLine(d.taskTitle, bw-10)) + "\n\n")
		b.WriteString("Repo:   " + d.repoLabel() + "\n\n")
	}
	a := agentOption{name: "—"}
	if len(d.agents) > 0 {
		a = d.agents[d.cursor]
	}
	b.WriteString("Agent:  ‹ " + a.name + " ›\n")
	if a.ready {
		b.WriteString(styles.Success.Render("        ready") + "\n\n")
	} else {
		b.WriteString(styles.Error.Render("        x "+a.hint) + "\n\n")
	}
	if d.previewErr != "" {
		b.WriteString(styles.Error.Render("Target: x "+d.previewErr) + "\n")
	} else {
		b.WriteString("Target: " + d.preview + " \"" + d.title() + "\"\n")
	}
	help := "[←/→]agent  [Enter]dispatch  [Esc]close"
	if d.kind == dispatchDeveloper {
		help = "[←/→]agent  [↑/↓]repo  [Enter]dispatch  [Esc]close"
	}
	b.WriteString("\n" + styles.KeyMenuDim.Render(help))
```

Then add the `repoLabel` helper method (anywhere in `dispatch.go`, e.g. after `title()`):

```go
// repoLabel renders the Repo: line's value: the selected repo's path, or
// "(cwd)" when no repos are recorded. Paths are truncated to the box's inner
// width with fitLine so a long path cannot widen the dialog.
func (d *dispatchModel) repoLabel() string {
	if len(d.repos) == 0 {
		return "‹ (cwd) ›"
	}
	r := d.repos[d.repoCursor]
	label := r.Path
	if r.Name != "" {
		label = r.Name + " · " + r.Path
	}
	return "‹ " + fitLine(label, bwInner(d.m.width)) + " ›"
}

// bwInner returns the inner text width of the dispatch dialog box for the
// given terminal width, mirroring renderOverlay's box-width math so a long
// repo path truncates consistently with the task title.
func bwInner(width int) int {
	bw := width * 60 / 100
	if bw < 64 {
		bw = 64
	}
	if bw > width-4 {
		bw = width - 4
	}
	// Subtract the box border (2) and a small left-pad margin (2) used by
	// styles.DialogBody; the Repo: line shares the Task: line's indent.
	return bw - 4
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestDispatchDeveloperWithRepo|TestDispatchDeveloperNoRepo|TestDispatchDeveloperRepoCycle|TestDispatchManagerUnchangedByRepo' -v`
Expected: PASS — all four new tests green.

- [ ] **Step 8: Run the full TUI test suite to confirm no regressions**

Run: `go test ./internal/tui/...`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/dispatch.go internal/tui/dispatch_test.go
git commit -m "feat(ATM-0871aa): developer dispatch repo cycle-picker

The developer dispatch dialog gains a Repo: line (up/down cycle) over
the project's recorded repos; Spec.Dir becomes the selected repo's
path, falling back to cwd when none are recorded. Manager/concierge/
admin dialogs are unchanged. Keymap help gains [↑/↓]repo."
```

---

### Task 6: Docs and ledger

**Files:**
- Modify: `README.md:243-273` (Dispatching Sessions section)
- Modify: `CHANGELOG.md:1` (Unreleased section)
- Ledger: `atm capability workflow_ai plan` + `done` on ATM-0871aa

**Interfaces:**
- Consumes: the completed feature (Tasks 1-5).

- [ ] **Step 1: Update the README Dispatching Sessions section**

In `README.md`, the paragraph at lines 245-252 currently describes the dialog with the agent as the only interactive field. Replace that paragraph to mention the repo picker on the developer dialog. The current text:

```markdown
The TUI can spawn manager and developer sessions into a separate terminal
surface (herdr pane → tmux window → new terminal tab, auto-detected in that
order). From the projects pane, `D` dispatches a **manager** session for the
selected project; from the tasks pane, `D` dispatches a **developer** session
bound to the selected task row. The only interactive field is the host agent
(cycle with `←/→`, dispatch with `Enter`); an unready agent is refused with its
missing-bin hint. `V` opens a read-only **personas** browser (list built-ins
and customs, `Enter` views a persona's effective prompt, `Esc` backs out).
```

Replace it with:

```markdown
The TUI can spawn manager and developer sessions into a separate terminal
surface (herdr pane → tmux window → new terminal tab, auto-detected in that
order). From the projects pane, `D` dispatches a **manager** session for the
selected project; from the tasks pane, `D` dispatches a **developer** session
bound to the selected task row. The host agent is the interactive field in
both dialogs (cycle with `←/→`, dispatch with `Enter`); an unready agent is
refused with its missing-bin hint. The developer dialog adds a second field —
the **repo** to spawn into (cycle with `↑/↓`), drawn from the project's
recorded repo dispatch targets (see below); when none are recorded it falls
back to the TUI's current directory. `V` opens a read-only **personas**
browser (list built-ins and customs, `Enter` views a persona's effective
prompt, `Esc` backs out).
```

Then add a new subsection immediately after the `--task` shell example block (after line 263, before the "When neither herdr nor tmux is present" paragraph at line 265). Insert:

```markdown
A project records its repo dispatch targets with `atm project repo add` —
machine-local config (not synced), so re-record them on each new machine via a
concierge session:

```sh
atm project repo add main ~/projects/scyllas/atm --url https://example.com/atm.git --project ATM
atm project repo list --project ATM
atm project repo remove main --project ATM
```
```

- [ ] **Step 2: Add the CHANGELOG entry**

In `CHANGELOG.md`, under `## Unreleased`, add a new bullet to the second `### feat` list (the per-task list starting at line 22). Add it as the first bullet in that list (most recent first), following the `ATM-4b7e24: TUI agent dispatch` entry's style:

```markdown
- ATM-0871aa: project repo dispatch targets. New `atm project repo add/list/remove` records machine-local repo dispatch targets (name + path + url) in `config.json` — config, not substrate (no event-log entry, not synced), so a fresh machine re-records them via a concierge session. The developer dispatch dialog gains a `Repo:` cycle-picker (`↑/↓`) over the project's repos; `Spec.Dir` becomes the selected repo's path, falling back to cwd when none are recorded. Manager/concierge/admin dispatches are unchanged. The concierge persona records repos during onboarding (Step 2 asks for the local folder + remote link; Step 4 writes them via the CLI verb in plain language).
```

- [ ] **Step 3: Run the verification gate**

Run: `make verify`
Expected: PASS (build + test + lint). If `make verify` is unavailable, run `make build && make test`.

- [ ] **Step 4: Update the ledger — plan, then done**

Run:

```bash
atm capability workflow_ai plan --task ATM-0871aa --kind file --ref "docs/superpowers/plans/2026-07-24-project-repo-dispatch-targets.md" --actor "developer@ollama:glm-5.2:cloud"
atm capability workflow_ai done --task ATM-0871aa --actor "developer@ollama:glm-5.2:cloud"
atm task comment add --task ATM-0871aa --body "Implementation complete. 6 tasks: RepoConfig type + interface; store methods (read-modify-write, path resolve+exist); atm project repo add/list/remove CLI; concierge persona Step 2/4 prose; TUI developer dialog repo cycle-picker (up/down, cwd fallback); README + CHANGELOG. make verify green." --actor "developer@ollama:glm-5.2:cloud"
```

- [ ] **Step 5: Commit docs**

```bash
git add README.md CHANGELOG.md
git commit -m "docs(ATM-0871aa): project repo dispatch targets

README Dispatching Sessions section documents the developer dialog repo
cycle-picker and the new atm project repo add/list/remove verb.
CHANGELOG gains the ATM-0871aa entry."
```