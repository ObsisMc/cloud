package core

import "context"

// Workspace permission foundation (Step 2B — Workspace Sharing Model).
//
// Workspace is the resource-sharing boundary: a Workspace member may eventually
// access the resources shared inside that Workspace, and per-resource membership
// (project_members / issue_members / …) is not the target model. These helpers
// are the minimal reusable predicates behind that model; they deliberately reuse
// the existing membership/role data (collab_workspace_members) and do not add a
// generic RBAC engine.
//
// They are current-state neutral: they answer "what role does this user hold in
// this workspace", not which resources that role can reach. Wiring them into
// real resource authorization (Project/Issue/Agent/…) is a later migration step.

// workspaceRole returns the active membership role of uid in spaceID, or "" when
// uid is not an active member of a live, unarchived space whose tenant is active
// (or when the user is no longer an active tenant member). It is the non-panicking
// core reused by the Store methods and, later, by delete authorization inside an
// existing transaction. Returns "owner" | "admin" | "member".
func workspaceRole(t *transaction, spaceID, uid string) string {
	if !validID(spaceID) || !validID(uid) {
		return ""
	}
	m := t.one(`SELECT wm.role FROM collab_workspace_members wm
JOIN collab_workspaces w ON w.id=wm.workspace_id
JOIN tenant_memberships tm ON tm.tenant_id=w.tenant_id AND tm.user_id=wm.user_id
JOIN users u ON u.id=wm.user_id
JOIN tenants tn ON tn.id=w.tenant_id
WHERE wm.workspace_id=$1 AND wm.user_id=$2 AND wm.status='active'
AND w.archived_at IS NULL
AND tm.status='active' AND u.status='active' AND u.deleted_at IS NULL
AND tn.status='active' AND tn.deleted_at IS NULL`, spaceID, uid)
	if m == nil {
		return ""
	}
	return m.S("role")
}

// workspacePerm runs a boolean predicate inside a transaction and surfaces any
// database error to the caller; a failed check still reads false (fail closed).
func (s *Store) workspacePerm(ctx context.Context, pred func(*transaction) bool) (bool, error) {
	out, err := s.transact(ctx, func(t *transaction) Object {
		return Object{"ok": pred(t)}
	})
	if err != nil {
		return false, err
	}
	return out.B("ok"), nil
}

// IsWorkspaceMember reports whether uid is an active member of spaceID.
func (s *Store) IsWorkspaceMember(ctx context.Context, spaceID, uid string) (bool, error) {
	return s.workspacePerm(ctx, func(t *transaction) bool {
		return workspaceRole(t, spaceID, uid) != ""
	})
}

// IsWorkspaceAdmin reports whether uid is an active owner or admin of spaceID.
func (s *Store) IsWorkspaceAdmin(ctx context.Context, spaceID, uid string) (bool, error) {
	return s.workspacePerm(ctx, func(t *transaction) bool {
		switch workspaceRole(t, spaceID, uid) {
		case "owner", "admin":
			return true
		}
		return false
	})
}

// workspaceCanDelete applies the unified workspace delete rule inside an existing
// transaction: the creator may always delete their own resource; otherwise the
// actor must be a workspace owner or admin. Ordinary members cannot delete
// another member's resource, and non-members have no delete permission. It is the
// transaction-scoped mirror of CanDeleteWorkspaceResource, reused by the project
// DELETE path so the rule is decided in the same transact as project().
func workspaceCanDelete(t *transaction, spaceID, uid, creatorUserID string) bool {
	if uid == creatorUserID {
		return true
	}
	switch workspaceRole(t, spaceID, uid) {
	case "owner", "admin":
		return true
	}
	return false
}

// CanDeleteWorkspaceResource applies the unified workspace delete rule:
// the creator may always delete their own resource; otherwise the actor must be
// a workspace owner or admin. Ordinary members cannot delete another member's
// resource, and non-members have no delete permission.
func (s *Store) CanDeleteWorkspaceResource(ctx context.Context, spaceID, uid, creatorUserID string) (bool, error) {
	return s.workspacePerm(ctx, func(t *transaction) bool {
		return workspaceCanDelete(t, spaceID, uid, creatorUserID)
	})
}
