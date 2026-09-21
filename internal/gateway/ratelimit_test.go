package gateway

import (
	"testing"
	"time"
)

func TestRateLimiterBurstRefillAndBoundedKeys(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	l := NewRateLimiter(60, 3, 2, clock)
	for i := range 3 {
		if !l.Allow("a") {
			t.Fatalf("burst request %d must be allowed", i)
		}
	}
	if l.Allow("a") {
		t.Fatal("fourth request within the burst window must be rejected")
	}
	now = now.Add(time.Second)
	if !l.Allow("a") {
		t.Fatal("one token must refill after one second at 60/min")
	}
	if l.Allow("a") {
		t.Fatal("only one token should have refilled")
	}
	if !l.Allow("b") {
		t.Fatal("second key gets its own bucket")
	}
	if l.Allow("c") {
		t.Fatal("key table is bounded; a third live key must be refused rather than evict a live bucket")
	}
	now = now.Add(2 * time.Minute)
	if !l.Allow("c") {
		t.Fatal("after buckets fully refill and are swept, new keys are accepted again")
	}
}
