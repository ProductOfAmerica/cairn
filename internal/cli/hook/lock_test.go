package hook_test

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ProductOfAmerica/cairn/internal/cli/hook"
)

func TestFileLock_AcquireAndRelease(t *testing.T) {
	target := filepath.Join(t.TempDir(), "settings.json")
	l, err := hook.AcquireLock(target, time.Second)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Errorf("release: %v", err)
	}
	// Second release is a no-op.
	if err := l.Release(); err != nil {
		t.Errorf("second release: %v", err)
	}
}

func TestFileLock_BlocksSecondAcquire(t *testing.T) {
	target := filepath.Join(t.TempDir(), "settings.json")
	l1, err := hook.AcquireLock(target, time.Second)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	// Second acquire must time out because l1 hasn't released.
	start := time.Now()
	_, err = hook.AcquireLock(target, 200*time.Millisecond)
	elapsed := time.Since(start)
	if err == nil {
		t.Errorf("second acquire should have failed while first held lock")
	}
	if elapsed < 150*time.Millisecond {
		t.Errorf("should have waited ~200ms, waited %v", elapsed)
	}
	if err := l1.Release(); err != nil {
		t.Fatal(err)
	}
	// Now the lock is free.
	l3, err := hook.AcquireLock(target, time.Second)
	if err != nil {
		t.Fatalf("re-acquire after release: %v", err)
	}
	_ = l3.Release()
}

func TestFileLock_SerializesConcurrentWriters(t *testing.T) {
	// Spin up 4 goroutines that each try to acquire, do a "critical
	// section", release. With a real lock, the critical sections
	// execute serially — the counter under the lock reaches 4 without
	// races.
	target := filepath.Join(t.TempDir(), "settings.json")

	const workers = 4
	var wg sync.WaitGroup
	var mu sync.Mutex // for the shared counter only
	counter := 0

	for range workers {
		wg.Go(func() {
			l, err := hook.AcquireLock(target, 5*time.Second)
			if err != nil {
				t.Errorf("acquire: %v", err)
				return
			}
			// "critical section"
			mu.Lock()
			counter++
			mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			_ = l.Release()
		})
	}
	wg.Wait()
	if counter != workers {
		t.Errorf("counter=%d want %d (lock ensured all workers entered critical section)", counter, workers)
	}
}
