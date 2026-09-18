// Command demo-issue-board exercises the migrated Issue Board end-to-end against real
// PostgreSQL 17: migration → bootstrap → HTTP create/move/list → board snapshots → persistence.
// It uses an isolated schema and cleans itself up, so it is safe to run repeatedly.
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/wanglongan587/cloud/internal/api/router"
	"github.com/wanglongan587/cloud/internal/core"
	"github.com/wanglongan587/cloud/internal/simulator"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "demo failed:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	dsn := os.Getenv("DEMO_DATABASE_URL")
	if dsn == "" {
		dsn = "host=127.0.0.1 port=55432 user=ora password=ora-local dbname=ora sslmode=disable"
	}
	config, e := pgx.ParseConfig(dsn)
	if e != nil {
		return e
	}
	admin := stdlib.OpenDB(*config)
	if e = admin.Ping(); e != nil {
		return fmt.Errorf("connect PostgreSQL: %w", e)
	}
	defer admin.Close()

	schema := "demo_issues_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = admin.Exec("CREATE SCHEMA " + schema); e != nil {
		return e
	}
	defer admin.Exec("DROP SCHEMA " + schema + " CASCADE")
	config.RuntimeParams["search_path"] = schema
	pool := stdlib.OpenDB(*config)
	defer pool.Close()
	db, e := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		return e
	}
	store, e := core.NewStore(db)
	if e != nil {
		return e
	}
	if e = store.Migrate(ctx); e != nil {
		return fmt.Errorf("migrate: %w", e)
	}
	fmt.Println("[1/5] Migration applied (issues table on PostgreSQL 17).")

	credentials, e := simulator.NewCredentials()
	if e != nil {
		return e
	}
	auth, e := core.NewAuthenticator("ora-cloud", credentials.Trust)
	if e != nil {
		return e
	}
	gin.SetMode(gin.TestMode)
	log, _ := zap.NewProduction()
	cloud := httptest.NewServer(router.New(store, auth, log))
	defer cloud.Close()

	bootstrap, e := store.Bootstrap(ctx, "Demo Board", "demo", "alice", "Alice")
	if e != nil {
		return e
	}
	tid := bootstrap.S("tenantId")
	client := &simulator.Client{URL: cloud.URL, Credentials: credentials, HTTP: &http.Client{Timeout: 10 * time.Second}, Subject: "demo-gateway"}
	user := core.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "alice"}, Source: "demo", DisplayName: "Alice"}
	base := "/api/v1/tenants/" + tid

	call := func(method, path, key string, body core.Object) core.Object {
		out, status, err := client.Call(ctx, method, base+path, "gateway", core.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "demo-gateway"}}, &user, key, body)
		if err != nil {
			fmt.Fprintln(os.Stderr, "call:", err)
			os.Exit(1)
		}
		if status != 200 {
			fmt.Fprintf(os.Stderr, "%s %s: %d %v\n", method, path, status, out)
			os.Exit(1)
		}
		return out
	}
	show := func(label string) {
		list := call("GET", "/issues", "", nil)
		items := list["items"].([]any)
		fmt.Printf("\n%s\n", label)
		column := ""
		for _, raw := range items {
			o := core.Object(raw.(map[string]any))
			if o.S("status") != column {
				column = o.S("status")
				fmt.Printf("  [%s]\n", column)
			}
			fmt.Printf("    pos=%6.2f  (%s)  %s\n", o["position"].(float64), o.S("priority"), o.S("title"))
		}
	}

	fmt.Println("[2/5] Tenant bootstrapped; creating issues via HTTP…")
	login := call("POST", "/issues", "d1", core.Object{"title": "Fix login", "status": "todo", "priority": "urgent"}).O("resource")
	call("POST", "/issues", "d2", core.Object{"title": "Onboarding flow", "status": "todo", "priority": "medium"})
	api := call("POST", "/issues", "d3", core.Object{"title": "API rate limits", "status": "in_progress", "priority": "high"}).O("resource")
	docs := call("POST", "/issues", "d4", core.Object{"title": "Docs refresh", "status": "done", "priority": "low"}).O("resource")
	show("[3/5] Board after create (each new issue lands at the column top):")

	fmt.Println("[4/5] Moving cards…")
	// Status change with no neighbours → re-rank to the top of the destination column.
	call("POST", "/issues/"+login.S("id")+"/move", "m1", core.Object{"status": "in_progress", "version": login.N("version")})
	// Cross-column move between two neighbours → fractional midpoint (-0.5).
	call("POST", "/issues/"+docs.S("id")+"/move", "m2", core.Object{"status": "in_progress", "beforeId": login.S("id"), "afterId": api.S("id"), "version": docs.N("version")})
	show("[5/5] Board after move:")

	// Persistence: the very same rows are visible through a fresh SQL connection.
	var count int
	if e = store.Pool.QueryRowContext(ctx, "SELECT count(*) FROM issues WHERE tenant_id=$1 AND deleted_at IS NULL", tid).Scan(&count); e != nil {
		return e
	}
	fmt.Printf("\nPersistence check: %d live issue rows in PostgreSQL schema %q (dropped on exit).\n", count, schema)
	fmt.Println("The same semantics are covered at HTTP level by `integration/issues_test.go` in CI.")
	return nil
}
