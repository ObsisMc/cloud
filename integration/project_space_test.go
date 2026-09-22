package integration

import (
	"context"
	"testing"

	"github.com/golang-jwt/jwt/v5"

	"github.com/wanglongan587/cloud/internal/core"
)

// callUser runs a public request as the given final user and asserts the status.
// The fixture's f.call always acts as the bootstrap owner (alice); owner-isolation
// scenarios need to act as a specific space member.
func (f *fixture) callUser(t *testing.T, u core.Claims, method, path string, body core.Object, key string, want int) core.Object {
	t.Helper()
	gw := core.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "gateway-a"}}
	o, status, e := f.client.Call(context.Background(), method, path, "gateway", gw, &u, key, body)
	must(t, e)
	if status != want {
		t.Fatalf("%s %s: want %d got %d %v", method, path, want, status, o)
	}
	return o
}

// TestProjectSpaceScopingAndSharing covers scenarios 2-4, 9-12 and the project
// workspace-sharing boundary (Step 3). Space membership scopes the member
// collection and gates the space, and Project / Runtime Workspace visibility
// follows workspace membership: every active member can read a space-scoped
// project and its runtime workspace, while a non-member stays fully hidden (404).
// The tenant-level project list keeps its owner filter.
func TestProjectSpaceScopingAndSharing(t *testing.T) {
	f := setup(t)
	space := f.createSpace("Scoped", "scoped", "space-scoped")
	sid := space.S("id")
	bob, bobID := f.addUser(t, "bob", "Bob")
	f.call("PUT", f.path("/spaces/"+sid+"/members/"+bobID), core.Object{"role": "member", "status": "active", "version": 0}, "", 200)
	dave, daveID := f.addUser(t, "dave", "Dave")
	f.call("PUT", f.path("/spaces/"+sid+"/members/"+daveID), core.Object{"role": "member", "status": "active", "version": 0}, "", 200)
	carol, _ := f.addUser(t, "carol", "Carol") // not a member of `sid`

	// Scenario 2: a member creates a project inside the space and becomes its owner.
	created := f.callUser(t, bob, "POST", f.path("/spaces/"+sid+"/projects"), core.Object{"name": "Shared", "repositoryUrl": "https://example.invalid/repo.git", "defaultBranch": "main"}, "bob-project", 202)
	pid := created.O("resource").S("id")
	wid := created.O("workspace").S("id")
	if created.O("resource").S("spaceId") != sid {
		t.Fatal("space-scoped project missing spaceId")
	}

	// The workspace is the sharing boundary: every member sees every active
	// project in the space, regardless of creator.
	bobList := f.callUser(t, bob, "GET", f.path("/spaces/"+sid+"/projects"), nil, "", 200)
	if len(bobList["items"].([]any)) != 1 || core.Object(bobList["items"].([]any)[0].(map[string]any)).S("id") != pid {
		t.Fatalf("creator space project list: %v", bobList)
	}
	daveList := f.callUser(t, dave, "GET", f.path("/spaces/"+sid+"/projects"), nil, "", 200)
	if len(daveList["items"].([]any)) != 1 || core.Object(daveList["items"].([]any)[0].(map[string]any)).S("id") != pid {
		t.Fatalf("member space project list should include bob's project: %v", daveList)
	}

	// A space member who is not the creator can read the project and its runtime
	// workspace but cannot delete it (delete rule: creator OR owner/admin → 403);
	// a non-member of the space stays fully hidden (404, no existence leak).
	f.callUser(t, dave, "GET", f.path("/projects/"+pid), nil, "", 200)
	f.callUser(t, dave, "GET", f.path("/workspaces/"+wid), nil, "", 200)
	f.callUser(t, dave, "DELETE", f.path("/projects/"+pid), core.Object{"version": 1}, "member-delete", 403)
	f.callUser(t, carol, "GET", f.path("/projects/"+pid), nil, "", 404)
	f.callUser(t, carol, "GET", f.path("/workspaces/"+wid), nil, "", 404)
	f.callUser(t, carol, "DELETE", f.path("/projects/"+pid), core.Object{"version": 1}, "nonmember-delete", 404)
	// The tenant-level list keeps its owner filter: dave sees only his own
	// projects there (empty in this scenario), even though he can see the shared
	// project in the space view.
	ownerList := f.callUser(t, dave, "GET", f.path("/projects"), nil, "", 200)
	if len(ownerList["items"].([]any)) != 0 {
		t.Fatalf("tenant project list should stay owner-filtered: %v", ownerList)
	}

	// Drain so the project reaches active and its version moves past the baseline.
	f.drain()

	// Scenario 4: the owner patches business content.
	ownerRead := f.callUser(t, bob, "GET", f.path("/projects/"+pid), nil, "", 200)
	f.callUser(t, bob, "PATCH", f.path("/projects/"+pid), core.Object{"name": "Shared Renamed", "version": ownerRead.N("version")}, "", 200)
	// Scenario 12: a stale version conflicts.
	f.callUser(t, bob, "PATCH", f.path("/projects/"+pid), core.Object{"name": "Stale", "version": ownerRead.N("version")}, "", 409)

	// Scenario 10: the owner deletes through the lifecycle state machine.
	active := f.callUser(t, bob, "GET", f.path("/projects/"+pid), nil, "", 200)
	if active.S("lifecycle") != "active" {
		t.Fatalf("project did not reach active: %s", active.S("lifecycle"))
	}
	deleted := f.callUser(t, bob, "DELETE", f.path("/projects/"+pid), core.Object{"version": active.N("version")}, "owner-delete", 202)
	if deleted.O("resource").S("lifecycle") != "deleting" || deleted.O("operation").S("id") == "" {
		t.Fatal("owner delete did not enter lifecycle state machine")
	}
	f.drain()
	if f.scalar("SELECT count(*) FROM projects WHERE id=$1 AND deleted_at IS NULL", pid) != 0 {
		t.Fatal("delete lifecycle did not complete")
	}
}

// TestProjectSpaceOptionalScope covers D2: a Space is an optional grouping. The
// tenant-level creation path never depends on a default space and leaves
// space_id NULL, while the space-scoped path records the space.
func TestProjectSpaceOptionalScope(t *testing.T) {
	f := setup(t)
	// Tenant-level project has no space, even though the bootstrap default space exists.
	unscoped := f.call("POST", f.path("/projects"), core.Object{"name": "Unscoped", "repositoryUrl": "https://example.invalid/repo.git", "defaultBranch": "main"}, "unscoped", 202)
	if unscoped.O("resource").S("spaceId") != "" {
		t.Fatalf("tenant-level project should have no spaceId: %v", unscoped)
	}
	// A space-scoped project records its space.
	space := f.createSpace("Scoped", "scoped", "space-scoped")
	sid := space.S("id")
	gw := core.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "gateway-a"}}
	scoped, status, e := f.client.Call(context.Background(), "POST", f.path("/spaces/"+sid+"/projects"), "gateway", gw, &f.user, "scoped-project", core.Object{"name": "Scoped", "repositoryUrl": "https://example.invalid/repo.git", "defaultBranch": "main"})
	must(t, e)
	if status != 202 {
		t.Fatalf("space-scoped create: want 202 got %d", status)
	}
	if scoped.O("resource").S("spaceId") != sid {
		t.Fatalf("space-scoped project missing spaceId: %v", scoped)
	}
}

// TestSpaceMemberLastOwnerRace covers the owner-immutable invariant under
// concurrency (Step 3A): the owner role can never be demoted or granted through
// the member API, so concurrent attempts to destroy the ownership (owner
// self-demote -> 409 ownership_transfer_not_supported) or to create a second
// owner (member self-promote -> 403) are all rejected, leaving exactly one
// owner. Ownership transfer is NOT implemented.
func TestSpaceMemberLastOwnerRace(t *testing.T) {
	f := setup(t)
	gw := core.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "gateway-a"}}
	space := f.createSpace("Race", "race", "space-race")
	sid := space.S("id")
	bob, bobID := f.addUser(t, "bob", "Bob")
	f.call("PUT", f.path("/spaces/"+sid+"/members/"+bobID), core.Object{"role": "member", "status": "active", "version": 0}, "", 200)
	// Alice (owner) self-demotes while bob (member) self-promotes — fired
	// concurrently; both must be rejected, and exactly one owner remains.
	subjects := []core.Claims{f.user, bob}
	ids := []string{f.uid, bobID}
	bodies := []core.Object{
		{"role": "member", "status": "active", "version": 1}, // owner self-demote
		{"role": "admin", "status": "active", "version": 1},  // member self-promote
	}
	statuses := make(chan int, 2)
	for i := range subjects {
		go func(i int) {
			_, status, _ := f.client.Call(context.Background(), "PUT", f.path("/spaces/"+sid+"/members/"+ids[i]), "gateway", gw, &subjects[i], "", bodies[i])
			statuses <- status
		}(i)
	}
	got := map[int]int{}
	for range 2 {
		got[<-statuses]++
	}
	if got[409] != 1 || got[403] != 1 {
		t.Fatalf("owner-immutable race: want one 409 and one 403 got %v", got)
	}
	if f.scalar("SELECT count(*) FROM collab_workspace_members WHERE workspace_id=$1 AND role='owner' AND status='active'", sid) != 1 {
		t.Fatal("space lost its last owner")
	}
	if f.scalar("SELECT count(*) FROM collab_workspace_members WHERE workspace_id=$1 AND user_id=$2 AND role='owner'", sid, f.uid) != 1 {
		t.Fatal("alice is no longer the owner")
	}
}