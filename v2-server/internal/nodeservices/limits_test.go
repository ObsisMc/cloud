package nodeservices

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// streamingProvider writes one SSE chunk every interval for the given duration, then a terminal.
type streamingProvider struct{ interval, length time.Duration }

func (p streamingProvider) Complete(w http.ResponseWriter, r *http.Request, _ Model) {
	w.Header().Set("Content-Type", "text/event-stream")
	for began := time.Now(); time.Since(began) < p.length; {
		if _, err := (flushWriter{w}).Write([]byte("data: {}\n\n")); err != nil {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(p.interval):
		}
	}
	_, _ = flushWriter{w}.Write([]byte("data: [DONE]\n\n"))
}

// An admitted call outlives the expiry its token had when it began, once Node has renewed.
func TestRenewedGrantKeepsInFlightCallPastOriginalExpiry(t *testing.T) {
	const ttl = 600 * time.Millisecond
	s := testService(t, streamingProvider{interval: 50 * time.Millisecond, length: 3 * ttl})
	s.mu.Lock()
	s.grantTTL = ttl
	s.mu.Unlock()
	c := serviceClient(t, s, true)
	token, id := issueToken(t, s, c, "execution")
	began := time.Now()
	renewed := make(chan int, 1)
	go func() {
		time.Sleep(ttl / 2)
		status, _ := request(t, c, "POST", s.URL+"/internal/v1/model-grants/"+id+"/renew", "", `{"executionId":"execution"}`)
		renewed <- status
	}()
	status, body := request(t, c, "POST", s.URL+"/runtime/openai/v1/chat/completions", token, `{}`)
	if status != 200 || !strings.HasSuffix(string(body), "data: [DONE]\n\n") {
		t.Fatalf("in-flight call cut: %d %q", status, body)
	}
	if elapsed := time.Since(began); elapsed < 2*ttl {
		t.Fatalf("call ended after %s, before outliving the original expiry", elapsed)
	}
	if status := <-renewed; status != 200 {
		t.Fatalf("renew=%d", status)
	}
}

