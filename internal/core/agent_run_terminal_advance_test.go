package core

// Phase 2B A-side harness: seeds a real-Agent run Workspace + its create_workspace operation at a
// given step (with the node/sandbox a Controller would have stood up) and drives the control core
// (planEffect / effectResult / advance) directly inside caller-owned transactions — the same code
// the HTTP control plane runs. Evidence:

import (
	"context"
	"testing"
)

// runWsScene is the identity set of one seeded real-Agent run create_workspace scene.
type runWsScene struct {
	seed  dispSeed
	runws string
	op    string
}

// seedRunWorkspaceCreateScene inserts a schema-valid scene for one Agent run's create_workspace
// operation at the given step: the isolated run Workspace (issue_run_id bound), the run
// provisioning/dispatched with its pinned-plugin snapshot, the operation, and a live sandbox +
// connected, initialized Node so currentNode/openWorkspace succeed as the Controller would see it.
func seedRunWorkspaceCreateScene(t *testing.T, store *Store, seed dispSeed, pluginID, version, step string) runWsScene {
	t.Helper()
	var owner string
	if err := store.Pool.QueryRow(`SELECT owner_user_id FROM projects WHERE id=$1`, seed.project).Scan(&owner); err != nil {
		t.Fatalf("load project owner: %v", err)
	}
	runws, op := newID(), newID()
	tx, err := store.Pool.Begin()
	if err != nil {
		t.Fatalf("scene begin: %v", err)
	}
	exec := func(name, q string, args ...any) {
		if _, err := tx.Exec(q, args...); err != nil {
			t.Fatalf("scene %s: %v", name, err)
		}
	}
	exec("runws", `INSERT INTO workspaces(id,tenant_id,owner_user_id,project_id,kind,desired_state,observed_state,runtime_generation,requested_ref,issue_run_id)
		VALUES($1,$2,$3,$4,'isolated','running','starting',1,'HEAD',$5)`,
		runws, seed.tenant, owner, seed.project, seed.run)
	exec("run", `UPDATE issue_runs SET phase='provisioning',status='dispatched',workspace_id=$2,input=$3,version=version+1,updated_at=now() WHERE id=$1`,
		seed.run, runws, jsonText(Object{"agentPluginId": pluginID, "agentPluginVersion": version}))
	exec("op", `INSERT INTO operations(id,tenant_id,actor_user_id,project_id,workspace_id,kind,state,step,request,result,idempotency_key,request_hash,controller_epoch,version)
		VALUES($1,$2,$3,$4,$5,'create_workspace','running',$6,'{}','{}',$7,$8,1,1)`,
		op, seed.tenant, owner, seed.project, runws, step, "idem-"+runws, "hash-"+runws)
	exec("sandbox", `INSERT INTO sandbox_instances(id,workspace_id,generation,observed_state) VALUES($1,$2,1,'running')`, newID(), runws)
	// A live isolated workspace needs exactly one task identity (0003 deferred trigger).
	exec("task", `INSERT INTO tasks(id,workspace_id,title) VALUES($1,$2,$3)`, newID(), runws, "run-task")
	exec("node", `INSERT INTO node_instances(id,workspace_id,sandbox_instance_id,service_subject,connection_state,protocol_version,initialized)
		VALUES($1,$2,(SELECT id FROM sandbox_instances WHERE workspace_id=$2 AND terminated_at IS NULL LIMIT 1),$3,'connected',1,true)`,
		newID(), runws, "node-"+seed.tenant)
	if err := tx.Commit(); err != nil {
		t.Fatalf("scene commit: %v", err)
	}
	return runWsScene{seed: seed, runws: runws, op: op}
}

func loadOp(tx *transaction, id string) Object {
	return tx.one("SELECT * FROM operations WHERE id=$1", id)
}

// planRunPlugin plans the run Workspace's plugin_ensure effect inside a fresh transaction and
// returns the planned effect row; it mirrors the Controller's /effects call.
func planRunPlugin(t *testing.T, store *Store, scene runWsScene) Object {
	t.Helper()
	var out Object
	_, err := store.transact(context.Background(), func(tx *transaction) Object {
		o := loadOp(tx, scene.op)
		planned := planEffect(tx, &ControlRequest{Body: Object{"kind": "plugin_ensure", "workspaceId": scene.runws}}, o, new([]SpaceEvent))
		out = planned.O("effect")
		return nil
	})
	if err != nil {
		t.Fatalf("plan plugin_ensure: %v", err)
	}
	return out
}

// resultRunPluginSucceeded reports a plugin_ensure success at the pinned version (the evidence the
// Node returns) inside a fresh transaction.
func resultRunPluginSucceeded(t *testing.T, store *Store, scene runWsScene, effectID string) {
	t.Helper()
	_, err := store.transact(context.Background(), func(tx *transaction) Object {
		o := loadOp(tx, scene.op)
		e := tx.one("SELECT * FROM external_effects WHERE id=$1", effectID)
		version := e.O("request").S("version")
		effectResult(tx, &ControlRequest{EffectID: effectID, Body: Object{
			"state":      "succeeded",
			"externalId": "ext-" + effectID,
			"result":     Object{"installed": true, "version": version},
		}}, o, new([]SpaceEvent))
		return nil
	})
	if err != nil {
		t.Fatalf("effect_result(plugin_ensure succeeded): %v", err)
	}
}

// advanceRunWorkspaceCreate runs one advance transition on the create_workspace operation and
// returns the resulting operation Object plus any error (a hook error surfaces as a rollback error).
func advanceRunWorkspaceCreate(t *testing.T, store *Store, scene runWsScene) (Object, error) {
	t.Helper()
	var out Object
	_, err := store.transact(context.Background(), func(tx *transaction) Object {
		out = advance(tx, &ControlRequest{}, loadOp(tx, scene.op), new([]SpaceEvent))
		return nil
	})
	return out, err
}

// wsState returns the displayed run-Workspace state a test asserts on.
func wsState(t *testing.T, store *Store, wsID string) Object {
	t.Helper()
	return poolObject(t, store, `SELECT observed_state,admission_open FROM workspaces WHERE id=$1`, wsID)
}

// runInstanceRead returns the run Workspace's own plugin-instance evidence row.
func runInstanceRead(t *testing.T, store *Store, wsID, pluginID string) Object {
	t.Helper()
	ns, idf, _ := pluginIdentity(pluginID)
	return poolObject(t, store, `SELECT observed_state,observed_version,install_error FROM workspace_plugin_instances WHERE workspace_id=$1 AND source_namespace=$2 AND identifier=$3`, wsID, ns, idf)
}

// loadOpWith reads one operations row in its own transaction.
func loadOpWith(store *Store, opID string) Object {
	var out Object
	_, _ = store.transact(context.Background(), func(tx *transaction) Object {
		out = loadOp(tx, opID)
		return nil
	})
	return out
}

// poolObject reads a single query row into an Object keyed by camelCased column name, so tests can
// assert on space_plugins/workspace_plugin_instances/workspaces columns without a per-row struct.
func poolObject(t *testing.T, store *Store, q string, args ...any) Object {
	t.Helper()
	rows, err := store.Pool.Query(q, args...)
	if err != nil {
		t.Fatalf("pool query %s: %v", q, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("pool columns: %v", err)
	}
	if !rows.Next() {
		t.Fatalf("pool query returned no rows: %s", q)
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		t.Fatalf("pool scan: %v", err)
	}
	out := Object{}
	for i, c := range cols {
		switch v := vals[i].(type) {
		case string:
			out[camel(c)] = v
		case []byte:
			out[camel(c)] = string(v)
		case int64:
			out[camel(c)] = v
		case float64:
			out[camel(c)] = v
		case bool:
			out[camel(c)] = v
		case nil:
			// column is null; leave key unset
		}
	}
	return out
}

// TestPhase2BTerminalCreateSettlesRunInSameTx (G-003): driving the real A-side terminal — plan the
// run Workspace's pinned plugin_ensure, confirm evidence, advance — the operation reaches
// done/succeeded AND the run settles provisioning → starting in the same transaction, with the
// workspace open for admission and the plugin instance installed at the pinned version; no delete
// is declared for a ready run.
func TestPhase2BTerminalCreateSettlesRunInSameTx(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{accepted: true}
	store.AgentRunControlPlane = stub
	store.AgentRunHooks = businessAgentRunHooks{store: store}
	seed := seedDispatchScene(t, store.Pool, false)
	scene := seedRunWorkspaceCreateScene(t, store, seed, "official/hello-world", "1.0.0", "plugin")

	effect := planRunPlugin(t, store, scene)
	// Admission stays closed through the plugin step (G-002): the workspace is not ready yet.
	if s := wsState(t, store, scene.runws); s.S("observedState") != "starting" || s.B("admissionOpen") {
		t.Fatalf("plugin step must hold admission closed, got %v", s)
	}
	resultRunPluginSucceeded(t, store, scene, effect.S("id"))
	out, err := advanceRunWorkspaceCreate(t, store, scene)
	if err != nil {
		t.Fatalf("terminal advance: %v", err)
	}
	if out.S("step") != "done" || out.S("state") != "succeeded" {
		t.Fatalf("operation must reach done/succeeded, got step=%s state=%s", out.S("step"), out.S("state"))
	}
	phase, status, reason, _, _, _ := runFields(t, store, seed.run)
	if !phase.Valid || phase.String != "starting" || status != "dispatched" {
		t.Fatalf("terminal create must settle run to starting/dispatched, got phase=%v status=%q", phase, status)
	}
	if reason != "" {
		t.Fatalf("ready run must not set a failure_reason, got %q", reason)
	}
	if w := wsState(t, store, scene.runws); w.S("observedState") != "ready" || !w.B("admissionOpen") {
		t.Fatalf("create success must open the workspace, got %v", w)
	}
	inst := runInstanceRead(t, store, scene.runws, "official/hello-world")
	if inst.S("observedState") != "installed" || inst.S("observedVersion") != "1.0.0" {
		t.Fatalf("run-instance must converge installed@1.0.0, got %v", inst)
	}
	if stub.deleteN != 0 {
		t.Fatalf("ready run must not declare delete, got deleteN=%d", stub.deleteN)
	}
}

// TestPhase2BHookErrorRollsBackTerminalWrite (G-003, §12): when the B settle hook fails — here
// because the run was cancelled and the DeleteRunWorkspace seam errors, aborting the settle — the
// whole terminal transaction rolls back: the operation is not done, the run stays provisioning, the
// workspace admission stays closed, and the plugin instance writeback is undone.
func TestPhase2BHookErrorRollsBackTerminalWrite(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{accepted: true, deleteErr: context.DeadlineExceeded}
	store.AgentRunControlPlane = stub
	store.AgentRunHooks = businessAgentRunHooks{store: store}
	seed := seedDispatchScene(t, store.Pool, false)
	scene := seedRunWorkspaceCreateScene(t, store, seed, "official/hello-world", "1.0.0", "plugin")
	// The run is cancelled before the terminal settles; the settle chooses the cancel path and its
	// DeleteRunWorkspace declaration fails, which must roll back the shared transaction.
	if _, err := store.Pool.Exec(`UPDATE issue_runs SET cancel_requested_at=now(),version=version+1,updated_at=now() WHERE id=$1`, seed.run); err != nil {
		t.Fatalf("cancel run: %v", err)
	}

	effect := planRunPlugin(t, store, scene)
	resultRunPluginSucceeded(t, store, scene, effect.S("id"))
	if _, err := advanceRunWorkspaceCreate(t, store, scene); err == nil {
		t.Fatalf("hook failure must roll back the terminal transaction and surface as an error")
	}
	// The operation write and the B transition must roll back together.
	out := loadOpWith(store, scene.op)
	if out.S("step") != "plugin" || out.S("state") != "running" {
		t.Fatalf("rollback must keep the operation running at plugin, got step=%s state=%s", out.S("step"), out.S("state"))
	}
	phase, status, _, _, _, _ := runFields(t, store, seed.run)
	if !phase.Valid || phase.String != "provisioning" || status != "dispatched" {
		t.Fatalf("rollback must keep the run provisioning/dispatched, got phase=%v status=%q", phase, status)
	}
	if w := wsState(t, store, scene.runws); w.S("observedState") != "starting" || w.B("admissionOpen") {
		t.Fatalf("rollback must not open admission, got %v", w)
	}
}

// TestPhase2BPlanUsesPinnedSnapshotVersionImmutable (G-002): the create_workspace plugin step plans
// the run's *pinned* version from the run snapshot even though the Space roster has since upgraded
// to 2.0.0 — the plan never re-reads the current roster (IssueRun D1/D6 upgrade immutability).
func TestPhase2BPlanUsesPinnedSnapshotVersionImmutable(t *testing.T) {
	store := dispatcherDB(t)
	store.AgentRunHooks = businessAgentRunHooks{store: store}
	seed := seedDispatchScene(t, store.Pool, false)
	ns, idf, _ := pluginIdentity("official/hello-world")
	if _, err := store.Pool.Exec(`INSERT INTO space_plugins(space_id,tenant_id,source_namespace,identifier,desired_state,desired_version,observed_state,observed_version)
		VALUES($1,$2,$3,$4,'installed','2.0.0','installed','2.0.0')`,
		collabSpaceForTenant(t, store, seed.tenant), seed.tenant, ns, idf); err != nil {
		t.Fatalf("seed upgraded roster: %v", err)
	}
	scene := seedRunWorkspaceCreateScene(t, store, seed, "official/hello-world", "1.0.0", "plugin")

	effect := planRunPlugin(t, store, scene)
	request := effect.O("request")
	if request.S("pluginId") != "official/hello-world" || request.S("version") != "1.0.0" {
		t.Fatalf("plan must use the pinned snapshot version 1.0.0, not roster 2.0.0: %v", request)
	}
	inst := runInstanceRead(t, store, scene.runws, "official/hello-world")
	if inst.S("observedState") != "pending" {
		t.Fatalf("planned run plugin must create a pending instance, got %v", inst)
	}
	// The Space roster is untouched by planning a run-scoped instance.
	row := poolObject(t, store, `SELECT desired_version,observed_version FROM space_plugins WHERE space_id=$1 AND source_namespace=$2 AND identifier=$3`,
		collabSpaceForTenant(t, store, seed.tenant), ns, idf)
	if row.S("desiredVersion") != "2.0.0" || row.S("observedVersion") != "2.0.0" {
		t.Fatalf("planning a run instance must not touch the Space roster: %v", row)
	}
}

// TestPhase2BRunInstanceWriterLeavesSpaceAggregateAlone (D-011, §14): failing a disposable run
// Workspace's pinned plugin writes only that instance row — the space_plugins aggregate and the
// Space Agent roster are untouched, so a run-scoped failure can never retire the Agent.
func TestPhase2BRunInstanceWriterLeavesSpaceAggregateAlone(t *testing.T) {
	store := dispatcherDB(t)
	seed := seedDispatchScene(t, store.Pool, false)
	ns, idf, _ := pluginIdentity("official/hello-world")
	sid := collabSpaceForTenant(t, store, seed.tenant)
	if _, err := store.Pool.Exec(`INSERT INTO space_plugins(space_id,tenant_id,source_namespace,identifier,desired_state,desired_version,observed_state,observed_version)
		VALUES($1,$2,$3,$4,'installed','1.0.0','installed','1.0.0')`, sid, seed.tenant, ns, idf); err != nil {
		t.Fatalf("seed space_plugins: %v", err)
	}
	scene := seedRunWorkspaceCreateScene(t, store, seed, "official/hello-world", "1.0.0", "plugin")
	// Ensure the run instance row, then fail it through the run-instance-only writer.
	effect := planRunPlugin(t, store, scene)
	msg := "download failed"
	_, err := store.transact(context.Background(), func(tx *transaction) Object {
		e := tx.one("SELECT * FROM external_effects WHERE id=$1", effect.S("id"))
		writeRunPluginInstance(tx, e, "failed", "", &msg)
		return nil
	})
	if err != nil {
		t.Fatalf("fail run instance: %v", err)
	}
	inst := runInstanceRead(t, store, scene.runws, "official/hello-world")
	if inst.S("observedState") != "failed" || inst.S("installError") != "download failed" {
		t.Fatalf("run instance must fail in isolation, got %v", inst)
	}
	// The Space aggregate and roster are unchanged: still installed@1.0.0 and agent still active.
	row := poolObject(t, store, `SELECT desired_version,observed_version,observed_state FROM space_plugins WHERE space_id=$1 AND source_namespace=$2 AND identifier=$3`, sid, ns, idf)
	if row.S("observedState") != "installed" || row.S("observedVersion") != "1.0.0" {
		t.Fatalf("run instance failure must not perturb the Space aggregate, got %v", row)
	}
	var status string
	if err := store.Pool.QueryRow(`SELECT status FROM space_agents WHERE tenant_id=$1`, seed.tenant).Scan(&status); err != nil {
		t.Fatalf("read roster: %v", err)
	}
	if status != "active" {
		t.Fatalf("run instance failure must not retire the Space Agent, got status=%s", status)
	}
}
