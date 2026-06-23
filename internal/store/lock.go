package store

import (
	"fmt"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

var locker = struct {
	sync.Mutex
	fds map[string]*heldLock
}{fds: map[string]*heldLock{}}

type heldLock struct {
	mu  sync.Mutex
	f   *os.File
	cnt int
}

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
	locker.Lock()
	defer locker.Unlock()
	h, ok := locker.fds[code]
	if !ok {
		_ = os.MkdirAll(s.projectsDir(), 0o755)
		f, err := os.OpenFile(s.lockPath(code), os.O_CREATE|os.O_RDWR, 0o644)
		if err != nil {
			h = &heldLock{}
		} else {
			h = &heldLock{f: f}
		}
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
