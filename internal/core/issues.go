package core

import "strings"

var issuePriorities = map[string]bool{
	"urgent": true, "high": true, "medium": true, "low": true, "none": true,
}

// issue loads a live issue scoped to the tenant; foreign, deleted, and missing are all 404.
func issue(t *transaction, tid, iid string) Object {
	require(validID(iid), 404, "not_found")
	i := t.one("SELECT * FROM issues WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL", iid, tid)
	require(i != nil, 404, "not_found")
	return attachLabels(t, tid, i)
}

// positionOf reads an issue's fractional board position (decoded as JSON number → float64).
func positionOf(o Object) float64 {
	if v, ok := o["position"].(float64); ok {
		return v
	}
	return 0
}

// topPosition is Multica's NextTopPosition: MIN(position)-1, or 0 for an empty column.
func topPosition(t *transaction, tid, status string) float64 {
	top := t.one("SELECT min(position) FROM issues WHERE tenant_id=$1 AND status=$2 AND deleted_at IS NULL", tid, status)
	if v, ok := top["min"].(float64); ok {
		return v - 1
	}
	return 0
}

// issueList returns the tenant board in status-catalog order, then position, then id.
func issueList(t *transaction, r *PublicRequest) Object {
	return Object{"items": issueListItems(t, r), "nextCursor": ""}
}

// issueListItems returns board items ordered by the status catalog position (unknown/archived
// statuses sort last), then fractional position, then id. ?q= filters title/description (ILIKE).
func issueListItems(t *transaction, r *PublicRequest) []Object {
	seedIssueStatuses(t, r.TenantID)
	query := "SELECT i.* FROM issues i LEFT JOIN issue_statuses s ON s.tenant_id=i.tenant_id AND s.key=i.status AND s.deleted_at IS NULL WHERE i.tenant_id=$1 AND i.deleted_at IS NULL"
	args := []any{r.TenantID}
	if term := strings.TrimSpace(r.Query); term != "" {
		require(len(term) <= 200, 400, "invalid_query")
		args = append(args, likePattern(term))
		query += " AND (i.title ILIKE $2 OR i.description ILIKE $2)"
	}
	query += " ORDER BY COALESCE(s.position,1000), i.position, i.id"
	items := t.list(query, args...)
	for _, it := range items {
		attachLabels(t, r.TenantID, it)
	}
	return items
}

// likePattern escapes LIKE wildcards so ?q= treats user input literally.
func likePattern(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "_", "\\_")
	return "%" + s + "%"
}

// resolveAssignee validates an optional assignee from the body; empty string clears it.
func resolveAssignee(t *transaction, body Object) (assignee any, present bool) {
	v, ok := body["assigneeUserId"]
	if !ok {
		return nil, false
	}
	s, _ := v.(string)
	if s == "" {
		return nil, true
	}
	require(validID(s), 400, "invalid_assignee")
	require(t.one("SELECT id FROM users WHERE id=$1 AND status='active' AND deleted_at IS NULL", s) != nil, 404, "assignee_not_found")
	return s, true
}

// resolveParent validates an optional parent issue; empty string clears it; self-reference is rejected.
func resolveParent(t *transaction, tid, self string, body Object) (parent any, present bool) {
	v, ok := body["parentIssueId"]
	if !ok {
		return nil, false
	}
	s, _ := v.(string)
	if s == "" {
		return nil, true
	}
	require(validID(s) && s != self, 400, "invalid_parent")
	require(t.one("SELECT id FROM issues WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL", s, tid) != nil, 404, "parent_not_found")
	return s, true
}

func createIssue(t *transaction, r *PublicRequest, uid string) Object {
	title := validText(r.Body.S("title"), 200)
	status := r.Body.S("status")
	if status == "" {
		status = "todo"
	}
	status = resolveStatus(t, r.TenantID, status)
	priority := r.Body.S("priority")
	if priority == "" {
		priority = "none"
	}
	require(issuePriorities[priority], 400, "invalid_priority")
	description := strings.TrimSpace(r.Body.S("description"))
	require(len(description) <= 20000, 400, "invalid_input")
	assignee, _ := resolveAssignee(t, r.Body)
	parent, _ := resolveParent(t, r.TenantID, "", r.Body)
	id := newID()
	number := int64(t.one("SELECT COALESCE(max(number),0)+1 AS n FROM issues WHERE tenant_id=$1", r.TenantID)["n"].(float64))
	t.exec("INSERT INTO issues(id,tenant_id,creator_user_id,assignee_user_id,parent_issue_id,title,description,status,priority,position,number,properties) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)", id, r.TenantID, uid, assignee, parent, title, description, status, priority, topPosition(t, r.TenantID, status), number, jsonText(r.Body.O("properties")))
	return issue(t, r.TenantID, id)
}

func updateIssue(t *transaction, r *PublicRequest) Object {
	i := issue(t, r.TenantID, r.IssueID)
	version(i, r.Body.N("version"))
	cols := []string{}
	vals := []any{}
	add := func(col string, val any) {
		cols = append(cols, col+"=$"+itoa(len(vals)+1))
		vals = append(vals, val)
	}
	if v, ok := r.Body["title"]; ok {
		add("title", validText(v.(string), 200))
	}
	if v, ok := r.Body["description"]; ok {
		d := strings.TrimSpace(v.(string))
		require(len(d) <= 20000, 400, "invalid_input")
		add("description", d)
	}
	if v, ok := r.Body["priority"]; ok {
		p := v.(string)
		require(issuePriorities[p], 400, "invalid_priority")
		add("priority", p)
	}
	if v, ok := r.Body["status"]; ok {
		s := resolveStatus(t, r.TenantID, v.(string))
		add("status", s)
		if s != i.S("status") {
			add("position", topPosition(t, r.TenantID, s))
		}
	}
	if assignee, present := resolveAssignee(t, r.Body); present {
		add("assignee_user_id", assignee)
	}
	if parent, present := resolveParent(t, r.TenantID, i.S("id"), r.Body); present {
		add("parent_issue_id", parent)
	}
	if v, ok := r.Body["properties"]; ok {
		props, ok := v.(map[string]any)
		require(ok, 400, "invalid_input")
		add("properties", jsonText(props))
	}
	require(len(cols) > 0, 400, "invalid_input")
	vals = append(vals, i.S("id"), r.TenantID)
	idPh, tidPh := itoa(len(vals)-1), itoa(len(vals))
	t.exec("UPDATE issues SET "+strings.Join(cols, ",")+",version=version+1,updated_at=now() WHERE id=$"+idPh+" AND tenant_id=$"+tidPh, vals...)
	return issue(t, r.TenantID, i.S("id"))
}

// moveAnchor resolves a before/after anchor to its position within the tenant (not the column).
func moveAnchor(t *transaction, tid string, body Object, name string) (float64, bool) {
	v, ok := body[name]
	if !ok {
		return 0, false
	}
	s, _ := v.(string)
	if s == "" {
		return 0, false
	}
	require(validID(s), 400, "invalid_anchor")
	a := t.one("SELECT position FROM issues WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL", s, tid)
	require(a != nil, 404, "not_found")
	return positionOf(a), true
}

func moveIssue(t *transaction, r *PublicRequest) Object {
	i := issue(t, r.TenantID, r.IssueID)
	version(i, r.Body.N("version"))
	status := i.S("status")
	if v, ok := r.Body["status"]; ok {
		status = resolveStatus(t, r.TenantID, v.(string))
	}
	position := positionOf(i)
	if before, ok := moveAnchor(t, r.TenantID, r.Body, "beforeId"); ok {
		if after, has := moveAnchor(t, r.TenantID, r.Body, "afterId"); has {
			require(after > before, 409, "position_conflict")
			position = before + (after-before)/2
			require(position > before && position < after, 409, "position_conflict")
		} else {
			position = before + 1
		}
	} else if after, ok := moveAnchor(t, r.TenantID, r.Body, "afterId"); ok {
		position = after - 1
	} else if status != i.S("status") {
		position = topPosition(t, r.TenantID, status)
	}
	t.exec("UPDATE issues SET status=$2,position=$3,version=version+1,updated_at=now() WHERE id=$1 AND tenant_id=$4", i.S("id"), status, position, r.TenantID)
	return issue(t, r.TenantID, i.S("id"))
}

func deleteIssue(t *transaction, r *PublicRequest) Object {
	i := issue(t, r.TenantID, r.IssueID)
	version(i, r.Body.N("version"))
	// Soft delete orphans children first (Multica's hard delete relies on ON DELETE SET NULL).
	t.exec("UPDATE issues SET parent_issue_id=NULL,version=version+1,updated_at=now() WHERE parent_issue_id=$1 AND tenant_id=$2", i.S("id"), r.TenantID)
	t.exec("UPDATE issues SET deleted_at=now(),version=version+1,updated_at=now() WHERE id=$1 AND tenant_id=$2", i.S("id"), r.TenantID)
	return attachLabels(t, r.TenantID, t.one("SELECT * FROM issues WHERE id=$1 AND tenant_id=$2", i.S("id"), r.TenantID))
}

// batchUpdate applies a single status/priority/assignee patch to a set of issues atomically.
func batchUpdate(t *transaction, r *PublicRequest) Object {
	raw, ok := r.Body["ids"].([]any)
	require(ok && len(raw) > 0 && len(raw) <= 100, 400, "invalid_input")
	ids := make([]string, 0, len(raw))
	for _, v := range raw {
		s, _ := v.(string)
		require(validID(s), 400, "invalid_input")
		ids = append(ids, s)
	}
	var patchStatus, patchPriority string
	hasStatus, hasPriority := false, false
	if v, ok := r.Body["status"]; ok {
		hasStatus = true
		patchStatus = resolveStatus(t, r.TenantID, v.(string))
	}
	if v, ok := r.Body["priority"]; ok {
		hasPriority = true
		patchPriority = v.(string)
		require(issuePriorities[patchPriority], 400, "invalid_priority")
	}
	assignee, hasAssignee := resolveAssignee(t, r.Body)
	require(hasStatus || hasPriority || hasAssignee, 400, "invalid_input")
	out := []Object{}
	for _, id := range ids {
		i := issue(t, r.TenantID, id)
		cols := []string{}
		vals := []any{}
		add := func(col string, val any) {
			cols = append(cols, col+"=$"+itoa(len(vals)+1))
			vals = append(vals, val)
		}
		if hasStatus && patchStatus != i.S("status") {
			add("status", patchStatus)
			add("position", topPosition(t, r.TenantID, patchStatus))
		}
		if hasPriority {
			add("priority", patchPriority)
		}
		if hasAssignee {
			add("assignee_user_id", assignee)
		}
		if len(cols) > 0 {
			vals = append(vals, i.S("id"))
			t.exec("UPDATE issues SET "+strings.Join(cols, ",")+",version=version+1,updated_at=now() WHERE id=$"+itoa(len(vals)), vals...)
		}
		out = append(out, issue(t, r.TenantID, id))
	}
	return Object{"items": out, "nextCursor": ""}
}

// issueGroups buckets board items by status, priority, or assigneeUserId.
func issueGroups(t *transaction, r *PublicRequest) Object {
	by := r.GroupBy
	if by == "" {
		by = "status"
	}
	require(by == "status" || by == "priority" || by == "assigneeUserId", 400, "invalid_group")
	items := issueListItems(t, r)
	order := []string{}
	grouped := map[string][]Object{}
	for _, it := range items {
		key := ""
		switch by {
		case "status":
			key = it.S("status")
		case "priority":
			key = it.S("priority")
		case "assigneeUserId":
			key = it.S("assigneeUserId")
			if key == "" {
				key = "unassigned"
			}
		}
		if _, ok := grouped[key]; !ok {
			order = append(order, key)
		}
		grouped[key] = append(grouped[key], it)
	}
	groups := []Object{}
	for _, k := range order {
		groups = append(groups, Object{"key": k, "items": grouped[k]})
	}
	return Object{"groups": groups}
}
