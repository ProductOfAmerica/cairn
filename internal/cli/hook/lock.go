package hook

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DefaultLockWait is the max time AcquireLock will block waiting for a
// conflicting writer to release. Five seconds is generous for a human-
// -speed operator workflow (`cairn hook enable/disable` runs once per
// session) and short enough that a stale lock from a crashed cairn
// process is noticeable.
const DefaultLockWait = 5 * time.Second

// FileLock is a cross-platform advisory file lock implemented via a
// sidecar `<path>.cairn-lock` file opened with O_CREATE|O_EXCL. The
// first writer wins; subsequent writers retry with exponential backoff
// up to a caller-chosen max wait.
//
// Portability: works on every OS Go's os.OpenFile supports because
// the EXCL semantics are enforced by the filesystem, not the kernel's
// flock/LockFileEx APIs. Trade-off: a crashed cairn leaves a stale
// lock file that operators must remove by hand. AcquireLock's error
// message names the path so the fix is mechanical.
type FileLock struct {
	path     string
	released bool
}

// AcquireLock takes an exclusive advisory lock on targetPath by
// creating a sidecar `<targetPath>.cairn-lock` file. Retries with
// exponential backoff (25 ms → 250 ms cap) up to maxWait wall-clock.
//
// Returns hook_config_locked-kinded cairnerr when the deadline elapses
// so callers can surface a stable error.code in the JSON envelope.
func AcquireLock(targetPath string, maxWait time.Duration) (*FileLock, error) {
	if maxWait <= 0 {
		maxWait = DefaultLockWait
	}
	// Create the target's parent dir if absent so the sidecar lock file
	// can be opened. Settings.json may not exist yet on a fresh enable,
	// but mkdir is always safe; Save below also mkdir-alls before write.
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir for lock %s: %w", targetPath, err)
	}
	lockPath := targetPath + ".cairn-lock"
	deadline := time.Now().Add(maxWait)
	backoff := 25 * time.Millisecond
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
			return &FileLock{path: lockPath}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("acquire lock %s: %w", lockPath, err)
		}
		if time.Now().After(deadline) {
			// Caller wraps this in a cairnerr with kind
			// hook_config_locked; we return a plain sentinel-style
			// error to keep this file independent of cairnerr.
			return nil, fmt.Errorf("lock %s held after %s (remove the lock file if stale)", lockPath, maxWait)
		}
		time.Sleep(backoff)
		if backoff < 250*time.Millisecond {
			backoff *= 2
		}
	}
}

// Release removes the sidecar lock file. Safe to call multiple times;
// only the first call removes. Typical pattern:
//
//	lock, err := AcquireLock(path, 0)
//	if err != nil { ... }
//	defer lock.Release()
func (l *FileLock) Release() error {
	if l.released {
		return nil
	}
	l.released = true
	if err := os.Remove(l.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
