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
