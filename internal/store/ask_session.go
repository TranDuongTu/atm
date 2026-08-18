package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"atm/internal/core"
)

var askSessionIDRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// validateAskSessionID rejects rather than sanitizes. The id becomes a path
// component, so a silent rewrite would hand an agent a different conversation
// than the one it named — which it would never find out about.
func validateAskSessionID(id string) error {
	if id == "." || id == ".." || !askSessionIDRe.MatchString(id) {
		return fmt.Errorf("%w: session id %q must match [A-Za-z0-9._-] and be 1-64 characters", core.ErrUsage, id)
	}
	return nil
}

// Ask history lives beside vectors/ and inquiry-log.jsonl, NOT under
// projects/<CODE>/cache/: that directory holds the launcher's regenerable
// rendered .md files, and ask history is derivable from nothing. Parking
// non-regenerable user data under a name that advertises disposability is how
// it gets deleted by a cache-clear doing exactly what its name promises.
func (s *Store) askSessionsDir(code string) string {
	return filepath.Join(s.projectDir(code), "ask-sessions")
}

func (s *Store) askSessionPath(code, id string) string {
	return filepath.Join(s.askSessionsDir(code), id+".jsonl")
}

func (s *Store) AppendAskTurn(code, sessionID string, t core.AskTurn) error {
	if err := validateAskSessionID(sessionID); err != nil {
		return err
	}
	return s.WithLock(code, func() error {
		if err := os.MkdirAll(s.askSessionsDir(code), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(s.askSessionPath(code, sessionID), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		if t.At == "" {
			t.At = core.RFC3339UTC(core.Now())
		}
		b, err := json.Marshal(t)
		if err != nil {
			return err
		}
		_, err = f.Write(append(b, '\n'))
		return err
	})
}

func (s *Store) ReadAskTurns(code, sessionID string) ([]core.AskTurn, error) {
	if err := validateAskSessionID(sessionID); err != nil {
		return nil, err
	}
	f, err := os.Open(s.askSessionPath(code, sessionID))
	if err != nil {
		if os.IsNotExist(err) {
			// A session's first turn reads before it writes. Absent is empty.
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []core.AskTurn
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var t core.AskTurn
		if err := json.Unmarshal([]byte(line), &t); err != nil {
			continue
		}
		out = append(out, t)
	}
	return out, sc.Err()
}
