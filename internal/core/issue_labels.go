package core

import "strings"

// label loads a live label scoped to the tenant.
func label(t *transaction, tid, lid string) Object {
	require(validID(lid), 404, "not_found")
	l := t.one("SELECT * FROM labels WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL", lid, tid)
	require(l != nil, 404, "not_found")
	return l
}

func labelList(t *transaction, r *PublicRequest) Object {
	items := t.list("SELECT * FROM labels WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY name, id", r.TenantID)
	return Object{"items": items, "nextCursor": ""}
}

func createLabel(t *transaction, r *PublicRequest) Object {
	name := validText(r.Body.S("name"), 200)
	require(t.one("SELECT id FROM labels WHERE tenant_id=$1 AND name=$2 AND deleted_at IS NULL", r.TenantID, name) == nil, 409, "label_conflict")
	id := newID()
	t.exec("INSERT INTO labels(id,tenant_id,name,color) VALUES($1,$2,$3,$4)", id, r.TenantID, name, r.Body.S("color"))
	return label(t, r.TenantID, id)
}

func updateLabel(t *transaction, r *PublicRequest) Object {
	l := label(t, r.TenantID, r.LabelID)
	version(l, r.Body.N("version"))
	cols := []string{}
	vals := []any{}
	add := func(col string, val any) {
		cols = append(cols, col+"=$"+itoa(len(vals)+1))
		vals = append(vals, val)
	}
	if v, ok := r.Body["name"]; ok {
		name := validText(v.(string), 200)
		require(t.one("SELECT id FROM labels WHERE tenant_id=$1 AND name=$2 AND deleted_at IS NULL AND id<>$3", r.TenantID, name, l.S("id")) == nil, 409, "label_conflict")
		add("name", name)
	}
	if v, ok := r.Body["color"]; ok {
		add("color", v.(string))
	}
	require(len(cols) > 0, 400, "invalid_input")
	vals = append(vals, l.S("id"), r.TenantID)
	idPh, tidPh := itoa(len(vals)-1), itoa(len(vals))
	t.exec("UPDATE labels SET "+strings.Join(cols, ",")+",version=version+1,updated_at=now() WHERE id=$"+idPh+" AND tenant_id=$"+tidPh, vals...)
	return label(t, r.TenantID, l.S("id"))
}

func deleteLabel(t *transaction, r *PublicRequest) Object {
	l := label(t, r.TenantID, r.LabelID)
	version(l, r.Body.N("version"))
	t.exec("UPDATE labels SET deleted_at=now(),version=version+1,updated_at=now() WHERE id=$1", l.S("id"))
	t.exec("DELETE FROM issue_labels WHERE label_id=$1", l.S("id"))
	return t.one("SELECT * FROM labels WHERE id=$1 AND tenant_id=$2", l.S("id"), r.TenantID)
}

// issueLabels returns the live labels attached to one issue, ordered by name then id.
func issueLabels(t *transaction, tid, iid string) []Object {
	return t.list("SELECT l.* FROM issue_labels il JOIN labels l ON l.id=il.label_id WHERE il.issue_id=$1 AND l.tenant_id=$2 AND l.deleted_at IS NULL ORDER BY l.name, l.id", iid, tid)
}

// attachLabels sets the labels array on an issue-shaped object.
func attachLabels(t *transaction, tid string, o Object) Object {
	if o != nil {
		o["labels"] = issueLabels(t, tid, o.S("id"))
	}
	return o
}

func issueLabelList(t *transaction, r *PublicRequest) Object {
	issue(t, r.TenantID, r.IssueID)
	return Object{"items": issueLabels(t, r.TenantID, r.IssueID), "nextCursor": ""}
}

func attachLabel(t *transaction, r *PublicRequest) Object {
	issue(t, r.TenantID, r.IssueID)
	lid := r.Body.S("labelId")
	require(validID(lid), 400, "invalid_label")
	l := label(t, r.TenantID, lid)
	t.exec("INSERT INTO issue_labels(issue_id,label_id) VALUES($1,$2) ON CONFLICT DO NOTHING", r.IssueID, l.S("id"))
	return l
}

func detachLabel(t *transaction, r *PublicRequest) Object {
	issue(t, r.TenantID, r.IssueID)
	l := label(t, r.TenantID, r.LabelID)
	t.exec("DELETE FROM issue_labels WHERE issue_id=$1 AND label_id=$2", r.IssueID, l.S("id"))
	return l
}
