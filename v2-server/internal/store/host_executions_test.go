package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/store"
	"github.com/wanglongan587/cloud/v2-server/internal/store/storetest"
)

// hostExecution returns the one execution recorded for a session.
func hostExecution(t *testing.T, db *pgx.Conn, sid string) storetest.HostExecution {
	t.Helper()
	executions := storetest.HostExecutions(t, db, sid)
	if len(executions) != 1 {
		t.Fatalf("executions of %s = %+v", sid, executions)
	}
	return executions[0]
}

func TestAHostExecutionIsSettledOnce(t *testing.T) {
	s, db := storetest.OpenWithConn(t)
	user, tenant := storetest.Login(t, s, "host-executions")
	scope := execution.Scope{TenantID: tenant, UserID: user}
	ctx := context.Background()
	project := workflowProject(t, s, user, tenant, "project-host")
	workflowAgentSession(t, s, scope, project.ID, "run", "node", "session")
	if err := s.RecordHostExecution(ctx, "lost", "unknown-session"); !errors.Is(err, execution.ErrSessionEnded) {
		t.Fatalf("record for an unknown session = %v", err)
	}
	for _, id := range []string{"first", "second"} {
		if err := s.RecordHostExecution(ctx, id, "session"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecordHostExecution(ctx, "first", "session"); err == nil {
		t.Fatal("an execution was recorded twice")
	}
	for _, reason := range []string{execution.FailureNodeLost, "interrupted"} {
		if err := s.EndHostExecution(ctx, "first", reason); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.EndHostExecution(ctx, "unknown", "interrupted"); err != nil {
		t.Fatal(err)
	}
	// Ending the session settles what still runs, in the same transaction, and nothing else.
	if err := s.EndExecutionSession(ctx, scope, "session"); err != nil {
		t.Fatal(err)
	}
	executions := storetest.HostExecutions(t, db, "session")
	if len(executions) != 2 {
		t.Fatalf("executions = %+v", executions)
	}
	for i, reason := range []string{execution.FailureNodeLost, execution.HostSessionEnded} {
		if executions[i].EndedAt == nil || executions[i].Reason != reason {
			t.Fatalf("execution %d = %+v, want %s", i, executions[i], reason)
		}
	}
	if err := s.RecordHostExecution(ctx, "third", "session"); !errors.Is(err, execution.ErrSessionEnded) {
		t.Fatalf("record for an ended session = %v", err)
	}
}

// A restart settles every execution still running, with its name, whether its session is kept or
// ended; a session without one gets none.
func TestRecoverySettlesRunningHostExecutions(t *testing.T) {
	s, db := storetest.OpenWithConn(t)
	user, tenant := storetest.Login(t, s, "host-recovery")
	scope := execution.Scope{TenantID: tenant, UserID: user}
	ctx := context.Background()
	project := workflowProject(t, s, user, tenant, "project-host")
	w, published := publishedWorkflow(t, s, tenant, "宿主恢复", "v1")
	run, err := s.CreateWorkflowRun(ctx, tenant, w.ID, published.ID, store.WorkflowRunInput{ProjectID: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartWorkflowRun(ctx, tenant, run.ID); err != nil {
		t.Fatal(err)
	}
	attempt := func(nodeID, sid string, park bool) {
		t.Helper()
		node, err := s.CreateWorkflowNodeRun(ctx, tenant, run.ID, store.WorkflowNodeRunInput{NodeID: nodeID, NodeType: "agent"})
		if err != nil {
			t.Fatal(err)
		}
		workflowAgentSession(t, s, scope, project.ID, run.ID, node.ID, sid)
		if _, err := s.BindWorkflowNodeSession(ctx, tenant, run.ID, node.ID, sid); err != nil {
			t.Fatal(err)
		}
		if park {
			if moved, err := s.MoveWorkflowEngineNode(ctx, run.ID, node.ID, store.WorkflowStatusRunning, store.WorkflowStatusPending,
				func([]byte) ([]byte, error) { return []byte(`{"interactive":{"turns":1}}`), nil }); err != nil || !moved {
				t.Fatalf("park = %v %v", moved, err)
			}
		}
	}
	attempt("a", "parked", true)
	attempt("b", "executing", false)
	attempt("c", "node-lost", true)
	attempt("d", "end-turn", true)
	attempt("e", "upgraded", true) // its Agent started before executions were recorded
	for _, sid := range []string{"parked", "executing", "node-lost"} {
		if err := s.RecordHostExecution(ctx, "x-"+sid, sid); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.EndHostExecution(ctx, "x-node-lost", execution.FailureNodeLost); err != nil {
		t.Fatal(err)
	}
	lost := hostExecution(t, db, "node-lost")

	kept, err := s.RecoverExecutions(ctx)
	if err != nil || len(kept) != 4 {
		t.Fatalf("kept = %+v %v", kept, err)
	}
	for _, sid := range []string{"parked", "executing"} {
		if x := hostExecution(t, db, sid); x.EndedAt == nil || x.Reason != execution.FailureServerRestarted {
			t.Fatalf("%s after the restart = %+v", sid, x)
		}
	}
	// Its Node was lost before the restart, and no turn started another execution since.
	if x := hostExecution(t, db, "node-lost"); x.Reason != execution.FailureNodeLost || !x.EndedAt.Equal(*lost.EndedAt) {
		t.Fatalf("node-lost after the restart = %+v, was %+v", x, lost)
	}
	for _, sid := range []string{"end-turn", "upgraded"} {
		if executions := storetest.HostExecutions(t, db, sid); len(executions) != 0 {
			t.Fatalf("executions of %s = %+v", sid, executions)
		}
	}
	for sid, open := range map[string]bool{"parked": true, "node-lost": true, "end-turn": true, "upgraded": true, "executing": false} {
		if session, err := s.ReadExecutionSession(ctx, scope, sid); err != nil || (session.EndedAt == nil) != open {
			t.Fatalf("session %s = %+v %v, want open %v", sid, session, err, open)
		}
	}
	// A parked session waits through another restart with no execution of its own: nothing changes.
	settled := hostExecution(t, db, "parked")
	if _, err := s.RecoverExecutions(ctx); err != nil {
		t.Fatal(err)
	}
	if x := hostExecution(t, db, "parked"); !x.EndedAt.Equal(*settled.EndedAt) || x.Reason != settled.Reason {
		t.Fatalf("parked after another restart = %+v, was %+v", x, settled)
	}
	// What Node reports of the old execution later neither rewrites the reason nor ends the session.
	if err := s.EndHostExecution(ctx, "x-parked", "interrupted"); err != nil {
		t.Fatal(err)
	}
	if x := hostExecution(t, db, "parked"); x.Reason != execution.FailureServerRestarted {
		t.Fatalf("parked after Node's report = %+v", x)
	}
	if err := s.RecordHostExecution(ctx, "x-parked-2", "parked"); err != nil {
		t.Fatalf("the next turn's execution: %v", err)
	}
}

// A session that ends while the reason of its execution is still to be written keeps that reason,
// whichever write comes first.
func TestEndingASessionKeepsADecidedReason(t *testing.T) {
	s, db := storetest.OpenWithConn(t)
	user, tenant := storetest.Login(t, s, "host-decided")
	scope := execution.Scope{TenantID: tenant, UserID: user}
	ctx := context.Background()
	project := workflowProject(t, s, user, tenant, "project-host")
	for _, sid := range []string{"session", "other"} {
		workflowAgentSession(t, s, scope, project.ID, "run", "node-"+sid, sid)
	}
	for id, sid := range map[string]string{"failed": "session", "running": "session", "elsewhere": "other"} {
		if err := s.RecordHostExecution(ctx, id, sid); err != nil {
			t.Fatal(err)
		}
	}
	// The driver's write of "failed" did not succeed; the session ends before it is repeated.
	if err := s.EndHostExecutionSession(ctx, scope, "session", map[string]string{"failed": "agent_failed", "elsewhere": "agent_failed"}); err != nil {
		t.Fatal(err)
	}
	if err := s.EndHostExecution(ctx, "failed", "agent_failed"); err != nil {
		t.Fatal(err)
	}
	executions := storetest.HostExecutions(t, db, "session")
	if len(executions) != 2 {
		t.Fatalf("executions = %+v", executions)
	}
	want := map[string]string{"failed": "agent_failed", "running": execution.HostSessionEnded}
	for _, x := range executions {
		if x.EndedAt == nil || x.Reason != want[x.ID] {
			t.Fatalf("%s = %+v, want %s", x.ID, x, want[x.ID])
		}
	}
	if x := hostExecution(t, db, "other"); x.EndedAt != nil {
		t.Fatalf("another session's execution = %+v", x)
	}
}

// An execution is not recorded for a session that is being ended: the record waits for the end and
// then finds it. Otherwise it would stay running in a session that no longer settles anything.
func TestARecordWaitsForTheEndOfItsSession(t *testing.T) {
	s, db := storetest.OpenWithConn(t)
	user, tenant := storetest.Login(t, s, "host-ending")
	scope := execution.Scope{TenantID: tenant, UserID: user}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	project := workflowProject(t, s, user, tenant, "project-host")
	workflowAgentSession(t, s, scope, project.ID, "run", "node", "session")
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	// What endExecutionSession does, up to its commit.
	if _, err := tx.Exec(ctx, `SELECT id FROM v2_execution_sessions WHERE id = 'session' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE v2_execution_sessions SET ended_at = now() WHERE id = 'session'`); err != nil {
		t.Fatal(err)
	}
	recorded := make(chan error, 1)
	go func() { recorded <- s.RecordHostExecution(ctx, "late", "session") }()
	// Observe the real PostgreSQL lock wait instead of relying on a scheduling race or sleep.
	for blocked := false; !blocked; time.Sleep(time.Millisecond) {
		select {
		case err := <-recorded:
			t.Fatalf("the record did not wait for the end of its session: %v", err)
		default:
		}
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pg_backend_pid() = ANY(pg_blocking_pids(pid)))`).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-recorded; !errors.Is(err, execution.ErrSessionEnded) {
		t.Fatalf("record in an ended session = %v", err)
	}
	if executions := storetest.HostExecutions(t, db, "session"); len(executions) != 0 {
		t.Fatalf("executions of an ended session = %+v", executions)
	}
}
