package store_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/store"
)

func TestTenantAccessGrantsRolesOnlyToActiveMembersOfActiveTenants(t *testing.T) {
	f := newSquadFixture(t)
	ctx := f.ctx
	owner := f.login(t, "owner", store.RoleOwner)
	member := f.login(t, "member", store.RoleMember)

	for user, role := range map[string]string{owner: store.RoleOwner, member: store.RoleMember} {
		a, e := f.store.TenantAccess(ctx, f.tenant, user)
		if e != nil || a != (store.Access{TenantID: f.tenant, UserID: user, Role: role}) {
			t.Fatalf("access of %s = %+v %v", role, a, e)
		}
		if a.Owner() != (role == store.RoleOwner) {
			t.Fatalf("%s owner = %v", role, a.Owner())
		}
	}

	var otherTenant string
	if e := f.db.QueryRow(ctx, `INSERT INTO tenants (name) VALUES ('别的工作区') RETURNING id::text`).Scan(&otherTenant); e != nil {
		t.Fatal(e)
	}
	denied := func(name, tenant, user string) {
		t.Helper()
		if a, e := f.store.TenantAccess(ctx, tenant, user); !errors.Is(e, store.ErrNotFound) {
			t.Fatalf("%s: access = %+v %v", name, a, e)
		}
	}
	denied("no membership", otherTenant, member)
	denied("unknown tenant", "00000000-0000-0000-0000-000000000001", member)
	denied("unknown user", f.tenant, "00000000-0000-0000-0000-000000000002")
	for _, malformed := range []string{"", "not-a-uuid", "00000000000000000000000000000000", f.tenant + "x"} {
		denied("malformed tenant "+malformed, malformed, member)
		denied("malformed user "+malformed, f.tenant, malformed)
	}

	for _, tc := range []struct{ name, revoke, restore, id string }{
		{"disabled user", `UPDATE users SET status = 'disabled' WHERE id = $1`, `UPDATE users SET status = 'active' WHERE id = $1`, member},
		{"deleted user", `UPDATE users SET deleted_at = now() WHERE id = $1`, `UPDATE users SET deleted_at = NULL WHERE id = $1`, member},
		{"paused tenant", `UPDATE tenants SET status = 'paused' WHERE id = $1`, `UPDATE tenants SET status = 'active' WHERE id = $1`, f.tenant},
	} {
		if _, e := f.db.Exec(ctx, tc.revoke, tc.id); e != nil {
			t.Fatal(e)
		}
		denied(tc.name, f.tenant, member)
		if _, e := f.db.Exec(ctx, tc.restore, tc.id); e != nil {
			t.Fatal(e)
		}
		if _, e := f.store.TenantAccess(ctx, f.tenant, member); e != nil {
			t.Fatalf("%s restored: %v", tc.name, e)
		}
	}
	if _, e := f.db.Exec(ctx, `DELETE FROM memberships WHERE tenant_id = $1 AND user_id = $2`, f.tenant, member); e != nil {
		t.Fatal(e)
	}
	denied("removed member", f.tenant, member)
}

// A member removed from the tenant keeps their configurations, but nothing they scheduled runs on
// their behalf any more: the scanner stops listing them and every trigger is refused.
func TestScheduledWorkStopsWhenTheCreatorLosesTenantAccess(t *testing.T) {
	f := newSquadFixture(t)
	ctx := f.ctx
	f.login(t, "owner", store.RoleOwner)
	member := f.login(t, "member", store.RoleMember)
	cfg, e := f.store.CreateScheduleConfig(ctx, f.tenant, member, store.ScheduleInput{
		Name:   "日报",
		Target: json.RawMessage(`{"kind":"chat","prompt":"写日报","agent":"","model":""}`),
		Rule:   json.RawMessage(`{"kind":"hours","time":"09:00","days":[],"hours":1,"timezone":"UTC"}`),
	})
	if e != nil {
		t.Fatal(e)
	}
	listed := func() bool {
		t.Helper()
		scopes, e := f.store.ScheduleScopes(ctx)
		if e != nil {
			t.Fatal(e)
		}
		for _, scope := range scopes {
			if scope.TenantID == f.tenant && scope.UserID == member {
				return true
			}
		}
		return false
	}
	trigger := store.Trigger{Kind: "manual", Key: "before", At: cfg.CreatedAt, Version: cfg.Version}
	if !listed() {
		t.Fatal("creator with access is not scanned")
	}
	if _, e := f.store.RecordOccurrence(ctx, f.tenant, member, cfg.ID, trigger, trigger.At); e != nil {
		t.Fatalf("trigger with access: %v", e)
	}

	if _, e := f.db.Exec(ctx, `DELETE FROM memberships WHERE tenant_id = $1 AND user_id = $2`, f.tenant, member); e != nil {
		t.Fatal(e)
	}
	if listed() {
		t.Fatal("removed creator is still scanned")
	}
	scheduled := store.Trigger{Kind: "scheduled", At: cfg.CreatedAt.Add(2 * time.Hour), Version: cfg.Version}
	for _, tr := range []store.Trigger{{Kind: "manual", Key: "after", At: trigger.At, Version: cfg.Version}, scheduled} {
		if _, e := f.store.RecordOccurrence(ctx, f.tenant, member, cfg.ID, tr, tr.At); !errors.Is(e, store.ErrNotFound) {
			t.Fatalf("%s trigger after removal = %v", tr.Kind, e)
		}
	}
	var count int
	if e := f.db.QueryRow(ctx, `SELECT count(*) FROM v2_schedule_occurrences WHERE schedule_id = $1`, cfg.ID).Scan(&count); e != nil || count != 1 {
		t.Fatalf("occurrences after removal = %d %v", count, e)
	}
}
