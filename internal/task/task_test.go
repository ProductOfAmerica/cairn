package task_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/ids"
	"github.com/ProductOfAmerica/cairn/internal/task"
)

func openDB(t *testing.T) *db.DB {
	t.Helper()
	p := filepath.Join(t.TempDir(), "state.db")
	h, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	return h
}

func TestOpLog_HitReturnsCached(t *testing.T) {
	h := openDB(t)
	clk := clock.NewFake(1)
	opID := "01HNBXBT9J6MGK3Z5R7WVXTM0A"

	// First record: no hit.
	var firstResult struct{ V int }
	err := h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := task.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		cached, hit, err := store.CheckOpLog(opID, "task.claim")
		if err != nil {
			t.Fatal(err)
		}
		if hit {
			t.Fatal("expected miss on first call")
		}
		_ = cached
		// Write our sentinel.
		payload, _ := json.Marshal(struct{ V int }{V: 42})
		firstResult.V = 42
		return store.RecordOpLog(opID, "task.claim", payload)
	})
	if err != nil {
		t.Fatal(err)
	}

	// Second call with same opID: must hit.
	err = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := task.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		cached, hit, err := store.CheckOpLog(opID, "task.claim")
		if err != nil {
			t.Fatal(err)
		}
		if !hit {
			t.Fatal("expected hit on replay")
		}
		var got struct{ V int }
		_ = json.Unmarshal(cached, &got)
		if got.V != 42 {
			t.Fatalf("cached result mismatch: %+v", got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpLog_KindMismatchIsConflict(t *testing.T) {
	h := openDB(t)
	clk := clock.NewFake(1)
	opID := "01HNBXBT9J6MGK3Z5R7WVXTM0B"
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := task.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		return store.RecordOpLog(opID, "task.claim", []byte(`{}`))
	})
	err := h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := task.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		_, _, err := store.CheckOpLog(opID, "task.heartbeat")
		return err
	})
	if err == nil {
		t.Fatal("kind mismatch should error")
	}
}

func TestList_FilterByStatus(t *testing.T) {
	h := openDB(t)
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, _ = tx.Exec(`INSERT INTO requirements (id, spec_path, spec_hash, created_at, updated_at)
		                VALUES ('REQ-1','p','h',0,0)`)
		_, _ = tx.Exec(`INSERT INTO tasks (id, requirement_id, spec_path, spec_hash,
		                 depends_on_json, required_gates_json, status, created_at, updated_at)
		                 VALUES ('T-A','REQ-1','p','h','[]','[]','open',0,0),
		                        ('T-B','REQ-1','p','h','[]','[]','done',0,0)`)
		return nil
	})

	clk := clock.NewFake(1)
	var openOnly []task.TaskRow
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := task.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		list, err := store.List("open")
		openOnly = list
		return err
	})
	if len(openOnly) != 1 || openOnly[0].ID != "T-A" {
		t.Fatalf("unexpected list: %+v", openOnly)
	}
}

func seedClaimable(t *testing.T, h *db.DB, id string, deps []string) {
	t.Helper()
	depsJSON, _ := json.Marshal(deps)
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, _ = tx.Exec(`INSERT OR IGNORE INTO requirements
		                 (id, spec_path, spec_hash, created_at, updated_at)
		                 VALUES ('REQ-1','p','h',0,0)`)
		_, err := tx.Exec(`INSERT INTO tasks (id, requirement_id, spec_path, spec_hash,
		                   depends_on_json, required_gates_json, status,
		                   created_at, updated_at)
		                   VALUES (?,'REQ-1','p','h',?,'[]','open',0,0)`,
			id, string(depsJSON))
		return err
	})
}

func TestClaim_HappyPath(t *testing.T) {
	h := openDB(t)
	seedClaimable(t, h, "T-1", nil)

	clk := clock.NewFake(1_000_000)
	var res task.ClaimResult
	err := h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := task.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		r, err := store.Claim(task.ClaimInput{
			OpID:    "01HNBXBT9J6MGK3Z5R7WVXTM0A",
			TaskID:  "T-1",
			AgentID: "agent-1",
			TTLMs:   30 * 60 * 1000,
		})
		res = r
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ClaimID == "" || res.RunID == "" {
		t.Fatalf("bad result: %+v", res)
	}
	if res.ExpiresAt != 1_000_000+30*60*1000 {
		t.Fatalf("expires_at wrong: %d", res.ExpiresAt)
	}

	// Verify task flipped to claimed.
	var status string
	_ = h.SQL().QueryRow("SELECT status FROM tasks WHERE id='T-1'").Scan(&status)
	if status != "claimed" {
		t.Fatalf("status=%s", status)
	}
}

func TestClaim_DepNotDone(t *testing.T) {
	h := openDB(t)
	seedClaimable(t, h, "T-dep", nil)
	seedClaimable(t, h, "T-main", []string{"T-dep"})

	clk := clock.NewFake(1_000)
	err := h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := task.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		_, err := store.Claim(task.ClaimInput{
			OpID: "01HNBXBT9J6MGK3Z5R7WVXTM0B", TaskID: "T-main",
			AgentID: "a", TTLMs: 1000,
		})
		return err
	})
	if err == nil {
		t.Fatal("expected dep_not_done")
	}
}

func TestClaim_AlreadyClaimedConflict(t *testing.T) {
	h := openDB(t)
	seedClaimable(t, h, "T-x", nil)

	clk := clock.NewFake(1_000)
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := task.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		_, err := store.Claim(task.ClaimInput{
			OpID: "01HNBXBT9J6MGK3Z5R7WVXTM0C", TaskID: "T-x",
			AgentID: "a", TTLMs: 60_000,
		})
		return err
	})

	err := h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := task.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		_, err := store.Claim(task.ClaimInput{
			OpID: "01HNBXBT9J6MGK3Z5R7WVXTM0D", TaskID: "T-x",
			AgentID: "a", TTLMs: 60_000,
		})
		return err
	})
	if err == nil {
		t.Fatal("second claim should conflict")
	}
}

func TestClaim_OpLogReplayReturnsCached(t *testing.T) {
	h := openDB(t)
	seedClaimable(t, h, "T-y", nil)

	clk := clock.NewFake(1_000)
	opID := "01HNBXBT9J6MGK3Z5R7WVXTM0E"
	var first task.ClaimResult
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := task.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		r, err := store.Claim(task.ClaimInput{OpID: opID, TaskID: "T-y", AgentID: "a", TTLMs: 60_000})
		first = r
		return err
	})
	var second task.ClaimResult
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := task.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		r, err := store.Claim(task.ClaimInput{OpID: opID, TaskID: "T-y", AgentID: "a", TTLMs: 60_000})
		second = r
		return err
	})
	if first.ClaimID != second.ClaimID || first.RunID != second.RunID {
		t.Fatalf("replay did not return cached result: first=%+v second=%+v", first, second)
	}
}

func TestClaim_ExpiredLeaseClearedInline(t *testing.T) {
	h := openDB(t)
	seedClaimable(t, h, "T-z", nil)

	clk := clock.NewFake(1_000)
	// First claim with 1s ttl.
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := task.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		_, err := store.Claim(task.ClaimInput{
			OpID: "01HNBXBT9J6MGK3Z5R7WVXTM0F", TaskID: "T-z", AgentID: "a", TTLMs: 1000,
		})
		return err
	})
	// Advance clock past lease.
	clk.Advance(2000)
	// Second claim — inline rule 1 should flip the old claim released, task back to open.
	err := h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := task.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		_, err := store.Claim(task.ClaimInput{
			OpID: "01HNBXBT9J6MGK3Z5R7WVXTM0G", TaskID: "T-z", AgentID: "b", TTLMs: 60_000,
		})
		return err
	})
	if err != nil {
		t.Fatalf("expected re-claim to succeed after expiry, got: %v", err)
	}
}

func TestHeartbeat_ExtendsExpiry(t *testing.T) {
	h := openDB(t)
	seedClaimable(t, h, "T-hb", nil)

	clk := clock.NewFake(1_000)
	var claim task.ClaimResult
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := task.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		r, err := store.Claim(task.ClaimInput{
			OpID: "01HNBXBT9J6MGK3Z5R7WVXTM01", TaskID: "T-hb", AgentID: "a", TTLMs: 10_000,
		})
		claim = r
		return err
	})
	clk.Advance(5_000)

	var hbRes task.HeartbeatResult
	err := h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := task.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		r, err := store.Heartbeat(task.HeartbeatInput{
			OpID: "01HNBXBT9J6MGK3Z5R7WVXTM02", ClaimID: claim.ClaimID,
		})
		hbRes = r
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// Heartbeat reuses original TTL (10s) from clk.NowMilli()=6000 → expires_at=16000.
	if hbRes.ExpiresAt != 6_000+10_000 {
		t.Fatalf("expires_at=%d", hbRes.ExpiresAt)
	}
}

func TestRelease_FlipsTaskBackToOpen(t *testing.T) {
	h := openDB(t)
	seedClaimable(t, h, "T-rel", nil)

	clk := clock.NewFake(1_000)
	var claim task.ClaimResult
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := task.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		r, err := store.Claim(task.ClaimInput{
			OpID: "01HNBXBT9J6MGK3Z5R7WVXTM03", TaskID: "T-rel", AgentID: "a", TTLMs: 60_000,
		})
		claim = r
		return err
	})
	err := h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := task.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		return store.Release(task.ReleaseInput{
			OpID: "01HNBXBT9J6MGK3Z5R7WVXTM04", ClaimID: claim.ClaimID,
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	var status string
	_ = h.SQL().QueryRow("SELECT status FROM tasks WHERE id='T-rel'").Scan(&status)
	if status != "open" {
		t.Fatalf("status=%s, expected open", status)
	}
}

func TestComplete_GatesNotFreshPassConflict(t *testing.T) {
	h := openDB(t)
	// Seed: requirement + gate + task requiring the gate + claim + run.
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, _ = tx.Exec(`INSERT INTO requirements (id, spec_path, spec_hash, created_at, updated_at)
		                VALUES ('REQ-1','p','h',0,0)`)
		_, _ = tx.Exec(`INSERT INTO gates (id, requirement_id, kind, definition_json,
		                 gate_def_hash, producer_kind, producer_config)
		                 VALUES ('AC-1','REQ-1','test','{}',
		                    'abc123def456000000000000000000000000000000000000000000000000abcd',
		                    'executable','{}')`)
		_, _ = tx.Exec(`INSERT INTO tasks (id, requirement_id, spec_path, spec_hash,
		                 depends_on_json, required_gates_json, status, created_at, updated_at)
		                 VALUES ('T-c','REQ-1','p','h','[]','["AC-1"]','claimed',0,0)`)
		_, _ = tx.Exec(`INSERT INTO claims (id, task_id, agent_id, acquired_at, expires_at, op_id)
		                 VALUES ('CL-c','T-c','a',0,9999999999999,'01HNBXBT9J6MGK3Z5R7WVXTM0X')`)
		_, _ = tx.Exec(`INSERT INTO runs (id, task_id, claim_id, started_at)
		                 VALUES ('RUN-c','T-c','CL-c',0)`)
		return nil
	})
	clk := clock.NewFake(1_000)
	err := h.WithTx(context.Background(), func(tx *db.Tx) error {
		store := task.NewStore(tx, events.NewAppender(clk), ids.NewGenerator(clk), clk)
		_, err := store.Complete(task.CompleteInput{
			OpID: "01HNBXBT9J6MGK3Z5R7WVXTM0Y", ClaimID: "CL-c",
		})
		return err
	})
	if err == nil {
		t.Fatal("complete with no verdicts should conflict")
	}
}
