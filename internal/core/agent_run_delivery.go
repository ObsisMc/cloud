package core

import (
	"fmt"
	"time"
)

// The delivery half of an Agent IssueRun: the A-side takeover of a `deliver_revision` execution's
// terminal result ("agent_delivery_takeover", served by ExecutionService.TakeOverNodeEvent) and the
// B-owned settlement behind the DeliverySettled hook (controller-integration D2/D6, IssueRun D3/D4/D5;
// plan §5 Batch 2).
//
// It is the direct sibling of agentSessionTakeover (agent_run_session_end.go) and keeps the same
// commit boundary, because the obligation is the same shape — one terminal Node event, one receipt,
// one durable result, one business transition, one fenced advance:
//
//	BEGIN (lease-validated, submission-wrapped)
//	  A: require the event to be the next sequence after last_event_sequence (or an exact replay)
//	  A: INSERT node_event_receipts                      [the only basis for an EventAck]
//	  A: UPDATE node_executions.result                   [the durable terminal fact]
//	  B: AgentRunHooks.DeliverySettled(...)              [same transaction]
//	  A: UPDATE node_executions.last_event_sequence      [fenced advance]
//	COMMIT
//	  → only now may the Controller send EventAck for the event
//
// A delivery execution streams no events of its own — it is one dispatch with one terminal result —
// so its terminal event is sequence 1 (last_event_sequence starts at 0). The continuity, replay and
// conflict rules are nonetheless identical to the session path, and deliberately so: the identity of
// a taken-over delivery fact is the same `(execution_id, sequence)` receipt, and the same
// "committed but ack lost" retry must be an idempotent no-op rather than a second settlement.
//
// What the terminal result now also carries: the Revision. Cloud Revision D4 puts two steps outside
// this transaction — the local input-consistency and shape comparison, and the object-store HEAD that
// confirms every declared object exists with the declared size and SHA-256 — because this repository
// forbids holding a transaction across HTTP, and then writes the `revisions` row inside it, after the
// durable result and before the hook. The verdict those two steps produced is what this function
// consumes (see verifyDeliveryObjects): verified results are registered and settle the run;
// unverified ones are rewritten to Cloud's own `failed{verification_failed}` and settle as failures,
// so IssueRun D5 retries the delivery instead of releasing the run.

// Delivery terminal outcomes: the wire's snake_case spelling of the proto messages RevisionDelivered,
// RevisionUnchanged and RevisionFailed (proto ora/cloud/internal/v1/agent_executions.proto). Keeping
// the spelling in one place means the durable result object, the A-layer validation and the gRPC
// projection cannot drift apart.
const (
	revisionDeliveredOutcome = "revision_delivered"
	revisionUnchangedOutcome = "revision_unchanged"
	revisionFailedOutcome    = "revision_failed"
)

// revisionFailureReasons is the closed set of RevisionFailureReason names a Node may report. Two
// values of the proto enum are deliberately absent:
//
//   - UNSPECIFIED is not a reason at all;
//   - `verification_failed` is the reason Cloud records when the declared objects are missing or
//     differ, and the proto states "a Node never reports it" — accepting it from the wire would let
//     a Node assert Cloud's own verification verdict about objects Cloud has not looked at.
//
// An unknown name is refused rather than stored, so no reason Cloud cannot act on can reach the
// delivery-settlement policy (which is what decides between a backoff retry and giving up).
var revisionFailureReasons = map[string]bool{
	"session_not_settled":  true,
	"checkout_unavailable": true,
	"snapshot_failed":      true,
	"bundle_failed":        true,
	"history_unavailable":  true,
	"upload_failed":        true,
}

// deliveryResult reduces a delivery terminal result object to the hook's typed outcome.
// ok=false means the object is not a well-formed delivery terminal fact and the caller must refuse
// it before writing anything.
func deliveryResult(result Object) (DeliverySettledResult, bool) {
	switch result.S("outcome") {
	case revisionDeliveredOutcome:
		return DeliverySettledResult{Kind: DeliverySaved}, true
	case revisionUnchangedOutcome:
		return DeliverySettledResult{Kind: DeliveryUnchanged}, true
	case revisionFailedOutcome:
		reason := result.S("reason")
		if !revisionFailureReasons[reason] {
			return DeliverySettledResult{}, false
		}
		return DeliverySettledResult{Kind: DeliveryFailed, Reason: reason}, true
	default:
		return DeliverySettledResult{}, false
	}
}

// agentDeliveryTakeover commits one delivery execution's terminal Node event.
//
//	Settled facts:   the event's sequence is exactly last_event_sequence+1, or one already taken
//	                 over (an overlap/replay), never a jump past it (a gap).
//	Takeover:        receipt, the durable result, the B-owned delivery settlement and the sequence
//	                 advance commit together or not at all.
//	Replay:          an event already taken over with an identical canonical event and an identical
//	                 result returns the recorded row and runs no hook, so a "committed but ack lost"
//	                 retry can never release a second delivery or a second completion.
//	Conflict:        the same sequence with a different canonical event, or a different result under
//	                 a recorded receipt, is refused and the original receipt is left untouched.
//
// Errors use the repository's Fault contract: 400 for a result the approved wire contract cannot
// accept, 409 for a sequence gap, a wrong execution or a payload conflict, 404 for an execution Cloud
// never registered. A database or internal invariant failure — including a Revision that contradicts
// an already-registered one, and a hook that refuses the transition — panics databaseFailure and
// surfaces as UNAVAILABLE: "retry later", not "this result is wrong", so the Node keeps the result
// and replays it.
func (s *Store) agentDeliveryTakeover(t *transaction, r *ControlRequest, verdict revisionVerdict) Object {
	executionID, operationID := r.Body.S("executionId"), r.Body.S("operationId")
	sequence, event := r.Body.N("sequence"), r.Body.O("event")
	result := r.Body.O("result")
	require(executionID != "" && len(executionID) <= 200, 400, "invalid_takeover")
	// Sequence 0 is not an event sequence: last_event_sequence starts at 0 and every Node event is
	// 1-based, so a 0 here would claim continuity the Node never established. The canonical event must
	// be an object for the same reason the session path's must — node_event_receipts.event is jsonb.
	require(sequence > 0 && len(event) > 0, 400, "invalid_receipt")

	e := t.one("SELECT * FROM node_executions WHERE execution_id=$1 AND operation_id=$2", executionID, operationID)
	require(e != nil, 404, "not_found")
	// Only a registered delivery execution settles a delivery. A session execution's terminal event
	// belongs to agent_session_takeover and is refused here rather than projected onto the delivery
	// policy — the two paths write different business state and neither may stand in for the other.
	require(e.S("kind") == "deliver_revision", 409, "takeover_conflict")
	outcome, ok := deliveryResult(result)
	require(ok, 400, "invalid_result")
	// The result must come from the Node the execution was registered for: a result naming another
	// Node is a conflict, never a reason to re-target the delivery.
	require(result.O("node").S("nodeId") == e.S("nodeId"), 409, "result_conflict")

	// D4 step 4: an unverified result is recorded as Cloud's own verdict, never as the Node's claim.
	// The rewrite is confined to the durable result; the receipt keeps the Node's own bytes and
	// payload beside it, which is what makes the replay comparison below still exact.
	stored := result
	if verdict == revisionObjectsRejected {
		stored = Object{"node": result.O("node"), "outcome": revisionFailedOutcome, "reason": revisionVerificationFailure}
		outcome = DeliverySettledResult{Kind: DeliveryFailed, Reason: revisionVerificationFailure}
	}

	last := e.N("lastEventSequence")
	// Gap: a delivery terminal event may only be taken over once every earlier event of the same
	// execution has been taken over. A delivery streams none, so this is the ordinary
	// "sequence 1 after last=0" case; the check stays because a Node may legitimately resume an
	// execution whose earlier events a previous Controller already delivered.
	require(sequence <= last+1, 409, "sequence_gap")

	if sequence <= last {
		// Overlap: the recorded receipt and the recorded result are compared byte-for-byte, and neither
		// is rewritten. Identical is the "committed but ack lost" replay — a deterministic no-op that
		// runs no hook and advances nothing; different is a conflict that leaves the original intact.
		old := t.one("SELECT event FROM node_event_receipts WHERE execution_id=$1 AND sequence=$2", executionID, sequence)
		if old == nil {
			// last_event_sequence is only ever advanced after its receipts are inserted, so a missing
			// receipt below it is corrupted state, not a client error.
			panic(databaseFailure{fmt.Errorf("delivery takeover: execution %s has last_event_sequence=%d without receipt %d", executionID, last, sequence)})
		}
		require(jsonText(old.O("event")) == jsonText(event), 409, "receipt_conflict")
		// The payload is compared against the receipt's own copy of the Node's result, not against the
		// durable `node_executions.result`. The two are the same bytes whenever Cloud registered what
		// the Node reported, and differ exactly when Cloud rewrote it (D4 step 4) — where the durable
		// fact is Cloud's verdict while the receipt still holds what the Node sent. A replay carries
		// the Node's original claim again, so the receipt is the comparison that stays exact in both
		// cases; comparing against the rewritten result would refuse every replay of a failed
		// verification forever.
		recorded := old.O("event").O("result")
		if len(recorded) == 0 {
			recorded = e.O("result")
		}
		require(jsonText(recorded) == jsonText(result), 409, "result_conflict")
		return e
	}

	// Receipt first: it is what authorizes the Controller's EventAck, and it is written inside this
	// transaction so a rolled-back takeover never acked anything.
	t.exec("INSERT INTO node_event_receipts(execution_id,sequence,event) VALUES($1,$2,$3)", executionID, sequence, jsonText(event))
	// The durable terminal fact, written before the hook for the same reason the session path does:
	// both are in the one transaction and the hook re-reads every authoritative row it uses, so the
	// order between them is not observable.
	t.exec("UPDATE node_executions SET result=$2, updated_at=now() WHERE execution_id=$1", executionID, jsonText(stored))
	// D4 step 3: the Revision row, in this transaction, after the durable result and before the hook
	// — so the hook receives the id Cloud just committed, and a failure below rolls the row back with
	// everything else rather than leaving a Revision whose run never advanced.
	//
	// A registration that decides not to write (the run left `delivering`; invariant 9) leaves the
	// outcome without an id, and the hook's own phase gate then decides the result against the run.
	if verdict == revisionObjectsVerified {
		revisionID, registered, err := s.registerRevision(t, e.S("operationId"), result)
		if err != nil {
			panic(databaseFailure{err})
		}
		if registered {
			outcome.RevisionID = revisionID
		}
	}
	// The business hook runs in this transaction. A non-nil error panics databaseFailure so the
	// receipt, the result, the Revision and the settlement roll back together and the Node replays
	// the event.
	h := t.hooks
	if h == nil {
		h = UnavailableAgentRunHooks{} // nil-safe fail closed
	}
	if err := h.DeliverySettled(t, Object{"id": e.S("operationId")}, e, outcome); err != nil {
		panic(databaseFailure{err})
	}
	// Fenced advance: monotonic, and never a jump over a sequence whose receipt is missing. The
	// advisory lock makes the fence unconditional in practice; a 0 here means an external writer moved
	// the execution, which must abort rather than silently accept the event.
	if t.execRows(`UPDATE node_executions SET last_event_sequence=$2, updated_at=now() WHERE execution_id=$1 AND last_event_sequence=$3`,
		executionID, sequence, last) == 0 {
		panic(databaseFailure{fmt.Errorf("delivery takeover: execution %s last_event_sequence moved during the takeover transaction", executionID)})
	}
	return t.one("SELECT * FROM node_executions WHERE execution_id=$1", executionID)
}

// Delivery retry policy (IssueRun D5). The ADR fixes both numbers as first-version defaults, so they
// are named constants rather than invented values: the backoff starts at 30 s and doubles per
// attempt, capped at 10 minutes.
const (
	deliveryRetryBase = 30 * time.Second
	deliveryRetryCap  = 10 * time.Minute
)

// deliveryRetryBackoff is D5's exponential backoff for the attempt after `attempted` failed delivery
// executions: 30s * 2^(attempted-1), capped at 10 minutes. `attempted` is the number of delivery
// executions already registered for the run, so the first retry (one failure) waits 30 seconds.
//
// The doubling is bounded at 16 before the shift rather than trusted: the cap is reached at 5, and
// bounding the exponent is what keeps the arithmetic in range for any caller value.
func deliveryRetryBackoff(attempted int) time.Duration {
	if attempted < 1 {
		attempted = 1
	}
	if attempted > 16 {
		return deliveryRetryCap
	}
	if d := deliveryRetryBase << (attempted - 1); d > 0 && d < deliveryRetryCap {
		return d
	}
	return deliveryRetryCap
}

// deliverySettled is the B-owned delivery-settlement core behind the A→B hook DeliverySettled
// (controller-integration D6 deliverySettled, IssueRun D3/D4/D5; plan §5 Batch 2).
//
// It runs on the caller-owned *transaction of the delivery execution's terminal-event takeover,
// inside which everything it decides must commit or roll back with the receipt and the durable
// result. It never opens its own transaction, spawns a goroutine, or defers a post-commit effect.
//
// Contract (the rows are checked in this order, and the first one matches first):
//
//	phase != 'delivering'     → deterministic no-op: a replay or a late result decides nothing,
//	                            whether it reports a failure or a success.
//	saved / unchanged         → releasing with deliveryState=saved|unchanged and the registered
//	                            Revision id (releaseAfterDelivery). The id is part of the contract,
//	                            not an extra: the control plane writes the Revision before this hook
//	                            may see a success, so a success without one is an internal
//	                            contradiction rather than a run Cloud should release as saved.
//	failed, given up         → releasing with deliveryState=failed (releaseAfterDelivery).
//	failed, not given up     → the run STAYS `delivering` and a new delivery work item is released
//	                            with D5's backoff (a retry never creates a new logical delivery —
//	                            the run, its Revision target and its Workspace are unchanged).
//
// Identity is re-read, never taken from the caller: the run must be the one the registered delivery
// execution was dispatched for, through execution → execution_work → run, and the work item must be
// this run's `deliver_revision` work. A mismatch is an error that rolls the takeover back rather than
// a settlement projected onto some other run.
func (s *Store) deliverySettled(t *transaction, run, execution Object, result DeliverySettledResult) error {
	runID := run.S("id")
	if !validID(runID) {
		return fmt.Errorf("deliverySettled: invalid run id %q", run.S("id"))
	}
	executionID := execution.S("executionId")
	if executionID == "" {
		return fmt.Errorf("deliverySettled: run %s has no execution identity", runID)
	}
	o := t.one("SELECT * FROM issue_runs WHERE id=$1 AND deleted_at IS NULL", runID)
	if o == nil {
		return fmt.Errorf("deliverySettled: issue_run %s not found", runID)
	}
	if o.S("executorType") != "agent" {
		return fmt.Errorf("deliverySettled: run %s is executor_type=%q, not agent", runID, o.S("executorType"))
	}
	e := t.one("SELECT * FROM node_executions WHERE execution_id=$1", executionID)
	if e == nil {
		return fmt.Errorf("deliverySettled: execution %s is not registered", executionID)
	}
	if e.S("kind") != "deliver_revision" {
		return fmt.Errorf("deliverySettled: execution %s is kind=%q, not deliver_revision", executionID, e.S("kind"))
	}
	work := t.one("SELECT * FROM execution_work WHERE id=$1", e.S("workId"))
	if work == nil || work.S("runId") != runID || work.S("kind") != "deliver_revision" {
		// Matching execution → work → run is what proves this terminal result really belongs to this
		// run's delivery; a mismatch is refused rather than resolved to "some other run".
		return fmt.Errorf("deliverySettled: execution %s does not belong to run %s deliver_revision work", executionID, runID)
	}
	// The outcome kind is re-validated here even though the A layer already reduced it: the hook is
	// the last gate before a business transition, and an unknown kind is an internal contradiction
	// rather than a decision any policy below can make.
	switch result.Kind {
	case DeliverySaved, DeliveryUnchanged, DeliveryFailed:
	default:
		return fmt.Errorf("deliverySettled: run %s execution %s carries unknown outcome kind %q", runID, executionID, result.Kind)
	}

	// A settlement arriving for a run that already left `delivering` decides nothing, whatever it
	// carries — that is Cloud Revision invariant 9, and it is also what keeps this path live: the A
	// layer writes the receipt before this hook runs and rolls it back if the hook fails
	// (agentDeliveryTakeover), so refusing here would leave the Node's terminal event unacknowledged
	// forever, the liveness hole controller-integration D2's receipt design exists to prevent.
	// Receipted as a plain fact about a finished attempt, the same result lets the Node stop replaying
	// once the run has moved on (mandate §19, IssueRun D3 "阶段不倒退").
	if o.S("phase") != "delivering" {
		return nil
	}

	switch result.Kind {
	case DeliverySaved, DeliveryUnchanged:
		// The Revision was written by the control plane in this same transaction, before this hook ran
		// (D4 step 3); its absence means the two layers disagree about what was committed, and moving
		// the run to `releasing` as saved would record a success no row backs (invariant 3).
		if !validID(result.RevisionID) {
			return fmt.Errorf("deliverySettled: run %s execution %s reports %s without a registered Revision", runID, executionID, result.Kind)
		}
	case DeliveryFailed:
		// Two sets, deliberately: the wire's closed set a Node may report, and Cloud's own
		// `verification_failed` verdict, which the A layer writes when the declared objects could not
		// be confirmed (D4 step 4). Accepting the second from the wire is what revisionFailureReasons
		// refuses, and that refusal still stands — this branch sees the reason only after the A layer
		// decided it, never straight from a Node's payload.
		if result.Reason != revisionVerificationFailure && !revisionFailureReasons[result.Reason] {
			return fmt.Errorf("deliverySettled: run %s execution %s carries failure reason %q, outside the closed set", runID, executionID, result.Reason)
		}
	}

	// The outcome kinds ARE D4's deliveryState values (`saved`, `unchanged`, `failed`), which is why
	// the run's result can carry the kind straight through: one spelling of the delivery outcome,
	// used by the hook contract and by the stored run result alike.
	if result.Kind != DeliveryFailed {
		return s.releaseAfterDelivery(t, o, result.Kind, result.RevisionID)
	}
	if s.deliveryGivenUp(t, o) {
		return s.releaseAfterDelivery(t, o, DeliveryFailed, "")
	}
	// D5: the failure releases a new delivery execution and the run keeps `delivering` — the sandbox
	// stays running because the Revision is not registered yet. The retry is a fresh work item with a
	// fresh attempt key, so it can never overwrite the objects of the attempt that failed. The session
	// execution is the run's settled `agent_session` one: the delivery spec names it, and its durable
	// result carries the end reason the final status is derived from.
	sess := runSessionExecution(t, runID)
	if sess == nil {
		return fmt.Errorf("deliverySettled: run %s has no settled agent_session execution to retry the delivery of", runID)
	}
	spec, err := s.sessionDeliverySpec(t, o, sess.S("executionId"))
	if err != nil {
		return err
	}
	attempts := t.one(`
		SELECT count(*) AS n FROM node_executions e
		JOIN execution_work w ON w.id = e.work_id
		WHERE w.run_id=$1 AND w.kind='deliver_revision'`, runID).N("n")
	availableAt := time.Now().UTC().Add(deliveryRetryBackoff(int(attempts)))
	if _, err := s.agentRunControlPlane().EnqueueExecutionWork(t, o, "deliver_revision", spec, sessionStartTarget(t, o.S("workspaceId")), &availableAt); err != nil {
		return err
	}
	return nil
}
