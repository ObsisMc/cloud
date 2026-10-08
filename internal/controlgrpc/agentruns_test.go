package controlgrpc

import (
	"encoding/json"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/wanglongan587/cloud/internal/controlpb"
	"github.com/wanglongan587/cloud/internal/core"
)

// storedRow renders a command row the way the control core hands it to this layer: the body was
// written as jsonb and read back through row_to_json, so nested values are map[string]any and the
// row's own keys are camelCased. Testing the in-memory shape instead would prove a conversion that
// never runs.
func storedRow(t *testing.T, command core.Object) core.Object {
	t.Helper()
	raw, err := json.Marshal(command.O("body"))
	if err != nil {
		t.Fatalf("marshal command body: %v", err)
	}
	var body core.Object
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal command body: %v", err)
	}
	return core.Object{
		"id": "cmd-1", "runId": "run-1", "executionId": "exec-1", "nodeId": "node-1",
		"kind": command.S("kind"), "body": body,
	}
}

// rawRow builds a row from a literal jsonb body, which is how a corrupted or hand-written row
// reaches this layer.
func rawRow(t *testing.T, kind, body string) core.Object {
	t.Helper()
	var decoded core.Object
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("unmarshal %s body: %v", kind, err)
	}
	return core.Object{"id": "cmd-1", "kind": kind, "body": decoded}
}

// The stored command body is the durable canonical shape and the wire message is the proto oneof;
// the translation is this layer's whole job, so it is tested directly rather than only through a
// database round trip.
func TestThreadCommandRendersDurableBody(t *testing.T) {
	turnID := "11111111-1111-1111-1111-111111111111"
	out, err := threadCommand(storedRow(t, core.SubmitUserTurnCommand(turnID, []core.Object{{"type": "text", "text": "hello"}})))
	if err != nil {
		t.Fatalf("submit_user_turn must render: %v", err)
	}
	if out.GetCommandId() != "cmd-1" || out.GetRunId() != "run-1" || out.GetExecutionId() != "exec-1" {
		t.Fatalf("identity must survive the conversion: %v", out)
	}
	if out.GetTarget().GetNodeId() != "node-1" {
		t.Fatalf("target node = %q, want node-1", out.GetTarget().GetNodeId())
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

	end, err := threadCommand(storedRow(t, core.Object{"kind": "end_session", "body": core.Object{"reason": "idle_timeout"}}))
	if err != nil {
		t.Fatalf("end_session must render: %v", err)
	}
	if end.GetEndSession().GetReason() != controlpb.EndSessionReason_END_SESSION_REASON_IDLE_TIMEOUT {
		t.Fatalf("reason = %v, want idle_timeout", end.GetEndSession().GetReason())
	}
	if end.GetSubmitUserTurn() != nil {
		t.Fatalf("an end_session must not render a submit_user_turn arm")
	}

	// A body that no longer parses is Cloud's own corrupted state, reported rather than silently
	// dropped — dropping would lose user content or invent an end reason.
	for name, row := range map[string]core.Object{
		"unknown kind":       rawRow(t, "delete_everything", `{"reason":"user_ended"}`),
		"unknown reason":     rawRow(t, "end_session", `{"reason":"because"}`),
		"non-text block":     rawRow(t, "submit_user_turn", `{"turn":{"turn_id":"t","content":[{"type":"image","text":"x"}]}}`),
		"empty content":      rawRow(t, "submit_user_turn", `{"turn":{"turn_id":"t","content":[]}}`),
		"content not a list": rawRow(t, "submit_user_turn", `{"turn":{"turn_id":"t","content":{"type":"text"}}}`),
		"missing turn":       rawRow(t, "submit_user_turn", `{}`),
	} {
		if _, err := threadCommand(row); status.Code(err) != codes.Internal {
			t.Fatalf("%s: want Internal, got %v", name, err)
		}
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
