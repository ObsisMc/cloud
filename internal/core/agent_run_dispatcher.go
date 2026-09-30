package core

import (
	"context"
)

// agentDispatchBatchSize bounds the retry loop's scan so a single tick claims at most this many
// queued AgentRuns. Ordering is deterministic (created_at, id) so every tick drains the oldest first
// without starving; runs beyond the bound are claimed on the next tick.
const agentDispatchBatchSize = 100

// AgentRunDispatcher is the Slice 1 Claim + Busy owner of a queued real Space Agent IssueRun (IssueRun
// D6, B→A). It runs the single short claim transaction that re-validates the run is claimable,
// reflects project busy/idle, and — only when the AgentRunControlPlane seam *accepts* — moves the run
// to status='dispatched', phase='provisioning'. It never creates execution/session/delivery work
// (later slices own the back-half); it only claims. Team, workflow and dev-fixture agent runs keep the
// legacy ExecutionDispatcher.
type AgentRunDispatcher struct {
	store *Store
}

// isSpaceAgentRun reports whether executorID maps to a real Space Agent row in the tenant, across any
// lifecycle status. It is the discriminator between real Space Agent runs (→ AgentRunDispatcher) and
// dev/integration fixture agents (which are never space_agents rows → legacy dispatcher). Callers only
// invoke it for executor_type='agent' runs, whose executor_id passed validID at creation, so the value
// is always a well-formed uuid for the parameterized lookup.
func isSpaceAgentRun(t *transaction, tid, agentID string) bool {
	return t.one("SELECT id FROM space_agents WHERE id=$1 AND tenant_id=$2", agentID, tid) != nil
}

// projectBusy is the non-panicking busy predicate the dispatcher uses (it deliberately never reuses the
// idleProject 409-guard panic, which is reserved for request-time validation). A project is busy when it
// has an operation in flight; the authoritative acceptance nonetheless comes from the seam below, so
// this is only an optimization that avoids a needless seam call when the run's project is known busy.
func projectBusy(t *transaction, pid string) bool {
	return t.one("SELECT id FROM operations WHERE project_id=$1 AND state IN ('queued','running','retry_wait','blocked')", pid) != nil
}

// Dispatch claims a single queued real Space Agent IssueRun. It is the exact beforeEach-safe switch the
// ADR requires:
//
//	queried contract:  executor_type='agent' AND status='queued' AND phase IS NULL, and the executor
//	                    id is a space_agents row in the tenant — otherwise no-op, stays queued.
//	busy precheck:     a best-effort skip (projectBusy). It is never authoritative: the seam's
//	                    Busy/Accepted outcome wins, so a precheck-idle-then-seam-busy race stays queued.
//	seam accept:       only RunWorkspaceOutcome.Accepted advances; Busy commits and stays queued; a
//	                    genuine seam error panics databaseFailure so the shared advisory-lock claim
//	                    transaction rolls back (stay queued) — never marks the run failed (Slice 1).
//	atomicity:         the A-seam declaration and the B-side phase/status transition land in one
//	                    transaction; a seam error rolls back the whole claim with no partial state.
//	CAS/replay-guard:  the phase/status UPDATE matches status='queued' AND phase IS NULL, so a claim
//	                    that lost the race to a concurrent dispatcher (defense-in-depth on top of the
//	                    global advisory tx lock) does not double-transition.
//
// It returns an error only for a real failure (which keeps the run queued); a busy or no-op is nil.
func (d *AgentRunDispatcher) Dispatch(ctx context.Context, runID string) error {
	_, err := d.store.transact(ctx, func(t *transaction) Object {
		o := t.one(`
			SELECT ir.*, iss.project_ref AS project_ref
			FROM issue_runs ir
			LEFT JOIN issues iss ON iss.id = ir.issue_id AND iss.tenant_id = ir.tenant_id
			WHERE ir.id = $1
			  AND ir.executor_type = 'agent'
			  AND ir.status = 'queued'
			  AND ir.phase IS NULL
			  AND ir.deleted_at IS NULL
			  AND EXISTS (
			    SELECT 1 FROM space_agents sa WHERE sa.id = ir.executor_id AND sa.tenant_id = ir.tenant_id
			  )`, runID)
		if o == nil {
			// Not a claimable real Space Agent run: leave it untouched, no claim, no seam call.
			return Object{}
		}
		// Busy precheck — optimization only, never authoritative. An empty/absent project ref has no
		// operation to be busy about, so defer entirely to the seam.
		if pid := o.S("projectRef"); pid != "" && projectBusy(t, pid) {
			return Object{}
		}
		// Authoritative claim: the seam decides. Busy is a value, not an error, so returning normally
		// commits and keeps the run queued for the retry loop.
		out, err := d.store.agentRunControlPlane().CreateRunWorkspace(t, o)
		if err != nil {
			// Real seam failure: roll back the shared claim transaction (run stays queued). Never
			// mark failed in Slice 1; the retry loop resurfaces it.
			panic(databaseFailure{err})
		}
		if out.Busy {
			return Object{}
		}
		if !out.Accepted {
			// The seam neither accepted nor declared busy — the fail-closed default or an undefined
			// outcome. Stay queued rather than advancing on an ambiguity.
			return Object{}
		}
		// Accept → CAS to provisioning/dispatched inside the same transaction as the acceptance.
		// The WHERE guard is single-flight insurance: if a concurrent claim already advanced the run
		// (impossible under the advisory tx lock, but defense-in-depth), we do not double-transition;
		// the seam call already made is idempotent (keyed on run+create_workspace).
		t.execRows(`
			UPDATE issue_runs
			SET phase = 'provisioning', status = 'dispatched', version = version + 1, updated_at = now()
			WHERE id = $1 AND status = 'queued' AND phase IS NULL`, runID)
		return Object{}
	})
	return err
}

// scanQueuedAgentRuns returns the bounded, deterministically ordered ids of claimable real Space Agent
// runs for the retry loop. It is a read-only short scan outside the advisory lock (claim correctness
// lives in each Dispatch transaction), bounded by LIMIT and ordered by (created_at, id). Eligibility
// exactly mirrors Dispatch: executor_type='agent', status='queued', phase IS NULL, and a space_agents
// row backing the executor.
func (s *Store) scanQueuedAgentRuns(ctx context.Context, limit int) ([]string, error) {
	rows, err := s.Pool.QueryContext(ctx, `
		SELECT ir.id FROM issue_runs ir
		WHERE ir.executor_type = 'agent'
		  AND ir.status = 'queued'
		  AND ir.phase IS NULL
		  AND ir.deleted_at IS NULL
		  AND EXISTS (SELECT 1 FROM space_agents sa WHERE sa.id = ir.executor_id AND sa.tenant_id = ir.tenant_id)
		ORDER BY ir.created_at, ir.id
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// DispatchQueuedAgentRunsOnce is one bounded retry-loop pass: scan the eligible queued real Space Agent
// runs (read-only) and claim each in its own short Dispatch transaction, with the sleep between ticks
// owned by the caller. Per-run failures are deliberately not surfaced here — Dispatch already keeps the
// run queued and rolls back on error, and the loop must not flood logs merely because the A seam is
// unwired (Unavailable) or a single run is transiently stuck. Only a scan-level failure cancels the
// pass.
func (s *Store) DispatchQueuedAgentRunsOnce(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	ids, err := s.scanQueuedAgentRuns(ctx, agentDispatchBatchSize)
	if err != nil {
		return err
	}
	for _, id := range ids {
		_ = s.agentRunDispatcher().Dispatch(ctx, id)
	}
	return nil
}
