package server

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/wanglongan587/cloud/v2-server/internal/store"
)

// The handlers find these optional parts of the store by type assertion, so a signature that
// drifts from *store.Store would silently answer 503. Fail the build instead.
var (
	_ Store                 = (*store.Store)(nil)
	_ chatStore             = (*store.Store)(nil)
	_ dashboardStore        = (*store.Store)(nil)
	_ historyStore          = (*store.Store)(nil)
	_ scheduleActionStore   = (*store.Store)(nil)
	_ scheduleStore         = (*store.Store)(nil)
	_ scheduleWorkflowStore = (*store.Store)(nil)
	_ squadStore            = (*store.Store)(nil)
	_ taskRunStore          = (*store.Store)(nil)
	_ taskStore             = (*store.Store)(nil)
	_ workspaceStore        = (*store.Store)(nil)
	_ WorkflowRunStore      = (*store.Store)(nil)
	_ WorkflowStore         = (*store.Store)(nil)
)

const (
	routeTenant = "11111111-1111-4111-8111-111111111111"
	routeParam  = "22222222-2222-4222-8222-222222222222"
)

// authorizationOnlyStore answers sessions and tenant access and nothing else. Its embedded store is
// nil, so a request that gets past authorization into any handler panics and fails the test.
type authorizationOnlyStore struct {
	*store.Store
}

func (authorizationOnlyStore) SessionUser(_ context.Context, token string) (store.User, error) {
	switch token {
	case "outsider", "member", "owner":
		return store.User{ID: map[string]string{
			"outsider": "33333333-3333-4333-8333-333333333333",
			"member":   "44444444-4444-4444-8444-444444444444",
			"owner":    "55555555-5555-4555-8555-555555555555",
		}[token], Status: "active"}, nil
	}
	return store.User{}, store.ErrNotFound
}

func (authorizationOnlyStore) TenantAccess(_ context.Context, tenant, user string) (store.Access, error) {
	if tenant != routeTenant {
		return store.Access{}, store.ErrNotFound
	}
	switch user {
	case "44444444-4444-4444-8444-444444444444":
		return store.Access{TenantID: tenant, UserID: user, Role: store.RoleMember}, nil
	case "55555555-5555-4555-8555-555555555555":
		return store.Access{TenantID: tenant, UserID: user, Role: store.RoleOwner}, nil
	}
	return store.Access{}, store.ErrNotFound
}

var routeParamPattern = regexp.MustCompile(`\{[A-Za-z]+(\.\.\.)?\}`)

// requestFor turns a registered pattern into a concrete request: {tid} is the tenant under test
// and every other wildcard a well-formed id, so only authorization can decide the answer.
func requestFor(t *testing.T, pattern, tenant, session string) *http.Request {
	t.Helper()
	method, path, ok := strings.Cut(pattern, " ")
	if !ok {
		t.Fatalf("route %q has no method", pattern)
	}
	path = strings.ReplaceAll(path, "{tid}", tenant)
	path = routeParamPattern.ReplaceAllString(path, routeParam)
	r := httptest.NewRequest(method, path, strings.NewReader("{}"))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "route-table")
	if session != "" {
		r.AddCookie(&http.Cookie{Name: SessionCookie, Value: session})
	}
	return r
}

func serve(t *testing.T, h http.Handler, r *http.Request) (int, string, http.Header) {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return w.Code, body.Code, w.Header()
}

// Every tenant route goes through the one tenant authorization entry: whoever has no access to
// the tenant gets the same 404 on every route, a signed-out caller gets 401, and the owner-only
// routes refuse a member with 403. A route registered any other way fails here.
func TestEveryTenantRouteRequiresTenantAccess(t *testing.T) {
	rt := newRouter(newHandler(authorizationOnlyStore{}, Options{}))
	var tenantRoutes, ownerRoutes []string
	for _, r := range rt.table {
		if tenantRoute(r.pattern) != (r.level == levelTenant || r.level == levelOwner) {
			t.Errorf("%s: level %d does not match whether it names a tenant", r.pattern, r.level)
		}
		switch r.level {
		case levelTenant:
			tenantRoutes = append(tenantRoutes, r.pattern)
		case levelOwner:
			ownerRoutes = append(ownerRoutes, r.pattern)
		}
	}
	if len(tenantRoutes) < 80 {
		t.Fatalf("only %d tenant routes registered; the table is incomplete", len(tenantRoutes))
	}

	for _, pattern := range append(slices.Clone(tenantRoutes), ownerRoutes...) {
		if code, _, _ := serve(t, rt.mux, requestFor(t, pattern, routeTenant, "")); code != http.StatusUnauthorized {
			t.Errorf("%s signed out: %d, want 401", pattern, code)
		}
		for name, tc := range map[string]struct{ tenant, session string }{
			"outsider":           {routeTenant, "outsider"},
			"member elsewhere":   {"66666666-6666-4666-8666-666666666666", "member"},
			"malformed tenant":   {"not-a-tenant", "owner"},
			"owner of elsewhere": {"66666666-6666-4666-8666-666666666666", "owner"},
		} {
			code, body, header := serve(t, rt.mux, requestFor(t, pattern, tc.tenant, tc.session))
			if code != http.StatusNotFound || body != "not_found" || header.Get("Cache-Control") != "no-store" {
				t.Errorf("%s as %s: %d %q cache=%q, want 404 not_found no-store", pattern, name, code, body, header.Get("Cache-Control"))
			}
		}
	}
	for _, pattern := range ownerRoutes {
		code, body, header := serve(t, rt.mux, requestFor(t, pattern, routeTenant, "member"))
		if code != http.StatusForbidden || body != "forbidden" || header.Get("Cache-Control") != "no-store" {
			t.Errorf("%s as member: %d %q, want 403 forbidden", pattern, code, body)
		}
	}

	// The owner-only operations are exactly these; a new one is a decision, so it changes this list.
	want := []string{
		"POST /api/v2/tenants/{tid}/members",
		"POST /api/v1/tenants/{tid}/spaces/{sid}/plugins/sources",
		"PATCH /api/v1/tenants/{tid}/spaces/{sid}/plugins/sources/{source}",
		"DELETE /api/v1/tenants/{tid}/spaces/{sid}/plugins/sources/{source}",
		"POST /api/v1/tenants/{tid}/spaces/{sid}/plugins/sources/{source}/sync",
		"POST /api/v1/tenants/{tid}/spaces/{sid}/plugins",
		"DELETE /api/v1/tenants/{tid}/spaces/{sid}/plugins",
	}
	slices.Sort(want)
	slices.Sort(ownerRoutes)
	if !slices.Equal(ownerRoutes, want) {
		t.Errorf("owner routes = %v\nwant %v", ownerRoutes, want)
	}
}

// Registration that bypasses the route table would also bypass its level, so the router is the
// only code in this package allowed to touch a ServeMux.
func TestRoutesAreRegisteredOnlyThroughTheRouteTable(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || name == "routes.go" {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && (sel.Sel.Name == "HandleFunc" || sel.Sel.Name == "Handle") {
				t.Errorf("%s: %s registers a route outside the route table", fset.Position(call.Pos()), sel.Sel.Name)
			}
			return true
		})
	}
}

func TestRouterRefusesTenantRoutesWithoutTenantAuthorization(t *testing.T) {
	for name, register := range map[string]func(*router){
		"session route naming a tenant": func(rt *router) {
			rt.session("GET /api/v2/tenants/{tid}/leak", func(http.ResponseWriter, *http.Request, store.User) {})
		},
		"public route naming a tenant": func(rt *router) {
			rt.public("GET /api/v2/tenants/{tid}/leak", func(http.ResponseWriter, *http.Request) {})
		},
		"tenant route without a tenant": func(rt *router) {
			rt.tenant("GET /api/v2/leak", func(http.ResponseWriter, *http.Request, access) {})
		},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("registration accepted")
				}
			}()
			register(&router{h: newHandler(authorizationOnlyStore{}, Options{}), mux: http.NewServeMux()})
		})
	}
}
