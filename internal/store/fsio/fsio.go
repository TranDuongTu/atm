// Package fsio holds the store's two filesystem primitives — the
// cross-process advisory lock and atomic JSON file I/O — shared by the store
// facade and the eventlog engine. It knows nothing about domains or events.
package fsio

import (
	"fmt"
	"os"
	"path/filepath"
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

// WithLock serializes fn against every other holder of name, in this process
// (mutex) and across processes (flock on <projectsDir>/<name>.lock). It is
// NOT reentrant for the same name; different names nest. The registry is
// keyed by bare name — identical to the pre-carve Store.WithLock behavior.
func WithLock(projectsDir, name string, fn func() error) error {
	h := getHeldLock(projectsDir, name)
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

func getHeldLock(projectsDir, name string) *heldLock {
	locker.Lock()
	defer locker.Unlock()
	h, ok := locker.fds[name]
	if !ok {
		_ = os.MkdirAll(projectsDir, 0o755)
		f, err := os.OpenFile(filepath.Join(projectsDir, name+".lock"), os.O_CREATE|os.O_RDWR, 0o644)
		if err != nil {
			h = &heldLock{}
		} else {
			h = &heldLock{f: f}
		}
		locker.fds[name] = h
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
