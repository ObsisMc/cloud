//go:build linux

package agentruntime

import (
	"encoding/json"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/node"
	"github.com/wanglongan587/cloud/v2-server/internal/nodehistory"
)

// defaultTurnCancelWait is how long Run waits for TurnCancelSettled after sending CancelTurn, or
// after the last message of the session that arrived meanwhile, before it ends the session as a
// stop did before turn_cancel. Node settles within G + F + Q + R of the cancel (cancel-turn ADR
// D2): the Agent's answer, the turn process fence, the quiet window and its re-check, 15 s + 5 s +
// 2 s + 1 s = 23 s, below this window. Restarting the window on every message approximates ADR
// D4's rule of waiting on delivery progress, so a backlog of events is never taken for a Node that
// does not settle. The whole wait never exceeds execution.TurnStopSettleLimit.
const defaultTurnCancelWait = 30 * time.Second

// turnStop is what stopTurn did with a caller's stop.
type turnStop int

const (
	// turnStopSent: CancelTurn was sent; Node's settlement completes the turn.
	turnStopSent turnStop = iota
	// turnStopFinished: the turn already has its outcome on call.done: it ended on its own, or
	// Node never held it and it was stopped here.
	turnStopFinished
	// turnStopUnavailable: this session cannot keep its Agent after a stop (it did not choose
	// keep_session_on_cancel, or it is ending or broken); the caller ends the session.
	turnStopUnavailable
)

// stopTurn asks Node to cancel only call's turn. The command is sent under commandMu, after the
// turn's own submission, so Node has accepted the turn by the time it reads the cancel. A
// submission Node refused and that was not sent again is no turn of Node's (CancelTurn would
// only learn unknown, ADR D1), so it is stopped here, before its resubmission, and the session
// is kept: nothing of it reached the Agent.
func (a *agentSession) stopTurn(call *activeTurn) turnStop {
	a.commandMu.Lock()
	defer a.commandMu.Unlock()
	a.mu.Lock()
	if a.active != call {
		a.mu.Unlock()
		// complete clears active before it hands the outcome over, so it is on its way.
		return turnStopFinished
	}
	if !a.keep.onCancel || !a.started || a.ended || a.ending || a.broken || call.cancel != "" {
		a.mu.Unlock()
		return turnStopUnavailable
	}
	if call.refused {
		a.mu.Unlock()
		result := execution.Result{Status: execution.StatusCancelled, StopReason: "cancelled", SessionKept: true, NotDelivered: true}
		if a.publish(execution.Event{Ref: execution.Ref{TurnID: call.id}, Kind: execution.EventTurnEnded, Result: &result}) != nil {
			a.fail("agent_event_write_failed", errEventStore)
			a.requestEnd()
			return turnStopFinished
		}
		a.complete(turnOutcome{call.id, result, nil})
		return turnStopFinished
	}
	call.cancel = "cancel-" + digest(call.id)
	command := call.cancel
	a.mu.Unlock()
	if a.project.send("cancel_turn", a.execution, map[string]string{"node_id": a.project.nodeID(), "command_id": command, "turn_id": call.id}) != nil {
		// The command may or may not have arrived; ending the session settles either way.
		return turnStopUnavailable
	}
	return turnStopSent
}

// observeCancelSettled handles TurnCancelSettled, the one settlement Node writes for every
// accepted CancelTurn. A settlement for a turn that already ended on its own is acknowledged and
// ignored: the turn has its result, and the cancel changed nothing.
func (a *agentSession) observeCancelSettled(message node.Message, call *activeTurn, publicationFailed bool) (*turnOutcome, bool) {
	var settled struct {
		CommandID string `json:"command_id"`
		TurnID    string `json:"turn_id"`
		Outcome   string `json:"outcome"`
	}
	if json.Unmarshal(message.Payload, &settled) != nil || message.Sequence == nil || *message.Sequence == 0 || settled.CommandID == "" || settled.TurnID == "" {
		a.fail("agent_turn_cancel_invalid", execution.ErrUnavailable)
		a.requestEnd()
		return nil, false
	}
	a.mu.Lock()
	ours := call != nil && call.cancel == settled.CommandID && call.id == settled.TurnID
	var stopped *nodehistory.Turn
	var fallingBack bool
	if ours {
		stopped, fallingBack = call.stopped, call.fallingBack
	}
	a.mu.Unlock()
	if !ours {
		return nil, !publicationFailed
	}
	result, ended, ok := a.settleCancel(call, settled.Outcome, stopped)
	if !ok {
		call.fallBack()
		return nil, !publicationFailed
	}
	if fallingBack {
		// Run stopped waiting and is ending the session: whatever Node concluded, the session
		// does not outlive this turn.
		result.SessionKept, result.AgentLost, ended = false, true, true
	}
	if publicationFailed || a.publish(execution.Event{Ref: execution.Ref{TurnID: call.id}, Kind: execution.EventTurnEnded, Result: &result}) != nil {
		a.fail("agent_event_write_failed", errEventStore)
		a.requestEnd()
		return nil, false
	}
	var err error
	if ended {
		// As for any session that ends under a turn: nobody may keep it for another turn.
		err = execution.ErrSessionEnded
	}
	return &turnOutcome{call.id, result, err}, true
}

// settleCancel turns Node's settlement of the caller's stop into the turn's result; ended means
// Node is ending the session. ok is false for a settlement a kept session cannot rely on, so the
// caller ends the session instead.
//
// The caller asked for this stop, so a turn that ended cancelled because of it stays cancelled
// even when its last model call had been cut at a limit (model-proxy ADR D4: only a turn the user
// did not stop fails with the cut). A turn that ended on its own before the cancel took effect
// keeps the result it earned, cut included.
func (a *agentSession) settleCancel(call *activeTurn, outcome string, stopped *nodehistory.Turn) (result execution.Result, ended, ok bool) {
	switch outcome {
	case "stopped":
		// The turn stopped because of this cancel and Node keeps the session for the next turn.
		if stopped == nil {
			return execution.Result{}, false, false // Node settles a stopped turn after its TurnEnded
		}
		result = turnResult(stopped, "")
		result.SessionKept = true
		return result, false, true
	case "not_started":
		// Node dropped the turn before its Agent received it; the session is untouched.
		return execution.Result{Status: execution.StatusCancelled, StopReason: "cancelled", SessionKept: true, NotDelivered: true}, false, true
	case "already_ended":
		if stopped == nil {
			return execution.Result{}, false, false
		}
		return turnResult(stopped, call.guard.failure()), false, true
	case "session_ending":
		// Node will not keep the session: it stops the Agent and sends the terminal once it has
		// the stop proof. The stop still is the user's, whatever cut the turn saw before.
		result = execution.Result{Status: execution.StatusCancelled, StopReason: "cancelled", AgentLost: true}
		if stopped != nil {
			result.Answer = stopped.Answer
		}
		a.mu.Lock()
		// Node is ending the session on its own; End waits for its terminal, without EndSession.
		a.ending = true
		a.mu.Unlock()
		return result, true, true
	default:
		// unknown: Node never accepted the turn this session is running (an MCP refresh still
		// holds its submission, for one). It may run yet, so only ending the session is safe.
		return execution.Result{}, false, false
	}
}
