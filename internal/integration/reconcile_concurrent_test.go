package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/ids"
	"github.com/ProductOfAmerica/cairn/internal/reconcile"
	"github.com/ProductOfAmerica/cairn/internal/repoid"
)

// TestConcurrentReconcile fires 5 in-process goroutines (sharing a single
// *db.DB) + 2 subprocesses (each with its own connection) at reconcile
// simultaneously. All 7 must exit cleanly; exactly one must observe the
// rule-2 drift flip; the other six must be no-ops; all reconcile_ids must
// be distinct.
//
// This exercises both contention paths:
//   - goroutines: connection-pool contention inside a single process. SQLite
//     serializes writers via BEGIN IMMEDIATE; database/sql's pool is
//     concurrency-safe.
//   - subprocesses: file-level locking between separate processes, each
//     with an independent *sql.DB. busy_timeout=5000 absorbs the window
//     between writers.
//
// Mirrors internal/integration/concurrent_claim_test.go's harness.
func TestConcurrentReconcile(t *testing.T) {
	repo := mustReconcileRepo(t)
	cairnHome := t.TempDir()

	// Seed: plan → claim → evidence → verdict → complete → edit spec → re-plan.
	// Mirrors reconcile_e2e_test.go subtest 1 (rule2_drift_flip).
	runCairnExit(t, repo, cairnHome, 0, "init")
	runCairnExit(t, repo, cairnHome, 0, "spec", "validate")
	runCairnExit(t, repo, cairnHome, 0, "task", "plan")

	claimEnv := runCairnExit(t, repo, cairnHome, 0, "task", "claim", "TASK-001",
		"--agent", "a", "--ttl", "30m")
	data := claimEnv["data"].(map[string]any)
	claimID := data["claim_id"].(string)
	runID := data["run_id"].(string)

	outPath := filepath.Join(repo, "ok.txt")
	if err := os.WriteFile(outPath, []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("write ok.txt: %v", err)
	}
	runCairnExit(t, repo, cairnHome, 0, "evidence", "put", outPath)
	runCairnExit(t, repo, cairnHome, 0, "verdict", "report",
		"--gate", "AC-001", "--run", runID, "--status", "pass",
		"--evidence", outPath,
		"--producer-hash", strings.Repeat("a", 64),
		"--inputs-hash", strings.Repeat("b", 64))
	runCairnExit(t, repo, cairnHome, 0, "task", "complete", claimID)

	// Edit gate config so gate_def_hash changes, then re-plan so the
	// materialized gate_def_hash diverges from the verdict's recorded hash.
	// Exactly one reconcile invocation will flip the task stale.
	writeReq001(t, repo, "dogfood (edited)", "changed")
	runCairnExit(t, repo, cairnHome, 0, "task", "plan")

	// Open a single shared *db.DB for the 5 goroutines. Each goroutine
	// constructs its own Orchestrator but shares the handle — this exercises
	// the connection-pool contention path. SQLite WAL + BEGIN IMMEDIATE
	// serialize writers; busy_timeout (5s) + WithTx retries absorb races.
	dbPath := resolveStateDBPath(t, repo, cairnHome)
	sharedDB, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open shared db: %v", err)
	}
	defer sharedDB.Close()

	clk := clock.Wall{}
	gen := ids.NewGenerator(clk)
	blobRoot := filepath.Join(filepath.Dir(dbPath), "blobs")

	const goroutines = 5
	const subprocs = 2

	type goResult struct {
		reconcileID     string
		rule2FlipsStale int
		err             error
	}
	goResults := make(chan goResult, goroutines)

	type subResult struct {
		reconcileID     string
		rule2FlipsStale int
		err             error
		exitCode        int
	}
	subResults := make(chan subResult, subprocs)

	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			orch := reconcile.NewOrchestrator(sharedDB, clk, gen, blobRoot)
			res, rerr := orch.Run(context.Background(), reconcile.Opts{})
			goResults <- goResult{
				reconcileID:     res.ReconcileID,
				rule2FlipsStale: res.Stats.Rule2TasksFlippedStale,
				err:             rerr,
			}
		}()
	}

	subCmds := make([]*exec.Cmd, subprocs)
	subOuts := make([]*bytes.Buffer, subprocs)
	for i := 0; i < subprocs; i++ {
		cmd := exec.Command(cairnBinary, "reconcile")
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "CAIRN_HOME="+cairnHome)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &bytes.Buffer{}
		subCmds[i] = cmd
		subOuts[i] = &out
	}
	for i := 0; i < subprocs; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			err := subCmds[idx].Run()
			code := 0
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else if err != nil {
				subResults <- subResult{err: err, exitCode: -1}
				return
			}
			// Parse stdout envelope.
			id, flips, perr := parseReconcileEnvelope(subOuts[idx].Bytes())
			subResults <- subResult{
				reconcileID:     id,
				rule2FlipsStale: flips,
				err:             perr,
				exitCode:        code,
			}
		}(i)
	}

	close(start)
	wg.Wait()
	close(goResults)
	close(subResults)

	// Collect all 7 outcomes.
	allIDs := make([]string, 0, goroutines+subprocs)
	totalFlips := 0
	oneCount := 0
	for r := range goResults {
		if r.err != nil {
			t.Errorf("goroutine error: %v", r.err)
			continue
		}
		if r.reconcileID == "" {
			t.Errorf("goroutine: empty reconcile_id")
			continue
		}
		allIDs = append(allIDs, r.reconcileID)
		totalFlips += r.rule2FlipsStale
		if r.rule2FlipsStale == 1 {
			oneCount++
		}
	}
	for r := range subResults {
		if r.err != nil {
			t.Errorf("subprocess error (exit=%d): %v", r.exitCode, r.err)
			continue
		}
		if r.exitCode != 0 {
			t.Errorf("subprocess exit=%d, want 0", r.exitCode)
			continue
		}
		if r.reconcileID == "" {
			t.Errorf("subprocess: empty reconcile_id")
			continue
		}
		allIDs = append(allIDs, r.reconcileID)
		totalFlips += r.rule2FlipsStale
		if r.rule2FlipsStale == 1 {
			oneCount++
		}
	}

	// Exactly one winner of the rule-2 flip.
	if totalFlips != 1 {
		t.Fatalf("expected total rule_2_tasks_flipped_stale=1 across all 7 runs, got %d", totalFlips)
	}
	if oneCount != 1 {
		t.Fatalf("expected exactly one run to report rule_2_tasks_flipped_stale==1, got %d", oneCount)
	}

	// All reconcile_ids distinct.
	if len(allIDs) != goroutines+subprocs {
		t.Fatalf("expected %d reconcile_ids, got %d: %v", goroutines+subprocs, len(allIDs), allIDs)
	}
	seen := map[string]bool{}
	for _, id := range allIDs {
		if seen[id] {
			t.Errorf("duplicate reconcile_id: %s (all=%v)", id, allIDs)
		}
		seen[id] = true
	}

	// Bracket event counts — verify via a fresh subprocess query so we're
	// reading through the same DB the real events live in.
	evEnv := runCairnExit(t, repo, cairnHome, 0, "events", "since", "0", "--limit", "500")
	evs, _ := evEnv["data"].(map[string]any)["events"].([]any)
	var started, ended int
	for _, raw := range evs {
		e, _ := raw.(map[string]any)
		switch e["Kind"] {
		case "reconcile_started":
			started++
		case "reconcile_ended":
			ended++
		}
	}
	if started != goroutines+subprocs {
		t.Errorf("reconcile_started count=%d, want %d", started, goroutines+subprocs)
	}
	if ended != goroutines+subprocs {
		t.Errorf("reconcile_ended count=%d, want %d", ended, goroutines+subprocs)
	}
}

// resolveStateDBPath replicates cmd/cairn's openStateDBWithBlobs path logic
// so goroutines can share a single *db.DB pointed at the same file the
// subprocess CLI opens.
func resolveStateDBPath(t *testing.T, repo, cairnHome string) string {
	t.Helper()
	id, err := repoid.Resolve(repo)
	if err != nil {
		t.Fatalf("repoid.Resolve: %v", err)
	}
	return filepath.Join(cairnHome, id, "state.db")
}

// parseReconcileEnvelope extracts reconcile_id + rule_2_tasks_flipped_stale
// from a subprocess stdout envelope.
func parseReconcileEnvelope(stdout []byte) (string, int, error) {
	stripped := bytes.TrimSpace(stdout)
	if len(stripped) == 0 {
		return "", 0, nil
	}
	var env map[string]any
	if err := json.Unmarshal(stripped, &env); err != nil {
		return "", 0, err
	}
	data, _ := env["data"].(map[string]any)
	if data == nil {
		return "", 0, nil
	}
	id, _ := data["reconcile_id"].(string)
	stats, _ := data["stats"].(map[string]any)
	flips := 0
	if stats != nil {
		if f, ok := stats["rule_2_tasks_flipped_stale"].(float64); ok {
			flips = int(f)
		}
	}
	return id, flips, nil
}
