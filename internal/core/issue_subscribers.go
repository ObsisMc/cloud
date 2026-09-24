package core

// subscriberUser loads an active tenant member by id, used by subscribe and unsubscribe.
func subscriberUser(t *transaction, tid, uid string) Object {
	require(validID(uid), 400, "invalid_user")
	require(t.one("SELECT user_id FROM tenant_memberships WHERE tenant_id=$1 AND user_id=$2 AND status='active'", tid, uid) != nil, 404, "user_not_found")
	u := t.one("SELECT * FROM users WHERE id=$1 AND status='active' AND deleted_at IS NULL", uid)
	require(u != nil, 404, "user_not_found")
	return u
}

func subscriberList(t *transaction, r *PublicRequest) Object {
	issue(t, r.TenantID, r.IssueID)
	items := t.list("SELECT u.* FROM issue_subscribers s JOIN users u ON u.id=s.user_id WHERE s.issue_id=$1 AND s.tenant_id=$2 AND u.deleted_at IS NULL ORDER BY u.display_name, u.id", r.IssueID, r.TenantID)
	return Object{"items": items, "nextCursor": ""}
}

func subscribe(t *transaction, r *PublicRequest) Object {
	issue(t, r.TenantID, r.IssueID)
	u := subscriberUser(t, r.TenantID, r.Body.S("userId"))
	t.exec("INSERT INTO issue_subscribers(issue_id,user_id,tenant_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", r.IssueID, u.S("id"), r.TenantID)
	return u
}

func unsubscribe(t *transaction, r *PublicRequest) Object {
	issue(t, r.TenantID, r.IssueID)
	u := subscriberUser(t, r.TenantID, r.Body.S("userId"))
	t.exec("DELETE FROM issue_subscribers WHERE issue_id=$1 AND user_id=$2", r.IssueID, u.S("id"))
	return u
}
