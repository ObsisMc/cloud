package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestAgentUpgradeRetainsHistoricalPluginEffectsAndRestartsNodeStep(t *testing.T) {
	pool, _ := testSchema(t, "test_plugin_upgrade_")
	entries, err := os.ReadDir(filepath.Join("..", "internal", "core", "migrations"))
	must(t, err)
	var versions []string
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".sql" && entry.Name() < "0024" {
			versions = append(versions, entry.Name())
		}
	}
	applyMigrationsUpTo(t, pool, versions)
	user, tenant, space, project, workspace, operation, effect := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
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
		{"INSERT INTO collab_workspaces(id,tenant_id,name,slug,created_by) VALUES($1,$2,'Upgrade','plugin-upgrade',$3)", []any{space, tenant, user}},
		{"INSERT INTO projects(id,tenant_id,owner_user_id,space_id,name,repository_url,default_branch,lifecycle) VALUES($1,$2,$3,$4,'Upgrade','https://example.invalid/repo.git','main','active')", []any{project, tenant, user, space}},
		{"INSERT INTO workspaces(id,tenant_id,owner_user_id,project_id,kind,desired_state,observed_state,requested_ref) VALUES($1,$2,$3,$4,'main','running','ready','main')", []any{workspace, tenant, user, project}},
		{`INSERT INTO operations(id,tenant_id,actor_user_id,project_id,workspace_id,kind,state,step,request,idempotency_key,request_hash,controller_epoch,error_code,retry_at) VALUES($1,$2,$3,$4,$5,'install_plugin','retry_wait','plugin','{"plugins":[{"pluginId":"historical/plugin"}]}','legacy-plugin','legacy-hash',1,'external_failure',now()+interval '1 day')`, []any{operation, tenant, user, project, workspace}},
		{`INSERT INTO external_effects(id,operation_id,project_id,workspace_id,kind,state,reconciled_epoch,request) VALUES($1,$2,$3,$4,'plugin_ensure','running',1,'{"historicalIntent":"retain exactly"}')`, []any{effect, operation, project, workspace}},
	} {
		_, err = tx.Exec(item.query, item.args...)
		must(t, err)
	}
	must(t, tx.Commit())
	store := newStoreOnSchema(t, pool)
	must(t, store.Migrate(t.Context()))
	must(t, store.Migrate(t.Context()))
	var state, request string
	var valid bool
	must(t, pool.QueryRow("SELECT state,request::text FROM external_effects WHERE id=$1", effect).Scan(&state, &request))
	if state != "running" || request != `{"historicalIntent": "retain exactly"}` {
		t.Fatal("migration rewrote historical plugin evidence")
	}
	must(t, pool.QueryRow("SELECT state='queued' AND step='plugin' AND controller_epoch IS NULL AND retry_at IS NULL AND error_code IS NULL AND NOT request ? 'plugins' FROM operations WHERE id=$1", operation).Scan(&valid))
	if !valid {
		t.Fatal("old in-flight operation was not released to freeze a Node execution input")
	}
	_, err = pool.Exec("INSERT INTO external_effects(id,operation_id,project_id,workspace_id,kind,state,reconciled_epoch) VALUES($1,$2,$3,$4,'plugin_delete','planned',1)", uuid.NewString(), operation, project, workspace)
	if err == nil {
		t.Fatal("forward upgrade allowed a new retired plugin effect")
	}
}
