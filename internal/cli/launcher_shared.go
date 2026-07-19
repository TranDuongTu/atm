package cli

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"atm/internal/core"
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

func ensureProjectForLaunch(s core.Service, code string) (*core.Project, error) {
	p, err := s.GetProject(code)
	if err == nil {
		return p, nil
	}
	if !core.IsNotFound(err) {
		return nil, err
	}
	p, err = s.CreateProject(code, code, "admin@cli:unset")
	if err == nil {
		return p, nil
	}
	if core.IsConflict(err) {
		return s.GetProject(code)
	}
	return nil, err
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

// contextCachePath returns the stable on-disk path for a rendered context
// prompt keyed on (project, role, persona, action, capability). Repeated
// launches of the same tuple reuse the same file.
//
// role is "dev" or "manage". For "dev", action and capability are ignored.
// For "manage", an empty capability becomes "all" in the filename.
func contextCachePath(storePath, code, role, persona, action, capability string) string {
	key := cacheKey(role, persona, action, capability)
	return filepath.Join(storePath, "projects", code, "cache", key+".md")
}

// cacheKey builds the filename stem for a context cache file. Non-alphanumeric
// characters collapse to a single "-"; the result is lowercased and trimmed
// of leading/trailing "-".
func cacheKey(role, persona, action, capability string) string {
	parts := []string{role, persona}
	if role == "manage" {
		parts = append(parts, action)
		if capability == "" {
			parts = append(parts, "all")
		} else {
			parts = append(parts, capability)
		}
	}
	for i, p := range parts {
		parts[i] = sanitizeCacheSegment(p)
	}
	return strings.Join(parts, "-")
}

// sanitizeCacheSegment lowercases and collapses non-alphanumeric runs to "-".
func sanitizeCacheSegment(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	prevDash := true // suppress leading "-"
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else {
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if out == "" {
		return "x"
	}
	return out
}

// writeContextIfDiff writes content to path only when the existing file's
// bytes differ. When the existing file matches byte-for-byte, it is a no-op
// (mtime unchanged). Parent dirs are created with MkdirAll. The write is
// atomic via a temp file in the same dir followed by a rename.
func writeContextIfDiff(path string, content []byte) error {
	if existing, err := os.ReadFile(path); err == nil {
		if bytes.Equal(existing, content) {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read existing context %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create context dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ctx-*.md")
	if err != nil {
		return fmt.Errorf("create temp context: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("write temp context: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp context: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename temp context: %w", err)
	}
	return nil
}
