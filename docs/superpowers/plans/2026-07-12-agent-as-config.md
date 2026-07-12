# Agent as Config, Not Flags — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the host agent a stored, switchable default (`agents.json`) so day-to-day launches (`atm dev`, `atm manage`) stop naming the agent, backed by an `atm agents` inspect/switch surface over a fixed catalog with live readiness.

**Architecture:** A new global store file `<ATM_HOME>/agents.json` holds only `selected` + per-entry `args`. A new `internal/agent` package defines the fixed catalog (native `opencode`/`codex`/`claude` + `ollama:<integration>` variants) and computes live readiness from `exec.LookPath` + `developing.PluginStatus`. The CLI resolves the agent per launch (`--agent` flag → `ATM_AGENT` env → `selected`), maps the catalog entry to the existing role-specific `Launcher`, and replaces the per-agent subcommands with `atm dev` / default-using `atm manage`.

**Tech Stack:** Go, cobra CLI, existing `internal/store` (atomic JSON) and `internal/developing` / `internal/manager` launcher packages.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-12-agent-as-config-design.md`.
- Catalog is fixed built-ins only: launchers `opencode`, `codex`, `claude`, `ollama`; ollama variants named `ollama:<integration>` for integrations `opencode`/`codex`/`claude`. No arbitrary/custom agents.
- Actor stamping is UNCHANGED: `<persona>@<launcher>:unset`; `ATM_AGENT` = launcher name (ollama entries stamp `@ollama`). The catalog entry `name` never enters the actor string.
- Ollama has no plugin of its own; its readiness/plugin check uses the integration's plugin.
- argv order: `launcher base` + `entry default args (agents.json)` + `ATM_<AGENT>_ARGS env` + `trailing passthrough`. Keep `ATM_<AGENT>_ARGS` for back-compat.
- Launch resolution order: `--agent <name>` flag → `ATM_AGENT` env → `agents.json:selected` → usage error.
- Breaking change: remove `atm claude`/`codex`/`opencode`/`ollama` and `atm manage <agent>` subcommands. No aliases.
- Errors that are user misuse wrap `ErrUsage` (see existing cli code).
- Actor for CLI-originated writes to `agents.json`: `admin@cli:unset` (matches other cli store writes).
- Run `gofmt`/build+tests before each commit: `go build ./... && go test ./...`.

---

### Task 1: Store `AgentsConfig` (global agents.json)

**Files:**
- Create: `internal/store/agents.go`
- Test: `internal/store/agents_test.go`

**Interfaces:**
- Consumes: existing `ReadJSON`, `WriteFileAtomic`, `RFC3339UTC`, `Now`, `(*Store).validateActor`, `(*Store).Root`.
- Produces:
  - `type AgentsConfig struct { UpdatedAt string; UpdatedBy string; Selected string; Args map[string][]string }`
  - `func (s *Store) GetAgentsConfig() (AgentsConfig, error)` — zero value when file missing.
  - `func (s *Store) SetSelectedAgent(name, actor string) error`
  - `func (s *Store) SetAgentArgs(name string, args []string, actor string) error` — `args == nil` or empty clears the entry.

- [ ] **Step 1: Write the failing test**

```go
package store

import (
	"reflect"
	"testing"
)

func TestAgentsConfigRoundTrip(t *testing.T) {
	s := mustInitStore(t) // existing test helper that Opens+Inits a temp store

	got, err := s.GetAgentsConfig()
	if err != nil {
		t.Fatalf("GetAgentsConfig on empty store: %v", err)
	}
	if got.Selected != "" || len(got.Args) != 0 {
		t.Fatalf("expected zero config, got %+v", got)
	}

	if err := s.SetSelectedAgent("ollama:opencode", "admin@cli:unset"); err != nil {
		t.Fatalf("SetSelectedAgent: %v", err)
	}
	if err := s.SetAgentArgs("ollama:opencode", []string{"--yolo"}, "admin@cli:unset"); err != nil {
		t.Fatalf("SetAgentArgs: %v", err)
	}

	got, err = s.GetAgentsConfig()
	if err != nil {
		t.Fatalf("GetAgentsConfig: %v", err)
	}
	if got.Selected != "ollama:opencode" {
		t.Fatalf("Selected = %q", got.Selected)
	}
	if !reflect.DeepEqual(got.Args["ollama:opencode"], []string{"--yolo"}) {
		t.Fatalf("Args = %+v", got.Args)
	}
	if got.UpdatedBy != "admin@cli:unset" {
		t.Fatalf("UpdatedBy = %q", got.UpdatedBy)
	}

	// clearing args removes the entry
	if err := s.SetAgentArgs("ollama:opencode", nil, "admin@cli:unset"); err != nil {
		t.Fatalf("clear SetAgentArgs: %v", err)
	}
	got, _ = s.GetAgentsConfig()
	if _, ok := got.Args["ollama:opencode"]; ok {
		t.Fatalf("expected cleared args, got %+v", got.Args)
	}
	if got.Selected != "ollama:opencode" {
		t.Fatalf("clearing args must not touch Selected; got %q", got.Selected)
	}
}

func TestSetSelectedAgentRejectsBadActor(t *testing.T) {
	s := mustInitStore(t)
	if err := s.SetSelectedAgent("opencode", "not-an-actor"); err == nil {
		t.Fatal("expected actor validation error")
	}
}
```

If `mustInitStore` does not exist in the store test package, add this helper at the top of `agents_test.go`:

```go
func mustInitStore(t *testing.T) *Store {
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestAgentsConfig -v`
Expected: FAIL — `GetAgentsConfig`/`SetSelectedAgent` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package store

import "os"

type AgentsConfig struct {
	UpdatedAt string              `json:"updated_at,omitempty"`
	UpdatedBy string              `json:"updated_by,omitempty"`
	Selected  string              `json:"selected,omitempty"`
	Args      map[string][]string `json:"args,omitempty"`
}

func (s *Store) agentsConfigPath() string {
	return filepath.Join(s.Root, "agents.json")
}

func (s *Store) GetAgentsConfig() (AgentsConfig, error) {
	var c AgentsConfig
	if err := ReadJSON(s.agentsConfigPath(), &c); err != nil {
		if os.IsNotExist(err) {
			return AgentsConfig{}, nil
		}
		return AgentsConfig{}, err
	}
	return c, nil
}

func (s *Store) writeAgentsConfig(mutate func(*AgentsConfig), actor string) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	c, err := s.GetAgentsConfig()
	if err != nil {
		return err
	}
	mutate(&c)
	c.UpdatedAt = RFC3339UTC(Now())
	c.UpdatedBy = actor
	return WriteFileAtomic(s.agentsConfigPath(), &c)
}

func (s *Store) SetSelectedAgent(name, actor string) error {
	return s.writeAgentsConfig(func(c *AgentsConfig) {
		c.Selected = name
	}, actor)
}

func (s *Store) SetAgentArgs(name string, args []string, actor string) error {
	return s.writeAgentsConfig(func(c *AgentsConfig) {
		if len(args) == 0 {
			delete(c.Args, name)
			return
		}
		if c.Args == nil {
			c.Args = map[string][]string{}
		}
		c.Args[name] = args
	}, actor)
}
```

Add `"path/filepath"` to the import block (alongside `"os"`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestAgentsConfig -v && go test ./internal/store/ -run TestSetSelectedAgentRejectsBadActor -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/agents.go internal/store/agents_test.go
git commit -m "feat: store global agents.json config (selected + args)"
```

---

### Task 2: Agent catalog (`internal/agent`)

**Files:**
- Create: `internal/agent/catalog.go`
- Test: `internal/agent/catalog_test.go`

**Interfaces:**
- Produces:
  - `type Entry struct { Name string; Launcher string; Integration string }`
  - `func Catalog() []Entry` — the 6 fixed entries, stable order.
  - `func Lookup(name string) (Entry, bool)`
  - `func (e Entry) PluginAgent() string` — integration for ollama, else launcher.
  - `func (e Entry) Base() []string` — display/launch base argv: `[]string{launcher}` native, `[]string{"ollama","launch",integration,"--"}` for ollama.

- [ ] **Step 1: Write the failing test**

```go
package agent

import (
	"reflect"
	"testing"
)

func TestCatalogEntries(t *testing.T) {
	names := map[string]Entry{}
	for _, e := range Catalog() {
		names[e.Name] = e
	}
	for _, want := range []string{"opencode", "codex", "claude", "ollama:opencode", "ollama:codex", "ollama:claude"} {
		if _, ok := names[want]; !ok {
			t.Fatalf("catalog missing %q", want)
		}
	}
	if len(Catalog()) != 6 {
		t.Fatalf("expected 6 catalog entries, got %d", len(Catalog()))
	}
}

func TestLookupAndDerivations(t *testing.T) {
	e, ok := Lookup("ollama:opencode")
	if !ok {
		t.Fatal("expected ollama:opencode in catalog")
	}
	if e.Launcher != "ollama" || e.Integration != "opencode" {
		t.Fatalf("bad entry: %+v", e)
	}
	if e.PluginAgent() != "opencode" {
		t.Fatalf("PluginAgent = %q", e.PluginAgent())
	}
	if got := e.Base(); !reflect.DeepEqual(got, []string{"ollama", "launch", "opencode", "--"}) {
		t.Fatalf("Base = %v", got)
	}

	n, _ := Lookup("codex")
	if n.PluginAgent() != "codex" {
		t.Fatalf("native PluginAgent = %q", n.PluginAgent())
	}
	if got := n.Base(); !reflect.DeepEqual(got, []string{"codex"}) {
		t.Fatalf("native Base = %v", got)
	}

	if _, ok := Lookup("gemini"); ok {
		t.Fatal("gemini should not be in catalog")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -v`
Expected: FAIL — package/`Catalog` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package agent

type Entry struct {
	Name        string
	Launcher    string // opencode | codex | claude | ollama
	Integration string // set iff Launcher == "ollama"
}

var integrations = []string{"opencode", "codex", "claude"}

func Catalog() []Entry {
	out := make([]Entry, 0, len(integrations)*2)
	for _, name := range integrations {
		out = append(out, Entry{Name: name, Launcher: name})
	}
	for _, integ := range integrations {
		out = append(out, Entry{Name: "ollama:" + integ, Launcher: "ollama", Integration: integ})
	}
	return out
}

func Lookup(name string) (Entry, bool) {
	for _, e := range Catalog() {
		if e.Name == name {
			return e, true
		}
	}
	return Entry{}, false
}

func (e Entry) PluginAgent() string {
	if e.Launcher == "ollama" {
		return e.Integration
	}
	return e.Launcher
}

func (e Entry) Base() []string {
	if e.Launcher == "ollama" {
		return []string{"ollama", "launch", e.Integration, "--"}
	}
	return []string{e.Launcher}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/catalog.go internal/agent/catalog_test.go
git commit -m "feat: fixed agent catalog (native + ollama variants)"
```

---

### Task 3: Agent readiness

**Files:**
- Create: `internal/agent/readiness.go`
- Test: `internal/agent/readiness_test.go`

**Interfaces:**
- Consumes: `Entry` (Task 2), `developing.PluginStatus(agent, home string) developing.Status`.
- Produces:
  - `type Readiness struct { MissingBin bool; MissingPlugin bool }`
  - `func (r Readiness) Ready() bool`
  - `func (r Readiness) String() string` — `"ready"` | `"needs binary"` | `"needs plugin (atm init)"` | `"needs binary + plugin"`. For ollama entries the binary label reads `"needs ollama binary"`.
  - `func Status(e Entry, home string, lookPath func(string) (string, error)) Readiness`

- [ ] **Step 1: Write the failing test**

```go
package agent

import "testing"

func fakeLookPath(present map[string]bool) func(string) (string, error) {
	return func(bin string) (string, error) {
		if present[bin] {
			return "/usr/bin/" + bin, nil
		}
		return "", errNotFound
	}
}

func TestReadinessStates(t *testing.T) {
	// A native entry whose binary is missing but plugin is present would be
	// "needs binary". We isolate the binary axis by pointing at a home with no
	// plugins so the plugin axis is deterministic across machines: assert only
	// via the injected lookPath + a temp home (no plugin installed anywhere).
	home := t.TempDir()
	e, _ := Lookup("opencode")

	r := Status(e, home, fakeLookPath(map[string]bool{"opencode": true}))
	if !r.MissingPlugin {
		t.Fatal("expected MissingPlugin with empty home")
	}
	if r.MissingBin {
		t.Fatal("binary present; MissingBin should be false")
	}
	if r.String() != "needs plugin (atm init)" {
		t.Fatalf("String = %q", r.String())
	}

	r = Status(e, home, fakeLookPath(map[string]bool{}))
	if !r.MissingBin || !r.MissingPlugin {
		t.Fatalf("expected both missing, got %+v", r)
	}
	if r.String() != "needs binary + plugin" {
		t.Fatalf("String = %q", r.String())
	}

	oll, _ := Lookup("ollama:codex")
	r = Status(oll, home, fakeLookPath(map[string]bool{}))
	if r.String() != "needs ollama binary + plugin" {
		t.Fatalf("ollama String = %q", r.String())
	}
}
```

Add a shared sentinel to `readiness_test.go`:

```go
import "errors"

var errNotFound = errors.New("not found")
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestReadiness -v`
Expected: FAIL — `Status`/`Readiness` undefined.

- [ ] **Step 3: Write minimal implementation**

The `String()` label depends on the entry's launcher (ollama is named
explicitly, since its integration plugin may be present while `ollama` itself is
missing), so `Status` stamps the launcher into an unexported field:

```go
package agent

import "atm/internal/developing"

type Readiness struct {
	launcher      string
	MissingBin    bool
	MissingPlugin bool
}

func (r Readiness) Ready() bool { return !r.MissingBin && !r.MissingPlugin }

func Status(e Entry, home string, lookPath func(string) (string, error)) Readiness {
	r := Readiness{launcher: e.Launcher}
	if _, err := lookPath(e.Launcher); err != nil {
		r.MissingBin = true
	}
	if developing.PluginStatus(e.PluginAgent(), home).State != "installed" {
		r.MissingPlugin = true
	}
	return r
}

func (r Readiness) String() string {
	bin := "needs binary"
	if r.launcher == "ollama" {
		bin = "needs ollama binary"
	}
	switch {
	case r.Ready():
		return "ready"
	case r.MissingBin && r.MissingPlugin:
		return bin + " + plugin"
	case r.MissingBin:
		return bin
	default:
		return "needs plugin (atm init)"
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestReadiness -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/readiness.go internal/agent/readiness_test.go
git commit -m "feat: compute live agent readiness (binary + plugin)"
```

---

### Task 4: CLI agent resolution + role launcher mapping

**Files:**
- Create: `internal/cli/agent_resolve.go`
- Test: `internal/cli/agent_resolve_test.go`

**Interfaces:**
- Consumes: `agent.Entry`, `agent.Lookup`, `store.AgentsConfig`, `developing.LauncherFor`, `developing.OllamaLauncher`, `manager.LauncherFor`, `manager.OllamaLauncher`, `ErrUsage`.
- Produces:
  - `func resolveAgentName(flagAgent string, cfg store.AgentsConfig) (string, error)` — order: flag → `ATM_AGENT` env → `cfg.Selected` → `ErrUsage`.
  - `func resolveEntry(flagAgent string, cfg store.AgentsConfig) (agent.Entry, []string, error)` — resolves name, validates against catalog, returns entry + `cfg.Args[name]`.
  - `func devLauncherFor(e agent.Entry) (developing.Launcher, bool)`
  - `func manageLauncherFor(e agent.Entry) (manager.Launcher, bool)`

- [ ] **Step 1: Write the failing test**

```go
package cli

import (
	"testing"

	"atm/internal/store"
)

func TestResolveAgentNameOrder(t *testing.T) {
	t.Setenv("ATM_AGENT", "")
	cfg := store.AgentsConfig{Selected: "codex"}

	name, err := resolveAgentName("opencode", cfg)
	if err != nil || name != "opencode" {
		t.Fatalf("flag should win: %q %v", name, err)
	}

	t.Setenv("ATM_AGENT", "claude")
	name, err = resolveAgentName("", cfg)
	if err != nil || name != "claude" {
		t.Fatalf("env should win over selected: %q %v", name, err)
	}

	t.Setenv("ATM_AGENT", "")
	name, err = resolveAgentName("", cfg)
	if err != nil || name != "codex" {
		t.Fatalf("selected should be used: %q %v", name, err)
	}

	name, err = resolveAgentName("", store.AgentsConfig{})
	if err == nil {
		t.Fatalf("expected usage error, got %q", name)
	}
}

func TestResolveEntryValidatesCatalog(t *testing.T) {
	t.Setenv("ATM_AGENT", "")
	cfg := store.AgentsConfig{Selected: "ollama:opencode", Args: map[string][]string{"ollama:opencode": {"--yolo"}}}
	e, args, err := resolveEntry("", cfg)
	if err != nil {
		t.Fatalf("resolveEntry: %v", err)
	}
	if e.Launcher != "ollama" || e.Integration != "opencode" {
		t.Fatalf("entry = %+v", e)
	}
	if len(args) != 1 || args[0] != "--yolo" {
		t.Fatalf("args = %v", args)
	}

	if _, _, err := resolveEntry("gemini", cfg); err == nil {
		t.Fatal("expected error for unknown agent name")
	}
}

func TestDevAndManageLauncherFor(t *testing.T) {
	e, _, err := resolveEntry("ollama:codex", store.AgentsConfig{Selected: "ollama:codex"})
	if err != nil {
		t.Fatal(err)
	}
	if dl, ok := devLauncherFor(e); !ok || dl.Name() != "ollama" {
		t.Fatalf("dev launcher ok=%v name=%v", ok, dl)
	}
	if ml, ok := manageLauncherFor(e); !ok || ml.Name() != "ollama" {
		t.Fatalf("manage launcher ok=%v name=%v", ok, ml)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestResolve|TestDevAndManage' -v`
Expected: FAIL — resolve functions undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package cli

import (
	"fmt"
	"os"

	"atm/internal/agent"
	"atm/internal/developing"
	"atm/internal/manager"
	"atm/internal/store"
)

func resolveAgentName(flagAgent string, cfg store.AgentsConfig) (string, error) {
	if flagAgent != "" {
		return flagAgent, nil
	}
	if v := os.Getenv("ATM_AGENT"); v != "" {
		return v, nil
	}
	if cfg.Selected != "" {
		return cfg.Selected, nil
	}
	return "", fmt.Errorf("%w: no agent selected; run `atm agents select <name>` or `atm init`", ErrUsage)
}

func resolveEntry(flagAgent string, cfg store.AgentsConfig) (agent.Entry, []string, error) {
	name, err := resolveAgentName(flagAgent, cfg)
	if err != nil {
		return agent.Entry{}, nil, err
	}
	e, ok := agent.Lookup(name)
	if !ok {
		return agent.Entry{}, nil, fmt.Errorf("%w: unknown agent %q (see `atm agents list`)", ErrUsage, name)
	}
	return e, cfg.Args[name], nil
}

func devLauncherFor(e agent.Entry) (developing.Launcher, bool) {
	if e.Launcher == "ollama" {
		return developing.OllamaLauncher{Integration: e.Integration}, true
	}
	return developing.LauncherFor(e.Launcher)
}

func manageLauncherFor(e agent.Entry) (manager.Launcher, bool) {
	if e.Launcher == "ollama" {
		return manager.OllamaLauncher{Integration: e.Integration}, true
	}
	return manager.LauncherFor(e.Launcher)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run 'TestResolve|TestDevAndManage' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/agent_resolve.go internal/cli/agent_resolve_test.go
git commit -m "feat: resolve launch agent from flag/env/selected config"
```

---

### Task 5: `atm agents` command group

**Files:**
- Create: `internal/cli/agents.go`
- Modify: `internal/cli/root.go:96-115` (register `newAgentsCmd(st)`)
- Test: `internal/cli/agents_test.go`

**Interfaces:**
- Consumes: `agent.Catalog`, `agent.Lookup`, `agent.Status`, `store.GetAgentsConfig`/`SetSelectedAgent`/`SetAgentArgs`, `cliState.openStore`, `cliState.emit`/`stdout`/`stderr`, `ErrUsage`, `exec.LookPath`, `os.UserHomeDir`.
- Produces: `func newAgentsCmd(st *cliState) *cobra.Command` with subcommands `list`, `select`, `args`.

- [ ] **Step 1: Write the failing test**

Use the existing `goldenHarness` helper `h.run(args...) (stdout, stderr string, code int)` (JSON mode; wires `--store` internally; `code == ExitSuccess` on success). Do NOT invent a new runner.

```go
package cli

import (
	"strings"
	"testing"
)

func TestAgentsSelectThenList(t *testing.T) {
	h := newGoldenHarness(t)

	// select an entry
	if _, _, code := h.run("agents", "select", "opencode"); code != ExitSuccess {
		t.Fatalf("agents select exit=%d", code)
	}
	cfg, err := h.store.GetAgentsConfig()
	if err != nil || cfg.Selected != "opencode" {
		t.Fatalf("selected not persisted: %q %v", cfg.Selected, err)
	}

	// unknown name errors
	if _, _, code := h.run("agents", "select", "gemini"); code == ExitSuccess {
		t.Fatal("expected non-zero exit selecting unknown agent")
	}

	// list mentions the selected entry
	stdout, _, code := h.run("agents", "list")
	if code != ExitSuccess {
		t.Fatalf("agents list exit=%d", code)
	}
	if !strings.Contains(stdout, "opencode") {
		t.Fatalf("list output missing opencode: %s", stdout)
	}
}

func TestAgentsArgsGetSet(t *testing.T) {
	h := newGoldenHarness(t)
	if _, _, code := h.run("agents", "args", "codex", "--", "--foo", "--bar"); code != ExitSuccess {
		t.Fatalf("set args exit=%d", code)
	}
	cfg, _ := h.store.GetAgentsConfig()
	if got := cfg.Args["codex"]; len(got) != 2 || got[0] != "--foo" || got[1] != "--bar" {
		t.Fatalf("args not stored: %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestAgents -v`
Expected: FAIL — `agents` command unknown.

- [ ] **Step 3: Write minimal implementation**

```go
package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"atm/internal/agent"

	"github.com/spf13/cobra"
)

func newAgentsCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Inspect and switch the host agent used by atm dev / atm manage",
	}
	cmd.AddCommand(newAgentsListCmd(st))
	cmd.AddCommand(newAgentsSelectCmd(st))
	cmd.AddCommand(newAgentsArgsCmd(st))
	return cmd
}

type agentRow struct {
	Name     string `json:"name"`
	Launch   string `json:"launch"`
	Status   string `json:"status"`
	Args     string `json:"args,omitempty"`
	Selected bool   `json:"selected"`
}

func newAgentsListCmd(st *cliState) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List supported agents with live readiness",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			cfg, err := s.GetAgentsConfig()
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home dir: %w", err)
			}
			rows := make([]agentRow, 0, len(agent.Catalog()))
			for _, e := range agent.Catalog() {
				rows = append(rows, agentRow{
					Name:     e.Name,
					Launch:   strings.Join(e.Base(), " "),
					Status:   agent.Status(e, home, exec.LookPath).String(),
					Args:     strings.Join(cfg.Args[e.Name], " "),
					Selected: cfg.Selected == e.Name,
				})
			}
			return st.emit(st.stdout(), map[string]any{"agents": rows}, func() {
				for _, r := range rows {
					marker := ""
					if r.Selected {
						marker = "  *selected"
					}
					fmt.Fprintf(st.stdout(), "%-16s  %-26s  %-24s  %s%s\n",
						r.Name, r.Launch, r.Status, r.Args, marker)
				}
			})
		},
	}
}

func newAgentsSelectCmd(st *cliState) *cobra.Command {
	return &cobra.Command{
		Use:   "select <name>",
		Short: "Set the default agent for atm dev / atm manage",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			e, ok := agent.Lookup(name)
			if !ok {
				return fmt.Errorf("%w: unknown agent %q (see `atm agents list`)", ErrUsage, name)
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.SetSelectedAgent(name, "admin@cli:unset"); err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home dir: %w", err)
			}
			r := agent.Status(e, home, exec.LookPath)
			if !r.Ready() {
				fmt.Fprintf(st.stderr(), "warning: %s is not ready (%s)\n", name, r.String())
			}
			return st.emit(st.stdout(), map[string]any{"selected": name}, func() {
				fmt.Fprintf(st.stdout(), "selected %s\n", name)
			})
		},
	}
}

func newAgentsArgsCmd(st *cliState) *cobra.Command {
	return &cobra.Command{
		Use:   "args <name> [-- <args...>]",
		Short: "Get or set an agent's default passthrough args",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if _, ok := agent.Lookup(name); !ok {
				return fmt.Errorf("%w: unknown agent %q (see `atm agents list`)", ErrUsage, name)
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			// Everything after the name is the args to set. With only the name,
			// print the current value.
			if len(args) == 1 {
				cfg, err := s.GetAgentsConfig()
				if err != nil {
					return err
				}
				cur := cfg.Args[name]
				return st.emit(st.stdout(), map[string]any{"name": name, "args": cur}, func() {
					fmt.Fprintln(st.stdout(), strings.Join(cur, " "))
				})
			}
			set := args[1:]
			if err := s.SetAgentArgs(name, set, "admin@cli:unset"); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"name": name, "args": set}, func() {
				fmt.Fprintf(st.stdout(), "set args for %s: %s\n", name, strings.Join(set, " "))
			})
		},
	}
}
```

Register in `root.go` (add after `newActivityCmd`/near the other user commands, before the version command):

```go
root.AddCommand(newAgentsCmd(st))
```

Note on `args`: cobra treats tokens after `--` as args with `ArgsLenAtDash`. `cobra.MinimumNArgs(1)` with a bare `--` yields `args == [name]` (print). `atm agents args codex -- --foo` yields `args == [codex --foo]`. This matches the test. Keep `DisableFlagParsing` off; the `--` terminator is standard cobra behavior.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestAgents -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/agents.go internal/cli/agents_test.go internal/cli/root.go
git commit -m "feat: atm agents list/select/args command group"
```

---

### Task 6: `atm dev` command; remove per-agent developer subcommands

**Files:**
- Modify: `internal/cli/developing.go` (add `newDevCmd`, thread default args; remove `newDeveloperAgentCmd`, `newDeveloperOllamaCmd`)
- Modify: `internal/cli/root.go:108-112` (replace per-agent + ollama registration with `newDevCmd`)
- Test: `internal/cli/developing_test.go` (add dev-command coverage; drop tests asserting removed subcommands)

**Interfaces:**
- Consumes: `resolveEntry` (Task 4), `devLauncherFor` (Task 4), existing `runDeveloping`.
- Produces: `func newDevCmd(st *cliState) *cobra.Command`. Extends `developingOpts` with `DefaultArgs []string` and `Agent string`.

- [ ] **Step 1: Write the failing test**

The launch emits the JSON header (which includes `"agent":<launcher>`) BEFORE
`runChild`, so the test asserts on that header and tolerates the exec failure
when the host binary is absent. If the existing developer-launch tests inject a
fake `runChild` via `cliState`, reuse that seam instead.

```go
package cli

import (
	"strings"
	"testing"
)

func TestDevLaunchesSelectedAgent(t *testing.T) {
	h := newGoldenHarness(t)

	// no selection -> non-zero exit
	if _, _, code := h.run("dev", "--project", "ATM"); code == ExitSuccess {
		t.Fatal("expected non-zero exit with no agent selected")
	}

	// selecting then launching resolves the entry; the header is emitted before
	// any exec attempt, so assert on it regardless of exit code.
	if _, _, code := h.run("agents", "select", "opencode"); code != ExitSuccess {
		t.Fatalf("select exit=%d", code)
	}
	stdout, _, _ := h.run("dev", "--project", "ATM") // may exit non-zero at exec; header still emitted
	if !strings.Contains(stdout, `"agent":"opencode"`) {
		t.Fatalf("dev did not resolve selected agent: %s", stdout)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestDevLaunches -v`
Expected: FAIL — `dev` command unknown.

- [ ] **Step 3: Write minimal implementation**

In `developing.go`, extend the opts struct:

```go
type developingOpts struct {
	Project     string
	Integration string
	Persona     string
	Agent       string
	DefaultArgs []string
	ExtraArgs   []string
}
```

Add the new command and delete `newDeveloperAgentCmd` + `newDeveloperOllamaCmd`:

```go
func newDevCmd(st *cliState) *cobra.Command {
	var opts developingOpts
	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Launch the selected agent with ATM developer context",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			cfg, err := s.GetAgentsConfig()
			if err != nil {
				return err
			}
			e, defArgs, err := resolveEntry(opts.Agent, cfg)
			if err != nil {
				return err
			}
			l, ok := devLauncherFor(e)
			if !ok {
				return fmt.Errorf("%w: unknown developer agent %q", ErrUsage, e.Launcher)
			}
			opts.ExtraArgs = args
			opts.Integration = e.Integration
			opts.DefaultArgs = defArgs
			return runDeveloping(st, l, e.Launcher, e.Integration, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project to use as the work ledger")
	cmd.Flags().StringVar(&opts.Persona, "persona", "", "persona name; injects its prompt and defaults actor to <persona>@<agent>:unset")
	cmd.Flags().StringVar(&opts.Agent, "agent", "", "override the selected agent for this launch (see `atm agents list`)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
```

Thread `DefaultArgs` into `runDeveloping`'s argv assembly. Change the argv line (developing.go ~114) from:

```go
argv := appendAgentArgs(base, envArgs, opts.ExtraArgs)
```

to:

```go
argv := appendAgentArgs(append(base, opts.DefaultArgs...), envArgs, opts.ExtraArgs)
```

In `root.go`, replace the per-agent loop and ollama registration (lines 109-112):

```go
for _, name := range []string{"opencode", "codex", "claude"} {
	root.AddCommand(newDeveloperAgentCmd(st, name))
}
root.AddCommand(newDeveloperOllamaCmd(st))
```

with:

```go
root.AddCommand(newDevCmd(st))
```

Leave `root.AddCommand(newManageContextCmd(st))` (line 114) untouched. Remove any now-unused imports the compiler flags.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestDev -v && go build ./...`
Expected: PASS + clean build. Delete/adjust any existing tests that reference `atm claude`/`atm opencode`/`atm ollama` developer subcommands (they are intentionally removed).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/developing.go internal/cli/root.go internal/cli/developing_test.go
git commit -m "feat: atm dev launches selected agent; drop per-agent dev subcommands"
```

---

### Task 7: Rework `atm manage` to use the selected agent; remove per-agent manage subcommands

**Files:**
- Modify: `internal/cli/manager.go` (make `manage` resolve the agent directly; thread default args; remove `newManageAgentCmd` + `newManageOllamaCmd` agent subcommands)
- Test: `internal/cli/manager_test.go`

**Interfaces:**
- Consumes: `resolveEntry`, `manageLauncherFor` (Task 4), existing `runManager`, `bindManagerActionFlags`, `validateManagerAction`.
- Produces: `atm manage --project <CODE> --<action> [--agent <name>] [--persona <P>] [-- passthrough…]`. Extends `managerOpts` with `Agent string` and `DefaultArgs []string`.

- [ ] **Step 1: Write the failing test**

```go
package cli

import (
	"strings"
	"testing"
)

func TestManageUsesSelectedAgent(t *testing.T) {
	h := newGoldenHarness(t)
	if _, _, code := h.run("agents", "select", "codex"); code != ExitSuccess {
		t.Fatalf("select exit=%d", code)
	}
	stdout, _, _ := h.run("manage", "--project", "ATM", "--planning") // may exit non-zero at exec
	if !strings.Contains(stdout, `"agent":"codex"`) {
		t.Fatalf("manage did not resolve selected agent: %s", stdout)
	}
}

func TestManageRequiresExactlyOneAction(t *testing.T) {
	h := newGoldenHarness(t)
	if _, _, code := h.run("agents", "select", "codex"); code != ExitSuccess {
		t.Fatalf("select exit=%d", code)
	}
	if _, _, code := h.run("manage", "--project", "ATM"); code == ExitSuccess {
		t.Fatal("expected non-zero exit with no action flag")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestManage -v`
Expected: FAIL — `manage` no longer/does not resolve without an agent subcommand.

- [ ] **Step 3: Write minimal implementation**

Extend `managerOpts`:

```go
type managerOpts struct {
	Project     string
	Integration string
	Persona     string
	Agent       string
	DefaultArgs []string
	Planning    bool
	Grooming    bool
	Tracking    bool
	Asking      bool
	Glossary    bool
	Onboarding  bool
	ExtraArgs   []string
}
```

Rewrite `newManageCmd` so `manage` itself launches (no agent subcommand):

```go
func newManageCmd(st *cliState) *cobra.Command {
	var opts managerOpts
	cmd := &cobra.Command{
		Use:   "manage",
		Short: "Launch the selected agent with ATM manager context",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			cfg, err := s.GetAgentsConfig()
			if err != nil {
				return err
			}
			e, defArgs, err := resolveEntry(opts.Agent, cfg)
			if err != nil {
				return err
			}
			l, ok := manageLauncherFor(e)
			if !ok {
				return fmt.Errorf("%w: unknown manager agent %q", ErrUsage, e.Launcher)
			}
			opts.ExtraArgs = args
			opts.Integration = e.Integration
			opts.DefaultArgs = defArgs
			return runManager(st, l, e.Launcher, e.Integration, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Project, "project", "", "ATM project the manager owns")
	cmd.Flags().StringVar(&opts.Persona, "persona", "", "persona name; defaults actor to <persona>@<agent>:unset")
	cmd.Flags().StringVar(&opts.Agent, "agent", "", "override the selected agent for this launch (see `atm agents list`)")
	bindManagerActionFlags(cmd, &opts)
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
```

Delete `newManageAgentCmd` and `newManageOllamaCmd`. Keep `newManageContextCmd` and register it separately if it was previously added under `manage`; if `manage context` was a subcommand, re-add it:

```go
	cmd.AddCommand(newManageContextCmd(st)) // if context was previously a manage subcommand
```

(Check current registration in `root.go` — `newManageContextCmd` is already added at top level via `root.AddCommand(newManageContextCmd(st))`, so leave it; do NOT nest it.)

Thread `DefaultArgs` into `runManager`'s argv (manager.go ~324). Change:

```go
argv := appendAgentArgs(base, envArgs, opts.ExtraArgs)
```

to:

```go
argv := appendAgentArgs(append(base, opts.DefaultArgs...), envArgs, opts.ExtraArgs)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestManage -v && go build ./...`
Expected: PASS + clean build. Delete/adjust existing tests referencing `atm manage claude|codex|opencode|ollama` subcommands.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/manager.go internal/cli/manager_test.go
git commit -m "feat: atm manage uses selected agent; drop per-agent manage subcommands"
```

---

### Task 8: `atm init` writes `selected`

**Files:**
- Modify: `internal/cli/root.go` (`newInitCmd` RunE — after `installInitPlugins`, set `selected`)
- Test: `internal/cli/root_test.go` (or `init_test.go` if that is where init tests live)

**Interfaces:**
- Consumes: `installInitPlugins` result (`[]initInstallResult` with `Agent` field), `store.SetSelectedAgent`, `agent.Lookup`.
- Produces: init sets `agents.json:selected` to the first installed native harness when none is selected yet.

- [ ] **Step 1: Write the failing test**

```go
func TestInitSelectsFirstInstalledAgent(t *testing.T) {
	h := newGoldenHarness(t)
	// Install writes plugin files under HOME; point it at a temp dir so the
	// install (and thus readiness) is real and isolated.
	t.Setenv("HOME", t.TempDir())

	if _, _, code := h.run("init", "--agent", "opencode"); code != ExitSuccess {
		t.Fatalf("init exit=%d", code)
	}
	cfg, err := h.store.GetAgentsConfig()
	if err != nil {
		t.Fatalf("GetAgentsConfig: %v", err)
	}
	if cfg.Selected != "opencode" {
		t.Fatalf("init did not select installed agent: %q", cfg.Selected)
	}
}
```

`h.run` sets `h.st.flags.store` to the harness store before executing, and `init`
resolves it via `store.ResolveStorePath(st.flags.store)`, so init writes to the
same store `h.store` reads.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestInitSelects -v`
Expected: FAIL — selected not set by init.

- [ ] **Step 3: Write minimal implementation**

In `newInitCmd`'s `RunE`, after `installed, err := installInitPlugins(...)` and its error check, before emitting output:

```go
		if len(installed) > 0 {
			if cfg, cErr := s.GetAgentsConfig(); cErr == nil && cfg.Selected == "" {
				for _, res := range installed {
					if _, ok := agent.Lookup(res.Agent); ok {
						if sErr := s.SetSelectedAgent(res.Agent, "admin@cli:unset"); sErr != nil {
							return sErr
						}
						break
					}
				}
			}
		}
```

Add `"atm/internal/agent"` to `root.go` imports if not present. Note `installInitPlugins` returns results possibly containing both `developing` and `manager` roles per agent — dedupe is unnecessary since the loop breaks on the first catalog-native agent (`res.Agent` is `opencode`/`codex`/`claude`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestInitSelects -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat: atm init selects the first installed agent"
```

---

### Task 9: Docs — conventions, README, CHANGELOG

**Files:**
- Modify: `internal/cli/conventions.go` (`day_to_day_development` prose + struct string; `extra agent args` bullet)
- Modify: `README.md`
- Modify: `CHANGELOG.md`
- Test: `internal/cli/conventions_test.go` (update golden/asserted substrings if the suite pins conventions text)

**Interfaces:**
- Consumes: nothing new. Pure documentation/string updates to match the shipped command surface.

- [ ] **Step 1: Update `conventions.go`**

Replace day-to-day guidance so it reads (both the backtick-formatted string near line 56 and the plain `day_to_day_development` map value near line 123):

> For day-to-day development, first pick your host agent once with `atm agents select <name>` (see `atm agents list` for what is installed and ready), then start sessions with `atm dev --project <CODE>`. Override the agent for a single launch with `--agent <name>` or `ATM_AGENT`. To pass per-agent flags, append them after `--` (e.g. `atm dev --project ATM -- --yolo`), or set per-agent defaults with `atm agents args <name> -- <flags>`. Manager work starts with `atm manage --project <CODE> --planning|--grooming|--tracking|--asking|--glossary|--onboarding`.

Update the `extra agent args` bullet (near line 78) to mention `atm agents args` as the persistent default alongside `ATM_<AGENT>_ARGS`.

Update the `atm manage <agent> ...` reference near line 52/121 to `atm manage --project <CODE> --asking`.

- [ ] **Step 2: Update README + CHANGELOG**

In `README.md`, replace the quick-start launch lines (`atm <agent> --project` / `atm manage <agent> ...`) with the `atm agents select` + `atm dev` / `atm manage` flow. Add a short "Choosing an agent" subsection describing `atm agents list|select|args`.

In `CHANGELOG.md`, add an unreleased entry:

```markdown
### Changed
- The host agent is now a stored default. Choose it with `atm agents select <name>`
  (`atm agents list` shows readiness; `atm agents args <name> -- <flags>` sets
  per-agent defaults). Launch with `atm dev --project <CODE>` and
  `atm manage --project <CODE> --<action>`; override per launch with `--agent`
  or `ATM_AGENT`.

### Removed
- Per-agent launch subcommands `atm claude|codex|opencode|ollama` and
  `atm manage <agent>`. Ollama variants are now catalog entries
  (`ollama:<integration>`); the `--integration` flag is gone.
```

- [ ] **Step 3: Update and run conventions test**

If `conventions_test.go` pins substrings/golden output, update the expectations to the new text. Run:

Run: `go test ./internal/cli/ -run TestConventions -v`
Expected: PASS.

- [ ] **Step 4: Full build + test sweep**

Run: `go build ./... && go test ./...`
Expected: all packages PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/conventions.go internal/cli/conventions_test.go README.md CHANGELOG.md
git commit -m "docs: document atm agents / atm dev surface; drop per-agent subcommands"
```

---

## Notes for the implementer

- **Removed subcommands ripple into tests.** Tasks 6–7 delete `atm claude`/`atm opencode`/`atm ollama` and `atm manage <agent>`. Grep the cli test suite for those strings and update/remove affected cases as part of the owning task — don't leave the build red.
- **`manage context` stays top-level.** `newManageContextCmd` is registered at the root today (`atm manage-context` / its own path); do not nest it under the rewritten `manage`. Verify against `root.go` before touching.
- **Golden fixtures.** Some cli tests compare against golden files and support `-update`. If output shape changes (e.g. `atm agents list`), regenerate intentionally with `go test ./internal/cli/ -run TestAgents -update` and eyeball the diff.
- **`LauncherNames()` may not exist.** Task 6 references it defensively; if `root.go` uses a literal `[]string{"opencode","codex","claude"}` loop for dev registration, delete that loop directly instead.
```
