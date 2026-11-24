package server_test

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/server"
	"github.com/wanglongan587/cloud/v2-server/internal/store"
)

// fakeResumeHost adds resume from failure to the lifecycle fake.
type fakeResumeHost struct {
	fakeRunHost
	preview          server.WorkflowRunResumePreview
	previews, resume int
}

func (f *fakeResumeHost) PreviewRunResume(_ context.Context, scope server.WorkflowRunScope, _ string) (server.WorkflowRunResumePreview, error) {
	f.previews++
	f.lastScope = scope
	return f.preview, f.err
}

func (f *fakeResumeHost) ResumeRun(_ context.Context, scope server.WorkflowRunScope, runID string) (store.WorkflowRun, error) {
	f.resume++
	f.lastScope = scope
	return f.run(runID, store.WorkflowStatusRunning)
}

func TestWorkflowRunResumeRoutesDelegateToBridge(t *testing.T) {
	host := &fakeResumeHost{preview: server.WorkflowRunResumePreview{
		ClearNodeIDs: []string{"b", "c", "iter", "worker"}, RestartContainerNodeIDs: []string{"iter"},
		FailedNodes: []server.WorkflowRunResumeFailedNode{
			{NodeID: "b", NodeRunID: "44444444-4444-4444-8444-444444444444", Status: "failed"},
			{NodeID: "worker", NodeRunID: "55555555-5555-4555-8555-555555555555", Status: "cancelled", ContainerNodeID: "iter"},
		},
	}}
	call := workflowServer(t, func(o *server.Options) { o.WorkflowRuns = host })
	tid := workflowTenant(t, call)
	_, me := call("GET", "/api/v1/me", nil)
	base := "/api/v2/tenants/" + tid + "/workflow-runs/11111111-1111-4111-8111-111111111111"

	status, preview := call("GET", base+"/resume-preview", nil)
	want := map[string]any{
		"resumable": true, "reason": nil, "runningNodeIds": []any{}, "rollback": "keep",
		"clearNodeIds": []any{"b", "c", "iter", "worker"}, "restartContainerNodeIds": []any{"iter"},
		"failedNodes": []any{
			map[string]any{"nodeId": "b", "nodeRunId": "44444444-4444-4444-8444-444444444444", "status": "failed", "containerNodeId": nil},
			map[string]any{"nodeId": "worker", "nodeRunId": "55555555-5555-4555-8555-555555555555", "status": "cancelled", "containerNodeId": "iter"},
		},
	}
	if status != 200 || !reflect.DeepEqual(preview, want) {
		t.Fatalf("preview = %d %v", status, preview)
	}
	host.preview = server.WorkflowRunResumePreview{Reason: "node_running", RunningNodeIDs: []string{"d"}}
	if status, refused := call("GET", base+"/resume-preview", nil); status != 200 || refused["resumable"] != false || refused["reason"] != "node_running" ||
		!reflect.DeepEqual(refused["runningNodeIds"], []any{"d"}) || !reflect.DeepEqual(refused["failedNodes"], []any{}) {
		t.Fatalf("refused preview = %d %v", status, refused)
	}

	status, resumed := call("POST", base+"/resume", nil)
	if status != 200 || resumed["run"].(map[string]any)["status"] != "running" {
		t.Fatalf("resume = %d %v", status, resumed)
	}
	if host.previews != 2 || host.resume != 1 || host.lastScope.TenantID != tid || host.lastScope.UserID != me["id"] {
		t.Fatalf("bridge calls = %+v scope %+v", host, host.lastScope)
	}
	if status, _ := call("POST", "/api/v2/tenants/"+tid+"/workflow-runs/not-a-uuid/resume", nil); status != 404 || host.resume != 1 {
		t.Fatalf("malformed id reached the bridge: %d", status)
	}
}

func TestWorkflowRunResumeRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
		params map[string]any
	}{
		{"refused", &server.WorkflowRunNotResumable{Reason: "node_running", RunningNodeIDs: []string{"d"}}, 409, "workflow_run_not_resumable",
			map[string]any{"reason": "node_running", "runningNodeIds": []any{"d"}}},
		{"refused by status", &server.WorkflowRunNotResumable{Reason: "run_not_ended"}, 409, "workflow_run_not_resumable",
			map[string]any{"reason": "run_not_ended", "runningNodeIds": []any{}}},
		{"run missing", store.ErrWorkflowRunNotFound, 404, "workflow_run_not_found", map[string]any{}},
		{"no completion tool", fmt.Errorf("%w: gone", server.ErrWorkflowCompletionToolUnavailable), 400, "workflow_completion_tool_unavailable", map[string]any{}},
		{"execution unavailable", execution.ErrUnavailable, 503, "execution_unavailable", map[string]any{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host := &fakeResumeHost{fakeRunHost: fakeRunHost{err: tc.err}}
			call := workflowServer(t, func(o *server.Options) { o.WorkflowRuns = host })
			tid := workflowTenant(t, call)
			status, failure := call("POST", "/api/v2/tenants/"+tid+"/workflow-runs/11111111-1111-4111-8111-111111111111/resume", nil)
			if status != tc.status || failure["code"] != tc.code || !reflect.DeepEqual(failure["params"], tc.params) {
				t.Fatalf("resume = %d %v", status, failure)
			}
		})
	}
}

func TestWorkflowRunResumeNeedsAResumingBridge(t *testing.T) {
	for _, tc := range []struct {
		name string
		host server.WorkflowRunHost
	}{{"no bridge", nil}, {"bridge without resume", &fakeRunHost{}}} {
		t.Run(tc.name, func(t *testing.T) {
			call := workflowServer(t, func(o *server.Options) { o.WorkflowRuns = tc.host })
			tid := workflowTenant(t, call)
			base := "/api/v2/tenants/" + tid + "/workflow-runs/11111111-1111-4111-8111-111111111111"
			for _, route := range []struct{ method, path string }{{"GET", base + "/resume-preview"}, {"POST", base + "/resume"}} {
				if status, failure := call(route.method, route.path, nil); status != http.StatusServiceUnavailable || failure["code"] != "workflow_run_host_unavailable" {
					t.Fatalf("%s %s = %d %v", route.method, route.path, status, failure)
				}
			}
		})
	}
}
