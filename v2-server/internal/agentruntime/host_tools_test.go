//go:build linux

package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/node"
)

// scriptedTools numbers what it issues, so a test can tell one sending from another.
type scriptedTools struct {
	mu                 sync.Mutex
	attached, instruct int
	session            string
	started            int
}

func (s *scriptedTools) Attach(session string) execution.HostServers {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attached++
	s.session = session
	credential := strings.Repeat("c", 31) + string(rune('0'+s.attached))
	return execution.HostServers{Servers: []execution.HostMCPServer{{Name: "ora-test", URL: "http://127.0.0.1:41234/mcp/" + credential, Headers: map[string]string{}}}, Secrets: []string{credential}}
}

func (s *scriptedTools) Started() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.started++
}

// starts is how often a start was said to be sent.
func (s *scriptedTools) starts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

func (s *scriptedTools) token(n int) string { return strings.Repeat("d", 31) + string(rune('0'+n)) }

func (s *scriptedTools) Instruct() execution.HostInstruction {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instruct++
	return execution.HostInstruction{Text: "<host>" + s.token(s.instruct) + "</host>", Token: s.token(s.instruct)}
}

func runHosted(h *ipcHarness, ctx context.Context, session execution.Session, turn string, host execution.HostTools, emit Emit) <-chan ipcRunOutcome {
	done := make(chan ipcRunOutcome, 1)
	go func() {
		result, err := h.driver.Run(ctx, h.scope, session, execution.Turn{Ref: execution.Ref{SessionID: session.ID, TurnID: turn}, Prompt: execution.TextPrompt("prompt"), Host: host}, emit)
		done <- ipcRunOutcome{result, err}
	}()
	return done
}

// sentText lists the text blocks of a start or a submitted turn.
func sentText(t *testing.T, m node.Message) []string {
	t.Helper()
	var payload struct {
		Spec struct {
			Turn struct{ Content []struct{ Text string } } `json:"initial_turn"`
		}
		Turn struct{ Content []struct{ Text string } }
	}
	if err := json.Unmarshal(m.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, c := range append(payload.Spec.Turn.Content, payload.Turn.Content...) {
		texts = append(texts, c.Text)
	}
	return texts
}

// The servers go out once, with the start; each turn ends with its own instruction block; and
// nothing published from Node's history (events, answers, the journal) carries either.
func TestHostToolsReachTheAgentAndNothingElse(t *testing.T) {
	h := newIPCHarness(t, nil)
	ctx := ipcContext(t)
	session := h.session("hosted", "workspace")
	var mu sync.Mutex
	var published []execution.Event
	emit := func(_ context.Context, e execution.Event) error {
		mu.Lock()
		defer mu.Unlock()
		published = append(published, e)
		return nil
	}
	tools := &scriptedTools{}
	done := runHosted(h, ctx, session, "first", tools, emit)
	f := receiveIPC(t, h.created)
	start := nextIPCCommand(t, f, "start_agent_session")
	var spec struct {
		Spec struct {
			Host []execution.HostMCPServer `json:"host_mcp_servers"`
		}
	}
	if err := json.Unmarshal(start.Payload, &spec); err != nil {
		t.Fatal(err)
	}
	credential := strings.Repeat("c", 31) + "1"
	if len(spec.Spec.Host) != 1 || spec.Spec.Host[0].URL != "http://127.0.0.1:41234/mcp/"+credential || tools.session != session.ID {
		t.Fatalf("host_mcp_servers = %s", start.Payload)
	}
	block := "<host>" + tools.token(1) + "</host>"
	if texts := sentText(t, start); len(texts) != 2 || texts[0] != "prompt" || texts[1] != block {
		t.Fatalf("content = %q", texts)
	}
	// Node records the block as its own user message, and the Agent's call carries the token.
	update := func(seq int, u map[string]any) json.RawMessage {
		return historyLine(seq, "update", map[string]any{"update": u})
	}
	f.history[start.ExecutionID] = []json.RawMessage{
		historyLine(0, "meta", map[string]any{"sessionId": start.ExecutionID, "schemaVersion": 2}),
		update(1, map[string]any{"sessionUpdate": "user_message_chunk", "content": map[string]string{"type": "text", "text": block}}),
		update(2, map[string]any{"sessionUpdate": "tool_call", "toolCallId": "x", "title": "complete " + tools.token(1), "rawInput": map[string]string{"attemptToken": tools.token(1)}}),
	}
	// The real Node's thread_event carries the record itself, which the journal would keep.
	path := filepath.Join(f.project.root, "w", "n", "sessions", start.ExecutionID[:2], start.ExecutionID[2:4], start.ExecutionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	var lines []byte
	for _, line := range f.history[start.ExecutionID] {
		lines = append(append(lines, line...), '\n')
	}
	if err := os.WriteFile(path, lines, 0600); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		f.seq[start.ExecutionID]++
		sequence := f.seq[start.ExecutionID]
		event, _ := json.Marshal(map[string]any{"turn_id": "first", "history_sequence": i, "record": f.history[start.ExecutionID][i]})
		f.in <- node.Message{Type: "thread_event", Version: 1, OperationID: start.OperationID, ExecutionID: start.ExecutionID, Sequence: &sequence, Payload: event}
		receiveIPC(t, f.acked)
	}
	preview, _ := json.Marshal(map[string]any{"version": 1, "incomplete": false, "blocks": []any{
		map[string]any{"history_sequence": 1, "record": f.history[start.ExecutionID][1]},
		map[string]any{"history_sequence": 2, "record": f.history[start.ExecutionID][2]},
	}})
	f.in <- node.Message{Type: "thread_preview", Version: 1, ExecutionID: start.ExecutionID, Payload: preview}
	f.finishTurn(t, start, "first", "token "+tools.token(1)+" at "+credential)
	first := receiveIPC(t, done)
	if first.err != nil || first.result.Answer != "token [redacted] at [redacted]" {
		t.Fatalf("first = %+v", first)
	}

	done = runHosted(h, ctx, session, "second", tools, emit)
	second := nextIPCCommand(t, f, "submit_user_turn")
	if strings.Contains(string(second.Payload), "host_mcp_servers") || tools.attached != 1 {
		t.Fatalf("a follow-up carried the servers: %s", second.Payload)
	}
	if texts := sentText(t, second); len(texts) != 2 || texts[1] != "<host>"+tools.token(2)+"</host>" {
		t.Fatalf("content = %q", texts)
	}
	// A command Node refused is sent again unchanged, as the same turn.
	h.driver.mu.Lock()
	a := h.driver.sessions[session.ID]
	h.driver.mu.Unlock()
	a.commandMu.Lock()
	err := a.submit(execution.Turn{Ref: execution.Ref{SessionID: session.ID, TurnID: "second"}, Host: tools}, "prompt", false)
	a.commandMu.Unlock()
	if again := nextIPCCommand(t, f, "submit_user_turn"); err != nil || string(again.Payload) != string(second.Payload) {
		t.Fatalf("resent %s, want %s: %v", again.Payload, second.Payload, err)
	}
	f.finishTurn(t, second, "second", "again "+tools.token(2))
	if got := receiveIPC(t, done); got.err != nil || got.result.Answer != "again [redacted]" {
		t.Fatalf("second = %+v", got)
	}
	receiveIPC(t, f.acked)
	receiveIPC(t, f.acked)

	mu.Lock()
	defer mu.Unlock()
	var data []byte
	data, err = json.Marshal(published)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := os.ReadFile(filepath.Join(f.project.root, "node-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"events": string(data), "journal": string(journal)} {
		for _, secret := range []string{credential, tools.token(1), tools.token(2)} {
			if strings.Contains(text, secret) {
				t.Errorf("%s contain %q", name, secret)
			}
		}
	}
	// Published events leave the block out altogether; the journal keeps it without its token.
	if strings.Contains(string(data), `u003chost`) || strings.Contains(string(data), "<host>") {
		t.Errorf("events contain the block: %s", data)
	}
	if !strings.Contains(string(data), `\"attemptToken\":\"[redacted]\"`) && !strings.Contains(string(data), `"attemptToken":"[redacted]"`) {
		t.Fatalf("the tool call was not published at all: %s", data)
	}
}

// Node replays what was not acknowledged to whoever dials next, so events of executions no session
// of this process runs are journaled as well, without the credentials an earlier process issued.
func TestTheJournalHasNoCredentialOfAnUnknownExecution(t *testing.T) {
	h := newIPCHarness(t, nil)
	done := startIPCRun(h, ipcContext(t), h.session("plain", "workspace"), "first", discardIPCEvent)
	f := receiveIPC(t, h.created)
	start := nextIPCCommand(t, f, "start_agent_session")
	token, credential := strings.Repeat("0123456789abcdef", 2), strings.Repeat("fedcba9876543210", 2)
	block := "<workflow_completion>\nWhen this step is complete, pass this attemptToken unchanged: " + token + "\n</workflow_completion>"
	quoted, _ := json.Marshal(map[string]string{"attemptToken": token})
	for i, u := range []map[string]any{
		{"sessionUpdate": "user_message_chunk", "content": map[string]string{"type": "text", "text": block}},
		{"sessionUpdate": "tool_call", "toolCallId": "x", "title": "complete " + token, "rawInput": map[string]string{"attemptToken": token}},
		{"sessionUpdate": "tool_call_update", "toolCallId": "x", "content": string(quoted)},
		{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "<b>" + token + "\n" + token + " at http://127.0.0.1:41234/mcp/" + credential}},
	} {
		// Node writes these records without escaping < and >; Go's encoder escapes them.
		record := string(historyLine(i, "update", map[string]any{"update": u}))
		for _, record := range []string{record, strings.NewReplacer(`\u003c`, "<", `\u003e`, ">").Replace(record)} {
			f.seq["gone"]++
			sequence := f.seq["gone"]
			f.in <- node.Message{Type: "thread_event", Version: 1, ExecutionID: "gone", Sequence: &sequence, Payload: json.RawMessage(`{"turn_id":"old","record":` + record + `}`)}
			receiveIPC(t, f.acked)
		}
	}
	f.finishTurn(t, start, "first", "done")
	if got := receiveIPC(t, done); got.err != nil {
		t.Fatal(got.err)
	}
	journal, err := os.ReadFile(filepath.Join(f.project.root, "node-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(journal), token) || strings.Contains(string(journal), credential) {
		t.Fatalf("journal = %s", journal)
	}
	if strings.Count(string(journal), `"gone"`) != 8 || !strings.Contains(string(journal), "agent_message_chunk") {
		t.Fatalf("journal lost events: %s", journal)
	}
}

// A document the replacement would break is dropped, not written broken.
func TestAnUnknownDocumentStaysValid(t *testing.T) {
	for _, data := range []string{`{"n":` + strings.Repeat("1", 40) + `}`, `{"a":"\f` + strings.Repeat("a", 32) + `"}`, `{"a":"\u00e9` + strings.Repeat("a", 32) + `"}`} {
		if out := censorUnknown([]byte(data)); !json.Valid(out) || strings.Contains(string(out), strings.Repeat("a", 32)) {
			t.Fatalf("%s = %s", data, out)
		}
	}
}

// A Node that does not take host MCP servers never gets the start, and the turn says why.
func TestHostToolsNeedANodeThatTakesThem(t *testing.T) {
	missing := "session_host_mcp_servers"
	h := newIPCHarness(t, func(f *fakeNodeIPC, _ node.Message) bool {
		f.missingCapability.Store(&missing)
		return false
	})
	ctx := ipcContext(t)
	if err := h.driver.CheckHostTools(ctx, h.scope, "project"); !errors.Is(err, execution.ErrHostMCPUnavailable) {
		t.Fatalf("check = %v", err)
	}
	tools := &scriptedTools{}
	got := receiveIPC(t, runHosted(h, ctx, h.session("hosted", "workspace"), "first", tools, discardIPCEvent))
	if got.result.FailureCode != execution.FailureHostMCPNode || !got.result.NotStarted || tools.attached != 0 || tools.instruct != 0 {
		t.Fatalf("outcome = %+v", got)
	}
	// A session without host tools still runs there.
	f := receiveIPC(t, h.created)
	done := startIPCRun(h, ctx, h.session("plain", "workspace-2"), "first", discardIPCEvent)
	start := nextIPCCommand(t, f, "start_agent_session")
	if strings.Contains(string(start.Payload), "host_mcp_servers") || len(sentText(t, start)) != 1 {
		t.Fatalf("a session without host tools sent %s", start.Payload)
	}
	f.finishTurn(t, start, "first", "done")
	if got := receiveIPC(t, done); got.err != nil {
		t.Fatal(got.err)
	}
}

func TestCheckHostToolsAcceptsACapableNode(t *testing.T) {
	h := newIPCHarness(t, nil)
	if err := h.driver.CheckHostTools(ipcContext(t), h.scope, "project"); err != nil {
		t.Fatal(err)
	}
}

// An Agent without HTTP MCP ends the session with Node's stable detail.
func TestAnAgentWithoutHTTPMCPKeepsNodesDetail(t *testing.T) {
	h := newIPCHarness(t, nil)
	done := runHosted(h, ipcContext(t), h.session("hosted", "workspace"), "first", &scriptedTools{}, discardIPCEvent)
	f := receiveIPC(t, h.created)
	f.terminalAs(nextIPCCommand(t, f, "start_agent_session"), "agent_failed", execution.FailureHostMCPAgent)
	if got := receiveIPC(t, done); got.result.FailureCode != execution.FailureHostMCPAgent || !got.result.AgentLost {
		t.Fatalf("outcome = %+v", got)
	}
}

// A host list Node would refuse is never sent: it would close the whole control connection.
func TestAnInvalidHostListIsNotSent(t *testing.T) {
	h := newIPCHarness(t, nil)
	got := receiveIPC(t, runHosted(h, ipcContext(t), h.session("hosted", "workspace"), "first", invalidTools{}, discardIPCEvent))
	if got.err == nil || got.result.Status != execution.StatusFailed || !got.result.NotStarted {
		t.Fatalf("outcome = %+v", got)
	}
	f := receiveIPC(t, h.created)
	for {
		select {
		case m := <-f.sent:
			if m.Type == "start_agent_session" {
				t.Fatal("sent")
			}
			continue
		default:
		}
		break
	}
}

type invalidTools struct{}

func (invalidTools) Attach(string) execution.HostServers {
	return execution.HostServers{Servers: []execution.HostMCPServer{{Name: "ora-test", URL: "http://localhost/mcp", Headers: map[string]string{}}}}
}
func (invalidTools) Started() {}
func (invalidTools) Instruct() execution.HostInstruction {
	return execution.HostInstruction{Text: "x", Token: "x"}
}
