package integration

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/wanglongan587/cloud/internal/controlpb"
)

// Initialization cancellation is a failure settlement; rollback retains the termination plan.
func TestWorkspaceFailedHookAndForceEvidenceCommitTogether(t *testing.T) {
	f := setup(t)
	p := f.create("failed-workspace-hook")
	f.drain()
	run := f.insertAgentRun(t, p.O("resource").S("id"))
	created, err := f.store.CreateRunWorkspace(t.Context(), run)
	must(t, err)
	wid, operation := created.O("workspace").S("id"), created.O("operation").S("id")
	force := uuid.NewString()
	// The isolated initialization has no sandbox yet. Its durable force intent is therefore
	// an empty exact-target plan; this fixture does not claim actual Docker termination evidence.
	_, err = f.store.Pool.Exec(`INSERT INTO runtime_force_stops(id,tenant_id,workspace_id,actor_user_id,reason,state,control_epoch,runtime_generation)
	 SELECT $1,tenant_id,id,creator_user_id,'test cancellation','registered',1,runtime_generation FROM workspaces WHERE id=$2`, force, wid)
	must(t, err)
	_, err = f.store.Pool.Exec("CREATE TABLE failed_workspace_hook_evidence(run_id uuid PRIMARY KEY)")
	must(t, err)
	fail := true
	f.store.OnRunWorkspaceSettled = func(ctx context.Context, tx *sql.Tx, gotRun, status string) error {
		if gotRun != run || status != "failed" {
			return errors.New("incomplete failure evidence")
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO failed_workspace_hook_evidence VALUES($1)", run); err != nil {
			return err
		}
		if fail {
			return errors.New("injected failure transition rollback")
		}
		return nil
	}
	client := controlpb.NewRuntimeControlServiceClient(f.controlConn)
	req := &controlpb.ConfirmForceStopRequest{SubmissionId: "failed-workspace-hook", Epoch: f.controller.Epoch, ForceStopId: force, Version: 1}
	_, err = client.ConfirmForceStop(asController(f.client.Subject), req)
	if err == nil || f.scalar("SELECT count(*) FROM failed_workspace_hook_evidence") != 0 || f.scalar("SELECT count(*) FROM operations WHERE id=$1 AND state='failed'", operation) != 0 || f.scalar("SELECT count(*) FROM runtime_force_stops WHERE id=$1 AND state='registered'", force) != 1 {
		t.Fatal("hook failure committed a partial cancellation")
	}
	fail = false
	for range 2 {
		_, err = client.ConfirmForceStop(asController(f.client.Subject), req)
		must(t, err)
	}
	if f.scalar("SELECT count(*) FROM failed_workspace_hook_evidence") != 1 || f.scalar("SELECT count(*) FROM operations WHERE id=$1 AND state='failed'", operation) != 1 {
		t.Fatal("failed settlement was not exactly once")
	}
}
