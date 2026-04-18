package clock

import "sync"

// Fake is a deterministic clock for tests. Safe for concurrent use.
type Fake struct {
	mu  sync.Mutex
	now int64
}

// NewFake returns a Fake starting at the given ms.
func NewFake(startMilli int64) *Fake { return &Fake{now: startMilli} }

func (f *Fake) NowMilli() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Advance moves the clock by delta milliseconds; negative delta moves it backward.
func (f *Fake) Advance(deltaMilli int64) {
	f.mu.Lock()
	f.now += deltaMilli
	f.mu.Unlock()
}

// Set overwrites the current time.
func (f *Fake) Set(milli int64) {
	f.mu.Lock()
	f.now = milli
	f.mu.Unlock()
}
