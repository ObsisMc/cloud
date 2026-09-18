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

// createRun enqueues an IssueRun record. It never dispatches external execution this wave: there is
// no ExecutionDispatcher, so a manual run simply persists as `queued` (the least misleading behavior
// until dispatch exists). The executor ref is opaque (UUID shape only) — resolved by ports later.
func createRun(t *transaction, r *PublicRequest, uid string) Object {
	i := issue(t, r.TenantID, r.IssueID)
	executorType := r.Body.S("executorType")
	require(executorType == "agent" || executorType == "team" || executorType == "workflow", 400, "invalid_executor")
	executorID := r.Body.S("executorId")
	require(validID(executorID), 400, "invalid_executor")
	// Pending dedup (mirrors the partial unique index; a clean 409 instead of a constraint failure).
	require(t.one("SELECT id FROM issue_runs WHERE issue_id=$1 AND executor_type=$2 AND executor_id=$3 AND status IN ('queued','dispatched') AND deleted_at IS NULL", i.S("id"), executorType, executorID) == nil, 409, "pending_run_exists")
	id := newID()
	t.exec("INSERT INTO issue_runs(id,tenant_id,issue_id,executor_type,executor_id,input,status) VALUES($1,$2,$3,$4,$5,$6,'queued')", id, r.TenantID, i.S("id"), executorType, executorID, jsonText(r.Body.O("input")))
	appendActivity(t, r.TenantID, i.S("id"), "user", uid, "run.enqueued", Object{"runId": id, "executorType": executorType, "executorId": executorID})
	return run(t, r.TenantID, i.S("id"), id)
}
