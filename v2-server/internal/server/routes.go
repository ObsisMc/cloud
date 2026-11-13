package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/store"
)

// level is how much a route requires of its caller before the handler runs.
type level int

const (
	// levelPublic needs nothing: health and the login routes.
	levelPublic level = iota
	// levelSession needs a signed-in user and names no tenant.
	levelSession
	// levelTenant needs tenant access to the route's {tid}.
	levelTenant
	// levelOwner needs tenant access with the owner role.
	levelOwner
)

// route is one row of the route table: the pattern exactly as registered and what it requires.
type route struct {
	pattern string
	level   level
}

// access is what tenant authorization hands a tenant route: the verified Access plus the signed-in
// user it was decided for.
type access struct {
	store.Access
	User store.User
}

// scope is the caller's identity in the execution vocabulary, for routes backed by execution.
func (a access) scope() execution.Scope {
	return execution.Scope{TenantID: a.TenantID, UserID: a.UserID}
}

// tenantRoutePrefix marks a tenant API route; every pattern under it must name {tid}.
const tenantRoutePrefix = "/tenants/{tid}"

// router is the only place routes are registered. Each registration records its level, so the
// route table test can walk every route, and the handler signatures make the level structural: a
// tenant handler takes an access, which only tenant and owner registrations can supply.
type router struct {
	h     *handler
	mux   *http.ServeMux
	table []route
}

func (rt *router) add(pattern string, l level, fn http.HandlerFunc) {
	if tenantRoute(pattern) != (l == levelTenant || l == levelOwner) {
		panic(fmt.Sprintf("route %q: tenant routes must be registered with tenant or owner, and only they", pattern))
	}
	rt.table = append(rt.table, route{pattern: pattern, level: l})
	rt.mux.HandleFunc(pattern, fn)
}

func tenantRoute(pattern string) bool { return strings.Contains(pattern, tenantRoutePrefix) }

// public registers a route anyone may call.
func (rt *router) public(pattern string, fn http.HandlerFunc) { rt.add(pattern, levelPublic, fn) }

// session registers a route for any signed-in user. It cannot name a tenant.
func (rt *router) session(pattern string, fn func(http.ResponseWriter, *http.Request, store.User)) {
	rt.add(pattern, levelSession, rt.h.authed(fn))
}

// tenant registers a route for users with access to the route's tenant.
func (rt *router) tenant(pattern string, fn func(http.ResponseWriter, *http.Request, access)) {
	rt.add(pattern, levelTenant, rt.h.tenant(false, fn))
}

// owner registers a route only the tenant's owner may call.
func (rt *router) owner(pattern string, fn func(http.ResponseWriter, *http.Request, access)) {
	rt.add(pattern, levelOwner, rt.h.tenant(true, fn))
}

// tenant is the single HTTP entry of tenant authorization. A caller without access to {tid}, or a
// {tid} that cannot name a tenant, gets 404 so a hidden tenant is indistinguishable from a missing
// one. A member calling an owner-only route already knows the tenant exists and gets 403.
func (h *handler) tenant(ownerOnly bool, next func(http.ResponseWriter, *http.Request, access)) http.HandlerFunc {
	return h.authed(func(w http.ResponseWriter, r *http.Request, u store.User) {
		a, e := h.store.TenantAccess(r.Context(), r.PathValue("tid"), u.ID)
		if errors.Is(e, store.ErrNotFound) {
			// A refusal depends on who asks, so no cache may answer it for anyone else.
			w.Header().Set("Cache-Control", "no-store")
			fault(w, http.StatusNotFound, "not_found")
			return
		}
		if e != nil {
			internal(w, "tenant access", e)
			return
		}
		if ownerOnly && !a.Owner() {
			w.Header().Set("Cache-Control", "no-store")
			fault(w, http.StatusForbidden, "forbidden")
			return
		}
		next(w, r, access{Access: a, User: u})
	})
}
