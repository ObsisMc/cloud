package core

// Phase 2B DB tests (priority G-002 + G-003): the create_workspace Agent plugin step and the
// A-side terminal wiring that settles the run in the same transaction. White-box (package core)
// for the same reason as the Phase 2A settle suite: the AgentRunHooks seam and the A-side
// advance/planPluginEffect/effectResult take the unexported *transaction. They reuse
// dispatcherDB/seedDispatchScene/stubAgentRunControlPlane and the settle-test helpers
// (bindRunWorkspace/stageSettleState/runFields/settleRun), all against real PostgreSQL.
//
// Evidence targets:
//   - T2-3 (plan §2.14, DEFERRED_TO_PHASE_2B_G002) is un-deferred: a provisioning run whose
//     pinned agent-plugin instance is failed settles releasing/failed/agent_plugin_unavailable
//     and declares the delete once — the authoritative plugin-evidence failure path.
//   - G-002 plugin step: plan uses the run snapshot's pinned version, admission stays closed
//     until the plugin step, and the run-instance writer never perturbs the space aggregate.
//   - G-003 A→B wiring: the terminal create_workspace settles the run in the same transaction,
//     and a hook error rolls the whole terminal write back.

import (
	"database/sql"
	"testing"
)

// seedRunPinnedPlugin writes the run's AgentSession-style snapshot (agentPluginId /
// agentPluginVersion) into issue_runs.input and loses the run-to-workspace binding to nothing;
// the version is the *pinned* one, decoupled from any current roster version (IssueRun D1/D6).
func seedRunPinnedPlugin(t *testing.T, pool *sql.DB, runID, pluginID, version string) {
	t.Helper()
	_, err := pool.Exec(`UPDATE issue_runs SET input=$2, version=version+1, updated_at=now() WHERE id=$1`,
		runID, jsonText(Object{"agentPluginId": pluginID, "agentPluginVersion": version}))
	if err != nil {
		t.Fatalf("pin run plugin snapshot: %v", err)
	}
}

// seedRunPluginInstanceRow converges the run Workspace's own plugin-instance row to the given
// observed state/version (or removes it when state == ""), by the workspace composite identity.
func seedRunPluginInstanceRow(t *testing.T, pool *sql.DB, workspaceID, pluginID, state, version string) {
	t.Helper()
	namespace, identifier, ok := pluginIdentity(pluginID)
	if !ok {
		t.Fatalf("bad plugin id %q", pluginID)
	}
	if state == "" {
		if _, err := pool.Exec(`DELETE FROM workspace_plugin_instances WHERE workspace_id=$1 AND source_namespace=$2 AND identifier=$3`,
			workspaceID, namespace, identifier); err != nil {
			t.Fatalf("clear instance: %v", err)
		}
		return
	}
	_, err := pool.Exec(`INSERT INTO workspace_plugin_instances(workspace_id,tenant_id,owner_user_id,project_id,source_namespace,identifier,observed_state,observed_version)
		SELECT w.id,w.tenant_id,w.owner_user_id,w.project_id,$2,$3,$4,$5 FROM workspaces w WHERE w.id=$1
		ON CONFLICT (workspace_id,source_namespace,identifier) DO UPDATE SET observed_state=$4,observed_version=$5,version=workspace_plugin_instances.version+1,updated_at=now()`,
		workspaceID, namespace, identifier, state, nullable(version))
	if err != nil {
		t.Fatalf("seed instance row: %v", err)
	}
}

// stagePluginSettleScene binds the run to the seed main workspace, pins the run snapshot, sets
// the run-instance evidence, and stages provisioning/dispatched — the pre-settle authoritative
// state the plugin classification tests start from. Returns the bound workspace id.
func stagePluginSettleScene(t *testing.T, store *Store, seed dispSeed, pluginID, pinnedVersion, instState, instVersion string) string {
	t.Helper()
	wid := bindRunWorkspace(t, store, seed.run, seed.project)
	seedRunPinnedPlugin(t, store.Pool, seed.run, pluginID, pinnedVersion)
	seedRunPluginInstanceRow(t, store.Pool, wid, pluginID, instState, instVersion)
	stageSettleState(t, store, seed.run, "provisioning", "dispatched", false)
	return wid
}

// TestPhase2BT2_3PluginFailureSettlesAgentPluginUnavailable (T2-3, was DEFERRED_TO_PHASE_2B_G002):
// with the pinned agent-plugin instance observed failed, a ready terminal still settles the run
// releasing/failed/agent_plugin_unavailable and declares the delete once — the authoritative
// plugin-evidence failure (not a generic workspace_unavailable guess).
func TestPhase2BT2_3PluginFailureSettlesAgentPluginUnavailable(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{accepted: true}
	store.AgentRunControlPlane = stub
	seed := seedDispatchScene(t, store.Pool, false)
	stagePluginSettleScene(t, store, seed, "official/hello-world", "1.0.0", "failed", "")

	if err := settleRun(t, store, seed.run, seed.tenant, true, ""); err != nil {
		t.Fatalf("plugin-failure settle must not error: %v", err)
	}
	phase, status, reason, _, _, _ := runFields(t, store, seed.run)
	if !phase.Valid || phase.String != "releasing" || status != "failed" {
		t.Fatalf("plugin failure must reach releasing/failed, got phase=%v status=%q", phase, status)
	}
	if reason != "agent_plugin_unavailable" {
		t.Fatalf("plugin failure must set failure_reason=agent_plugin_unavailable, got %q", reason)
	}
	if stub.deleteN != 1 {
		t.Fatalf("plugin failure must declare delete once, got deleteN=%d", stub.deleteN)
	}
	if n := countRunActivities(t, store, seed.issue, "run.failed"); n != 1 {
		t.Fatalf("plugin failure must append one run.failed activity, got %d", n)
	}
}

// TestPhase2BPluginInstalledAtPinnedSettlesReady: a run whose pinned plugin instance is installed
// at the pinned version settles ready (provisioning → starting, no delete) — the positive
// plugin-readiness terminal (T2-1 semantics extended by plugin evidence).
func TestPhase2BPluginInstalledAtPinnedSettlesReady(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{accepted: true}
	store.AgentRunControlPlane = stub
	seed := seedDispatchScene(t, store.Pool, false)
	stagePluginSettleScene(t, store, seed, "official/hello-world", "1.0.0", "installed", "1.0.0")

	if err := settleRun(t, store, seed.run, seed.tenant, true, ""); err != nil {
		t.Fatalf("ready settle with installed plugin: %v", err)
	}
	phase, status, reason, _, _, _ := runFields(t, store, seed.run)
	if !phase.Valid || phase.String != "starting" || status != "dispatched" {
		t.Fatalf("installed@pin must settle starting/dispatched, got phase=%v status=%q", phase, status)
	}
	if reason != "" {
		t.Fatalf("installed@pin must not set a failure_reason, got %q", reason)
	}
	if stub.deleteN != 0 {
		t.Fatalf("installed@pin must not declare delete, got deleteN=%d", stub.deleteN)
	}
}

// TestPhase2BPluginVersionMismatchSettlesUnavailable: the settle compares the instance version to
// the *snapshot* version — a version installed but not the pinned one is still unavailable.
func TestPhase2BPluginVersionMismatchSettlesUnavailable(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{accepted: true}
	store.AgentRunControlPlane = stub
	seed := seedDispatchScene(t, store.Pool, false)
	// pinned 1.0.0 but a different 2.0.0 is what got installed.
	stagePluginSettleScene(t, store, seed, "official/hello-world", "1.0.0", "installed", "2.0.0")

	if err := settleRun(t, store, seed.run, seed.tenant, true, ""); err != nil {
		t.Fatalf("version-mismatch settle: %v", err)
	}
	if _, _, reason, _, _, _ := runFields(t, store, seed.run); reason != "agent_plugin_unavailable" {
		t.Fatalf("wrong installed version must settle agent_plugin_unavailable, got %q", reason)
	}
}

// collabSpaceForTenant returns the tenant's collaboration space id (the space_agents/space_plugins
// scope) — the one seedDispatchScene created.
func collabSpaceForTenant(t *testing.T, store *Store, tenantID string) string {
	t.Helper()
	var sid string
	if err := store.Pool.QueryRow(`SELECT id FROM collab_workspaces WHERE tenant_id=$1 LIMIT 1`, tenantID).Scan(&sid); err != nil {
		t.Fatalf("load collab space: %v", err)
	}
	return sid
}

// TestPhase2BPluginUpgradeKeepsHistoricalSnapshot: after the Space upgrades the plugin to 2.0.0,
// a historical run whose snapshot pinned 1.0.0 and whose instance is installed at 1.0.0 still
// settles ready — the roster version is never re-read, the snapshot is authoritative.
func TestPhase2BPluginUpgradeKeepsHistoricalSnapshot(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{accepted: true}
	store.AgentRunControlPlane = stub
	seed := seedDispatchScene(t, store.Pool, false)
	// Simulate the roster having moved to 2.0.0 (as if installSpacePlugin had been called).
	namespace, identifier, _ := pluginIdentity("official/hello-world")
	if _, err := store.Pool.Exec(`INSERT INTO space_plugins(space_id,tenant_id,source_namespace,identifier,desired_state,desired_version,observed_state,observed_version)
		VALUES($1,$2,$3,$4,'installed','2.0.0','installed','2.0.0') ON CONFLICT (space_id,source_namespace,identifier) DO UPDATE SET desired_version='2.0.0',observed_version='2.0.0',version=space_plugins.version+1`,
		collabSpaceForTenant(t, store, seed.tenant), seed.tenant, namespace, identifier); err != nil {
		t.Fatalf("seed upgraded roster: %v", err)
	}
	// The historical run pinned 1.0.0 and has 1.0.0 installed on its workspace.
	stagePluginSettleScene(t, store, seed, "official/hello-world", "1.0.0", "installed", "1.0.0")

	if err := settleRun(t, store, seed.run, seed.tenant, true, ""); err != nil {
		t.Fatalf("upgraded-roster settle: %v", err)
	}
	if phase, _, reason, _, _, _ := runFields(t, store, seed.run); !phase.Valid || phase.String != "starting" || reason != "" {
		t.Fatalf("snapshot-pinned 1.0.0 must still settle ready after roster moved to 2.0.0, got phase=%v reason=%q", phase, reason)
	}
	if stub.deleteN != 0 {
		t.Fatalf("snapshot-pinned ready must not declare delete, got deleteN=%d", stub.deleteN)
	}
}

// TestPhase2BPluginMissingInstanceTreatsReady (legacy compatibility): a run whose snapshot pins a
// plugin but whose workspace as yet has no instance row (pre-plugin-engine settle) is treated as
// ready, per plan §2.6 — it keeps T2-1 semantics until G-002's plugin step creates the row.
func TestPhase2BPluginMissingInstanceTreatsReady(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{accepted: true}
	store.AgentRunControlPlane = stub
	seed := seedDispatchScene(t, store.Pool, false)
	stagePluginSettleScene(t, store, seed, "official/hello-world", "1.0.0", "", "")

	if err := settleRun(t, store, seed.run, seed.tenant, true, ""); err != nil {
		t.Fatalf("no-instance settle: %v", err)
	}
	if phase, status, reason, _, _, _ := runFields(t, store, seed.run); !phase.Valid || phase.String != "starting" || status != "dispatched" || reason != "" {
		t.Fatalf("no-instance must settle ready (legacy), got phase=%v status=%q reason=%q", phase, status, reason)
	}
}

// TestPhase2BPluginPendingSettlesUnavailable: a plugin still pending (not yet installed) when the
// terminal settles is not available evidence — fail-safe to agent_plugin_unavailable.
func TestPhase2BPluginPendingSettlesUnavailable(t *testing.T) {
	store := dispatcherDB(t)
	stub := &stubAgentRunControlPlane{accepted: true}
	store.AgentRunControlPlane = stub
	seed := seedDispatchScene(t, store.Pool, false)
	stagePluginSettleScene(t, store, seed, "official/hello-world", "1.0.0", "pending", "")

	if err := settleRun(t, store, seed.run, seed.tenant, true, ""); err != nil {
		t.Fatalf("pending-instance settle: %v", err)
	}
	if _, _, reason, _, _, _ := runFields(t, store, seed.run); reason != "agent_plugin_unavailable" {
		t.Fatalf("pending instance must settle agent_plugin_unavailable, got %q", reason)
	}
}
