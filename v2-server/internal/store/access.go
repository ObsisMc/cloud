package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"
)

// Access is a user's verified standing in one tenant. It is what tenant authorization hands to
// everything after it: queries filter by TenantID and enforce business rules with Role, but never
// re-check membership.
type Access struct {
	TenantID string
	UserID   string
	// Role is RoleOwner or RoleMember.
	Role string
}

// Owner reports whether the user may perform the tenant's owner-only operations.
func (a Access) Owner() bool { return a.Role == RoleOwner }

var accessUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// TenantAccess is the single tenant authorization decision. The user has access only while a
// membership exists, the user is active and not deleted, and the tenant is active; anything else,
// including identifiers that cannot name a row, is ErrNotFound so callers cannot tell a hidden
// tenant from a missing one. HTTP routes call it once per request and background work calls it
// when it starts executing on the user's behalf.
func (s *Store) TenantAccess(ctx context.Context, tenant, user string) (Access, error) {
	return tenantAccess(ctx, s.pool, tenant, user)
}

// tenantAccess is TenantAccess inside a caller's transaction, so a start-of-execution check and
// the rows it admits commit together.
func tenantAccess(ctx context.Context, q dbTX, tenant, user string) (Access, error) {
	if !accessUUID.MatchString(tenant) || !accessUUID.MatchString(user) {
		return Access{}, ErrNotFound
	}
	a := Access{TenantID: tenant, UserID: user}
	e := q.QueryRow(ctx, `
		SELECT m.role FROM memberships m
		JOIN users u ON u.id = m.user_id
		JOIN tenants t ON t.id = m.tenant_id
		WHERE m.tenant_id = $1 AND m.user_id = $2
			AND u.status = 'active' AND u.deleted_at IS NULL AND t.status = 'active'`, tenant, user).Scan(&a.Role)
	if errors.Is(e, pgx.ErrNoRows) {
		return Access{}, ErrNotFound
	}
	if e != nil {
		return Access{}, fmt.Errorf("look up tenant access: %w", e)
	}
	return a, nil
}
