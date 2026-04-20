// Package verdict owns the verdicts table. A verdict binds a gate evaluation
// result (pass/fail/inconclusive) to a run, gate, and evidence item.
package verdict

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/db"
	"github.com/ProductOfAmerica/cairn/internal/events"
	"github.com/ProductOfAmerica/cairn/internal/evidence"
	"github.com/ProductOfAmerica/cairn/internal/ids"
)

// hashPattern matches a lowercase 64-char hex string (SHA-256).
var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// defaultHistoryLimit is the page size returned by Store.History when no
// explicit limit is provided.
const defaultHistoryLimit = 50

// validStatuses is the set of allowed verdict status values.
var validStatuses = map[string]bool{
	"pass":         true,
	"fail":         true,
	"inconclusive": true,
}

// Store owns the verdicts table. It is bound to an externally-managed
// transaction; the caller opens the txn and Store runs inside it.
type Store struct {
	tx       *db.Tx
	events   events.Appender
	ids      *ids.Generator
	evidence *evidence.Store
	clock    clock.Clock
}

// NewStore returns a Store bound to the given transaction.
func NewStore(tx *db.Tx, a events.Appender, g *ids.Generator, ev *evidence.Store, c clock.Clock) *Store {
	return &Store{tx: tx, events: a, ids: g, evidence: ev, clock: c}
}

// ReportInput is the caller-supplied data for a verdict.
type ReportInput struct {
	OpID         string
	GateID       string
	RunID        string
	Status       string
	Sha256       string
	ProducerHash string
	InputsHash   string
	ScoreJSON    string
}

// ReportResult is the outcome of a successful Report call.
type ReportResult struct {
	VerdictID string `json:"verdict_id"`
	GateID    string `json:"gate_id"`
	RunID     string `json:"run_id"`
	Status    string `json:"status"`
	Sequence  int64  `json:"sequence"`
	BoundAt   int64  `json:"bound_at"`
}

// Report validates the input, re-verifies evidence, reads the gate's
// gate_def_hash from the DB, inserts a verdict row, emits a verdict_bound
// event, and returns the result.
//
// Methods on Store do NOT take context.Context; they operate on s.tx directly.
func (s *Store) Report(in ReportInput) (ReportResult, error) {
	// 1. Validate ProducerHash and InputsHash.
	if !hashPattern.MatchString(in.ProducerHash) {
		return ReportResult{}, cairnerr.New(cairnerr.CodeBadInput, "bad_input",
			"producer_hash must be a 64-char lowercase hex string").
			WithDetails(map[string]any{"flag": "producer_hash"})
	}
	if !hashPattern.MatchString(in.InputsHash) {
		return ReportResult{}, cairnerr.New(cairnerr.CodeBadInput, "bad_input",
			"inputs_hash must be a 64-char lowercase hex string").
			WithDetails(map[string]any{"flag": "inputs_hash"})
	}

	// 2. Validate Status.
	if !validStatuses[in.Status] {
		return ReportResult{}, cairnerr.New(cairnerr.CodeBadInput, "bad_input",
			fmt.Sprintf("status must be one of pass, fail, inconclusive; got %q", in.Status)).
			WithDetails(map[string]any{"flag": "status"})
	}

	// 3. Re-verify evidence via evidence.Store (surfaces not_stored / hash mismatch).
	if err := s.evidence.Verify(in.Sha256); err != nil {
		return ReportResult{}, err
	}

	// 4. SELECT evidence_id by sha256.
	var evidenceID string
	err := s.tx.QueryRow(
		`SELECT id FROM evidence WHERE sha256 = ?`, in.Sha256,
	).Scan(&evidenceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ReportResult{}, cairnerr.New(cairnerr.CodeNotFound, "evidence_not_stored",
				fmt.Sprintf("no evidence row for sha256 %s", in.Sha256))
		}
		return ReportResult{}, fmt.Errorf("query evidence: %w", err)
	}

	// 5. SELECT gate_def_hash from gates WHERE id=?
	var gateDefHash string
	err = s.tx.QueryRow(
		`SELECT gate_def_hash FROM gates WHERE id = ?`, in.GateID,
	).Scan(&gateDefHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ReportResult{}, cairnerr.New(cairnerr.CodeNotFound, "gate_not_found",
				fmt.Sprintf("no gate row for id %s", in.GateID))
		}
		return ReportResult{}, fmt.Errorf("query gate: %w", err)
	}

	// 6. SELECT ended_at from runs WHERE id=?
	var endedAt sql.NullInt64
	err = s.tx.QueryRow(
		`SELECT ended_at FROM runs WHERE id = ?`, in.RunID,
	).Scan(&endedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ReportResult{}, cairnerr.New(cairnerr.CodeNotFound, "run_not_found",
				fmt.Sprintf("no run row for id %s", in.RunID))
		}
		return ReportResult{}, fmt.Errorf("query run: %w", err)
	}
	if endedAt.Valid {
		return ReportResult{}, cairnerr.New(cairnerr.CodeBadInput, "run_already_ended",
			fmt.Sprintf("run %s has already ended", in.RunID))
	}

	// 7. Compute sequence: COALESCE(MAX(sequence), 0) + 1 for this gate.
	var sequence int64
	if err := s.tx.QueryRow(
		`SELECT COALESCE(MAX(sequence), 0) + 1 FROM verdicts WHERE gate_id = ?`, in.GateID,
	).Scan(&sequence); err != nil {
		return ReportResult{}, fmt.Errorf("compute sequence: %w", err)
	}

	// 8. Generate verdict_id.
	verdictID := s.ids.ULID()

	// 9. INSERT INTO verdicts.
	boundAt := s.clock.NowMilli()
	_, err = s.tx.Exec(
		`INSERT INTO verdicts
		     (id, run_id, gate_id, status, score_json, producer_hash,
		      gate_def_hash, inputs_hash, evidence_id, bound_at, sequence)
		 VALUES (?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?)`,
		verdictID, in.RunID, in.GateID, in.Status, in.ScoreJSON,
		in.ProducerHash, gateDefHash, in.InputsHash, evidenceID,
		boundAt, sequence,
	)
	if err != nil {
		return ReportResult{}, fmt.Errorf("insert verdict: %w", err)
	}

	// 10. Emit verdict_bound event.
	if err := s.events.Append(s.tx, events.Record{
		Kind:       "verdict_bound",
		EntityKind: "verdict",
		EntityID:   verdictID,
		OpID:       in.OpID,
		Payload: map[string]any{
			"gate_id":       in.GateID,
			"run_id":        in.RunID,
			"status":        in.Status,
			"gate_def_hash": gateDefHash,
			"producer_hash": in.ProducerHash,
			"inputs_hash":   in.InputsHash,
			"sequence":      sequence,
		},
	}); err != nil {
		return ReportResult{}, err
	}

	// 11. Return result.
	return ReportResult{
		VerdictID: verdictID,
		GateID:    in.GateID,
		RunID:     in.RunID,
		Status:    in.Status,
		Sequence:  sequence,
		BoundAt:   boundAt,
	}, nil
}

// Verdict is the on-disk shape returned by Latest / History.
type Verdict struct {
	ID                  string `json:"verdict_id"`
	RunID               string `json:"run_id"`
	GateID              string `json:"gate_id"`
	Status              string `json:"status"`
	ScoreJSON           string `json:"score_json,omitempty"`
	ProducerHash        string `json:"producer_hash"`
	GateDefHash         string `json:"gate_def_hash"`
	InputsHash          string `json:"inputs_hash"`
	EvidenceID          string `json:"evidence_id,omitempty"`
	BoundAt             int64  `json:"bound_at"`
	Sequence            int64  `json:"sequence"`
	EvidenceInvalidated bool   `json:"evidence_invalidated"`
}

// LatestResult is the envelope shape for `verdict latest`.
type LatestResult struct {
	Verdict *Verdict `json:"verdict"` // nil if no verdicts exist
	Fresh   bool     `json:"fresh"`
}

// Latest returns the latest verdict for a gate, with derived freshness.
func (s *Store) Latest(gateID string) (LatestResult, error) {
	var curGateHash string
	err := s.tx.QueryRow(
		"SELECT gate_def_hash FROM gates WHERE id=?", gateID,
	).Scan(&curGateHash)
	if errors.Is(err, sql.ErrNoRows) {
		return LatestResult{}, cairnerr.New(cairnerr.CodeNotFound, "gate_not_found",
			fmt.Sprintf("gate %q", gateID))
	}
	if err != nil {
		return LatestResult{}, fmt.Errorf("query current gate hash for %s: %w", gateID, err)
	}

	var v Verdict
	var score, evID sql.NullString
	var invalidated sql.NullInt64
	err = s.tx.QueryRow(
		`SELECT v.id, v.run_id, v.gate_id, v.status, v.score_json, v.producer_hash,
                v.gate_def_hash, v.inputs_hash, v.evidence_id, v.bound_at, v.sequence,
                e.invalidated_at
         FROM verdicts v LEFT JOIN evidence e ON e.id = v.evidence_id
         WHERE v.gate_id=?
         ORDER BY v.bound_at DESC, v.sequence DESC LIMIT 1`,
		gateID,
	).Scan(&v.ID, &v.RunID, &v.GateID, &v.Status, &score, &v.ProducerHash,
		&v.GateDefHash, &v.InputsHash, &evID, &v.BoundAt, &v.Sequence, &invalidated)
	if errors.Is(err, sql.ErrNoRows) {
		return LatestResult{Verdict: nil, Fresh: false}, nil
	}
	if err != nil {
		return LatestResult{}, fmt.Errorf("query latest verdict for gate %s: %w", gateID, err)
	}
	if score.Valid {
		v.ScoreJSON = score.String
	}
	if evID.Valid {
		v.EvidenceID = evID.String
	}
	v.EvidenceInvalidated = invalidated.Valid
	fresh := v.GateDefHash == curGateHash && v.Status == "pass"
	return LatestResult{Verdict: &v, Fresh: fresh}, nil
}

// History returns up to limit verdicts for a gate, newest first, each with
// its derived freshness.
func (s *Store) History(gateID string, limit int) ([]VerdictWithFresh, error) {
	if limit <= 0 {
		limit = defaultHistoryLimit
	}
	var curGateHash string
	err := s.tx.QueryRow(
		"SELECT gate_def_hash FROM gates WHERE id=?", gateID,
	).Scan(&curGateHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, cairnerr.New(cairnerr.CodeNotFound, "gate_not_found",
			fmt.Sprintf("gate %q", gateID))
	}
	if err != nil {
		return nil, fmt.Errorf("query current gate hash for %s: %w", gateID, err)
	}

	rows, err := s.tx.Query(
		`SELECT v.id, v.run_id, v.gate_id, v.status, v.score_json, v.producer_hash,
                v.gate_def_hash, v.inputs_hash, v.evidence_id, v.bound_at, v.sequence,
                e.invalidated_at
         FROM verdicts v LEFT JOIN evidence e ON e.id = v.evidence_id
         WHERE v.gate_id=?
         ORDER BY v.bound_at DESC, v.sequence DESC LIMIT ?`,
		gateID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query verdict history for gate %s: %w", gateID, err)
	}
	defer rows.Close()

	var out []VerdictWithFresh
	for rows.Next() {
		var v Verdict
		var score, evID sql.NullString
		var invalidated sql.NullInt64
		if err := rows.Scan(&v.ID, &v.RunID, &v.GateID, &v.Status, &score, &v.ProducerHash,
			&v.GateDefHash, &v.InputsHash, &evID, &v.BoundAt, &v.Sequence, &invalidated); err != nil {
			return nil, fmt.Errorf("scan verdict row: %w", err)
		}
		if score.Valid {
			v.ScoreJSON = score.String
		}
		if evID.Valid {
			v.EvidenceID = evID.String
		}
		v.EvidenceInvalidated = invalidated.Valid
		fresh := v.GateDefHash == curGateHash && v.Status == "pass"
		out = append(out, VerdictWithFresh{Verdict: v, Fresh: fresh})
	}
	return out, rows.Err()
}

// VerdictWithFresh pairs a verdict with its derived freshness flag.
// The Verdict fields are flattened into the JSON object via anonymous embed.
type VerdictWithFresh struct {
	Verdict
	Fresh bool `json:"fresh"`
}

// IsFreshPass returns (true, nil) iff the latest verdict for gateID has
// status=pass AND its gate_def_hash matches the current gate row.
// Called by task.Complete for each required gate.
func (s *Store) IsFreshPass(gateID string) (bool, string, error) {
	r, err := s.Latest(gateID)
	if err != nil {
		return false, "", err
	}
	if r.Verdict == nil {
		return false, "no_verdict", nil
	}
	if r.Fresh {
		return true, "", nil
	}
	if r.Verdict.Status != "pass" {
		return false, "status_not_pass", nil
	}
	return false, "stale", nil
}
