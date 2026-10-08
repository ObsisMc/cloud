package core

import "fmt"

// The A-side takeover of a session execution's terminal Node event (controller-integration D2/D6,
// Thread D4, IssueRun D3/D4; control action "agent_session_takeover", served to Controllers by
// ExecutionService.TakeOverNodeEvent).
//
// A session execution's terminal event is *not* a Thread event and does not travel through
// TakeOverThreadEvents: approved controller-integration D2 says the terminal event still goes
// through TakeOverNodeEvent, and only after every Thread event before it has been taken over —
// otherwise CONFLICT. So this is the single-event sibling of agentThreadTakeover, on the clone
// result path, and the Thread batch path keeps its own action and its own batch bound.
//
// One terminal event is one caller-owned transaction. The global advisory lock of Store.transact
// serializes every takeover, so the continuity decision, the receipt, the business hook and the
// advance of last_event_sequence cannot interleave with another writer. The commit boundary is:
//
//	BEGIN (lease-validated, submission-wrapped)
//	  A: require the event to be the next sequence after last_event_sequence (or an exact replay)
//	  A: INSERT node_event_receipts                      [the only basis for an EventAck, D4]
//	  A: UPDATE node_executions.result                   [the durable terminal fact]
//	  B: AgentRunHooks.SessionEnded(...)                 [same transaction]
//	  A: UPDATE node_executions.last_event_sequence      [fenced advance]
//	COMMIT
//	  → only now may the Controller send EventAck for the event
//
// which is the mandate's own order (receipt → takeover → hook → advance). Every step is in one
// transaction, so no failure can leave a receipted terminal event whose Thread is not `ended`, an
// `ended` Thread whose queued turns are still `queued`, or an advanced sequence without the
// lifecycle the event authorized.

// threadTakeoverMaxBatch's single-event sibling has no batch bound: one terminal event is exactly
// one event, and a request carrying more is rejected by the gRPC layer's own shape (one sequence,
// one result, one event). There is deliberately no per-run event cap here either — see G-015.

// agentSessionEndReasons is the closed set of session end reasons the approved wire contract
// defines (proto AgentSessionEndReason, Thread D4 `reason`). Cloud stores and reasons about these
// five names; AGENT_SESSION_END_REASON_UNSPECIFIED is not a reason a Node may report, and an
// unknown name is refused rather than stored, so no unhandled reason can reach the IssueRun status
// that D4 derives from it.
var agentSessionEndReasons = map[string]bool{
	"user_ended":   true,
	"idle_timeout": true,
	"cancelled":    true,
	"agent_failed": true,
	"interrupted":  true,
}

// agentSessionEndedOutcome is the canonical outcome name of the session terminal result, kept in
// one place so the A-layer validation, the durable result object and the gRPC projection cannot
// drift apart.
const agentSessionEndedOutcome = "agent_session_ended"

// agentSessionTakeover commits one session execution's terminal Node event.
//
//	Settled facts:   the event's sequence is exactly last_event_sequence+1, or one already taken
//	                 over (an overlap/replay), never a jump past it (a gap).
//	Takeover:        receipt, the durable result, the B-owned session-terminal transition and the
//	                 sequence advance commit together or not at all.
//	Replay:          an event already taken over with an identical canonical event and an identical
//	                 result returns the recorded row and runs no hook, so a "committed but ack lost"
//	                 retry can never repeat the Thread terminal transition or its events.
//	Conflict:        the same sequence with a different canonical event, or a different result under
//	                 a recorded receipt, is refused and the original receipt is left untouched
//	                 (plan §4B.7, D-022).
//
// Errors use the repository's Fault contract: 400 for a body the contract cannot accept, 409 for a
// sequence gap, a wrong execution or a payload conflict, 404 for an execution Cloud never
// registered. A database or internal invariant failure panics databaseFailure and surfaces as
// UNAVAILABLE — "retry later" rather than "this event is wrong" — and the Node replays.
func agentSessionTakeover(t *transaction, r *ControlRequest) Object {
	executionID, operationID := r.Body.S("executionId"), r.Body.S("operationId")
	sequence, event := r.Body.N("sequence"), r.Body.O("event")
	result := r.Body.O("result")
	require(executionID != "" && len(executionID) <= 200, 400, "invalid_takeover")
	// Sequence 0 is not a session event sequence: last_event_sequence starts at 0 and every Node
	// event is 1-based, so a 0 here would claim continuity the Node never established. The canonical
	// event must be an object for the same reason the Thread path's is — it is the receipt's stored
	// value, and node_event_receipts.event is a jsonb object.
	require(sequence > 0 && len(event) > 0, 400, "invalid_receipt")

	e := t.one("SELECT * FROM node_executions WHERE execution_id=$1 AND operation_id=$2", executionID, operationID)
	require(e != nil, 404, "not_found")
	// Only a registered Agent-session execution ends a session. A delivery execution's terminal
	// event belongs to the delivery path (Phase 5 Batch 2) and is refused here rather than being
	// projected onto the Thread.
	require(e.S("kind") == "agent_session", 409, "takeover_conflict")
	// The terminal result is the one thing this action accepts: a non-terminal outcome reaching the
	// session terminal path is protocol misuse, refused before anything is written.
	require(result.S("outcome") == agentSessionEndedOutcome, 400, "invalid_result")
	require(agentSessionEndReasons[result.S("reason")], 400, "invalid_result")
	require(result.O("node").S("nodeId") == e.S("nodeId"), 409, "result_conflict")

	last := e.N("lastEventSequence")
	// Gap: the terminal event may only be taken over once every Thread event before it has been
	// taken over (controller-integration D2). Letting it through would commit a Thread `ended` for
	// a conversation Cloud has not finished writing.
	require(sequence <= last+1, 409, "sequence_gap")

	if sequence <= last {
		// Overlap: the recorded receipt and the recorded result are compared byte-for-byte, and
		// neither is rewritten. Identical is D2's/C5's "committed but ack lost" replay — a
		// deterministic no-op that runs no hook and advances nothing; different is a conflict that
		// leaves the original receipt intact.
		old := t.one("SELECT event FROM node_event_receipts WHERE execution_id=$1 AND sequence=$2", executionID, sequence)
		if old == nil {
			// last_event_sequence is only ever advanced after its receipts are inserted, so a
			// missing receipt below it is a corrupted invariant, not a client error.
			panic(databaseFailure{fmt.Errorf("session takeover: execution %s has last_event_sequence=%d without receipt %d", executionID, last, sequence)})
		}
		require(jsonText(old.O("event")) == jsonText(event), 409, "receipt_conflict")
		require(jsonText(e.O("result")) == jsonText(result), 409, "result_conflict")
		return e
	}

	// Receipt first: it is what authorizes the Controller's EventAck, and it is written inside this
	// transaction so a rolled-back takeover never acked anything.
	t.exec("INSERT INTO node_event_receipts(execution_id,sequence,event) VALUES($1,$2,$3)", executionID, sequence, jsonText(event))
	// The durable terminal fact. Written before the hook for the same reason the Thread batch writes
	// its receipts first: both are in the one transaction, and the hook re-reads every authoritative
	// row it uses, so the order between them is not observable.
	t.exec("UPDATE node_executions SET result=$2, updated_at=now() WHERE execution_id=$1", executionID, jsonText(result))
	// The business hook runs in this transaction. A non-nil error panics databaseFailure so the
	// receipt, the result, the Thread terminal transition, the discards, the delivering transition
	// and the released delivery work all roll back together and the Node replays the event.
	run := Object{"id": e.S("operationId")}
	h := t.hooks
	if h == nil {
		h = UnavailableAgentRunHooks{} // nil-safe fail closed
	}
	if err := h.SessionEnded(t, run, e, result); err != nil {
		panic(databaseFailure{err})
	}
	// Fenced advance: monotonic, and never a jump over a sequence whose receipt is missing. The
	// advisory lock makes the fence unconditional in practice; a 0 here means an external writer
	// moved the execution, which must abort rather than silently accept the event.
	if t.execRows(`UPDATE node_executions SET last_event_sequence=$2, updated_at=now() WHERE execution_id=$1 AND last_event_sequence=$3`,
		executionID, sequence, last) == 0 {
		panic(databaseFailure{fmt.Errorf("session takeover: execution %s last_event_sequence moved during the takeover transaction", executionID)})
	}
	return t.one("SELECT * FROM node_executions WHERE execution_id=$1", executionID)
}
