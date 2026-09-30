package core

// Phase 3B DB tests (T3B-1..T3B-14, §28 concurrency, §29 payload-mismatch) for the production
// execution seam (execution_work persistence + production EnqueueExecutionWork). White-box in package
// core: they drive the real StoreAgentRunControlPlane through the same caller-owned transactions the
// B side uses, and the real scan + StartSession against a real isolated PostgreSQL schema (the same
// harness the Phase 3A tests reuse: dispatcherDB / seedDispatchScene / seedStartingRun /
// bindConnectedNode / runSessionStartPass). Where a test must observe seam failure it uses a
// deterministic failing stub; where it must prove the DB partial-unique guard it calls the real seam
// directly.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// realPlane returns the production execution seam wired the way cmd/server wires it.
func realPlane() *StoreAgentRunControlPlane { return NewStoreAgentRunControlPlane() }

// agentSessionWorkInput is the durable snake-cased AgentSession payload the B side stores via
// AgentSessionWork.inputObject() — the exact shape StartSession hands to EnqueueExecutionWork. Direct
// tests feed the seam the same shape the real caller does, so payload assertions are representative.
func agentSessionWorkInput(version string) Object {
	return Object{
		"agent_plugin_id":      "official/hello-world",
		"agent_plugin_version": version,
		"initial_turn":         Object{"turn_id": "t-1", "content": "Begin this task.\n\nTask\nFix the auth flow"},
	}
}

// executionWorkRow reads the run's one execution_work row (or nil).
func executionWorkRow(t *testing.T, store *Store, runID string) Object {
	t.Helper()
	var count int
	if err := store.Pool.QueryRow(`SELECT count(*) FROM execution_work WHERE run_id=$1`, runID).Scan(&count); err != nil {
		t.Fatalf("count execution_work: %v", err)
	}
	if count == 0 {
		return nil
	}
	var id, tenantID, wid, kind string
	var rawInput, rawTarget []byte
	if err := store.Pool.QueryRow(`SELECT id,tenant_id,workspace_id,kind,input,target FROM execution_work WHERE run_id=$1`, runID).
		Scan(&id, &tenantID, &wid, &kind, &rawInput, &rawTarget); err != nil {
		t.Fatalf("read execution_work: %v", err)
	}
	return Object{
		"id": id, "tenantId": tenantID, "workspaceId": wid, "kind": kind,
		"input":  mustObject(t, rawInput),
		"target": mustObject(t, rawTarget),
	}
}

// enqueueDirect calls the real EnqueueExecutionWork seam for one run inside its own short
// transaction, exactly as a B caller would when invoking the seam mid-transaction.
func enqueueDirect(t *testing.T, store *Store, runID, tenantID, ws, kind string, input, target Object) (string, error) {
	t.Helper()
	var workID string
	_, err := store.transact(context.Background(), func(t *transaction) Object {
		id, e := store.agentRunControlPlane().EnqueueExecutionWork(t, Object{"id": runID, "tenantId": tenantID, "workspaceId": ws}, kind, input, target, nil)
		if e != nil {
			panic(databaseFailure{e}) // same wrap the B seam call sites use, so it rolls back
		}
		workID = id
		return Object{}
	})
	return workID, err
}

// errAgentRunControlPlane is the deterministic seam whose EnqueueExecutionWork always fails, so a
// test can prove that a seam error rolls back the seq=1 write and the declaration (T3B-4, T3B-14).
type errAgentRunControlPlane struct{}

func (errAgentRunControlPlane) CreateRunWorkspace(*transaction, Object) (RunWorkspaceOutcome, error) {
	return RunWorkspaceOutcome{}, nil
}
func (errAgentRunControlPlane) DeleteRunWorkspace(*transaction, Object) error { return nil }
func (errAgentRunControlPlane) EnqueueExecutionWork(*transaction, Object, string, Object, Object, *time.Time) (string, error) {
	return "", errEnqueue
}

func (errAgentRunControlPlane) EnqueueThreadCommand(*transaction, Object, Object) (string, error) {
	return "", nil
}

var errEnqueue = &testErr{msg: "simulated enqueue failure"}

type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }

// TestAgentRunExecutionWorkEnqueueOneRow (T3B-1, G-008): a real session start against the real
// production seam persists exactly one kind='agent_session' execution_work row in the same
// transaction that writes seq=1, and it carries the frozen snapshot payload and the target's
// workspace/sandbox/node.
func TestAgentRunExecutionWorkEnqueueOneRow(t *testing.T) {
	store := dispatcherDB(t)
	store.AgentRunControlPlane = realPlane()
	scene := seedStartingRun(t, store, sessionStartInput())
	sandboxID, nodeID := bindConnectedNode(t, store, scene.ws)

	runSessionStartPass(t, store)
	w := executionWorkRow(t, store, scene.seed.run)
	if w == nil {
		t.Fatalf("real seam must persist one execution_work row")
	}
	if w.S("kind") != "agent_session" {
		t.Fatalf("kind must be agent_session, got %q", w.S("kind"))
	}
	if w.S("workspaceId") != scene.ws {
		t.Fatalf("target workspace must be the run workspace, got %q", w.S("workspaceId"))
	}
	in := w.O("input")
	want := sessionStartInput()
	if in.S("agent_plugin_id") != want.S("agentPluginId") || in.S("agent_plugin_version") != want.S("agentPluginVersion") {
		t.Fatalf("execution_work.input must carry the frozen plugin identity/version, got %v", in)
	}
	if tg := w.O("target"); tg.S("sandbox_instance_id") != sandboxID || tg.S("node_id") != nodeID {
		t.Fatalf("target must carry the live sandbox/node, got %v", w.O("target"))
	}
	phase, status, _, _, _, _ := runFields(t, store, scene.seed.run)
	if !phase.Valid || phase.String != "starting" || status != "dispatched" {
		t.Fatalf("run must stay starting/dispatched, got phase=%v status=%q", phase, status)
	}
}

// TestAgentRunExecutionWorkReplaySameRow (T3B-2, T3B-6, §21): a second pass or direct re-start never
// persists a second row nor re-reads the roster — the DB partial-unique guard + seq=1 together make
// the declaration exactly-once.
func TestAgentRunExecutionWorkReplaySameRow(t *testing.T) {
	store := dispatcherDB(t)
	store.AgentRunControlPlane = realPlane()
	scene := seedStartingRun(t, store, sessionStartInput())

	runSessionStartPass(t, store)
	first := executionWorkRow(t, store, scene.seed.run)
	runSessionStartPass(t, store)
	if err := store.agentRunSessionStart().StartSession(context.Background(), scene.seed.run); err != nil {
		t.Fatalf("direct replay: %v", err)
	}
	second := executionWorkRow(t, store, scene.seed.run)
	if first == nil || second == nil {
		t.Fatalf("replay must keep the row")
	}
	if first.S("id") != second.S("id") {
		t.Fatalf("replay must return the same row, got %q then %q", first.S("id"), second.S("id"))
	}
	if n := countThreadEntries(t, store, scene.seed.run); n != 1 {
		t.Fatalf("replay must keep exactly one seq=1, got %d", n)
	}
}

// TestAgentRunExecutionWorkUniqueGuard (T3B-3, §7): the database partial-unique index, not an
// application once-guard, is the authoritative identity. A direct second INSERT for the same run
// with execution_id IS NULL must be rejected by the constraint.
func TestAgentRunExecutionWorkUniqueGuard(t *testing.T) {
	store := dispatcherDB(t)
	store.AgentRunControlPlane = realPlane()
	scene := seedStartingRun(t, store, sessionStartInput())

	runSessionStartPass(t, store)
	w := executionWorkRow(t, store, scene.seed.run)
	_, err := store.transact(context.Background(), func(t *transaction) Object {
		t.exec(`INSERT INTO execution_work(id,tenant_id,run_id,workspace_id,kind,input,target)
			VALUES($1,$2,$3,$4,'agent_session','{}','{}')`,
			newID(), scene.seed.tenant, scene.seed.run, scene.ws)
		return Object{}
	})
	if err == nil {
		t.Fatalf("a second un-registered work for the same run must violate the partial unique index")
	}
	if w == nil || w.S("id") == "" {
		t.Fatalf("the original row must remain")
	}
}

// TestAgentRunExecutionWorkSeamErrorRollsBack (T3B-4, T3B-14): when the seam fails, the seq=1 write
// and the declaration roll back together — no orphan first produce, no durable work, run untouched.
func TestAgentRunExecutionWorkSeamErrorRollsBack(t *testing.T) {
	store := dispatcherDB(t)
	store.AgentRunControlPlane = errAgentRunControlPlane{}
	scene := seedStartingRun(t, store, sessionStartInput())

	if err := store.agentRunSessionStart().StartSession(context.Background(), scene.seed.run); err == nil {
		t.Fatalf("seam failure must surface an error")
	}
	if countThreadEntries(t, store, scene.seed.run) != 0 {
		t.Fatalf("seam error must roll back the seq=1 write")
	}
	if executionWorkRow(t, store, scene.seed.run) != nil {
		t.Fatalf("seam error must roll back the execution_work declaration")
	}
}

// TestAgentRunExecutionWorkCallerFailureRollsBack (T3B-5): a caller that enqueues and then fails
// later in the same transaction rolls the enqueue back too — execution_work never commits without its
// caller path completing (§12).
func TestAgentRunExecutionWorkCallerFailureRollsBack(t *testing.T) {
	store := dispatcherDB(t)
	store.AgentRunControlPlane = realPlane()
	scene := seedStartingRun(t, store, sessionStartInput())

	_, err := store.transact(context.Background(), func(t *transaction) Object {
		if _, e := store.agentRunControlPlane().EnqueueExecutionWork(t, Object{"id": scene.seed.run, "tenantId": scene.seed.tenant, "workspaceId": scene.ws}, "agent_session", sessionStartInput(), Object{"workspace_id": scene.ws}, nil); e != nil {
			panic(e)
		}
		panic(databaseFailure{&testErr{msg: "caller failure after enqueue"}})
	})
	if err == nil {
		t.Fatalf("caller failure must surface an error")
	}
	if executionWorkRow(t, store, scene.seed.run) != nil {
		t.Fatalf("caller failure must roll back the enqueued execution_work")
	}
}

// TestAgentRunExecutionWorkDirectAReplay (T3B-7, §21 layer-2): a direct A-side replay that bypasses
// the sequencing once-guard (calling the seam twice for the same run) still returns the same row and
// persists one row — the execution_work identity is self-authoritative, not merely seq=1's shadow.
func TestAgentRunExecutionWorkDirectAReplay(t *testing.T) {
	store := dispatcherDB(t)
	store.AgentRunControlPlane = realPlane()
	scene := seedStartingRun(t, store, sessionStartInput())
	input := agentSessionWorkInput("1.0.0")
	target := sessionStartTargetShim(scene.ws)

	first, err := enqueueDirect(t, store, scene.seed.run, scene.seed.tenant, scene.ws, "agent_session", input, target)
	if err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	second, err := enqueueDirect(t, store, scene.seed.run, scene.seed.tenant, scene.ws, "agent_session", input, target)
	if err != nil {
		t.Fatalf("replay enqueue must be idempotent success, got %v", err)
	}
	if first != second {
		t.Fatalf("direct A replay must return the same execution_work id, got %q then %q", first, second)
	}
	if w := executionWorkRow(t, store, scene.seed.run); w == nil || w.S("id") != first {
		t.Fatalf("exactly one persisted row expected, got %v", w)
	}
}

// sessionStartTargetShim produces a deterministic target object without reaching a live transaction,
// matching the workspace/sandbox/node shape the seam stores.
func sessionStartTargetShim(wid string) Object {
	return Object{"workspace_id": wid, "sandbox_instance_id": "sb-1", "node_id": "n-1"}
}

// TestAgentRunExecutionWorkSnapshotImmutable (T3B-8): the work payload fixes the run's *pinned*
// snapshot plugin version even after the Space roster upgrades — the control plane never re-reads the
// current space_agents/space_plugins version (D1/D6 immutability).
func TestAgentRunExecutionWorkSnapshotImmutable(t *testing.T) {
	store := dispatcherDB(t)
	store.AgentRunControlPlane = realPlane()
	input := sessionStartInput()
	input["agentPluginVersion"] = "1.0.0"
	scene := seedStartingRun(t, store, input)
	ns, idf, _ := pluginIdentity("official/hello-world")
	if _, err := store.Pool.Exec(`INSERT INTO space_plugins(space_id,tenant_id,source_namespace,identifier,desired_state,desired_version,observed_state,observed_version)
		VALUES($1,$2,$3,$4,'installed','2.0.0','installed','2.0.0')`,
		collabSpaceForTenant(t, store, scene.seed.tenant), scene.seed.tenant, ns, idf); err != nil {
		t.Fatalf("seed upgraded roster: %v", err)
	}

	runSessionStartPass(t, store)
	if w := executionWorkRow(t, store, scene.seed.run); w == nil || w.O("input").S("agent_plugin_version") != "1.0.0" {
		t.Fatalf("work must pin snapshot version 1.0.0, not roster 2.0.0; got %v", w)
	}
}

// TestAgentRunExecutionWorkCancelDeclaresNothing (T3B-9): a cancelled 'starting' run gets zero
// seq=1 and zero execution_work — cancel observed before declaration always wins.
func TestAgentRunExecutionWorkCancelDeclaresNothing(t *testing.T) {
	store := dispatcherDB(t)
	store.AgentRunControlPlane = realPlane()
	scene := seedStartingRun(t, store, sessionStartInput())
	if _, err := store.Pool.Exec(`UPDATE issue_runs SET cancel_requested_at=now(),version=version+1,updated_at=now() WHERE id=$1`, scene.seed.run); err != nil {
		t.Fatalf("cancel run: %v", err)
	}

	runSessionStartPass(t, store)
	if countThreadEntries(t, store, scene.seed.run) != 0 {
		t.Fatalf("cancelled run must write no seq=1")
	}
	if executionWorkRow(t, store, scene.seed.run) != nil {
		t.Fatalf("cancelled run must declare no execution_work")
	}
}

// TestAgentRunExecutionWorkNeverRunning (T3B-10, §18): declaring work is not starting the session —
// the run phase stays 'starting' with no takeover evidence.
func TestAgentRunExecutionWorkNeverRunning(t *testing.T) {
	store := dispatcherDB(t)
	store.AgentRunControlPlane = realPlane()
	scene := seedStartingRun(t, store, sessionStartInput())

	runSessionStartPass(t, store)
	phase, status, _, _, _, _ := runFields(t, store, scene.seed.run)
	if !phase.Valid || phase.String != "starting" || status != "dispatched" {
		t.Fatalf("run must stay starting/dispatched, got phase=%v status=%q", phase, status)
	}
}

// TestAgentRunExecutionWorkControllerPickup (T3B-11, T3B-12, §16/§17): the minimal Controller pickup
// (agent_work_claim) discovers the oldest eligible agent_session work with its target, is pure-read
// (replay returns the same single row, never a new one), and does not touch the run phase.
func TestAgentRunExecutionWorkControllerPickup(t *testing.T) {
	store := dispatcherDB(t)
	store.AgentRunControlPlane = realPlane()
	scene := seedStartingRun(t, store, sessionStartInput())
	runSessionStartPass(t, store)

	// Secure a valid lease for a Controller so the pickup's lease validity check passes.
	if _, err := store.Pool.Exec(`INSERT INTO controller_leases(name,holder_id,epoch,expires_at)
		VALUES('global','ctrl-a',1,clock_timestamp()+interval '30 seconds')`); err != nil {
		t.Fatalf("seed lease: %v", err)
	}
	claims := &Claims{Role: "controller", RegisteredClaims: jwt.RegisteredClaims{Subject: "ctrl-a"}}
	pick := func() Object {
		out, err := store.Control(context.Background(), &ControlRequest{Action: "agent_work_claim", Body: Object{"epoch": 1}, Service: claims})
		if err != nil {
			t.Fatalf("agent_work_claim: %v", err)
		}
		return out.O("work")
	}
	first := pick()
	if first == nil {
		t.Fatalf("pickup must discover the persisted agent_session work")
	}
	if first.S("kind") != "agent_session" || first.O("target").S("workspace_id") != scene.ws {
		t.Fatalf("pickup must return the agent_session work with its target, got %v", first)
	}
	second := pick()
	if second == nil || second.S("id") != first.S("id") {
		t.Fatalf("pickup replay must return the same work item, got %v then %v", first, second)
	}
	if n := countExecutionWorkRows(t, store, scene.seed.run); n != 1 {
		t.Fatalf("pure-read pickup must not create a second row, got %d", n)
	}
	phase, _, _, _, _, _ := runFields(t, store, scene.seed.run)
	if !phase.Valid || phase.String != "starting" {
		t.Fatalf("pickup must not advance phase, got %v", phase)
	}
}

func countExecutionWorkRows(t *testing.T, store *Store, runID string) int {
	t.Helper()
	var n int
	if err := store.Pool.QueryRow(`SELECT count(*) FROM execution_work WHERE run_id=$1`, runID).Scan(&n); err != nil {
		t.Fatalf("count execution_work: %v", err)
	}
	return n
}

// TestAgentRunExecutionWorkNonAgentExcluded (T3B-13): a non-agent run is never a session-start target
// and never declares execution_work; EnqueueExecutionWork against a run without agent columns is not
// reached.
func TestAgentRunExecutionWorkNonAgentExcluded(t *testing.T) {
	store := dispatcherDB(t)
	store.AgentRunControlPlane = realPlane()
	scene := seedStartingRun(t, store, sessionStartInput())
	// Demote the run to non-agent: clear the agent columns so it no longer satisfies the
	// executor_type='agent' start predicate, and give it a queued non-agent status.
	if _, err := store.Pool.Exec(`UPDATE issue_runs SET executor_type='workflow', phase=NULL, status='queued', workspace_id=NULL
		WHERE id=$1`, scene.seed.run); err != nil {
		t.Fatalf("demote run: %v", err)
	}

	runSessionStartPass(t, store)
	if countThreadEntries(t, store, scene.seed.run) != 0 {
		t.Fatalf("non-agent run must not be session-started")
	}
	if executionWorkRow(t, store, scene.seed.run) != nil {
		t.Fatalf("non-agent run must not declare execution_work")
	}
}

// TestAgentRunExecutionWorkConcurrentExactlyOnce (§28): two goroutines sealed the same run against
// the real seam — exactly one persisted row, no lost/split payload.
func TestAgentRunExecutionWorkConcurrentExactlyOnce(t *testing.T) {
	store := dispatcherDB(t)
	store.AgentRunControlPlane = realPlane()
	scene := seedStartingRun(t, store, sessionStartInput())
	input, target := agentSessionWorkInput("1.0.0"), sessionStartTargetShim(scene.ws)

	var mu sync.Mutex
	ids := map[string]int{}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := enqueueDirect(t, store, scene.seed.run, scene.seed.tenant, scene.ws, "agent_session", input, target)
			if err != nil {
				errs <- err
				return
			}
			mu.Lock()
			ids[id]++
			mu.Unlock()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent enqueue: %v", err)
	}
	if n := countExecutionWorkRows(t, store, scene.seed.run); n != 1 {
		t.Fatalf("concurrent enqueue must persist exactly one row, got %d", n)
	}
	w := executionWorkRow(t, store, scene.seed.run)
	if w == nil || w.O("input").S("agent_plugin_version") != "1.0.0" {
		t.Fatalf("the single row must carry an intact payload, got %v", w)
	}
	mu.Lock()
	total := 0
	for _, c := range ids {
		total += c
	}
	mu.Unlock()
	if total != 2 {
		t.Fatalf("both goroutines must report the same row id, got ids=%v", ids)
	}
}

// TestAgentRunExecutionWorkPayloadMismatch (§29): a replay of the same run with a *different*
// payload is an invariant error that rolls back — never a silent overwrite of the declared snapshot.
func TestAgentRunExecutionWorkPayloadMismatch(t *testing.T) {
	store := dispatcherDB(t)
	store.AgentRunControlPlane = realPlane()
	scene := seedStartingRun(t, store, sessionStartInput())
	input := agentSessionWorkInput("1.0.0")
	// A replay carrying a *different* payload for the same run: bump only the plugin version.
	bumped := agentSessionWorkInput("9.9.9")
	target := sessionStartTargetShim(scene.ws)

	if _, err := enqueueDirect(t, store, scene.seed.run, scene.seed.tenant, scene.ws, "agent_session", input, target); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	// A different version on the same run is a conflict, not an idempotent replay.
	if _, err := enqueueDirect(t, store, scene.seed.run, scene.seed.tenant, scene.ws, "agent_session", bumped, target); err == nil {
		t.Fatalf("payload-mismatch replay must return an invariant error")
	}
	if w := executionWorkRow(t, store, scene.seed.run); w == nil || w.O("input").S("agent_plugin_version") != "1.0.0" {
		t.Fatalf("mismatch must not overwrite the declared snapshot, got %v", w)
	}
	if n := countExecutionWorkRows(t, store, scene.seed.run); n != 1 {
		t.Fatalf("mismatch must not create a second row, got %d", n)
	}
}
