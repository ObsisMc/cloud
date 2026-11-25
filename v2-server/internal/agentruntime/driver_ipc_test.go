//go:build linux

package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/node"
	"github.com/wanglongan587/cloud/v2-server/internal/nodehistory"
	"github.com/wanglongan587/cloud/v2-server/internal/nodeservices"
	"github.com/wanglongan587/cloud/v2-server/internal/rotatefile"
)

// The fake replaces only the IPC peer and supervisor proof. The dispatcher, control waits,
// session ownership, history reader, journal, publication and lifecycle are the real driver.
type fakeNodeIPC struct {
	project           *projectRuntime
	id                node.Identity
	in                chan node.Message
	sent              chan node.Message
	acked             chan node.Message
	closed            chan struct{}
	once              sync.Once
	stopOK            atomic.Bool
	stops             atomic.Int32
	history           map[string][]json.RawMessage
	seq               map[string]uint64
	hook              func(*fakeNodeIPC, node.Message) bool
	missingCapability atomic.Pointer[string]
	// turnCancel is the harness's: whether this Node negotiated turn_cancel.
	turnCancel *atomic.Bool
	// holdAck, when set, holds the ACK of the message types it names until the channel closes.
	holdAck atomic.Pointer[map[string]chan struct{}]
	// failSend, when set, fails the write of the commands it returns an error for.
	failSend atomic.Pointer[func(node.Message) error]
}

func (f *fakeNodeIPC) Supports(capability string) bool {
	if missing := f.missingCapability.Load(); missing != nil && *missing == capability {
		return false
	}
	switch capability {
	case "agent_session", "session_mcp", "session_mcp_lifecycle", "session_host_mcp_servers":
		return true
	case "turn_cancel":
		return f.turnCancel.Load()
	default:
		return false
	}
}

func (f *fakeNodeIPC) Identity() node.Identity { return f.id }
func (f *fakeNodeIPC) Close() error {
	f.once.Do(func() { close(f.closed) })
	return nil
}
func (f *fakeNodeIPC) Receive(ctx context.Context) (node.Message, error) {
	select {
	case <-ctx.Done():
		return node.Message{}, ctx.Err()
	case <-f.closed:
		return node.Message{}, net.ErrClosed
	case m := <-f.in:
		return m, nil
	}
}
func (f *fakeNodeIPC) Ack(ctx context.Context, m node.Message) error {
	if held := f.holdAck.Load(); held != nil {
		if gate, ok := (*held)[m.Type]; ok {
			select {
			case <-gate:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	select {
	case f.acked <- m:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (f *fakeNodeIPC) Send(ctx context.Context, m node.Message) error {
	if fail := f.failSend.Load(); fail != nil {
		if err := (*fail)(m); err != nil {
			return err
		}
	}
	select {
	case <-f.closed:
		return net.ErrClosed
	case <-ctx.Done():
		return ctx.Err()
	case f.sent <- m:
	}
	if f.hook != nil && f.hook(f, m) {
		return nil
	}
	switch m.Type {
	case "install_plugins":
		var request struct {
			Spec struct {
				Plugins []struct {
					ID      string `json:"plugin_id"`
					Version string
				}
			}
		}
		_ = json.Unmarshal(m.Payload, &request)
		id, version := "", ""
		if len(request.Spec.Plugins) == 1 {
			id, version = request.Spec.Plugins[0].ID, request.Spec.Plugins[0].Version
		}
		f.reply(m, "plugins_result", map[string]any{"kind": "plugins_completed", "result": map[string]any{"items": []any{map[string]any{"plugin_id": id, "outcome": map[string]string{"kind": "installed", "version": version}}}}})
	case "clone_repository":
		f.reply(m, "clone_result", map[string]any{"kind": "clone_ready", "result": map[string]string{"path": filepath.Join(f.project.root, "w", "r", m.ExecutionID), "commit": f.project.configuration.Commit}})
	}
	return nil
}
func (f *fakeNodeIPC) reply(command node.Message, kind string, payload any) {
	data, _ := json.Marshal(payload)
	f.in <- node.Message{Type: kind, Version: 1, OperationID: command.OperationID, ExecutionID: command.ExecutionID, Payload: data}
}
func (f *fakeNodeIPC) stop(context.Context) error {
	f.stops.Add(1)
	if !f.stopOK.Load() {
		return execution.ErrStopUnconfirmed
	}
	return f.Close()
}

type ipcHarness struct {
	driver  *Driver
	created chan *fakeNodeIPC
	config  Project
	scope   execution.Scope
	// turnCancel makes every Node the harness starts negotiate turn_cancel.
	turnCancel atomic.Bool
}

// ipcModel is the frozen model of the harness bindings.
func ipcModel() nodeservices.Model {
	return nodeservices.Model{ID: "fixture", Name: "Fixture", ContextWindow: 4096, MaxTokens: 256}
}

func newIPCHarness(t *testing.T, hook func(*fakeNodeIPC, node.Message) bool) *ipcHarness {
	t.Helper()
	return newIPCHarnessWith(t, map[string]nodeservices.Binding{
		"fixture":          {Model: ipcModel(), Protocol: nodeservices.OpenAICompletions, Provider: nodeservices.FixtureProvider{}},
		"messages-fixture": {Model: ipcModel(), Protocol: nodeservices.AnthropicMessages, Provider: nodeservices.MessagesFixtureProvider{}},
	}, hook)
}

// newIPCHarnessWith runs the driver on the given bindings, keyed "fixture" (OpenAI) and
// "messages-fixture" (Messages) like the default ones.
func newIPCHarnessWith(t *testing.T, bindings map[string]nodeservices.Binding, hook func(*fakeNodeIPC, node.Message) bool) *ipcHarness {
	t.Helper()
	root := t.TempDir()
	config := Project{MirrorPath: filepath.Join(root, "mirror.git"), Commit: "0123456789012345678901234567890123456789", SnapshotRef: "refs/snapshots/demo"}
	h := &ipcHarness{created: make(chan *fakeNodeIPC, 16), config: config, scope: execution.Scope{TenantID: "tenant", UserID: "user"}}
	d := &Driver{opts: Options{Root: filepath.Join(root, "projects"), Bindings: bindings, ResolveProject: func(context.Context, execution.Scope, string) (Project, error) { return config, nil },
		RecordHostExecution: func(context.Context, string, string) error { return nil }, EndHostExecution: func(context.Context, string, string) error { return nil }}, boot: "fake", projects: map[string]*projectRuntime{}, sessions: map[string]*agentSession{}, recorded: map[string]Checkout{}, controlTimeout: time.Second, mcpRefreshWait: 5 * time.Minute, turnCancelWait: defaultTurnCancelWait, turnCancelLimit: execution.TurnStopSettleLimit}
	h.driver = d
	d.startProject = func(p *projectRuntime) error {
		if err := os.MkdirAll(filepath.Join(p.root, "w", "n"), 0700); err != nil {
			return err
		}
		packagePath := filepath.Join(p.root, "test.orax")
		data := []byte("synthetic test package")
		if err := os.WriteFile(packagePath, data, 0600); err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		var err error
		p.services, err = nodeservices.Start(nodeservices.Options{StateDirectory: p.root, Repository: config.MirrorPath, GitBackend: "/bin/false", Packages: []nodeservices.Package{{Name: "test", Path: packagePath, SHA256: hex.EncodeToString(sum[:])}}, Bindings: bindings})
		if err != nil {
			return err
		}
		p.reader, err = nodehistory.Open(filepath.Join(p.root, "w", "n"))
		if err != nil {
			return err
		}
		p.journal, err = rotatefile.New(filepath.Join(p.root, "node-events.jsonl"), 64<<20, 3)
		if err != nil {
			return err
		}
		f := &fakeNodeIPC{project: p, id: node.Identity{NodeID: "node", IncarnationID: d.nextID("incarnation")}, in: make(chan node.Message, 64), sent: make(chan node.Message, 64), acked: make(chan node.Message, 64), closed: make(chan struct{}), history: map[string][]json.RawMessage{}, seq: map[string]uint64{}, hook: hook, turnCancel: &h.turnCancel}
		f.stopOK.Store(true)
		p.client, p.stop, p.receiveStarted = f, f.stop, true
		go p.receive()
		h.created <- f
		m, err := p.control(p.ctx, d.nextID("install"), "install_plugins", "plugins_result", map[string]string{"node_id": f.id.NodeID})
		if err != nil {
			return err
		}
		var result struct{ Kind string }
		if json.Unmarshal(m.Payload, &result) != nil || result.Kind != "plugins_completed" {
			return execution.ErrUnavailable
		}
		return nil
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := d.Close(ctx); err != nil {
			t.Errorf("driver cleanup: %v", err)
		}
	})
	return h
}

func (h *ipcHarness) session(id, workspace string) execution.Session {
	return execution.Session{ID: id, WorkspaceID: workspace, Target: &execution.Target{ProjectID: "project"}, Executor: &execution.Executor{ID: "executor", AgentID: pluginID, PluginVersion: pluginVersion, Model: execution.Model{ID: "fixture"}}}
}
func ipcContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func receiveIPC[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("fake IPC operation did not finish")
		var zero T
		return zero
	}
}
func nextIPCCommand(t *testing.T, f *fakeNodeIPC, kind string) node.Message {
	t.Helper()
	for {
		m := receiveIPC(t, f.sent)
		if m.Type == kind {
			return m
		}
	}
}
func historyLine(sequence int, kind string, extra map[string]any) json.RawMessage {
	v := map[string]any{"at": "2026-10-30T12:00:00Z", "seq": sequence, "type": kind}
	for k, value := range extra {
		v[k] = value
	}
	data, _ := json.Marshal(v)
	return data
}
func (f *fakeNodeIPC) finishTurn(t *testing.T, command node.Message, turnID, answer string) {
	t.Helper()
	f.finishTurnAs(t, command, turnID, answer, "end_turn")
}

// finishTurnAs settles the turn with Node's stop reason stop.
func (f *fakeNodeIPC) finishTurnAs(t *testing.T, command node.Message, turnID, answer, stop string) {
	t.Helper()
	lines := f.history[command.ExecutionID]
	if len(lines) == 0 {
		lines = append(lines, historyLine(0, "meta", map[string]any{"sessionId": command.ExecutionID, "schemaVersion": 2}))
	}
	lines = append(lines, historyLine(len(lines), "update", map[string]any{"update": map[string]any{"sessionUpdate": "user_message_chunk", "content": map[string]string{"type": "text", "text": "prompt"}}}))
	lines = append(lines, historyLine(len(lines), "update", map[string]any{"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": answer}}}))
	end := historyLine(len(lines), "turnEnded", map[string]any{"stop_reason": stop})
	lines = append(lines, end)
	f.history[command.ExecutionID] = lines
	path := filepath.Join(f.project.root, "w", "n", "sessions", command.ExecutionID[:2], command.ExecutionID[2:4], command.ExecutionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(file, string(line)); err != nil {
			file.Close()
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	f.seq[command.ExecutionID]++
	sequence := f.seq[command.ExecutionID]
	payload, _ := json.Marshal(map[string]any{"turn_id": turnID, "history_sequence": len(lines) - 1, "record": end})
	f.in <- node.Message{Type: "thread_event", Version: 1, OperationID: command.OperationID, ExecutionID: command.ExecutionID, Sequence: &sequence, Payload: payload}
}
func (f *fakeNodeIPC) terminal(command node.Message) { f.terminalAs(command, "user_ended", "") }

// terminalAs ends the execution with Node's reason and, when set, its bounded detail code.
func (f *fakeNodeIPC) terminalAs(command node.Message, reason, detail string) {
	f.seq[command.ExecutionID]++
	sequence := f.seq[command.ExecutionID]
	result := map[string]any{"reason": reason, "node": f.id}
	if detail != "" {
		result["detail"] = detail
	}
	payload, _ := json.Marshal(map[string]any{"kind": "agent_session_ended", "result": result})
	f.in <- node.Message{Type: "agent_session_ended", Version: 1, OperationID: command.OperationID, ExecutionID: command.ExecutionID, Sequence: &sequence, Payload: payload}
}

type ipcRunOutcome struct {
	result execution.Result
	err    error
}

func startIPCRun(h *ipcHarness, ctx context.Context, session execution.Session, turn string, emit Emit) <-chan ipcRunOutcome {
	done := make(chan ipcRunOutcome, 1)
	go func() {
		result, err := h.driver.Run(ctx, h.scope, session, execution.Turn{Ref: execution.Ref{SessionID: session.ID, TurnID: turn}, Prompt: execution.TextPrompt("prompt")}, emit)
		done <- ipcRunOutcome{result, err}
	}()
	return done
}
func discardIPCEvent(context.Context, execution.Event) error { return nil }

func TestDriverIPCTurnFailurePublishesOnceAndKeepsSession(t *testing.T) {
	h := newIPCHarness(t, nil)
	ctx := ipcContext(t)
	session := h.session("session", "workspace")
	events := make(chan execution.Event, 4)
	emit := func(_ context.Context, event execution.Event) error {
		if event.Kind != execution.EventPreparation {
			events <- event
		}
		return nil
	}
	first := startIPCRun(h, ctx, session, "first", emit)
	f := receiveIPC(t, h.created)
	start := nextIPCCommand(t, f, "start_agent_session")
	for i, turn := range []string{"first", "second"} {
		command, done := start, first
		payload := map[string]string{"turn_id": turn, "code": "mcp_refresh_wait_timeout"}
		if i > 0 {
			done = startIPCRun(h, ctx, session, turn, emit)
			command = nextIPCCommand(t, f, "submit_user_turn")
			var submission struct {
				CommandID string `json:"command_id"`
			}
			if err := json.Unmarshal(command.Payload, &submission); err != nil || submission.CommandID == "" {
				t.Fatal("missing command ID", err)
			}
			payload["command_id"] = submission.CommandID
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		seq := uint64(i + 1)
		frame := node.Message{Type: "session_turn_failed", Version: 1, OperationID: command.OperationID, ExecutionID: command.ExecutionID, Sequence: &seq, Payload: raw}
		f.in <- frame
		event := receiveIPC(t, events)
		// Node failed the queued turn before the Agent was given it.
		if event.Kind != execution.EventTurnEnded || event.Ref.TurnID != turn || event.Result == nil || event.Result.FailureCode != payload["code"] || !event.Result.NotDelivered {
			t.Fatalf("turn failure publication = %+v", event)
		}
		receiveIPC(t, f.acked)
		got := receiveIPC(t, done)
		if got.err != nil || got.result.Status != execution.StatusFailed || got.result.AgentLost || got.result.FailureCode != payload["code"] || !got.result.NotDelivered {
			t.Fatalf("turn failure = %+v", got)
		}
		f.in <- frame
		receiveIPC(t, f.acked)
		select {
		case duplicate := <-events:
			t.Fatalf("replay published twice: %+v", duplicate)
		default:
		}
	}
}

func TestDriverIPCFullTurnPublicationAppendAndEnd(t *testing.T) {
	h := newIPCHarness(t, nil)
	ctx := ipcContext(t)
	session := h.session("session", "workspace")
	publicationStarted, publish := make(chan struct{}), make(chan struct{})
	done := startIPCRun(h, ctx, session, "first", func(ctx context.Context, event execution.Event) error {
		if event.Kind == execution.EventTurnEnded {
			close(publicationStarted)
			select {
			case <-publish:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})
	f := receiveIPC(t, h.created)
	start := nextIPCCommand(t, f, "start_agent_session")
	f.finishTurn(t, start, "first", "complete answer")
	receiveIPC(t, publicationStarted)
	select {
	case ack := <-f.acked:
		t.Fatalf("ACK before terminal publication: %+v", ack)
	case result := <-done:
		t.Fatalf("Run returned before terminal publication: %+v", result)
	default:
	}
	close(publish)
	ack := receiveIPC(t, f.acked)
	if ack.ExecutionID != start.ExecutionID || ack.Sequence == nil || *ack.Sequence != 1 {
		t.Fatalf("wrong retained ACK: %+v", ack)
	}
	first := receiveIPC(t, done)
	if first.err != nil || first.result.Status != execution.StatusCompleted || first.result.Answer != "complete answer" || !first.result.AnswerComplete {
		t.Fatalf("first turn: %+v", first)
	}
	secondDone := startIPCRun(h, ctx, session, "second", discardIPCEvent)
	appendCommand := nextIPCCommand(t, f, "submit_user_turn")
	if appendCommand.ExecutionID != start.ExecutionID {
		t.Fatal("Append silently replaced physical Agent")
	}
	f.finishTurn(t, appendCommand, "second", "second answer")
	second := receiveIPC(t, secondDone)
	if second.err != nil || second.result.Answer != "second answer" || !second.result.AnswerComplete {
		t.Fatalf("second turn: %+v", second)
	}
	receiveIPC(t, f.acked)
	ended := make(chan error, 1)
	go func() { ended <- h.driver.End(ctx, session.ID) }()
	endCommand := nextIPCCommand(t, f, "end_session")
	select {
	case err := <-ended:
		t.Fatalf("End completed before Node terminal: %v", err)
	default:
	}
	f.terminal(endCommand)
	if err := receiveIPC(t, ended); err != nil {
		t.Fatal(err)
	}
	if err := h.driver.End(ctx, session.ID); err != nil {
		t.Fatalf("End is not idempotent: %v", err)
	}
}

// A turn the Agent ended without completing (a refusal) reports no error and leaves the physical
// Agent ready for the next turn, so a held workflow session can keep talking. A session that ends
// while a turn runs reports the turn as AgentLost with ErrSessionEnded, so nobody keeps it.
func TestDriverIPCAgentEndedTurnKeepsTheAgentButALostSessionDoesNot(t *testing.T) {
	h := newIPCHarness(t, nil)
	ctx := ipcContext(t)
	session := h.session("session", "workspace")
	done := startIPCRun(h, ctx, session, "first", discardIPCEvent)
	f := receiveIPC(t, h.created)
	start := nextIPCCommand(t, f, "start_agent_session")
	f.finishTurnAs(t, start, "first", "I cannot", "refusal")
	first := receiveIPC(t, done)
	if first.err != nil || first.result.Status != execution.StatusFailed || first.result.FailureCode != "agent_turn_incomplete" || first.result.AgentLost {
		t.Fatalf("refused turn: %+v", first)
	}
	receiveIPC(t, f.acked)
	secondDone := startIPCRun(h, ctx, session, "second", discardIPCEvent)
	appendCommand := nextIPCCommand(t, f, "submit_user_turn")
	if appendCommand.ExecutionID != start.ExecutionID {
		t.Fatal("the turn after a refusal replaced the physical Agent")
	}
	f.terminal(appendCommand)
	second := receiveIPC(t, secondDone)
	if !errors.Is(second.err, execution.ErrSessionEnded) || !second.result.AgentLost || second.result.NotStarted || second.result.Status != execution.StatusCancelled {
		t.Fatalf("turn of a session that ended under it: %+v", second)
	}
}

// Claude Code is installed on first use with its declaration binding, and its session carries
// the frozen configuration Node re-proves before granting Messages access.
func TestDriverIPCClaudeSessionInstallsAndFreezesItsConfiguration(t *testing.T) {
	h := newIPCHarness(t, nil)
	ctx := ipcContext(t)
	claude := h.session("claude", "workspace")
	claude.Executor.AgentID, claude.Executor.PluginVersion = "official/ora-space.claude", "0.4.5"
	mismatched := claude
	mismatched.ID, mismatched.WorkspaceID = "mismatched", "other"
	if got := receiveIPC(t, startIPCRun(h, ctx, mismatched, "first", discardIPCEvent)); !errors.Is(got.err, execution.ErrUnsupported) {
		t.Fatalf("Claude Code on an OpenAI binding=%+v", got)
	}
	executor := *claude.Executor
	executor.Model.ID = "messages-fixture"
	claude.Executor = &executor
	done := startIPCRun(h, ctx, claude, "first", discardIPCEvent)
	f := receiveIPC(t, h.created)
	pkg, err := nodeservices.HostClaudePackage()
	if err != nil {
		t.Fatal(err)
	}
	var install struct {
		Spec struct {
			Plugins []struct {
				PluginID           string `json:"plugin_id"`
				Version            string
				AgentConfiguration map[string]any `json:"agent_configuration"`
			}
		}
	}
	for {
		m := nextIPCCommand(t, f, "install_plugins")
		if err := json.Unmarshal(m.Payload, &install); err != nil {
			t.Fatal(err)
		}
		// The harness installs its own synthetic package first.
		if len(install.Spec.Plugins) == 1 && install.Spec.Plugins[0].PluginID == "official/ora-space.claude" {
			break
		}
	}
	if got, want := install.Spec.Plugins[0].AgentConfiguration, map[string]any{"schema_version": 2.0, "declaration_sha256": nodeservices.ClaudeDeclarationSHA256}; install.Spec.Plugins[0].Version != "0.4.5" || !reflect.DeepEqual(got, want) {
		t.Fatalf("Claude Code install=%+v", install.Spec.Plugins[0])
	}
	command := nextIPCCommand(t, f, "start_agent_session")
	var start struct {
		Spec struct {
			PluginID           string         `json:"agent_plugin_id"`
			Binding            string         `json:"model_binding_id"`
			AgentConfiguration map[string]any `json:"agent_configuration"`
		}
	}
	if err := json.Unmarshal(command.Payload, &start); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"plugin_id": "official/ora-space.claude", "plugin_version": "0.4.5", "target": pkg.Target, "package_sha256": pkg.SHA256,
		"schema_version": 2.0, "declaration_fingerprint": nodeservices.ClaudeDeclarationSHA256, "adapter_version": 1.0, "configuration_revision": 0.0,
		"settings_json": "{}", "protocol": "anthropic-messages",
		"model": map[string]any{"id": "fixture", "name": "Fixture", "context_window": 4096.0, "max_tokens": 256.0},
	}
	if start.Spec.PluginID != "official/ora-space.claude" || start.Spec.Binding != "messages-fixture" || !reflect.DeepEqual(start.Spec.AgentConfiguration, want) {
		t.Fatalf("Claude Code start=%+v", start.Spec)
	}
	f.finishTurn(t, command, "first", "claude answer")
	if got := receiveIPC(t, done); got.err != nil || got.result.Answer != "claude answer" {
		t.Fatalf("Claude Code turn=%+v", got)
	}
}

func TestDriverIPCRetriesFailedPreparationAndWorkspaceReservation(t *testing.T) {
	var cloneCalls atomic.Int32
	h := newIPCHarness(t, func(f *fakeNodeIPC, m node.Message) bool {
		if m.Type == "clone_repository" && cloneCalls.Add(1) == 1 {
			f.reply(m, "clone_result", map[string]string{"kind": "clone_failed"})
			return true
		}
		return false
	})
	ctx := ipcContext(t)
	session := h.session("session", "workspace")
	first := receiveIPC(t, startIPCRun(h, ctx, session, "first", discardIPCEvent))
	if !errors.Is(first.err, execution.ErrUnavailable) || !first.result.NotStarted {
		t.Fatalf("transient clone failure = %+v, want a turn marked NotStarted", first)
	}
	f := receiveIPC(t, h.created)
	failedClone := nextIPCCommand(t, f, "clone_repository")
	done := startIPCRun(h, ctx, session, "retry", discardIPCEvent)
	retriedClone := nextIPCCommand(t, f, "clone_repository")
	if retriedClone.ExecutionID == failedClone.ExecutionID {
		t.Fatal("clone retry reused execution ID; a late reply could satisfy it")
	}
	command := nextIPCCommand(t, f, "start_agent_session")
	f.finishTurn(t, command, "retry", "recovered")
	result := receiveIPC(t, done)
	if result.err != nil || result.result.Answer != "recovered" || cloneCalls.Load() != 2 {
		t.Fatalf("same session/workspace did not recover: %+v cloneCalls=%d", result, cloneCalls.Load())
	}
	// Another logical session cannot acquire this checkout before a valid physical End.
	busy := receiveIPC(t, startIPCRun(h, ctx, h.session("other", "workspace"), "busy", discardIPCEvent))
	if !errors.Is(busy.err, execution.ErrWorkspaceBusy) {
		t.Fatalf("live checkout was shared: %+v", busy)
	}
	ended := make(chan error, 1)
	go func() { ended <- h.driver.End(ctx, session.ID) }()
	f.terminal(nextIPCCommand(t, f, "end_session"))
	if err := receiveIPC(t, ended); err != nil {
		t.Fatal(err)
	}
	done = startIPCRun(h, ctx, h.session("other", "workspace"), "after-end", discardIPCEvent)
	command = nextIPCCommand(t, f, "start_agent_session")
	f.finishTurn(t, command, "after-end", "next session")
	if got := receiveIPC(t, done); got.err != nil || got.result.Answer != "next session" {
		t.Fatalf("checkout not released after confirmed End: %+v", got)
	}
}

func TestDriverIPCInitialFailureCanRetrySameSession(t *testing.T) {
	var installs atomic.Int32
	h := newIPCHarness(t, func(f *fakeNodeIPC, m node.Message) bool {
		if m.Type == "install_plugins" && installs.Add(1) == 1 {
			f.reply(m, "plugins_result", map[string]string{"kind": "plugins_failed"})
			return true
		}
		return false
	})
	ctx := ipcContext(t)
	session := h.session("session", "workspace")
	got := receiveIPC(t, startIPCRun(h, ctx, session, "first", discardIPCEvent))
	if !errors.Is(got.err, execution.ErrUnavailable) {
		t.Fatalf("install failure: %+v", got)
	}
	old := receiveIPC(t, h.created)
	done := startIPCRun(h, ctx, session, "retry", discardIPCEvent)
	f := receiveIPC(t, h.created)
	if f.id == old.id || old.stops.Load() == 0 {
		t.Fatal("retry did not stop failed incarnation before replacing it")
	}
	command := nextIPCCommand(t, f, "start_agent_session")
	f.finishTurn(t, command, "retry", "recovered install")
	if got := receiveIPC(t, done); got.err != nil || got.result.Answer != "recovered install" {
		t.Fatalf("init failure remained cached: %+v", got)
	}
}

func TestDriverIPCDisconnectRequiresStopProofBeforeRebuild(t *testing.T) {
	h := newIPCHarness(t, nil)
	ctx := ipcContext(t)
	done := startIPCRun(h, ctx, h.session("first", "workspace"), "turn", discardIPCEvent)
	old := receiveIPC(t, h.created)
	nextIPCCommand(t, old, "start_agent_session")
	old.stopOK.Store(false)
	old.Close()
	if got := receiveIPC(t, done); got.result.FailureCode != "agent_transport_closed" || got.err == nil {
		t.Fatalf("disconnect result: %+v", got)
	}
	got := receiveIPC(t, startIPCRun(h, ctx, h.session("next", "workspace"), "blocked", discardIPCEvent))
	if !errors.Is(got.err, execution.ErrUnavailable) || errors.Is(got.err, execution.ErrWorkspaceBusy) {
		t.Fatalf("unconfirmed dead project = %+v", got)
	}
	select {
	case <-h.created:
		t.Fatal("replacement started without supervisor stop proof")
	default:
	}
	if err := h.driver.End(ctx, "first"); !errors.Is(err, execution.ErrStopUnconfirmed) {
		t.Fatalf("disconnect falsely confirmed old session stopped: %v", err)
	}
	old.stopOK.Store(true)
	done = startIPCRun(h, ctx, h.session("next", "workspace"), "retry", discardIPCEvent)
	f := receiveIPC(t, h.created)
	if f.id == old.id || old.stops.Load() < 2 {
		t.Fatal("replacement did not retry supervisor stop")
	}
	command := nextIPCCommand(t, f, "start_agent_session")
	f.finishTurn(t, command, "retry", "after disconnect")
	if got := receiveIPC(t, done); got.err != nil || got.result.Answer != "after disconnect" {
		t.Fatalf("dead project did not rebuild: %+v", got)
	}
	if err := h.driver.End(ctx, "first"); err != nil {
		t.Fatalf("supervisor stop proof did not settle old session: %v", err)
	}
}

func TestDriverIPCEndDisconnectedSessionStopsProjectBeforeWorkspaceReuse(t *testing.T) {
	h := newIPCHarness(t, nil)
	ctx := ipcContext(t)
	session := h.session("first", "workspace")
	done := startIPCRun(h, ctx, session, "turn", discardIPCEvent)
	old := receiveIPC(t, h.created)
	nextIPCCommand(t, old, "start_agent_session")
	old.stopOK.Store(false)
	t.Cleanup(func() { old.stopOK.Store(true) })
	old.Close()
	if got := receiveIPC(t, done); got.result.FailureCode != "agent_transport_closed" || got.err == nil {
		t.Fatalf("disconnect result: %+v", got)
	}
	if err := h.driver.End(ctx, session.ID); !errors.Is(err, execution.ErrStopUnconfirmed) {
		t.Fatalf("End without supervisor proof: %v", err)
	}
	if old.stops.Load() != 1 {
		t.Fatalf("End did not request supervisor stop: calls=%d", old.stops.Load())
	}
	h.driver.mu.Lock()
	retained := h.driver.projects["project"] == old.project
	a := h.driver.sessions[session.ID]
	h.driver.mu.Unlock()
	old.project.mu.Lock()
	_, owner := old.project.owners[session.WorkspaceID][session.ID]
	old.project.mu.Unlock()
	if !retained || !owner {
		t.Fatalf("unconfirmed End released project or workspace: retained=%v owner=%v", retained, owner)
	}
	select {
	case <-a.stopped:
		t.Fatal("unconfirmed End marked physical session stopped")
	default:
	}

	// End alone must recover: no other Run or Driver.Close supplies the stop proof.
	old.stopOK.Store(true)
	ended := make(chan error, 2)
	for range 2 {
		go func() { ended <- h.driver.End(ctx, session.ID) }()
	}
	for range 2 {
		if err := receiveIPC(t, ended); err != nil {
			t.Fatalf("End retry after stop became available: %v", err)
		}
	}
	h.driver.mu.Lock()
	remaining := h.driver.projects["project"]
	h.driver.mu.Unlock()
	if remaining != nil {
		t.Fatal("confirmed End retained the dead project")
	}
	if err := h.driver.End(ctx, session.ID); err != nil || old.stops.Load() != 2 {
		t.Fatalf("End was not idempotent: err=%v supervisorCalls=%d", err, old.stops.Load())
	}
	done = startIPCRun(h, ctx, h.session("next", "workspace"), "next-turn", discardIPCEvent)
	f := receiveIPC(t, h.created)
	if f.id == old.id {
		t.Fatal("next session reused the stopped incarnation")
	}
	command := nextIPCCommand(t, f, "start_agent_session")
	f.finishTurn(t, command, "next-turn", "workspace reused")
	if got := receiveIPC(t, done); got.err != nil || got.result.Answer != "workspace reused" {
		t.Fatalf("same workspace did not recover after End: %+v", got)
	}
}

func TestDriverIPCEndDisconnectedSessionHonorsCloseWaitContext(t *testing.T) {
	h := newIPCHarness(t, nil)
	ctx := ipcContext(t)
	session := h.session("session", "workspace")
	done := startIPCRun(h, ctx, session, "turn", discardIPCEvent)
	f := receiveIPC(t, h.created)
	nextIPCCommand(t, f, "start_agent_session")
	f.Close()
	receiveIPC(t, done)
	if err := f.project.lockClose(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.project.unlockClose)
	ending, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if err := h.driver.End(ending, session.ID); !errors.Is(err, execution.ErrStopUnconfirmed) {
		t.Fatalf("End while another stop owns the gate: %v", err)
	}
	if !errors.Is(ending.Err(), context.DeadlineExceeded) {
		t.Fatal("End did not wait for supervisor proof until the caller's deadline")
	}
	if f.stops.Load() != 0 {
		t.Fatalf("End bypassed another stop's gate: calls=%d", f.stops.Load())
	}
}

func TestDriverIPCControlTimeoutRetiresIncarnation(t *testing.T) {
	var clones atomic.Int32
	h := newIPCHarness(t, func(_ *fakeNodeIPC, m node.Message) bool {
		return m.Type == "clone_repository" && clones.Add(1) == 1
	})
	h.driver.controlTimeout = 100 * time.Millisecond
	ctx := ipcContext(t)
	session := h.session("session", "workspace")
	got := receiveIPC(t, startIPCRun(h, ctx, session, "timeout", discardIPCEvent))
	if !errors.Is(got.err, execution.ErrUnavailable) {
		t.Fatalf("missing clone reply: %+v", got)
	}
	old := receiveIPC(t, h.created)
	late := nextIPCCommand(t, old, "clone_repository")
	done := startIPCRun(h, ctx, session, "retry", discardIPCEvent)
	f := receiveIPC(t, h.created)
	command := nextIPCCommand(t, f, "start_agent_session")
	if f.id == old.id || old.stops.Load() == 0 {
		t.Fatal("timed-out clone's incarnation was reused")
	}
	// A valid-looking late reply on the retired transport cannot affect the new request.
	old.reply(late, "clone_result", map[string]any{"kind": "clone_ready", "result": map[string]string{"path": "/untrusted/late", "commit": h.config.Commit}})
	f.finishTurn(t, command, "retry", "after timeout")
	if got := receiveIPC(t, done); got.err != nil || got.result.Answer != "after timeout" {
		t.Fatalf("clone timeout remained cached or accepted late reply: %+v", got)
	}
}

func TestDriverIPCPublicationFailureNeverAcknowledgesHistory(t *testing.T) {
	h := newIPCHarness(t, nil)
	ctx := ipcContext(t)
	session := h.session("session", "workspace")
	done := startIPCRun(h, ctx, session, "turn", func(context.Context, execution.Event) error {
		return errors.New("synthetic persistence failure")
	})
	f := receiveIPC(t, h.created)
	command := nextIPCCommand(t, f, "start_agent_session")
	f.finishTurn(t, command, "turn", "must not be published")
	got := receiveIPC(t, done)
	if got.err == nil || got.result.Status != execution.StatusFailed || got.result.FailureCode != "agent_event_write_failed" || got.result.AnswerComplete {
		t.Fatalf("publication failure reported success: %+v", got)
	}
	endCommand := nextIPCCommand(t, f, "end_session")
	f.terminal(endCommand)
	if err := h.driver.End(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-f.acked:
		t.Fatalf("ACK after failed publication: %+v", m)
	default:
	}
}

func TestDriverIPCInstallationTimeoutCanRebuild(t *testing.T) {
	var installs atomic.Int32
	h := newIPCHarness(t, func(_ *fakeNodeIPC, m node.Message) bool {
		return m.Type == "install_plugins" && installs.Add(1) == 1
	})
	h.driver.controlTimeout = 100 * time.Millisecond
	ctx := ipcContext(t)
	session := h.session("session", "workspace")
	if got := receiveIPC(t, startIPCRun(h, ctx, session, "timeout", discardIPCEvent)); !errors.Is(got.err, execution.ErrUnavailable) {
		t.Fatalf("missing plugin reply did not fail: %+v", got)
	}
	old := receiveIPC(t, h.created)
	done := startIPCRun(h, ctx, session, "retry", discardIPCEvent)
	f := receiveIPC(t, h.created)
	if f.id == old.id || old.stops.Load() == 0 {
		t.Fatal("silent installation retained its incarnation")
	}
	command := nextIPCCommand(t, f, "start_agent_session")
	f.finishTurn(t, command, "retry", "after installation timeout")
	if got := receiveIPC(t, done); got.err != nil || got.result.Answer != "after installation timeout" {
		t.Fatalf("installation timeout remained cached: %+v", got)
	}
}

func TestProjectCloseHonorsContextWhileAnotherStopOwnsGate(t *testing.T) {
	ready := make(chan struct{})
	close(ready)
	lifetime, stop := context.WithCancel(context.Background())
	defer stop()
	p := &projectRuntime{ready: ready, ctx: lifetime, cancel: stop}
	if err := p.lockClose(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer p.unlockClose()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.close(ctx); !errors.Is(err, execution.ErrStopUnconfirmed) {
		t.Fatalf("close waited past caller cancellation: %v", err)
	}
}

// Previews stream as pending-only history_preview events: coalesced to one per interval, the last
// one before a pause still published, never ACKed, and superseded by the settled snapshot.
func TestDriverIPCPreviewsStreamPendingRecordsWithoutAck(t *testing.T) {
	defer func(interval time.Duration) { previewInterval = interval }(previewInterval)
	previewInterval = 100 * time.Millisecond
	h := newIPCHarness(t, nil)
	ctx := ipcContext(t)
	session := h.session("session", "workspace")
	var mu sync.Mutex
	var events []execution.Event
	published := make(chan execution.Event, 64)
	done := startIPCRun(h, ctx, session, "turn", func(_ context.Context, event execution.Event) error {
		if event.Kind == execution.EventPreparation {
			return nil
		}
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
		published <- event
		return nil
	})
	f := receiveIPC(t, h.created)
	start := nextIPCCommand(t, f, "start_agent_session")
	preview := func(version int, text string) {
		block := map[string]any{"history_sequence": 2, "turn_id": "turn", "record": map[string]any{"type": "update", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": text}}}}
		payload, _ := json.Marshal(map[string]any{"version": version, "incomplete": false, "blocks": []any{block}})
		f.in <- node.Message{Type: "thread_preview", Version: 1, OperationID: start.OperationID, ExecutionID: start.ExecutionID, Payload: payload}
	}
	text := ""
	for version := 1; version <= 20; version++ {
		text += "字"
		preview(version, text)
	}
	// The burst ends without settling: the trailing flush must still show the newest text.
	var last execution.Event
	for {
		last = receiveIPC(t, published)
		if last.Kind != execution.EventHistoryPreview {
			t.Fatalf("unexpected event %+v", last)
		}
		var line struct {
			Update struct{ Content struct{ Text string } }
		}
		if len(last.History.Records) != 1 || !last.History.Records[0].Preview || last.History.Records[0].Sequence != 2 || json.Unmarshal(last.History.Records[0].Data, &line) != nil {
			t.Fatalf("preview event %+v", last.History)
		}
		if last.Ref.TurnID != "turn" {
			t.Fatalf("preview turn %q", last.Ref.TurnID)
		}
		if line.Update.Content.Text == text {
			break
		}
	}
	mu.Lock()
	burst := len(events)
	mu.Unlock()
	if burst > 3 {
		t.Fatalf("%d preview events for one burst; previews were not coalesced", burst)
	}
	// Repeating the same pending set retains nothing new.
	preview(21, text)
	time.Sleep(3 * previewInterval)
	select {
	case event := <-published:
		t.Fatalf("unchanged preview retained: %+v", event)
	case m := <-f.acked:
		t.Fatalf("preview ACKed: %+v", m)
	default:
	}
	f.finishTurn(t, start, "turn", text)
	got := receiveIPC(t, done)
	if got.err != nil || got.result.Answer != text || !got.result.AnswerComplete {
		t.Fatalf("turn: %+v", got)
	}
	snapshot := receiveIPC(t, published)
	if snapshot.Kind != execution.EventHistorySnapshot {
		t.Fatalf("settled history published as %q", snapshot.Kind)
	}
	for _, record := range snapshot.History.Records {
		if record.Preview {
			t.Fatalf("settled snapshot kept a preview: %+v", snapshot.History)
		}
	}
}

func TestDemoWarmCheckoutIsClaimedOnceByChat(t *testing.T) {
	h := newIPCHarness(t, nil)
	ctx := ipcContext(t)
	h.driver.WarmDemo(ctx, "project", h.config)
	f := receiveIPC(t, h.created)
	first := nextIPCCommand(t, f, "clone_repository")
	run := func(id string, kind execution.CallerKind) string {
		done := make(chan ipcRunOutcome, 1)
		go func() {
			r, e := h.driver.Run(ctx, h.scope, h.session(id, id), execution.Turn{Caller: execution.Caller{Kind: kind}, Ref: execution.Ref{SessionID: id, TurnID: id}, Prompt: execution.TextPrompt("prompt")}, discardIPCEvent)
			done <- ipcRunOutcome{r, e}
		}()
		cmd := nextIPCCommand(t, f, "start_agent_session")
		var payload struct {
			Spec struct {
				Checkout string `json:"checkout_execution_id"`
			}
		}
		if err := json.Unmarshal(cmd.Payload, &payload); err != nil || payload.Spec.Checkout == "" {
			t.Fatalf("missing checkout: %v", err)
		}
		f.finishTurn(t, cmd, id, "OK")
		if result := receiveIPC(t, done); result.err != nil || result.result.Answer != "OK" {
			t.Fatalf("failed turn: %+v", result)
		}
		return payload.Spec.Checkout
	}
	if got := run("workflow", execution.CallerWorkflow); got == first.ExecutionID {
		t.Fatal("workflow consumed demo spare")
	}
	if got := run("one", execution.CallerChat); got != first.ExecutionID {
		t.Fatalf("first chat used %s; want %s", got, first.ExecutionID)
	}
	next := nextIPCCommand(t, f, "clone_repository")
	if got := run("two", execution.CallerChat); got != next.ExecutionID || got == first.ExecutionID {
		t.Fatalf("second chat did not receive an independent replenished checkout: %s", got)
	}
}

// A workflow run's sessions share the run's checkout side by side (the Node's
// shared_checkout_sessions capability): the second session starts on the first one's checkout
// without ending it, each takes further turns, and ending one leaves the other live.
func TestDriverIPCWorkflowSessionsShareACheckout(t *testing.T) {
	var cloneCalls atomic.Int32
	h := newIPCHarness(t, func(_ *fakeNodeIPC, m node.Message) bool {
		if m.Type == "clone_repository" {
			cloneCalls.Add(1)
		}
		return false
	})
	ctx := ipcContext(t)
	run := func(session execution.Session, turn string) <-chan ipcRunOutcome {
		done := make(chan ipcRunOutcome, 1)
		go func() {
			result, err := h.driver.Run(ctx, h.scope, session, execution.Turn{
				Ref: execution.Ref{SessionID: session.ID, TurnID: turn}, Prompt: execution.TextPrompt("prompt"),
				Caller: execution.Caller{Kind: execution.CallerWorkflow, ID: "run", AttemptID: turn},
			}, discardIPCEvent)
			done <- ipcRunOutcome{result, err}
		}()
		return done
	}
	checkoutOf := func(command node.Message) string {
		var payload struct {
			Spec struct {
				Checkout string `json:"checkout_execution_id"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(command.Payload, &payload); err != nil || payload.Spec.Checkout == "" {
			t.Fatalf("start_agent_session payload %s: %v", command.Payload, err)
		}
		return payload.Spec.Checkout
	}
	parked, side := h.session("parked", "run-workspace"), h.session("side", "run-workspace")
	done := run(parked, "parked-1")
	f := receiveIPC(t, h.created)
	parkedStart := nextIPCCommand(t, f, "start_agent_session")
	f.finishTurn(t, parkedStart, "parked-1", "draft")
	if got := receiveIPC(t, done); got.err != nil || got.result.Answer != "draft" {
		t.Fatalf("first session: %+v", got)
	}

	done = run(side, "side-1")
	sideStart := nextIPCCommand(t, f, "start_agent_session")
	if checkoutOf(sideStart) != checkoutOf(parkedStart) || cloneCalls.Load() != 1 {
		t.Fatalf("the run's second session did not share the checkout: %s vs %s, clones = %d", checkoutOf(sideStart), checkoutOf(parkedStart), cloneCalls.Load())
	}
	f.finishTurn(t, sideStart, "side-1", "side work")
	if got := receiveIPC(t, done); got.err != nil || got.result.Answer != "side work" {
		t.Fatalf("second session beside a live one: %+v", got)
	}
	h.driver.mu.Lock()
	live := h.driver.sessions[parked.ID]
	h.driver.mu.Unlock()
	select {
	case <-live.stopped:
		t.Fatal("starting the run's second session stopped the first")
	default:
	}

	ended := make(chan error, 1)
	go func() { ended <- h.driver.End(ctx, side.ID) }()
	if end := nextIPCCommand(t, f, "end_session"); end.ExecutionID != sideStart.ExecutionID {
		t.Fatalf("ended execution %s, want the second session's %s", end.ExecutionID, sideStart.ExecutionID)
	} else {
		f.terminal(end)
	}
	if err := receiveIPC(t, ended); err != nil {
		t.Fatal(err)
	}
	// The first session takes its next turn in the same Agent conversation.
	done = run(parked, "parked-2")
	next := nextIPCCommand(t, f, "submit_user_turn")
	if next.ExecutionID != parkedStart.ExecutionID {
		t.Fatalf("follow-up went to execution %s, want %s", next.ExecutionID, parkedStart.ExecutionID)
	}
	f.finishTurn(t, parkedStart, "parked-2", "final")
	if got := receiveIPC(t, done); got.err != nil || got.result.Answer != "final" {
		t.Fatalf("follow-up after the sibling ended: %+v", got)
	}
	h.driver.mu.Lock()
	project := h.driver.projects["project"]
	h.driver.mu.Unlock()
	project.mu.Lock()
	owners := len(project.owners["run-workspace"])
	project.mu.Unlock()
	if owners != 1 {
		t.Fatalf("workspace owners = %d, want only the live session", owners)
	}
}

// Two sessions of one run start at the same time with different Skills. Both wait on the same
// clone and then project into the shared checkout; whichever projects second must keep the other's
// Skill, so both are on disk once both Agents have been started. The race is timing-dependent, so
// the case repeats; before the fix a round lost a Skill roughly one time in seven.
func TestDriverIPCConcurrentWorkflowStartsKeepEachOthersSkills(t *testing.T) {
	h := newIPCHarness(t, func(f *fakeNodeIPC, m node.Message) bool {
		if m.Type == "clone_repository" {
			_ = os.MkdirAll(filepath.Join(f.project.root, "w", "r", m.ExecutionID), 0700)
		}
		return false
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	run := func(session execution.Session, turn string) <-chan ipcRunOutcome {
		done := make(chan ipcRunOutcome, 1)
		go func() {
			result, err := h.driver.Run(ctx, h.scope, session, execution.Turn{
				Ref: execution.Ref{SessionID: session.ID, TurnID: turn}, Prompt: execution.TextPrompt("prompt"),
				Caller: execution.Caller{Kind: execution.CallerWorkflow, ID: "run", AttemptID: turn},
			}, discardIPCEvent)
			done <- ipcRunOutcome{result, err}
		}()
		return done
	}
	turnOf := func(command node.Message) string {
		var payload struct {
			Spec struct {
				Turn struct {
					ID string `json:"turn_id"`
				} `json:"initial_turn"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(command.Payload, &payload); err != nil || payload.Spec.Turn.ID == "" {
			t.Fatalf("start payload %s: %v", command.Payload, err)
		}
		return payload.Spec.Turn.ID
	}
	skill := func(name string) *execution.Skills {
		return &execution.Skills{Documents: []execution.SkillDocument{{Name: name, Description: name, Content: "CONTENT-" + name}}}
	}
	const rounds = 30
	lost := 0
	var f *fakeNodeIPC
	for i := range rounds {
		ws := fmt.Sprintf("run-workspace-%d", i)
		a, b := h.session(fmt.Sprintf("a-%d", i), ws), h.session(fmt.Sprintf("b-%d", i), ws)
		a.Skills, b.Skills = skill("alpha"), skill("beta")
		doneA, doneB := run(a, fmt.Sprintf("ta-%d", i)), run(b, fmt.Sprintf("tb-%d", i))
		if f == nil {
			f = receiveIPC(t, h.created)
		}
		first := nextIPCCommand(t, f, "start_agent_session")
		second := nextIPCCommand(t, f, "start_agent_session")
		h.driver.mu.Lock()
		project := h.driver.projects["project"]
		h.driver.mu.Unlock()
		project.mu.Lock()
		path := project.checkouts[ws].path
		project.mu.Unlock()
		for _, name := range []string{"alpha", "beta"} {
			if _, err := os.Stat(filepath.Join(path, ".opencode/skills", name, "SKILL.md")); err != nil {
				lost++
				t.Logf("round %d: skill %s missing while both sessions are live: %v", i, name, err)
			}
		}
		f.finishTurn(t, first, turnOf(first), "one")
		f.finishTurn(t, second, turnOf(second), "two")
		if got := receiveIPC(t, doneA); got.err != nil {
			t.Fatalf("round %d A: %+v", i, got)
		}
		if got := receiveIPC(t, doneB); got.err != nil {
			t.Fatalf("round %d B: %+v", i, got)
		}
	}
	if lost != 0 {
		t.Fatalf("%d of %d rounds lost a live session's Skill", lost, rounds)
	}
}

// Two sessions of one wave select different Skills under one name. Whichever projects first keeps
// its Skill; only the other is refused, never both: a sibling that is admitted but has not
// projected anything yet does not count against the session projecting.
func TestDriverIPCClashingSkillsOfOneWaveRefuseOnlyOneSession(t *testing.T) {
	h := newIPCHarness(t, func(f *fakeNodeIPC, m node.Message) bool {
		if m.Type == "clone_repository" {
			_ = os.MkdirAll(filepath.Join(f.project.root, "w", "r", m.ExecutionID), 0700)
		}
		return false
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	run := func(session execution.Session, turn string) <-chan ipcRunOutcome {
		done := make(chan ipcRunOutcome, 1)
		go func() {
			result, err := h.driver.Run(ctx, h.scope, session, execution.Turn{
				Ref: execution.Ref{SessionID: session.ID, TurnID: turn}, Prompt: execution.TextPrompt("prompt"),
				Caller: execution.Caller{Kind: execution.CallerWorkflow, ID: "run", AttemptID: turn},
			}, discardIPCEvent)
			done <- ipcRunOutcome{result, err}
		}()
		return done
	}
	review := func(content string) *execution.Skills {
		return &execution.Skills{Documents: []execution.SkillDocument{{Name: "review", Description: "review", Content: content}}}
	}
	var f *fakeNodeIPC
	for i := range 20 {
		ws := fmt.Sprintf("clash-workspace-%d", i)
		a, b := h.session(fmt.Sprintf("ca-%d", i), ws), h.session(fmt.Sprintf("cb-%d", i), ws)
		a.Skills, b.Skills = review("A-REVIEW"), review("B-REVIEW")
		doneA, doneB := run(a, fmt.Sprintf("cta-%d", i)), run(b, fmt.Sprintf("ctb-%d", i))
		if f == nil {
			f = receiveIPC(t, h.created)
		}
		var refused *ipcRunOutcome
		var pending <-chan ipcRunOutcome
		select {
		case got := <-doneA:
			refused, pending = &got, doneB
		case got := <-doneB:
			refused, pending = &got, doneA
		case <-time.After(30 * time.Second):
			t.Fatalf("round %d: neither session was refused", i)
		}
		if refused.result.FailureCode != "workflow_skill_name_conflict" {
			t.Fatalf("round %d: first outcome %+v, want workflow_skill_name_conflict", i, refused)
		}
		start := nextIPCCommand(t, f, "start_agent_session")
		var payload struct {
			Spec struct {
				Turn struct {
					ID string `json:"turn_id"`
				} `json:"initial_turn"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(start.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		f.finishTurn(t, start, payload.Spec.Turn.ID, "kept")
		if got := receiveIPC(t, pending); got.err != nil {
			t.Fatalf("round %d: the session that projected first failed too: %+v", i, got)
		}
	}
}

// Node refuses the session over a checkout's limit by ending only that execution, with a stable
// detail and the control connection open. The turn fails with that code as a lost Agent, and the
// run's other session on the checkout keeps working.
func TestDriverIPCCheckoutRefusalEndsOnlyTheRefusedSession(t *testing.T) {
	h := newIPCHarness(t, nil)
	ctx := ipcContext(t)
	run := func(session execution.Session, turn string) <-chan ipcRunOutcome {
		done := make(chan ipcRunOutcome, 1)
		go func() {
			result, err := h.driver.Run(ctx, h.scope, session, execution.Turn{
				Ref: execution.Ref{SessionID: session.ID, TurnID: turn}, Prompt: execution.TextPrompt("prompt"),
				Caller: execution.Caller{Kind: execution.CallerWorkflow, ID: "run", AttemptID: turn},
			}, discardIPCEvent)
			done <- ipcRunOutcome{result, err}
		}()
		return done
	}
	live, refused := h.session("live", "run-workspace"), h.session("refused", "run-workspace")
	done := run(live, "live-1")
	f := receiveIPC(t, h.created)
	liveStart := nextIPCCommand(t, f, "start_agent_session")
	f.finishTurn(t, liveStart, "live-1", "ready")
	if got := receiveIPC(t, done); got.err != nil {
		t.Fatalf("first session: %+v", got)
	}
	done = run(refused, "refused-1")
	f.terminalAs(nextIPCCommand(t, f, "start_agent_session"), "agent_failed", "checkout_session_limit")
	got := receiveIPC(t, done)
	if !errors.Is(got.err, execution.ErrSessionEnded) || !got.result.AgentLost || got.result.NotStarted || got.result.Status != execution.StatusFailed || got.result.FailureCode != "checkout_session_limit" {
		t.Fatalf("refused session: %+v", got)
	}
	done = run(live, "live-2")
	if next := nextIPCCommand(t, f, "submit_user_turn"); next.ExecutionID != liveStart.ExecutionID {
		t.Fatalf("follow-up went to execution %s, want %s", next.ExecutionID, liveStart.ExecutionID)
	}
	f.finishTurn(t, liveStart, "live-2", "still here")
	if got := receiveIPC(t, done); got.err != nil || got.result.Answer != "still here" {
		t.Fatalf("the live session after its sibling was refused: %+v", got)
	}
}

// A Node that never acknowledges end_session is still connected, so only Rebuild marks it.
func stuckEnd(t *testing.T, h *ipcHarness, ctx context.Context, sid string) *fakeNodeIPC {
	t.Helper()
	done := startIPCRun(h, ctx, h.session(sid, "workspace-"+sid), "turn", discardIPCEvent)
	old := receiveIPC(t, h.created)
	command := nextIPCCommand(t, old, "start_agent_session")
	old.finishTurn(t, command, "turn", "first answer")
	if got := receiveIPC(t, done); got.err != nil || got.result.Answer != "first answer" {
		t.Fatalf("first turn: %+v", got)
	}
	ending, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if err := h.driver.End(ending, sid); !errors.Is(err, execution.ErrStopUnconfirmed) {
		t.Fatalf("unacknowledged End: %v", err)
	}
	nextIPCCommand(t, old, "end_session")
	if old.stops.Load() != 0 {
		t.Fatal("a healthy but slow Node was stopped without Rebuild")
	}
	return old
}

func TestDriverIPCRebuildStopsOldNodeBeforeNextSession(t *testing.T) {
	h := newIPCHarness(t, nil)
	ctx := ipcContext(t)
	old := stuckEnd(t, h, ctx, "first")
	h.driver.Rebuild("first")

	old.stopOK.Store(false)
	got := receiveIPC(t, startIPCRun(h, ctx, h.session("next", "workspace-next"), "blocked", discardIPCEvent))
	if !errors.Is(got.err, execution.ErrUnavailable) {
		t.Fatalf("unconfirmed rebuild = %+v", got)
	}
	select {
	case <-h.created:
		t.Fatal("replacement Node started before the old process tree stopped")
	default:
	}
	if old.stops.Load() != 1 {
		t.Fatalf("next session did not ask the supervisor to stop the old Node: calls=%d", old.stops.Load())
	}

	old.stopOK.Store(true)
	done := startIPCRun(h, ctx, h.session("next", "workspace-next"), "retry", discardIPCEvent)
	f := receiveIPC(t, h.created)
	if f.id == old.id || old.stops.Load() != 2 {
		t.Fatalf("next session reused the marked Node: stops=%d", old.stops.Load())
	}
	command := nextIPCCommand(t, f, "start_agent_session")
	f.finishTurn(t, command, "retry", "on a new Node")
	if got := receiveIPC(t, done); got.err != nil || got.result.Answer != "on a new Node" {
		t.Fatalf("rebuilt Node did not run the next session: %+v", got)
	}
	if err := h.driver.End(ctx, "first"); err != nil {
		t.Fatalf("supervisor stop did not settle the stuck session: %v", err)
	}
}

func TestDriverIPCEndRetiresNodeMarkedForRebuild(t *testing.T) {
	h := newIPCHarness(t, nil)
	ctx := ipcContext(t)
	old := stuckEnd(t, h, ctx, "first")
	h.driver.Rebuild("first")
	// A workflow successor ends its predecessor before reusing the directory; that End
	// must obtain supervisor proof instead of waiting for an acknowledgement that never comes.
	if err := h.driver.End(ctx, "first"); err != nil {
		t.Fatalf("End on a marked Node: %v", err)
	}
	h.driver.mu.Lock()
	remaining := h.driver.projects["project"]
	h.driver.mu.Unlock()
	if remaining != nil || old.stops.Load() != 1 {
		t.Fatalf("marked Node was not retired: retained=%v stops=%d", remaining != nil, old.stops.Load())
	}
	h.driver.Rebuild("unknown") // An unknown or settled session is a no-op.
}

// A session that was prepared but never started has no physical Agent, so ending it needs no
// stop proof, even from a Node that is marked for rebuild and cannot be stopped right now.
func TestDriverIPCEndOfANeverStartedSessionNeedsNoStopProofFromAMarkedNode(t *testing.T) {
	h := newIPCHarness(t, nil)
	ctx := ipcContext(t)
	old := stuckEnd(t, h, ctx, "first")
	// A turn cancelled during preparation leaves its session in this state.
	idle := h.session("idle", "workspace-idle")
	if _, err := h.driver.session(ctx, h.scope, idle, h.config, execution.Turn{Caller: execution.Caller{Kind: execution.CallerChat}}, newPreparation(execution.Ref{SessionID: idle.ID, TurnID: "turn"}, discardIPCEvent)); err != nil {
		t.Fatal(err)
	}
	h.driver.Rebuild("first")
	old.stopOK.Store(false)
	defer old.stopOK.Store(true)
	if err := h.driver.End(ctx, idle.ID); err != nil {
		t.Fatalf("End of a never-started session on a marked Node: %v", err)
	}
	if old.stops.Load() != 0 {
		t.Fatalf("End of a never-started session asked the supervisor to stop the Node: calls=%d", old.stops.Load())
	}
}

// sentIPC drains the commands the fake Node has received so far and returns their types.
func sentIPC(f *fakeNodeIPC) []string {
	var kinds []string
	for {
		select {
		case m := <-f.sent:
			kinds = append(kinds, m.Type)
		default:
			return kinds
		}
	}
}

// A first turn refused before anything reached Node (here: the model permit) never started its
// Agent. The turn is marked NotStarted, so a workflow fails the node as a session that could not
// be set up instead of parking it as a lost conversation, and ending the session needs nothing
// from Node.
func TestDriverIPCFirstTurnRefusedBeforeNodeIsNotStarted(t *testing.T) {
	h := newIPCHarness(t, nil)
	ctx := ipcContext(t)
	session := h.session("session", "workspace")
	a, err := h.driver.session(ctx, h.scope, session, h.config, execution.Turn{Caller: execution.Caller{Kind: execution.CallerWorkflow}}, newPreparation(execution.Ref{SessionID: session.ID, TurnID: "first"}, discardIPCEvent))
	if err != nil {
		t.Fatal(err)
	}
	f := receiveIPC(t, h.created)
	// The execution already holds a permit for another model, so the session's own is refused.
	if err := a.project.services.Permit(a.execution, "messages-fixture"); err != nil {
		t.Fatal(err)
	}
	sentIPC(f)
	got := receiveIPC(t, startIPCRun(h, ctx, session, "first", discardIPCEvent))
	if got.err == nil || got.result.Status != execution.StatusFailed || got.result.FailureCode != "agent_dispatch_failed" || !got.result.NotStarted {
		t.Fatalf("turn refused before Node = %+v, want a failed turn marked NotStarted", got)
	}
	if err := h.driver.End(ctx, session.ID); err != nil {
		t.Fatalf("End of the session that never started: %v", err)
	}
	if kinds := sentIPC(f); len(kinds) != 0 {
		t.Fatalf("Node received %v for a session that never started", kinds)
	}
	// Ending released the execution's permit and its hold on the workspace.
	if err := a.project.services.Permit(a.execution, "fixture"); err != nil {
		t.Fatalf("the refused session kept its permit: %v", err)
	}
	a.project.services.End(a.execution)
	a.project.mu.Lock()
	owners := len(a.project.owners[session.WorkspaceID])
	a.project.mu.Unlock()
	if owners != 0 {
		t.Fatalf("the refused session still owns its workspace: %d", owners)
	}
}

// A Node whose transport fails while the first turn is still being permitted never receives the
// start, so the session does not count as started and its turn is reported NotStarted.
func TestDriverIPCFirstTurnLostBeforeItsStartIsNotStarted(t *testing.T) {
	h := newIPCHarness(t, nil)
	ctx := ipcContext(t)
	session := h.session("session", "workspace")
	a, err := h.driver.session(ctx, h.scope, session, h.config, execution.Turn{Caller: execution.Caller{Kind: execution.CallerWorkflow}}, newPreparation(execution.Ref{SessionID: session.ID, TurnID: "first"}, discardIPCEvent))
	if err != nil {
		t.Fatal(err)
	}
	f := receiveIPC(t, h.created)
	sentIPC(f)
	a.commandMu.Lock()
	if _, fresh, err := a.begin("first", session.MCP, discardIPCEvent); err != nil || !fresh {
		t.Fatalf("begin: fresh=%v err=%v", fresh, err)
	}
	_ = f.Close()
	select {
	case <-a.project.dead:
	case <-time.After(5 * time.Second):
		t.Fatal("the closed transport never failed the project")
	}
	err = a.submit(execution.Turn{Ref: execution.Ref{TurnID: "first"}}, "prompt", true)
	a.commandMu.Unlock()
	if !errors.Is(err, execution.ErrUnavailable) {
		t.Fatalf("submit on a dead Node = %v, want ErrUnavailable", err)
	}
	if h.driver.agentStarted(session.ID) {
		t.Fatal("the session counts as started although its start was never sent")
	}
	if kinds := sentIPC(f); len(kinds) != 0 {
		t.Fatalf("Node received %v after its transport failed", kinds)
	}
}

// An End that arrives while the first turn is still being permitted finds no Agent to stop, so
// the turn must not start one afterwards: nothing would ever end it.
func TestDriverIPCFirstTurnDoesNotStartAnAgentAfterEnd(t *testing.T) {
	h := newIPCHarness(t, nil)
	ctx := ipcContext(t)
	session := h.session("session", "workspace")
	a, err := h.driver.session(ctx, h.scope, session, h.config, execution.Turn{Caller: execution.Caller{Kind: execution.CallerWorkflow}}, newPreparation(execution.Ref{SessionID: session.ID, TurnID: "first"}, discardIPCEvent))
	if err != nil {
		t.Fatal(err)
	}
	f := receiveIPC(t, h.created)
	sentIPC(f)
	a.commandMu.Lock()
	if _, fresh, err := a.begin("first", session.MCP, discardIPCEvent); err != nil || !fresh {
		t.Fatalf("begin: fresh=%v err=%v", fresh, err)
	}
	// End runs between begin and submit: the session has not started, so it only waits for
	// commandMu to release the session.
	ended := make(chan error, 1)
	go func() { ended <- h.driver.End(ctx, session.ID) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		a.mu.Lock()
		ending := a.ending
		a.mu.Unlock()
		if ending {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("End never reached the session")
		}
		time.Sleep(time.Millisecond)
	}
	err = a.submit(execution.Turn{Ref: execution.Ref{TurnID: "first"}}, "prompt", true)
	a.commandMu.Unlock()
	if !errors.Is(err, execution.ErrSessionEnded) {
		t.Fatalf("submit after End = %v, want ErrSessionEnded", err)
	}
	if err := receiveIPC(t, ended); err != nil {
		t.Fatalf("End: %v", err)
	}
	if kinds := sentIPC(f); len(kinds) != 0 {
		t.Fatalf("Node received %v after the session was ended", kinds)
	}
	// End released the permit the turn had taken.
	if err := a.project.services.Permit(a.execution, "messages-fixture"); err != nil {
		t.Fatalf("the ended session kept its permit: %v", err)
	}
	a.project.services.End(a.execution)
}
