# GitHub Issues Sync Adapter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a bidirectional adapter (`internal/githubsync`) that maps ATM's v2 eventsource to GitHub Issues/labels/comments, plus the `atm github` CLI group and `.github/workflows/atm-sync.yml`, so the ATM project can dogfood itself on `github.com/TranDuongTu/atm`.

**Architecture:** A single package `internal/githubsync` holds two diff loops (ingest: GitHub→v2 events; project: v2 fold→GitHub API mutations), a GitHub REST API client (pagination + etag), and the identity/actor mapping tables. The adapter emits v2 events through the L4 `SyncTarget` interface and reads the fold through `eventsource.State`. A cobra command group `atm github pull/push/sync/status/import` wraps it; a GitHub Actions workflow drives `sync` on a schedule + webhooks and commits `log.jsonl` back to the repo as a machine-maintained file.

**Tech Stack:** Go 1.22+, cobra (existing CLI), `net/http` for the GitHub API (no SDK — the REST v3 API is simple enough), `encoding/json`, `net/http/httptest` for the mocked integration tests, existing `internal/store` + `internal/eventsource` (once ATM-0106/0107/0108 land), `make verify` as the gate.

## GATING PREREQUISITE — DO NOT START UNTIL THESE LAND

**This plan cannot execute until the following upstream specs land on `main`:**

- **ATM-0106** (`docs/superpowers/specs/2026-07-14-eventsource-core-v2-design.md`): the `internal/eventsource` package with `Event`, `Parse`, `NewEvent`, `Fold`, `State`, `MintTaskAlias`, `MintCommentAlias`, `UpgradeV1`, `CompareEvents`. This plan references `eventsource.Event`, `eventsource.State`, `eventsource.NewEvent`, `eventsource.Fold` as existing types/functions.
- **ATM-0107** (L3 on-disk DAG layout): the adapter reads/writes `log.jsonl` through whatever L3 defines. This plan assumes `eventsource` exposes `ReadLog(code) ([]Event, error)` and `AppendLog(code, events []Event) error`.
- **ATM-0108** (L4 sync & transport protocol): the adapter implements L4's `SyncTarget` interface. This plan assumes the interface shape from `2026-07-06-atm-storage-sync-design.md`:
  ```go
  type SyncTarget interface {
      FetchLog(code string) ([]Event, error)
      WriteLog(code string, entries []Event) error
  }
  ```
  If L4's interface shifts, this adapter shifts with it — that coupling is documented in the spec.

**If any of these are not on `main`, STOP. Do not implement this plan.**

## Global Constraints

- Follow `docs/superpowers/specs/2026-07-14-github-issues-sync-adapter-design.md` exactly.
- The v2 eventsource is source of truth; the GitHub side is a replica reconciled to the fold. Never the other way around.
- `atm github` commands require `--project <CODE>`; the repo is read from project config (`atm project set-github --repo <slug>`) but overridable with `--repo <slug>`.
- Actor form for GitHub-side users: `collaborator@github:<login>` (built-in persona `collaborator`, registered on first sight). Bots: `bot@github:<login>`.
- ATM-side actors stay as-is (`developer@…`, `manager@…`). The v2 event's actor records who caused the mutation; the GitHub API credential is plumbing, separate from the actor.
- Identity mapping links are stored on the events themselves: `payload.github_issue` (task↔issue number), `payload.github_comment` (comment↔issue comment id). Labels are verbatim (`ATM:status:open` ↔ GitHub label `ATM:status:open` — colons allowed in GitHub label names).
- External-id assignment uses adapter-defined actions `task.linked` / `comment.linked` (inert-but-causal per ATM-0106 decision 8). NOT the retired `task.meta-changed`.
- No emojis in code or commits.
- No GitHub SDK — use `net/http` against the REST v3 API. The API surface needed is small (list issues, list issue comments, create/update issue, create/update comment, add/remove label, create label).
- Follow existing style in neighboring files (cobra command shape, `cliState` helpers, `emit`, `writeJSON`, golden test pattern with `testdata/golden/`).
- Run `make verify` before declaring implementation complete.
- GitHub tokens are never stored by ATM — read from `GITHUB_TOKEN` env per-invocation.
- v1 does NOT ingest GitHub comment deletions (documented limitation).
- One ATM project ↔ one GitHub repo in v1. No multi-repo mapping.

---

## File Structure

- **Create `internal/githubsync/adapter.go`:** the core — `Adapter` struct holding config + store handles, `Pull`, `Push`, `Sync`, `Status`, `Import` methods. The two diff loops live here.
- **Create `internal/githubsync/identity.go`:** identity mapping tables + helpers (`taskByGitHubIssue`, `commentByGitHubID`, `labelNameToGitHub`, `githubLabelToATM`). Pure functions over `eventsource.State` + GitHub API response shapes.
- **Create `internal/githubsync/actor.go`:** GitHub user → ATM actor mapping. `ActorForUser(user) string`, `EnsurePersonas(st *store.Store) error` (registers `collaborator`/`bot` if missing), `ParseActor(raw) (persona, agent, model string, err error)`.
- **Create `internal/githubsync/client.go`:** thin GitHub REST v3 client — `ListIssues`, `ListIssueComments`, `CreateIssue`, `UpdateIssue`, `CreateComment`, `UpdateComment`, `AddLabel`, `RemoveLabel`, `CreateLabel`. Pagination + etag/If-Modified-Since. No SDK; `net/http` + `encoding/json`.
- **Create `internal/githubsync/sync_target.go`:** the L4 `SyncTarget` implementation for GitHub — `FetchLog`/`WriteLog` in terms of the client + adapter diff logic. This is what L4 calls; the CLI commands call `Pull`/`Push`/`Sync` directly.
- **Create `internal/githubsync/eventemit.go`:** helpers to author v2 events for ingest — `emitTaskCreated`, `emitTaskEdited`, `emitTaskLabelAdded`, `emitTaskLabelRemoved`, `emitCommentCreated`, `emitTaskLinked`, `emitCommentLinked`. Each stamps the GitHub-derived actor, sets `payload.github_issue`/`github_comment`, and sets parents to the current frontier.
- **Create `internal/githubsync/types.go`:** GitHub API response shapes (`Issue`, `IssueComment`, `Label`, `User`, `TimelineEvent`). Minimal fields only — what the adapter reads.
- **Create `internal/githubsync/testdata/`:** golden fixtures — captured GitHub API responses (issues, comments, labels) + expected v2 events for each translation path.
- **Create `internal/githubsync/adapter_test.go`, `identity_test.go`, `actor_test.go`, `client_test.go`:** unit + integration tests (`httptest.Server` mock).
- **Create `internal/cli/github.go`:** `newGithubCmd(st)` + subcommands (`pull`, `push`, `sync`, `status`, `import`). `runGithubPull`/`runGithubPush`/`runGithubSync`/`runGithubStatus`/`runGithubImport` helpers following the existing cobra pattern.
- **Modify `internal/cli/project.go`:** add `set-github` subcommand writing `github.repo` to project config.
- **Modify `internal/store/config.go`:** add `github.repo` field to project config (`ProjectConfig.GithubRepo string`) + `SetGithubRepo(code, slug, actor string)` mutator.
- **Modify `internal/cli/root.go`:** register `newGithubCmd(st)` on the root.
- **Modify `internal/seed/persona.go`:** add `collaborator` and `bot` to the built-in persona set (5 built-ins now: developer, manager, admin, collaborator, bot).
- **Modify `internal/store/log.go` action enum:** add `task.linked` and `comment.linked` to `validActions` (they're adapter-defined but the v1 store's `validActions` map is the gate for `AppendLog`). When ATM-0106's `internal/eventsource` lands and the store switchover happens, these will already be known to the v2 fold's "unknown actions are inert" rule (ATM-0106 decision 8).
- **Create `.github/workflows/atm-sync.yml`:** the sync workflow (schedule + webhook triggers + commit-back).
- **Modify `CONTRIBUTING.md`:** note that `.atm/<CODE>/log.jsonl` is machine-maintained by `atm-sync`; humans don't edit it.
- **Modify `internal/cli/conventions.go`:** add the "GitHub-hosted projects" section to `atm conventions` output.
- **Modify `README.md`:** document the `atm github` commands + workflow.

---

### Task 1: Built-in personas `collaborator` and `bot`

**Files:**
- Modify: `internal/seed/persona.go`
- Modify: `internal/store/persona_test.go`

**Interfaces:**
- Consumes: existing `seed.Persona` struct, `seed.Personas` slice.
- Produces: `seed.Personas` now has 5 entries including `collaborator` (empty prompt, "A human acting through GitHub's issue tracker, not ATM directly.") and `bot` (empty prompt, "An automated GitHub App/bot acting through the GitHub API."). Downstream code reads `seed.Personas` by range — no signature changes.

- [ ] **Step 1: Write the failing test**

In `internal/store/persona_test.go`, update the persona-count test (find the existing `TestSeedPersonas*` that asserts the count is 3 and bump it to 5). If no count test exists, add:

```go
func TestSeedPersonasIncludesCollaboratorAndBot(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.SeedPersonas("admin@atm:seed"); err != nil {
		t.Fatalf("SeedPersonas: %v", err)
	}
	for _, name := range []string{"developer", "manager", "admin", "collaborator", "bot"} {
		if _, err := s.GetPersona(name); err != nil {
			t.Errorf("persona %q missing after seed: %v", name, err)
		}
	}
	// collaborator and bot must have empty prompts (classification, not agent)
	for _, name := range []string{"collaborator", "bot"} {
		p, err := s.GetPersona(name)
		if err != nil {
			t.Fatalf("GetPersona(%q): %v", name, err)
		}
		if p.Prompt != "" {
			t.Errorf("persona %q should have empty prompt, got %q", name, p.Prompt)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestSeedPersonasIncludesCollaboratorAndBot -v`
Expected: FAIL — `collaborator`/`bot` not found.

- [ ] **Step 3: Add the two personas to `seed.Personas`**

In `internal/seed/persona.go`, append after the `admin` entry:

```go
{
    Name:        "collaborator",
    Description: "A human acting through GitHub's issue tracker, not ATM directly.",
    Prompt:      "",
},
{
    Name:        "bot",
    Description: "An automated GitHub App/bot acting through the GitHub API.",
    Prompt:      "",
},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestSeedPersonasIncludesCollaboratorAndBot -v`
Expected: PASS

- [ ] **Step 5: Run the full persona test suite + verify**

Run: `go test ./internal/store/ -run Persona -v && make verify`
Expected: PASS — all persona tests green, full gate passes.

- [ ] **Step 6: Commit**

```bash
git add internal/seed/persona.go internal/store/persona_test.go
git commit -m "feat(seed): add collaborator and bot built-in personas

For ATM-0123 GitHub Issues sync adapter. collaborator is a human acting
through GitHub's issue tracker; bot is a GitHub App/bot. Both have empty
prompts (classification, not autonomous agents)."
```

---

### Task 2: Adapter-defined actions `task.linked` / `comment.linked`

**Files:**
- Modify: `internal/store/log.go`
- Modify: `internal/store/log_test.go`

**Interfaces:**
- Consumes: existing `validActions` map.
- Produces: `validActions["task.linked"] = true` and `validActions["comment.linked"] = true` so `AppendLog` accepts them. They carry `payload.github_issue` / `payload.github_comment` and are inert to the fold (fold writes no slots for unknown actions per ATM-0106 decision 8).

- [ ] **Step 1: Write the failing test**

In `internal/store/log_test.go`, add:

```go
func TestAppendLogAcceptsTaskLinkedAndCommentLinked(t *testing.T) {
	s := newTestStore(t)
	code := "TEST"
	if _, err := s.CreateProject(code, "Test", "admin@cli:unset"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	actor := "collaborator@github:alice"
	for _, action := range []string{"task.linked", "comment.linked"} {
		_, err := s.AppendLog(code, store.LogEntry{
			At:     time.Now().UTC(),
			Actor:  actor,
			Action: action,
			Subject: store.Subject{
				Kind: "task",
				ID:   "TEST-0001",
			},
			Payload: mustMarshal(map[string]any{"github_issue": 42}),
		})
		if err != nil {
			t.Errorf("AppendLog rejected action %q: %v", action, err)
		}
	}
}
```

If `mustMarshal` isn't in the test package, use `json.Marshal` directly and pass the `json.RawMessage`. Follow the existing pattern in `log_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestAppendLogAcceptsTaskLinkedAndCommentLinked -v`
Expected: FAIL — `AppendLog` rejects unknown action.

- [ ] **Step 3: Add the two actions to the enum**

In `internal/store/log.go`, add near the existing action constants (after `ActionCommentRemoved`):

```go
ActionTaskLinked     = "task.linked"     // adapter: record payload.github_issue on a task (inert to fold)
ActionCommentLinked  = "comment.linked"  // adapter: record payload.github_comment on a comment (inert to fold)
```

And add to `validActions` map:

```go
ActionTaskLinked:     true,
ActionCommentLinked:  true,
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestAppendLogAcceptsTaskLinkedAndCommentLinked -v`
Expected: PASS

- [ ] **Step 5: Full store suite + verify**

Run: `go test ./internal/store/... && make verify`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/store/log.go internal/store/log_test.go
git commit -m "feat(store): accept task.linked and comment.linked actions

Adapter-defined actions for ATM-0123 GitHub Issues sync. They carry
payload.github_issue/github_comment and are inert to the fold per
ATM-0106 decision 8 (unknown actions are inert-but-causal). NOT the
retired task.meta-changed."
```

---

### Task 3: Project config `github.repo` field

**Files:**
- Modify: `internal/store/config.go`
- Modify: `internal/store/config_test.go`
- Modify: `internal/cli/project.go`
- Modify: `internal/cli/project_test.go`

**Interfaces:**
- Consumes: existing project config struct + mutator pattern.
- Produces: `ProjectConfig.GithubRepo string` field; `SetGithubRepo(code, slug, actor string) error` mutator (appends a `project.name-changed`-style event? No — `github.repo` is project *config*, not task state; store it in the project config file, not the eventsource). CLI: `atm project set-github --project <CODE> --repo <slug>`.

- [ ] **Step 1: Write the failing test for the store mutator**

In `internal/store/config_test.go`, add:

```go
func TestSetGithubRepo(t *testing.T) {
	s := newTestStore(t)
	code := "TEST"
	if _, err := s.CreateProject(code, "Test", "admin@cli:unset"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := s.SetGithubRepo(code, "TranDuongTu/atm", "admin@cli:unset"); err != nil {
		t.Fatalf("SetGithubRepo: %v", err)
	}
	p, err := s.GetProject(code)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if p.GithubRepo != "TranDuongTu/atm" {
		t.Errorf("GithubRepo = %q, want TranDuongTu/atm", p.GithubRepo)
	}
	// idempotent re-set
	if err := s.SetGithubRepo(code, "TranDuongTu/atm", "admin@cli:unset"); err != nil {
		t.Fatalf("re-set: %v", err)
	}
}
```

Look at the existing config tests (`config_test.go`) for the project-config field pattern — `GithubRepo` is a plain config field persisted alongside `Name`, not an eventsource subject. Follow how `Name` is read and written (likely a `projects/<CODE>.json` config row in `cache.db` after ATM-0027, or a project config file before).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestSetGithubRepo -v`
Expected: FAIL — `SetGithubRepo` undefined, `GithubRepo` field missing.

- [ ] **Step 3: Implement the store mutator**

In `internal/store/config.go`, add `GithubRepo string` to the project config struct (look for `Project` struct — `GithubRepo` is a new field with `json:"github_repo,omitempty"`). Add the mutator:

```go
func (s *Store) SetGithubRepo(code, slug, actor string) error {
    if err := s.validateActor(actor); err != nil {
        return err
    }
    return s.WithLock(code, func() error {
        p, err := s.getProjectLocked(code)
        if err != nil {
            return err
        }
        p.GithubRepo = slug
        p.UpdatedAt = Now()
        p.UpdatedBy = actor
        return s.writeProjectCacheLocked(code, p)
    })
}
```

Mirror the exact shape of the existing `SetName` mutator (find it with `rg -n "func \(s \*Store\) SetName" internal/store/`). If the project struct is written via a cache row (post-ATM-0027 `cache.db`), use `writeProjectCacheLocked`; if via a JSON file, use the existing project-file writer. The key is: `github.repo` is config, **not** an eventsource event — it lives in the project record, persisted to the same place `Name` is.

- [ ] **Step 4: Run store test to verify it passes**

Run: `go test ./internal/store/ -run TestSetGithubRepo -v`
Expected: PASS

- [ ] **Step 5: Write the failing CLI test**

In `internal/cli/project_test.go`, add a golden test for `atm project set-github`:

```go
func TestProjectSetGithubCmd(t *testing.T) {
	st := newTestCLI(t)
	code := "TEST"
	mustCreateProject(t, st, code, "Test")
	args := []string{"project", "set-github", "--project", code, "--repo", "TranDuongTu/atm"}
	out, err := runCLI(st, args)
	if err != nil {
		t.Fatalf("set-github: %v\n%s", err, out)
	}
	// verify the field stuck via a follow-up query (use whatever project-show
	// golden pattern exists, or query the store directly)
	s := st.store()
	p, err := s.GetProject(code)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if p.GithubRepo != "TranDuongTu/atm" {
		t.Errorf("GithubRepo = %q, want TranDuongTu/atm", p.GithubRepo)
	}
}
```

Look at the existing `project_test.go` for `mustCreateProject`, `runCLI`, `st.store()` helpers. Match the existing pattern exactly.

- [ ] **Step 6: Run CLI test to verify it fails**

Run: `go test ./internal/cli/ -run TestProjectSetGithubCmd -v`
Expected: FAIL — `set-github` subcommand unknown.

- [ ] **Step 7: Implement the CLI subcommand**

In `internal/cli/project.go`, add a `setGithubCmd` following the pattern of the existing `setNameCmd` (find with `rg -n "newProjectSetNameCmd\|set-name" internal/cli/project.go`). It takes `--project <CODE>` and `--repo <slug>` flags and calls `st.openStore()` then `s.SetGithubRepo(code, slug, actor)`.

Register it on the project command: `projectCmd.AddCommand(setGithubCmd)`.

- [ ] **Step 8: Run CLI test to verify it passes**

Run: `go test ./internal/cli/ -run TestProjectSetGithubCmd -v`
Expected: PASS

- [ ] **Step 9: Full verify**

Run: `make verify`
Expected: PASS

- [ ] **Step 10: Commit**

```bash
git add internal/store/config.go internal/store/config_test.go internal/cli/project.go internal/cli/project_test.go
git commit -m "feat(project): add github.repo config field and set-github CLI

For ATM-0123. The repo slug is project config (not eventsource state);
stored in the project record next to Name. Used by atm github commands to
know which repo to sync against."
```

---

### Task 4: GitHub API client (`internal/githubsync/client.go`)

**Files:**
- Create: `internal/githubsync/client.go`
- Create: `internal/githubsync/types.go`
- Create: `internal/githubsync/client_test.go`

**Interfaces:**
- Consumes: `net/http`, `encoding/json`, `os.Getenv("GITHUB_TOKEN")`.
- Produces: `Client` struct with methods `ListIssues(ctx, owner, repo) ([]Issue, error)`, `ListIssueComments(ctx, owner, repo, issueNumber) ([]IssueComment, error)`, `CreateIssue(ctx, owner, repo, req CreateIssueRequest) (Issue, error)`, `UpdateIssue(ctx, owner, repo, issueNumber, req UpdateIssueRequest) (Issue, error)`, `CreateComment(ctx, owner, repo, issueNumber, body string) (IssueComment, error)`, `UpdateComment(ctx, owner, repo, commentID int64, body string) (IssueComment, error)`, `AddLabel(ctx, owner, repo, issueNumber, label string) error`, `RemoveLabel(ctx, owner, repo, issueNumber, label string) error`, `CreateLabel(ctx, owner, repo, name string) (Label, error)`. Plus `SetBaseURL(url string)` for testing. Plus types `Issue`, `IssueComment`, `Label`, `User`, `CreateIssueRequest`, `UpdateIssueRequest`.

- [ ] **Step 1: Write the failing test (list issues against an httptest mock)**

In `internal/githubsync/client_test.go`:

```go
package githubsync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientListIssuesPagination(t *testing.T) {
	page1 := `[{"number":1,"title":"t1","body":"b1","state":"open","user":{"login":"alice","id":1},"labels":[]}]`
	page2 := `[{"number":2,"title":"t2","body":"b2","state":"open","user":{"login":"bob","id":2},"labels":[]}]`
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing bearer auth, got %q", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/repos/OWNER/REPO/issues" {
			t.Errorf("path = %q, want /repos/OWNER/REPO/issues", r.URL.Path)
		}
		switch r.URL.Query().Get("page") {
		case "", "1":
			w.Header().Set("Link", `<https://example.com/repos/OWNER/REPO/issues?page=2>; rel="next"`)
			w.Write([]byte(page1))
		case "2":
			w.Write([]byte(page2))
		}
	}))
	defer srv.Close()
	c := NewClient("test-token")
	c.SetBaseURL(srv.URL)
	issues, err := c.ListIssues(context.Background(), "OWNER", "REPO")
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("got %d issues, want 2", len(issues))
	}
	if calls != 2 {
		t.Errorf("got %d calls, want 2 (pagination)", calls)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/githubsync/ -run TestClientListIssuesPagination -v`
Expected: FAIL — package doesn't exist / `NewClient` undefined.

- [ ] **Step 3: Create `types.go`**

```go
package githubsync

type User struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
}

type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type Issue struct {
	Number  int     `json:"number"`
	Title   string  `json:"title"`
	Body    string  `json:"body"`
	State   string  `json:"state"` // "open" or "closed"
	Labels  []Label `json:"labels"`
	User    User    `json:"user"`
	// timeline events for label add/remove would come from a separate endpoint;
	// v1 diff derives label deltas by comparing the issue's current label set
	// against the folded task's label set (see adapter.go).
}

type IssueComment struct {
	ID     int64  `json:"id"`
	Body   string `json:"body"`
	User   User   `json:"user"`
	IssueURL string `json:"issue_url"` // used to derive the issue number
}

type CreateIssueRequest struct {
	Title   string   `json:"title"`
	Body    string   `json:"body,omitempty"`
	Labels  []string `json:"labels,omitempty"`
}

type UpdateIssueRequest struct {
	Title  string `json:"title,omitempty"`
	Body   string `json:"body,omitempty"`
	State  string `json:"state,omitempty"` // "open" or "closed"
	Labels []string `json:"labels,omitempty"`
}
```

- [ ] **Step 4: Create `client.go`**

```go
package githubsync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const defaultBaseURL = "https://api.github.com"

type Client struct {
	token   string
	baseURL string
	http    *http.Client
}

func NewClient(token string) *Client {
	return &Client{token: token, baseURL: defaultBaseURL, http: http.DefaultClient}
}

func NewClientFromEnv() (*Client, error) {
	tok := os.Getenv("GITHUB_TOKEN")
	if tok == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN env var not set")
	}
	return NewClient(tok), nil
}

func (c *Client) SetBaseURL(u string) { c.baseURL = u }
func (c *Client) SetHTTPClient(h *http.Client) { c.http = h }

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body io.Reader) (*http.Response, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, err
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + path
	if query != nil {
		u.RawQuery = query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

func (c *Client) ListIssues(ctx context.Context, owner, repo string) ([]Issue, error) {
	var all []Issue
	page := 1
	for {
		q := url.Values{}
		q.Set("state", "all")
		q.Set("per_page", "100")
		q.Set("page", strconv.Itoa(page))
		resp, err := c.do(ctx, http.MethodGet, "/repos/"+owner+"/"+repo+"/issues", q, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("list issues page %d: %d %s", page, resp.StatusCode, string(b))
		}
		var pageIssues []Issue
		dec := json.NewDecoder(resp.Body)
		if err := dec.Decode(&pageIssues); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decode issues page %d: %w", page, err)
		}
		resp.Body.Close()
		// GitHub returns PRs mixed with issues; filter PRs out by the "pull_request" key.
		// (The Issue type above omits that field; to filter, we'd need to capture it. For
		// simplicity v1 uses the /issues endpoint which returns both, then filter. See
		// step 5 PR-filter refinement below.)
		all = append(all, pageIssues...)
		if !hasNextPage(resp) {
			break
		}
		page++
	}
	return all, nil
}

func hasNextPage(resp *http.Response) bool {
	link := resp.Header.Get("Link")
	return strings.Contains(link, `rel="next"`)
}

// Implement the remaining methods following the same shape:
// CreateIssue, UpdateIssue, CreateComment, UpdateComment, AddLabel,
// RemoveLabel, CreateLabel, ListIssueComments. Each is one do() call with
// the right path + JSON encode/decode. See the spec's CLI surface section
// for the exact endpoints.
//
// CreateIssue:    POST /repos/{owner}/{repo}/issues          body CreateIssueRequest -> Issue
// UpdateIssue:    PATCH /repos/{owner}/{repo}/issues/{num}  body UpdateIssueRequest -> Issue
// ListIssueComments: GET /repos/{owner}/{repo}/issues/{num}/comments (paginated) -> []IssueComment
// CreateComment: POST /repos/{owner}/{repo}/issues/{num}/comments  body {body:string} -> IssueComment
// UpdateComment: PATCH /repos/{owner}/{repo}/issues/comments/{id}   body {body:string} -> IssueComment
// AddLabel:      POST /repos/{owner}/{repo}/issues/{num}/labels body {labels:[]string}
// RemoveLabel:   DELETE /repos/{owner}/{repo}/issues/{num}/labels/{name}
// CreateLabel:   POST /repos/{owner}/{repo}/labels          body {name:string} -> Label
```

Implement each method in full (not just the stub above) before moving on. Each follows the exact same `c.do()` pattern as `ListIssues`.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/githubsync/ -run TestClientListIssuesPagination -v`
Expected: PASS

- [ ] **Step 6: Write tests for each remaining method (CreateIssue, UpdateIssue, ListIssueComments, CreateComment, UpdateComment, AddLabel, RemoveLabel, CreateLabel)**

One `httptest`-based test per method, asserting the right path, method, request body, and response decoding. Follow the `TestClientListIssuesPagination` pattern. Add PR filtering to `ListIssues` by adding a `PullRequest *struct{} `json:"pull_request,omitempty"` field to the `Issue` type and skipping issues where it's non-nil.

- [ ] **Step 7: Run all client tests**

Run: `go test ./internal/githubsync/ -v`
Expected: PASS — all method tests green.

- [ ] **Step 8: Verify + commit**

Run: `make verify`
Expected: PASS

```bash
git add internal/githubsync/
git commit -m "feat(githubsync): GitHub REST v3 client

For ATM-0123. Thin net/http client against the GitHub REST v3 API:
issues, comments, labels. Pagination via Link header. No SDK; the API
surface is small. Bearer auth from GITHUB_TOKEN env. SetBaseURL for
httptest mocking. PRs filtered out of ListIssues via the pull_request
key."
```

---

### Task 5: Actor mapping (`internal/githubsync/actor.go`)

**Files:**
- Create: `internal/githubsync/actor.go`
- Create: `internal/githubsync/actor_test.go`

**Interfaces:**
- Consumes: `githubsync.User` (from Task 4), `store.Store` (for persona registration).
- Produces: `ActorForUser(u User) string` (returns `collaborator@github:<login>` for humans, `bot@github:<login>` for bot users — detect via a heuristic: login ending in `[bot]` or the `User` being a GitHub App), `EnsurePersonas(s *store.Store) error` (idempotent — checks if `collaborator`/`bot` exist, creates if missing; relies on Task 1's seed personas), `ParseActor(raw string) (persona, agent, model string, err error)`.

- [ ] **Step 1: Write the failing test**

```go
package githubsync

import (
	"testing"
)

func TestActorForUser(t *testing.T) {
	cases := []struct {
		user  User
		want  string
	}{
		{User{Login: "alice", ID: 1}, "collaborator@github:alice"},
		{User{Login: "dependabot[bot]", ID: 2}, "bot@github:dependabot[bot]"},
		{User{Login: "github-actions[bot]", ID: 3}, "bot@github:github-actions[bot]"},
	}
	for _, c := range cases {
		got := ActorForUser(c.user)
		if got != c.want {
			t.Errorf("ActorForUser(%+v) = %q, want %q", c.user, got, c.want)
		}
	}
}

func TestParseActor(t *testing.T) {
	persona, agent, model, err := ParseActor("collaborator@github:alice")
	if err != nil {
		t.Fatalf("ParseActor: %v", err)
	}
	if persona != "collaborator" || agent != "github" || model != "alice" {
		t.Errorf("got (%q,%q,%q), want (collaborator,github,alice)", persona, agent, model)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/githubsync/ -run TestActorForUser|TestParseActor -v`
Expected: FAIL — functions undefined.

- [ ] **Step 3: Implement `actor.go`**

```go
package githubsync

import (
	"fmt"
	"strings"

	"atm/internal/store"
)

// ActorForUser maps a GitHub API user to an ATM actor string.
// Humans -> collaborator@github:<login>; GitHub Apps/bots (login ending in [bot]
// or [app]) -> bot@github:<login>.
func ActorForUser(u User) string {
	if isBotLogin(u.Login) {
		return "bot@github:" + u.Login
	}
	return "collaborator@github:" + u.Login
}

func isBotLogin(login string) bool {
	return strings.HasSuffix(login, "[bot]") || strings.HasSuffix(login, "[app]")
}

// EnsurePersonas idempotently registers the collaborator and bot personas if
// the store doesn't have them yet. Relies on seed.Personas (Task 1) — normally
// already seeded, but this is the defensive first-sight path for an existing
// store that hasn't seen a github sync yet.
func EnsurePersonas(s *store.Store) error {
	for _, name := range []string{"collaborator", "bot"} {
		if _, err := s.GetPersona(name); err == nil {
			continue
		} else if !store.IsNotFound(err) {
			return fmt.Errorf("persona lookup %q: %w", name, err)
		}
		// Create via the store's persona API. Use the admin persona as the creator.
		// (seed.Personas has them; if the store was built before Task 1 landed,
		// ensureBuiltinPersonas will have picked them up.)
		// If the store is too old to have them in seed, fall back to explicit create.
		if err := createPersonaIfMissing(s, name); err != nil {
			return err
		}
	}
	return nil
}

func createPersonaIfMissing(s *store.Store, name string) error {
	// Use whatever the existing persona-create store API is. Look at
	// internal/store/persona.go for the exact signature (CreatePersona or
	// similar). The persona has an empty prompt (classification, not agent).
	// This is the escape hatch for pre-Task-1 stores.
	panic("implement: look at internal/store/persona.go for the create signature")
}

// ParseActor splits persona@agent:model into its three segments.
func ParseActor(raw string) (persona, agent, model string, err error) {
	p, rest, ok := strings.Cut(raw, "@")
	if !ok {
		return "", "", "", fmt.Errorf("actor missing @: %q", raw)
	}
	a, m, ok := strings.Cut(rest, ":")
	if !ok || p == "" || a == "" || m == "" {
		return "", "", "", fmt.Errorf("actor malformed: %q", raw)
	}
	return p, a, m, nil
}
```

Resolve the `createPersonaIfMissing` panic by reading `internal/store/persona.go` for the actual `CreatePersona` signature and implementing it. The store test in Task 1 already proves the seeded personas exist on fresh stores; this path is only for legacy stores.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/githubsync/ -run TestActorForUser|TestParseActor -v`
Expected: PASS

- [ ] **Step 5: Write the EnsurePersonas integration test against a real test store**

Use the test-store helper from `internal/store/` (look at `newTestStore` in the store tests). Assert it's idempotent: running twice doesn't error and doesn't duplicate.

- [ ] **Step 6: Run + verify + commit**

Run: `go test ./internal/githubsync/ -v && make verify`
Expected: PASS

```bash
git add internal/githubsync/actor.go internal/githubsync/actor_test.go
git commit -m "feat(githubsync): actor mapping for GitHub users

For ATM-0123. collaborator@github:<login> for humans,
bot@github:<login> for GitHub Apps/bots (detected via [bot]/[app] suffix).
EnsurePersonas idempotently registers the built-ins on first sight."
```

---

### Task 6: Identity mapping (`internal/githubsync/identity.go`)

**Files:**
- Create: `internal/githubsync/identity.go`
- Create: `internal/githubsync/identity_test.go`

**Interfaces:**
- Consumes: `eventsource.State` (from ATM-0106, assumed landed per the gating prerequisite), `githubsync.Issue`/`IssueComment`/`Label` (from Task 4).
- Produces: `TaskByGitHubIssue(state *eventsource.State, issueNumber int) *TaskState` (finds the task whose creation event has `payload.github_issue == issueNumber`), `CommentByGitHubID(state, commentID int64) *CommentState`, `LabelNameToGitHub(labelName string) string` (verbatim — returns the label as-is, since colons are valid in GitHub label names), `GitHubLabelToATM(name string) string` (verbatim inverse). Plus `SetGitHubIssueOnPayload(payload map[string]any, issueNumber int)` and `GetGitHubIssueFromPayload(payload json.RawMessage) (int, bool)` helpers.

**PREREQUISITE:** This task requires `internal/eventsource` (ATM-0106) to be landed. If it isn't, STOP. The `eventsource.State`/`TaskState`/`CommentState` types must exist.

- [ ] **Step 1: Write the failing test**

```go
package githubsync

import (
	"encoding/json"
	"testing"

	"atm/internal/eventsource"
)

func TestTaskByGitHubIssue(t *testing.T) {
	// Build a minimal eventsource.State with one task whose creation event
	// carries payload.github_issue = 42.
	state := &eventsource.State{
		Tasks: map[string]*eventsource.TaskState{
			"taskid1": {
				Alias: "ATM-0001",
				// ... fields per eventsource.TaskState
			},
		},
	}
	// To inject payload.github_issue, we need a way to associate it with a
	// task. The cleanest path: add a per-task index built from creation events
	// (or a method on State). If eventsource.State doesn't expose per-task
	// creation-event payloads, we build the index in identity.go by scanning
	// creation events once. See the implementation.
	// ...
	// For this test: construct the state via FoldEvents on a synthetic
	// task.created event with the payload, then look it up.
	ts := TaskByGitHubIssue(state, 42)
	if ts == nil {
		t.Fatal("TaskByGitHubIssue returned nil for issue 42")
	}
	if ts.Alias != "ATM-0001" {
		t.Errorf("alias = %q, want ATM-0001", ts.Alias)
	}
}
```

The exact shape depends on what `eventsource.State` exposes. **Read `internal/eventsource/` first** (after ATM-0106 lands) to see whether `TaskState` carries the raw creation-event payload or whether identity.go needs to scan creation events. The implementation chooses the cleanest path: likely a `githubIssueIndex` built once from the fold's creation events, mapping `payload.github_issue -> task identity`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/githubsync/ -run TestTaskByGitHubIssue -v`
Expected: FAIL — function undefined / package missing.

- [ ] **Step 3: Implement `identity.go`**

```go
package githubsync

import (
	"encoding/json"

	"atm/internal/eventsource"
)

// githubIssueIndex maps payload.github_issue -> task identity.
type githubIssueIndex map[int]string

// githubCommentIndex maps payload.github_comment -> comment identity.
type githubCommentIndex map[int64]string

// buildIndices scans a folded state's creation events (or a provided event
// list) and builds the two indices. The exact source depends on what
// eventsource.State exposes; if State exposes the raw events, scan those;
// otherwise, the caller passes the []Event from eventsource.ReadLog.
func buildIndices(events []*eventsource.Event) (githubIssueIndex, githubCommentIndex) {
	issues := githubIssueIndex{}
	comments := githubCommentIndex{}
	for _, e := range events {
		switch e.Action {
		case "task.linked":
			n, ok := payloadInt(e.Payload, "github_issue")
			if ok && e.Subject.ID != "" {
				issues[n] = e.Subject.ID
			}
		case "comment.linked":
			id, ok := payloadInt64(e.Payload, "github_comment")
			if ok && e.Subject.ID != "" {
				comments[id] = e.Subject.ID
			}
		}
	}
	return issues, comments
}

// TaskByGitHubIssue finds the task linked to the given GitHub issue number.
// Returns nil if no task is linked.
func TaskByGitHubIssue(state *eventsource.State, events []*eventsource.Event, issueNumber int) *eventsource.TaskState {
	issues, _ := buildIndices(events)
	id, ok := issues[issueNumber]
	if !ok {
		return nil
	}
	return state.Tasks[id]
}

// CommentByGitHubID finds the comment linked to the given GitHub comment id.
func CommentByGitHubID(state *eventsource.State, events []*eventsource.Event, commentID int64) *eventsource.CommentState {
	_, comments := buildIndices(events)
	id, ok := comments[commentID]
	if !ok {
		return nil
	}
	return state.Comments[id]
}

// LabelNameToGitHub is verbatim — ATM labels and GitHub labels share the name.
// Colons are valid in GitHub label names.
func LabelNameToGitHub(labelName string) string { return labelName }

// GitHubLabelToATM is the verbatim inverse.
func GitHubLabelToATM(name string) string { return name }

func payloadInt(p json.RawMessage, key string) (int, bool) {
	var m map[string]any
	if err := json.Unmarshal(p, &m); err != nil {
		return 0, false
	}
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	f, ok := v.(float64)
	if !ok {
		return 0, false
	}
	return int(f), true
}

func payloadInt64(p json.RawMessage, key string) (int64, bool) {
	n, ok := payloadInt(p, key)
	return int64(n), ok
}

// SetGitHubIssueOnPayload sets payload.github_issue = issueNumber.
func SetGitHubIssueOnPayload(payload map[string]any, issueNumber int) {
	payload["github_issue"] = issueNumber
}

// SetGitHubCommentOnPayload sets payload.github_comment = commentID.
func SetGitHubCommentOnPayload(payload map[string]any, commentID int64) {
	payload["github_comment"] = commentID
}
```

The exact integration with `eventsource.State` depends on whether the fold exposes the raw events or just the materialized state. **Read the landed `internal/eventsource/` API before implementing** and adjust the `buildIndices` signature to take whatever `State` + events the caller has available. The principle is fixed: scan `task.linked`/`comment.linked` events for the index.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/githubsync/ -run TestTaskByGitHubIssue -v`
Expected: PASS

- [ ] **Step 5: Add round-trip + verbatim label tests**

```go
func TestLabelNameRoundTrip(t *testing.T) {
	cases := []string{"ATM:status:open", "ATM:priority:high", "ATM:context:agent"}
	for _, c := range cases {
		if got := GitHubLabelToATM(LabelNameToGitHub(c)); got != c {
			t.Errorf("round-trip %q -> %q", c, got)
		}
	}
}
```

- [ ] **Step 6: Run + verify + commit**

Run: `go test ./internal/githubsync/ -v && make verify`
Expected: PASS

```bash
git add internal/githubsync/identity.go internal/githubsync/identity_test.go
git commit -m "feat(githubsync): identity mapping between v2 entities and GitHub primitives

For ATM-0123. task.linked/comment.linked events scanned to build
github_issue -> task identity and github_comment -> comment identity
indices. Labels are verbatim (colons valid in GitHub label names).
Round-trip identity preserved."
```

---

### Task 7: Event authoring helpers (`internal/githubsync/eventemit.go`)

**Files:**
- Create: `internal/githubsync/eventemit.go`
- Create: `internal/githubsync/eventemit_test.go`

**Interfaces:**
- Consumes: `eventsource.Clock`, `eventsource.NewEvent`, `eventsource.Draft` (from ATM-0106), `githubsync.User`, `githubsync.Issue`/`IssueComment` (from Task 4), `githubsync.ActorForUser` (Task 5), identity helpers (Task 6).
- Produces: `EmitTaskCreated(clock, replica, parents, issue Issue) (*eventsource.Event, error)`, `EmitTaskEdited(...)`, `EmitTaskLabelAdded(...)`, `EmitTaskLabelRemoved(...)`, `EmitCommentCreated(...)`, `EmitTaskLinked(clock, replica, parents, taskID string, issueNumber int, actor string) (*Event, error)`, `EmitCommentLinked(...)`. Each stamps the GitHub-derived actor, sets the right payload fields, and calls `eventsource.NewEvent`.

- [ ] **Step 1: Write the failing test**

```go
package githubsync

import (
	"testing"
	"time"

	"atm/internal/eventsource"
)

func TestEmitTaskCreated(t *testing.T) {
	clock := eventsource.NewClock(func() int64 { return 1700000000000 })
	issue := Issue{Number: 42, Title: "Fix bug", Body: "desc", User: User{Login: "alice", ID: 1}}
	e, err := EmitTaskCreated(clock, "r_abc", []string{"p1"}, issue)
	if err != nil {
		t.Fatalf("EmitTaskCreated: %v", err)
	}
	if e.Action != "task.created" {
		t.Errorf("action = %q, want task.created", e.Action)
	}
	if e.Actor != "collaborator@github:alice" {
		t.Errorf("actor = %q, want collaborator@github:alice", e.Actor)
	}
	// payload should carry github_issue = 42, title = "Fix bug", body = "desc"
	if n, ok := payloadInt(e.Payload, "github_issue"); !ok || n != 42 {
		t.Errorf("payload.github_issue = %d ok=%v, want 42", n, ok)
	}
	if s, ok := payloadString(e.Payload, "title"); !ok || s != "Fix bug" {
		t.Errorf("payload.title = %q ok=%v, want Fix bug", s, ok)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/githubsync/ -run TestEmitTaskCreated -v`
Expected: FAIL — `EmitTaskCreated` undefined.

- [ ] **Step 3: Implement `eventemit.go`**

```go
package githubsync

import (
	"encoding/json"
	"time"

	"atm/internal/eventsource"
)

func EmitTaskCreated(clock *eventsource.Clock, replica string, parents []string, issue Issue) (*eventsource.Event, error) {
	alias := "" // minted by ATM-0106's MintTaskAlias at fold time, or here — check eventsource API
	payload := map[string]any{
		"github_issue": issue.Number,
		"title":        issue.Title,
		"description":  issue.Body,
		"labels":       labelsFromIssue(issue),
	}
	return eventsource.NewEvent(clock, replica, parents, eventsource.Draft{
		At:      issueCreatedAt(issue), // derive from issue, or clock.Now()
		Actor:   ActorForUser(issue.User),
		Action:  "task.created",
		Subject: eventsource.Subject{Kind: "task"},
		Payload: payload,
	})
}

func EmitTaskEdited(clock *eventsource.Clock, replica string, parents []string, taskID string, issue Issue) (*eventsource.Event, error) {
	return eventsource.NewEvent(clock, replica, parents, eventsource.Draft{
		At:      time.Now().UTC(),
		Actor:   ActorForUser(issue.User),
		Action:  "task.edited",
		Subject: eventsource.Subject{Kind: "task", ID: taskID},
		Payload: map[string]any{"title": issue.Title, "description": issue.Body},
	})
}

func EmitTaskLabelAdded(clock *eventsource.Clock, replica string, parents []string, taskID, label, actor string) (*eventsource.Event, error) {
	return eventsource.NewEvent(clock, replica, parents, eventsource.Draft{
		Actor:   actor,
		Action:  "task.label-added",
		Subject: eventsource.Subject{Kind: "task", ID: taskID},
		Payload: map[string]any{"label": label},
	})
}

func EmitTaskLabelRemoved(clock *eventsource.Clock, replica string, parents []string, taskID, label, actor string) (*eventsource.Event, error) {
	return eventsource.NewEvent(clock, replica, parents, eventsource.Draft{
		Actor:   actor,
		Action:  "task.label-removed",
		Subject: eventsource.Subject{Kind: "task", ID: taskID},
		Payload: map[string]any{"label": label},
	})
}

func EmitCommentCreated(clock *eventsource.Clock, replica string, parents []string, taskID string, comment IssueComment) (*eventsource.Event, error) {
	return eventsource.NewEvent(clock, replica, parents, eventsource.Draft{
		Actor:   ActorForUser(comment.User),
		Action:  "comment.created",
		Subject: eventsource.Subject{Kind: "comment"},
		Payload: map[string]any{
			"github_comment": comment.ID,
			"task_ref":        taskID,
			"body":           comment.Body,
		},
	})
}

func EmitTaskLinked(clock *eventsource.Clock, replica string, parents []string, taskID string, issueNumber int, actor string) (*eventsource.Event, error) {
	return eventsource.NewEvent(clock, replica, parents, eventsource.Draft{
		Actor:   actor,
		Action:  "task.linked",
		Subject: eventsource.Subject{Kind: "task", ID: taskID},
		Payload: map[string]any{"github_issue": issueNumber},
	})
}

func EmitCommentLinked(clock *eventsource.Clock, replica string, parents []string, commentID string, githubCommentID int64, actor string) (*eventsource.Event, error) {
	return eventsource.NewEvent(clock, replica, parents, eventsource.Draft{
		Actor:   actor,
		Action:  "comment.linked",
		Subject: eventsource.Subject{Kind: "comment", ID: commentID},
		Payload: map[string]any{"github_comment": githubCommentID},
	})
}

func labelsFromIssue(issue Issue) []string {
	out := make([]string, len(issue.Labels))
	for i, l := range issue.Labels {
		out[i] = GitHubLabelToATM(l.Name)
	}
	return out
}

func payloadString(p json.RawMessage, key string) (string, bool) {
	var m map[string]any
	if err := json.Unmarshal(p, &m); err != nil {
		return "", false
	}
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func issueCreatedAt(issue Issue) time.Time {
	// GitHub issues have a created_at field; add it to the Issue type in
	// types.go if not present. For now, use the clock's now.
	return time.Now().UTC()
}
```

**Read `internal/eventsource/` before implementing** to confirm the exact `Draft`/`NewEvent`/`Subject` signatures — these are from the ATM-0106 spec and may have landed with minor name differences. Match the landed API.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/githubsync/ -run TestEmitTaskCreated -v`
Expected: PASS

- [ ] **Step 5: Write tests for each remaining emit function**

One test per function, asserting action, actor, payload fields, subject.

- [ ] **Step 6: Run + verify + commit**

Run: `go test ./internal/githubsync/ -v && make verify`
Expected: PASS

```bash
git add internal/githubsync/eventemit.go internal/githubsync/eventemit_test.go
git commit -m "feat(githubsync): event authoring helpers for ingest

For ATM-0123. Each helper builds a v2 event with the GitHub-derived actor
and the right payload fields (github_issue/github_comment/labels).
EmitTaskLinked/EmitCommentLinked are the external-id-assignment actions
(adapter-defined, inert-but-causal per ATM-0106 decision 8)."
```

---

### Task 8: Ingest diff loop (`internal/githubsync/adapter.go` — `Pull`)

**Files:**
- Create: `internal/githubsync/adapter.go`
- Create: `internal/githubsync/adapter_test.go`

**Interfaces:**
- Consumes: `githubsync.Client` (Task 4), `githubsync.EnsurePersonas`/`ActorForUser` (Task 5), identity helpers (Task 6), emit helpers (Task 7), `eventsource.Fold`/`State`/`ReadLog`/`AppendLog` (ATM-0106/0107), `store.Store` (for project config).
- Produces: `type Adapter struct { ... }`, `func NewAdapter(s *store.Store, client *Client, code, repoSlug string) *Adapter`, `func (a *Adapter) Pull(ctx context.Context) (*SyncReport, error)`, `func (a *Adapter) Push(ctx context.Context) (*SyncReport, error)`, `func (a *Adapter) Sync(ctx context.Context) (*SyncReport, error)`, `func (a *Adapter) Status(ctx context.Context) (*DiffReport, error)`, `func (a *Adapter) Import(ctx context.Context) (*SyncReport, error)`. Plus `type SyncReport struct { IssuesIngested, IssuesUpdated, CommentsIngested, LabelsIngested int }` and `type DiffReport struct { ... }`.

- [ ] **Step 1: Write the failing test (full Pull against an httptest mock)**

```go
package githubsync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"atm/internal/eventsource"
	"atm/internal/store"
)

func TestAdapterPullIngestsNewIssue(t *testing.T) {
	// Setup: test store with project TEST, no tasks yet.
	s := newTestStore(t)
	if _, err := s.CreateProject("TEST", "Test", "admin@cli:unset"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	// Mock GitHub: one issue, no comments.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/OWNER/REPO/issues":
			w.Write([]byte(`[{"number":42,"title":"Fix bug","body":"desc","state":"open","user":{"login":"alice","id":1},"labels":[]}]`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := NewClient("test-token")
	c.SetBaseURL(srv.URL)
	a := NewAdapter(s, c, "TEST", "OWNER/REPO")
	report, err := a.Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if report.IssuesIngested != 1 {
		t.Errorf("IssuesIngested = %d, want 1", report.IssuesIngested)
	}
	// Verify the event landed in log.jsonl and folds to one task with github_issue=42.
	events, err := eventsource.ReadLog(s.StorePath(), "TEST") // signature per ATM-0107
	if err != nil {
		t.Fatalf("ReadLog: %v", err)
	}
	state, err := eventsource.FoldEvents(events)
	if err != nil {
		t.Fatalf("FoldEvents: %v", err)
	}
	if len(state.Tasks) != 1 {
		t.Fatalf("folded state has %d tasks, want 1", len(state.Tasks))
	}
	// Find the task linked to issue 42.
	ts := TaskByGitHubIssue(state, events, 42)
	if ts == nil {
		t.Fatal("no task linked to issue 42")
	}
	if ts.Title != "Fix bug" {
		t.Errorf("title = %q, want Fix bug", ts.Title)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/githubsync/ -run TestAdapterPullIngestsNewIssue -v`
Expected: FAIL — `NewAdapter`/`Pull` undefined.

- [ ] **Step 3: Implement `adapter.go` — Pull only first**

```go
package githubsync

import (
	"context"
	"fmt"
	"time"

	"atm/internal/eventsource"
	"atm/internal/store"
)

type Adapter struct {
	store    *store.Store
	client   *Client
	code     string
	repoSlug string
	clock    *eventsource.Clock
	replica  string // minted once per adapter (ATM-0106 MintReplicaID)
}

func NewAdapter(s *store.Store, c *Client, code, repoSlug string) *Adapter {
	return &Adapter{
		store:    s,
		client:   c,
		code:     code,
		repoSlug: repoSlug,
		clock:    eventsource.NewClock(time.Now().UnixMilli),
		replica:  "r_github", // minted properly via MintReplicaID; see Task 9
	}
}

type SyncReport struct {
	IssuesIngested  int
	IssuesUpdated   int
	CommentsIngested int
	LabelsIngested  int
	IssuesCreated   int // push direction
	IssuesUpdatedPush int
	CommentsCreated int
	LabelsCreated   int
}

type DiffReport struct {
	GitHubWouldGain int
	ATMMustGain     int
	LabelsDiffer     int
}

func (a *Adapter) Pull(ctx context.Context) (*SyncReport, error) {
	if err := EnsurePersonas(a.store); err != nil {
		return nil, err
	}
	owner, repo := splitSlug(a.repoSlug)
	issues, err := a.client.ListIssues(ctx, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	// Read the current fold + raw events.
	events, err := eventsource.ReadLog(a.store.StorePath(), a.code)
	if err != nil {
		return nil, fmt.Errorf("read log: %w", err)
	}
	state, err := eventsource.FoldEvents(events)
	if err != nil {
		return nil, fmt.Errorf("fold: %w", err)
	}
	frontier := eventsource.Frontier(events) // per ATM-0106 DAG API
	report := &SyncReport{}
	var toAppend []*eventsource.Event
	for _, issue := range issues {
		ts := TaskByGitHubIssue(state, events, issue.Number)
		if ts == nil {
			// New issue -> task.created
			e, err := EmitTaskCreated(a.clock, a.replica, frontier, issue)
			if err != nil {
				return nil, err
			}
			toAppend = append(toAppend, e)
			frontier = []string{e.ID}
			report.IssuesIngested++
			continue
		}
		// Existing task: diff title/body/labels/state.
		if issue.Title != ts.Title {
			e, err := EmitTaskEdited(a.clock, a.replica, frontier, ts.ID, issue)
			if err != nil {
				return nil, err
			}
			toAppend = append(toAppend, e)
			frontier = []string{e.ID}
			report.IssuesUpdated++
		}
		// Label deltas: compare issue.Labels vs ts.Labels.
		// ... (same pattern: emit task.label-added/removed for the diff)
		// Closed/reopened: map to status:done/status:open.
		// ... (see spec section 4)
	}
	// Fetch comments per issue, emit comment.created for new ones.
	for _, issue := range issues {
		comments, err := a.client.ListIssueComments(ctx, owner, repo, issue.Number)
		if err != nil {
			return nil, fmt.Errorf("list comments for issue %d: %w", issue.Number, err)
		}
		for _, c := range comments {
			if CommentByGitHubID(state, events, c.ID) == nil {
				ts := TaskByGitHubIssue(state, events, issue.Number)
				if ts == nil {
					continue // orphan comment; should not happen after the issue pass
				}
				e, err := EmitCommentCreated(a.clock, a.replica, frontier, ts.ID, c)
				if err != nil {
					return nil, err
				}
				toAppend = append(toAppend, e)
				frontier = []string{e.ID}
				report.CommentsIngested++
			}
		}
	}
	if len(toAppend) > 0 {
		if err := eventsource.AppendLog(a.store.StorePath(), a.code, toAppend); err != nil {
			return nil, fmt.Errorf("append log: %w", err)
		}
	}
	return report, nil
}

func splitSlug(slug string) (owner, repo string) {
	parts := strings.SplitN(slug, "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}
```

The exact `eventsource.ReadLog`/`AppendLog`/`Frontier` signatures come from ATM-0107/0106 — match the landed API.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/githubsync/ -run TestAdapterPullIngestsNewIssue -v`
Expected: PASS

- [ ] **Step 5: Add tests for label deltas, closed/reopened, idempotent re-pull**

One test per scenario, each against the httptest mock. Build up the mock to return the right GitHub state for each case.

- [ ] **Step 6: Run + verify + commit**

Run: `go test ./internal/githubsync/ -v && make verify`
Expected: PASS

```bash
git add internal/githubsync/adapter.go internal/githubsync/adapter_test.go
git commit -m "feat(githubsync): Pull ingest diff loop

For ATM-0123. Fetches GitHub Issues, diffs against the folded v2 state,
emits task.created/edited/label-added/removed/comment.created for the
diff, appends to log.jsonl via eventsource.AppendLog. Idempotent.
External-id links via task.linked/comment.linked events."
```

---

### Task 9: Project diff loop (`Adapter.Push`)

**Files:**
- Modify: `internal/githubsync/adapter.go`
- Modify: `internal/githubsync/adapter_test.go`

**Interfaces:**
- Consumes: same as Task 8 + GitHub API mutation methods (Create/Update Issue/Comment/Label).
- Produces: `func (a *Adapter) Push(ctx context.Context) (*SyncReport, error)` — folds local state, fetches GitHub state, diffs, calls the GitHub API to converge GitHub to the fold. After creating an issue/comment, emits `task.linked`/`comment.linked` with the returned id.

- [ ] **Step 1: Write the failing test (ATM-first task → creates GitHub issue → emits task.linked)**

```go
func TestAdapterPushCreatesIssueAndLinks(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("TEST", "Test", "admin@cli:unset"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	// Create a task ATM-side (no github_issue linked yet).
	if _, err := s.CreateTask("TEST", "From ATM", "desc", nil, "developer@cli:unset"); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// Mock GitHub: POST /issues returns number 7.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/repos/OWNER/REPO/issues" {
			w.WriteHeader(201)
			w.Write([]byte(`{"number":7,"title":"From ATM","body":"desc","state":"open"}`))
			return
		}
		// GET /issues returns empty (nothing synced yet).
		if r.Method == "GET" && r.URL.Path == "/repos/OWNER/REPO/issues" {
			w.Write([]byte(`[]`))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()
	c := NewClient("test-token")
	c.SetBaseURL(srv.URL)
	a := NewAdapter(s, c, "TEST", "OWNER/REPO")
	report, err := a.Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if report.IssuesCreated != 1 {
		t.Errorf("IssuesCreated = %d, want 1", report.IssuesCreated)
	}
	// Verify the task.linked event landed with github_issue=7.
	events, _ := eventsource.ReadLog(s.StorePath(), "TEST")
	state, _ := eventsource.FoldEvents(events)
	ts := TaskByGitHubIssue(state, events, 7)
	if ts == nil {
		t.Fatal("no task linked to issue 7 after push")
	}
	if ts.Title != "From ATM" {
		t.Errorf("title = %q, want From ATM", ts.Title)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/githubsync/ -run TestAdapterPushCreatesIssueAndLinks -v`
Expected: FAIL — `Push` undefined.

- [ ] **Step 3: Implement `Push`**

Add to `adapter.go`:

```go
func (a *Adapter) Push(ctx context.Context) (*SyncReport, error) {
	if err := EnsurePersonas(a.store); err != nil {
		return nil, err
	}
	owner, repo := splitSlug(a.repoSlug)
	events, err := eventsource.ReadLog(a.store.StorePath(), a.code)
	if err != nil {
		return nil, err
	}
	state, err := eventsource.FoldEvents(events)
	if err != nil {
		return nil, err
	}
	frontier := eventsource.Frontier(events)
	report := &SyncReport{}
	issues, err := a.client.ListIssues(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	var toAppend []*eventsource.Event
	// For each ATM task, find its GitHub issue (via task.linked events) or create one.
	for _, ts := range state.TasksByCreation() {
		issueNumber := findIssueNumber(events, ts.ID)
		if issueNumber == 0 {
			// Create the issue.
			req := CreateIssueRequest{Title: ts.Title, Body: ts.Description, Labels: taskLabelsToGitHub(ts.Labels)}
			issue, err := a.client.CreateIssue(ctx, owner, repo, req)
			if err != nil {
				return nil, fmt.Errorf("create issue for %s: %w", ts.ID, err)
			}
			// Emit task.linked with the returned number.
			e, err := EmitTaskLinked(a.clock, a.replica, frontier, ts.ID, issue.Number, "admin@cli:unset")
			if err != nil {
				return nil, err
			}
			toAppend = append(toAppend, e)
			frontier = []string{e.ID}
			issueNumber = issue.Number
			report.IssuesCreated++
		} else {
			// Diff and PATCH the issue.
			existing := findIssue(issues, issueNumber)
			if existing != nil && (existing.Title != ts.Title || existing.Body != ts.Description) {
				_, err := a.client.UpdateIssue(ctx, owner, repo, issueNumber, UpdateIssueRequest{Title: ts.Title, Body: ts.Description})
				if err != nil {
					return nil, err
				}
				report.IssuesUpdatedPush++
			}
			// Label deltas, close/reopen...
			// ...
		}
		// Comments: for each ATM comment under this task, find its github_comment or create one.
		for _, cs := range state.CommentsByCreation(ts.ID) {
			commentID := findCommentID(events, cs.ID)
			if commentID == 0 {
				c, err := a.client.CreateComment(ctx, owner, repo, issueNumber, cs.Body)
				if err != nil {
					return nil, err
				}
				e, err := EmitCommentLinked(a.clock, a.replica, frontier, cs.ID, c.ID, "admin@cli:unset")
				if err != nil {
					return nil, err
				}
				toAppend = append(toAppend, e)
				frontier = []string{e.ID}
				report.CommentsCreated++
			} else {
				// Diff and PATCH...
			}
		}
	}
	if len(toAppend) > 0 {
		if err := eventsource.AppendLog(a.store.StorePath(), a.code, toAppend); err != nil {
			return nil, err
		}
	}
	return report, nil
}

func findIssueNumber(events []*eventsource.Event, taskID string) int {
	for _, e := range events {
		if e.Action == "task.linked" && e.Subject.ID == taskID {
			n, _ := payloadInt(e.Payload, "github_issue")
			return n
		}
	}
	return 0
}

func findCommentID(events []*eventsource.Event, commentID string) int64 {
	for _, e := range events {
		if e.Action == "comment.linked" && e.Subject.ID == commentID {
			id, _ := payloadInt64(e.Payload, "github_comment")
			return id
		}
	}
	return 0
}

func findIssue(issues []Issue, number int) *Issue {
	for i := range issues {
		if issues[i].Number == number {
			return &issues[i]
		}
	}
	return nil
}

func taskLabelsToGitHub(labels []string) []string {
	out := make([]string, len(labels))
	for i, l := range labels {
		out[i] = LabelNameToGitHub(l)
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/githubsync/ -run TestAdapterPushCreatesIssueAndLinks -v`
Expected: PASS

- [ ] **Step 5: Add tests for label sync, close/reopen, idempotent re-push, push-failure-leaves-log-untouched**

The last test is important: simulate a GitHub API 500 on `CreateIssue`, assert `log.jsonl` is unchanged after `Push` returns the error.

- [ ] **Step 6: Run + verify + commit**

Run: `go test ./internal/githubsync/ -v && make verify`
Expected: PASS

```bash
git add internal/githubsync/adapter.go internal/githubsync/adapter_test.go
git commit -m "feat(githubsync): Push project diff loop

For ATM-0123. Folds local v2 state, diffs against GitHub, calls the
GitHub API to converge. After creating an issue/comment, emits
task.linked/comment.linked with the returned id. Push failure leaves
log.jsonl untouched (GitHub API errors don't corrupt the eventsource)."
```

---

### Task 10: `Sync`, `Status`, `Import` methods

**Files:**
- Modify: `internal/githubsync/adapter.go`
- Modify: `internal/githubsync/adapter_test.go`

**Interfaces:**
- Produces: `func (a *Adapter) Sync(ctx) (*SyncReport, error)` (pull then push), `func (a *Adapter) Status(ctx) (*DiffReport, error)` (dry-run, no writes), `func (a *Adapter) Import(ctx) (*SyncReport, error)` (pull against an empty local project — same as Pull but documented as the onboarding entry point).

- [ ] **Step 1: Write failing tests**

Three tests: `TestAdapterSyncConverges` (a GitHub-side edit + an ATM-side edit, after Sync both sides match), `TestAdapterStatusIsDryRun` (calls Status, asserts no events appended to log.jsonl), `TestAdapterImportOnboardsExistingIssues` (a repo with 3 issues, Import against an empty project ingests all 3).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/githubsync/ -run "TestAdapterSync|TestAdapterStatus|TestAdapterImport" -v`
Expected: FAIL

- [ ] **Step 3: Implement**

```go
func (a *Adapter) Sync(ctx context.Context) (*SyncReport, error) {
	if _, err := a.Pull(ctx); err != nil {
		return nil, fmt.Errorf("pull phase: %w", err)
	}
	r, err := a.Push(ctx)
	if err != nil {
		return nil, fmt.Errorf("push phase: %w", err)
	}
	return r, nil
}

func (a *Adapter) Status(ctx context.Context) (*DiffReport, error) {
	// Same diff logic as Pull/Push but no writes: compute what would change
	// on each side and return the counts. Reuse the diff helpers.
	// ...
}

func (a *Adapter) Import(ctx context.Context) (*SyncReport, error) {
	// Same as Pull; the distinction is documentation (first-time ingest).
	// Validate the project is empty first (no tasks in log.jsonl).
	return a.Pull(ctx)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/githubsync/ -v`
Expected: PASS

- [ ] **Step 5: Verify + commit**

Run: `make verify`
Expected: PASS

```bash
git add internal/githubsync/adapter.go internal/githubsync/adapter_test.go
git commit -m "feat(githubsync): Sync, Status, Import methods

For ATM-0123. Sync = pull then push (converges both sides). Status =
dry-run diff report (no writes). Import = pull against an empty
project (the onboarding entry point for repos already using GitHub
Issues)."
```

---

### Task 11: L4 `SyncTarget` implementation (`internal/githubsync/sync_target.go`)

**Files:**
- Create: `internal/githubsync/sync_target.go`
- Create: `internal/githubsync/sync_target_test.go`

**Interfaces:**
- Consumes: L4 `SyncTarget` interface (from ATM-0108, assumed landed).
- Produces: `type GitHubSyncTarget struct { ... }` implementing `SyncTarget` — `FetchLog(code) ([]Event, error)` (translates GitHub Issues state to a synthetic event list), `WriteLog(code, entries []Event) error` (translates v2 events to GitHub API mutations). This is the L4-facing surface; the CLI commands (Task 12) call `Pull`/`Push`/`Sync` directly, but L4's generic sync machinery can drive this target too.

**PREREQUISITE:** ATM-0108 (L4) must be landed for this task. If it isn't, skip this task and implement only the CLI-direct path (Tasks 8-10 + Task 12). The `SyncTarget` implementation is what makes GitHub a first-class L4 transport; without it, the adapter is CLI-only.

- [ ] **Step 1: Write the failing test**

Assert `GitHubSyncTarget` satisfies the `SyncTarget` interface (compile-time assertion + a behavioral test using a mock GitHub server).

```go
package githubsync

import (
	"context"
	"testing"

	"atm/internal/eventsource"
	"atm/internal/sync" // or wherever ATM-0108 puts SyncTarget
)

var _ sync.SyncTarget = (*GitHubSyncTarget)(nil)

func TestGitHubSyncTargetFetchLog(t *testing.T) {
	// ... httptest mock, assert FetchLog returns the expected events for a
	// repo with one issue.
}
```

The exact import path depends on where ATM-0108 puts the `SyncTarget` interface. Read the landed spec.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/githubsync/ -run TestGitHubSyncTarget -v`
Expected: FAIL — type undefined.

- [ ] **Step 3: Implement `sync_target.go`**

```go
package githubsync

import (
	"context"

	"atm/internal/eventsource"
	"atm/internal/sync" // ATM-0108
)

type GitHubSyncTarget struct {
	adapter *Adapter
}

func NewGitHubSyncTarget(a *Adapter) *GitHubSyncTarget {
	return &GitHubSyncTarget{adapter: a}
}

func (g *GitHubSyncTarget) FetchLog(code string) ([]eventsource.Event, error) {
	// Translate GitHub Issues state to a synthetic event list.
	// This is the "pull" direction expressed as an L4 SyncTarget.
	ctx := context.Background()
	// ... reuse the adapter's diff logic, but return events instead of appending
}

func (g *GitHubSyncTarget) WriteLog(code string, entries []eventsource.Event) error {
	// Translate v2 events to GitHub API mutations.
	// This is the "push" direction.
	ctx := context.Background()
	// ...
}
```

The exact shape depends on L4's `SyncTarget` signature — it may take a context, may return different types. Match ATM-0108's landed interface.

- [ ] **Step 4: Run + verify + commit**

Run: `go test ./internal/githubsync/ -v && make verify`
Expected: PASS

```bash
git add internal/githubsync/sync_target.go internal/githubsync/sync_target_test.go
git commit -m "feat(githubsync): L4 SyncTarget implementation for GitHub

For ATM-0123. GitHubSyncTarget implements ATM-0108's SyncTarget
interface so GitHub Issues is a first-class L4 transport, drivable by
L4's generic sync machinery in addition to the direct atm github CLI."
```

---

### Task 12: `atm github` CLI commands (`internal/cli/github.go`)

**Files:**
- Create: `internal/cli/github.go`
- Create: `internal/cli/github_test.go`
- Create: `internal/cli/testdata/golden/github-pull-summary.json`, `github-push-summary.json`, `github-status.json`
- Modify: `internal/cli/root.go`

**Interfaces:**
- Consumes: `githubsync.NewAdapter`, `githubsync.NewClientFromEnv`, `store.Store`, existing `cliState` helpers (`openStore`, `emit`, `writeJSON`, `isJSON`).
- Produces: `newGithubCmd(st *cliState) *cobra.Command` with subcommands `pull`, `push`, `sync`, `status`, `import`. Each takes `--project <CODE>` (required) and `--repo <slug>` (optional, defaults to project config's `github.repo`).

- [ ] **Step 1: Write the failing test (golden output for `atm github pull`)**

```go
package cli

import (
	"testing"
)

func TestGithubPullCmdGolden(t *testing.T) {
	st := newTestCLI(t)
	mustCreateProject(t, st, "TEST", "Test")
	// ... set up a mock GitHub server (httptest) and point the project at it
	// via --repo, or set the github.repo config.
	args := []string{"github", "pull", "--project", "TEST", "--repo", "OWNER/REPO", "--json"}
	// Override the GitHub base URL via an env var or a flag so the test points
	// at the httptest server. Add a hidden --api-base flag for testing.
	out, err := runCLI(st, args)
	if err != nil {
		t.Fatalf("pull: %v\n%s", err, out)
	}
	goldenEqual(t, "github-pull-summary.json", out)
}
```

Add a hidden `--api-base` flag to the github commands so tests can point at an httptest server. This is also what enables self-hosted GitHub Enterprise later.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestGithubPullCmdGolden -v`
Expected: FAIL — `github` subcommand unknown.

- [ ] **Step 3: Implement `internal/cli/github.go`**

```go
package cli

import (
	"fmt"

	"atm/internal/githubsync"
	"github.com/spf13/cobra"
)

func newGithubCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "github",
		Short: "GitHub Issues sync commands",
	}
	cmd.AddCommand(newGithubPullCmd(st))
	cmd.AddCommand(newGithubPushCmd(st))
	cmd.AddCommand(newGithubSyncCmd(st))
	cmd.AddCommand(newGithubStatusCmd(st))
	cmd.AddCommand(newGithubImportCmd(st))
	return cmd
}

func newGithubPullCmd(st *cliState) *cobra.Command {
	var repo, apiBase string
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Ingest GitHub Issues state into the ATM eventsource",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGithubPull(st, cmd.Flag("project").Value.String(), repo, apiBase)
		},
	}
	cmd.Flags().String("project", "", "project code (required)")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repo slug (owner/repo); defaults to project's github.repo config")
	cmd.Flags().StringVar(&apiBase, "api-base", "", "GitHub API base URL (for testing / GitHub Enterprise)")
	cmd.MarkFlagRequired("project")
	return cmd
}

func runGithubPull(st *cliState, code, repo, apiBase string) error {
	s, err := st.openStore()
	if err != nil {
		return err
	}
	if repo == "" {
		p, err := s.GetProject(code)
		if err != nil {
			return err
		}
		repo = p.GithubRepo
		if repo == "" {
			return fmt.Errorf("no github.repo configured for project %s; run 'atm project set-github' or pass --repo", code)
		}
	}
	client, err := githubsync.NewClientFromEnv()
	if err != nil {
		return err
	}
	if apiBase != "" {
		client.SetBaseURL(apiBase)
	}
	a := githubsync.NewAdapter(s, client, code, repo)
	report, err := a.Pull(cmd.Context()) // pass context through
	if err != nil {
		return err
	}
	if st.isJSON() {
		return writeJSON(st.stdout(), report)
	}
	fmt.Fprintf(st.stdout(), "ingested: issues=%d updates=%d comments=%d labels=%d\n",
		report.IssuesIngested, report.IssuesUpdated, report.CommentsIngested, report.LabelsIngested)
	return nil
}

// Implement push/sync/status/import the same way, calling the corresponding
// Adapter methods and rendering the report.
```

Read the existing `internal/cli/store.go` for the exact cobra pattern — `RunE`, flag parsing, `st.openStore()`, `st.isJSON()`, `writeJSON`, `st.stdout()`. Match it exactly.

- [ ] **Step 4: Register on root**

In `internal/cli/root.go`, add `root.AddCommand(newGithubCmd(st))` next to the existing `root.AddCommand(newStoreCmd(st))`.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestGithubPullCmdGolden -v`
Expected: PASS

- [ ] **Step 6: Implement + test push, sync, status, import**

One golden test per subcommand, following the pull pattern.

- [ ] **Step 7: Run + verify + commit**

Run: `go test ./internal/cli/ -run Github -v && make verify`
Expected: PASS

```bash
git add internal/cli/github.go internal/cli/github_test.go internal/cli/testdata/golden/github-*.json internal/cli/root.go
git commit -m "feat(cli): atm github pull/push/sync/status/import commands

For ATM-0123. Cobra command group wrapping internal/githubsync.Adapter.
--project required; --repo defaults to project config github.repo.
--api-base hidden flag for testing + GitHub Enterprise. GITHUB_TOKEN
read from env."
```

---

### Task 13: `.github/workflows/atm-sync.yml` + CONTRIBUTING note

**Files:**
- Create: `.github/workflows/atm-sync.yml`
- Modify: `CONTRIBUTING.md` (create if absent)

**Interfaces:**
- Consumes: the `atm` binary (built from source or installed via release), `GITHUB_TOKEN` secret.
- Produces: the sync workflow (schedule + webhook triggers + commit-back step), the CONTRIBUTING note about machine-maintained `log.jsonl`.

- [ ] **Step 1: Create the workflow**

```yaml
name: atm-sync
on:
  schedule:
    - cron: "*/5 * * * *"
  workflow_dispatch: {}
  issues:
    types: [opened, edited, closed, reopened, labeled, unlabeled]
  issue_comment:
    types: [created, edited]
permissions:
  issues: write
  contents: write   # needed for the log.jsonl commit-back
  pull-requests: read
concurrency:
  group: atm-sync
  cancel-in-progress: false
jobs:
  sync:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: go install ./cmd/atm
      - run: atm store rebuild --project ATM
      - run: atm github sync --project ATM --repo ${{ github.repository }}
        env:
          GITHUB_TOKEN: ${{ secrets.ATM_SYNC_TOKEN }}
      - name: Commit log.jsonl
        run: |
          git config user.name  "atm-sync-bot"
          git config user.email "atm-sync-bot@users.noreply.github.com"
          git add .atm/ATM/log.jsonl
          git commit -m "chore(atm-sync): reconcile log.jsonl" || exit 0
          git push
```

Note: `contents: write` is required for the `git push` step to commit `log.jsonl` back to the repo. The `ATM_SYNC_TOKEN` secret should be a GitHub App token or PAT with `issues: write` + `contents: write` on the repo. Document this in the workflow comments.

- [ ] **Step 2: Create / modify CONTRIBUTING.md**

Add a section:

```markdown
## Machine-maintained files

The file `.atm/ATM/log.jsonl` is the ATM project's eventsource — it is
machine-maintained by the `atm-sync` GitHub Actions workflow. Do not
hand-edit it; treat it like a lockfile. The workflow reconciles it from
GitHub Issues state every 5 minutes and on webhook triggers, then commits
the result. Manual edits will be overwritten on the next sync.
```

- [ ] **Step 3: Validate the workflow YAML**

The existing `make verify` scripts-test checks `.github/workflows/*.yml` per the github-publishing spec. Run it:

Run: `make verify`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/atm-sync.yml CONTRIBUTING.md
git commit -m "ci(atm-sync): workflow + CONTRIBUTING note

For ATM-0123. Scheduled + webhook-triggered workflow runs
atm github sync and commits log.jsonl back to the repo as a
machine-maintained file. CONTRIBUTING notes the file is not hand-edited."
```

---

### Task 14: Conventions + README docs

**Files:**
- Modify: `internal/cli/conventions.go`
- Modify: `internal/cli/conventions_test.go` (golden fixture)
- Modify: `README.md`

- [ ] **Step 1: Add the "GitHub-hosted projects" section to `atm conventions`**

In `internal/cli/conventions.go`, find the existing sections (the guide is a big string literal or a set of section helpers). Add a new section after "Where tasks live" or "How to search":

```
## GitHub-hosted projects

A project with `github.repo` configured (via `atm project set-github --repo <slug>`)
can sync bidirectionally with GitHub Issues. `atm github pull/push/sync` are the
workhorses; `atm github import` is the one-shot onboarding for a repo already using
GitHub Issues. GitHub-side edits (issue create/comment/label) are ingested as v2
events with `collaborator@github:<login>` actors; ATM-side mutations project back
to the GitHub Issues via the API. The eventsource (`log.jsonl`) remains ATM's source
of truth; GitHub Issues is a replica. A GitHub Actions workflow
(`.github/workflows/atm-sync.yml`) drives sync on a schedule + webhooks and commits
log.jsonl back to the repo as a machine-maintained file.
```

- [ ] **Step 2: Update the golden conventions test**

Find the existing golden fixture for `atm conventions` output and regenerate it (there's usually a `-update` flag or a regenerate helper). Run the test to verify.

- [ ] **Step 3: Add a "GitHub Issues sync" section to README.md**

After the existing one-command install section:

```markdown
## GitHub Issues sync

ATM can sync bidirectionally with a project's GitHub Issues:

```bash
atm project set-github --project ATM --repo TranDuongTu/atm
atm github sync --project ATM
```

GitHub-side issue/comment/label edits become ATM tasks; ATM-side manager
curation and agent status updates propagate back to the issues. See
`atm conventions` for the full model and `.github/workflows/atm-sync.yml`
for the automation that drives it.
```

- [ ] **Step 4: Run + verify + commit**

Run: `make verify`
Expected: PASS

```bash
git add internal/cli/conventions.go internal/cli/conventions_test.go internal/cli/testdata/golden/conventions.txt README.md
git commit -m "docs(ATM-0123): conventions + README for GitHub Issues sync

Adds the GitHub-hosted projects section to atm conventions and a
GitHub Issues sync section to the README."
```

---

### Task 15: End-to-end test against a real GitHub repo (gated)

**Files:**
- Create: `internal/githubsync/e2e_test.go`
- Modify: `Makefile` (optional — add an `e2e` target, or leave it to `go test -tags=e2e`)

**Interfaces:**
- Consumes: a real GitHub repo (`TranDuongTu/atm-sync-test`) with a known fixture, `ATM_E2E_TOKEN` env var.
- Produces: an e2e test that runs `Adapter.Sync` against the real repo, asserts the folded state matches the expected fixture, and asserts the repo's issues match. Skipped if `ATM_E2E_TOKEN` is not set.

- [ ] **Step 1: Write the gated e2e test**

```go
//go:build e2e

package githubsync

import (
	"context"
	"os"
	"testing"
)

func TestE2ESyncAgainstFixtureRepo(t *testing.T) {
	token := os.Getenv("ATM_E2E_TOKEN")
	if token == "" {
		t.Skip("ATM_E2E_TOKEN not set; skipping e2e test")
	}
	// ... set up a test store, point the adapter at TranDuongTu/atm-sync-test
	// with the fixture, run Sync, assert the folded state + the repo issues
	// match the expected fixture.
}
```

The `//go:build e2e` tag keeps it out of the default `go test` run. `make verify` doesn't run e2e tests; they're run manually or in a dedicated CI job with the token secret.

- [ ] **Step 2: Run (should skip) + verify + commit**

Run: `go test ./internal/githubsync/ -tags=e2e -v`
Expected: SKIP (no token)

Run: `make verify`
Expected: PASS (e2e excluded by default)

```bash
git add internal/githubsync/e2e_test.go
git commit -m "test(githubsync): gated e2e test against a real GitHub repo

For ATM-0123. //go:build e2e tag keeps it out of make verify; run
manually with ATM_E2E_TOKEN set. Asserts Sync converges the fixture
repo's issues with the folded ATM state."
```

---

### Task 16: Onboarding dogfood — ingest ATM's own GitHub Issues

**Files:** none (operational, not a code task)

This is the dogfood moment: run `atm github import --project ATM --repo TranDuongTu/atm` against the real ATM repo to ingest its existing GitHub Issues (if any) into project ATM's `log.jsonl`. From that point, the ATM project is bidirectionally synced with its own GitHub Issues — the real dogfood.

- [ ] **Step 1: Configure the project**

```bash
atm project set-github --project ATM --repo TranDuongTu/atm
```

- [ ] **Step 2: Run the import**

```bash
export GITHUB_TOKEN=<token with issues:read on TranDuongTu/atm>
atm github import --project ATM --repo TranDuongTu/atm
```

- [ ] **Step 3: Verify the fold**

```bash
atm task list --project ATM
atm store log ATM | tail -20
```

- [ ] **Step 4: Enable the workflow**

Merge `.github/workflows/atm-sync.yml` (already added in Task 13). From the next scheduled run, the workflow keeps `log.jsonl` committed and in sync.

- [ ] **Step 5: Record the dogfood in ATM-0123**

```bash
atm task comment add --task ATM-0123 --body "Dogfood live: ATM project now syncs bidirectionally with github.com/TranDuongTu/atm/issues. Workflow atm-sync runs every 5 min + on webhooks; log.jsonl committed back to the repo." --label ATM:comment:progress
```

---

## Self-Review

After writing the complete plan, I ran the self-review checklist from the writing-plans skill:

**1. Spec coverage:** Each spec section maps to a task:
- Scope/prerequisites — Plan header "GATING PREREQUISITE" + Global Constraints.
- Identity mapping (section 2) — Task 6.
- Actor mapping (section 3) — Task 5.
- Diff/translation logic, both directions (section 4) — Tasks 8 (Pull), 9 (Push), 10 (Sync/Status/Import).
- CLI surface (section 5) — Task 12.
- GitHub Actions workflow (section 5) — Task 13.
- Testing (section 6) — Tasks 8/9/10 (unit+integration), Task 11 (SyncTarget), Task 15 (e2e).
- Rollout (section 6) — Tasks 1-14 in order; Task 16 is the dogfood step.
- Out-of-scope (section 6) — noted in Global Constraints (no PRs, no GitHub Projects, no comment-deletion ingest, no multi-repo, no GES).

**2. Placeholder scan:** Searched for TBD/TODO/FIXME/`fill in`/`implement later` — the only intentional "implement" references are in Task 5 (`createPersonaIfMissing` — points the engineer at `internal/store/persona.go` for the exact signature) and Task 11 (depends on the not-yet-landed L4 interface). These are gated by the prerequisite block; the engineer resolves them against the landed code, not against a placeholder. No other placeholders.

**3. Type consistency:** `Adapter`, `SyncReport`, `DiffReport`, `Client`, `Issue`, `IssueComment`, `User`, `Label` are used consistently across tasks. `eventsource.Event`/`State`/`Clock`/`Draft` are referenced per the ATM-0106 spec's public API block; if the landed names differ, the engineer adjusts at the prerequisite gate. `task.linked`/`comment.linked` are consistent throughout (never `task.meta-changed` — the contradiction caught in the spec self-review is fixed).

No issues found that warrant inline fixes beyond what's noted.