package core

import "strings"

// Tenants are the isolation boundary every other resource hangs from. They
// have two provisioning paths that share one transaction shape:
//
//   - Bootstrap (cloudctl) provisions a tenant for a deployment with a space
//     whose slug is fixed to "default".
//   - POST /api/v1/tenants lets a signed-in user provision a tenant for
//     themselves. The product never shows the tenant; the user only sees the
//     space they named, so the tenant borrows that name and the first space
//     carries the user-chosen slug.
//
// Both paths make the actor the tenant's first administrator and the first
// space's owner in the same transaction, so a tenant never exists without an
// administrator and a space never exists without an owner.

// provisionTenant inserts a tenant, its first administrator, its first space
// and that space's owner. Callers validate every input first; this only owns
// the atomic write order.
func provisionTenant(t *transaction, uid, tenantName, spaceName, slug string) (tenant, space Object) {
	tid := newID()
	t.exec("INSERT INTO tenants(id,name,status) VALUES($1,$2,'active')", tid, tenantName)
	t.exec("INSERT INTO tenant_memberships(tenant_id,user_id,role,status) VALUES($1,$2,'admin','active')", tid, uid)
	sid := newID()
	t.exec("INSERT INTO collab_workspaces(id,tenant_id,name,slug,created_by) VALUES($1,$2,$3,$4,$5)", sid, tid, spaceName, slug, uid)
	t.exec("INSERT INTO collab_workspace_members(workspace_id,user_id,role,status,created_by) VALUES($1,$2,'owner','active',$2)", sid, uid)
	return t.one("SELECT t.id,t.name,t.status,m.role FROM tenants t JOIN tenant_memberships m ON m.tenant_id=t.id WHERE t.id=$1 AND m.user_id=$2", tid, uid),
		t.one("SELECT * FROM collab_workspaces WHERE id=$1", sid)
}

// createTenant serves POST /api/v1/tenants: the signed-in user provisions a
// tenant named after the space they are creating and becomes its
// administrator and the space owner.
//
// Idempotency records are keyed by tenant, and no tenant exists before this
// call succeeds, so replay is matched on (user, key) across the tenants the
// user belongs to and the record is stored under the tenant the call created.
// The request hash still rejects a reused key with a different body.
func createTenant(t *transaction, r *PublicRequest, uid string) (out Object, status int) {
	name := validText(r.Body.S("name"), 128)
	slug := strings.ToLower(strings.TrimSpace(r.Body.S("slug")))
	require(validSlug(slug), 400, "invalid_slug")
	require(r.Key != "" && len(r.Key) <= 200, 400, "idempotency_key_required")
	hash := requestHash(r.Method, r.Path, r.Body)
	if old := t.one("SELECT * FROM idempotency_records WHERE user_id=$1 AND key=$2", uid, r.Key); old != nil {
		require(old.S("requestHash") == hash, 409, "idempotency_conflict")
		return old.O("response"), int(old.N("status"))
	}
	tenant, space := provisionTenant(t, uid, name, name, slug)
	out = Object{"tenant": tenant, "space": space}
	t.exec("INSERT INTO idempotency_records(tenant_id,user_id,key,request_hash,response,status) VALUES($1,$2,$3,$4,$5,$6)", tenant.S("id"), uid, r.Key, hash, jsonText(out), 201)
	return out, 201
}
