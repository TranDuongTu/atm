package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"
)

// locker is a process-wide registry of held project locks so that WithLock is
// reentrant within the same process (an agent calling store APIs in sequence).
// Each project code has at most one open lock file fd held by this process.
var locker = struct {
	sync.Mutex
	fds map[string]*heldLock
}{fds: map[string]*heldLock{}}

type heldLock struct {
	mu  sync.Mutex
	f   *os.File
	cnt int
}

// WithLock runs fn while holding an exclusive lock on the project's lock file.
// It is safe to call WithLock for the same code reentrantly within the process:
// the per-code mutex serializes in-process callers and the flock is held once.
func (s *Store) WithLock(code string, fn func() error) error {
	h := getHeldLock(s, code)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cnt == 0 {
		if err := acquireFlock(h.f); err != nil {
			return err
		}
	}
	h.cnt++
	defer func() {
		h.cnt--
		if h.cnt == 0 {
			_ = releaseFlock(h.f)
		}
	}()
	return fn()
}

func getHeldLock(s *Store, code string) *heldLock {
	locker.Mutex.Lock()
	defer locker.Mutex.Unlock()
	h, ok := locker.fds[code]
	if !ok {
		if err := os.MkdirAll(s.projectsDir(), 0o755); err != nil {
			// Defer error to first use; build the heldLock anyway.
			h = &heldLock{}
			locker.fds[code] = h
			return h
		}
		f, err := os.OpenFile(s.lockPath(code), os.O_CREATE|os.O_RDWR, 0o644)
		if err != nil {
			h = &heldLock{}
			locker.fds[code] = h
			return h
		}
		h = &heldLock{f: f}
		locker.fds[code] = h
		return h
	}
	return h
}

func acquireFlock(f *os.File) error {
	if f == nil {
		return fmt.Errorf("lock file not open")
	}
	return unix.Flock(int(f.Fd()), unix.LOCK_EX)
}

func releaseFlock(f *os.File) error {
	if f == nil {
		return nil
	}
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}

// lockForTest exposes the per-code mutex for tests that need to assert serialization.
func lockForTest(code string) *sync.Mutex {
	h, ok := locker.fds[code]
	if !ok {
		return nil
	}
	return &h.mu
}

// _ filepath import retained for future path helpers.
var _ = filepath.Join
