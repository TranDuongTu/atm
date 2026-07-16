package eventsync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"atm/libs/eventsource"
)

// GitTarget is a SyncTarget whose remote is a git repository: each
// project's full event log lives at <subpath>/<CODE>/events.v2.jsonl in
// a cached local clone, mirroring DirTarget's layout but with git itself
// providing the durable, shared storage and push/fetch as the transport.
// A GitTarget never trusts its clone to be current: every Fetch and every
// Publish attempt refreshes to origin/HEAD first.
type GitTarget struct {
	url     string
	subpath string
	workdir string
}

// NewGitTarget returns a GitTarget for the repository at url, cloned (on
// first use) into a deterministic subdirectory of cacheDir keyed by a
// hash of url, so repeated calls for the same url reuse the same clone.
// subpath is the directory within the repo holding project event logs.
func NewGitTarget(cacheDir, url, subpath string) *GitTarget {
	sum := sha256.Sum256([]byte(url))
	return &GitTarget{
		url:     url,
		subpath: subpath,
		workdir: filepath.Join(cacheDir, hex.EncodeToString(sum[:])[:12]),
	}
}

func (g *GitTarget) eventsPath(project string) string {
	return filepath.Join(g.workdir, g.subpath, project, "events.v2.jsonl")
}

// Fetch reports project's full event log as it stands on origin/HEAD
// after refreshing the cached clone. A missing file means the project
// doesn't exist there yet: RemoteSnapshot{Absent: true}, not an error.
// State carries the head commit hash so Publish's caller can round-trip
// it, though GitTarget's own Publish always re-reads rather than trusting
// a caller-supplied base — the clone itself may have moved on since.
func (g *GitTarget) Fetch(ctx context.Context, project string) (*RemoteSnapshot, error) {
	if err := g.sync(ctx); err != nil {
		return nil, err
	}
	head, err := g.headCommit(ctx)
	if err != nil {
		return nil, err
	}

	events, ids, existed, err := g.readEventFile(project)
	if err != nil {
		return nil, err
	}
	if !existed {
		return &RemoteSnapshot{Absent: true, State: head}, nil
	}
	return &RemoteSnapshot{Events: events, Digest: SetDigest(ids), State: head}, nil
}

// Publish appends missing's canonical lines to project's event log and
// pushes the result to origin, retrying up to 3 times against a fresh
// refresh whenever the push loses a race to another publisher. Each
// attempt refreshes to origin/HEAD, re-reads the file, and drops any
// event another publisher already landed there, so a losing attempt's
// retry only ever adds what's still actually missing — never reintroduces
// or duplicates what the winner already pushed. base is unused: unlike a
// caller-supplied snapshot, the clone's own origin/HEAD after refresh is
// always the authoritative compare-and-swap base here.
func (g *GitTarget) Publish(ctx context.Context, project string, missing []RawEvent, base *RemoteSnapshot) error {
	if len(missing) == 0 {
		return nil
	}

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if err := g.sync(ctx); err != nil {
			return err
		}

		have, _, _, err := g.readEventFile(project)
		if err != nil {
			return err
		}
		seen := make(map[string]bool, len(have))
		for _, ev := range have {
			seen[ev.ID] = true
		}
		var toWrite []RawEvent
		for _, ev := range missing {
			if !seen[ev.ID] {
				toWrite = append(toWrite, ev)
			}
		}
		if len(toWrite) == 0 {
			return nil // someone else already published everything we had
		}

		if err := g.appendEvents(project, toWrite); err != nil {
			return err
		}
		if err := g.ensureGitattributes(project); err != nil {
			return err
		}

		if _, err := run(ctx, g.workdir, "add", "-A", g.subpath); err != nil {
			return err
		}
		msg := fmt.Sprintf("chore(atm-sync): %s +%d events", project, len(toWrite))
		if _, err := run(ctx, g.workdir,
			"-c", "user.name=atm-sync", "-c", "user.email=atm-sync@localhost",
			"commit", "-m", msg,
		); err != nil {
			return err
		}

		if _, err := run(ctx, g.workdir, "push", "origin", "HEAD"); err != nil {
			lastErr = err
			continue
		}
		return nil
	}

	return fmt.Errorf("eventsync: gittarget: publish %s: retry exhausted after 3 attempts, last push error: %w", project, lastErr)
}

// sync ensures workdir holds a clone of url, cloning it on first use and
// otherwise fetching and hard-resetting to origin/HEAD so every caller
// (Fetch, and each Publish attempt) always starts from the remote's
// current state rather than a possibly stale local one.
func (g *GitTarget) sync(ctx context.Context) error {
	if _, err := os.Stat(filepath.Join(g.workdir, ".git")); err != nil {
		parent := filepath.Dir(g.workdir)
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("eventsync: gittarget: mkdir %s: %w", parent, err)
		}
		if _, err := run(ctx, parent, "clone", g.url, g.workdir); err != nil {
			return err
		}
		return nil
	}
	if _, err := run(ctx, g.workdir, "fetch", "origin"); err != nil {
		return err
	}
	if _, err := run(ctx, g.workdir, "reset", "--hard", "origin/HEAD"); err != nil {
		return err
	}
	return nil
}

func (g *GitTarget) headCommit(ctx context.Context) (string, error) {
	out, err := run(ctx, g.workdir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// readEventFile reads and parses project's event log exactly like
// DirTarget.Fetch: every line is re-parsed via eventsource.Parse to
// recompute its id (the wire is never trusted), repeats of an id are
// dropped, and a final line with no trailing newline (a torn write) is
// skipped. existed is false, with no error, when the file simply doesn't
// exist yet.
func (g *GitTarget) readEventFile(project string) (events []RawEvent, ids []string, existed bool, err error) {
	data, err := os.ReadFile(g.eventsPath(project))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, false, nil
		}
		return nil, nil, false, fmt.Errorf("eventsync: gittarget: read %s: %w", project, err)
	}

	lines := bytes.Split(data, []byte("\n"))
	lines = lines[:len(lines)-1] // drop the tail: "" after a trailing \n, or a torn line without one

	seen := make(map[string]bool, len(lines))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		raw := make([]byte, len(line))
		copy(raw, line) // independent of data's backing array

		ev, perr := eventsource.Parse(raw)
		if perr != nil {
			return nil, nil, false, fmt.Errorf("eventsync: gittarget: %s: %w", project, perr)
		}
		if seen[ev.ID] {
			continue
		}
		seen[ev.ID] = true
		events = append(events, RawEvent{ID: ev.ID, Raw: raw})
		ids = append(ids, ev.ID)
	}
	return events, ids, true, nil
}

// appendEvents writes events's canonical lines to project's event log in
// the working tree, creating the project's directory and log file on
// first publish.
func (g *GitTarget) appendEvents(project string, events []RawEvent) error {
	path := g.eventsPath(project)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("eventsync: gittarget: mkdir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("eventsync: gittarget: open %s: %w", project, err)
	}
	defer f.Close()
	for _, ev := range events {
		line := make([]byte, len(ev.Raw)+1)
		copy(line, ev.Raw)
		line[len(ev.Raw)] = '\n'
		if _, err := f.Write(line); err != nil {
			return fmt.Errorf("eventsync: gittarget: write %s: %w", project, err)
		}
	}
	return nil
}

// ensureGitattributes makes sure <subpath>/.gitattributes tells git to
// union-merge project's event log rather than conflict on it: since
// .gitattributes patterns are relative to the directory holding the
// file, the entry names the path from <subpath>/, not the repo root.
// It's a no-op if the line is already there.
func (g *GitTarget) ensureGitattributes(project string) error {
	path := filepath.Join(g.workdir, g.subpath, ".gitattributes")
	line := fmt.Sprintf("%s/events.v2.jsonl merge=union", project)

	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("eventsync: gittarget: read .gitattributes: %w", err)
	}
	if strings.Contains(string(data), line) {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("eventsync: gittarget: mkdir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("eventsync: gittarget: open .gitattributes: %w", err)
	}
	defer f.Close()
	if _, err := fmt.Fprintln(f, line); err != nil {
		return fmt.Errorf("eventsync: gittarget: write .gitattributes: %w", err)
	}
	return nil
}

// run invokes git as a subprocess in dir, capturing stdout and folding
// stderr into any error so callers get git's own diagnostic. A missing
// git binary is reported clearly up front rather than surfacing as a
// generic "executable file not found" from exec.
func run(ctx context.Context, dir string, args ...string) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("eventsync: gittarget: git binary not found: %w", err)
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("eventsync: gittarget: git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
