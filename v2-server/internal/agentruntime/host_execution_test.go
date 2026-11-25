//go:build linux

package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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

// No one sees an execution being settled before its reason is listed: a caller that finds another one
// settling goes on, and may end the session at once.
func TestASettlingExecutionIsListedWithItsReason(t *testing.T) {
	h := newIPCHarness(t, nil)
	records := recordExecutions(h)
	f, start := parkHosted(t, h, h.session("hosted", "workspace"), nil)
	h.driver.mu.Lock()
	a := h.driver.sessions["hosted"]
	h.driver.mu.Unlock()
	entered, release := make(chan struct{}), make(chan struct{})
	releaseWrite := sync.OnceFunc(func() { close(release) })
	defer releaseWrite()
	settle := h.driver.opts.EndHostExecution
	h.driver.opts.EndHostExecution = func(ctx context.Context, id, reason string) error {
		close(entered)
		<-release
		return settle(ctx, id, reason)
	}
	marked := func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.hostSettled
	}
	// While the list cannot be written, the execution is not marked either.
	h.driver.mu.Lock()
	first := make(chan struct{})
	go func() { a.settleHost(execution.FailureNodeLost); close(first) }()
	for range 50 {
		if marked() {
			h.driver.mu.Unlock()
			t.Fatal("an execution is marked settled before its reason is listed")
		}
		time.Sleep(time.Millisecond)
	}
	h.driver.mu.Unlock()
	receiveIPC(t, entered)
	// The other caller decides nothing and does not wait for the write.
	second := make(chan struct{})
	go func() { a.settleHost("user_ended"); close(second) }()
	receiveIPC(t, second)
	if ends := h.driver.HostExecutionEnds("hosted"); len(ends) != 1 || ends[start.ExecutionID] != execution.FailureNodeLost {
		t.Fatalf("ends = %v", ends)
	}
	releaseWrite()
	receiveIPC(t, first)
	if end := receiveIPC(t, records.ended); end != [2]string{start.ExecutionID, execution.FailureNodeLost} {
		t.Fatalf("settled %v", end)
	}
	if ends := h.driver.HostExecutionEnds("hosted"); len(ends) != 0 {
		t.Fatalf("ends once written = %v", ends)
	}
	f.terminal(start)
}

// Nor between the two: whoever takes the driver's lock next finds both or neither. The lock is handed
// over in order, to the settler and then to the observer, which is what sync.Mutex does once the first
// of its waiters was woken in vain; a settler that released it halfway would queue behind the observer.
func TestNothingComesBetweenSettlingAnExecutionAndListingIt(t *testing.T) {
	h := newIPCHarness(t, nil)
	recordExecutions(h)
	f, start := parkHosted(t, h, h.session("hosted", "workspace"), nil)
	d := h.driver
	d.mu.Lock()
	a := d.sessions["hosted"]
	d.mu.Unlock()
	const wait = 5 * time.Millisecond
	observed := 0
	for range 20 {
		release := make(chan struct{})
		d.opts.EndHostExecution = func(context.Context, string, string) error { <-release; return nil }
		settled, listed := false, false
		first, observer := make(chan struct{}), make(chan struct{})
		d.mu.Lock()
		go func() { a.settleHost(execution.FailureNodeLost); close(first) }()
		time.Sleep(wait)
		go func() {
			d.mu.Lock()
			a.mu.Lock()
			settled = a.hostSettled
			a.mu.Unlock()
			_, listed = d.unsettled[start.ExecutionID]
			d.mu.Unlock()
			close(observer)
		}()
		time.Sleep(wait)
		d.mu.Unlock() // wakes the settler,
		d.mu.Lock()   // which finds the lock taken again
		time.Sleep(wait)
		d.mu.Unlock()
		receiveIPC(t, observer)
		close(release)
		receiveIPC(t, first)
		if settled && !listed {
			t.Fatal("an execution was seen settled before its reason was listed")
		}
		if settled {
			observed++
		}
		a.mu.Lock()
		a.hostSettled = false
		a.mu.Unlock()
	}
	if observed == 0 {
		t.Fatal("the observer never came after the settler")
	}
	f.terminal(start)
}

// Settling competes with the Node's loss, its reports and an End of the same session: whichever comes
// first, the reason of the one end is the one written, and everyone who went on could read it.
func TestSettlingRacesKeepOneReason(t *testing.T) {
	for range 30 {
		h := newIPCHarness(t, nil)
		records := recordExecutions(h)
		f, start := parkHosted(t, h, h.session("hosted", "workspace"), nil)
		h.driver.opts.EndHostExecution = func(context.Context, string, string) error { return errors.New("database unavailable") }
		h.driver.hostExecutionRetry = 20 * time.Millisecond
		f.terminalAs(start, "idle_timeout", "")
		go func() { _ = f.Close() }()
		if err := h.driver.End(ipcContext(t), "hosted"); err != nil {
			t.Fatal(err)
		}
		ends := h.driver.HostExecutionEnds("hosted")
		h.driver.mu.Lock()
		a := h.driver.sessions["hosted"]
		h.driver.mu.Unlock()
		a.mu.Lock()
		reason := a.hostReason
		a.mu.Unlock()
		if len(ends) != 1 || reason == "" || ends[start.ExecutionID] != reason {
			t.Fatalf("ends = %v, decided %q", ends, reason)
		}
		records.none(t)
	}
}

// The failure that makes the server end a session is the reason its execution ends, not the end it
// leads to.
func TestAFailureThatEndsTheSessionIsTheReason(t *testing.T) {
	t.Run("unwritable event", func(t *testing.T) {
		h := newIPCHarness(t, nil)
		records := recordExecutions(h)
		session := h.session("hosted", "workspace")
		f, start := parkHosted(t, h, session, nil)
		done := runHosted(h, ipcContext(t), session, "second", &scriptedTools{}, func(context.Context, execution.Event) error {
			return errors.New("synthetic persistence failure")
		})
		f.finishTurn(t, nextIPCCommand(t, f, "submit_user_turn"), "second", "never published")
		nextIPCCommand(t, f, "end_session")
		f.terminal(start)
		if got := receiveIPC(t, done); got.result.FailureCode != "agent_event_write_failed" {
			t.Fatalf("outcome = %+v", got)
		}
		if got := receiveIPC(t, records.ended); got != [2]string{start.ExecutionID, "agent_event_write_failed"} {
			t.Fatalf("settled %v", got)
		}
	})
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

// blockRecord makes the next record wait: recording receives its execution, and release lets it
// through. Writing it, the store fails with what release was given.
func (r *executionRecords) blockRecord(h *ipcHarness) (recording chan string, release chan error) {
	recording, release = make(chan string, 1), make(chan error, 1)
	record := h.driver.opts.RecordHostExecution
	h.driver.opts.RecordHostExecution = func(ctx context.Context, id, session string) error {
		recording <- id
		if err := <-release; err != nil {
			return err
		}
		return record(ctx, id, session)
	}
	return recording, release
}

// countStarts counts the starts the driver tries to send, delivered or not.
func countStarts(f *fakeNodeIPC) *atomic.Int32 {
	starts := &atomic.Int32{}
	fail := func(m node.Message) error {
		if m.Type == "start_agent_session" {
			starts.Add(1)
		}
		select {
		case <-f.closed: // as Node's client does; the fake's own select may still take a message
			return net.ErrClosed
		default:
			return nil
		}
	}
	f.failSend.Store(&fail)
	return starts
}

// What ends an execution while its record is written settles it once the record is there, and no start
// follows.
func TestWhatEndsAnExecutionWhileItIsRecordedSettlesIt(t *testing.T) {
	// begin parks a session on a Node, and starts another one there up to its record.
	begin := func(t *testing.T) (h *ipcHarness, records *executionRecords, f *fakeNodeIPC, starts *atomic.Int32, done <-chan ipcRunOutcome, id string, release chan error) {
		t.Helper()
		h = newIPCHarness(t, nil)
		records = recordExecutions(h)
		f, _ = parkHosted(t, h, h.session("other", "workspace-2"), nil)
		starts = countStarts(f)
		recording, release := records.blockRecord(h)
		done = runHosted(h, ipcContext(t), h.session("hosted", "workspace"), "first", &scriptedTools{}, discardIPCEvent)
		return h, records, f, starts, done, receiveIPC(t, recording), release
	}
	settled := func(t *testing.T, records *executionRecords, id, reason string) {
		t.Helper()
		for {
			if end := receiveIPC(t, records.ended); end[0] == id {
				if end[1] != reason {
					t.Fatalf("settled %v, want %s", end, reason)
				}
				break
			}
		}
	}
	t.Run("an End", func(t *testing.T) {
		h, records, _, starts, done, id, release := begin(t)
		h.driver.mu.Lock()
		a := h.driver.sessions["hosted"]
		h.driver.mu.Unlock()
		ending := make(chan error, 1)
		go func() { ending <- h.driver.End(ipcContext(t), "hosted") }()
		for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
			a.mu.Lock()
			arrived := a.ending
			a.mu.Unlock()
			if arrived {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("End did not arrive")
			}
		}
		records.none(t)
		release <- nil
		if got := receiveIPC(t, done); got.err == nil || !got.result.NotStarted {
			t.Fatalf("outcome = %+v", got)
		}
		if err := receiveIPC(t, ending); err != nil {
			t.Fatal(err)
		}
		settled(t, records, id, execution.HostSessionEnded)
		if starts.Load() != 0 {
			t.Fatal("a start was sent")
		}
	})
	t.Run("the loss of the Node", func(t *testing.T) {
		_, records, f, starts, done, id, release := begin(t)
		p := f.project
		_ = f.Close()
		receiveIPC(t, p.dead)
		release <- nil
		if got := receiveIPC(t, done); got.err == nil || got.result.FailureCode != execution.FailureNodeLost {
			t.Fatalf("outcome = %+v", got)
		}
		settled(t, records, id, execution.FailureNodeLost)
		if starts.Load() != 0 {
			t.Fatal("a start was sent")
		}
	})
	// The loss decided the reason, and is not announced yet: it waits for another session's write. The
	// start is still tried here, and fails on the closed connection.
	t.Run("the loss of the Node, not announced", func(t *testing.T) {
		h, records, f, _, done, id, release := begin(t)
		write, announce := h.driver.opts.EndHostExecution, make(chan struct{})
		announced := sync.OnceFunc(func() { close(announce) })
		defer announced()
		h.driver.opts.EndHostExecution = func(ctx context.Context, x, reason string) error {
			if x != id {
				<-announce
			}
			return write(ctx, x, reason)
		}
		h.driver.mu.Lock()
		a := h.driver.sessions["hosted"]
		h.driver.mu.Unlock()
		_ = f.Close()
		for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
			a.mu.Lock()
			decided := a.hostReason != ""
			a.mu.Unlock()
			if decided {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("the loss decided nothing")
			}
		}
		release <- nil
		settled(t, records, id, execution.FailureNodeLost)
		select {
		case <-f.project.dead:
			t.Fatal("the loss was announced before the other session's execution was settled")
		default:
		}
		announced()
		if got := receiveIPC(t, done); got.err == nil {
			t.Fatalf("outcome = %+v", got)
		}
	})
	// A loss settles before it is announced, so the driver does not get here by itself; submit settles
	// all the same.
	t.Run("a dead Node nobody settled for", func(t *testing.T) {
		_, records, f, starts, done, id, release := begin(t)
		f.project.deadOnce.Do(func() { close(f.project.dead) })
		release <- nil
		if got := receiveIPC(t, done); got.err == nil || got.result.FailureCode != execution.FailureNodeLost {
			t.Fatalf("outcome = %+v", got)
		}
		settled(t, records, id, execution.FailureNodeLost)
		if starts.Load() != 0 {
			t.Fatal("a start was sent")
		}
	})
}
