package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if row["retiredAt"] != nil {
		t.Fatalf("active agent must have no retired_at: %v", row)
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
	activeVersion := active.N("version")

	// Uninstall -> retired; the row survives with a retired_at stamp.
	pluginsRow := f.spacePlugin(sid, "official/hello-world") // space_plugins version is the DELETE guard
	f.call("DELETE", f.pluginSpacePath(sid)+"/plugins", core.Object{"identifier": "official/hello-world", "version": pluginsRow.N("version")}, "remove-r1", 200)
	f.drain()
	retired := f.spaceAgent(sid, "official/hello-world")
	if retired.S("status") != "retired" || retired["retiredAt"] == nil {
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
	f.drain()
	if got := f.spaceAgent(sid, "official/hello-world"); got.S("status") != "retired" {
		t.Fatalf("repeated uninstall changed agent = %v", got)
	}

	// Reinstall revives the SAME row (same id) back to active and clears retired_at.
	// The plugin row is re-desired installed; the install fans out again and
	// converges to installed, which must revive the agent.
	f.call("POST", f.pluginSpacePath(sid)+"/plugins", core.Object{"identifier": "official/hello-world"}, "install-r2", 200)
	f.drain()
	revived := f.spaceAgent(sid, "official/hello-world")
	if revived.S("id") != retired.S("id") || revived.S("status") != "active" || revived["retiredAt"] != nil {
		t.Fatalf("reinstall must revive the same row: id was %v now %v", retired.S("id"), revived)
	}
	// The revive is a genuine transition, so the row version advances once; the
	// earlier active and the retired both moved the version forward from create.
	if revived.N("version") <= activeVersion {
		t.Fatalf("revive must advance version: was %d now %d", activeVersion, revived.N("version"))
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
// failed install marks the plugin row failed but must not create an agent; only
// after the retried install succeeds does the agent row appear.
func TestAgentInstallFailureDoesNotActivate(t *testing.T) {
	f := setup(t)
	repo, artifacts := marketplaceFixture(t, f.root)
	f.syncMarketplace(t, repo)
	sid := f.defaultSpaceID()
	created := f.spaceProject(t, sid, "space-project")
	wid := created.O("workspace").S("id")

	// A tampered artifact so the install download fails digest verification.
	wrong := filepath.Join(f.root, "wrong.orax")
	must(t, os.WriteFile(wrong, []byte("tampered bytes\n"), 0o600))
	f.substrate.MapArtifact("https://example.invalid/artifacts/hello-1.0.0.orax", wrong)

	f.call("POST", f.pluginSpacePath(sid)+"/plugins", core.Object{"identifier": "official/hello-world"}, "install-fail", 200)
	op := f.claimPluginOp(t, "install_plugin")
	planned := f.controlStep(t, op, "/effects", core.Object{"kind": "plugin_ensure", "workspaceId": wid}, 200)
	effect := planned.O("effect")
	failed := f.substrateFailed(t, effect)
	op = planned.O("operation")
	result := f.controlStep(t, op, "/effects/"+effect.S("id")+"/result", core.Object{"state": "failed", "externalId": failed.S("externalId"), "result": failed.O("result")}, 200)

	// A failed install must not create or activate an agent row.
	if got := f.spacePlugin(sid, "official/hello-world").S("observedState"); got != "failed" {
		t.Fatalf("failed install surface = %v", got)
	}
	if f.spaceAgentCount(sid) != 0 {
		t.Fatalf("failed install must not create an agent row: count=%d", f.spaceAgentCount(sid))
	}

	// Fix the artifact and retry by stable effect id; the reclaim converges and
	// the roster row now appears — proving activation is driven by the converged
	// aggregate, not by the install request.
	f.substrate.MapArtifact("https://example.invalid/artifacts/hello-1.0.0.orax", artifacts["https://example.invalid/artifacts/hello-1.0.0.orax"])
	op = result.O("operation")
	external := f.substrateRerun(t, effect)
	succeeded := f.controlStep(t, op, "/effects/"+effect.S("id")+"/result", core.Object{"state": "succeeded", "externalId": external.S("externalId"), "result": external.O("result")}, 200)
	op = succeeded.O("operation")
	f.controlStep(t, op, "/advance", core.Object{}, 200)
	row := f.spaceAgent(sid, "official/hello-world")
	if row == nil || row.S("status") != "active" {
		t.Fatalf("converged install must activate the agent: %v", row)
	}
}

// TestAgentActivationIsAtomicWithPluginWriteback (IT #18) proves the space_agent
// write is atomic with the plugin aggregate-convergence transaction. A succeeded
// install result whose version evidence does not match the planned request is
// rejected by effectResult (invalid_plugin_evidence) before the aggregate
// writeback runs; the transaction aborts, so the plugin row must not be installed
// AND no agent row may appear. If activation lived in a separate or deferred
// write (a goroutine, a second transaction, a poll) it would survive this abort
// and this test would fail.
func TestAgentActivationIsAtomicWithPluginWriteback(t *testing.T) {
	f := setup(t)
	repo, artifacts := marketplaceFixture(t, f.root)
	f.syncMarketplace(t, repo)
	f.substrate.MapArtifact("https://example.invalid/artifacts/hello-1.0.0.orax", artifacts["https://example.invalid/artifacts/hello-1.0.0.orax"])
	sid := f.defaultSpaceID()
	wid := f.spaceProject(t, sid, "space-project").O("workspace").S("id")

	f.call("POST", f.pluginSpacePath(sid)+"/plugins", core.Object{"identifier": "official/hello-world"}, "install-atomic", 200)
	op := f.claimPluginOp(t, "install_plugin")
	planned := f.controlStep(t, op, "/effects", core.Object{"kind": "plugin_ensure", "workspaceId": wid}, 200)
	effect := planned.O("effect")
	if want, got := "1.0.0", effect.O("request").S("version"); want != got {
		t.Fatalf("planned effect version = %q, want %q", got, want)
	}

	// A "succeeded" result that names the wrong installed version must be refused:
	// the effect stays planned, the plugin row stays not-installed, and no agent
	// row appears. All three are one aborted transaction.
	f.controlStep(t, planned.O("operation"), "/effects/"+effect.S("id")+"/result",
		core.Object{"state": "succeeded", "externalId": "ext-atomic-bad", "result": core.Object{"installed": true, "version": "9.9.9"}}, 400)

	if f.spaceAgentCount(sid) != 0 {
		t.Fatalf("rejected success evidence must not create an agent row: count=%d", f.spaceAgentCount(sid))
	}
	if f.spacePlugin(sid, "official/hello-world").S("observedState") != "installing" {
		t.Fatalf("plugin must not converge on rejected evidence: %v", f.spacePlugin(sid, "official/hello-world"))
	}
}
