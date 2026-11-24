package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/server"
	"github.com/wanglongan587/cloud/v2-server/internal/store"
)

type fakeRunHost struct {
	started, cancelled, updated, completed int
	lastScope                              server.WorkflowRunScope
	lastInput                              *string
	lastVariables                          map[string]json.RawMessage
	lastNodeID                             string
	lastCompletion                         server.WorkflowNodeCompletion
	err                                    error
}

func (f *fakeRunHost) StartRun(_ context.Context, scope server.WorkflowRunScope, runID string) (store.WorkflowRun, error) {
	f.started++
	f.lastScope = scope
	return f.run(runID, store.WorkflowStatusRunning)
}

func (f *fakeRunHost) CancelRun(_ context.Context, scope server.WorkflowRunScope, runID string) (store.WorkflowRun, error) {
	f.cancelled++
	f.lastScope = scope
	return f.run(runID, store.WorkflowStatusCancelled)
}

func (f *fakeRunHost) UpdateRunInput(_ context.Context, scope server.WorkflowRunScope, runID string, input *string, variables map[string]json.RawMessage) (store.WorkflowRun, error) {
	f.updated++
	f.lastScope = scope
	f.lastInput = input
	f.lastVariables = variables
	return f.run(runID, store.WorkflowStatusPending)
}

func (f *fakeRunHost) CompleteNode(_ context.Context, scope server.WorkflowRunScope, runID, nodeID string, completion server.WorkflowNodeCompletion) (store.WorkflowRun, error) {
	f.completed++
	f.lastScope = scope
	f.lastNodeID = nodeID
	f.lastCompletion = completion
	return f.run(runID, store.WorkflowStatusRunning)
}

func (f *fakeRunHost) run(runID, status string) (store.WorkflowRun, error) {
	if f.err != nil {
		return store.WorkflowRun{}, f.err
	}
	now := time.Date(2026, 10, 31, 9, 30, 0, 0, time.UTC)
	if runID == "" {
		runID = "11111111-1111-4111-8111-111111111111"
	}
	return store.WorkflowRun{
		ID: runID, TenantID: "tenant", WorkflowID: "22222222-2222-4222-8222-222222222222",
		SnapshotID: "33333333-3333-4333-8333-333333333333", ProjectID: "project", Name: "运行",
		Status: status, State: []byte(`{"current_nodes":[]}`), CreatedAt: now, UpdatedAt: now,
	}, nil
}

func lifecycleServer(t *testing.T, host server.WorkflowRunHost) (workflowCall, *fakeRunHost) {
	t.Helper()
	fake, _ := host.(*fakeRunHost)
	call := workflowServer(t, func(o *server.Options) { o.WorkflowRuns = host })
	return call, fake
}

func TestWorkflowRunLifecycleRoutesDelegateToBridge(t *testing.T) {
	host := &fakeRunHost{}
	call, _ := lifecycleServer(t, host)
	tid := workflowTenant(t, call)
	_, me := call("GET", "/api/v1/me", nil)
	runID := "11111111-1111-4111-8111-111111111111"
	base := "/api/v2/tenants/" + tid + "/workflow-runs/" + runID

	status, started := call("POST", base+"/start", nil)
	if status != 200 || started["run"].(map[string]any)["status"] != "running" {
		t.Fatalf("start = %d %v", status, started)
	}
	if host.started != 1 || host.lastScope.TenantID != tid || host.lastScope.UserID != me["id"] {
		t.Fatalf("start bridge call = %+v scope=%+v", host, host.lastScope)
	}

	if status, _ := call("POST", base+"/cancel", nil); status != 200 {
		t.Fatalf("cancel = %d", status)
	}
	if host.cancelled != 1 || host.lastScope.TenantID != tid {
		t.Fatalf("cancel bridge call = %+v", host)
	}

	input := "补充输入"
	status, updated := call("PATCH", base+"/input", map[string]any{"input": input, "variables": map[string]any{"count": 3}})
	if status != 200 || updated["run"] == nil {
		t.Fatalf("input = %d %v", status, updated)
	}
	if host.updated != 1 || host.lastInput == nil || *host.lastInput != input || string(host.lastVariables["count"]) != "3" {
		t.Fatalf("input bridge call = %+v input=%v variables=%v", host, host.lastInput, host.lastVariables)
	}

	status, completed := call("POST", base+"/nodes/agent-1/complete", map[string]any{"force": true, "requester": "human"})
	if status != 200 || completed["run"] == nil {
		t.Fatalf("complete = %d %v", status, completed)
	}
	if host.completed != 1 || host.lastNodeID != "agent-1" || host.lastCompletion != (server.WorkflowNodeCompletion{Force: true}) {
		t.Fatalf("complete bridge call = %+v", host)
	}
	status, completed = call("POST", base+"/nodes/agent-1/complete", map[string]any{"force": false, "confirmInitialTurn": true})
	if status != 200 || completed["run"] == nil || host.lastCompletion != (server.WorkflowNodeCompletion{ConfirmInitialTurn: true}) {
		t.Fatalf("confirm = %d %v, bridge %+v", status, completed, host.lastCompletion)
	}

	status, completed = call("POST", base+"/nodes/agent-1/complete", map[string]any{"force": false, "confirmInitialTurn": true, "confirmAgentCompletion": map[string]any{"signalId": "signal-1"}})
	if status != 200 || completed["run"] == nil || host.lastCompletion != (server.WorkflowNodeCompletion{ConfirmInitialTurn: true, ConfirmSignalID: "signal-1"}) {
		t.Fatalf("confirm a report = %d %v, bridge %+v", status, completed, host.lastCompletion)
	}
	if status, failure := call("POST", base+"/nodes/agent-1/complete", map[string]any{"force": false, "confirmAgentCompletion": map[string]any{"signalId": ""}}); status != 400 || failure["code"] != "invalid_json" {
		t.Fatalf("a confirmation naming no report = %d %v", status, failure)
	}

	// A malformed id never reaches the bridge.
	if status, _ := call("POST", "/api/v2/tenants/"+tid+"/workflow-runs/not-a-uuid/start", nil); status != 404 {
		t.Fatalf("malformed id = %d", status)
	}
	// Only the human requester is accepted at this phase.
	status, failure := call("POST", base+"/nodes/agent-1/complete", map[string]any{"force": false, "requester": "agent"})
	if status != 400 || failure["code"] != "invalid_json" {
		t.Fatalf("unknown requester = %d %v", status, failure)
	}
}

func TestWorkflowRunLifecycleRefusals(t *testing.T) {
	runBase := func(tid string) string {
		return "/api/v2/tenants/" + tid + "/workflow-runs/11111111-1111-4111-8111-111111111111"
	}
	for _, tc := range []struct {
		name   string
		err    error
		code   string
		want   int
		method string
		suffix string
		body   any
	}{
		{"no completion tool", fmt.Errorf("%w: gone", server.ErrWorkflowCompletionToolUnavailable), "workflow_completion_tool_unavailable", 400, "POST", "/start", nil},
		{"execution unavailable", execution.ErrUnavailable, "execution_unavailable", 503, "POST", "/start", nil},
		{"not startable", server.ErrWorkflowRunNotStartable, "workflow_run_start_failed", 400, "POST", "/start", nil},
		{"not editable", server.ErrWorkflowRunNotEditable, "workflow_run_not_editable", 409, "PATCH", "/input", map[string]any{"input": "x"}},
		{"node not parked", server.ErrWorkflowNodeNotAwaitingInput, "workflow_node_not_awaiting_input", 400, "POST", "/nodes/agent-1/complete", map[string]any{"force": false}},
		{"node missing", server.ErrWorkflowNodeNotFound, "workflow_node_not_found", 404, "POST", "/nodes/agent-1/complete", map[string]any{"force": false}},
		{"run missing", store.ErrWorkflowRunNotFound, "workflow_run_not_found", 404, "POST", "/start", nil},
		{"stop unconfirmed", server.ErrWorkflowRunStopUnconfirmed, "workflow_run_stop_unconfirmed", 503, "POST", "/cancel", nil},
		{"state conflict", store.ErrWorkflowRunState, "workflow_run_state_conflict", 409, "POST", "/start", nil},
		{"role missing", server.ErrWorkflowRoleNotFound, "workflow_role_not_found", 400, "POST", "/start", nil},
		{"skill missing", &server.WorkflowSkillNotFound{NodeID: "reviewer", NodeTitle: "代码审查", SkillID: "code_review"}, "workflow_skill_not_found", 400, "POST", "/start", nil},
		{"input invalid", &server.WorkflowRunInputInvalid{Variable: "count", Reason: "must be a number"}, "workflow_run_input_invalid", 400, "PATCH", "/input", map[string]any{"input": "x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host := &fakeRunHost{err: tc.err}
			call, _ := lifecycleServer(t, host)
			tid := workflowTenant(t, call)
			status, failure := call(tc.method, runBase(tid)+tc.suffix, tc.body)
			if status != tc.want || failure["code"] != tc.code {
				t.Fatalf("refusal = %d %v", status, failure)
			}
			if tc.code == "workflow_run_input_invalid" {
				params := failure["params"].(map[string]any)
				if params["variable"] != "count" || params["reason"] != "must be a number" {
					t.Fatalf("params = %v", params)
				}
			}
			if tc.code == "workflow_skill_not_found" {
				params := failure["params"].(map[string]any)
				if params["nodeId"] != "reviewer" || params["nodeTitle"] != "代码审查" || params["skillId"] != "code_review" {
					t.Fatalf("skill params = %v", params)
				}
			}
		})
	}
}

func TestWorkflowRunLifecycleNeedsBridge(t *testing.T) {
	call := workflowServer(t, nil)
	tid := workflowTenant(t, call)
	base := "/api/v2/tenants/" + tid + "/workflow-runs/11111111-1111-4111-8111-111111111111"
	for _, route := range []struct{ method, path string }{
		{"POST", base + "/start"},
		{"POST", base + "/cancel"},
		{"PATCH", base + "/input"},
		{"POST", base + "/nodes/agent-1/complete"},
	} {
		status, failure := call(route.method, route.path, map[string]any{"input": nil, "force": false})
		if status != http.StatusServiceUnavailable || failure["code"] != "workflow_run_host_unavailable" {
			t.Fatalf("%s %s = %d %v", route.method, route.path, status, failure)
		}
	}
}

// fakeTurnHost adds the interactive node session to the lifecycle fake.
type fakeTurnHost struct {
	fakeRunHost
	turns, stops int
	lastContent  string
}

func (f *fakeTurnHost) SendNodeTurn(_ context.Context, scope server.WorkflowRunScope, runID, nodeID, content string) (server.WorkflowNodeTurnAccepted, error) {
	f.turns++
	f.lastScope, f.lastNodeID, f.lastContent = scope, nodeID, content
	run, err := f.run(runID, store.WorkflowStatusRunning)
	// Not the call count, so the route is seen to pass the bridge's number through.
	return server.WorkflowNodeTurnAccepted{Run: run, Sent: 7}, err
}

func (f *fakeTurnHost) StopNodeTurn(_ context.Context, scope server.WorkflowRunScope, runID, nodeID string) (store.WorkflowRun, error) {
	f.stops++
	f.lastScope, f.lastNodeID = scope, nodeID
	return f.run(runID, store.WorkflowStatusRunning)
}

func TestWorkflowNodeTurnRoutesDelegateToBridge(t *testing.T) {
	host := &fakeTurnHost{}
	call := workflowServer(t, func(o *server.Options) { o.WorkflowRuns = host })
	tid := workflowTenant(t, call)
	_, me := call("GET", "/api/v1/me", nil)
	base := "/api/v2/tenants/" + tid + "/workflow-runs/11111111-1111-4111-8111-111111111111/nodes/agent-1"

	status, sent := call("POST", base+"/turns", map[string]any{"content": "再改一版"})
	// The response names which of the starter's messages this one is, as the bridge numbered it.
	if status != 200 || sent["run"] == nil || sent["sent"] != float64(7) {
		t.Fatalf("turn = %d %v", status, sent)
	}
	if host.turns != 1 || host.lastNodeID != "agent-1" || host.lastContent != "再改一版" || host.lastScope.UserID != me["id"] || host.lastScope.TenantID != tid {
		t.Fatalf("turn bridge call = %+v", host)
	}
	if status, _ := call("POST", base+"/stop", nil); status != 200 || host.stops != 1 {
		t.Fatalf("stop = %d, stops = %d", status, host.stops)
	}
	for _, body := range []any{map[string]any{"content": "  "}, map[string]any{"content": strings.Repeat("长", 16001)}} {
		if status, failure := call("POST", base+"/turns", body); status != 400 || failure["code"] != "invalid_content" {
			t.Fatalf("bad content = %d %v", status, failure)
		}
	}
	if status, failure := call("POST", base+"/turns", map[string]any{"content": "x", "role": "agent"}); status != 400 || failure["code"] != "invalid_json" {
		t.Fatalf("unknown field = %d %v", status, failure)
	}
	if host.turns != 1 {
		t.Fatalf("a refused request reached the bridge: %d turns", host.turns)
	}
}

func TestWorkflowNodeTurnRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		code   string
		want   int
		suffix string
		body   any
	}{
		{"turn by another member", server.ErrWorkflowRunOwned, "run_owned", 409, "/turns", map[string]any{"content": "x"}},
		{"stop by another member", server.ErrWorkflowRunOwned, "run_owned", 409, "/stop", nil},
		{"complete by another member", server.ErrWorkflowRunOwned, "run_owned", 409, "/complete", map[string]any{"force": false}},
		{"session ended", server.ErrWorkflowNodeSessionEnded, "workflow_node_session_ended", 409, "/turns", map[string]any{"content": "x"}},
		{"turn while busy", server.ErrWorkflowNodeNotAwaitingInput, "workflow_node_not_awaiting_input", 400, "/turns", map[string]any{"content": "x"}},
		{"nothing to stop", server.ErrWorkflowNodeTurnNotRunning, "workflow_node_turn_not_running", 409, "/stop", nil},
		{"stop unconfirmed", server.ErrWorkflowRunStopUnconfirmed, "workflow_run_stop_unconfirmed", 503, "/stop", nil},
		{"incomplete", &server.WorkflowNodeIncomplete{Blockers: []server.WorkflowNodeBlocker{{Kind: "turn_unconfirmed"}, {Kind: "file_missing", Path: "plan.md"}}}, "workflow_node_incomplete", 409, "/complete", map[string]any{"force": false}},
		{"turn changed", &server.WorkflowNodeTurnChanged{NodeID: "agent-1", Expected: &server.WorkflowNodeTurnKey{UserSeq: 0, EndedSeq: 1}, Actual: &server.WorkflowNodeTurnKey{UserSeq: 1, EndedSeq: 2}}, "workflow_node_turn_changed", 409, "/complete", map[string]any{"force": true}},
		{"signal changed", &server.WorkflowNodeSignalChanged{NodeID: "agent-1", Expected: "a", Actual: "b"}, "workflow_node_signal_changed", 409, "/complete", map[string]any{"force": true}},
		{"output changed", &server.WorkflowNodeOutputChanged{NodeID: "agent-1", Expected: "sha256:a", Actual: "sha256:b"}, "workflow_node_output_changed", 409, "/complete", map[string]any{"force": true}},
		{"confirm with force", server.ErrWorkflowNodeCompletionInvalid, "invalid_request", 400, "/complete", map[string]any{"force": true, "confirmInitialTurn": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host := &fakeTurnHost{fakeRunHost: fakeRunHost{err: tc.err}}
			call := workflowServer(t, func(o *server.Options) { o.WorkflowRuns = host })
			tid := workflowTenant(t, call)
			path := "/api/v2/tenants/" + tid + "/workflow-runs/11111111-1111-4111-8111-111111111111/nodes/agent-1" + tc.suffix
			status, failure := call("POST", path, tc.body)
			if status != tc.want || failure["code"] != tc.code {
				t.Fatalf("refusal = %d %v", status, failure)
			}
			if tc.code == "workflow_node_incomplete" {
				blockers, _ := failure["params"].(map[string]any)["blockers"].([]any)
				if len(blockers) != 2 || blockers[0].(map[string]any)["kind"] != "turn_unconfirmed" || blockers[1].(map[string]any)["path"] != "plan.md" {
					t.Fatalf("params = %v", failure["params"])
				}
			}
			if tc.code == "workflow_node_turn_changed" {
				if params := failure["params"].(map[string]any); params["expected"].(map[string]any)["endedSeq"] != float64(1) || params["actual"].(map[string]any)["userSeq"] != float64(1) {
					t.Fatalf("params = %v", failure["params"])
				}
			}
		})
	}
}

func TestWorkflowNodeTurnRoutesNeedATurnHost(t *testing.T) {
	call := workflowServer(t, func(o *server.Options) { o.WorkflowRuns = &fakeRunHost{} })
	tid := workflowTenant(t, call)
	base := "/api/v2/tenants/" + tid + "/workflow-runs/11111111-1111-4111-8111-111111111111/nodes/agent-1"
	for _, suffix := range []string{"/turns", "/stop"} {
		if status, failure := call("POST", base+suffix, map[string]any{"content": "x"}); status != 503 || failure["code"] != "workflow_run_host_unavailable" {
			t.Fatalf("%s = %d %v", suffix, status, failure)
		}
	}
}
