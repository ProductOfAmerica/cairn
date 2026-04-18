package ids_test

import (
	"testing"
	"time"

	"github.com/ProductOfAmerica/cairn/internal/clock"
	"github.com/ProductOfAmerica/cairn/internal/ids"
)

func TestNewULID_UniqueAcrossManyCalls(t *testing.T) {
	gen := ids.NewGenerator(clock.Wall{})
	seen := map[string]struct{}{}
	for i := 0; i < 10_000; i++ {
		u := gen.ULID()
		if _, dup := seen[u]; dup {
			t.Fatalf("duplicate ULID at i=%d: %s", i, u)
		}
		seen[u] = struct{}{}
	}
}

func TestNewULID_LexicographicallySortable(t *testing.T) {
	gen := ids.NewGenerator(clock.Wall{})
	a := gen.ULID()
	// Windows timer resolution is ~15.6ms; 25ms leaves comfortable margin
	// for scheduler jitter so two UnixMilli() reads fall in different ms buckets.
	time.Sleep(25 * time.Millisecond)
	b := gen.ULID()
	if a >= b {
		t.Fatalf("expected a<b lexicographically; a=%s b=%s", a, b)
	}
}

func TestNewULID_FixedLen(t *testing.T) {
	gen := ids.NewGenerator(clock.Wall{})
	u := gen.ULID()
	if len(u) != 26 {
		t.Fatalf("ULID is 26 chars, got %d (%s)", len(u), u)
	}
}
