package core

import "fmt"

// Thread takeover business core (Thread D1..D4, IssueRun D3/D6; plan §4B.4..§4B.8).
//
// It runs on the caller-owned *transaction of the A-side takeover, inside which the Thread entries,
// the run's `starting → running` transition and the A-side receipts must commit or roll back
// together. It never opens its own transaction and never defers a post-commit effect: a non-nil
// error aborts the whole takeover, so the Controller does not ack and the Node replays.
//
// The hook takes identity from the caller and re-reads everything else authoritatively (§14): the
// caller Object carries only the run id, and the run, the execution, the work item and the Thread's
// first entry are all re-read here, so a caller cannot assert a business fact the database does not
// hold. Matching (execution → work → run) is what proves the execution really belongs to this run
// and this session work (§14, §18): a wrong execution is rejected instead of being projected onto
// some other run's Thread.

// threadRecordKinds is the closed set of `ora-history` record type tags Thread D2 allows as a
// Thread entry `kind`. Cloud does not own the record format — desktop `ora-history` does (Thread
// D2) — and the tag is the top-level `type` discriminator of its `HistoryRecord`, a closed tagged
// enum (`#[serde(tag = "type", rename_all = "camelCase")]` in desktop
// `crates/history/src/record.rs`): meta, update, turnEnded, agentSwitched, handoffDelivered, gap.
// These six tags are therefore the "known set" D2 requires Cloud to validate against, and they are
// the only kind Cloud can derive without parsing the business fields D2 forbids it to interpret.
// An unrecognized tag is an invariant error that rolls the batch back (fail closed) rather than a
// kind Cloud invented; widening the set is a coordinated change with the format's owner.
var threadRecordKinds = map[string]bool{
	"meta":             true,
	"update":           true,
	"turnEnded":        true,
	"agentSwitched":    true,
	"handoffDelivered": true,
	"gap":              true,
}

// threadRecordKind returns the Thread entry kind for one taken-over Node record: the type tag the
// record itself carries, validated against the known set above.
func threadRecordKind(record Object) (string, bool) {
	kind := record.S("type")
	return kind, threadRecordKinds[kind]
}

// threadEventsTakenOver is the B-owned core behind the A→B hook ThreadEventsTakenOver
// (controller-integration D6, IssueRun D3, Thread D1/D4; plan §4B.7).
//
//	authoritative re-read: issue_runs, node_executions and execution_work are read here, never taken
//	                      from the caller Object; the execution must be an agent_session execution
//	                      whose work item is this run's own session work.
//	echo dedupe:          an event whose turn_id is the Thread's first prompt turn_id is the echo of
//	                      the prompt Cloud already wrote as seq=1: it is persisted as a receipt by
//	                      the caller but produces no entry and consumes no seq (Thread D3 rule).
//	seq allocation:       one gapless run-scoped seq per real record, from MAX(seq)+1 — seq=1 stays
//	                      the immutable Cloud-authored first prompt (D-023, G-009/§21/§23).
//	running authority:    the first real record commits `starting → running` in this same
//	                      transaction, and only when the run is still a valid starting run with a
//	                      usable workspace; nothing else about the run changes (§15, §30).
//	stale/terminal:       a run that is no longer `starting`, a cancelled run and an unusable
//	                      workspace are not advanced (G-011 fail-closed, §17/§20) but the taken-over
//	                      records are still appended to the Thread, so an acked event is never
//	                      silently dropped from the conversation.
func (s *Store) threadEventsTakenOver(t *transaction, run, execution Object, events []Object) error {
	runID := run.S("id")
	if !validID(runID) {
		return fmt.Errorf("threadEventsTakenOver: invalid run id %q", run.S("id"))
	}
	executionID := execution.S("executionId")
	if executionID == "" {
		return fmt.Errorf("threadEventsTakenOver: run %s has no execution identity", runID)
	}

	o := t.one("SELECT * FROM issue_runs WHERE id=$1 AND deleted_at IS NULL", runID)
	if o == nil {
		// No authoritative run to accept the events: an invariant violation, not a replay, so the
		// batch must not be acked (plan §4.10 "unknown run").
		return fmt.Errorf("threadEventsTakenOver: issue_run %s not found", runID)
	}
	if o.S("executorType") != "agent" {
		return fmt.Errorf("threadEventsTakenOver: run %s is executor_type=%q, not agent", runID, o.S("executorType"))
	}
	e := t.one("SELECT * FROM node_executions WHERE execution_id=$1", executionID)
	if e == nil {
		return fmt.Errorf("threadEventsTakenOver: execution %s is not registered", executionID)
	}
	if e.S("kind") != "agent_session" {
		return fmt.Errorf("threadEventsTakenOver: execution %s is kind=%q, not agent_session", executionID, e.S("kind"))
	}
	work := t.one("SELECT * FROM execution_work WHERE id=$1", e.S("workId"))
	if work == nil || work.S("runId") != runID || work.S("kind") != "agent_session" {
		// The execution belongs to a different run's work item: refusing here is what keeps a
		// mis-addressed batch from being written into another run's Thread (§18).
		return fmt.Errorf("threadEventsTakenOver: execution %s does not belong to run %s agent_session work", executionID, runID)
	}

	// The Thread's first entry is immutable (Phase 3A, §21) and is also the authoritative echo
	// identity: the first prompt's turn_id is the one the Node echoes back on the user turn it was
	// given (Thread D3, Node protocol D2). A run with no first prompt cannot have taken a session
	// execution over, so its absence is an invariant violation rather than a reason to allocate
	// seq=1 for a Node record.
	firstTurn := t.one("SELECT turn_id FROM thread_entries WHERE run_id=$1 AND seq=1", runID)
	if firstTurn == nil {
		return fmt.Errorf("threadEventsTakenOver: run %s has no first prompt entry (seq=1); refusing to allocate seq=1 to a Node record", runID)
	}
	initialTurnID := firstTurn.S("turnId")

	next := t.one("SELECT COALESCE(MAX(seq),0) AS m FROM thread_entries WHERE run_id=$1", runID).N("m") + 1
	taken := 0
	for _, ev := range events {
		if turnID := ev.S("turnId"); turnID != "" && turnID == initialTurnID {
			continue // echo of the first prompt: receipt only, seq=1 already presents it
		}
		kind, ok := threadRecordKind(ev.O("record"))
		if !ok {
			return fmt.Errorf("threadEventsTakenOver: run %s node sequence %d carries unknown record type %q", runID, ev.N("sequence"), ev.O("record").S("type"))
		}
		// Thread D1: the same (node_execution_id, node_sequence) yields at most one entry, even if
		// the A-side batch classification ever let a duplicate through. Identical content is a
		// no-op; different content under the same identity is an invariant error, never a rewrite.
		if prior := t.one("SELECT * FROM thread_entries WHERE node_execution_id=$1 AND node_sequence=$2", executionID, ev.N("sequence")); prior != nil {
			if prior.S("kind") != kind || jsonText(prior.O("record")) != jsonText(ev.O("record")) {
				return fmt.Errorf("threadEventsTakenOver: run %s already has a different entry for execution %s node sequence %d", runID, executionID, ev.N("sequence"))
			}
			continue
		}
		t.exec(`
			INSERT INTO thread_entries(run_id, seq, source, kind, record, turn_id, node_execution_id, node_sequence)
			VALUES($1,$2,'node',$3,$4,$5,$6,$7)`,
			runID, next, kind, jsonText(ev.O("record")), nullable(ev.S("turnId")), executionID, ev.N("sequence"))
		next++
		taken++
	}
	if taken == 0 {
		// Every event was an echo (or an identical duplicate): no record was taken over, so the run
		// stays exactly where it was — not running, thread_state still pending (plan §4B.4/§31).
		return nil
	}

	// The running transition (§15/§30). The gate is re-read here rather than passed in: a run that
	// a cancel, a settlement or an earlier batch already moved must not be moved again, and an
	// unusable workspace fails closed (G-011) instead of being recorded as a terminal state.
	if o.S("phase") == "starting" && o.S("status") == "dispatched" && o.S("cancelRequestedAt") == "" && runWorkspaceLive(t, o.S("workspaceId"), runID) {
		// The CAS must move exactly the row the gate above approved. The gate reads and this write
		// share one transaction snapshot while the caller's advisory lock excludes the other
		// takeover callers, so zero affected rows means some other writer moved the run under a
		// predicate this transaction did not observe — an invariant violation, not a race to
		// tolerate. Returning an error rolls the whole batch back (§13): a receipted record must
		// never be committed without the transition it is the authority for.
		if moved := t.execRows(`
			UPDATE issue_runs
			SET phase='running', status='running', thread_state='active', version=version+1, updated_at=now()
			WHERE id=$1 AND executor_type='agent' AND phase='starting' AND status='dispatched' AND cancel_requested_at IS NULL`, runID); moved != 1 {
			return fmt.Errorf("threadEventsTakenOver: run %s was not moved to running (rows affected %d)", runID, moved)
		}
	}
	return nil
}
