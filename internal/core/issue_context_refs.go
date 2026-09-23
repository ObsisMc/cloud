package core

var contextRefTypes = map[string]bool{
	"parent_issue": true, "run": true, "timeline_message": true, "pull_request": true,
	"project": true, "workspace": true, "acceptance_criteria": true,
}

func contextRefList(t *transaction, r *PublicRequest) Object {
	issue(t, r.TenantID, r.IssueID)
	items := t.list("SELECT * FROM issue_context_refs WHERE issue_id=$1 AND tenant_id=$2 ORDER BY ref_type, created_at, id", r.IssueID, r.TenantID)
	return Object{"items": items, "nextCursor": ""}
}

// createContextRef stores an opaque pointer to an external resource; nothing is copied or hydrated.
func createContextRef(t *transaction, r *PublicRequest) Object {
	i := issue(t, r.TenantID, r.IssueID)
	refType := r.Body.S("refType")
	require(contextRefTypes[refType], 400, "invalid_ref_type")
	refID := r.Body.S("refId")
	require(validID(refID), 400, "invalid_ref")
	require(t.one("SELECT id FROM issue_context_refs WHERE issue_id=$1 AND ref_type=$2 AND ref_id=$3", i.S("id"), refType, refID) == nil, 409, "context_ref_exists")
	id := newID()
	t.exec("INSERT INTO issue_context_refs(id,tenant_id,issue_id,ref_type,ref_id) VALUES($1,$2,$3,$4,$5)", id, r.TenantID, i.S("id"), refType, refID)
	return t.one("SELECT * FROM issue_context_refs WHERE id=$1", id)
}

// deleteContextRef hard-deletes a reference (join-like rows have no version/soft delete).
func deleteContextRef(t *transaction, r *PublicRequest) Object {
	require(validID(r.ContextRefID), 404, "not_found")
	o := t.one("SELECT * FROM issue_context_refs WHERE id=$1 AND issue_id=$2 AND tenant_id=$3", r.ContextRefID, r.IssueID, r.TenantID)
	require(o != nil, 404, "not_found")
	t.exec("DELETE FROM issue_context_refs WHERE id=$1", o.S("id"))
	return o
}
