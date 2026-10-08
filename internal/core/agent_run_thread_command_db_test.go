package core

// Phase 4C S3 DB tests (T4C-21..T4C-24): the Thread command control plane over a real isolated
// PostgreSQL schema. They drive the production seam (StoreAgentRunControlPlane.EnqueueThreadCommand)
// and the real control actions ("agent_thread_claim" / "agent_thread_delivered") through
// Store.Control, so what is proven is the shipped path and not a double: the command row, the
// delivery registration and the post-commit signal are all real.
//
// White-box (package core) only because the A→B seam takes the unexported *transaction, exactly as
// the Phase 3B/4A/4B DB tests are. There is no in-memory substitute for PostgreSQL anywhere here.

import (
	"context"
	"database/sql"
	"testing"
)

// commandStore is dispatcherDB with the control signal hub wired: the Thread command path publishes
// ThreadCommandAvailable, and a nil hub would silently swallow the signal T4C-24 has to observe.
func commandStore(t *testing.T) *Store {
	t.Helper()
	store := dispatcherDB(t)
	store.Signals = NewControlHub()
	store.AgentRunControlPlane = realPlane()
	return store
}

// enqueueCommand runs the production seam inside a real caller-owned transaction, exactly as the
// Thread POST will. fail forces a business-side failure *after* the seam returned, so the caller's
// rollback has to take the command with it (T4C-21).
func enqueueCommand(t *testing.T, store *Store, runID string, command Object, fail bool) (string, error) {
	t.Helper()
	plane := store.agentRunControlPlane()
	var id string
	_, err := store.transact(context.Background(), func(tx *transaction) Object {
		var e error
		id, e = plane.EnqueueThreadCommand(tx, Object{"id": runID}, command)
		if e != nil {
			// Exactly how every production caller treats a seam error: the caller's transaction
			// rolls back, so the command cannot outlive the write that produced it.
			panic(databaseFailure{e})
		}
		if fail {
			reject(409, "forced_business_failure")
		}
		return Object{}
	})
	return id, err
}

// userTurnCommand is one canonical SubmitUserTurn command body as the business layer builds it.
func userTurnCommand(turnID, text string) Object {
	return SubmitUserTurnCommand(turnID, []Object{{"type": "text", "text": text}})
}

// claimCommands drives the real claim action for the lease holder.
func claimCommands(t *testing.T, store *Store, claims *Claims, limit int64) []Object {
	t.Helper()
	out, err := store.Control(context.Background(), &ControlRequest{
		Action: "agent_thread_claim", Body: Object{"epoch": 1, "limit": limit}, Service: claims,
	})
	if err != nil {
		t.Fatalf("claim thread commands: %v", err)
	}
	rows, _ := out["commands"].([]Object)
	return rows
}

// deliverCommand drives the real delivery registration for one command.
func deliverCommand(t *testing.T, store *Store, claims *Claims, submission, commandID, execution string) error {
	t.Helper()
	_, err := store.Control(context.Background(), &ControlRequest{
		Action: "agent_thread_delivered", SubmissionID: submission,
		Body: Object{"epoch": 1, "commandId": commandID, "executionId": execution}, Service: claims,
	})
	return err
}

// countCommands reports how many Thread commands a run holds.
func countCommands(t *testing.T, store *Store, runID string) int {
	t.Helper()
	var n int
	if err := store.Pool.QueryRow(`SELECT count(*) FROM thread_commands WHERE run_id=$1`, runID).Scan(&n); err != nil {
		t.Fatalf("count thread commands: %v", err)
	}
	return n
}

// commandDelivery reads the delivery state of one command straight from PostgreSQL.
func commandDelivery(t *testing.T, store *Store, commandID string) (kind string, body Object, deliveredAt, execution sql.NullString) {
	t.Helper()
	var raw []byte
	if err := store.Pool.QueryRow(`SELECT kind,body,delivered_at,delivered_execution_id FROM thread_commands WHERE id=$1`, commandID).
		Scan(&kind, &raw, &deliveredAt, &execution); err != nil {
		t.Fatalf("read thread command %s: %v", commandID, err)
	}
	return kind, mustObject(t, raw), deliveredAt, execution
}

// drainSignals collects every signal already delivered to a subscriber without waiting: Publish is
// synchronous into a buffered channel, so after the publishing transaction returned there is nothing
// left to wait for. No sleeps anywhere in these tests.
func drainSignals(ch <-chan ControlSignal) []ControlSignal {
	var out []ControlSignal
	for {
		select {
		case s := <-ch:
			out = append(out, s)
		default:
			return out
		}
	}
}

// registerSecondSessionExecution inserts a second *registered* agent_session execution for the run
// at the storage boundary. The partial-unique index only forbids two unregistered work items per
// run, so a run can legitimately carry several registered executions over its life — which is what
// makes "a different execution must not overwrite the delivery registration" a real case.
func registerSecondSessionExecution(t *testing.T, store *Store, work Object, execution string) {
	t.Helper()
	second := newID()
	if _, err := store.Pool.Exec(`INSERT INTO execution_work(id,tenant_id,run_id,workspace_id,kind,input,target,execution_id)
		SELECT $1,tenant_id,run_id,workspace_id,kind,input,target,$2 FROM execution_work WHERE id=$3`,
		second, execution, work.S("id")); err != nil {
		t.Fatalf("seed second execution_work: %v", err)
	}
	if _, err := store.Pool.Exec(`INSERT INTO node_executions(execution_id,kind,operation_id,work_id,node_id,input,dispatched_epoch)
		SELECT $1,kind,operation_id,$2,node_id,input,1 FROM node_executions WHERE work_id=$3`,
		execution, second, work.S("id")); err != nil {
		t.Fatalf("seed second node_executions: %v", err)
	}
}

// T4C-21 — the seam is a caller-transaction write: the command exists iff the caller's transaction
// committed, and the command_id is generated by the control plane (A), never supplied by the caller.
func TestThreadCommandEnqueueIsCallerTransactionScoped(t *testing.T) {
	store := commandStore(t)
	scene := seedTakeoverScene(t, store)
	turnID := newID()

	id, err := enqueueCommand(t, store, scene.run, userTurnCommand(turnID, "hello"), false)
	if err != nil {
		t.Fatalf("enqueue in a committing transaction: %v", err)
	}
	if !validID(id) {
		t.Fatalf("the seam must return an A-generated command id, got %q", id)
	}
	kind, body, deliveredAt, execution := commandDelivery(t, store, id)
	if kind != "submit_user_turn" {
		t.Fatalf("stored kind = %q, want submit_user_turn", kind)
	}
	if body.O("turn").S("turn_id") != turnID {
		t.Fatalf("stored turn_id = %q, want the caller's %q", body.O("turn").S("turn_id"), turnID)
	}
	if deliveredAt.Valid || execution.Valid {
		t.Fatalf("a freshly enqueued command must be undelivered, got delivered_at=%v execution=%v", deliveredAt, execution)
	}

	// A second enqueue is a second, independently identified command: the id is A's, not derived
	// from the run, the turn or anything the caller passed.
	other, err := enqueueCommand(t, store, scene.run, userTurnCommand(newID(), "again"), false)
	if err != nil {
		t.Fatalf("second enqueue: %v", err)
	}
	if other == id {
		t.Fatalf("two enqueues must not share a command id")
	}

	// The caller's transaction owns the outcome: a failure after the seam returned rolls the command
	// back, so nothing durable is left behind.
	rolled, err := enqueueCommand(t, store, scene.run, userTurnCommand(newID(), "rolled back"), true)
	if err == nil {
		t.Fatalf("a forced business failure must surface as an error")
	}
	if countCommands(t, store, scene.run) != 2 {
		t.Fatalf("the rolled-back command must not be persisted, run holds %d commands", countCommands(t, store, scene.run))
	}
	var present int
	if e := store.Pool.QueryRow(`SELECT count(*) FROM thread_commands WHERE id=$1`, rolled).Scan(&present); e != nil {
		t.Fatalf("look up the rolled-back command: %v", e)
	}
	if present != 0 {
		t.Fatalf("the rolled-back command %s must not exist", rolled)
	}
}

// T4C-21 (rejection) — the seam refuses what the schema and the ADR cannot accept, with a caller
// error rather than a rolled-back transaction carrying SQL detail.
func TestThreadCommandEnqueueRejectsInvalidCommands(t *testing.T) {
	store := commandStore(t)
	scene := seedTakeoverScene(t, store)
	for name, command := range map[string]Object{
		"unknown kind":             {"kind": "delete_everything", "body": Object{"reason": "user_ended"}},
		"missing body":             {"kind": "submit_user_turn"},
		"turn_id not an id":        SubmitUserTurnCommand("not-a-uuid", []Object{{"type": "text", "text": "x"}}),
		"empty content":            SubmitUserTurnCommand(newID(), nil),
		"unapproved end reason":    {"kind": "end_session", "body": Object{"reason": "because"}},
		"unspecified end reason":   {"kind": "end_session", "body": Object{"reason": ""}},
		"end_session with no body": {"kind": "end_session"},
	} {
		if _, err := enqueueCommand(t, store, scene.run, command, false); err == nil {
			t.Fatalf("%s: the seam must reject the command", name)
		}
	}
	if n := countCommands(t, store, scene.run); n != 0 {
		t.Fatalf("no rejected command may be persisted, got %d rows", n)
	}
}

// T4C-22 — claim gating: a command of a run whose session execution is not registered stays in
// Cloud and is not returned; once RecordDispatch registers the execution it becomes claimable with
// that execution and the work's target Node.
func TestThreadCommandClaimWaitsForRegisteredExecution(t *testing.T) {
	store := commandStore(t)
	scene, claims := seedDispatchableWork(t, store)
	turnID := newID()
	commandID, err := enqueueCommand(t, store, scene.run, userTurnCommand(turnID, "are you there?"), false)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// StartSession has committed but no Controller has registered the execution yet: the command is
	// durable but not deliverable.
	if got := claimCommands(t, store, claims, 100); len(got) != 0 {
		t.Fatalf("a run with no registered session execution must yield no commands, got %v", got)
	}

	execution := execOf(scene.work, "a")
	if _, err := dispatchAgentWork(t, store, claims, scene.work, execution, "", nil); err != nil {
		t.Fatalf("register the session execution: %v", err)
	}
	got := claimCommands(t, store, claims, 100)
	if len(got) != 1 {
		t.Fatalf("after registration the command must be claimable, got %d", len(got))
	}
	if got[0].S("id") != commandID || got[0].S("runId") != scene.run {
		t.Fatalf("claimed command identity = %v, want id=%s run=%s", got[0], commandID, scene.run)
	}
	if got[0].S("executionId") != execution {
		t.Fatalf("claimed executionId = %q, want the registered %q", got[0].S("executionId"), execution)
	}
	if got[0].S("nodeId") != scene.node {
		t.Fatalf("claimed nodeId = %q, want the work target %q", got[0].S("nodeId"), scene.node)
	}
	if got[0].O("body").O("turn").S("turn_id") != turnID {
		t.Fatalf("the claimed command must carry the caller's turn_id")
	}
}

// T4C-22 (ordering and grouping) — commands are grouped by run and ordered by creation within a
// run, and the limit is the contract's own 1..100 bound.
func TestThreadCommandClaimGroupsByRunInCreationOrder(t *testing.T) {
	store := commandStore(t)
	scene := seedTakeoverScene(t, store)
	var ids []string
	for _, text := range []string{"one", "two", "three"} {
		id, err := enqueueCommand(t, store, scene.run, userTurnCommand(newID(), text), false)
		if err != nil {
			t.Fatalf("enqueue %s: %v", text, err)
		}
		ids = append(ids, id)
	}
	got := claimCommands(t, store, scene.claims, 100)
	if len(got) != len(ids) {
		t.Fatalf("claim returned %d commands, want %d", len(got), len(ids))
	}
	for i, want := range ids {
		if got[i].S("id") != want {
			t.Fatalf("command %d = %s, want creation order %s", i, got[i].S("id"), want)
		}
	}
	if limited := claimCommands(t, store, scene.claims, 2); len(limited) != 2 {
		t.Fatalf("limit=2 must bound the answer, got %d", len(limited))
	}
	if _, err := store.Control(context.Background(), &ControlRequest{
		Action: "agent_thread_claim", Body: Object{"epoch": 1, "limit": 0}, Service: scene.claims,
	}); err == nil {
		t.Fatalf("limit=0 is outside the contract's 1..100 and must be rejected")
	}
}

// T4C-23 — delivery registration: the first call registers, the same logical submission replays,
// the same execution converges idempotently, a different execution never overwrites, and a
// registered command stops being claimed.
func TestThreadCommandDeliveryIsIdempotentAndFenced(t *testing.T) {
	store := commandStore(t)
	scene := seedTakeoverScene(t, store)
	commandID, err := enqueueCommand(t, store, scene.run, userTurnCommand(newID(), "ship it"), false)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := deliverCommand(t, store, scene.claims, "sub-1", commandID, scene.execution); err != nil {
		t.Fatalf("first registration: %v", err)
	}
	_, _, deliveredAt, execution := commandDelivery(t, store, commandID)
	if !deliveredAt.Valid || execution.String != scene.execution {
		t.Fatalf("registration must record both facts, got delivered_at=%v execution=%v", deliveredAt, execution)
	}
	first := deliveredAt.String

	// The same logical submission replayed (the Controller lost the reply) succeeds and changes
	// nothing: the recorded response is replayed, the row is untouched.
	if err := deliverCommand(t, store, scene.claims, "sub-1", commandID, scene.execution); err != nil {
		t.Fatalf("a replayed submission must succeed: %v", err)
	}
	// A new submission naming the same execution converges idempotently too.
	if err := deliverCommand(t, store, scene.claims, "sub-2", commandID, scene.execution); err != nil {
		t.Fatalf("re-registering the same execution must converge: %v", err)
	}
	if _, _, again, _ := commandDelivery(t, store, commandID); again.String != first {
		t.Fatalf("delivered_at moved on a replay: %s -> %s", first, again.String)
	}

	// A different execution of the same run must never take the registration over.
	registerSecondSessionExecution(t, store, scene.work, "exec-other")
	if err := deliverCommand(t, store, scene.claims, "sub-3", commandID, "exec-other"); err == nil {
		t.Fatalf("a different execution must not overwrite the registration")
	} else {
		expectFault(t, err, 409, "delivery_conflict")
	}
	if _, _, _, execution := commandDelivery(t, store, commandID); execution.String != scene.execution {
		t.Fatalf("the original delivery must survive the conflict, got %q", execution.String)
	}

	// An execution that is not this run's registered session execution is refused outright.
	if err := deliverCommand(t, store, scene.claims, "sub-4", commandID, "exec-unknown"); err == nil {
		t.Fatalf("an unknown execution must be refused")
	}
	// An unknown command is a not-found, not a conflict.
	if err := deliverCommand(t, store, scene.claims, "sub-5", newID(), scene.execution); err == nil {
		t.Fatalf("an unknown command must be refused")
	} else {
		expectFault(t, err, 404, "not_found")
	}

	// A registered command is no longer deliverable, so at-least-once delivery terminates.
	if got := claimCommands(t, store, scene.claims, 100); len(got) != 0 {
		t.Fatalf("a registered command must not be claimed again, got %v", got)
	}
}

// T4C-24 — ThreadCommandAvailable is a post-commit side effect: a committed enqueue publishes
// exactly one signal naming the run, and a rolled-back enqueue publishes none (no phantom signal).
func TestThreadCommandSignalOnlyAfterCommit(t *testing.T) {
	store := commandStore(t)
	scene := seedTakeoverScene(t, store)
	signals, cancel, ok := store.Signals.Subscribe()
	if !ok {
		t.Fatalf("the hub must accept a subscription")
	}
	defer cancel()

	if _, err := enqueueCommand(t, store, scene.run, userTurnCommand(newID(), "committed"), false); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	published := drainSignals(signals)
	if len(published) != 1 || published[0].Kind != SignalThreadCommandAvailable || published[0].RunID != scene.run {
		t.Fatalf("a committed enqueue must publish exactly one ThreadCommandAvailable for the run, got %v", published)
	}

	if _, err := enqueueCommand(t, store, scene.run, userTurnCommand(newID(), "rolled back"), true); err == nil {
		t.Fatalf("the forced business failure must surface as an error")
	}
	if rolled := drainSignals(signals); len(rolled) != 0 {
		t.Fatalf("a rolled-back enqueue must publish nothing, got %v", rolled)
	}
}
