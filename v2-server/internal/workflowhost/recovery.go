package workflowhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/store"
	"github.com/wanglongan587/cloud/v2-server/internal/workflow"
)

// Desktop keeps a valid interactive wait across a restart (docs/workflows/workflow-runs.md
// "Recovery after restart"; crates/backend/src/workflow/run/engine.rs is_awaiting_input): a node
// parked awaiting input stays parked, and the starter's next message continues it. Here the
// parked row and its held session survive the restart sweep (workflowInterrupts and
// store.RecoverExecutions), the node's process-local state is rebuilt from what was persisted
// (recoverParked), and the execution service starts a new Agent for the session on its next turn,
// handing it the recorded conversation. Nothing is replayed: a node whose turn was executing at
// the restart is running, not parked, and fails with its run.

// parkedInteractive reports whether a row left by the previous process is a valid interactive
// wait, desktop's is_awaiting_input: a pending root Agent attempt with a session whose frozen node
// is interactive, and which parked with a session summary (only an interactive node writes one).
func parkedInteractive(g *workflow.Graph, row store.WorkflowNodeRun) bool {
	if row.Status != store.WorkflowStatusPending || row.NodeType != "agent" || row.SessionID == nil || row.Iteration != nil {
		return false
	}
	n, ok := g.Node(row.NodeID)
	if !ok || n.Kind != "agent" || n.RegionOwner != "" || !n.Agent().Interactive {
		return false
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(row.Payload, &payload) != nil {
		return false
	}
	_, summarized := payload["interactive"]
	return summarized
}

// recoverParked rebuilds the process-local state of the run's parked interactive nodes, so the
// starter can send a turn, stop it or complete the node again. A node whose executor the catalog
// no longer offers, or whose session is gone, cannot continue and fails like a lost session; a
// catalog or session that cannot be read aborts startup instead (see recoveredExecutor). A start
// without an Agent runtime offers no executor at all, which says nothing about the node's: the
// node fails as interrupted by the restart instead, which a resume from the failure continues.
func (h *Host) recoverParked(ctx context.Context, st *runState, run store.WorkflowEngineRun) error {
	g, err := workflow.ParseGraph(run.Graph)
	if err != nil {
		return nil // the sweep kept nothing of a graph it cannot read
	}
	rows, err := h.store.WorkflowEngineNodeRuns(ctx, run.Run.ID)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if !parkedInteractive(g, row) {
			continue
		}
		node, _ := g.Node(row.NodeID)
		d := workflow.AgentDispatch{RunID: run.Run.ID, NodeRunID: row.ID, ProjectID: run.Run.ProjectID, Node: node, Graph: g}
		n, err := h.parkedNode(ctx, st, d, row)
		if err != nil {
			return err
		}
		if n == nil {
			continue
		}
		// A recovered node holds one of the run's session slots from now on, although its new
		// Agent starts only with its next turn: a conservative count, as the slot is taken back
		// then anyway.
		st.track(n)
		// The row's summary was written before the restart and may lag the session: a node being
		// completed then had its session ended without the summary saying so, and the page would
		// offer a message box its session refuses. Write it again from the rebuilt node; a write
		// that fails aborts startup like a session that cannot be read, rather than serving a
		// summary the node contradicts.
		if err := h.writeSummary(ctx, st, n, store.WorkflowStatusPending); err != nil {
			return err
		}
		h.hub.Publish(run.Run.ID)
		if n.sessionEnded && !n.sessionGone {
			// The node's session ended before the restart (a stop, a lost Agent) but its End was
			// never confirmed, so nothing retries it and it would keep its slot until the node
			// completes. The new process's Node does not hold it: End confirms it at once, unless
			// the end cannot be written, and is then retried like any unconfirmed End.
			h.endInteractive(st, n)
		}
	}
	return nil
}

// parkedNode rebuilds one parked node from its row and its session's recorded turns, or fails
// the node and returns nil when it cannot continue.
func (h *Host) parkedNode(ctx context.Context, st *runState, d workflow.AgentDispatch, row store.WorkflowNodeRun) (*interactiveNode, error) {
	if h.noAgentRuntime {
		return nil, h.failParked(st, d, *row.SessionID, workflow.Failure{Kind: workflow.FailureInterruptedByRestart,
			Message: "this start has no Agent runtime, so the node's conversation is not kept"})
	}
	cfg := d.Node.Agent()
	executor, failure, err := h.recoveredExecutor(ctx, st.scope, cfg)
	if err != nil {
		return nil, fmt.Errorf("resolve the executor of node run %s: %w", row.ID, err)
	}
	if failure != nil {
		return nil, h.failParked(st, d, *row.SessionID, failure.Interactive())
	}
	var payload struct {
		Task        string `json:"task"`
		Interactive struct {
			SessionEnded  bool                `json:"session_ended"`
			Sent          int                 `json:"sent"`
			LastTurnError string              `json:"last_turn_error"`
			Unconnected   bool                `json:"completion_tool_unconnected"`
			Completion    *completionSnapshot `json:"completion"`
		} `json:"interactive"`
		Baseline *fileBaseline `json:"completion_baseline"`
	}
	if err := json.Unmarshal(row.Payload, &payload); err != nil {
		return nil, fmt.Errorf("read the payload of node run %s: %w", row.ID, err)
	}
	n := &interactiveNode{d: d, executor: executor, cfg: cfg, task: payload.Task, session: *row.SessionID, phase: phaseParked,
		lastOrigin: originWorkflow, baseline: payload.Baseline}
	// The policy stays the one the node parked under: a node that parked before v2 applied the
	// agent policy has no Agent that was given the tool, and completes as end_turn (default
	// end_turn ADR D4). The report of the settled turn is kept only if the turns rebuilt below
	// still end with that turn; the credentials its Agent had died with the old process.
	n.policy = workflow.CompletionSignalEndTurn
	if c := payload.Interactive.Completion; c != nil && c.Policy == workflow.CompletionSignalAgent {
		n.policy, n.signal = c.Policy, c.AgentSignal
	}
	// Only the park knew that the starter stopped the turn (or that its result was lost); the
	// turns themselves do not say so.
	if c := payload.Interactive.Completion; c != nil && (c.StopCause == stopUserStopped || c.StopCause == stopSessionLost) {
		n.lastStopCause = c.StopCause
	}
	// A turn whose result was never read has no recorded turn either; the park said so.
	unsettled := payload.Interactive.Completion != nil && payload.Interactive.Completion.TurnKey == nil
	session, err := h.exec.ReadSession(ctx, st.scope, n.session)
	if errors.Is(err, execution.ErrNotFound) {
		return nil, h.failParked(st, d, n.session, workflow.Failure{Kind: workflow.FailureInterruptedByRestart, Message: "the node's session no longer exists after the restart"}.Interactive())
	}
	if err != nil {
		return nil, fmt.Errorf("read the session of node run %s: %w", row.ID, err)
	}
	// A session that ended before the restart (the starter stopped it, or its Agent was lost)
	// still lets the node complete: force takes the last normal answer, as completion.go says.
	n.sessionGone = session.EndedAt != nil
	n.sessionEnded = n.sessionGone || payload.Interactive.SessionEnded
	// The summary is the only record of a message the execution service refused at Append: it
	// left no turn, so the turns below neither count its number nor tell that it was lost. Keep
	// both, or rewriting the summary after the recovery would hide the notice and reuse its number.
	n.undelivered = payload.Interactive.LastTurnError == turnErrorNotDelivered
	// The Agent it was said of is gone; the next one is judged when it first parks.
	n.toolUnconnected = payload.Interactive.Unconnected
	n.seq = payload.Interactive.Sent
	prefix := d.NodeRunID + "/turn-"
	for cursor := ""; ; {
		page, err := h.exec.ReadTurns(ctx, st.scope, n.session, cursor, 100)
		if err != nil {
			return nil, fmt.Errorf("read the turns of node run %s: %w", row.ID, err)
		}
		for _, turn := range page.Items {
			// A refused follow-up consumed its number too, so the next one never reuses it.
			if seq, err := strconv.Atoi(strings.TrimPrefix(turn.Caller.AttemptID, prefix)); err == nil && strings.HasPrefix(turn.Caller.AttemptID, prefix) && seq > n.seq {
				n.seq = seq
			}
			if turn.Result == nil || undeliveredFollowUp(turn, prefix) {
				continue
			}
			if strings.HasPrefix(turn.Caller.AttemptID, prefix) && stoppedBeforeDelivery(*turn.Result) {
				n.turns++ // counted, as when it settled, but it answered nothing (see settleInteractiveTurn)
				continue
			}
			result := *turn.Result
			n.turns++
			n.last = &result
			n.lastSeq, n.lastOrigin = 0, originWorkflow
			if seq, err := strconv.Atoi(strings.TrimPrefix(turn.Caller.AttemptID, prefix)); err == nil && strings.HasPrefix(turn.Caller.AttemptID, prefix) {
				n.lastSeq, n.lastOrigin = seq, originHuman
			}
			if endedNormally(result) {
				n.accepted = &result
			}
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	n.unsettled = unsettled && n.last != nil
	return n, nil
}

// undeliveredFollowUp reports whether turn is a starter's message the Agent never got: the
// execution service recorded it, but it was no turn of the conversation, and the summary's
// last_turn_error says it was lost.
func undeliveredFollowUp(turn execution.Turn, prefix string) bool {
	return strings.HasPrefix(turn.Caller.AttemptID, prefix) && !delivered(*turn.Result) && !stoppedBeforeDelivery(*turn.Result)
}

// failParked fails a parked node that cannot continue after the restart and ends its session.
// The engine fails only an executing attempt, so the row moves back to running first, as a turn
// would move it. Like any session no node tracks, it keeps a slot until its End is confirmed.
// The new process's Node never held it, so only the execution service's write of the end can
// leave it unconfirmed, and the End is retried while the run runs (see retryEnd).
func (h *Host) failParked(st *runState, d workflow.AgentDispatch, session string, f workflow.Failure) error {
	moved, err := h.store.MoveWorkflowEngineNode(h.ctx, d.RunID, d.NodeRunID, store.WorkflowStatusPending, store.WorkflowStatusRunning,
		func(node []byte) ([]byte, error) { return node, nil })
	if err != nil {
		return fmt.Errorf("fail node run %s: %w", d.NodeRunID, err)
	}
	st.mu.Lock()
	lingering := st.linger(session)
	st.mu.Unlock()
	if lingering {
		h.endLingering(st, d.RunID, session)
	}
	if moved {
		h.fail(d, f)
	}
	return nil
}

// endUnparkedSessions ends the sessions execution recovery kept for nodes that turned out not to
// wait any more: the sweep failed them or their run. runID, when not empty, limits it to one run.
//
// Unlike a session a run's state tracks, an unconfirmed End here is only logged and not retried
// (see retryEnd): it runs at startup, before any session starts, so the new process's Node never
// held these sessions and only the execution service's write of the end can fail. Their runs
// failed and start no session that a slot would guard, and the next start's execution recovery
// ends whatever is left open, as their rows no longer wait.
func (h *Host) endUnparkedSessions(ctx context.Context, runID string) error {
	sessions, err := h.store.UnparkedWorkflowSessions(ctx, runID)
	if err != nil {
		return err
	}
	for _, s := range sessions {
		stopCtx, cancel := context.WithTimeout(ctx, h.stopTimeout)
		err := h.exec.End(stopCtx, execution.Scope{TenantID: s.TenantID, UserID: s.UserID}, s.SessionID)
		cancel()
		if err != nil {
			h.log.Warn("end the session of a node that no longer waits after the restart", "session", s.SessionID, "error", publicReason(err))
		}
	}
	return nil
}
