package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/store"
	"github.com/wanglongan587/cloud/v2-server/internal/store/storetest"
)

// hostExecution returns the one execution recorded for a session.
func hostExecution(t *testing.T, s *store.Store, sid string) store.HostExecution {
	t.Helper()
	executions, err := s.HostExecutions(context.Background(), sid)
	if err != nil || len(executions) != 1 {
		t.Fatalf("executions of %s = %+v %v", sid, executions, err)
	}
	return executions[0]
}

func TestAHostExecutionIsSettledOnce(t *testing.T) {
	s := storetest.Open(t)
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
	executions, err := s.HostExecutions(ctx, "session")
	if err != nil || len(executions) != 2 {
		t.Fatalf("executions = %+v %v", executions, err)
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
	s := storetest.Open(t)
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
	lost := hostExecution(t, s, "node-lost")

	kept, err := s.RecoverExecutions(ctx)
	if err != nil || len(kept) != 4 {
		t.Fatalf("kept = %+v %v", kept, err)
	}
	for _, sid := range []string{"parked", "executing"} {
		if x := hostExecution(t, s, sid); x.EndedAt == nil || x.Reason != execution.FailureServerRestarted {
			t.Fatalf("%s after the restart = %+v", sid, x)
		}
	}
	// Its Node was lost before the restart, and no turn started another execution since.
	if x := hostExecution(t, s, "node-lost"); x.Reason != execution.FailureNodeLost || !x.EndedAt.Equal(*lost.EndedAt) {
		t.Fatalf("node-lost after the restart = %+v, was %+v", x, lost)
	}
	for _, sid := range []string{"end-turn", "upgraded"} {
		if executions, err := s.HostExecutions(ctx, sid); err != nil || len(executions) != 0 {
			t.Fatalf("executions of %s = %+v %v", sid, executions, err)
		}
	}
	for sid, open := range map[string]bool{"parked": true, "node-lost": true, "end-turn": true, "upgraded": true, "executing": false} {
		if session, err := s.ReadExecutionSession(ctx, scope, sid); err != nil || (session.EndedAt == nil) != open {
			t.Fatalf("session %s = %+v %v, want open %v", sid, session, err, open)
		}
	}
	// A parked session waits through another restart with no execution of its own: nothing changes.
	settled := hostExecution(t, s, "parked")
	if _, err := s.RecoverExecutions(ctx); err != nil {
		t.Fatal(err)
	}
	if x := hostExecution(t, s, "parked"); !x.EndedAt.Equal(*settled.EndedAt) || x.Reason != settled.Reason {
		t.Fatalf("parked after another restart = %+v, was %+v", x, settled)
	}
	// What Node reports of the old execution later neither rewrites the reason nor ends the session.
	if err := s.EndHostExecution(ctx, "x-parked", "interrupted"); err != nil {
		t.Fatal(err)
	}
	if x := hostExecution(t, s, "parked"); x.Reason != execution.FailureServerRestarted {
		t.Fatalf("parked after Node's report = %+v", x)
	}
	if err := s.RecordHostExecution(ctx, "x-parked-2", "parked"); err != nil {
		t.Fatalf("the next turn's execution: %v", err)
	}
}
