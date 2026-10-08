package core

import "fmt"

// The B-owned release half of an Agent IssueRun: `delivering → releasing` with D4's final status,
// and `releasing → done` once the run Workspace's delete has been taken over
// (IssueRun D3/D4/D5, controller-integration D6 deliverRevision/RunWorkspaceDeleted; plan §5 Batch 2).
//
// Two transitions, one shape: each is a CAS on the phase the caller re-read, and each declares or
// completes the run Workspace's delete in the same transaction that commits the phase write, so the
// run can never be `releasing` without a durable delete intent, nor `done` without a succeeded
// delete. Neither transition writes anything else about the run: `releasing` sets `status` exactly
// once from the session end reason (D4), and `done` deliberately changes no business outcome —
// "终态不变" in D3's own words, which is why `done` carries no separate status of its own and why
// `done` is never a synonym for `completed` (mandate §15).

// runSessionExecution resolves the run's settled agent_session execution: the execution whose durable
// result carries the session end reason D4 derives the final status from, and the one the delivery
// spec names as `session_execution_id`. There is at most one per run — one `agent_session` work item
// gives at most one execution (D-021) — and `result IS NOT NULL` is what makes it "settled" rather
// than in flight. nil means the run never had a session terminal fact, which no caller of this file
// can legitimately be looking at.
func runSessionExecution(t *transaction, runID string) Object {
	return t.one(`
		SELECT e.execution_id, e.node_id, e.result FROM node_executions e
		JOIN execution_work w ON w.id = e.work_id
		WHERE w.run_id=$1 AND w.kind='agent_session' AND e.result IS NOT NULL
		ORDER BY e.created_at DESC, e.execution_id DESC LIMIT 1`, runID)
}

// runReleaseStatus maps the session end reason to the IssueRun `status` D4 derives from it. The three
// groups are exactly D4's table: a conversation the user ended or that went idle is `completed`; a
// cancelled run is `cancelled`; an Agent failure, a Node interruption and an unknown session outcome
// are all `failed`. Anything outside the closed set is a caller error — the reasons are validated
// before this is reached, and guessing a status for an unknown reason would silently invent a
// business outcome.
func runReleaseStatus(reason string) string {
	switch reason {
	case "user_ended", "idle_timeout":
		return "completed"
	case "cancelled":
		return "cancelled"
	case "agent_failed", "interrupted":
		return "failed"
	default:
		return ""
	}
}

// releaseAfterDelivery moves a `delivering` run to `releasing` with the given deliveryState — and,
// when the delivery succeeded, the Revision it registered — and declares the run Workspace's delete
// in the same transaction (IssueRun D3/D5, operation D4; Cloud Revision D4).
//
// It is the only writer of `releasing` on the delivery path, and it is reached from exactly three
// approved places in DeliverySettled: a verified delivery (`saved`/`unchanged`, with the Revision id
// the control plane committed just before the hook ran), D5's give-up on a delivery that kept failing
// (`failed`, with no Revision), and a delivery whose declared objects Cloud could not confirm, which
// settles as `failed` under the same D5 rules. The `status` it writes is derived from the session's
// own end reason, so the business outcome ("how did the Agent run end") stays independent of the
// delivery outcome ("was the Revision saved"), which IssueRun invariant 4 requires: a `completed` run
// may legitimately carry `deliveryState=failed`.
//
// What is deliberately NOT here: the Agent-authored reply comment D4 asks for on `completed`. D4 says
// Cloud writes "Thread 中 Agent 的最后一条消息" as a reply comment, but that text lives inside a
// `SessionUpdate` record — a business payload owned by desktop `ora-history` — and Thread D2 forbids
// Cloud from parsing business fields ("Cloud 不解析业务字段") while Thread invariant 6 forbids
// rewriting Node record content. There is no approved field path to read the message text from, so
// writing a comment would mean guessing a foreign schema. The conflict is registered as a gap in the
// plan rather than resolved by a silent choice (mandate §1: ADR over plan; register, do not choose).
func (s *Store) releaseAfterDelivery(t *transaction, o Object, state, revisionID string) error {
	runID := o.S("id")
	sess := runSessionExecution(t, runID)
	if sess == nil {
		return fmt.Errorf("release agent run: run %s has no settled agent_session execution", runID)
	}
	reason := sess.O("result").S("reason")
	status := runReleaseStatus(reason)
	if status == "" {
		return fmt.Errorf("release agent run: run %s session execution %s carries end reason %q, outside the closed set", runID, sess.S("executionId"), reason)
	}
	// One CAS for the phase and D4's status together, so they cannot in principle happen apart. The
	// source phase is `delivering` and nothing else: the other two ways into `releasing` (the
	// provisioning failure and the no-session cancel) already ran their own write, and a second
	// entry from here would be a second authority over the same transition.
	//
	// `result` gains `revisionId` beside the deliveryState: D4 spells the run's result as
	// `{revisionId | null, deliveryState}`, and an absent key would be indistinguishable from a
	// Revision whose id Cloud failed to record. A delivery that saved nothing writes an explicit JSON
	// null, so "no Revision" is a value rather than a missing field.
	//
	// Both parameters are cast explicitly because `jsonb_build_object` is variadic `"any"`: the server
	// cannot infer a bare parameter's type from it, so `$3` alone is rejected at parse time with
	// SQLSTATE 42P18. The `revisionId` cast also makes the empty string mean NULL, which is what a
	// failed delivery passes: `NULLIF($4::text,'')::uuid` cannot turn a Revision id into an invalid
	// uuid, because the only other value ever passed is one the control plane just generated.
	if t.execRows(`
		UPDATE issue_runs
		SET phase='releasing', status=$2, completed_at=now(),
		    result = COALESCE(result, '{}'::jsonb) || jsonb_build_object('revisionId', NULLIF($4::text,'')::uuid, 'deliveryState', $3::text),
		    version=version+1, updated_at=now()
		WHERE id=$1 AND executor_type='agent' AND deleted_at IS NULL AND phase='delivering'`,
		runID, status, state, revisionID) != 1 {
		now := t.one("SELECT phase, status FROM issue_runs WHERE id=$1", runID)
		return fmt.Errorf("release agent run: run %s was not moved to releasing (phase=%q status=%q)", runID, now.S("phase"), now.S("status"))
	}
	// D4's Timeline activity, in the same transaction as the status it reports. The details carry the
	// two independent outcomes so the timeline can explain a `completed` run whose Revision was not
	// saved, which is the pairing D5's give-up produces.
	appendActivity(t, o.S("tenantId"), o.S("issueId"), activityActor(o.S("executorType")), o.S("executorId"), "run."+status,
		runActivityDetails(runID, o.S("executorType"), o.S("executorId"),
			Object{"sessionEndReason": reason, "deliveryState": state}))
	// The delete declaration goes through the A seam in this transaction: a seam failure rolls the
	// releasing transition back, so a run is never releasing without its delete intent (§12/§17).
	return s.declareDelete(t, o)
}

// deliveryGivenUp reports whether D5's give-up limits have been exceeded for a run still in
// `delivering`, which is what turns an unending delivery into a released run:
//
//	continuous failure:  more than `DeliveryGiveUpAfter` (D5's first-version default 2 h) has passed
//	                     since the run's FIRST delivery work item was declared. While the run is
//	                     `delivering` every attempt has failed — a success would have left the phase —
//	                     so that instant is the start of the unbroken failure run D5 measures.
//	unreachable Workspace: the run's Workspace has no live sandbox carrying a connected Node seen
//	                     within `DeliveryUnreachableAfter` (D5's default 30 min). A Workspace whose
//	                     Node's state has been unknown that long cannot deliver, so waiting out the
//	                     full failure window would only hold the sandbox longer.
//
// Both comparisons are made on database time, and both limits are judged by the caller's configured
// values. A limit that was never configured (zero or negative) fails closed: it is treated as "no
// give-up on this condition" rather than as an elapsed zero-length window, which would abandon every
// delivery the moment it was declared. The caller is a background pass with no client to tell, so
// giving up nothing is the safe answer.
func (s *Store) deliveryGivenUp(t *transaction, o Object) bool {
	runID, wid := o.S("id"), o.S("workspaceId")
	if s.DeliveryGiveUpAfter > 0 && t.one(`
		SELECT id FROM execution_work
		WHERE run_id=$1 AND kind='deliver_revision' AND created_at < now() - make_interval(secs => $2)
		LIMIT 1`, runID, s.DeliveryGiveUpAfter.Seconds()) != nil {
		return true
	}
	if s.DeliveryUnreachableAfter > 0 && validID(wid) {
		return t.one(`
			SELECT w.id FROM workspaces w
			WHERE w.id=$1 AND w.issue_run_id=$2
			  AND NOT EXISTS (
			    SELECT 1 FROM sandbox_instances sb
			    JOIN node_instances n ON n.sandbox_instance_id = sb.id
			    WHERE sb.workspace_id = w.id AND sb.terminated_at IS NULL
			      AND n.ended_at IS NULL AND n.connection_state='connected'
			      AND n.last_seen_at > clock_timestamp() - make_interval(secs => $3))`,
			wid, runID, s.DeliveryUnreachableAfter.Seconds()) != nil
	}
	return false
}

// runWorkspaceDeleted is the B-owned core behind the A→B hook RunWorkspaceDeleted
// (controller-integration D6 runWorkspaceDeleted, IssueRun D3; plan §5 Batch 2).
//
// It runs on the caller-owned *transaction in which the run Workspace's delete_workspace operation
// was written `succeeded`, and it is the last transition of the run's lifecycle: `releasing → done`.
// Nothing else changes — D3's table gives `done` no status of its own ("终态不变"), so whatever D4
// derived when the run entered `releasing` stands, and a `done` run may be `completed`, `cancelled`
// or `failed` depending on how the Agent's session ended rather than on the delete succeeding.
//
// A run already `done` is a deterministic no-op: the operation's terminal state is a fact that may be
// replayed, and settling it twice must not bump the version or write a second business fact
// (mandate §19). Any other phase is an invariant contradiction — the delete is declared only from
// `releasing`, so no other phase can have a succeeded delete to settle.
func (s *Store) runWorkspaceDeleted(t *transaction, run Object) error {
	runID := run.S("id")
	if !validID(runID) {
		return fmt.Errorf("runWorkspaceDeleted: invalid run id %q", run.S("id"))
	}
	o := t.one("SELECT * FROM issue_runs WHERE id=$1 AND deleted_at IS NULL", runID)
	if o == nil {
		return fmt.Errorf("runWorkspaceDeleted: issue_run %s not found", runID)
	}
	if o.S("executorType") != "agent" {
		return fmt.Errorf("runWorkspaceDeleted: run %s is executor_type=%q, not agent", runID, o.S("executorType"))
	}
	switch o.S("phase") {
	case "done":
		return nil
	case "releasing":
		if t.execRows(`
			UPDATE issue_runs SET phase='done', version=version+1, updated_at=now()
			WHERE id=$1 AND executor_type='agent' AND phase='releasing'`, runID) != 1 {
			return fmt.Errorf("runWorkspaceDeleted: run %s was not moved to done from releasing", runID)
		}
		return nil
	default:
		return fmt.Errorf("runWorkspaceDeleted: run %s is phase=%q, not releasing or done", runID, o.S("phase"))
	}
}
