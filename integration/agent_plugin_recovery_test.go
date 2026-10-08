package integration

import (
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/wanglongan587/cloud/internal/controlpb"
	"github.com/wanglongan587/cloud/internal/core"
)

func TestStartPluginPlanSurvivesSelectionChangesAndExecutionFailures(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "failed", true: "unknown"}[unknown], func(t *testing.T) {
			f := setup(t)
			repo, _ := marketplaceFixture(t, f.root)
			f.syncMarketplace(t, repo)
			sid := f.defaultSpaceID()
			f.call("POST", f.pluginSpacePath(sid)+"/plugins", core.Object{"identifier": "official/hello-world"}, "initial-plugin", 200)
			created := f.spaceProject(t, sid, "start-plugin-recovery")
			wid := created.O("workspace").S("id")
			clones := f.scalar("SELECT count(*) FROM clone_executions WHERE workspace_id=$1", wid)
			f.call("POST", f.path("/workspaces/"+wid+"/stop"), f.lifecycleBody(wid, f.ws(wid).N("version")), "stop-before-plugins", 202)
			f.drain()
			started := f.call("POST", f.path("/workspaces/"+wid+"/start"), f.lifecycleBody(wid, f.ws(wid).N("version")), "start-with-plugins", 202)
			oid := started.O("operation").S("id")
			op := f.stepTo(oid, "plugin")
			before := f.internal("/internal/v1/operations/"+oid+"/snapshot", core.Object{"epoch": op.N("epoch"), "version": op.N("version")}, 200).O("operation")
			f.call("POST", f.pluginSpacePath(sid)+"/plugins", core.Object{"identifier": "official/native-tool"}, "selection-after-snapshot", 200)
			after := f.internal("/internal/v1/operations/"+oid+"/snapshot", core.Object{"epoch": op.N("epoch"), "version": op.N("version")}, 200).O("operation")
			if !proto.Equal(pluginInputProto(before), pluginInputProto(after)) || f.ws(wid).B("admissionOpen") || f.scalar("SELECT count(*) FROM clone_executions WHERE workspace_id=$1", wid) != clones {
				t.Fatal("start recomputed its frozen plugin input, cloned again or opened admission early")
			}
			f.dispatchPlugin(t, before, "start-plugin-attempt-1")
			state, reason := "blocked", "plugin_result_unknown"
			if !unknown {
				_, node, incarnation := f.liveNode(t, wid)
				_, err := f.executions.RecordQueriedResult(asController(f.client.Subject), &controlpb.RecordQueriedResultRequest{SubmissionId: "failed-start-plugin", Epoch: f.controller.Epoch, OperationId: oid, ExecutionId: "start-plugin-attempt-1", Result: &controlpb.ExecutionResult{Node: &controlpb.NodeIdentity{NodeId: node, NodeIncarnationId: incarnation}, Outcome: &controlpb.ExecutionResult_PluginsFailed{PluginsFailed: &controlpb.PluginsFailed{Reason: controlpb.PluginsFailureReason_PLUGINS_FAILURE_REASON_INTERRUPTED}}}})
				must(t, err)
				state, reason = "retry_wait", "plugin_execution_failed"
			}
			deferred := f.controlStep(t, before, "/defer", core.Object{"state": state, "errorCode": reason, "retrySeconds": 1}, 200)
			if deferred.S("state") != state || f.ws(wid).B("admissionOpen") {
				t.Fatal("failure or unknown result opened admission")
			}
			if unknown {
				_, err := f.executions.RecordDispatch(asController(f.client.Subject), &controlpb.RecordDispatchRequest{SubmissionId: "unsafe-plugin-retry", Epoch: f.controller.Epoch, OperationId: oid, ExecutionId: "start-plugin-attempt-2", NodeId: f.liveNodeID(t, wid), Input: pluginInputProto(before)})
				if err == nil {
					t.Fatal("unknown plugin execution was retried")
				}
				return
			}
			_, err := f.store.Pool.Exec("UPDATE operations SET retry_at=clock_timestamp(),version=version+1 WHERE id=$1", oid)
			must(t, err)
			retried := f.claimPluginOp(t, "start")
			f.dispatchPlugin(t, retried, "start-plugin-attempt-2")
			must(t, f.reportPlugin(t, retried, "start-plugin-attempt-2", []*controlpb.PluginItemResult{{PluginId: "official/hello-world", Outcome: &controlpb.PluginItemResult_Installed{Installed: &controlpb.PluginItemInstalled{Version: "1.0.0"}}}}))
			f.controlStep(t, retried, "/advance", core.Object{}, 200)
			if !f.ws(wid).B("admissionOpen") || f.scalar("SELECT count(*) FROM node_executions WHERE operation_id=$1", oid) != 2 {
				t.Fatal("new execution did not settle the original start plan")
			}
		})
	}
}
