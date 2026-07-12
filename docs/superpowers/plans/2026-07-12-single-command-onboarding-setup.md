# Single-Command Onboarding Setup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make interactive `atm init` handle plugin install, default agent selection, and default passthrough args in one first-run flow.

**Architecture:** Extend the existing `internal/cli/root.go` init flow instead of adding a new command. Keep scripted `--agent` behavior non-interactive, persist selection and args through the existing store `agents.json` API, and update README/conventions so `atm agents` becomes an advanced maintenance surface.

**Tech Stack:** Go 1.25 module, Cobra CLI, existing `internal/store` JSON config, existing `internal/agent` catalog/readiness, standard library only.

## Global Constraints

- No new store entities; continue using `<ATM_HOME>/agents.json`.
- No custom agent registration.
- No plugin install for `ollama`; ollama entries use the integration host's plugin.
- Do not remove `atm agents list|select|args`.
- Preserve non-interactive and JSON `atm init` behavior: no prompts unless text output, TTY stdin, and no explicit `--agent`.
- `--dry-run` must not mutate `agents.json`.
- Full verification remains `make verify`.

---

## File Structure

- `internal/cli/root.go` owns `atm init`; add small helper types/functions near the current init helpers.
- `internal/cli/root_test.go` already tests interactive init; extend it for selected agent, args, ollama variants, dry-run, and non-interactive preservation.
- `internal/cli/init_test.go` keeps scripted/non-interactive init tests; adjust expectations for any new JSON/text output fields.
- `internal/cli/conventions.go` updates first-run and daily-work text.
- `internal/cli/conventions_test.go` and `internal/cli/testdata/golden/conventions-*.json` validate conventions output.
- `README.md` updates the 30-second start and maintenance notes.

---

### Task 1: Init Setup Parsing Helpers

**Files:**
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/root_test.go`

**Interfaces:**
- Consumes: existing `parseInitAgentSelection(input string) ([]string, error)`, `initAgents(selected []string) ([]string, error)`.
- Produces:
  - `type initSetupPromptResult struct { PluginAgents []string; SelectedAgent string; SelectedAgentProvided bool; Args []string; ArgsProvided bool }`
  - `func parseInitDefaultAgentSelection(input string, entries []agent.Entry) (string, bool, error)`
  - `func parseInitArgsLine(input string) ([]string, bool, error)`

- [ ] **Step 1: Write failing tests for default-agent and args parsing**

Add these tests to `internal/cli/root_test.go` near the existing init tests:

```go
func TestParseInitDefaultAgentSelection(t *testing.T) {
	entries := []agent.Entry{
		{Name: "opencode", Launcher: "opencode"},
		{Name: "codex", Launcher: "codex"},
		{Name: "ollama:opencode", Launcher: "ollama", Integration: "opencode"},
	}
	tests := []struct {
		name    string
		input   string
		want    string
		wantOK  bool
		wantErr bool
	}{
		{name: "blank", input: "", wantOK: false},
		{name: "number", input: "2", want: "codex", wantOK: true},
		{name: "name", input: "ollama:opencode", want: "ollama:opencode", wantOK: true},
		{name: "trim", input: "  opencode  ", want: "opencode", wantOK: true},
		{name: "bad number", input: "4", wantErr: true},
		{name: "bad name", input: "gemini", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, err := parseInitDefaultAgentSelection(tt.input, entries)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("got (%q,%v), want (%q,%v)", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestParseInitArgsLine(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantOK  bool
		wantErr bool
	}{
		{name: "blank", input: "   ", wantOK: false},
		{name: "words", input: "--yolo --auto", want: []string{"--yolo", "--auto"}, wantOK: true},
		{name: "double quoted", input: `--profile "work laptop"`, want: []string{"--profile", "work laptop"}, wantOK: true},
		{name: "single quoted", input: `--profile 'work laptop'`, want: []string{"--profile", "work laptop"}, wantOK: true},
		{name: "escaped space", input: `--profile work\ laptop`, want: []string{"--profile", "work laptop"}, wantOK: true},
		{name: "unterminated", input: `"oops`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, err := parseInitArgsLine(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok != tt.wantOK || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got (%v,%v), want (%v,%v)", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
```

Update the `root_test.go` import block to include:

```go
	"reflect"

	"atm/internal/agent"
```

- [ ] **Step 2: Run parsing tests and verify they fail**

Run:

```sh
go test ./internal/cli -run 'TestParseInit(DefaultAgentSelection|ArgsLine)' -v
```

Expected: compile failure because `parseInitDefaultAgentSelection` and `parseInitArgsLine` are undefined.

- [ ] **Step 3: Add helper types and parsers**

In `internal/cli/root.go`, after `promptInitAgents`, add:

```go
type initSetupPromptResult struct {
	PluginAgents          []string
	SelectedAgent         string
	SelectedAgentProvided bool
	Args                  []string
	ArgsProvided          bool
}

func parseInitDefaultAgentSelection(input string, entries []agent.Entry) (string, bool, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", false, nil
	}
	for i, e := range entries {
		if input == fmt.Sprintf("%d", i+1) || input == e.Name {
			return e.Name, true, nil
		}
	}
	return "", false, fmt.Errorf("%w: unknown init default agent selection %q", ErrUsage, input)
}

func parseInitArgsLine(input string) ([]string, bool, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, false, nil
	}
	var out []string
	var cur strings.Builder
	var quote rune
	escaped := false
	emit := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range input {
		if escaped {
			cur.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			cur.WriteRune(r)
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
		case ' ', '\t', '\n':
			emit()
		default:
			cur.WriteRune(r)
		}
	}
	if escaped {
		cur.WriteRune('\\')
	}
	if quote != 0 {
		return nil, false, fmt.Errorf("%w: unterminated quote in init args", ErrUsage)
	}
	emit()
	return out, true, nil
}
```

- [ ] **Step 4: Run parsing tests and verify they pass**

Run:

```sh
go test ./internal/cli -run 'TestParseInit(DefaultAgentSelection|ArgsLine)' -v
```

Expected: PASS.

- [ ] **Step 5: Commit Task 1**

```sh
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "test: add init setup parsing helpers"
```

---

### Task 2: Interactive Init Selection and Args Persistence

**Files:**
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/root_test.go`
- Modify: `internal/cli/init_test.go`

**Interfaces:**
- Consumes: `parseInitDefaultAgentSelection`, `parseInitArgsLine`, `initSetupPromptResult`.
- Produces:
  - `func promptInitSetup(st *cliState, cfg store.AgentsConfig) (initSetupPromptResult, error)`
  - `func viableInitDefaultAgents(installed []initInstallResult, cfg store.AgentsConfig) []agent.Entry`
  - `func persistInitSetup(s *store.Store, setup initSetupPromptResult, dryRun bool) error`

- [ ] **Step 1: Write failing tests for interactive setup persistence**

Add these tests to `internal/cli/root_test.go` after `TestInitInteractiveGuidesAgentSelection`:

```go
func TestInitInteractiveSelectsAgentAndArgs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	installFakeCodexForCLI(t, home)
	h := newGoldenHarness(t)
	h.output = outputText
	h.st.in = strings.NewReader("2\n1\n--yolo --profile \"work laptop\"\n")
	h.st.stdinIsTerminal = func() bool { return true }

	out, _, code := h.run("init")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, h.stderr.String())
	}
	cfg, err := h.store.GetAgentsConfig()
	if err != nil {
		t.Fatalf("GetAgentsConfig: %v", err)
	}
	if cfg.Selected != "codex" {
		t.Fatalf("selected = %q, want codex\noutput:\n%s", cfg.Selected, out)
	}
	if got := cfg.Args["codex"]; !reflect.DeepEqual(got, []string{"--yolo", "--profile", "work laptop"}) {
		t.Fatalf("codex args = %v", got)
	}
	for _, want := range []string{
		"Default agent",
		"Agent args",
		"selected\tcodex",
		"args\tcodex\t--yolo --profile work laptop",
		"Next: atm manage --project <CODE> --onboarding",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("interactive init output missing %q:\n%s", want, out)
		}
	}
}

func TestInitInteractiveCanSelectOllamaIntegration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	h := newGoldenHarness(t)
	h.output = outputText
	h.st.in = strings.NewReader("1\n2\n\n")
	h.st.stdinIsTerminal = func() bool { return true }

	out, _, code := h.run("init")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, h.stderr.String())
	}
	cfg, err := h.store.GetAgentsConfig()
	if err != nil {
		t.Fatalf("GetAgentsConfig: %v", err)
	}
	if cfg.Selected != "ollama:opencode" {
		t.Fatalf("selected = %q, want ollama:opencode\noutput:\n%s", cfg.Selected, out)
	}
	if _, ok := cfg.Args["ollama:opencode"]; ok {
		t.Fatalf("blank args should not store an entry: %v", cfg.Args)
	}
}

func TestInitInteractiveBlankSelectionAndArgsPreserveExistingConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	h := newGoldenHarness(t)
	h.output = outputText
	if err := h.store.SetSelectedAgent("codex", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetAgentArgs("codex", []string{"--existing"}, "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	h.st.in = strings.NewReader("\n\n\n")
	h.st.stdinIsTerminal = func() bool { return true }

	if _, _, code := h.run("init"); code != ExitSuccess {
		t.Fatalf("init exit=%d stderr=%s", code, h.stderr.String())
	}
	cfg, _ := h.store.GetAgentsConfig()
	if cfg.Selected != "codex" {
		t.Fatalf("selected changed: %q", cfg.Selected)
	}
	if got := cfg.Args["codex"]; !reflect.DeepEqual(got, []string{"--existing"}) {
		t.Fatalf("args changed: %v", got)
	}
}

func TestInitInteractiveDryRunDoesNotPersistSelectionOrArgs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	h := newGoldenHarness(t)
	h.output = outputText
	h.st.in = strings.NewReader("1\n1\n--yolo\n")
	h.st.stdinIsTerminal = func() bool { return true }

	out, _, code := h.run("init", "--dry-run")
	if code != ExitSuccess {
		t.Fatalf("exit = %d, want 0; stderr=%s", code, h.stderr.String())
	}
	cfg, _ := h.store.GetAgentsConfig()
	if cfg.Selected != "" || len(cfg.Args) != 0 {
		t.Fatalf("dry-run mutated agents config: %+v", cfg)
	}
	if !strings.Contains(out, "would select\topencode") || !strings.Contains(out, "would set args\topencode\t--yolo") {
		t.Fatalf("dry-run summary missing config preview:\n%s", out)
	}
}
```

- [ ] **Step 2: Run interactive setup tests and verify they fail**

Run:

```sh
go test ./internal/cli -run 'TestInitInteractive(SelectsAgentAndArgs|CanSelectOllamaIntegration|BlankSelectionAndArgsPreserveExistingConfig|DryRunDoesNotPersistSelectionOrArgs)' -v
```

Expected: failures because `atm init` does not yet prompt for default agent or args.

- [ ] **Step 3: Implement setup prompt helpers**

In `internal/cli/root.go`, replace `promptInitAgents` with a wrapper plus the new full prompt:

```go
func promptInitAgents(st *cliState) ([]string, error) {
	res, err := promptInitSetup(st, store.AgentsConfig{})
	return res.PluginAgents, err
}

func promptInitSetup(st *cliState, cfg store.AgentsConfig) (initSetupPromptResult, error) {
	var res initSetupPromptResult
	fmt.Fprintln(st.stdout())
	fmt.Fprintln(st.stdout(), "ATM setup")
	fmt.Fprintln(st.stdout(), "Choose agent integrations to install (multiple allowed):")
	fmt.Fprintln(st.stdout(), "  1) opencode")
	fmt.Fprintln(st.stdout(), "  2) codex")
	fmt.Fprintln(st.stdout(), "  3) claude")
	fmt.Fprint(st.stdout(), "Agents [comma-separated numbers/names, all, or Enter to skip]: ")

	scanner := bufio.NewScanner(st.stdin())
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return res, fmt.Errorf("read init selection: %w", err)
		}
		return res, nil
	}
	plugins, err := parseInitAgentSelection(scanner.Text())
	if err != nil {
		return res, err
	}
	res.PluginAgents = plugins
	previewInstalled, err := previewInitInstallResults(plugins)
	if err != nil {
		return res, err
	}
	entries := viableInitDefaultAgents(previewInstalled, cfg)
	if len(entries) == 0 {
		fmt.Fprintln(st.stdout(), "No default agent candidates yet; install an agent plugin or run `atm agents select <name>` later.")
		return res, nil
	}
	fmt.Fprintln(st.stdout())
	fmt.Fprintln(st.stdout(), "Default agent:")
	for i, e := range entries {
		marker := ""
		if cfg.Selected == e.Name {
			marker = " (current)"
		}
		fmt.Fprintf(st.stdout(), "  %d) %s%s\n", i+1, e.Name, marker)
	}
	fmt.Fprint(st.stdout(), "Default agent [number/name, or Enter to keep current]: ")
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return res, fmt.Errorf("read init default agent: %w", err)
		}
		return res, nil
	}
	selected, ok, err := parseInitDefaultAgentSelection(scanner.Text(), entries)
	if err != nil {
		return res, err
	}
	if !ok {
		selected = cfg.Selected
	} else {
		res.SelectedAgentProvided = true
	}
	res.SelectedAgent = selected
	if selected == "" {
		return res, nil
	}
	fmt.Fprintf(st.stdout(), "Agent args for %s [optional, shell-like quoting; Enter to keep current]: ", selected)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return res, fmt.Errorf("read init agent args: %w", err)
		}
		return res, nil
	}
	args, argsOK, err := parseInitArgsLine(scanner.Text())
	if err != nil {
		return res, err
	}
	res.Args = args
	res.ArgsProvided = argsOK
	return res, nil
}
```

Add the preview helpers near `installInitPlugins`:

```go
func previewInitInstallResults(selected []string) ([]initInstallResult, error) {
	agents, err := initAgents(selected)
	if err != nil {
		return nil, err
	}
	out := make([]initInstallResult, 0, len(agents))
	for _, name := range agents {
		out = append(out, initInstallResult{Agent: name})
	}
	return out, nil
}

func viableInitDefaultAgents(installed []initInstallResult, cfg store.AgentsConfig) []agent.Entry {
	pluginAgents := map[string]bool{}
	for _, res := range installed {
		pluginAgents[res.Agent] = true
	}
	if cfg.Selected != "" {
		if e, ok := agent.Lookup(cfg.Selected); ok {
			pluginAgents[e.PluginAgent()] = true
		}
	}
	var out []agent.Entry
	seen := map[string]bool{}
	for _, e := range agent.Catalog() {
		if !pluginAgents[e.PluginAgent()] || seen[e.Name] {
			continue
		}
		out = append(out, e)
		seen[e.Name] = true
	}
	return out
}
```

- [ ] **Step 4: Wire setup persistence into `newInitCmd`**

In `newInitCmd`, replace the current `selected := agents ... installInitPlugins(selected, dryRun)` block with:

```go
cfg, err := s.GetAgentsConfig()
if err != nil {
	return err
}
setup := initSetupPromptResult{PluginAgents: agents}
interactive := len(agents) == 0 && st.flags.output == outputText && st.isStdinTerminal()
if interactive {
	var err error
	setup, err = promptInitSetup(st, cfg)
	if err != nil {
		return err
	}
}
installed, err := installInitPlugins(setup.PluginAgents, dryRun)
if err != nil {
	return err
}
if !interactive && !dryRun && len(installed) > 0 && cfg.Selected == "" {
	for _, res := range installed {
		if _, ok := agent.Lookup(res.Agent); ok {
			setup.SelectedAgent = res.Agent
			setup.SelectedAgentProvided = true
			break
		}
	}
}
if err := persistInitSetup(s, setup, dryRun); err != nil {
	return err
}
```

Add `persistInitSetup`:

```go
func persistInitSetup(s *store.Store, setup initSetupPromptResult, dryRun bool) error {
	if dryRun {
		return nil
	}
	if setup.SelectedAgentProvided && setup.SelectedAgent != "" {
		if err := s.SetSelectedAgent(setup.SelectedAgent, "admin@cli:unset"); err != nil {
			return err
		}
	}
	if setup.ArgsProvided && setup.SelectedAgent != "" {
		if err := s.SetAgentArgs(setup.SelectedAgent, setup.Args, "admin@cli:unset"); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 5: Emit selected/args summary**

In the JSON branch of `newInitCmd`, add selected/args only when provided:

```go
if setup.SelectedAgent != "" {
	out["selected"] = setup.SelectedAgent
}
if setup.ArgsProvided {
	out["args"] = setup.Args
}
```

In the text branch of `newInitCmd`, after plugin lines, add:

```go
if setup.SelectedAgent != "" {
	label := "selected"
	if dryRun {
		label = "would select"
	}
	fmt.Fprintf(st.stdout(), "%s\t%s\n", label, setup.SelectedAgent)
}
if setup.ArgsProvided && setup.SelectedAgent != "" {
	label := "args"
	if dryRun {
		label = "would set args"
	}
	fmt.Fprintf(st.stdout(), "%s\t%s\t%s\n", label, setup.SelectedAgent, strings.Join(setup.Args, " "))
}
if interactive {
	fmt.Fprintln(st.stdout(), "Next: atm manage --project <CODE> --onboarding")
}
```

- [ ] **Step 6: Run focused init tests**

Run:

```sh
go test ./internal/cli -run 'TestInit(Interactive|SelectsFirstInstalledAgent|NoAgentLeavesSelectionEmpty|DryRunAllReportsPluginsWithoutWriting|NonInteractiveWithoutAgentDoesNotPrompt)' -v
```

Expected: PASS.

- [ ] **Step 7: Commit Task 2**

```sh
git add internal/cli/root.go internal/cli/root_test.go internal/cli/init_test.go
git commit -m "feat: complete interactive init setup"
```

---

### Task 3: README and Conventions Update

**Files:**
- Modify: `README.md`
- Modify: `internal/cli/conventions.go`
- Modify: `internal/cli/testdata/golden/conventions-text.json`
- Modify: `internal/cli/testdata/golden/conventions-json.json`
- Modify: `internal/cli/testdata/golden/determinism-conventions.json`

**Interfaces:**
- Consumes: new setup flow from Task 2.
- Produces: user-facing docs that no longer require `atm agents list/select/args` in the primary onboarding path.

- [ ] **Step 1: Write failing docs/conventions assertions**

In `internal/cli/conventions_test.go`, add:

```go
func TestConventionsFirstRunUsesInitSetup(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	out, _, code := h.run("conventions")
	if code != ExitSuccess {
		t.Fatalf("exit=%d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, "First run setup: `atm init`") {
		t.Fatalf("conventions missing first-run init setup guidance:\n%s", out)
	}
	if strings.Contains(out, "first pick your host agent once with `atm agents select <name>`") {
		t.Fatalf("conventions still makes atm agents select the primary path:\n%s", out)
	}
}
```

- [ ] **Step 2: Run the new conventions assertion and verify it fails**

Run:

```sh
go test ./internal/cli -run TestConventionsFirstRunUsesInitSetup -v
```

Expected: FAIL because conventions still says to pick an agent with `atm agents select`.

- [ ] **Step 3: Update README 30-second start**

In `README.md`, replace the step 2 command block with:

```md
**2. Onboard.** Run the guided setup once, then onboard each project -- one manager onboarding run per project:

```sh
atm init                       # guided setup: store, plugins, default agent, args
atm manage --project ATM --onboarding
```
```

After the existing passthrough args examples, add:

```md
Use `atm agents list`, `atm agents select <name>`, and `atm agents args <name> -- <args...>` later when you want to inspect readiness or change the default agent after setup.
```

- [ ] **Step 4: Update conventions text and structured JSON**

In `internal/cli/conventions.go`, replace the `day_to_day_development` paragraph in `conventionsText` with:

```go
For first run setup: `atm init` initializes the store, installs selected ATM agent plugins, records the default agent, and can save default host-agent args. Then run `atm manage --project <CODE> --onboarding` from a repository to let the manager learn and organize the project. Day-to-day development starts with `atm dev --project <CODE>`. Override the agent for a single launch with `--agent <name>` or `ATM_AGENT`. To pass per-agent flags, append them after `--` (e.g. `atm dev --project ATM -- --yolo`), or set per-agent defaults with `atm agents args <name> -- <flags>`. Manager work starts with `atm manage --project <CODE> --planning|--grooming|--tracking|--asking|--glossary|--onboarding`. Use `atm agents list|select|args` later to inspect readiness or change agent defaults after setup.
```

In `conventionsStructured()`, set the `day_to_day_development` value to the same text without backtick formatting.

- [ ] **Step 5: Regenerate conventions goldens**

Run:

```sh
go test ./internal/cli -run 'TestConventions(Text|JSON)$|TestDeterminismConventions' -update
```

Expected: PASS and updated golden fixture files.

- [ ] **Step 6: Run docs/conventions tests**

Run:

```sh
go test ./internal/cli -run 'TestConventions|TestDeterminismConventions' -v
```

Expected: PASS.

- [ ] **Step 7: Commit Task 3**

```sh
git add README.md internal/cli/conventions.go internal/cli/conventions_test.go internal/cli/testdata/golden/conventions-text.json internal/cli/testdata/golden/conventions-json.json internal/cli/testdata/golden/determinism-conventions.json
git commit -m "docs: simplify onboarding setup path"
```

---

### Task 4: Final Verification and Ledger Update

**Files:**
- No code files expected.

**Interfaces:**
- Consumes: all previous tasks.
- Produces: verified branch state and ATM progress note.

- [ ] **Step 1: Run full verification**

Run:

```sh
make verify
```

Expected: PASS.

- [ ] **Step 2: Inspect final diff/status**

Run:

```sh
git status --short
git log --oneline -5
```

Expected: clean worktree except intentional uncommitted files if verification generated none; recent commits include the three task commits.

- [ ] **Step 3: Record ATM progress**

Dispatch the ATM manager subagent to add a progress comment on `ATM-0101` with actor `manager@codex:unset`, summarizing implementation commits and `make verify` result.

---

## Self-Review

- Spec coverage: Task 2 covers interactive setup, persistence, dry-run, JSON/text summary, and non-interactive preservation. Task 3 covers README and conventions. Task 4 covers full verification and ledger update.
- Placeholder scan: no TBD/TODO/fill-in-later steps remain; every code-changing step includes concrete code.
- Type consistency: `initSetupPromptResult`, `parseInitDefaultAgentSelection`, `parseInitArgsLine`, `promptInitSetup`, `viableInitDefaultAgents`, and `persistInitSetup` are named consistently across tasks.
