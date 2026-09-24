package core

import "strings"

func issueView(t *transaction, tid, vid string) Object {
	require(validID(vid), 404, "not_found")
	v := t.one("SELECT * FROM issue_views WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL", vid, tid)
	require(v != nil, 404, "not_found")
	return v
}

func viewList(t *transaction, r *PublicRequest, uid string) Object {
	items := t.list("SELECT * FROM issue_views WHERE tenant_id=$1 AND owner_user_id=$2 AND deleted_at IS NULL ORDER BY created_at, id", r.TenantID, uid)
	return Object{"items": items, "nextCursor": ""}
}

func createView(t *transaction, r *PublicRequest, uid string) Object {
	name := validText(r.Body.S("name"), 200)
	filter := r.Body.O("filter")
	id := newID()
	t.exec("INSERT INTO issue_views(id,tenant_id,owner_user_id,name,filter) VALUES($1,$2,$3,$4,$5)", id, r.TenantID, uid, name, jsonText(filter))
	return issueView(t, r.TenantID, id)
}

func updateView(t *transaction, r *PublicRequest, uid string) Object {
	v := issueView(t, r.TenantID, r.ViewID)
	require(v.S("ownerUserId") == uid, 404, "not_found")
	version(v, r.Body.N("version"))
	cols := []string{}
	vals := []any{}
	add := func(col string, val any) {
		cols = append(cols, col+"=$"+itoa(len(vals)+1))
		vals = append(vals, val)
	}
	if _, ok := r.Body["name"]; ok {
		add("name", validText(r.Body.S("name"), 200))
	}
	if _, ok := r.Body["filter"]; ok {
		add("filter", jsonText(r.Body.O("filter")))
	}
	require(len(cols) > 0, 400, "invalid_input")
	vals = append(vals, v.S("id"), r.TenantID)
	idPh, tidPh := itoa(len(vals)-1), itoa(len(vals))
	t.exec("UPDATE issue_views SET "+strings.Join(cols, ",")+",version=version+1,updated_at=now() WHERE id=$"+idPh+" AND tenant_id=$"+tidPh, vals...)
	return issueView(t, r.TenantID, v.S("id"))
}

func deleteView(t *transaction, r *PublicRequest, uid string) Object {
	v := issueView(t, r.TenantID, r.ViewID)
	require(v.S("ownerUserId") == uid, 404, "not_found")
	version(v, r.Body.N("version"))
	t.exec("UPDATE issue_views SET deleted_at=now(),version=version+1,updated_at=now() WHERE id=$1", v.S("id"))
	return t.one("SELECT * FROM issue_views WHERE id=$1 AND tenant_id=$2", v.S("id"), r.TenantID)
}
