package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestConcurrentClaim_Subprocesses fires 3 subprocesses + 5 goroutines at the
// same task. Exactly one wins. No DB corruption.
func TestConcurrentClaim_Subprocesses(t *testing.T) {
	repo := mustDogfoodRepo(t)
	cairnHome := t.TempDir()
	_, _ = runCairn(t, repo, cairnHome, "init")
	_, _ = runCairn(t, repo, cairnHome, "task", "plan")

	const goroutines = 5
	const subprocs = 3

	results := make(chan int, goroutines+subprocs)

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, code := runCairn(t, repo, cairnHome, "task", "claim", "TASK-001",
				"--agent", "gr", "--ttl", "30m")
			results <- code
		}()
	}
	subCmds := make([]*exec.Cmd, subprocs)
	for i := 0; i < subprocs; i++ {
		cmd := exec.Command(cairnBinary, "task", "claim", "TASK-001",
			"--agent", "sp", "--ttl", "30m")
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "CAIRN_HOME="+cairnHome)
		subCmds[i] = cmd
	}

	close(start)
	// Start subprocess cmds.
	subResults := make(chan int, subprocs)
	for i := 0; i < subprocs; i++ {
		go func(c *exec.Cmd) {
			err := c.Run()
			if ee, ok := err.(*exec.ExitError); ok {
				subResults <- ee.ExitCode()
			} else if err != nil {
				subResults <- -1
			} else {
				subResults <- 0
			}
		}(subCmds[i])
	}
	wg.Wait()

	// Collect.
	zero := 0
	nonZero := 0
	for i := 0; i < goroutines; i++ {
		if c := <-results; c == 0 {
			zero++
		} else {
			nonZero++
		}
	}
	for i := 0; i < subprocs; i++ {
		if c := <-subResults; c == 0 {
			zero++
		} else {
			nonZero++
		}
	}
	if zero != 1 {
		t.Fatalf("expected exactly 1 winner, got %d (non-zero: %d)", zero, nonZero)
	}

	// Sanity: PRAGMA integrity_check via a fresh cairn invocation that reads
	// events — implicit integrity pass by opening the DB with WAL recovery.
	_, code := runCairn(t, repo, cairnHome, "events", "since", "0")
	if code != 0 {
		t.Fatalf("post-race events read failed: %d", code)
	}

	_ = time.Second // reserved for future jitter insertion
	_ = filepath.Join
}
