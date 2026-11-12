package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/wanglongan587/cloud/v2-server/internal/execution"
	"github.com/wanglongan587/cloud/v2-server/internal/server"
	"github.com/wanglongan587/cloud/v2-server/internal/store"
	"github.com/wanglongan587/cloud/v2-server/internal/store/storetest"
)

func TestScheduleLatestValidation(t *testing.T) {
	f := newWorkflowTargetFixture(t)
	call := f.client(t, f.options(true))
	latest := f.target(map[string]any{"versionPolicy": "latest"})
	delete(latest, "snapshotId")
	if w := call("POST", "/schedule-configs", f.config(f.project.ID, latest)); w.Code != 201 {
		t.Fatalf("latest: %d %s", w.Code, w.Body)
	}
	for name, target := range map[string]map[string]any{
		"unknown policy":         f.target(map[string]any{"versionPolicy": "future"}),
		"ambiguous latest":       f.target(map[string]any{"versionPolicy": "latest"}),
		"missing fixed snapshot": {"kind": "workflow", "workflowId": f.workflow.ID, "versionPolicy": "fixed", "input": ""},
	} {
		t.Run(name, func(t *testing.T) {
			if w := call("POST", "/schedule-configs", f.config(f.project.ID, target)); w.Code != 400 {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
		})
	}
	// A tenant-local but unpublished definition is unavailable, not a draft fallback.
	workflow, _, err := f.db.CreateWorkflow(context.Background(), f.scope.TenantID, "未发布", `{"nodes":[],"edges":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	latest["workflowId"] = workflow.ID
	if w := call("POST", "/schedule-configs", f.config(f.project.ID, latest)); w.Code != 404 {
		t.Fatalf("unpublished: %d %s", w.Code, w.Body)
	}
	latest["workflowId"] = "44444444-4444-4444-8444-444444444444"
	if w := call("POST", "/schedule-configs", f.config(f.project.ID, latest)); w.Code != 404 {
		t.Fatalf("unknown: %d %s", w.Code, w.Body)
	}
}

func TestScheduledLatestResolvesEveryManualAndTimerTrigger(t *testing.T) {
	f := newWorkflowTargetFixture(t)
	ctx := context.Background()
	create := func(policy string) store.ScheduleConfig {
		t.Helper()
		target := f.target(nil)
		if policy != "" {
			target["versionPolicy"] = policy
			delete(target, "snapshotId")
		}
		raw, _ := json.Marshal(target)
		config, err := f.db.CreateScheduleConfig(ctx, f.scope.TenantID, f.scope.UserID, store.ScheduleInput{Name: "策略 " + policy, ProjectID: f.project.ID, Target: raw, Rule: json.RawMessage(`{"kind":"hours","hours":1,"timezone":"UTC"}`)})
		if err != nil {
			t.Fatal(err)
		}
		return config
	}
	latest, fixed := create("latest"), create("")
	now := latest.CreatedAt.Add(time.Minute)
	c, err := server.NewScheduleCoordinator(f.db, f.options(true), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Recover(ctx, f.scope); err != nil {
		t.Fatal(err)
	}
	if err = c.Tick(ctx, f.scope); err != nil {
		t.Fatal(err)
	}
	trigger := func(config store.ScheduleConfig, key, snapshot string) store.WorkflowRun {
		t.Helper()
		o, err := c.Manual(ctx, f.scope, config.ID, key)
		if err != nil {
			t.Fatal(err)
		}
		run, err := f.db.ScheduledWorkflowRun(ctx, f.scope.TenantID, o.ID)
		if err != nil || run.SnapshotID != snapshot {
			t.Fatalf("run snapshot %s want %s: %v", run.SnapshotID, snapshot, err)
		}
		if run.Status == store.WorkflowStatusRunning {
			if _, err = f.db.FinishWorkflowRun(ctx, f.scope.TenantID, run.ID, store.WorkflowStatusSucceeded, nil, nil); err != nil {
				t.Fatal(err)
			}
		}
		if err = c.Tick(ctx, f.scope); err != nil {
			t.Fatal(err)
		}
		return run
	}
	old := trigger(latest, "before", f.v1.ID)
	if _, err := f.db.UpdateWorkflowDraft(ctx, f.scope.TenantID, f.workflow.ID, `{"nodes":[],"edges":[],"note":"new release"}`); err != nil {
		t.Fatal(err)
	}
	v2, err := f.db.PublishWorkflow(ctx, f.scope.TenantID, f.workflow.ID, "v2", func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	trigger(latest, "after", v2.ID)
	trigger(fixed, "pinned", f.v1.ID)
	// Reusing the old request key must never resolve a different release.
	trigger(latest, "before", f.v1.ID)
	frozen, err := f.db.WorkflowRun(ctx, f.scope.TenantID, old.ID)
	if err != nil || frozen.ID != old.ID || frozen.SnapshotID != f.v1.ID {
		t.Fatalf("history changed: %+v %v", frozen, err)
	}
	// Both scheduled and manual admissions use the same adapter and atomic resolution.
	now = fixed.CreatedAt.Add(time.Hour + time.Second)
	if err = c.Tick(ctx, f.scope); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		config   store.ScheduleConfig
		snapshot string
	}{{latest, v2.ID}, {fixed, f.v1.ID}} {
		history, err := f.db.ScheduleHistory(ctx, f.scope.TenantID, f.scope.UserID, tc.config.ID, store.OccurrenceCursor{}, 20)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, o := range history {
			if o.Kind == "scheduled" {
				found = true
				run, err := f.db.ScheduledWorkflowRun(ctx, f.scope.TenantID, o.ID)
				if err != nil || run.SnapshotID != tc.snapshot {
					t.Fatalf("timer snapshot = %s want %s: %v", run.SnapshotID, tc.snapshot, err)
				}
			}
		}
		if !found {
			t.Fatal("timer occurrence missing")
		}
	}
	// The host sees two additional starts at the timer tick, one for each configuration.
	if len(f.host.scopes) != 5 {
		t.Fatalf("starts = %d, want 5", len(f.host.scopes))
	}
}

func TestScheduledLatestDeletedWorkflowIsSkipped(t *testing.T) {
	f := newWorkflowTargetFixture(t)
	ctx := context.Background()
	raw, _ := json.Marshal(map[string]any{"kind": "workflow", "workflowId": f.workflow.ID, "versionPolicy": "latest", "input": ""})
	config, err := f.db.CreateScheduleConfig(ctx, f.scope.TenantID, f.scope.UserID, store.ScheduleInput{Name: "失效计划", ProjectID: f.project.ID, Target: raw, Rule: json.RawMessage(`{"kind":"hours","hours":1,"timezone":"UTC"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.db.DeleteWorkflow(ctx, f.scope.TenantID, f.workflow.ID); err != nil {
		t.Fatal(err)
	}
	c, err := server.NewScheduleCoordinator(f.db, f.options(true), func() time.Time { return config.CreatedAt.Add(time.Minute) })
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Recover(ctx, f.scope); err != nil {
		t.Fatal(err)
	}
	o, _ := c.Manual(ctx, f.scope, config.ID, "gone")
	o, err = f.db.GetOccurrence(ctx, f.scope.TenantID, f.scope.UserID, o.ID)
	if err != nil || o.Status != "skipped" || o.Reason != "workflow_unavailable" || !o.Released || len(f.host.scopes) != 0 {
		t.Fatalf("unavailable: %+v %v starts=%d", o, err, len(f.host.scopes))
	}
}

func TestLatestScheduleRemainsOwnerScoped(t *testing.T) {
	f := newWorkflowTargetFixture(t)
	call := f.client(t, f.options(true))
	target := map[string]any{"kind": "workflow", "workflowId": f.workflow.ID, "versionPolicy": "latest", "input": ""}
	w := call("POST", "/schedule-configs", f.config(f.project.ID, target))
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	var saved store.ScheduleConfig
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	f.token = storetest.SignIn(t, f.db, "other-scheduler", "Other", store.RoleMember)
	w = call("GET", "/schedule-configs", nil)
	var list struct {
		Items []store.ScheduleConfig `json:"items"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Items) != 0 {
		t.Fatalf("other owner's list: %d %s", w.Code, w.Body)
	}
	in := f.config(f.project.ID, target)
	in["version"] = saved.Version
	if w = call("PUT", "/schedule-configs/"+saved.ID, in); w.Code != 404 {
		t.Fatalf("other owner's update: %d %s", w.Code, w.Body)
	}
	member, err := f.db.SessionUser(context.Background(), f.token)
	if err != nil {
		t.Fatal(err)
	}
	c, err := server.NewScheduleCoordinator(f.db, f.options(true), func() time.Time { return saved.CreatedAt.Add(time.Minute) })
	if err != nil {
		t.Fatal(err)
	}
	scope := execution.Scope{TenantID: f.scope.TenantID, UserID: member.ID}
	if err = c.Recover(context.Background(), scope); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Manual(context.Background(), scope, saved.ID, "foreign"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign manual: %v", err)
	}
	if len(f.host.scopes) != 0 {
		t.Fatal("foreign trigger started a workflow")
	}
}

// lostWorkflowStart simulates a committed start whose response never reaches the scheduler.
// Recovery must look up that run, even if the effective release changed in the meantime.
type lostWorkflowStart struct{ *storeRunHost }

func (h lostWorkflowStart) StartRun(ctx context.Context, scope server.WorkflowRunScope, id string) (store.WorkflowRun, error) {
	run, err := h.storeRunHost.StartRun(ctx, scope, id)
	if err != nil {
		return run, err
	}
	return store.WorkflowRun{}, context.DeadlineExceeded
}

func TestLatestWorkflowRecoveryAfterRepublishKeepsOriginalRun(t *testing.T) {
	f := newWorkflowTargetFixture(t)
	ctx := context.Background()
	raw, _ := json.Marshal(map[string]any{"kind": "workflow", "workflowId": f.workflow.ID, "versionPolicy": "latest", "input": "原需求"})
	config, err := f.db.CreateScheduleConfig(ctx, f.scope.TenantID, f.scope.UserID, store.ScheduleInput{Name: "恢复计划", ProjectID: f.project.ID, Target: raw, Rule: json.RawMessage(`{"kind":"hours","hours":1,"timezone":"UTC"}`)})
	if err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return config.CreatedAt.Add(time.Minute) }
	options := f.options(true)
	options.WorkflowRuns = lostWorkflowStart{f.host}
	coordinator, err := server.NewScheduleCoordinator(f.db, options, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = coordinator.Recover(ctx, f.scope); err != nil {
		t.Fatal(err)
	}
	occurrence, err := coordinator.Manual(ctx, f.scope, config.ID, "lost-start-response")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected ambiguous start: %v", err)
	}
	occurrence, err = f.db.GetOccurrence(ctx, f.scope.TenantID, f.scope.UserID, occurrence.ID)
	if err != nil || occurrence.Status != "reconciling" || occurrence.ExecutionRef != "" {
		t.Fatalf("ambiguous occurrence: %+v %v", occurrence, err)
	}
	original, err := f.db.ScheduledWorkflowRun(ctx, f.scope.TenantID, occurrence.ID)
	if err != nil || original.SnapshotID != f.v1.ID || original.Status != store.WorkflowStatusRunning {
		t.Fatalf("original: %+v %v", original, err)
	}
	if _, err = f.db.UpdateWorkflowDraft(ctx, f.scope.TenantID, f.workflow.ID, `{"nodes":[],"edges":[],"note":"new release"}`); err != nil {
		t.Fatal(err)
	}
	v2, err := f.db.PublishWorkflow(ctx, f.scope.TenantID, f.workflow.ID, "v2", func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	// Rebuild only the coordinator, discarding its in-process knowledge of the lost
	// response. The execution bridge still owns the original running execution.
	recovered, err := server.NewScheduleCoordinator(f.db, f.options(true), now)
	if err != nil {
		t.Fatal(err)
	}
	if err = recovered.Recover(ctx, f.scope); err != nil {
		t.Fatal(err)
	}
	adopted, err := f.db.GetOccurrence(ctx, f.scope.TenantID, f.scope.UserID, occurrence.ID)
	if err != nil || adopted.Status != "running" || adopted.ExecutionRef != `{"workflowRunId":"`+original.ID+`"}` || len(f.host.scopes) != 1 {
		t.Fatalf("recovery did not adopt the original execution: %+v %v starts=%d", adopted, err, len(f.host.scopes))
	}
	if _, err = f.db.FinishWorkflowRun(ctx, f.scope.TenantID, original.ID, store.WorkflowStatusSucceeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err = recovered.Tick(ctx, f.scope); err != nil {
		t.Fatal(err)
	}
	settled, err := f.db.GetOccurrence(ctx, f.scope.TenantID, f.scope.UserID, occurrence.ID)
	if err != nil || settled.Status != "succeeded" || !settled.Released || settled.ExecutionRef != `{"workflowRunId":"`+original.ID+`"}` {
		t.Fatalf("recovered occurrence: %+v %v", settled, err)
	}
	frozen, err := f.db.ScheduledWorkflowRun(ctx, f.scope.TenantID, occurrence.ID)
	if err != nil || frozen.ID != original.ID || frozen.SnapshotID != f.v1.ID || len(f.host.scopes) != 1 {
		t.Fatalf("recovery changed execution: %+v %v starts=%d", frozen, err, len(f.host.scopes))
	}
	snapshot, err := f.db.WorkflowSnapshot(ctx, f.scope.TenantID, f.workflow.ID, frozen.SnapshotID)
	if err != nil || snapshot.Graph != f.v1.Graph {
		t.Fatalf("recovered graph changed: %+v %v", snapshot, err)
	}
	// Only an explicitly new trigger resolves the new release.
	next, err := recovered.Manual(ctx, f.scope, config.ID, "new-after-recovery")
	if err != nil {
		t.Fatal(err)
	}
	nextRun, err := f.db.ScheduledWorkflowRun(ctx, f.scope.TenantID, next.ID)
	if err != nil || nextRun.ID == original.ID || nextRun.SnapshotID != v2.ID || len(f.host.scopes) != 2 {
		t.Fatalf("new trigger: %+v %v starts=%d", nextRun, err, len(f.host.scopes))
	}
}
