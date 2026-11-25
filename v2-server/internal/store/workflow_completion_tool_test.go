package store_test

import (
	"context"
	"testing"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/store"
	"github.com/wanglongan587/cloud/v2-server/internal/store/storetest"
)

// A node's turn is live only while its row runs, a turn of its session has not ended and the execution
// that asks runs; each condition refuses on its own.
func TestWorkflowNodeTurnLive(t *testing.T) {
	s := storetest.Open(t)
	user, tenant := storetest.Login(t, s, "turn-live")
	scope := execution.Scope{TenantID: tenant, UserID: user}
	ctx := context.Background()
	run, project := startedRun(t, s, user, tenant, "完成工具")
	node, err := s.CreateWorkflowNodeRun(ctx, tenant, run.ID, store.WorkflowNodeRunInput{NodeID: "agent-1", NodeType: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	workflowAgentSession(t, s, scope, project, run.ID, node.ID, "live")
	if err := s.RecordHostExecution(ctx, "x-live", "live"); err != nil {
		t.Fatal(err)
	}
	live := func(want bool, when string) {
		t.Helper()
		if got, err := s.WorkflowNodeTurnLive(ctx, node.ID, "live", "x-live"); err != nil || got != want {
			t.Fatalf("%s: live = %v %v, want %v", when, got, err, want)
		}
	}
	begin := func(id string) execution.Ref {
		t.Helper()
		ref := execution.Ref{SessionID: "live", TurnID: id}
		turn := execution.Turn{Ref: ref, Caller: execution.Caller{Kind: execution.CallerWorkflow, ID: run.ID, NodeID: "agent-1", AttemptID: node.ID + "/" + id}, Prompt: execution.TextPrompt("再改")}
		if err := s.AppendExecutionTurn(ctx, scope, "live", turn); err != nil {
			t.Fatal(err)
		}
		return ref
	}
	second := begin("turn-1")
	live(false, "the node is not bound to the session")
	if _, err := s.BindWorkflowNodeSession(ctx, tenant, run.ID, node.ID, "live"); err != nil {
		t.Fatal(err)
	}
	live(true, "a turn runs")
	move := func(from, to string) {
		t.Helper()
		if moved, err := s.MoveWorkflowEngineNode(ctx, run.ID, node.ID, from, to, func(p []byte) ([]byte, error) { return p, nil }); err != nil || !moved {
			t.Fatalf("move to %s = %v %v", to, moved, err)
		}
	}
	move(store.WorkflowStatusRunning, store.WorkflowStatusPending)
	live(false, "the node is parked while a turn has not ended")
	move(store.WorkflowStatusPending, store.WorkflowStatusRunning)
	live(true, "the node runs again")
	// The turn ends before the node leaves running: the report of that turn is settled by then.
	if _, err := s.FinishExecutionTurn(ctx, scope, second, execution.Result{Status: execution.StatusCompleted, Answer: "定稿", AnswerComplete: true, StopReason: "end_turn"}); err != nil {
		t.Fatal(err)
	}
	live(false, "the turn ended while the node still runs")
	begin("turn-2")
	live(true, "the next turn runs")
	workflowAgentSession(t, s, scope, project, run.ID, "another-attempt", "other")
	if err := s.RecordHostExecution(ctx, "x-other", "other"); err != nil {
		t.Fatal(err)
	}
	for id, what := range map[string]string{"x-none": "an execution without a record", "x-other": "an execution of another session", "": "no execution"} {
		if got, err := s.WorkflowNodeTurnLive(ctx, node.ID, "live", id); err != nil || got {
			t.Fatalf("%s: live = %v %v", what, got, err)
		}
	}
	// What an Agent that ended or lost its Node leaves until its turn ends.
	if err := s.EndHostExecution(ctx, "x-live", "agent_failed"); err != nil {
		t.Fatal(err)
	}
	live(false, "the execution ended while its turn still runs")
	if err := s.RecordHostExecution(ctx, "x-next", "live"); err != nil {
		t.Fatal(err)
	}
	live(false, "another execution of the session runs")
	if got, err := s.WorkflowNodeTurnLive(ctx, node.ID, "live", "x-next"); err != nil || !got {
		t.Fatalf("the next execution: live = %v %v", got, err)
	}
	if _, err := s.CancelWorkflowRun(ctx, tenant, run.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := s.WorkflowNodeTurnLive(ctx, node.ID, "live", "x-next"); err != nil || got {
		t.Fatalf("the run is over: live = %v %v", got, err)
	}
}
