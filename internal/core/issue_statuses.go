package core

import (
	"regexp"
	"strings"
)

// statusKeyPattern matches tenant-defined status keys; it mirrors the issues.status format CHECK.
var statusKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{0,31}$`)

// canonicalStatuses are the 7 system columns seeded lazily per tenant, in board order.
var canonicalStatuses = []struct {
	key, name, category string
	position            float64
}{
	{"backlog", "Backlog", "unstarted", 0},
	{"todo", "Todo", "unstarted", 1},
	{"in_progress", "In Progress", "started", 2},
	{"in_review", "In Review", "started", 3},
	{"blocked", "Blocked", "started", 4},
	{"done", "Done", "done", 5},
	{"cancelled", "Cancelled", "closed", 6},
}

// seedIssueStatuses lazily inserts the canonical system columns for a tenant. Safe under the
// global advisory lock and idempotent per (tenant_id, key).
func seedIssueStatuses(t *transaction, tid string) {
	for _, c := range canonicalStatuses {
		t.exec("INSERT INTO issue_statuses(id,tenant_id,key,name,category,position,is_system) VALUES($1,$2,$3,$4,$5,$6,true) ON CONFLICT(tenant_id,key) DO NOTHING", newID(), tid, c.key, c.name, c.category, c.position)
	}
}

// resolveStatus validates a status key against the tenant's live catalog (seeding it first).
func resolveStatus(t *transaction, tid, key string) string {
	seedIssueStatuses(t, tid)
	require(statusKeyPattern.MatchString(key), 400, "invalid_status")
	require(t.one("SELECT key FROM issue_statuses WHERE tenant_id=$1 AND key=$2 AND deleted_at IS NULL", tid, key) != nil, 400, "invalid_status")
	return key
}

// issueStatus loads a live status-column row scoped to the tenant.
func issueStatus(t *transaction, tid, sid string) Object {
	require(validID(sid), 404, "not_found")
	s := t.one("SELECT * FROM issue_statuses WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL", sid, tid)
	require(s != nil, 404, "not_found")
	return s
}

func statusCatalogList(t *transaction, r *PublicRequest) Object {
	seedIssueStatuses(t, r.TenantID)
	items := t.list("SELECT * FROM issue_statuses WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY position, id", r.TenantID)
	return Object{"items": items, "nextCursor": ""}
}

// nextStatusPosition appends a new column after the current highest position.
func nextStatusPosition(t *transaction, tid string) float64 {
	top := t.one("SELECT max(position) AS p FROM issue_statuses WHERE tenant_id=$1 AND deleted_at IS NULL", tid)
	if v, ok := top["p"].(float64); ok {
		return v + 1
	}
	return 0
}

func createIssueStatus(t *transaction, r *PublicRequest) Object {
	seedIssueStatuses(t, r.TenantID)
	key := strings.TrimSpace(r.Body.S("key"))
	require(statusKeyPattern.MatchString(key), 400, "invalid_status_key")
	require(t.one("SELECT id FROM issue_statuses WHERE tenant_id=$1 AND key=$2 AND deleted_at IS NULL", r.TenantID, key) == nil, 409, "status_conflict")
	name := validText(r.Body.S("name"), 200)
	category := r.Body.S("category")
	if category == "" {
		category = "started"
	}
	require(category == "unstarted" || category == "started" || category == "done" || category == "closed", 400, "invalid_category")
	description := strings.TrimSpace(r.Body.S("description"))
	require(len(description) <= 2000, 400, "invalid_input")
	id := newID()
	t.exec("INSERT INTO issue_statuses(id,tenant_id,key,name,description,category,color,icon,position,is_system) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,false)", id, r.TenantID, key, name, description, category, r.Body.S("color"), r.Body.S("icon"), nextStatusPosition(t, r.TenantID))
	return issueStatus(t, r.TenantID, id)
}

func updateIssueStatus(t *transaction, r *PublicRequest) Object {
	seedIssueStatuses(t, r.TenantID)
	s := issueStatus(t, r.TenantID, r.StatusID)
	version(s, r.Body.N("version"))
	cols := []string{}
	vals := []any{}
	add := func(col string, val any) {
		cols = append(cols, col+"=$"+itoa(len(vals)+1))
		vals = append(vals, val)
	}
	if v, ok := r.Body["name"]; ok {
		add("name", validText(v.(string), 200))
	}
	if v, ok := r.Body["description"]; ok {
		d := strings.TrimSpace(v.(string))
		require(len(d) <= 2000, 400, "invalid_input")
		add("description", d)
	}
	if v, ok := r.Body["category"]; ok {
		c := v.(string)
		require(c == "unstarted" || c == "started" || c == "done" || c == "closed", 400, "invalid_category")
		add("category", c)
	}
	if v, ok := r.Body["color"]; ok {
		add("color", v.(string))
	}
	if v, ok := r.Body["icon"]; ok {
		add("icon", v.(string))
	}
	if _, ok := r.Body["position"]; ok {
		add("position", float64(r.Body.N("position")))
	}
	require(len(cols) > 0, 400, "invalid_input")
	vals = append(vals, s.S("id"), r.TenantID)
	idPh, tidPh := itoa(len(vals)-1), itoa(len(vals))
	t.exec("UPDATE issue_statuses SET "+strings.Join(cols, ",")+",version=version+1,updated_at=now() WHERE id=$"+idPh+" AND tenant_id=$"+tidPh, vals...)
	return issueStatus(t, r.TenantID, s.S("id"))
}

func deleteIssueStatus(t *transaction, r *PublicRequest) Object {
	seedIssueStatuses(t, r.TenantID)
	s := issueStatus(t, r.TenantID, r.StatusID)
	version(s, r.Body.N("version"))
	require(!s.B("isSystem"), 409, "system_status_required")
	t.exec("UPDATE issue_statuses SET deleted_at=now(),version=version+1,updated_at=now() WHERE id=$1 AND tenant_id=$2", s.S("id"), r.TenantID)
	return t.one("SELECT * FROM issue_statuses WHERE id=$1 AND tenant_id=$2", s.S("id"), r.TenantID)
}
