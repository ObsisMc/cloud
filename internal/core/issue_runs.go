package core

// run loads a live run scoped to its issue and tenant; foreign/deleted/missing are all 404.
func run(t *transaction, tid, iid, rid string) Object {
	require(validID(rid), 404, "not_found")
	o := t.one("SELECT * FROM issue_runs WHERE id=$1 AND tenant_id=$2 AND issue_id=$3 AND deleted_at IS NULL", rid, tid, iid)
	require(o != nil, 404, "not_found")
	return o
}

func runList(t *transaction, r *PublicRequest) Object {
	issue(t, r.TenantID, r.IssueID)
	items := t.list("SELECT * FROM issue_runs WHERE issue_id=$1 AND tenant_id=$2 AND deleted_at IS NULL ORDER BY created_at, id", r.IssueID, r.TenantID)
	return Object{"items": items, "nextCursor": ""}
}

// createRun enqueues a manual IssueRun record. It never dispatches this wave: a manual run persists
// as `queued` (the least misleading behavior). The executor ref is opaque (UUID shape only) — resolved
// by ports later. Task Mode runs (via a comment's @ targets) use enqueueRun directly and are
// dispatched post-commit.
func createRun(t *transaction, r *PublicRequest, uid string) Object {
	i := issue(t, r.TenantID, r.IssueID)
	executorType := r.Body.S("executorType")
	require(executorType == "agent" || executorType == "team" || executorType == "workflow", 400, "invalid_executor")
	executorID := r.Body.S("executorId")
	require(validID(executorID), 400, "invalid_executor")
	return enqueueRun(t, r.TenantID, i.S("id"), executorType, executorID, r.Body.O("input"), "", nil, "user", uid)
}
