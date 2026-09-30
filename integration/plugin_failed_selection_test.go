package integration

import (
	"testing"

	"github.com/wanglongan587/cloud/internal/controlpb"
	"github.com/wanglongan587/cloud/internal/core"
)

// A failed item finishes its accepted attempt; it cannot strand a later removal behind an old binding.
func TestPluginFailedResultReleasesMaintenanceForNewSelection(t *testing.T) {
	for _, changeBeforeResult := range []bool{false, true} {
		t.Run(map[bool]string{false: "selection_after_failure", true: "selection_before_failure"}[changeBeforeResult], func(t *testing.T) {
			f := setup(t)
			repo, _ := marketplaceFixture(t, f.root)
			f.syncMarketplace(t, repo)
			sid := f.defaultSpaceID()
			wid := f.spaceProject(t, sid, "failed-plugin-selection").O("workspace").S("id")
			f.call("POST", f.pluginSpacePath(sid)+"/plugins", core.Object{"identifier": "official/hello-world"}, "install-before-failure", 200)
			op := f.claimPluginOp(t, "install_plugin")
			f.dispatchPlugin(t, op, "failed-plugin-selection")
			remove := func() {
				f.call("DELETE", f.pluginSpacePath(sid)+"/plugins", core.Object{"identifier": "official/hello-world", "version": f.spacePlugin(sid, "official/hello-world").N("version")}, "remove-after-selection", 200)
			}
			if changeBeforeResult {
				remove()
			}
			must(t, f.reportPlugin(t, op, "failed-plugin-selection", []*controlpb.PluginItemResult{{PluginId: "official/hello-world", Outcome: &controlpb.PluginItemResult_Failed{Failed: &controlpb.PluginItemFailed{Reason: controlpb.PluginFailureReason_PLUGIN_FAILURE_REASON_CHECKSUM_MISMATCH}}}}))
			state := "failed"
			if changeBeforeResult {
				state = "pending"
			}
			if f.scalar("SELECT count(*) FROM workspace_plugin_instances WHERE workspace_id=$1 AND identifier='hello-world' AND observed_state=$2 AND maintenance_operation_id IS NULL", wid, state) != 1 {
				t.Fatal("terminal item retained an old maintenance binding or lost the latest selection")
			}
			if changeBeforeResult && f.scalar("SELECT count(*) FROM workspace_plugin_instances WHERE workspace_id=$1 AND identifier='hello-world' AND install_error IS NOT NULL", wid) != 0 {
				t.Fatal("failure from an older selection leaked into the current intent")
			}
			f.controlStep(t, op, "/advance", core.Object{}, 200)
			if !changeBeforeResult {
				remove()
			}
			f.completeNextPlugin(t, "remove_plugin")
			if f.spacePlugin(sid, "official/hello-world").S("observedState") != "removed" || f.scalar("SELECT count(*) FROM workspace_plugin_instances WHERE workspace_id=$1 AND identifier='hello-world' AND observed_state='removed' AND maintenance_operation_id IS NULL AND install_error IS NULL", wid) != 1 {
				t.Fatal("new removal did not converge after the failed install")
			}
		})
	}
}
