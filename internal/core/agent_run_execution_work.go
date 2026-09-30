package core

import (
	"fmt"
	"time"
)

// StoreAgentRunControlPlane is the production A-side control plane wired into a running Store by
// cmd/server. It makes the execution seam (EnqueueExecutionWork) real: it persists an execution_work
// row inside the caller's transaction, backed by the database partial-unique index, so a run's
// AgentSession work is durably declared exactly once (G-001 execution portion, G-008, D-012).
//
// The workspace and thread seams (CreateRunWorkspace/DeleteRunWorkspace/EnqueueThreadCommand) remain
// fail-closed "not implemented" at this slice: Phase 3B deliberately closes the *execution* seam
// (queued → claimable) while the run-Workspace/Thread command paths stay outstanding (G-001 is only
// partially closed here, per the plan's PARTIAL marking). B never writes execution_work directly; the
// business side reaches it only through this seam inside its own transaction, exactly as D6 requires.
type StoreAgentRunControlPlane struct{}

// NewStoreAgentRunControlPlane returns the production A-side execution seam bound to the caller's
// transactions. It is stateless: every method runs on the *transaction the caller owns.
func NewStoreAgentRunControlPlane() *StoreAgentRunControlPlane {
	return &StoreAgentRunControlPlane{}
}

// CreateRunWorkspace declares the run Workspace create intent. Not implemented in Phase 3B: the
// workspace seam is outstanding, so it fails closed (G-001 PARTIAL) rather than fabricate an
// operation the Controller is not wired to execute.
func (StoreAgentRunControlPlane) CreateRunWorkspace(*transaction, Object) (RunWorkspaceOutcome, error) {
	return RunWorkspaceOutcome{}, fmt.Errorf("control-plane seam createRunWorkspace not implemented: A side (execution seam only in Phase 3B)")
}

// DeleteRunWorkspace declares the run Workspace delete intent. Not implemented in Phase 3B (fails
// closed like CreateRunWorkspace).
func (StoreAgentRunControlPlane) DeleteRunWorkspace(*transaction, Object) error {
	return fmt.Errorf("control-plane seam deleteRunWorkspace not implemented: A side (execution seam only in Phase 3B)")
}

// EnqueueExecutionWork persists one AgentSession/delivery work item in the caller's transaction and
// returns its execution_work id (D6 enqueueExecutionWork → WorkAvailable).
//
// Exactly-once identity is the database itself, not an application once-guard: the partial unique
// index execution_work_unregistered_once allows at most one un-registered work per run (D-012,
// G-008). A first enqueue inserts; a replay of the same run/kind/payload is an idempotent success
// returning the *same* row ($21 layer-2=A seam replay, §10) via INSERT ... ON CONFLICT DO NOTHING then
// reading back the existing row. A replay whose payload, target or run Workspace differs is an
// invariant error that rolls the caller's transaction back ($29): the slot is already claimed by a
// genuinely different declaration and must not be silently overwritten. Only a real database failure
// surfaces as an error here; "the A subsystem is unavailable" is not fabricated because persistence
// lives in the caller's own database transaction ($25).
//
// input/target are the fixed snapshots the caller passed; they are stored verbatim and never re-read
// against the current roster (IssueRun D1/D6, §19). availableAt defers eligibility (nil means now).
func (StoreAgentRunControlPlane) EnqueueExecutionWork(t *transaction, run Object, kind string, input, target Object, availableAt *time.Time) (string, error) {
	runID, tenantID, wid := run.S("id"), run.S("tenantId"), run.S("workspaceId")
	if runID == "" || tenantID == "" || wid == "" || len(input) == 0 || len(target) == 0 {
		return "", fmt.Errorf("enqueueExecutionWork: run identity (id/tenantId/workspaceId), input and target are required")
	}
	if kind != "agent_session" && kind != "deliver_revision" {
		return "", fmt.Errorf("enqueueExecutionWork: unsupported kind %q", kind)
	}
	avail := time.Now().UTC()
	if availableAt != nil {
		avail = *availableAt
	}
	id := newID()
	in, tg := jsonText(input), jsonText(target)
	if t.execRows(`
		INSERT INTO execution_work(id, tenant_id, run_id, workspace_id, kind, input, target, available_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (run_id) WHERE execution_id IS NULL DO NOTHING`,
		id, tenantID, runID, wid, kind, in, tg, avail) == 1 {
		return id, nil
	}
	// Conflict: a work item for this run already exists and is un-registered. Recover it; an
	// identical replay is idempotent, a different payload is an invariant error.
	existing := t.one(`SELECT * FROM execution_work WHERE run_id=$1 AND execution_id IS NULL`, runID)
	if existing == nil {
		// Slot freed concurrently (registered between our INSERT and read): do not fabricate a
		// second unregistered work, fail closed so the caller retries/keeps its invariant.
		return "", fmt.Errorf("enqueueExecutionWork: run %s already has a registered execution; cannot declare new unregistered work", runID)
	}
	same := existing.S("kind") == kind &&
		existing.S("workspaceId") == wid &&
		jsonText(existing.O("input")) == in &&
		jsonText(existing.O("target")) == tg
	if !same {
		return "", fmt.Errorf("enqueueExecutionWork conflict for run %s kind %s: declared work already exists with a different payload (%v)", runID, kind, existing.S("id"))
	}
	return existing.S("id"), nil
}

// EnqueueThreadCommand releases a Thread command. Not implemented in Phase 3B (Thread is Phase 4),
// fails closed.
func (StoreAgentRunControlPlane) EnqueueThreadCommand(*transaction, Object, Object) (string, error) {
	return "", fmt.Errorf("control-plane seam enqueueThreadCommand not implemented: A side (Phase 4)")
}

// agentWorkCommand runs the execution_work pickup actions of the internal control contract. Lease
// validity was already checked by Control; the epoch is not recorded here because these are pure
// reads (ownership of an execution moves only when a Controller registers it, Phase 4), so a
// Controller that dies between claim and registration leaves nothing to recover — exactly like
// clone_claim.
func agentWorkCommand(t *transaction, r *ControlRequest) Object {
	switch r.Action {
	case "agent_work_claim":
		// Pure read of the oldest eligible (available_at reached, not yet registered) agent_session
		// work. Pickup never advances issue_runs phase/status ($18, T3B-10/T3B-11/T3B-12): declaring
		// work is not starting the session — starting arrives only on Phase 4 takeover evidence.
		w := t.one(`
			SELECT * FROM execution_work
			WHERE kind='agent_session' AND execution_id IS NULL AND available_at <= clock_timestamp()
			ORDER BY available_at, created_at, id LIMIT 1`)
		if w == nil {
			return Object{"work": nil}
		}
		return Object{"work": w}
	default:
		reject(404, "not_found")
	}
	return nil
}
