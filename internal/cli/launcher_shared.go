package cli

import (
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// newRunID builds a run id of the form <CODE>-<YYYYMMDDHHMMSS>-<6-hex>.
// Shared by the developing and manager launchers.
func newRunID(code string) string {
	return fmt.Sprintf("%s-%s-%s",
		code,
		time.Now().UTC().Format("20060102150405"),
		shortUUID(),
	)
}

// shortUUID returns a 6-char hex suffix for collision safety in run IDs.
func shortUUID() string {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "000000"
	}
	return fmt.Sprintf("%x", b[:])
}

// runChild executes the host agent with inherited stdio and the given env.
// Returns the exit code and error (if any). Shared by developing and manager.
// notFoundHint is included in the error message when the binary is not on PATH.
func runChild(name string, argv []string, env []string, notFoundHint string) (int, error) {
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		hint := notFoundHint
		if hint == "" {
			hint = "install: see host docs"
		}
		return 0, fmt.Errorf("%s not found on PATH; install: %s", name, hint)
	}
	cmd := exec.Command(bin, argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = env
	err = cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), err
		}
		return 1, err
	}
	return 0, nil
}

// assembleEnv returns os.Environ() plus the extras, extras winning on conflict.
func assembleEnv(extras map[string]string) []string {
	env := os.Environ()
	for k, v := range extras {
		env = append(env, k+"="+v)
	}
	return env
}

// emitLaunchHeader writes the pre-launch summary in JSON or text form.
// role is "developing" or "manager".
func emitLaunchHeader(st *cliState, role, project, runID, contextPath, agent string, argv []string, env map[string]string) error {
	return st.emit(st.stdout(), map[string]any{
		"role":         role,
		"run_id":       runID,
		"project":      project,
		"agent":        agent,
		"context_path": contextPath,
		"argv":         argv,
		"env":          env,
	}, func() {
		fmt.Fprintf(st.stdout(), "%s %s  run=%s  agent=%s\n", role, project, runID, agent)
		fmt.Fprintf(st.stdout(), "  context:  %s\n", contextPath)
		fmt.Fprintf(st.stdout(), "  launching: %s\n", strings.Join(argv, " "))
	})
}

// emitLaunchTail writes the post-launch summary in JSON or text form.
func emitLaunchTail(st *cliState, role, project, runID, contextPath, agent string, agentExit int) error {
	return st.emit(st.stdout(), map[string]any{
		"role":         role,
		"run_id":       runID,
		"project":      project,
		"agent":        agent,
		"context_path": contextPath,
		"agent_exit":   agentExit,
	}, func() {
		fmt.Fprintf(st.stdout(), "%s %s  run=%s\n", role, project, runID)
		fmt.Fprintf(st.stdout(), "  context: %s\n", contextPath)
		fmt.Fprintf(st.stdout(), "%s exited %d\n", agent, agentExit)
	})
}

// agentEnvArgs returns env-derived extra args for a host agent.
// For ollama hosts with an integration set, ATM_<INTEGRATION>_ARGS wins
// over the generic ATM_OLLAMA_ARGS. Parsed with strings.Fields (no quoting).
func agentEnvArgs(agent, integration string) []string {
	if agent == "ollama" && integration != "" {
		if v := os.Getenv("ATM_" + strings.ToUpper(integration) + "_ARGS"); v != "" {
			return strings.Fields(v)
		}
	}
	if v := os.Getenv("ATM_" + strings.ToUpper(agent) + "_ARGS"); v != "" {
		return strings.Fields(v)
	}
	return nil
}

// appendAgentArgs returns base + envArgs + extraArgs with no dedup.
// The host agent's flag parser resolves any conflicts.
func appendAgentArgs(base, envArgs, extraArgs []string) []string {
	out := make([]string, 0, len(base)+len(envArgs)+len(extraArgs))
	out = append(out, base...)
	out = append(out, envArgs...)
	out = append(out, extraArgs...)
	return out
}
