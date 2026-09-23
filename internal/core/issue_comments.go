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
	items := t.list("SELECT * FROM issue_comments WHERE issue_id=$1 AND tenant_id=$2 AND deleted_at IS NULL ORDER BY seq, id", r.IssueID, r.TenantID)
	return Object{"items": items, "nextCursor": ""}
}

func createComment(t *transaction, r *PublicRequest, uid string, dispatches *[]dispatchTarget) Object {
	i := issue(t, r.TenantID, r.IssueID)
	body := validText(r.Body.S("body"), 20000)
	// Thread parent must belong to the same issue and tenant (app-layer check; the self-FK cannot
	// express the same-issue invariant). Replies to deleted comments are rejected this wave.
	var parent any
	if p, _ := r.Body["parentId"].(string); p != "" {
		require(validID(p), 400, "invalid_parent")
		require(t.one("SELECT id FROM issue_comments WHERE id=$1 AND tenant_id=$2 AND issue_id=$3 AND deleted_at IS NULL", p, r.TenantID, i.S("id")) != nil, 404, "parent_not_found")
		parent = p
	}
	id := newID()
	seq := nextTimelineSeq(t, i.S("id"))
	t.exec("INSERT INTO issue_comments(id,tenant_id,issue_id,author_type,author_id,author_user_id,parent_id,body,seq) VALUES($1,$2,$3,'user',$4,$4,$5,$6,$7)", id, r.TenantID, i.S("id"), uid, parent, body, seq)
	// Collaboration targets (@): persist the typed interaction spine and enqueue Task Mode runs.
	applyCommentTargets(t, i, r, uid, id, dispatches)
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
