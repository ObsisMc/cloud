//go:build linux

package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/node"
)

// executionRecords stands in for the stored host executions.
type executionRecords struct {
	mu       sync.Mutex
	sessions map[string]string // session by execution
	ended    chan [2]string    // every successful end: execution and reason
	failures int               // the next writes that fail
	// onRecord runs before a record is stored.
	onRecord func(execution string) error
}

func recordExecutions(h *ipcHarness) *executionRecords {
	r := &executionRecords{sessions: map[string]string{}, ended: make(chan [2]string, 16)}
	h.driver.opts.RecordHostExecution = func(_ context.Context, id, session string) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.onRecord != nil {
			if err := r.onRecord(id); err != nil {
				return err
			}
		}
		r.sessions[id] = session
		return nil
	}
	h.driver.opts.EndHostExecution = func(_ context.Context, id, reason string) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.failures > 0 {
			r.failures--
			return errors.New("database unavailable")
		}
		r.ended <- [2]string{id, reason}
		return nil
	}
	return r
}

func (r *executionRecords) session(id string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessions[id]
}

// none fails the test when an execution was settled (again).
func (r *executionRecords) none(t *testing.T) {
	t.Helper()
	select {
	case end := <-r.ended:
		t.Fatalf("settled %v", end)
	default:
	}
}

// parkHosted runs the first turn of a session given host tools and returns its start.
func parkHosted(t *testing.T, h *ipcHarness, session execution.Session, f *fakeNodeIPC) (*fakeNodeIPC, node.Message) {
	t.Helper()
	done := runHosted(h, ipcContext(t), session, session.ID+"-first", &scriptedTools{}, discardIPCEvent)
	if f == nil {
		f = receiveIPC(t, h.created)
	}
	start := nextIPCCommand(t, f, "start_agent_session")
	f.finishTurn(t, start, session.ID+"-first", "done")
	if got := receiveIPC(t, done); got.err != nil {
		t.Fatalf("first turn: %+v", got)
	}
	return f, start
}

// An execution with host tools is on record before Node gets its start, one without them never is.
func TestAHostExecutionIsRecordedBeforeItsStart(t *testing.T) {
	var early bool
	var records *executionRecords
	h := newIPCHarness(t, func(_ *fakeNodeIPC, m node.Message) bool {
		if m.Type == "start_agent_session" && m.ExecutionID != "" && records.session(m.ExecutionID) == "" {
			early = true
		}
		return false
	})
	records = recordExecutions(h)
	f, start := parkHosted(t, h, h.session("hosted", "workspace"), nil)
	if early || records.session(start.ExecutionID) != "hosted" {
		t.Fatalf("the start went out before its record: %v", records.sessions)
	}
	// Further turns run in the same execution.
	done := runHosted(h, ipcContext(t), h.session("hosted", "workspace"), "second", &scriptedTools{}, discardIPCEvent)
	f.finishTurn(t, nextIPCCommand(t, f, "submit_user_turn"), "second", "again")
	if got := receiveIPC(t, done); got.err != nil {
		t.Fatal(got.err)
	}
	plain := startIPCRun(h, ipcContext(t), h.session("plain", "workspace-2"), "first", discardIPCEvent)
	f.finishTurn(t, nextIPCCommand(t, f, "start_agent_session"), "first", "done")
	if got := receiveIPC(t, plain); got.err != nil {
		t.Fatal(got.err)
	}
	records.mu.Lock()
	defer records.mu.Unlock()
	if len(records.sessions) != 1 {
		t.Fatalf("records = %v", records.sessions)
	}
	records.none(t)
}

// Without its record a start is not sent: the turn did not start, and whatever the failed write left
// behind is settled.
func TestAStartIsNotSentWithoutItsRecord(t *testing.T) {
	for name, set := range map[string]func(*ipcHarness, *executionRecords){
		"the write fails": func(_ *ipcHarness, r *executionRecords) {
			r.onRecord = func(string) error { return errors.New("database unavailable") }
		},
		"nothing records": func(h *ipcHarness, _ *executionRecords) { h.driver.opts.RecordHostExecution = nil },
		"nothing settles": func(h *ipcHarness, _ *executionRecords) { h.driver.opts.EndHostExecution = nil },
	} {
		t.Run(name, func(t *testing.T) {
			h := newIPCHarness(t, nil)
			records := recordExecutions(h)
			set(h, records)
			got := receiveIPC(t, runHosted(h, ipcContext(t), h.session("hosted", "workspace"), "first", &scriptedTools{}, discardIPCEvent))
			if got.err == nil || got.result.Status != execution.StatusFailed || !got.result.NotStarted || !got.result.NotDelivered {
				t.Fatalf("outcome = %+v", got)
			}
			if sentCommand(receiveIPC(t, h.created), "start_agent_session") {
				t.Fatal("a start was sent without its record")
			}
			if name == "the write fails" {
				if end := receiveIPC(t, records.ended); end[1] != "agent_dispatch_failed" {
					t.Fatalf("settled %v", end)
				}
			}
		})
	}
}

func TestEndSettlesAHostExecution(t *testing.T) {
	h := newIPCHarness(t, nil)
	records := recordExecutions(h)
	f, start := parkHosted(t, h, h.session("hosted", "workspace"), nil)
	ending := make(chan error, 1)
	go func() { ending <- h.driver.End(ipcContext(t), "hosted") }()
	nextIPCCommand(t, f, "end_session")
	f.terminal(start) // Node says user_ended: it was this server that ended the session
	if err := receiveIPC(t, ending); err != nil {
		t.Fatal(err)
	}
	if end := receiveIPC(t, records.ended); end != [2]string{start.ExecutionID, execution.HostSessionEnded} {
		t.Fatalf("settled %v", end)
	}
	records.none(t)
}

// An Agent that ends by itself settles its execution with Node's reason, parked or in a turn, before
// the turn's outcome is handed over.
func TestAnEndedAgentSettlesItsHostExecution(t *testing.T) {
	t.Run("parked", func(t *testing.T) {
		h := newIPCHarness(t, nil)
		records := recordExecutions(h)
		f, start := parkHosted(t, h, h.session("hosted", "workspace"), nil)
		f.terminalAs(start, "idle_timeout", "")
		if end := receiveIPC(t, records.ended); end != [2]string{start.ExecutionID, "idle_timeout"} {
			t.Fatalf("settled %v", end)
		}
		if err := h.driver.End(ipcContext(t), "hosted"); err != nil {
			t.Fatal(err)
		}
		records.none(t)
	})
	t.Run("in a turn", func(t *testing.T) {
		h := newIPCHarness(t, nil)
		records := recordExecutions(h)
		done := runHosted(h, ipcContext(t), h.session("hosted", "workspace"), "first", &scriptedTools{}, discardIPCEvent)
		f := receiveIPC(t, h.created)
		start := nextIPCCommand(t, f, "start_agent_session")
		f.terminalAs(start, "agent_failed", execution.FailureHostMCPAgent)
		receiveIPC(t, done)
		select {
		case end := <-records.ended:
			if end != [2]string{start.ExecutionID, "agent_failed"} {
				t.Fatalf("settled %v", end)
			}
		default:
			t.Fatal("the turn's outcome came before its execution was settled")
		}
	})
}

// A lost Node settles every execution it ran before anyone learns of the loss; ending those sessions
// afterwards changes nothing, and the Agent that continues one is a new execution.
func TestALostNodeSettlesItsHostExecutions(t *testing.T) {
	h := newIPCHarness(t, nil)
	records := recordExecutions(h)
	first, second := h.session("one", "workspace"), h.session("two", "workspace-2")
	f, one := parkHosted(t, h, first, nil)
	_, two := parkHosted(t, h, second, f)
	h.driver.mu.Lock()
	p := h.driver.projects["project"]
	h.driver.mu.Unlock()
	_ = f.Close()
	receiveIPC(t, p.dead)
	lost := map[[2]string]bool{}
	for range 2 {
		select {
		case end := <-records.ended:
			lost[end] = true
		default:
			t.Fatal("the loss was known before its executions were settled")
		}
	}
	if !lost[[2]string{one.ExecutionID, execution.FailureNodeLost}] || !lost[[2]string{two.ExecutionID, execution.FailureNodeLost}] {
		t.Fatalf("settled %v", lost)
	}
	if err := h.driver.End(ipcContext(t), "two"); err != nil {
		t.Fatal(err)
	}
	records.none(t)
	first.Recovery = &execution.Recovery{Handoff: "<ora_previous_conversation>…</ora_previous_conversation>"}
	done := runHosted(h, ipcContext(t), first, "again", &scriptedTools{}, discardIPCEvent)
	f = receiveIPC(t, h.created)
	start := nextIPCCommand(t, f, "start_agent_session")
	if start.ExecutionID == one.ExecutionID || records.session(start.ExecutionID) != "one" {
		t.Fatalf("the new Agent runs in %s, recorded as %q", start.ExecutionID, records.session(start.ExecutionID))
	}
	f.finishTurn(t, start, "again", "continued")
	if got := receiveIPC(t, done); got.err != nil {
		t.Fatal(got.err)
	}
	records.none(t)
}

// Retiring a Node that no longer confirms stops ends the session it was asked to end, and the others
// it ran lose their Node.
func TestARetiredNodeSettlesItsOtherHostExecutionsAsLost(t *testing.T) {
	h := newIPCHarness(t, nil)
	records := recordExecutions(h)
	f, one := parkHosted(t, h, h.session("one", "workspace"), nil)
	_, two := parkHosted(t, h, h.session("two", "workspace-2"), f)
	h.driver.Rebuild("one")
	if err := h.driver.End(ipcContext(t), "one"); err != nil {
		t.Fatal(err)
	}
	ends := map[string]string{}
	for range 2 {
		end := receiveIPC(t, records.ended)
		ends[end[0]] = end[1]
	}
	if ends[one.ExecutionID] != execution.HostSessionEnded || ends[two.ExecutionID] != execution.FailureNodeLost {
		t.Fatalf("settled %v", ends)
	}
}

// A shutdown settles nothing: the next start does, as it does after a crash, so the reason is the same.
func TestAShutdownLeavesHostExecutionsToTheNextStart(t *testing.T) {
	h := newIPCHarness(t, nil)
	records := recordExecutions(h)
	parkHosted(t, h, h.session("hosted", "workspace"), nil)
	if err := h.driver.Close(ipcContext(t)); err != nil {
		t.Fatal(err)
	}
	records.none(t)
}

// An end that could not be written is written later, with the reason it had then.
func TestAHostExecutionsEndIsWrittenOnceTheStoreTakesIt(t *testing.T) {
	h := newIPCHarness(t, nil)
	h.driver.hostExecutionRetry = 10 * time.Millisecond
	records := recordExecutions(h)
	f, start := parkHosted(t, h, h.session("hosted", "workspace"), nil)
	records.mu.Lock()
	records.failures = 3
	records.mu.Unlock()
	f.terminalAs(start, "idle_timeout", "")
	if end := receiveIPC(t, records.ended); end != [2]string{start.ExecutionID, "idle_timeout"} {
		t.Fatalf("settled %v", end)
	}
	if err := h.driver.End(ipcContext(t), "hosted"); err != nil {
		t.Fatal(err)
	}
	records.none(t)
}

// Until it is written, an end is known to whoever ends the session.
func TestAnUnwrittenEndIsKnownUntilItIsWritten(t *testing.T) {
	h := newIPCHarness(t, nil)
	h.driver.hostExecutionRetry = 10 * time.Millisecond
	records := recordExecutions(h)
	f, start := parkHosted(t, h, h.session("hosted", "workspace"), nil)
	if ends := h.driver.HostExecutionEnds("hosted"); len(ends) != 0 {
		t.Fatalf("ends of a running execution = %v", ends)
	}
	records.mu.Lock()
	records.failures = 1 << 30
	records.mu.Unlock()
	f.terminalAs(start, "agent_failed", "")
	for deadline := time.Now().Add(5 * time.Second); len(h.driver.HostExecutionEnds("hosted")) == 0; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("an unwritten end is not listed")
		}
	}
	if err := h.driver.End(ipcContext(t), "hosted"); err != nil {
		t.Fatal(err)
	}
	if ends := h.driver.HostExecutionEnds("hosted"); len(ends) != 1 || ends[start.ExecutionID] != "agent_failed" {
		t.Fatalf("ends = %v", ends)
	}
	if ends := h.driver.HostExecutionEnds("another"); len(ends) != 0 {
		t.Fatalf("ends of another session = %v", ends)
	}
	records.mu.Lock()
	records.failures = 0
	records.mu.Unlock()
	receiveIPC(t, records.ended)
	for deadline := time.Now().Add(5 * time.Second); len(h.driver.HostExecutionEnds("hosted")) != 0; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("a written end is still listed")
		}
	}
}

// The failure that makes the server end a session is the reason its execution ends, not the end it
// leads to.
func TestAFailureThatEndsTheSessionIsTheReason(t *testing.T) {
	t.Run("damaged history", func(t *testing.T) {
		h := newIPCHarness(t, nil)
		records := recordExecutions(h)
		f, start := parkHosted(t, h, h.session("hosted", "workspace"), nil)
		path := filepath.Join(f.project.root, "w", "n", "sessions", start.ExecutionID[:2], start.ExecutionID[2:4], start.ExecutionID+".jsonl")
		if err := os.WriteFile(path, []byte("not a history\n"), 0600); err != nil {
			t.Fatal(err)
		}
		f.seq[start.ExecutionID]++
		sequence := f.seq[start.ExecutionID]
		end := historyLine(9, "turnEnded", map[string]any{"stop_reason": "end_turn"})
		payload, _ := json.Marshal(map[string]any{"turn_id": "x", "history_sequence": 9, "record": end})
		f.in <- node.Message{Type: "thread_event", Version: 1, OperationID: start.OperationID, ExecutionID: start.ExecutionID, Sequence: &sequence, Payload: payload}
		nextIPCCommand(t, f, "end_session")
		f.terminal(start)
		if got := receiveIPC(t, records.ended); got[0] != start.ExecutionID || got[1] == execution.HostSessionEnded || !strings.HasPrefix(got[1], "history_") {
			t.Fatalf("settled %v", got)
		}
	})
	t.Run("changed MCP selection", func(t *testing.T) {
		h := newIPCHarness(t, nil)
		records := recordExecutions(h)
		session := h.session("hosted", "workspace")
		f, start := parkHosted(t, h, session, nil)
		session.MCP = &execution.MCPSelection{Bindings: []execution.MCPBinding{{PluginID: "official/other"}}}
		done := runHosted(h, ipcContext(t), session, "second", &scriptedTools{}, discardIPCEvent)
		nextIPCCommand(t, f, "end_session")
		f.terminal(start)
		receiveIPC(t, done)
		if got := receiveIPC(t, records.ended); got != [2]string{start.ExecutionID, "mcp_selection_invalid"} {
			t.Fatalf("settled %v", got)
		}
	})
	t.Run("withdrawn MCP authorization", func(t *testing.T) {
		h := newIPCHarness(t, nil)
		records := recordExecutions(h)
		f, start := parkHosted(t, h, h.session("hosted", "workspace"), nil)
		h.driver.opts.ReadMCPTarget = func(context.Context, execution.Scope, string, *execution.MCPSelection, int64) (*execution.MCPTarget, error) {
			return nil, &execution.MCPFailure{Code: "mcp_selection_invalid"}
		}
		h.driver.mu.Lock()
		a := h.driver.sessions["hosted"]
		h.driver.mu.Unlock()
		a.mu.Lock()
		a.mcpWake, a.mcpSync = make(chan struct{}, 1), make(chan struct{}, 1)
		a.mcpWake <- struct{}{}
		a.mu.Unlock()
		a.followMCP(ipcMCP())
		nextIPCCommand(t, f, "end_session")
		f.terminal(start)
		if got := receiveIPC(t, records.ended); got != [2]string{start.ExecutionID, "mcp_selection_invalid"} {
			t.Fatalf("settled %v", got)
		}
	})
}

// A lost Node's executions are settled at once: the loss is announced after one write's time, not
// after one per session.
func TestALostNodeSettlesItsHostExecutionsTogether(t *testing.T) {
	h := newIPCHarness(t, nil)
	recordExecutions(h)
	var writing sync.WaitGroup
	var f *fakeNodeIPC
	const sessions = 4
	writing.Add(sessions)
	release := make(chan struct{})
	h.driver.opts.EndHostExecution = func(context.Context, string, string) error {
		writing.Done()
		<-release
		return nil
	}
	for i := range sessions {
		f, _ = parkHosted(t, h, h.session(fmt.Sprint("s", i), fmt.Sprint("workspace-", i)), f)
	}
	p := f.project
	_ = f.Close()
	together := make(chan struct{})
	go func() { writing.Wait(); close(together) }()
	receiveIPC(t, together) // every write has begun while none has returned
	select {
	case <-p.dead:
		t.Fatal("the loss was announced before its executions were settled")
	default:
	}
	close(release)
	receiveIPC(t, p.dead)
}

// Node reports how an execution of an earlier process ended to whoever connects next. This process
// has no session for it and settles nothing.
func TestATerminalOfAnEarlierProcessSettlesNothing(t *testing.T) {
	h := newIPCHarness(t, nil)
	records := recordExecutions(h)
	f, _ := parkHosted(t, h, h.session("hosted", "workspace"), nil)
	for len(f.acked) > 0 {
		<-f.acked
	}
	sequence := uint64(1)
	f.in <- node.Message{Type: "agent_session_ended", Version: 1, OperationID: "op-earlier", ExecutionID: "earlier", Sequence: &sequence,
		Payload: json.RawMessage(`{"kind":"agent_session_ended","result":{"reason":"interrupted"}}`)}
	if acked := receiveIPC(t, f.acked); acked.ExecutionID != "earlier" {
		t.Fatalf("acknowledged %+v", acked)
	}
	records.none(t)
}
