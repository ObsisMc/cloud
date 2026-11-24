package server_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/wanglongan587/cloud/v2-server/internal/server"
	"github.com/wanglongan587/cloud/v2-server/internal/store"
	"github.com/wanglongan587/cloud/v2-server/internal/taskworkflow"
)

type workflowTaskHTTPPort struct{ finishingRuns }

func (workflowTaskHTTPPort) Prepare(context.Context, string, string, string, *string, map[string]json.RawMessage) error {
	return nil
}

func TestTaskWorkflowsHTTP(t *testing.T) {
	f := newSquadServer(t)
	host := taskworkflow.New(f.store, workflowTaskHTTPPort{finishingRuns{store: f.store, output: `{}`}})
	t.Cleanup(host.Close)
	f.srv.Close()
	f.srv = httptest.NewServer(server.New(f.store, server.Options{SessionTTL: time.Hour, TaskWorkflows: host}))
	t.Cleanup(f.srv.Close)
	c := f.taskClient()
	c.login()
	tid := f.tenant(t)
	f.project(t)
	w, _, err := f.store.CreateWorkflow(f.ctx, tid, "Handling", `{"nodes":[{"id":"start","data":{"kind":"start","inputVariables":[{"name":"branch","valueType":"string"}]}}],"edges":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.PublishWorkflow(f.ctx, tid, w.ID, "v1", nil); err != nil {
		t.Fatal(err)
	}
	base := "/api/v2/tenants/" + tid + "/tasks"
	status, task := c.do("POST", base, map[string]any{"title": "Task", "projectId": taskProjectID})
	if status != 201 {
		t.Fatalf("create=%d %v", status, task)
	}
	path := base + "/" + task["id"].(string) + "/workflow-runs"
	doc, err := openapi3.NewLoader().LoadFromFile("../../task-workflows.openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(f.ctx); err != nil {
		t.Fatal(err)
	}
	routes, err := legacy.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}
	original := c.routes
	c.routes = routes
	key := map[string]string{"Idempotency-Key": "handle"}
	body := map[string]any{"workflowId": w.ID, "input": "Do this task", "values": map[string]any{"branch": "main"}}
	if status, got := c.do("POST", path, body); status != 400 || got["code"] != "idempotency_key_required" {
		t.Fatalf("no key=%d %v", status, got)
	}
	if status, got := c.doHeaders("POST", path, body, key); status != 409 || got["code"] != "not_agent_task" {
		t.Fatalf("unassigned=%d %v", status, got)
	}
	c.routes = original
	status, task = c.do("PUT", base+"/"+task["id"].(string)+"/assignment", map[string]any{"version": task["version"], "assignee": map[string]any{"type": "human", "id": task["createdBy"]}})
	if status != 200 {
		t.Fatalf("assign=%d %v", status, task)
	}
	c.routes = routes
	status, started := c.doHeaders("POST", path, body, key)
	if status != 201 || started["workflowRunId"] == "" {
		t.Fatalf("start=%d %v", status, started)
	}
	status, retry := c.doHeaders("POST", path, body, key)
	if status != 201 || retry["workflowRunId"] != started["workflowRunId"] {
		t.Fatalf("retry=%d %v", status, retry)
	}
	status, list := c.do("GET", path, nil)
	items, _ := list["items"].([]any)
	if status != 200 || len(items) != 1 || items[0].(map[string]any)["kind"] != "handle" || items[0].(map[string]any)["status"] != "succeeded" {
		t.Fatalf("list=%d %v", status, list)
	}
	c.routes = original
	status, task = c.do("GET", base+"/"+task["id"].(string), nil)
	if status != 200 || task["status"] != store.TaskReview {
		t.Fatalf("settled task=%d %v", status, task)
	}
	c.routes = routes
	invalid := map[string]any{"workflowId": w.ID, "projectId": taskProjectID}
	if status, got := c.doHeaders("POST", path, invalid, key); status != 400 {
		t.Fatalf("scope injection=%d %v", status, got)
	}
}

// conflictWorkflowPort injects an admission-following failure through the real HTTP route.
type conflictWorkflowPort struct{ workflowTaskHTTPPort }

func (conflictWorkflowPort) Prepare(context.Context, string, string, string, *string, map[string]json.RawMessage) error {
	return store.ErrConflict
}

func TestTaskWorkflowChoiceErrorsHTTP(t *testing.T) {
	f := newSquadServer(t)
	host := taskworkflow.New(f.store, workflowTaskHTTPPort{finishingRuns{store: f.store, output: `{}`}})
	t.Cleanup(host.Close)
	f.srv.Close()
	f.srv = httptest.NewServer(server.New(f.store, server.Options{SessionTTL: time.Hour, TaskWorkflows: host}))
	t.Cleanup(f.srv.Close)
	c := f.taskClient()
	c.login()
	tid := f.tenant(t)
	f.project(t)
	graph := `{"nodes":[{"id":"start","data":{"kind":"start"}}],"edges":[]}`
	w, draft, err := f.store.CreateWorkflow(f.ctx, tid, "Handling", graph)
	if err != nil {
		t.Fatal(err)
	}
	unpublished, _, err := f.store.CreateWorkflow(f.ctx, tid, "Unpublished", graph)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := f.store.CreateWorkflow(f.ctx, tid, "Other", graph)
	if err != nil {
		t.Fatal(err)
	}
	otherSnap, err := f.store.PublishWorkflow(f.ctx, tid, other.ID, "v1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.PublishWorkflow(f.ctx, tid, w.ID, "v1", nil); err != nil {
		t.Fatal(err)
	}
	base := "/api/v2/tenants/" + tid + "/tasks"
	_, task := c.do("POST", base, map[string]any{"title": "Task", "projectId": taskProjectID})
	_, task = c.do("PUT", base+"/"+task["id"].(string)+"/assignment", map[string]any{"version": task["version"], "assignee": map[string]any{"type": "human", "id": task["createdBy"]}})
	path := base + "/" + task["id"].(string) + "/workflow-runs"
	doc, err := openapi3.NewLoader().LoadFromFile("../../task-workflows.openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(f.ctx); err != nil {
		t.Fatal(err)
	}
	original := c.routes
	routes, err := legacy.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}
	c.routes = routes
	for _, tc := range []struct {
		name, wid, snapshot, code string
		status                    int
	}{
		{"draft", w.ID, draft.ID, "workflow_run_cannot_use_draft_snapshot", 400},
		{"unpublished", unpublished.ID, "", "workflow_no_published_snapshot", 400},
		{"foreign snapshot", w.ID, otherSnap.ID, "workflow_snapshot_not_found", 404},
		{"missing snapshot", w.ID, "00000000-0000-4000-8000-000000000099", "workflow_snapshot_not_found", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{"workflowId": tc.wid}
			if tc.snapshot != "" {
				body["snapshotId"] = tc.snapshot
			}
			status, got := c.doHeaders("POST", path, body, map[string]string{"Idempotency-Key": tc.name})
			if status != tc.status || got["code"] != tc.code {
				t.Fatalf("choice=%d %v want %d %s", status, got, tc.status, tc.code)
			}
		})
	}
	bad, _, err := f.store.CreateWorkflow(f.ctx, tid, "Invalid graph", `{"nodes":[],"edges":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.PublishWorkflow(f.ctx, tid, bad.ID, "v1", nil); err != nil {
		t.Fatal(err)
	}
	if status, got := c.doHeaders("POST", path, map[string]any{"workflowId": bad.ID}, map[string]string{"Idempotency-Key": "graph"}); status != 400 || got["code"] != "workflow_run_start_failed" {
		t.Fatalf("invalid graph=%d %v", status, got)
	}
	if _, err := f.db.Exec(f.ctx, `UPDATE v2_tasks SET project_id='missing' WHERE id=$1`, task["id"]); err != nil {
		t.Fatal(err)
	}
	status, got := c.doHeaders("POST", path, map[string]any{"workflowId": w.ID}, map[string]string{"Idempotency-Key": "project"})
	if status != 404 || got["code"] != "project_not_found" {
		t.Fatalf("project=%d %v", status, got)
	}
	if _, err := f.db.Exec(f.ctx, `UPDATE v2_tasks SET project_id=$2 WHERE id=$1`, task["id"], taskProjectID); err != nil {
		t.Fatal(err)
	}
	conflict := taskworkflow.New(f.store, conflictWorkflowPort{workflowTaskHTTPPort{finishingRuns{store: f.store}}})
	t.Cleanup(conflict.Close)
	f.srv.Close()
	f.srv = httptest.NewServer(server.New(f.store, server.Options{SessionTTL: time.Hour, TaskWorkflows: conflict}))
	t.Cleanup(f.srv.Close)
	c.base = f.srv.URL
	status, got = c.doHeaders("POST", path, map[string]any{"workflowId": w.ID}, map[string]string{"Idempotency-Key": "conflict"})
	if status != 409 || got["code"] != "version_conflict" {
		t.Fatalf("conflict=%d %v", status, got)
	}

	commentWorkflowServer(t, f)
	c = f.taskClient()
	c.login()
	c.routes = routes
	roleGraph := `{"nodes":[{"id":"start","data":{"kind":"start"}},{"id":"a","data":{"kind":"agent","agentConfig":{"prompt":"Review","roleId":"reviewer"}}}],"edges":[{"source":"start","target":"a"}]}`
	roleWF, _, err := f.store.CreateWorkflow(f.ctx, tid, "Needs role", roleGraph)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.PublishWorkflow(f.ctx, tid, roleWF.ID, "v1", nil); err != nil {
		t.Fatal(err)
	}
	status, got = c.doHeaders("POST", path, map[string]any{"workflowId": roleWF.ID}, map[string]string{"Idempotency-Key": "missing-role"})
	if status != 400 || got["code"] != "workflow_role_not_found" {
		t.Fatalf("missing role = %d %v, want 400 workflow_role_not_found", status, got)
	}
	c.routes = original
	status, gotTask := c.do("GET", base+"/"+task["id"].(string), nil)
	if status != 200 || gotTask["status"] != store.TaskPending {
		t.Fatalf("missing role moved task status: %d %v", status, gotTask)
	}
	c.routes = routes
	if _, err := f.store.CreateRole(f.ctx, tid, task["createdBy"].(string), store.RoleInput{Name: "reviewer", Description: "Reviewer", Content: "# Reviewer"}); err != nil {
		t.Fatal(err)
	}
	if status, started := c.doHeaders("POST", path, map[string]any{"workflowId": roleWF.ID}, map[string]string{"Idempotency-Key": "role-ready"}); status != 201 || started["workflowRunId"] == "" {
		t.Fatalf("start after creating role = %d %v", status, started)
	}

	// Reset task back to pending to verify missing Skill preflight refusal and recovery.
	c.routes = original
	_, secondTask := c.do("POST", base, map[string]any{"title": "Skill handling", "projectId": taskProjectID, "assignee": map[string]any{"type": "human", "id": task["createdBy"]}})
	secondPath := base + "/" + secondTask["id"].(string) + "/workflow-runs"
	c.routes = routes
	skillGraph := `{"nodes":[{"id":"start","data":{"kind":"start"}},{"id":"reviewer","data":{"kind":"agent","title":"审查","agentConfig":{"prompt":"Review","skills":[{"skillId":"review-checklist","enabled":true}]}}}],"edges":[{"source":"start","target":"reviewer"}]}`
	skillWF, _, err := f.store.CreateWorkflow(f.ctx, tid, "Needs skill", skillGraph)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.PublishWorkflow(f.ctx, tid, skillWF.ID, "v1", nil); err != nil {
		t.Fatal(err)
	}
	status, got = c.doHeaders("POST", secondPath, map[string]any{"workflowId": skillWF.ID}, map[string]string{"Idempotency-Key": "missing-skill"})
	if status != 400 || got["code"] != "workflow_skill_not_found" {
		t.Fatalf("missing skill = %d %v, want 400 workflow_skill_not_found", status, got)
	}
	skillParams, _ := got["params"].(map[string]any)
	if skillParams["nodeId"] != "reviewer" || skillParams["nodeTitle"] != "审查" || skillParams["skillId"] != "review-checklist" {
		t.Fatalf("missing skill params = %v", skillParams)
	}
	if _, err := f.store.CreateSkill(f.ctx, tid, secondTask["createdBy"].(string), store.SkillInput{Name: "review-checklist", Description: "审查清单", Content: "# 清单"}); err != nil {
		t.Fatal(err)
	}
	if status, started := c.doHeaders("POST", secondPath, map[string]any{"workflowId": skillWF.ID}, map[string]string{"Idempotency-Key": "skill-ready"}); status != 201 || started["workflowRunId"] == "" {
		t.Fatalf("start after creating skill = %d %v", status, started)
	}
}

func TestTaskWorkflowListHidesDeletedHandleFailureOnFirstSettlementHTTP(t *testing.T) {
	f := newSquadServer(t)
	host := taskworkflow.New(f.store, workflowTaskHTTPPort{finishingRuns{store: f.store, output: `{}`}})
	t.Cleanup(host.Close)
	f.srv.Close()
	f.srv = httptest.NewServer(server.New(f.store, server.Options{SessionTTL: time.Hour, TaskWorkflows: host}))
	t.Cleanup(f.srv.Close)
	c := f.taskClient()
	c.login()
	tid := f.tenant(t)
	f.project(t)
	wf, snap, err := f.store.CreateWorkflow(f.ctx, tid, "Handling", `{"nodes":[{"id":"start","data":{"kind":"start"}}],"edges":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	if snap, err = f.store.PublishWorkflow(f.ctx, tid, wf.ID, "v1", nil); err != nil {
		t.Fatal(err)
	}
	base := "/api/v2/tenants/" + tid + "/tasks"
	_, task := c.do("POST", base, map[string]any{"title": "Deleted handle", "projectId": taskProjectID})
	_, task = c.do("PUT", base+"/"+task["id"].(string)+"/assignment", map[string]any{"version": task["version"], "assignee": map[string]any{"type": "human", "id": task["createdBy"]}})

	runID, fresh, err := f.store.CreateTaskWorkflowRun(f.ctx, store.NewTaskWorkflow{
		TenantID: tid, TaskID: task["id"].(string), UserID: task["createdBy"].(string),
		Key: "deleted-before-settle", WorkflowID: wf.ID, SnapshotID: snap.ID,
	})
	if err != nil || !fresh {
		t.Fatalf("CreateTaskWorkflowRun = %q %t %v", runID, fresh, err)
	}
	if _, err := f.store.StartWorkflowRun(f.ctx, tid, runID); err != nil {
		t.Fatal(err)
	}
	handleErr := "private handle error"
	if _, err := f.store.FinishWorkflowRun(f.ctx, tid, runID, store.WorkflowStatusFailed, nil, &handleErr); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DeleteWorkflowRun(f.ctx, tid, runID); err != nil {
		t.Fatal(err)
	}

	doc, err := openapi3.NewLoader().LoadFromFile("../../task-workflows.openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	routes, err := legacy.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}
	taskRoutes := c.routes
	c.routes = routes
	path := base + "/" + task["id"].(string) + "/workflow-runs"
	for _, attempt := range []string{"first settlement read", "post-settlement read"} {
		status, list := c.do("GET", path, nil)
		items, _ := list["items"].([]any)
		if status != 200 || len(items) != 1 {
			t.Fatalf("%s workflow-runs = %d %v", attempt, status, list)
		}
		item := items[0].(map[string]any)
		if item["workflowRunId"] != runID || item["status"] != "failed" || item["deleted"] != true || item["failure"] != "" {
			t.Fatalf("%s leaked or misreported deleted handle run: %v", attempt, item)
		}
	}

	c.routes = taskRoutes
	status, gotTask := c.do("GET", base+"/"+task["id"].(string), nil)
	if status != 200 || gotTask["status"] != store.TaskReview {
		t.Fatalf("task status after List settlement = %d %v", status, gotTask)
	}
	var linkStatus, linkFailure string
	if err := f.db.QueryRow(f.ctx, `SELECT status, failure FROM v2_task_workflow_runs WHERE tenant_id = $1 AND run_id = $2`, tid, runID).Scan(&linkStatus, &linkFailure); err != nil {
		t.Fatal(err)
	}
	if linkStatus != "failed" || linkFailure != handleErr {
		t.Fatalf("stored link = %q %q, want failed %q", linkStatus, linkFailure, handleErr)
	}
}
