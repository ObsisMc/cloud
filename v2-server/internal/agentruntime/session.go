//go:build linux

package agentruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/node"
	"github.com/wanglongan587/cloud/v2-server/internal/nodehistory"
)

var errEventStore = errors.New("agent_event_write_failed")

// nodeCheckoutUnavailable is the detail Node ends a session with when it does not know the
// session's checkout_execution_id (apps/ora-node/src/session/driver.rs).
//
// It is ambiguous: Node also reports it when it cannot read its ledger. Desktop's ledger.rs reads
// the checkout as cloned_checkout(...).ok().flatten(), and branch_heads.rs returns None for a
// SQLite, JSON or lock error and for unknown progress as for a missing row, so a transient read
// error after a restart looks like a checkout Node never had. Mapping it only for a reattached
// checkout is as precise as v2 can be; such a read error still fails the parked node, drops the
// record and lets the run's later nodes clone again away from the directory's uncommitted edits.
// A distinct code for a read error belongs to Node (circus#1230).
const nodeCheckoutUnavailable = "checkout_unavailable"

// previewInterval bounds how often Node previews become retained history_preview events. Node
// sends up to ten a second; every retained event is stored and replayed when a page loads.
var previewInterval = 250 * time.Millisecond

type turnOutcome struct {
	turnID string
	result execution.Result
	err    error
}

type activeTurn struct {
	id             string
	commandID      string
	done           chan turnOutcome
	updateRequired chan struct{}
	// guard limits the Agent's re-sends of model calls cut off at a limit during this turn.
	guard retryGuard
	// cancel is the command ID of the CancelTurn sent because the caller stopped this turn; ""
	// while nobody did. It is how a cancelled TurnEnded is known to be the user's stop rather
	// than a failure (see settleCancel). Protected by agentSession.mu.
	cancel string
	// stopped holds the turn's TurnEnded while its cancel awaits TurnCancelSettled, which decides
	// whether the session stays. Protected by agentSession.mu.
	stopped *nodehistory.Turn
	// cancelFallback tells Run that Node will not settle the cancel the way a kept session needs
	// (it refused the command, did not know the turn, or contradicted it): Run ends the session
	// as a stop did before turn_cancel.
	cancelFallback chan struct{}
	// stopRequested is closed by StopTurn (requestStop) when the caller asks to stop only this turn.
	stopRequested chan struct{}
	stopOnce      sync.Once
	// progress is signalled for every message of this session while a cancel awaits its
	// settlement, so Run's wait for it restarts (cancel-turn ADR D4: wait on delivery progress).
	progress chan struct{}
	// fallingBack is set once Run ends the session instead of waiting longer: a settlement that
	// arrives after that cannot keep the session. Protected by agentSession.mu.
	fallingBack bool
	// refused marks a submission Node refused (mcp_update_required) and has not been sent again:
	// Node holds no such turn, so a stop ends it here instead of sending CancelTurn. Protected by
	// agentSession.mu; commandMu orders it with the resubmission.
	refused bool
	// instruction is the turn's host instruction once it was issued. Protected by agentSession.mu.
	instruction *execution.HostInstruction
}

// requestStop asks Run to stop only this turn; repeated requests coalesce.
func (call *activeTurn) requestStop() {
	call.stopOnce.Do(func() { close(call.stopRequested) })
}

// signal wakes a buffered notification channel without blocking; repeated signals coalesce.
func signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// fallBack hands the stop back to Run's session-ending path; repeated signals coalesce.
func (call *activeTurn) fallBack() { signal(call.cancelFallback) }

// keepChoices are what a session chose in its StartAgentSession to survive (cancel-turn ADR D5).
// They are frozen with the session, so a later connection cannot change its rules.
type keepChoices struct {
	// onCancel keeps the session after a turn stopped through CancelTurn. Chosen whenever the
	// connection negotiated turn_cancel.
	onCancel bool
	// onFailure would keep it after a failed turn (ACP prompt error, watchdog). Never chosen yet:
	// a kept failure carries no reason the pages could show (ora-space/circus#1138). Enabling it
	// means choosing it in chooseKeep and settling a failed turn that Node keeps as SessionKept.
	onFailure bool
}

// chooseKeep decides a new session's keep choices from what its connection negotiated.
func chooseKeep(turnCancel bool) keepChoices {
	return keepChoices{onCancel: turnCancel}
}

// apply writes the chosen fields into an AgentSessionSpec. Fields left false are omitted, so a
// session that chose nothing starts exactly as before turn_cancel, also on an older Node.
func (k keepChoices) apply(spec map[string]any) {
	if k.onCancel {
		spec["keep_session_on_cancel"] = true
	}
	if k.onFailure {
		spec["keep_session_on_failure"] = true
	}
}

type agentSession struct {
	observeRequests   bool
	frozen            execution.Session
	scope             execution.Scope
	execution         string
	project           *projectRuntime
	checkout          *checkout
	projection        *nodehistory.Projection
	ready             chan struct{}
	prepareErr        error
	stopped           chan struct{}
	stopOnce          sync.Once
	commandMu         sync.Mutex
	mu                sync.Mutex
	started           bool
	ended             bool
	ending            bool
	broken            bool
	publicationFailed bool
	active            *activeTurn
	// sent is the ID of the latest turn whose command may have reached Node: set just before the
	// command is written (a failed write can be ambiguous) and cleared when Node refuses that
	// command without queueing or running it. Run reads it to mark a turn NotDelivered.
	sent string
	emit Emit
	// keep is fixed when the Agent is started (submit) and read under mu.
	keep keepChoices
	// stopNext is a turn StopTurn was asked to stop before it began; begin hands the stop to it.
	stopNext string
	// publishMu orders history publications, so a preview never lands after a newer snapshot.
	publishMu     sync.Mutex
	preview       *execution.HistorySnapshot // latest pending set not yet published
	previewQueued bool
	previewAt     time.Time // when the last preview flush ran
	previewSent   []byte    // the pending set the page last received
	timing        *preparation
	// recovered continues a held session whose Agent the previous server process lost: the new
	// Agent starts with the handoff block (see execution.Recovery).
	recovered    *recoveredHistory
	mcpRequests  map[string]chan mcpReply // protected by mu
	mcpWake      chan struct{}            // coalesces committed changes for this selection
	mcpSync      chan struct{}            // serializes desired delivery and refused-command reconciliation
	mcpDelivered int64                    // last desired generation confirmed by this Node incarnation
	// hostBlocks are the blocks only the Agent was sent, hostSecrets the credentials it was given
	// with its host tools (see host_tools.go). Protected by mu.
	hostBlocks  map[string]bool
	hostSecrets []string
	// The record of this execution when it was given host tools (see host_execution.go): whether it
	// may exist, why it ended, and whether that was written. Protected by mu.
	hostRecorded bool
	hostReason   string
	hostSettled  bool
}

// recoveredHistory joins a recovered session's new Node history to the one the page showed
// before: the old settled records come first, the new ones follow at positions after them, and
// the handoff block, which the new Agent was sent as its first user block, is never shown.
type recoveredHistory struct {
	handoff string
	base    []execution.HistoryRecord
	offset  uint32
}

func newRecoveredHistory(r *execution.Recovery) *recoveredHistory {
	h := &recoveredHistory{handoff: r.Handoff, base: append([]execution.HistoryRecord(nil), r.History...)}
	for _, record := range h.base {
		if record.Sequence >= h.offset {
			h.offset = record.Sequence + 1
		}
	}
	return h
}

// join maps a snapshot of the new Node history onto the page's conversation. A full snapshot
// carries the earlier records; a preview carries only pending records, which the page places
// among the records it has.
func (h *recoveredHistory) join(snapshot *execution.HistorySnapshot, full bool) *execution.HistorySnapshot {
	if h == nil {
		return snapshot
	}
	joined := &execution.HistorySnapshot{Incomplete: snapshot.Incomplete}
	if full {
		joined.Records = append(joined.Records, h.base...)
	}
	for _, record := range snapshot.Records {
		if isHandoff(record.Data, h.handoff) {
			continue
		}
		record.Sequence += h.offset
		joined.Records = append(joined.Records, record)
	}
	return joined
}

// isHandoff reports whether a Node history record is the user block carrying exactly the handoff.
// Node records each block of a user turn as its own user_message_chunk, so the handoff, sent as a
// separate block, is one record with that exact text.
func isHandoff(data []byte, handoff string) bool {
	return handoff != "" && userBlock(data) == handoff
}

func (d *Driver) session(ctx context.Context, scope execution.Scope, session execution.Session, config Project, turn execution.Turn, timing *preparation) (*agentSession, error) {
	caller, host := turn.Caller.Kind, turn.Host
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil, execution.ErrUnavailable
	}
	a := d.sessions[session.ID]
	if a != nil && session.Recovery != nil && a.replaceable() {
		// The session's Agent is lost (its Node died) and the caller brings it back: the new Agent
		// is a new execution, prepared like a first start.
		delete(d.sessions, session.ID)
		a = nil
	}
	if a != nil && (a.scope != scope || a.frozen.WorkspaceID != session.WorkspaceID || a.frozen.Target.ProjectID != session.Target.ProjectID || *a.frozen.Executor != *session.Executor) {
		d.mu.Unlock()
		return nil, execution.ErrConflict
	}
	if a == nil {
		target, executor := *session.Target, *session.Executor
		session.Target, session.Executor = &target, &executor
		recovery := session.Recovery
		session.Recovery = nil // frozen sessions compare by value; recovery is per start
		a = &agentSession{observeRequests: caller == execution.CallerTask || caller == execution.CallerChat, frozen: session, scope: scope, execution: d.nextID("a") + "-" + digest(session.ID), ready: make(chan struct{}), stopped: make(chan struct{})}
		if recovery != nil {
			a.recovered = newRecoveredHistory(recovery)
		}
		d.sessions[session.ID] = a
		go func() {
			defer func() {
				if a.prepareErr != nil {
					// Preparation has not sent Start: there is no physical Agent to retain.
					// Release only this reservation; another attempt may already own the map key.
					a.markStopped()
					d.mu.Lock()
					if d.sessions[session.ID] == a {
						delete(d.sessions, session.ID)
					}
					d.mu.Unlock()
				}
				close(a.ready)
			}()
			prepare, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			timing.enter(stageRuntime, time.Now())
			p, err := d.project(prepare, session.Target.ProjectID, config)
			a.project = p
			if err != nil {
				a.prepareErr = err
				return
			}
			if p.configuration.MirrorPath != config.MirrorPath {
				a.prepareErr = execution.ErrUnavailable
				return
			}
			if a.prepareErr = a.prepareMCP(prepare, session.MCP); a.prepareErr != nil {
				return
			}
			if host != nil && !p.takesHostTools() {
				a.prepareErr = execution.ErrHostMCPUnavailable
				return
			}
			p.mu.Lock()
			if p.fatal {
				p.mu.Unlock()
				a.prepareErr = execution.ErrUnavailable
				return
			}
			// Only a workflow run's sessions share a checkout; any other workspace has one owner.
			owners := p.owners[session.WorkspaceID]
			if _, own := owners[session.ID]; len(owners) != 0 && !own && caller != execution.CallerWorkflow {
				p.mu.Unlock()
				a.prepareErr = execution.ErrWorkspaceBusy
				return
			}
			if owners == nil {
				owners = map[string]*execution.Skills{}
				p.owners[session.WorkspaceID] = owners
			}
			// A replaced session (see replaceable) may still be stopping under the same ID: the
			// entries are this one's from now on, and only their holder removes them.
			owners[session.ID] = session.Skills
			p.holders[session.ID] = a
			delete(p.projected, session.ID)
			p.mu.Unlock()
			// Run has checked the executor names a known plugin; its install overlaps the clone.
			plugin, _ := findPlugin(session.Executor.AgentID, session.Executor.PluginVersion)
			installed := p.install(plugin)
			timing.enter(stageWorkspace, time.Now())
			a.checkout, a.prepareErr = p.prepareCheckout(prepare, scope, session.WorkspaceID, config, caller, timing)
			if a.prepareErr != nil {
				return
			}
			// The install overlaps the clone; this stage is only what remains of it.
			timing.enter(stagePlugin, time.Now())
			if a.prepareErr = installed.wait(prepare, p); a.prepareErr != nil {
				return
			}
			// Siblings start concurrently, so the live selections are read inside the same skillsMu
			// section that projects them: a snapshot taken at admission could be projected after a
			// later sibling's, removing that sibling's skills while its Agent is starting.
			p.skillsMu.Lock()
			defer p.skillsMu.Unlock()
			if session.Skills != nil {
				if err := p.applyWorkflowSkills(a.checkout.path, session.WorkspaceID, *session.Skills, p.liveSiblingSkills(session.WorkspaceID, session.ID)); err != nil {
					a.prepareErr = err
					return
				}
				p.mu.Lock()
				p.projected[session.ID] = true
				p.mu.Unlock()
			}
			a.projection = nodehistory.NewProjection(p.reader, a.execution)
			p.mu.Lock()
			if p.fatal {
				p.mu.Unlock()
				a.prepareErr = execution.ErrUnavailable
				return
			}
			p.agents[a.execution] = a
			p.mu.Unlock()
		}()
	}
	d.mu.Unlock()
	select {
	case <-a.ready:
		return a, a.prepareErr
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// replaceable reports whether a prepared session can no longer take a turn because its Agent is
// gone: it ended, broke, or its Node incarnation died. Callers hold the Driver's mu.
func (a *agentSession) replaceable() bool {
	select {
	case <-a.ready:
	default:
		return false // still preparing; its outcome decides
	}
	if a.project != nil {
		select {
		case <-a.project.dead:
			return true
		default:
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ended || a.broken
}

func (a *agentSession) begin(turn string, selection *execution.MCPSelection, emit Emit) (*activeTurn, bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ended || a.ending {
		return nil, false, execution.ErrSessionEnded
	}
	// Checked before broken: a Node's death also marks its sessions broken.
	select {
	case <-a.project.dead:
		return nil, false, execution.ErrNodeLost
	default:
	}
	if a.broken {
		return nil, false, execution.ErrUnavailable
	}
	if !a.frozen.MCP.SameMembers(selection) {
		// The live Agent could still use the formerly authorized members. Fence all
		// turns while holding the admission lock; the caller requests a confirmed End.
		a.broken = true
		return nil, false, &execution.MCPFailure{Code: "mcp_selection_invalid"}
	}
	if a.active != nil {
		return nil, false, execution.ErrWorkspaceBusy
	}
	call := &activeTurn{id: turn, done: make(chan turnOutcome, 1), cancelFallback: make(chan struct{}, 1), stopRequested: make(chan struct{}), progress: make(chan struct{}, 1)}
	if a.stopNext == turn {
		a.stopNext = ""
		call.requestStop()
	}
	if a.started && selection != nil && len(selection.Bindings) > 0 {
		call.updateRequired = make(chan struct{}, 1)
	}
	if a.started {
		call.commandID = "turn-" + digest(turn)
	}
	a.active, a.emit = call, emit
	// The first turn starts the Agent; submit marks the session started just before it sends.
	return call, !a.started, nil
}

func (a *agentSession) submit(admitted execution.Turn, text string, fresh bool) error {
	turn := admitted.Ref.TurnID
	lifecycle := a.frozen.MCP != nil && len(a.frozen.MCP.Bindings) > 0
	floor := admitted.MCPGeneration
	if lifecycle != (floor != nil) || floor != nil && (*floor < 0 || *floor > execution.MaxMCPCounter || fresh && *floor != 0) {
		return execution.ErrInvalidRequest
	}
	content := []any{map[string]string{"type": "text", "text": text}}
	if fresh && a.recovered != nil {
		// The recorded conversation goes first, as its own block, so the page can leave it out.
		content = append([]any{map[string]string{"type": "text", "text": a.recovered.handoff}}, content...)
	}
	if admitted.Host != nil {
		// Last, after the prompt. Sending a command Node refused again repeats it unchanged: the
		// Agent never got it, and Node may know the command by its ID.
		instruction := a.instruction(admitted)
		if len(instruction.Text) > execution.MaxHostInstruction {
			return execution.ErrInvalidRequest
		}
		a.conceal(instruction.Text, instruction.Token)
		content = append(content, map[string]string{"type": "text", "text": instruction.Text})
	}
	if fresh {
		// Run has already checked that the executor names a known plugin and binding.
		plugin, _ := findPlugin(a.frozen.Executor.AgentID, a.frozen.Executor.PluginVersion)
		configuration, err := plugin.agentConfiguration(a.project.driver.opts.Bindings[a.frozen.Executor.Model.ID].Model)
		if err != nil {
			return execution.ErrUnavailable
		}
		if err := a.project.services.Permit(a.execution, a.frozen.Executor.Model.ID); err != nil {
			return execution.ErrUnavailable
		}
		var hostServers []execution.HostMCPServer
		if admitted.Host != nil {
			attached := admitted.Host.Attach(a.frozen.ID)
			// Node closes the whole control connection on a list it refuses, so it is checked here.
			if len(attached.Servers) == 0 || execution.ValidateHostMCPServers(attached.Servers) != nil {
				return execution.ErrInvalidRequest
			}
			a.conceal("", attached.Secrets...)
			hostServers = attached.Servers
			// Every execution that holds a credential is on record before Node can start it.
			if a.recordHost() != nil {
				a.settleHost("agent_dispatch_failed")
				return execution.ErrUnavailable
			}
		}
		// Nothing above reached Node, so a failure there leaves the session not started: its turn
		// is reported NotStarted and End has no Agent to stop. From here on the write can be
		// ambiguous, so the session counts as started and End addresses this execution.
		//
		// What begin checked may have changed since. An End that arrived meanwhile found nothing
		// to stop, so no Agent may start after it: nothing would end it. (Whoever releases the
		// session, End itself or the retirement of a dead Node, revokes the permit just taken.)
		// A Node whose transport failed meanwhile cannot receive the start either, and the
		// session must not count as started for a command that was never sent.
		a.mu.Lock()
		if a.ended || a.ending {
			a.mu.Unlock()
			a.settleHost(execution.HostSessionEnded)
			return execution.ErrSessionEnded
		}
		select {
		case <-a.project.dead:
			a.mu.Unlock()
			a.settleHost(execution.FailureNodeLost)
			return execution.ErrUnavailable
		default:
		}
		a.started = true
		a.sent = turn
		// Node declares turn_cancel only to a Controller that asked and only beside agent_session.
		capable, ok := a.project.ipc().(interface{ Supports(string) bool })
		a.keep = chooseKeep(ok && capable.Supports("turn_cancel"))
		keep := a.keep
		a.mu.Unlock()
		spec := map[string]any{
			"node_id": a.project.nodeID(), "agent_plugin_id": plugin.id, "agent_plugin_version": plugin.version,
			"checkout_execution_id": a.checkout.id, "model_binding_id": a.frozen.Executor.Model.ID,
			"git_identity": map[string]string{"name": "V2", "email": "v2@example.test"},
			"initial_turn": map[string]any{"turn_id": turn, "content": content},
		}
		if configuration != nil {
			spec["agent_configuration"] = configuration
		}
		keep.apply(spec)
		if len(hostServers) != 0 {
			spec["host_mcp_servers"] = hostServers
		}
		if lifecycle {
			spec["mcp"] = mcpSpec(a.frozen.MCP)
			spec["mcp_lifecycle"] = true
		}
		err = a.project.send("start_agent_session", a.execution, map[string]any{"spec": spec})
		if err == nil && lifecycle && a.project.driver.opts.ReadMCPTarget != nil {
			// The watcher needs only the original authorization identities.
			ids := &execution.MCPSelection{Bindings: make([]execution.MCPBinding, len(a.frozen.MCP.Bindings))}
			for i, b := range a.frozen.MCP.Bindings {
				ids.Bindings[i].PluginID = b.PluginID
			}
			a.mu.Lock()
			a.frozen.MCP = ids
			a.mcpWake = make(chan struct{}, 1)
			a.mcpSync = make(chan struct{}, 1)
			a.mcpWake <- struct{}{}
			a.mu.Unlock()
			go a.followMCP(ids)
		}
		return err
	}
	command := map[string]any{"node_id": a.project.nodeID(), "command_id": "turn-" + digest(turn), "turn": map[string]any{"turn_id": turn, "content": content}}
	if lifecycle {
		command["mcp_generation"] = *floor
	}
	// Nothing above reached Node. From here on the write can be ambiguous, so the turn counts
	// as sent unless Node refuses its command.
	a.mu.Lock()
	a.sent = turn
	a.mu.Unlock()
	return a.project.send("submit_user_turn", a.execution, command)
}

// requestEnd never takes down the project's shared Node. The Driver owns process-tree retirement.
func (a *agentSession) requestEnd() {
	a.mu.Lock()
	if a.ended || a.ending {
		a.mu.Unlock()
		return
	}
	if !a.started {
		a.ending = true
		a.mu.Unlock()
		if a.project != nil {
			select {
			case <-a.project.dead:
				return
			default:
			}
		}
		a.markStopped()
		return
	}
	a.ending = true
	a.decideHostLocked(execution.HostSessionEnded)
	a.mu.Unlock()
	go func() {
		a.commandMu.Lock()
		defer a.commandMu.Unlock()
		if a.project.send("end_session", a.execution, map[string]string{"node_id": a.project.nodeID(), "command_id": "end-" + digest(a.frozen.ID), "reason": "user_ended"}) != nil {
			a.fail("agent_stop_unconfirmed", execution.ErrStopUnconfirmed)
		}
	}()
}

func (a *agentSession) markStopped() {
	a.stopOnce.Do(func() {
		a.commandMu.Lock()
		a.mu.Lock()
		a.ended = true
		a.frozen.MCP = nil
		a.mu.Unlock()
		a.commandMu.Unlock()
		if a.project != nil {
			if a.project.services != nil {
				a.project.services.End(a.execution)
			}
			a.project.mu.Lock()
			// A session replaced under the same ID leaves its successor's entries alone.
			if owners := a.project.owners[a.frozen.WorkspaceID]; owners != nil && a.project.holders[a.frozen.ID] == a {
				delete(owners, a.frozen.ID)
				delete(a.project.projected, a.frozen.ID)
				delete(a.project.holders, a.frozen.ID)
				if len(owners) == 0 {
					delete(a.project.owners, a.frozen.WorkspaceID)
				}
			}
			a.project.mu.Unlock()
		}
		close(a.stopped)
	})
}

func (a *agentSession) complete(outcome turnOutcome) {
	a.mu.Lock()
	call := a.active
	if call != nil && call.id == outcome.turnID {
		a.active = nil
	} else {
		call = nil
	}
	a.mu.Unlock()
	if call != nil {
		call.done <- outcome
	}
}

func (a *agentSession) fail(code string, err error) {
	a.mu.Lock()
	a.broken = true
	a.decideHostLocked(code)
	call := a.active
	stoppedByCaller := call != nil && call.cancel != ""
	a.mu.Unlock()
	if call == nil {
		return
	}
	if stoppedByCaller {
		// The caller stopped this turn and the session is lost before Node settled the stop (its
		// Node went away, say): the turn is the caller's stop, and nothing can follow it.
		if !errors.Is(err, execution.ErrStopUnconfirmed) {
			err = execution.ErrSessionEnded
		}
		a.complete(turnOutcome{call.id, execution.Result{Status: execution.StatusCancelled, StopReason: "cancelled", AgentLost: true}, err})
		return
	}
	// Infrastructure strings are deliberately not exposed through Run.
	if !errors.Is(err, execution.ErrStopUnconfirmed) && !errors.Is(err, execution.ErrNodeLost) {
		err = errors.New(code)
	}
	a.complete(turnOutcome{call.id, failed(code), err})
}

func (a *agentSession) publish(event execution.Event) error {
	a.mu.Lock()
	emit := a.emit
	a.mu.Unlock()
	if emit == nil {
		return errEventStore
	}
	event.Ref.SessionID = a.frozen.ID
	event.OccurredAt = time.Now().UTC()
	ctx, cancel := context.WithTimeout(a.project.ctx, 30*time.Second)
	defer cancel()
	if emit(ctx, event) != nil {
		a.mu.Lock()
		a.publicationFailed = true
		a.mu.Unlock()
		return errEventStore
	}
	return nil
}

func (a *agentSession) observe(message node.Message) (*turnOutcome, bool) {
	a.mu.Lock()
	call, broken, publicationFailed := a.active, a.broken, a.publicationFailed
	a.mu.Unlock()
	if message.OperationID != "op-"+a.execution && message.Type != "thread_preview" {
		a.fail("agent_identity_mismatch", execution.ErrUnavailable)
		a.requestEnd()
		return nil, false
	}
	if call != nil {
		a.mu.Lock()
		settling := call.cancel != ""
		a.mu.Unlock()
		if settling {
			signal(call.progress)
		}
	}
	if message.Type == "session_turn_failed" {
		var failure struct {
			TurnID    string  `json:"turn_id"`
			CommandID *string `json:"command_id"`
			Code      string  `json:"code"`
		}
		if json.Unmarshal(message.Payload, &failure) != nil || message.Sequence == nil || *message.Sequence == 0 ||
			call == nil || call.id != failure.TurnID || failure.Code != "mcp_refresh_wait_timeout" ||
			(failure.CommandID == nil && call.commandID != "") ||
			(failure.CommandID != nil && (*failure.CommandID == "" || *failure.CommandID != call.commandID)) {
			a.fail("agent_turn_failure_invalid", execution.ErrUnavailable)
			a.requestEnd()
			return nil, false
		}
		result := failed(failure.Code)
		// Node failed the queued command before the Agent was given it: the turn waited for an
		// MCP generation that never came, and the session stays open.
		result.NotDelivered = true
		if broken || publicationFailed || a.publish(execution.Event{Ref: execution.Ref{TurnID: call.id}, Kind: execution.EventTurnEnded, Result: &result}) != nil {
			a.fail("agent_event_write_failed", errEventStore)
			a.requestEnd()
			return nil, false
		}
		return &turnOutcome{call.id, result, nil}, true
	}
	if message.Type == "turn_cancel_settled" {
		return a.observeCancelSettled(message, call, publicationFailed)
	}
	if message.Type == "agent_session_ended" {
		var ended struct {
			Kind   string
			Result struct {
				Reason    string
				Node      node.Identity
				Detail    string
				PluginID  string `json:"plugin_id"`
				SettingID string `json:"setting_id"`
				Transport string
			}
		}
		validReason := func(reason string) bool {
			switch reason {
			case "user_ended", "idle_timeout", "cancelled", "agent_failed", "interrupted":
				return true
			default:
				return false
			}
		}
		if json.Unmarshal(message.Payload, &ended) != nil || ended.Kind != "agent_session_ended" || !validReason(ended.Result.Reason) || ended.Result.Node.NodeID == "" || ended.Result.Node != a.project.ipc().Identity() {
			a.fail("agent_terminal_invalid", execution.ErrUnavailable)
			return nil, false
		}
		// Before anyone learns of the end: what follows from it finds the execution settled.
		a.settleHost(ended.Result.Reason)
		var outcome *turnOutcome
		if call != nil {
			result := failed("agent_ended_before_turn")
			if ended.Result.Reason == "agent_failed" && execution.CheckoutRefused(ended.Result.Detail) {
				// Node refused only this session at admission; the checkout's other sessions and
				// the control connection are untouched. Its stable code says why.
				result = failed(ended.Result.Detail)
			}
			if ended.Result.Reason == "agent_failed" && ended.Result.Detail == nodeCheckoutUnavailable && a.checkout != nil && a.checkout.reattached {
				// Node no longer knows the clone execution an earlier incarnation prepared (its
				// ledger was lost): the checkout is gone for Node even if its directory is not.
				result = failed(execution.FailureWorkspaceMissing)
				a.project.forgetCheckout(a.frozen.WorkspaceID, a.checkout)
			}
			if ended.Result.Reason == "agent_failed" && ended.Result.Detail == execution.FailureHostMCPAgent {
				result = failed(ended.Result.Detail)
			}
			if ended.Result.Reason == "agent_failed" && execution.IsMCPFailureCode(ended.Result.Detail) {
				result = failed(ended.Result.Detail)
				result.MCPFailure = &execution.MCPFailure{Code: ended.Result.Detail, PluginID: ended.Result.PluginID, SettingID: ended.Result.SettingID, Transport: ended.Result.Transport}
			}
			a.mu.Lock()
			stoppedByCaller := call.cancel != ""
			a.mu.Unlock()
			if ended.Result.Reason == "cancelled" || ended.Result.Reason == "user_ended" || stoppedByCaller {
				// A session that ends under a turn the caller stopped still reports the stop.
				result = execution.Result{Status: execution.StatusCancelled, StopReason: "cancelled"}
			}
			result.AgentLost = true
			if !publicationFailed && a.publish(execution.Event{Ref: execution.Ref{TurnID: call.id}, Kind: execution.EventTurnEnded, Result: &result}) == nil {
				// The session is gone, not just the turn: the error tells the execution service
				// not to keep a held session for another turn.
				outcome = &turnOutcome{call.id, result, execution.ErrSessionEnded}
			} else {
				a.fail("agent_event_write_failed", errEventStore)
				publicationFailed = true
			}
		}
		if outcome != nil {
			// Hand the turn its outcome before the session reads as stopped. End, and a turn that
			// fell back to End, wait for the stop and then take the turn's outcome: they must find
			// this published one, not substitute a result of their own, which the receive loop's
			// hand-over after the ACK would leave room for.
			a.complete(*outcome)
		}
		a.markStopped()
		return nil, !publicationFailed
	}
	if message.Type == "session_command_rejected" {
		var rejection struct {
			CommandID string `json:"command_id"`
			Reason    string `json:"reason"`
		}
		parsed := json.Unmarshal(message.Payload, &rejection) == nil
		if parsed && call != nil && call.commandID != "" && call.commandID == rejection.CommandID {
			// Node refused the turn's command without queueing it (desktop's
			// SessionCommandRejection: a session that already ended, or an MCP generation still
			// to deliver), so its prompt has not reached the Agent; a resubmission sends it again.
			a.mu.Lock()
			if a.sent == call.id {
				a.sent = ""
			}
			a.mu.Unlock()
		}
		if parsed && call != nil {
			a.mu.Lock()
			refused := call.cancel != "" && call.cancel == rejection.CommandID
			a.mu.Unlock()
			if refused {
				// turn_cancel_unsupported or session_ended: the session cannot be kept.
				call.fallBack()
				return nil, true
			}
		}
		if parsed && rejection.Reason == "mcp_update_required" {
			if message.Sequence == nil && call != nil && call.updateRequired != nil && call.commandID == rejection.CommandID {
				a.mu.Lock()
				call.refused = true
				a.mu.Unlock()
				select {
				case call.updateRequired <- struct{}{}:
				default:
				}
			}
			return nil, false
		}
		a.fail("agent_session_ended", execution.ErrSessionEnded)
		return nil, true
	}
	if message.Type != "thread_event" && message.Type != "thread_preview" {
		return nil, !publicationFailed
	}
	if broken {
		return nil, !publicationFailed
	}
	observation, err := a.projection.Consume(a.project.ctx, message)
	if err != nil {
		a.mu.Lock()
		a.broken = true
		a.decideHostLocked(nodehistory.FailureCode(err))
		a.mu.Unlock()
		a.requestEnd()
		if call == nil {
			return nil, false
		}
		result := failed(nodehistory.FailureCode(err))
		result.AgentLost = true // requestEnd above ends the session
		if a.publish(execution.Event{Ref: execution.Ref{TurnID: call.id}, Kind: execution.EventTurnEnded, Result: &result}) != nil {
			a.fail("agent_event_write_failed", errEventStore)
			return nil, false
		}
		return &turnOutcome{call.id, result, errors.New(result.FailureCode)}, true
	}
	own := a.withhold(historyOf(observation.Snapshot))
	if observation.Turn != nil {
		observation.Turn.Answer = a.censor(observation.Turn.Answer)
	}
	snapshot := a.recovered.join(own, !observation.Preview)
	a.mu.Lock()
	timing := a.timing
	a.mu.Unlock()
	if timing != nil {
		// The first output this Agent produced, not the conversation it continues.
		timing.observeHistory(own)
	}
	if observation.Preview {
		a.queuePreview(snapshot)
		return nil, true
	}
	turnID := observation.TurnID
	if turnID == "" && call != nil {
		turnID = call.id
	}
	// A full snapshot carries the current pending records too and supersedes any queued preview.
	a.publishMu.Lock()
	a.mu.Lock()
	a.preview = nil
	a.mu.Unlock()
	err = a.publish(execution.Event{Ref: execution.Ref{TurnID: turnID}, Kind: execution.EventHistorySnapshot, History: snapshot})
	if err == nil {
		a.mu.Lock()
		a.previewSent = pendingKey(snapshot)
		a.mu.Unlock()
	}
	a.publishMu.Unlock()
	if err != nil {
		a.fail("agent_event_write_failed", errEventStore)
		a.requestEnd()
		return nil, false
	}
	if observation.Turn == nil {
		return nil, true
	}
	if call == nil || observation.TurnID != call.id {
		a.fail("agent_turn_identity_mismatch", execution.ErrUnavailable)
		a.requestEnd()
		return nil, false
	}
	a.mu.Lock()
	cancelling := call.cancel != ""
	if cancelling {
		call.stopped = observation.Turn
	}
	a.mu.Unlock()
	if cancelling {
		// The caller stopped this turn: its TurnCancelSettled follows and decides both its result
		// and whether the session stays. The history is stored, so the event can be acknowledged.
		return nil, true
	}
	result := turnResult(observation.Turn, call.guard.failure())
	if a.publish(execution.Event{Ref: execution.Ref{TurnID: turnID}, Kind: execution.EventTurnEnded, Result: &result}) != nil {
		a.fail("agent_event_write_failed", errEventStore)
		a.requestEnd()
		return nil, false
	}
	return &turnOutcome{turnID, result, nil}, true
}

func historyOf(projected nodehistory.Snapshot) *execution.HistorySnapshot {
	snapshot := &execution.HistorySnapshot{Incomplete: projected.Incomplete}
	for _, record := range projected.Records {
		snapshot.Records = append(snapshot.Records, execution.HistoryRecord{Sequence: record.Sequence, Data: record.Data, Preview: record.Preview})
	}
	return snapshot
}

// pendingKey identifies the pending part of a snapshot, so an unchanged preview is not retained.
func pendingKey(snapshot *execution.HistorySnapshot) []byte {
	pending := execution.HistorySnapshot{Incomplete: snapshot.Incomplete}
	for _, record := range snapshot.Records {
		if record.Preview {
			pending.Records = append(pending.Records, record)
		}
	}
	key, _ := json.Marshal(pending)
	return key
}

// queuePreview keeps only the newest pending set and schedules one flush per previewInterval;
// the trailing flush still publishes the last preview before a pause, such as a running tool.
func (a *agentSession) queuePreview(pending *execution.HistorySnapshot) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.emit == nil || a.ended {
		return
	}
	a.preview = pending
	if a.previewQueued {
		return
	}
	a.previewQueued = true
	time.AfterFunc(time.Until(a.previewAt.Add(previewInterval)), a.flushPreview)
}

// flushPreview publishes the queued pending set. Previews are best effort and never ACKed or
// treated as an answer: a lost one is replaced by the next preview or by settled history.
func (a *agentSession) flushPreview() {
	a.publishMu.Lock()
	defer a.publishMu.Unlock()
	a.mu.Lock()
	preview, emit, call := a.preview, a.emit, a.active
	a.preview, a.previewQueued, a.previewAt = nil, false, time.Now()
	if preview == nil || a.broken || a.ended || bytes.Equal(pendingKey(preview), a.previewSent) {
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	ref := execution.Ref{SessionID: a.frozen.ID}
	if call != nil {
		ref.TurnID = call.id
	}
	ctx, cancel := context.WithTimeout(a.project.ctx, 10*time.Second)
	defer cancel()
	if emit(ctx, execution.Event{Ref: ref, OccurredAt: time.Now().UTC(), Kind: execution.EventHistoryPreview, History: preview}) == nil {
		a.mu.Lock()
		a.previewSent = pendingKey(preview)
		a.mu.Unlock()
	}
}

// turnResult maps Node's stop reason. Node records every failed turn as cancelled, so a turn
// whose last model call was cut at a limit (cut) fails with that limit as its reason instead.
func turnResult(turn *nodehistory.Turn, cut string) execution.Result {
	result := execution.Result{Answer: turn.Answer, StopReason: turn.StopReason}
	switch turn.StopReason {
	case "end_turn":
		result.Status, result.AnswerComplete = execution.StatusCompleted, true
	case "cancelled":
		result.Status = execution.StatusCancelled
		if cut != "" {
			result.Status, result.FailureCode = execution.StatusFailed, cut
		}
	case "max_tokens", "max_turn_requests", "refusal":
		result.Status, result.FailureCode = execution.StatusFailed, "agent_turn_incomplete"
	default:
		result.Status, result.FailureCode, result.StopReason = execution.StatusFailed, "agent_stop_unknown", ""
	}
	return result
}
