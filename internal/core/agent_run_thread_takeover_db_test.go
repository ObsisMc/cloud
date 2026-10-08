package core

import (
	"context"
	"database/sql"
	"sync"
	"testing"
)

// Phase 4B DB tests (T4B-1..T4B-19): the Thread takeover path over a real isolated PostgreSQL
// schema. They drive the real control action ("agent_thread_takeover") through Store.Control with
// the real business hook set (NewBusinessAgentRunHooks) wired exactly the way cmd/server wires it,
// so what is proven here is the production path, not a test double: receipts, thread_entries,
// `starting → running`, thread_state and last_event_sequence all commit or roll back together.
//
// White-box (package core) only because the A→B seam takes the unexported *transaction; there is no
// in-memory substitute for PostgreSQL anywhere in this file.

// takeoverScene is one dispatched agent_session execution of a starting run, ready to be taken over.
type takeoverScene struct {
	dispatchableScene
	claims        *Claims
	execution     string
	initialTurnID string
}

// seedTakeoverScene starts a run through the Phase 3A path, registers its session execution through
// the Phase 4A path, and wires the production hook set. It ends in exactly the state plan §4B.4
// calls `pending`: execution registered, no Node record taken over yet.
func seedTakeoverScene(t *testing.T, store *Store) takeoverScene {
	t.Helper()
	store.AgentRunHooks = NewBusinessAgentRunHooks(store)
	scene, claims := seedDispatchableWork(t, store)
	execution := execOf(scene.work, "a")
	if _, err := dispatchAgentWork(t, store, claims, scene.work, execution, "", nil); err != nil {
		t.Fatalf("register the session execution: %v", err)
	}
	_, _, _, turnID, present := firstTurn(t, store, scene.run)
	id, ok := turnID.(*string)
	if !present || !ok || id == nil {
		t.Fatalf("Phase 3A must have written the first prompt as seq=1 with a turn id")
	}
	return takeoverScene{dispatchableScene: scene, claims: claims, execution: execution, initialTurnID: *id}
}

// threadRecord builds a settled `ora-history` line as the Node sends it: the HistoryLine's `at` and
// its own `seq`, plus the flattened HistoryRecord with its `type` tag. The Cloud-visible `kind` is
// that tag (Thread D2); the inner `seq` is the Node's line number and stays deliberately unrelated
// to both the Node event sequence and the Cloud-assigned Thread seq.
func threadRecord(tag string, line int64, text string) Object {
	return Object{
		"at":   "2026-09-30T10:00:00+00:00",
		"seq":  line,
		"type": tag,
		"update": Object{
			"sessionUpdate": "agent_message_chunk",
			"content":       Object{"type": "text", "text": text},
		},
	}
}

// threadEvent builds one wire ThreadEvent in the canonical shape the gRPC boundary produces.
func threadEvent(sequence int64, record Object, turnID string) Object {
	return Object{"sequence": sequence, "turnId": turnID, "record": record, "truncated": false}
}

// takeOver drives the real control action for one batch.
func takeOver(t *testing.T, store *Store, scene takeoverScene, execution string, events []Object) (Object, error) {
	t.Helper()
	return store.Control(context.Background(), &ControlRequest{
		Action:  "agent_thread_takeover",
		Body:    Object{"epoch": 1, "operationId": scene.run, "executionId": execution, "events": events},
		Service: scene.claims,
	})
}

// expectFault asserts the stable Fault contract (status + code) of a rejected takeover.
func expectFault(t *testing.T, err error, status int, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected fault %d/%s, got success", status, code)
	}
	f := ErrorCode(err)
	if f.Status != status || f.Code != code {
		t.Fatalf("fault %d/%s, want %d/%s", f.Status, f.Code, status, code)
	}
}

// threadSeqs lists the run's Thread entry seqs in order, so a test can assert continuity and the
// absence of duplicates in one comparison.
func threadSeqs(t *testing.T, store *Store, runID string) []int64 {
	t.Helper()
	rows, err := store.Pool.Query(`SELECT seq FROM thread_entries WHERE run_id=$1 ORDER BY seq`, runID)
	if err != nil {
		t.Fatalf("list thread seqs: %v", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			t.Fatalf("scan thread seq: %v", err)
		}
		out = append(out, seq)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate thread seqs: %v", err)
	}
	return out
}

// nodeEntry reads the Thread entry a Node record produced, keyed by the business duplicate guard.
func nodeEntry(t *testing.T, store *Store, runID string, sequence int64) (seq int64, source, kind string, record Object, turnID *string, present bool) {
	t.Helper()
	var raw []byte
	err := store.Pool.QueryRow(`SELECT seq,source,kind,record,turn_id FROM thread_entries WHERE run_id=$1 AND node_sequence=$2`, runID, sequence).Scan(&seq, &source, &kind, &raw, &turnID)
	if err != nil {
		return 0, "", "", nil, nil, false
	}
	return seq, source, kind, mustObject(t, raw), turnID, true
}

func countReceipts(t *testing.T, store *Store, execution string) int {
	t.Helper()
	var n int
	if err := store.Pool.QueryRow(`SELECT count(*) FROM node_event_receipts WHERE execution_id=$1`, execution).Scan(&n); err != nil {
		t.Fatalf("count receipts: %v", err)
	}
	return n
}

func lastEventSequence(t *testing.T, store *Store, execution string) int64 {
	t.Helper()
	var n int64
	if err := store.Pool.QueryRow(`SELECT last_event_sequence FROM node_executions WHERE execution_id=$1`, execution).Scan(&n); err != nil {
		t.Fatalf("read last_event_sequence: %v", err)
	}
	return n
}

func runThreadState(t *testing.T, store *Store, runID string) sql.NullString {
	t.Helper()
	var state sql.NullString
	if err := store.Pool.QueryRow(`SELECT thread_state FROM issue_runs WHERE id=$1`, runID).Scan(&state); err != nil {
		t.Fatalf("read thread_state: %v", err)
	}
	return state
}

// threadActive reports whether the run's Thread has been activated — the one Thread-state transition
// Phase 4B owns, and the one Phase 4B tests assert on. A run whose session was declared but whose
// first Node record has not been taken over is `pending`, not NULL: StartSession materializes that
// value in the same transaction as seq=1 (Phase 4C Slice 1, plan D-4C-01/G-016), so "not activated
// yet" is `pending` here, and `active` is written only by the takeover below.
// TestThreadTakeoverActivatesPendingThread covers the pending → active case.
func threadActive(t *testing.T, store *Store, runID string) bool {
	t.Helper()
	state := runThreadState(t, store, runID)
	return state.Valid && state.String == "active"
}

// TestThreadTakeoverFirstRecordRunsTheRun (T4B-1/T4B-2/T4B-10): a registered execution with no
// takeover is `starting`/`pending`; the first real Node record commits `running`/`running`/`active`
// in the takeover transaction and becomes Thread seq=2, leaving the Cloud-authored seq=1 untouched.
func TestThreadTakeoverFirstRecordRunsTheRun(t *testing.T) {
	store := dispatcherDB(t)
	scene := seedTakeoverScene(t, store)

	// T4B-2: registration is not running. Claim + RecordDispatch already happened in the scene.
	phase, status, _, _, _, _ := runFields(t, store, scene.run)
	if phase.String != "starting" || status != "dispatched" {
		t.Fatalf("after registration the run must stay starting/dispatched, got %s/%s", phase.String, status)
	}
	if threadActive(t, store, scene.run) {
		t.Fatalf("a registered execution with no taken-over record has not activated its Thread")
	}

	out, err := takeOver(t, store, scene, scene.execution, []Object{
		threadEvent(1, threadRecord("update", 0, "working on it"), ""),
	})
	if err != nil {
		t.Fatalf("first takeover: %v", err)
	}
	if out.N("takenOverThrough") != 1 {
		t.Fatalf("takenOverThrough must be the highest taken sequence, got %d", out.N("takenOverThrough"))
	}
	var failureReason string
	phase, status, failureReason, _, _, _ = runFields(t, store, scene.run)
	if phase.String != "running" || status != "running" {
		t.Fatalf("first real record must run the run, got %s/%s (%s)", phase.String, status, failureReason)
	}
	if !threadActive(t, store, scene.run) {
		t.Fatalf("the first real record must activate the Thread, got %v", runThreadState(t, store, scene.run))
	}
	if got := threadSeqs(t, store, scene.run); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("the first Node record is seq=2 after the preserved seq=1, got %v", got)
	}
	// T4B-10: seq=1 stays the Cloud-authored first prompt, byte for byte.
	source, kind, record, _, present := firstTurn(t, store, scene.run)
	if !present || source != "system" || kind != "user_turn" || record.S("content") == "" {
		t.Fatalf("seq=1 must stay the Phase 3A first prompt, got %s/%s %v", source, kind, record)
	}
	// The entry stores the record verbatim (Thread D2/invariant 6) and keeps the Node's own line
	// numbering separate from the Cloud seq (Node seq != Thread seq).
	seq, entrySource, entryKind, entryRecord, turnID, present := nodeEntry(t, store, scene.run, 1)
	if !present || seq != 2 || entrySource != "node" || entryKind != "update" {
		t.Fatalf("node entry must be seq=2/source=node/kind=update, got %d/%s/%s present=%v", seq, entrySource, entryKind, present)
	}
	if jsonText(entryRecord) != jsonText(threadRecord("update", 0, "working on it")) {
		t.Fatalf("Cloud must store the Node record verbatim, got %v", entryRecord)
	}
	if turnID != nil {
		t.Fatalf("a record with no user turn must store a NULL turn_id, got %v", *turnID)
	}
}

// TestThreadTakeoverActivatesPendingThread (T4B-1, §29): with the plan's literal `thread_state =
// 'pending'` in place, the first real Node record writes exactly `active` — the one Thread-state
// transition Phase 4B owns. No other value is written, and nothing else on the run changes.
//
// The state is seeded explicitly rather than inherited from the scene's StartSession, so this test
// states its own precondition and stays a statement about the takeover alone: whichever writer put
// the run in `pending` — the scene's session start, or this line — the takeover must move it to
// exactly `active` and nothing else. This test is what keeps the mandated transition proven rather
// than assumed.
func TestThreadTakeoverActivatesPendingThread(t *testing.T) {
	store := dispatcherDB(t)
	scene := seedTakeoverScene(t, store)
	if _, err := store.Pool.Exec(`UPDATE issue_runs SET thread_state='pending', version=version+1, updated_at=now() WHERE id=$1`, scene.run); err != nil {
		t.Fatalf("seed pending thread_state: %v", err)
	}

	if _, err := takeOver(t, store, scene, scene.execution, []Object{threadEvent(1, threadRecord("update", 0, "a"), "")}); err != nil {
		t.Fatalf("takeover: %v", err)
	}
	if state := runThreadState(t, store, scene.run); !state.Valid || state.String != "active" {
		t.Fatalf("pending must become exactly active, got %v", state)
	}
	phase, status, _, _, _, _ := runFields(t, store, scene.run)
	if phase.String != "running" || status != "running" {
		t.Fatalf("the same commit is the running transition, got %s/%s", phase.String, status)
	}
}

// TestThreadTakeoverContiguousBatch (T4B-3/T4B-11): a contiguous multi-event batch is accepted as a
// whole, allocates a gapless run of Thread seqs in input order, and advances last_event_sequence to
// the batch's highest sequence.
func TestThreadTakeoverContiguousBatch(t *testing.T) {
	store := dispatcherDB(t)
	scene := seedTakeoverScene(t, store)

	out, err := takeOver(t, store, scene, scene.execution, []Object{
		threadEvent(1, threadRecord("update", 0, "a"), ""),
		threadEvent(2, threadRecord("update", 1, "b"), ""),
		threadEvent(3, threadRecord("turnEnded", 2, ""), ""),
	})
	if err != nil {
		t.Fatalf("contiguous batch: %v", err)
	}
	if out.N("takenOverThrough") != 3 {
		t.Fatalf("takenOverThrough must be 3, got %d", out.N("takenOverThrough"))
	}
	if n := countReceipts(t, store, scene.execution); n != 3 {
		t.Fatalf("one receipt per event, got %d", n)
	}
	if got := lastEventSequence(t, store, scene.execution); got != 3 {
		t.Fatalf("last_event_sequence must be 3, got %d", got)
	}
	if got := threadSeqs(t, store, scene.run); len(got) != 4 || got[0] != 1 || got[1] != 2 || got[2] != 3 || got[3] != 4 {
		t.Fatalf("batch must produce seq 2,3,4 in order, got %v", got)
	}
	if _, _, kind, _, _, present := nodeEntry(t, store, scene.run, 3); !present || kind != "turnEnded" {
		t.Fatalf("the kind is the record's type tag, got %q present=%v", kind, present)
	}

	// A later batch continues at MAX(seq)+1, never at a value derived from the Node sequence.
	if _, err := takeOver(t, store, scene, scene.execution, []Object{threadEvent(4, threadRecord("gap", 3, "hole"), "")}); err != nil {
		t.Fatalf("second batch: %v", err)
	}
	if got := threadSeqs(t, store, scene.run); len(got) != 5 || got[4] != 5 {
		t.Fatalf("second batch must continue at seq=5, got %v", got)
	}
}

// TestThreadTakeoverReplayIsIdempotent (T4B-4/T4B-15): replaying a committed batch — the C5 case
// where the response was lost after the commit — writes no second receipt, no second entry, does not
// re-run the running transition and reports the same taken-over-through.
func TestThreadTakeoverReplayIsIdempotent(t *testing.T) {
	store := dispatcherDB(t)
	scene := seedTakeoverScene(t, store)
	batch := []Object{
		threadEvent(1, threadRecord("update", 0, "a"), ""),
		threadEvent(2, threadRecord("update", 1, "b"), ""),
	}
	first, err := takeOver(t, store, scene, scene.execution, batch)
	if err != nil {
		t.Fatalf("first takeover: %v", err)
	}
	phase, status, _, _, _, _ := runFields(t, store, scene.run)
	replay, err := takeOver(t, store, scene, scene.execution, batch)
	if err != nil {
		t.Fatalf("replay must be idempotent success, got %v", err)
	}
	if replay.N("takenOverThrough") != first.N("takenOverThrough") {
		t.Fatalf("replay must report the same taken-over-through, got %d then %d", first.N("takenOverThrough"), replay.N("takenOverThrough"))
	}
	if n := countReceipts(t, store, scene.execution); n != 2 {
		t.Fatalf("replay must not add receipts, got %d", n)
	}
	if got := threadSeqs(t, store, scene.run); len(got) != 3 {
		t.Fatalf("replay must not add entries, got %v", got)
	}
	if n := lastEventSequence(t, store, scene.execution); n != 2 {
		t.Fatalf("replay must not move last_event_sequence, got %d", n)
	}
	afterPhase, afterStatus, _, _, _, _ := runFields(t, store, scene.run)
	if afterPhase.String != phase.String || afterStatus != status {
		t.Fatalf("replay must not rewrite phase/status, got %s/%s then %s/%s", phase.String, status, afterPhase.String, afterStatus)
	}
	if !threadActive(t, store, scene.run) {
		t.Fatalf("replay must not reset thread_state, got %v", runThreadState(t, store, scene.run))
	}
}

// TestThreadTakeoverReceiptConflictKeepsOriginal (T4B-5): the same (execution, sequence) with a
// different payload is a conflict that writes nothing and leaves the original receipt and entry
// untouched (D-022).
func TestThreadTakeoverReceiptConflictKeepsOriginal(t *testing.T) {
	store := dispatcherDB(t)
	scene := seedTakeoverScene(t, store)
	if _, err := takeOver(t, store, scene, scene.execution, []Object{threadEvent(1, threadRecord("update", 0, "original"), "")}); err != nil {
		t.Fatalf("first takeover: %v", err)
	}
	var before []byte
	if err := store.Pool.QueryRow(`SELECT event FROM node_event_receipts WHERE execution_id=$1 AND sequence=1`, scene.execution).Scan(&before); err != nil {
		t.Fatalf("read original receipt: %v", err)
	}

	_, err := takeOver(t, store, scene, scene.execution, []Object{threadEvent(1, threadRecord("update", 0, "tampered"), "")})
	expectFault(t, err, 409, "receipt_conflict")

	var after []byte
	if err := store.Pool.QueryRow(`SELECT event FROM node_event_receipts WHERE execution_id=$1 AND sequence=1`, scene.execution).Scan(&after); err != nil {
		t.Fatalf("re-read receipt: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("a conflicting payload must leave the original receipt unchanged:\n%s\n%s", before, after)
	}
	if got := threadSeqs(t, store, scene.run); len(got) != 2 {
		t.Fatalf("a conflicting replay must not add entries, got %v", got)
	}
	if n := lastEventSequence(t, store, scene.execution); n != 1 {
		t.Fatalf("a rejected batch must not move last_event_sequence, got %d", n)
	}
}

// TestThreadTakeoverRejectsGapsAndReordering (T4B-6/T4B-7): a hole between batches, a hole inside a
// batch and a descending batch are all rejected as sequence gaps, and nothing is written.
func TestThreadTakeoverRejectsGapsAndReordering(t *testing.T) {
	store := dispatcherDB(t)
	scene := seedTakeoverScene(t, store)
	if _, err := takeOver(t, store, scene, scene.execution, []Object{
		threadEvent(1, threadRecord("update", 0, "a"), ""),
		threadEvent(2, threadRecord("update", 1, "b"), ""),
		threadEvent(3, threadRecord("update", 2, "c"), ""),
	}); err != nil {
		t.Fatalf("seed batch: %v", err)
	}

	for _, tc := range []struct {
		name   string
		events []Object
	}{
		{"gap between batches (expected 4, got 5)", []Object{threadEvent(5, threadRecord("update", 4, "e"), "")}},
		{"hole inside the batch", []Object{threadEvent(4, threadRecord("update", 3, "d"), ""), threadEvent(6, threadRecord("update", 5, "f"), "")}},
		{"descending batch", []Object{threadEvent(5, threadRecord("update", 4, "e"), ""), threadEvent(4, threadRecord("update", 3, "d"), "")}},
		{"restart from an old sequence", []Object{threadEvent(1, threadRecord("update", 0, "a"), ""), threadEvent(2, threadRecord("update", 1, "b"), "")}},
	} {
		_, err := takeOver(t, store, scene, scene.execution, tc.events)
		if tc.name == "restart from an old sequence" {
			// An exact prefix replay is legal (and idempotent); a differing prefix is not. This
			// case replays sequences 1..2 with the content they were taken over with.
			if err != nil {
				t.Fatalf("%s: a prefix replay must be accepted, got %v", tc.name, err)
			}
			continue
		}
		expectFault(t, err, 409, "sequence_gap")
	}
	if n := countReceipts(t, store, scene.execution); n != 3 {
		t.Fatalf("rejected batches must write no receipts, got %d", n)
	}
	if n := lastEventSequence(t, store, scene.execution); n != 3 {
		t.Fatalf("rejected batches must not move last_event_sequence, got %d", n)
	}
	if got := threadSeqs(t, store, scene.run); len(got) != 4 {
		t.Fatalf("rejected batches must add no entries, got %v", got)
	}
}

// TestThreadTakeoverOverlapReplayAndForward (T4B-4): an overlapping batch verifies its already
// taken-over prefix and forwards only the first-taken contiguous suffix.
func TestThreadTakeoverOverlapReplayAndForward(t *testing.T) {
	store := dispatcherDB(t)
	scene := seedTakeoverScene(t, store)
	first := threadEvent(1, threadRecord("update", 0, "a"), "")
	if _, err := takeOver(t, store, scene, scene.execution, []Object{first}); err != nil {
		t.Fatalf("first takeover: %v", err)
	}
	out, err := takeOver(t, store, scene, scene.execution, []Object{
		threadEvent(1, threadRecord("update", 0, "a"), ""),
		threadEvent(2, threadRecord("update", 1, "b"), ""),
	})
	if err != nil {
		t.Fatalf("overlapping batch must be accepted when the overlap replays identically: %v", err)
	}
	if out.N("takenOverThrough") != 2 {
		t.Fatalf("takenOverThrough must reach the new suffix, got %d", out.N("takenOverThrough"))
	}
	if got := threadSeqs(t, store, scene.run); len(got) != 3 || got[2] != 3 {
		t.Fatalf("only the new suffix becomes an entry, got %v", got)
	}
	if n := countReceipts(t, store, scene.execution); n != 2 {
		t.Fatalf("no receipt is duplicated, got %d", n)
	}

	// The same overlap with different content for the already-taken sequence is a conflict.
	_, err = takeOver(t, store, scene, scene.execution, []Object{
		threadEvent(1, threadRecord("update", 0, "changed"), ""),
		threadEvent(2, threadRecord("update", 1, "b"), ""),
	})
	expectFault(t, err, 409, "receipt_conflict")
}

// TestThreadTakeoverRejectsWrongExecutionAndEmptyBatch (T4B-8/T4B-19/§6): an unknown execution, an
// execution registered for another run, and a batch with no event are all rejected with nothing
// written.
func TestThreadTakeoverRejectsWrongExecutionAndEmptyBatch(t *testing.T) {
	store := dispatcherDB(t)
	scene := seedTakeoverScene(t, store)

	_, err := takeOver(t, store, scene, newID(), []Object{threadEvent(1, threadRecord("update", 0, "a"), "")})
	expectFault(t, err, 404, "not_found")

	// A second run's registered execution must not be assignable to this run's Thread (§18).
	second := seedStartingRun(t, store, sessionStartInput())
	bindConnectedNode(t, store, second.ws)
	runSessionStartPass(t, store)
	pick, err := store.Control(context.Background(), &ControlRequest{Action: "agent_work_claim", Body: Object{"epoch": 1}, Service: scene.claims})
	if err != nil || pick.O("work") == nil {
		t.Fatalf("claim the second run's work: %v", err)
	}
	otherWork := pick.O("work")
	otherExecution := execOf(otherWork, "b")
	if _, err := dispatchAgentWork(t, store, scene.claims, otherWork, otherExecution, "", nil); err != nil {
		t.Fatalf("register the second execution: %v", err)
	}
	_, err = takeOver(t, store, scene, otherExecution, []Object{threadEvent(1, threadRecord("update", 0, "a"), "")})
	expectFault(t, err, 409, "takeover_conflict")

	_, err = takeOver(t, store, scene, scene.execution, nil)
	expectFault(t, err, 400, "empty_thread_batch")

	if n := countReceipts(t, store, scene.execution); n != 0 {
		t.Fatalf("rejected batches must write no receipts, got %d", n)
	}
	if n := countReceipts(t, store, otherExecution); n != 0 {
		t.Fatalf("the other execution must stay untouched, got %d receipts", n)
	}
	if got := threadSeqs(t, store, scene.run); len(got) != 1 {
		t.Fatalf("rejected batches must add no entries, got %v", got)
	}
	phase, status, _, _, _, _ := runFields(t, store, scene.run)
	if phase.String != "starting" || status != "dispatched" {
		t.Fatalf("rejected batches must not run the run, got %s/%s", phase.String, status)
	}
}

// TestThreadTakeoverInitialTurnEchoIsDeduped (T4B-12): the echo of the first prompt is persisted as
// a receipt but produces no entry, consumes no seq, and leaves seq=1 untouched.
func TestThreadTakeoverInitialTurnEchoIsDeduped(t *testing.T) {
	store := dispatcherDB(t)
	scene := seedTakeoverScene(t, store)

	out, err := takeOver(t, store, scene, scene.execution, []Object{
		threadEvent(1, threadRecord("update", 0, "the first prompt"), scene.initialTurnID),
		threadEvent(2, threadRecord("update", 1, "agent answers"), ""),
	})
	if err != nil {
		t.Fatalf("echo batch: %v", err)
	}
	if out.N("takenOverThrough") != 2 {
		t.Fatalf("the echo still advances the execution's sequence, got %d", out.N("takenOverThrough"))
	}
	if n := countReceipts(t, store, scene.execution); n != 2 {
		t.Fatalf("the echo must be receipted (it is what the ack is for), got %d receipts", n)
	}
	if !threadActive(t, store, scene.run) {
		t.Fatalf("the agent's own record still activates the Thread, got %v", runThreadState(t, store, scene.run))
	}
	got := threadSeqs(t, store, scene.run)
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("the echo must not add a duplicate prompt: expected exactly seq 1,2 got %v", got)
	}
	if _, _, _, _, _, present := nodeEntry(t, store, scene.run, 1); present {
		t.Fatalf("the echoed prompt must not be stored as a node entry")
	}
	if seq, _, _, _, _, present := nodeEntry(t, store, scene.run, 2); !present || seq != 2 {
		t.Fatalf("the agent record is the first node entry at seq=2, got seq=%d present=%v", seq, present)
	}
	source, kind, record, turnID, present := firstTurn(t, store, scene.run)
	id, _ := turnID.(*string)
	if !present || source != "system" || kind != "user_turn" || record.S("content") == "" || id == nil || *id != scene.initialTurnID {
		t.Fatalf("seq=1 must remain the same Cloud-authored prompt, got %s/%s %v turn=%v", source, kind, record, turnID)
	}
}

// TestThreadTakeoverEchoOnlyBatchDoesNotRun (T4B-13/§31): a batch that carries only the echoed first
// prompt advances the execution's receipts but leaves the run `starting` and the Thread `pending`.
func TestThreadTakeoverEchoOnlyBatchDoesNotRun(t *testing.T) {
	store := dispatcherDB(t)
	scene := seedTakeoverScene(t, store)

	out, err := takeOver(t, store, scene, scene.execution, []Object{
		threadEvent(1, threadRecord("update", 0, "the first prompt"), scene.initialTurnID),
	})
	if err != nil {
		t.Fatalf("echo-only batch: %v", err)
	}
	if out.N("takenOverThrough") != 1 {
		t.Fatalf("the receipt still advances, got %d", out.N("takenOverThrough"))
	}
	if n := countReceipts(t, store, scene.execution); n != 1 {
		t.Fatalf("the receipt is the ack basis and must exist, got %d", n)
	}
	if n := lastEventSequence(t, store, scene.execution); n != 1 {
		t.Fatalf("last_event_sequence advances on a receipted event, got %d", n)
	}
	if got := threadSeqs(t, store, scene.run); len(got) != 1 || got[0] != 1 {
		t.Fatalf("an echo produces no entry, got %v", got)
	}
	phase, status, _, _, _, _ := runFields(t, store, scene.run)
	if phase.String != "starting" || status != "dispatched" {
		t.Fatalf("an echo-only batch is not session start evidence, got %s/%s", phase.String, status)
	}
	if threadActive(t, store, scene.run) {
		t.Fatalf("the Thread must not be activated, got %v", runThreadState(t, store, scene.run))
	}
}

// TestThreadTakeoverHookFailureRollsBackEverything (T4B-14): a failing business hook aborts the
// whole takeover — receipts, last_event_sequence, Thread entries, phase and thread_state all roll
// back, so the events are never acked and the Node replays them.
func TestThreadTakeoverHookFailureRollsBackEverything(t *testing.T) {
	store := dispatcherDB(t)
	scene := seedTakeoverScene(t, store)

	// A record type outside the known set is an invariant error raised by the real hook.
	_, err := takeOver(t, store, scene, scene.execution, []Object{threadEvent(1, threadRecord("notAHistoryRecord", 0, "?"), "")})
	if err == nil {
		t.Fatalf("an unknown record type must fail the takeover")
	}
	if f := ErrorCode(err); f.Status != 500 {
		t.Fatalf("a hook failure surfaces as an internal failure, got %d/%s", f.Status, f.Code)
	}
	if n := countReceipts(t, store, scene.execution); n != 0 {
		t.Fatalf("hook failure must roll the receipts back, got %d", n)
	}
	if n := lastEventSequence(t, store, scene.execution); n != 0 {
		t.Fatalf("hook failure must not advance last_event_sequence, got %d", n)
	}
	if got := threadSeqs(t, store, scene.run); len(got) != 1 {
		t.Fatalf("hook failure must write no entries, got %v", got)
	}
	phase, status, _, _, _, _ := runFields(t, store, scene.run)
	if phase.String != "starting" || status != "dispatched" {
		t.Fatalf("hook failure must not run the run, got %s/%s", phase.String, status)
	}
	if threadActive(t, store, scene.run) {
		t.Fatalf("hook failure must not change thread_state, got %v", runThreadState(t, store, scene.run))
	}

	// The other fail-closed default behaves the same way: a Store with no hook wired at all must
	// refuse the batch rather than commit receipts whose business meaning nobody applied (G-003).
	store.AgentRunHooks = UnavailableAgentRunHooks{}
	_, err = takeOver(t, store, scene, scene.execution, []Object{threadEvent(1, threadRecord("update", 0, "a"), "")})
	if err == nil {
		t.Fatalf("an unwired hook set must fail closed")
	}
	if n := countReceipts(t, store, scene.execution); n != 0 {
		t.Fatalf("an unwired hook set must roll the receipts back, got %d", n)
	}
	store.AgentRunHooks = NewBusinessAgentRunHooks(store)
	if _, err := takeOver(t, store, scene, scene.execution, []Object{threadEvent(1, threadRecord("update", 0, "a"), "")}); err != nil {
		t.Fatalf("the batch must be replayable after the failure is fixed: %v", err)
	}
	if n := countReceipts(t, store, scene.execution); n != 1 {
		t.Fatalf("the recovered batch writes its receipt once, got %d", n)
	}
	if got := threadSeqs(t, store, scene.run); len(got) != 2 || got[1] != 2 {
		t.Fatalf("the recovered batch still starts the Thread at seq=2, got %v", got)
	}
}

// TestThreadTakeoverStaleRunIsNotRerun (T4B-9): a run that already left `starting` is never moved
// back, and a cancel that committed first keeps the run out of `running` (T4B-17). The taken-over
// records are still appended, so a receipted event is never lost from the Thread.
func TestThreadTakeoverStaleRunIsNotRerun(t *testing.T) {
	store := dispatcherDB(t)
	scene := seedTakeoverScene(t, store)

	// Cancel first: the run keeps its cancel request and must not enter running (plan §4.11).
	if _, err := store.Pool.Exec(`UPDATE issue_runs SET cancel_requested_at=now(), version=version+1, updated_at=now() WHERE id=$1`, scene.run); err != nil {
		t.Fatalf("request cancel: %v", err)
	}
	if _, err := takeOver(t, store, scene, scene.execution, []Object{threadEvent(1, threadRecord("update", 0, "a"), "")}); err != nil {
		t.Fatalf("takeover after cancel: %v", err)
	}
	phase, status, _, _, _, cancelAt := runFields(t, store, scene.run)
	if phase.String != "starting" || status != "dispatched" || !cancelAt.Valid {
		t.Fatalf("a cancel that committed first must keep the run starting, got %s/%s cancel=%v", phase.String, status, cancelAt)
	}
	if threadActive(t, store, scene.run) {
		t.Fatalf("a cancelled run never activates its Thread, got %v", runThreadState(t, store, scene.run))
	}
	if got := threadSeqs(t, store, scene.run); len(got) != 2 {
		t.Fatalf("the record is still taken over into the Thread, got %v", got)
	}

	// A run already in a terminal phase is not re-run either.
	if _, err := store.Pool.Exec(`UPDATE issue_runs SET phase='releasing', status='cancelled', version=version+1, updated_at=now() WHERE id=$1`, scene.run); err != nil {
		t.Fatalf("move the run to releasing: %v", err)
	}
	if _, err := takeOver(t, store, scene, scene.execution, []Object{threadEvent(2, threadRecord("update", 1, "b"), "")}); err != nil {
		t.Fatalf("takeover on a releasing run: %v", err)
	}
	phase, status, failureReason, _, _, _ := runFields(t, store, scene.run)
	if phase.String != "releasing" || status != "cancelled" || failureReason != "" {
		t.Fatalf("a terminal run must not be rewritten, got %s/%s (%s)", phase.String, status, failureReason)
	}
	if got := threadSeqs(t, store, scene.run); len(got) != 3 || got[2] != 3 {
		t.Fatalf("the record is still taken over, got %v", got)
	}
}

// TestThreadTakeoverInvalidWorkspaceFailsClosed (T4B-17/G-011): a run whose Workspace is gone is
// never moved to running and records no terminal state; the gap stays G-011's, not a new lifecycle
// rule invented here.
func TestThreadTakeoverInvalidWorkspaceFailsClosed(t *testing.T) {
	store := dispatcherDB(t)
	scene := seedTakeoverScene(t, store)
	if _, err := store.Pool.Exec(`UPDATE workspaces SET deleted_at=now(), version=version+1 WHERE id=$1`, scene.ws); err != nil {
		t.Fatalf("soft-delete the run workspace: %v", err)
	}

	if _, err := takeOver(t, store, scene, scene.execution, []Object{threadEvent(1, threadRecord("update", 0, "a"), "")}); err != nil {
		t.Fatalf("takeover with an unusable workspace: %v", err)
	}
	phase, status, failureReason, _, _, _ := runFields(t, store, scene.run)
	if phase.String != "starting" || status != "dispatched" || failureReason != "" {
		t.Fatalf("an unusable workspace must fail closed, got %s/%s (%s)", phase.String, status, failureReason)
	}
	if threadActive(t, store, scene.run) {
		t.Fatalf("the Thread must not be activated, got %v", runThreadState(t, store, scene.run))
	}
	if n := countReceipts(t, store, scene.execution); n != 1 {
		t.Fatalf("the receipt is still committed, got %d", n)
	}
	if got := threadSeqs(t, store, scene.run); len(got) != 2 {
		t.Fatalf("the record is still part of the Thread, got %v", got)
	}
}

// TestThreadTakeoverConcurrentBatches (T4B-16, mandate §42): two concurrent takeovers of the same
// execution serialize on the control transaction — one commits the batch, the other replays it —
// and the Thread's seq values stay unique, gapless and monotonic.
func TestThreadTakeoverConcurrentBatches(t *testing.T) {
	store := dispatcherDB(t)
	scene := seedTakeoverScene(t, store)
	batches := [][]Object{
		{threadEvent(1, threadRecord("update", 0, "a"), ""), threadEvent(2, threadRecord("update", 1, "b"), "")},
		{threadEvent(1, threadRecord("update", 0, "a"), ""), threadEvent(2, threadRecord("update", 1, "b"), ""), threadEvent(3, threadRecord("update", 2, "c"), "")},
	}

	var wg sync.WaitGroup
	// The barrier makes the two callers contend for the same advisory lock instead of racing the
	// goroutine scheduler, so the test exercises the serialization rather than the scheduling.
	start := make(chan struct{})
	results := make([]Object, len(batches))
	errs := make([]error, len(batches))
	for i := range batches {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = takeOver(t, store, scene, scene.execution, batches[i])
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent batch %d failed: %v", i, err)
		}
	}
	if n := countReceipts(t, store, scene.execution); n != 3 {
		t.Fatalf("the durable receipt set is the union, taken once: got %d", n)
	}
	if got := threadSeqs(t, store, scene.run); len(got) != 4 || got[0] != 1 || got[1] != 2 || got[2] != 3 || got[3] != 4 {
		t.Fatalf("Thread seqs must stay unique and gapless, got %v", got)
	}
	phase, status, _, _, _, _ := runFields(t, store, scene.run)
	if phase.String != "running" || status != "running" {
		t.Fatalf("exactly one running transition, got %s/%s", phase.String, status)
	}
	if n := lastEventSequence(t, store, scene.execution); n != 3 {
		t.Fatalf("last_event_sequence is monotonic to the union, got %d", n)
	}
	// The advisory lock admits exactly two orders, and both are permitted outcomes: the 3-event
	// batch commits first (the 2-event batch then replays it and sees 3) or the 2-event batch
	// commits first (it sees 2, and the 3-event batch then takes the third event and sees 3). A
	// caller therefore observes at least the highest sequence of its OWN batch, never more than the
	// union, and the union is always reached by whoever holds the third event.
	first, second := results[0].N("takenOverThrough"), results[1].N("takenOverThrough")
	for i, got := range []int64{first, second} {
		if got < int64(len(batches[i])) {
			t.Fatalf("caller %d took over %d, less than the %d events it submitted", i, got, len(batches[i]))
		}
		if got != 2 && got != 3 {
			t.Fatalf("caller %d took over %d, which is not a durable prefix of the union", i, got)
		}
	}
	if first != 3 && second != 3 {
		t.Fatalf("the union must be fully taken over, got %d and %d", first, second)
	}
}

// TestThreadTakeoverHasNoLaterPhaseSideEffects (T4B-19): the takeover writes no Thread command, no
// delivery/idle state and no Thread API surface — Phase 4C's writers and Phase 5 stay untouched. The
// assertion is about what the takeover transaction *writes*, not about which tables exist: 4C Slice
// 1 legitimately lands `thread_commands` and `thread_entries.status` (plan D-4C-12), and the
// takeover must leave both alone.
func TestThreadTakeoverHasNoLaterPhaseSideEffects(t *testing.T) {
	store := dispatcherDB(t)
	scene := seedTakeoverScene(t, store)
	if _, err := takeOver(t, store, scene, scene.execution, []Object{threadEvent(1, threadRecord("update", 0, "a"), "")}); err != nil {
		t.Fatalf("takeover: %v", err)
	}
	// No command was enqueued: the takeover reports records, it never queues a user turn or an end.
	var commands int
	if err := store.Pool.QueryRow(`SELECT count(*) FROM thread_commands WHERE run_id=$1`, scene.run).Scan(&commands); err != nil {
		t.Fatalf("count thread_commands: %v", err)
	}
	if commands != 0 {
		t.Fatalf("the takeover must not enqueue Thread commands, got %d", commands)
	}
	// No turn lifecycle was attached: every entry the takeover wrote is node-sourced, and the D3
	// status belongs to user turns only.
	var turned int
	if err := store.Pool.QueryRow(`SELECT count(*) FROM thread_entries WHERE run_id=$1 AND status IS NOT NULL`, scene.run).Scan(&turned); err != nil {
		t.Fatalf("count settled turns: %v", err)
	}
	if turned != 0 {
		t.Fatalf("the takeover must not settle any turn, got %d", turned)
	}
	var delivered bool
	if err := store.Pool.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='thread_entries' AND column_name='delivered')`).Scan(&delivered); err != nil {
		t.Fatalf("check thread_entries.delivered: %v", err)
	}
	if delivered {
		t.Fatalf("Thread D3's delivered marking needs the Phase 4C queue and gives no column in 4B")
	}
	var deliveries int
	if err := store.Pool.QueryRow(`SELECT count(*) FROM execution_work WHERE kind='deliver_revision'`).Scan(&deliveries); err != nil {
		t.Fatalf("count deliver_revision work: %v", err)
	}
	if deliveries != 0 {
		t.Fatalf("the takeover must not release revision delivery, got %d", deliveries)
	}
	phase, status, _, _, _, _ := runFields(t, store, scene.run)
	if phase.String != "running" || status != "running" {
		t.Fatalf("the only transition 4B owns is starting→running, got %s/%s", phase.String, status)
	}
	if !threadActive(t, store, scene.run) {
		t.Fatalf("4B writes pending→active only, got %v", runThreadState(t, store, scene.run))
	}
}
