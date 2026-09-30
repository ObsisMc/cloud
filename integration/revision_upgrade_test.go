package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestRevisionUpgradePreservesRegisteredInputAndRawNodeEvidence(t *testing.T) {
	pool, _ := testSchema(t, "test_revision_upgrade_")
	entries, err := os.ReadDir(filepath.Join("..", "internal", "core", "migrations"))
	must(t, err)
	var versions []string
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".sql" && entry.Name() < "0027" {
			versions = append(versions, entry.Name())
		}
	}
	applyMigrationsUpTo(t, pool, versions)
	user, tenant, issue, run, work := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	tx, err := pool.BeginTx(t.Context(), nil)
	must(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	for _, item := range []struct {
		query string
		args  []any
	}{
		{"INSERT INTO users(id,status) VALUES($1,'active')", []any{user}},
		{"INSERT INTO tenants(id,name,status) VALUES($1,'Upgrade','active')", []any{tenant}},
		{"INSERT INTO tenant_memberships(tenant_id,user_id,role,status) VALUES($1,$2,'admin','active')", []any{tenant, user}},
		{"INSERT INTO issues(id,tenant_id,creator_user_id,title,number) VALUES($1,$2,$3,'Revision upgrade',1)", []any{issue, tenant, user}},
		{"INSERT INTO issue_runs(id,tenant_id,issue_id,executor_type,executor_id) VALUES($1,$2,$3,'agent',$4)", []any{run, tenant, issue, uuid.NewString()}},
		{`INSERT INTO execution_work(id,run_id,kind,input,target,execution_id) VALUES($1,$2,'deliver_revision','{"kind":"deliver_revision","bundleKey":"historical/bundle","historyKey":"historical/history"}','{}','historical-delivery')`, []any{work, run}},
		{`INSERT INTO node_executions(execution_id,kind,operation_id,work_id,node_id,node_operation_id,input,result,dispatched_epoch,last_event_sequence) SELECT execution_id,kind,run_id,id,'historical-node','historical-operation',input,'{"outcome":"revision_failed","reason":"upload_failed"}',1,1 FROM execution_work WHERE id=$1`, []any{work}},
		{`INSERT INTO node_event_receipts(execution_id,sequence,event) VALUES('historical-delivery',1,'original-evidence')`, nil},
	} {
		_, err = tx.Exec(item.query, item.args...)
		must(t, err)
	}
	must(t, tx.Commit())
	var beforeInput, beforeResult string
	must(t, pool.QueryRow("SELECT input::text,result::text FROM node_executions WHERE execution_id='historical-delivery'").Scan(&beforeInput, &beforeResult))
	store := newStoreOnSchema(t, pool)
	must(t, store.Migrate(t.Context()))
	must(t, store.Migrate(t.Context()))
	must(t, store.CheckSchema(t.Context()))
	var afterInput, afterResult, event string
	must(t, pool.QueryRow("SELECT e.input::text,e.result::text,r.event FROM node_executions e JOIN node_event_receipts r ON r.execution_id=e.execution_id WHERE e.execution_id='historical-delivery'").Scan(&afterInput, &afterResult, &event))
	if beforeInput != afterInput || beforeResult != afterResult || event != "original-evidence" {
		t.Fatal("upgrade changed fixed execution input or raw Node evidence")
	}
	var count int
	must(t, pool.QueryRow("SELECT (SELECT count(*) FROM revisions)+(SELECT count(*) FROM revision_verifications)").Scan(&count))
	if count != 0 {
		t.Fatal("upgrade inferred verified delivery from historical Node declarations")
	}
}
