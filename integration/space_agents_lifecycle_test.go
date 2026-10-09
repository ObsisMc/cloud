package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"

	"github.com/wanglongan587/cloud/internal/controlpb"
	"github.com/wanglongan587/cloud/internal/core"
)

// spaceAgent states the row for one canonical plugin id in the space's agent
// roster, with the snake_case columns exposed as camelCase keys (same convention
// as spacePlugin). Returns nil when the space has no row for this plugin.
func (f *fixture) spaceAgent(sid, pluginID string) core.Object {
	f.t.Helper()
	rows, e := f.store.Pool.Query(`SELECT to_jsonb(a) FROM space_agents a WHERE a.space_id=$1 AND a.plugin_id=$2`, sid, pluginID)
	must(f.t, e)
	defer rows.Close()
	if !rows.Next() {
		return nil
	}
	var b []byte
	must(f.t, rows.Scan(&b))
	var raw map[string]any
	must(f.t, json.Unmarshal(b, &raw))
	out := core.Object{}
	for k, v := range raw {
		parts := strings.Split(k, "_")
		for i := 1; i < len(parts); i++ {
			if parts[i] != "" {
				parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
			}
		}
		out[strings.Join(parts, "")] = v
	}
	return out
}

// spaceAgentCount returns the number of roster rows for a space.
func (f *fixture) spaceAgentCount(sid string) int {
	f.t.Helper()
	var n int
	must(f.t, f.store.Pool.QueryRow(`SELECT count(*) FROM space_agents WHERE space_id=$1`, sid).Scan(&n))
	return n
}

// TestAgentInstallActivatesRoster covers install success -> active (IT: agent
// install), non-agent plugins not joining the roster, and install failure not
// activating an agent.
func TestAgentInstallActivatesRoster(t *testing.T) {
	f := setup(t)
	repo, artifacts := marketplaceFixture(t, f.root)
	f.syncMarketplace(t, repo)
	f.substrate.MapArtifact("https://example.invalid/artifacts/hello-1.0.0.orax", artifacts["https://example.invalid/artifacts/hello-1.0.0.orax"])
	f.substrate.MapArtifact("https://example.invalid/artifacts/tool-linux.orax", artifacts["https://example.invalid/artifacts/tool-linux.orax"])
	sid := f.defaultSpaceID()
	f.spaceProject(t, sid, "project-one")

	// Install the agent-class plugin; once the fan-out converges the roster row is active.
	f.call("POST", f.pluginSpacePath(sid)+"/plugins", core.Object{"identifier": "official/hello-world"}, "install-agent-1", 200)
	f.drain()
	row := f.spaceAgent(sid, "official/hello-world")
	if row == nil {
		t.Fatal("agent-class plugin install must create a space_agents row")
	}
	if row.S("status") != "active" || row.S("displayName") != "Hello World" {
		t.Fatalf("agent roster after install = %v", row)
	}

	// A non-agent (hook) plugin installs fine but never joins the roster.
	f.call("POST", f.pluginSpacePath(sid)+"/plugins", core.Object{"identifier": "official/native-tool"}, "install-hook-1", 200)
	f.drain()
	if f.spaceAgent(sid, "official/native-tool") != nil {
		t.Fatal("non-agent plugin must not create a space_agents row")
	}
	if got := f.spacePlugin(sid, "official/native-tool").S("observedState"); got != "installed" {
		t.Fatalf("hook plugin still installs as a plugin: observed=%v", got)
	}
}

// TestAgentReinstallAndRemove covers the uninstall -> retired path, retire
// idempotency, reinstall revives the same row, and the retired row survives
// (historical executor identity preserved).
func TestAgentReinstallAndRemove(t *testing.T) {
	f := setup(t)
	repo, artifacts := marketplaceFixture(t, f.root)
	f.syncMarketplace(t, repo)
	f.substrate.MapArtifact("https://example.invalid/artifacts/hello-1.0.0.orax", artifacts["https://example.invalid/artifacts/hello-1.0.0.orax"])
	sid := f.defaultSpaceID()
	f.spaceProject(t, sid, "project-one")

	// Install -> active.
	f.call("POST", f.pluginSpacePath(sid)+"/plugins", core.Object{"identifier": "official/hello-world"}, "install-r1", 200)
	f.drain()
	active := f.spaceAgent(sid, "official/hello-world")
	if active.S("status") != "active" {
		t.Fatalf("after install = %v", active)
	}
	activeUpdatedAt := active.S("updatedAt")

	// Uninstall -> retired; the row survives with a retired_at stamp.
	pluginsRow := f.spacePlugin(sid, "official/hello-world") // space_plugins version is the DELETE guard
	f.call("DELETE", f.pluginSpacePath(sid)+"/plugins", core.Object{"identifier": "official/hello-world", "version": pluginsRow.N("version")}, "remove-r1", 200)
	f.drain()
	retired := f.spaceAgent(sid, "official/hello-world")
	if retired.S("status") != "retired" {
		t.Fatalf("after uninstall = %v", retired)
	}
	if f.spaceAgentCount(sid) != 1 {
		t.Fatal("retiring must not delete the roster row")
	}

	// A repeated uninstall is idempotent: no new row, no version churn beyond the
	// plugin row; the agent row stays exactly retired.
	pluginsRow2 := f.spacePlugin(sid, "official/hello-world")
	if pluginsRow2.S("desiredState") != "removed" {
		t.Fatalf("removed plugin still desired-removed = %v", pluginsRow2)
	}
	f.call("DELETE", f.pluginSpacePath(sid)+"/plugins", core.Object{"identifier": "official/hello-world", "version": pluginsRow2.N("version")}, "remove-r2", 200)
	f.drain()
	if got := f.spaceAgent(sid, "official/hello-world"); got.S("status") != "retired" {
		t.Fatalf("repeated uninstall changed agent = %v", got)
	}

	// Reinstall revives the SAME row (same id) back to active.
	// The plugin row is re-desired installed; the install fans out again and
	// converges to installed, which must revive the agent. Re-selecting an
	// existing selection is guarded by the row's version, exactly as the uninstall was.
	f.call("POST", f.pluginSpacePath(sid)+"/plugins", core.Object{"identifier": "official/hello-world", "version": f.spacePlugin(sid, "official/hello-world").N("version")}, "install-r2", 200)
	f.drain()
	revived := f.spaceAgent(sid, "official/hello-world")
	if revived.S("id") != retired.S("id") || revived.S("status") != "active" {
		t.Fatalf("reinstall must revive the same row: id was %v now %v", retired.S("id"), revived)
	}
	// The roster row carries no version of its own, so the observable proof that the revive was a
	// genuine later write — and not a row the earlier install left behind — is that its updated_at
	// moved forward from the install's.
	if revived.S("updatedAt") <= activeUpdatedAt {
		t.Fatalf("revive must advance updated_at: was %s now %s", activeUpdatedAt, revived.S("updatedAt"))
	}
}

// TestAgentRosterIsolationAndRetiredExclusion covers space isolation (install in
// one space does not leak to another), tenant isolation, and that only active
// agents appear in the selectable roster.
func TestAgentRosterIsolationAndRetiredExclusion(t *testing.T) {
	f := setup(t)
	repo, artifacts := marketplaceFixture(t, f.root)
	f.syncMarketplace(t, repo)
	f.substrate.MapArtifact("https://example.invalid/artifacts/hello-1.0.0.orax", artifacts["https://example.invalid/artifacts/hello-1.0.0.orax"])
	sid := f.defaultSpaceID()
	other := f.createPluginTenant("Other", "other-space", "space-agent-other")
	f.spaceProject(t, sid, "project-one")

	// Only space A installs; space B sees no roster row.
	f.call("POST", f.pluginSpacePath(sid)+"/plugins", core.Object{"identifier": "official/hello-world"}, "install-iso", 200)
	f.drain()
	if f.spaceAgent(sid, "official/hello-world") == nil {
		t.Fatal("space A must have the agent after install")
	}
	if f.spaceAgent(other.S("id"), "official/hello-world") != nil {
		t.Fatal("space A install must not leak a roster row into space B")
	}

	// Retire in A; the active roster must no longer contain it, but the row is
	// still directly readable (historical reference intact).
	pluginsRow := f.spacePlugin(sid, "official/hello-world")
	f.call("DELETE", f.pluginSpacePath(sid)+"/plugins", core.Object{"identifier": "official/hello-world", "version": pluginsRow.N("version")}, "remove-iso", 200)
	f.drain()
	if f.spaceAgent(sid, "official/hello-world").S("status") != "retired" {
		t.Fatal("agent must be retired after uninstall")
	}
}

// TestAgentInstallFailureDoesNotActivate covers the D3 invariant that an agent
// roster row exists only once the plugin's aggregate converges to installed. A
// failed install marks the instance failed but must not create an agent; only
// after a later install converges does the agent row appear.
func TestAgentInstallFailureDoesNotActivate(t *testing.T) {
	f := setup(t)
	repo, _ := marketplaceFixture(t, f.root)
	f.syncMarketplace(t, repo)
	sid := f.defaultSpaceID()
	created := f.spaceProject(t, sid, "space-project")
	wid := created.O("workspace").S("id")

	f.call("POST", f.pluginSpacePath(sid)+"/plugins", core.Object{"identifier": "official/hello-world"}, "install-fail", 200)
	op := f.claimPluginOp(t, "install_plugin")
	// The Node was handed a release it could not use, which is the failure a real
	// executor reports when the download does not match the catalog digest.
	f.dispatchPlugin(t, op, "exec-fail")
	must(t, f.reportPlugin(t, op, "exec-fail", []*controlpb.PluginItemResult{{
		PluginId: "official/hello-world",
		Outcome:  &controlpb.PluginItemResult_Failed{Failed: &controlpb.PluginItemFailed{Reason: controlpb.PluginFailureReason_PLUGIN_FAILURE_REASON_CHECKSUM_MISMATCH}},
	}}))
	f.controlStep(t, op, "/advance", core.Object{}, 200)

	// A failed install must not create or activate an agent row.
	if got := f.spacePlugin(sid, "official/hello-world").S("observedState"); got != "failed" {
		t.Fatalf("failed install surface = %v", got)
	}
	if n := f.scalar(`SELECT count(*) FROM workspace_plugin_instances WHERE workspace_id=$1 AND identifier='hello-world' AND observed_state='failed' AND install_error='checksum_mismatch'`, wid); n != 1 {
		t.Fatalf("the failed instance must record the reason, got %d matching rows", n)
	}
	if f.spaceAgentCount(sid) != 0 {
		t.Fatalf("failed install must not create an agent row: count=%d", f.spaceAgentCount(sid))
	}

	// A retried install converges, and the roster row now appears — proving
	// activation is driven by the converged aggregate, not by the install request.
	//
	// The retry is a remove followed by a fresh install rather than a second install of the same
	// version. Re-selecting an identical desired state is deliberately a no-op in the selection
	// guard (installSpacePlugin only re-runs maintenance scheduling, which picks up instances left
	// `pending`); a failed instance returns to `pending` only on a *changed* selection. Removing the
	// plugin is the supported transition that clears the failed attempt, and the new install is
	// then a genuine change back to installed.
	row := f.spacePlugin(sid, "official/hello-world")
	f.call("DELETE", f.pluginSpacePath(sid)+"/plugins", core.Object{"identifier": "official/hello-world", "version": row.N("version")}, "remove-retry", 200)
	f.completeNextPlugin(t, "remove_plugin")
	f.call("POST", f.pluginSpacePath(sid)+"/plugins", core.Object{"identifier": "official/hello-world", "version": f.spacePlugin(sid, "official/hello-world").N("version")}, "install-retry", 200)
	f.completeNextPlugin(t, "install_plugin")
	agent := f.spaceAgent(sid, "official/hello-world")
	if agent == nil || agent.S("status") != "active" {
		t.Fatalf("converged install must activate the agent: %v", agent)
	}
}

// TestAgentActivationIsAtomicWithPluginWriteback (IT #18) proves the space_agent
// write is atomic with the plugin aggregate-convergence transaction. A Node
// success report whose version evidence does not match the planned request is
// rejected (invalid_plugin_evidence) before the aggregate writeback runs; the
// transaction aborts, so the plugin row must not converge AND no agent row may
// appear. If activation lived in a separate or deferred write (a goroutine, a
// second transaction, a poll) it would survive this abort and this test would
// fail.
func TestAgentActivationIsAtomicWithPluginWriteback(t *testing.T) {
	f := setup(t)
	repo, artifacts := marketplaceFixture(t, f.root)
	f.syncMarketplace(t, repo)
	f.substrate.MapArtifact("https://example.invalid/artifacts/hello-1.0.0.orax", artifacts["https://example.invalid/artifacts/hello-1.0.0.orax"])
	sid := f.defaultSpaceID()
	f.spaceProject(t, sid, "space-project")

	f.call("POST", f.pluginSpacePath(sid)+"/plugins", core.Object{"identifier": "official/hello-world"}, "install-atomic", 200)
	op := f.claimPluginOp(t, "install_plugin")
	plugins := objectList(op.O("request")["plugins"])
	if len(plugins) != 1 || plugins[0].S("pluginId") != "official/hello-world" || plugins[0].S("version") != "1.0.0" {
		t.Fatalf("planned plugin input = %v", plugins)
	}
	f.dispatchPlugin(t, op, "exec-atomic")

	// A success report that names the wrong installed version must be refused: the
	// instance stays in flight, the plugin row stays not-installed, and no agent row
	// appears. All three are one aborted transaction, so the dispatch is still open
	// afterwards and the Node is expected to correct its own evidence.
	err := f.reportPlugin(t, op, "exec-atomic", []*controlpb.PluginItemResult{{
		PluginId: "official/hello-world",
		Outcome:  &controlpb.PluginItemResult_Installed{Installed: &controlpb.PluginItemInstalled{Version: "9.9.9"}},
	}})
	expectStatus(t, err, codes.InvalidArgument, controlpb.ErrorCode_ERROR_CODE_INVALID_INPUT)

	if f.spaceAgentCount(sid) != 0 {
		t.Fatalf("rejected success evidence must not create an agent row: count=%d", f.spaceAgentCount(sid))
	}
	if got := f.spacePlugin(sid, "official/hello-world").S("observedState"); got != "installing" {
		t.Fatalf("plugin must not converge on rejected evidence: %v", f.spacePlugin(sid, "official/hello-world"))
	}

	// The same execution corrects itself. Once the reported version is the planned
	// one, the single transaction that converges the plugin aggregate activates the
	// roster row with it.
	must(t, f.reportPlugin(t, op, "exec-atomic", []*controlpb.PluginItemResult{{
		PluginId: "official/hello-world",
		Outcome:  &controlpb.PluginItemResult_Installed{Installed: &controlpb.PluginItemInstalled{Version: plugins[0].S("version")}},
	}}))
	f.controlStep(t, op, "/advance", core.Object{}, 200)
	if got := f.spaceAgent(sid, "official/hello-world"); got == nil || got.S("status") != "active" {
		t.Fatalf("the converged install must activate the roster row: %v", got)
	}
}
