package nodeservices

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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

// unwrapping stands for a provider wrapper such as request metrics: it hides the route's
// response behind its own writer and exposes it only through Unwrap.
type unwrapping struct{ Provider }

type unwrappingWriter struct{ http.ResponseWriter }

func (w unwrappingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w unwrappingWriter) Flush()                      { w.ResponseWriter.(http.Flusher).Flush() }

func (p unwrapping) Complete(w http.ResponseWriter, r *http.Request, m Model) {
	p.Provider.Complete(unwrappingWriter{w}, r, m)
}

// readCutOff calls the Agent model route, which must fail mid-body rather than end normally.
func readCutOff(t *testing.T, p Provider) (string, time.Duration) {
	t.Helper()
	s := testService(t, p)
	c := serviceClient(t, s, true)
	token, _ := issueToken(t, s, c, "execution")
	req, _ := http.NewRequest("POST", s.URL+"/runtime/openai/v1/chat/completions", strings.NewReader(`{"stream":true}`))
	req.Header.Set("Authorization", "Bearer "+token)
	began := time.Now()
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("want a broken 200 stream, got %d err=%v body=%q", resp.StatusCode, err, data)
	}
	return string(data), time.Since(began)
}

func TestRemoteProviderIdleTimeoutAbortsResponse(t *testing.T) {
	stopped := make(chan struct{}, 2)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := range 3 {
			_, _ = fmt.Fprintf(flushWriter{w}, "data: {\"n\":%d}\n\n", i)
			time.Sleep(100 * time.Millisecond)
		}
		<-r.Context().Done()
		stopped <- struct{}{}
	}))
	defer upstream.Close()
	p, err := NewRemoteProvider(upstream.URL, "backend-key", upstream.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	// Chunks 100ms apart stay under the idle limit; the stall after them does not.
	if err := p.SetLimits(300*time.Millisecond, time.Minute); err != nil {
		t.Fatal(err)
	}
	for name, provider := range map[string]Provider{"direct": p, "wrapped": unwrapping{p}} {
		data, elapsed := readCutOff(t, provider)
		if !strings.Contains(data, `{"n":2}`) || elapsed > 5*time.Second {
			t.Fatalf("%s: idle cut after %s with %q", name, elapsed, data)
		}
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: upstream call not cancelled", name)
		}
	}
}

func TestRemoteProviderMaxDurationAbortsActiveStream(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for {
			if _, err := (flushWriter{w}).Write([]byte("data: {}\n\n")); err != nil {
				return
			}
			select {
			case <-r.Context().Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	}))
	defer upstream.Close()
	p, err := NewRemoteProvider(upstream.URL, "backend-key", upstream.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	// The stream never goes idle; only the total limit can end it.
	if err := p.SetLimits(300*time.Millisecond, 800*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	data, elapsed := readCutOff(t, p)
	if elapsed < 800*time.Millisecond || elapsed > 5*time.Second || strings.Count(data, "data: {}") < 5 {
		t.Fatalf("total cut after %s with %d chunks", elapsed, strings.Count(data, "data: {}"))
	}
}

func TestRemoteProviderLimitsValidation(t *testing.T) {
	p, err := NewRemoteProvider("https://example.test/v1", "key", nil)
	if err != nil {
		t.Fatal(err)
	}
	if idle, total := p.Limits(); idle != DefaultIdleTimeout || total != DefaultMaxDuration {
		t.Fatalf("defaults=%s %s", idle, total)
	}
	for _, l := range [][2]time.Duration{{0, time.Minute}, {time.Minute, 0}, {2 * time.Minute, time.Minute}} {
		if p.SetLimits(l[0], l[1]) == nil {
			t.Fatalf("accepted %v", l)
		}
	}
}

// Expiry never cancels an admitted call: neither the expired-grant sweep run by another
// execution's grant request nor a refused renewal stops it, while End still does.
func TestExpiredGrantKeepsRunningCallsUntilRevoked(t *testing.T) {
	s := testService(t, streamingProvider{interval: 50 * time.Millisecond, length: 1500 * time.Millisecond})
	c := serviceClient(t, s, true)
	token, id := issueToken(t, s, c, "execution")
	expire := func() {
		s.mu.Lock()
		s.grants[id].expires = time.Now().Add(-time.Second)
		s.mu.Unlock()
	}
	type result struct {
		body []byte
		err  error
	}
	call := func() chan result {
		done := make(chan result, 1)
		go func() {
			req, _ := http.NewRequest("POST", s.URL+"/runtime/openai/v1/chat/completions", strings.NewReader(`{}`))
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := c.Do(req)
			if err != nil {
				done <- result{nil, err}
				return
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			done <- result{body, err}
		}()
		return done
	}
	running := func() int {
		s.mu.Lock()
		defer s.mu.Unlock()
		if g := s.grants[id]; g != nil {
			return g.calls
		}
		return 0
	}
	waitRunning := func() {
		for deadline := time.Now().Add(5 * time.Second); running() == 0; time.Sleep(10 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatal("call did not start")
			}
		}
	}

	done := call()
	waitRunning()
	expire()
	if status, _ := request(t, c, "POST", s.URL+"/internal/v1/model-grants/"+id+"/renew", "", `{"executionId":"execution"}`); status != 403 {
		t.Fatalf("expired renew=%d", status)
	}
	issueToken(t, s, c, "other") // sweeps expired grants of every execution
	if status, _ := request(t, c, "POST", s.URL+"/runtime/openai/v1/chat/completions", token, `{}`); status != 401 {
		t.Fatalf("expired token started a new call: %d", status)
	}
	r := <-done
	if r.err != nil || !strings.HasSuffix(string(r.body), "data: [DONE]\n\n") {
		t.Fatalf("expiry cut an admitted call: err=%v body=%q", r.err, r.body)
	}
	s.mu.Lock()
	_, kept := s.grants[id]
	s.mu.Unlock()
	if kept {
		t.Fatal("expired grant not removed after its last call")
	}

	token, id = issueToken(t, s, c, "execution")
	done = call()
	waitRunning()
	expire()
	issueToken(t, s, c, "other")
	began := time.Now()
	s.End("execution")
	r = <-done
	if strings.HasSuffix(string(r.body), "data: [DONE]\n\n") || time.Since(began) > time.Second {
		t.Fatalf("End did not cancel the call of an expired grant: err=%v body=%q", r.err, r.body)
	}
}

// Only the idle limit bounds the wait for the response header; there is no separate header timeout.
func TestRemoteProviderHeaderWaitBoundedOnlyByIdleLimit(t *testing.T) {
	p, err := NewRemoteProvider("https://example.test/v1", "key", nil)
	if err != nil {
		t.Fatal(err)
	}
	if transport := p.client.Transport.(*http.Transport); transport.ResponseHeaderTimeout != 0 || p.client.Timeout != 0 {
		t.Fatalf("default client still has fixed timeouts: header=%s client=%s", transport.ResponseHeaderTimeout, p.client.Timeout)
	}
	for _, c := range []struct {
		delay, idle time.Duration
		status      int
	}{{400 * time.Millisecond, time.Second, 200}, {time.Second, 300 * time.Millisecond, 502}} {
		upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-time.After(c.delay):
			case <-r.Context().Done():
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"choices":[]}`)
		}))
		p, err := NewRemoteProvider(upstream.URL, "backend-key", upstream.Client().Transport)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.SetLimits(c.idle, time.Minute); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		p.Complete(w, httptest.NewRequest("POST", "/", strings.NewReader(`{}`)), testModel)
		upstream.Close()
		if w.Code != c.status {
			t.Fatalf("header after %s with idle %s: %d %s", c.delay, c.idle, w.Code, w.Body)
		}
	}
}
