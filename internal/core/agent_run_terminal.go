package core

// The A-side create_workspace terminal wiring for an Agent run's isolated Workspace
// (priority G-002 + G-003, plan §2B.1/§2B.2, controller-integration D6 runWorkspaceSettled).
//
// A real-Agent run's Workspace must not open admission the instant its infra is ready: its
// terminal success is conditioned on the required pinned agent plugin being installed at the
// run-create snapshot version (IssueRun D1/D6), and the run must transition out of
// provisioning only in the same transaction that commits that terminal fact. This file holds
// the three pieces that make that true without touching the shared install/remove_plugin
// machinery (G-005 stays out of scope):
//
//   - identity predicates that recognize an Agent run Workspace and its create_workspace op,
//   - the plugin step branch: a run-scoped plugin_ensure effect for the *pinned* plugin that
//     is admitted from the run snapshot (never the current roster), written back to a
//     run-instance-only row, and
//   - the on-done hook: the B-owned settle (RunWorkspaceSettled) called in this same
//     transaction, whose error rolls the whole terminal write back.
//
// Deliberate boundary: this A-side code never writes issue_runs. The run only ever moves out of
// provisioning through the AgentRunHooks seam (settleRunWorkspace), never by a direct A-side
// UPDATE. The workspace_plugin_instances row this code creates/observes is A-side operation
// evidence that B re-reads (settle) at settlement, exactly the authoritative plugin-readiness
// fact the mandate §4/§7 require — never a B field written by A.

// isAgentRunWorkspace reports whether wid is the isolated Workspace a real-Agent run creates,
// identified by its workspaces.issue_run_id binding (IssueRun D2, ADR D4). Only these take the
// create_workspace plugin step and the terminal RunWorkspaceSettled hook; every other
// workspace's create flow is untouched.
func isAgentRunWorkspace(t *transaction, wid string) bool {
	return wid != "" && t.one("SELECT id FROM workspaces WHERE id=$1 AND issue_run_id IS NOT NULL AND deleted_at IS NULL", wid) != nil
}

// agentRunWorkspaceOp reports whether o is the create_workspace operation of an Agent run
// Workspace. It is the only operation shape that gains the pinned plugin step and the
// terminal settle wiring; create_project and non-run create_workspace are unaffected.
func agentRunWorkspaceOp(t *transaction, o Object) bool {
	return o.S("kind") == "create_workspace" && isAgentRunWorkspace(t, o.S("workspaceId"))
}

// runPinnedAgentPlugin reads the run-create snapshot's pinned agent plugin identity/version
// from issue_runs.input (keys agentPluginId / agentPluginVersion, snapshotAgentRunInput), via
// the workspace→run binding. required=false means the run pins no agent plugin (legacy engine
// or a non-space-agent executor); such a run never drove a create_workspace plugin step, so the
// caller must open the Workspace without one (plan §2.6 legacy compatibility).
func runPinnedAgentPlugin(t *transaction, wid string) (pluginID, version string, required bool) {
	row := t.one(`SELECT r.input FROM issue_runs r JOIN workspaces w ON w.id=$1 AND r.id=w.issue_run_id`, wid)
	if row == nil {
		return "", "", false
	}
	in := row.O("input")
	id, ver := in.S("agentPluginId"), in.S("agentPluginVersion")
	if id == "" || ver == "" {
		return "", "", false
	}
	return id, ver, true
}

// ensureRunPluginInstance ensures the run Workspace carries one pending workspace_plugin_instances
// row for the pinned plugin, drawing tenant/owner/project from the workspace row so the composite
// foreign keys hold. The row is the A-side evidence settle re-reads; it is stamped pending now and
// converges to installed/failed only from the plugin_ensure effect outcome.
func ensureRunPluginInstance(t *transaction, wid, pluginID string) {
	namespace, identifier, ok := pluginIdentity(pluginID)
	if !ok {
		return
	}
	t.exec(`INSERT INTO workspace_plugin_instances(workspace_id,tenant_id,owner_user_id,project_id,source_namespace,identifier,observed_state)
		SELECT w.id,w.tenant_id,w.owner_user_id,w.project_id,$2,$3,'pending' FROM workspaces w WHERE w.id=$1
		ON CONFLICT (workspace_id,source_namespace,identifier) DO NOTHING`, wid, namespace, identifier)
}

// writeRunPluginInstance converges the run Workspace's own plugin-instance row to the given
// state from a plugin_ensure outcome. It deliberately does NOT touch space_plugins nor
// reconcileAgentOnAggregate: a disposable run Workspace failing its pinned agent plugin must never
// retire the space Agent or perturb the space aggregate (plan §14, record D-011). The row is
// guaranteed to exist by ensureRunPluginInstance at plan time. e carries the effect request whose
// pluginId is the pinned identity and whose workspaceId scopes the row.
func writeRunPluginInstance(t *transaction, e Object, state, version string, installError *string) {
	namespace, identifier, ok := pluginIdentity(e.O("request").S("pluginId"))
	if !ok {
		return
	}
	var err any
	if installError != nil {
		err = *installError
	}
	t.exec(`UPDATE workspace_plugin_instances SET observed_state=$4,observed_version=$5,install_error=$6,version=version+1,updated_at=now()
		WHERE workspace_id=$1 AND source_namespace=$2 AND identifier=$3`, e.S("workspaceId"), namespace, identifier, state, nullable(version), err)
}

// planAgentRunPluginEffect builds the self-contained plugin_ensure payload for the run
// Workspace's pinned agent plugin. The identity AND version come from the run-create snapshot
// (agentPluginId/agentPluginVersion), never the current space_plugins/spaced_agent roster, so a
// later upgrade cannot re-point a historical run (IssueRun D1/D6 snapshot immutability); the
// download release (url/sha256/targets) is copied from the catalog snapshot, matching the shared
// plugin_ensure payload shape the Node execution plane already consumes.
func planAgentRunPluginEffect(t *transaction, o Object, wid string) Object {
	pluginID, version, required := runPinnedAgentPlugin(t, wid)
	require(required && pluginID != "" && version != "", 409, "invalid_plugin_request")
	if _, _, ok := pluginIdentity(pluginID); !ok {
		reject(409, "invalid_plugin_request")
	}
	ensureRunPluginInstance(t, wid, pluginID)
	request := Object{"kind": "plugin_ensure", "projectId": o.S("projectId"), "workspaceId": wid, "pluginId": pluginID, "version": version}
	if entry := t.pluginCatalogEntry(pluginID); entry != nil {
		if entry.S("url") != "" {
			request["universal"] = Object{"url": entry.S("url"), "sha256": entry.S("sha256")}
		}
		if entry["targets"] != nil {
			request["targets"] = entry["targets"]
		}
	}
	return request
}

// advanceRunWorkspacePlugin completes the run Workspace's pinned plugin step. Per ADR D2 the
// single plugin_ensure install does not gate Workspace admission: a succeeded install is written
// installed@the pinned version, a failed install was already written failed by effect_result, and
// in both cases the Workspace opens and the operation finishes. It is the settlement core that
// classifies the run from the instance — installed@pinned → starting, else
// agent_plugin_unavailable — so the read of the failure kind lives on the B side, never guessed
// in the hook (mandate §8/§15).
func advanceRunWorkspacePlugin(t *transaction, o Object, wid string) string {
	e := effectFor(t, o.S("id"), "plugin_ensure", wid)
	require(e != nil, 409, "effect_incomplete")
	switch e.S("state") {
	case "succeeded":
		writeRunPluginInstance(t, e, "installed", e.O("request").S("version"), nil)
	case "failed":
		// instance row already converged to failed by effect_result; nothing more to write.
	default:
		reject(409, "effect_incomplete")
	}
	openWorkspace(t, o, wid)
	return "done"
}

// settleRunWorkspaceOnDone is the terminal A→B hook invocations. It runs inside the caller-owned
// create_workspace transaction, after the operation has been written state='succeeded', and moves
// the run out of provisioning via the AgentRunHooks seam. A hook error panics with databaseFailure
// so the enclosing transaction rolls back the terminal write too: the operation can never be
// committed succeeded while its run stays provisioning (mandate §12/§13, plan §4/§6). The run to
// settle is looked up through the workspace→run binding, never taken from caller input.
func settleRunWorkspaceOnDone(t *transaction, o Object) {
	run := t.one(`SELECT r.id,r.tenant_id FROM issue_runs r WHERE r.workspace_id=$1 AND r.deleted_at IS NULL`, o.S("workspaceId"))
	if run == nil {
		return
	}
	h := t.hooks
	if h == nil {
		h = UnavailableAgentRunHooks{} // nil-safe fail closed: no business transition without a wired hook
	}
	if err := h.RunWorkspaceSettled(t, Object{"id": run.S("id"), "tenantId": run.S("tenantId")}, true); err != nil {
		panic(databaseFailure{err})
	}
}
