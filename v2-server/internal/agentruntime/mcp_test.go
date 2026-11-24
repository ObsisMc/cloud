//go:build linux

package agentruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/node"
)

func ipcMCP() *execution.MCPSelection {
	archive := []byte("MCP package fixture")
	sum := sha256.Sum256(archive)
	return &execution.MCPSelection{
		Bindings: []execution.MCPBinding{{PluginID: "official/acme.tools", PluginVersion: "1.0.0", Settings: map[string]any{"token": "MCP_SECRET_MARKER"}}},
		Packages: []execution.MCPPackage{{PluginID: "official/acme.tools", PluginVersion: "1.0.0", Digest: hex.EncodeToString(sum[:]), Archive: archive}},
	}
}

func TestMCPSpecSerializesOnlyTheSelectedBindings(t *testing.T) {
	// Package settings are restricted to the explicit startup bindings.
	selection := ipcMCP()
	data, err := json.Marshal(mcpSpec(selection))
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"plugin_id":"official/acme.tools","plugin_version":"1.0.0","settings":{"token":"MCP_SECRET_MARKER"}}]`
	if string(data) != want {
		t.Fatalf("unexpected MCP frame: %d bytes", len(data))
	}
	empty, err := json.Marshal(mcpSpec(&execution.MCPSelection{}))
	if err != nil || string(empty) != "[]" {
		t.Fatal("empty selection inherited previous bindings")
	}
}

func TestLostMCPSessionCannotRestartWithHistoryHandoff(t *testing.T) {
	h := newIPCHarness(t, nil)
	session := h.session("lost", "workspace")
	session.MCP = ipcMCP()
	session.Recovery = &execution.Recovery{Handoff: "previous history"}
	generation := int64(0)
	turn := execution.Turn{Ref: execution.Ref{SessionID: session.ID, TurnID: "after-loss"}, Prompt: execution.TextPrompt("continue"), MCPGeneration: &generation}
	result, err := h.driver.Run(ipcContext(t), h.scope, session, turn, discardIPCEvent)
	if !errors.Is(err, execution.ErrNodeLost) || !result.NotStarted || result.FailureCode != execution.FailureNodeLost {
		t.Fatal("a new provider execution replaced the lost MCP session", result.Status, result.FailureCode, err)
	}
	select {
	case <-h.created:
		t.Fatal("a replacement Node was created")
	default:
	}
}

func TestMCPStartReadsChangesCommittedAfterAdmission(t *testing.T) {
	for _, code := range []string{"", "mcp_plugin_missing", "mcp_setting_missing"} {
		t.Run("latest/"+code, func(t *testing.T) {
			h := newIPCHarness(t, nil)
			ctx := ipcContext(t)
			session := h.session("cold-start", "workspace")
			session.MCP = ipcMCP()
			id := session.MCP.Bindings[0].PluginID
			h.driver.opts.ReadMCPTarget = func(_ context.Context, _ execution.Scope, _ string, _ *execution.MCPSelection, known int64) (*execution.MCPTarget, error) {
				if known >= 1 {
					return nil, nil
				}
				if code != "" {
					return &execution.MCPTarget{Generation: 1, Unavailable: map[string]string{id: code}}, nil
				}
				latest := ipcMCP()
				latest.Bindings[0].Settings["token"] = "changed-before-start"
				return &execution.MCPTarget{Generation: 1, Configured: *latest}, nil
			}
			done := make(chan ipcRunOutcome, 1)
			go func() {
				turn := execution.Turn{Ref: execution.Ref{SessionID: session.ID, TurnID: "first"}, Prompt: execution.TextPrompt("first"), MCPGeneration: new(int64)}
				result, err := h.driver.Run(ctx, h.scope, session, turn, discardIPCEvent)
				done <- ipcRunOutcome{result, err}
			}()
			f := receiveIPC(t, h.created)
			if code != "" {
				outcome := receiveIPC(t, done)
				if outcome.result.MCPFailure == nil || outcome.result.MCPFailure.Code != code || outcome.result.MCPFailure.PluginID != id || !outcome.result.NotStarted {
					t.Fatal("latest unavailable member lost its startup diagnostic")
				}
				return
			}
			start := nextIPCCommand(t, f, "start_agent_session")
			if !bytes.Contains(start.Payload, []byte("changed-before-start")) || bytes.Contains(start.Payload, []byte("MCP_SECRET_MARKER")) {
				t.Fatal("startup did not use the latest authorized settings")
			}
			member := map[string]any{"state": "configured", "plugin_id": id, "plugin_version": "1.0.0", "configuration_revision": 0}
			for generation, kind := range []string{"get_session_mcp_state", "update_session_mcp"} {
				select {
				case message := <-f.sent:
					var request struct {
						ID         string `json:"request_id"`
						Generation int
					}
					if message.Type != kind || json.Unmarshal(message.Payload, &request) != nil || request.Generation != generation {
						t.Fatalf("after startup got %s generation %d, want %s generation %d", message.Type, request.Generation, kind, generation)
					}
					f.reply(message, "session_mcp_reply", map[string]any{"request_id": request.ID, "node": f.id, "result": "accepted", "state": map[string]any{"desired": map[string]any{"generation": generation, "members": []any{member}}}})
				case <-time.After(time.Second):
					t.Fatalf("startup did not send %s before periodic reconciliation", kind)
				}
			}
			f.finishTurn(t, start, "first", "done")
			if outcome := receiveIPC(t, done); outcome.err != nil {
				t.Fatal(outcome.err)
			}
		})
	}
}

func TestMCPSyncWaitHonorsCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		session := &agentSession{mcpSync: make(chan struct{}, 1), project: &projectRuntime{driver: &Driver{}}}
		session.project.driver.opts.ReadMCPTarget = func(context.Context, execution.Scope, string, *execution.MCPSelection, int64) (*execution.MCPTarget, error) {
			return nil, nil
		}
		session.mcpSync <- struct{}{}
		defer func() { <-session.mcpSync }()
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- session.syncMCP(ctx, nil, 0) }()
		synctest.Wait()
		cancel()
		synctest.Wait()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("waiting for MCP sync: %v", err)
			}
		default:
			t.Fatal("cancelled sync still waits for the other reconciliation")
		}
	})
}

func TestRefusedMCPTimeoutRetainsSession(t *testing.T) {
	var refuse atomic.Bool
	primed := make(chan struct{}, 1)
	deadlines := make(chan time.Time, 2)
	var reconciles atomic.Int32
	failedRead := make(chan struct{}, 1)
	h := newIPCHarness(t, func(f *fakeNodeIPC, m node.Message) bool {
		if m.Type == "submit_user_turn" && refuse.Load() {
			var command struct {
				ID string `json:"command_id"`
			}
			if err := json.Unmarshal(m.Payload, &command); err != nil {
				return false
			}
			f.reply(m, "session_command_rejected", map[string]string{"command_id": command.ID, "reason": "mcp_update_required"})
			return true
		}
		return false
	})
	h.driver.mcpRefreshWait = 50 * time.Millisecond
	h.driver.opts.ReadMCPTarget = func(ctx context.Context, _ execution.Scope, _ string, _ *execution.MCPSelection, known int64) (*execution.MCPTarget, error) {
		if known == -1 {
			return &execution.MCPTarget{Configured: *ipcMCP()}, nil
		}
		if refuse.Load() {
			if attempt := reconciles.Add(1); attempt <= 2 {
				deadline, _ := ctx.Deadline()
				deadlines <- deadline
				if attempt == 1 {
					return nil, nil
				}
				<-ctx.Done()
				failedRead <- struct{}{}
				return nil, nil
			}
			select {
			case failedRead <- struct{}{}:
			default:
			}
			return nil, execution.ErrUnavailable
		}
		select {
		case primed <- struct{}{}:
		default:
		}
		return nil, nil
	}
	ctx := ipcContext(t)
	session := h.session("refused-session", "workspace")
	session.MCP = ipcMCP()
	// ended receives every published turn end: that is what the execution service stores and the
	// workflow host reads, not Run's return value.
	ended := make(chan execution.Result, 4)
	emit := func(_ context.Context, event execution.Event) error {
		if event.Kind == execution.EventTurnEnded && event.Result != nil {
			ended <- *event.Result
		}
		return nil
	}
	run := func(ctx context.Context, id string) <-chan ipcRunOutcome {
		done := make(chan ipcRunOutcome, 1)
		go func() {
			turn := execution.Turn{Ref: execution.Ref{SessionID: session.ID, TurnID: id}, Prompt: execution.TextPrompt(id), MCPGeneration: new(int64)}
			result, err := h.driver.Run(ctx, h.scope, session, turn, emit)
			done <- ipcRunOutcome{result, err}
		}()
		return done
	}
	first := run(ctx, "first")
	f := receiveIPC(t, h.created)
	start := nextIPCCommand(t, f, "start_agent_session")
	f.finishTurn(t, start, "first", "ready")
	if got := receiveIPC(t, first); got.err != nil {
		t.Fatal(got.err)
	}
	receiveIPC(t, ended)
	receiveIPC(t, primed)
	refuse.Store(true)
	result := receiveIPC(t, run(ctx, "refused"))
	if result.err != nil || result.result.FailureCode != "mcp_refresh_wait_timeout" || result.result.AgentLost {
		t.Fatal("refused turn did not time out on its own", result.err, result.result.FailureCode)
	}
	// Node never queued the refused command, so the Agent never got the prompt.
	if !result.result.NotDelivered {
		t.Fatalf("refused turn = %+v, want it reported not delivered", result.result)
	}
	firstDeadline, secondDeadline := receiveIPC(t, deadlines), receiveIPC(t, deadlines)
	if got := reconciles.Load(); got != 2 {
		t.Fatalf("late successful reconciliation resubmitted the turn: %d reconciliations", got)
	}
	if firstDeadline.IsZero() || !firstDeadline.Equal(secondDeadline) {
		t.Fatalf("second refusal moved the deadline by %v", secondDeadline.Sub(firstDeadline))
	}
	if stored := receiveIPC(t, ended); stored.FailureCode != "mcp_refresh_wait_timeout" || !stored.NotDelivered {
		t.Fatalf("published end of the refused turn = %+v, want it stored as not delivered", stored)
	}
	// The refused submission is visible on the wire, but never accepted by Node.
	nextIPCCommand(t, f, "submit_user_turn")
	nextIPCCommand(t, f, "submit_user_turn")
	refuse.Store(false)
	last := run(ctx, "next")
	command := nextIPCCommand(t, f, "submit_user_turn")
	if command.ExecutionID != start.ExecutionID {
		t.Fatal("timeout replaced the Agent")
	}
	f.finishTurn(t, command, "next", "healthy")
	if got := receiveIPC(t, last); got.err != nil || got.result.Status != execution.StatusCompleted {
		t.Fatal("same session could not continue after timeout", got.err)
	}
	// An explicit cancellation while waiting must use normal confirmed End,
	// not publish a synthetic configuration failure.
	receiveIPC(t, failedRead)
	h.driver.mcpRefreshWait = time.Minute
	refuse.Store(true)
	interrupted, cancel := context.WithCancel(ctx)
	defer cancel()
	waiting := run(interrupted, "cancel-me")
	nextIPCCommand(t, f, "submit_user_turn")
	receiveIPC(t, failedRead)
	cancel()
	end := nextIPCCommand(t, f, "end_session")
	f.terminal(end)
	got := receiveIPC(t, waiting)
	if got.result.Status != execution.StatusCancelled || got.result.FailureCode != "" {
		t.Fatal("explicit stop became a refresh failure", got.result.Status, got.result.FailureCode)
	}
}

func TestMCPRefusedTurnReplaysOriginalCommandAfterLatestTarget(t *testing.T) {
	var desired atomic.Int64
	var current atomic.Int64
	var submits atomic.Int32
	var update node.Message
	commands := make(chan node.Message, 2)
	h := newIPCHarness(t, func(f *fakeNodeIPC, m node.Message) bool {
		switch m.Type {
		case "get_session_mcp_state", "update_session_mcp":
			var request struct {
				Request    string `json:"request_id"`
				Generation int64
			}
			if json.Unmarshal(m.Payload, &request) != nil {
				return false
			}
			if m.Type == "update_session_mcp" {
				update = m
				desired.Store(request.Generation)
			}
			members := []any{map[string]any{"state": "configured", "plugin_id": "official/acme.tools", "plugin_version": "1.0.0", "configuration_revision": desired.Load()}}
			f.reply(m, "session_mcp_reply", map[string]any{"request_id": request.Request, "node": f.id, "result": "accepted", "state": map[string]any{"desired": map[string]any{"generation": desired.Load(), "members": members}}})
			return true
		case "submit_user_turn":
			commands <- m
			if submits.Add(1) == 1 {
				current.Store(3)
				f.reply(m, "session_command_rejected", map[string]string{"command_id": "turn-" + digest("append"), "reason": "mcp_update_required"})
			}
			return true
		}
		return false
	})
	h.driver.opts.ReadMCPTarget = func(_ context.Context, _ execution.Scope, _ string, selected *execution.MCPSelection, known int64) (*execution.MCPTarget, error) {
		g := current.Load()
		if known == g {
			return nil, nil
		}
		s := ipcMCP()
		if !selected.SameMembers(s) {
			return nil, &execution.MCPFailure{Code: "mcp_selection_invalid"}
		}
		s.Bindings[0].Revision = g
		return &execution.MCPTarget{Generation: g, Configured: *s}, nil
	}
	ctx := ipcContext(t)
	session := h.session("lifecycle", "workspace")
	session.MCP = ipcMCP()
	run := func(id string, floor int64) <-chan ipcRunOutcome {
		done := make(chan ipcRunOutcome, 1)
		go func() {
			r, e := h.driver.Run(ctx, h.scope, session, execution.Turn{Ref: execution.Ref{SessionID: session.ID, TurnID: id}, Prompt: execution.TextPrompt(id), MCPGeneration: &floor}, discardIPCEvent)
			done <- ipcRunOutcome{r, e}
		}()
		return done
	}
	first := run("first", 0)
	f := receiveIPC(t, h.created)
	start := nextIPCCommand(t, f, "start_agent_session")
	f.finishTurn(t, start, "first", "ready")
	if r := receiveIPC(t, first); r.err != nil {
		t.Fatal(r.err)
	}
	next := run("append", 2)
	original := receiveIPC(t, commands)
	replayed := receiveIPC(t, commands)
	if !bytes.Equal(original.Payload, replayed.Payload) || original.ExecutionID != replayed.ExecutionID || original.OperationID != replayed.OperationID || !bytes.Contains(replayed.Payload, []byte(`"mcp_generation":2`)) {
		t.Fatal("replayed command changed its coordinates, input or generation floor")
	}
	if desired.Load() != 3 || !bytes.Contains(update.Payload, []byte(`"configuration_revision":3`)) || !bytes.Contains(update.Payload, []byte("MCP_SECRET_MARKER")) {
		t.Fatal("turn replayed before the complete committed update reached the Node")
	}
	f.finishTurn(t, replayed, "append", "done")
	if r := receiveIPC(t, next); r.err != nil || r.result.Status != execution.StatusCompleted {
		t.Fatal("append did not complete", r.err)
	}
}

func TestMCPPackageInstallationKeepsSettingsOutOfControlFrames(t *testing.T) {
	h := newIPCHarness(t, nil)
	ctx := ipcContext(t)
	session := h.session("plain-session", "workspace")
	done := startIPCRun(h, ctx, session, "first", discardIPCEvent)
	f := receiveIPC(t, h.created)
	start := nextIPCCommand(t, f, "start_agent_session")
	f.finishTurn(t, start, "first", "done")
	if result := receiveIPC(t, done); result.err != nil {
		t.Fatal(result.err)
	}
	selection := ipcMCP()
	if err := f.project.installMCP(ctx, selection.Packages[0]); err != nil {
		t.Fatal(err)
	}
	install := nextIPCCommand(t, f, "install_plugins")
	if !bytes.Contains(install.Payload, []byte("official/acme.tools")) || !bytes.Contains(install.Payload, []byte("1.0.0")) {
		t.Fatal("package identity did not reach install_plugins")
	}
	if bytes.Contains(install.Payload, []byte("MCP_SECRET_MARKER")) || bytes.Contains(install.Payload, []byte("settings")) {
		t.Fatal("package installation received session settings")
	}
	invalid := selection.Packages[0]
	invalid.Digest = "incorrect"
	if err := f.project.installMCP(ctx, invalid); err == nil {
		t.Fatal("invalid package digest accepted")
	}
}

// Exercise dispatch after preparation. Normal admission stays gated until updates and
// observations are also wired; this fixture supplies the already-authorized selection.
func TestMCPDispatchRequiresCommittedGeneration(t *testing.T) {
	h := newIPCHarness(t, nil)
	session := h.session("dispatch", "workspace")
	a, err := h.driver.session(ipcContext(t), h.scope, session, h.config, execution.Turn{Caller: execution.Caller{Kind: execution.CallerWorkflow}}, newPreparation(execution.Ref{}, discardIPCEvent))
	if err != nil {
		t.Fatal(err)
	}
	f := receiveIPC(t, h.created)
	sentIPC(f)
	a.commandMu.Lock()
	defer a.commandMu.Unlock()
	a.frozen.MCP = ipcMCP()
	turn := execution.Turn{Ref: execution.Ref{SessionID: session.ID, TurnID: "first"}}
	for _, gen := range []*int64{nil, new(int64(-1)), new(int64(1)), new(execution.MaxMCPCounter + 1)} {
		turn.MCPGeneration = gen
		if err := a.submit(turn, "first", true); !errors.Is(err, execution.ErrInvalidRequest) {
			t.Fatalf("invalid start generation accepted: %v", err)
		}
	}
	if len(sentIPC(f)) != 0 {
		t.Fatal("invalid generation reached the Node")
	}
	turn.MCPGeneration = new(int64)
	if err := a.submit(turn, "first", true); err != nil {
		t.Fatal(err)
	}
	start := nextIPCCommand(t, f, "start_agent_session")
	var payload struct {
		Spec struct {
			Lifecycle bool `json:"mcp_lifecycle"`
			MCP       []struct {
				ID string `json:"plugin_id"`
			} `json:"mcp"`
		}
	}
	if json.Unmarshal(start.Payload, &payload) != nil || !payload.Spec.Lifecycle || len(payload.Spec.MCP) != 1 || payload.Spec.MCP[0].ID != "official/acme.tools" {
		t.Fatal("start omitted the lifecycle flag or explicit selection")
	}
	turn.Ref.TurnID = "next"
	turn.MCPGeneration = nil
	if err := a.submit(turn, "next", false); !errors.Is(err, execution.ErrInvalidRequest) || len(sentIPC(f)) != 0 {
		t.Fatal("follow-up without an admission generation was sent")
	}
	turn.MCPGeneration = new(int64(23))
	if err := a.submit(turn, "next", false); err != nil {
		t.Fatal(err)
	}
	followup := nextIPCCommand(t, f, "submit_user_turn")
	var cmd struct {
		Generation *int64 `json:"mcp_generation"`
	}
	if json.Unmarshal(followup.Payload, &cmd) != nil || cmd.Generation == nil || *cmd.Generation != 23 || bytes.Contains(followup.Payload, []byte("MCP_SECRET_MARKER")) {
		t.Fatal("follow-up must send exactly the admitted generation and no settings")
	}
}

func TestMCPRejectsBeforeStartUntilBothPeersSupportLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name, missingCapability, code string
	}{
		{"missing agent session", "agent_session", "mcp_node_capability_missing"},
		{"missing session MCP", "session_mcp", "mcp_node_capability_missing"},
		{"frozen setup only", "session_mcp_lifecycle", "mcp_node_capability_missing"},
		{"lifecycle Node without Controller integration", "", "mcp_lifecycle_not_ready"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newIPCHarness(t, func(f *fakeNodeIPC, _ node.Message) bool {
				f.missingCapability.Store(&tc.missingCapability)
				return false
			})
			s := h.session("mcp", "workspace")
			s.MCP = ipcMCP()
			done := startIPCRun(h, ipcContext(t), s, "first", discardIPCEvent)
			f := receiveIPC(t, h.created)
			result := receiveIPC(t, done)
			if result.result.FailureCode != tc.code || result.result.MCPFailure == nil {
				t.Fatalf("wrong failure: %+v", result.result)
			}
			for len(f.sent) > 0 {
				m := <-f.sent
				if m.Type == "start_agent_session" || m.Type == "clone_repository" {
					t.Fatal("started preparing a session with unsupported MCP")
				}
				if m.Type == "install_plugins" {
					var payload struct {
						Spec struct{ Plugins []json.RawMessage }
					}
					if err := json.Unmarshal(m.Payload, &payload); err != nil || len(payload.Spec.Plugins) != 0 {
						t.Fatal("installed packages before the MCP capability gate")
					}
				}
				if bytes.Contains(m.Payload, []byte("official/acme.tools")) || bytes.Contains(m.Payload, []byte("MCP_SECRET_MARKER")) {
					t.Fatal("rejected MCP selection reached the Node")
				}
			}
		})
	}
}

func TestEmptyMCPDoesNotRequireLifecycleCapability(t *testing.T) {
	for _, present := range []bool{false, true} {
		name := "omitted"
		if present {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			h := newIPCHarness(t, func(f *fakeNodeIPC, _ node.Message) bool {
				missing := "session_mcp_lifecycle"
				f.missingCapability.Store(&missing)
				return false
			})
			s := h.session("empty-mcp", "workspace")
			if present {
				s.MCP = &execution.MCPSelection{}
			}
			done := startIPCRun(h, ipcContext(t), s, "first", discardIPCEvent)
			f := receiveIPC(t, h.created)
			start := nextIPCCommand(t, f, "start_agent_session")
			var payload struct{ Spec map[string]json.RawMessage }
			if err := json.Unmarshal(start.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"mcp", "mcp_lifecycle"} {
				if _, exists := payload.Spec[field]; exists {
					t.Fatalf("empty selection sent %s to a legacy Node", field)
				}
			}
			f.finishTurn(t, start, "first", "done")
			if result := receiveIPC(t, done); result.err != nil || result.result.Status != execution.StatusCompleted {
				t.Fatalf("empty selection failed: %+v", result)
			}
		})
	}
}

func TestLostMCPSelectionStopsExistingExecutionBeforeAppend(t *testing.T) {
	h := newIPCHarness(t, nil)
	ctx := ipcContext(t)
	session := h.session("retained", "workspace")
	first := startIPCRun(h, ctx, session, "first", discardIPCEvent)
	f := receiveIPC(t, h.created)
	start := nextIPCCommand(t, f, "start_agent_session")
	f.finishTurn(t, start, "first", "done")
	if outcome := receiveIPC(t, first); outcome.err != nil {
		t.Fatal(outcome.err)
	}
	// The fixture's live Agent represents a lifecycle execution; the business session
	// presented on reattachment has lost its selection. No blank update may replace it.
	h.driver.mu.Lock()
	a := h.driver.sessions[session.ID]
	h.driver.mu.Unlock()
	a.commandMu.Lock()
	a.frozen.MCP = ipcMCP()
	a.commandMu.Unlock()
	appendDone := startIPCRun(h, ctx, session, "second", discardIPCEvent)
	outcome := receiveIPC(t, appendDone)
	if outcome.result.MCPFailure == nil || outcome.result.MCPFailure.Code != "mcp_selection_invalid" || !outcome.result.AgentLost {
		t.Fatal("session with missing authorization remained usable")
	}
	end := receiveIPC(t, f.sent)
	if end.Type != "end_session" || end.ExecutionID != start.ExecutionID {
		t.Fatalf("selection loss sent %s to %s", end.Type, end.ExecutionID)
	}
	select {
	case <-a.stopped:
		t.Fatal("stopped before physical confirmation")
	default:
	}
	f.terminal(end)
	if err := h.driver.End(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
}

func TestMCPTerminalPreservesSafeFailureDetails(t *testing.T) {
	for _, code := range []string{"mcp_plugin_missing", "mcp_setting_missing", "mcp_http_capability_missing", "mcp_load_session_missing"} {
		t.Run(code, func(t *testing.T) {
			h := newIPCHarness(t, nil)
			s := h.session("session", "workspace")
			done := startIPCRun(h, ipcContext(t), s, "first", discardIPCEvent)
			f := receiveIPC(t, h.created)
			start := nextIPCCommand(t, f, "start_agent_session")
			sequence := uint64(1)
			payload, _ := json.Marshal(map[string]any{"kind": "agent_session_ended", "result": map[string]any{"reason": "agent_failed", "node": f.id, "detail": code, "plugin_id": "official/acme.tools", "setting_id": "token", "transport": "http"}})
			f.in <- node.Message{Type: "agent_session_ended", OperationID: start.OperationID, ExecutionID: start.ExecutionID, Sequence: &sequence, Payload: payload}
			result := receiveIPC(t, done)
			if result.result.FailureCode != code || result.result.MCPFailure == nil || result.result.MCPFailure.PluginID != "official/acme.tools" || result.result.MCPFailure.SettingID != "token" || result.result.MCPFailure.Transport != "http" {
				t.Fatalf("failure details lost: %+v", result.result)
			}
		})
	}
}
