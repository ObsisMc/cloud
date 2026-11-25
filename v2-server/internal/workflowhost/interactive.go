package workflowhost

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/server"
	"github.com/wanglongan587/cloud/v2-server/internal/store"
	"github.com/wanglongan587/cloud/v2-server/internal/workflow"
)

// Interactive Agent nodes (desktop@ebd5b725 docs/workflows/workflow-runs.md "Interactive nodes"):
// after its first turn the node parks as pending with its session open; the run's starter sends
// further turns into the same session or completes the node through the completion gate of completion.go: the turn the node parked with
// must have ended normally and, for the workflow's own opening turn, be confirmed; force covers
// the other blockers and leaves an audit. A node whose policy is agent also waits for its Agent to
// report the step done in that turn (package completiontool) and for the starter, or the node's
// auto-accept, to confirm the report.
//
// How a turn ended never fails the node (workflow-runs.md:125-126, 152-155): a refused, truncated,
// unknown or unrequested cancelled turn keeps it parked with its held session open, so the starter
// can keep talking, and completion waits for a turn that ended normally. So does a turn the
// starter stopped when the Node keeps its session through the stop. Only a lost Agent, a stop that
// lost it included, ends the session; the node then parks for completion with the last normal
// answer, if there is one.
//
// Every transition of one node is claimed under runState.mu through interactiveNode.phase, so a
// follow-up turn, a completion and a stop never act on the same node at once: exactly one claim
// wins and the others see the phase it left. A follow-up turn executes at once, concurrently with
// the run's other Agents, as each node's turn is its own session's: the directory holds several
// live sessions (the Node's shared_checkout_sessions capability, which the Agent driver requires),
// and a parked node keeps its session while the run's other Agents start, run and end theirs
// beside it, so the starter's next message reaches the same session whatever ran in between.

// interactivePhase is what an interactive node is doing.
type interactivePhase int

const (
	// phaseTurn: a turn is queued or executing; the row is running.
	phaseTurn interactivePhase = iota
	// phaseParked: no turn in flight; the row is pending (awaiting input).
	phaseParked
	// phaseCompleting: a completion claimed the parked node; the row is still pending.
	phaseCompleting
)

// turnErrorNotDelivered is payload.interactive.last_turn_error for a message the starter sent
// that never reached the node's session.
const turnErrorNotDelivered = "not_delivered"

// interactiveNode is one root-scope interactive Agent attempt with an open session. Fields other
// than the immutable dispatch data are guarded by the owning runState.mu.
type interactiveNode struct {
	d        workflow.AgentDispatch
	executor resolvedExecutor
	cfg      workflow.AgentConfig
	task     string
	session  string

	// policy is the completion policy the node's session runs under: its frozen one, except that
	// a node that parked before v2 applied the agent policy stays end_turn until its session ends.
	policy string
	// signal is the completion the Agent reported in the settled turn its TurnKey names.
	signal *agentSignal

	phase interactivePhase
	// turns counts the turns that settled; seq numbers the follow-up caller attempts, that is
	// the messages the starter sent (the summary's sent).
	turns, seq int
	// last is the latest settled turn; accepted the latest one that ended normally.
	last, accepted *execution.Result
	// lastOrigin says who opened last (originWorkflow for the first turn), lastSeq the number of
	// the message that opened it (0 for the first turn). lastStopCause is why last stopped when
	// the snapshot cannot tell from its result alone (the starter's stop, a lost result).
	lastOrigin    string
	lastSeq       int
	lastStopCause string
	// unsettled means the latest turn ended without a readable result: last is an earlier turn
	// and the current one has no answer.
	unsettled bool
	// baseline is what a mustChange deliverable is compared with, nil without one.
	baseline *fileBaseline
	// baselineUnwritten means the baseline is not in the payload yet: the first summary write,
	// at dispatch, carries it, and later writes leave the stored one in place.
	baselineUnwritten bool
	// sessionEnded means the session accepts no further turn: the Agent was lost, or a stop the
	// Node could not keep the session through ended it (execution.Service.Cancel).
	sessionEnded bool
	// sessionGone means Node no longer holds the session: its End returned (or the session was
	// already gone). Until then the node keeps its slot of the run's checkout (see sessionsInUse).
	sessionGone bool
	// endRetrying marks a background retry of an unconfirmed End (see endInteractive).
	endRetrying bool
	// stopping marks a turn the starter asked to stop, so its end is recorded as their stop. The
	// session outlives the stop unless the turn's result reports it lost (AgentLost).
	stopping bool
	// undelivered means the last message the starter sent was accepted (SendNodeTurn returned)
	// and then never reached the Agent: the execution service refused it, or accepted it and the
	// Agent driver refused it (execution.Result.NotDelivered). The summary says so until the next
	// message is claimed; nothing else records the lost message as such.
	undelivered bool
	// toolUnconnected means the node's Agent had not connected to its completion tool when the node
	// last parked. It says so on the node and changes nothing else (host completion tool ADR D8).
	toolUnconnected bool
}

// followUp is one human turn of an interactive node.
type followUp struct {
	node   *interactiveNode
	caller execution.Caller
	prompt execution.Prompt
	// seq is the message's number among the starter's messages to the node.
	seq int
}

// delivered reports whether a settled turn's prompt reached the Agent. Only NotDelivered says it
// did not: the execution service sets it on NotStarted turns too, while a turn recorded before
// the flag existed carries NotStarted alone and was counted as a turn, so it still is (its
// summary never said the message was lost, and a rebuild must not change that silently).
func delivered(r execution.Result) bool {
	return !r.NotDelivered
}

// stoppedBeforeDelivery reports whether a settled turn is one the starter stopped before its
// prompt reached the Agent (Node dropped it, or never held a refused submission). It is a stopped
// turn of the conversation, not a lost message, but it answered nothing.
func stoppedBeforeDelivery(r execution.Result) bool {
	return r.NotDelivered && r.Status == execution.StatusCancelled
}

// endedNormally is the end_turn rule: only a complete EndTurn answer completes without force.
func endedNormally(r execution.Result) bool {
	return r.Status == execution.StatusCompleted && r.AnswerComplete && r.StopReason == "end_turn"
}

// attempt is the caller attempt of the node's turn opened by message seq, 0 for its first turn.
func (n *interactiveNode) attempt(seq int) string {
	if seq == 0 {
		return n.d.NodeRunID
	}
	return n.d.NodeRunID + "/turn-" + strconv.Itoa(seq)
}

// tool is the completion tool of the turn opened by message seq, nil when the policy gives none.
func (h *Host) tool(n *interactiveNode, seq int) execution.HostTools {
	if n.policy != workflow.CompletionSignalAgent || h.tools == nil {
		return nil
	}
	return h.tools.Attempt(n.d.NodeRunID, n.attempt(seq))
}

// track registers a node whose first turn is executing.
func (st *runState) track(n *interactiveNode) *interactiveNode {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.interactive == nil {
		st.interactive = map[string]*interactiveNode{}
	}
	st.interactive[n.d.Node.ID] = n
	return n
}

// summary is payload.interactive, what the run page shows about a parked node: the completion
// snapshot and the answer a completion would take. Callers hold st.mu.
func (n *interactiveNode) summary() map[string]any {
	snap := n.snapshot()
	// sent numbers the starter's messages claimed so far; last_turn_error is about the latest
	// one, so a page can tell whether a notice concerns a message it sent itself.
	s := map[string]any{"turns": n.turns, "sent": n.seq, "session_ended": n.sessionEnded,
		"completable": len(snap.Blockers) == 0, "completion": snap}
	if n.last != nil {
		s["last_stop_reason"] = n.last.StopReason
	}
	if n.undelivered {
		s["last_turn_error"] = turnErrorNotDelivered
	}
	if n.toolUnconnected {
		s["completion_tool_unconnected"] = true
	}
	// Members who cannot read the starter's session still see what completing would take: the
	// settled turn's answer, or what a forced completion would take instead.
	if answer, source := n.candidate(snap); source != outputWithoutTurn {
		s["answer"], s["answer_source"] = answer, source
	}
	return s
}

// candidate is the answer a completion of snap takes and where it comes from: the settled turn's
// for a turn the starter can confirm or complete, otherwise the forced choice. Callers hold st.mu.
func (n *interactiveNode) candidate(snap completionSnapshot) (string, string) {
	for _, b := range snap.Blockers {
		if !participation(b.Kind) {
			g, _ := n.gate(snap, nil, approval{force: true})
			source, _ := g.forced["output_source"].(string)
			return g.answer, source
		}
	}
	if n.last == nil || n.unsettled {
		return "", outputWithoutTurn
	}
	return n.last.Answer, outputTurnWindow
}

// summaryKeys are the payload keys a park or refresh writes; the first one also records the
// baseline. Callers hold st.mu.
func (n *interactiveNode) summaryKeys() map[string]any {
	keys := map[string]any{"interactive": n.summary()}
	if n.baseline != nil && n.baselineUnwritten {
		keys["completion_baseline"] = n.baseline
		n.baselineUnwritten = false
	}
	return keys
}

// settleInteractiveTurn records how a turn of n ended and parks the node again; a turn's outcome
// never fails the node. The session ends only when the turn lost its Agent (its result could not
// be read, or the execution service reports AgentLost, as it does for a stop that ended the
// session); any other turn, a stop the session outlived included, leaves the held session open,
// and completion waits for a turn that ended normally.
//
// The one exception is a first turn whose session was never set up: its Agent never started (the
// project, checkout, plugin or Skills could not be prepared) or Node refused it at the checkout
// (the run already holds its limit of live sessions, for example). The node then fails like any
// other session that could not start, resumable once the cause is gone, instead of parking with
// neither a conversation nor an answer. A session that was lost after its Agent started still
// parks, as desktop keeps a crashed turn awaiting input.
//
// seq is the number of the starter's message that opened the turn, 0 for the workflow's own first
// turn.
func (h *Host) settleInteractiveTurn(st *runState, a *attemptState, n *interactiveNode, seq int, result execution.Result, err error) {
	// The turn is over: whatever its Agent reported belongs to it, and a later call is refused.
	reported := h.tools.Take(n.d.NodeRunID, n.attempt(seq))
	if cancelling, last := st.endTurn(a, nil); cancelling {
		h.tools.Revoke(n.d.NodeRunID)
		h.end(st.scope, n.session)
		if last {
			h.finishCancel(st, n.d.RunID)
		}
		return
	}
	if err != nil && h.ctx.Err() != nil {
		return // shutting down; the next start interrupts the run
	}
	st.mu.Lock()
	if !result.NotStarted {
		// Otherwise the execution the tool still knows is an earlier one, over with its Agent.
		h.judgeTool(n)
	}
	// A turn the starter stopped is never a setup failure, even when the stop caught the session
	// still being prepared: it parks like any stopped turn.
	if err == nil && n.turns == 0 && !n.stopping && result.AgentLost &&
		(result.NotStarted || execution.CheckoutRefused(result.FailureCode) || execution.HostMCPFailure(result.FailureCode)) {
		h.tools.Revoke(n.d.NodeRunID)
		// Nothing is running, so End is idempotent; until it is confirmed the session keeps
		// its slot.
		lingering := st.dropInteractive(n)
		st.mu.Unlock()
		if lingering {
			h.endLingering(st, n.d.RunID, n.session)
		}
		message := "the Agent session could not be set up: " + result.FailureCode
		if execution.CheckoutRefused(result.FailureCode) {
			message = "Node refused the Agent session on the run's directory: " + result.FailureCode
		}
		if execution.HostMCPFailure(result.FailureCode) {
			message = "the node's Agent cannot be given the completion tool its policy needs: " + result.FailureCode
		}
		h.fail(n.d, workflow.Failure{Kind: workflow.FailureInteractiveSession, Message: message})
		return
	}
	if err == nil && result.FailureCode == execution.FailureWorkspaceMissing {
		// The node's session outlived a restart, but the checkout its Agent worked in is gone (or
		// unknown to Node): nothing can continue the conversation or produce what it was asked for.
		// As for a first turn that could not be set up, the session keeps its slot until its End
		// is confirmed (the execution service has ended it, so End is idempotent).
		message := "the run's checkout no longer exists, so the node's conversation cannot continue"
		if n.turns == 0 {
			// Only a reattached checkout Node no longer knows fails a first turn this way, such
			// as the successor of a node completed after the restart: it never had a conversation
			// to lose, and the record was dropped with it, so a resume from the failure clones.
			message = "Node no longer knows the run's checkout; resuming from the failure starts the node on a fresh clone"
		}
		lingering := st.dropInteractive(n)
		st.mu.Unlock()
		h.tools.Revoke(n.d.NodeRunID)
		if lingering {
			h.endLingering(st, n.d.RunID, n.session)
		}
		h.fail(n.d, workflow.Failure{Kind: workflow.FailureInteractiveSession, Message: message})
		return
	}
	if err == nil && seq > 0 && stoppedBeforeDelivery(result) {
		// The starter stopped the message before the Agent got it. It counts as a turn, the stopped
		// one the conversation shows, but it answered nothing: the node keeps the turn it last
		// answered, so the answer it would complete with, and whether it can, stay as they were.
		// The stop kept the session unless the result says it is lost.
		n.turns++
		if n.signal != nil {
			n.signal.TurnKey.EndedSeq = n.turns // it still speaks of the answer the node keeps
		}
		ended := result.AgentLost
		n.stopping = false
		if ended {
			n.sessionEnded = true
		}
		st.mu.Unlock()
		if ended {
			h.endInteractive(st, n)
		}
		h.park(st, n)
		return
	}
	if err == nil && seq > 0 && !delivered(result) {
		// The execution service accepted the starter's message and the Agent driver then refused
		// it before the Agent got it (the session had ended, broken or lost its Node and no new
		// Agent could take it, or Node refused the command). It is not a turn: the node stays as
		// its last delivered turn left it, and the summary says the message was lost, as for one
		// refused at Append. A session the refusal ended (AgentLost) takes no further message; a
		// stop of the message does not end it by itself, the session outlives a stop the Node keeps
		// it through, as for any stopped turn.
		n.undelivered = true
		ended := result.AgentLost
		n.stopping = false
		if ended {
			n.sessionEnded = true
		}
		st.mu.Unlock()
		if ended {
			h.endInteractive(st, n)
		}
		h.log.Warn("interactive workflow turn not delivered", "run", n.d.RunID, "node_run", n.d.NodeRunID, "failure", result.FailureCode, "session_ended", ended)
		h.park(st, n)
		return
	}
	n.lastStopCause = ""
	if n.stopping {
		n.lastStopCause = stopUserStopped
	}
	n.unsettled = err != nil
	n.signal = nil
	if err == nil {
		n.turns++
		n.last = &result
		n.lastSeq, n.lastOrigin = seq, originHuman
		if seq == 0 {
			n.lastOrigin = originWorkflow
		}
		if endedNormally(result) {
			n.accepted = &result
		}
		// Only a turn that ended normally keeps its report; any other drops it (completion signal ADR
		// D2), where desktop 22c5b83f binds it to the park all the same.
		if reported != nil && n.policy == workflow.CompletionSignalAgent && endedNormally(result) {
			n.signal = &agentSignal{SignalID: reported.ID, RequestedAt: reported.At.UnixMilli(), TurnKey: turnKey{UserSeq: seq, EndedSeq: n.turns}}
		}
	} else if !n.stopping {
		n.lastStopCause = stopSessionLost
	}
	ended := err != nil || result.AgentLost
	n.stopping = false
	if ended {
		n.sessionEnded = true
	}
	st.mu.Unlock()
	if ended {
		// A lost Agent (or a stop that lost it) has already ended the session; ending again is
		// idempotent and covers a turn whose result could not be read. Like an automatic attempt, the slot frees
		// only once End is confirmed; until then the node keeps it, and completing the node or
		// the run's end ends the session again.
		h.endInteractive(st, n)
	}
	if err != nil {
		h.log.Warn("lost an interactive workflow turn", "run", n.d.RunID, "node_run", n.d.NodeRunID, "error", publicReason(err))
	}
	if h.park(st, n) {
		h.autoAccept(st, n)
	}
}

// judgeTool concludes whether the node's Agent connected to its completion tool, as a turn ends and
// before anything withdraws the tool with the session; the park that follows writes it. It speaks of
// the execution the node has now: without one (its start was never sent, or the server restarted
// since) the last conclusion stands until the next execution first parks. Callers hold st.mu.
func (h *Host) judgeTool(n *interactiveNode) {
	if unconnected, known := h.tools.Unconnected(n.d.NodeRunID); known {
		n.toolUnconnected = unconnected
	}
}

// refreshSummary rewrites payload.interactive of n's row, which has status, in place: the run
// page reads the node as an interactive session from it, from its first turn on. A failed write
// is only logged: the next write of the node records the summary again. It is not retried like a
// park, which would hold a completion's request for as long as storage is out.
func (h *Host) refreshSummary(st *runState, n *interactiveNode, status string) {
	if err := h.writeSummary(h.ctx, st, n, status); err != nil {
		h.log.Error("record the session summary of an interactive workflow node", "run", n.d.RunID, "node_run", n.d.NodeRunID, "error", err)
	}
	h.hub.Publish(n.d.RunID)
}

// writeSummary is refreshSummary's write, returning its error to callers that cannot go on
// without it.
func (h *Host) writeSummary(ctx context.Context, st *runState, n *interactiveNode, status string) error {
	st.mu.Lock()
	keys := n.summaryKeys()
	st.mu.Unlock()
	if _, err := h.moveEngineNode(ctx, n.d.RunID, n.d.NodeRunID, status, status,
		func(node []byte) ([]byte, error) { return workflow.MergePayload(node, keys) }); err != nil {
		if _, ok := keys["completion_baseline"]; ok {
			st.mu.Lock()
			n.baselineUnwritten = true // the next write records it
			st.mu.Unlock()
		}
		return fmt.Errorf("record the session summary of node run %s: %w", n.d.NodeRunID, err)
	}
	return nil
}

// moveEngineNode is store.MoveWorkflowEngineNode, or moveNode where a test replaces it.
func (h *Host) moveEngineNode(ctx context.Context, runID, nodeRunID, from, to string, merge func(node []byte) ([]byte, error)) (bool, error) {
	if h.moveNode != nil {
		return h.moveNode(ctx, runID, nodeRunID, from, to, merge)
	}
	return h.store.MoveWorkflowEngineNode(ctx, runID, nodeRunID, from, to, merge)
}

// park moves the node back to awaiting input with its summary. A run that stopped running
// meanwhile (a sibling failed it) has no use for the node: its session ends and the row closes.
// It reports whether the node parked.
func (h *Host) park(st *runState, n *interactiveNode) bool {
	st.mu.Lock()
	keys := n.summaryKeys()
	keys["task"] = n.task
	st.mu.Unlock()
	var moved bool
	writeUncertain := false
	err := h.resultRetry.Store(h.ctx, func(ctx context.Context) error {
		var err error
		moved, err = h.moveEngineNode(ctx, n.d.RunID, n.d.NodeRunID, store.WorkflowStatusRunning, store.WorkflowStatusPending,
			func(node []byte) ([]byte, error) { return workflow.MergePayload(node, keys) })
		if err != nil {
			writeUncertain = true
			return err
		}
		if !moved && writeUncertain {
			// An earlier attempt may have committed before its connection broke. Refreshing in
			// pending succeeds only while the run is still running and the row already parked.
			moved, err = h.moveEngineNode(ctx, n.d.RunID, n.d.NodeRunID, store.WorkflowStatusPending, store.WorkflowStatusPending,
				func(node []byte) ([]byte, error) { return workflow.MergePayload(node, keys) })
		}
		return err
	})
	if err != nil {
		if h.ctx.Err() == nil {
			// The row stays running and the node unreachable until the run ends; cancelling it still
			// ends the session through the sweep.
			h.log.Error("park interactive workflow node", "run", n.d.RunID, "node_run", n.d.NodeRunID, "error", err)
		}
		return false
	}
	if !moved {
		// Only a run that stopped running refuses the park of its own running attempt.
		h.endInteractive(st, n)
		h.forgetInteractive(st, n)
		h.closeOrphan(n)
		h.hub.Publish(n.d.RunID)
		return false
	}
	st.mu.Lock()
	n.phase = phaseParked
	st.mu.Unlock()
	h.hub.Publish(n.d.RunID)
	return true
}

// starter authorizes a turn, stop or completion: the node's session belongs to whoever started the
// run, so nobody else can act on it. Other members still read the run.
func (h *Host) starter(ctx context.Context, scope server.WorkflowRunScope, runID string) (*runState, error) {
	if _, err := h.store.WorkflowRun(ctx, scope.TenantID, runID); err != nil {
		return nil, err
	}
	starter, err := h.store.WorkflowStarter(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("read workflow run starter: %w", err)
	}
	if starter == "" {
		return nil, nil // never started: nothing to talk to yet
	}
	if starter != scope.UserID {
		return nil, server.ErrWorkflowRunOwned
	}
	return h.state(runID), nil
}

// nodeOf finds a tracked node, or reports it as not awaiting input. Callers hold st.mu.
func (st *runState) nodeOf(nodeID string) (*interactiveNode, error) {
	if n := st.interactive[nodeID]; n != nil && !st.cancelling {
		return n, nil
	}
	return nil, server.ErrWorkflowNodeNotAwaitingInput
}

// SendNodeTurn claims a parked node for a human turn, marks it running and starts the turn. The
// answer arrives over the node's session; the node parks again after it. It answers with the
// number the message was claimed under, the value payload.interactive.sent has from now on.
func (h *Host) SendNodeTurn(ctx context.Context, scope server.WorkflowRunScope, runID, nodeID, content string) (server.WorkflowNodeTurnAccepted, error) {
	st, err := h.starter(ctx, scope, runID)
	if err != nil {
		return server.WorkflowNodeTurnAccepted{}, err
	}
	if st == nil {
		return server.WorkflowNodeTurnAccepted{}, server.ErrWorkflowNodeNotAwaitingInput
	}
	st.mu.Lock()
	n, err := st.nodeOf(nodeID)
	switch {
	case err != nil:
	case n.phase != phaseParked:
		err = server.ErrWorkflowNodeNotAwaitingInput
	case n.sessionEnded:
		err = server.ErrWorkflowNodeSessionEnded
	}
	if err != nil {
		st.mu.Unlock()
		return server.WorkflowNodeTurnAccepted{}, err
	}
	n.phase = phaseTurn
	n.seq++
	sent := n.seq
	caller := execution.Caller{Kind: execution.CallerWorkflow, ID: runID, NodeID: nodeID, AttemptID: n.attempt(sent)}
	// This message replaces the one that was not delivered, so the row stops saying so as it
	// starts running; if this one is refused too, the park writes the notice again.
	undelivered := n.undelivered
	n.undelivered = false
	keys := n.summaryKeys()
	st.mu.Unlock()

	moved, err := h.store.MoveWorkflowEngineNode(ctx, runID, n.d.NodeRunID, store.WorkflowStatusPending, store.WorkflowStatusRunning,
		func(node []byte) ([]byte, error) { return workflow.MergePayload(node, keys) })
	if err != nil || !moved {
		st.mu.Lock()
		n.phase = phaseParked
		// The row was not rewritten and no turn was registered: this message was never the
		// starter's next one, so neither the notice nor the count moves.
		n.undelivered = undelivered
		n.seq--
		st.mu.Unlock()
		if err == nil {
			err = server.ErrWorkflowNodeNotAwaitingInput
		}
		return server.WorkflowNodeTurnAccepted{}, err
	}
	h.hub.Publish(runID)
	st.mu.Lock()
	if st.cancelling {
		// The run was cancelled after the row moved: the turn would never run, so do not accept
		// it. The cancellation ends the session and closes the row.
		st.mu.Unlock()
		return server.WorkflowNodeTurnAccepted{}, server.ErrWorkflowNodeNotAwaitingInput
	}
	// Registered before the goroutine runs, so a stop arriving first finds the turn starting.
	a := st.register(caller.AttemptID, n.d.NodeRunID, "", st.generation)
	if a == nil {
		st.mu.Unlock()
		return server.WorkflowNodeTurnAccepted{}, server.ErrWorkflowNodeNotAwaitingInput
	}
	st.dispatched++
	h.workers.Add(1)
	go func() {
		defer h.settled(runID, st)
		h.runFollowUp(st, a, followUp{node: n, caller: caller, prompt: execution.TextPrompt(content), seq: sent})
	}()
	st.mu.Unlock()
	run, err := h.store.WorkflowRun(ctx, scope.TenantID, runID)
	if err != nil {
		return server.WorkflowNodeTurnAccepted{}, err
	}
	return server.WorkflowNodeTurnAccepted{Run: run, Sent: sent}, nil
}

// runFollowUp executes one human turn registered as a and parks the node after it.
func (h *Host) runFollowUp(st *runState, a *attemptState, f followUp) {
	n := f.node
	if h.followingUp != nil {
		h.followingUp(n.d.Node.ID)
	}
	// Another node may have failed the run since the turn was accepted. Like an attempt (see
	// runAgent), check with the turn registered, and run no turn for a run that stopped; the park
	// is then refused, which ends the session and closes the row.
	if h.runStopped(h.ctx, n.d.RunID) {
		if st.releaseStart(a) {
			h.end(st.scope, n.session) // a cancellation stopped the run; close it once
			h.finishCancel(st, n.d.RunID)
			return
		}
		// Accepted and not run. The park is normally refused and closes the node; should the
		// run be running again by then, the node waits on and says the message was lost.
		st.mu.Lock()
		n.undelivered = true
		st.mu.Unlock()
		h.park(st, n)
		return
	}
	ref, err := h.appendTurn(st.scope, execution.AppendRequest{SessionID: n.session, Caller: f.caller, Prompt: f.prompt, Host: h.tool(n, f.seq)})
	if cancelling, last, _ := st.endStart(a, ref, err); cancelling {
		h.tools.Revoke(n.d.NodeRunID)
		h.end(st.scope, n.session)
		if err == nil {
			_, last = st.endTurn(a, nil)
		}
		if last {
			h.finishCancel(st, n.d.RunID)
		}
		return
	}
	if err != nil {
		if h.ctx.Err() != nil {
			return
		}
		h.tools.Take(n.d.NodeRunID, n.attempt(f.seq)) // no Agent has it
		// The turn never started. A session that ended underneath accepts no more turns; any other
		// refusal leaves the node waiting for another attempt. SendNodeTurn has already answered
		// the starter, so the summary is the only place that can say the message was lost.
		h.log.Warn("send interactive workflow turn", "run", n.d.RunID, "node_run", n.d.NodeRunID, "error", publicReason(err))
		st.mu.Lock()
		n.undelivered = true
		if errors.Is(err, execution.ErrSessionEnded) {
			// As for a turn that started nothing, the execution the tool still knows is over.
			n.sessionEnded, n.sessionGone = true, true
			st.slotsMayHaveChanged()
		} else {
			h.judgeTool(n)
		}
		st.mu.Unlock()
		h.park(st, n)
		return
	}
	result, err := h.awaitTurn(st.scope, ref, f.caller)
	h.settleInteractiveTurn(st, a, n, f.seq, result, err)
}

// appendTurn waits out ErrWorkspaceBusy while the session's previous turn finishes cleaning up:
// its terminal event is published before the execution worker releases the session.
func (h *Host) appendTurn(scope execution.Scope, req execution.AppendRequest) (execution.Ref, error) {
	deadline := time.Now().Add(h.busyRetry)
	for {
		ref, err := h.exec.Append(h.ctx, scope, req)
		if !errors.Is(err, execution.ErrWorkspaceBusy) || time.Now().After(deadline) {
			return ref, err
		}
		select {
		case <-time.After(50 * time.Millisecond):
		case <-h.ctx.Done():
			return execution.Ref{}, h.ctx.Err()
		}
	}
}

// StopNodeTurn stops the turn an interactive node is executing (execution.Service.Cancel). The
// node parks awaiting the starter, who can talk on when the session outlived the stop; when the
// Node could not keep it, the session ended and the node parks for completion only. The run
// itself keeps running. A turn still starting is awaited first.
func (h *Host) StopNodeTurn(ctx context.Context, scope server.WorkflowRunScope, runID, nodeID string) (store.WorkflowRun, error) {
	st, err := h.starter(ctx, scope, runID)
	if err != nil {
		return store.WorkflowRun{}, err
	}
	if st == nil {
		return store.WorkflowRun{}, server.ErrWorkflowNodeTurnNotRunning
	}
	stopCtx, cancel := context.WithTimeout(ctx, h.stopBudget())
	defer cancel()
	ref, err := st.turnOf(stopCtx, nodeID)
	if err != nil {
		return store.WorkflowRun{}, err
	}
	if _, err := h.exec.Cancel(stopCtx, st.scope, ref); err != nil {
		return store.WorkflowRun{}, fmt.Errorf("%w: %w", server.ErrWorkflowRunStopUnconfirmed, err)
	}
	return h.store.WorkflowRun(ctx, scope.TenantID, runID)
}

// turnOf finds the turn nodeID is executing, marking the node stopping so the turn's end is
// recorded as the starter's stop, and waits out a start in flight.
func (st *runState) turnOf(ctx context.Context, nodeID string) (execution.Ref, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for {
		n, err := st.nodeOf(nodeID)
		if err != nil || n.phase != phaseTurn {
			return execution.Ref{}, server.ErrWorkflowNodeTurnNotRunning
		}
		var turn *attemptState
		for _, a := range st.attempts {
			if a.nodeRun == n.d.NodeRunID {
				turn = a
			}
		}
		if turn != nil && turn.starting != nil {
			starting := turn.starting
			st.mu.Unlock()
			select {
			case <-starting:
			case <-ctx.Done():
				st.mu.Lock()
				return execution.Ref{}, fmt.Errorf("%w: %w", server.ErrWorkflowRunStopUnconfirmed, ctx.Err())
			}
			st.mu.Lock()
			continue
		}
		if turn != nil && turn.ref != nil {
			n.stopping = true
			return *turn.ref, nil
		}
		// Before its registration, or between the turn's end and its park: nothing to stop.
		return execution.Ref{}, server.ErrWorkflowNodeTurnNotRunning
	}
}

// CompleteNode completes a parked interactive node for its starter (see completeNode).
func (h *Host) CompleteNode(ctx context.Context, scope server.WorkflowRunScope, runID, nodeID string, req server.WorkflowNodeCompletion) (store.WorkflowRun, error) {
	st, err := h.starter(ctx, scope, runID)
	if err != nil {
		return store.WorkflowRun{}, err
	}
	if st == nil {
		return store.WorkflowRun{}, server.ErrWorkflowNodeNotAwaitingInput
	}
	return h.completeNode(ctx, st, scope.TenantID, runID, nodeID, approval{
		confirmInitialTurn: req.ConfirmInitialTurn, force: req.Force, confirmSignal: req.ConfirmSignalID})
}

// autoAccept completes a node that opted into it, through the same gate as its starter, once a turn
// the starter opened parked with the Agent's report and nothing else in the way (completion signal
// ADR D4). The opening turn and an abnormal park are never accepted this way. A refusal, such as a
// missing deliverable or a message the starter sent first, leaves the node to its starter.
func (h *Host) autoAccept(st *runState, n *interactiveNode) {
	st.mu.Lock()
	snap := n.snapshot()
	st.mu.Unlock()
	if !n.cfg.AutoAcceptAgentCompletion || n.policy != workflow.CompletionSignalAgent || snap.AgentSignal == nil ||
		snap.TurnOrigin != originHuman || len(snap.Blockers) != 1 || snap.Blockers[0].Kind != blockerConfirmationPending {
		return
	}
	signal := snap.AgentSignal.SignalID
	// The caller still counts in workers; the completion must not hold it, as it waits for the
	// session's turn to finish cleaning up.
	h.workers.Add(1)
	go func() {
		defer h.workers.Done()
		if _, err := h.completeNode(h.ctx, st, st.scope.TenantID, n.d.RunID, n.d.Node.ID, approval{confirmSignal: signal, auto: true}); err != nil && h.ctx.Err() == nil {
			h.log.Warn("auto-accept of an interactive workflow node was refused; leaving it to its starter", "run", n.d.RunID, "node_run", n.d.NodeRunID, "error", err)
		}
	}()
}

// completeNode completes a parked interactive node through the completion gate (completion.go): the
// snapshot the node parked with must still describe its settled turn and the Agent's report, the
// live deliverable checks join its blockers, confirmInitialTurn lifts only the opening turn's
// turn_unconfirmed, a confirmed report its agent_confirmation_pending, and force covers every other
// blocker, leaving payload.completion_forced. It then ends the session, checks the snapshot again,
// writes the output once and schedules successors.
func (h *Host) completeNode(ctx context.Context, st *runState, tenant, runID, nodeID string, a approval) (store.WorkflowRun, error) {
	st.mu.Lock()
	n, err := st.nodeOf(nodeID)
	if err == nil && n.phase != phaseParked {
		err = server.ErrWorkflowNodeNotAwaitingInput
	}
	var snap completionSnapshot
	if err == nil {
		snap = n.snapshot()
		if !a.coherent(snap) {
			err = server.ErrWorkflowNodeCompletionInvalid
		}
	}
	if err != nil {
		st.mu.Unlock()
		return store.WorkflowRun{}, err
	}
	// The claim keeps any turn from settling until the completion commits or gives it back.
	n.phase = phaseCompleting
	files, baseline := n.cfg.CompletionFiles, n.baseline
	st.mu.Unlock()
	// release gives the claim back and republishes the summary, which may have changed while the
	// claim was held (the session ended); a row that is no longer pending is left as it is.
	release := func() {
		st.mu.Lock()
		n.phase = phaseParked
		st.mu.Unlock()
		h.refreshSummary(st, n, store.WorkflowStatusPending)
	}
	refuse := func(err error) (store.WorkflowRun, error) {
		release()
		return store.WorkflowRun{}, err
	}

	// The park recorded the turn the starter saw; a node whose turn or answer is no longer that
	// one refuses, force or not, and the page reloads the run.
	row, err := h.nodeRow(ctx, tenant, runID, n.d.NodeRunID)
	if err != nil {
		return refuse(err)
	}
	if err := stale(nodeID, storedSnapshot(row.Payload), snap); err != nil {
		return refuse(err)
	}
	if err := a.signalStands(nodeID, snap); err != nil {
		return refuse(err)
	}
	fileBlockers, warnings := h.fileBlockers(ctx, tenant, runID, files, baseline)
	st.mu.Lock()
	g, err := n.gate(snap, fileBlockers, a)
	gone := n.sessionGone
	st.mu.Unlock()
	if err != nil {
		return refuse(err)
	}

	// Desktop revalidates after shutting the session down: no turn can slip in between the
	// accepted answer and the output that names it. A session Node may still hold is ended
	// again, including after an earlier completion's End went unconfirmed: the retry confirms
	// the end before the node gives up its slot.
	h.tools.Revoke(n.d.NodeRunID)
	if !gone {
		stopCtx, cancel := context.WithTimeout(ctx, h.stopTimeout)
		err := h.exec.End(stopCtx, st.scope, n.session)
		cancel()
		if endConfirmed(err) {
			err = nil
		}
		// Confirmed or not, the execution service takes no further turn for a session it was
		// asked to end, so the node stops offering the conversation either way: a message sent
		// now would be accepted and then lost.
		// An unconfirmed End keeps the slot: Node may still hold the session.
		st.mu.Lock()
		n.sessionEnded = true
		if err == nil {
			n.sessionGone = true
			st.slotsMayHaveChanged()
		}
		st.mu.Unlock()
		if err != nil {
			release()
			if h.runStopped(context.WithoutCancel(ctx), runID) {
				// A sibling failed the run meanwhile and its sweep left this node to the
				// completion: close it now, or it would stay pending under the failed run.
				h.closeLost(st, n)
			}
			return store.WorkflowRun{}, fmt.Errorf("%w: %w", server.ErrWorkflowRunStopUnconfirmed, err)
		}
	}
	st.mu.Lock()
	after := n.snapshot()
	source := n.last
	if g.forced != nil && g.forced["output_source"] == outputLastNormal {
		source = n.accepted
	}
	st.mu.Unlock()
	if err := stale(nodeID, &snap, after); err != nil {
		return refuse(err)
	}
	var result execution.Result
	if source != nil {
		result = *source
	}
	snap.Warnings = dedupe(append(snap.Warnings, warnings...))
	keys := completionKeys(result, n.executor, n.cfg, n.task)
	keys["interactive"] = map[string]any{"turns": n.turns, "session_ended": true, "completable": true, "answer": g.answer, "completion": snap}
	g.record(keys, snap, a, h.now())
	if g.forced != nil {
		keys["completion_forced"] = g.forced
	}
	completion := workflow.Completion{Output: &g.answer, Structured: g.structured, Keys: keys}
	// The run outlives the request: a disconnecting client must not abandon the successor wave,
	// nor the reads that settle the claim once the completion may have committed.
	settle := context.WithoutCancel(ctx)
	engineErr := h.engine.CompleteNode(settle, runID, n.d.NodeRunID, completion)
	// The row decides who won, not the engine's error: the completion can commit and the successor
	// wave then meet a cancellation that landed in between, which is still this completion's win.
	status, err := h.nodeStatus(settle, tenant, runID, n.d.NodeRunID)
	if err != nil {
		release()
		return store.WorkflowRun{}, errors.Join(engineErr, err)
	}
	if status != store.WorkflowStatusSucceeded && status != store.WorkflowStatusCancelled && status != store.WorkflowStatusFailed {
		if h.runStopped(settle, runID) {
			// A sibling failed the run while the session was ending, and releaseOrphans left this
			// node to the completion: close its row as the orphan path does and drop the run.
			h.closeLost(st, n)
			return store.WorkflowRun{}, server.ErrWorkflowNodeNotAwaitingInput
		}
		release() // nothing committed; the starter can try again
		if engineErr == nil {
			engineErr = server.ErrWorkflowNodeNotAwaitingInput
		}
		return store.WorkflowRun{}, engineErr
	}
	st.mu.Lock()
	if st.interactive[nodeID] == n {
		delete(st.interactive, nodeID)
		st.slotsMayHaveChanged()
	}
	st.mu.Unlock()
	h.forgetFinished(runID, st)
	if status != store.WorkflowStatusSucceeded {
		// The run stopped running before the completion committed (it was cancelled or a sibling
		// failed it), so this completion did not win.
		return store.WorkflowRun{}, server.ErrWorkflowNodeNotAwaitingInput
	}
	if engineErr != nil {
		h.log.Warn("schedule after interactive completion", "run", runID, "node_run", n.d.NodeRunID, "error", engineErr)
	}
	return h.store.WorkflowRun(ctx, tenant, runID)
}

// runStopped reports whether the run is no longer running. A read error counts as running, so
// the node stays claimable and the starter can try again.
func (h *Host) runStopped(ctx context.Context, runID string) bool {
	c, err := h.store.WorkflowEngineContext(ctx, runID)
	return err == nil && c.Run.Status != workflow.StatusRunning
}

// closeLost forgets a node whose run stopped running without settling it, closes its row and
// drops the run's state once nothing else of it is in flight. Its session was ended, or lingers
// until a retry confirms the End (see linger).
func (h *Host) closeLost(st *runState, n *interactiveNode) {
	h.forgetInteractive(st, n) // its completion already tried to end the session
	h.closeOrphan(n)
	h.hub.Publish(n.d.RunID)
	h.forgetFinished(n.d.RunID, st)
}

// closeOrphan cancels n's attempt under its stopped run and reports whether the attempt is
// settled: false only while the run is running again (a resume from failure won the race), when
// the attempt is still the run's and must be left as it is.
func (h *Host) closeOrphan(n *interactiveNode) bool {
	_, err := h.store.CloseOrphanedWorkflowNode(h.ctx, n.d.RunID, n.d.NodeRunID)
	if errors.Is(err, store.ErrWorkflowRunActive) {
		return false
	}
	if err != nil {
		h.log.Error("close orphaned interactive workflow node", "run", n.d.RunID, "node_run", n.d.NodeRunID, "error", err)
	}
	return true
}

// nodeStatus reads one attempt's stored status.
func (h *Host) nodeStatus(ctx context.Context, tenant, runID, nodeRunID string) (string, error) {
	row, err := h.nodeRow(ctx, tenant, runID, nodeRunID)
	return row.Status, err
}

// nodeRow reads one attempt's stored row.
func (h *Host) nodeRow(ctx context.Context, tenant, runID, nodeRunID string) (store.WorkflowNodeRun, error) {
	nodes, err := h.store.WorkflowRunNodes(ctx, tenant, runID)
	if err != nil {
		return store.WorkflowNodeRun{}, err
	}
	for _, row := range nodes {
		if row.ID == nodeRunID {
			return row, nil
		}
	}
	return store.WorkflowNodeRun{}, store.ErrWorkflowNodeRunNotFound
}

// endParked ends the session of every tracked node for a run that is closing. Callers have marked
// the run cancelling, so no turn or completion can claim a node any more.
func (h *Host) endParked(ctx context.Context, st *runState) error {
	st.mu.Lock()
	sessions := make([]string, 0, len(st.interactive))
	for _, n := range st.interactive {
		h.tools.Revoke(n.d.NodeRunID)
		sessions = append(sessions, n.session)
	}
	st.mu.Unlock()
	for _, id := range sessions {
		if err := h.exec.End(ctx, st.scope, id); !endConfirmed(err) {
			return err
		}
	}
	return nil
}

// releaseOrphans closes the rows of nodes parked under a run that stopped running without a
// cancellation (a sibling failed it), which the failure left pending, and ends their sessions.
// Callers have read the run as no longer running, but a resume from failure can return it to
// running at any moment. So each row is closed first, in a transaction that refuses once the run
// runs again, and a node stays tracked until then: ResumeRun, which releases the orphans itself
// before it admits the resume, never finds the run's state empty while another caller is still
// about to end a session, and a node the resumed run kept is left alone with its session.
//
// A node with a turn in flight is left to that turn, like an automatic sibling that keeps running
// when another node fails the run: an executing turn finishes and its park is refused, which ends
// the session and closes the row; a turn not yet executing sees the stopped run and does not run.
func (h *Host) releaseOrphans(st *runState, runID string) {
	st.mu.Lock()
	inFlight := map[string]bool{}
	for _, a := range st.attempts {
		inFlight[a.nodeRun] = true
	}
	nodes := make([]*interactiveNode, 0, len(st.interactive))
	for _, n := range st.interactive {
		if n.phase == phaseCompleting || inFlight[n.d.NodeRunID] {
			continue // the completion or the turn settles it
		}
		nodes = append(nodes, n)
	}
	st.mu.Unlock()
	for _, n := range nodes {
		if !h.closeOrphan(n) {
			continue // the run was resumed meanwhile; the attempt is no longer an orphan
		}
		// End first and only then give the slot back, as park does: a resume releases the
		// orphans before it admits its fan-out, which must not be refused by Node at the
		// checkout's session limit while Node still holds this session.
		h.endInteractive(st, n)
		h.forgetInteractive(st, n)
		h.hub.Publish(runID)
	}
}
