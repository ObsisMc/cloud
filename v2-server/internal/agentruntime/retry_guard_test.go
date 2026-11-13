package agentruntime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/nodehistory"
	"github.com/wanglongan587/cloud/v2-server/internal/nodeservices"
)

// cutWriter stands in for the Agent model route's response, which knows why a call was cut.
type cutWriter struct {
	*httptest.ResponseRecorder
	reason string
}

func (w *cutWriter) CutReason() string { return w.reason }

// scriptedCalls ends each call it serves with the next scripted cut reason ("" completes).
type scriptedCalls struct {
	cuts  []string
	calls int
}

func (s *scriptedCalls) Complete(w http.ResponseWriter, _ *http.Request, _ nodeservices.Model) {
	w.WriteHeader(200)
	w.(*cutWriter).reason = s.cuts[s.calls]
	s.calls++
}

func call(t *testing.T, p nodeservices.Provider) *cutWriter {
	t.Helper()
	w := &cutWriter{ResponseRecorder: httptest.NewRecorder()}
	p.Complete(w, httptest.NewRequest("POST", "/v1/chat/completions", nil), nodeservices.Model{})
	return w
}

// OpenCode re-sends when the error message or response body matches one of these patterns
// (RETRYABLE_MESSAGE_PATTERNS in OpenCode v1.19.3 packages/opencode/src/session/retry.ts),
// besides the statuses its provider SDK treats as retryable.
var retryablePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)429|500|502|503|504|524`),
	regexp.MustCompile(`(?i)rate increased too quickly|rate limit|rate-limit|rate_limit|too many requests`),
	regexp.MustCompile(`(?i)overloaded|service unavailable|service_unavailable|service-unavailable|internal error|internal_error|internal server error|server error|server_error|server-error|provider returned error|provider_returned_error|provider-returned-error`),
	regexp.MustCompile(`(?i)terminated|fetch failed|failed to fetch|network[-_\s]error|upstream connect|connection error|connection refused|connection lost|socket connection was closed|socket hang up|reset before headers|getaddrinfo|enotfound|eai_again|econnrefused|econnreset|etimedout`),
	regexp.MustCompile(`(?i)^timeout$|\b(?:request|response|connection|network|stream|read) (?:timeout|timed out|time out)\b`),
	regexp.MustCompile(`(?i)try your request again|retry your request|resource exhausted|resource_exhausted`),
	regexp.MustCompile(`(?i)\btry again (?:later|in\b)|\b(?:currently|temporarily) at capacity\b`),
}

func retryable(text string) bool {
	for _, p := range retryablePatterns {
		if p.MatchString(text) {
			return true
		}
	}
	return false
}

func assertRefused(t *testing.T, w *cutWriter, protocol nodeservices.Protocol, code string) {
	t.Helper()
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("refusal status %d", w.Code)
	}
	var body struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if protocol == nodeservices.AnthropicMessages && body.Type != "error" {
		t.Fatalf("not an Anthropic error: %s", w.Body)
	}
	if protocol == nodeservices.OpenAICompletions && body.Error.Code != code {
		t.Fatalf("code %q, want %q", body.Error.Code, code)
	}
	if body.Error.Message == "" || retryable(body.Error.Message) || retryable(w.Body.String()) {
		t.Fatalf("refusal %s would be retried or has no message", w.Body)
	}
}

func TestRetryGuardRefusesEveryCallAfterTheTotalLimit(t *testing.T) {
	for _, protocol := range []nodeservices.Protocol{nodeservices.OpenAICompletions, nodeservices.AnthropicMessages} {
		var g retryGuard
		upstream := &scriptedCalls{cuts: []string{nodeservices.CutMaxDuration}}
		p := g.provider(upstream, protocol)
		call(t, p)
		for range 3 {
			assertRefused(t, call(t, p), protocol, nodeservices.CutMaxDuration)
		}
		if upstream.calls != 1 {
			t.Fatalf("refused calls reached upstream: %d", upstream.calls)
		}
		if g.failure() != nodeservices.CutMaxDuration {
			t.Fatalf("failure %q", g.failure())
		}
	}
}

func TestRetryGuardAllowsOneRetryAfterAnIdleCut(t *testing.T) {
	var g retryGuard
	upstream := &scriptedCalls{cuts: []string{nodeservices.CutIdle, nodeservices.CutIdle}}
	p := g.provider(upstream, nodeservices.OpenAICompletions)
	call(t, p)
	if g.failure() != nodeservices.CutIdle {
		t.Fatalf("failure after first idle cut %q", g.failure())
	}
	if w := call(t, p); w.Code != 200 {
		t.Fatal("the one retry after an idle cut was refused")
	}
	assertRefused(t, call(t, p), nodeservices.OpenAICompletions, nodeservices.CutIdle)
	if upstream.calls != 2 {
		t.Fatalf("upstream calls %d, want 2", upstream.calls)
	}
}

func TestRetryGuardCompletedCallClearsIdleCuts(t *testing.T) {
	var g retryGuard
	upstream := &scriptedCalls{cuts: []string{nodeservices.CutIdle, "", nodeservices.CutIdle, ""}}
	p := g.provider(upstream, nodeservices.OpenAICompletions)
	call(t, p)
	call(t, p)
	if g.failure() != "" {
		t.Fatalf("completed retry kept failure %q", g.failure())
	}
	// A later idle cut in the same turn is a new stall and gets its own retry.
	call(t, p)
	if w := call(t, p); w.Code != 200 || upstream.calls != 4 {
		t.Fatal("new stall after a completed call was refused")
	}
}

func TestRetryGuardIgnoresCallsStartedBeforeTheCut(t *testing.T) {
	var g retryGuard
	_, cut := g.begin()
	_, title := g.begin() // e.g. a title request that ran alongside the cut call
	g.finish(cut, nodeservices.CutIdle)
	g.finish(title, "")
	if g.failure() != nodeservices.CutIdle {
		t.Fatalf("concurrent call cleared the cut: %q", g.failure())
	}
}

func TestCancelledTurnCutAtLimitFailsWithTheLimit(t *testing.T) {
	result := turnResult(&nodehistory.Turn{Answer: "Start. ", StopReason: "cancelled"}, nodeservices.CutMaxDuration)
	if result.Status != execution.StatusFailed || result.FailureCode != nodeservices.CutMaxDuration || result.AnswerComplete {
		t.Fatalf("result %+v", result)
	}
	if result := turnResult(&nodehistory.Turn{StopReason: "cancelled"}, ""); result.Status != execution.StatusCancelled {
		t.Fatalf("plain cancel changed: %+v", result)
	}
	if result := turnResult(&nodehistory.Turn{Answer: "done", StopReason: "end_turn"}, nodeservices.CutIdle); result.Status != execution.StatusCompleted {
		t.Fatalf("completed turn after a recovered cut: %+v", result)
	}
}

func TestRetryGuardCountsOneStallForCallsCutTogether(t *testing.T) {
	var g retryGuard
	_, title := g.begin()
	_, turn := g.begin()
	g.finish(turn, nodeservices.CutIdle)
	g.finish(title, nodeservices.CutIdle) // stalled alongside: the same stall
	stopped, retry := g.begin()
	if stopped != "" {
		t.Fatal("the one retry after a stall was refused because two calls were cut together")
	}
	g.finish(retry, nodeservices.CutIdle)
	if stopped, _ := g.begin(); stopped != nodeservices.CutIdle {
		t.Fatalf("a second stall did not stop the turn: %q", stopped)
	}
}
