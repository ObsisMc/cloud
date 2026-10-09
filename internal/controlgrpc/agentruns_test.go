package controlgrpc

import (
	"encoding/json"
	"testing"

	"github.com/wanglongan587/cloud/internal/controlpb"
	"github.com/wanglongan587/cloud/internal/core"
)

// storedRow renders one claimed command row the way the control core hands it to this layer:
// claimThreadCommands reads it with row_to_json, so the row's own keys are camelCased and the durable
// body is the business layer's canonical object. Testing the in-memory shape instead would prove a
// conversion that never runs.
func storedRow(t *testing.T, kind string, body core.Object) core.Object {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal command body: %v", err)
	}
	var decoded core.Object
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal command body: %v", err)
	}
	return core.Object{
		"commandId": "cmd-1", "runId": "run-1", "executionId": "exec-1",
		"workspaceId": "ws-1", "sandboxInstanceId": "sb-1", "nodeId": "node-1",
		"kind": kind, "body": decoded,
	}
}

// The stored command body is the durable canonical shape the business layer writes and the wire
// message is the proto oneof; the translation between them is this layer's whole job, so it is tested
// directly rather than only through a database round trip. The target's three identities matter as
// much as the command itself: a Controller delivers the command to the Node named here, so a dropped
// field would send a user's turn nowhere.
func TestThreadCommandRendersDurableBody(t *testing.T) {
	turnID := "11111111-1111-1111-1111-111111111111"
	out := threadCommand(storedRow(t, "submit_user_turn", core.Object{
		"turnId":  turnID,
		"content": []core.Object{{"text": "hello"}},
	}))
	if out.GetCommandId() != "cmd-1" || out.GetRunId() != "run-1" || out.GetExecutionId() != "exec-1" {
		t.Fatalf("identity must survive the conversion: %v", out)
	}
	if target := out.GetTarget(); target.GetWorkspaceId() != "ws-1" || target.GetSandboxInstanceId() != "sb-1" || target.GetNodeId() != "node-1" {
		t.Fatalf("target = %v, want the claimed workspace/sandbox/node", target)
	}
	got := out.GetSubmitUserTurn().GetTurn()
	if got.GetTurnId() != turnID {
		t.Fatalf("turn id = %q, want %q", got.GetTurnId(), turnID)
	}
	if blocks := got.GetContent(); len(blocks) != 1 || blocks[0].GetText().GetText() != "hello" {
		t.Fatalf("content blocks = %v, want one text block carrying the user's text", blocks)
	}
	if out.GetEndSession() != nil {
		t.Fatalf("a submit_user_turn must not render an end_session arm")
	}

	end := threadCommand(storedRow(t, "end_session", core.Object{"reason": "idle_timeout"}))
	if end.GetEndSession().GetReason() != controlpb.EndSessionReason_END_SESSION_REASON_IDLE_TIMEOUT {
		t.Fatalf("reason = %v, want idle_timeout", end.GetEndSession().GetReason())
	}
	if end.GetSubmitUserTurn() != nil {
		t.Fatalf("an end_session must not render a submit_user_turn arm")
	}
}

// A ThreadCommandAvailable signal must reach the Controller as its own oneof arm; the default arm of
// response() maps anything unrecognized to a NodeAssignment, which is exactly the mistake this case
// exists to prevent.
func TestWatchResponseCarriesThreadCommandAvailable(t *testing.T) {
	out := response(core.ControlSignal{Kind: core.SignalThreadCommandAvailable, RunID: "run-1"})
	if out.GetThreadCommandAvailable().GetRunId() != "run-1" {
		t.Fatalf("ThreadCommandAvailable.run_id = %q, want run-1", out.GetThreadCommandAvailable().GetRunId())
	}
	if out.GetNodeAssignment() != nil {
		t.Fatalf("a ThreadCommandAvailable must never be rendered as a NodeAssignment")
	}
}
