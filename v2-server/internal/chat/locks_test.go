package chat

import (
	"sync"
	"testing"
)

func TestKeyedLocksExcludeAndLeaveNothingBehind(t *testing.T) {
	var l keyedLocks
	var wg sync.WaitGroup
	inside := map[string]int{}
	var mu sync.Mutex // guards inside, which the keyed lock must already make exclusive per key
	for i := range 200 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := []string{"a", "b"}[i%2]
			release := l.lock(key)
			defer release()
			mu.Lock()
			inside[key]++
			n := inside[key]
			mu.Unlock()
			if n != 1 {
				t.Errorf("%d holders of %q at once", n, key)
			}
			mu.Lock()
			inside[key]--
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(l.entries) != 0 {
		t.Fatalf("released keys must not keep entries: %v", l.entries)
	}
}
