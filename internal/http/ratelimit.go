package http

import (
	"sync"
	"time"
)

// slidingLimiter is an in-process sliding-window rate limiter. Good enough
// for the current single-replica deployment; if the API ever scales
// horizontally, per-replica limits multiply by the replica count and this
// should move to a shared store — flag that at scale-out time, don't
// silently accept it.
type slidingLimiter struct {
	limit  int
	window time.Duration

	mu        sync.Mutex
	hits      map[string][]time.Time
	lastSweep time.Time
}

func newSlidingLimiter(limit int, window time.Duration) *slidingLimiter {
	return &slidingLimiter{limit: limit, window: window, hits: make(map[string][]time.Time), lastSweep: time.Now()}
}

// allow records an attempt for key and reports whether it is within the
// limit. Prunes the key's expired entries on each call, and sweeps the
// whole map once per window so keys an attacker touches once (e.g. spraying
// distinct phone numbers) don't accumulate forever.
func (l *slidingLimiter) allow(key string) bool {
	now := time.Now()
	cutoff := now.Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastSweep) > l.window {
		for k, times := range l.hits {
			live := false
			for _, t := range times {
				if t.After(cutoff) {
					live = true
					break
				}
			}
			if !live {
				delete(l.hits, k)
			}
		}
		l.lastSweep = now
	}

	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.limit {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}
