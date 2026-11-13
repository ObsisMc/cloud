package agentruntime

import (
	"net/http"
	"sync"

	"github.com/wanglongan587/cloud/v2-server/internal/nodeservices"
)

// Agents re-send a model call that was cut off as if the network had failed: OpenCode up to
// five times, Claude Code ten or more, each re-send streaming its partial answer again. A cut
// at a model limit is not a network failure, so one turn allows only what the limit intends:
// none after the total limit, one after an idle cut. Every later call of the turn is refused
// before any upstream work, and the turn reports the limit as its failure.
type retryGuard struct {
	mu sync.Mutex
	// idleCuts counts stalls (idle cuts of calls started after the previous cut) since the last
	// call that started after a cut and completed.
	idleCuts int
	// calls numbers the turn's calls as they start; cutAfter is the number of the newest call
	// started when the latest cut happened, so only a call started later can clear it.
	calls, cutAfter int
	// last is the limit that cut the turn's most recent call, cleared by a later completed call.
	last string
	// stopped is the limit that ended the turn; every later call is refused with it.
	stopped string
}

var refusals = map[string]string{
	nodeservices.CutMaxDuration: "模型请求超过时长上限被切断，这一轮已停止",
	nodeservices.CutIdle:        "模型多次长时间没有输出，这一轮已停止",
}

func (g *retryGuard) provider(next nodeservices.Provider, protocol nodeservices.Protocol) nodeservices.Provider {
	return guardedProvider{g, next, protocol}
}

// failure is the limit a turn ended at, or "" when its last call was not cut at a limit.
func (g *retryGuard) failure() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopped != "" {
		return g.stopped
	}
	return g.last
}

func (g *retryGuard) begin() (stopped string, call int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	return g.stopped, g.calls
}

func (g *retryGuard) finish(call int, reason string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch reason {
	case nodeservices.CutMaxDuration:
		g.last, g.stopped, g.cutAfter = reason, reason, g.calls
	case nodeservices.CutIdle:
		// Calls that were already running at the previous cut stalled with it (a title request
		// beside the turn's call): one stall, however many calls it cuts, earns one retry.
		if call > g.cutAfter {
			g.idleCuts++
		}
		g.last, g.cutAfter = reason, g.calls
		if g.idleCuts > 1 {
			g.stopped = reason
		}
	case "":
		// A call running alongside the cut one (a title request) proves nothing about the retry.
		if call > g.cutAfter {
			g.idleCuts, g.last = 0, ""
		}
	}
}

type guardedProvider struct {
	guard    *retryGuard
	next     nodeservices.Provider
	protocol nodeservices.Protocol
}

func (p guardedProvider) Complete(w http.ResponseWriter, r *http.Request, m nodeservices.Model) {
	stopped, n := p.guard.begin()
	if stopped != "" {
		nodeservices.RefuseCall(w, p.protocol, stopped, refusals[stopped])
		return
	}
	p.next.Complete(w, r, m)
	p.guard.finish(n, nodeservices.CutReason(w))
}
