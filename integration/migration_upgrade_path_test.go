package integration

// Migration reconciliation guards preserve the immutable upstream 0001-0016 sequence.
// Compatible tenants upgrade to 0017 without losing project bindings or runtime history;
// fresh databases must reach the same mandatory project-space association.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/wanglongan587/cloud/internal/core"
)

// applyMigrationsUpTo applies and records the named migrations exactly as the runner would
// (executing each file, recording filename + SHA256 checksum), stopping before the new additions.
// This mirrors how an upstream deployment reached 0007.
func applyMigrationsUpTo(t *testing.T, pool *sql.DB, versions []string) {
	t.Helper()
	_, e := pool.Exec("CREATE TABLE IF NOT EXISTS schema_migrations(version text PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())")
	must(t, e)
	for _, version := range versions {
		b, e := os.ReadFile(filepath.Join("..", "internal", "core", "migrations", version))
		must(t, e)
		_, e = pool.Exec(string(b))
		must(t, e)
		sum := sha256.Sum256(b)
		_, e = pool.Exec("INSERT INTO schema_migrations(version,checksum) VALUES($1,$2)", version, hex.EncodeToString(sum[:]))
		must(t, e)
	}
}

func newStoreOnSchema(t *testing.T, pool *sql.DB) *core.Store {
	t.Helper()
	db, e := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	must(t, e)
	store, e := core.NewStore(db)
	must(t, e)
	return store
}

func tableExists(t *testing.T, pool *sql.DB, name string) bool {
	t.Helper()
	var n int
	must(t, pool.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=$1`, name).Scan(&n))
	return n == 1
}

func columnNullable(t *testing.T, pool *sql.DB, table, column string) bool {
	t.Helper()
	var isNullable string
	must(t, pool.QueryRow(`SELECT is_nullable FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 AND column_name=$2`, table, column).Scan(&isNullable))
	return isNullable == "YES"
}

// TestMigrationUpstream0007UpgradePath is the most important upgrade path: a database that ran the
// real upstream 0001-0007 (including 0007's project->default-space binding and NOT NULL space_id)
// must upgrade cleanly without re-running upstream migrations or losing project bindings.
func TestMigrationUpstream0007UpgradePath(t *testing.T) {
	pool, _ := testSchema(t, "test_upg_")
	// The current 0001-0007 files are byte-identical to upstream/main (asserted separately by the
	// git-level reconciliation audit); applying them here reproduces an existing upstream deployment.
	applyMigrationsUpTo(t, pool, []string{
		"0001_core.sql", "0002_aggregate_guards.sql", "0003_resource_versions.sql",
		"0004_effect_intent_and_ticket_scope.sql", "0005_gateway_auth.sql",
		"0006_collab_spaces.sql", "0007_project_space_scope.sql",
	})

	// Simulate data 0007 left behind: a tenant with a default space and a project bound to it
	// (space_id NOT NULL at this point, exactly as upstream 0007 forces).
	ids := make([]string, 6)
	for i := range ids {
		ids[i] = uuid.NewString()
	}
	seed := []struct {
		query string
		args  []any
	}{
		{"INSERT INTO users(id,display_name,status) VALUES($1,'Upgrade user','active')", []any{ids[0]}},
		{"INSERT INTO tenants(id,name,status) VALUES($1,'Upgrade tenant','active')", []any{ids[1]}},
		{"INSERT INTO tenant_memberships(tenant_id,user_id,role,status) VALUES($1,$2,'admin','active')", []any{ids[1], ids[0]}},
		{"INSERT INTO collab_workspaces(id,tenant_id,name,slug,description,created_by) VALUES($1,$2,'Upgrade tenant','default','',$3)", []any{ids[2], ids[1], ids[0]}},
		{"INSERT INTO projects(id,tenant_id,owner_user_id,space_id,name,repository_url,default_branch,lifecycle) VALUES($1,$2,$3,$4,'Bound project','https://example.invalid/upg.git','main','active')", []any{ids[3], ids[1], ids[0], ids[2]}},
		// A live project requires exactly one main workspace (deferred one_main trigger in 0001).
		{"INSERT INTO workspaces(id,tenant_id,owner_user_id,project_id,kind,desired_state,observed_state) VALUES($1,$2,$3,$4,'main','running','ready')", []any{ids[5], ids[1], ids[0], ids[3]}},
	}
	tx, e := pool.Begin()
	must(t, e)
	for _, s := range seed {
		_, e = tx.Exec(s.query, s.args...)
		must(t, e)
	}
	must(t, tx.Commit())

	// Now the current binary migrates the rest (0008-0017) on top of the upstream baseline.
	store := newStoreOnSchema(t, pool)
	must(t, store.Migrate(context.Background()))
	// Idempotent: a second Migrate and a strict CheckSchema must both pass with no unknown/missing
	// versions and no checksum mismatches.
	must(t, store.Migrate(context.Background()))
	must(t, store.CheckSchema(context.Background()))

	// 0007's binding is preserved: 0013 must NOT unbind existing projects.
	var bound sql.NullString
	must(t, pool.QueryRow(`SELECT space_id FROM projects WHERE id=$1`, ids[3]).Scan(&bound))
	if !bound.Valid || bound.String != ids[2] {
		t.Fatalf("upstream 0007 project binding lost: want %s got %v", ids[2], bound)
	}

	if columnNullable(t, pool, "projects", "space_id") {
		t.Fatal("projects.space_id must be mandatory after 0017")
	}

	// Issue capability schema (0008-0012) is present.
	for _, table := range []string{"issues", "issue_comments", "issue_runs", "issue_activities", "issue_context_refs", "issue_interactions"} {
		if !tableExists(t, pool, table) {
			t.Fatalf("missing issue migration table %s", table)
		}
	}
}

// TestMigrationFullSequenceFreshDB verifies a clean database from empty through the complete
// sequence: Migrate PASS, CheckSchema PASS, and the final schema matches the application contract
// (mandatory projects.space_id, composite space FK, project_space_list index, Issue schema intact).
func TestMigrationFullSequenceFreshDB(t *testing.T) {
	pool, _ := testSchema(t, "test_fresh_")
	store := newStoreOnSchema(t, pool)
	must(t, store.Migrate(context.Background()))
	must(t, store.Migrate(context.Background()))
	must(t, store.CheckSchema(context.Background()))

	if columnNullable(t, pool, "projects", "space_id") {
		t.Fatal("projects.space_id must be mandatory after full sequence")
	}
	var idxCount int
	must(t, pool.QueryRow(`SELECT count(*) FROM pg_indexes WHERE schemaname=current_schema() AND indexname='project_space_list'`).Scan(&idxCount))
	if idxCount != 1 {
		t.Fatalf("project_space_list index missing (want 1, got %d)", idxCount)
	}
	// Composite FK (space_id, tenant_id) -> collab_workspaces must be intact.
	var fkCount int
	must(t, pool.QueryRow(`SELECT count(*) FROM information_schema.referential_constraints rc JOIN information_schema.key_column_usage kcu
		ON rc.constraint_name=kcu.constraint_name AND rc.constraint_schema=kcu.constraint_schema
		WHERE rc.unique_constraint_schema=current_schema() AND kcu.table_schema=current_schema()
		AND kcu.table_name='projects' AND kcu.column_name='space_id' AND rc.delete_rule='NO ACTION'`).Scan(&fkCount))
	if fkCount < 1 {
		t.Fatalf("projects.space_id FK missing (want >=1, got %d)", fkCount)
	}

	for _, table := range []string{
		"issues", "issue_statuses", "issue_comments", "labels", "issue_labels", "issue_subscribers",
		"issue_views", "issue_runs", "issue_activities", "issue_context_refs", "issue_interactions",
		"collab_workspaces", "tenant_invitations", "tenant_join_links", "tenant_join_requests",
	} {
		if !tableExists(t, pool, table) {
			t.Fatalf("missing table %s", table)
		}
	}

	// Sanity: a project can be created in the tenant's sole space. All rows go in one tx so the
	// deferred triggers (tenant_admin, one_main) fire once, at commit.
	fuid, ftid, fpid := uuid.NewString(), uuid.NewString(), uuid.NewString()
	tx, e := pool.Begin()
	must(t, e)
	_, e = tx.Exec(`INSERT INTO users(id,display_name,status) VALUES($1,'Fresh user','active')`, fuid)
	must(t, e)
	_, e = tx.Exec(`INSERT INTO tenants(id,name,status) VALUES($1,'Fresh tenant','active')`, ftid)
	must(t, e)
	_, e = tx.Exec(`INSERT INTO tenant_memberships(tenant_id,user_id,role,status) VALUES($1,$2,'admin','active')`, ftid, fuid)
	must(t, e)
	fspace := uuid.NewString()
	_, e = tx.Exec(`INSERT INTO collab_workspaces(id,tenant_id,name,slug,description,created_by) VALUES($1,$2,'Fresh tenant','fresh-tenant','',$3)`, fspace, ftid, fuid)
	must(t, e)
	_, e = tx.Exec(`INSERT INTO projects(id,tenant_id,owner_user_id,space_id,name,repository_url,default_branch,lifecycle)
		VALUES($1,$2,$3,$4,'Fresh','https://example.invalid/fresh.git','main','active')`, fpid, ftid, fuid, fspace)
	must(t, e)
	// A live project requires exactly one main workspace (deferred one_main trigger in 0001).
	_, e = tx.Exec(`INSERT INTO workspaces(id,tenant_id,owner_user_id,project_id,kind,desired_state,observed_state,requested_ref)
		VALUES($1,$2,$3,$4,'main','running','ready','main')`, uuid.NewString(), ftid, fuid, fpid)
	must(t, e)
	must(t, tx.Commit())
}

// Migration 0016 retires the Project storage and worktree steps without touching their history:
// in-flight operations still in storage or worktree fail with lifecycle_flow_retired, a project
// deletion waiting on storage deletion returns to cleanup, planned effects keep their records, and
// every Workspace gets the ref it will clone. Running Migrate again changes nothing.
//
// Evidence for specs test-cases/cloud/operation/workspace-runtime-lifecycle.md
// #retired-storage-and-worktree-steps-leave-history-intact.
func TestMigration0016RetiresStorageAndWorktreeStepsKeepingHistory(t *testing.T) {
	pool, _ := testSchema(t, "test_m16_")
	entries, e := os.ReadDir(filepath.Join("..", "internal", "core", "migrations"))
	must(t, e)
	var previous []string
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".sql" && entry.Name() < "0016" {
			previous = append(previous, entry.Name())
		}
	}
	applyMigrationsUpTo(t, pool, previous)

	user, tenant := uuid.NewString(), uuid.NewString()
	type legacy struct{ project, workspace, operation, effect, kind, step, state, effectKind, effectState string }
	cases := map[string]*legacy{
		"storage":        {kind: "create_project", step: "storage", state: "queued"},
		"worktree":       {kind: "create_project", step: "worktree", state: "running", effectKind: "worktree_ensure", effectState: "planned"},
		"storage_delete": {kind: "delete_project", step: "storage_delete", state: "retry_wait", effectKind: "worktree_delete", effectState: "succeeded"},
		"done":           {kind: "create_project", step: "done", state: "succeeded", effectKind: "storage_ensure", effectState: "succeeded"},
	}
	tx, e := pool.Begin()
	must(t, e)
	exec := func(q string, args ...any) {
		t.Helper()
		_, err := tx.Exec(q, args...)
		must(t, err)
	}
	exec("INSERT INTO users(id,display_name,status) VALUES($1,'Legacy user','active')", user)
	exec("INSERT INTO tenants(id,name,status) VALUES($1,'Legacy tenant','active')", tenant)
	exec("INSERT INTO tenant_memberships(tenant_id,user_id,role,status) VALUES($1,$2,'admin','active')", tenant, user)
	space := uuid.NewString()
	exec("INSERT INTO collab_workspaces(id,tenant_id,name,slug,created_by) VALUES($1,$2,'Legacy tenant','legacy-runtime',$3)", space, tenant, user)
	for _, c := range cases {
		c.project, c.workspace, c.operation, c.effect = uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
		exec("INSERT INTO projects(id,tenant_id,owner_user_id,space_id,name,repository_url,default_branch,lifecycle) VALUES($1,$2,$3,$4,'Legacy','https://example.invalid/legacy.git','trunk','active')", c.project, tenant, user, space)
		exec("INSERT INTO project_storage(project_id,substrate_storage_id,observed_state) VALUES($1,$2,'ready')", c.project, "storage-"+c.project)
		exec("INSERT INTO workspaces(id,tenant_id,owner_user_id,project_id,kind,desired_state,observed_state) VALUES($1,$2,$3,$4,'main','running','provisioning')", c.workspace, tenant, user, c.project)
		exec("INSERT INTO workspace_worktrees(workspace_id,relative_path,branch_name,requested_ref,provisioning_state) VALUES($1,$2,$3,'release','pending')", c.workspace, "workspaces/"+c.workspace+"/checkout", "ora/"+c.workspace)
		exec("INSERT INTO operations(id,tenant_id,actor_user_id,project_id,workspace_id,kind,state,step,request,idempotency_key,request_hash,controller_epoch) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'{}','k','h',1)", c.operation, tenant, user, c.project, c.workspace, c.kind, c.state, c.step)
		if c.effectKind != "" {
			exec("INSERT INTO external_effects(id,operation_id,project_id,workspace_id,kind,state,request,reconciled_epoch) VALUES($1,$2,$3,$4,$5,$6,'{\"kind\":\"legacy\"}',1)", c.effect, c.operation, c.project, c.workspace, c.effectKind, c.effectState)
		}
	}
	// A Workspace without a worktree row takes its Project's default branch.
	bare := uuid.NewString()
	exec("INSERT INTO workspaces(id,tenant_id,owner_user_id,project_id,kind,desired_state,observed_state) VALUES($1,$2,$3,$4,'isolated','stopped','stopped')", bare, tenant, user, cases["done"].project)
	exec("INSERT INTO tasks(id,workspace_id,title) VALUES($1,$2,'Bare')", uuid.NewString(), bare)
	must(t, tx.Commit())
	snapshot := func(q string) string {
		t.Helper()
		var out string
		must(t, pool.QueryRow(q).Scan(&out))
		return out
	}
	storageBefore := snapshot("SELECT string_agg(row_to_json(s)::text,'|' ORDER BY project_id) FROM project_storage s")
	worktreesBefore := snapshot("SELECT string_agg(row_to_json(w)::text,'|' ORDER BY workspace_id) FROM workspace_worktrees w")
	effectsBefore := snapshot("SELECT string_agg(row_to_json(e)::text,'|' ORDER BY id) FROM external_effects e")

	store := newStoreOnSchema(t, pool)
	must(t, store.Migrate(context.Background()))
	must(t, store.Migrate(context.Background()))
	must(t, store.CheckSchema(context.Background()))

	if snapshot("SELECT string_agg(row_to_json(s)::text,'|' ORDER BY project_id) FROM project_storage s") != storageBefore ||
		snapshot("SELECT string_agg(row_to_json(w)::text,'|' ORDER BY workspace_id) FROM workspace_worktrees w") != worktreesBefore ||
		snapshot("SELECT string_agg(row_to_json(e)::text,'|' ORDER BY id) FROM external_effects e") != effectsBefore {
		t.Fatal("migration rewrote retired storage, worktree or effect history")
	}
	want := map[string]struct{ state, step, errorCode string }{
		"storage":        {"failed", "storage", "lifecycle_flow_retired"},
		"worktree":       {"failed", "worktree", "lifecycle_flow_retired"},
		"storage_delete": {"retry_wait", "cleanup", ""},
		"done":           {"succeeded", "done", ""},
	}
	for name, c := range cases {
		var state, step string
		var code sql.NullString
		must(t, pool.QueryRow("SELECT state,step,error_code FROM operations WHERE id=$1", c.operation).Scan(&state, &step, &code))
		if w := want[name]; state != w.state || step != w.step || code.String != w.errorCode {
			t.Errorf("%s operation became %s/%s/%s, want %v", name, state, step, code.String, w)
		}
		var ref string
		var base sql.NullString
		must(t, pool.QueryRow("SELECT requested_ref,base_commit_id FROM workspaces WHERE id=$1", c.workspace).Scan(&ref, &base))
		if ref != "release" || base.Valid {
			t.Errorf("%s workspace got ref %q base %v, want the worktree's ref and no baseline", name, ref, base)
		}
	}
	if snapshot("SELECT requested_ref FROM workspaces WHERE id='"+bare+"'") != "trunk" {
		t.Error("a Workspace without a worktree must take its Project's default branch")
	}
}

// TestMigration0019ExecutionWorkAppliesFreshAndUpgrades (§30): the 0019 execution_work migration
// applies cleanly on a database already at 0018 (upgrade path), is idempotent (re-running Migrate
// changes nothing and passes CheckSchema), and leaves the table, partial-unique once-guard and
// pickup index in place. The authoritative constraint behavior itself is covered by the white-box
// core TestAgentRunExecutionWorkUniqueGuard; here we verify the schema lands as declared on a real
// schema, both fresh and after the previous migrations.
//
// Evidence for the G-008 execution-identity persistence obligation: the migration is what installs the
// DB-enforced exactly-once identity (execution_work_unregistered_once).
func TestMigration0019ExecutionWorkAppliesFreshAndUpgrades(t *testing.T) {
	// Upgrade path: start from all migrations through 0018, then let store.Migrate run 0019.
	pool, _ := testSchema(t, "test_ew19_")
	entries, e := os.ReadDir(filepath.Join("..", "internal", "core", "migrations"))
	must(t, e)
	var previous []string
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".sql" && entry.Name() < "0019_execution_work.sql" {
			previous = append(previous, entry.Name())
		}
	}
	applyMigrationsUpTo(t, pool, previous)
	store := newStoreOnSchema(t, pool)
	must(t, store.Migrate(context.Background()))
	// Idempotent: a second Migrate + CheckSchema must be clean.
	must(t, store.Migrate(context.Background()))
	must(t, store.CheckSchema(context.Background()))

	if !tableExists(t, pool, "execution_work") {
		t.Fatal("execution_work table must exist after 0019")
	}
	// The partial unique index is the authoritative at-most-one-unregistered-work guard.
	var onceIdx int
	must(t, pool.QueryRow(`SELECT count(*) FROM pg_indexes WHERE schemaname=current_schema() AND tablename='execution_work' AND indexname='execution_work_unregistered_once'`).Scan(&onceIdx))
	if onceIdx != 1 {
		t.Fatal("execution_work_unregistered_once partial unique index must exist")
	}
	// The pickup ordering index must exist.
	var pickupIdx int
	must(t, pool.QueryRow(`SELECT count(*) FROM pg_indexes WHERE schemaname=current_schema() AND tablename='execution_work' AND indexname='execution_work_pickup'`).Scan(&pickupIdx))
	if pickupIdx != 1 {
		t.Fatal("execution_work_pickup index must exist")
	}
	// The unique index is partial over the un-registered predicate (execution_id IS NULL).
	var pred sql.NullString
	must(t, pool.QueryRow(`SELECT indexdef FROM pg_indexes WHERE schemaname=current_schema() AND tablename='execution_work' AND indexname='execution_work_unregistered_once'`).Scan(&pred))
	if pred.String == "" || !strings.Contains(pred.String, "execution_id IS NULL") {
		t.Fatalf("once-guard must be partial over execution_id IS NULL, got %q", pred.String)
	}
	// run_id and workspace_id are enforced references (preserve tenant/owner scope and run identity).
	for _, col := range []string{"run_id", "workspace_id", "tenant_id"} {
		var has bool
		must(t, pool.QueryRow(`SELECT EXISTS (
			SELECT 1 FROM information_schema.table_constraints tc
			JOIN information_schema.key_column_usage kcu
			  ON kcu.constraint_name = tc.constraint_name
			  AND kcu.table_schema = tc.table_schema
			  AND kcu.table_name = tc.table_name
			WHERE tc.table_schema=current_schema() AND tc.table_name='execution_work'
			  AND tc.constraint_type='FOREIGN KEY' AND kcu.column_name=$1
		)`, col).Scan(&has))
		if !has {
			t.Fatalf("execution_work.%s must be a foreign key reference", col)
		}
	}
}

// TestMigration0020NodeExecutionsAppliesFreshAndUpgrades (T4A, phase 4 design §4.13): the 0020
// node_executions migration applies cleanly on a database already at 0019 (upgrade path), is
// idempotent (re-running Migrate changes nothing and passes CheckSchema), and leaves the table with
// its authoritative constraints: PRIMARY KEY on execution_id (the Controller-generated global
// identity), UNIQUE work_id (at most one execution per work, D-021), work_id FK to execution_work,
// and the pending partial index (result IS NULL) by node for C2 crash recovery. Fresh-DB coverage is
// the full-sequence test, which migrates from scratch; this test pins the upgrade path.
//
// Evidence for the D-020/D-021 node_executions registration obligation: the migration is what
// installs the DB-enforced one-work→one-execution and the recovery lookup.
func TestMigration0020NodeExecutionsAppliesFreshAndUpgrades(t *testing.T) {
	// Upgrade path: start from all migrations through 0019, then let store.Migrate run 0020.
	pool, _ := testSchema(t, "test_nd20_")
	entries, e := os.ReadDir(filepath.Join("..", "internal", "core", "migrations"))
	must(t, e)
	var previous []string
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".sql" && entry.Name() < "0020_node_executions.sql" {
			previous = append(previous, entry.Name())
		}
	}
	applyMigrationsUpTo(t, pool, previous)
	store := newStoreOnSchema(t, pool)
	must(t, store.Migrate(context.Background()))
	// Idempotent: a second Migrate + CheckSchema must be clean.
	must(t, store.Migrate(context.Background()))
	must(t, store.CheckSchema(context.Background()))

	if !tableExists(t, pool, "node_executions") {
		t.Fatal("node_executions table must exist after 0020")
	}
	// execution_id is PRIMARY KEY: the Controller-generated identity is globally unique.
	var pkCount int
	must(t, pool.QueryRow(`SELECT count(*) FROM information_schema.table_constraints
		WHERE table_schema=current_schema() AND table_name='node_executions' AND constraint_type='PRIMARY KEY'`).Scan(&pkCount))
	if pkCount != 1 {
		t.Fatalf("node_executions must have a PRIMARY KEY, got %d", pkCount)
	}
	// work_id is UNIQUE: one work can never host two executions (D-021, one-work→one-execution).
	var workUnique int
	must(t, pool.QueryRow(`SELECT count(*) FROM information_schema.table_constraints
		WHERE table_schema=current_schema() AND table_name='node_executions' AND constraint_type='UNIQUE'`).Scan(&workUnique))
	if workUnique != 1 {
		t.Fatalf("node_executions.work_id must be UNIQUE, got %d", workUnique)
	}
	// work_id is an enforced reference to execution_work (ownership path node_executions → execution_work).
	var hasFK bool
	must(t, pool.QueryRow(`SELECT EXISTS (
		SELECT 1 FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON kcu.constraint_name = tc.constraint_name AND kcu.table_schema = tc.table_schema AND kcu.table_name = tc.table_name
		WHERE tc.table_schema=current_schema() AND tc.table_name='node_executions'
		  AND tc.constraint_type='FOREIGN KEY' AND kcu.column_name='work_id'
	)`).Scan(&hasFK))
	if !hasFK {
		t.Fatal("node_executions.work_id must be a foreign key reference to execution_work")
	}
	// The pending partial index (result IS NULL, by node) backs C2 crash recovery.
	var pendingIdx int
	must(t, pool.QueryRow(`SELECT count(*) FROM pg_indexes WHERE schemaname=current_schema() AND tablename='node_executions' AND indexname='node_executions_pending_node'`).Scan(&pendingIdx))
	if pendingIdx != 1 {
		t.Fatalf("node_executions_pending_node index must exist, got %d", pendingIdx)
	}
	var pred sql.NullString
	must(t, pool.QueryRow(`SELECT indexdef FROM pg_indexes WHERE schemaname=current_schema() AND tablename='node_executions' AND indexname='node_executions_pending_node'`).Scan(&pred))
	if pred.String == "" || !strings.Contains(pred.String, "result IS NULL") {
		t.Fatalf("pending index must be partial over result IS NULL, got %q", pred.String)
	}
}

// TestMigration0021NodeEventReceiptsAppliesFreshAndUpgrades (T4B, mandate §40): the 0021
// node_event_receipts migration applies cleanly on a database already at 0020 (upgrade path), is
// idempotent (re-running Migrate changes nothing and passes CheckSchema), and leaves the table with
// exactly the shape receipt identity needs: PRIMARY KEY (execution_id, sequence) — one receipt per
// (execution, node sequence), which is the identity a replayed batch is compared against (D-022) —
// a FOREIGN KEY to node_executions so a receipt can never exist for an execution Cloud never
// registered, an `event` jsonb column constrained to an object (the canonical ThreadEvent Cloud
// stores verbatim), and the created_at index reserved for the still-deferred retention policy
// (G-015). Fresh-DB coverage is the full-sequence test, which migrates from scratch; this test pins
// the upgrade path.
//
// Evidence for the plan §4B.6 receipt obligation: the table and its constraints are the durable
// basis for EventAck (protocol root D4) and for receipt-level replay idempotency.
func TestMigration0021NodeEventReceiptsAppliesFreshAndUpgrades(t *testing.T) {
	// Upgrade path: start from all migrations through 0020, then let store.Migrate run 0021.
	pool, _ := testSchema(t, "test_ner21_")
	entries, e := os.ReadDir(filepath.Join("..", "internal", "core", "migrations"))
	must(t, e)
	var previous []string
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".sql" && entry.Name() < "0021_node_event_receipts.sql" {
			previous = append(previous, entry.Name())
		}
	}
	applyMigrationsUpTo(t, pool, previous)
	store := newStoreOnSchema(t, pool)
	must(t, store.Migrate(context.Background()))
	// Idempotent: a second Migrate + CheckSchema must be clean.
	must(t, store.Migrate(context.Background()))
	must(t, store.CheckSchema(context.Background()))

	if !tableExists(t, pool, "node_event_receipts") {
		t.Fatal("node_event_receipts table must exist after 0021")
	}
	// PRIMARY KEY (execution_id, sequence): the receipt identity D-022 is defined on.
	var pkColumns []string
	rows, e := pool.Query(`SELECT kcu.column_name FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON kcu.constraint_name = tc.constraint_name AND kcu.table_schema = tc.table_schema AND kcu.table_name = tc.table_name
		WHERE tc.table_schema=current_schema() AND tc.table_name='node_event_receipts' AND tc.constraint_type='PRIMARY KEY'
		ORDER BY kcu.ordinal_position`)
	must(t, e)
	defer rows.Close()
	for rows.Next() {
		var col string
		must(t, rows.Scan(&col))
		pkColumns = append(pkColumns, col)
	}
	must(t, rows.Err())
	if len(pkColumns) != 2 || pkColumns[0] != "execution_id" || pkColumns[1] != "sequence" {
		t.Fatalf("node_event_receipts PRIMARY KEY must be (execution_id, sequence), got %v", pkColumns)
	}
	// execution_id is an enforced reference to node_executions: no receipt without a registration.
	var hasFK bool
	must(t, pool.QueryRow(`SELECT EXISTS (
		SELECT 1 FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON kcu.constraint_name = tc.constraint_name AND kcu.table_schema = tc.table_schema AND kcu.table_name = tc.table_name
		WHERE tc.table_schema=current_schema() AND tc.table_name='node_event_receipts'
		  AND tc.constraint_type='FOREIGN KEY' AND kcu.column_name='execution_id'
	)`).Scan(&hasFK))
	if !hasFK {
		t.Fatal("node_event_receipts.execution_id must be a foreign key reference to node_executions")
	}
	// `event` is jsonb: the canonical ThreadEvent is stored as JSON, compared structurally.
	var eventType string
	must(t, pool.QueryRow(`SELECT data_type FROM information_schema.columns
		WHERE table_schema=current_schema() AND table_name='node_event_receipts' AND column_name='event'`).Scan(&eventType))
	if eventType != "jsonb" {
		t.Fatalf("node_event_receipts.event must be jsonb, got %q", eventType)
	}
	// A non-object event is refused by the database, not only by the caller.
	_, e = pool.Exec(`INSERT INTO node_event_receipts(execution_id,sequence,event) VALUES('x',1,'[]')`)
	if e == nil {
		t.Fatal("node_event_receipts.event must reject a non-object JSON value")
	}
	// The created_at index backs the deferred retention sweep (G-015), which Phase 4B does not run.
	var retentionIdx int
	must(t, pool.QueryRow(`SELECT count(*) FROM pg_indexes WHERE schemaname=current_schema() AND tablename='node_event_receipts' AND indexname='node_event_receipts_retention'`).Scan(&retentionIdx))
	if retentionIdx != 1 {
		t.Fatalf("node_event_receipts_retention index must exist, got %d", retentionIdx)
	}
}

// seedDeclaredAgentRun inserts one more agent run on the seeded issue with its own live isolated run
// Workspace, optionally cancelled and optionally carrying the seq=1 first prompt. It reproduces the
// pre-0022 worlds the pending backfill has to classify: a declared-but-unactivated session, a
// cancelled one, a terminal one, and a run that merely reached a stage without a declaration.
func seedDeclaredAgentRun(t *testing.T, pool *sql.DB, s skeletonSeed, phase, status string, cancelled, withFirstPrompt bool) string {
	t.Helper()
	runID, wsID := uuid.NewString(), uuid.NewString()
	tx, e := pool.Begin()
	must(t, e)
	defer func() { _ = tx.Rollback() }()
	exec := func(q string, args ...any) {
		_, e := tx.Exec(q, args...)
		must(t, e)
	}
	exec(`INSERT INTO issue_runs(id, tenant_id, issue_id, executor_type, executor_id, phase, status)
		VALUES($1,$2,$3,'agent',$4,$5,$6)`, runID, s.tenantID, s.issueID, uuid.NewString(), phase, status)
	exec(`INSERT INTO workspaces(id, tenant_id, owner_user_id, project_id, kind, desired_state, observed_state, requested_ref)
		VALUES($1,$2,$3,$4,'isolated','running','ready','main')`, wsID, s.tenantID, s.userID, s.projectID)
	// 0003: a live isolated workspace owns exactly one task identity (deferred task_identity trigger).
	exec(`INSERT INTO tasks(id, workspace_id, title) VALUES($1,$2,'0022 run workspace task')`, uuid.NewString(), wsID)
	exec(`UPDATE issue_runs SET workspace_id=$1 WHERE id=$2`, wsID, runID)
	exec(`UPDATE workspaces SET issue_run_id=$1 WHERE id=$2`, runID, wsID)
	if cancelled {
		exec(`UPDATE issue_runs SET cancel_requested_at=now() WHERE id=$1`, runID)
	}
	if withFirstPrompt {
		exec(`INSERT INTO thread_entries(run_id, seq, source, kind, record, turn_id)
			VALUES($1, 1, 'system', 'user_turn', '{"content":"Begin this task."}', $2)`, runID, uuid.NewString())
	}
	must(t, tx.Commit())
	return runID
}

// TestMigration0022ThreadCommandsAndPendingAppliesFreshAndUpgrades (T4C-5, D-4C-12, G-016/G-022):
// the 0022 migration applies cleanly on a database already at 0021 (upgrade path), is idempotent
// (re-running Migrate changes nothing and passes CheckSchema), and lands the Thread API's persistence
// with the shape the design requires.
//
// The upgrade path is what pins the two data decisions, because both are only observable when rows
// exist before the migration runs:
//
//   - the `pending` backfill keys on the *business fact* — a real seq=1 session declaration — not on
//     a stage proxy, and excludes cancelled and terminal runs;
//   - the `thread_entries.status` CHECK is safe because a pre-existing source='user' row is
//     backfilled to 'queued' first, so the constraint can never fail an upgrade.
//
// Evidence for the plan §4R.5 S1 obligation: migration 0022 fresh + upgrade, idempotent, and the
// backfill scoped to declared, non-cancelled, non-terminal agent runs.
func TestMigration0022ThreadCommandsAndPendingAppliesFreshAndUpgrades(t *testing.T) {
	// Upgrade path: start from all migrations through 0021, then let store.Migrate run 0022.
	pool, _ := testSchema(t, "test_tc22_")
	entries, e := os.ReadDir(filepath.Join("..", "internal", "core", "migrations"))
	must(t, e)
	var previous []string
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".sql" && entry.Name() < "0022_thread_api_and_commands.sql" {
			previous = append(previous, entry.Name())
		}
	}
	applyMigrationsUpTo(t, pool, previous)

	// The ownership chain (0018 columns included, since 0018 is already applied at this point).
	seed := seedAgentIssueRunSkeleton(t, pool)
	// Four runs differing only in the facts the backfill must read.
	declared := seedDeclaredAgentRun(t, pool, seed, "starting", "dispatched", false, true)
	cancelled := seedDeclaredAgentRun(t, pool, seed, "starting", "dispatched", true, true)
	terminal := seedDeclaredAgentRun(t, pool, seed, "releasing", "cancelled", false, true)
	undeclared := seedDeclaredAgentRun(t, pool, seed, "starting", "dispatched", false, false)
	// A user turn that predates the column: the CHECK below is only safe if 0022 backfills it first.
	_, e = pool.Exec(`INSERT INTO thread_entries(run_id, seq, source, kind, record, turn_id)
		VALUES($1, 2, 'user', 'user_turn', '{"content":[{"type":"text","text":"hello"}]}', $2)`, declared, uuid.NewString())
	must(t, e)

	store := newStoreOnSchema(t, pool)
	must(t, store.Migrate(context.Background()))
	// Idempotent: a second Migrate + CheckSchema must be clean (the backfills are no-ops).
	must(t, store.Migrate(context.Background()))
	must(t, store.CheckSchema(context.Background()))

	// The backfill materializes exactly the declared, live, non-cancelled, non-terminal run.
	for _, tc := range []struct {
		name string
		run  string
		want string
	}{
		{"declared session becomes pending", declared, "pending"},
		{"cancelled run stays NULL", cancelled, ""},
		{"terminal run stays NULL", terminal, ""},
		{"run without a declaration stays NULL", undeclared, ""},
		{"run with no session at all stays NULL", seed.runID, ""},
	} {
		var state sql.NullString
		must(t, pool.QueryRow(`SELECT thread_state FROM issue_runs WHERE id=$1`, tc.run).Scan(&state))
		if state.String != tc.want {
			t.Fatalf("%s: thread_state = %q, want %q", tc.name, state.String, tc.want)
		}
	}

	// The pre-existing user turn was backfilled before the CHECK was added, so the upgrade succeeded.
	var status sql.NullString
	must(t, pool.QueryRow(`SELECT status FROM thread_entries WHERE run_id=$1 AND seq=2`, declared).Scan(&status))
	if !status.Valid || status.String != "queued" {
		t.Fatalf("a pre-0022 user turn must be backfilled to 'queued', got %v", status)
	}

	// thread_entries.status: the turn lifecycle exists exactly for user turns.
	_, e = pool.Exec(`UPDATE thread_entries SET status='bogus' WHERE run_id=$1 AND seq=2`, declared)
	wantPGError(t, e, "23514")
	_, e = pool.Exec(`UPDATE thread_entries SET status='queued' WHERE run_id=$1 AND seq=1`, declared)
	wantPGError(t, e, "23514") // a system entry can never carry a turn lifecycle
	_, e = pool.Exec(`INSERT INTO thread_entries(run_id, seq, source, kind, record) VALUES($1, 3, 'user', 'user_turn', '{}')`, declared)
	wantPGError(t, e, "23514") // a user turn can never be missing one

	// thread_commands: controller-integration D6's control-plane table, with the identity D3 needs.
	if !tableExists(t, pool, "thread_commands") {
		t.Fatal("thread_commands table must exist after 0022")
	}
	var pkColumns []string
	rows, e := pool.Query(`SELECT kcu.column_name FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON kcu.constraint_name = tc.constraint_name AND kcu.table_schema = tc.table_schema AND kcu.table_name = tc.table_name
		WHERE tc.table_schema=current_schema() AND tc.table_name='thread_commands' AND tc.constraint_type='PRIMARY KEY'
		ORDER BY kcu.ordinal_position`)
	must(t, e)
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var col string
		must(t, rows.Scan(&col))
		pkColumns = append(pkColumns, col)
	}
	must(t, rows.Err())
	if len(pkColumns) != 1 || pkColumns[0] != "id" {
		t.Fatalf("thread_commands PRIMARY KEY must be (id) — the command_id the proto carries, got %v", pkColumns)
	}
	// A delivery is registered against an execution Cloud already registered (mirrors node_event_receipts).
	var hasFK bool
	must(t, pool.QueryRow(`SELECT EXISTS (
		SELECT 1 FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON kcu.constraint_name = tc.constraint_name AND kcu.table_schema = tc.table_schema AND kcu.table_name = tc.table_name
		WHERE tc.table_schema=current_schema() AND tc.table_name='thread_commands'
		  AND tc.constraint_type='FOREIGN KEY' AND kcu.column_name='delivered_execution_id'
	)`).Scan(&hasFK))
	if !hasFK {
		t.Fatal("thread_commands.delivered_execution_id must be a foreign key reference to node_executions")
	}
	_, e = pool.Exec(`INSERT INTO thread_commands(id, run_id, kind, body) VALUES($1,$2,'bogus','{}')`, uuid.NewString(), declared)
	wantPGError(t, e, "23514") // kind mirrors the proto oneof
	_, e = pool.Exec(`INSERT INTO thread_commands(id, run_id, kind, body) VALUES($1,$2,'submit_user_turn','[]')`, uuid.NewString(), declared)
	wantPGError(t, e, "23514") // body is a command payload object
	_, e = pool.Exec(`INSERT INTO thread_commands(id, run_id, kind, body, delivered_at) VALUES($1,$2,'end_session','{}',now())`, uuid.NewString(), declared)
	wantPGError(t, e, "23514") // a half-registered delivery is refused
	_, e = pool.Exec(`INSERT INTO thread_commands(id, run_id, kind, body, delivered_at, delivered_execution_id) VALUES($1,$2,'end_session','{}',now(),'no-such-execution')`, uuid.NewString(), declared)
	wantPGError(t, e, "23503")
	// An undelivered command is a legal row: commands wait in Cloud until the session execution is
	// registered (D3).
	_, e = pool.Exec(`INSERT INTO thread_commands(id, run_id, kind, body) VALUES($1,$2,'submit_user_turn','{"turnId":"t"}')`, uuid.NewString(), declared)
	must(t, e)

	// The two partial indexes 4C's readers depend on: the undelivered backlog and the idle window.
	for _, idx := range []struct{ table, name, predicate string }{
		{"thread_entries", "thread_entries_queued", "status = 'queued'"},
		{"thread_commands", "thread_commands_undelivered", "delivered_at IS NULL"},
		{"issue_runs", "issue_runs_idle_threads", "thread_state = 'idle'"},
	} {
		var def string
		must(t, pool.QueryRow(`SELECT indexdef FROM pg_indexes WHERE schemaname=current_schema() AND tablename=$1 AND indexname=$2`, idx.table, idx.name).Scan(&def))
		if !strings.Contains(def, idx.predicate) {
			t.Fatalf("%s must be partial over %q, got %q", idx.name, idx.predicate, def)
		}
	}
}
