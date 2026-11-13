package nodeservices

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// outcomeProbe stands for a caller that wraps the route's provider (the per-turn retry guard):
// it reads Outcome after the provider returns, through a wrapping writer as request metrics adds.
type outcomeProbe struct {
	Provider
	seen chan [2]any
}

func (p outcomeProbe) Complete(w http.ResponseWriter, r *http.Request, m Model) {
	p.Provider.Complete(unwrappingWriter{w}, r, m)
	completed, limit := Outcome(w)
	p.seen <- [2]any{completed, limit}
}

// stall holds an upstream call open until it is cancelled or the test is long over, so a server
// Close never waits on a handler the client side has already given up on.
func stall(r *http.Request) {
	select {
	case <-r.Context().Done():
	case <-time.After(3 * time.Second):
	}
}

// callRoute posts one Agent model call through the route and returns its status and body; a body
// cut off mid-stream is returned as far as it arrived.
func callRoute(t *testing.T, p Provider) (int, string) {
	t.Helper()
	s := testService(t, p)
	c := serviceClient(t, s, true)
	token, _ := issueToken(t, s, c, "execution")
	req, _ := http.NewRequest("POST", s.URL+"/runtime/openai/v1/chat/completions", strings.NewReader(`{"stream":true}`))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(data)
}

func TestOutcomeReportsLimitsBeforeAndAfterTheHeaderAndOnlyCountsA2xxAsCompleted(t *testing.T) {
	cases := []struct {
		name      string
		upstream  http.HandlerFunc
		idle      time.Duration
		total     time.Duration
		status    int
		completed bool
		limit     string
	}{
		{"idle before the header", func(w http.ResponseWriter, r *http.Request) { stall(r) },
			200 * time.Millisecond, time.Minute, 502, false, CutIdle},
		{"idle after the header", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(flushWriter{w}, "data: {}\n\n")
			stall(r)
		}, 200 * time.Millisecond, time.Minute, 200, false, CutIdle},
		{"total limit on an active stream", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			for {
				if _, err := io.WriteString(flushWriter{w}, "data: {}\n\n"); err != nil {
					return
				}
				select {
				case <-r.Context().Done():
					return
				case <-time.After(50 * time.Millisecond):
				}
			}
		}, 300 * time.Millisecond, 600 * time.Millisecond, 200, false, CutMaxDuration},
		{"provider rejects", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) },
			time.Second, time.Minute, 502, false, ""},
		{"normal completion", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
		}, time.Second, time.Minute, 200, true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			upstream := httptest.NewTLSServer(c.upstream)
			defer upstream.Close()
			p, err := NewRemoteProvider(upstream.URL, "backend-key", upstream.Client().Transport)
			if err != nil {
				t.Fatal(err)
			}
			if err := p.SetLimits(c.idle, c.total); err != nil {
				t.Fatal(err)
			}
			probe := outcomeProbe{p, make(chan [2]any, 1)}
			status, body := callRoute(t, probe)
			if status != c.status {
				t.Fatalf("status %d body %q, want %d", status, body, c.status)
			}
			select {
			case got := <-probe.seen:
				if got[0] != c.completed || got[1] != c.limit {
					t.Fatalf("Outcome = %v %q, want %v %q", got[0], got[1], c.completed, c.limit)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("provider never returned")
			}
		})
	}
}
