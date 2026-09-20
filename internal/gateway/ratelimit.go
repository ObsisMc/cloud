package gateway

import (
	"sync"
	"time"
)

// RateLimiter is a per-key token bucket for the unauthenticated login entry points. It is
// per-process by design: the ADR requires that a limit exists and rejects before any attempt row is
// written; exact global fairness across replicas is not a goal. The key table is bounded so an
// attacker cannot grow memory by rotating keys.
type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	rate     float64 // tokens per second
	burst    float64
	maxKeys  int
	now      func() time.Time
	lastSwep time.Time
}

type bucket struct {
	tokens float64
	seen   time.Time
}

// NewRateLimiter allows perMinute sustained requests per key with the given burst.
func NewRateLimiter(perMinute, burst, maxKeys int, now func() time.Time) *RateLimiter {
	if now == nil {
		now = time.Now
	}
	return &RateLimiter{buckets: map[string]*bucket{}, rate: float64(perMinute) / 60, burst: float64(burst), maxKeys: maxKeys, now: now, lastSwep: now()}
}

// Allow consumes one token for key and reports whether the request may proceed.
func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= l.maxKeys {
			// Refuse rather than evict a live bucket: exceeding the key table is itself abuse pressure.
			return false
		}
		b = &bucket{tokens: l.burst}
		l.buckets[key] = b
	} else {
		b.tokens += now.Sub(b.seen).Seconds() * l.rate
		if b.tokens > l.burst {
			b.tokens = l.burst
		}
	}
	b.seen = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// sweep drops buckets that have fully refilled; they carry no information any more.
func (l *RateLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSwep) < time.Minute {
		return
	}
	l.lastSwep = now
	for key, b := range l.buckets {
		if now.Sub(b.seen).Seconds()*l.rate >= l.burst {
			delete(l.buckets, key)
		}
	}
}
