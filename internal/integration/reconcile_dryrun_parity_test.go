package integration_test

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ProductOfAmerica/cairn/internal/db"
)

// TestDryRunParity locks down the design-spec §8.4 contract: a dry-run produces
// the exact same mutation set a real run would apply. The protocol is:
//
//  1. Seed a DB that exercises rules 1, 2, 3, and 5 (rule 4 omitted — see
//     note below).
//  2. SNAPSHOT state.db bytes + events count.
//  3. Run `cairn reconcile --dry-run --evidence-sample-full`. Convert each
//     reported `would_mutate` into a {rule, entity_id, action, reason} tuple
//     and each rule-5 authoring_error into a {task_id, missing_gate_id} tuple.
//  4. Assert zero writes: events count unchanged AND state.db bytes unchanged.
//  5. RESTORE the snapshot.
//  6. Run `cairn reconcile --evidence-sample-full` (the real run). Replay the
//     emitted events into the same tuple shape.
//  7. Assert the two sets match exactly (no timestamp comparison — see caveat).
//  8. Assert rule-5 authoring errors match between dry and real.
//
// Rule 4 omission: orphaning a run requires its claim to have been released
// more than 10 minutes ago, which can't be driven via the CLI's wall clock
// under a test timeout. Hitting rule 4 via direct SQL here would duplicate
// rule4_orphans_test.go's coverage without strengthening the parity claim.
// The dry/real tuples for rules 1/2/3/5 still fully exercise the parity
// contract: same DB read path, same mutation derivation, same serialized tuple
// shape.
//
// Timestamp caveat: both paths use the wall clock and run ms apart, so the
// mutation's `at` timestamp will differ between dry and real. Asserting on
// timestamps would be a flaky false-positive. We only compare the identity
// tuple (rule, entity_id, action, reason).
func TestDryRunParity(t *testing.T) {
	repo := mustEmptyRepo(t)
	cairnHome := t.TempDir()

	// -------------------------------------------------------------------
	// Spec seed: one REQ with three gates. Each real task references one
	// gate; rule 5's task will be INSERTed directly to reference a gate
	// that never existed.
	// -------------------------------------------------------------------
	if err := os.MkdirAll(filepath.Join(repo, "specs", "requirements"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "specs", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeReqThreeGates(t, repo, "v1")
	writeTask(t, repo, "TASK-RULE1", "AC-001")
	writeTask(t, repo, "TASK-RULE2", "AC-002")
	writeTask(t, repo, "TASK-RULE3", "AC-003")

	runCairnExit(t, repo, cairnHome, 0, "init")
	runCairnExit(t, repo, cairnHome, 0, "spec", "validate")
	runCairnExit(t, repo, cairnHome, 0, "task", "plan")

	// -------------------------------------------------------------------
	// Rule 3 seed: complete TASK-RULE3 with its own blob, then delete
	// the blob off disk. The evidence row stays; rule 3 will mark it
	// invalidated (reason=missing).
	// -------------------------------------------------------------------
	task3Blob := filepath.Join(repo, "rule3.txt")
	if err := os.WriteFile(task3Blob, []byte("rule3-blob\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	completeTask(t, repo, cairnHome, "TASK-RULE3", "AC-003", task3Blob)

	// Look up the blob URI via `evidence get` and delete the on-disk file.
	// The evidence row still references it; rule 3 flips it on probe.
	shaTask3 := mustEvidenceSha(t, repo, cairnHome, task3Blob)
	getEnv := runCairnExit(t, repo, cairnHome, 0, "evidence", "get", shaTask3)
	getData, _ := getEnv["data"].(map[string]any)
	blobURI, _ := getData["uri"].(string)
	if blobURI == "" {
		t.Fatalf("evidence get: missing uri; env=%+v", getEnv)
	}
	if err := os.Remove(blobURI); err != nil {
		t.Fatalf("remove rule-3 blob: %v", err)
	}

	// -------------------------------------------------------------------
	// Rule 2 seed: complete TASK-RULE2, then mutate AC-002's producer
	// config so its gate_def_hash drifts. On re-plan the gate row gets
	// the new hash; the stored verdict's gate_def_hash no longer matches,
	// so rule 2 flips the task to 'stale'.
	// -------------------------------------------------------------------
	task2Blob := filepath.Join(repo, "rule2.txt")
	if err := os.WriteFile(task2Blob, []byte("rule2-blob\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	completeTask(t, repo, cairnHome, "TASK-RULE2", "AC-002", task2Blob)

	// Edit spec → re-plan so AC-002's hash drifts. The other two gate
	// hashes are unchanged; TASK-RULE3 stays fresh even though its
	// blob is gone (rule 2 doesn't probe evidence blobs).
	writeReqThreeGates(t, repo, "v2-ac002-drifted")
	runCairnExit(t, repo, cairnHome, 0, "task", "plan")

	// -------------------------------------------------------------------
	// Rule 1 seed: claim TASK-RULE1 with a 1ms TTL, then wait for the
	// wall clock to pass expires_at. Inline rule-1 cleanup only fires on
	// the NEXT claim attempt; reconcile must be the one that releases it.
	// -------------------------------------------------------------------
	claimEnv := runCairnExit(t, repo, cairnHome, 0, "task", "claim", "TASK-RULE1",
		"--agent", "parity", "--ttl", "1ms")
	claimData, _ := claimEnv["data"].(map[string]any)
	claimID, _ := claimData["claim_id"].(string)
	if claimID == "" {
		t.Fatalf("claim TASK-RULE1: missing claim_id: %+v", claimData)
	}
	// Sleep long enough for wall clock to tick past expires_at on slow
	// Windows timers. reconcile_e2e_test uses 50ms for the same reason.
	time.Sleep(50 * time.Millisecond)

	// -------------------------------------------------------------------
	// Rule 5 seed: a task with a required_gate that doesn't exist in the
	// gates table. Plan rejects this (spec validation catches it), so we
	// INSERT directly into the already-initialized state.db.
	// -------------------------------------------------------------------
	dbPath := resolveStateDBPath(t, repo, cairnHome)
	insertRule5Task(t, dbPath)

	// -------------------------------------------------------------------
	// SNAPSHOT: state.db bytes + events count. Both are the "no-mutation"
	// invariants the dry-run must preserve.
	// -------------------------------------------------------------------
	snap := snapshotBytes(t, dbPath)
	eventsBefore := countEvents(t, dbPath)

	// -------------------------------------------------------------------
	// DRY RUN: read-only simulator. MUST NOT write state or emit events.
	// -------------------------------------------------------------------
	dryEnv := runCairnExit(t, repo, cairnHome, 0, "reconcile",
		"--dry-run", "--evidence-sample-full")
	drySet, dryAuthoring := tuplesFromDryRun(t, dryEnv)

	// Zero-write invariants.
	if eventsAfter := countEvents(t, dbPath); eventsAfter != eventsBefore {
		t.Fatalf("dry-run wrote events: before=%d after=%d", eventsBefore, eventsAfter)
	}
	if !reflect.DeepEqual(snap, snapshotBytes(t, dbPath)) {
		t.Fatalf("dry-run mutated state.db bytes (expected zero writes)")
	}

	// -------------------------------------------------------------------
	// RESTORE the snapshot. The blobs on disk are unchanged (dry-run
	// never writes them), so rule 3 will fire again with identical
	// {evidence_id, reason} on the real run.
	// -------------------------------------------------------------------
	restoreBytes(t, dbPath, snap)

	// Watermark: the highest event id in the restored DB. Events emitted
	// by the real reconcile will all have id > lastPreID (SQLite's
	// AUTOINCREMENT never reuses ids, and the restore put us back in the
	// pre-reconcile state). Using event.id (monotonic int) as the
	// watermark avoids ambiguity from same-ms events and from the CLI's
	// `events since` taking a timestamp rather than an id.
	lastPreID := lastEventID(t, dbPath)

	// -------------------------------------------------------------------
	// REAL RUN: mutates + emits events. Parse the newly-emitted events
	// into the same tuple shape.
	// -------------------------------------------------------------------
	runCairnExit(t, repo, cairnHome, 0, "reconcile", "--evidence-sample-full")

	evEnv := runCairnExit(t, repo, cairnHome, 0, "events", "since", "0", "--limit", "500")
	allEvs, _ := evEnv["data"].(map[string]any)["events"].([]any)
	realSet, realAuthoring := tuplesFromEvents(t, allEvs, lastPreID)

	// -------------------------------------------------------------------
	// PARITY: dry tuples == real tuples.
	// -------------------------------------------------------------------
	if !reflect.DeepEqual(sortedTuples(drySet), sortedTuples(realSet)) {
		t.Fatalf("tuple parity violated\ndry:  %v\nreal: %v",
			sortedTuples(drySet), sortedTuples(realSet))
	}

	// Rule 5 authoring errors: same count and same (task_id, missing_gate_id)
	// pairs. Rule 5 is read-only so the real run surfaces them in
	// reconcile_ended.payload.authoring_errors.
	if !reflect.DeepEqual(sortedAuthoring(dryAuthoring), sortedAuthoring(realAuthoring)) {
		t.Fatalf("rule 5 authoring parity violated\ndry:  %v\nreal: %v",
			dryAuthoring, realAuthoring)
	}

	// Sanity: we expected the test to actually drive all four rules.
	// A collapse to zero tuples would silently pass the reflect.DeepEqual.
	wantRules := map[int]bool{1: true, 2: true, 3: true}
	for _, tup := range drySet {
		delete(wantRules, tup.Rule)
	}
	if len(wantRules) != 0 {
		t.Fatalf("dry-run missed rules %v; drySet=%v", keysOf(wantRules), drySet)
	}
	if len(dryAuthoring) != 1 || dryAuthoring[0].TaskID != "TASK-RULE5" ||
		dryAuthoring[0].MissingGateID != "AC-BOGUS" {
		t.Fatalf("dry authoring errors not as expected: %+v", dryAuthoring)
	}
}

// -----------------------------------------------------------------------
// Snapshot / restore helpers.
// -----------------------------------------------------------------------

// snapshotBytes forces a WAL checkpoint (TRUNCATE) so every committed page
// lives in the main DB file, then returns its bytes. Without the checkpoint,
// the main file + the WAL together form the logical state; a bare os.ReadFile
// of the main file would miss pages still in the WAL, and the later
// restore → reopen → WAL-replay would resurrect those pages and desync the
// snapshot.
func snapshotBytes(t *testing.T, path string) []byte {
	t.Helper()
	h, err := db.Open(path)
	if err != nil {
		t.Fatalf("snapshot open %q: %v", path, err)
	}
	if _, err := h.SQL().Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		h.Close()
		t.Fatalf("snapshot checkpoint %q: %v", path, err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("snapshot close %q: %v", path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("snapshot read %q: %v", path, err)
	}
	return data
}

func restoreBytes(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("restore write %q: %v", path, err)
	}
	// WAL / SHM files may hold pages the DB-proper doesn't see. Nuke both
	// so the next CLI invocation re-opens a clean file matching `data`.
	_ = os.Remove(path + "-wal")
	_ = os.Remove(path + "-shm")
}

func countEvents(t *testing.T, dbPath string) int {
	t.Helper()
	h, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer h.Close()
	var n int
	if err := h.SQL().QueryRow(`SELECT COUNT(*) FROM events`).Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	return n
}

func lastEventID(t *testing.T, dbPath string) int64 {
	t.Helper()
	h, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer h.Close()
	var id sql.NullInt64
	if err := h.SQL().QueryRow(`SELECT MAX(id) FROM events`).Scan(&id); err != nil {
		t.Fatalf("max event id: %v", err)
	}
	if !id.Valid {
		return 0
	}
	return id.Int64
}

// -----------------------------------------------------------------------
// Tuple shape + extraction.
// -----------------------------------------------------------------------

type mutTuple struct {
	Rule     int
	EntityID string
	Action   string
	Reason   string
}

type authTuple struct {
	TaskID        string
	MissingGateID string
}

// tuplesFromDryRun pulls would_mutate entries and rule-5 authoring errors
// from the dry-run envelope.
func tuplesFromDryRun(t *testing.T, env map[string]any) ([]mutTuple, []authTuple) {
	t.Helper()
	data, _ := env["data"].(map[string]any)
	if data == nil {
		t.Fatalf("dry-run: no data in envelope: %+v", env)
	}
	if dr, _ := data["dry_run"].(bool); !dr {
		t.Fatalf("dry-run: data.dry_run != true; env=%+v", env)
	}
	rules, _ := data["rules"].([]any)
	var muts []mutTuple
	var auth []authTuple
	for _, raw := range rules {
		r, _ := raw.(map[string]any)
		if r == nil {
			continue
		}
		if wm, ok := r["would_mutate"].([]any); ok {
			for _, m := range wm {
				mm, _ := m.(map[string]any)
				if mm == nil {
					continue
				}
				muts = append(muts, mutTuple{
					Rule:     int(asFloat(mm["rule"])),
					EntityID: asString(mm["entity_id"]),
					Action:   asString(mm["action"]),
					Reason:   asString(mm["reason"]),
				})
			}
		}
		if ae, ok := r["authoring_errors"].([]any); ok {
			for _, e := range ae {
				em, _ := e.(map[string]any)
				if em == nil {
					continue
				}
				auth = append(auth, authTuple{
					TaskID:        asString(em["task_id"]),
					MissingGateID: asString(em["missing_gate_id"]),
				})
			}
		}
	}
	return muts, auth
}

// tuplesFromEvents converts the subset of events with id > afterID into the
// parity tuples. Also returns the authoring_errors from reconcile_ended
// (rule 5 emits no per-entity events — its findings live in the summary
// payload).
func tuplesFromEvents(t *testing.T, evs []any, afterID int64) ([]mutTuple, []authTuple) {
	t.Helper()
	var muts []mutTuple
	var auth []authTuple
	for _, raw := range evs {
		e, _ := raw.(map[string]any)
		if e == nil {
			continue
		}
		id := int64(asFloat(e["ID"]))
		if id <= afterID {
			continue
		}
		kind, _ := e["Kind"].(string)
		entityID, _ := e["EntityID"].(string)
		payload, _ := e["Payload"].(map[string]any)
		switch kind {
		case "claim_released":
			// entity_id is the claim_id; reason comes from payload.reason.
			// Rule 1 emits this ONLY for expired claims it released itself.
			reason := asString(payload["reason"])
			muts = append(muts, mutTuple{
				Rule: 1, EntityID: entityID, Action: "release", Reason: reason,
			})
		case "task_status_changed":
			to := asString(payload["to"])
			reason := asString(payload["reason"])
			switch {
			case to == "open" && reason == "lease_expired":
				muts = append(muts, mutTuple{
					Rule: 1, EntityID: entityID, Action: "revert_to_open", Reason: "lease_expired",
				})
			case to == "stale" && reason == "spec_drift":
				muts = append(muts, mutTuple{
					Rule: 2, EntityID: entityID, Action: "flip_stale", Reason: "spec_drift",
				})
			}
			// Other task_status_changed events (claim/complete/etc.) are
			// from the seed phase; they're filtered out by afterID.
		case "evidence_invalidated":
			muts = append(muts, mutTuple{
				Rule: 3, EntityID: entityID, Action: "invalidate",
				Reason: asString(payload["reason"]),
			})
		case "run_ended":
			// Only reconcile's orphan sweep tags runs with outcome=orphaned;
			// normal task-complete paths set outcome=done. Kept here so a
			// future rule-4 seed flips parity without code changes.
			if asString(payload["outcome"]) == "orphaned" {
				muts = append(muts, mutTuple{
					Rule: 4, EntityID: entityID, Action: "orphan",
					Reason: asString(payload["reason"]),
				})
			}
		case "reconcile_ended":
			// Rule 5 authoring errors live in the summary payload.
			ae, _ := payload["authoring_errors"].([]any)
			for _, item := range ae {
				em, _ := item.(map[string]any)
				if em == nil {
					continue
				}
				auth = append(auth, authTuple{
					TaskID:        asString(em["task_id"]),
					MissingGateID: asString(em["missing_gate_id"]),
				})
			}
		}
	}
	return muts, auth
}

func sortedTuples(s []mutTuple) []mutTuple {
	out := append([]mutTuple(nil), s...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Rule != out[j].Rule {
			return out[i].Rule < out[j].Rule
		}
		if out[i].EntityID != out[j].EntityID {
			return out[i].EntityID < out[j].EntityID
		}
		if out[i].Action != out[j].Action {
			return out[i].Action < out[j].Action
		}
		return out[i].Reason < out[j].Reason
	})
	return out
}

func sortedAuthoring(s []authTuple) []authTuple {
	out := append([]authTuple(nil), s...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].TaskID != out[j].TaskID {
			return out[i].TaskID < out[j].TaskID
		}
		return out[i].MissingGateID < out[j].MissingGateID
	})
	return out
}

// -----------------------------------------------------------------------
// Seed helpers.
// -----------------------------------------------------------------------

// writeReqThreeGates writes specs/requirements/REQ-001.yaml with three gates
// AC-001..AC-003. `ac002Arg` customizes AC-002's echo arg so re-writing with
// a different value changes AC-002's gate_def_hash without touching the
// other two gates.
func writeReqThreeGates(t *testing.T, repo, ac002Arg string) {
	t.Helper()
	body := `id: REQ-001
title: parity
why: parity
gates:
  - id: AC-001
    kind: test
    producer:
      kind: executable
      config:
        command: [echo, ac001]
        pass_on_exit_code: 0
  - id: AC-002
    kind: test
    producer:
      kind: executable
      config:
        command: [echo, ` + ac002Arg + `]
        pass_on_exit_code: 0
  - id: AC-003
    kind: test
    producer:
      kind: executable
      config:
        command: [echo, ac003]
        pass_on_exit_code: 0
`
	if err := os.WriteFile(filepath.Join(repo, "specs", "requirements", "REQ-001.yaml"),
		[]byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeTask(t *testing.T, repo, id, gateID string) {
	t.Helper()
	body := `id: ` + id + `
implements: [REQ-001]
required_gates: [` + gateID + `]
`
	if err := os.WriteFile(filepath.Join(repo, "specs", "tasks", id+".yaml"),
		[]byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// completeTask drives the claim → evidence → verdict → complete lifecycle
// for one task/gate pair. Uses the given blob path as the evidence file.
func completeTask(t *testing.T, repo, cairnHome, taskID, gateID, blobPath string) {
	t.Helper()
	env := runCairnExit(t, repo, cairnHome, 0, "task", "claim", taskID,
		"--agent", "parity", "--ttl", "30m")
	data, _ := env["data"].(map[string]any)
	claimID, _ := data["claim_id"].(string)
	runID, _ := data["run_id"].(string)
	if claimID == "" || runID == "" {
		t.Fatalf("claim %s: missing ids: %+v", taskID, data)
	}
	runCairnExit(t, repo, cairnHome, 0, "evidence", "put", blobPath)
	runCairnExit(t, repo, cairnHome, 0, "verdict", "report",
		"--gate", gateID, "--run", runID, "--status", "pass",
		"--evidence", blobPath,
		"--producer-hash", strings.Repeat("a", 64),
		"--inputs-hash", strings.Repeat("b", 64),
	)
	runCairnExit(t, repo, cairnHome, 0, "task", "complete", claimID)
}

// mustEvidenceSha does `evidence put` (idempotent if already present) and
// returns the sha256 from its envelope.
func mustEvidenceSha(t *testing.T, repo, cairnHome, blobPath string) string {
	t.Helper()
	env := runCairnExit(t, repo, cairnHome, 0, "evidence", "put", blobPath)
	data, _ := env["data"].(map[string]any)
	sha, _ := data["sha256"].(string)
	if len(sha) != 64 {
		t.Fatalf("evidence put: sha missing/short: %+v", env)
	}
	return sha
}

// insertRule5Task directly INSERTs a task row whose required_gates references
// a gate id that never existed. `cairn task plan` rejects this via spec
// validation, so direct SQL is the only way to seed it from a test.
func insertRule5Task(t *testing.T, dbPath string) {
	t.Helper()
	h, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer h.Close()
	_, err = h.SQL().Exec(`
		INSERT INTO tasks (id, requirement_id, spec_path, spec_hash,
			depends_on_json, required_gates_json, status,
			created_at, updated_at)
		VALUES ('TASK-RULE5', 'REQ-001', 'synthetic', 'synthetic',
			'[]', '["AC-BOGUS"]', 'open', 0, 0)
	`)
	if err != nil {
		t.Fatalf("insert rule-5 task: %v", err)
	}
}

// -----------------------------------------------------------------------
// Small JSON coercion helpers.
// -----------------------------------------------------------------------

func asString(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	default:
		return ""
	}
}

func asFloat(v any) float64 {
	if v == nil {
		return 0
	}
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	default:
		return 0
	}
}

func keysOf(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}
