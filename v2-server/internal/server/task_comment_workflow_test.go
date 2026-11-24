package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/wanglongan587/cloud/v2-server/internal/boot"
	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/execution/executiontest"
	"github.com/wanglongan587/cloud/v2-server/internal/server"
	"github.com/wanglongan587/cloud/v2-server/internal/store"
	"github.com/wanglongan587/cloud/v2-server/internal/store/storetest"
	"github.com/wanglongan587/cloud/v2-server/internal/taskdecomp"
	"github.com/wanglongan587/cloud/v2-server/internal/taskrun"
	"github.com/wanglongan587/cloud/v2-server/internal/taskworkflow"
	"github.com/wanglongan587/cloud/v2-server/internal/workflowhost"
)

type realWorkflowTaskPort struct{ host *workflowhost.Host }

func (p realWorkflowTaskPort) Prepare(ctx context.Context, tenant, user, id string, input *string, values map[string]json.RawMessage) error {
	_, err := p.host.UpdateRunInput(ctx, server.WorkflowRunScope{TenantID: tenant, UserID: user}, id, input, values)
	return err
}

func (p realWorkflowTaskPort) Start(ctx context.Context, tenant, user, id string) error {
	_, err := p.host.StartRun(ctx, server.WorkflowRunScope{TenantID: tenant, UserID: user}, id)
	return err
}

func (p realWorkflowTaskPort) Cancel(ctx context.Context, tenant, user, id string) error {
	_, err := p.host.CancelRun(ctx, server.WorkflowRunScope{TenantID: tenant, UserID: user}, id)
	return err
}

func (p realWorkflowTaskPort) Subscribe(id string) (<-chan struct{}, func()) {
	return p.host.Hub().Subscribe(id)
}

func commentWorkflowServer(t *testing.T, f *squadFixture) *taskworkflow.Host {
	t.Helper()
	exec := &executiontest.Stub{}
	wfHost, err := workflowhost.NewHost(workflowhost.Config{Store: f.store, Execution: exec, Catalog: exec})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(wfHost.Close)
	twHost := taskworkflow.New(f.store, realWorkflowTaskPort{host: wfHost})
	t.Cleanup(twHost.Close)
	trHost := taskrun.New(f.store, exec, exec, nil)
	t.Cleanup(trHost.Close)
	f.srv.Close()
	f.srv = httptest.NewServer(server.New(f.store, server.Options{
		SessionTTL:        time.Hour,
		WorkflowRuns:      wfHost,
		WorkflowRunEvents: wfHost.Hub(),
		TaskWorkflows:     twHost,
		TaskRuns:          trHost,
	}))
	t.Cleanup(f.srv.Close)
	return twHost
}

const commentWorkflowGraph = `{
	"nodes":[
		{"id":"start","data":{"kind":"start","inputVariables":[
			{"name":"branch","valueType":"string","required":true},
			{"name":"count","valueType":"number","required":false,"value":1},
			{"name":"mode","valueType":"string","fieldType":"select","options":["fast","safe"],"required":false,"value":"fast"},
			{"name":"maxNote","valueType":"string","maxLength":5,"required":false},
			{"name":"tags","valueType":"array[string]","required":false},
			{"name":"meta","valueType":"object","required":false},
			{"name":"files","valueType":"array[file]","required":false}
		]}},
		{"id":"out","data":{"kind":"output","outputs":[
			{"name":"branch","variableSelector":["start","branch"]},
			{"name":"count","variableSelector":["start","count"]},
			{"name":"mode","variableSelector":["start","mode"]},
			{"name":"tags","variableSelector":["start","tags"]},
			{"name":"meta","variableSelector":["start","meta"]},
			{"name":"comment","variableSelector":["start","input"]}
		]}}
	],
	"edges":[{"source":"start","target":"out"}]
}`

func TestTaskCommentWorkflowHTTP(t *testing.T) {
	f := newSquadServer(t)
	commentWorkflowServer(t, f)

	ownerClient := f.taskClient()
	ownerClient.login()
	tid := f.tenant(t)
	f.project(t)

	// Add a workspace member who is NOT the task assignee.
	memberToken := storetest.SignIn(t, f.store, "comment-member", "成员评论者", store.RoleOwner)
	memberUser, err := f.store.SessionUser(f.ctx, memberToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `UPDATE memberships SET role = 'member' WHERE tenant_id = $1 AND user_id = $2`, tid, memberUser.ID); err != nil {
		t.Fatal(err)
	}
	memberClient := f.taskClient()
	req, _ := http.NewRequest("GET", memberClient.base, nil)
	memberClient.http.Jar.SetCookies(req.URL, []*http.Cookie{{Name: server.SessionCookie, Value: memberToken}})

	wf, _, err := f.store.CreateWorkflow(f.ctx, tid, "发布检查", commentWorkflowGraph)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.PublishWorkflow(f.ctx, tid, wf.ID, "v1", nil); err != nil {
		t.Fatal(err)
	}
	draftWF, _, err := f.store.CreateWorkflow(f.ctx, tid, "未发布草稿", commentWorkflowGraph)
	if err != nil {
		t.Fatal(err)
	}

	base := "/api/v2/tenants/" + tid + "/tasks"
	status, task := ownerClient.do("POST", base, map[string]any{"title": "评论运行工作流", "projectId": taskProjectID})
	if status != 201 {
		t.Fatalf("create task = %d %v", status, task)
	}
	taskID := task["id"].(string)
	commentsPath := base + "/" + taskID + "/comments"
	timelinePath := base + "/" + taskID + "/timeline"

	// Per-field input validation errors must reject before recording a comment or workflow run.
	invalidCases := []struct {
		name     string
		values   map[string]any
		variable string
		reason   string
	}{
		{"missing required", map[string]any{}, "branch", "required value is missing"},
		{"blank required", map[string]any{"branch": ""}, "branch", "required value is missing"},
		{"type mismatch", map[string]any{"branch": "main", "count": "not-a-number"}, "count", "type_mismatch"},
		{"invalid option", map[string]any{"branch": "main", "mode": "turbo"}, "mode", "value is not one of the configured options"},
		{"length exceeded", map[string]any{"branch": "main", "maxNote": "123456"}, "maxNote", "length_exceeded"},
		{"malformed structured json", map[string]any{"branch": "main", "tags": "[unclosed"}, "tags", "type_mismatch"},
		{"wrong structured json type", map[string]any{"branch": "main", "meta": "[1, 2]"}, "meta", "type_mismatch"},
		{"undeclared field", map[string]any{"branch": "main", "extra": 1}, "extra", "undeclared"},
	}
	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			code, got := memberClient.doHeaders("POST", commentsPath, map[string]any{
				"body":     "@发布检查 " + tc.name,
				"mentions": []map[string]any{{"type": "workflow", "id": wf.ID}},
				"workflow": map[string]any{"workflowId": wf.ID, "values": tc.values},
			}, map[string]string{"Idempotency-Key": "invalid-" + tc.name})
			params, _ := got["params"].(map[string]any)
			if code != 400 || got["code"] != "workflow_run_input_invalid" || params["variable"] != tc.variable || params["reason"] != tc.reason {
				t.Fatalf("%s: got %d %v", tc.name, code, got)
			}
		})
	}

	if status, got := memberClient.doHeaders("POST", commentsPath, map[string]any{
		"body":     "conflict",
		"toAgent":  true,
		"workflow": map[string]any{"workflowId": wf.ID, "values": map[string]any{"branch": "main"}},
	}, map[string]string{"Idempotency-Key": "both"}); status != 400 || got["code"] != "invalid_request" {
		t.Fatalf("toAgent+workflow = %d %v", status, got)
	}
	if status, got := memberClient.doHeaders("POST", commentsPath, map[string]any{
		"body":     "@未发布草稿",
		"mentions": []map[string]any{{"type": "workflow", "id": draftWF.ID}},
		"workflow": map[string]any{"workflowId": draftWF.ID, "values": map[string]any{"branch": "main"}},
	}, map[string]string{"Idempotency-Key": "draft-wf"}); status != 400 || got["code"] != "invalid_mention" {
		t.Fatalf("draft workflow mention = %d %v", status, got)
	}
	if status, got := memberClient.doHeaders("POST", commentsPath, map[string]any{
		"body":     "run draft directly",
		"workflow": map[string]any{"workflowId": draftWF.ID, "values": map[string]any{"branch": "main"}},
	}, map[string]string{"Idempotency-Key": "draft-direct"}); status != 400 || got["code"] != "workflow_no_published_snapshot" {
		t.Fatalf("draft workflow direct = %d %v", status, got)
	}

	var runCount int
	if err := f.db.QueryRow(f.ctx, `SELECT COUNT(*) FROM v2_workflow_runs WHERE tenant_id = $1`, tid).Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if runCount != 0 {
		t.Fatalf("invalid submissions created %d workflow runs, want 0", runCount)
	}

	// Discover published workflow target and its FormDescriptor from #1175 endpoints.
	origMemberRoutes := memberClient.routes
	memberClient.routes = nil
	if status, targetsBody := memberClient.do("GET", "/api/v1/tenants/"+tid+"/collaboration/targets", nil); status != 200 {
		t.Fatalf("targets = %d %v", status, targetsBody)
	} else {
		items, _ := targetsBody["items"].([]any)
		var foundTarget bool
		for _, raw := range items {
			item := raw.(map[string]any)
			if item["type"] == "workflow" && item["id"] == wf.ID {
				foundTarget = true
			}
		}
		if !foundTarget {
			t.Fatalf("published workflow missing from targets: %v", items)
		}
	}
	if status, formBody := memberClient.do("GET", "/api/v1/tenants/"+tid+"/collaboration/forms/"+wf.ID, nil); status != 200 || formBody["formRef"] != wf.ID {
		t.Fatalf("form = %d %v", status, formBody)
	}
	memberClient.routes = origMemberRoutes

	// Non-assignee workspace member triggers @ workflow comment with valid inputs (including textarea JSON strings).
	validKey := map[string]string{"Idempotency-Key": "comment-wf-1"}
	validPayload := map[string]any{
		"body":     "@发布检查 请检查 main 分支",
		"mentions": []map[string]any{{"type": "workflow", "id": wf.ID}},
		"workflow": map[string]any{
			"workflowId": wf.ID,
			"values": map[string]any{
				"branch":  "main",
				"count":   3,
				"mode":    "safe",
				"maxNote": "ok",
				"tags":    `["ci","release"]`,
				"meta":    `{"env":"prod"}`,
				"files":   `["docs/spec.md"]`,
			},
		},
	}
	status, created := memberClient.doHeaders("POST", commentsPath, validPayload, validKey)
	if status != 201 {
		t.Fatalf("valid comment workflow = %d %v", status, created)
	}
	if created["authorId"] != memberUser.ID || created["runId"] != "" {
		t.Fatalf("created author/runId = %v", created)
	}
	wfRun, _ := created["workflowRun"].(map[string]any)
	if wfRun == nil || wfRun["workflowId"] != wf.ID || wfRun["workflowName"] != "发布检查" || wfRun["version"] != "v1" || wfRun["deleted"] != false {
		t.Fatalf("workflowRun summary = %v", wfRun)
	}
	if s, _ := wfRun["status"].(string); s != "pending" && s != "running" && s != "succeeded" {
		t.Fatalf("unexpected initial workflowRun status = %v", wfRun)
	}
	runID := wfRun["id"].(string)
	settledRun := waitForCommentWorkflowStatus(t, memberClient, timelinePath, runID, "succeeded")
	outText, _ := settledRun["output"].(string)
	if !strings.Contains(outText, `"branch":"main"`) || !strings.Contains(outText, `"mode":"safe"`) || !strings.Contains(outText, `"ci"`) || !strings.Contains(outText, `"env":"prod"`) || !strings.Contains(outText, `@发布检查`) {
		t.Fatalf("workflow output = %q", outText)
	}

	// Run starter must be the commenter, and task status must remain pending.
	var starterID string
	if err := f.db.QueryRow(f.ctx, `SELECT COALESCE(started_by::text, '') FROM v2_workflow_runs WHERE id = $1`, runID).Scan(&starterID); err != nil {
		t.Fatal(err)
	}
	if starterID != memberUser.ID {
		t.Fatalf("workflow started_by = %s, want commenter %s", starterID, memberUser.ID)
	}
	if status, reloadedTask := memberClient.do("GET", base+"/"+taskID, nil); status != 200 || reloadedTask["status"] != store.TaskPending || reloadedTask["activeRun"] != nil {
		t.Fatalf("task mutated by comment workflow = %d %v", status, reloadedTask)
	}

	// Idempotent retry creates no extra workflow run or comment.
	status, retried := memberClient.doHeaders("POST", commentsPath, validPayload, validKey)
	if status != 201 || retried["id"] != created["id"] {
		t.Fatalf("retry = %d %v, want %v", status, retried, created["id"])
	}
	retryRun, _ := retried["workflowRun"].(map[string]any)
	if retryRun == nil || retryRun["id"] != runID {
		t.Fatalf("retry workflowRun = %v, want %s", retryRun, runID)
	}
	if err := f.db.QueryRow(f.ctx, `SELECT COUNT(*) FROM v2_workflow_runs WHERE tenant_id = $1`, tid).Scan(&runCount); err != nil {
		t.Fatal(err)
	}
	if runCount != 1 {
		t.Fatalf("workflow run count after retry = %d, want 1", runCount)
	}

	// Timeline preserves the linked workflow run and final output across reloads.
	status, tl := memberClient.do("GET", timelinePath, nil)
	if status != 200 {
		t.Fatalf("timeline = %d %v", status, tl)
	}
	comments, _ := tl["comments"].([]any)
	if len(comments) != 1 {
		t.Fatalf("timeline comments = %v", tl)
	}
	tlRun, _ := comments[0].(map[string]any)["workflowRun"].(map[string]any)
	if tlRun == nil || tlRun["id"] != runID || tlRun["status"] != "succeeded" || tlRun["output"] != settledRun["output"] {
		t.Fatalf("timeline workflowRun = %v", tlRun)
	}

	// Task workflow runs list also exposes the comment-started run under kind="mention".
	doc, err := openapi3.NewLoader().LoadFromFile("../../task-workflows.openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(f.ctx); err != nil {
		t.Fatal(err)
	}
	twRoutes, err := legacy.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}
	origRoutes := memberClient.routes
	memberClient.routes = twRoutes
	status, wfList := memberClient.do("GET", base+"/"+taskID+"/workflow-runs", nil)
	if status != 200 {
		t.Fatalf("task workflow-runs = %d %v", status, wfList)
	}
	items, _ := wfList["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["kind"] != "mention" || items[0].(map[string]any)["workflowRunId"] != runID || items[0].(map[string]any)["startedBy"] != memberUser.ID {
		t.Fatalf("task workflow-runs items = %v", items)
	}
	memberClient.routes = origRoutes

	// Closing the task blocks new @ workflow starts (409 task_closed) while preserving retries of existing keys.
	if status, closed := ownerClient.do("POST", base+"/"+taskID+"/status", map[string]any{"status": store.TaskCancelled, "version": task["version"]}); status != 200 {
		t.Fatalf("close task = %d %v", status, closed)
	}
	if status, got := memberClient.doHeaders("POST", commentsPath, validPayload, map[string]string{"Idempotency-Key": "after-close"}); status != 409 || got["code"] != "task_closed" {
		t.Fatalf("closed task comment workflow = %d %v", status, got)
	}
	if status, retriedClosed := memberClient.doHeaders("POST", commentsPath, validPayload, validKey); status != 201 || retriedClosed["id"] != created["id"] {
		t.Fatalf("retry after close = %d %v", status, retriedClosed)
	}

	// Soft-deleting the workflow run keeps timeline readable with deleted=true.
	if _, err := f.db.Exec(f.ctx, `UPDATE v2_workflow_runs SET deleted_at = now() WHERE id = $1`, runID); err != nil {
		t.Fatal(err)
	}
	status, tlAfterDelete := memberClient.do("GET", timelinePath, nil)
	if status != 200 {
		t.Fatalf("timeline after soft-delete = %d %v", status, tlAfterDelete)
	}
	deletedRun := tlAfterDelete["comments"].([]any)[0].(map[string]any)["workflowRun"].(map[string]any)
	if deletedRun["deleted"] != true {
		t.Fatalf("deleted flag = %v", deletedRun)
	}
}

func TestTaskCommentWorkflowIsolationAndAwaitingStatus(t *testing.T) {
	f := newSquadServer(t)
	commentWorkflowServer(t, f)

	c := f.taskClient()
	c.login()
	tid := f.tenant(t)
	f.project(t)

	// Check 503 when only TaskRuns is absent vs only TaskWorkflows is absent.
	noWorkflowHost := newSquadServerWith(t, server.Options{SessionTTL: time.Hour, TaskRuns: taskrun.New(f.store, &executiontest.Stub{}, &executiontest.Stub{}, nil)})
	noWfClient := noWorkflowHost.taskClient()
	noWfClient.login()
	noWfTid := noWorkflowHost.tenant(t)
	if status, got := noWfClient.doHeaders("POST", "/api/v2/tenants/"+noWfTid+"/tasks/00000000-0000-4000-8000-000000000001/comments", map[string]any{
		"body":     "missing host",
		"workflow": map[string]any{"workflowId": "00000000-0000-4000-8000-000000000002"},
	}, map[string]string{"Idempotency-Key": "no-host"}); status != 503 || got["code"] != "workflow_run_host_unavailable" {
		t.Fatalf("without TaskWorkflows = %d %v", status, got)
	}

	wf, _, err := f.store.CreateWorkflow(f.ctx, tid, "互动流程", `{"nodes":[{"id":"start","data":{"kind":"start","inputVariables":[{"name":"topic","valueType":"string","required":true},{"name":"items","valueType":"array[object]","value":[{"key":"k1","title":"待确认","description":"细节"}]}]}},{"id":"review","data":{"kind":"form","formConfig":{"title":"人工确认","submitLabel":"提交","reportSelector":["start","topic"],"itemsSelector":["start","items"],"fields":[]}}},{"id":"out","data":{"kind":"output","outputs":[{"name":"reply","variableSelector":["start","topic"]}]}}],"edges":[{"source":"start","target":"review"},{"source":"review","target":"out"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.PublishWorkflow(f.ctx, tid, wf.ID, "v1", nil); err != nil {
		t.Fatal(err)
	}
	status, task := c.do("POST", "/api/v2/tenants/"+tid+"/tasks", map[string]any{"title": "并存与状态派生", "projectId": taskProjectID})
	if status != 201 {
		t.Fatalf("create task = %d %v", status, task)
	}
	taskID := task["id"].(string)

	// Concurrent identical idempotency key calls must settle onto the exact same run.
	const workers = 4
	results := make([]string, workers)
	statuses := make([]int, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cli := f.taskClient()
			cli.login()
			code, got := cli.doHeaders("POST", "/api/v2/tenants/"+tid+"/tasks/"+taskID+"/comments", map[string]any{
				"body":     "@互动流程",
				"mentions": []map[string]any{{"type": "workflow", "id": wf.ID}},
				"workflow": map[string]any{"workflowId": wf.ID, "values": map[string]any{"topic": "spec"}},
			}, map[string]string{"Idempotency-Key": "concurrent-same"})
			statuses[idx] = code
			if r, ok := got["workflowRun"].(map[string]any); ok {
				results[idx], _ = r["id"].(string)
			}
		}(i)
	}
	wg.Wait()
	for i := range workers {
		if statuses[i] != 201 || results[i] == "" || results[i] != results[0] {
			t.Fatalf("concurrent[%d] status=%d run=%q (want %q)", i, statuses[i], results[i], results[0])
		}
	}

	// Real form node parks the run naturally so TaskTimeline derives awaitingInput without altering task status.
	runID := results[0]
	timelinePath := "/api/v2/tenants/" + tid + "/tasks/" + taskID + "/timeline"
	wfRun := waitForCommentWorkflowStatus(t, c, timelinePath, runID, "awaitingInput")
	if wfRun["status"] != "awaitingInput" {
		t.Fatalf("derived awaitingInput status = %v", wfRun["status"])
	}

	// Second comment @ workflow succeeds concurrently without "run_active" blocking.
	status, second := c.doHeaders("POST", "/api/v2/tenants/"+tid+"/tasks/"+taskID+"/comments", map[string]any{
		"body":     "@互动流程 第二次",
		"workflow": map[string]any{"workflowId": wf.ID, "values": map[string]any{"topic": "second"}},
	}, map[string]string{"Idempotency-Key": "second-key"})
	if status != 201 || second["workflowRun"].(map[string]any)["id"] == runID {
		t.Fatalf("second comment workflow = %d %v", status, second)
	}
	secondRunID := second["workflowRun"].(map[string]any)["id"].(string)
	waitForCommentWorkflowStatus(t, c, timelinePath, secondRunID, "awaitingInput")
}

func waitForCommentWorkflowStatus(t *testing.T, client *taskClient, timelinePath, runID, want string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		status, tl := client.do("GET", timelinePath, nil)
		if status != 200 {
			t.Fatalf("timeline = %d %v", status, tl)
		}
		for _, raw := range tl["comments"].([]any) {
			if wr, _ := raw.(map[string]any)["workflowRun"].(map[string]any); wr != nil && wr["id"] == runID {
				if wr["status"] == want {
					return wr
				}
				if s, _ := wr["status"].(string); s == "failed" || s == "cancelled" || (want != "succeeded" && s == "succeeded") {
					t.Fatalf("workflowRun %s ended in %q (want %q): %v", runID, s, want, wr)
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for comment workflow %s status=%s", runID, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTaskCommentWorkflowCancelsOnStartFailure(t *testing.T) {
	for _, prepare := range []bool{true, false} {
		name := "start"
		if prepare {
			name = "prepare"
		}
		t.Run(name, func(t *testing.T) {
			f := newSquadServer(t)
			boom := errors.New("start boom")
			failing := taskworkflow.New(f.store, failingStartRuns{err: boom, prepare: prepare})
			t.Cleanup(failing.Close)
			f.srv.Close()
			f.srv = httptest.NewServer(server.New(f.store, server.Options{SessionTTL: time.Hour, TaskWorkflows: failing}))
			t.Cleanup(f.srv.Close)

			c := f.taskClient()
			c.login()
			tid := f.tenant(t)
			f.project(t)
			wf, _, err := f.store.CreateWorkflow(f.ctx, tid, "Failure", commentWorkflowGraph)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.store.PublishWorkflow(f.ctx, tid, wf.ID, "v1", nil); err != nil {
				t.Fatal(err)
			}
			_, task := c.do("POST", "/api/v2/tenants/"+tid+"/tasks", map[string]any{"title": "StartFailure", "projectId": taskProjectID})
			path := "/api/v2/tenants/" + tid + "/tasks/" + task["id"].(string) + "/comments"
			payload := map[string]any{
				"body":     "@Failure",
				"workflow": map[string]any{"workflowId": wf.ID, "values": map[string]any{"branch": "main", "tags": []any{}, "meta": map[string]any{}}},
			}
			key := map[string]string{"Idempotency-Key": "fail-start"}
			status, _ := c.doHeaders("POST", path, payload, key)
			if status != 500 {
				t.Fatalf("failing start status = %d, want 500", status)
			}
			for _, table := range []string{"v2_task_comments", "v2_workflow_runs", "v2_workflow_scopes", "v2_workflow_run_roots"} {
				var count int
				if err := f.db.QueryRow(f.ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != 0 {
					t.Fatalf("refused admission left %d rows in %s", count, table)
				}
			}
			if status, timeline := c.do("GET", "/api/v2/tenants/"+tid+"/tasks/"+task["id"].(string)+"/timeline", nil); status != 200 || len(timeline["comments"].([]any)) != 0 {
				t.Fatalf("refused admission timeline = %d %v", status, timeline)
			}
			// Restore the real host, then retry exactly the failed request and key.
			commentWorkflowServer(t, f)
			c = f.taskClient()
			c.login()
			status, created := c.doHeaders("POST", path, payload, key)
			if status != 201 {
				t.Fatalf("retry after refusal = %d %v", status, created)
			}
			runID := created["workflowRun"].(map[string]any)["id"].(string)
			waitForCommentWorkflowStatus(t, c, "/api/v2/tenants/"+tid+"/tasks/"+task["id"].(string)+"/timeline", runID, "succeeded")
			status, replay := c.doHeaders("POST", path, payload, key)
			if status != 201 || replay["id"] != created["id"] || replay["workflowRun"].(map[string]any)["id"] != runID {
				t.Fatalf("successful replay = %d %v", status, replay)
			}
			for _, table := range []string{"v2_task_comments", "v2_workflow_runs"} {
				var count int
				if err := f.db.QueryRow(f.ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != 1 {
					t.Fatalf("successful retry left %d rows in %s, want 1", count, table)
				}
			}
		})
	}
}

func TestTaskCommentWorkflowRetainsHistoryAfterExecutionStarted(t *testing.T) {
	f := newSquadServer(t)
	var mu sync.Mutex
	var cancelCalls []string
	port := &failingStartRuns{
		err: errors.New("private infrastructure detail"),
		start: func(ctx context.Context, tenant, runID string) error {
			_, err := f.store.StartWorkflowRun(ctx, tenant, runID)
			return err
		},
		cancel: func(_ context.Context, _ string, _ string, runID string) error {
			mu.Lock()
			cancelCalls = append(cancelCalls, runID)
			mu.Unlock()
			return nil
		},
	}
	host := taskworkflow.New(f.store, port)
	t.Cleanup(host.Close)
	f.srv.Close()
	f.srv = httptest.NewServer(server.New(f.store, server.Options{SessionTTL: time.Hour, TaskWorkflows: host}))
	t.Cleanup(f.srv.Close)
	c := f.taskClient()
	c.login()
	tid := f.tenant(t)
	f.project(t)
	wf, _, err := f.store.CreateWorkflow(f.ctx, tid, "Started failure", commentWorkflowGraph)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.PublishWorkflow(f.ctx, tid, wf.ID, "v1", nil); err != nil {
		t.Fatal(err)
	}
	_, task := c.do("POST", "/api/v2/tenants/"+tid+"/tasks", map[string]any{"title": "StartedFailure", "projectId": taskProjectID})
	path := "/api/v2/tenants/" + tid + "/tasks/" + task["id"].(string) + "/comments"
	payload := map[string]any{"body": "@Started failure", "workflow": map[string]any{"workflowId": wf.ID, "values": map[string]any{"branch": "main"}}}
	key := map[string]string{"Idempotency-Key": "already-started"}
	if status, body := c.doHeaders("POST", path, payload, key); status != 500 {
		t.Fatalf("failed start = %d %v", status, body)
	}
	status, replay := c.doHeaders("POST", path, payload, key)
	if status != 201 {
		t.Fatalf("replay = %d %v", status, replay)
	}
	run := replay["workflowRun"].(map[string]any)
	if run["status"] != "cancelled" || run["error"] != "工作流启动失败，请重新发起。" {
		t.Fatalf("failed start lost its safe reason: %v", run)
	}
	mu.Lock()
	gotCancels := append([]string(nil), cancelCalls...)
	mu.Unlock()
	if len(gotCancels) != 1 || gotCancels[0] != run["id"].(string) {
		t.Fatalf("host Cancel calls = %v, want [%v]", gotCancels, run["id"])
	}
	for _, table := range []string{"v2_task_comments", "v2_workflow_runs"} {
		var count int
		if err := f.db.QueryRow(f.ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("started admission left %d rows in %s, want 1", count, table)
		}
	}
}

func TestTaskCommentWorkflowMissingRoleReturns400AndRecovers(t *testing.T) {
	f := newSquadServer(t)
	commentWorkflowServer(t, f)
	c := f.taskClient()
	c.login()
	tid := f.tenant(t)
	f.project(t)

	graph := `{"nodes":[{"id":"start","data":{"kind":"start"}},{"id":"a","data":{"kind":"agent","agentConfig":{"prompt":"Review","roleId":"reviewer"}}},{"id":"out","data":{"kind":"output","outputs":[{"name":"comment","variableSelector":["start","input"]}]}}],"edges":[{"source":"start","target":"a"},{"source":"a","target":"out"}]}`
	wf, _, err := f.store.CreateWorkflow(f.ctx, tid, "MissingRoleWF", graph)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.PublishWorkflow(f.ctx, tid, wf.ID, "v1", nil); err != nil {
		t.Fatal(err)
	}
	_, task := c.do("POST", "/api/v2/tenants/"+tid+"/tasks", map[string]any{"title": "MissingRoleTask", "projectId": taskProjectID})
	path := "/api/v2/tenants/" + tid + "/tasks/" + task["id"].(string) + "/comments"
	payload := map[string]any{
		"body":     "@MissingRoleWF",
		"mentions": []map[string]any{{"type": "workflow", "id": wf.ID}},
		"workflow": map[string]any{"workflowId": wf.ID, "values": map[string]any{}},
	}
	key := map[string]string{"Idempotency-Key": "missing-role-comment"}
	status, got := c.doHeaders("POST", path, payload, key)
	if status != 400 || got["code"] != "workflow_role_not_found" {
		t.Fatalf("missing role comment = %d %v, want 400 workflow_role_not_found", status, got)
	}
	for _, table := range []string{"v2_task_comments", "v2_workflow_runs", "v2_workflow_scopes", "v2_workflow_run_roots"} {
		var count int
		if err := f.db.QueryRow(f.ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("missing role refusal left %d rows in %s", count, table)
		}
	}

	if _, err := f.store.CreateRole(f.ctx, tid, task["createdBy"].(string), store.RoleInput{Name: "reviewer", Description: "Reviewer", Content: "# Reviewer"}); err != nil {
		t.Fatal(err)
	}
	// Replace the graph with a no-agent output path so completion succeeds deterministically once role check passes.
	readyGraph := `{"nodes":[{"id":"start","data":{"kind":"start"}},{"id":"note","data":{"kind":"agent","agentConfig":{"prompt":"Review","roleId":"reviewer"}}},{"id":"out","data":{"kind":"output","outputs":[{"name":"comment","variableSelector":["start","input"]}]}}],"edges":[{"source":"start","target":"out"}]}`
	wfReady, _, err := f.store.CreateWorkflow(f.ctx, tid, "ReadyRoleWF", readyGraph)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.PublishWorkflow(f.ctx, tid, wfReady.ID, "v1", nil); err != nil {
		t.Fatal(err)
	}
	payload["mentions"] = []map[string]any{{"type": "workflow", "id": wfReady.ID}}
	payload["workflow"] = map[string]any{"workflowId": wfReady.ID, "values": map[string]any{}}
	status, created := c.doHeaders("POST", path, payload, key)
	if status != 201 {
		t.Fatalf("retry after role creation = %d %v", status, created)
	}
	runID := created["workflowRun"].(map[string]any)["id"].(string)
	waitForCommentWorkflowStatus(t, c, "/api/v2/tenants/"+tid+"/tasks/"+task["id"].(string)+"/timeline", runID, "succeeded")
}

func TestTaskCommentWorkflowMissingSkillReturns400AndRecovers(t *testing.T) {
	f := newSquadServer(t)
	commentWorkflowServer(t, f)
	c := f.taskClient()
	c.login()
	tid := f.tenant(t)
	f.project(t)

	// Keep the Agent on a branch this input does not take so preflight catches the missing Skill
	// before execution, and retrying the exact same workflow succeeds deterministically once created.
	graph := `{"nodes":[{"id":"start","data":{"kind":"start"}},{"id":"cond","data":{"kind":"condition","cases":[{"id":"never","logic":"and","conditions":[{"variableSelector":["start","input"],"operator":"is","value":"__never__"}]}]}},{"id":"a","data":{"kind":"agent","title":"代码检查","agentConfig":{"prompt":"Review","skills":[{"skillId":"review-checklist","enabled":true}]}}},{"id":"out","data":{"kind":"output","outputs":[{"name":"comment","variableSelector":["start","input"]}]}}],"edges":[{"source":"start","target":"cond"},{"source":"cond","sourceHandle":"never","target":"a"},{"source":"cond","target":"out"}]}`
	wf, _, err := f.store.CreateWorkflow(f.ctx, tid, "MissingSkillWF", graph)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.PublishWorkflow(f.ctx, tid, wf.ID, "v1", nil); err != nil {
		t.Fatal(err)
	}
	_, task := c.do("POST", "/api/v2/tenants/"+tid+"/tasks", map[string]any{"title": "MissingSkillTask", "projectId": taskProjectID})
	path := "/api/v2/tenants/" + tid + "/tasks/" + task["id"].(string) + "/comments"
	payload := map[string]any{
		"body":     "@MissingSkillWF",
		"mentions": []map[string]any{{"type": "workflow", "id": wf.ID}},
		"workflow": map[string]any{"workflowId": wf.ID, "values": map[string]any{}},
	}
	key := map[string]string{"Idempotency-Key": "missing-skill-comment"}
	status, got := c.doHeaders("POST", path, payload, key)
	if status != 400 || got["code"] != "workflow_skill_not_found" {
		t.Fatalf("missing skill comment = %d %v, want 400 workflow_skill_not_found", status, got)
	}
	params, _ := got["params"].(map[string]any)
	if params["nodeId"] != "a" || params["nodeTitle"] != "代码检查" || params["skillId"] != "review-checklist" {
		t.Fatalf("missing skill comment params = %v", params)
	}
	for _, table := range []string{"v2_task_comments", "v2_workflow_runs", "v2_workflow_scopes", "v2_workflow_run_roots"} {
		var count int
		if err := f.db.QueryRow(f.ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("missing skill refusal left %d rows in %s", count, table)
		}
	}

	if _, err := f.store.CreateSkill(f.ctx, tid, task["createdBy"].(string), store.SkillInput{Name: "review-checklist", Description: "审查清单", Content: "# 清单"}); err != nil {
		t.Fatal(err)
	}
	status, created := c.doHeaders("POST", path, payload, key)
	if status != 201 {
		t.Fatalf("retry after skill creation = %d %v", status, created)
	}
	runID := created["workflowRun"].(map[string]any)["id"].(string)
	waitForCommentWorkflowStatus(t, c, "/api/v2/tenants/"+tid+"/tasks/"+task["id"].(string)+"/timeline", runID, "succeeded")
}

func TestTaskCommentWorkflowBootRecoverClearsPendingCrashWindow(t *testing.T) {
	f := newSquadServer(t)

	c := f.taskClient()
	c.login()
	tid := f.tenant(t)
	f.project(t)
	wf, _, err := f.store.CreateWorkflow(f.ctx, tid, "CrashWindowWF", commentWorkflowGraph)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := f.store.PublishWorkflow(f.ctx, tid, wf.ID, "v1", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, task := c.do("POST", "/api/v2/tenants/"+tid+"/tasks", map[string]any{"title": "CrashRecover", "projectId": taskProjectID})
	taskID := task["id"].(string)

	// Simulate CreateTaskWorkflowComment committing right before process death (bypassing rejectCommentWorkflow).
	crashed, fresh, err := f.store.CreateTaskWorkflowComment(f.ctx, store.NewTaskWorkflowComment{
		TenantID: tid, TaskID: taskID, AuthorID: task["createdBy"].(string), CreateKey: "crash-window",
		Body: "@CrashWindowWF", Mentions: []store.TaskMention{{Type: "workflow", ID: wf.ID}},
		WorkflowID: wf.ID, SnapshotID: snap.ID,
	})
	if err != nil || !fresh || crashed.WorkflowRun == nil {
		t.Fatalf("seed crash-window comment: fresh=%t err=%v", fresh, err)
	}
	// Also seed an already-started comment run so boot.Recover proves it only discards unstarted pending runs.
	started, fresh, err := f.store.CreateTaskWorkflowComment(f.ctx, store.NewTaskWorkflowComment{
		TenantID: tid, TaskID: taskID, AuthorID: task["createdBy"].(string), CreateKey: "started-before-crash",
		Body: "@CrashWindowWF started", Mentions: []store.TaskMention{{Type: "workflow", ID: wf.ID}},
		WorkflowID: wf.ID, SnapshotID: snap.ID,
	})
	if err != nil || !fresh || started.WorkflowRun == nil {
		t.Fatalf("seed started comment: fresh=%t err=%v", fresh, err)
	}
	if _, err := f.store.StartWorkflowRun(f.ctx, tid, started.WorkflowRun.ID); err != nil {
		t.Fatal(err)
	}

	exec := &executiontest.Stub{}
	wfHost, err := workflowhost.NewHost(workflowhost.Config{Store: f.store, Execution: exec, Catalog: exec})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(wfHost.Close)
	twHost := taskworkflow.New(f.store, realWorkflowTaskPort{host: wfHost})
	t.Cleanup(twHost.Close)
	decompHost := taskdecomp.New(f.store, realWorkflowTaskPort{host: wfHost}, nil)
	t.Cleanup(decompHost.Close)
	trHost := taskrun.New(f.store, exec, exec, nil)
	t.Cleanup(trHost.Close)

	if err := boot.Recover(f.ctx, boot.Hosts{
		TaskWorkflows: twHost, Decompositions: decompHost, WorkflowRuns: wfHost, TaskRuns: trHost,
	}); err != nil {
		t.Fatalf("boot.Recover = %v", err)
	}

	if _, err := f.store.TaskCommentByKey(f.ctx, tid, taskID, task["createdBy"].(string), "crash-window"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unstarted crash-window comment survived boot.Recover: %v", err)
	}
	if got, err := f.store.TaskCommentByKey(f.ctx, tid, taskID, task["createdBy"].(string), "started-before-crash"); err != nil || got.WorkflowRun == nil || got.WorkflowRun.Status != store.WorkflowStatusFailed {
		t.Fatalf("started comment run state after boot.Recover = %+v %v", got, err)
	}

	f.srv.Close()
	f.srv = httptest.NewServer(server.New(f.store, server.Options{
		SessionTTL: time.Hour, WorkflowRuns: wfHost, WorkflowRunEvents: wfHost.Hub(), TaskWorkflows: twHost, TaskRuns: trHost,
	}))
	t.Cleanup(f.srv.Close)
	c = f.taskClient()
	c.login()
	path := "/api/v2/tenants/" + tid + "/tasks/" + taskID + "/comments"
	payload := map[string]any{
		"body":     "@CrashWindowWF",
		"mentions": []map[string]any{{"type": "workflow", "id": wf.ID}},
		"workflow": map[string]any{"workflowId": wf.ID, "values": map[string]any{"branch": "main", "tags": []any{}, "meta": map[string]any{}}},
	}
	status, created := c.doHeaders("POST", path, payload, map[string]string{"Idempotency-Key": "crash-window"})
	if status != 201 {
		t.Fatalf("same-key retry after boot.Recover = %d %v", status, created)
	}
	runID := created["workflowRun"].(map[string]any)["id"].(string)
	waitForCommentWorkflowStatus(t, c, "/api/v2/tenants/"+tid+"/tasks/"+taskID+"/timeline", runID, "succeeded")
}

type blockingTurnStream struct {
	events chan execution.Event
}

func (s *blockingTurnStream) Next(ctx context.Context) (execution.Event, error) {
	select {
	case ev, ok := <-s.events:
		if !ok {
			return execution.Event{}, io.EOF
		}
		return ev, nil
	case <-ctx.Done():
		return execution.Event{}, ctx.Err()
	}
}

func (s *blockingTurnStream) Close() error { return nil }

type postStartFailingPort struct {
	realWorkflowTaskPort
	started <-chan struct{}
	err     error
}

func (p postStartFailingPort) Start(ctx context.Context, tenant, user, id string) error {
	if err := p.realWorkflowTaskPort.Start(ctx, tenant, user, id); err != nil {
		return err
	}
	select {
	case <-p.started:
		return p.err
	case <-time.After(10 * time.Second):
		return errors.New("timed out waiting for the Agent turn to start")
	}
}

func TestTaskCommentWorkflowRealWorkflowHostCancelAndUnconfirmedStop(t *testing.T) {
	agentGraph := `{"nodes":[{"id":"start","data":{"kind":"start"}},{"id":"a","data":{"kind":"agent","agentConfig":{"executor":{"agentCli":"official/ora-space.opencode","modelId":"fixture"},"prompt":"Run"}}}],"edges":[{"source":"start","target":"a"}]}`
	for _, confirmed := range []bool{true, false} {
		name := "confirmed stop marks cancelled"
		if !confirmed {
			name = "unconfirmed stop keeps running without failure"
		}
		t.Run(name, func(t *testing.T) {
			f := newSquadServer(t)
			var mu sync.Mutex
			var cancelCalls []execution.Ref
			startedCh := make(chan struct{})
			var startedOnce sync.Once
			stream := &blockingTurnStream{events: make(chan execution.Event, 4)}
			exec := &executiontest.Stub{
				ListExecutorsFunc: func(context.Context, execution.Scope) ([]execution.Executor, error) {
					return []execution.Executor{{
						ID: "opencode:fixture", Name: "OpenCode / Fixture", AgentID: "official/ora-space.opencode",
						AgentName: "OpenCode", PluginVersion: "0.6.11", Model: execution.Model{ID: "fixture", Name: "Fixture"},
					}}, nil
				},
				StartFunc: func(context.Context, execution.Scope, execution.StartRequest) (execution.Ref, error) {
					startedOnce.Do(func() { close(startedCh) })
					return execution.Ref{SessionID: "sess-1", TurnID: "turn-1"}, nil
				},
				SubscribeFunc: func(context.Context, execution.Scope, string, uint64) (execution.Stream, error) {
					return stream, nil
				},
				CancelFunc: func(_ context.Context, _ execution.Scope, ref execution.Ref) (execution.Result, error) {
					mu.Lock()
					cancelCalls = append(cancelCalls, ref)
					mu.Unlock()
					if !confirmed {
						return execution.Result{}, errors.New("stop unconfirmed")
					}
					res := execution.Result{Status: execution.StatusCancelled}
					stream.events <- execution.Event{Kind: execution.EventTurnEnded, Ref: ref, Result: &res}
					close(stream.events)
					return res, nil
				},
				EndFunc: func(context.Context, execution.Scope, string) error { return nil },
			}
			wfHost, err := workflowhost.NewHost(workflowhost.Config{Store: f.store, Execution: exec, Catalog: exec})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if !confirmed {
					select {
					case <-stream.events:
					default:
						close(stream.events)
					}
				}
				wfHost.Close()
			})
			twHost := taskworkflow.New(f.store, postStartFailingPort{
				realWorkflowTaskPort: realWorkflowTaskPort{host: wfHost},
				started:              startedCh,
				err:                  errors.New("start error after dispatch"),
			})
			t.Cleanup(twHost.Close)
			f.srv.Close()
			f.srv = httptest.NewServer(server.New(f.store, server.Options{
				SessionTTL: time.Hour, WorkflowRuns: wfHost, WorkflowRunEvents: wfHost.Hub(), TaskWorkflows: twHost,
			}))
			t.Cleanup(f.srv.Close)

			c := f.taskClient()
			c.login()
			tid := f.tenant(t)
			f.project(t)
			wf, _, err := f.store.CreateWorkflow(f.ctx, tid, "AgentCommentWF", agentGraph)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.store.PublishWorkflow(f.ctx, tid, wf.ID, "v1", nil); err != nil {
				t.Fatal(err)
			}
			_, task := c.do("POST", "/api/v2/tenants/"+tid+"/tasks", map[string]any{"title": "RealCancel", "projectId": taskProjectID})
			path := "/api/v2/tenants/" + tid + "/tasks/" + task["id"].(string) + "/comments"
			payload := map[string]any{"body": "@AgentCommentWF", "workflow": map[string]any{"workflowId": wf.ID, "values": map[string]any{}}}
			key := map[string]string{"Idempotency-Key": "real-cancel-" + name}

			status, _ := c.doHeaders("POST", path, payload, key)
			if status < 500 {
				t.Fatalf("expected 5xx from post-start/cancel failure, got %d", status)
			}

			mu.Lock()
			gotExecCancels := len(cancelCalls)
			mu.Unlock()
			if gotExecCancels == 0 {
				t.Fatalf("execution Cancel was never invoked through workflowhost.CancelRun")
			}

			replayStatus, replay := c.doHeaders("POST", path, payload, key)
			if replayStatus != 201 {
				t.Fatalf("replay = %d %v", replayStatus, replay)
			}
			run := replay["workflowRun"].(map[string]any)
			if confirmed {
				if run["status"] != "cancelled" || run["error"] != "工作流启动失败，请重新发起。" {
					t.Fatalf("confirmed stop run = %v", run)
				}
			} else {
				if run["status"] != "running" || run["error"] != nil {
					t.Fatalf("unconfirmed stop illegally cancelled run: %v", run)
				}
			}
		})
	}
}

func TestTaskCommentWorkflowPreservesStartVariableNamedInput(t *testing.T) {
	f := newSquadServer(t)
	commentWorkflowServer(t, f)
	c := f.taskClient()
	c.login()
	tid := f.tenant(t)
	f.project(t)

	graph := `{"nodes":[
		{"id":"start","data":{"kind":"start","inputVariables":[{"name":"input","valueType":"string","required":true}]}},
		{"id":"out","data":{"kind":"output","outputs":[{"name":"result","variableSelector":["start","input"]}]}}
	],"edges":[{"source":"start","target":"out"}]}`
	wf, _, err := f.store.CreateWorkflow(f.ctx, tid, "InputVarWF", graph)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.PublishWorkflow(f.ctx, tid, wf.ID, "v1", nil); err != nil {
		t.Fatal(err)
	}
	_, task := c.do("POST", "/api/v2/tenants/"+tid+"/tasks", map[string]any{"title": "InputVarTask", "projectId": taskProjectID})
	taskID := task["id"].(string)
	path := "/api/v2/tenants/" + tid + "/tasks/" + taskID + "/comments"
	timelinePath := "/api/v2/tenants/" + tid + "/tasks/" + taskID + "/timeline"

	for _, tc := range []struct {
		name      string
		key       string
		input     any
		wantValue string
	}{
		{name: "form values input without kickoff input", key: "input-var-nil", input: nil, wantValue: "custom from form"},
		{name: "form values input overrides kickoff input", key: "input-var-kickoff", input: "kickoff text", wantValue: "explicit wins"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wfBody := map[string]any{"workflowId": wf.ID, "values": map[string]any{"input": tc.wantValue}}
			if tc.input != nil {
				wfBody["input"] = tc.input
			}
			status, created := c.doHeaders("POST", path, map[string]any{
				"body":     "@InputVarWF",
				"mentions": []map[string]any{{"type": "workflow", "id": wf.ID}},
				"workflow": wfBody,
			}, map[string]string{"Idempotency-Key": tc.key})
			if status != 201 {
				t.Fatalf("comment start = %d %v", status, created)
			}
			runID := created["workflowRun"].(map[string]any)["id"].(string)
			settled := waitForCommentWorkflowStatus(t, c, timelinePath, runID, "succeeded")
			out, _ := settled["output"].(string)
			want := `{"result":"` + tc.wantValue + `"}`
			if out != want {
				t.Fatalf("settled output = %q, want %q", out, want)
			}
		})
	}
}

type failingStartRuns struct {
	err     error
	prepare bool
	start   func(context.Context, string, string) error
	cancel  func(context.Context, string, string, string) error
}

func (f failingStartRuns) Prepare(context.Context, string, string, string, *string, map[string]json.RawMessage) error {
	if f.prepare {
		return f.err
	}
	return nil
}
func (f failingStartRuns) Start(ctx context.Context, tenant, _ string, runID string) error {
	if f.start != nil {
		if err := f.start(ctx, tenant, runID); err != nil {
			return err
		}
	}
	return f.err
}
func (f failingStartRuns) Cancel(ctx context.Context, tenant, user, runID string) error {
	if f.cancel != nil {
		return f.cancel(ctx, tenant, user, runID)
	}
	return nil
}
func (failingStartRuns) Subscribe(string) (<-chan struct{}, func()) { return nil, func() {} }
