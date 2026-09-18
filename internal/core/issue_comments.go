package core

// comment loads a live comment scoped to the tenant; foreign/deleted/missing are 404.
func comment(t *transaction, tid, cid string) Object {
	require(validID(cid), 404, "not_found")
	c := t.one("SELECT * FROM issue_comments WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL", cid, tid)
	require(c != nil, 404, "not_found")
	return c
}

func commentList(t *transaction, r *PublicRequest) Object {
	issue(t, r.TenantID, r.IssueID)
	items := t.list("SELECT * FROM issue_comments WHERE issue_id=$1 AND tenant_id=$2 AND deleted_at IS NULL ORDER BY created_at, id", r.IssueID, r.TenantID)
	return Object{"items": items, "nextCursor": ""}
}

func createComment(t *transaction, r *PublicRequest, uid string) Object {
	i := issue(t, r.TenantID, r.IssueID)
	body := validText(r.Body.S("body"), 20000)
	id := newID()
	t.exec("INSERT INTO issue_comments(id,tenant_id,issue_id,author_user_id,body) VALUES($1,$2,$3,$4,$5)", id, r.TenantID, i.S("id"), uid, body)
	return comment(t, r.TenantID, id)
}

func updateComment(t *transaction, r *PublicRequest) Object {
	c := comment(t, r.TenantID, r.CommentID)
	require(c.S("issueId") == r.IssueID, 404, "not_found")
	version(c, r.Body.N("version"))
	body := validText(r.Body.S("body"), 20000)
	t.exec("UPDATE issue_comments SET body=$2,version=version+1,updated_at=now() WHERE id=$1", c.S("id"), body)
	return comment(t, r.TenantID, c.S("id"))
}

func deleteComment(t *transaction, r *PublicRequest) Object {
	c := comment(t, r.TenantID, r.CommentID)
	require(c.S("issueId") == r.IssueID, 404, "not_found")
	version(c, r.Body.N("version"))
	t.exec("UPDATE issue_comments SET deleted_at=now(),version=version+1,updated_at=now() WHERE id=$1", c.S("id"))
	return t.one("SELECT * FROM issue_comments WHERE id=$1 AND tenant_id=$2", c.S("id"), r.TenantID)
}
