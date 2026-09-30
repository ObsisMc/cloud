package core

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// dispatcherDB creates an isolated PostgreSQL schema, migrates it, and returns a Store. These claim
// tests are white-box (package core) only because AgentRunControlPlane's seam takes the unexported
// *transaction, so an external package cannot implement the fake — but the stores themselves are real
// PostgreSQL, exactly like the integration package. The deterministic stubAgentRunControlPlane is the
// only control-plane provider used; no real A-side implementation is touched (Slice 1 §23).
func dispatcherDB(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("REQUIRE_POSTGRES") == "1" {
			t.Fatal("TEST_DATABASE_URL is required; dispatcher DB tests must not skip under REQUIRE_POSTGRES")
		}
		t.Skip("real PostgreSQL: set TEST_DATABASE_URL")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	admin := stdlib.OpenDB(*config)
	if err := admin.Ping(); err != nil {
		t.Fatalf("ping admin: %v", err)
	}
	schema := "core_disp_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	config.RuntimeParams["search_path"] = schema
	pool := stdlib.OpenDB(*config)
	t.Cleanup(func() {
		_ = pool.Close()
		_, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE")
		_ = admin.Close()
	})
	store := &Store{Pool: pool}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return store
}

// dispSeed is the identity set of one seeded project + issue + active Space Agent + queued real-agent
// run, plus the project uuid needed to make it busy.
type dispSeed struct {
	tenant, project, issue, agent, run string
}

// seedDispatchScene loads a minimal but schema-valid scene in one transaction: an active tenant with
// an admin member, its collaboration workspace, an active project, a real Space Agent, an issue bound
// to the project, and one queued executor_type='agent' run referencing the Space Agent. When busy is
// true an in-flight operation is added so the project is busy. IssueRun D2's project gate is satisfied
// via issues.project_ref.
func seedDispatchScene(t *testing.T, pool *sql.DB, busy bool) dispSeed {
	t.Helper()
	agent, run := uuid.NewString(), uuid.NewString()
	seed := dispSeed{tenant: uuid.NewString(), project: uuid.NewString(), issue: uuid.NewString(), agent: agent, run: run}
	user := uuid.NewString()
	space := uuid.NewString()
	tx, err := pool.Begin()
	if err != nil {
		t.Fatalf("seed begin: %v", err)
	}
	exec := func(name, q string, args ...any) {
		if _, err := tx.Exec(q, args...); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	exec("user", `INSERT INTO users(id,display_name,status) VALUES($1,'disp','active')`, user)
	exec("tenant", `INSERT INTO tenants(id,name,status) VALUES($1,'disp','active')`, seed.tenant)
	exec("membership", `INSERT INTO tenant_memberships(tenant_id,user_id,role,status) VALUES($1,$2,'admin','active')`, seed.tenant, user)
	exec("workspace", `INSERT INTO collab_workspaces(id,tenant_id,name,slug,created_by) VALUES($1,$2,'disp','disp-space',$3)`, space, seed.tenant, user)
	exec("project", `INSERT INTO projects(id,tenant_id,owner_user_id,space_id,name,repository_url,default_branch,lifecycle) VALUES($1,$2,$3,$4,'disp','https://example.invalid/disp.git','main','active')`, seed.project, seed.tenant, user, space)
	// A live project must carry exactly one main workspace (deferred project_main trigger); the run
	// Workspace the seam would create is a separate kind='isolated' workspace later.
	exec("main-workspace", `INSERT INTO workspaces(id,tenant_id,owner_user_id,project_id,kind,desired_state,observed_state,runtime_generation,requested_ref) VALUES($1,$2,$3,$4,'main','running','ready',1,'HEAD')`, uuid.NewString(), seed.tenant, user, seed.project)
	exec("agent", `INSERT INTO space_agents(id,space_id,tenant_id,plugin_id,display_name,status) VALUES($1,$2,$3,'official/hello-world','Hello','active')`, seed.agent, space, seed.tenant)
	exec("issue", `INSERT INTO issues(id,tenant_id,creator_user_id,project_ref,title,number) VALUES($1,$2,$3,$4,'disp issue',1)`, seed.issue, seed.tenant, user, seed.project)
	exec("run", `INSERT INTO issue_runs(id,tenant_id,issue_id,executor_type,executor_id,status) VALUES($1,$2,$3,'agent',$4,'queued')`, seed.run, seed.tenant, seed.issue, seed.agent)
	if busy {
		exec("busy-op", `INSERT INTO operations(id,tenant_id,actor_user_id,project_id,kind,state,step,request,idempotency_key,request_hash) VALUES($1,$2,$3,$4,'start','running','ready','{}','disp-busy','disp-busy-hash')`, uuid.NewString(), seed.tenant, user, seed.project)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("seed commit: %v", err)
	}
	return seed
}

// runClaim reads a run's status/phase directly from PostgreSQL (run() strips the Slice-1 agent
// columns from the public shape, so phase must be read at the storage boundary).
func runClaim(t *testing.T, store *Store, tid, runID string) (status string, phase sql.NullString) {
	t.Helper()
	if err := store.Pool.QueryRow(`SELECT status, phase FROM issue_runs WHERE id=$1 AND tenant_id=$2`, runID, tid).Scan(&status, &phase); err != nil {
		t.Fatalf("read run claim: %v", err)
	}
	return status, phase
}

// TestAgentRunDispatchIdleAccepted is the core Slice 1 obligation: an idle project's queued real-agent
// run is claimed through the seam and advances to status='dispatched', phase='provisioning' in the same
// transaction the seam accepted (atomicity), with the seam observing the caller-owned transaction.
func TestAgentRunDispatchIdleAccepted(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{accepted: true}
	store.AgentRunControlPlane = stub
	seed := seedDispatchScene(t, store.Pool, false)

	if err := store.agentRunDispatcher().Dispatch(context.Background(), seed.run); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if stub.createN != 1 || stub.createTx == nil {
		t.Fatalf("seam must be called once with a transaction: n=%d txNil=%v", stub.createN, stub.createTx == nil)
	}
	if stub.createRun.S("id") != seed.run {
		t.Fatalf("seam received wrong run: %v", stub.createRun)
	}
	status, phase := runClaim(t, store, seed.tenant, seed.run)
	if status != "dispatched" || !phase.Valid || phase.String != "provisioning" {
		t.Fatalf("accepted claim must reach dispatched/provisioning, got status=%s phase=%v", status, phase)
	}
}

// TestAgentRunDispatchBusyStaysQueued: a project with an in-flight operation is busy, so its queued
// run is left untouched — status queued, phase NULL — and the seam is not even consulted (the busy
// precheck short-circuits). No panic, no 409, no provision.
func TestAgentRunDispatchBusyStaysQueued(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{accepted: true}
	store.AgentRunControlPlane = stub
	seed := seedDispatchScene(t, store.Pool, true)

	if err := store.agentRunDispatcher().Dispatch(context.Background(), seed.run); err != nil {
		t.Fatalf("busy Dispatch must not error: %v", err)
	}
	status, phase := runClaim(t, store, seed.tenant, seed.run)
	if status != "queued" || phase.Valid {
		t.Fatalf("busy run must stay queued with phase NULL, got status=%s phase=%v", status, phase)
	}
	if stub.createN != 0 {
		t.Fatalf("busy precheck must not consult the seam, got createN=%d", stub.createN)
	}
}

// TestAgentRunDispatchBusyThenRetryClaims: once the project becomes idle, the B-owned retry loop
// (one DispatchQueuedAgentRunsOnce pass) re-claims the still-queued run to provisioning/dispatched.
func TestAgentRunDispatchBusyThenRetryClaims(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{accepted: true}
	store.AgentRunControlPlane = stub
	seed := seedDispatchScene(t, store.Pool, true)

	if err := store.agentRunDispatcher().Dispatch(context.Background(), seed.run); err != nil {
		t.Fatalf("first Dispatch: %v", err)
	}
	if status, phase := runClaim(t, store, seed.tenant, seed.run); status != "queued" || phase.Valid {
		t.Fatalf("pre-unblock must stay queued, got status=%s phase=%v", status, phase)
	}
	if _, err := store.Pool.Exec(`DELETE FROM operations WHERE project_id=$1 AND tenant_id=$2`, seed.project, seed.tenant); err != nil {
		t.Fatalf("unblock project: %v", err)
	}
	if err := store.DispatchQueuedAgentRunsOnce(context.Background()); err != nil {
		t.Fatalf("retry pass: %v", err)
	}
	status, phase := runClaim(t, store, seed.tenant, seed.run)
	if status != "dispatched" || !phase.Valid || phase.String != "provisioning" {
		t.Fatalf("retry must claim to provisioning, got status=%s phase=%v", status, phase)
	}
	if stub.createN != 1 {
		t.Fatalf("retry pass must consult the seam once, got createN=%d", stub.createN)
	}
}

// TestAgentRunDispatchBusyRace: precheck is only an optimization. When the project is idle at precheck
// but the seam reports Busy (a value, not an error), the run must stay queued with phase NULL and the
// seam call recorded (+1).
func TestAgentRunDispatchBusyRace(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{busy: true}
	store.AgentRunControlPlane = stub
	seed := seedDispatchScene(t, store.Pool, false)

	if err := store.agentRunDispatcher().Dispatch(context.Background(), seed.run); err != nil {
		t.Fatalf("busy-race Dispatch must not error: %v", err)
	}
	if stub.createN != 1 {
		t.Fatalf("busy-race must consult the authoritative seam, got createN=%d", stub.createN)
	}
	status, phase := runClaim(t, store, seed.tenant, seed.run)
	if status != "queued" || phase.Valid {
		t.Fatalf("seam-busy must keep queued with phase NULL, got status=%s phase=%v", status, phase)
	}
}

// TestAgentRunDispatchSeamErrorRollsBack: a genuine seam error rolls back the shared claim transaction,
// surfacing as a Dispatch error while the run stays queued with phase NULL. Nothing is marked failed.
func TestAgentRunDispatchSeamErrorRollsBack(t *testing.T) {
	for _, provider := range []func(*Store){
		func(s *Store) {
			s.AgentRunControlPlane = &stubAgentRunControlPlane{createErr: context.DeadlineExceeded}
		},
		func(s *Store) { s.AgentRunControlPlane = nil }, // Unavailable fail-closed
	} {
		store := dispatcherDB(t)
		provider(store)
		seed := seedDispatchScene(t, store.Pool, false)

		if err := store.agentRunDispatcher().Dispatch(context.Background(), seed.run); err == nil {
			t.Fatalf("seam failure must surface as a Dispatch error")
		}
		status, phase := runClaim(t, store, seed.tenant, seed.run)
		if status != "queued" || phase.Valid {
			t.Fatalf("seam error must keep queued with phase NULL, got status=%s phase=%v", status, phase)
		}
	}
}

// TestAgentRunDispatchReplayIsIdempotent: calling Dispatch twice on the same run claims once and never
// double-declares — the second pass finds the run already provisioned and makes no seam call.
func TestAgentRunDispatchReplayIsIdempotent(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{accepted: true}
	store.AgentRunControlPlane = stub
	seed := seedDispatchScene(t, store.Pool, false)

	if err := store.agentRunDispatcher().Dispatch(context.Background(), seed.run); err != nil {
		t.Fatalf("first Dispatch: %v", err)
	}
	if err := store.agentRunDispatcher().Dispatch(context.Background(), seed.run); err != nil {
		t.Fatalf("replay Dispatch: %v", err)
	}
	if stub.createN != 1 {
		t.Fatalf("replay must not double-declare, got createN=%d", stub.createN)
	}
	status, phase := runClaim(t, store, seed.tenant, seed.run)
	if status != "dispatched" || !phase.Valid || phase.String != "provisioning" {
		t.Fatalf("run must stay claimed, got status=%s phase=%v", status, phase)
	}
}

// TestAgentRunDispatchImmediateRoute claims through the post-commit immediate path (dispatchRun): a real
// Space Agent run must route to AgentRunDispatcher — not the legacy ExecutionDispatcher (nil here) —
// and reach provisioning even though s.Dispatcher is unwired.
func TestAgentRunDispatchImmediateRoute(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{accepted: true}
	store.AgentRunControlPlane = stub
	if store.Dispatcher != nil {
		t.Fatalf("test precondition: legacy Dispatcher must be unwired")
	}
	seed := seedDispatchScene(t, store.Pool, false)

	if err := store.dispatchRun(context.Background(), seed.tenant, seed.run); err != nil {
		t.Fatalf("immediate dispatch: %v", err)
	}
	if stub.createN != 1 {
		t.Fatalf("real-agent run must route to AgentRunDispatcher, got createN=%d", stub.createN)
	}
	status, phase := runClaim(t, store, seed.tenant, seed.run)
	if status != "dispatched" || !phase.Valid || phase.String != "provisioning" {
		t.Fatalf("immediate dispatch must reach provisioning, got status=%s phase=%v", status, phase)
	}
}

// TestAgentRunDispatchScanEligibility is the bounded batch-scan + non-agent-ignore contract: a retry
// pass claims exactly the eligible real-agent queued runs (agent, queued, phase NULL, backed by a
// space_agents row) and leaves team/workflow/fixture-alike/phase-set runs untouched.
func TestAgentRunDispatchScanEligibility(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{accepted: true}
	store.AgentRunControlPlane = stub

	eligible := seedDispatchScene(t, store.Pool, false)

	// A team run whose phase is constrained NULL by the placeholder-Check is not scanned.
	teamRun := uuid.NewString()
	if _, err := store.Pool.Exec(`INSERT INTO issue_runs(id,tenant_id,issue_id,executor_type,executor_id,status) VALUES($1,$2,$3,'team',$4,'queued')`,
		teamRun, eligible.tenant, eligible.issue, uuid.NewString()); err != nil {
		t.Fatalf("seed team run: %v", err)
	}
	// A workflow run is likewise excluded.
	wfRun := uuid.NewString()
	if _, err := store.Pool.Exec(`INSERT INTO issue_runs(id,tenant_id,issue_id,executor_type,executor_id,status) VALUES($1,$2,$3,'workflow',$4,'queued')`,
		wfRun, eligible.tenant, eligible.issue, uuid.NewString()); err != nil {
		t.Fatalf("seed workflow run: %v", err)
	}
	// A real-agent run already past claim (phase set, not NULL) is not re-scanned.
	movedRun := uuid.NewString()
	movedAgent := uuid.NewString()
	if _, err := store.Pool.Exec(`INSERT INTO space_agents(id,space_id,tenant_id,plugin_id,display_name,status) VALUES($1,(SELECT id FROM collab_workspaces WHERE tenant_id=$2 LIMIT 1),$2,'official/moved','Moved','active')`, movedAgent, eligible.tenant); err != nil {
		t.Fatalf("seed moved agent: %v", err)
	}
	if _, err := store.Pool.Exec(`INSERT INTO issue_runs(id,tenant_id,issue_id,executor_type,executor_id,status,phase) VALUES($1,$2,$3,'agent',$4,'running','running')`,
		movedRun, eligible.tenant, eligible.issue, movedAgent); err != nil {
		t.Fatalf("seed moved run: %v", err)
	}
	// A fixture-alike agent run whose executor_id is NOT a space_agents row is not scanned.
	fixtureRun := uuid.NewString()
	if _, err := store.Pool.Exec(`INSERT INTO issue_runs(id,tenant_id,issue_id,executor_type,executor_id,status) VALUES($1,$2,$3,'agent',$4,'queued')`,
		fixtureRun, eligible.tenant, eligible.issue, uuid.NewString()); err != nil {
		t.Fatalf("seed fixture run: %v", err)
	}

	if err := store.DispatchQueuedAgentRunsOnce(context.Background()); err != nil {
		t.Fatalf("retry pass: %v", err)
	}
	// Only the eligible run was claimed (provisioned); everything else stays untouched.
	if status, phase := runClaim(t, store, eligible.tenant, eligible.run); status != "dispatched" || !phase.Valid || phase.String != "provisioning" {
		t.Fatalf("eligible run must be claimed, got status=%s phase=%v", status, phase)
	}
	expected := map[string][2]string{ // runID -> {status, phase-or-empty}
		teamRun:    {"queued", ""},
		wfRun:      {"queued", ""},
		movedRun:   {"running", "running"},
		fixtureRun: {"queued", ""},
	}
	for _, id := range []string{teamRun, wfRun, movedRun, fixtureRun} {
		status, phase := runClaim(t, store, eligible.tenant, id)
		wantStatus, wantPhase := expected[id][0], expected[id][1]
		if status != wantStatus {
			t.Fatalf("run %s must stay %s (scan must not touch it), got status=%s", id, wantStatus, status)
		}
		gotPhase := ""
		if phase.Valid {
			gotPhase = phase.String
		}
		if gotPhase != wantPhase {
			t.Fatalf("run %s phase must stay %q, got %q", id, wantPhase, gotPhase)
		}
	}
	if stub.createN != 1 {
		t.Fatalf("batch scan must claim exactly one run, got createN=%d", stub.createN)
	}
}
