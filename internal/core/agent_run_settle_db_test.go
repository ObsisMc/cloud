package core

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// Phase 2A settlement DB tests. They reuse dispatcherDB/seedDispatchScene from the
// dispatcher test file (same package core, fresh isolated PostgreSQL schema per test)
// and drive the B-owned core settleRunWorkspace inside a caller-owned transaction —
// the same white-box reason they live in package core: the hook takes the unexported
// *transaction. The deterministic stubAgentRunControlPlane observes DeleteRunWorkspace.

// stageSettleState rewrites the seeded run's phase/status/cancel_requested_at to the
// exact pre-settle authoritative state a test needs.
func stageSettleState(t *testing.T, store *Store, runID, phase, status string, cancelAt bool) {
	t.Helper()
	q := `UPDATE issue_runs SET phase=$2, status=$3, cancel_requested_at=now(), version=version+1, updated_at=now() WHERE id=$1`
	if !cancelAt {
		q = `UPDATE issue_runs SET phase=$2, status=$3, cancel_requested_at=NULL, version=version+1, updated_at=now() WHERE id=$1`
	}
	if _, err := store.Pool.Exec(q, runID, phase, status); err != nil {
		t.Fatalf("stage settle state (phase=%s status=%s cancel=%v): %v", phase, status, cancelAt, err)
	}
}

// bindRunWorkspace links the run to an existing workspace row (the main workspace the
// seed creates) so tests can verify the settlement never regenerates or overwrites the
// authoritative workspace binding.
func bindRunWorkspace(t *testing.T, store *Store, runID, projectID string) string {
	t.Helper()
	var wid string
	if err := store.Pool.QueryRow(`SELECT id FROM workspaces WHERE project_id=$1 AND kind='main' LIMIT 1`, projectID).Scan(&wid); err != nil {
		t.Fatalf("load main workspace: %v", err)
	}
	if _, err := store.Pool.Exec(`UPDATE issue_runs SET workspace_id=$2, version=version+1, updated_at=now() WHERE id=$1`, runID, wid); err != nil {
		t.Fatalf("bind run workspace: %v", err)
	}
	return wid
}

// settleRun invokes the B-owned settle core inside one caller-owned transaction, rolling
// the whole transaction back (via the databaseFailure control-flow boundary) exactly as
// the future A terminal caller would when the hook returns an error. A nil return means
// the transaction committed.
func settleRun(t *testing.T, store *Store, runID, tenantID string, ready bool, workspaceID string) error {
	t.Helper()
	run := Object{"id": runID, "tenantId": tenantID}
	if workspaceID != "" {
		run["workspaceId"] = workspaceID
	}
	_, err := store.transact(context.Background(), func(tx *transaction) Object {
		if e := store.settleRunWorkspace(tx, run, ready); e != nil {
			panic(databaseFailure{e})
		}
		return Object{}
	})
	return err
}

// runFields reads the durable run columns a settlement test asserts on.
func runFields(t *testing.T, store *Store, runID string) (phase sql.NullString, status, failureReason string, result, workspaceID, cancelAt sql.NullString) {
	t.Helper()
	err := store.Pool.QueryRow(
		`SELECT phase, status, failure_reason, result, workspace_id, cancel_requested_at FROM issue_runs WHERE id=$1`,
		runID,
	).Scan(&phase, &status, &failureReason, &result, &workspaceID, &cancelAt)
	if err != nil {
		t.Fatalf("read run fields: %v", err)
	}
	return phase, status, failureReason, result, workspaceID, cancelAt
}

// countRunActivities reports how many issue_activities of the given action exist for the run's issue.
func countRunActivities(t *testing.T, store *Store, issueID, action string) int {
	t.Helper()
	var n int
	if err := store.Pool.QueryRow(`SELECT count(*) FROM issue_activities WHERE issue_id=$1 AND action=$2`, issueID, action).Scan(&n); err != nil {
		t.Fatalf("count activities %s: %v", action, err)
	}
	return n
}

// TestPhase2ASettleReady (T2-1): an idle provisioning/dispatched run with no cancel and
// ready=true settles provisioning → starting, status stays dispatched, no delete declared.
func TestPhase2ASettleReady(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{accepted: true}
	store.AgentRunControlPlane = stub
	seed := seedDispatchScene(t, store.Pool, false)
	stageSettleState(t, store, seed.run, "provisioning", "dispatched", false)

	if err := settleRun(t, store, seed.run, seed.tenant, true, ""); err != nil {
		t.Fatalf("ready settle: %v", err)
	}
	phase, status, reason, _, _, _ := runFields(t, store, seed.run)
	if !phase.Valid || phase.String != "starting" {
		t.Fatalf("ready settle must reach starting, got phase=%v", phase)
	}
	if status != "dispatched" {
		t.Fatalf("ready settle must keep status=dispatched, got %q", status)
	}
	if reason != "" {
		t.Fatalf("ready settle must not set a failure_reason, got %q", reason)
	}
	if stub.deleteN != 0 {
		t.Fatalf("ready settle must not declare delete, got deleteN=%d", stub.deleteN)
	}
}

// TestPhase2ASettleWorkspaceFailure (T2-2): ready=false with no cancel settles
// provisioning → releasing/failed/workspace_unavailable and declares delete once.
func TestPhase2ASettleWorkspaceFailure(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{accepted: true}
	store.AgentRunControlPlane = stub
	seed := seedDispatchScene(t, store.Pool, false)
	stageSettleState(t, store, seed.run, "provisioning", "dispatched", false)

	if err := settleRun(t, store, seed.run, seed.tenant, false, ""); err != nil {
		t.Fatalf("failure settle: %v", err)
	}
	phase, status, reason, _, _, _ := runFields(t, store, seed.run)
	if !phase.Valid || phase.String != "releasing" {
		t.Fatalf("failure settle must reach releasing, got phase=%v", phase)
	}
	if status != "failed" {
		t.Fatalf("failure settle must set status=failed, got %q", status)
	}
	if reason != "workspace_unavailable" {
		t.Fatalf("failure settle must set failure_reason=workspace_unavailable, got %q", reason)
	}
	if stub.deleteN != 1 {
		t.Fatalf("failure settle must declare delete once, got deleteN=%d", stub.deleteN)
	}
	if stub.deleteRun.S("id") != seed.run {
		t.Fatalf("delete must be declared for the settled run, got %v", stub.deleteRun)
	}
	if n := countRunActivities(t, store, seed.issue, "run.failed"); n != 1 {
		t.Fatalf("failure settle must append one run.failed activity, got %d", n)
	}
}

// TestPhase2ASettleHookRollback (T2-4): when the DeleteRunWorkspace seam returns an error,
// the whole caller transaction rolls back — the run stays provisioning/dispatched, no
// failure activity commits, and the delete intent has no durable effect.
func TestPhase2ASettleHookRollback(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{accepted: true, deleteErr: context.DeadlineExceeded}
	store.AgentRunControlPlane = stub
	seed := seedDispatchScene(t, store.Pool, false)
	stageSettleState(t, store, seed.run, "provisioning", "dispatched", false)

	if err := settleRun(t, store, seed.run, seed.tenant, false, ""); err == nil {
		t.Fatalf("delete seam error must surface as a settle error (rollback)")
	}
	phase, status, reason, _, _, _ := runFields(t, store, seed.run)
	if !phase.Valid || phase.String != "provisioning" {
		t.Fatalf("rollback must keep the run provisioning, got phase=%v", phase)
	}
	if status != "dispatched" {
		t.Fatalf("rollback must keep status=dispatched, got %q", status)
	}
	if reason != "" {
		t.Fatalf("rollback must not persist a failure_reason, got %q", reason)
	}
	if n := countRunActivities(t, store, seed.issue, "run.failed"); n != 0 {
		t.Fatalf("rollback must not persist a run.failed activity, got %d", n)
	}
}

// TestPhase2ASettleReplayReady (T2-5): settling ready twice claims once — the second call
// finds the run already starting and is a no-op, with no delete and no side effect.
func TestPhase2ASettleReplayReady(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{accepted: true}
	store.AgentRunControlPlane = stub
	seed := seedDispatchScene(t, store.Pool, false)
	stageSettleState(t, store, seed.run, "provisioning", "dispatched", false)

	if err := settleRun(t, store, seed.run, seed.tenant, true, ""); err != nil {
		t.Fatalf("first ready settle: %v", err)
	}
	if err := settleRun(t, store, seed.run, seed.tenant, true, ""); err != nil {
		t.Fatalf("replay ready settle must be a clean no-op: %v", err)
	}
	phase, status, _, _, _, _ := runFields(t, store, seed.run)
	if !phase.Valid || phase.String != "starting" || status != "dispatched" {
		t.Fatalf("replay ready must keep starting/dispatched, got phase=%v status=%q", phase, status)
	}
	if stub.deleteN != 0 {
		t.Fatalf("replay ready must not declare delete, got deleteN=%d", stub.deleteN)
	}
}

// TestPhase2ASettleReplayFailure (T2-6): settling failure twice declares delete once — the
// second call finds the run already releasing and is a no-op.
func TestPhase2ASettleReplayFailure(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{accepted: true}
	store.AgentRunControlPlane = stub
	seed := seedDispatchScene(t, store.Pool, false)
	stageSettleState(t, store, seed.run, "provisioning", "dispatched", false)

	if err := settleRun(t, store, seed.run, seed.tenant, false, ""); err != nil {
		t.Fatalf("first failure settle: %v", err)
	}
	if err := settleRun(t, store, seed.run, seed.tenant, false, ""); err != nil {
		t.Fatalf("replay failure settle must be a clean no-op: %v", err)
	}
	phase, status, reason, _, _, _ := runFields(t, store, seed.run)
	if !phase.Valid || phase.String != "releasing" || status != "failed" || reason != "workspace_unavailable" {
		t.Fatalf("replay failure must keep releasing/failed/workspace_unavailable, got phase=%v status=%q reason=%q", phase, status, reason)
	}
	if stub.deleteN != 1 {
		t.Fatalf("replay failure must declare delete exactly once, got deleteN=%d", stub.deleteN)
	}
}

// TestPhase2ASettleStaleCallback (T2-7): a settlement arriving when the run is already
// starting or releasing is a deterministic no-op — the state is unchanged and no delete
// intent is created.
func TestPhase2ASettleStaleCallback(t *testing.T) {
	for _, tc := range []struct {
		name        string
		phase, stat string
		ready       bool
	}{
		{"starting", "starting", "dispatched", true},
		{"releasing", "releasing", "failed", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := dispatcherDB(t)
			stub := &stubAgentRunControlPlane{accepted: true}
			store.AgentRunControlPlane = stub
			seed := seedDispatchScene(t, store.Pool, false)
			stageSettleState(t, store, seed.run, tc.phase, tc.stat, false)

			if err := settleRun(t, store, seed.run, seed.tenant, tc.ready, ""); err != nil {
				t.Fatalf("stale settle must be a clean no-op: %v", err)
			}
			gotPhase, status, _, _, _, _ := runFields(t, store, seed.run)
			if !gotPhase.Valid || gotPhase.String != tc.phase || status != tc.stat {
				t.Fatalf("stale settle must not move the run, got phase=%v status=%q (want %s/%s)", gotPhase, status, tc.phase, tc.stat)
			}
			if stub.deleteN != 0 {
				t.Fatalf("stale settle must not declare delete, got deleteN=%d", stub.deleteN)
			}
		})
	}
}

// TestPhase2ASettleCancelBeforeSettle (T2-8): a provisioning run with cancel_requested_at
// set settles to releasing/cancelled with result.deliveryState=skipped and declares delete
// even when the caller passes ready=true — cancel wins over ready (IssueRun D6).
func TestPhase2ASettleCancelBeforeSettle(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{accepted: true}
	store.AgentRunControlPlane = stub
	seed := seedDispatchScene(t, store.Pool, false)
	stageSettleState(t, store, seed.run, "provisioning", "dispatched", true)

	if err := settleRun(t, store, seed.run, seed.tenant, true, ""); err != nil {
		t.Fatalf("cancel settle with ready=true must not error: %v", err)
	}
	phase, status, reason, result, _, _ := runFields(t, store, seed.run)
	if !phase.Valid || phase.String != "releasing" {
		t.Fatalf("cancel-before-settle must reach releasing (not starting), got phase=%v", phase)
	}
	if status != "cancelled" {
		t.Fatalf("cancel-before-settle must set status=cancelled, got %q", status)
	}
	if reason != "" {
		t.Fatalf("cancel-before-settle must not set a failure_reason, got %q", reason)
	}
	if !result.Valid || !strings.Contains(result.String, "deliveryState") || !strings.Contains(result.String, "skipped") {
		t.Fatalf("cancel-before-settle must set result.deliveryState=skipped, got %v", result)
	}
	if stub.deleteN != 1 {
		t.Fatalf("cancel-before-settle must declare delete once, got deleteN=%d", stub.deleteN)
	}
	if n := countRunActivities(t, store, seed.issue, "run.cancelled"); n != 1 {
		t.Fatalf("cancel-before-settle must append one run.cancelled activity, got %d", n)
	}
}

// TestPhase2ASettleWorkspaceBinding (T2-9): the settlement never regenerates or overwrites
// the authoritative workspace binding. A caller-provided workspaceId (even a conflicting
// one, the caller not being authoritative) is ignored; the run keeps its bound workspace
// and the delete declaration carries the authoritative workspaceId.
func TestPhase2ASettleWorkspaceBinding(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{accepted: true}
	store.AgentRunControlPlane = stub
	seed := seedDispatchScene(t, store.Pool, false)
	wid := bindRunWorkspace(t, store, seed.run, seed.project)
	stageSettleState(t, store, seed.run, "provisioning", "dispatched", false)

	// A conflicting caller-supplied workspace id must not change the authoritative binding.
	mismatch := "00000000-0000-0000-0000-000000000000"
	if err := settleRun(t, store, seed.run, seed.tenant, false, mismatch); err != nil {
		t.Fatalf("failure settle with mismatched caller workspace: %v", err)
	}
	_, _, _, _, bound, _ := runFields(t, store, seed.run)
	if !bound.Valid || bound.String != wid {
		t.Fatalf("settlement must not overwrite the authoritative workspace binding, got %v (want %s)", bound, wid)
	}
	if stub.deleteN != 1 || stub.deleteRun.S("workspaceId") != wid {
		t.Fatalf("delete declaration must carry the authoritative workspaceId, got run=%v", stub.deleteRun)
	}
}

// TestPhase2ASettleAtomicity (T2-10): within a single transaction, a simulated A-side
// terminal mutation and the B transition must commit or roll back together. When the
// DeleteRunWorkspace seam errors, both the A mutation and the B transition are rolled back.
func TestPhase2ASettleAtomicity(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{accepted: true, deleteErr: context.DeadlineExceeded}
	store.AgentRunControlPlane = stub
	seed := seedDispatchScene(t, store.Pool, false)
	stageSettleState(t, store, seed.run, "provisioning", "dispatched", false)

	// One transaction: a simulated A terminal write (dispatched_at is A-ish and NULL on a
	// seeded provisioning run) followed by the B settle with a failing delete seam.
	_, err := store.transact(context.Background(), func(tx *transaction) Object {
		tx.exec(`UPDATE issue_runs SET dispatched_at=now(), version=version+1, updated_at=now() WHERE id=$1`, seed.run)
		if e := store.settleRunWorkspace(tx, Object{"id": seed.run, "tenantId": seed.tenant}, false); e != nil {
			panic(databaseFailure{e})
		}
		return Object{}
	})
	if err == nil {
		t.Fatalf("delete seam error must abort the shared A/B transaction")
	}
	// Both the A terminal write and the B transition must be rolled back together.
	var dispatchedAt sql.NullString
	phase, status, reason, _, _, _ := runFields(t, store, seed.run)
	if err := store.Pool.QueryRow(`SELECT dispatched_at FROM issue_runs WHERE id=$1`, seed.run).Scan(&dispatchedAt); err != nil {
		t.Fatalf("read dispatched_at: %v", err)
	}
	if !phase.Valid || phase.String != "provisioning" || status != "dispatched" || reason != "" {
		t.Fatalf("A/B rollback must leave run provisioning/dispatched with no reason, got phase=%v status=%q reason=%q", phase, status, reason)
	}
	if dispatchedAt.Valid {
		t.Fatalf("A/B rollback must undo the simulated A terminal write, dispatched_at still set: %v", dispatchedAt.String)
	}
	if n := countRunActivities(t, store, seed.issue, "run.failed"); n != 0 {
		t.Fatalf("A/B rollback must not persist a run.failed activity, got %d", n)
	}
}

// TestPhase2ASettleInvariantErrors: genuine invariant violations abort the transaction —
// a settlement for a nonexistent run and for a non-agent run both surface as errors that
// roll back the caller-owned transaction.
func TestPhase2ASettleInvariantErrors(t *testing.T) {
	store := dispatcherDB(t)
	store.AgentRunControlPlane = &stubAgentRunControlPlane{accepted: true}
	seed := seedDispatchScene(t, store.Pool, false)
	stageSettleState(t, store, seed.run, "provisioning", "dispatched", false)

	// Nonexistent run id (still a well-formed uuid) → invariant error.
	if err := settleRun(t, store, newID(), seed.tenant, true, ""); err == nil {
		t.Fatalf("settle for a nonexistent run must return an invariant error")
	}
	// A team run settling as if it were an Agent run → invariant error.
	teamRun := newID()
	if _, err := store.Pool.Exec(
		`INSERT INTO issue_runs(id,tenant_id,issue_id,executor_type,executor_id,status) VALUES($1,$2,$3,'team',$4,'queued')`,
		teamRun, seed.tenant, seed.issue, newID(),
	); err != nil {
		t.Fatalf("seed team run: %v", err)
	}
	if err := settleRun(t, store, teamRun, seed.tenant, true, ""); err == nil {
		t.Fatalf("settle for a non-agent run must return an invariant error")
	}

	// The rejected invariances must not touch the valid run.
	phase, status, _, _, _, _ := runFields(t, store, seed.run)
	if !phase.Valid || phase.String != "provisioning" || status != "dispatched" {
		t.Fatalf("invariant-error settlements must not touch the valid run, got phase=%v status=%q", phase, status)
	}
}

// businessAgentRunHooks must satisfy the seam and delegate RunWorkspaceSettled to the core. Since
// Phase 5 Batch 2 every hook of the approved lifecycle is real, so the fail-closed half of this test
// is now about the seam's own input contract rather than about a placeholder: a hook handed a caller
// Object with no usable identity refuses it instead of projecting the settlement onto some run, and
// UnavailableAgentRunHooks still fails closed for a deployment that wired no hooks at all.
func TestPhase2ABusinessHooksDelegateAndFailClosed(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{accepted: true}
	store.AgentRunControlPlane = stub
	seed := seedDispatchScene(t, store.Pool, false)
	stageSettleState(t, store, seed.run, "provisioning", "dispatched", false)

	hooks := businessAgentRunHooks{store: store}
	_, err := store.transact(context.Background(), func(tx *transaction) Object {
		if e := hooks.RunWorkspaceSettled(tx, Object{"id": seed.run, "tenantId": seed.tenant}, true); e != nil {
			panic(databaseFailure{e})
		}
		return Object{}
	})
	if err != nil {
		t.Fatalf("RunWorkspaceSettled via businessAgentRunHooks: %v", err)
	}
	if phase, _, _, _, _, _ := runFields(t, store, seed.run); !phase.Valid || phase.String != "starting" {
		t.Fatalf("business hooks must settle to starting, got phase=%v", phase)
	}
	// A real hook refuses a caller Object that names no run, so a mis-addressed takeover can never be
	// settled onto an arbitrary run.
	if e := hooks.ThreadEventsTakenOver(nil, Object{}, Object{}, nil); e == nil {
		t.Fatalf("threadEventsTakenOver must fail closed on a caller Object with no run identity")
	}
	unavailable := UnavailableAgentRunHooks{}
	if e := unavailable.RunWorkspaceSettled(nil, Object{}, true); e == nil {
		t.Fatalf("UnavailableAgentRunHooks.RunWorkspaceSettled must fail closed")
	}
}
