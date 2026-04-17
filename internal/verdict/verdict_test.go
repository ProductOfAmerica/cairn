package verdict_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/evidence"
	"github.com/ProductOfAmerica/cairn/internal/ids"
	"github.com/ProductOfAmerica/cairn/internal/verdict"
)

// openDB creates a temp-file SQLite database for tests.
func openDB(t *testing.T) *db.DB {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "cairn-test-*.db")
	if err != nil {
		t.Fatalf("create temp db file: %v", err)
	}
	path := f.Name()
	f.Close()

	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// seedFixture inserts the minimum rows needed for a Report call:
//   - 1 requirement
//   - 1 gate (with a valid 64-char hex gate_def_hash)
//   - 1 task
//   - 1 claim
//   - 1 run (not yet ended)
//
// This is a test-only SQL fixture; real callers use their respective stores.
func seedFixture(t *testing.T, d *db.DB) (gateID, runID string) {
	t.Helper()
	gateID = "gate-001"
	runID = "run-001"
	const gateDefHash = "abc123def456000000000000000000000000000000000000000000000000abcd"
	const now = int64(1_000_000)

	err := d.WithTx(context.Background(), func(tx *db.Tx) error {
		stmts := []struct {
			query string
			args  []any
		}{
			{
				`INSERT INTO requirements (id, spec_path, spec_hash, created_at, updated_at)
				 VALUES ('req-001', 'specs/req-001.yaml', 'aabbcc', ?, ?)`,
				[]any{now, now},
			},
			{
				`INSERT INTO gates (id, requirement_id, kind, definition_json,
				     gate_def_hash, producer_kind, producer_config)
				 VALUES (?, 'req-001', 'test', '{}', ?, 'executable', '{}')`,
				[]any{gateID, gateDefHash},
			},
			{
				`INSERT INTO tasks (id, requirement_id, spec_path, spec_hash,
				     depends_on_json, required_gates_json, status, created_at, updated_at)
				 VALUES ('task-001', 'req-001', 'specs/task-001.yaml', 'ccbbaa', '[]', '[]',
				     'in_progress', ?, ?)`,
				[]any{now, now},
			},
			{
				`INSERT INTO claims (id, task_id, agent_id, acquired_at, expires_at, op_id)
				 VALUES ('claim-001', 'task-001', 'agent-a', ?, ?, 'op-seed-001')`,
				[]any{now, now + 60_000},
			},
			{
				`INSERT INTO runs (id, task_id, claim_id, started_at)
				 VALUES (?, 'task-001', 'claim-001', ?)`,
				[]any{runID, now},
			},
		}
		for _, s := range stmts {
			if _, err := tx.Exec(s.query, s.args...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed fixture: %v", err)
	}
	return gateID, runID
}

// putEvidence writes a temp file, stores it via evidence.Store, and returns
// the sha256 of the content.
func putEvidence(t *testing.T, d *db.DB, blobRoot string, content []byte) string {
	t.Helper()
	clk := clock.NewFake(1_000_000)
	f, err := os.CreateTemp(t.TempDir(), "evidence-*.bin")
	if err != nil {
		t.Fatalf("create temp evidence file: %v", err)
	}
	if _, err := f.Write(content); err != nil {
		t.Fatalf("write evidence content: %v", err)
	}
	f.Close()

	var sha string
	err = d.WithTx(context.Background(), func(tx *db.Tx) error {
		ev := evidence.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), blobRoot, clk)
		res, err := ev.Put("op-ev-001", f.Name(), "application/octet-stream")
		sha = res.SHA256
		return err
	})
	if err != nil {
		t.Fatalf("put evidence: %v", err)
	}
	return sha
}

// seed creates a minimal fixture for verdict tests: db, runID, gateID, evidence SHA256,
// blob root, and clock. Returns (h, runID, gateID, evSha, blobRoot, clk).
func seed(t *testing.T) (*db.DB, string, string, string, string, clock.Clock) {
	t.Helper()
	h := openDB(t)
	blobRoot := filepath.Join(t.TempDir(), "blobs")
	gateID, runID := seedFixture(t, h)
	evSha := putEvidence(t, h, blobRoot, []byte("test evidence for verdict"))
	clk := clock.NewFake(2_000_000)
	return h, runID, gateID, evSha, blobRoot, clk
}

func TestReport_HappyPath(t *testing.T) {
	d := openDB(t)
	blobRoot := filepath.Join(t.TempDir(), "blobs")

	gateID, runID := seedFixture(t, d)
	sha := putEvidence(t, d, blobRoot, []byte("test evidence for verdict"))

	const (
		producerHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		inputsHash   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)

	clk := clock.NewFake(2_000_000)
	appender := events.NewAppender(clk)
	gen := ids.NewGenerator(clk)

	var res verdict.ReportResult
	err := d.WithTx(context.Background(), func(tx *db.Tx) error {
		ev := evidence.NewStore(tx, appender, gen, blobRoot, clk)
		vs := verdict.NewStore(tx, appender, gen, ev, clk)
		var reportErr error
		res, reportErr = vs.Report(verdict.ReportInput{
			OpID:         "op-verdict-001",
			GateID:       gateID,
			RunID:        runID,
			Status:       "pass",
			Sha256:       sha,
			ProducerHash: producerHash,
			InputsHash:   inputsHash,
			ScoreJSON:    `{"score":1.0}`,
		})
		return reportErr
	})
	if err != nil {
		t.Fatalf("Report: %v", err)
	}

	if res.VerdictID == "" {
		t.Error("VerdictID must not be empty")
	}
	if res.Sequence != 1 {
		t.Errorf("Sequence: got %d, want 1", res.Sequence)
	}
	if res.GateID != gateID {
		t.Errorf("GateID: got %q, want %q", res.GateID, gateID)
	}
	if res.RunID != runID {
		t.Errorf("RunID: got %q, want %q", res.RunID, runID)
	}
	if res.Status != "pass" {
		t.Errorf("Status: got %q, want %q", res.Status, "pass")
	}
	if res.BoundAt == 0 {
		t.Error("BoundAt must not be zero")
	}
}

func TestLatest_ReturnsMostRecent(t *testing.T) {
	h, runID, gateID, evSha, _, clk := seed(t)
	report := func(opID, status string) {
		_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
			store := verdict.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk),
				evidence.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), "", clk), clk)
			_, err := store.Report(verdict.ReportInput{
				OpID: opID, GateID: gateID, RunID: runID, Status: status,
				Sha256:       evSha,
				ProducerHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				InputsHash:   "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			})
			return err
		})
	}
	report("01HNBXBT9J6MGK3Z5R7WVXTM01", "fail")
	report("01HNBXBT9J6MGK3Z5R7WVXTM02", "pass")

	var got verdict.LatestResult
	err := h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := verdict.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk),
			evidence.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), "", clk), clk)
		r, err := store.Latest(gateID)
		got = r
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict == nil || got.Verdict.Status != "pass" {
		t.Fatalf("latest should be pass, got: %+v", got)
	}
	if !got.Fresh {
		t.Fatalf("latest pass with matching gate_def_hash should be fresh")
	}
}

func TestLatest_StaleOnGateHashChange(t *testing.T) {
	h, runID, gateID, evSha, _, clk := seed(t)
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := verdict.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk),
			evidence.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), "", clk), clk)
		_, err := store.Report(verdict.ReportInput{
			OpID: "01HNBXBT9J6MGK3Z5R7WVXTM03", GateID: gateID, RunID: runID, Status: "pass",
			Sha256:       evSha,
			ProducerHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			InputsHash:   "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		})
		return err
	})
	// Mutate gate_def_hash out-of-band to simulate spec drift.
	_, _ = h.SQL().Exec(
		"UPDATE gates SET gate_def_hash=? WHERE id=?",
		"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", gateID,
	)
	var got verdict.LatestResult
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := verdict.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk),
			evidence.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), "", clk), clk)
		r, _ := store.Latest(gateID)
		got = r
		return nil
	})
	if got.Fresh {
		t.Fatalf("verdict should be stale after gate_def_hash mutation")
	}
}
