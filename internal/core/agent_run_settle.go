package core

import (
	"fmt"
)

// settleRunWorkspace is the B-owned Phase 2A settlement core behind the A→B hook
// RunWorkspaceSettled (controller-integration D6 runWorkspaceSettled, phase 2A).
//
// It runs on the caller-owned *transaction — the run Workspace's create_workspace
// operation terminal transaction — inside which its B transition and the deleting
// declaration must commit or roll back together. It never opens its own transaction,
// spawns a goroutine, or defers a post-commit effect.
//
// Contract (IssueRun D3/D6, plan §18):
//
//	provisioning + no cancel + ready=true   → phase=starting, status stays dispatched
//	provisioning + no cancel + ready=false  → phase=releasing, status=failed, failure_reason=workspace_unavailable (+ DeleteRunWorkspace)
//	provisioning + cancel_requested_at set  → phase=releasing, status=cancelled, result.deliveryState=skipped (+ DeleteRunWorkspace)
//	phase != provisioning                   → stale / replay no-op
//
// A caller-provided run Object carries identity only (id, tenant); every business
// field used for the transition (phase, status, cancel_requested_at, workspace_id)
// is re-read from authoritative issue_runs state in this transaction, never taken
// from the caller Object (plan §3). The Phase 2 state machine is not re-designed
// here; only the committed matrix above is implemented.
//
// The plugin-specific failure (agent_plugin_unavailable) is deliberately NOT produced
// here: the hook carries only a boolean ready and the create_workspace plugin step is
// not wired (G-002), so there is no authoritative evidence to distinguish it. A
// ready=false settlement always maps to the generic workspace_unavailable. Deferred
// to Phase 2B (plan §18.2.6, §8).
func (s *Store) settleRunWorkspace(t *transaction, run Object, ready bool) error {
	// Identity from the caller; the rest of the row is re-read authoritatively below.
	runID := run.S("id")
	if !validID(runID) {
		return fmt.Errorf("runWorkspaceSettled: invalid run id %q", run.S("id"))
	}
	callerTenant := run.S("tenantId")

	o := t.one("SELECT * FROM issue_runs WHERE id=$1 AND deleted_at IS NULL", runID)
	if o == nil {
		// A create_workspace terminal firing for a run that does not exist is an invariant
		// violation, not a replay: returning an error aborts the whole transaction.
		return fmt.Errorf("runWorkspaceSettled: issue_run %s not found", runID)
	}
	if o.S("executorType") != "agent" {
		return fmt.Errorf("runWorkspaceSettled: run %s is executor_type=%q, not agent", runID, o.S("executorType"))
	}
	if callerTenant != "" && callerTenant != o.S("tenantId") {
		return fmt.Errorf("runWorkspaceSettled: run %s tenant mismatch (caller %s, authoritative %s)", runID, callerTenant, o.S("tenantId"))
	}

	// Replay/stale: a settlement arriving after the run already left provisioning is a
	// deterministic no-op. It must not regress phase, return a user-facing 409, create a
	// new delete intent, or enqueue execution (plan §11, matrix "not provisioning").
	if o.S("phase") != "provisioning" {
		return nil
	}

	switch {
	case o.S("cancelRequestedAt") != "":
		// IssueRun D6: cancel during provisioning (no session yet) releases with
		// status=cancelled and deliveryState=skipped, and declares the delete.
		return s.settleCancelled(t, o)
	case ready:
		// Plan §2A.2 + §2.6: the run's creation succeeded at the infra level, but the run is
		// only ready when its *required pinned agent plugin* (IssueRun D1/D6 snapshot key) is
		// installed at the pinned version on the run workspace. A failed or absent-at-version
		// pinned-plugin instance settles agent_plugin_unavailable (plugin-marketplace D3, G-002),
		// even though the operation terminal itself succeeded. Runs whose snapshot pins no agent
		// plugin (legacy engine / other executors) and runs with no instance row yet are treated
		// as ready, exactly as the pre-plugin-engine settle (§2.6 legacy compatibility).
		if inst, req := s.agentPluginInstance(t, o); req && inst != nil &&
			(inst.S("observedState") != "installed" || inst.S("observedVersion") != o.O("input").S("agentPluginVersion")) {
			return s.settleFailed(t, o, "agent_plugin_unavailable")
		}
		// Plan §2A.2: ready settles provisioning → starting, status stays dispatched.
		if s.settleReadyGuard(t, runID) == 0 {
			return nil // stale under the serialized lock; see call site doc
		}
		return nil
	default:
		// ready=false with no cancel: generic create_workspace terminal failure.
		return s.settleFailed(t, o, "workspace_unavailable")
	}
}

// agentPluginInstance resolves the run's required pinned agent plugin, taken from the
// run-create snapshot (issue_runs.input keys agentPluginId / agentPluginVersion, never re-read
// from the current roster — IssueRun D1/D6), to its workspace_plugin_instances row for the run
// workspace, in the caller's transaction. required=false means the run snapshot pins no agent
// plugin (legacy engine or a non-space-agent executor), so the caller must treat the run as
// ready rather than downgrade. The returned instance is nil when the workspace has no row for
// the pinned plugin yet (which the pre-plugin-engine settle treats as ready; G-002 creates the
// row on the plugin step).
func (s *Store) agentPluginInstance(t *transaction, run Object) (inst Object, required bool) {
	pid := run.O("input").S("agentPluginId")
	version := run.O("input").S("agentPluginVersion")
	if pid == "" || version == "" {
		return nil, false
	}
	namespace, identifier, ok := pluginIdentity(pid)
	if !ok {
		return nil, false
	}
	wid := run.S("workspaceId")
	if !validID(wid) {
		return nil, true
	}
	row := t.one("SELECT * FROM workspace_plugin_instances WHERE workspace_id=$1 AND source_namespace=$2 AND identifier=$3", wid, namespace, identifier)
	return row, true
}

// settleReadyGuard is the CAS that moves provisioning → starting. It is guarded by
// phase='provisioning' AND status='dispatched' (plan §2A.2). Returns affected rows;
// under the global advisory lock the SELECT and this CAS are serialized against other
// writers, so after re-reading provisioning an affected count of 0 can only mean the
// caller's evidence raced a legitimate concurrent settle — treated as a stale no-op.
func (s *Store) settleReadyGuard(t *transaction, runID string) int64 {
	return t.execRows(`
		UPDATE issue_runs
		SET phase='starting', version=version+1, updated_at=now()
		WHERE id=$1 AND executor_type='agent' AND phase='provisioning' AND status='dispatched'`, runID)
}

// settleFailed moves a provisioning run to releasing/failed with the given reason and,
// in the same transaction, declares the run Workspace delete via the B→A seam. It is
// shared by the generic workspace failure path; the plugin-specific code is deferred to
// Phase 2B (G-002) because the reason is a parameter supplied by the caller of this
// helper, not guessed here.
func (s *Store) settleFailed(t *transaction, o Object, reason string) error {
	runID := o.S("id")
	if t.execRows(`
		UPDATE issue_runs
		SET phase='releasing', status='failed', failure_reason=$2, completed_at=now(),
		    version=version+1, updated_at=now()
		WHERE id=$1 AND executor_type='agent' AND phase='provisioning' AND status='dispatched'`, runID, reason) == 0 {
		return nil // stale under the serialized lock
	}
	appendActivity(t, o.S("tenantId"), o.S("issueId"), activityActor(o.S("executorType")), o.S("executorId"), "run.failed",
		runActivityDetails(runID, o.S("executorType"), o.S("executorId"), Object{"failureReason": reason}))
	return s.declareDelete(t, o)
}

// settleCancelled moves a provisioning run to releasing/cancelled with
// result.deliveryState=skipped and declares the delete, all in the caller's transaction.
func (s *Store) settleCancelled(t *transaction, o Object) error {
	runID := o.S("id")
	if t.execRows(`
		UPDATE issue_runs
		SET phase='releasing', status='cancelled', completed_at=now(),
		    result = COALESCE(result, '{}'::jsonb) || '{"deliveryState":"skipped"}'::jsonb,
		    version=version+1, updated_at=now()
		WHERE id=$1 AND executor_type='agent' AND phase='provisioning' AND status='dispatched'`, runID) == 0 {
		return nil // stale under the serialized lock
	}
	appendActivity(t, o.S("tenantId"), o.S("issueId"), activityActor(o.S("executorType")), o.S("executorId"), "run.cancelled",
		runActivityDetails(runID, o.S("executorType"), o.S("executorId"), nil))
	return s.declareDelete(t, o)
}

// declareDelete declares the run Workspace's delete_workspace operation through the B→A
// seam DeleteRunWorkspace in the same transaction as the releasing transition, so a
// failure of the delete declaration aborts the whole transaction and the run can never
// be left releasing while the delete intent did not commit (plan §4/§6/§13). The caller
// owns atomicity; this declares, it never executes the delete.
func (s *Store) declareDelete(t *transaction, o Object) error {
	// Reconstruct the run object with authoritative identity/binding for the seam.
	return s.agentRunControlPlane().DeleteRunWorkspace(t, Object{
		"id":          o.S("id"),
		"tenantId":    o.S("tenantId"),
		"workspaceId": o.S("workspaceId"),
	})
}

// businessAgentRunHooks is the B-side AgentRunHooks implementation. RunWorkspaceSettled
// delegates to the Phase 2A settlement core, ThreadEventsTakenOver to the Phase 4B Thread
// takeover core, SessionEnded to the Phase 5 session-terminal core, DeliverySettled to the
// Phase 5 Batch 2 delivery-settlement core and RunWorkspaceDeleted to the Phase 5 Batch 2
// release core. All five hooks are real, so every phase transition the approved IssueRun
// decision defines now has exactly one implementation. NewBusinessAgentRunHooks binds it on the
// Store in production, so the same control-plane transactions that persist A-side evidence drive
// the B-side transitions.
type businessAgentRunHooks struct {
	store *Store
}

// NewBusinessAgentRunHooks returns the production B-side hook set for store, the value
// cmd/server assigns to Store.AgentRunHooks. Every hook runs inside the caller's transaction;
// the returned value holds no state beyond the store it delegates to.
func NewBusinessAgentRunHooks(store *Store) AgentRunHooks {
	return businessAgentRunHooks{store: store}
}

// RunWorkspaceSettled fulfills the A→B hook: it runs the B settlement core in the
// caller-owned transaction.
func (h businessAgentRunHooks) RunWorkspaceSettled(t *transaction, run Object, ready bool) error {
	return h.store.settleRunWorkspace(t, run, ready)
}

// ThreadEventsTakenOver fulfills the A→B hook: it runs the Thread takeover core in the
// caller-owned takeover transaction.
func (h businessAgentRunHooks) ThreadEventsTakenOver(t *transaction, run, execution Object, events []Object) error {
	return h.store.threadEventsTakenOver(t, run, execution, events)
}

// SessionEnded fulfills the A→B hook: it runs the session-terminal core in the caller-owned
// terminal-takeover transaction (Thread `ended`, queued turns `discarded`, run `delivering` and
// the released delivery work item — all one commit).
func (h businessAgentRunHooks) SessionEnded(t *transaction, run, execution, ended Object) error {
	return h.store.sessionEnded(t, run, execution, ended)
}

// DeliverySettled fulfills the A→B hook: it runs the delivery-settlement core in the caller-owned
// terminal-takeover transaction. A failure keeps the run `delivering` and releases D5's backoff
// retry; the two give-up limits release it instead; a `saved`/`unchanged` outcome is refused because
// registering a Revision needs the still-unapproved Cloud Revision decision.
func (h businessAgentRunHooks) DeliverySettled(t *transaction, run, execution Object, result DeliverySettledResult) error {
	return h.store.deliverySettled(t, run, execution, result)
}

// RunWorkspaceDeleted fulfills the A→B hook: it runs the release core in the caller-owned
// delete_workspace terminal transaction, moving the run `releasing → done`.
func (h businessAgentRunHooks) RunWorkspaceDeleted(t *transaction, run Object) error {
	return h.store.runWorkspaceDeleted(t, run)
}

// compile-time guard: businessAgentRunHooks satisfies the AgentRunHooks seam, with every hook of the
// approved lifecycle implemented.
var _ AgentRunHooks = businessAgentRunHooks{}
