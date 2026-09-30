package core

// Space Agent roster (IssueRun D1, plugin-marketplace "node executes plugin
// installs" D3).
//
// A Space Agent is the Cloud-side projection of an *agent-class plugin* that the
// Space has selected and whose aggregate install has converged to `installed`.
// It is one row per (space, plugin_id) in `space_agents`: `active` while the
// plugin is installed, `retired` after removal. The rows are never deleted, so
// historical IssueRuns keep resolving the executor that ran them.
//
// Ownership and transaction boundary: the roster is derived entirely from the
// Space-level plugin selection and its fan-out aggregate — never from any single
// Workspace's instance (node-executes-plugin-installs D3). The authoritative
// writebacks are the same transactions that converge `space_plugins`:
//   - when the aggregate converges to `installed`, activate the agent row;
//   - when the plugin's `desired_state` becomes `removed`, retire it.
// These run inside the plugin effect/operation transaction; nothing here opens
// its own transaction, spawns a goroutine, or polls the plugin tables.

// agentKind is the plugin-marketplace category that maps to a Space Agent. Only
// plugins whose catalog entry kind is "agent" ever join the roster; every other
// kind (skill, hook, mcp, webview, workbench, workflow, pack) installs/removes
// with no effect on `space_agents` (IssueRun D1, plugin-marketplace D3).
const agentKind = "agent"

// spaceAgentRow loads one space_agents row for a canonical plugin id, or nil if
// the Space has never installed this plugin.
func spaceAgentRow(t *transaction, spaceID, pluginID string) Object {
	return t.one("SELECT * FROM space_agents WHERE space_id=$1 AND plugin_id=$2", spaceID, pluginID)
}

// activeSpaceAgentRoster returns the Space's current selectable agents
// (`status = active`), ordered for a stable @-mention list. Retired agents are
// deliberately excluded: they no longer appear as new targets, though their
// historical IssueRun references remain intact (IssueRun D1).
func activeSpaceAgentRoster(t *transaction, spaceID string) []Object {
	return t.list("SELECT * FROM space_agents WHERE space_id=$1 AND status='active' ORDER BY plugin_id", spaceID)
}

// spaceAgentFromPlugin is the internal policy that turns a catalog entry into
// the roster row's mutable identity fields. Only the plugin's own persisted
// metadata is authoritative: the canonical plugin id and its title. This keeps
// the roster decoupled from any editor-facing Agent configuration, which does
// not exist in v1 (IssueRun D1: "第一版不开放 Agent CRUD").
func spaceAgentFromPlugin(entry Object) (pluginID, displayName string) {
	return entry.S("id"), entry.S("title")
}

// activateSpaceAgent marks the agent-class plugin's roster row `active`,
// creating it on first install and reviving a previously-retired row on
// reinstall. Idempotent and replay-safe: an already-active row is a no-op (no
// version bump, no new row), so re-installing the same pinned version or
// replaying the same convergence event never duplicates identity or advances the
// version spuriously. Caller must already hold the authoritative install
// transaction (a converged space_plugins aggregate) — this does not verify the
// plugin is agent-class, that gating is the caller's responsibility so the
// roster stays consistent with exactly the D3 terminal transitions.
func activateSpaceAgent(t *transaction, spaceID, pluginID, displayName string) Object {
	row := t.one("SELECT id,status FROM space_agents WHERE space_id=$1 AND plugin_id=$2", spaceID, pluginID)
	if row != nil && row.S("status") == "active" {
		return spaceAgentRow(t, spaceID, pluginID)
	}
	tenant := t.one("SELECT tenant_id FROM collab_workspaces WHERE id=$1", spaceID)
	require(tenant != nil, 409, "invalid_effect_scope")
	if row == nil {
		id := newID()
		t.exec(`INSERT INTO space_agents(id,space_id,tenant_id,plugin_id,display_name,status) VALUES($1,$2,$3,$4,$5,'active')`,
			id, spaceID, tenant.S("tenantId"), pluginID, displayName)
		return spaceAgentRow(t, spaceID, pluginID)
	}
	// Revive: retired -> active, clear the retirement timestamp, bump version.
	t.exec(`UPDATE space_agents SET status='active',retired_at=NULL,display_name=$3,version=version+1,updated_at=now() WHERE space_id=$1 AND plugin_id=$2`,
		spaceID, pluginID, displayName)
	return spaceAgentRow(t, spaceID, pluginID)
}

// retireSpaceAgent marks the agent-class plugin's roster row `retired` and stamps
// retired_at. Idempotent: an already-retired row is a no-op, and a row that was
// never activated (plugin removed before install converged) is not created. The
// row is never deleted, preserving the executor identity for historical runs.
// Caller must already hold the transaction where the plugin's desired_state
// becomes `removed` (plugin-marketplace D3).
func retireSpaceAgent(t *transaction, spaceID, pluginID string) {
	row := spaceAgentRow(t, spaceID, pluginID)
	if row == nil || row.S("status") == "retired" {
		return
	}
	t.exec(`UPDATE space_agents SET status='retired',retired_at=now(),version=version+1,updated_at=now() WHERE space_id=$1 AND plugin_id=$2`,
		spaceID, pluginID)
}

// reconcileAgentOnAggregate is the single decision point that maps a recomputed
// space_plugins aggregate onto the roster, inside the same transaction that
// persists that aggregate (plugin-marketplace D3). It is called only when the
// aggregate reaches a terminal state relevant to agents:
//
//   - installed  -> activate the roster row if the plugin is agent-class
//   - removed    -> no action here: retirement is desired_state-driven and was
//     already handled at remove-intent time (D3), so nothing else touches the
//     roster.
//
// It looks up the catalog entry itself so the caller only needs the space and the
// canonical plugin id; the catalog is the sole authority for the agent-class kind
// and the roster display name. Because this runs in the aggregate writeback
// transaction, an agent row can never appear before the plugin converged, and a
// failed/non-agent install can never create one.
func reconcileAgentOnAggregate(t *transaction, spaceID, pluginID, aggregate string) {
	if aggregate != "installed" {
		return
	}
	entry := t.pluginCatalogEntry(pluginID)
	if entry == nil || entry.S("kind") != agentKind {
		return
	}
	canonical, title := spaceAgentFromPlugin(entry)
	activateSpaceAgent(t, spaceID, canonical, title)
}
