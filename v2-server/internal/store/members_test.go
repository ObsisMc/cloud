package store_test

import (
	"testing"
)

func TestTenantMembers(t *testing.T) {
	f := newSquadFixture(t)
	ctx := f.ctx
	owner := f.login(t, "owner", "owner")
	second := f.login(t, "beta", "member")
	alpha := f.login(t, "alpha", "member")
	gone := f.login(t, "gone", "member")
	if _, e := f.db.Exec(ctx, `UPDATE users SET deleted_at = now() WHERE id = $1`, gone); e != nil {
		t.Fatal(e)
	}

	items, e := f.store.TenantMembers(ctx, f.tenant)
	if e != nil {
		t.Fatal(e)
	}
	if len(items) != 3 {
		t.Fatalf("members = %+v", items)
	}
	// The roster lists every live account in the workspace, ordered by display name.
	want := []struct{ user, name, role string }{
		{alpha, "alpha", "member"},
		{second, "beta", "member"},
		{owner, "owner", "owner"},
	}
	for i, w := range want {
		m := items[i]
		if m.UserID != w.user || m.DisplayName != w.name || m.Role != w.role {
			t.Fatalf("member %d = %+v, want %+v", i, m, w)
		}
		// v2 has no membership identity of its own; the row repeats the user id.
		if m.ID != m.UserID || m.TenantID != f.tenant || m.Version != 1 || m.Status != "active" {
			t.Fatalf("member %d identity = %+v", i, m)
		}
		if m.ActorDisplayName != nil || m.JoinMethod != nil || m.JoinedAt != nil {
			t.Fatalf("member %d provenance = %+v", i, m)
		}
	}

	// The roster is filtered by tenant; tenant access is the route's decision.
	if items, e := f.store.TenantMembers(ctx, "00000000-0000-0000-0000-000000000001"); e != nil || len(items) != 0 {
		t.Fatalf("another tenant's roster = %+v %v", items, e)
	}
}
