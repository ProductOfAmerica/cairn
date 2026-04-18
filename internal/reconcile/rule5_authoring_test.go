package reconcile_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/reconcile"
)

func TestRule5_FindsMissingGateRefs(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()

	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, _ = tx.Exec(`INSERT INTO requirements (id, spec_path, spec_hash, created_at, updated_at)
		                VALUES ('REQ-1','p','h',0,0)`)
		// Only AC-1 exists; task references AC-1 + AC-BOGUS.
		_, _ = tx.Exec(`INSERT INTO gates (id, requirement_id, kind, definition_json,
		                  gate_def_hash, producer_kind, producer_config)
		                  VALUES ('AC-1','REQ-1','test','{}',
		                          '0000000000000000000000000000000000000000000000000000000000000001',
		                          'executable','{}')`)
		_, _ = tx.Exec(`INSERT INTO tasks (id, requirement_id, spec_path, spec_hash,
		                  depends_on_json, required_gates_json, status, created_at, updated_at)
		                  VALUES ('T-1','REQ-1','p','h','[]',
		                          '["AC-1","AC-BOGUS"]','open',0,0)`)
		_, _ = tx.Exec(`INSERT INTO tasks (id, requirement_id, spec_path, spec_hash,
		                  depends_on_json, required_gates_json, status, created_at, updated_at)
		                  VALUES ('T-2','REQ-1','p','h','[]','["AC-1"]','open',0,0)`)
		return nil
	})

	var errs []reconcile.AuthoringError
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		var err error
		errs, err = reconcile.Rule5CollectAuthoringErrors(tx)
		return err
	})
	if len(errs) != 1 {
		t.Fatalf("authoring errors = %d, want 1: %+v", len(errs), errs)
	}
	if errs[0].TaskID != "T-1" || errs[0].MissingGateID != "AC-BOGUS" {
		t.Errorf("unexpected: %+v", errs[0])
	}
}

func TestRule5_EmitsNoEvents(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	h, _ := db.Open(p)
	defer h.Close()

	// Seed a case with one authoring error.
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, _ = tx.Exec(`INSERT INTO requirements (id, spec_path, spec_hash, created_at, updated_at)
		                VALUES ('REQ-1','p','h',0,0)`)
		_, _ = tx.Exec(`INSERT INTO tasks (id, requirement_id, spec_path, spec_hash,
		                  depends_on_json, required_gates_json, status, created_at, updated_at)
		                  VALUES ('T-1','REQ-1','p','h','[]','["AC-BOGUS"]','open',0,0)`)
		return nil
	})

	var before, after int
	_ = h.SQL().QueryRow(`SELECT COUNT(*) FROM events`).Scan(&before)
	_ = h.WithTx(context.Background(), func(tx *db.Tx) error {
		_, err := reconcile.Rule5CollectAuthoringErrors(tx)
		return err
	})
	_ = h.SQL().QueryRow(`SELECT COUNT(*) FROM events`).Scan(&after)
	if before != after {
		t.Errorf("rule 5 must emit no events; count changed %d -> %d", before, after)
	}
}
