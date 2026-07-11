package store

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWithLockSerializes(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	_ = s.Init("")

	var order []int
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstEntered int32

	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = s.WithLock("LOCK", func() error {
			atomic.StoreInt32(&firstEntered, 1)
			mu.Lock()
			order = append(order, 1)
			mu.Unlock()
			time.Sleep(100 * time.Millisecond)
			return nil
		})
	}()
	time.Sleep(20 * time.Millisecond)
	go func() {
		defer wg.Done()
		_ = s.WithLock("LOCK", func() error {
			if atomic.LoadInt32(&firstEntered) != 1 {
				t.Error("second goroutine entered before first released")
			}
			mu.Lock()
			order = append(order, 2)
			mu.Unlock()
			return nil
		})
	}()
	wg.Wait()

	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("order = %v want [1 2]", order)
	}
}

// --- Regression tests for the lock-reentrancy deadlock ---
//
// WithLock holds a plain, non-reentrant sync.Mutex for its entire closure.
// Several mutation paths call GetProject/GetTask/GetComment (or, transitively,
// labelProjectExists) from INSIDE their own already-held WithLock closure. In
// steady state the cache read is a fast-path hit and this is never a problem,
// but the moment the relevant cache row is missing or stale — reachable via
// the documented "cache.db is disposable, delete it to force a rebuild" flow
// — that inner Get* call finds the cache empty, tries to rebuild by
// re-entering s.WithLock(code, ...) for the SAME code, and deadlocks forever
// on the held (non-reentrant) mutex.
//
// Each test below simulates a cache miss by hand-deleting the relevant cache
// row (the same technique used by TestGetTaskLazyMissRebuildsFromLog and
// friends), then drives the previously-deadlocking mutation from a goroutine
// guarded by a hard timeout: before the fix these hang past the deadline and
// fail; after the fix they complete promptly.

func TestSetTitleAfterTaskCacheMissDoesNotDeadlock(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("DLA", "x", testActor)
	tk, _ := s.CreateTask("DLA", "t", "", nil, testActor)

	db, _ := s.cacheDB()
	// mutateTask (shared by SetTitle/SetDescription/TaskLabelRemove) calls
	// s.GetTask(id) from inside its own s.WithLock(code, ...) closure.
	_, _ = db.Exec(`DELETE FROM tasks WHERE id = ?`, tk.ID)

	done := make(chan error, 1)
	go func() { done <- s.SetTitle(tk.ID, "changed", testActor) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SetTitle after cache miss: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SetTitle deadlocked: nested WithLock re-entry on task cache miss")
	}
}

func TestRemoveTaskAfterTaskCacheMissDoesNotDeadlock(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("DLB", "x", testActor)
	tk, _ := s.CreateTask("DLB", "t", "", nil, testActor)

	db, _ := s.cacheDB()
	// RemoveTask calls s.GetTask(id) from inside its own s.WithLock(code, ...)
	// closure.
	_, _ = db.Exec(`DELETE FROM tasks WHERE id = ?`, tk.ID)

	done := make(chan error, 1)
	go func() { done <- s.RemoveTask(tk.ID, testActor) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RemoveTask after cache miss: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RemoveTask deadlocked: nested WithLock re-entry on task cache miss")
	}
}

func TestSetProjectNameAfterProjectCacheMissDoesNotDeadlock(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("DLC", "x", testActor)

	db, _ := s.cacheDB()
	// SetProjectName calls s.GetProject(code) from inside its own
	// s.WithLock(code, ...) closure.
	_, _ = db.Exec(`DELETE FROM projects WHERE code = ?`, "DLC")

	done := make(chan error, 1)
	go func() { done <- s.SetProjectName("DLC", "new-name", testActor) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SetProjectName after cache miss: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SetProjectName deadlocked: nested WithLock re-entry on project cache miss")
	}
}

func TestSetCommentBodyAfterCommentCacheMissDoesNotDeadlock(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("DLD", "x", testActor)
	tk, _ := s.CreateTask("DLD", "t", "", nil, testActor)
	c, _ := s.CreateComment(tk.ID, "hi", nil, "", testActor)

	db, _ := s.cacheDB()
	// mutateComment (shared by SetCommentBody/CommentLabelRemove) calls
	// s.GetComment(id) from inside its own s.WithLock(code, ...) closure.
	_, _ = db.Exec(`DELETE FROM comments WHERE id = ?`, c.ID)

	done := make(chan error, 1)
	go func() { done <- s.SetCommentBody(c.ID, "changed", testActor) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SetCommentBody after cache miss: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SetCommentBody deadlocked: nested WithLock re-entry on comment cache miss")
	}
}

func TestCreateTaskAfterProjectCacheMissDoesNotDeadlock(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("DLE", "x", testActor)

	db, _ := s.cacheDB()
	// CreateTask calls s.GetProject(projectCode) from inside its own
	// s.WithLock(projectCode, ...) closure (and, for supplied labels,
	// s.labelProjectExists -> s.GetProject one level deeper).
	_, _ = db.Exec(`DELETE FROM projects WHERE code = ?`, "DLE")

	done := make(chan error, 1)
	go func() {
		_, err := s.CreateTask("DLE", "t", "", []string{"DLE:type:bug"}, testActor)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("CreateTask after cache miss: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CreateTask deadlocked: nested WithLock re-entry on project cache miss")
	}
}

func TestWithLockDifferentCodesParallel(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	_ = s.Init("")

	done := make(chan struct{}, 2)
	go func() {
		_ = s.WithLock("AAA", func() error {
			time.Sleep(50 * time.Millisecond)
			return nil
		})
		done <- struct{}{}
	}()
	go func() {
		_ = s.WithLock("BBB", func() error {
			done <- struct{}{}
			return nil
		})
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("different lock codes should not block each other")
	}
}
