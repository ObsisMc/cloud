package workflowhost

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/server"
	"github.com/wanglongan587/cloud/v2-server/internal/workflow"
)

// The completion gate of an interactive node (desktop 22c5b83f
// crates/backend/src/workflow/run/completion_snapshot.rs and interactive/completion.rs; specs
// decisions/desktop/core/workflow/20261021-interactive-node-completion.md,
// 20261102-first-turn-confirmation.md, 20261023-interactive-deliverable-contract.md and
// 20261110-agent-completion-signal.md). Every park records a snapshot of the settled turn: its identity, the digest of its
// answer, who opened it, why it stopped, and the blockers that keep it from completing. A
// completion re-derives the snapshot, refuses one that changed, adds the live deliverable checks,
// and lets only an explicit confirmation lift the workflow's own opening turn and only force cover
// the other blockers, recording what force covered. Under the agent policy a turn that ended
// normally also waits for its Agent's report and then for a confirmation naming that very report.

// Blocker kinds, desktop's CompletionBlockerKind.
const (
	blockerTurnStopped     = "turn_stopped"
	blockerTurnTruncated   = "turn_truncated"
	blockerTurnRefused     = "turn_refused"
	blockerTurnUnknown     = "turn_unknown"
	blockerTurnUnconfirmed = "turn_unconfirmed"
	// blockerSignalPending: the Agent has not reported the step done in this turn.
	// blockerConfirmationPending: it has, and nobody confirmed the report.
	blockerSignalPending       = "agent_signal_pending"
	blockerConfirmationPending = "agent_confirmation_pending"
	// blockerStructuredInvalid: the answer does not satisfy the node's output schema.
	blockerStructuredInvalid = "structured_invalid"
	blockerFileMissing       = "file_missing"
	blockerFileUnchanged     = "file_unchanged"
)

// Turn origins: the workflow opens the first turn, the starter every later one.
const (
	originWorkflow = "workflow"
	originHuman    = "human"
)

// Stop causes, desktop's StopCause, set only for a turn that did not settle normally.
const (
	stopUserStopped   = "user_stopped"
	stopSessionLost   = "session_crashed"
	stopTimedOut      = "prompt_timed_out"
	stopCauseUnknown  = "unknown"
	parkAwaitingUser  = "awaiting_user"
	parkAbnormal      = "abnormal"
	warningFileCheck  = "file_check_unavailable"
	requesterHuman    = "human"
	requesterAgent    = "agent"
	confirmedByHuman  = "human"
	confirmedByAuto   = "auto"
	outputLastNormal  = "last_completed_turn"
	outputTurnWindow  = "turn_window"
	outputWithoutTurn = "none"
)

// completionBlocker is one reason a parked node cannot complete without force.
type completionBlocker struct {
	Kind   string         `json:"kind"`
	Params map[string]any `json:"params,omitempty"`
}

// turnKey identifies the settled turn the starter sees: UserSeq is the number of the message that
// opened it (0 for the workflow's opening turn), EndedSeq how many turns had settled with it.
type turnKey struct {
	UserSeq  int `json:"userSeq"`
	EndedSeq int `json:"endedSeq"`
}

// agentSignal is the completion an Agent reported in the turn TurnKey names.
type agentSignal struct {
	SignalID    string  `json:"signalId"`
	RequestedAt int64   `json:"requestedAt"`
	TurnKey     turnKey `json:"turnKey"`
}

// completionSnapshot is payload.interactive.completion, desktop's CompletionSnapshot.
type completionSnapshot struct {
	TurnKey      *turnKey            `json:"turnKey"`
	StopReason   string              `json:"stopReason,omitempty"`
	TurnOrigin   string              `json:"turnOrigin"`
	StopCause    string              `json:"stopCause,omitempty"`
	ParkKind     string              `json:"parkKind,omitempty"`
	Blockers     []completionBlocker `json:"blockers"`
	Warnings     []string            `json:"warnings"`
	OutputDigest string              `json:"outputDigest,omitempty"`
	// Policy is the policy the node's session runs under. Snapshots written before v2 applied the
	// agent policy all say end_turn.
	Policy string `json:"completionPolicy"`
	// AgentSignal is the Agent's report for TurnKey, absent without one.
	AgentSignal *agentSignal `json:"agentSignal,omitempty"`
	// AutoAccept says the node accepts a report on its own where it may (see Host.autoAccept).
	AutoAccept bool `json:"autoAcceptAgentCompletion,omitempty"`
}

// approval is what one completion request explicitly decided. confirmSignal is the ID of the report
// it confirms, and auto says the node's auto-accept, not the starter, confirms it.
type approval struct {
	confirmInitialTurn, force bool
	confirmSignal             string
	auto                      bool
}

// coherent refuses the one combination that means nothing (completion signal ADR D3): confirming
// the opening turn and forcing at once is only for an agent policy turn that ended normally without
// a report, where the confirmation lifts turn_unconfirmed and force covers the missing report.
func (a approval) coherent(s completionSnapshot) bool {
	if !a.force || !a.confirmInitialTurn {
		return true
	}
	return s.Policy == workflow.CompletionSignalAgent && s.AgentSignal == nil && s.blockedBy(blockerTurnUnconfirmed)
}

// signalStands refuses a confirmation of a report other than the one of the settled turn; force
// never covers it. Under end_turn there is no report, so a confirmation of one is ignored.
func (a approval) signalStands(nodeID string, s completionSnapshot) error {
	if a.confirmSignal == "" || s.Policy != workflow.CompletionSignalAgent {
		return nil
	}
	if s.AgentSignal == nil || s.AgentSignal.SignalID != a.confirmSignal {
		return &server.WorkflowNodeSignalChanged{NodeID: nodeID, Expected: s.signalID(), Actual: a.confirmSignal}
	}
	return nil
}

func (s completionSnapshot) blockedBy(kind string) bool {
	for _, b := range s.Blockers {
		if b.Kind == kind {
			return true
		}
	}
	return false
}

func (s completionSnapshot) signalID() string {
	if s.AgentSignal == nil {
		return ""
	}
	return s.AgentSignal.SignalID
}

// precise reports the blockers that describe how the turn itself ended.
func precise(kind string) bool {
	switch kind {
	case blockerTurnStopped, blockerTurnTruncated, blockerTurnRefused, blockerTurnUnknown, blockerStructuredInvalid:
		return true
	}
	return false
}

// turnBlocker classifies how a settled turn ended, with the stop cause desktop records for it.
// stopped says the starter asked to stop the turn.
func turnBlocker(r execution.Result, stopped bool) (kind, cause string) {
	switch {
	case endedNormally(r):
		return "", ""
	case r.StopReason == "max_tokens" || r.StopReason == "max_turn_requests":
		return blockerTurnTruncated, ""
	case r.StopReason == "refusal":
		return blockerTurnRefused, ""
	case stopped:
		return blockerTurnStopped, stopUserStopped
	case r.AgentLost:
		return blockerTurnUnknown, stopSessionLost
	case r.FailureCode == "model_max_duration":
		// The model call ran into its time limit and the turn was cut.
		return blockerTurnStopped, stopTimedOut
	case r.Status == execution.StatusCancelled || r.StopReason == "cancelled":
		return blockerTurnStopped, stopCauseUnknown
	}
	return blockerTurnUnknown, stopCauseUnknown
}

// answerDigest is desktop's outputDigest of a final answer.
func answerDigest(answer string) string {
	sum := sha256.Sum256([]byte(answer))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// snapshot derives the completion snapshot of n's settled turn. Callers hold st.mu.
func (n *interactiveNode) snapshot() completionSnapshot {
	s := completionSnapshot{TurnOrigin: n.lastOrigin, Policy: workflow.CompletionSignalEndTurn, Blockers: []completionBlocker{}, Warnings: []string{}}
	if n.policy == workflow.CompletionSignalAgent {
		s.Policy, s.AutoAccept = n.policy, n.cfg.AutoAcceptAgentCompletion
	}
	if n.baseline != nil && n.baseline.unavailable() {
		s.Warnings = append(s.Warnings, warningFileCheck)
	}
	if n.last == nil || n.unsettled {
		// No turn settled: its result could not be read, or it never ran.
		if n.last == nil {
			s.TurnOrigin = originWorkflow
		}
		s.Blockers = append(s.Blockers, completionBlocker{Kind: blockerTurnUnknown})
		s.StopCause = stopCauseUnknown
		if n.lastStopCause != "" {
			s.StopCause = n.lastStopCause
		}
		s.ParkKind = parkKind(s.Blockers, s.StopCause)
		return s
	}
	r := *n.last
	s.TurnKey = &turnKey{UserSeq: n.lastSeq, EndedSeq: n.turns}
	s.StopReason = r.StopReason
	s.OutputDigest = answerDigest(r.Answer)
	if kind, cause := turnBlocker(r, n.lastStopCause == stopUserStopped); kind != "" {
		s.Blockers = append(s.Blockers, completionBlocker{Kind: kind})
		s.StopCause = cause
	}
	// A turn the Agent itself ended has an answer to judge; a lost one has only a fragment.
	if n.cfg.Structured != nil && !r.AgentLost && r.StopReason != "" {
		if _, err := workflow.ParseStructuredOutput(r.Answer, n.cfg.Structured); err != nil {
			s.Blockers = append(s.Blockers, completionBlocker{Kind: blockerStructuredInvalid, Params: map[string]any{"message": err.Error()}})
		}
	}
	exact := len(s.Blockers) != 0
	// The workflow's own opening turn never completes the step by itself: its end_turn says the
	// reply finished, which may have been a question. A more precise blocker names the reason
	// instead (first-turn ADR D1).
	if n.lastOrigin == originWorkflow && !exact {
		s.Blockers = append(s.Blockers, completionBlocker{Kind: blockerTurnUnconfirmed})
	}
	if n.signal != nil && n.signal.TurnKey == *s.TurnKey {
		signal := *n.signal
		s.AgentSignal = &signal
	}
	// A turn that did not end normally already says why it blocks; the report changes nothing.
	if s.Policy == workflow.CompletionSignalAgent && !exact {
		kind := blockerSignalPending
		if s.AgentSignal != nil {
			kind = blockerConfirmationPending
		}
		s.Blockers = append(s.Blockers, completionBlocker{Kind: kind})
	}
	s.ParkKind = parkKind(s.Blockers, s.StopCause)
	return s
}

// participation reports the blockers that only wait for the Agent's report or the starter's word on
// a turn that ended normally.
func participation(kind string) bool {
	return kind == blockerTurnUnconfirmed || kind == blockerSignalPending || kind == blockerConfirmationPending
}

// parkKind is desktop's park_kind: awaiting_user for the starter's participation (an unconfirmed
// opening turn, a report the Agent owes or they have to confirm, their own stop), abnormal for everything else that blocks, absent when ready.
func parkKind(blockers []completionBlocker, cause string) string {
	switch {
	case len(blockers) == 0:
		return ""
	case !slices.ContainsFunc(blockers, func(b completionBlocker) bool { return !participation(b.Kind) }):
		return parkAwaitingUser
	case len(blockers) == 1 && blockers[0].Kind == blockerTurnStopped && cause == stopUserStopped:
		return parkAwaitingUser
	}
	return parkAbnormal
}

// gated is a completion the gate admitted: what it writes and whether force covered blockers.
type gated struct {
	answer     string
	structured any
	// forced is the audit payload.completion_forced records, nil for a normal completion.
	forced map[string]any
}

// record writes who completed the node beside the output (completion signal ADR D6): the Agent
// when the settled turn carries its report, whoever pressed the button otherwise, and the
// confirmation of the report, null when force replaced it. The end_turn policy records only the
// requester, as before.
func (g gated) record(keys map[string]any, s completionSnapshot, a approval, now time.Time) {
	keys["requester"] = requesterHuman
	if s.Policy != workflow.CompletionSignalAgent {
		return
	}
	keys["completionPolicy"], keys["agentSignal"], keys["confirmation"] = s.Policy, nil, nil
	if s.AgentSignal == nil {
		return
	}
	keys["requester"], keys["agentSignal"] = requesterAgent, s.AgentSignal
	if a.confirmSignal != "" {
		by := confirmedByHuman
		if a.auto {
			by = confirmedByAuto
		}
		keys["confirmation"] = map[string]any{"by": by, "at": now.UnixMilli()}
	}
}

// gate decides a completion of snapshot s plus the live deliverable blockers. Callers hold st.mu.
// Only confirmInitialTurn lifts turn_unconfirmed, only a confirmation of the report (already
// checked by signalStands) agent_confirmation_pending, and force covers every other blocker but
// never turn_unconfirmed (first-turn ADR D2).
func (n *interactiveNode) gate(s completionSnapshot, files []completionBlocker, a approval) (gated, error) {
	if !a.coherent(s) {
		return gated{}, server.ErrWorkflowNodeCompletionInvalid
	}
	effective := append(append([]completionBlocker{}, s.Blockers...), files...)
	var unfulfilled []completionBlocker
	for _, b := range effective {
		if a.confirmInitialTurn && b.Kind == blockerTurnUnconfirmed {
			continue
		}
		if a.confirmSignal != "" && b.Kind == blockerConfirmationPending {
			continue
		}
		unfulfilled = append(unfulfilled, b)
	}
	if len(unfulfilled) == 0 {
		g := gated{answer: n.last.Answer}
		if n.cfg.Structured != nil {
			// No structured_invalid blocker, so the answer parses.
			g.structured, _ = workflow.ParseStructuredOutput(g.answer, n.cfg.Structured)
		}
		return g, nil
	}
	for _, b := range unfulfilled {
		if !a.force || b.Kind == blockerTurnUnconfirmed {
			return gated{}, incomplete(unfulfilled)
		}
	}
	// Forced. While the session is open the starter could have continued, so the current turn's
	// answer is taken as desktop takes it. Once the session ended (v2 stops a turn only by ending
	// the session, until #1111), nothing can improve on the last answer that ended normally, so
	// that one is taken when there is one; the audit says which.
	g := gated{}
	source := outputWithoutTurn
	switch {
	case n.sessionEnded && n.accepted != nil:
		g.answer, source = n.accepted.Answer, outputLastNormal
	case n.last != nil && !n.unsettled:
		g.answer, source = n.last.Answer, outputTurnWindow
	}
	if n.cfg.Structured != nil {
		if value, err := workflow.ParseStructuredOutput(g.answer, n.cfg.Structured); err == nil {
			g.structured = value
		}
	}
	g.forced = map[string]any{
		"by": requesterHuman, "blockers": unfulfilled, "stop_cause": nullable(s.StopCause),
		"without_turn": n.last == nil || n.unsettled, "output_source": source,
	}
	return g, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// incomplete is the refusal listing the blockers the request left unresolved.
func incomplete(blockers []completionBlocker) error {
	public := make([]server.WorkflowNodeBlocker, len(blockers))
	for i, b := range blockers {
		public[i] = server.WorkflowNodeBlocker{Kind: b.Kind}
		if path, ok := b.Params["path"].(string); ok {
			public[i].Path = path
		}
		if message, ok := b.Params["message"].(string); ok {
			public[i].Message = message
		}
	}
	return &server.WorkflowNodeIncomplete{Blockers: public}
}

// storedSnapshot reads the snapshot the last park wrote, nil for a node that parked before
// snapshots existed (or never parked).
func storedSnapshot(payload []byte) *completionSnapshot {
	var p struct {
		Interactive struct {
			Completion *completionSnapshot `json:"completion"`
		} `json:"interactive"`
	}
	if json.Unmarshal(payload, &p) != nil {
		return nil
	}
	return p.Interactive.Completion
}

// stale refuses a completion whose settled turn, answer or Agent's report is no longer the one the last park
// recorded (completion ADR D5): force never covers it.
func stale(nodeID string, stored *completionSnapshot, live completionSnapshot) error {
	if stored == nil || stored.TurnKey == nil {
		return nil
	}
	if live.TurnKey == nil || *live.TurnKey != *stored.TurnKey {
		var actual *server.WorkflowNodeTurnKey
		if live.TurnKey != nil {
			actual = &server.WorkflowNodeTurnKey{UserSeq: live.TurnKey.UserSeq, EndedSeq: live.TurnKey.EndedSeq}
		}
		return &server.WorkflowNodeTurnChanged{NodeID: nodeID,
			Expected: &server.WorkflowNodeTurnKey{UserSeq: stored.TurnKey.UserSeq, EndedSeq: stored.TurnKey.EndedSeq}, Actual: actual}
	}
	if stored.OutputDigest != "" && stored.OutputDigest != live.OutputDigest {
		return &server.WorkflowNodeOutputChanged{NodeID: nodeID, Expected: stored.OutputDigest, Actual: live.OutputDigest}
	}
	if stored.signalID() != live.signalID() {
		return &server.WorkflowNodeSignalChanged{NodeID: nodeID, Expected: stored.signalID(), Actual: live.signalID()}
	}
	return nil
}
