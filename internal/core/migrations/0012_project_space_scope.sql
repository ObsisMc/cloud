-- Provision a default Collaboration Space for every live tenant and add its active
-- members so the Space surface is consistent for pre-existing tenants. Projects are
-- intentionally NOT backfilled: projects.space_id stays NULL until a caller opts a
-- project into a space (D2=C — Space is an optional grouping, not a mandatory parent).
INSERT INTO collab_workspaces(id, tenant_id, name, slug, description, created_by)
SELECT gen_random_uuid(), t.id, 'Default', 'default', '', COALESCE((
 SELECT m.user_id FROM tenant_memberships m WHERE m.tenant_id=t.id ORDER BY m.created_at, m.user_id LIMIT 1
), (
 SELECT u.id FROM users u ORDER BY u.id LIMIT 1
))
FROM tenants t
WHERE t.deleted_at IS NULL OR EXISTS (SELECT 1 FROM projects p WHERE p.tenant_id=t.id);

INSERT INTO collab_workspace_members(workspace_id, user_id, role, status)
SELECT w.id, m.user_id,
 CASE WHEN m.role='admin' THEN 'owner' ELSE 'member' END, 'active'
FROM collab_workspaces w
JOIN tenant_memberships m ON m.tenant_id=w.tenant_id AND m.status='active'
JOIN users u ON u.id=m.user_id AND u.status='active' AND u.deleted_at IS NULL;

-- projects.space_id is nullable: an existing or newly tenant-scoped project may have
-- no space. The composite FK still rejects cross-tenant space references when set.
ALTER TABLE projects ADD COLUMN space_id uuid;
ALTER TABLE projects ADD FOREIGN KEY(space_id,tenant_id) REFERENCES collab_workspaces(id,tenant_id);
CREATE INDEX project_space_list ON projects(space_id,id);