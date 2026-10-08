package core

import "fmt"

// The A-side takeover of a session execution's non-terminal Thread events
// (controller-integration D2/D6, Node protocol D2, plan §4B.6/§4B.7; control action
// "agent_thread_takeover", served to Controllers by AgentRunService.TakeOverThreadEvents).
//
// One batch is one caller-owned transaction: the global advisory lock of Store.transact serializes
// every takeover, so the continuity decision, the receipts, the business hook and the advance of
// last_event_sequence cannot interleave with another writer. The commit boundary is exactly:
//
//	BEGIN (lease-validated, submission-wrapped)
//	  A: classify the batch against node_executions.last_event_sequence / existing receipts
//	  A: INSERT node_event_receipts for the first-taken (contiguous) suffix   [receipt = the only
//	     basis for an EventAck, protocol root D4]
//	  B: AgentRunHooks.ThreadEventsTakenOver(first-taken events)             [same transaction]
//	  A: UPDATE node_executions.last_event_sequence = highest taken over
//	COMMIT
//	  → only now may the Controller send EventAck for the batch
//
// The mandate's §12 order (receipts → hook → advance) is used; the plan's §4B.7 sketch advances
// last_event_sequence before the hook. Both run in one transaction, and the hook re-reads every
// authoritative row it uses and never reads last_event_sequence, so the two orders are
// observationally identical; atomicity — all of it commits or none of it does — is what matters.
//
// The batch classification lives here and never in the hook (§4B.6/§4B.15 item 9): the hook only
// ever sees the first-taken, contiguous suffix, which is also why a replay can never re-run a
// side-effecting business hook (D6).

// threadTakeoverMaxBatch bounds one batch. The bound is the approved wire contract's own
// (controller-integration D2: "一次最多 64 个事件"), not an invented per-run cap: the per-run event
// cap and the receipt retention of plan §4B.10/D-025 are deliberately NOT implemented in Phase 4B
// (they have no approved ADR value yet; G-015 stays PARTIAL).
const threadTakeoverMaxBatch = 64

// agentThreadTakeover classifies and commits one Thread event batch. A batch is either accepted as
// a whole or the transaction is aborted; there is no partial batch.
//
//	Settled facts:   a batch is 1..64 events in strictly ascending, gap-free sequence order.
//	First event:     must be the highest sequence already taken over plus one, or an already
//	                 taken-over sequence (an overlap/replay), never beyond that (a gap).
//	Overlap:         every event at or below last_event_sequence must have an identical receipt
//	                 (identical → skipped as a replay); different content is a receipt_conflict
//	                 that leaves the original receipt untouched (D-022).
//	Replay:          a batch whose events are all already taken over calls no hook and advances
//	                 nothing — the C5 "committed but ack lost" case (plan §4B.7).
//	Empty batch:     rejected; a takeover with no event is protocol misuse and can never be the
//	                 authority for anything (plan §4B.4).
//
// Errors use the repository's existing Fault contract: 400 for a body the contract cannot accept,
// 409 (ABORTED on the wire, controller-integration D2) for a sequence gap, an out-of-order batch or
// a payload conflict, 404 for an execution Cloud never registered. A database or internal
// invariant failure panics databaseFailure and surfaces as UNAVAILABLE, which tells the Controller
// "retry later" instead of "this batch is wrong" — the Node replays the un-acked batch either way.
func agentThreadTakeover(t *transaction, r *ControlRequest) Object {
	executionID := r.Body.S("executionId")
	events := threadTakeoverEvents(r.Body["events"])
	require(executionID != "" && len(executionID) <= 200, 400, "invalid_takeover")
	require(len(events) > 0, 400, "empty_thread_batch")
	require(len(events) <= threadTakeoverMaxBatch, 400, "invalid_takeover")

	e := t.one("SELECT * FROM node_executions WHERE execution_id=$1", executionID)
	require(e != nil, 404, "not_found")
	// Only a registered Agent-session execution has Thread events, and the batch must name the run
	// that execution was registered for: a wrong or stale execution is rejected rather than being
	// silently resolved to "the latest execution of that run" (§18).
	require(e.S("kind") == "agent_session", 409, "takeover_conflict")
	require(r.Body.S("operationId") == e.S("operationId"), 409, "takeover_conflict")

	last := e.N("lastEventSequence")
	for i, ev := range events {
		sequence := ev.N("sequence")
		require(sequence > 0, 400, "invalid_thread_event")
		if i > 0 {
			// Strictly ascending and gap-free inside the batch: Cloud must never reorder the Node's
			// conversation into a different order (D2), and a hole inside a batch would make the
			// receipts claim a continuity that was never delivered.
			require(sequence == events[i-1].N("sequence")+1, 409, "sequence_gap")
		}
	}
	first := events[0].N("sequence")
	require(first <= last+1, 409, "sequence_gap")

	// Overlap: sequences already taken over must replay identically. The original receipt is never
	// rewritten, so a conflicting payload cannot corrupt the fact the Node was already acked for.
	newEvents := make([]Object, 0, len(events))
	for _, ev := range events {
		if ev.N("sequence") > last {
			newEvents = append(newEvents, ev)
			continue
		}
		old := t.one("SELECT event FROM node_event_receipts WHERE execution_id=$1 AND sequence=$2", executionID, ev.N("sequence"))
		if old == nil {
			// last_event_sequence is only ever advanced after its receipts are inserted, so a
			// missing receipt below it is a corrupted invariant, not a client error.
			panic(databaseFailure{fmt.Errorf("thread takeover: execution %s has last_event_sequence=%d without receipt %d", executionID, last, ev.N("sequence"))})
		}
		require(jsonText(old.O("event")) == jsonText(ev), 409, "receipt_conflict")
	}
	if len(newEvents) == 0 {
		// Pure replay: nothing new to persist and no business hook to run (C5).
		return Object{"takenOverThrough": last}
	}

	highest := last
	for _, ev := range newEvents {
		t.exec("INSERT INTO node_event_receipts(execution_id,sequence,event) VALUES($1,$2,$3)",
			executionID, ev.N("sequence"), jsonText(ev))
		highest = ev.N("sequence")
	}
	// The business hook runs in this transaction, after the receipts and before the sequence
	// advance: a non-nil error panics databaseFailure so receipts, Thread entries, the running
	// transition and last_event_sequence roll back together and the Node replays the batch (§13).
	run := Object{"id": e.S("operationId")}
	h := t.hooks
	if h == nil {
		h = UnavailableAgentRunHooks{} // nil-safe fail closed
	}
	if err := h.ThreadEventsTakenOver(t, run, e, newEvents); err != nil {
		panic(databaseFailure{err})
	}
	// Fenced advance: monotonic, and never a jump over a sequence whose receipt is missing. The
	// advisory lock makes the fence unconditional in practice; a 0 here means an external writer
	// moved the execution, which must abort rather than silently accept the batch.
	if t.execRows(`UPDATE node_executions SET last_event_sequence=$2, updated_at=now() WHERE execution_id=$1 AND last_event_sequence=$3`,
		executionID, highest, last) == 0 {
		panic(databaseFailure{fmt.Errorf("thread takeover: execution %s last_event_sequence moved during the takeover transaction", executionID)})
	}
	return Object{"takenOverThrough": highest}
}

// threadTakeoverEvents narrows the request body's events to the canonical []Object the receipt
// identity is defined on. The gRPC translation layer builds []Object directly; a JSON-bodied
// control caller decodes []any of map[string]any, which is normalized here so both callers agree
// on one shape instead of one of them failing an unexpected type assertion.
func threadTakeoverEvents(v any) []Object {
	switch events := v.(type) {
	case []Object:
		return events
	case []any:
		out := make([]Object, 0, len(events))
		for _, ev := range events {
			o, ok := ev.(map[string]any)
			if !ok {
				return nil
			}
			out = append(out, Object(o))
		}
		return out
	default:
		return nil
	}
}
