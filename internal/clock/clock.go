// Package clock provides a millisecond-resolution clock abstraction.
//
// All cairn timestamps are integer milliseconds since Unix epoch (UTC).
// Production code uses Wall{}; tests inject Fake via clock.Clock.
package clock

import "time"

// Clock is the single source of time.
type Clock interface {
	NowMilli() int64
}

// Wall returns real wall-clock time in milliseconds since Unix epoch.
type Wall struct{}

func (Wall) NowMilli() int64 { return time.Now().UnixMilli() }
