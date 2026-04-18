package task

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ProductOfAmerica/cairn/internal/cairnerr"
	"github.com/ProductOfAmerica/cairn/internal/events"
)

// HeartbeatInput is what the CLI passes.
type HeartbeatInput struct {
	OpID    string
	ClaimID string
}

// HeartbeatResult echoes the new expiry.
type HeartbeatResult struct {
	ExpiresAt int64 `json:"expires_at"`
}

// Heartbeat renews the lease by reusing the original TTL.
func (s *Store) Heartbeat(in HeartbeatInput) (HeartbeatResult, error) {
	cached, hit, err := s.CheckOpLog(in.OpID, "task.heartbeat")
	if err != nil {
		return HeartbeatResult{}, err
	}
	if hit {
		var r HeartbeatResult
		_ = json.Unmarshal(cached, &r)
		return r, nil
	}

	var acquired, oldExpires int64
	var released sql.NullInt64
	err = s.tx.QueryRow(
		`SELECT acquired_at, expires_at, released_at FROM claims WHERE id=?`,
		in.ClaimID,
	).Scan(&acquired, &oldExpires, &released)
	if errors.Is(err, sql.ErrNoRows) {
		return HeartbeatResult{}, cairnerr.New(cairnerr.CodeNotFound, "claim_not_found",
			fmt.Sprintf("claim %q", in.ClaimID))
	}
	if err != nil {
		return HeartbeatResult{}, err
	}
	if released.Valid {
		return HeartbeatResult{}, cairnerr.New(cairnerr.CodeConflict,
			"claim_released_or_expired",
			fmt.Sprintf("claim %q already released", in.ClaimID))
	}
	ttl := oldExpires - acquired
	newExpires := s.clock.NowMilli() + ttl
	result, err := s.tx.Exec(
		`UPDATE claims SET expires_at=? WHERE id=? AND released_at IS NULL`,
		newExpires, in.ClaimID,
	)
	if err != nil {
		return HeartbeatResult{}, err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return HeartbeatResult{}, cairnerr.New(cairnerr.CodeConflict,
			"claim_released_or_expired",
			fmt.Sprintf("claim %q released between read and update", in.ClaimID))
	}
	if err := s.events.Append(s.tx, events.Record{
		Kind: "claim_heartbeat", EntityKind: "claim", EntityID: in.ClaimID,
		Payload: map[string]any{"new_expires_at": newExpires},
		OpID:    in.OpID,
	}); err != nil {
		return HeartbeatResult{}, err
	}

	r := HeartbeatResult{ExpiresAt: newExpires}
	payload, _ := json.Marshal(r)
	if err := s.RecordOpLog(in.OpID, "task.heartbeat", payload); err != nil {
		return HeartbeatResult{}, err
	}
	return r, nil
}
