package core

// agentRunSkeletonFields are the B-skeleton-only columns added to issue_runs and
// workspaces by migration 0018. The schema and the D6 business hooks land ahead of
// the feature that owns them (dispatcher, OpenAPI contract, Thread API); until then
// they are deliberately not part of the public resource shape, so they are removed
// before an issue_runs or workspaces resource reaches the API. The AgentRunDispatcher
// that reads and writes them is a later change and reads through the same transaction
// helpers, not through these shaping functions.
var agentRunSkeletonFields = []string{"phase", "workspaceId", "cancelRequestedAt", "threadState", "idleSince", "issueRunId"}

// stripAgentRunSkeleton removes the migration-0018 agent columns from a resource in
// place and returns it, keeping the public contract byte-identical for now.
func stripAgentRunSkeleton(o Object) Object {
	for _, k := range agentRunSkeletonFields {
		delete(o, k)
	}
	return o
}

// run loads a live run scoped to its issue and tenant; foreign/deleted/missing are all 404.
func run(t *transaction, tid, iid, rid string) Object {
	require(validID(rid), 404, "not_found")
	o := t.one("SELECT * FROM issue_runs WHERE id=$1 AND tenant_id=$2 AND issue_id=$3 AND deleted_at IS NULL", rid, tid, iid)
	require(o != nil, 404, "not_found")
	return stripAgentRunSkeleton(o)
}

func runList(t *transaction, r *PublicRequest) Object {
	issue(t, r.TenantID, r.IssueID)
	items := t.list("SELECT * FROM issue_runs WHERE issue_id=$1 AND tenant_id=$2 AND deleted_at IS NULL ORDER BY created_at, id", r.IssueID, r.TenantID)
	for _, o := range items {
		stripAgentRunSkeleton(o)
	}
	return Object{"items": items, "nextCursor": ""}
}

// createRun enqueues a manual IssueRun record. It never dispatches this wave: a manual run persists
// as `queued` (the least misleading behavior). For executor_type='agent' the executor is hardened to
// the same authoritative resolution and snapshot as the comment/@agent path, instead of accepting any
// UUID shape: a real Space Agent is resolved in-transaction against space_agents and its plugin
// identity/version is snapshotted into the run input as authoritative (IssueRun D1/D6); an id that is
// not a real Space Agent falls through to the directory (dev/compat fixtures) or is rejected 404
// without leaking whether it exists. Team/workflow keep the opaque UUID shape, resolved by ports later.
func createRun(t *transaction, r *PublicRequest, uid string) Object {
	i := issue(t, r.TenantID, r.IssueID)
	executorType := r.Body.S("executorType")
	require(executorType == "agent" || executorType == "team" || executorType == "workflow", 400, "invalid_executor")
	executorID := r.Body.S("executorId")
	require(validID(executorID), 400, "invalid_executor")
	input := r.Body.O("input")
	if executorType == "agent" {
		// Authoritative Space Agent (IssueRun D1): reuses the same evidence resolver as the comment
		// path. On success the snapshot overwrites any caller-supplied plugin identity/version so they
		// can never be spoofed (IssueRun D6 authoritative server fields win). Otherwise fall through to
		// the directory exactly like the comment/@agent path; an unresolvable id (retired/foreign) is a
		// 404 with no existence leak.
		if agent, ok := agentRunEvidence(t, r.TenantID, executorID); ok {
			input = snapshotAgentRunInput(input, agent)
		} else {
			resolveCollaborationTarget(t, r.TenantID, "agent", executorID)
		}
	}
	return enqueueRun(t, r.TenantID, i.S("id"), executorType, executorID, input, "", nil, "user", uid)
}
