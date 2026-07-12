# Init Plugin and Project Simplification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `atm init` the only user-facing plugin setup flow and let launchers auto-create missing projects.

**Architecture:** Keep plugin install/status implementation in `internal/developing` and `internal/manager`, but expose combined installation only through `atm init`. Add a text-mode TTY prompt for default setup and keep repeatable `--agent` flags for deterministic scripts. Add a small shared CLI helper that returns an existing project or creates it with a fallback name before rendering launcher context.

**Tech Stack:** Go 1.22+, Cobra CLI, existing `internal/store`, existing plugin asset installers.

## Global Constraints

- `atm init` remains idempotent.
- `atm init` in text-mode TTY prompts for agent selection when `--agent` is not supplied.
- `atm init` in JSON mode or non-TTY mode stays non-interactive unless `--agent` is supplied.
- Supported setup agents are exactly `opencode`, `codex`, `claude`, and `all`.
- Old `atm manage plugin ...` commands are hard-removed from the user-facing Cobra tree.
- `--project` stays required on launchers.
- Missing launcher projects are created with code `<CODE>` and name `<CODE>`.
- No database or store model changes.
- Tests must be written and observed failing before implementation.

---

### Task 1: Combined `atm init` Plugin Setup

**Files:**
- Modify: `internal/cli/root.go`
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: `developing.InstallPlugin(agent, home, dryRun)` and `manager.InstallPlugin(agent, home, dryRun)`.
- Produces: interactive `atm init` agent selection plus repeatable `atm init --agent <agent>` and `atm init --dry-run --agent <agent>`.

- [ ] **Step 1: Write failing CLI tests**

Add tests that set a temporary `HOME`, install with multiple agents, and assert both developing and manager assets exist. Add a dry-run `all` test that asserts no plugin asset is written.

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/cli -run 'TestInitInstalls|TestInitDryRun'`

Expected: FAIL because `--agent` and `--dry-run` are unknown flags.

- [ ] **Step 3: Implement minimal init setup**

Add init options, parse selected agents in stable order, call both plugin installers for each selected agent, and extend text/JSON output.

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/cli -run 'TestInitInstalls|TestInitDryRun'`

Expected: PASS.

### Task 2: Remove User-Facing Manager Plugin Commands

**Files:**
- Modify: `internal/cli/manager.go`
- Test: `internal/cli/manager_test.go`

**Interfaces:**
- Consumes: existing `newManageCmd`.
- Produces: `atm manage plugin ...` exits non-zero because the command is not registered.

- [ ] **Step 1: Write failing removal test**

Add a test asserting `h.run("manage", "plugin", "status")` does not return success.

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/cli -run TestManagePluginCommandRemoved`

Expected: FAIL because the command still exists.

- [ ] **Step 3: Remove command registration**

Remove `cmd.AddCommand(newManagerPluginCmd(st))` from `newManageCmd`. Leave the helper functions in place if `atm init` uses them.

- [ ] **Step 4: Run test to verify pass**

Run: `go test ./internal/cli -run TestManagePluginCommandRemoved`

Expected: PASS.

### Task 3: Auto-Create Projects From Launchers

**Files:**
- Modify: `internal/cli/developing.go`
- Modify: `internal/cli/manager.go`
- Modify: `internal/cli/launcher_shared.go`
- Test: `internal/cli/developing_test.go`
- Test: `internal/cli/manager_test.go`

**Interfaces:**
- Produces: `ensureProjectForLaunch(s *store.Store, code string) (store.Project, error)`.
- Consumes: `s.GetProject(code)` and `s.CreateProject(store.ProjectCreate{Code: code, Name: code}, "admin@cli:unset")`.

- [ ] **Step 1: Write failing launcher tests**

Add tests that launch developer and manager commands without pre-creating `FOO`, assert exit success, then assert `project show --code FOO` succeeds and includes name `FOO`.

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/cli -run 'TestDeveloperLaunchAutoCreatesProject|TestManageLaunchAutoCreatesProject'`

Expected: FAIL with the current missing-project behavior.

- [ ] **Step 3: Implement shared helper**

Add `ensureProjectForLaunch` in `launcher_shared.go` and use it in `runDeveloping` and `runManager` instead of returning the old "create it first" error.

- [ ] **Step 4: Run focused launcher tests**

Run: `go test ./internal/cli -run 'TestDeveloperLaunchAutoCreatesProject|TestManageLaunchAutoCreatesProject|TestDeveloperCodexLaunchJSON|TestManageCodexPlanningLaunchJSON'`

Expected: PASS.

### Task 4: Verification and Commit

**Files:**
- All modified files.

**Interfaces:**
- Consumes: all prior tasks.
- Produces: verified branch ready for handoff.

- [ ] **Step 1: Run package tests**

Run: `go test ./internal/cli ./internal/developing ./internal/manager`

Expected: PASS.

- [ ] **Step 2: Run repository verification**

Run: `make verify`

Expected: PASS.

- [ ] **Step 3: Commit**

Run: `git status --short`, then stage the spec, plan, tests, and implementation and commit with message `feat: simplify init setup and project launches`.
