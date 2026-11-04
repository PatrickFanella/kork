package server

import (
	"sync"
	"time"
)

// limiter is a fixed-window counter keyed by an arbitrary string (IP, email).
// It is per-process; run one replica or put a shared limiter in front.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string]*window
}

type window struct {
	start time.Time
	n     int
}

// limiterMaxKeys caps distinct keys per limiter. Keys come from client
// addresses and request fields, so a flood of new keys must not grow memory
// without bound.
const limiterMaxKeys = 50_000

func newLimiter(max int, per time.Duration) *limiter {
	return &limiter{max: max, window: per, hits: map[string]*window{}}
}

func (l *limiter) allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.hits) >= limiterMaxKeys {
		for k, w := range l.hits {
			if now.Sub(w.start) > l.window {
				delete(l.hits, k)
			}
		}
		// Still full of live windows: drop some so memory stays bounded. The
		// dropped keys get a fresh window, which is the lesser harm.
		for k := range l.hits {
			if len(l.hits) < limiterMaxKeys {
				break
			}
			delete(l.hits, k)
		}
	}
	w, ok := l.hits[key]
	if !ok || now.Sub(w.start) > l.window {
		l.hits[key] = &window{start: now, n: 1}
		return true
	}
	if w.n >= l.max {
		return false
	}
	w.n++
	return true
}
