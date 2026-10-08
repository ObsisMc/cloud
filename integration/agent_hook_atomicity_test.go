package integration

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"

	"github.com/wanglongan587/cloud/internal/controlpb"
	"github.com/wanglongan587/cloud/internal/core"
)

func TestSessionEndedHookRollsBackAndReplaysOnce(t *testing.T) {
	f := setup(t)
	run, execution, node, incarnation := f.registeredSession(t, "session-hook")
	_, err := f.store.Pool.Exec("CREATE TABLE session_hook_evidence(execution_id text PRIMARY KEY)")
	must(t, err)
	fail := true
	f.store.OnSessionEnded = func(ctx context.Context, tx *sql.Tx, gotRun, gotExecution string, ended core.Object) error {
		if gotRun != run || gotExecution != execution || ended.S("reason") != "user_ended" {
			return errors.New("incomplete hook input")
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO session_hook_evidence VALUES($1)", execution); err != nil {
			return err
		}
		if fail {
			return errors.New("injected rollback")
		}
		return nil
	}
	req := &controlpb.TakeOverNodeEventRequest{SubmissionId: "session-hook-end", Epoch: f.controller.Epoch, OperationId: run, ExecutionId: execution, Sequence: 1, Event: []byte("end"), Result: &controlpb.ExecutionResult{Node: &controlpb.NodeIdentity{NodeId: node, NodeIncarnationId: incarnation}, Outcome: &controlpb.ExecutionResult_AgentSessionEnded{AgentSessionEnded: &controlpb.AgentSessionEnded{Reason: controlpb.AgentSessionEndReason_AGENT_SESSION_END_REASON_USER_ENDED}}}}
	_, err = f.executions.TakeOverNodeEvent(asController(f.client.Subject), req)
	expectStatus(t, err, codes.Aborted, controlpb.ErrorCode_ERROR_CODE_CONFLICT)
	if f.scalar("SELECT count(*) FROM session_hook_evidence") != 0 || f.scalar("SELECT count(*) FROM node_event_receipts WHERE execution_id=$1", execution) != 0 || f.scalar("SELECT count(*) FROM node_executions WHERE execution_id=$1 AND result IS NOT NULL", execution) != 0 {
		t.Fatal("failed session hook committed")
	}
	fail = false
	_, err = f.executions.TakeOverNodeEvent(asController(f.client.Subject), req)
	must(t, err)
	req.SubmissionId = "session-hook-replay"
	_, err = f.executions.TakeOverNodeEvent(asController(f.client.Subject), req)
	must(t, err)
	if f.scalar("SELECT count(*) FROM session_hook_evidence") != 1 {
		t.Fatal("session hook ran twice")
	}
}

func TestWorkspaceReadyAndDeletedHooksShareTheOperationTransaction(t *testing.T) {
	f := setup(t)
	p := f.create("workspace-hooks")
	f.drain()
	run := f.insertAgentRun(t, p.O("resource").S("id"))
	_, err := f.store.Pool.Exec("CREATE TABLE workspace_hook_evidence(kind text PRIMARY KEY)")
	must(t, err)
	fail := true
	write := func(ctx context.Context, tx *sql.Tx, kind string) error {
		if _, err := tx.ExecContext(ctx, "INSERT INTO workspace_hook_evidence VALUES($1)", kind); err != nil {
			return err
		}
		if fail {
			return errors.New("injected Workspace hook rollback")
		}
		return nil
	}
	f.store.OnRunWorkspaceSettled = func(ctx context.Context, tx *sql.Tx, gotRun, status string) error {
		if gotRun != run || status != "ready" {
			return errors.New("unexpected settlement")
		}
		return write(ctx, tx, status)
	}
	created, err := f.store.CreateRunWorkspace(t.Context(), run)
	must(t, err)
	wid := created.O("workspace").S("id")
	f.acknowledgeSimulatorBindings()
	err = f.controller.Drain(t.Context())
	if err == nil || f.scalar("SELECT count(*) FROM workspace_hook_evidence") != 0 || f.scalar("SELECT count(*) FROM workspaces WHERE id=$1 AND admission_open", wid) != 0 {
		t.Fatal("ready hook failure committed readiness")
	}
	fail = false
	f.drain()
	_, err = f.store.CreateRunWorkspace(t.Context(), run)
	must(t, err)
	if f.scalar("SELECT count(*) FROM workspace_hook_evidence WHERE kind='ready'") != 1 {
		t.Fatal("create replay invoked readiness hook")
	}
	f.store.OnRunWorkspaceDeleted = func(ctx context.Context, tx *sql.Tx, gotRun string) error {
		if gotRun != run {
			return errors.New("unexpected run")
		}
		return write(ctx, tx, "deleted")
	}
	fail = true
	_, err = f.store.DeleteRunWorkspace(t.Context(), run)
	must(t, err)
	f.acknowledgeSimulatorBindings()
	err = f.controller.Drain(t.Context())
	if err == nil || f.scalar("SELECT count(*) FROM workspace_hook_evidence WHERE kind='deleted'") != 0 || f.scalar("SELECT count(*) FROM workspaces WHERE id=$1 AND deleted_at IS NOT NULL", wid) != 0 {
		t.Fatal("deletion hook failure committed cleanup")
	}
	fail = false
	f.drain()
	_, err = f.store.DeleteRunWorkspace(t.Context(), run)
	must(t, err)
	if f.scalar("SELECT count(*) FROM workspace_hook_evidence WHERE kind='deleted'") != 1 {
		t.Fatal("deletion replay invoked hook")
	}
}

func TestUnconfiguredRevisionStorageSkipsOnceAndRollsBackWithBusiness(t *testing.T) {
	f := setup(t)
	run, session, _, _ := f.registeredSession(t, "skip-storage")
	var wid, sandbox, node string
	must(t, f.store.Pool.QueryRow("SELECT id::text FROM workspaces WHERE issue_run_id=$1", run).Scan(&wid))
	sandbox, node, _ = f.liveNode(t, wid)
	input := core.Object{"kind": "deliver_revision", "sessionExecutionId": session, "checkoutExecutionId": "checkout-1", "baseCommit": f.commit}
	target := core.Object{"workspaceId": wid, "sandboxInstanceId": sandbox, "nodeId": node}
	_, err := f.store.Pool.Exec("CREATE TABLE skip_hook_evidence(run_id uuid PRIMARY KEY)")
	must(t, err)
	fail := true
	f.store.OnDeliverySettled = func(ctx context.Context, tx *sql.Tx, gotRun, execution string, settled core.Object) error {
		if gotRun != run || execution != "" || settled.S("outcome") != "skipped" {
			return errors.New("skip fabricated an execution")
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO skip_hook_evidence VALUES($1)", run); err != nil {
			return err
		}
		if fail {
			return errors.New("rollback skipped transition")
		}
		return nil
	}
	_, err = f.store.EnqueueExecutionWork(t.Context(), run, "deliver_revision", input, target, time.Time{})
	if err == nil || f.scalar("SELECT count(*) FROM revision_delivery_skips") != 0 || f.scalar("SELECT count(*) FROM skip_hook_evidence") != 0 {
		t.Fatal("failed skipped transition committed")
	}
	fail = false
	for range 2 {
		out, err := f.store.EnqueueExecutionWork(t.Context(), run, "deliver_revision", input, target, time.Time{})
		must(t, err)
		if !out.B("skipped") {
			t.Fatal("unconfigured storage offered work")
		}
	}
	if f.scalar("SELECT count(*) FROM skip_hook_evidence") != 1 || f.scalar("SELECT count(*) FROM execution_work WHERE kind='deliver_revision'") != 0 || f.scalar("SELECT count(*) FROM revisions") != 0 {
		t.Fatal("skip was not idempotent")
	}
}
