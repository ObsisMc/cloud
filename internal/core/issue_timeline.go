package core

// timelineList returns the issue Timeline: comments and activities merged into one uniform entry per
// row, ordered by the shared per-issue seq (not created_at). Each entry carries kind (comment vs
// activity), author ActorRef, the comment body or activity action/details, and the parent reference;
// run-bearing activities keep the run id inside details.runId. This is a high-level projection, not
// an execution log and not an event source.
func timelineList(t *transaction, r *PublicRequest) Object {
	issue(t, r.TenantID, r.IssueID)
	items := t.list(`
		SELECT 'comment'::text AS kind, c.id, c.seq, c.created_at, c.author_type, c.author_id, c.author_user_id, c.body, c.parent_id, NULL::text AS action, NULL::jsonb AS details
		FROM issue_comments c
		WHERE c.issue_id=$1 AND c.tenant_id=$2 AND c.deleted_at IS NULL
		UNION ALL
		SELECT 'activity'::text AS kind, a.id, a.seq, a.created_at, a.actor_type AS author_type, a.actor_id AS author_id, NULL::uuid AS author_user_id, NULL::text AS body, NULL::uuid AS parent_id, a.action, a.details
		FROM issue_activities a
		WHERE a.issue_id=$1 AND a.tenant_id=$2
		ORDER BY seq, id`, r.IssueID, r.TenantID)
	return Object{"items": items, "nextCursor": ""}
}
