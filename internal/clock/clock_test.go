package clock_test

import (
	"testing"
	"time"

	"github.com/ProductOfAmerica/cairn/internal/clock"
)

func TestWall_NowMilliMonotonicRoughly(t *testing.T) {
	c := clock.Wall{}
	a := c.NowMilli()
	deadline := time.Now().Add(200 * time.Millisecond)
	for {
		if b := c.NowMilli(); b > a {
			return // passed
		}
		if time.Now().After(deadline) {
			t.Fatalf("wall clock did not advance within 200ms")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestFake_NowMilliIsSettable(t *testing.T) {
	f := clock.NewFake(1_000)
	if got := f.NowMilli(); got != 1_000 {
		t.Fatalf("want 1000 got %d", got)
	}
	f.Advance(500)
	if got := f.NowMilli(); got != 1_500 {
		t.Fatalf("want 1500 got %d", got)
	}
	f.Set(42)
	if got := f.NowMilli(); got != 42 {
		t.Fatalf("want 42 got %d", got)
	}
}
