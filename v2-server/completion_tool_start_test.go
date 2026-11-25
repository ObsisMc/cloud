//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/listentest"
	"github.com/wanglongan587/cloud/v2-server/internal/projectrepos"
	"github.com/wanglongan587/cloud/v2-server/internal/store"
	"github.com/wanglongan587/cloud/v2-server/internal/store/storetest"
)

// Over a real start: while the execution of a parked node is not settled, this process listens
// nowhere, so the completion tool's endpoint takes no request; once it listens, the execution is
// settled while the session that waits stays open.
func TestTheCompletionToolIsServedOnceParkedExecutionsAreSettled(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := t.Context()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	admin, err := pgx.Connect(ctx, dsn)
	must(err)
	defer admin.Close(ctx)
	schema := fmt.Sprintf("boot_tool_%d", time.Now().UnixNano())
	_, err = admin.Exec(ctx, "CREATE SCHEMA "+schema)
	must(err)
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	dsn = withSearchPath(t, dsn, schema+",public")
	preset, err := store.LoadPreset("users.json")
	must(err)
	db, err := store.Open(ctx, dsn)
	must(err)
	defer db.Close()
	must(db.SeedUsers(ctx, preset))
	root := t.TempDir()
	projects, err := projectrepos.New(db, projectrepos.Options{Root: filepath.Join(root, "repositories")})
	must(err)
	scope, project, err := prepareDemo(ctx, db, projects, preset.Users[0].Email)
	must(err)

	// The held session of a parked interactive node, whose Agent the earlier process started.
	w, _, err := db.CreateWorkflow(ctx, scope.TenantID, "启动", `{"nodes":[{"id":"start","data":{"kind":"start"}}],"edges":[]}`)
	must(err)
	published, err := db.PublishWorkflow(ctx, scope.TenantID, w.ID, "v1", nil)
	must(err)
	run, err := db.CreateWorkflowRun(ctx, scope.TenantID, w.ID, published.ID, store.WorkflowRunInput{ProjectID: project.ID})
	must(err)
	_, err = db.StartWorkflowRun(ctx, scope.TenantID, run.ID)
	must(err)
	row, err := db.CreateWorkflowNodeRun(ctx, scope.TenantID, run.ID, store.WorkflowNodeRunInput{NodeID: "a", NodeType: "agent"})
	must(err)
	session := execution.Session{ID: "parked-session", WorkspaceID: "parked-workspace", Target: &execution.Target{ProjectID: project.ID}}
	turn := execution.Turn{Ref: execution.Ref{SessionID: session.ID, TurnID: "parked-turn"}, Prompt: execution.TextPrompt("起草"),
		Caller: execution.Caller{Kind: execution.CallerWorkflow, ID: run.ID, NodeID: "a", AttemptID: row.ID}}
	must(db.CreateExecutionSession(ctx, scope, session, turn))
	_, err = db.BindWorkflowNodeSession(ctx, scope.TenantID, run.ID, row.ID, session.ID)
	must(err)
	_, err = db.FinishExecutionTurn(ctx, scope, turn.Ref, execution.Result{Status: execution.StatusCompleted, Answer: "初稿", AnswerComplete: true, StopReason: "end_turn"})
	must(err)
	moved, err := db.MoveWorkflowEngineNode(ctx, run.ID, row.ID, store.WorkflowStatusRunning, store.WorkflowStatusPending,
		func([]byte) ([]byte, error) { return []byte(`{"interactive":{"turns":1}}`), nil })
	if err != nil || !moved {
		t.Fatalf("park = %v %v", moved, err)
	}
	must(db.RecordHostExecution(ctx, "parked-execution", session.ID))

	for name, value := range map[string]string{"V2_DATA_DIR": root, "V2_LOGIN": "email", "V2_USERS_FILE": "users.json",
		"V2_NODE_ASSETS_DIR": "", "V2_FRONTEND_DIR": "", "V2_PLUGIN_MARKETPLACE_URL": ""} {
		t.Setenv(name, value)
	}
	_, err = admin.Exec(ctx, "SET search_path TO "+schema)
	must(err)
	// Recovering the parked node comes after the endpoint. It waits there for the test, then is refused,
	// which ends the start.
	later := time.Now().UnixNano() // an advisory lock of this test alone
	_, err = admin.Exec(ctx, fmt.Sprintf(`CREATE FUNCTION refuse_node() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN PERFORM pg_advisory_xact_lock(%d); RAISE EXCEPTION 'node refused'; END $$;
		CREATE TRIGGER refuse_node BEFORE UPDATE ON v2_workflow_node_runs FOR EACH ROW EXECUTE FUNCTION refuse_node()`, later))
	must(err)
	gate, err := pgx.Connect(ctx, dsn)
	must(err)
	defer gate.Close(ctx)
	_, err = gate.Exec(ctx, `SELECT pg_advisory_lock($1)`, later)
	must(err)
	// Holding the record keeps the start from settling it.
	holder, err := pgx.Connect(ctx, dsn)
	must(err)
	defer holder.Close(ctx)
	held, err := holder.Begin(ctx)
	must(err)
	_, err = held.Exec(ctx, `SELECT id FROM v2_host_executions FOR UPDATE`)
	must(err)

	before := map[netip.AddrPort]bool{}
	for _, at := range listentest.Listening(t) {
		before[at] = true
	}
	opened := func() []netip.AddrPort {
		t.Helper()
		var opened []netip.AddrPort
		for _, at := range listentest.Listening(t) {
			if !before[at] {
				opened = append(opened, at)
			}
		}
		return opened
	}
	started := make(chan error, 1)
	go func() {
		started <- runWithListener("127.0.0.1:0", dsn, false, false, true, func(*http.Server) error {
			return errors.New("served although its parked node was not recovered")
		})
	}()
	var once sync.Once
	var ended error
	stop := func() error {
		once.Do(func() {
			_ = held.Rollback(context.Background())
			_, _ = gate.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, later)
			ended = <-started
		})
		return ended
	}
	defer stop()
	// waits returns once the start waits for what connection holds, seen as the real PostgreSQL wait.
	waits := func(connection *pgx.Conn, what string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for blocked := false; !blocked; time.Sleep(5 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("the start does not wait for %s", what)
			}
			select {
			case err := <-started:
				started <- err
				t.Fatalf("the start did not wait for %s: %v", what, err)
			default:
			}
			must(admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)))`,
				int32(connection.PgConn().PID())).Scan(&blocked))
		}
	}
	waits(holder, "the record")
	if at := opened(); len(at) != 0 {
		t.Fatalf("the start listens on %v before the parked execution is settled", at)
	}
	must(held.Rollback(ctx))
	waits(gate, "the parked node")
	at := opened()
	if s, err := db.ReadExecutionSession(ctx, scope, session.ID); err != nil || s.EndedAt != nil {
		t.Errorf("the parked session = %+v %v, want it open", s, err)
	}
	executions := storetest.HostExecutions(t, admin, session.ID)
	if len(executions) != 1 || executions[0].EndedAt == nil || executions[0].Reason != execution.FailureServerRestarted {
		t.Errorf("executions once the endpoint listens = %+v", executions)
	}
	if len(at) != 1 || at[0].Addr() != netip.MustParseAddr("127.0.0.1") {
		t.Errorf("the start listens on %v", at)
	} else if resp, err := http.Post("http://"+at[0].String()+"/mcp/unknown", "application/json", strings.NewReader(`{}`)); err != nil {
		t.Error(err)
	} else {
		// It is the completion tool's, which knows no credential yet.
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("the endpoint answers %d", resp.StatusCode)
		}
	}
	if err := stop(); err == nil || !strings.Contains(err.Error(), "node refused") {
		t.Fatalf("start: %v", err)
	}
}
