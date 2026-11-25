package completiontool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
)

// harness is a served registry whose stored state the test sets.
type harness struct {
	r     *Registry
	live  atomic.Bool
	broke atomic.Bool
}

func serve(t *testing.T) *harness {
	t.Helper()
	h := &harness{}
	h.live.Store(true)
	h.r = NewRegistry(func(context.Context, string, string) (bool, error) {
		if h.broke.Load() {
			return false, errors.New("down")
		}
		return h.live.Load(), nil
	})
	s, err := Serve(h.r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return h
}

func post(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if len(bytes.TrimSpace(data)) != 0 {
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatalf("%s: %v", data, err)
		}
	}
	return resp.StatusCode, out
}

// call calls the tool and returns its text and whether it is an error.
func call(t *testing.T, url, token string) (string, bool) {
	t.Helper()
	status, out := post(t, url, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"`+ToolName+`","arguments":{"attemptToken":"`+token+`"}}}`)
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	result := out["result"].(map[string]any)
	return result["content"].([]any)[0].(map[string]any)["text"].(string), result["isError"].(bool)
}

func attach(t *testing.T, a *Attempt, session string) string {
	t.Helper()
	servers := a.Attach(session)
	if err := execution.ValidateHostMCPServers(servers.Servers); err != nil || len(servers.Servers) != 1 {
		t.Fatalf("servers = %v: %v", servers.Servers, err)
	}
	return servers.Servers[0].URL
}

func TestTheEndpointServesTheOneTool(t *testing.T) {
	h := serve(t)
	a := h.r.Attempt("n1", "n1")
	url := attach(t, a, "s1")
	status, out := post(t, url, `{"jsonrpc":"2.0","id":"a","method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	if status != 200 || out["id"] != "a" || out["result"].(map[string]any)["protocolVersion"] != protocolVersion {
		t.Fatalf("initialize = %d %v", status, out)
	}
	if status, _ := post(t, url, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); status != http.StatusAccepted {
		t.Fatalf("notification = %d", status)
	}
	if status, out := post(t, url, `{"jsonrpc":"2.0","id":0,"method":"ping"}`); status != 200 || out["result"] == nil {
		t.Fatalf("ping = %d %v", status, out)
	}
	_, out = post(t, url, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	tools := out["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != ToolName {
		t.Fatalf("tools = %v", tools)
	}
	for _, body := range []string{
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"other","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"` + ToolName + `"}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"` + ToolName + `","arguments":{"attemptToken":5}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"resources/list"}`,
	} {
		if _, out := post(t, url, body); out["error"] == nil {
			t.Fatalf("%s = %v, want an error", body, out)
		}
	}
	if status, _ := post(t, url, "["); status != http.StatusBadRequest {
		t.Fatalf("malformed = %d", status)
	}
	if status, _ := post(t, url, `{"id":1,"method":"`+strings.Repeat("x", maxRequestBytes)+`"}`); status != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized = %d", status)
	}
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET = %d, want no event stream", resp.StatusCode)
	}
}

func TestACallIsAcceptedOnceAndOnlyForItsOwnTurn(t *testing.T) {
	h := serve(t)
	first := h.r.Attempt("n1", "n1")
	url := attach(t, first, "s1")
	token := first.Instruct().Token
	text, refused := call(t, url, token)
	if refused || !strings.HasPrefix(text, "accepted:") {
		t.Fatalf("call = %q %v", text, refused)
	}
	if again, refused := call(t, url, token); refused || again != text {
		t.Fatalf("repeated call = %q, want the same signal", again)
	}
	signal := h.r.Take("n1", "n1")
	if signal == nil || "accepted:"+signal.ID != text || signal.At.IsZero() {
		t.Fatalf("signal = %+v", signal)
	}
	if h.r.Take("n1", "n1") != nil {
		t.Fatal("a settled turn's signal was taken twice")
	}
	// The next turn of the same Agent: the earlier token is stale and leaves no signal behind.
	second := h.r.Attempt("n1", "n1/turn-1")
	next := second.Instruct().Token
	if why, refused := call(t, url, token); !refused || why != string(RefusedStale) {
		t.Fatalf("earlier token = %q", why)
	}
	if s := h.r.Take("n1", "n1/turn-1"); s != nil {
		t.Fatalf("stale call left %+v", s)
	}
	if why, _ := call(t, url, next); why != string(RefusedStale) {
		t.Fatalf("token after its turn settled = %q", why)
	}
	if why, _ := call(t, url, "made-up"); why != string(RefusedUnknown) {
		t.Fatalf("made-up token = %q", why)
	}
}

// Resending a prompt withdraws the token of the earlier sending and what it requested.
func TestResendingAPromptReplacesItsToken(t *testing.T) {
	h := serve(t)
	a := h.r.Attempt("n1", "n1")
	url := attach(t, a, "s1")
	old := a.Instruct()
	if _, refused := call(t, url, old.Token); refused {
		t.Fatal("refused")
	}
	fresh := a.Instruct()
	if fresh.Token == old.Token || !strings.Contains(fresh.Text, fresh.Token) || len(fresh.Text) > execution.MaxHostInstruction {
		t.Fatalf("instruction = %q", fresh.Text)
	}
	if why, _ := call(t, url, old.Token); why != string(RefusedStale) {
		t.Fatalf("earlier sending = %q", why)
	}
	if s := h.r.Take("n1", "n1"); s != nil {
		t.Fatalf("signal %+v of the earlier sending survived", s)
	}
}

// A sibling Agent may see the endpoint, as OpenCode registers servers process-wide: neither its
// own credential with this node's token nor this node's credential with its token does anything.
func TestAnotherNodesCredentialOrTokenIsRefused(t *testing.T) {
	h := serve(t)
	mine, other := h.r.Attempt("n1", "n1"), h.r.Attempt("n2", "n2")
	myURL, otherURL := attach(t, mine, "s1"), attach(t, other, "s2")
	myToken, otherToken := mine.Instruct().Token, other.Instruct().Token
	if why, _ := call(t, otherURL, myToken); why != string(RefusedUnknown) {
		t.Fatalf("my token through the other endpoint = %q", why)
	}
	if why, _ := call(t, myURL, otherToken); why != string(RefusedUnknown) {
		t.Fatalf("the other token through my endpoint = %q", why)
	}
	if h.r.Take("n1", "n1") != nil || h.r.Take("n2", "n2") != nil {
		t.Fatal("a cross-node call left a signal")
	}
	if status, _ := post(t, strings.Replace(myURL, "/mcp/", "/mcp/x", 1), `{"id":1,"method":"ping"}`); status != http.StatusUnauthorized {
		t.Fatalf("made-up credential = %d", status)
	}
}

// Only one physical Agent of a node holds the credential, and none after the node takes no turn.
func TestCredentialsEndWithTheirAgent(t *testing.T) {
	h := serve(t)
	a := h.r.Attempt("n1", "n1")
	old := attach(t, a, "s1")
	fresh := attach(t, a, "s1")
	if status, _ := post(t, old, `{"id":1,"method":"ping"}`); status != http.StatusUnauthorized {
		t.Fatalf("the replaced Agent's credential = %d", status)
	}
	token := a.Instruct().Token
	h.r.Revoke("n1")
	if status, _ := post(t, fresh, `{"id":1,"method":"ping"}`); status != http.StatusUnauthorized {
		t.Fatalf("a revoked node's credential = %d", status)
	}
	// What a revoked attempt still hands out (a start racing the end of its run) never works.
	late := attach(t, a, "s1")
	if status, _ := post(t, late, `{"id":1,"method":"ping"}`); status != http.StatusUnauthorized {
		t.Fatalf("a credential issued after the revocation = %d", status)
	}
	next := attach(t, h.r.Attempt("n1", "n1/turn-1"), "s1")
	if why, _ := call(t, next, token); why != string(RefusedNotAwaiting) {
		t.Fatalf("the revoked node's token = %q", why)
	}
	if why, _ := call(t, next, a.Instruct().Token); why != string(RefusedUnknown) {
		t.Fatalf("a token issued after the revocation = %q", why)
	}
}

// Stored state decides: a turn it no longer calls running refuses the call and keeps no signal,
// and an unreadable one refuses without withdrawing the token.
func TestStoredStateDecidesACall(t *testing.T) {
	h := serve(t)
	a := h.r.Attempt("n1", "n1")
	url := attach(t, a, "s1")
	token := a.Instruct().Token
	h.live.Store(false)
	if why, refused := call(t, url, token); !refused || why != string(RefusedNotAwaiting) {
		t.Fatalf("call = %q", why)
	}
	h.live.Store(true)
	h.broke.Store(true)
	if why, refused := call(t, url, token); !refused || why != string(RefusedUnavailable) {
		t.Fatalf("call = %q", why)
	}
	h.broke.Store(false)
	if _, refused := call(t, url, token); refused {
		t.Fatal("the token did not survive an unreadable state")
	}
}

func TestAMissingRegistryHasNoTool(t *testing.T) {
	var r *Registry
	if r.Take("n", "a") != nil {
		t.Fatal("signal")
	}
	r.Revoke("n")
}

// A request that was authorized before its credential was withdrawn says nothing about the Agent that
// replaced the one it came from.
func TestALateConnectionIsNotTheNewAgents(t *testing.T) {
	h := serve(t)
	a := h.r.Attempt("n1", "n1")
	url := attach(t, a, "s1")
	a.Started()
	// The server asks for the body once the handler reads it, which is after it was authorized.
	replaced := false
	trace := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{Got100Continue: func() {
		a.Attach("s1")
		a.Started()
		replaced = true
	}})
	req, err := http.NewRequestWithContext(trace, http.MethodPost, url, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Expect", "100-continue")
	client := &http.Client{Transport: &http.Transport{ExpectContinueTimeout: time.Minute, DisableKeepAlives: true}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !replaced {
		t.Fatalf("status %d, replaced %v", resp.StatusCode, replaced)
	}
	if unconnected, known := h.r.Unconnected("n1"); !unconnected || !known {
		t.Fatalf("the new Agent: unconnected = %v, known %v", unconnected, known)
	}
}

// A connection counts for the Agent whose endpoint was asked, once its start was sent: any method but
// initialize and tools/list, and a withdrawn credential, say nothing.
func TestAConnectionIsRecordedForTheStartedAgent(t *testing.T) {
	h := serve(t)
	ask := func(url, method string) int {
		t.Helper()
		status, _ := post(t, url, `{"jsonrpc":"2.0","id":1,"method":"`+method+`"}`)
		return status
	}
	said := func(when, nodeRun string, unconnected, known bool) {
		t.Helper()
		if u, k := h.r.Unconnected(nodeRun); u != unconnected || k != known {
			t.Fatalf("%s: unconnected = %v, known %v; want %v, %v", when, u, k, unconnected, known)
		}
	}
	said("no such node", "n1", false, false)
	a, other := h.r.Attempt("n1", "n1"), h.r.Attempt("n2", "n2")
	said("no Agent", "n1", false, false)
	url, otherURL := attach(t, a, "s1"), attach(t, other, "s2")
	said("no start", "n1", false, false)
	a.Started()
	other.Started()
	said("started", "n1", true, true)
	ask(url, "ping")
	ask(url, "resources/list")
	call(t, url, a.Instruct().Token)
	if status, _ := post(t, url, `{"jsonrpc":"2.0","method":"initialize"}`); status != http.StatusAccepted {
		t.Fatalf("notification = %d", status)
	}
	said("other methods", "n1", true, true)
	ask(url, "initialize")
	said("initialize", "n1", false, true)
	said("another node", "n2", true, true)
	ask(otherURL, "tools/list")
	said("tools/list", "n2", false, true)
	h.r.Take("n1", "n1")
	said("between turns", "n1", false, true)

	next := h.r.Attempt("n1", "n1/turn-1")
	fresh := attach(t, next, "s1")
	said("a new Agent that was not started", "n1", false, false)
	next.Started()
	said("a new Agent", "n1", true, true)
	if status := ask(url, "initialize"); status != http.StatusUnauthorized {
		t.Fatalf("the old endpoint = %d", status)
	}
	ask(fresh, "tools/list")
	said("the new Agent's request", "n1", false, true)
	h.r.Revoke("n1")
	next.Started()
	said("revoked", "n1", false, false)
	if u, k := (*Registry)(nil).Unconnected("n1"); u || k {
		t.Fatal("a deployment without the tool said something")
	}
}
