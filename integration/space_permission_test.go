package integration

import (
	"context"
	"testing"

	"github.com/wanglongan587/cloud/internal/core"
)

// workspacePermissionFixture builds one workspace with the role spread used by
// every permission test: alice (fixture user) = owner, bob = plain member,
// carol = admin (promoted via the role PUT), dave = registered but NOT a member.
// It returns the space id and the four user ids.
func workspacePermissionFixture(t *testing.T) (f *fixture, sid string, aliceID, bobID, carolID, daveID string) {
	t.Helper()
	f = setup(t)
	space := f.createSpace("Team", "team", "space-create")
	sid = space.S("id")
	aliceID = f.uid

	_, bobID = f.registerUser(t, "bob@example.com", "Bob")
	f.enrollByEmail(sid, "bob@example.com", "enroll-bob")

	_, carolID = f.registerUser(t, "carol@example.com", "Carol")
	f.enrollByEmail(sid, "carol@example.com", "enroll-carol")
	f.call("PUT", f.path("/spaces/"+sid+"/members/"+carolID), core.Object{"role": "admin", "status": "active", "version": 1}, "", 200)

	_, daveID = f.registerUser(t, "dave@example.com", "Dave")
	return f, sid, aliceID, bobID, carolID, daveID
}

func (f *fixture) isWorkspaceAdmin(t *testing.T, sid, uid string) bool {
	t.Helper()
	ok, e := f.store.IsWorkspaceAdmin(context.Background(), sid, uid)
	must(t, e)
	return ok
}

func (f *fixture) isWorkspaceMember(t *testing.T, sid, uid string) bool {
	t.Helper()
	ok, e := f.store.IsWorkspaceMember(context.Background(), sid, uid)
	must(t, e)
	return ok
}

func (f *fixture) canDelete(t *testing.T, sid, uid, creator string) bool {
	t.Helper()
	ok, e := f.store.CanDeleteWorkspaceResource(context.Background(), sid, uid, creator)
	must(t, e)
	return ok
}

// TestSpacePermissionOwnerIsAdmin — the workspace owner is a workspace admin and
// a workspace member (IsWorkspaceAdmin = owner OR admin).
func TestSpacePermissionOwnerIsAdmin(t *testing.T) {
	f, sid, aliceID, _, _, _ := workspacePermissionFixture(t)
	if !f.isWorkspaceAdmin(t, sid, aliceID) {
		t.Fatal("owner is not a workspace admin")
	}
	if !f.isWorkspaceMember(t, sid, aliceID) {
		t.Fatal("owner is not a workspace member")
	}
}

// TestSpacePermissionAdminIsAdmin — a promoted admin counts as a workspace admin.
func TestSpacePermissionAdminIsAdmin(t *testing.T) {
	f, sid, _, _, carolID, _ := workspacePermissionFixture(t)
	if !f.isWorkspaceAdmin(t, sid, carolID) {
		t.Fatal("admin is not a workspace admin")
	}
	if !f.isWorkspaceMember(t, sid, carolID) {
		t.Fatal("admin is not a workspace member")
	}
}

// TestSpacePermissionMemberNotAdmin — a plain member is a workspace member but not
// a workspace admin.
func TestSpacePermissionMemberNotAdmin(t *testing.T) {
	f, sid, _, bobID, _, _ := workspacePermissionFixture(t)
	if f.isWorkspaceAdmin(t, sid, bobID) {
		t.Fatal("plain member is a workspace admin")
	}
	if !f.isWorkspaceMember(t, sid, bobID) {
		t.Fatal("plain member is not a workspace member")
	}
}

// TestSpacePermissionNonMemberNeither — a registered but unenrolled user is
// neither a member nor an admin.
func TestSpacePermissionNonMemberNeither(t *testing.T) {
	f, sid, _, _, _, daveID := workspacePermissionFixture(t)
	if f.isWorkspaceMember(t, sid, daveID) {
		t.Fatal("non-member is a workspace member")
	}
	if f.isWorkspaceAdmin(t, sid, daveID) {
		t.Fatal("non-member is a workspace admin")
	}
}

// TestSpacePermissionCreatorCanDeleteOwn — the creator may always delete their own
// resource, even as a plain member.
func TestSpacePermissionCreatorCanDeleteOwn(t *testing.T) {
	f, sid, _, bobID, _, _ := workspacePermissionFixture(t)
	if !f.canDelete(t, sid, bobID, bobID) {
		t.Fatal("creator (plain member) cannot delete their own resource")
	}
}

// TestSpacePermissionOwnerCanDeleteOthers — the workspace owner may delete a
// resource created by another member.
func TestSpacePermissionOwnerCanDeleteOthers(t *testing.T) {
	f, sid, aliceID, bobID, _, _ := workspacePermissionFixture(t)
	if !f.canDelete(t, sid, aliceID, bobID) {
		t.Fatal("owner cannot delete another creator's resource")
	}
}

// TestSpacePermissionAdminCanDeleteOthers — the workspace admin may delete a
// resource created by another member.
func TestSpacePermissionAdminCanDeleteOthers(t *testing.T) {
	f, sid, _, bobID, carolID, _ := workspacePermissionFixture(t)
	if !f.canDelete(t, sid, carolID, bobID) {
		t.Fatal("admin cannot delete another creator's resource")
	}
}

// TestSpacePermissionMemberCannotDeleteOthers — a plain member cannot delete a
// resource created by someone else, whether that creator is the owner, an admin,
// or a member peer.
func TestSpacePermissionMemberCannotDeleteOthers(t *testing.T) {
	f, sid, aliceID, bobID, carolID, _ := workspacePermissionFixture(t)
	if f.canDelete(t, sid, bobID, aliceID) {
		t.Fatal("plain member deleted the owner's resource")
	}
	if f.canDelete(t, sid, bobID, carolID) {
		t.Fatal("plain member deleted an admin's resource")
	}
}

// TestSpacePermissionNonMemberCannotDelete — a registered non-member cannot use
// the workspace delete permission against any workspace participant's resource:
// the delete rule is creator OR owner/admin, and a non-member is neither.
func TestSpacePermissionNonMemberCannotDelete(t *testing.T) {
	f, sid, aliceID, _, carolID, daveID := workspacePermissionFixture(t)
	if f.canDelete(t, sid, daveID, aliceID) {
		t.Fatal("non-member deleted the owner's resource")
	}
	if f.canDelete(t, sid, daveID, carolID) {
		t.Fatal("non-member deleted an admin's resource")
	}
}
