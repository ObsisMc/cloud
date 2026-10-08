package core

// Phase 4A DB tests (T4A-1..T4A-16) for the A-side dispatch registration: the node_executions
// migration (0020) and the agent_work_dispatch control action that records the Controller-generated
// execution_id, fences execution_work.execution_id, and enforces one-work→one-execution (D-020,
// D-021) with replay idempotency and invariant conflicts. White-box in package core: they drive the
// real StoreAgentRunControlPlane (to produce execution_work) and the real Control dispatch path,
// against a genuine isolated PostgreSQL schema (dispatcherDB), exactly as the Phase 3B/3A tests do.
// There is no running anywhere: every test asserts the run stays starting/dispatched and that no
// Phase 4B side effect (thread seq≥2, node_event_receipts, running) exists.

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

// dispatchableScene seeds a starting run with a connected Node, declares its execution_work via the
// real production seam, and returns the claimed work Object (its immutable input/target) plus the
// scene identity so dispatch can be driven and the run's phase asserted.
type dispatchableScene struct {
	seed sessionScene
	ws   string
	node string
	work Object
	run  string
}

// seedDispatchableWork binds a Controller lease and claims the one declared agent_session work of a
// freshly started run. The real plane persists execution_work exactly as cmd/server wires it.
func seedDispatchableWork(t *testing.T, store *Store) (dispatchableScene, *Claims) {
	t.Helper()
	store.AgentRunControlPlane = realPlane()
	scene := seedStartingRun(t, store, sessionStartInput())
	_, nodeID := bindConnectedNode(t, store, scene.ws)
	runSessionStartPass(t, store) // declares one execution_work via the real seam
	seedControllerLease(t, store)
	claims := &Claims{Role: "controller", RegisteredClaims: jwt.RegisteredClaims{Subject: "ctrl-a"}}
	pick, err := store.Control(context.Background(), &ControlRequest{
		Action: "agent_work_claim", Body: Object{"epoch": 1}, Service: claims,
	})
	if err != nil {
		t.Fatalf("claim after pass: %v", err)
	}
	work := pick.O("work")
	if work == nil {
		t.Fatalf("claim must discover the declared agent_session work")
	}
	return dispatchableScene{seed: scene, ws: scene.ws, node: nodeID, work: work, run: scene.seed.run}, claims
}

// seedControllerLease secures the global controller lease a dispatch registration requires.
func seedControllerLease(t *testing.T, store *Store) {
	t.Helper()
	if _, err := store.Pool.Exec(`INSERT INTO controller_leases(name,holder_id,epoch,expires_at)
		VALUES('global','ctrl-a',1,clock_timestamp()+interval '30 seconds')`); err != nil {
		t.Fatalf("seed lease: %v", err)
	}
}

// dispatch registers one work through the real agent_work_dispatch action. node/input override
// defaults to the work's immutable target/input when empty.
func dispatchAgentWork(t *testing.T, store *Store, claims *Claims, work Object, execution, node string, input Object) (Object, error) {
	t.Helper()
	if node == "" {
		node = work.O("target").S("node_id")
	}
	if len(input) == 0 {
		input = work.O("input")
	}
	return store.Control(context.Background(), &ControlRequest{
		Action:  "agent_work_dispatch",
		Body:    Object{"workId": work.S("id"), "executionId": execution, "nodeId": node, "input": input, "epoch": 1},
		Service: claims,
	})
}

// countNodeExecutionsByWork reports node_executions rows registered for a work.
func countNodeExecutionsByWork(t *testing.T, store *Store, workID string) int {
	t.Helper()
	var n int
	if err := store.Pool.QueryRow(`SELECT count(*) FROM node_executions WHERE work_id=$1`, workID).Scan(&n); err != nil {
		t.Fatalf("count node_executions: %v", err)
	}
	return n
}

// workExecutionID reads the fenced execution_id off an execution_work row.
func workExecutionID(t *testing.T, store *Store, workID string) sql.NullString {
	t.Helper()
	var s sql.NullString
	if err := store.Pool.QueryRow(`SELECT execution_id FROM execution_work WHERE id=$1`, workID).Scan(&s); err != nil {
		t.Fatalf("read work execution_id: %v", err)
	}
	return s
}

// nodeExecutionInput returns the stale node_executions row's stored input for a work.
func nodeExecutionInput(t *testing.T, store *Store, workID string) Object {
	t.Helper()
	var raw []byte
	if err := store.Pool.QueryRow(`SELECT input FROM node_executions WHERE work_id=$1`, workID).Scan(&raw); err != nil {
		t.Fatalf("read node_executions input: %v", err)
	}
	var o Object
	if err := json.Unmarshal(raw, &o); err != nil {
		t.Fatalf("decode node_executions input: %v", err)
	}
	return o
}

// execOf returns the Controller-supplied deterministic execution id used in a scene.
func execOf(work Object, tag string) string { return "exec-" + tag + "-" + work.S("id")[:8] }

// T4A-1 — first registration: un-registered agent_session work → one node_executions row, the fenced
// execution_id, and the run still starting/dispatched.
func TestAgentRunDispatchFirstRegistration(t *testing.T) {
	store := dispatcherDB(t)
	scene, claims := seedDispatchableWork(t, store)
	execution := execOf(scene.work, "a")
	out, err := dispatchAgentWork(t, store, claims, scene.work, execution, "", nil)
	if err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	if out.S("executionId") != execution {
		t.Fatalf("dispatch must return the recorded execution, got %v", out.S("executionId"))
	}
	if n := countNodeExecutionsByWork(t, store, scene.work.S("id")); n != 1 {
		t.Fatalf("first dispatch must register exactly one node_execution, got %d", n)
	}
	if id := workExecutionID(t, store, scene.work.S("id")); !id.Valid || id.String != execution {
		t.Fatalf("execution_work.execution_id must be fenced to %s, got %v", execution, id)
	}
	phase, status, _, _, _, _ := runFields(t, store, scene.run)
	if !phase.Valid || phase.String != "starting" || status != "dispatched" {
		t.Fatalf("dispatch must not run the run, got phase=%v status=%q", phase, status)
	}
}

// T4A-2 — same replay: identical work/execution/node/input dispatch returns success, adds no row,
// and does not rewrite the recorded identity.
func TestAgentRunDispatchSameReplay(t *testing.T) {
	store := dispatcherDB(t)
	scene, claims := seedDispatchableWork(t, store)
	execution := execOf(scene.work, "a")
	if _, err := dispatchAgentWork(t, store, claims, scene.work, execution, "", nil); err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	out, err := dispatchAgentWork(t, store, claims, scene.work, execution, "", nil)
	if err != nil {
		t.Fatalf("same replay must be idempotent success, got %v", err)
	}
	if out.S("executionId") != execution {
		t.Fatalf("replay must return the same execution, got %v", out.S("executionId"))
	}
	if n := countNodeExecutionsByWork(t, store, scene.work.S("id")); n != 1 {
		t.Fatalf("replay must not add a second node_execution, got %d", n)
	}
	if id := workExecutionID(t, store, scene.work.S("id")); !id.Valid || id.String != execution {
		t.Fatalf("replay must not change the binding, got %v", id)
	}
}

// T4A-3 — different execution id for the same work is a dispatch_conflict; the recorded binding is
// unchanged and no second node_execution appears.
func TestAgentRunDispatchDifferentExecutionIDConflict(t *testing.T) {
	store := dispatcherDB(t)
	scene, claims := seedDispatchableWork(t, store)
	first := execOf(scene.work, "a")
	if _, err := dispatchAgentWork(t, store, claims, scene.work, first, "", nil); err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	second := execOf(scene.work, "b")
	if _, err := dispatchAgentWork(t, store, claims, scene.work, second, "", nil); err == nil {
		t.Fatalf("a different execution id for the same work must conflict")
	}
	if id := workExecutionID(t, store, scene.work.S("id")); !id.Valid || id.String != first {
		t.Fatalf("conflicting dispatch must not overwrite the binding, got %v", id)
	}
	if n := countNodeExecutionsByWork(t, store, scene.work.S("id")); n != 1 {
		t.Fatalf("conflicting dispatch must not add a row, got %d", n)
	}
}

// T4A-4 — the same execution id registered to a different work is a conflict (the execution identity
// is globally unique as node_executions PK). Two runs are seeded and declared in one pass so the
// two works are both eligible; registering execution X on one then reusing X on the other conflicts.
func TestAgentRunDispatchSameExecutionDifferentWorkConflict(t *testing.T) {
	store := dispatcherDB(t)
	store.AgentRunControlPlane = realPlane()
	// Two independent starting runs, each on its own connected Node, declared in a single recovery
	// pass so both works are eligible.
	first := seedStartingRun(t, store, sessionStartInput())
	bindConnectedNode(t, store, first.ws)
	second := seedStartingRun(t, store, sessionStartInput())
	bindConnectedNode(t, store, second.ws)
	runSessionStartPass(t, store)
	seedControllerLease(t, store)
	claims := &Claims{Role: "controller", RegisteredClaims: jwt.RegisteredClaims{Subject: "ctrl-a"}}

	claimOne := func() Object {
		pick, err := store.Control(context.Background(), &ControlRequest{
			Action: "agent_work_claim", Body: Object{"epoch": 1}, Service: claims,
		})
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		w := pick.O("work")
		if w == nil {
			t.Fatalf("a work item was expected")
		}
		return w
	}
	work := claimOne()
	execution := execOf(work, "x")
	if _, err := dispatchAgentWork(t, store, claims, work, execution, "", nil); err != nil {
		t.Fatalf("dispatch work with execX: %v", err)
	}
	// Claim again: now the other run's work is the only un-registered one.
	other := claimOne()
	if other.S("id") == work.S("id") {
		t.Fatalf("a second distinct work item was expected")
	}
	if _, err := dispatchAgentWork(t, store, claims, other, execution, "", nil); err == nil {
		t.Fatalf("reusing an execution id already registered to another work must conflict")
	}
	if id := workExecutionID(t, store, other.S("id")); id.Valid {
		t.Fatalf("conflicting execution id must not bind the second work, got %v", id)
	}
}

// T4A-5 — node mismatch: dispatching to a node that is not the work's target is a conflict and leaves
// the recorded execution untouched.
func TestAgentRunDispatchNodeMismatchConflict(t *testing.T) {
	store := dispatcherDB(t)
	scene, claims := seedDispatchableWork(t, store)
	execution := execOf(scene.work, "a")
	otherNode := "node-other"
	if _, err := dispatchAgentWork(t, store, claims, scene.work, execution, otherNode, nil); err == nil {
		t.Fatalf("a dispatch to a non-target node must conflict")
	}
	if n := countNodeExecutionsByWork(t, store, scene.work.S("id")); n != 0 {
		t.Fatalf("invalid node dispatch must not register, got %d", n)
	}
	if id := workExecutionID(t, store, scene.work.S("id")); id.Valid {
		t.Fatalf("invalid node dispatch must not bind an execution, got %v", id)
	}
}

// T4A-6 — input mismatch: a dispatch whose supplied immutable input differs from the declared work
// input is rejected; and a same-execution dispatch after a valid registration must not silently
// overwrite the recorded input — a mismatched input replay is a conflict and the stored input is
// unchanged (§12/§13 mandate: a different input is never a silent override).
func TestAgentRunDispatchInputMismatchConflict(t *testing.T) {
	store := dispatcherDB(t)
	scene, claims := seedDispatchableWork(t, store)
	execution := execOf(scene.work, "a")
	// Valid immutable input registers.
	if _, err := dispatchAgentWork(t, store, claims, scene.work, execution, "", nil); err != nil {
		t.Fatalf("valid input first dispatch must succeed: %v", err)
	}
	// A replay of the same execution with a different immutable input must conflict.
	wrong := Object{"agent_plugin_id": "official/evil", "agent_plugin_version": "9.9.9", "initial_turn": Object{"turn_id": "x", "content": "tampered"}}
	if _, err := dispatchAgentWork(t, store, claims, scene.work, execution, "", wrong); err == nil {
		t.Fatalf("input-mismatch replay must conflict")
	}
	stored := nodeExecutionInput(t, store, scene.work.S("id"))
	if jsonText(stored) != jsonText(scene.work.O("input")) {
		t.Fatalf("existing row input must remain the immutable declared input, got %v", stored)
	}
}

// T4A-7 — atomicity / one-work-one-execution at the storage layer (§9/§10, D-021): the schema
// itself forbids two node_executions for one work. A scrolling-attempted second registration (a
// direct INSERT of a *different* execution for the same work) is rejected by the work_id UNIQUE
// constraint independently of any application ordering, so a single work cannot hold two executions
// even if an actor skips the control seam. The complementary atomicity — an unbound INSERT is
// discarded when its transaction rolls back and is never committed alone — is proven end-to-end by
// T4A-8 through the real dispatch path.
func TestAgentRunDispatchAtomicRollbackNoOrphan(t *testing.T) {
	store := dispatcherDB(t)
	scene, claims := seedDispatchableWork(t, store)
	execution := execOf(scene.work, "a")
	if _, err := dispatchAgentWork(t, store, claims, scene.work, execution, "", nil); err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	if n := countNodeExecutionsByWork(t, store, scene.work.S("id")); n != 1 {
		t.Fatalf("one registration must yield exactly one node_execution, got %d", n)
	}
	// A second node_execution for the same work, whatever the execution id, must be rejected by the
	// schema (work_id UNIQUE). Assert the single existing row is untouched after the rejected write.
	second := execOf(scene.work, "b")
	if _, err := store.Pool.Exec(`INSERT INTO node_executions(execution_id,kind,operation_id,work_id,node_id,input,dispatched_epoch)
		VALUES($1,$2,$3,$4,$5,$6,$7)`, second, "agent_session", scene.run, scene.work.S("id"), scene.node, jsonText(scene.work.O("input")), int64(1)); err == nil {
		t.Fatalf("a second node_execution for one work must be rejected by the schema")
	}
	if n := countNodeExecutionsByWork(t, store, scene.work.S("id")); n != 1 {
		t.Fatalf("rejected second insert must leave exactly one node_execution, got %d", n)
	}
	if id := workExecutionID(t, store, scene.work.S("id")); !id.Valid || id.String != execution {
		t.Fatalf("rejected second insert must not disturb the binding, got %v", id)
	}
}

// T4A-8 — caller rollback: a caller that fails its transaction after a successful dispatch rolls back
// the node_executions INSERT and the execution_work.execution_id fence together — never a half-committed
// registration.
func TestAgentRunDispatchCallerRollback(t *testing.T) {
	store := dispatcherDB(t)
	store.AgentRunControlPlane = realPlane()
	scene := seedStartingRun(t, store, sessionStartInput())
	bindConnectedNode(t, store, scene.ws)
	runSessionStartPass(t, store)
	seedControllerLease(t, store)
	claims := &Claims{Role: "controller", RegisteredClaims: jwt.RegisteredClaims{Subject: "ctrl-a"}}
	pick, err := store.Control(context.Background(), &ControlRequest{
		Action: "agent_work_claim", Body: Object{"epoch": 1}, Service: claims,
	})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	work := pick.O("work")
	execution := execOf(work, "a")
	_, errRoll := store.transact(context.Background(), func(t *transaction) Object {
		agentWorkDispatch(t, &ControlRequest{
			Body: Object{"workId": work.S("id"), "executionId": execution, "nodeId": work.O("target").S("node_id"), "input": work.O("input"), "epoch": 1},
		})
		panic(&Fault{Code: "caller_failed", Status: 500}) // caller-owned tx aborts after a successful dispatch
	})
	if errRoll == nil {
		t.Fatalf("caller rollback must surface an error")
	}
	if n := countNodeExecutionsByWork(t, store, work.S("id")); n != 0 {
		t.Fatalf("caller rollback must leave no node_execution, got %d", n)
	}
	if id := workExecutionID(t, store, work.S("id")); id.Valid {
		t.Fatalf("caller rollback must leave execution_work.execution_id NULL, got %v", id)
	}
}

// T4A-9 — claim is a pure read: after claiming (and even after dispatching), the run is still
// starting — claim itself never transitions it.
func TestAgentRunClaimDoesNotRun(t *testing.T) {
	store := dispatcherDB(t)
	scene, _ := seedDispatchableWork(t, store)
	phase, status, _, _, _, _ := runFields(t, store, scene.run)
	if !phase.Valid || phase.String != "starting" || status != "dispatched" {
		t.Fatalf("claim must leave the run starting/dispatched, got phase=%v status=%q", phase, status)
	}
}

// T4A-10 — RecordDispatch does not run: after registration the run is still starting/dispatched, its
// status unchanged from Phase 3.
func TestAgentRunRecordDispatchDoesNotRun(t *testing.T) {
	store := dispatcherDB(t)
	scene, claims := seedDispatchableWork(t, store)
	if _, err := dispatchAgentWork(t, store, claims, scene.work, execOf(scene.work, "a"), "", nil); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	phase, status, _, _, _, _ := runFields(t, store, scene.run)
	if !phase.Valid || phase.String != "starting" || status != "dispatched" {
		t.Fatalf("registration must not run the run, got phase=%v status=%q", phase, status)
	}
}

// T4A-11 — crash C1 recovery: an un-registered work remains discoverable by claim after a restart
// (fresh Control claim returns it again, still un-bound).
func TestAgentRunCrashC1UnregisteredRecoverable(t *testing.T) {
	store := dispatcherDB(t)
	scene, claims := seedDispatchableWork(t, store)
	// Simulate Controller death before dispatch: nothing was registered.
	pick, err := store.Control(context.Background(), &ControlRequest{
		Action: "agent_work_claim", Body: Object{"epoch": 1}, Service: claims,
	})
	if err != nil {
		t.Fatalf("reclaimed after crash: %v", err)
	}
	if pick.O("work") == nil || pick.O("work").S("id") != scene.work.S("id") {
		t.Fatalf("un-registered work must remain claimable after restart")
	}
	if id := workExecutionID(t, store, scene.work.S("id")); id.Valid {
		t.Fatalf("C1 crash must leave execution_work.execution_id NULL (reclaimable), got %v", id)
	}
}

// T4A-12 — crash C2 recovery: a registered-but-not-yet-taken execution is found by its deterministic
// execution_id and by the pending list, so the replacement Controller reuses the same identity
// instead of registering a second one.
func TestAgentRunCrashC2RegisteredRecoverable(t *testing.T) {
	store := dispatcherDB(t)
	scene, claims := seedDispatchableWork(t, store)
	execution := execOf(scene.work, "a")
	if _, err := dispatchAgentWork(t, store, claims, scene.work, execution, "", nil); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	// agent_work_get (no lease: recovery read) returns the same registered execution.
	rec, err := store.Control(context.Background(), &ControlRequest{
		Action: "agent_work_get", Body: Object{"executionId": execution}, Service: claims,
	})
	if err != nil {
		t.Fatalf("recover by execution_id: %v", err)
	}
	if rec.S("executionId") != execution || rec.S("workId") != scene.work.S("id") {
		t.Fatalf("recovered dispatch must be the registered one, got %v", rec)
	}
	// agent_work_pending (by node) lists the still-in-flight execution.
	pending, err := store.Control(context.Background(), &ControlRequest{
		Action: "agent_work_pending", Body: Object{"nodeId": scene.node}, Service: claims,
	})
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	execs, ok := pending["executions"].([]Object)
	if !ok || len(execs) != 1 || execs[0].S("executionId") != execution {
		t.Fatalf("pending must list the registered in-flight execution, got %v", pending["executions"])
	}
	if n := countNodeExecutionsByWork(t, store, scene.work.S("id")); n != 1 {
		t.Fatalf("recovery reads must not register a second execution, got %d", n)
	}
}

// T4A-13 — concurrent same execution id: two dispatches of the same work to the same execution
// converge on one authoritative registration with no inconsistent error.
func TestAgentRunDispatchConcurrentSameID(t *testing.T) {
	store := dispatcherDB(t)
	scene, claims := seedDispatchableWork(t, store)
	execution := execOf(scene.work, "a")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := dispatchAgentWork(t, store, claims, scene.work, execution, "", nil); err != nil && err.Error() != "" {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent same-id dispatch: %v", err)
	}
	if n := countNodeExecutionsByWork(t, store, scene.work.S("id")); n != 1 {
		t.Fatalf("concurrent same-id must converge on one registration, got %d", n)
	}
	if id := workExecutionID(t, store, scene.work.S("id")); !id.Valid || id.String != execution {
		t.Fatalf("concurrent same-id must fence to %s, got %v", execution, id)
	}
}

// T4A-14 — concurrent different execution ids: exactly one wins and is authoritative; the runner-up
// conflicts. Never two registrations for one work.
func TestAgentRunDispatchConcurrentDifferentIDs(t *testing.T) {
	store := dispatcherDB(t)
	scene, claims := seedDispatchableWork(t, store)
	execA, execB := execOf(scene.work, "a"), execOf(scene.work, "b")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, e := range []string{execA, execB} {
		wg.Add(1)
		go func(e string) {
			defer wg.Done()
			if _, err := dispatchAgentWork(t, store, claims, scene.work, e, "", nil); err != nil {
				errs <- err // one conflict is expected; a DB-integrity failure or double-registration is not
			}
		}(e)
	}
	wg.Wait()
	close(errs)
	sawConflict := false
	for err := range errs {
		if err != nil && err.Error() == "dispatch_conflict" {
			sawConflict = true
		} else if err != nil {
			t.Fatalf("unexpected concurrent dispatch error: %v", err)
		}
	}
	if !sawConflict {
		t.Fatalf("with two different execution ids on one work exactly one must conflict")
	}
	if n := countNodeExecutionsByWork(t, store, scene.work.S("id")); n != 1 {
		t.Fatalf("concurrent different-id must leave exactly one authoritative registration, got %d", n)
	}
	id := workExecutionID(t, store, scene.work.S("id"))
	if !id.Valid || (id.String != execA && id.String != execB) {
		t.Fatalf("the surviving binding must be one of the two ids, got %v", id)
	}
}

// T4A-15 — non-Agent regression: the agent_work_ control prefix does not shadow the clone or
// operation registry; a clone recovery read still routes to clone and an unknown agent_work action
// stays not_found.
func TestAgentRunDispatchNonAgentRegression(t *testing.T) {
	store := dispatcherDB(t)
	seedDispatchableWork(t, store)
	claims := &Claims{Role: "controller", RegisteredClaims: jwt.RegisteredClaims{Subject: "ctrl-a"}}
	// A clone_get on a nonexistent execution must resolve through the clone registry, not agent_work,
	// returning its own not_found.
	if _, err := store.Control(context.Background(), &ControlRequest{
		Action: "clone_get", Body: Object{"executionId": "does-not-exist"}, Service: claims,
	}); err == nil {
		t.Fatalf("clone_get must remain its own registry (404), not be hijacked")
	}
	// An unknown agent_work_ action is not_found (the prefix branch does not silently forward).
	if _, err := store.Control(context.Background(), &ControlRequest{
		Action: "agent_work_nope", Body: Object{"epoch": 1, "executionId": "x"}, Service: claims,
	}); err == nil {
		t.Fatalf("an unknown agent_work_ action must be not_found")
	}
}

// T4A-16 — no Phase 4B side effects: registering an execution leaves exactly the Phase 3 thread
// entries (seq=1), no running transition, and no receipts. Since the Phase 4B migration (0021) does
// create node_event_receipts, the obligation "dispatch alone registers nothing a Node event could
// have produced" is asserted on the rows instead of on the table's absence: dispatching writes no
// receipt for the execution it registered.
func TestAgentRunDispatchNoPhase4BSideEffects(t *testing.T) {
	store := dispatcherDB(t)
	scene, claims := seedDispatchableWork(t, store)
	if _, err := dispatchAgentWork(t, store, claims, scene.work, execOf(scene.work, "a"), "", nil); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if n := countThreadEntries(t, store, scene.run); n != 1 {
		t.Fatalf("no phase 4B takeover: thread entries must stay exactly 1 (seq=1), got %d", n)
	}
	var i int
	if err := store.Pool.QueryRow(`SELECT count(*) FROM thread_entries WHERE run_id=$1 AND seq>1`, scene.run).Scan(&i); err != nil || i != 0 {
		t.Fatalf("no thread seq>=2 expected, got %d err=%v", i, err)
	}
	phase, status, _, _, _, _ := runFields(t, store, scene.run)
	if !phase.Valid || phase.String != "starting" || status != "dispatched" {
		t.Fatalf("no running transition, got phase=%v status=%q", phase, status)
	}
	var receipts int
	if err := store.Pool.QueryRow(`SELECT count(*) FROM node_event_receipts WHERE execution_id=$1`, execOf(scene.work, "a")).Scan(&receipts); err != nil {
		t.Fatalf("count node_event_receipts: %v", err)
	}
	if receipts != 0 {
		t.Fatalf("registration must not receipt any Node event, got %d", receipts)
	}
	var lastEventSequence int64
	if err := store.Pool.QueryRow(`SELECT last_event_sequence FROM node_executions WHERE execution_id=$1`, execOf(scene.work, "a")).Scan(&lastEventSequence); err != nil {
		t.Fatalf("read last_event_sequence: %v", err)
	}
	if lastEventSequence != 0 {
		t.Fatalf("registration must leave last_event_sequence at 0, got %d", lastEventSequence)
	}
}
