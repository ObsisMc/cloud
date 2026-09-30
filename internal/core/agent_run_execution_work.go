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
		// work. Pickup never advances issue_runs phase/status (§18, T3B-10/T3B-11/T3B-12): declaring
		// work is not starting the session — starting arrives only on Phase 4 takeover evidence.
		w := t.one(`
			SELECT * FROM execution_work
			WHERE kind='agent_session' AND execution_id IS NULL AND available_at <= clock_timestamp()
			ORDER BY available_at, created_at, id LIMIT 1`)
		if w == nil {
			return Object{"work": nil}
		}
		return Object{"work": w}
	case "agent_work_dispatch":
		// Registration is a state change, so it is wrapped in the submission identity: a retry of
		// the same submission_id+content replays the recorded response, never a second effect. The
		// authority for idempotency is still the database fence in agentWorkDispatch (D-021).
		return submitted(t, r, func() Object { return agentWorkDispatch(t, r) })
	case "agent_work_get":
		// A replacement Controller recovers a single registered dispatch by its deterministic
		// execution_id to reuse the same execution identity (C2, Phase 4A) instead of registering a
		// second one. Pure read; no lease required to look up the original execution.
		e := t.one("SELECT * FROM node_executions WHERE execution_id=$1", r.Body.S("executionId"))
		require(e != nil, 404, "not_found")
		return e
	case "agent_work_pending":
		// List registered-but-not-yet-finished dispatches, optionally by target Node, for a
		// Controller that crashed after a dispatch commit but before its Node received the command.
		// Pure read; result IS NULL means the execution is still in flight (no termination fact).
		node := r.Body.S("nodeId")
		if node == "" {
			return Object{"executions": t.list("SELECT * FROM node_executions WHERE result IS NULL ORDER BY created_at,execution_id")}
		}
		return Object{"executions": t.list("SELECT * FROM node_executions WHERE result IS NULL AND node_id=$1 ORDER BY created_at,execution_id", node)}
	default:
		reject(404, "not_found")
	}
	return nil
}

// agentWorkDispatch registers execution identity, the target Node and the immutable dispatch input
// before any Node sees the command (D-020/D-021, Phase 4A). It is the execution_work analog of
// cloneDispatch: the Controller supplies execution_id/node/input; A records the node_executions row
// and binds it to the work through execution_work.execution_id. The responsibilities that keep one
// work to at most one execution (D-021) are enforced here and by the schema:
//
//   - work_id is UNIQUE on node_executions, and execution_id is the PK, so the row set itself
//     cannot hold two executions for one work or two works for one execution;
//   - the binding uses a fenced UPDATE (WHERE execution_id IS NULL), so an already-registered work
//     is never overwritten with a different execution;
//   - node must equal the work's target.node_id, and the dispatch input must equal the immutable
//     execution_work.input (canonical json object equality) — a mismatch is a dispatch_conflict
//     invariant error, never a silent override (mandate §8/§12/§13).
//
// A first dispatch inserts the execution row and writes execution_id in one transaction; a replay
// with the same execution_id/node/input is an idempotent success returning the recorded row; a
// different execution id, node or input is a dispatch_conflict that leaves the recorded identity
// and row untouched. Dispatch never writes issue_runs phase/status — registration is not running
// (D-017/D-019): the run stays exactly where RecordDispatch found it.
func agentWorkDispatch(t *transaction, r *ControlRequest) Object {
	workID := r.Body.S("workId")
	execution, node := r.Body.S("executionId"), r.Body.S("nodeId")
	input := r.Body.O("input")
	require(validID(workID) && execution != "" && node != "" && len(input) > 0, 400, "invalid_dispatch")

	work := t.one("SELECT * FROM execution_work WHERE id=$1", workID)
	require(work != nil, 404, "not_found")
	require(work.S("kind") == "agent_session", 409, "dispatch_conflict")
	// The Node must be exactly the one the work item was declared for (D6 WorkTarget), and the
	// dispatch input must match the immutable snapshot the B side declared — never re-resolved.
	require(work.O("target").S("node_id") == node, 409, "dispatch_conflict")
	require(jsonText(work.O("input")) == jsonText(input), 409, "dispatch_conflict")

	// Already registered: replay idempotency vs invariant conflict, decided from the recorded
	// execution — never by overwriting it.
	if registered := work.S("executionId"); registered != "" {
		e := t.one("SELECT * FROM node_executions WHERE work_id=$1", workID)
		require(e != nil, 409, "dispatch_conflict")
		require(e.S("executionId") == execution && e.S("nodeId") == node && jsonText(e.O("input")) == jsonText(input), 409, "dispatch_conflict")
		return e
	}

	// First registration. INSERT + fence must succeed together or roll back together: any failure
	// below panics (reject/require/internal databaseFailure) and the transaction aborts the pending
	// INSERT, so a fence failure can never leave an orphan node_executions row.
	//
	// The global advisory lock on the control transaction serializes writers, so under this control
	// plane the work read above is already authoritative. This guard is belt-and-braces for any
	// external writer (or another Store sharing the lock): if the supplied execution identity was
	// recorded in the meantime, accept it when it is the same work/node/immutable-input (idempotent
	// success, agnostic to our stale read), and conflict only on a genuinely different registration.
	if prior := t.one("SELECT * FROM node_executions WHERE execution_id=$1", execution); prior != nil {
		require(prior.S("workId") == workID && prior.S("nodeId") == node && jsonText(prior.O("input")) == jsonText(input), 409, "dispatch_conflict")
		return prior
	}
	t.execRows(`
		INSERT INTO node_executions(execution_id, kind, operation_id, work_id, node_id, input, dispatched_epoch)
		VALUES($1,$2,$3,$4,$5,$6,$7)`,
		execution, work.S("kind"), work.S("runId"), work.S("id"), node, jsonText(input), r.Body.N("epoch"))
	fenced := t.execRows(`UPDATE execution_work SET execution_id=$2 WHERE id=$1 AND execution_id IS NULL`, workID, execution)
	if fenced == 0 {
		// The slot was registered concurrently; roll the whole dispatch (including the INSERT)
		// back so we never leave a node_execution whose work is not bound to it.
		reject(409, "dispatch_conflict")
	}
	return t.one("SELECT * FROM node_executions WHERE execution_id=$1", execution)
}
