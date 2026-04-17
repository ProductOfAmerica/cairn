// Package ids provides ULID generation and op_id validation.
//
// ULIDs are used for every internal cairn identifier (claim_id, run_id,
// verdict_id, evidence_id, memory_id, etc.). Callers supply op_id; cairn
// validates its shape but does not generate it unless the caller omits it.
package ids

import (
	"crypto/rand"
	"io"
	"sync"

	ulidpkg "github.com/oklog/ulid/v2"

	"github.com/ProductOfAmerica/cairn/internal/clock"
)

// Generator produces ULIDs. Safe for concurrent use.
type Generator struct {
	mu      sync.Mutex
	clock   clock.Clock
	entropy io.Reader
}

// NewGenerator returns a Generator backed by the given clock and crypto/rand.
func NewGenerator(c clock.Clock) *Generator {
	return &Generator{clock: c, entropy: rand.Reader}
}

// ULID returns a new ULID as a Crockford-base32 string (26 chars).
func (g *Generator) ULID() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	ms := uint64(g.clock.NowMilli()) //nolint:gosec // ms is always >=0
	u, err := ulidpkg.New(ms, g.entropy)
	if err != nil {
		panic("ulid: entropy exhausted: " + err.Error())
	}
	return u.String()
}
