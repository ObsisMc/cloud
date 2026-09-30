package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
)

// Phase 3A Session Start DB tests (D-012..D-015, G-007/G-011). They reuse
// dispatcherDB/seedDispatchScene (fresh isolated PostgreSQL schema per test, package core so the
// unexported *transaction seam and renderer are reachable) and drive the B-owned core through the
// real scan + StartSession transactions and the real thread_entries/issue_runs rows — the same
// white-box reason they live in package core. The deterministic stubAgentRunControlPlane observes
// EnqueueExecutionWork; the nil control plane exercises the real Unavailable fail-closed seam.

// sessionStartInput is a representative frozen run-create snapshot (D-013, G-007): the business
// layer's buildRunContext fallback shape plus the pinned plugin identity/version added by
// snapshotAgentRunInput.
func sessionStartInput() Object {
	return Object{
		"task":              "Fix the auth flow",
		"interactionValues": Object{"scope": "web", "assignee": "alice"},
		"target":            Object{"type": "issue", "id": "T-1"},
		"contextRefs": []any{
			Object{"refType": "commit", "refId": "abc123"},
			Object{"refType": "file", "refId": "internal/auth.go"},
		},
		"agentPluginId":      "official/hello-world",
		"agentPluginVersion": "1.0.0",
	}
}

// sessionScene is one seeded Phase 3A test run bound to a live run Workspace.
type sessionScene struct {
	seed dispSeed
	ws   string
}

// seedStartingRun sets the seeded run to phase='starting', status='dispatched' with the given frozen
// input snapshot and creates a live isolated run Workspace (kind='isolated', bound via issue_run_id)
// so the session start's workspace-alive predicate passes.
func seedStartingRun(t *testing.T, store *Store, input Object) sessionScene {
	t.Helper()
	seed := seedDispatchScene(t, store.Pool, false)
	var owner string
	if err := store.Pool.QueryRow(`SELECT owner_user_id FROM projects WHERE id=$1`, seed.project).Scan(&owner); err != nil {
		t.Fatalf("load project owner: %v", err)
	}
	ws := newID()
	tx, err := store.Pool.Begin()
	if err != nil {
		t.Fatalf("workspace begin: %v", err)
	}
	if _, err := tx.Exec(`INSERT INTO workspaces(id,tenant_id,owner_user_id,project_id,kind,desired_state,observed_state,runtime_generation,requested_ref,issue_run_id)
		VALUES($1,$2,$3,$4,'isolated','running','ready',1,'HEAD',$5)`, ws, seed.tenant, owner, seed.project, seed.run); err != nil {
		t.Fatalf("seed live run workspace: %v", err)
	}
	// A live isolated workspace requires exactly one task identity (deferred trigger); the workspace
	// and task INSERTs must land in the same transaction for the trigger to pass.
	if _, err := tx.Exec(`INSERT INTO tasks(id,workspace_id,title) VALUES($1,$2,'run-task')`, newID(), ws); err != nil {
		t.Fatalf("seed run workspace task: %v", err)
	}
	if _, err := tx.Exec(`UPDATE issue_runs
		SET phase='starting', status='dispatched', workspace_id=$2, input=$3, version=version+1, updated_at=now()
		WHERE id=$1`, seed.run, ws, jsonText(input)); err != nil {
		t.Fatalf("seed starting run: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("scene commit: %v", err)
	}
	return sessionScene{seed: seed, ws: ws}
}

// bindConnectedNode gives the run Workspace a live sandbox + connected Node so the session-start
// target derivation can observe sandbox_instance_id/node_id (§26 minimal target).
func bindConnectedNode(t *testing.T, store *Store, ws string) (sandboxID, nodeID string) {
	t.Helper()
	sandboxID, nodeID = newID(), newID()
	if _, err := store.Pool.Exec(`INSERT INTO sandbox_instances(id,workspace_id,generation,observed_state) VALUES($1,$2,1,'running')`, sandboxID, ws); err != nil {
		t.Fatalf("seed sandbox: %v", err)
	}
	if _, err := store.Pool.Exec(`INSERT INTO node_instances(id,workspace_id,sandbox_instance_id,service_subject,connection_state,protocol_version,initialized)
		VALUES($1,$2,$3,'node','connected',1,true)`, nodeID, ws, sandboxID); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	return sandboxID, nodeID
}

// firstTurn reads the run's Thread entry seq=1, if present.
func firstTurn(t *testing.T, store *Store, runID string) (source, kind string, record Object, turnID interface{}, present bool) {
	t.Helper()
	var src, k string
	var rec []byte
	var id *string
	err := store.Pool.QueryRow(`SELECT source,kind,record,turn_id FROM thread_entries WHERE run_id=$1 AND seq=1`, runID).Scan(&src, &k, &rec, &id)
	if err == sql.ErrNoRows {
		return "", "", nil, nil, false
	}
	if err != nil {
		t.Fatalf("read first turn: %v", err)
	}
	return src, k, mustObject(t, rec), id, true
}

func mustObject(t *testing.T, b []byte) Object {
	t.Helper()
	var obj Object
	if err := json.Unmarshal(b, &obj); err != nil {
		t.Fatalf("decode jsonb: %v", err)
	}
	return obj
}

// countThreadEntries reports how many Thread entries a run has (Phase 3A only ever writes seq=1).
func countThreadEntries(t *testing.T, store *Store, runID string) int {
	t.Helper()
	var n int
	if err := store.Pool.QueryRow(`SELECT count(*) FROM thread_entries WHERE run_id=$1`, runID).Scan(&n); err != nil {
		t.Fatalf("count thread entries: %v", err)
	}
	return n
}

// runSessionStartPass drives one full recovery pass exactly like StartQueuedAgentSessionsOnce but
// surfaces per-run StartSession errors, so a test reveals the true failure instead of the once-pass
// swallowing it (the production loop deliberately swallows per-run errors for isolation).
func runSessionStartPass(t *testing.T, store *Store) {
	t.Helper()
	ids, err := store.scanStartingAgentRuns(context.Background(), agentSessionStartBatchSize)
	if err != nil {
		t.Fatalf("session-start scan: %v", err)
	}
	for _, id := range ids {
		if err := store.agentRunSessionStart().StartSession(context.Background(), id); err != nil {
			t.Fatalf("start run %s: %v", id, err)
		}
	}
}

// TestAgentSessionStartOnceDeclaresExactlyOne (T3-1, T3-10, T3-11, T3-14): a fresh recovery pass
// discovers a pending 'starting'/'dispatched' run (restart recovery) and declares exactly one first
// produce — thread_entries seq=1 (source=system, kind=user_turn, deterministic body) and one
// EnqueueExecutionWork — while the run stays 'starting'/'dispatched' (never 'running' without
// takeover evidence). The work payload fixes the frozen plugin identity/version and the
// initial_turn's turn_id matches the seq=1 turn, and the target carries the connected sandbox/node.
func TestAgentSessionStartOnceDeclaresExactlyOne(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{workID: "w1"}
	store.AgentRunControlPlane = stub
	scene := seedStartingRun(t, store, sessionStartInput())
	sandboxID, nodeID := bindConnectedNode(t, store, scene.ws)

	runSessionStartPass(t, store)
	src, kind, record, turnID, ok := firstTurn(t, store, scene.seed.run)
	if !ok {
		t.Fatalf("first prompt must be written as thread_entries seq=1")
	}
	if src != "system" || kind != "user_turn" {
		t.Fatalf("seq=1 must be source=system kind=user_turn, got %q/%q", src, kind)
	}
	want := renderAgentInitialTurn(sessionStartInput())
	if got := record.S("content"); got != want {
		t.Fatalf("seq=1 content must equal the deterministic render, got %q want %q", got, want)
	}
	if stub.workN != 1 {
		t.Fatalf("exactly one enqueue expected, got %d", stub.workN)
	}
	if stub.workKind != "agent_session" {
		t.Fatalf("work kind must be agent_session, got %q", stub.workKind)
	}
	in := stub.workIn
	if in.S("agent_plugin_id") != "official/hello-world" || in.S("agent_plugin_version") != "1.0.0" {
		t.Fatalf("work must carry the frozen plugin identity/version, got %v", in)
	}
	it := in.O("initial_turn")
	if w := stringVal(turnID); w == "" || it.S("turn_id") != w {
		t.Fatalf("initial_turn.turn_id must equal the seq=1 turn_id %v, got %q", turnID, it.S("turn_id"))
	}
	if it.S("content") != want {
		t.Fatalf("initial_turn.content must equal the deterministic first prompt, got %q want %q", it.S("content"), want)
	}
	// Phase 3A exits still 'starting'/'dispatched' (§16, D-014).
	phase, status, _, _, _, _ := runFields(t, store, scene.seed.run)
	if !phase.Valid || phase.String != "starting" || status != "dispatched" {
		t.Fatalf("run must stay starting/dispatched, got phase=%v status=%q", phase, status)
	}
	// The minimal deterministic target carries the connected sandbox/node when present.
	if stub.workTarget.S("workspace_id") != scene.ws {
		t.Fatalf("target must scope the run Workspace, got %v", stub.workTarget)
	}
	if stub.workTarget.S("sandbox_instance_id") != sandboxID || stub.workTarget.S("node_id") != nodeID {
		t.Fatalf("target must carry the live sandbox/node, got %v", stub.workTarget)
	}
}

// TestAgentSessionStartReplayIsNoOp (T3-2, T3-14): a second pass or a direct re-Start never writes a
// second seq=1 nor re-enqueues — exactly-once holds because seq=1 is the durable once-guard and the
// INSERT ON CONFLICT DO NOTHING short-circuits a restart-after-commit.
func TestAgentSessionStartReplayIsNoOp(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{workID: "w1"}
	store.AgentRunControlPlane = stub
	scene := seedStartingRun(t, store, sessionStartInput())

	runSessionStartPass(t, store)
	runSessionStartPass(t, store)
	if err := store.agentRunSessionStart().StartSession(context.Background(), scene.seed.run); err != nil {
		t.Fatalf("direct replay: %v", err)
	}
	if n := countThreadEntries(t, store, scene.seed.run); n != 1 {
		t.Fatalf("replay must keep exactly one seq=1, got %d entries", n)
	}
	if stub.workN != 1 {
		t.Fatalf("replay must not re-enqueue, got %d enqueues", stub.workN)
	}
}

// TestAgentSessionStartCascSnapshotImmutable (T3-3, T3-12): the session start plans the run's *pinned*
// snapshot plugin version even when the Space roster has since upgraded — the payload never re-reads
// the current space_plugins/space_agents version (IssueRun D1/D6 upgrade immutability).
func TestAgentSessionStartCascSnapshotImmutable(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{workID: "w1"}
	store.AgentRunControlPlane = stub
	input := sessionStartInput()
	input["agentPluginVersion"] = "1.0.0"
	scene := seedStartingRun(t, store, input)
	// Upgrade the Space roster AFTER the run snapshot froze 1.0.0 — the exact upgrade-immutability case.
	ns, idf, _ := pluginIdentity("official/hello-world")
	if _, err := store.Pool.Exec(`INSERT INTO space_plugins(space_id,tenant_id,source_namespace,identifier,desired_state,desired_version,observed_state,observed_version)
		VALUES($1,$2,$3,$4,'installed','2.0.0','installed','2.0.0')`,
		collabSpaceForTenant(t, store, scene.seed.tenant), scene.seed.tenant, ns, idf); err != nil {
		t.Fatalf("seed upgraded roster: %v", err)
	}

	runSessionStartPass(t, store)
	if v := stub.workIn.S("agent_plugin_version"); v != "1.0.0" {
		t.Fatalf("work must use the pinned snapshot 1.0.0, not roster 2.0.0, got %q", v)
	}
}

// TestAgentSessionStartCanceledRedeclaresNothing (T3-4, T3-5 serialized): a cancelled 'starting' run
// never receives a first produce — zero seq=1, zero enqueue — and the run is untouched. Under the
// global advisory lock the cancel is re-read authoritatively in the StartSession transaction (the
// WHERE excludes cancel_requested_at), so a cancel observed before declaration always wins.
func TestAgentSessionStartCancelledRunDeclaresNothing(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{workID: "w1"}
	store.AgentRunControlPlane = stub
	scene := seedStartingRun(t, store, sessionStartInput())
	if _, err := store.Pool.Exec(`UPDATE issue_runs SET cancel_requested_at=now(),version=version+1,updated_at=now() WHERE id=$1`, scene.seed.run); err != nil {
		t.Fatalf("cancel run: %v", err)
	}

	runSessionStartPass(t, store)
	if stub.workN != 0 {
		t.Fatalf("cancelled run must get zero enqueues, got %d", stub.workN)
	}
	if countThreadEntries(t, store, scene.seed.run) != 0 {
		t.Fatalf("cancelled run must write no seq=1")
	}
	phase, status, _, _, _, _ := runFields(t, store, scene.seed.run)
	if !phase.Valid || phase.String != "starting" || status != "dispatched" {
		t.Fatalf("cancelled run must stay untouched, got phase=%v status=%q", phase, status)
	}
}

// TestAgentSessionStartSeamRollsBackSeq1 (T3-6, T3-13): when the A seam fails closed (no control
// plane wired → UnavailableAgentRunControlPlane), the seq=1 write and the enqueue must not commit —
// the run stays 'starting' with no first produce and no declaration. A/B atomicity: identical to the
// errant-stub case, because both roll back the shared transaction through databaseFailure.
func TestAgentSessionStartSeamRollsBackSeq1(t *testing.T) {
	store := dispatcherDB(t)
	store.AgentRunControlPlane = nil // real Unavailable fail-closed seam
	scene := seedStartingRun(t, store, sessionStartInput())

	if err := store.agentRunSessionStart().StartSession(context.Background(), scene.seed.run); err == nil {
		t.Fatalf("unavailable seam must surface an error")
	}
	if countThreadEntries(t, store, scene.seed.run) != 0 {
		t.Fatalf("seam error must roll back the seq=1 write")
	}
	phase, status, reason, _, _, _ := runFields(t, store, scene.seed.run)
	if !phase.Valid || phase.String != "starting" || status != "dispatched" {
		t.Fatalf("run must stay starting/dispatched after rollback, got phase=%v status=%q", phase, status)
	}
	if reason != "" {
		t.Fatalf("rollback must not record a failure_reason, got %q", reason)
	}
}

// TestAgentSessionStartSoftDeletedWorkspaceFailsClosed (T3-8, G-011): a 'starting' run whose run
// Workspace is soft-deleted is left untouched — no enqueue, no seq=1, no terminal failure — so the
// retry loop keeps it pending rather than fabricating a session against a dead Workspace (fail closed,
// never status=failed for test convenience).
func TestAgentSessionStartSoftDeletedWorkspaceFailsClosed(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{workID: "w1"}
	store.AgentRunControlPlane = stub
	scene := seedStartingRun(t, store, sessionStartInput())
	if _, err := store.Pool.Exec(`UPDATE workspaces SET deleted_at=now() WHERE id=$1`, scene.ws); err != nil {
		t.Fatalf("soft-delete workspace: %v", err)
	}

	runSessionStartPass(t, store)
	if stub.workN != 0 {
		t.Fatalf("soft-deleted workspace must not enqueue, got %d", stub.workN)
	}
	if countThreadEntries(t, store, scene.seed.run) != 0 {
		t.Fatalf("soft-deleted workspace must write no seq=1")
	}
	phase, status, reason, _, _, _ := runFields(t, store, scene.seed.run)
	if !phase.Valid || phase.String != "starting" || status != "dispatched" {
		t.Fatalf("must fail closed and stay starting/dispatched, got phase=%v status=%q", phase, status)
	}
	if reason != "" {
		t.Fatalf("fail-closed skip must not set a failure_reason, got %q", reason)
	}
}

func stringVal(v interface{}) string {
	if p, ok := v.(*string); ok && p != nil {
		return *p
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
